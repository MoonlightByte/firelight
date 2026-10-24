package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func removalAbilityFixture() core.Blueprint {
	b := gearFixture()
	b.Characters[0].Resources["focus"] = core.Resource{Current: 2, Maximum: 2}
	b.Conditions = append(b.Conditions,
		core.ConditionInstance{ID: "one", Type: "strong", Target: "a"},
		core.ConditionInstance{ID: "two", Type: "strong", Target: "a"},
		core.ConditionInstance{ID: "child", Type: "guarded", Target: "b", Anchor: gearPtr(core.ConditionInstanceID("one"))})
	b.AbilityDefinitions = append(b.AbilityDefinitions, core.AbilityDefinition{ID: "retrain", Name: "Retrain", Parameters: []core.AbilityParameter{{ID: "old", Kind: core.ConditionRefArgument}}, Steps: []core.AbilityStep{
		{RemoveCondition: &core.ConditionRemovalTemplate{Instance: core.AbilityOperand{Parameter: "old"}, Type: "strong", Target: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}}},
	}})
	return b
}
func removeInvocation(id core.ConditionInstanceID) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "retrain", Arguments: []core.AbilityArgument{{Name: "old", Value: core.AbilityValue{ConditionRef: &id}}}}}
}
func TestConditionRefRemovalAtomicReplayAndDetachment(t *testing.T) {
	b := removalAbilityFixture()
	w := gearWorld(t, b)
	for _, id := range []core.ConditionInstanceID{"absent", "a-martial", "b-martial"} {
		gearReject(t, w, "reject", removeInvocation(id))
	}
	op := removeInvocation("one")
	receipt := gearExecute(t, w, "selected", op)
	if len(receipt.Events) != 3 || receipt.Events[0].Condition.Before.ID != "child" || receipt.Events[1].Condition.Before.ID != "one" || receipt.Events[1].Condition.ExpectedType != "strong" || receipt.Events[2].Kind != core.AbilityInvoked {
		t.Fatal(receipt)
	}
	x, err := w.Explain("a", "strength")
	if err != nil || x.Effective != 18 {
		t.Fatal(x, err)
	}
	gearDefense(t, w, "b", 13)
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := rebuilt.Execute(receipt.Request)
	if err != nil || !reflect.DeepEqual(receipt, retry) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("retry mutated", err)
	}
	*op.Invoke.Arguments[0].Value.ConditionRef = "two"
	changed := receipt.Request
	changed.Operations = []core.Operation{op}
	gearReject(t, w, "selected", changed.Operations...)
	*receipt.Request.Operations[0].Invoke.Arguments[0].Value.ConditionRef = "two"
	snap := w.Snapshot()
	snap.AbilityDefinitions[len(snap.AbilityDefinitions)-1].Steps[0].RemoveCondition.Target.Literal.Character = gearPtr(core.CharacterID("b"))
	hist := w.History()
	*hist[0].Request.Operations[0].Invoke.Arguments[0].Value.ConditionRef = "two"
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("nested removal alias")
	}
	if partial, e := core.Replay(b, hist); partial != nil || e == nil {
		t.Fatal("inconsistent removed identity replayed")
	}
}
func TestConditionRefRemovalRollbackTypeAndTarget(t *testing.T) {
	b := removalAbilityFixture()
	b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(3))}}}})
	w := gearWorld(t, b)
	gearReject(t, w, "late", removeInvocation("one"))
	for _, v := range []core.RemoveCondition{{Instance: "one", Target: "b", Type: "strong"}, {Instance: "one", Target: "a", Type: "guarded"}} {
		gearReject(t, w, "guard", core.Operation{RemoveCondition: &v})
	}
	// Optional guard stays part of direct request identity; legacy omission stays omitted.
	for _, typ := range []core.ConditionTypeID{"", "strong"} {
		w = gearWorld(t, removalAbilityFixture())
		r := gearExecute(t, w, "direct", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "one", Target: "a", Type: typ}})
		cp := w.ExportCheckpoint()
		rebuilt, err := core.Replay(cp.Genesis, cp.History)
		if err != nil {
			t.Fatal(err)
		}
		retry, err := rebuilt.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(r, retry) {
			t.Fatal("direct guarded identity", err)
		}
	}
}
func TestConditionRefClosedSumAndLiteralDetachment(t *testing.T) {
	b := removalAbilityFixture()
	id := core.ConditionInstanceID("one")
	b.AbilityDefinitions[0].Steps[0].RemoveCondition.Instance = core.AbilityOperand{Literal: &core.AbilityValue{ConditionRef: &id}}
	w := gearWorld(t, b)
	id = "two"
	r := gearExecute(t, w, "literal", removeInvocation("two"))
	if r.Events[1].Condition.Before.ID != "one" {
		t.Fatal("blueprint literal alias")
	}
	bad := removeInvocation("two")
	bad.Invoke.Arguments[0].Value.ConditionID = gearPtr(core.ConditionInstanceID("new"))
	gearReject(t, w, "sum", bad)
	b = removalAbilityFixture()
	b.AbilityDefinitions[0].Steps[0].Heal = &core.HealTemplate{}
	if _, e := core.NewWorld(b); e == nil {
		t.Fatal("multi-variant template accepted")
	}
	b = removalAbilityFixture()
	b.AbilityDefinitions[0].Parameters[0].Kind = core.ConditionIDArgument
	if _, e := core.NewWorld(b); e == nil {
		t.Fatal("creation kind used as reference")
	}
}

func TestConditionRefRemovalInCheckBranch(t *testing.T) {
	for _, face := range []int64{6, 7} {
		b := actorCheckFixture()
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "one", Type: "strong", Target: "a"})
		b.AbilityDefinitions[0].Steps[1].Check.Success = append(b.AbilityDefinitions[0].Steps[1].Check.Success, core.AbilityPrimitive{RemoveCondition: &core.ConditionRemovalTemplate{Instance: core.AbilityOperand{Literal: &core.AbilityValue{ConditionRef: gearPtr(core.ConditionInstanceID("one"))}}, Type: "strong", Target: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}}})
		w := gearWorld(t, b)
		op := battleInvoke("care", "a", "a")
		op.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{face}}}
		r := gearExecute(t, w, "branch", op)
		removed := false
		for _, e := range r.Events {
			if e.Kind == core.ConditionRemoved {
				removed = true
				if !reflect.DeepEqual(e.AbilityOrigin.Path, []int{0, 1}) {
					t.Fatal("lost branch origin", e.AbilityOrigin)
				}
			}
		}
		if removed != (face == 7) {
			t.Fatal("wrong branch removal", face, r)
		}
		cp := w.ExportCheckpoint()
		if rebuilt, e := core.Replay(cp.Genesis, cp.History); e != nil || rebuilt == nil {
			t.Fatal(e)
		}
	}
}
