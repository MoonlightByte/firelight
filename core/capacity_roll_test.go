package core_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func capacityRollFixture() core.Blueprint {
	b := rolledCareFixture()
	b.Characters[0].Stats["constitution"] = 14
	b.Characters[0].Resources["hp"] = core.Resource{Current: 5, Maximum: 10}
	b.AbilityDefinitions[0].Action.Phase = core.Exploration
	r := b.AbilityDefinitions[0].Steps[1].RollResource
	r.Kind, r.Count, r.Sides, r.Minimum = "expand", 1, 10, 1
	r.Bonus = core.ScalarRecipe{Terms: []core.StatTerm{{Stat: "constitution", Offset: -10, Divisor: 2}}}
	return b
}

func TestCapacityRollGainReceiptReplayAndFloor(t *testing.T) {
	b := capacityRollFixture()
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
	if err != nil || need.Count != 1 || need.Sides != 10 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("private dice preview", need, err)
	}
	r := battleExec(t, w, "gain", "a", rolledCare("a", "a", 7))
	roll := r.Events[2].ResourceRoll
	if r.Events[1].Kind != core.ResourceExpanded || r.Events[1].Capacity.Requested != 9 ||
		roll.Kind != "expand" || roll.Minimum != 1 || roll.Requested != 9 || roll.Actual != "9" || roll.Bonus != 2 ||
		roll.BonusRecipe.Terms[0].Input != 14 || roll.CallsBefore != roll.CallsAfter ||
		w.Snapshot().Characters[0].Resources["hp"] != (core.Resource{Current: 5, Maximum: 19}) {
		t.Fatal("d10=7 + Con14 modifier2 grows maximum only", r)
	}
	battleExec(t, w, "later", "a", core.Operation{Expand: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 3}}, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "slot:1", Amount: 1}})
	cp := w.ExportCheckpoint()
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, cp, nativesave.SessionState{Campaign: "capacity-roll", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("historical receipt recharged, rerolled or expanded", err)
	}
	history := w.History()
	history[0].Events[2].ResourceRoll.Minimum++
	if bad, err := core.Replay(b, history); err == nil || bad != nil {
		t.Fatal("forged floor accepted")
	}
	r.Events[2].ResourceRoll.Minimum = 99
	r.Events[2].ResourceRoll.Faces[0] = 1
	r.Events[2].ResourceRoll.BonusRecipe.Terms[0].Input = 99
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("returned roll aliases canonical history")
	}
	b = capacityRollFixture()
	b.AbilityDefinitions[0].Steps[1].RollResource.Bonus = core.ScalarRecipe{Base: -5}
	w = gearWorld(t, b)
	r = battleExec(t, w, "floor", "a", rolledCare("a", "a", 1))
	if r.Events[2].ResourceRoll.Requested != 1 || r.Events[2].ResourceRoll.Bonus != -5 || w.Snapshot().Characters[0].Resources["hp"] != (core.Resource{Current: 5, Maximum: 11}) {
		t.Fatal("negative total uses authored floor without healing", r)
	}
}

func TestCapacityRollRejectionPreservesCandidateAndRandom(t *testing.T) {
	w := gearWorld(t, capacityRollFixture())
	for name, faces := range map[string][]int64{"missing": nil, "zero": {0}, "high": {11}, "extra": {1, 2}} {
		battleReject(t, w, gearRequest(w, name, rolledCare("a", "a", faces...)))
	}
	battleReject(t, w, gearRequest(w, "late", rolledCare("a", "a", 7), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "slot:1", Amount: 2}}))
	for _, rawOverflow := range []bool{false, true} {
		b := capacityRollFixture()
		if rawOverflow {
			b.AbilityDefinitions[0].Steps[1].RollResource.Bonus = core.ScalarRecipe{Base: math.MaxInt64}
		} else {
			b.Characters[0].Resources["hp"] = core.Resource{Current: 5, Maximum: math.MaxInt64}
		}
		w = gearWorld(t, b)
		battleReject(t, w, gearRequest(w, "overflow", rolledCare("a", "a", 1)))
	}
	for _, kind := range []string{"expand", "heal", "damage"} {
		b := capacityRollFixture()
		r := b.AbilityDefinitions[0].Steps[1].RollResource
		r.Kind = kind
		if kind == "expand" {
			r.Minimum = 0
		}
		if kind == "heal" {
			r.Minimum = -1 // a positive heal floor is legal (7b); a negative one is not
		}
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid kind/minimum admitted", kind)
		}
	}
	// NPC uses the same recorded PCG. A late failure cannot spend its draw.
	b := capacityRollFixture()
	b.AbilityDefinitions[0].Action.Phase = core.EitherPhase
	b.Characters[1].Stats["constitution"] = 10
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "wait", "a", battleInvoke("wait", "a", ""))
	battleReject(t, w, core.Request{ID: "late-npc", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{
		rolledCare("b", "b"), {Spend: &core.ResourceOp{Character: "b", Resource: "slot:1", Amount: 2}},
	}})
	r := battleExec(t, w, "npc", "b", rolledCare("b", "b"))
	if r.Events[2].ResourceRoll.Source != core.AgentController || r.Events[2].ResourceRoll.CallsAfter <= r.Events[2].ResourceRoll.CallsBefore {
		t.Fatal("NPC lost recorded random source", r)
	}
	if replayed, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(replayed.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("NPC replay mismatch", err)
	}
}
