package core_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func consumableFixture() core.Blueprint {
	b := rolledCareFixture()
	b.Characters[0].Resources["hp"] = core.Resource{Current: 8, Maximum: 12}
	b.Items = append(b.Items, core.Item{ID: "potion", Name: "Potion", Owner: "a", Quantity: gearPtr(int64(2)), Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}})
	d := &b.AbilityDefinitions[0]
	d.Steps[0] = core.AbilityStep{Consume: &core.ConsumeTemplate{Item: core.AbilityOperand{Literal: &core.AbilityValue{Item: gearPtr(core.ItemID("potion"))}}, Owner: core.AbilityOperand{Parameter: "actor"}, Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}}
	d.Steps[1].RollResource.Sides = 4
	d.Steps[1].RollResource.Bonus = core.ScalarRecipe{Base: 2}
	return b
}

func TestAbilityConsumeRollAtomicHistoryAndDetachment(t *testing.T) {
	b := consumableFixture()
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	input, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
	if err != nil || input.Count != 2 || input.Sides != 4 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("pending consumption escaped", input, err)
	}
	first := battleExec(t, w, "drink", "a", rolledCare("a", "a", 1, 2))
	s := w.Snapshot()
	if first.Events[0].Consumption.After != 1 || s.Characters[0].Resources["hp"].Current != 12 || first.Events[0].Kind != core.ItemConsumed || first.Events[2].ResourceRoll.Requested != 5 || first.Events[2].ResourceRoll.Actual != "4" || first.Events[3].Kind != core.AbilityInvoked {
		t.Fatal("fixed consumed2d4+2 oracle", first)
	}
	last := battleExec(t, w, "full-hp", "a", rolledCare("a", "a", 1, 2))
	if last.Events[0].Consumption.After != 0 || last.Events[2].ResourceRoll.Actual != "0" {
		t.Fatal("full HP must still consume", last)
	}
	battleReject(t, w, gearRequest(w, "empty", rolledCare("a", "a", 1, 2)))
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("replay", err)
	}
	retry, err := rebuilt.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("historical retry", err)
	}
	*b.AbilityDefinitions[0].Steps[0].Consume.Amount.Literal.Integer = 999
	*s.AbilityDefinitions[0].Steps[0].Consume.Item.Literal.Item = "sword"
	*first.Events[0].Consumption.Before = 99
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("nested consume alias")
	}
	history := w.History()
	history[0].Events[0].Consumption.After++
	if partial, err := core.Replay(cp.Genesis, history); err == nil || partial != nil {
		t.Fatal("inconsistent stock history")
	}
}

func TestAbilityConsumeFailuresAndLateRollback(t *testing.T) {
	for _, tc := range []struct {
		name, operand string
		mutate        func(*core.Blueprint)
	}{
		{"owner", "owner", func(b *core.Blueprint) { b.Items[len(b.Items)-1].Owner = "b" }},
		{"stored", "item", func(b *core.Blueprint) {
			b.Items[len(b.Items)-1].Custody = &core.Ref{Kind: core.LocationKind, ID: "room"}
		}},
		{"empty", "item", func(b *core.Blueprint) { *b.Items[len(b.Items)-1].Quantity = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := consumableFixture()
			tc.mutate(&b)
			w := gearWorld(t, b)
			err := battleReject(t, w, gearRequest(w, tc.name, rolledCare("a", "a", 1, 2)))
			var a *core.AbilityError
			if !errors.As(err, &a) || a.Step != 0 || a.Operand != tc.operand {
				t.Fatal("consume context", err)
			}
		})
	}
	// Fail after consumption AND healing inside the same invocation.
	b := consumableFixture()
	b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Parameter: "actor"}, Resource: "slot:1", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(3))}}}})
	w := gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "late", rolledCare("a", "a", 1, 2)))
	// Same candidate rollback with an NPC draw and consumed turn.
	b.Items[len(b.Items)-1].Owner = "b"
	b.Items[len(b.Items)-1].Custody.ID = "b"
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "pass", "a", battleInvoke("wait", "a", ""))
	battleReject(t, w, core.Request{ID: "late-npc", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{rolledCare("b", "b")}})
	// Closed sum remains closed; invalid caller IR cannot run two steps at once.
	b = consumableFixture()
	b.AbilityDefinitions[0].Steps[0].Heal = &core.HealTemplate{}
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("invalid sum accepted")
	}
}
