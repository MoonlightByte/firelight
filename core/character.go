package core

import (
	"fmt"
	"unicode/utf8"
)

// Initial creation bounds do not retroactively restrict accepted genesis maps.
const MaxCreatedCharacterStats = 128
const MaxCreatedCharacterResources = 32

func cloneCharacter(c *Character) *Character {
	if c == nil {
		return nil
	}
	v := *c
	v.Buffers = append([]ResourceBuffer(nil), c.Buffers...)
	v.DamageTraits = append([]DamageTrait(nil), c.DamageTraits...)
	v.Stats = map[StatID]int64{}
	for k, value := range c.Stats {
		v.Stats[k] = value
	}
	v.Resources = map[ResourceID]Resource{}
	for k, value := range c.Resources {
		v.Resources[k] = value
	}
	return &v
}

// checkCharacterFacts checks local immutable facts, never effective values.
func checkCharacterFacts(l lookups, c Character) error {
	if err := checkRef(Ref{LocationKind, string(c.Location)}, l.kindOf); err != nil {
		return err
	}
	for _, s := range keys(c.Stats) {
		if !validID(string(s)) {
			return fail("E_ID", string(c.ID), "stat", "valid stat ID", string(s), "invalid stat ID")
		}
	}
	for _, id := range keys(c.Resources) {
		r := c.Resources[id]
		if !validID(string(id)) {
			return fail("E_ID", string(c.ID), "resource", "valid resource ID", string(id), "invalid resource ID")
		}
		if r.Minimum > r.Current || r.Current > r.Maximum {
			return fail("E_BOUNDS", string(c.ID), string(id), "minimum <= current <= maximum", fmt.Sprint(r), "invalid resource bounds")
		}
	}
	if err := checkDamageTraits(c); err != nil {
		return err
	}
	return l.blueprint().checkBuffers(c)
}

func (b Blueprint) checkCreateCharacter(c Character) error {
	if err := b.checkBattleBirth(c); err != nil {
		return err
	}
	if !validID(string(c.ID)) || !utf8.ValidString(c.Name) {
		return fail("E_ID", string(c.ID), "identity", "valid explicit ID and UTF-8 name", "invalid", "invalid identity or name")
	}
	if _, exists := b.KindOf(string(c.ID)); exists {
		return fail("E_DUPLICATE", string(c.ID), "identity", "unique canonical ID", string(c.ID), "duplicate canonical identity")
	}
	if len(c.Stats) > MaxCreatedCharacterStats || len(c.Resources) > MaxCreatedCharacterResources {
		return fail("E_LIMIT", string(c.ID), "character", "at most 128 stats and 32 resources", fmt.Sprintf("%d stats/%d resources", len(c.Stats), len(c.Resources)), "created character exceeds field bounds")
	}
	if err := checkCharacterFacts(scan{&b}, c); err != nil {
		return err
	}
	if err := checkTraitsHaveLaw(&b, c); err != nil {
		return err
	}
	// A recipe only applies when its target stat is declared on this character.
	// Check inputs directly without constructing/evaluating an intermediate world.
	if b.Equipment != nil {
		for _, d := range b.Equipment.Derived {
			if _, applies := c.Stats[d.Stat]; !applies {
				continue
			}
			terms := append([]StatTerm(nil), d.Terms...)
			for _, r := range d.Recipes {
				terms = append(terms, r.Terms...)
			}
			for _, term := range terms {
				if _, exists := c.Stats[term.Stat]; !exists {
					return fail("E_STAT", string(c.ID), string(term.Stat), "declared stat", "missing", "character lacks stat required by derived recipe")
				}
			}
		}
	}
	return nil
}
