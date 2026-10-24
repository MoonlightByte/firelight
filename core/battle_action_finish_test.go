package core_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func ordinaryOpenFixture() core.Blueprint {
	b := turnResourceFixture()
	bonus := b.AbilityDefinitions[0]
	bonus.ID = "bonus"
	// Rebuild the first step so neither definition shares a mutable cost operand.
	bonus.Steps = append([]core.AbilityStep(nil), bonus.Steps...)
	bonus.Steps[0] = core.AbilityStep{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Parameter: "actor"}, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}}
	b.AbilityDefinitions = append(b.AbilityDefinitions, bonus)
	b.Battle.Rules.Actions = append(b.Battle.Rules.Actions, "bonus")
	b.AbilityDefinitions[0].Action = &core.ActionPolicy{Actor: core.AbilityOperand{Parameter: "actor"}, Phase: core.EitherPhase, Finish: gearPtr(false)}
	return b
}

func TestBattleOrdinaryFinishOpenThenBonusAndReplay(t *testing.T) {
	b := ordinaryOpenFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	r := battleExec(t, w, "ordinary", "a", battleInvoke("care", "a", "a"))
	s := w.Snapshot()
	e := s.Battle.State.Encounters[0]
	if !e.ActionUsed || e.Cursor != 0 || e.Round != 1 || s.Clock.Tick != 0 || s.Characters[0].Resources["hp"].Current != 7 || s.Characters[0].Resources["turn:bonus"].Current != 1 {
		t.Fatal("ordinary action must spend once but preserve window/bonus", s)
	}
	battleReject(t, w, gearRequest(w, "another-normal", battleInvoke("wait", "a", "")))
	battleExec(t, w, "bonus", "a", battleInvoke("bonus", "a", "a"))
	s = w.Snapshot()
	if !s.Battle.State.Encounters[0].ActionUsed || s.Battle.State.Encounters[0].Cursor != 0 || s.Characters[0].Resources["hp"].Current != 9 || s.Characters[0].Resources["turn:bonus"].Current != 0 {
		t.Fatal("bonus lost prior ordinary action truth", s)
	}
	battleExec(t, w, "end", "a", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: "a"}}})
	if w.Snapshot().Battle.State.Encounters[0].Cursor != 1 {
		t.Fatal("explicit end did not advance")
	}
	cp := w.ExportCheckpoint()
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, cp, nativesave.SessionState{Campaign: "action-finish", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), cp) {
		t.Fatal("historical replay changed the window", err)
	}
	*b.AbilityDefinitions[0].Action.Finish = true
	for _, d := range s.AbilityDefinitions {
		if d.Action.Finish != nil {
			*d.Action.Finish = true
		}
	}
	if !reflect.DeepEqual(w.ExportCheckpoint(), cp) {
		t.Fatal("Finish pointer alias")
	}
}

func TestBattleOrdinaryFinishRollbackTerminalAndPolicyShape(t *testing.T) {
	b := ordinaryOpenFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "late", battleInvoke("care", "a", "a"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 2}}))
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Parameter: "actor"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(5))}}}}}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "down", "a", battleInvoke("care", "a", "a"))
	if w.Snapshot().Battle.State.Encounters[0].Status != core.Defeat {
		t.Fatal("false finish retained defeated encounter")
	}
	for _, incompatible := range []bool{false, true} {
		b := ordinaryOpenFixture()
		if incompatible {
			b.AbilityDefinitions[0].Action.TurnCost = &core.TurnCost{Resource: "turn:bonus", Amount: 1}
		} else {
			b.Battle = nil
		}
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("Finish admitted without Battle or with TurnCost")
		}
	}
}
