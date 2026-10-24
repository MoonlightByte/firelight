// Package core owns NQL's typed semantics and the only live-world writer.
// Public records are values for construction or detached inspection, not handles
// into canonical state. The source compiler is deliberately outside this package.
package core

import (
	"math/big"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type CharacterID string
type ItemID string
type LocationID string
type StatID string
type ResourceID string
type EffectID string
type RelationKind string
type Revision uint64
type EntityKind string

const (
	CharacterKind EntityKind = "character"
	ItemKind      EntityKind = "item"
	LocationKind  EntityKind = "location"
)

type Ref struct {
	Kind EntityKind `json:"kind"`
	ID   string     `json:"id"`
}
type Resource struct {
	Current int64 `json:"current"`
	Minimum int64 `json:"minimum"`
	Maximum int64 `json:"maximum"`
}
type Character struct {
	Buffers      []ResourceBuffer        `json:"buffers,omitempty"`
	DamageTraits []DamageTrait           `json:"damage_traits,omitempty"`
	ID           CharacterID             `json:"id"`
	Name         string                  `json:"name"`
	Location     LocationID              `json:"location"`
	Stats        map[StatID]int64        `json:"stats"`
	Resources    map[ResourceID]Resource `json:"resources"`
}
type Item struct {
	From        ItemDefinitionID   `json:"from,omitempty"`
	SplitFrom   ItemID             `json:"split_from,omitempty"`
	Custody     *Ref               `json:"custody,omitempty"`
	Container   bool               `json:"container,omitempty"`
	Description string             `json:"description,omitempty"`
	Quantity    *int64             `json:"quantity,omitempty"`
	Recoverable int64              `json:"recoverable,omitempty"` // spent since the last recovery
	Charges     *ItemCharges       `json:"charges,omitempty"`
	Equipment   *EquipmentInstance `json:"equipment,omitempty"`
	ID          ItemID             `json:"id"`
	Name        string             `json:"name"`
	Owner       CharacterID        `json:"owner"`
	Wearer      CharacterID        `json:"wearer"`
}
type Location struct {
	ID   LocationID `json:"id"`
	Name string     `json:"name"`
}
type TransferPolicy string
type WearPolicy string

const (
	KeepsWorn TransferPolicy = "keeps_worn"
	Unequips  TransferPolicy = "unequips"
	WearAny   WearPolicy     = "any"
	OwnerOnly WearPolicy     = "owner_only"
)

type Rules struct {
	Transfer TransferPolicy `json:"transfer"`
	Wear     WearPolicy     `json:"wear"`
	// A recovery returns floor(spent * numerator / denominator) of a stock's
	// recoverable count. A zero denominator declares no recovery rule.
	RecoveryNumerator   int64 `json:"recovery_numerator,omitempty"`
	RecoveryDenominator int64 `json:"recovery_denominator,omitempty"`
}
type Activation string

const (
	OnCharacter Activation = "character"
	WhileWorn   Activation = "worn"
)

type Effect struct {
	EquipmentOrigin *EquipmentOrigin       `json:"equipment_origin,omitempty"`
	ConditionOrigin *ConditionEffectOrigin `json:"condition_origin,omitempty"`
	ID              EffectID               `json:"id"`
	Source          Ref                    `json:"source"`
	Mode            Activation             `json:"mode"`
	Target          Ref                    `json:"target"`
	Stat            StatID                 `json:"stat"`
	Amount          int64                  `json:"amount"`
}
type Relationship struct {
	From     Ref          `json:"from"`
	Kind     RelationKind `json:"kind"`
	To       Ref          `json:"to"`
	Strength int64        `json:"strength"`
}
type Blueprint struct {
	ItemDefinitions      []ItemDefinition       `json:"item_definitions,omitempty"`
	RelationDefinitions  []RelationDefinition   `json:"relation_definitions,omitempty"`
	Battle               *Battle                `json:"battle,omitempty"`
	Map                  *WorldMap              `json:"map,omitempty"`
	Occupants            []Occupant             `json:"occupants,omitempty"`
	Quests               []Quest                `json:"quests,omitempty"`
	Journal              *Journal               `json:"journal,omitempty"`
	Attacks              *AttackLaw             `json:"attacks,omitempty"`
	DamageTraits         []DamageTraitPolicy    `json:"damage_traits,omitempty"`
	Clock                *Clock                 `json:"clock,omitempty"`
	Dice                 *Dice                  `json:"dice,omitempty"`
	Equipment            *EquipmentRules        `json:"equipment,omitempty"`
	Adventure            *Adventure             `json:"adventure,omitempty"`
	AbilityDefinitions   []AbilityDefinition    `json:"ability_definitions,omitempty"`
	ConditionDefinitions []ConditionDefinition  `json:"condition_definitions,omitempty"`
	ConditionChecks      []ConditionCheckPolicy `json:"condition_checks,omitempty"`
	Conditions           []ConditionInstance    `json:"conditions,omitempty"`
	RetiredConditionIDs  []ConditionInstanceID  `json:"retired_condition_ids,omitempty"`
	Rules                Rules                  `json:"rules"`
	Characters           []Character            `json:"characters"`
	Items                []Item                 `json:"items"`
	Locations            []Location             `json:"locations"`
	Effects              []Effect               `json:"effects"`
	Relationships        []Relationship         `json:"relationships"`
}
type Snapshot struct {
	ConditionEffects []Effect `json:"condition_effects,omitempty"`
	Revision         Revision `json:"revision"`
	Blueprint
}

// Fault is a semantic failure; the compiler adds source positions when lowering.
type Fault struct {
	Code     string `json:"code"`
	Subject  string `json:"subject,omitempty"`
	Field    string `json:"field,omitempty"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}

func (e *Fault) Error() string { return e.Code + ": " + e.Message }
func fail(code, subject, field, expected, actual, message string) error {
	return &Fault{code, subject, field, expected, actual, message}
}
func validID(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func keys[K ~string, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func character(b *Blueprint, id CharacterID) *Character {
	for i := range b.Characters {
		if b.Characters[i].ID == id {
			return &b.Characters[i]
		}
	}
	return nil
}
func item(b *Blueprint, id ItemID) *Item {
	for i := range b.Items {
		if b.Items[i].ID == id {
			return &b.Items[i]
		}
	}
	return nil
}
func effectIndex(b *Blueprint, id EffectID) int {
	for i := range b.Effects {
		if b.Effects[i].ID == id {
			return i
		}
	}
	return -1
}
func relationIndex(b *Blueprint, r Relationship) int {
	for i, v := range b.Relationships {
		if v.From == r.From && v.Kind == r.Kind && v.To == r.To {
			return i
		}
	}
	return -1
}

// KindOf resolves identity, never spelling conventions or display names.
func (b Blueprint) KindOf(id string) (EntityKind, bool) {
	for _, c := range b.Characters {
		if string(c.ID) == id {
			return CharacterKind, true
		}
	}
	for _, i := range b.Items {
		if string(i.ID) == id {
			return ItemKind, true
		}
	}
	for _, l := range b.Locations {
		if string(l.ID) == id {
			return LocationKind, true
		}
	}
	return "", false
}
func (b Blueprint) CheckRef(r Ref) error { return checkRef(r, b.KindOf) }
func checkRef(r Ref, kindOf func(string) (EntityKind, bool)) error {
	if r.Kind != CharacterKind && r.Kind != ItemKind && r.Kind != LocationKind {
		return fail("E_TYPE", r.ID, "reference", "character, item, or location", string(r.Kind), "unknown entity kind")
	}
	k, ok := kindOf(r.ID)
	if !ok {
		return fail("E_REFERENCE", r.ID, "reference", string(r.Kind), "missing", "entity does not exist")
	}
	if k != r.Kind {
		return fail("E_TYPE", r.ID, "reference", string(r.Kind), string(k), "entity has the wrong kind")
	}
	return nil
}
func (b Blueprint) CheckStat(id CharacterID, stat StatID) error { return checkStat(scan{&b}, id, stat) }
func checkStat(l lookups, id CharacterID, stat StatID) error {
	c := l.character(id)
	if c == nil {
		return checkRef(Ref{CharacterKind, string(id)}, l.kindOf)
	}
	if _, ok := c.Stats[stat]; !ok {
		return fail("E_STAT", string(id), string(stat), "declared stat", "missing", "character has no such stat")
	}
	return nil
}
func (b Blueprint) CheckResource(id CharacterID, r ResourceID) error {
	return checkResource(scan{&b}, id, r)
}
func checkResource(l lookups, id CharacterID, r ResourceID) error {
	c := l.character(id)
	if c == nil {
		return checkRef(Ref{CharacterKind, string(id)}, l.kindOf)
	}
	if _, ok := c.Resources[r]; !ok {
		return fail("E_RESOURCE", string(id), string(r), "declared resource", "missing", "character has no such resource")
	}
	return nil
}

func validateEffect(l lookups, e Effect, dynamic bool) error {
	if e.EquipmentOrigin != nil {
		return fail("E_EFFECT_ORIGIN", string(e.ID), "origin", "direct effect without equipment ownership", "forged origin", "equipment effects are derived from worn instances")
	}
	if e.ConditionOrigin != nil {
		return fail("E_EFFECT_ORIGIN", string(e.ID), "origin", "direct effect without Condition ownership", "forged origin", "Condition effects are derived from instances")
	}
	if !validID(string(e.ID)) {
		return fail("E_ID", string(e.ID), "effect", "nonempty valid effect ID", "invalid", "effect requires a valid ID")
	}
	if !validID(string(e.Stat)) {
		return fail("E_ID", string(e.ID), "stat", "nonempty valid stat ID", "invalid", "effect stat requires a valid ID")
	}
	if err := checkRef(e.Source, l.kindOf); err != nil {
		return err
	}
	if err := checkRef(e.Target, l.kindOf); err != nil {
		return err
	}
	switch e.Mode {
	case OnCharacter:
		if e.Target.Kind != CharacterKind {
			return fail("E_TYPE", string(e.ID), "target", "character", string(e.Target.Kind), "character effect must target a character")
		}
		return checkStat(l, CharacterID(e.Target.ID), e.Stat)
	case WhileWorn:
		if e.Target.Kind != ItemKind {
			return fail("E_TYPE", string(e.ID), "target", "item", string(e.Target.Kind), "worn effect must target an item")
		}
		if dynamic {
			i := l.item(ItemID(e.Target.ID))
			if i.Wearer != "" {
				return checkStat(l, i.Wearer, e.Stat)
			}
		}
	default:
		return fail("E_EFFECT", string(e.ID), "mode", "character or worn", string(e.Mode), "unknown effect activation")
	}
	return nil
}

// CheckEffectStatic checks immutable references; wearer compatibility is dynamic.
func (b Blueprint) CheckEffectStatic(e Effect) error { return validateEffect(scan{&b}, e, false) }

func Validate(b Blueprint) error {
	var catalogErr error
	b, catalogErr = resolveCatalog(b)
	if catalogErr != nil {
		return catalogErr
	}
	if b.Rules.Transfer != KeepsWorn && b.Rules.Transfer != Unequips {
		return fail("E_RULE", "", "transfer", "keeps_worn or unequips", string(b.Rules.Transfer), "explicit transfer rule required")
	}
	if b.Rules.Wear != WearAny && b.Rules.Wear != OwnerOnly {
		return fail("E_RULE", "", "wear", "any or owner_only", string(b.Rules.Wear), "explicit wear rule required")
	}
	if err := b.checkRecoveryRule(); err != nil {
		return err
	}
	seen := map[string]bool{}
	checkID := func(id, name string) error {
		if !validID(id) || !utf8.ValidString(name) {
			return fail("E_ID", id, "identity", "valid explicit ID and UTF-8 name", "invalid", "invalid identity or name")
		}
		if seen[id] {
			return fail("E_DUPLICATE", id, "identity", "unique canonical ID", id, "duplicate canonical identity")
		}
		seen[id] = true
		return nil
	}
	for _, c := range b.Characters {
		if err := checkID(string(c.ID), c.Name); err != nil {
			return err
		}
	}
	for _, i := range b.Items {
		if err := checkID(string(i.ID), i.Name); err != nil {
			return err
		}
	}
	for _, l := range b.Locations {
		if err := checkID(string(l.ID), l.Name); err != nil {
			return err
		}
	}
	// Nothing below changes b, so one derivation serves every check.
	d := newDerivation(&b)
	for _, c := range b.Characters {
		if err := checkCharacterFacts(d, c); err != nil {
			return err
		}
	}
	refs := d.references()
	for _, i := range b.Items {
		if err := checkItemLineage(i, refs.KindOf, refs.item); err != nil {
			return err
		}
		if err := checkItemQuantity(i); err != nil {
			return err
		}
		if err := b.checkRecoverable(i); err != nil {
			return err
		}
		if err := checkChargedItem(i); err != nil {
			return err
		}
		if err := checkItemCustody(i, refs.CheckCustodyRef); err != nil {
			return err
		}
		if !utf8.ValidString(i.Description) {
			return fail("E_TEXT", string(i.ID), "description", "UTF-8 text", "invalid", "invalid item description")
		}
		if i.Owner != "" {
			if err := refs.CheckRef(Ref{CharacterKind, string(i.Owner)}); err != nil {
				return err
			}
		}
		if i.Wearer != "" {
			if err := refs.CheckRef(Ref{CharacterKind, string(i.Wearer)}); err != nil {
				return err
			}
			if b.Rules.Wear == OwnerOnly && i.Owner != i.Wearer {
				return fail("E_RULE", string(i.ID), "wearer", "current owner", string(i.Wearer), "owner_only forbids this wearer")
			}
		}
	}
	effects := map[EffectID]bool{}
	for _, e := range b.Effects {
		if effects[e.ID] {
			return fail("E_DUPLICATE", string(e.ID), "effect", "unique effect ID", string(e.ID), "duplicate effect identity")
		}
		effects[e.ID] = true
		if err := validateEffect(d, e, true); err != nil {
			return err
		}
	}
	if err := b.validateRelationDefinitions(); err != nil {
		return err
	}
	relations := map[struct {
		From Ref
		Kind RelationKind
		To   Ref
	}]bool{}
	for _, r := range b.Relationships {
		if err := checkRelationship(d, r); err != nil {
			return err
		}
		if !validID(string(r.Kind)) {
			return fail("E_ID", "", "relation", "valid relation kind", string(r.Kind), "invalid relation kind")
		}
		if err := refs.CheckRef(r.From); err != nil {
			return err
		}
		if err := refs.CheckRef(r.To); err != nil {
			return err
		}
		key := struct {
			From Ref
			Kind RelationKind
			To   Ref
		}{r.From, r.Kind, r.To}
		if relations[key] {
			return fail("E_DUPLICATE", r.From.ID, "relation", "unique directed relationship", string(r.Kind), "duplicate relationship")
		}
		relations[key] = true
	}
	if err := b.validateRelationDecayCapacity(); err != nil {
		return err
	}
	if err := b.validateAdventure(); err != nil {
		return err
	}
	if err := validateAbilities(&b); err != nil {
		return err
	}
	if err := validateClock(&b); err != nil {
		return err
	}
	if err := validateDice(&b); err != nil {
		return err
	}
	if err := validateConditions(d); err != nil {
		return err
	}
	if err := validateConditionDrains(d); err != nil {
		return err
	}
	if err := validateWorldConditionChecks(&b); err != nil {
		return err
	}
	if err := validateEquipment(d); err != nil {
		return err
	}
	if err := b.validateBattle(); err != nil {
		return err
	}
	if err := validateMap(&b, refs); err != nil {
		return err
	}
	if err := validateOccupants(&b, refs); err != nil {
		return err
	}
	if err := validateQuests(&b, refs); err != nil {
		return err
	}
	if err := validateJournal(&b, refs); err != nil {
		return err
	}
	if err := validateAttackLaw(&b); err != nil {
		return err
	}
	if err := validateDamageTraitPolicies(&b); err != nil {
		return err
	}
	for _, c := range b.Characters {
		if err := checkTraitsHaveLaw(&b, c); err != nil {
			return err
		}
	}
	for _, c := range b.Characters {
		for _, s := range keys(c.Stats) {
			if _, err := d.explain(c.ID, s); err != nil {
				return err
			}
		}
	}
	return nil
}

type Contribution struct {
	EquipmentOrigin *EquipmentOrigin       `json:"equipment_origin,omitempty"`
	ConditionOrigin *ConditionEffectOrigin `json:"condition_origin,omitempty"`
	Effect          EffectID               `json:"effect"`
	Source          Ref                    `json:"source"`
	Target          Ref                    `json:"target"`
	Amount          int64                  `json:"amount"`
}
type StatExplanation struct {
	Suppressed    []SuppressedContribution `json:"suppressed,omitempty"`
	Bounds        []StatBound              `json:"bounds,omitempty"`
	Roll          RollMode                 `json:"roll,omitempty"`
	RollSources   []RollSource             `json:"roll_sources,omitempty"`
	AddedDice     []AddedDieSource         `json:"added_dice,omitempty"`
	Recipe        *RecipeExplanation       `json:"recipe,omitempty"`
	Stat          StatID                   `json:"stat"`
	Base          int64                    `json:"base"`
	Contributions []Contribution           `json:"contributions"`
	Effective     int64                    `json:"effective"`
}

type SuppressedContribution struct {
	Contribution
	By ConditionInstanceID `json:"by"`
}

func explain(b *Blueprint, id CharacterID, stat StatID) (StatExplanation, error) {
	return newDerivation(b).explain(id, stat)
}
func (d *derivation) explain(id CharacterID, stat StatID) (StatExplanation, error) {
	if err := checkStat(d, id, stat); err != nil {
		return StatExplanation{}, err
	}
	c := d.character(id)
	out := StatExplanation{Stat: stat, Base: c.Stats[stat], Contributions: []Contribution{}}
	base, terms, recipe, err := d.statBase(id, stat, out.Base)
	if err != nil {
		return StatExplanation{}, err
	}
	out.Base, out.Recipe = base, recipe
	sum := big.NewInt(out.Base)
	sum.Add(sum, terms)
	effects := d.allEffects()
	for _, i := range d.contributing(id, stat) {
		e := effects[i]
		contribution := Contribution{Effect: e.ID, Source: e.Source, Target: e.Target, Amount: e.Amount, ConditionOrigin: copyPtr(e.ConditionOrigin), EquipmentOrigin: copyPtr(e.EquipmentOrigin)}
		if winner := d.conditionSuppressedBy(e); winner != nil {
			out.Suppressed = append(out.Suppressed, SuppressedContribution{Contribution: contribution, By: *winner})
			continue
		}
		out.Contributions = append(out.Contributions, contribution)
		sum.Add(sum, big.NewInt(e.Amount))
	}
	sort.Slice(out.Contributions, func(i, j int) bool { return out.Contributions[i].Effect < out.Contributions[j].Effect })
	sort.Slice(out.Suppressed, func(i, j int) bool { return out.Suppressed[i].Effect < out.Suppressed[j].Effect })
	if out.Bounds = d.statBounds(id, stat); len(out.Bounds) > 0 {
		applyBounds(sum, out.Bounds)
	}
	if !sum.IsInt64() {
		return StatExplanation{}, fail("E_OVERFLOW", string(id), string(stat), "int64 effective stat", sum.String(), "final derived value exceeds int64")
	}
	out.Effective = sum.Int64()
	out.RollSources = d.rollSources(id, stat, out.Recipe)
	out.Roll = netRoll(out.RollSources, "")
	out.AddedDice = d.addedDice(id, stat, out.Recipe)
	return out, nil
}
func cloneBlueprint(b Blueprint) Blueprint {
	b.ItemDefinitions = cloneItemDefinitions(b.ItemDefinitions)
	b.RelationDefinitions = cloneRelationDefinitions(b.RelationDefinitions)
	b.Battle = cloneBattle(b.Battle)
	b.Map = cloneWorldMap(b.Map)
	b.Occupants = cloneOccupants(b.Occupants)
	b.Quests = cloneQuests(b.Quests)
	b.Journal = cloneJournal(b.Journal)
	b.Attacks = copyPtr(b.Attacks)
	b.DamageTraits = append([]DamageTraitPolicy(nil), b.DamageTraits...)
	b.Clock = copyPtr(b.Clock)
	b.Dice = cloneDice(b.Dice)
	if b.ConditionChecks != nil {
		b.ConditionChecks = append([]ConditionCheckPolicy{}, b.ConditionChecks...)
	}
	b.Equipment = cloneEquipment(b.Equipment)
	b.Adventure = cloneAdventure(b.Adventure)
	b.AbilityDefinitions = cloneAbilityDefinitions(b.AbilityDefinitions)
	b.Characters = append([]Character{}, b.Characters...)
	for i := range b.Characters {
		b.Characters[i] = *cloneCharacter(&b.Characters[i])
	}
	b.Items = append([]Item{}, b.Items...)
	for i := range b.Items {
		b.Items[i] = *cloneItem(&b.Items[i])
	}
	b.Locations = append([]Location{}, b.Locations...)
	b.Effects = cloneEffects(append([]Effect{}, b.Effects...))
	cloneConditionData(&b)
	b.Relationships = append([]Relationship{}, b.Relationships...)
	return b
}
func normalize(b *Blueprint) {
	sort.Slice(b.ItemDefinitions, func(i, j int) bool { return b.ItemDefinitions[i].ID < b.ItemDefinitions[j].ID })
	sort.Slice(b.RelationDefinitions, func(i, j int) bool { return b.RelationDefinitions[i].Kind < b.RelationDefinitions[j].Kind })
	sort.Slice(b.AbilityDefinitions, func(i, j int) bool { return b.AbilityDefinitions[i].ID < b.AbilityDefinitions[j].ID })
	normalizeConditions(b)
	sort.Slice(b.Characters, func(i, j int) bool { return b.Characters[i].ID < b.Characters[j].ID })
	sort.Slice(b.Items, func(i, j int) bool { return b.Items[i].ID < b.Items[j].ID })
	sort.Slice(b.Locations, func(i, j int) bool { return b.Locations[i].ID < b.Locations[j].ID })
	normalizeMap(b.Map)
	normalizeOccupants(b)
	normalizeQuests(b)
	normalizeJournal(b)
	sort.Slice(b.Effects, func(i, j int) bool { return b.Effects[i].ID < b.Effects[j].ID })
	sort.Slice(b.Relationships, func(i, j int) bool {
		a, z := b.Relationships[i], b.Relationships[j]
		if a.From.ID != z.From.ID {
			return a.From.ID < z.From.ID
		}
		if a.Kind != z.Kind {
			return a.Kind < z.Kind
		}
		return a.To.ID < z.To.ID
	})
}
