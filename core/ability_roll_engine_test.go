package core_test

// 7b-engine commit B (kit agents/kw/plan-7b-engine-r0.md): a human
// controller's per-step engine-dice opt-out for a rolled Resource step or an
// actor_roll Check.

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func engineRoll(step int) []core.AbilityRollInput {
	return []core.AbilityRollInput{{Step: step, Engine: true}}
}

func TestHumanEngineOptOutDrawsRecordsAndReplays(t *testing.T) {
	b := rolledCareFixture()
	w := gearWorld(t, b)
	op := rolledCare("a", "a")
	op.Invoke.HumanRolls = engineRoll(1)
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
	if err != nil || need.Stage != "complete" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("the dice question must treat an engine group as answered without drawing", need, err)
	}
	r := battleExec(t, w, "engine", "a", op)
	roll := resourceRoll(t, r)
	if roll.Source != core.HumanController || !roll.EngineDrawn || len(roll.Faces) != 2 || roll.CallsAfter != roll.CallsBefore+2 {
		t.Fatal("human engine draw", roll)
	}
	for _, f := range roll.Faces {
		if f < 1 || f > 8 {
			t.Fatal("face out of range", roll.Faces)
		}
	}
	if want := roll.Faces[0] + roll.Faces[1] + 3; roll.Requested != want || w.Snapshot().Characters[0].Resources["hp"].Current != 5+want {
		t.Fatal("engine faces not the healed amount", roll)
	}
	cp := w.ExportCheckpoint()
	var encoded bytes.Buffer
	if _, err := nativesave.Write(&encoded, cp, nativesave.SessionState{Campaign: "engine", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&encoded)
	if err != nil || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), cp) {
		t.Fatal("save round trip", err)
	}
	if !loaded.World.History()[0].Request.Operations[0].Invoke.HumanRolls[0].Engine {
		t.Fatal("saved request lost the opt-out")
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), cp) {
		t.Fatal("historical retry redrew", err)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), cp) {
		t.Fatal("engine replay", err)
	}
	h := w.History()
	forgedOne := false
	for i := range h[len(h)-1].Events {
		if roll := h[len(h)-1].Events[i].ResourceRoll; roll != nil {
			roll.EngineDrawn, forgedOne = false, true
		}
	}
	if forged, err := core.Replay(b, h); !forgedOne || forged != nil || err == nil {
		t.Fatal("history without engine_drawn accepted")
	}
}

func TestHumanEngineOptOutRefusals(t *testing.T) {
	w := gearWorld(t, rolledCareFixture())
	withFaces := rolledCare("a", "a", 4, 5)
	withFaces.Invoke.HumanRolls[0].Engine = true
	battleReject(t, w, gearRequest(w, "engine-and-faces", withFaces))
	wrongStep := rolledCare("a", "a")
	wrongStep.Invoke.HumanRolls = engineRoll(0)
	battleReject(t, w, gearRequest(w, "engine-wrong-step", wrongStep))
	twice := rolledCare("a", "a")
	twice.Invoke.HumanRolls = append(engineRoll(1), engineRoll(1)...)
	battleReject(t, w, gearRequest(w, "engine-twice", twice))

	b := rolledCareFixture()
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "wait", "a", battleInvoke("wait", "a", ""))
	npc := rolledCare("b", "b")
	npc.Invoke.HumanRolls = engineRoll(1)
	battleReject(t, w, core.Request{ID: "npc-engine", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{npc}})
}

func TestCanonicalInvocationKeepsTheEngineOptOut(t *testing.T) {
	op := rolledCare("a", "a")
	op.Invoke.HumanRolls = engineRoll(1)
	got := core.CanonicalInvocation(*op.Invoke)
	if len(got.HumanRolls) != 1 || !got.HumanRolls[0].Engine || got.HumanRolls[0].Step != 1 || got.HumanRolls[0].Faces != nil {
		t.Fatal("canonical identity dropped the opt-out", got.HumanRolls)
	}
	op.Invoke.HumanRolls[0].Engine = false
	if !got.HumanRolls[0].Engine {
		t.Fatal("canonical invocation aliases its input")
	}
}

func TestActorCheckEngineOptOut(t *testing.T) {
	b := actorCheckFixture()
	w := gearWorld(t, b)
	op := battleInvoke("care", "a", "a")
	op.Invoke.HumanRolls = engineRoll(1)
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
	if err != nil || need.Stage != "complete" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("check question with an engine group", need, err)
	}
	r := battleExec(t, w, "checked-engine", "a", op)
	check := r.Events[1].Check
	if check == nil || check.Source != core.HumanController || !check.EngineDrawn || check.CallsAfter != check.CallsBefore+1 || check.Draw < 1 || check.Draw > 20 || check.Total != int64(check.Draw)+3 || check.Success != (check.Total >= 10) {
		t.Fatal("engine-drawn check", r)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("engine check replay", err)
	}
	withFace := battleInvoke("care", "a", "a")
	withFace.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{7}, Engine: true}}
	battleReject(t, w, gearRequest(w, "check-engine-and-face", withFace))
}
