package core_test

import (
	"bytes"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func capacityFixture() core.Blueprint {
	return core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room"}}, Characters: []core.Character{
		{ID: "a", Location: "room", Stats: map[core.StatID]int64{"defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 5, Maximum: 10}, "slots": {Maximum: 2}, "wide": {Minimum: -3, Current: -2, Maximum: -1}}},
		{ID: "b", Location: "room", Resources: map[core.ResourceID]core.Resource{"hp": {Maximum: 10}}},
	}}
}
func expandOp(c core.CharacterID, r core.ResourceID, amount int64) core.Operation {
	return core.Operation{Expand: &core.ResourceOp{Character: c, Resource: r, Amount: amount}}
}
func TestExpandCapacityPreservesCurrentAndOrderedHealing(t *testing.T) {
	b := capacityFixture()
	w := gearWorld(t, b)
	r := gearExecute(t, w, "grow", expandOp("a", "hp", 6), expandOp("a", "slots", 1), expandOp("a", "wide", math.MaxInt64), expandOp("b", "hp", 6))
	expected := []core.Resource{{Current: 5, Maximum: 16}, {Maximum: 3}, {Minimum: -3, Current: -2, Maximum: math.MaxInt64 - 1}, {Maximum: 16}}
	for i, event := range r.Events {
		if event.Kind != core.ResourceExpanded || event.Capacity == nil || event.Capacity.After != expected[i] || event.Capacity.After.Current != event.Capacity.Before.Current || event.Capacity.After.Minimum != event.Capacity.Before.Minimum {
			t.Fatal("fixed expansion oracle", i, event)
		}
	}
	later := gearExecute(t, w, "ordered", expandOp("a", "hp", 4), core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 99}})
	if later.Events[0].Capacity.After != (core.Resource{Current: 5, Maximum: 20}) || later.Events[1].Resource.After.Current != 20 {
		t.Fatal("healing did not see grown candidate capacity", later)
	}
	before := w.ExportCheckpoint()
	retry, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("historical expansion executed again", err)
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "capacity", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("reconstructed retry", err)
	}
	for _, forge := range []func(*core.CapacityChange){func(v *core.CapacityChange) { v.After.Maximum++ }, func(v *core.CapacityChange) { v.After.Current++ }, func(v *core.CapacityChange) { v.Before.Maximum++ }, func(v *core.CapacityChange) { v.Requested++ }} {
		bad := w.History()
		forge(bad[0].Events[0].Capacity)
		if partial, err := core.Replay(b, bad); err == nil || partial != nil {
			t.Fatal("inconsistent capacity accepted")
		}
	}
	r.Request.Operations[0].Expand.Amount = 123
	r.Events[0].Capacity.After.Maximum = 123
	retry.Events[0].Capacity.Before.Current = 123
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("capacity record alias")
	}
}

func TestExpandCapacityRejectionRollbackAndContinuation(t *testing.T) {
	b := capacityFixture()
	b.Characters[0].Resources["limit"] = core.Resource{Maximum: math.MaxInt64}
	w := gearWorld(t, b)
	for _, bad := range []core.Operation{expandOp("a", "hp", 0), expandOp("a", "hp", -1), expandOp("a", "limit", 1), expandOp("missing", "hp", 1), expandOp("room", "hp", 1), expandOp("a", "missing", 1), {Expand: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}, Restore: &core.RestoreResource{Character: "a", Resource: "hp"}}} {
		gearReject(t, w, "invalid", bad)
	}
	gearReject(t, w, "reuse", expandOp("a", "hp", 6), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "slots", Amount: 1}})
	r := gearExecute(t, w, "reuse", expandOp("a", "hp", 6))
	if r.Revision != 1 || r.Events[0].Capacity.After != (core.Resource{Current: 5, Maximum: 16}) {
		t.Fatal("failure reserved request or changed facts", r)
	}
	changed := r.Request
	changed.Operations = []core.Operation{expandOp("a", "hp", 7)}
	before := w.ExportCheckpoint()
	_, err := w.Execute(changed)
	var fault *core.Fault
	if !errors.As(err, &fault) || fault.Code != "E_REQUEST_REUSE" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("changed historical request", err)
	}
}

func TestExpandAbilityOriginAliasAndStableLife(t *testing.T) {
	b := battleAbilityFixture()
	b.Characters[0].Resources["hp"] = core.Resource{Current: 5, Maximum: 10}
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Expand: &core.ExpandTemplate{Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(6))}}}}}
	b.AbilityDefinitions[0].Action.Requirements = nil
	w := gearWorld(t, b)
	gearExecute(t, w, "down", core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 5}})
	beforeLife := w.Snapshot().Battle.State.Life
	r := battleExec(t, w, "grow-down", "b", battleInvoke("care", "b", "a"))
	if r.Events[0].Capacity.After != (core.Resource{Maximum: 16}) || r.Events[0].AbilityOrigin.Step != 0 || r.Events[1].Ability.Ability != "care" || !reflect.DeepEqual(beforeLife, w.Snapshot().Battle.State.Life) {
		t.Fatal("expansion healed or lost origin", r)
	}
	before := w.ExportCheckpoint()
	*b.AbilityDefinitions[0].Steps[0].Expand.Amount.Literal.Integer = 99
	snapshot := w.Snapshot()
	*snapshot.AbilityDefinitions[0].Steps[0].Expand.Amount.Literal.Integer = 98
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested template alias")
	}
	// A defeated opposition character also stays dead; maximum growth is not healing.
	b = battleFixture()
	b.Characters[1].Resources["hp"] = core.Resource{Current: 10, Maximum: 10}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "dead", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true))
	beforeLife = w.Snapshot().Battle.State.Life
	r = gearExecute(t, w, "grow-dead", expandOp("b", "hp", 6))
	if r.Events[0].Capacity.After != (core.Resource{Maximum: 16}) || !reflect.DeepEqual(beforeLife, w.Snapshot().Battle.State.Life) {
		t.Fatal("capacity resurrected character", r)
	}
}

func TestExpandCheckBranchRetainsBindingAndRollback(t *testing.T) {
	for _, threshold := range []int64{0, 21} {
		b := battleAbilityFixture()
		expansion := core.AbilityPrimitive{Expand: &core.ExpandTemplate{Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(6))}}}}
		check := &core.CheckTemplate{ID: "growth", Sides: 20, Modifier: core.CheckValue{Literal: gearPtr(int64(0))}, Threshold: core.CheckValue{Literal: gearPtr(threshold)}}
		if threshold == 0 {
			check.Success = []core.AbilityPrimitive{expansion}
		} else {
			check.Failure = []core.AbilityPrimitive{expansion}
		}
		b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Check: check}}
		w := gearWorld(t, b)
		gearReject(t, w, "late-branch", battleInvoke("care", "a", "a"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 4}})
		r := gearExecute(t, w, "branch", battleInvoke("care", "a", "a"))
		found := false
		for _, event := range r.Events {
			if event.Capacity != nil {
				found = true
				if event.Capacity.After != (core.Resource{Current: 5, Maximum: 36}) || event.AbilityOrigin == nil || len(event.AbilityOrigin.Path) == 0 {
					t.Fatal("lost expansion branch or origin", event)
				}
			}
		}
		if !found {
			t.Fatal("branch lost capacity operation", r)
		}
		before := w.ExportCheckpoint()
		*expansion.Expand.Amount.Literal.Integer = 99
		if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("branch input alias")
		}
		replayed, err := core.Replay(before.Genesis, w.History())
		if err != nil || !reflect.DeepEqual(before, replayed.ExportCheckpoint()) {
			t.Fatal("branch replay", err)
		}
	}
}
