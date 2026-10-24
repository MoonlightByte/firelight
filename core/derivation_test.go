package core

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// TestDerivationMatchesScans checks every answer a derivation gives against
// the scan it replaced, on small random worlds whose IDs repeat, whose
// instances tie on provenance and whose references may be missing.
func TestDerivationMatchesScans(t *testing.T) {
	for seed := int64(0); seed < 3000; seed++ {
		b := randomDerivationWorld(rand.New(rand.NewSource(seed)))
		d := newDerivation(b)
		fail := func(what string, got, want any) {
			t.Helper()
			t.Fatalf("seed %d: %s: got %v, want %v", seed, what, got, want)
		}
		for _, id := range drawn("c", 6) {
			if got, want := d.character(CharacterID(id)), character(b, CharacterID(id)); got != want {
				fail("character "+id, got, want)
			}
			k, ok := d.kindOf(id)
			if wk, wok := b.KindOf(id); k != wk || ok != wok {
				fail("kindOf "+id, k, wk)
			}
			if got, want := d.heldConditions(CharacterID(id)), scanHeldConditions(b, CharacterID(id)); !reflect.DeepEqual(got, want) {
				fail("heldConditions "+id, got, want)
			}
			// The first fault a pass meets depends on these orders.
			var on, worn []int
			for i, c := range b.Conditions {
				if c.Target == CharacterID(id) {
					on = append(on, i)
				}
			}
			for i, x := range b.Items {
				if x.Wearer != "" && x.Wearer == CharacterID(id) {
					worn = append(worn, i)
				}
			}
			if got := d.conditionsOn(CharacterID(id)); !reflect.DeepEqual(got, on) {
				fail("conditionsOn "+id, got, on)
			}
			if got := d.wornBy(CharacterID(id)); !reflect.DeepEqual(got, worn) {
				fail("wornBy "+id, got, worn)
			}
			for _, t := range drawn("t", 7) {
				if got, want := d.hasCondition(CharacterID(id), ConditionTypeID(t)), b.hasCondition(CharacterID(id), ConditionTypeID(t)); got != want {
					fail("hasCondition "+id+" "+t, got, want)
				}
			}
		}
		for _, id := range drawn("x", 6) {
			if got, want := d.item(ItemID(id)), item(b, ItemID(id)); got != want {
				fail("item "+id, got, want)
			}
		}
		for _, id := range drawn("t", 7) {
			if got, want := d.definition(ConditionTypeID(id)), conditionDefinition(b, ConditionTypeID(id)); got != want {
				fail("definition "+id, got, want)
			}
		}
		for _, id := range drawn("i", 9) {
			if got, want := d.instance(ConditionInstanceID(id)), conditionIndex(b, ConditionInstanceID(id)); got != want {
				fail("instance "+id, got, want)
			}
		}
		for _, id := range drawn("e", 4) {
			if got, want := d.equipmentDefinition(EquipmentDefinitionID(id)), b.equipmentDefinition(EquipmentDefinitionID(id)); got != want {
				fail("equipmentDefinition "+id, got, want)
			}
		}
		for _, id := range drawn("s", 5) {
			if got, want := d.derivedRule(StatID(id)), b.derivedRule(StatID(id)); got != want {
				fail("derivedRule "+id, got, want)
			}
		}
		// Instances of the world, and instances it does not hold.
		instances := append([]ConditionInstance{}, b.Conditions...)
		for i := range b.Conditions {
			c := b.Conditions[i]
			c.ID += "-other"
			instances = append(instances, c)
		}
		for _, c := range instances {
			if got, want := d.instanceSuppressedBy(c), scanSuppressedBy(b, c); !reflect.DeepEqual(got, want) {
				fail(fmt.Sprintf("instanceSuppressedBy %+v", c), got, want)
			}
		}
		if got, want := d.allEffects(), scanAllEffects(b); !reflect.DeepEqual(got, want) {
			fail("allEffects", got, want)
		}
		effects := d.allEffects()
		for _, e := range effects {
			if got, want := d.conditionSuppressedBy(e), scanConditionSuppressedBy(b, e); !reflect.DeepEqual(got, want) {
				fail("conditionSuppressedBy "+string(e.ID), got, want)
			}
		}
		for _, id := range drawn("c", 6) {
			for _, stat := range drawn("s", 5) {
				var got, want []EffectID
				for _, i := range d.contributing(CharacterID(id), StatID(stat)) {
					got = append(got, effects[i].ID)
				}
				for _, e := range scanAllEffects(b) {
					if e.Stat == StatID(stat) && scanAppliesTo(b, e, CharacterID(id)) {
						want = append(want, e.ID)
					}
				}
				if !reflect.DeepEqual(got, want) {
					fail("contributing "+id+" "+stat, got, want)
				}
				if got, want := d.statBounds(CharacterID(id), StatID(stat)), scanStatBounds(b, CharacterID(id), StatID(stat)); !reflect.DeepEqual(got, want) {
					fail("statBounds "+id+" "+stat, got, want)
				}
				recipe := &RecipeExplanation{}
				for _, term := range drawn("s", 5) {
					recipe.Terms = append(recipe.Terms, TermExplanation{Term: StatTerm{Stat: StatID(term)}, Active: (len(term)+len(id)+int(seed))%3 != 0})
				}
				for _, r := range []*RecipeExplanation{nil, recipe} {
					if got, want := d.rollSources(CharacterID(id), StatID(stat), r), scanRollSources(b, CharacterID(id), StatID(stat), r); !reflect.DeepEqual(got, want) {
						fail("rollSources "+id+" "+stat, got, want)
					}
				}
			}
		}
	}
}

// drawn lists the IDs a random world draws from, one more than it uses, and
// the empty ID.
func drawn(prefix string, n int) []string {
	out := []string{""}
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s%d", prefix, i))
	}
	return out
}

func randomDerivationWorld(r *rand.Rand) *Blueprint {
	id := func(prefix string, n int) string { return fmt.Sprintf("%s%d", prefix, r.Intn(n)) }
	maybe := func(prefix string, n int) string {
		if r.Intn(3) == 0 {
			return ""
		}
		return id(prefix, n)
	}
	number := func() *int64 {
		if r.Intn(2) == 0 {
			return nil
		}
		v := int64(r.Intn(9) - 4)
		return &v
	}
	resources := []ResourceID{"hp", "mp"}
	b := &Blueprint{}
	for n := r.Intn(7); n > 0; n-- {
		c := Character{ID: CharacterID(id("c", 5)), Stats: map[StatID]int64{}, Resources: map[ResourceID]Resource{}}
		for _, s := range drawn("s", 4)[1:] {
			if r.Intn(3) > 0 {
				c.Stats[StatID(s)] = int64(r.Intn(20))
			}
		}
		for _, res := range resources {
			if r.Intn(3) > 0 {
				c.Resources[res] = Resource{Current: int64(r.Intn(3)), Minimum: int64(r.Intn(2)), Maximum: 5}
			}
		}
		b.Characters = append(b.Characters, c)
	}
	for n := r.Intn(3); n > 0; n-- {
		b.Locations = append(b.Locations, Location{ID: LocationID(id("c", 6))})
	}
	for n := r.Intn(8); n > 0; n-- {
		d := ConditionDefinition{ID: ConditionTypeID(id("t", 6)), Instances: Coexist}
		for k := r.Intn(3); k > 0; k-- {
			d.Includes = append(d.Includes, ConditionTypeID(id("t", 7)))
		}
		if r.Intn(3) == 0 {
			res := resources[r.Intn(2)]
			d.HeldAtMinimum = &res
		}
		if r.Intn(2) == 0 {
			d.ModifierSelection = LatestModifiers
		}
		for k := r.Intn(4); k > 0; k-- {
			m := ConditionModifier{ID: ConditionModifierID(id("m", 3)), Stat: StatID(id("s", 4))}
			switch r.Intn(4) {
			case 0:
				m.Bound = []ModifierBound{AtLeast, AtMost}[r.Intn(2)]
			case 1:
				m.Roll = []RollMode{Advantage, Disadvantage, AutoFail}[r.Intn(3)]
			case 2:
				m.ResourceMaximum = resources[r.Intn(2)]
			}
			if m.Fixed = number(); m.Fixed == nil {
				m.Magnitude = r.Intn(2) == 0
			}
			d.Modifiers = append(d.Modifiers, m)
		}
		b.ConditionDefinitions = append(b.ConditionDefinitions, d)
	}
	for n := r.Intn(10); n > 0; n-- {
		c := ConditionInstance{ID: ConditionInstanceID(id("i", 8)), Type: ConditionTypeID(id("t", 7)), Target: CharacterID(id("c", 6)), Magnitude: number(), AppliedRevision: Revision(r.Intn(3))}
		if r.Intn(3) > 0 {
			c.Cause = &ConditionCause{Operation: r.Intn(3)}
			if r.Intn(2) == 0 {
				c.Cause.AbilityOrigin = &AbilityOrigin{Step: r.Intn(2)}
				for k := r.Intn(3); k > 0; k-- {
					c.Cause.AbilityOrigin.Path = append(c.Cause.AbilityOrigin.Path, r.Intn(2))
				}
			}
		}
		b.Conditions = append(b.Conditions, c)
	}
	if r.Intn(4) > 0 {
		b.Equipment = &EquipmentRules{}
		for n := r.Intn(4); n > 0; n-- {
			d := EquipmentDefinition{ID: EquipmentDefinitionID(id("e", 3))}
			for k := 1 + r.Intn(2); k > 0; k-- {
				m := EquipmentMode{ID: EquipmentModeID(id("m", 2))}
				for j := r.Intn(3); j > 0; j-- {
					m.Modifiers = append(m.Modifiers, EquipmentModifier{ID: id("v", 3), Stat: StatID(id("s", 4)), Amount: int64(r.Intn(5) - 2), Requires: ConditionTypeID(maybe("t", 7))})
				}
				d.Modes = append(d.Modes, m)
			}
			b.Equipment.Definitions = append(b.Equipment.Definitions, d)
		}
		for n := r.Intn(3); n > 0; n-- {
			b.Equipment.Derived = append(b.Equipment.Derived, DerivedStatRule{Stat: StatID(id("s", 4))})
		}
	}
	for n := r.Intn(6); n > 0; n-- {
		i := Item{ID: ItemID(id("x", 5)), Wearer: CharacterID(maybe("c", 6))}
		if r.Intn(4) > 0 {
			i.Equipment = &EquipmentInstance{Definition: EquipmentDefinitionID(id("e", 4)), Mode: EquipmentModeID(id("m", 3))}
		}
		b.Items = append(b.Items, i)
	}
	for n := r.Intn(5); n > 0; n-- {
		e := Effect{ID: EffectID(id("f", 4)), Stat: StatID(id("s", 4)), Amount: int64(r.Intn(5) - 2), Mode: OnCharacter, Target: Ref{CharacterKind, id("c", 6)}}
		if r.Intn(2) == 0 {
			e.Mode, e.Target = WhileWorn, Ref{ItemKind, id("x", 6)}
		}
		b.Effects = append(b.Effects, e)
	}
	return b
}

// The scans the derivation replaced, its oracle. A scan read the modifiers
// of an instance's missing definition and panicked; Validate rejects such an
// instance before any explanation, and a derivation gives it no modifiers.

func scanSuppressedBy(b *Blueprint, c ConditionInstance) *ConditionInstanceID {
	d := conditionDefinition(b, c.Type)
	if d == nil || d.ModifierSelection != LatestModifiers {
		return nil
	}
	winner := c
	for _, other := range b.Conditions {
		if other.Type == c.Type && other.Target == c.Target && laterCondition(other, winner) {
			winner = other
		}
	}
	if winner.ID == c.ID {
		return nil
	}
	id := winner.ID
	return &id
}

func scanConditionSuppressedBy(b *Blueprint, e Effect) *ConditionInstanceID {
	if e.ConditionOrigin == nil {
		return nil
	}
	i := conditionIndex(b, e.ConditionOrigin.Instance)
	if i < 0 {
		return nil
	}
	return scanSuppressedBy(b, b.Conditions[i])
}

func scanAppliesTo(b *Blueprint, e Effect, id CharacterID) bool {
	if e.Mode == OnCharacter {
		return e.Target.ID == string(id)
	}
	i := item(b, ItemID(e.Target.ID))
	return i != nil && i.Wearer == id
}

func scanAllEffects(b *Blueprint) []Effect {
	var conditions []Effect
	for _, c := range b.Conditions {
		conditions = append(conditions, instanceEffects(c, conditionDefinition(b, c.Type))...)
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].ID < conditions[j].ID })
	var equipment []Effect
	for _, i := range b.Items {
		m := b.wornMode(i)
		if m == nil {
			continue
		}
		for _, v := range m.Modifiers {
			if v.Requires != "" && !b.hasCondition(i.Wearer, v.Requires) {
				continue
			}
			o := EquipmentOrigin{Item: i.ID, Definition: i.Equipment.Definition, Mode: m.ID, Member: v.ID}
			equipment = append(equipment, Effect{ID: EffectID(equipmentID("effect", o)), Source: Ref{ItemKind, string(i.ID)}, Mode: WhileWorn, Target: Ref{ItemKind, string(i.ID)}, Stat: v.Stat, Amount: v.Amount, EquipmentOrigin: &o})
		}
	}
	sort.Slice(equipment, func(i, j int) bool { return equipment[i].ID < equipment[j].ID })
	out := append([]Effect{}, b.Effects...)
	out = append(out, conditions...)
	out = append(out, equipment...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func scanHeldConditions(b *Blueprint, id CharacterID) []HeldCondition {
	var out []HeldCondition
	for i := range b.ConditionDefinitions {
		if d := &b.ConditionDefinitions[i]; heldAt(b, id, d) {
			out = append(out, HeldCondition{d.ID, *d.HeldAtMinimum})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

func scanStatBounds(b *Blueprint, id CharacterID, stat StatID) []StatBound {
	var out []StatBound
	for _, c := range b.Conditions {
		if d := conditionDefinition(b, c.Type); c.Target == id && d != nil {
			for _, m := range d.Modifiers {
				if m.Bound != "" && m.Stat == stat && scanSuppressedBy(b, c) == nil {
					out = append(out, StatBound{Instance: c.ID, Type: c.Type, Modifier: m.ID, Bound: m.Bound, Amount: modifierAmount(m, c)})
				}
			}
		}
	}
	for i := range b.ConditionDefinitions {
		d := &b.ConditionDefinitions[i]
		if !heldAt(b, id, d) {
			continue
		}
		for _, m := range d.Modifiers {
			if m.Bound != "" && m.Stat == stat {
				out = append(out, StatBound{HeldAtMinimum: *d.HeldAtMinimum, Type: d.ID, Modifier: m.ID, Bound: m.Bound, Amount: modifierAmount(m, ConditionInstance{})})
			}
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

func scanRollSources(b *Blueprint, id CharacterID, stat StatID, recipe *RecipeExplanation) []RollSource {
	reads := map[StatID]bool{stat: true}
	if recipe != nil {
		for _, t := range recipe.Terms {
			if t.Active {
				reads[t.Term.Stat] = true
			}
		}
	}
	var out []RollSource
	for _, c := range b.Conditions {
		if d := conditionDefinition(b, c.Type); c.Target == id && d != nil {
			for _, m := range d.Modifiers {
				if m.Roll != "" && reads[m.Stat] && scanSuppressedBy(b, c) == nil {
					out = append(out, RollSource{Instance: c.ID, Type: c.Type, Modifier: m.ID, Stat: m.Stat, Roll: m.Roll})
				}
			}
		}
	}
	for i := range b.ConditionDefinitions {
		d := &b.ConditionDefinitions[i]
		if !heldAt(b, id, d) {
			continue
		}
		for _, m := range d.Modifiers {
			if m.Roll != "" && reads[m.Stat] {
				out = append(out, RollSource{HeldAtMinimum: *d.HeldAtMinimum, Type: d.ID, Modifier: m.ID, Stat: m.Stat, Roll: m.Roll})
			}
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
