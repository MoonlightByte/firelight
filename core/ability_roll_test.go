package core_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func rolledCareFixture() core.Blueprint {
	b := battleAbilityFixture()
	b.Characters[0].Stats["wisdom"] = 16
	b.Characters[1].Stats["wisdom"] = 10
	b.Characters[0].Resources["slot:1"] = core.Resource{Current: 2, Maximum: 2}
	b.Characters[1].Resources["slot:1"] = core.Resource{Current: 2, Maximum: 2}
	b.AbilityDefinitions[0].Steps[0].Spend.Resource = "slot:1"
	b.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = gearPtr(int64(1))
	b.AbilityDefinitions[0].Steps[1] = core.AbilityStep{RollResource: &core.RollResourceTemplate{
		Kind: "heal", Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp", Count: 2, Sides: 8,
		Caster: core.AbilityOperand{Parameter: "actor"}, Bonus: core.ScalarRecipe{Terms: []core.StatTerm{{Stat: "wisdom", Offset: -10, Divisor: 2}}},
	}}
	return b
}
func rolledCare(actor, target core.CharacterID, faces ...int64) core.Operation {
	op := battleInvoke("care", actor, target)
	if faces != nil {
		op.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: faces}}
	}
	return op
}

func TestAbilityResourceRollHumanOracleReplayAndDetachedFaces(t *testing.T) {
	b := rolledCareFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	before := w.ExportCheckpoint()
	input, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
	if err != nil || input.Stage != "ability" || input.Count != 2 || input.Sides != 8 || *input.AbilityStep != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("read-only dice admission", input, err)
	}
	op := rolledCare("a", "a", 4, 5)
	r := battleExec(t, w, "heal", "a", op)
	roll := r.Events[2].ResourceRoll
	if len(r.Events) != 4 || roll == nil || roll.Bonus != 3 || roll.Requested != 12 || roll.Actual != "12" || roll.Source != core.HumanController || roll.CallsAfter != roll.CallsBefore || w.Snapshot().Characters[0].Resources["hp"].Current != 17 || w.Snapshot().Characters[0].Resources["slot:1"].Current != 1 || r.Events[3].BattleAction.After.Cursor != 1 {
		t.Fatal("fixed2d8+3 oracle", r)
	}
	battleExec(t, w, "empty-slots", "a", core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "slot:1", Amount: 1}})
	cp := w.ExportCheckpoint()
	var encoded bytes.Buffer
	if _, err := nativesave.Write(&encoded, cp, nativesave.SessionState{Campaign: "rolled", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), cp) {
		t.Fatal("retry redrew/recharged", err)
	}
	op.Invoke.HumanRolls[0].Faces[0] = 1
	r.Events[2].ResourceRoll.Faces[0] = 1
	r.Events[2].ResourceRoll.BonusRecipe.Terms[0].Term.Stat = "forged"
	r.Request.Operations[0].Invoke.HumanRolls[0].Faces[0] = 1
	if !reflect.DeepEqual(w.ExportCheckpoint(), cp) {
		t.Fatal("mutable roll output/input aliases world")
	}
	history := w.History()
	history[1].Events[2].ResourceRoll.Requested++
	if forged, err := core.Replay(b, history); forged != nil || err == nil {
		t.Fatal("corrupted roll history accepted")
	}
}

func TestAbilityResourceRollFailuresZeroAndNPC(t *testing.T) {
	w := gearWorld(t, rolledCareFixture())
	for name, op := range map[string]core.Operation{"missing": rolledCare("a", "a"), "short": rolledCare("a", "a", 1), "extra": rolledCare("a", "a", 1, 2, 3), "low": rolledCare("a", "a", 0, 1), "high": rolledCare("a", "a", 1, 9)} {
		battleReject(t, w, gearRequest(w, name, op))
	}
	op := rolledCare("a", "a", 4, 5)
	op.Invoke.HumanRolls[0].Step = 0
	battleReject(t, w, gearRequest(w, "wrong-step", op))
	battleReject(t, w, gearRequest(w, "late", rolledCare("a", "a", 4, 5), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "slot:1", Amount: 2}}))
	b := rolledCareFixture()
	b.AbilityDefinitions[0].Steps[1].RollResource.Bonus = core.ScalarRecipe{Base: -5}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	r := battleExec(t, w, "zero", "a", rolledCare("a", "a", 1, 1))
	if len(r.Events) != 3 || r.Events[1].ResourceRoll.Requested != 0 || r.Events[1].ResourceRoll.Actual != "0" || r.Events[2].BattleAction.After.Cursor != 1 || w.Snapshot().Characters[0].Resources["hp"].Current != 5 || w.Snapshot().Characters[0].Resources["slot:1"].Current != 1 {
		t.Fatal("zero is a paid committed outcome", r)
	}
	b = rolledCareFixture()
	b.AbilityDefinitions[0].Steps[1].RollResource.Bonus = core.ScalarRecipe{Base: math.MaxInt64}
	w = gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "overflow", rolledCare("a", "a", 1, 1)))
	b = rolledCareFixture()
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "wait", "a", battleInvoke("wait", "a", ""))
	npc := core.Request{ID: "npc", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{rolledCare("b", "b", 4, 5)}}
	battleReject(t, w, npc)
	npc.Operations = []core.Operation{rolledCare("b", "b"), {Spend: &core.ResourceOp{Character: "b", Resource: "slot:1", Amount: 2}}}
	battleReject(t, w, npc)
	r = battleExec(t, w, "valid-npc", "b", rolledCare("b", "b"))
	if r.Events[2].ResourceRoll.Source != core.AgentController || r.Events[2].ResourceRoll.CallsAfter <= r.Events[2].ResourceRoll.CallsBefore {
		t.Fatal("NPC did not use shared recorded RNG")
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("NPC replay", err)
	}
}
