package core

import (
	"fmt"
	"math/big"
	"sort"
)

const MaxDecayingRelationships = 64
const MaxRelationDecayIterations = 65536

// RelationDecay is an authored quantized recurrence at absolute Clock boundaries.
// It has no calendar, read-time mutation, or game-specific unit conversion.
type RelationDecay struct {
	Period      uint64 `json:"period"`
	Numerator   int64  `json:"numerator"`
	Denominator int64  `json:"denominator"`
	Baseline    int64  `json:"baseline"`
}
type RelationDecayChange struct {
	BeforeTick uint64 `json:"before_tick"`
	AfterTick  uint64 `json:"after_tick"`
	Periods    uint64 `json:"periods"`
}

func cloneRelationDefinitions(definitions []RelationDefinition) []RelationDefinition {
	if definitions == nil {
		return nil
	}
	out := append([]RelationDefinition{}, definitions...)
	for i := range out {
		out[i].Decay = copyPtr(out[i].Decay)
	}
	return out
}

func (b Blueprint) validateRelationDecayCapacity() error {
	count := 0
	for _, r := range b.Relationships {
		if d := b.relationDefinition(r.Kind); d != nil && d.Decay != nil {
			count++
		}
	}
	if count > MaxDecayingRelationships {
		return fail("E_LIMIT", "", "relationship_decay", "at most 64 decaying edges", fmt.Sprint(count), "decaying relationship capacity exceeded")
	}
	return nil
}

func decayRelationshipValue(current int64, d RelationDecay, periods uint64) (int64, error) {
	// Reuse small exact integers; a weighted int64 average needs only ~75 bits.
	numerator, denominator := big.NewInt(d.Numerator), big.NewInt(d.Denominator)
	base := new(big.Int).Mul(big.NewInt(d.Baseline), big.NewInt(d.Denominator-d.Numerator))
	var sum, quotient, remainder, twice big.Int
	one := big.NewInt(1)
	for step := 0; periods > 0; step++ {
		if step == MaxRelationDecayIterations {
			return 0, fail("E_LIMIT", "", "relationship_decay", "at most 65536 numerical iterations per edge", "still changing", "relationship decay numerical budget exceeded")
		}
		sum.SetInt64(current)
		sum.Mul(&sum, numerator)
		sum.Add(&sum, base)
		quotient.QuoRem(&sum, denominator, &remainder)
		twice.Abs(&remainder)
		twice.Lsh(&twice, 1)
		comparison := twice.Cmp(denominator)
		if comparison > 0 || (comparison == 0 && quotient.Bit(0) == 1) {
			if sum.Sign() < 0 {
				quotient.Sub(&quotient, one)
			} else {
				quotient.Add(&quotient, one)
			}
		}
		next := quotient.Int64() // Convex average of two in-bounds int64 values.
		if next == current {
			return current, nil
		}
		current = next
		periods--
	}
	return current, nil
}

func decayRelationships(b *Blueprint, before, after uint64, ctx applicationContext) ([]Event, error) {
	if err := b.validateRelationDecayCapacity(); err != nil {
		return nil, err
	}
	// Initial/created relationship order is not event ordering authority.
	indices := make([]int, 0, len(b.Relationships))
	for i, r := range b.Relationships {
		if d := b.relationDefinition(r.Kind); d != nil && d.Decay != nil {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		a, z := b.Relationships[indices[i]], b.Relationships[indices[j]]
		if a.From.Kind != z.From.Kind {
			return a.From.Kind < z.From.Kind
		}
		if a.From.ID != z.From.ID {
			return a.From.ID < z.From.ID
		}
		if a.Kind != z.Kind {
			return a.Kind < z.Kind
		}
		if a.To.Kind != z.To.Kind {
			return a.To.Kind < z.To.Kind
		}
		return a.To.ID < z.To.ID
	})
	var events []Event
	for _, i := range indices {
		old := b.Relationships[i]
		d := *b.relationDefinition(old.Kind).Decay
		periods := after/d.Period - before/d.Period
		if periods == 0 {
			continue
		}
		value, err := decayRelationshipValue(old.Strength, d, periods)
		if err != nil {
			return nil, err
		}
		if value == old.Strength {
			continue
		}
		*ctx.work++
		if *ctx.work > MaxOperations {
			return nil, fail("E_LIMIT", string(old.Kind), "work", "at most 128", "relationship decay exceeded", "clock decay exceeds request work budget")
		}
		b.Relationships[i].Strength = value
		events = append(events, Event{Kind: RelationshipDecayed, Relationship: &RelationshipChange{Before: copyPtr(&old), After: b.Relationships[i], Decay: &RelationDecayChange{BeforeTick: before, AfterTick: after, Periods: periods}}})
	}
	return events, nil
}
