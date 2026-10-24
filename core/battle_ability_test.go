package core_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func battleAbilityFixture() core.Blueprint {
	b := battleFixture()
	actor := core.AbilityOperand{Parameter: "actor"}
	target := core.AbilityOperand{Parameter: "target"}
	integer := func(v int64) core.AbilityOperand {
		return core.AbilityOperand{Literal: &core.AbilityValue{Integer: &v}}
	}
	b.Characters[0].Resources["focus"] = core.Resource{Current: 3, Maximum: 3}
	b.Characters[0].Resources["hp"] = core.Resource{Current: 5, Maximum: 30}
	b.AbilityDefinitions = []core.AbilityDefinition{{ID: "care", Parameters: []core.AbilityParameter{{ID: "actor", Kind: core.CharacterArgument}, {ID: "target", Kind: core.CharacterArgument}},
		Action: &core.ActionPolicy{Actor: actor, Phase: core.EitherPhase, Requirements: []core.Requirement{{Kind: core.Allied, Subject: actor, Other: &target}, {Kind: core.Colocated, Subject: actor, Other: &target}}},
		Steps:  []core.AbilityStep{{Spend: &core.SpendTemplate{Payer: actor, Resource: "focus", Amount: integer(2)}}, {Heal: &core.HealTemplate{Target: target, Resource: "hp", Amount: integer(4)}}}},
		{ID: "wait", Parameters: []core.AbilityParameter{{ID: "actor", Kind: core.CharacterArgument}}, Action: &core.ActionPolicy{Actor: actor, Phase: core.EncounterPhase}, Steps: []core.AbilityStep{}},
	}
	b.Battle.Rules.Actions = []core.AbilityID{"care", "wait"}
	return b
}

func battleInvoke(id core.AbilityID, actor, target core.CharacterID) core.Operation {
	v := &core.InvokeAbility{Ability: id, Arguments: []core.AbilityArgument{{Name: "actor", Value: core.AbilityValue{Character: &actor}}}}
	if target != "" {
		v.Arguments = append(v.Arguments, core.AbilityArgument{Name: "target", Value: core.AbilityValue{Character: &target}})
	}
	return core.Operation{Invoke: v}
}

func TestBattleAbilityTransactionHistoryAndDetachment(t *testing.T) {
	b := battleAbilityFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	r := battleExec(t, w, "care", "a", battleInvoke("care", "a", "a"))
	s := w.Snapshot()
	if w.Revision() != 2 || s.Characters[0].Resources["focus"].Current != 1 || s.Characters[0].Resources["hp"].Current != 9 || s.Clock.Tick != 0 || s.Battle.State.Encounters[0].Cursor != 1 || s.Battle.State.Encounters[0].ActionUsed {
		t.Fatal("fixed payment/heal/turn oracle", s)
	}
	if len(r.Events) != 3 || r.Events[0].Kind != core.ResourceSpent || r.Events[1].Kind != core.HealingApplied || r.Events[2].Kind != core.AbilityInvoked || r.Events[2].BattleAction == nil || !reflect.DeepEqual(r.Request.Operations[0].Invoke, battleInvoke("care", "a", "a").Invoke) {
		t.Fatal("lost ordered consequences/original invocation", r)
	}
	battleExec(t, w, "wait", "b", battleInvoke("wait", "b", ""))
	if w.Snapshot().Clock.Tick != 6 || w.Snapshot().Battle.State.Encounters[0].Round != 2 {
		t.Fatal("empty action did not settle one turn")
	}
	before := w.ExportCheckpoint()
	// Current funds cannot pay care again; historical success must still return.
	retry, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("historical retry", err)
	}
	battleReject(t, w, gearRequest(w, "new-care", battleInvoke("care", "a", "a")))
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "battle-ability", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("archive/replay", err)
	}
	retry, err = loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) {
		t.Fatal("reconstructed retry", err)
	}
	// Mutating input, snapshots and returned event rosters cannot alter authority.
	b.Battle.Rules.Actions[0] = "forged"
	s.Battle.Rules.Actions[0] = "forged"
	r.Events[2].BattleAction.After.Roster[0].Admission.Character = "forged"
	*r.Events[2].BattleAction.Before.Roster[0].Admission.Initiative = 1
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested alias")
	}
	history := w.History()
	history[1].Events[2].BattleAction.After.Cursor = 0
	if bad, err := core.Replay(battleAbilityFixture(), history); bad != nil || err == nil {
		t.Fatal("inconsistent turn history accepted")
	}
}

func TestBattleAbilityEligibilityAndLateRollback(t *testing.T) {
	w := gearWorld(t, battleAbilityFixture())
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "wrong-actor", battleInvoke("care", "b", "b")))
	battleReject(t, w, core.Request{ID: "wrong-turn", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{battleInvoke("wait", "b", "")}})
	battleReject(t, w, gearRequest(w, "not-allied", battleInvoke("care", "a", "b")))
	battleReject(t, w, gearRequest(w, "late", battleInvoke("care", "a", "a"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 2}}))
	battleExec(t, w, "miss-no-finish", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, false))
	battleReject(t, w, gearRequest(w, "used", battleInvoke("care", "a", "a")))
}

func TestBattleAbilityCheckRollbackAndCommittedMiss(t *testing.T) {
	b := battleAbilityFixture()
	d := &b.AbilityDefinitions[1]
	d.Steps = []core.AbilityStep{{Check: &core.CheckTemplate{ID: "bounded-check", Sides: 20, Modifier: core.CheckValue{Literal: gearPtr(int64(0))}, Threshold: core.CheckValue{Literal: gearPtr(int64(21))}}}}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "rolled-then-failed", battleInvoke("wait", "a", ""), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 4}}))
	r := battleExec(t, w, "miss", "a", battleInvoke("wait", "a", ""))
	if len(r.Events) != 2 || r.Events[0].Check == nil || r.Events[0].Check.Success || r.Events[1].BattleAction.After.Cursor != 1 {
		t.Fatal("miss must commit and end turn", r)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("check replay", err)
	}
	b.AbilityDefinitions[1].Action = nil
	b.Battle.Rules.Actions = []core.AbilityID{"care"}
	w = gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "legacy-check-still-rejects", battleInvoke("wait", "a", "")))
	b = battleAbilityFixture()
	b.AbilityDefinitions[1].Steps = []core.AbilityStep{{Check: &core.CheckTemplate{ID: "overflow", Sides: 20, Modifier: core.CheckValue{Literal: gearPtr(int64(math.MaxInt64))}, Threshold: core.CheckValue{Literal: gearPtr(int64(1))}}}}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "overflow", battleInvoke("wait", "a", "")))
}

func TestBattleAbilityTerminalClockAndStableRecovery(t *testing.T) {
	b := battleAbilityFixture()
	// Put the protected actor last and finish the encounter there. Terminal time
	// must be charged once, without an additional roster-wrap charge.
	b.Characters[0].Stats["initiative"] = -100
	b.AbilityDefinitions[0].Action.Requirements = nil
	b.AbilityDefinitions[0].Steps[1] = core.AbilityStep{Damage: &core.DamageTemplate{Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(30))}}}}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "wait", "b", battleInvoke("wait", "b", ""))
	r := battleExec(t, w, "finish", "a", battleInvoke("care", "a", "b"))
	if w.Snapshot().Clock.Tick != 6 || r.Events[len(r.Events)-1].BattleAction.After.Status != core.Victory {
		t.Fatal("terminal turn clock", r)
	}
	b = battleAbilityFixture()
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "b"}, Kind: "party-member", To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Strength: 1})
	w = gearWorld(t, b)
	battleExec(t, w, "down", "a", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 30}})
	battleReject(t, w, core.Request{ID: "down-actor", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{battleInvoke("care", "b", "b")}})
	battleExec(t, w, "recover", "a", battleInvoke("care", "a", "b"))
	if len(w.Snapshot().Battle.State.Life) != 0 || w.Snapshot().Characters[1].Resources["hp"].Current != 4 || w.Snapshot().Clock.Tick != 0 {
		t.Fatal("stable-down ally healing")
	}
}
