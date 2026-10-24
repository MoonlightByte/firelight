package core

import (
	"fmt"
	"math"
	"sort"
)

type ConditionTypeID string
type ConditionInstanceID string
type ConditionModifierID string
type InstancePolicy string
type ModifierSelection string
type ModifierBound string

const LatestModifiers ModifierSelection = "latest"

// A bound modifier holds a stat at least or at most its amount instead of
// adding to it (Gauntlets of Ogre Power: Strength 19; Grappled: Speed 0).
const (
	AtLeast ModifierBound = "at_least"
	AtMost  ModifierBound = "at_most"
)

const (
	Coexist InstancePolicy = "coexist"
	Unique  InstancePolicy = "unique"
	Replace InstancePolicy = "replace"
	// ConditionKind is provenance only, never an ordinary resolvable entity kind.
	ConditionKind         EntityKind = "condition"
	MaxConditionModifiers            = 16
	MaxConditionIncludes             = 8
)

// ConditionModifier adds to a stat, bounds it, sets how checks on it roll,
// adds a die to those checks, or raises a Resource maximum.
// An additive stat modifier is a derived Effect; a bound is applied to the
// total when a stat is explained; a Resource maximum is stored on the target,
// raised when the instance is applied and lowered again when it is removed.
type ConditionModifier struct {
	ID              ConditionModifierID `json:"id"`
	Stat            StatID              `json:"stat,omitempty"`
	ResourceMaximum ResourceID          `json:"resource_maximum,omitempty"`
	Bound           ModifierBound       `json:"bound,omitempty"`
	Roll            RollMode            `json:"roll,omitempty"`
	Fixed           *int64              `json:"fixed,omitempty"`
	Magnitude       bool                `json:"magnitude,omitempty"`
	Die             *AddedDie           `json:"die,omitempty"`
}

// ConditionDefinition is an authored, target-independent type. HeldAtMinimum
// makes the type present, without an instance, on every Character whose named
// Resource is at its minimum (Unconscious at 0 HP). Includes makes each named
// type present wherever this one is (Stunned includes Incapacitated).
type ConditionDefinition struct {
	// Attacks are granted to every character on whom the type is present:
	// by an instance, an including type, or a held floor.
	Attacks           []EquipmentAttack   `json:"attacks,omitempty"`
	UntilSourceTurn   bool                `json:"until_source_turn,omitempty"`
	EndsAfterAttack   bool                `json:"ends_after_attack,omitempty"`
	EndsOnCondition   *ConditionTypeID    `json:"ends_on_condition,omitempty"`
	ModifierSelection ModifierSelection   `json:"modifier_selection,omitempty"`
	EndsAtMinimum     *ResourceID         `json:"ends_at_minimum,omitempty"`
	HeldAtMinimum     *ResourceID         `json:"held_at_minimum,omitempty"`
	Includes          []ConditionTypeID   `json:"includes,omitempty"`
	ID                ConditionTypeID     `json:"id"`
	Instances         InstancePolicy      `json:"instances"`
	Modifiers         []ConditionModifier `json:"modifiers"`
}
type ConditionCause struct {
	AbilityOrigin *AbilityOrigin `json:"ability_origin,omitempty"`
	RequestID     string         `json:"request_id"`
	Operation     int            `json:"operation"`
	Actor         Ref            `json:"actor"`
}
type ConditionInstance struct {
	Anchor          *ConditionInstanceID `json:"anchor,omitempty"`
	ClockLifetime   *ClockLifetime       `json:"clock_lifetime,omitempty"`
	Lifetime        *ConditionLifetime   `json:"lifetime,omitempty"`
	ID              ConditionInstanceID  `json:"id"`
	Type            ConditionTypeID      `json:"type"`
	Target          CharacterID          `json:"target"`
	Source          *Ref                 `json:"source,omitempty"`
	Magnitude       *int64               `json:"magnitude,omitempty"`
	AppliedRevision Revision             `json:"applied_revision"`
	Cause           *ConditionCause      `json:"cause,omitempty"`
}
type ConditionEffectOrigin struct {
	Instance ConditionInstanceID `json:"instance"`
	Modifier ConditionModifierID `json:"modifier"`
}
type ApplyCondition struct {
	Anchor        *ConditionInstanceID `json:"anchor,omitempty"`
	ClockDuration *uint64              `json:"clock_duration,omitempty"`
	Duration      *uint64              `json:"duration,omitempty"`
	Instance      ConditionInstanceID  `json:"instance"`
	Type          ConditionTypeID      `json:"type"`
	Target        CharacterID          `json:"target"`
	Source        *Ref                 `json:"source,omitempty"`
	Magnitude     *int64               `json:"magnitude,omitempty"`
}
type RemoveCondition struct {
	Type     ConditionTypeID     `json:"type,omitempty"`
	Instance ConditionInstanceID `json:"instance"`
	Target   CharacterID         `json:"target"`
}
type ConditionChange struct {
	ExpectedType    ConditionTypeID           `json:"expected_type,omitempty"`
	BattleEnding    *BattleConditionEnding    `json:"battle_ending,omitempty"`
	EndingCondition *ConditionInstanceID      `json:"ending_condition,omitempty"`
	AnchorRemoved   *ConditionInstanceID      `json:"anchor_removed,omitempty"`
	ReplacedBy      *ConditionInstanceID      `json:"replaced_by,omitempty"`
	MinimumResource *ResourceID               `json:"minimum_resource,omitempty"`
	CheckFailed     bool                      `json:"check_failed,omitempty"`
	ClockExpiration *ClockExpiration          `json:"clock_expiration,omitempty"`
	Expiration      *ExpirationOrigin         `json:"expiration,omitempty"`
	Before          *ConditionInstance        `json:"before"`
	After           *ConditionInstance        `json:"after"`
	BeforeEffects   []Effect                  `json:"before_effects"`
	AfterEffects    []Effect                  `json:"after_effects"`
	Resources       []ConditionResourceChange `json:"resources,omitempty"`
}

// ConditionResourceChange records one Resource maximum modifier moving the
// target's stored maximum when its instance was applied or removed. Amount is
// the modifier's amount in force; Before and After show the direction, the
// minimum floor and any clamp of current.
type ConditionResourceChange struct {
	Modifier ConditionModifierID `json:"modifier"`
	Resource ResourceID          `json:"resource"`
	Amount   int64               `json:"amount"`
	Before   Resource            `json:"before"`
	After    Resource            `json:"after"`
}
type ConditionSummary struct {
	ClockLifetime *ClockLifetime      `json:"clock_lifetime,omitempty"`
	Instance      ConditionInstanceID `json:"instance"`
	Type          ConditionTypeID     `json:"type"`
}

func conditionDefinition(b *Blueprint, id ConditionTypeID) *ConditionDefinition {
	for i := range b.ConditionDefinitions {
		if b.ConditionDefinitions[i].ID == id {
			return &b.ConditionDefinitions[i]
		}
	}
	return nil
}
func conditionIndex(b *Blueprint, id ConditionInstanceID) int {
	for i := range b.Conditions {
		if b.Conditions[i].ID == id {
			return i
		}
	}
	return -1
}
func conditionIntent(c ConditionInstance) ApplyCondition {
	v := ApplyCondition{Instance: c.ID, Type: c.Type, Target: c.Target, Source: copyPtr(c.Source), Magnitude: copyPtr(c.Magnitude), Anchor: copyPtr(c.Anchor)}
	if c.Lifetime != nil {
		n := c.Lifetime.ExpiresAfterRound - c.Lifetime.AppliedRound + 1
		v.Duration = &n
	}
	if c.ClockLifetime != nil {
		n := c.ClockLifetime.ExpiresTick - c.ClockLifetime.AppliedTick
		v.ClockDuration = &n
	}
	return v
}
func conditionFault(code, id, field, expected, actual string) error {
	return fail(code, id, field, expected, actual, "invalid condition "+field)
}

// CheckConditionDefinition validates an authored, target-independent type.
func CheckConditionDefinition(d ConditionDefinition) error {
	if !validID(string(d.ID)) {
		return conditionFault("E_ID", string(d.ID), "condition_type", "valid type ID", "invalid")
	}
	if d.Instances != Coexist && d.Instances != Unique && d.Instances != Replace {
		return conditionFault("E_RULE", string(d.ID), "instances", "coexist, unique or replace", string(d.Instances))
	}
	if d.ModifierSelection != "" && d.ModifierSelection != LatestModifiers {
		return conditionFault("E_RULE", string(d.ID), "modifier_selection", "omitted additive default or latest", string(d.ModifierSelection))
	}
	if d.EndsAtMinimum != nil && !validID(string(*d.EndsAtMinimum)) {
		return conditionFault("E_ID", string(d.ID), "ends_at_minimum", "valid Resource ID", string(*d.EndsAtMinimum))
	}
	if d.EndsOnCondition != nil && (!validID(string(*d.EndsOnCondition)) || *d.EndsOnCondition == d.ID) {
		return conditionFault("E_CONDITION_TYPE", string(d.ID), "ends_on_condition", "valid different Condition type", string(*d.EndsOnCondition))
	}
	if d.HeldAtMinimum != nil {
		if !validID(string(*d.HeldAtMinimum)) {
			return conditionFault("E_ID", string(d.ID), "held_at_minimum", "valid Resource ID", string(*d.HeldAtMinimum))
		}
		// A type held at a minimum cannot also end there.
		if d.EndsAtMinimum != nil {
			return conditionFault("E_RULE", string(d.ID), "held_at_minimum", "no ends_at_minimum", string(*d.EndsAtMinimum))
		}
	}
	if len(d.Includes) > MaxConditionIncludes {
		return conditionFault("E_LIMIT", string(d.ID), "includes", "at most 8", fmt.Sprint(len(d.Includes)))
	}
	included := map[ConditionTypeID]bool{}
	for _, t := range d.Includes {
		if !validID(string(t)) || t == d.ID || included[t] {
			return conditionFault("E_CONDITION_TYPE", string(d.ID), "includes", "valid different Condition type, named once", string(t))
		}
		included[t] = true
	}
	if len(d.Modifiers) > MaxConditionModifiers {
		return conditionFault("E_LIMIT", string(d.ID), "modifiers", "at most 16", fmt.Sprint(len(d.Modifiers)))
	}
	seen := map[ConditionModifierID]bool{}
	for _, m := range d.Modifiers {
		if !validID(string(m.ID)) {
			return conditionFault("E_ID", string(d.ID), "modifier", "valid modifier ID", string(m.ID))
		}
		if seen[m.ID] {
			return conditionFault("E_DUPLICATE", string(d.ID), "modifier", "unique modifier ID", string(m.ID))
		}
		seen[m.ID] = true
		if m.Bound != "" && m.Bound != AtLeast && m.Bound != AtMost {
			return conditionFault("E_RULE", string(d.ID), "bound", "at_least or at_most", string(m.Bound))
		}
		if m.Roll != "" && (!rollMode(m.Roll) || m.Bound != "") {
			return conditionFault("E_RULE", string(d.ID), "roll", "advantage, disadvantage or fail without a bound", string(m.Roll))
		}
		// Held presence has no instance, so no magnitude, and bounds and
		// modes cannot double count with an instance of the same type.
		if d.HeldAtMinimum != nil && m.Roll == "" && (m.Bound == "" || m.Magnitude) {
			return conditionFault("E_RULE", string(d.ID), "held_at_minimum", "fixed bound or roll modifiers only", string(m.ID))
		}
		if m.ResourceMaximum != "" {
			if m.Stat != "" {
				return conditionFault("E_IR", string(d.ID), "modifier", "one stat or Resource maximum", string(m.ID))
			}
			if m.Bound != "" || m.Roll != "" || m.Die != nil {
				return conditionFault("E_RULE", string(d.ID), "bound", "stat modifier", string(m.ResourceMaximum))
			}
			if !validID(string(m.ResourceMaximum)) {
				return conditionFault("E_ID", string(d.ID), "resource_maximum", "valid Resource ID", string(m.ResourceMaximum))
			}
			// A stored maximum cannot follow a winner that changes on removal.
			if d.ModifierSelection == LatestModifiers {
				return conditionFault("E_RULE", string(d.ID), "resource_maximum", "additive modifier selection", string(m.ResourceMaximum))
			}
			if m.Fixed != nil && (*m.Fixed == 0 || *m.Fixed == math.MinInt64) {
				return conditionFault("E_CONDITION_AMOUNT", string(d.ID), "resource_maximum", "nonzero amount above the int64 minimum", string(m.ResourceMaximum))
			}
		} else if !validID(string(m.Stat)) {
			return conditionFault("E_ID", string(d.ID), "stat", "valid stat ID", string(m.Stat))
		}
		if m.Die != nil {
			if err := checkAddedDie(d, m); err != nil {
				return err
			}
		} else if m.Roll != "" {
			if m.Fixed != nil || m.Magnitude {
				return conditionFault("E_IR", string(d.ID), "amount", "no amount on a roll modifier", string(m.ID))
			}
		} else if (m.Fixed != nil) == m.Magnitude {
			return conditionFault("E_IR", string(d.ID), "amount", "exactly one fixed or magnitude amount", "invalid sum")
		}
	}
	if r, mixed := mixedResourceMaximum(d, nil); mixed {
		return conditionFault("E_CONDITION_AMOUNT", string(d.ID), "resource_maximum", "one sign per Resource maximum", string(r))
	}
	return nil
}

// mixedResourceMaximum finds a Resource maximum that one instance would both
// raise and drain, whose ending would then depend on modifier order. Without
// a magnitude only fixed amounts are compared.
func mixedResourceMaximum(d ConditionDefinition, magnitude *int64) (ResourceID, bool) {
	signs := map[ResourceID]bool{}
	for _, m := range d.Modifiers {
		amount := modifierAmount(m, ConditionInstance{Magnitude: magnitude})
		if m.ResourceMaximum == "" || amount == 0 {
			continue
		}
		if raises, seen := signs[m.ResourceMaximum]; seen && raises != (amount > 0) {
			return m.ResourceMaximum, true
		}
		signs[m.ResourceMaximum] = amount > 0
	}
	return "", false
}

func (b Blueprint) CheckConditionDefinitionReferences(d ConditionDefinition) error {
	return checkConditionDefinitionReferences(scan{&b}, d)
}
func checkConditionDefinitionReferences(l lookups, d ConditionDefinition) error {
	if d.EndsOnCondition != nil && l.definition(*d.EndsOnCondition) == nil {
		return conditionFault("E_CONDITION_TYPE", string(d.ID), "ends_on_condition", "declared Condition type", string(*d.EndsOnCondition))
	}
	// Inclusion is one level deep, so presence never chains or cycles.
	for _, t := range d.Includes {
		included := l.definition(t)
		if included == nil {
			return conditionFault("E_CONDITION_TYPE", string(d.ID), "includes", "declared Condition type", string(t))
		}
		if len(included.Includes) > 0 {
			return conditionFault("E_RULE", string(d.ID), "includes", "type that includes no other", string(t))
		}
	}
	return nil
}

// CheckConditionStatic resolves immutable type, source, target and stat facts.
// Active identity and authored coexistence are evaluated sequentially by Execute.
func (b Blueprint) CheckConditionStatic(v ApplyCondition) error {
	return checkConditionStatic(scan{&b}, v)
}
func checkConditionStatic(l lookups, v ApplyCondition) error {
	b := l.blueprint()
	if v.Anchor != nil && !validID(string(*v.Anchor)) {
		return conditionFault("E_ID", string(v.Instance), "anchor", "valid Condition instance ID", string(*v.Anchor))
	}
	if v.ClockDuration != nil && (v.Duration != nil || b.Clock == nil || *v.ClockDuration == 0) {
		return clockFault(string(v.Instance), "duration", "one positive clock duration with Clock", "invalid")
	}
	if v.Duration != nil && (*v.Duration == 0 || b.Adventure == nil) {
		return conditionFault("E_DURATION", string(v.Instance), "duration", "positive duration in Adventure", "invalid")
	}
	if !validID(string(v.Instance)) {
		return conditionFault("E_ID", string(v.Instance), "condition_instance", "valid instance ID", "invalid")
	}
	d := l.definition(v.Type)
	if d == nil {
		return conditionFault("E_CONDITION_TYPE", string(v.Type), "condition_type", "declared condition type", "missing")
	}
	if err := CheckConditionDefinition(*d); err != nil {
		return err
	}
	if d.UntilSourceTurn && (b.Battle == nil || v.Source == nil || v.Source.Kind != CharacterKind || v.Duration != nil || v.ClockDuration != nil) {
		return conditionFault("E_DURATION", string(v.Instance), "until_source_turn", "Battle Character source without clock/round duration", "invalid")
	}
	if d.EndsAfterAttack && b.Battle == nil {
		return conditionFault("E_RULE", string(v.Instance), "ends_after_attack", "Battle profile", "absent")
	}
	if err := checkRef(Ref{CharacterKind, string(v.Target)}, l.kindOf); err != nil {
		return err
	}
	if d.EndsAtMinimum != nil {
		if err := checkResource(l, v.Target, *d.EndsAtMinimum); err != nil {
			return err
		}
	}
	if v.Source != nil {
		if err := checkRef(*v.Source, l.kindOf); err != nil {
			return err
		}
	}
	if err := checkConditionCheckTarget(l, v.Type, v.Target); err != nil {
		return err
	}
	usesMagnitude := false
	for _, m := range d.Modifiers {
		usesMagnitude = usesMagnitude || m.Magnitude
		if err := checkModifierTarget(l, v.Target, m); err != nil {
			return err
		}
		if m.ResourceMaximum != "" && m.Magnitude && v.Magnitude != nil && (*v.Magnitude == 0 || *v.Magnitude == math.MinInt64) {
			return conditionFault("E_CONDITION_AMOUNT", string(v.Instance), "magnitude", "nonzero magnitude above the int64 minimum for a Resource maximum", fmt.Sprint(*v.Magnitude))
		}
	}
	if usesMagnitude != (v.Magnitude != nil) {
		if usesMagnitude {
			return conditionFault("E_CONDITION_AMOUNT", string(v.Instance), "magnitude", "magnitude argument required", "absent")
		}
		return conditionFault("E_CONDITION_AMOUNT", string(v.Instance), "magnitude", "no magnitude argument", "present")
	}
	if r, mixed := mixedResourceMaximum(*d, v.Magnitude); mixed {
		return conditionFault("E_CONDITION_AMOUNT", string(v.Instance), "magnitude", "one sign per Resource maximum", string(r))
	}
	return nil
}

// checkModifierTarget checks the modified stat, or a Resource maximum that is
// not a buffer pool (pools have their own grant law) or Battle health (whose
// minimum drives Battle life).
func (b Blueprint) checkModifierTarget(target CharacterID, m ConditionModifier) error {
	return checkModifierTarget(scan{&b}, target, m)
}
func checkModifierTarget(l lookups, target CharacterID, m ConditionModifier) error {
	b := l.blueprint()
	if m.ResourceMaximum == "" {
		return checkStat(l, target, m.Stat)
	}
	if err := checkResource(l, target, m.ResourceMaximum); err != nil {
		return err
	}
	if l.character(target).bufferPool(m.ResourceMaximum) {
		return bufferRule(target, m.ResourceMaximum, "buffer pools cannot carry a maximum modifier")
	}
	if b.Battle != nil && m.ResourceMaximum == b.Battle.Rules.Health {
		return conditionFault("E_RULE", string(target), "resource_maximum", "Resource other than Battle health", string(m.ResourceMaximum))
	}
	return nil
}

func modifierAmount(m ConditionModifier, c ConditionInstance) int64 {
	if m.Fixed != nil {
		return *m.Fixed
	}
	if m.Magnitude && c.Magnitude != nil {
		return *c.Magnitude
	}
	return 0
}

// headroom is how far v can rise and still fit int64; the wrapped unsigned
// difference is exact for every int64.
func headroom(v int64) uint64 {
	return uint64(math.MaxInt64) - uint64(v)
}

func saturatingAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// conditionMaximumTotals sums the active raises and drains on one Resource
// maximum. Only an inconsistent declaration can approach the saturation.
func conditionMaximumTotals(b *Blueprint, target CharacterID, resource ResourceID) (raises, drains uint64) {
	for _, c := range b.Conditions {
		if c.Target != target {
			continue
		}
		for _, m := range conditionDefinition(b, c.Type).Modifiers {
			if m.ResourceMaximum != resource {
				continue
			}
			if amount := modifierAmount(m, c); amount > 0 {
				raises = saturatingAdd(raises, uint64(amount))
			} else if amount < 0 {
				// uint64(-amount) is exact even for the int64 minimum.
				drains = saturatingAdd(drains, uint64(-amount))
			}
		}
	}
	return raises, drains
}

// raiseConditionMaxima applies an applied instance's positive Resource maximum
// modifiers in modifier ID order. Current is unchanged; healing is explicit.
// Active drains count against the headroom so their endings cannot overflow.
func raiseConditionMaxima(b *Blueprint, c ConditionInstance) ([]ConditionResourceChange, error) {
	var out []ConditionResourceChange
	for _, m := range conditionDefinition(b, c.Type).Modifiers {
		amount := modifierAmount(m, c)
		if m.ResourceMaximum == "" || amount < 0 {
			continue
		}
		ch := character(b, c.Target)
		before := ch.Resources[m.ResourceMaximum]
		_, drains := conditionMaximumTotals(b, c.Target, m.ResourceMaximum)
		if room := headroom(before.Maximum); drains > room || uint64(amount) > room-drains {
			if drains == 0 {
				return nil, fail("E_OVERFLOW", string(c.Target), string(m.ResourceMaximum), "maximum + amount fits int64", fmt.Sprintf("maximum=%d amount=%d", before.Maximum, amount), "Condition cannot raise the Resource maximum")
			}
			return nil, fail("E_OVERFLOW", string(c.Target), string(m.ResourceMaximum), "maximum + amount + active drains fits int64", fmt.Sprintf("maximum=%d amount=%d drains=%d", before.Maximum, amount, drains), "Condition cannot raise the Resource maximum")
		}
		after := before
		after.Maximum += amount
		ch.Resources[m.ResourceMaximum] = after
		out = append(out, ConditionResourceChange{Modifier: m.ID, Resource: m.ResourceMaximum, Amount: amount, Before: before, After: after})
	}
	return out, nil
}

type resourceFloor struct {
	Character CharacterID
	Resource  ResourceID
}

// drainConditionMaxima applies an incoming instance's negative Resource
// maximum modifiers in modifier ID order, before it joins the active set. A
// drain takes at most the room above the minimum that active raises do not
// hold, so maximum - minimum >= active raises stays true and every ending
// reverses exactly. Current clamps down; a clamp to the minimum is a floor.
func drainConditionMaxima(b *Blueprint, c ConditionInstance) ([]ConditionResourceChange, []resourceFloor, error) {
	var out []ConditionResourceChange
	var floors []resourceFloor
	for _, m := range conditionDefinition(b, c.Type).Modifiers {
		amount := modifierAmount(m, c)
		if m.ResourceMaximum == "" || amount >= 0 {
			continue
		}
		ch := character(b, c.Target)
		before := ch.Resources[m.ResourceMaximum]
		raises, _ := conditionMaximumTotals(b, c.Target, m.ResourceMaximum)
		// maximum >= minimum, so the unsigned difference is exact, and
		// uint64(-amount) is exact even for the int64 minimum, which
		// validation refuses after this runs.
		room, drain := uint64(before.Maximum)-uint64(before.Minimum), uint64(-amount)
		if raises > room || drain > room-raises {
			return nil, nil, conditionFault("E_CONDITION_AMOUNT", string(c.ID), "resource_maximum", "drain at most maximum - minimum - active raises", fmt.Sprintf("drain=%d maximum=%d minimum=%d raises=%d", drain, before.Maximum, before.Minimum, raises))
		}
		after := before
		after.Maximum += amount
		if after.Current > after.Maximum {
			after.Current = after.Maximum
			if after.Current == after.Minimum {
				floors = append(floors, resourceFloor{c.Target, m.ResourceMaximum})
			}
		}
		ch.Resources[m.ResourceMaximum] = after
		out = append(out, ConditionResourceChange{Modifier: m.ID, Resource: m.ResourceMaximum, Amount: amount, Before: before, After: after})
	}
	return out, floors, nil
}

// lowerConditionMaxima reverses them on any removal and never fails, so an
// instance can always end. A raise's reversal stops at the minimum and clamps
// current down to it; it reports each Resource the clamp brought down to its
// minimum. A drain gives its amount back and leaves current alone.
func lowerConditionMaxima(b *Blueprint, c ConditionInstance) ([]ConditionResourceChange, []resourceFloor) {
	var out []ConditionResourceChange
	var floors []resourceFloor
	for _, m := range conditionDefinition(b, c.Type).Modifiers {
		if m.ResourceMaximum == "" {
			continue
		}
		ch := character(b, c.Target)
		before, ok := ch.Resources[m.ResourceMaximum]
		if !ok {
			continue
		}
		amount := modifierAmount(m, c)
		after := before
		switch {
		case amount < 0:
			// maximum + active drains fits int64 (validateConditionDrains), so
			// the saturation only guards the never-refused ending.
			if uint64(-amount) > headroom(before.Maximum) {
				after.Maximum = math.MaxInt64
			} else {
				after.Maximum = before.Maximum - amount
			}
		// maximum >= minimum, so the unsigned difference is exact.
		case uint64(amount) >= uint64(before.Maximum)-uint64(before.Minimum):
			after.Maximum = before.Minimum
		default:
			after.Maximum = before.Maximum - amount
		}
		if after.Current > after.Maximum {
			after.Current = after.Maximum
			if after.Current == after.Minimum {
				floors = append(floors, resourceFloor{c.Target, m.ResourceMaximum})
			}
		}
		ch.Resources[m.ResourceMaximum] = after
		out = append(out, ConditionResourceChange{Modifier: m.ID, Resource: m.ResourceMaximum, Amount: amount, Before: before, After: after})
	}
	return out, floors
}

// validateConditionDrains keeps every drain's ending representable: a
// Resource maximum plus its active drains fits int64. Raises and expansion
// check the same bound, so it holds from genesis through every request.
func validateConditionDrains(d *derivation) error {
	b := d.b
	type key struct {
		Character CharacterID
		Resource  ResourceID
	}
	var order []key
	drains := map[key]uint64{}
	for _, c := range b.Conditions {
		for _, m := range d.definition(c.Type).Modifiers {
			if amount := modifierAmount(m, c); m.ResourceMaximum != "" && amount < 0 {
				k := key{c.Target, m.ResourceMaximum}
				if _, seen := drains[k]; !seen {
					order = append(order, k)
				}
				drains[k] = saturatingAdd(drains[k], uint64(-amount))
			}
		}
	}
	for _, k := range order {
		r := d.character(k.Character).Resources[k.Resource]
		if drains[k] > headroom(r.Maximum) {
			return fail("E_OVERFLOW", string(k.Character), string(k.Resource), "maximum + active drains fits int64", fmt.Sprintf("maximum=%d drains=%d", r.Maximum, drains[k]), "Condition drains cannot be given back")
		}
	}
	return nil
}

// Condition effects are generated values, never writable canonical records.
// Lengths are byte lengths; the pair encoding is injective even with punctuation.
// Resource maximum modifiers are stored, bounds are applied to totals and roll
// modes are applied to checks; none is a generated Effect.
func conditionEffects(b *Blueprint, c ConditionInstance) []Effect {
	return instanceEffects(c, conditionDefinition(b, c.Type))
}

// instanceEffects is conditionEffects for a definition already found, or nil
// for a missing one.
func instanceEffects(c ConditionInstance, d *ConditionDefinition) []Effect {
	var out []Effect
	if d == nil {
		return out
	} // Invalid declarations are rejected before evaluation.
	for _, m := range d.Modifiers {
		if m.ResourceMaximum != "" || m.Bound != "" || m.Roll != "" || m.Die != nil {
			continue
		}
		amount := modifierAmount(m, c)
		id := EffectID(fmt.Sprintf("condition:%d:%s:%d:%s", len(c.ID), c.ID, len(m.ID), m.ID))
		out = append(out, Effect{ID: id, Source: Ref{ConditionKind, string(c.ID)}, Mode: OnCharacter, Target: Ref{CharacterKind, string(c.Target)}, Stat: m.Stat, Amount: amount, ConditionOrigin: &ConditionEffectOrigin{c.ID, m.ID}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func derivedConditionEffects(b *Blueprint) []Effect {
	return newDerivation(b).derivedConditionEffects()
}

// The derivation's effect lists are built once and shared by its callers,
// which only read them.
func (d *derivation) derivedConditionEffects() []Effect {
	if !d.conditionEffectsBuilt {
		var out []Effect
		for _, c := range d.b.Conditions {
			out = append(out, instanceEffects(c, d.definition(c.Type))...)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		d.conditionEffects, d.conditionEffectsBuilt = out, true
	}
	return d.conditionEffects
}
func allEffects(b *Blueprint) []Effect { return newDerivation(b).allEffects() }
func (d *derivation) allEffects() []Effect {
	if !d.effectsBuilt {
		out := append([]Effect{}, d.b.Effects...)
		out = append(out, d.derivedConditionEffects()...)
		out = append(out, d.derivedEquipmentEffects()...)
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		d.effects, d.effectsBuilt = out, true
	}
	return d.effects
}
func effectExists(b *Blueprint, id EffectID) bool {
	for _, e := range allEffects(b) {
		if e.ID == id {
			return true
		}
	}
	return false
}

// validateConditions checks the Conditions of x's Blueprint, which does not
// change during the check.
func validateConditions(x *derivation) error {
	b := x.b
	if err := checkClockLifetimes(b); err != nil {
		return err
	}
	types := map[ConditionTypeID]bool{}
	for _, d := range b.ConditionDefinitions {
		if (d.UntilSourceTurn || d.EndsAfterAttack) && b.Battle == nil {
			return conditionFault("E_RULE", string(d.ID), "battle lifetime", "Battle profile", "absent")
		}
		if types[d.ID] {
			return conditionFault("E_DUPLICATE", string(d.ID), "condition_type", "unique type ID", "duplicate")
		}
		types[d.ID] = true
		if err := CheckConditionDefinition(d); err != nil {
			return err
		}
		if err := checkConditionDefinitionReferences(x, d); err != nil {
			return err
		}
	}
	ids := map[ConditionInstanceID]bool{}
	for _, id := range b.RetiredConditionIDs {
		if !validID(string(id)) {
			return conditionFault("E_ID", string(id), "retired_id", "valid instance ID", "invalid")
		}
		if ids[id] {
			return conditionFault("E_DUPLICATE", string(id), "retired_id", "unique instance ID", "duplicate")
		}
		ids[id] = true
	}
	unique := map[struct {
		Type   ConditionTypeID
		Target CharacterID
	}]bool{}
	for _, c := range b.Conditions {
		if ids[c.ID] {
			return conditionFault("E_DUPLICATE", string(c.ID), "condition_instance", "unused lifetime identity", "duplicate or retired")
		}
		ids[c.ID] = true
		if err := checkConditionStatic(x, conditionIntent(c)); err != nil {
			return err
		}
		if err := checkConditionMaintenance(x, c); err != nil {
			return err
		}
		if x.definition(c.Type).Instances != Coexist {
			k := struct {
				Type   ConditionTypeID
				Target CharacterID
			}{c.Type, c.Target}
			if unique[k] {
				return conditionFault("E_RULE", string(c.ID), "instances", "one instance per type/target", "multiple")
			}
			unique[k] = true
		}
		if c.AppliedRevision == 0 {
			if c.Cause != nil {
				return conditionFault("E_PROVENANCE", string(c.ID), "cause", "nil for genesis", "present")
			}
		} else {
			if c.Cause == nil || !validID(c.Cause.RequestID) || c.Cause.Operation < 0 || c.Cause.Operation >= MaxOperations {
				return conditionFault("E_PROVENANCE", string(c.ID), "cause", "valid committed causation", "invalid")
			}
			if err := checkAbilityOrigin(b, c.Cause.AbilityOrigin); err != nil {
				return err
			}
			if err := checkRef(c.Cause.Actor, x.kindOf); err != nil {
				return err
			}
		}
	}
	effects := map[EffectID]bool{}
	for _, e := range b.Effects {
		effects[e.ID] = true
	}
	for _, e := range x.derivedConditionEffects() {
		if effects[e.ID] {
			return conditionFault("E_DUPLICATE", string(e.ID), "effect", "unique active effect identity", "collision")
		}
		effects[e.ID] = true
	}
	return nil
}
func validateConditionRevisions(b *Blueprint, revision Revision) error {
	for _, c := range b.Conditions {
		if c.AppliedRevision > revision {
			return conditionFault("E_PROVENANCE", string(c.ID), "applied_revision", "at most world revision", fmt.Sprint(c.AppliedRevision))
		}
	}
	return nil
}

type applicationContext struct {
	conditionInput *conditionInputCursor
	random         *requestRandom
	work           *int
	revision       Revision
	cause          ConditionCause
}

func applyConditionOperation(b *Blueprint, o Operation, ctx applicationContext) ([]Event, error) {
	if o.ApplyCondition != nil {
		v := o.ApplyCondition
		c := ConditionInstance{ID: v.Instance, Type: v.Type, Target: v.Target, Source: copyPtr(v.Source), Magnitude: copyPtr(v.Magnitude), Anchor: copyPtr(v.Anchor), AppliedRevision: ctx.revision, Cause: cloneConditionCause(&ctx.cause)}
		if v.Duration != nil {
			lifetime, e := b.conditionLifetime(*v.Duration)
			if e != nil {
				return nil, e
			}
			c.Lifetime = lifetime
		}
		if v.ClockDuration != nil {
			lifetime, e := b.clockLifetime(*v.ClockDuration)
			if e != nil {
				return nil, e
			}
			c.ClockLifetime = lifetime
		}
		// Check the incoming absence law before any removal, including replacement.
		// A trigger cannot erase its own blocker to make itself legal.
		if err := checkConditionBlocker(scan{b}, c); err != nil {
			return nil, err
		}
		events, err := replaceConditions(b, c, ctx)
		if err != nil {
			return nil, err
		}
		ended, err := endConditionsOnApply(b, c, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, ended...)
		// Drains run before the instance joins, so a floor they reach cannot
		// end it; validation below refuses it if that release took its anchor
		// or it would itself end at that minimum. Releases precede the
		// terminal applied event, which carries the replayed intent.
		resources, floors, err := drainConditionMaxima(b, c)
		if err != nil {
			return nil, err
		}
		for _, f := range floors {
			released, err := endConditionsAtMinimum(b, f.Character, f.Resource, ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, released...)
		}
		b.Conditions = append(b.Conditions, c)
		// Structural rules hold after each operation; numeric bounds are final-only.
		if err := validateConditions(newDerivation(b)); err != nil {
			return nil, err
		}
		raised, err := raiseConditionMaxima(b, c)
		if err != nil {
			return nil, err
		}
		resources = append(resources, raised...)
		sort.SliceStable(resources, func(i, j int) bool { return resources[i].Modifier < resources[j].Modifier })
		return append(events, Event{Kind: ConditionApplied, Condition: &ConditionChange{After: cloneCondition(&c), AfterEffects: conditionEffects(b, c), Resources: resources}}), nil
	}
	v := o.RemoveCondition
	index := conditionIndex(b, v.Instance)
	if index < 0 {
		return nil, conditionFault("E_CONDITION", string(v.Instance), "condition_instance", "active instance", "missing")
	}
	c := b.Conditions[index]
	if c.Target != v.Target {
		return nil, conditionFault("E_TARGET", string(v.Instance), "target", string(c.Target), string(v.Target))
	}
	if v.Type != "" && c.Type != v.Type {
		return nil, conditionFault("E_CONDITION_TYPE", string(v.Instance), "condition_type", string(v.Type), string(c.Type))
	}
	events, err := removeConditionGroup(b, c, ctx)
	if err != nil {
		return nil, err
	}
	// The terminal root event retains optional intent; legacy removals omit it.
	events[len(events)-1].Condition.ExpectedType = v.Type
	return events, nil
}
func cloneCondition(c *ConditionInstance) *ConditionInstance {
	if c == nil {
		return nil
	}
	out := *c
	out.Source = copyPtr(c.Source)
	out.Anchor = copyPtr(c.Anchor)
	out.Magnitude = copyPtr(c.Magnitude)
	out.Cause = cloneConditionCause(c.Cause)
	out.Lifetime = copyPtr(c.Lifetime)
	out.ClockLifetime = copyPtr(c.ClockLifetime)
	return &out
}
func cloneApplyCondition(v *ApplyCondition) *ApplyCondition {
	if v == nil {
		return nil
	}
	out := *v
	out.Source = copyPtr(v.Source)
	out.Anchor = copyPtr(v.Anchor)
	out.Duration = copyPtr(v.Duration)
	out.ClockDuration = copyPtr(v.ClockDuration)
	out.Magnitude = copyPtr(v.Magnitude)
	return &out
}
func cloneEffect(e *Effect) *Effect {
	if e == nil {
		return nil
	}
	out := *e
	out.ConditionOrigin = copyPtr(e.ConditionOrigin)
	out.EquipmentOrigin = copyPtr(e.EquipmentOrigin)
	return &out
}
func cloneEffects(es []Effect) []Effect {
	if es == nil {
		return nil
	}
	out := make([]Effect, len(es))
	for i := range es {
		out[i] = *cloneEffect(&es[i])
	}
	return out
}
func cloneConditionData(b *Blueprint) {
	if b.ConditionDefinitions != nil {
		defs := make([]ConditionDefinition, len(b.ConditionDefinitions))
		for i, d := range b.ConditionDefinitions {
			defs[i] = d
			defs[i].EndsAtMinimum = copyPtr(d.EndsAtMinimum)
			defs[i].HeldAtMinimum = copyPtr(d.HeldAtMinimum)
			defs[i].EndsOnCondition = copyPtr(d.EndsOnCondition)
			if d.Includes != nil {
				defs[i].Includes = append([]ConditionTypeID{}, d.Includes...)
			}
			defs[i].Attacks = cloneEquipmentAttacks(d.Attacks)
			if d.Modifiers != nil {
				defs[i].Modifiers = make([]ConditionModifier, len(d.Modifiers))
				for j, m := range d.Modifiers {
					defs[i].Modifiers[j] = m
					defs[i].Modifiers[j].Fixed = copyPtr(m.Fixed)
					defs[i].Modifiers[j].Die = copyPtr(m.Die)
				}
			}
		}
		b.ConditionDefinitions = defs
	}
	if b.Conditions != nil {
		cs := make([]ConditionInstance, len(b.Conditions))
		for i := range b.Conditions {
			cs[i] = *cloneCondition(&b.Conditions[i])
		}
		b.Conditions = cs
	}
	if b.RetiredConditionIDs != nil {
		b.RetiredConditionIDs = append([]ConditionInstanceID{}, b.RetiredConditionIDs...)
	}
}
func normalizeConditions(b *Blueprint) {
	sort.Slice(b.ConditionDefinitions, func(i, j int) bool { return b.ConditionDefinitions[i].ID < b.ConditionDefinitions[j].ID })
	for i := range b.ConditionDefinitions {
		ms := b.ConditionDefinitions[i].Modifiers
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
		ts := b.ConditionDefinitions[i].Includes
		sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	}
	sort.Slice(b.Conditions, func(i, j int) bool { return b.Conditions[i].ID < b.Conditions[j].ID })
	sort.Slice(b.RetiredConditionIDs, func(i, j int) bool { return b.RetiredConditionIDs[i] < b.RetiredConditionIDs[j] })
}

func cloneConditionCause(v *ConditionCause) *ConditionCause {
	if v == nil {
		return nil
	}
	out := *v
	out.AbilityOrigin = cloneAbilityOrigin(v.AbilityOrigin)
	return &out
}
