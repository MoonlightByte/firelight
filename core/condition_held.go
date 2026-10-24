package core

import (
	"math/big"
	"sort"
)

// HeldCondition is a Condition type present on a Character because its
// Resource is at its minimum. It has no instance, lifetime or event: it
// begins and ends with the Resource, so a host never stores it.
type HeldCondition struct {
	Type     ConditionTypeID `json:"type"`
	Resource ResourceID      `json:"resource"`
}

// StatBound is one bound modifier in force on a stat. Instance names the
// Condition instance; a held type has none and names its Resource instead.
// Applied marks a bound that moved the total.
type StatBound struct {
	Instance      ConditionInstanceID `json:"instance,omitempty"`
	HeldAtMinimum ResourceID          `json:"held_at_minimum,omitempty"`
	Type          ConditionTypeID     `json:"type"`
	Modifier      ConditionModifierID `json:"modifier"`
	Bound         ModifierBound       `json:"bound"`
	Amount        int64               `json:"amount"`
	Applied       bool                `json:"applied"`
}

// heldAt reports whether d is held on the Character: it declares the
// Resource and the Resource is at its minimum.
func heldAt(b *Blueprint, id CharacterID, d *ConditionDefinition) bool {
	return heldBy(character(b, id), d)
}

// heldBy is heldAt for a Character already found, or nil for a missing one.
func heldBy(c *Character, d *ConditionDefinition) bool {
	if c == nil || d == nil || d.HeldAtMinimum == nil {
		return false
	}
	r, ok := c.Resources[*d.HeldAtMinimum]
	return ok && r.Current <= r.Minimum
}

// endsWhenHeld reports whether an instance of d ends because its
// ends_on_condition type is held, or included in a type held, at this
// Resource's minimum.
func endsWhenHeld(b *Blueprint, d *ConditionDefinition, resource ResourceID) bool {
	if d.EndsOnCondition == nil {
		return false
	}
	for i := range b.ConditionDefinitions {
		h := &b.ConditionDefinitions[i]
		if h.HeldAtMinimum != nil && *h.HeldAtMinimum == resource && presentAs(h, *d.EndsOnCondition) {
			return true
		}
	}
	return false
}

// presentAs reports whether a present type d makes type t present: it is t
// or includes t.
func presentAs(d *ConditionDefinition, t ConditionTypeID) bool {
	if d == nil {
		return false
	}
	if d.ID == t {
		return true
	}
	for _, x := range d.Includes {
		if x == t {
			return true
		}
	}
	return false
}

// statBounds lists the bounds in force on one stat: those of every instance
// on the Character that latest selection does not suppress, then those of
// every held type.
func (d *derivation) statBounds(id CharacterID, stat StatID) []StatBound {
	var out []StatBound
	d.indexModifiers()
	for _, x := range d.bounds[characterStat{id, stat}] {
		c := d.b.Conditions[x.owner]
		if d.instanceSuppressedBy(c) == nil {
			out = append(out, StatBound{Instance: c.ID, Type: c.Type, Modifier: x.m.ID, Bound: x.m.Bound, Amount: modifierAmount(*x.m, c)})
		}
	}
	holder, held := d.character(id), d.heldTypes()
	for _, x := range d.heldBounds[stat] {
		if def := held[x.owner]; heldBy(holder, def) {
			out = append(out, StatBound{HeldAtMinimum: *def.HeldAtMinimum, Type: def.ID, Modifier: x.m.ID, Bound: x.m.Bound, Amount: modifierAmount(*x.m, ConditionInstance{})})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Instance != out[j].Instance {
			return out[i].Instance < out[j].Instance
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Modifier < out[j].Modifier
	})
	return out
}

// applyBounds raises the total to its highest at_least bound, then lowers it
// to its lowest at_most bound, so a ceiling such as Speed 0 wins over a floor.
func applyBounds(sum *big.Int, bounds []StatBound) {
	unbounded := new(big.Int).Set(sum)
	for _, x := range bounds {
		if v := big.NewInt(x.Amount); x.Bound == AtLeast && sum.Cmp(v) < 0 {
			sum.Set(v)
		}
	}
	raised := new(big.Int).Set(sum)
	for _, x := range bounds {
		if v := big.NewInt(x.Amount); x.Bound == AtMost && sum.Cmp(v) > 0 {
			sum.Set(v)
		}
	}
	for i := range bounds {
		v := big.NewInt(bounds[i].Amount)
		if bounds[i].Bound == AtLeast {
			bounds[i].Applied = v.Cmp(sum) == 0 && v.Cmp(unbounded) > 0
		} else {
			bounds[i].Applied = v.Cmp(sum) == 0 && v.Cmp(raised) < 0
		}
	}
}
