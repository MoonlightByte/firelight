package tests

import (
	"github.com/MoonlightByte/firelight/core"
	"math"
	"testing"
)

// Frozen before implementation; expected endpoints are independent arithmetic
// oracles from the owner handoff, not values calculated by the engine.
var m10DamageRows = []struct {
	name                                     string
	current, minimum, maximum, amount, after int64
}{
	{"D01", 6, 0, 20, 4, 2}, {"D02", 4, 0, 20, 4, 0}, {"D03", 2, 0, 20, 4, 0},
	{"D04", 0, 0, 20, 4, 0}, {"D05", 5, 2, 12, 4, 2}, {"D06", 2, 2, 12, 4, 2},
	{"D07", -3, -5, 5, 4, -5}, {"D08", math.MaxInt64, math.MinInt64, math.MaxInt64, math.MaxInt64, 0},
	{"D09", -1, math.MinInt64, math.MaxInt64, math.MaxInt64, math.MinInt64},
	{"D10", math.MinInt64 + 1, math.MinInt64, math.MaxInt64, math.MaxInt64, math.MinInt64},
	{"D11", math.MinInt64, math.MinInt64, math.MaxInt64, 1, math.MinInt64},
}

func TestM10DirectDamageCharacterization(t *testing.T) {
	for _, tc := range m10DamageRows {
		t.Run(tc.name, func(t *testing.T) {
			b := costFixture(t)
			before := core.Resource{Current: tc.current, Minimum: tc.minimum, Maximum: tc.maximum}
			after := core.Resource{Current: tc.after, Minimum: tc.minimum, Maximum: tc.maximum}
			costCharacter(&b, "char:lyra").Resources["hp"] = before
			w := world(t, b)
			op := core.Operation{Damage: &core.ResourceOp{Character: "char:lyra", Resource: "hp", Amount: tc.amount}}
			r := costRequest(w, "m10:"+tc.name, op)
			got := commitC(t, w, r)
			want := core.Batch{Revision: 1, Request: r, Events: []core.Event{{ID: "event:1:1", Revision: 1, RequestID: r.ID, Actor: r.Actor, Operation: 0, Kind: core.DamageApplied, Resource: &core.ResourceChange{Character: "char:lyra", Resource: "hp", Requested: tc.amount, Before: before, After: after}}}}
			contractEqual(t, contractBytes(t, want), got)
			snapshot := w.Snapshot()
			if costCharacter(&snapshot.Blueprint, "char:lyra").Resources["hp"] != after {
				t.Fatal("endpoint")
			}
			costReplay(t, b, w)
		})
	}
}
