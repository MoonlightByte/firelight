package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func actorCheckFixture() core.Blueprint {
	b := battleAbilityFixture()
	d := &b.AbilityDefinitions[0]
	heal := d.Steps[1].Heal
	d.Steps[1] = core.AbilityStep{Check: &core.CheckTemplate{ID: "skill", ActorRoll: true, Sides: 20,
		Modifier: core.CheckValue{Literal: gearPtr(int64(3))}, Threshold: core.CheckValue{Literal: gearPtr(int64(10))},
		Success: []core.AbilityPrimitive{{Heal: heal}}, Failure: []core.AbilityPrimitive{}}}
	return b
}

func TestActorCheckHumanBranchInputAndHistoricalRetry(t *testing.T) {
	for _, face := range []int64{6, 7} {
		b := actorCheckFixture()
		w := gearWorld(t, b)
		op := battleInvoke("care", "a", "a")
		before := w.ExportCheckpoint()
		need, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
		if err != nil || need.Stage != "ability" || need.Count != 1 || need.Sides != 20 || *need.AbilityStep != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("input query changed authority", need, err)
		}
		battleReject(t, w, gearRequest(w, "no-face", op))
		op.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{face}}}
		r := battleExec(t, w, "checked", "a", op)
		check := r.Events[1].Check
		if r.Events[0].Kind != core.ResourceSpent || check.Source != core.HumanController || check.Draw != uint64(face) || check.Total != face+3 || check.Threshold != 10 || check.Success != (face == 7) || check.CallsAfter != check.CallsBefore {
			t.Fatal("fixed check oracle", r)
		}
		hp := int64(5)
		if face == 7 {
			hp = 9
		}
		if w.Snapshot().Characters[0].Resources["hp"].Current != hp || w.Snapshot().Characters[0].Resources["focus"].Current != 1 {
			t.Fatal("wrong chosen branch")
		}
		cp := w.ExportCheckpoint()
		rebuilt, err := core.Replay(cp.Genesis, cp.History)
		if err != nil {
			t.Fatal(err)
		}
		retry, err := rebuilt.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
			t.Fatal("historical check reran", err)
		}
		op.Invoke.HumanRolls[0].Faces[0] = 20
		r.Events[1].Check.Source = core.AgentController
		if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
			t.Fatal("nested check alias")
		}
		h := w.History()
		h[0].Events[1].Check.Source = core.AgentController
		if partial, err := core.Replay(b, h); partial != nil || err == nil {
			t.Fatal("changed source accepted")
		}
	}
}

func TestActorCheckBadInputLateRollbackAndNPC(t *testing.T) {
	b := actorCheckFixture()
	d := &b.AbilityDefinitions[0]
	d.Steps[1].Check.Success = append(d.Steps[1].Check.Success, core.AbilityPrimitive{Spend: &core.SpendTemplate{
		Payer: d.Steps[0].Spend.Payer, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(100))}}}})
	w := gearWorld(t, b)
	for _, input := range []core.AbilityRollInput{{Step: 0, Faces: []int64{7}}, {Step: 1, Faces: []int64{0}}, {Step: 1, Faces: []int64{21}}, {Step: 1, Faces: []int64{7, 7}}, {Step: 1, Faces: []int64{7}}} {
		op := battleInvoke("care", "a", "a")
		op.Invoke.HumanRolls = []core.AbilityRollInput{input}
		battleReject(t, w, gearRequest(w, "rejected", op))
	}
	b = actorCheckFixture()
	b.Characters[1].Resources["focus"] = core.Resource{Current: 3, Maximum: 3}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	endCurrent(t, w, "pass")
	op := battleInvoke("care", "b", "b")
	op.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{20}}}
	r := core.Request{ID: "forged-faces", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{op}}
	battleReject(t, w, r)
	op.Invoke.HumanRolls = nil
	result := battleExec(t, w, "npc-check", "b", op)
	check := result.Events[1].Check
	if check.Source != core.AgentController || check.Draw < 1 || check.Draw > 20 || check.CallsAfter <= check.CallsBefore {
		t.Fatal("NPC did not use recorded engine dice", result)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("NPC replay", err)
	}
}
