package core

import "sort"

// lookups answers the ID lookups a check makes, the first declaration winning
// a repeated ID. scan reads the Blueprint at each question, which suits one
// check; a derivation indexes it once, which suits a pass over every entity.
type lookups interface {
	blueprint() *Blueprint
	character(CharacterID) *Character
	item(ItemID) *Item
	definition(ConditionTypeID) *ConditionDefinition
	equipmentDefinition(EquipmentDefinitionID) *EquipmentDefinition
	derivedRule(StatID) *DerivedStatRule
	kindOf(string) (EntityKind, bool)
	hasCondition(CharacterID, ConditionTypeID) bool
}

// scan is lookups by reading the Blueprint at each question.
type scan struct{ b *Blueprint }

func (s scan) blueprint() *Blueprint               { return s.b }
func (s scan) character(id CharacterID) *Character { return character(s.b, id) }
func (s scan) item(id ItemID) *Item                { return item(s.b, id) }
func (s scan) kindOf(id string) (EntityKind, bool) { return s.b.KindOf(id) }
func (s scan) derivedRule(stat StatID) *DerivedStatRule {
	return s.b.derivedRule(stat)
}
func (s scan) definition(t ConditionTypeID) *ConditionDefinition {
	return conditionDefinition(s.b, t)
}
func (s scan) equipmentDefinition(id EquipmentDefinitionID) *EquipmentDefinition {
	return s.b.equipmentDefinition(id)
}
func (s scan) hasCondition(id CharacterID, t ConditionTypeID) bool {
	return s.b.hasCondition(id, t)
}

// A derivation answers stat and Condition questions about one Blueprint state
// that does not change while the derivation is in use. It builds each index
// once, when first asked, so a pass that explains every stat of every
// Character takes time linear in the world, where a scan per question took
// time quadratic in its Characters and Conditions. Each answer equals the
// scan's, including the first declaration winning on a repeated ID. Build a
// derivation after the last change and drop it at the next.
type derivation struct {
	b                *Blueprint
	refs             *Refs
	characters       map[CharacterID]int
	definitions      map[ConditionTypeID]int
	instances        map[ConditionInstanceID]int
	equipment        map[EquipmentDefinitionID]int
	derived          map[StatID]int
	targets          map[CharacterID][]int // instances on each Character, in order
	worn             map[CharacterID][]int // items each Character wears, in order
	present          map[CharacterID]map[ConditionTypeID]bool
	latest           map[conditionGroup]int
	contributors     map[characterStat][]int
	bounds, rolls    map[characterStat][]modifierAt // instance modifiers by target and stat
	dice             map[characterStat][]modifierAt // instance die modifiers by target and stat
	heldBounds       map[StatID][]modifierAt        // held types' modifiers by stat
	heldRolls        map[StatID][]modifierAt
	held             []*ConditionDefinition
	effects          []Effect
	conditionEffects []Effect
	equipmentEffects []Effect
	// Set once the slice of the same name is built; a built slice may be nil.
	heldBuilt, effectsBuilt, conditionEffectsBuilt, equipmentEffectsBuilt bool
}

type conditionGroup struct {
	Type   ConditionTypeID
	Target CharacterID
}

type characterStat struct {
	Character CharacterID
	Stat      StatID
}

// modifierAt is a modifier at its place in a scan: owner indexes the
// instances, or the held types, and index the owner's modifiers.
type modifierAt struct {
	owner, index int
	m            *ConditionModifier
}

// byPlace restores scan order to modifiers gathered from several buckets.
func byPlace(ms []modifierAt) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].owner != ms[j].owner {
			return ms[i].owner < ms[j].owner
		}
		return ms[i].index < ms[j].index
	})
}

func newDerivation(b *Blueprint) *derivation { return &derivation{b: b} }

// firstIndex maps each key to its first position, the one a scan finds.
func firstIndex[K comparable](n int, key func(int) K) map[K]int {
	m := make(map[K]int, n)
	for i := 0; i < n; i++ {
		if _, ok := m[key(i)]; !ok {
			m[key(i)] = i
		}
	}
	return m
}

func (d *derivation) blueprint() *Blueprint { return d.b }

// references is NewRefs(d.b), built once.
func (d *derivation) references() Refs {
	if d.refs == nil {
		refs := NewRefs(d.b)
		d.refs = &refs
	}
	return *d.refs
}

// kindOf is Blueprint.KindOf by lookup.
func (d *derivation) kindOf(id string) (EntityKind, bool) { return d.references().KindOf(id) }

// character is character(d.b, id) by lookup.
func (d *derivation) character(id CharacterID) *Character {
	if d.characters == nil {
		d.characters = firstIndex(len(d.b.Characters), func(i int) CharacterID { return d.b.Characters[i].ID })
	}
	if i, ok := d.characters[id]; ok {
		return &d.b.Characters[i]
	}
	return nil
}

// item is item(d.b, id) by lookup.
func (d *derivation) item(id ItemID) *Item { return d.references().item(id) }

// definition is conditionDefinition(d.b, t) by lookup.
func (d *derivation) definition(t ConditionTypeID) *ConditionDefinition {
	if d.definitions == nil {
		d.definitions = firstIndex(len(d.b.ConditionDefinitions), func(i int) ConditionTypeID { return d.b.ConditionDefinitions[i].ID })
	}
	if i, ok := d.definitions[t]; ok {
		return &d.b.ConditionDefinitions[i]
	}
	return nil
}

// instance is conditionIndex(d.b, id) by lookup.
func (d *derivation) instance(id ConditionInstanceID) int {
	if d.instances == nil {
		d.instances = firstIndex(len(d.b.Conditions), func(i int) ConditionInstanceID { return d.b.Conditions[i].ID })
	}
	if i, ok := d.instances[id]; ok {
		return i
	}
	return -1
}

// equipmentDefinition is Blueprint.equipmentDefinition by lookup.
func (d *derivation) equipmentDefinition(id EquipmentDefinitionID) *EquipmentDefinition {
	if d.b.Equipment == nil {
		return nil
	}
	defs := d.b.Equipment.Definitions
	if d.equipment == nil {
		d.equipment = firstIndex(len(defs), func(i int) EquipmentDefinitionID { return defs[i].ID })
	}
	if i, ok := d.equipment[id]; ok {
		return &defs[i]
	}
	return nil
}

// derivedRule is Blueprint.derivedRule by lookup.
func (d *derivation) derivedRule(stat StatID) *DerivedStatRule {
	if d.b.Equipment == nil {
		return nil
	}
	rules := d.b.Equipment.Derived
	if d.derived == nil {
		d.derived = firstIndex(len(rules), func(i int) StatID { return rules[i].Stat })
	}
	if i, ok := d.derived[stat]; ok {
		return &rules[i]
	}
	return nil
}

// wornMode is Blueprint.wornMode by lookup.
func (d *derivation) wornMode(i Item) *EquipmentMode {
	if i.Equipment == nil || i.Wearer == "" {
		return nil
	}
	return equipmentMode(d.equipmentDefinition(i.Equipment.Definition), i.Equipment.Mode)
}

// conditionsOn lists the indexes of the instances on a Character, in order.
func (d *derivation) conditionsOn(id CharacterID) []int {
	if d.targets == nil {
		d.targets = map[CharacterID][]int{}
		for i := range d.b.Conditions {
			t := d.b.Conditions[i].Target
			d.targets[t] = append(d.targets[t], i)
		}
	}
	return d.targets[id]
}

// wornBy lists the indexes of the items a Character wears, in order. An
// unworn item has no worn mode, so no scan for a wearer ever counted one.
func (d *derivation) wornBy(id CharacterID) []int {
	if d.worn == nil {
		d.worn = map[CharacterID][]int{}
		for i := range d.b.Items {
			if w := d.b.Items[i].Wearer; w != "" {
				d.worn[w] = append(d.worn[w], i)
			}
		}
	}
	return d.worn[id]
}

// heldTypes lists the definitions held at a minimum, in declaration order.
// heldAt is false for every other definition.
func (d *derivation) heldTypes() []*ConditionDefinition {
	if !d.heldBuilt {
		for i := range d.b.ConditionDefinitions {
			if d.b.ConditionDefinitions[i].HeldAtMinimum != nil {
				d.held = append(d.held, &d.b.ConditionDefinitions[i])
			}
		}
		d.heldBuilt = true
	}
	return d.held
}

// hasCondition is Blueprint.hasCondition by lookup: the types each instance
// on the Character makes present, and those of each type held on it.
func (d *derivation) hasCondition(id CharacterID, t ConditionTypeID) bool {
	if d.present == nil {
		d.present = map[CharacterID]map[ConditionTypeID]bool{}
	}
	present, ok := d.present[id]
	if !ok {
		present = map[ConditionTypeID]bool{}
		for _, i := range d.conditionsOn(id) {
			c := &d.b.Conditions[i]
			present[c.Type] = true
			if def := d.definition(c.Type); def != nil {
				for _, x := range def.Includes {
					present[x] = true
				}
			}
		}
		c := d.character(id)
		for _, def := range d.heldTypes() {
			if heldBy(c, def) {
				present[def.ID] = true
				for _, x := range def.Includes {
					present[x] = true
				}
			}
		}
		d.present[id] = present
	}
	return present[t]
}

// heldConditions is heldConditions(d.b, id) by lookup.
func (d *derivation) heldConditions(id CharacterID) []HeldCondition {
	var out []HeldCondition
	c := d.character(id)
	for _, def := range d.heldTypes() {
		if heldBy(c, def) {
			out = append(out, HeldCondition{def.ID, *def.HeldAtMinimum})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// effectTarget names the Character an effect applies to: the one it targets,
// or the wearer of the item it targets. It reports false for a missing item.
func (d *derivation) effectTarget(e Effect) (CharacterID, bool) {
	if e.Mode == OnCharacter {
		return CharacterID(e.Target.ID), true
	}
	if i := d.item(ItemID(e.Target.ID)); i != nil {
		return i.Wearer, true
	}
	return "", false
}

func (d *derivation) appliesTo(e Effect, id CharacterID) bool {
	t, ok := d.effectTarget(e)
	return ok && t == id
}

func (d *derivation) activeFor(e Effect, id CharacterID) bool {
	return d.appliesTo(e, id) && d.conditionSuppressedBy(e) == nil
}

// indexModifiers buckets the bound, roll and die modifiers of every instance
// by its target and stat, and the bound and roll modifiers of every held type
// by stat (a held type carries no die), each bucket in scan order. An instance of an undeclared type has none; Validate rejects
// one before any stat is explained.
func (d *derivation) indexModifiers() {
	if d.bounds != nil {
		return
	}
	d.bounds, d.rolls, d.dice = map[characterStat][]modifierAt{}, map[characterStat][]modifierAt{}, map[characterStat][]modifierAt{}
	for i := range d.b.Conditions {
		c := &d.b.Conditions[i]
		def := d.definition(c.Type)
		if def == nil {
			continue
		}
		for j := range def.Modifiers {
			m := &def.Modifiers[j]
			k := characterStat{c.Target, m.Stat}
			if m.Bound != "" {
				d.bounds[k] = append(d.bounds[k], modifierAt{i, j, m})
			}
			if m.Roll != "" {
				d.rolls[k] = append(d.rolls[k], modifierAt{i, j, m})
			}
			if m.Die != nil {
				d.dice[k] = append(d.dice[k], modifierAt{i, j, m})
			}
		}
	}
	d.heldBounds, d.heldRolls = map[StatID][]modifierAt{}, map[StatID][]modifierAt{}
	for h, def := range d.heldTypes() {
		for j := range def.Modifiers {
			m := &def.Modifiers[j]
			if m.Bound != "" {
				d.heldBounds[m.Stat] = append(d.heldBounds[m.Stat], modifierAt{h, j, m})
			}
			if m.Roll != "" {
				d.heldRolls[m.Stat] = append(d.heldRolls[m.Stat], modifierAt{h, j, m})
			}
		}
	}
}

// contributing lists the indexes into allEffects of the effects that apply to
// one stat of one Character, in their allEffects order.
func (d *derivation) contributing(id CharacterID, stat StatID) []int {
	if d.contributors == nil {
		d.contributors = map[characterStat][]int{}
		for i, e := range d.allEffects() {
			if t, ok := d.effectTarget(e); ok {
				k := characterStat{t, e.Stat}
				d.contributors[k] = append(d.contributors[k], i)
			}
		}
	}
	return d.contributors[characterStat{id, stat}]
}
