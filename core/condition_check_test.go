package core_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func conditionCheckFixture() core.Blueprint {
	b := battleAbilityFixture()
	for i := range b.Characters {
		b.Characters[i].Stats["save"] = 2
		b.Characters[i].Resources["hp"] = core.Resource{Current: 200, Maximum: 200}
	}
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "maintained", Instances: core.Replace, EndsAtMinimum: gearPtr(core.ResourceID("hp"))})
	b.Conditions = append(b.Conditions,
		core.ConditionInstance{ID: "root-a", Type: "maintained", Target: "a"},
		core.ConditionInstance{ID: "child-b", Type: "guarded", Target: "b", Anchor: gearPtr(core.ConditionInstanceID("root-a"))},
		core.ConditionInstance{ID: "root-b", Type: "maintained", Target: "b"},
		core.ConditionInstance{ID: "child-a", Type: "guarded", Target: "a", Anchor: gearPtr(core.ConditionInstanceID("root-b"))})
	b.Battle.Rules.ConditionChecks = []core.ConditionCheckPolicy{{Condition: "maintained", Resource: "hp", Stat: "save", Sides: 20, Minimum: 10, Divisor: 2, Maximum: 30}}
	return b
}

func conditionDamage(target core.CharacterID, amount int64, faces ...int64) core.Operation {
	o := core.Operation{Damage: &core.ResourceOp{Character: target, Resource: "hp", Amount: amount}}
	for _, face := range faces {
		o.ConditionRolls = append(o.ConditionRolls, core.ConditionRollInput{Condition: "root-a", Target: target, Face: face})
	}
	return o
}

func TestConditionCheckCandidateThresholdsAndRemoval(t *testing.T) {
	for _, tc := range []struct {
		damage, threshold, face int64
		success                 bool
	}{
		{1, 10, 8, true}, {20, 10, 7, false}, {22, 11, 9, true}, {100, 30, 20, false},
	} {
		b := conditionCheckFixture()
		w := gearWorld(t, b)
		before := w.ExportCheckpoint()
		need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "b"}, conditionDamage("a", tc.damage))
		if err != nil || need.Stage != "condition" || need.Condition.Target != "a" || need.Condition.Threshold != tc.threshold || need.Condition.Damage != tc.damage || need.Condition.Modifier != 2 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("fixed candidate threshold / target owns manual save", need, err)
		}
		r := battleExec(t, w, "damage", "b", conditionDamage("a", tc.damage, tc.face))
		if r.Events[0].Kind != core.ConditionCheckResolved || r.Events[0].ConditionCheck.Roll.Success != tc.success || r.Events[len(r.Events)-1].Kind != core.DamageApplied || w.Snapshot().Characters[0].Resources["hp"].Current != 200-tc.damage {
			t.Fatal("damage/check ordered oracle", r)
		}
		present := map[core.ConditionInstanceID]bool{}
		for _, c := range w.Snapshot().Conditions {
			present[c.ID] = true
		}
		if present["root-a"] != tc.success || present["child-b"] != tc.success || !present["root-b"] || !present["child-a"] {
			t.Fatal("check removed wrong casting", present)
		}
		rebuilt, err := core.Replay(b, w.History())
		if err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("check replay", err)
		}
		cp := rebuilt.ExportCheckpoint()
		retry, err := rebuilt.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
			t.Fatal("replayed receipt re-executed", err)
		}
		b.Battle.Rules.ConditionChecks[0].Minimum = 99
		r.Request.Operations[0].ConditionRolls[0].Face = 1
		r.Events[0].ConditionCheck.Condition.ID = "forged"
		r.Events[0].ConditionCheck.Policy.Minimum = 99
		if !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
			t.Fatal("nested evidence alias")
		}
		history := w.History()
		history[0].Events[0].ConditionCheck.Roll.Draw++
		if bad, err := core.Replay(conditionCheckFixture(), history); bad != nil || err == nil {
			t.Fatal("forged save history accepted")
		}
	}
}

func TestConditionCheckNoFalseTriggersAndEvidenceAdmission(t *testing.T) {
	for _, op := range []core.Operation{
		conditionDamage("a", 200),
		{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}},
		{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}},
	} {
		w := gearWorld(t, conditionCheckFixture())
		r := battleExec(t, w, "no-save", "b", op)
		for _, e := range r.Events {
			if e.ConditionCheck != nil {
				t.Fatal("false check on floor/spend/heal", r)
			}
		}
	}
	w := gearWorld(t, conditionCheckFixture())
	err := battleReject(t, w, gearRequest(w, "missing", conditionDamage("a", 1)))
	var required *core.InputRequired
	if !errors.As(err, &required) || required.Need.Condition.Target != "a" {
		t.Fatal("missing typed need", err)
	}
	for _, mutate := range []func(*core.Operation){
		func(o *core.Operation) { o.ConditionRolls[0].Target = "b" },
		func(o *core.Operation) { o.ConditionRolls[0].Condition = "root-b" },
		func(o *core.Operation) { o.ConditionRolls[0].Face = 21 },
		func(o *core.Operation) { o.ConditionRolls = append(o.ConditionRolls, o.ConditionRolls[0]) },
		func(o *core.Operation) { o.Damage.Amount = 200 },
		func(o *core.Operation) { o.Heal = o.Damage; o.Damage = nil },
	} {
		o := conditionDamage("a", 1, 8)
		mutate(&o)
		battleReject(t, w, gearRequest(w, "bad", o))
	}
	// Opponent-controlled target uses the same recorded RNG, no supplied face.
	r := battleExec(t, w, "npc-save", "a", conditionDamage("b", 1))
	if r.Events[0].ConditionCheck.Source != core.AgentController || r.Events[0].ConditionCheck.Roll.CallsAfter <= r.Events[0].ConditionCheck.Roll.CallsBefore {
		t.Fatal("NPC save did not record RNG", r)
	}
	if rebuilt, err := core.Replay(conditionCheckFixture(), w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("NPC check replay", err)
	}
}

func TestConditionCheckSequentialAbilityEvidenceAndLateFailure(t *testing.T) {
	b := conditionCheckFixture()
	actor := core.AbilityOperand{Parameter: "actor"}
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{
		{Damage: &core.DamageTemplate{Target: actor, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}},
		{Damage: &core.DamageTemplate{Target: actor, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}},
	}
	w := gearWorld(t, b)
	op := battleInvoke("care", "a", "a")
	op.ConditionRolls = []core.ConditionRollInput{{Condition: "root-a", Target: "a", Face: 8}}
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
	if err != nil || need.Stage != "condition" || need.Condition.Index != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("sequential pending prefix", need, err)
	}
	op.ConditionRolls = append(op.ConditionRolls, core.ConditionRollInput{Condition: "root-a", Target: "a", Face: 7})
	battleExec(t, w, "two-checks", "a", op)
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("ordered response reconstruction", err)
	}
	b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{Spend: &core.SpendTemplate{Payer: actor, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(99))}}}})
	w = gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "accepted-faces-late-failure", op))
	if _, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op); err == nil {
		t.Fatal("full candidate query missed late failure")
	}
}
