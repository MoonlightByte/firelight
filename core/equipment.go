package core

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"unicode/utf8"
)

type EquipmentDefinitionID string
type EquipmentModeID string
type SlotID string
type EquipmentGrantID string

const EquipmentVersion = "nql-equipment-v1"

// EquipmentRules is immutable authored content. Occupancy, contributions and
// grants are derived from Item.Equipment plus the existing canonical wearer.
type EquipmentRules struct {
	Version     string                `json:"version"`
	Slots       []SlotDefinition      `json:"slots"`
	Derived     []DerivedStatRule     `json:"derived"`
	Definitions []EquipmentDefinition `json:"definitions"`
}
type SlotDefinition struct {
	ID       SlotID `json:"id"`
	Capacity int64  `json:"capacity"`
}
type SlotUse struct {
	Slot   SlotID `json:"slot"`
	Amount int64  `json:"amount"`
}
type EquipmentInstance struct {
	Definition EquipmentDefinitionID `json:"definition"`
	Mode       EquipmentModeID       `json:"mode"`
}
type EquipmentDefinition struct {
	ID          EquipmentDefinitionID `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	DefaultMode EquipmentModeID       `json:"default_mode"`
	Modes       []EquipmentMode       `json:"modes"`
}
type EquipmentMode struct {
	EquipTicks   int64               `json:"equip_ticks,omitempty"`
	UnequipTicks int64               `json:"unequip_ticks,omitempty"`
	ID           EquipmentModeID     `json:"id"`
	Slots        []SlotUse           `json:"slots"`
	Modifiers    []EquipmentModifier `json:"modifiers"`
	Overrides    []StatBaseOverride  `json:"overrides"`
	Attacks      []EquipmentAttack   `json:"attacks"`
}
type EquipmentModifier struct {
	ID       string          `json:"id"`
	Stat     StatID          `json:"stat"`
	Amount   int64           `json:"amount"`
	Requires ConditionTypeID `json:"requires,omitempty"`
}

// StatTerm reads a non-derived effective stat. Division is mathematical floor;
// Ceiling is an upper bound only. With Steps, the floored quotient selects the
// Value of the last step whose At it reaches, and 0 below the first: a table
// such as experience thresholds. A term has Steps or a Ceiling, not both.
// There is no expression language or callback.
// The term counts only while its character has the Requires Condition type
// (when set) and lacks the Unless Condition type (when set), and, in an
// equipment recipe, wears nothing occupying one of its UnlessSlots.
type StatTerm struct {
	Stat        StatID          `json:"stat"`
	Offset      int64           `json:"offset"`
	Divisor     int64           `json:"divisor"`
	Ceiling     *int64          `json:"ceiling,omitempty"`
	Steps       []TermStep      `json:"steps,omitempty"`
	Requires    ConditionTypeID `json:"requires,omitempty"`
	Unless      ConditionTypeID `json:"unless,omitempty"`
	UnlessSlots []SlotID        `json:"unless_slots,omitempty"`
}

// MaxTermSteps bounds one term's table.
const MaxTermSteps = 32

// TermStep is one row of a term's table: from At upward, the term is Value.
type TermStep struct {
	At    int64 `json:"at"`
	Value int64 `json:"value"`
}
type DerivedStatRule struct {
	Stat  StatID     `json:"stat"`
	Terms []StatTerm `json:"terms"`
	// Recipes are gated alternatives to the default recipe (the terms on the
	// declared base, or a worn override). The stat takes the eligible recipe
	// with the highest total, base plus active terms; ties keep the default,
	// then the earlier recipe. A rule without recipes derives as before.
	Recipes []AlternativeRecipe `json:"recipes,omitempty"`
}

// AlternativeRecipe replaces the default recipe's base and terms while its
// character has the Requires Condition type and wears nothing occupying one
// of its UnlessSlots, and only when its total is the highest: an effect such
// as Mage Armor (13 + Dexterity while unarmored) that never adds to another
// base recipe such as Unarmored Defense.
type AlternativeRecipe struct {
	Name        string          `json:"name"`
	Base        int64           `json:"base"`
	Requires    ConditionTypeID `json:"requires"`
	UnlessSlots []SlotID        `json:"unless_slots,omitempty"`
	Terms       []StatTerm      `json:"terms"`
}

// MaxDerivedRecipes bounds one derived stat's alternative recipes.
const MaxDerivedRecipes = 64

type ScalarRecipe struct {
	Base  int64      `json:"base"`
	Terms []StatTerm `json:"terms"`
}
type StatBaseOverride struct {
	Stat   StatID       `json:"stat"`
	Recipe ScalarRecipe `json:"recipe"`
}
type EquipmentAttack struct {
	Ranged *RangedProfile `json:"ranged,omitempty"`
	Melee  *MeleeProfile  `json:"melee,omitempty"`
	Thrown *ThrownProfile `json:"thrown,omitempty"`
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	ToHit  ScalarRecipe   `json:"to_hit"`
	Damage DamageGrant    `json:"damage"`
	// The attack is granted only while its gate admits the holder.
	AttackGate
}

// AttackGate admits a holder that has the Requires Condition type, when one
// is named, and wears nothing occupying an Unless slot.
type AttackGate struct {
	Requires    ConditionTypeID `json:"requires,omitempty"`
	UnlessSlots []SlotID        `json:"unless_slots,omitempty"`
}

// DamageGrant is an authored capability description, not a damage executor.
type DamageGrant struct {
	Count int64        `json:"count"`
	Sides int64        `json:"sides"`
	Kind  string       `json:"kind"`
	Bonus ScalarRecipe `json:"bonus"`
	// The grant rolls the largest admitted die: the base, or an upgrade
	// whose gate admits the holder, by count x sides, ties keeping the
	// earlier. The bonus is unchanged.
	Upgrades []DamageUpgrade `json:"upgrades,omitempty"`
}

// DamageUpgrade is a larger die an attack rolls while its gate admits the
// holder, such as a Monk's Martial Arts die. Its gate names a Condition type.
type DamageUpgrade struct {
	Count int64 `json:"count"`
	Sides int64 `json:"sides"`
	AttackGate
}

// EquipmentOrigin is where a grant, effect or override comes from: a worn
// item's definition and mode, or, for an attack, a Condition type present on
// the character (Condition, with no item).
type EquipmentOrigin struct {
	Item       ItemID                `json:"item,omitempty"`
	Definition EquipmentDefinitionID `json:"definition,omitempty"`
	Mode       EquipmentModeID       `json:"mode,omitempty"`
	Condition  ConditionTypeID       `json:"condition,omitempty"`
	Member     string                `json:"member"`
}
type TermExplanation struct {
	Term   StatTerm `json:"term"`
	Input  int64    `json:"input"`
	Active bool     `json:"active"`
	Exact  string   `json:"exact"`
}
type RecipeExplanation struct {
	Base   int64             `json:"base"`
	Origin *EquipmentOrigin  `json:"origin,omitempty"`
	Terms  []TermExplanation `json:"terms"`
	// Name and Requires identify a chosen alternative recipe; Candidates
	// lists every recipe of a rule that declares alternatives, the default
	// first. All three are absent for a rule without recipes.
	Name       string            `json:"name,omitempty"`
	Requires   ConditionTypeID   `json:"requires,omitempty"`
	Candidates []RecipeCandidate `json:"candidates,omitempty"`
}

// RecipeCandidate is one recipe a derived stat chose among: Name is empty for
// the default recipe; Exact is the total (base plus active terms) of an
// eligible recipe.
type RecipeCandidate struct {
	Name     string `json:"name,omitempty"`
	Eligible bool   `json:"eligible"`
	Exact    string `json:"exact,omitempty"`
}
type AttackGrant struct {
	Ranged      *RangedProfile    `json:"ranged,omitempty"`
	Melee       *MeleeProfile     `json:"melee,omitempty"`
	Thrown      *ThrownProfile    `json:"thrown,omitempty"`
	ID          EquipmentGrantID  `json:"id"`
	Origin      EquipmentOrigin   `json:"origin"`
	Name        string            `json:"name"`
	ToHit       int64             `json:"to_hit"`
	HitRecipe   RecipeExplanation `json:"hit_recipe"`
	DamageCount int64             `json:"damage_count"`
	DamageSides int64             `json:"damage_sides"`
	DamageKind  string            `json:"damage_kind"`
	DamageBonus int64             `json:"damage_bonus"`
	BonusRecipe RecipeExplanation `json:"bonus_recipe"`
}

func equipmentFault(id, field, expected, actual string) error {
	return fail("E_EQUIPMENT", id, field, expected, actual, "invalid equipment "+field)
}
func (b *Blueprint) equipmentDefinition(id EquipmentDefinitionID) *EquipmentDefinition {
	if b.Equipment != nil {
		for i := range b.Equipment.Definitions {
			if b.Equipment.Definitions[i].ID == id {
				return &b.Equipment.Definitions[i]
			}
		}
	}
	return nil
}
func equipmentMode(d *EquipmentDefinition, id EquipmentModeID) *EquipmentMode {
	if d != nil {
		for i := range d.Modes {
			if d.Modes[i].ID == id {
				return &d.Modes[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) wornMode(i Item) *EquipmentMode {
	if i.Equipment == nil || i.Wearer == "" {
		return nil
	}
	return equipmentMode(b.equipmentDefinition(i.Equipment.Definition), i.Equipment.Mode)
}
func (b *Blueprint) derivedRule(stat StatID) *DerivedStatRule {
	if b.Equipment != nil {
		for i := range b.Equipment.Derived {
			if b.Equipment.Derived[i].Stat == stat {
				return &b.Equipment.Derived[i]
			}
		}
	}
	return nil
}

// checkEquipmentTerms checks an Ability's recipe terms (a roll bonus, a
// capacity formula). An Ability reads no worn equipment, so a slot gate is
// refused here; equipment recipes are checked by the function below.
func (b *Blueprint) checkEquipmentTerms(id string, terms []StatTerm) error {
	for _, t := range terms {
		if len(t.UnlessSlots) > 0 {
			return equipmentFault(id, "unless_slots", "no slot gate in an Ability recipe", string(t.UnlessSlots[0]))
		}
	}
	return checkEquipmentTerms(scan{b}, id, terms)
}

// checkAttackGate checks a gate's Condition type and slots; an upgrade's
// gate must name a type.
func checkAttackGate(x *derivation, owner string, g AttackGate, upgrade bool, slots map[SlotID]bool) error {
	if g.Requires != "" && x.definition(g.Requires) == nil || upgrade && g.Requires == "" {
		return equipmentFault(owner, "requires", "declared Condition type", string(g.Requires))
	}
	if len(g.UnlessSlots) > MaxUnlessSlots {
		return equipmentFault(owner, "unless_slots", fmt.Sprintf("at most %d", MaxUnlessSlots), fmt.Sprint(len(g.UnlessSlots)))
	}
	seen := map[SlotID]bool{}
	for _, s := range g.UnlessSlots {
		if !slots[s] || seen[s] {
			return equipmentFault(owner, "unless_slots", "unique declared slots", string(s))
		}
		seen[s] = true
	}
	return nil
}

// checkEquipmentAttacks checks one owner's attacks: an equipment mode's, or a
// Condition type's.
func checkEquipmentAttacks(x *derivation, owner string, attacks []EquipmentAttack, slots map[SlotID]bool) error {
	seen := map[string]bool{}
	for _, v := range attacks {
		if err := x.b.checkRangedProfile(v); err != nil {
			return err
		}
		if !validID(v.ID) || seen[v.ID] || !utf8.ValidString(v.Name) || !validID(v.Damage.Kind) || !flatDamage(v.Damage.Count, v.Damage.Sides) && (v.Damage.Count < 1 || v.Damage.Count > 100 || v.Damage.Sides < 2 || v.Damage.Sides > 1000) {
			return equipmentFault(owner, "attack", "unique ID, valid name/kind, 1..100 dice of 2..1000 sides", v.ID)
		}
		if v.Melee != nil && (v.Melee.Reach < 1 || v.Melee.Reach > 1000000 || !validID(string(v.Melee.Stat))) {
			return equipmentFault(owner, "melee", "positive reach <=1000000 and explicit stat", v.ID)
		}
		if p := v.Thrown; p != nil && (v.Melee != nil || v.Ranged != nil || !validID(string(p.Stat)) || p.Normal < 1 || p.Maximum < p.Normal || p.Maximum > 1000000) {
			return equipmentFault(owner, "thrown", "one profile, explicit stat and ranges 1 <= normal <= maximum <= 1000000", v.ID)
		}
		seen[v.ID] = true
		if err := checkAttackGate(x, owner, v.AttackGate, false, slots); err != nil {
			return err
		}
		if len(v.Damage.Upgrades) > MaxDamageUpgrades {
			return equipmentFault(owner, "upgrades", fmt.Sprintf("at most %d", MaxDamageUpgrades), v.ID)
		}
		for _, u := range v.Damage.Upgrades {
			if u.Count < 1 || u.Count > 100 || u.Sides < 2 || u.Sides > 1000 {
				return equipmentFault(owner, "upgrade", "1..100 dice of 2..1000 sides", v.ID)
			}
			if err := checkAttackGate(x, owner, u.AttackGate, true, slots); err != nil {
				return err
			}
		}
		if err := checkEquipmentTerms(x, owner, v.ToHit.Terms); err != nil {
			return err
		}
		if err := checkEquipmentTerms(x, owner, v.Damage.Bonus.Terms); err != nil {
			return err
		}
	}
	return nil
}
func checkEquipmentTerms(l lookups, id string, terms []StatTerm) error {
	if len(terms) > 8 {
		return equipmentFault(id, "terms", "at most 8", fmt.Sprint(len(terms)))
	}
	for _, t := range terms {
		if !validID(string(t.Stat)) || t.Divisor <= 0 {
			return equipmentFault(id, "term", "valid stat and positive divisor", string(t.Stat))
		}
		if len(t.Steps) > MaxTermSteps {
			return equipmentFault(id, "steps", "at most 32", fmt.Sprint(len(t.Steps)))
		}
		if len(t.Steps) > 0 && t.Ceiling != nil {
			return equipmentFault(id, "steps", "steps or a ceiling, not both", string(t.Stat))
		}
		for i := 1; i < len(t.Steps); i++ {
			if t.Steps[i].At <= t.Steps[i-1].At {
				return equipmentFault(id, "steps", "strictly increasing step inputs", fmt.Sprint(t.Steps[i].At))
			}
		}
		if l.derivedRule(t.Stat) != nil {
			return equipmentFault(id, "input", "non-derived effective stat", string(t.Stat))
		}
		if t.Requires != "" && l.definition(t.Requires) == nil {
			return equipmentFault(id, "requires", "declared Condition type", string(t.Requires))
		}
		// A term gated on and against one type could never count.
		if t.Unless != "" && (l.definition(t.Unless) == nil || t.Unless == t.Requires) {
			return equipmentFault(id, "unless", "declared Condition type other than requires", string(t.Unless))
		}
		if len(t.UnlessSlots) > MaxUnlessSlots {
			return equipmentFault(id, "unless_slots", fmt.Sprintf("at most %d", MaxUnlessSlots), fmt.Sprint(len(t.UnlessSlots)))
		}
		seen := map[SlotID]bool{}
		for _, s := range t.UnlessSlots {
			if !declaredSlot(l.blueprint(), s) || seen[s] {
				return equipmentFault(id, "unless_slots", "unique declared slots", string(s))
			}
			seen[s] = true
		}
	}
	return nil
}

// checkAlternativeRecipes checks a rule's recipes: unique valid names, a
// declared requires type, unique declared unless slots and valid terms.
func checkAlternativeRecipes(l lookups, d DerivedStatRule) error {
	if len(d.Recipes) > MaxDerivedRecipes {
		return equipmentFault(string(d.Stat), "recipes", fmt.Sprintf("at most %d", MaxDerivedRecipes), fmt.Sprint(len(d.Recipes)))
	}
	names := map[string]bool{}
	for _, r := range d.Recipes {
		if !validID(r.Name) || names[r.Name] {
			return equipmentFault(string(d.Stat), "recipe", "unique valid recipe name", r.Name)
		}
		names[r.Name] = true
		if r.Requires == "" || l.definition(r.Requires) == nil {
			return equipmentFault(r.Name, "requires", "declared Condition type", string(r.Requires))
		}
		if len(r.UnlessSlots) > MaxUnlessSlots {
			return equipmentFault(r.Name, "unless_slots", fmt.Sprintf("at most %d", MaxUnlessSlots), fmt.Sprint(len(r.UnlessSlots)))
		}
		seen := map[SlotID]bool{}
		for _, s := range r.UnlessSlots {
			if !declaredSlot(l.blueprint(), s) || seen[s] {
				return equipmentFault(r.Name, "unless_slots", "unique declared slots", string(s))
			}
			seen[s] = true
		}
		if err := checkEquipmentTerms(l, r.Name, r.Terms); err != nil {
			return err
		}
	}
	return nil
}

// declaredSlot reports whether b's Equipment rules declare slot s.
func declaredSlot(b *Blueprint, s SlotID) bool {
	if b == nil || b.Equipment == nil {
		return false
	}
	for _, v := range b.Equipment.Slots {
		if v.ID == s {
			return true
		}
	}
	return false
}

// World-size limits for a public game, not save-profile limits.
const (
	MaxWorldCharacters           = 4096
	MaxWorldItems                = 32768
	MaxWorldLocations            = 4096
	MaxWorldConditionDefinitions = 4096
	MaxWorldConditions           = 32768
	MaxWorldEquipmentDefinitions = 4096
	MaxEquipmentSlots            = 16
	MaxEquipmentDerived          = 64
	// MaxConditionAttacks bounds a Condition type's attacks, as a mode's.
	MaxConditionAttacks = 8
	// MaxDamageUpgrades bounds an attack's die upgrades, and MaxUnlessSlots a
	// gate's slots.
	MaxDamageUpgrades = 8
	MaxUnlessSlots    = 8
)

// validateEquipment checks the equipment of x's Blueprint, which does not
// change during the check.
func validateEquipment(x *derivation) error {
	b := x.b
	if b.Equipment == nil {
		for _, i := range b.Items {
			if i.Equipment != nil {
				return equipmentFault(string(i.ID), "definition", "Equipment rules", "absent")
			}
		}
		if t := x.attackTypes(); len(t) > 0 {
			return equipmentFault(string(t[0].ID), "attacks", "Equipment rules", "absent")
		}
		return nil
	}
	e := b.Equipment
	if e.Version != EquipmentVersion {
		return equipmentFault("", "version", EquipmentVersion, e.Version)
	}
	if len(b.Characters) > MaxWorldCharacters || len(b.Items) > MaxWorldItems || len(b.Locations) > MaxWorldLocations || len(b.ConditionDefinitions) > MaxWorldConditionDefinitions || len(b.Conditions) > MaxWorldConditions {
		return equipmentFault("", "capacity", "native entity/Condition limits", "exceeded")
	}
	if len(e.Slots) > MaxEquipmentSlots || len(e.Derived) > MaxEquipmentDerived || len(e.Definitions) > MaxWorldEquipmentDefinitions {
		return equipmentFault("", "capacity", "16 slots, 64 derived stats, 4096 equipment definitions", "exceeded")
	}
	slots := map[SlotID]bool{}
	for _, s := range e.Slots {
		if !validID(string(s.ID)) || slots[s.ID] || s.Capacity <= 0 {
			return equipmentFault(string(s.ID), "slot", "unique valid slot and positive capacity", "invalid")
		}
		slots[s.ID] = true
	}
	derived := map[StatID]bool{}
	for _, d := range e.Derived {
		if !validID(string(d.Stat)) || derived[d.Stat] {
			return equipmentFault(string(d.Stat), "derived", "unique valid target stat", "invalid")
		}
		derived[d.Stat] = true
		if err := checkEquipmentTerms(x, string(d.Stat), d.Terms); err != nil {
			return err
		}
		if err := checkAlternativeRecipes(x, d); err != nil {
			return err
		}
		for _, c := range b.Characters {
			if _, ok := c.Stats[d.Stat]; ok {
				if err := checkTermInputs(x, c.ID, d.Terms); err != nil {
					return err
				}
				for _, r := range d.Recipes {
					if err := checkTermInputs(x, c.ID, r.Terms); err != nil {
						return err
					}
				}
			}
		}
	}
	definitions := map[EquipmentDefinitionID]bool{}
	for _, d := range e.Definitions {
		if !validID(string(d.ID)) || definitions[d.ID] || !utf8.ValidString(d.Name) || !utf8.ValidString(d.Description) || len(d.Modes) == 0 || len(d.Modes) > 8 {
			return equipmentFault(string(d.ID), "definition", "unique valid definition, UTF-8 text and 1..8 modes", "invalid")
		}
		definitions[d.ID] = true
		modes := map[EquipmentModeID]bool{}
		for _, m := range d.Modes {
			if m.EquipTicks < 0 || m.UnequipTicks < 0 || ((m.EquipTicks > 0 || m.UnequipTicks > 0) && b.Clock == nil) {
				return equipmentFault(string(d.ID), "timing", "nonnegative ticks with Clock for nonzero cost", string(m.ID))
			}
			if !validID(string(m.ID)) || modes[m.ID] || len(m.Slots) > 16 || len(m.Modifiers) > 16 || len(m.Overrides) > 16 || len(m.Attacks) > 8 {
				return equipmentFault(string(d.ID), "mode", "unique mode within collection bounds", string(m.ID))
			}
			modes[m.ID] = true
			uses := map[SlotID]bool{}
			for _, s := range m.Slots {
				if !slots[s.Slot] || uses[s.Slot] || s.Amount <= 0 {
					return equipmentFault(string(d.ID), "occupancy", "unique declared slot and positive use", string(s.Slot))
				}
				uses[s.Slot] = true
			}
			modifiers := map[string]bool{}
			for _, v := range m.Modifiers {
				if !validID(v.ID) || modifiers[v.ID] || !validID(string(v.Stat)) {
					return equipmentFault(string(d.ID), "modifier", "unique valid modifier/stat", v.ID)
				}
				modifiers[v.ID] = true
				if v.Requires != "" && x.definition(v.Requires) == nil {
					return equipmentFault(string(d.ID), "requires", "declared Condition type", string(v.Requires))
				}
			}
			overrides := map[StatID]bool{}
			for _, v := range m.Overrides {
				if !derived[v.Stat] || overrides[v.Stat] {
					return equipmentFault(string(d.ID), "override", "unique declared derived target", string(v.Stat))
				}
				overrides[v.Stat] = true
				if err := checkEquipmentTerms(x, string(d.ID), v.Recipe.Terms); err != nil {
					return err
				}
			}
			if err := checkEquipmentAttacks(x, string(d.ID), m.Attacks, slots); err != nil {
				return err
			}
		}
		if !modes[d.DefaultMode] {
			return equipmentFault(string(d.ID), "default_mode", "declared mode", string(d.DefaultMode))
		}
	}
	for _, t := range x.attackTypes() {
		if len(t.Attacks) > MaxConditionAttacks {
			return equipmentFault(string(t.ID), "attacks", fmt.Sprintf("at most %d", MaxConditionAttacks), fmt.Sprint(len(t.Attacks)))
		}
		if err := checkEquipmentAttacks(x, string(t.ID), t.Attacks, slots); err != nil {
			return err
		}
	}
	if err := validateEquipmentLoadout(x); err != nil {
		return err
	}
	seen := map[EffectID]bool{}
	for _, e := range b.Effects {
		seen[e.ID] = true
	}
	for _, e := range x.derivedConditionEffects() {
		seen[e.ID] = true
	}
	for _, e := range x.derivedEquipmentEffects() {
		if seen[e.ID] {
			return equipmentFault(string(e.ID), "effect_identity", "unique generated identity", "collision")
		}
		seen[e.ID] = true
	}
	grants := map[EquipmentGrantID]bool{}
	for _, c := range b.Characters {
		gs, err := x.equipmentGrants(c.ID)
		if err != nil {
			return err
		}
		for _, g := range gs {
			if grants[g.ID] {
				return equipmentFault(string(g.ID), "grant_identity", "unique generated identity", "collision")
			}
			grants[g.ID] = true
		}
	}
	return nil
}
func (b *Blueprint) checkTermInputs(id CharacterID, terms []StatTerm) error {
	return checkTermInputs(scan{b}, id, terms)
}
func checkTermInputs(l lookups, id CharacterID, terms []StatTerm) error {
	for _, t := range terms {
		if err := checkStat(l, id, t.Stat); err != nil {
			return err
		}
	}
	return nil
}

// Called at each loadout step, not just after the enclosing transaction.
func validateEquipmentLoadout(l lookups) error {
	b := l.blueprint()
	if b.Equipment == nil {
		return nil
	}
	used := map[CharacterID]map[SlotID]*big.Int{}
	bases := map[CharacterID]map[StatID]bool{}
	for _, i := range b.Items {
		if i.Equipment == nil {
			if i.Wearer != "" {
				return equipmentFault(string(i.ID), "definition", "typed equipment for worn item", "untyped")
			}
			continue
		}
		d := l.equipmentDefinition(i.Equipment.Definition)
		if d == nil {
			return equipmentFault(string(i.ID), "definition", "declared definition", string(i.Equipment.Definition))
		}
		if i.Wearer == "" {
			if i.Equipment.Mode != "" {
				return equipmentFault(string(i.ID), "mode", "empty for unworn item", string(i.Equipment.Mode))
			}
			continue
		}
		m := equipmentMode(d, i.Equipment.Mode)
		if m == nil {
			return equipmentFault(string(i.ID), "mode", "declared worn mode", string(i.Equipment.Mode))
		}
		if used[i.Wearer] == nil {
			used[i.Wearer] = map[SlotID]*big.Int{}
			bases[i.Wearer] = map[StatID]bool{}
		}
		for _, s := range m.Slots {
			if used[i.Wearer][s.Slot] == nil {
				used[i.Wearer][s.Slot] = new(big.Int)
			}
			used[i.Wearer][s.Slot].Add(used[i.Wearer][s.Slot], big.NewInt(s.Amount))
		}
		for _, v := range m.Modifiers {
			if err := checkStat(l, i.Wearer, v.Stat); err != nil {
				return err
			}
		}
		for _, v := range m.Overrides {
			if bases[i.Wearer][v.Stat] {
				return equipmentFault(string(i.ID), "override", "one active base recipe per stat", string(v.Stat))
			}
			bases[i.Wearer][v.Stat] = true
			if err := checkStat(l, i.Wearer, v.Stat); err != nil {
				return err
			}
			if err := checkTermInputs(l, i.Wearer, v.Recipe.Terms); err != nil {
				return err
			}
		}
		for _, a := range m.Attacks {
			if a.Ranged != nil {
				if err := checkStat(l, i.Wearer, a.Ranged.Stat); err != nil {
					return err
				}
			}
			if a.Melee != nil {
				if err := checkStat(l, i.Wearer, a.Melee.Stat); err != nil {
					return err
				}
			}
			if a.Thrown != nil {
				if err := checkStat(l, i.Wearer, a.Thrown.Stat); err != nil {
					return err
				}
			}
			if err := checkTermInputs(l, i.Wearer, a.ToHit.Terms); err != nil {
				return err
			}
			if err := checkTermInputs(l, i.Wearer, a.Damage.Bonus.Terms); err != nil {
				return err
			}
		}
	}
	for _, id := range keys(used) {
		for _, slot := range b.Equipment.Slots {
			if n := used[id][slot.ID]; n != nil && n.Cmp(big.NewInt(slot.Capacity)) > 0 {
				return equipmentFault(string(id), "capacity."+string(slot.ID), fmt.Sprint(slot.Capacity), n.String())
			}
		}
	}
	return nil
}
func (b *Blueprint) checkEquipMode(v Equip) error {
	i := item(b, v.Item)
	if i == nil {
		return b.CheckRef(Ref{ItemKind, string(v.Item)})
	}
	resolved, err := resolveCatalogItem(*i, b.itemDefinition(i.From), true)
	if err != nil {
		return err
	}
	i = &resolved
	if i.Equipment == nil {
		if b.Equipment != nil {
			return equipmentFault(string(i.ID), "definition", "typed equipment for equip", "untyped")
		}
		if v.Mode != "" {
			return equipmentFault(string(i.ID), "mode", "no mode for untyped item", string(v.Mode))
		}
		return nil
	}
	d := b.equipmentDefinition(i.Equipment.Definition)
	id := v.Mode
	if d != nil && id == "" {
		id = d.DefaultMode
	}
	if equipmentMode(d, id) == nil {
		return equipmentFault(string(i.ID), "mode", "declared mode", string(id))
	}
	return nil
}
func equipmentID(kind string, o EquipmentOrigin) string {
	h := sha256.New()
	for _, s := range []string{kind, string(o.Item), string(o.Definition), string(o.Mode), o.Member} {
		fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	return fmt.Sprintf("equipment:%s:%x", kind, h.Sum(nil))
}
func (d *derivation) derivedEquipmentEffects() []Effect {
	if d.equipmentEffectsBuilt {
		return d.equipmentEffects
	}
	var out []Effect
	for k := range d.b.Items {
		// Skip an unworn item before copying it.
		if d.b.Items[k].Wearer == "" {
			continue
		}
		i := d.b.Items[k]
		m := d.wornMode(i)
		if m == nil {
			continue
		}
		for _, v := range m.Modifiers {
			if v.Requires != "" && !d.hasCondition(i.Wearer, v.Requires) {
				continue
			}
			o := EquipmentOrigin{Item: i.ID, Definition: i.Equipment.Definition, Mode: m.ID, Member: v.ID}
			out = append(out, Effect{ID: EffectID(equipmentID("effect", o)), Source: Ref{ItemKind, string(i.ID)}, Mode: WhileWorn, Target: Ref{ItemKind, string(i.ID)}, Stat: v.Stat, Amount: v.Amount, EquipmentOrigin: &o})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	d.equipmentEffects, d.equipmentEffectsBuilt = out, true
	return out
}
func (d *derivation) evaluateTerms(id CharacterID, terms []StatTerm) (*big.Int, []TermExplanation, error) {
	sum := new(big.Int)
	var out []TermExplanation
	for _, t := range terms {
		input, err := d.explain(id, t.Stat)
		if err != nil {
			return nil, nil, err
		}
		active := (t.Requires == "" || d.hasCondition(id, t.Requires)) && (t.Unless == "" || !d.hasCondition(id, t.Unless)) &&
			!d.wearsSlot(id, t.UnlessSlots)
		x := TermExplanation{Term: cloneStatTerm(t), Input: input.Effective, Active: active, Exact: "0"}
		if x.Active {
			n := new(big.Int).Add(big.NewInt(input.Effective), big.NewInt(t.Offset))
			n.Div(n, big.NewInt(t.Divisor))
			if t.Ceiling != nil && n.Cmp(big.NewInt(*t.Ceiling)) > 0 {
				n.SetInt64(*t.Ceiling)
			}
			if len(t.Steps) > 0 {
				value := int64(0)
				for _, s := range t.Steps {
					if n.Cmp(big.NewInt(s.At)) >= 0 {
						value = s.Value
					}
				}
				n.SetInt64(value)
			}
			x.Exact = n.String()
			sum.Add(sum, n)
		}
		out = append(out, x)
	}
	return sum, out, nil
}
func (d *derivation) statBase(id CharacterID, stat StatID, declared int64) (int64, *big.Int, *RecipeExplanation, error) {
	rule := d.derivedRule(stat)
	if rule == nil {
		return declared, new(big.Int), nil, nil
	}
	base := declared
	terms := rule.Terms
	var origin *EquipmentOrigin
	for _, k := range d.wornBy(id) {
		i := d.b.Items[k]
		m := d.wornMode(i)
		if m == nil {
			continue
		}
		for _, v := range m.Overrides {
			if v.Stat == stat {
				base = v.Recipe.Base
				terms = v.Recipe.Terms
				origin = &EquipmentOrigin{Item: i.ID, Definition: i.Equipment.Definition, Mode: m.ID, Member: string(stat)}
			}
		}
	}
	sum, details, err := d.evaluateTerms(id, terms)
	if err != nil || len(rule.Recipes) == 0 {
		return base, sum, &RecipeExplanation{Base: base, Origin: origin, Terms: details}, err
	}
	// Gated alternatives: the highest total wins; ties keep the default
	// (or worn override), then the earlier recipe.
	best := new(big.Int).Add(big.NewInt(base), sum)
	out := &RecipeExplanation{Base: base, Origin: origin, Terms: details,
		Candidates: []RecipeCandidate{{Eligible: true, Exact: best.String()}}}
	chosenSum := sum
	for _, r := range rule.Recipes {
		if !d.hasCondition(id, r.Requires) || d.wearsSlot(id, r.UnlessSlots) {
			out.Candidates = append(out.Candidates, RecipeCandidate{Name: r.Name})
			continue
		}
		rsum, rdetails, err := d.evaluateTerms(id, r.Terms)
		if err != nil {
			return 0, nil, nil, err
		}
		total := new(big.Int).Add(big.NewInt(r.Base), rsum)
		out.Candidates = append(out.Candidates, RecipeCandidate{Name: r.Name, Eligible: true, Exact: total.String()})
		if total.Cmp(best) > 0 {
			best, chosenSum = total, rsum
			out.Base, out.Origin, out.Terms, out.Name, out.Requires = r.Base, nil, rdetails, r.Name, r.Requires
		}
	}
	return out.Base, chosenSum, out, nil
}
func evaluateRecipe(b *Blueprint, id CharacterID, r ScalarRecipe) (int64, RecipeExplanation, error) {
	return newDerivation(b).evaluateRecipe(id, r)
}
func (d *derivation) evaluateRecipe(id CharacterID, r ScalarRecipe) (int64, RecipeExplanation, error) {
	sum, terms, err := d.evaluateTerms(id, r.Terms)
	if err != nil {
		return 0, RecipeExplanation{}, err
	}
	sum.Add(sum, big.NewInt(r.Base))
	if !sum.IsInt64() {
		return 0, RecipeExplanation{}, fail("E_OVERFLOW", string(id), "recipe", "int64 final result", sum.String(), "equipment recipe exceeds int64")
	}
	return sum.Int64(), RecipeExplanation{Base: r.Base, Terms: terms}, nil
}
func (d *derivation) equipmentGrants(id CharacterID) ([]AttackGrant, error) {
	var out []AttackGrant
	for _, k := range d.wornBy(id) {
		i := d.b.Items[k]
		m := d.wornMode(i)
		if m == nil {
			continue
		}
		for _, a := range m.Attacks {
			grant, ok, err := d.evaluateEquipmentGrant(id, i, m.ID, a)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, grant)
			}
		}
	}
	for _, t := range d.attackTypes() {
		if !d.hasCondition(id, t.ID) {
			continue
		}
		for _, a := range t.Attacks {
			grant, ok, err := d.evaluateConditionGrant(id, t.ID, a)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, grant)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// attackTypes are the Condition types that declare attacks.
func (d *derivation) attackTypes() []*ConditionDefinition {
	var out []*ConditionDefinition
	for i := range d.b.ConditionDefinitions {
		if len(d.b.ConditionDefinitions[i].Attacks) > 0 {
			out = append(out, &d.b.ConditionDefinitions[i])
		}
	}
	return out
}

// conditionGrantID names a Condition type's attack on one character, in a
// namespace of its own: the same type grants its attacks to many characters.
func conditionGrantID(id CharacterID, t ConditionTypeID, attack string) EquipmentGrantID {
	h := sha256.New()
	for _, s := range []string{"grant", string(id), string(t), attack} {
		fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	return EquipmentGrantID(fmt.Sprintf("condition:grant:%x", h.Sum(nil)))
}
func (d *derivation) evaluateConditionGrant(id CharacterID, t ConditionTypeID, a EquipmentAttack) (AttackGrant, bool, error) {
	return d.evaluateGrant(id, EquipmentOrigin{Condition: t, Member: a.ID}, conditionGrantID(id, t, a.ID), a)
}

// Evaluate the selected member only: unrelated recipes may transiently exceed
// their final bound in a larger transaction that repairs them before publication.
func (d *derivation) evaluateEquipmentGrant(id CharacterID, i Item, mode EquipmentModeID, a EquipmentAttack) (AttackGrant, bool, error) {
	o := EquipmentOrigin{Item: i.ID, Definition: i.Equipment.Definition, Mode: mode, Member: a.ID}
	return d.evaluateGrant(id, o, EquipmentGrantID(equipmentID("grant", o)), a)
}

// evaluateGrant reports false, with no grant, while a's gate shuts out id.
func (d *derivation) evaluateGrant(id CharacterID, o EquipmentOrigin, grant EquipmentGrantID, a EquipmentAttack) (AttackGrant, bool, error) {
	if !d.admits(id, a.AttackGate) {
		return AttackGrant{}, false, nil
	}
	hit, hp, err := d.evaluateRecipe(id, a.ToHit)
	if err != nil {
		return AttackGrant{}, false, err
	}
	bonus, bp, err := d.evaluateRecipe(id, a.Damage.Bonus)
	if err != nil {
		return AttackGrant{}, false, err
	}
	count, sides := a.Damage.Count, a.Damage.Sides
	for _, u := range a.Damage.Upgrades {
		if u.Count*u.Sides > count*sides && d.admits(id, u.AttackGate) {
			count, sides = u.Count, u.Sides
		}
	}
	return AttackGrant{Ranged: copyPtr(a.Ranged), Melee: copyPtr(a.Melee), Thrown: copyPtr(a.Thrown), ID: grant, Origin: o, Name: a.Name, ToHit: hit, HitRecipe: hp, DamageCount: count, DamageSides: sides, DamageKind: a.Damage.Kind, DamageBonus: bonus, BonusRecipe: bp}, true, nil
}

// admits reports whether g admits holder id: it has g's Condition type, when
// one is named, and wears nothing occupying one of g's slots.
func (d *derivation) admits(id CharacterID, g AttackGate) bool {
	if g.Requires != "" && !d.hasCondition(id, g.Requires) {
		return false
	}
	return !d.wearsSlot(id, g.UnlessSlots)
}

// wearsSlot reports whether an item id wears occupies one of slots.
func (d *derivation) wearsSlot(id CharacterID, slots []SlotID) bool {
	if len(slots) == 0 {
		return false
	}
	for _, k := range d.wornBy(id) {
		if m := d.wornMode(d.b.Items[k]); m != nil {
			for _, s := range m.Slots {
				if slices.Contains(slots, s.Slot) {
					return true
				}
			}
		}
	}
	return false
}

// maxDamageCount is the most dice an attack can roll before a critical.
func maxDamageCount(a EquipmentAttack) int64 {
	n := a.Damage.Count
	for _, u := range a.Damage.Upgrades {
		n = max(n, u.Count)
	}
	return n
}
func selectedEquipmentGrant(b *Blueprint, id CharacterID, grant EquipmentGrantID) (AttackGrant, bool, error) {
	d := newDerivation(b)
	for _, k := range d.wornBy(id) {
		i := b.Items[k]
		m := d.wornMode(i)
		if m == nil {
			continue
		}
		for _, a := range m.Attacks {
			o := EquipmentOrigin{Item: i.ID, Definition: i.Equipment.Definition, Mode: m.ID, Member: a.ID}
			if EquipmentGrantID(equipmentID("grant", o)) == grant {
				return d.evaluateEquipmentGrant(id, i, m.ID, a)
			}
		}
	}
	for _, t := range d.attackTypes() {
		if !d.hasCondition(id, t.ID) {
			continue
		}
		for _, a := range t.Attacks {
			if conditionGrantID(id, t.ID, a.ID) == grant {
				return d.evaluateConditionGrant(id, t.ID, a)
			}
		}
	}
	return AttackGrant{}, false, nil
}

func cloneStatTerm(t StatTerm) StatTerm {
	t.Ceiling = copyPtr(t.Ceiling)
	t.Steps = append([]TermStep(nil), t.Steps...)
	t.UnlessSlots = append([]SlotID(nil), t.UnlessSlots...)
	return t
}
func cloneTerms(in []StatTerm) []StatTerm {
	if in == nil {
		return nil
	}
	out := make([]StatTerm, len(in))
	for i, t := range in {
		out[i] = cloneStatTerm(t)
	}
	return out
}
func cloneRecipe(r ScalarRecipe) ScalarRecipe { r.Terms = cloneTerms(r.Terms); return r }
func cloneEquipmentAttacks(in []EquipmentAttack) []EquipmentAttack {
	if in == nil {
		return nil
	}
	out := make([]EquipmentAttack, len(in))
	for k, a := range in {
		out[k] = a
		out[k].Melee = copyPtr(a.Melee)
		out[k].Ranged = copyPtr(a.Ranged)
		out[k].Thrown = copyPtr(a.Thrown)
		out[k].ToHit = cloneRecipe(a.ToHit)
		out[k].Damage.Bonus = cloneRecipe(a.Damage.Bonus)
		out[k].UnlessSlots = append([]SlotID(nil), a.UnlessSlots...)
		if a.Damage.Upgrades != nil {
			out[k].Damage.Upgrades = make([]DamageUpgrade, len(a.Damage.Upgrades))
			for n, u := range a.Damage.Upgrades {
				u.UnlessSlots = append([]SlotID(nil), u.UnlessSlots...)
				out[k].Damage.Upgrades[n] = u
			}
		}
	}
	return out
}
func cloneEquipment(e *EquipmentRules) *EquipmentRules {
	if e == nil {
		return nil
	}
	out := *e
	if e.Slots != nil {
		out.Slots = append([]SlotDefinition{}, e.Slots...)
	}
	if e.Derived != nil {
		out.Derived = make([]DerivedStatRule, len(e.Derived))
		for i, r := range e.Derived {
			out.Derived[i] = DerivedStatRule{Stat: r.Stat, Terms: cloneTerms(r.Terms)}
			if r.Recipes != nil {
				out.Derived[i].Recipes = make([]AlternativeRecipe, len(r.Recipes))
				for j, v := range r.Recipes {
					v.UnlessSlots = append([]SlotID(nil), v.UnlessSlots...)
					v.Terms = cloneTerms(v.Terms)
					out.Derived[i].Recipes[j] = v
				}
			}
		}
	}
	if e.Definitions != nil {
		out.Definitions = make([]EquipmentDefinition, len(e.Definitions))
		for i, d := range e.Definitions {
			out.Definitions[i] = d
			if d.Modes == nil {
				continue
			}
			out.Definitions[i].Modes = make([]EquipmentMode, len(d.Modes))
			for j, m := range d.Modes {
				n := m
				if m.Slots != nil {
					n.Slots = append([]SlotUse{}, m.Slots...)
				}
				if m.Modifiers != nil {
					n.Modifiers = append([]EquipmentModifier{}, m.Modifiers...)
				}
				if m.Overrides != nil {
					n.Overrides = make([]StatBaseOverride, len(m.Overrides))
					for k, v := range m.Overrides {
						n.Overrides[k] = StatBaseOverride{v.Stat, cloneRecipe(v.Recipe)}
					}
				}
				if m.Attacks != nil {
					n.Attacks = cloneEquipmentAttacks(m.Attacks)
				}
				out.Definitions[i].Modes[j] = n
			}
		}
	}
	return &out
}

type EquipmentItemView struct {
	AmmunitionOrigin ItemID               `json:"ammunition_origin,omitempty"`
	Item             Item                 `json:"item"`
	Definition       *EquipmentDefinition `json:"definition,omitempty"`
}
type SlotOccupancy struct {
	Slot     SlotID `json:"slot"`
	Used     int64  `json:"used"`
	Capacity int64  `json:"capacity"`
}
type EquipmentProjection struct {
	AmmunitionRecovery []AmmunitionRecoveryProjection `json:"ammunition_recovery,omitempty"`
	Clock              *Clock                         `json:"clock,omitempty"`
	Revision           Revision                       `json:"revision"`
	Character          CharacterSummary               `json:"character"`
	Items              []EquipmentItemView            `json:"items"`
	Slots              []SlotOccupancy                `json:"slots"`
	Attacks            []AttackGrant                  `json:"attacks"`
}

// EquipmentView is a detached observation, never input to Execute. Availability
// for a proposed multi-operation action uses the existing shared Preview path.
func (w *World) EquipmentView(id CharacterID) (EquipmentProjection, error) {
	s := w.Snapshot()
	b := &s.Blueprint
	if err := b.CheckRef(Ref{CharacterKind, string(id)}); err != nil {
		return EquipmentProjection{}, err
	}
	c := character(b, id)
	out := EquipmentProjection{Clock: copyPtr(b.Clock), Revision: s.Revision, Character: CharacterSummary{c.ID, c.Name}}
	for _, i := range b.Items {
		if i.Owner != id && i.Wearer != id && !carriedBy(i, id) {
			continue
		}
		v := EquipmentItemView{Item: i, AmmunitionOrigin: b.projectedAmmunitionOrigin(i)}
		if i.Equipment != nil {
			v.Definition = b.equipmentDefinition(i.Equipment.Definition)
		}
		out.Items = append(out.Items, v)
	}
	out.Slots = b.equipmentSlots(id)
	out.AmmunitionRecovery = b.ammunitionRecoveryViews(id)
	var err error
	out.Attacks, err = newDerivation(b).equipmentGrants(id)
	return out, err
}

// Called on validated candidate state: nonnegative occupancy is bounded by
// each slot's int64 capacity. Display and action predicates share this view.
func (b *Blueprint) equipmentSlots(id CharacterID) []SlotOccupancy {
	var out []SlotOccupancy
	if b.Equipment == nil {
		return out
	}
	used := map[SlotID]int64{}
	for _, item := range b.Items {
		if item.Wearer == id {
			if mode := b.wornMode(item); mode != nil {
				for _, s := range mode.Slots {
					used[s.Slot] += s.Amount
				}
			}
		}
	}
	for _, slot := range b.Equipment.Slots {
		out = append(out, SlotOccupancy{slot.ID, used[slot.ID], slot.Capacity})
	}
	return out
}
