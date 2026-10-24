package core

import (
	"fmt"
	"math/big"
)

// RelationDefinition opts a relation kind into authored integer bounds. Units
// and the meaning of each kind belong to content, not language semantics.
type RelationDefinition struct {
	Decay   *RelationDecay `json:"decay,omitempty"`
	Kind    RelationKind   `json:"kind"`
	Minimum int64          `json:"minimum"`
	Maximum int64          `json:"maximum"`
}

type AdjustRelationship struct {
	From   Ref          `json:"from"`
	Kind   RelationKind `json:"kind"`
	To     Ref          `json:"to"`
	Amount int64        `json:"amount"`
}

type AdjustRelationshipTemplate struct {
	From   AbilityOperand `json:"from"`
	Kind   RelationKind   `json:"kind"`
	To     AbilityOperand `json:"to"`
	Amount AbilityOperand `json:"amount"`
}

func CheckRelationDefinition(d RelationDefinition) error {
	if !validID(string(d.Kind)) {
		return fail("E_ID", string(d.Kind), "relation_kind", "valid relation kind", string(d.Kind), "invalid relation kind")
	}
	if d.Minimum > 0 || d.Maximum < 0 || d.Minimum >= d.Maximum {
		return fail("E_RELATION_BOUNDS", string(d.Kind), "bounds", "minimum < maximum, including zero", fmt.Sprintf("minimum=%d maximum=%d", d.Minimum, d.Maximum), "invalid relation bounds")
	}
	if v := d.Decay; v != nil {
		if v.Period == 0 {
			return fail("E_RELATION_DECAY", string(d.Kind), "period", "positive logical period", "zero", "invalid relationship decay period")
		}
		if v.Denominator < 1 || v.Denominator > 1000 {
			return fail("E_RELATION_DECAY", string(d.Kind), "denominator", "1..1000", fmt.Sprint(v.Denominator), "invalid relationship decay denominator")
		}
		if v.Numerator < 0 || v.Numerator >= v.Denominator {
			return fail("E_RELATION_DECAY", string(d.Kind), "numerator", "0 <= numerator < denominator", fmt.Sprint(v.Numerator), "invalid relationship decay numerator")
		}
		if v.Baseline < d.Minimum || v.Baseline > d.Maximum {
			return fail("E_RELATION_BOUNDS", string(d.Kind), "baseline", "baseline within authored bounds", fmt.Sprint(v.Baseline), "relationship decay baseline exceeds bounds")
		}
	}
	return nil
}

func (b Blueprint) relationDefinition(kind RelationKind) *RelationDefinition {
	for i := range b.RelationDefinitions {
		if b.RelationDefinitions[i].Kind == kind {
			return &b.RelationDefinitions[i]
		}
	}
	return nil
}

func (b Blueprint) validateRelationDefinitions() error {
	seen := map[RelationKind]bool{}
	for _, d := range b.RelationDefinitions {
		if seen[d.Kind] {
			return fail("E_DUPLICATE", string(d.Kind), "relation_kind", "unique relation kind definition", string(d.Kind), "duplicate relation kind definition")
		}
		seen[d.Kind] = true
		if d.Decay != nil && b.Clock == nil {
			return fail("E_CLOCK", string(d.Kind), "relationship_decay", "Clock present", "absent", "relationship decay requires a Clock")
		}
		if err := CheckRelationDefinition(d); err != nil {
			return err
		}
	}
	return nil
}

// CheckRelationship preserves undeclared Relate kinds, including membership.
func (b Blueprint) CheckRelationship(r Relationship) error { return checkRelationship(scan{&b}, r) }
func checkRelationship(l lookups, r Relationship) error {
	if !validID(string(r.Kind)) {
		return fail("E_ID", "", "relation", "valid relation kind", string(r.Kind), "invalid relation kind")
	}
	for _, ref := range []Ref{r.From, r.To} {
		if err := checkRef(ref, l.kindOf); err != nil {
			return err
		}
	}
	if d := l.blueprint().relationDefinition(r.Kind); d != nil {
		if err := CheckRelationDefinition(*d); err != nil {
			return err
		}
		if r.Strength < d.Minimum || r.Strength > d.Maximum {
			return fail("E_RELATION_BOUNDS", string(r.Kind), "strength", fmt.Sprintf("%d..%d", d.Minimum, d.Maximum), fmt.Sprint(r.Strength), "relationship strength exceeds authored bounds")
		}
	}
	return nil
}

func (b Blueprint) checkAdjustRelationship(v AdjustRelationship) error {
	d := b.relationDefinition(v.Kind)
	if d == nil {
		return fail("E_RELATION_KIND", string(v.Kind), "relation_kind", "declared bounded relation kind", "missing", "adjustment requires authored relation bounds")
	}
	if err := CheckRelationDefinition(*d); err != nil {
		return err
	}
	if v.Amount == 0 {
		return fail("E_AMOUNT", string(v.Kind), "amount", "nonzero signed int64", "zero", "relationship adjustment must be nonzero")
	}
	for _, ref := range []Ref{v.From, v.To} {
		if err := b.CheckRef(ref); err != nil {
			return err
		}
	}
	return nil
}

func adjustRelationship(b *Blueprint, v AdjustRelationship) Event {
	d := b.relationDefinition(v.Kind)
	after := Relationship{From: v.From, Kind: v.Kind, To: v.To}
	index := relationIndex(b, after)
	change := &RelationshipChange{Adjustment: copyPtr(&v.Amount)}
	if index >= 0 {
		after = b.Relationships[index]
		change.Before = copyPtr(&after)
	}
	value := new(big.Int).Add(big.NewInt(after.Strength), big.NewInt(v.Amount))
	if value.Cmp(big.NewInt(d.Minimum)) < 0 {
		value.SetInt64(d.Minimum)
	}
	if value.Cmp(big.NewInt(d.Maximum)) > 0 {
		value.SetInt64(d.Maximum)
	}
	after.Strength = value.Int64()
	change.After = after
	if index < 0 {
		b.Relationships = append(b.Relationships, after)
	} else {
		b.Relationships[index] = after
	}
	return Event{Kind: RelationshipAdjusted, Relationship: change}
}
