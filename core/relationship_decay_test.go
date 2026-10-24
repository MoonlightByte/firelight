package core_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func decayingRelationshipFixture(value int64) core.Blueprint {
	b := relationshipFixture()
	b.Clock = &core.Clock{Unit: "seconds"}
	b.RelationDefinitions[0].Decay = &core.RelationDecay{Period: 86400, Numerator: 97, Denominator: 100, Baseline: 0}
	b.Relationships = []core.Relationship{{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "trust", To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Strength: value}}
	return b
}

func TestRelationshipDecayBoundariesRoundingAndPartition(t *testing.T) {
	b := decayingRelationshipFixture(10000)
	w := gearWorld(t, b)
	gearExecute(t, w, "before-boundary", advance(86399))
	if w.Snapshot().Relationships[0].Strength != 10000 {
		t.Fatal("early decay")
	}
	for i, want := range []int64{9700, 9409, 9127} {
		ticks := int64(86400)
		if i == 0 {
			ticks = 1
		}
		r := gearExecute(t, w, fmt.Sprint(i), advance(ticks))
		if len(r.Events) != 2 || r.Events[0].Kind != core.RelationshipDecayed || r.Events[0].Relationship.After.Strength != want || r.Events[0].Relationship.Decay.Periods != 1 || r.Events[1].Kind != core.TimeAdvanced {
			t.Fatal("fixed decay oracle", want, r)
		}
	}
	b = decayingRelationshipFixture(90)
	one, two := gearWorld(t, b), gearWorld(t, b)
	gearExecute(t, one, "combined", advance(172800))
	gearExecute(t, two, "first", advance(86400))
	gearExecute(t, two, "second", advance(86400))
	if one.Snapshot().Relationships[0].Strength != 84 || !reflect.DeepEqual(one.Snapshot().Blueprint, two.Snapshot().Blueprint) {
		t.Fatal("read/partition dependent quantization")
	}
	for _, row := range []struct{ baseline, current, want int64 }{{1, 0, 0}, {0, 1, 0}, {0, 3, 2}, {0, -3, -2}, {-1, 0, 0}, {math.MinInt64, math.MaxInt64, 0}} {
		b = decayingRelationshipFixture(row.current)
		b.RelationDefinitions[0].Minimum = math.MinInt64
		b.RelationDefinitions[0].Maximum = math.MaxInt64
		b.RelationDefinitions[0].Decay = &core.RelationDecay{Period: 1, Numerator: 1, Denominator: 2, Baseline: row.baseline}
		w = gearWorld(t, b)
		gearExecute(t, w, "tie", advance(1))
		if w.Snapshot().Relationships[0].Strength != row.want {
			t.Fatal("whole-value ties-even", row, w.Snapshot().Relationships)
		}
	}
	// A huge elapsed count stabilizes without iterating ticks or wrapping.
	b = decayingRelationshipFixture(math.MaxInt64)
	b.RelationDefinitions[0].Minimum = math.MinInt64
	b.RelationDefinitions[0].Maximum = math.MaxInt64
	b.RelationDefinitions[0].Decay = &core.RelationDecay{Period: 1, Numerator: 999, Denominator: 1000}
	w = gearWorld(t, b)
	r := gearExecute(t, w, "huge", advance(math.MaxInt64))
	if r.Events[0].Relationship.After.Strength != 500 || r.Events[0].Relationship.Decay.Periods != math.MaxInt64 {
		t.Fatal("fixed-point skip", r)
	}
}

func TestRelationshipDecayEquipmentExpiryRollbackReplay(t *testing.T) {
	b := clockFixture()
	rule := decayingRelationshipFixture(10000).RelationDefinitions[0]
	rule.Decay.Period = 60
	b.RelationDefinitions = []core.RelationDefinition{rule}
	b.Relationships = decayingRelationshipFixture(10000).Relationships
	w := gearWorld(t, b)
	gearExecute(t, w, "guard", timed("guard", 60))
	gearReject(t, w, "late", unwear("leather"), wear("chain", "a", ""), wear("sword", "a", "two"))
	first := gearExecute(t, w, "late", unwear("leather"))
	want := []core.EventKind{core.ConditionRemoved, core.RelationshipDecayed, core.TimeAdvanced, core.ItemUnequipped}
	if len(first.Events) != len(want) {
		t.Fatal(first)
	}
	for i, k := range want {
		if first.Events[i].Kind != k {
			t.Fatal("ordered expiry/decay/time/equipment", first)
		}
	}
	if w.Snapshot().Relationships[0].Strength != 9700 {
		t.Fatal("implicit equipment time did not decay")
	}
	gearExecute(t, w, "later", relationshipAdjustment("trust", -1000), advance(60))
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("derived decay replay", err)
	}
	retry, err := rebuilt.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("historical clock retry", err)
	}
	h := w.History()
	h[1].Events[1].Relationship.Decay.Periods++
	if partial, err := core.Replay(cp.Genesis, h); err == nil || partial != nil {
		t.Fatal("corrupt clock consequence accepted")
	}
	first.Events[1].Relationship.Decay.AfterTick = 9
	b.RelationDefinitions[0].Decay.Baseline = 9
	s := w.Snapshot()
	s.RelationDefinitions[0].Decay.Numerator = 1
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("decay pointer alias")
	}
}

func TestRelationshipDecayPolicyAndCapacityAdmission(t *testing.T) {
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Clock = nil }, func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Period = 0 },
		func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Numerator = -1 }, func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Numerator = 100 },
		func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Denominator = 0 }, func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Denominator = 1001 },
		func(b *core.Blueprint) { b.RelationDefinitions[0].Decay.Baseline = 10001 },
	} {
		b := decayingRelationshipFixture(1)
		mutate(&b)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid decay policy admitted")
		}
	}
	b := decayingRelationshipFixture(1)
	b.Relationships = nil
	for i := 0; i < 64; i++ {
		id := core.ItemID(fmt.Sprint(i))
		b.Items = append(b.Items, core.Item{ID: id})
		b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "trust", To: core.Ref{Kind: core.ItemKind, ID: string(id)}})
	}
	w := gearWorld(t, b)
	gearReject(t, w, "capacity", relationshipAdjustment("trust", 1))
	b.Relationships = append(b.Relationships, decayingRelationshipFixture(1).Relationships[0])
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("oversized genesis")
	}
}
