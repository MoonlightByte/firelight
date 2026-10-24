package core_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func relationshipFixture() core.Blueprint {
	return core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny},
		Locations: []core.Location{{ID: "room"}}, Characters: []core.Character{{ID: "a", Location: "room"}, {ID: "b", Location: "room"}},
		RelationDefinitions: []core.RelationDefinition{{Kind: "trust", Minimum: -10000, Maximum: 10000}, {Kind: "fear", Minimum: 0, Maximum: 10000}}}
}
func relationshipAdjustment(kind core.RelationKind, amount int64) core.Operation {
	return core.Operation{AdjustRelationship: &core.AdjustRelationship{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: kind, To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Amount: amount}}
}

func TestRelationshipAdjustmentOraclesAndHistory(t *testing.T) {
	b := relationshipFixture()
	w := gearWorld(t, b)
	for i, row := range []struct{ amount, want int64 }{{800, 800}, {2400, 3200}, {-5400, -2200}, {math.MaxInt64, 10000}, {math.MaxInt64, 10000}, {math.MinInt64, -10000}, {math.MinInt64, -10000}} {
		r := battleExec(t, w, string(rune('A'+i)), "a", relationshipAdjustment("trust", row.amount))
		if r.Events[0].Kind != core.RelationshipAdjusted || r.Events[0].Relationship.After.Strength != row.want || *r.Events[0].Relationship.Adjustment != row.amount {
			t.Fatal("fixed signed oracle", row, r)
		}
	}
	// Existing zero and absence differ even when the requested delta clamps away.
	r := battleExec(t, w, "absent-at-bound", "a", relationshipAdjustment("fear", -1))
	if r.Events[0].Relationship.Before != nil || r.Events[0].Relationship.After.Strength != 0 {
		t.Fatal("missing zero edge", r)
	}
	r = battleExec(t, w, "existing-at-bound", "a", relationshipAdjustment("fear", -1))
	if r.Events[0].Relationship.Before == nil || r.Events[0].Relationship.Before.Strength != 0 {
		t.Fatal("lost explicit zero", r)
	}
	// Exact overflow-safe arithmetic: 9990+400 and 9990+500 clamp identically,
	// but they are different requests and cannot exchange recorded intent.
	battleExec(t, w, "set", "a", core.Operation{Relate: &core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "trust", To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Strength: 9990}})
	first := battleExec(t, w, "clamp", "a", relationshipAdjustment("trust", 400))
	if first.Events[0].Relationship.After.Strength != 10000 {
		t.Fatal(first)
	}
	battleExec(t, w, "later", "a", relationshipAdjustment("trust", -1000))
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("reconstruction", err)
	}
	retry, err := rebuilt.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("historical retry", err)
	}
	history := w.History()
	*history[len(history)-2].Events[0].Relationship.Adjustment = 500
	if partial, err := core.Replay(cp.Genesis, history); err == nil || partial != nil {
		t.Fatal("same clamp corrupted intent admitted")
	}
	*first.Events[0].Relationship.Adjustment = 1
	first.Events[0].Relationship.Before.Strength = 2
	first.Request.Operations[0].AdjustRelationship.Amount = 3
	b.RelationDefinitions[0].Maximum = 4
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("returned/input alias")
	}
}

func TestRelationshipAdjustmentBoundsRollbackAndCompatibility(t *testing.T) {
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.RelationDefinitions[0].Minimum = 1 },
		func(b *core.Blueprint) { b.RelationDefinitions[0].Maximum = -1 },
		func(b *core.Blueprint) { b.RelationDefinitions[0].Maximum = b.RelationDefinitions[0].Minimum },
		func(b *core.Blueprint) {
			b.RelationDefinitions = append(b.RelationDefinitions, b.RelationDefinitions[0])
		},
		func(b *core.Blueprint) { b.RelationDefinitions[0].Kind = "" },
		func(b *core.Blueprint) {
			b.Relationships = []core.Relationship{{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "trust", To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Strength: 10001}}
		},
	} {
		b := relationshipFixture()
		mutate(&b)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid initialization")
		}
	}
	w := gearWorld(t, relationshipFixture())
	for _, op := range []core.Operation{relationshipAdjustment("trust", 0), relationshipAdjustment("missing", 1),
		{AdjustRelationship: &core.AdjustRelationship{From: core.Ref{Kind: core.ItemKind, ID: "a"}, To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Kind: "trust", Amount: 1}},
		{Relate: &core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Kind: "trust", Strength: 10001}},
	} {
		request := gearRequest(w, "failure", relationshipAdjustment("trust", 800))
		request.Operations = append(request.Operations, op)
		battleReject(t, w, request)
	}
	bad := relationshipAdjustment("trust", 1)
	bad.Move = &core.Move{Character: "a", To: "room"}
	battleReject(t, w, gearRequest(w, "sum", bad))
	// Preexisting unbounded kinds are still unrestricted (no opt-in reinterpretation).
	r := battleExec(t, w, "failure", "a", core.Operation{Relate: &core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Strength: math.MaxInt64}})
	if r.Revision != 1 || r.Events[0].Kind != core.RelationshipChanged || r.Events[0].Relationship.Adjustment != nil {
		t.Fatal("old Relate changed", r)
	}
}

func TestRelationshipAdjustmentAbilityComposition(t *testing.T) {
	b := relationshipFixture()
	one := int64(800)
	from, to := core.CharacterID("a"), core.CharacterID("b")
	template := func(kind core.RelationKind) core.AbilityStep {
		return core.AbilityStep{AdjustRelationship: &core.AdjustRelationshipTemplate{Kind: kind, From: core.AbilityOperand{Literal: &core.AbilityValue{Character: &from}}, To: core.AbilityOperand{Literal: &core.AbilityValue{Character: &to}}, Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: &one}}}}
	}
	b.AbilityDefinitions = []core.AbilityDefinition{{ID: "support", Name: "Support", Steps: []core.AbilityStep{template("trust"), template("fear")}}}
	w := gearWorld(t, b)
	op := core.Operation{Invoke: &core.InvokeAbility{Ability: "support"}}
	r := battleExec(t, w, "support", "a", op)
	if r.Revision != 1 || len(r.Events) != 3 || r.Events[0].Relationship.After.Strength != 800 || r.Events[1].Relationship.After.Strength != 800 || r.Events[2].Kind != core.AbilityInvoked || r.Events[1].AbilityOrigin.Step != 1 {
		t.Fatal("one ordered invocation", r)
	}
	cp := w.ExportCheckpoint()
	one = 999
	from = "b"
	s := w.Snapshot()
	s.RelationDefinitions[0].Maximum = 0
	*s.AbilityDefinitions[0].Steps[0].AdjustRelationship.Amount.Literal.Integer = 0
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("nested blueprint alias")
	}
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := rebuilt.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(r, retry) {
		t.Fatal("ability receipt", err)
	}
	// Two declared axes followed by a legal static operation that fails on candidate.
	b = cp.Genesis
	b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}})
	b.Characters[0].Resources = map[core.ResourceID]core.Resource{"focus": {Current: 0, Minimum: 0, Maximum: 1}}
	w = gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "late", op))
}
