package core_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func extraActionFixture() core.Blueprint {
	b := battleAbilityFixture()
	b.Battle.Rules.TurnResources = []core.ResourceID{"turn:surge"}
	b.Battle.Rules.Allowances = []core.BattleAllowance{{Resource: "extra", Weapon: true, Actions: []core.AbilityID{"care"}}}
	b.Characters[0].Resources["turn:surge"] = core.Resource{Maximum: 1}
	b.Characters[0].Resources["surge"] = core.Resource{Current: 1, Maximum: 1}
	b.Characters[0].Resources["extra"] = core.Resource{Maximum: 1}
	b.AbilityDefinitions[0].Action.Finish = gearPtr(false)
	actor := core.AbilityOperand{Parameter: "actor"}
	b.AbilityDefinitions = append(b.AbilityDefinitions, core.AbilityDefinition{ID: "surge", Parameters: []core.AbilityParameter{{ID: "actor", Kind: core.CharacterArgument}},
		Action: &core.ActionPolicy{Actor: actor, Phase: core.EncounterPhase, TurnCost: &core.TurnCost{Resource: "turn:surge", Amount: 1}},
		Steps:  []core.AbilityStep{{Spend: &core.SpendTemplate{Payer: actor, Resource: "surge", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}}, {Restore: &core.RestoreTemplate{Target: actor, Resource: "extra"}}}})
	b.Battle.Rules.Actions = append(b.Battle.Rules.Actions, "surge")
	return b
}

func TestBattleAllowanceWeaponAndHistoricalReceipt(t *testing.T) {
	b := extraActionFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "ordinary-miss", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, false))
	surge := battleExec(t, w, "surge", "a", battleInvoke("surge", "a", ""))
	extra := battleAttack(t, w, "a", "b", []int64{1}, nil, false)
	extra.WeaponAttack.Allowance = "extra"
	battleReject(t, w, gearRequest(w, "late", extra, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "surge", Amount: 1}}))
	bad := battleAttack(t, w, "a", "b", []int64{21}, nil, false)
	bad.WeaponAttack.Allowance = "extra"
	battleReject(t, w, gearRequest(w, "faces", bad))
	r := battleExec(t, w, "extra-miss", "a", extra)
	s := w.Snapshot()
	if !s.Battle.State.Encounters[0].ActionUsed || s.Battle.State.Encounters[0].Cursor != 0 || s.Characters[0].Resources["extra"].Current != 0 || s.Characters[0].Resources["surge"].Current != 0 || s.Characters[1].Resources["hp"].Current != 30 || len(r.Events) != 2 || r.Events[0].Kind != core.ResourceSpent || r.Events[1].WeaponAttack.Intent.Allowance != "extra" {
		t.Fatal("extra miss spends exactly one allowance, preserves spent normal action", r)
	}
	battleReject(t, w, gearRequest(w, "second-extra", extra))
	battleReject(t, w, gearRequest(w, "second-normal", battleAttack(t, w, "a", "b", []int64{1}, nil, false)))
	battleReject(t, w, gearRequest(w, "second-surge", battleInvoke("surge", "a", "")))
	want := w.ExportCheckpoint()
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, want, nativesave.SessionState{Campaign: "extra", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(want, loaded.World.ExportCheckpoint()) {
		t.Fatal("complete reconstruction", err)
	}
	for _, world := range []*core.World{w, loaded.World} {
		for _, original := range []core.Batch{surge, r} {
			retry, err := world.Execute(original.Request)
			if err != nil || !reflect.DeepEqual(original, retry) || !reflect.DeepEqual(want, world.ExportCheckpoint()) {
				t.Fatal("historical exact receipt", err)
			}
		}
		changed := r.Request
		changed.Operations = []core.Operation{extra}
		changed.Operations[0].WeaponAttack = gearPtr(*extra.WeaponAttack)
		changed.Operations[0].WeaponAttack.Allowance = ""
		battleReject(t, world, changed)
	}
	b.Battle.Rules.Allowances[0].Actions[0] = "forged"
	s.Battle.Rules.Allowances[0].Actions[0] = "forged"
	if !reflect.DeepEqual(want, w.ExportCheckpoint()) {
		t.Fatal("nested allowance rules aliased")
	}
	history := w.History()
	history[len(history)-1].Events[0].Resource.After.Current = 1
	if bad, err := core.Replay(extraActionFixture(), history); bad != nil || err == nil {
		t.Fatal("forged payment accepted")
	}
}

func TestBattleAllowanceAbilityPreviewAndExpiry(t *testing.T) {
	b := extraActionFixture()
	w := gearWorld(t, b)
	care := battleInvoke("care", "a", "a")
	care.Invoke.Allowance = "extra"
	battleReject(t, w, gearRequest(w, "exploration", care))
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "surge", "a", battleInvoke("surge", "a", ""))
	wrong := battleInvoke("wait", "a", "")
	wrong.Invoke.Allowance = "extra"
	battleReject(t, w, gearRequest(w, "not-granted", wrong))
	r := battleExec(t, w, "extra-care", "a", care)
	if len(r.Events) != 4 || r.Events[0].Resource.Resource != "extra" || r.Events[1].AbilityOrigin.Step != 0 || r.Events[3].Ability.Allowance != "extra" || w.Snapshot().Battle.State.Encounters[0].ActionUsed || w.Snapshot().Characters[0].Resources["hp"].Current != 9 {
		t.Fatal("explicit extra action preserves unused normal", r)
	}
	battleExec(t, w, "restore-extra", "a", core.Operation{Restore: &core.RestoreResource{Character: "a", Resource: "extra"}})
	battleExec(t, w, "ordinary", "a", battleInvoke("wait", "a", ""))
	if w.Snapshot().Characters[0].Resources["extra"].Current != 0 {
		t.Fatal("unused allowance survived turn end")
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("expiry replay", err)
	}

	// Real human input uses the payment candidate before asking for faces.
	b = extraActionFixture()
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	op := battleAttack(t, w, "a", "b", nil, nil, false)
	op.WeaponAttack.Allowance = "extra"
	before := w.ExportCheckpoint()
	if _, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("empty allowance asked for dice or changed state")
	}
	battleExec(t, w, "surge", "a", battleInvoke("surge", "a", ""))
	before = w.ExportCheckpoint()
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
	if err != nil || need.Stage != "attack" || need.Count != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("detached candidate preview", need, err)
	}
}

func TestBattleAllowanceRolledConsequencesAndTerminalClear(t *testing.T) {
	// Payment ends the source Condition before the existing roll evaluator.
	b := rolledCareFixture()
	b.Characters[0].Resources["extra"] = core.Resource{Maximum: 1}
	b.Battle.Rules.Allowances = []core.BattleAllowance{{Resource: "extra", Actions: []core.AbilityID{"care"}}}
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "while-extra", Instances: core.Unique, EndsAtMinimum: gearPtr(core.ResourceID("extra"))})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "grant", "a", core.Operation{Restore: &core.RestoreResource{Character: "a", Resource: "extra"}}, core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "grant-state", Type: "while-extra", Target: "a"}})
	rolled := rolledCare("a", "a")
	rolled.Invoke.Allowance = "extra"
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolled)
	if err != nil || need.Stage != "ability" || *need.AbilityStep != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("shared roll prefix", need, err)
	}
	rolled.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{4, 5}}}
	r := battleExec(t, w, "rolled", "a", rolled)
	if r.Events[0].Kind != core.ConditionRemoved || r.Events[1].Resource.Resource != "extra" || r.Events[4].ResourceRoll.Requested != 12 || w.Snapshot().Characters[0].Resources["hp"].Current != 17 {
		t.Fatal("recorded payment/condition/roll order", r)
	}
	// An unspent grant cannot escape a terminal turn, even with finish false.
	b = extraActionFixture()
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Damage: &core.DamageTemplate{Target: core.AbilityOperand{Parameter: "actor"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(5))}}}}}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "grant", "a", battleInvoke("surge", "a", ""))
	battleExec(t, w, "down", "a", battleInvoke("care", "a", "a"))
	if w.Snapshot().Battle.State.Encounters[0].Status != core.Defeat || w.Snapshot().Characters[0].Resources["extra"].Current != 0 {
		t.Fatal("terminal allowance survived")
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
		t.Fatal("terminal settlement replay", err)
	}
}

func TestBattleAllowanceRuleValidation(t *testing.T) {
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Battle.Rules.Allowances[0].Resource = "hp" },
		func(b *core.Blueprint) { b.Battle.Rules.Allowances[0].Resource = "missing" },
		func(b *core.Blueprint) { b.Battle.Rules.Allowances[0].Resource = "turn:surge" },
		func(b *core.Blueprint) { b.Battle.Rules.Allowances[0].Actions = []core.AbilityID{"surge"} },
		func(b *core.Blueprint) { b.Battle.Rules.Allowances[0].Actions = []core.AbilityID{"care", "care"} },
		func(b *core.Blueprint) {
			b.Battle.Rules.Allowances = append(b.Battle.Rules.Allowances, b.Battle.Rules.Allowances[0])
		},
		func(b *core.Blueprint) { b.Characters[0].Resources["extra"] = core.Resource{Minimum: -1, Maximum: 1} },
	} {
		b := extraActionFixture()
		mutate(&b)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid allowance admitted")
		}
	}
}
