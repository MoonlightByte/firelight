package core

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

type AbilityID string
type ParameterID string
type ArgumentKind string

const (
	CharacterArgument    ArgumentKind = "character"
	ItemArgument         ArgumentKind = "item"
	IntegerArgument      ArgumentKind = "int"
	ConditionIDArgument  ArgumentKind = "condition_id"
	ConditionRefArgument ArgumentKind = "condition_ref"
)

// AbilityValue is a closed sum. A creation ID is a value, not an EntityRef.
type AbilityValue struct {
	ConditionRef *ConditionInstanceID `json:"condition_ref,omitempty"`
	Character    *CharacterID         `json:"character,omitempty"`
	Item         *ItemID              `json:"item,omitempty"`
	Integer      *int64               `json:"integer,omitempty"`
	ConditionID  *ConditionInstanceID `json:"condition_id,omitempty"`
}
type AbilityParameter struct {
	ID   ParameterID  `json:"id"`
	Kind ArgumentKind `json:"kind"`
}
type AbilityOperand struct {
	Parameter ParameterID   `json:"parameter,omitempty"`
	Literal   *AbilityValue `json:"literal,omitempty"`
}
type DamageTemplate struct {
	Target   AbilityOperand `json:"target"`
	Resource ResourceID     `json:"resource"`
	Amount   AbilityOperand `json:"amount"`
}
type HealTemplate struct {
	Target   AbilityOperand `json:"target"`
	Resource ResourceID     `json:"resource"`
	Amount   AbilityOperand `json:"amount"`
}
type ExpandTemplate struct {
	Formula  *CapacityFormula `json:"formula,omitempty"`
	Target   AbilityOperand   `json:"target"`
	Resource ResourceID       `json:"resource"`
	Amount   AbilityOperand   `json:"amount"`
}
type RestoreTemplate struct {
	Target   AbilityOperand `json:"target"`
	Resource ResourceID     `json:"resource"`
}
type SpendTemplate struct {
	Payer    AbilityOperand `json:"payer"`
	Resource ResourceID     `json:"resource"`
	Amount   AbilityOperand `json:"amount"`
}
type ConditionTemplate struct {
	Anchor        *AbilityOperand `json:"anchor,omitempty"`
	ClockDuration *uint64         `json:"clock_duration,omitempty"`
	Duration      *uint64         `json:"duration,omitempty"`
	Instance      AbilityOperand  `json:"instance"`
	Type          ConditionTypeID `json:"type"`
	Target        AbilityOperand  `json:"target"`
	Source        *Ref            `json:"source,omitempty"`
	Magnitude     *AbilityOperand `json:"magnitude,omitempty"`
}

// ConditionRemovalTemplate selects one existing instance; the immutable type
// guard prevents substituting an unrelated Condition into an authored action.
type ConditionRemovalTemplate struct {
	Instance AbilityOperand  `json:"instance"`
	Type     ConditionTypeID `json:"type"`
	Target   AbilityOperand  `json:"target"`
}

// ConsumeTemplate composes the existing exact stock operation. It adds no
// item classification or effect rule; authors determine what consumption does.
type ConsumeTemplate struct {
	Item   AbilityOperand `json:"item"`
	Owner  AbilityOperand `json:"owner"`
	Amount AbilityOperand `json:"amount"`
}
type TransferTemplate struct {
	Item AbilityOperand `json:"item"`
	From AbilityOperand `json:"from"`
	To   AbilityOperand `json:"to"`
}
type EquipTemplate struct {
	Mode   EquipmentModeID `json:"mode,omitempty"`
	Item   AbilityOperand  `json:"item"`
	Wearer AbilityOperand  `json:"wearer"`
}
type UnequipTemplate struct {
	Item AbilityOperand `json:"item"`
}
type AbilityStep struct {
	AdjustRelationship *AdjustRelationshipTemplate `json:"adjust_relationship,omitempty"`
	Consume            *ConsumeTemplate            `json:"consume,omitempty"`
	RemoveCondition    *ConditionRemovalTemplate   `json:"remove_condition,omitempty"`
	Expand             *ExpandTemplate             `json:"expand,omitempty"`
	Place              *PlaceTemplate              `json:"place,omitempty"`
	RollResource       *RollResourceTemplate       `json:"roll_resource,omitempty"`
	AdvanceTime        *AdvanceTimeTemplate        `json:"advance_time,omitempty"`
	Check              *CheckTemplate              `json:"check,omitempty"`
	Travel             *PartyTravel                `json:"travel,omitempty"`
	Claim              *ClaimTemplate              `json:"claim,omitempty"`
	Equip              *EquipTemplate              `json:"equip,omitempty"`
	Unequip            *UnequipTemplate            `json:"unequip,omitempty"`
	Damage             *DamageTemplate             `json:"damage,omitempty"`
	Heal               *HealTemplate               `json:"heal,omitempty"`
	Restore            *RestoreTemplate            `json:"restore,omitempty"`
	Spend              *SpendTemplate              `json:"spend,omitempty"`
	ApplyCondition     *ConditionTemplate          `json:"apply_condition,omitempty"`
	Transfer           *TransferTemplate           `json:"transfer,omitempty"`
}
type AbilityDefinition struct {
	Action     *ActionPolicy      `json:"action,omitempty"`
	ID         AbilityID          `json:"id"`
	Name       string             `json:"name"`
	Parameters []AbilityParameter `json:"parameters"`
	Steps      []AbilityStep      `json:"steps"`
}
type AbilityArgument struct {
	Name  ParameterID  `json:"name"`
	Value AbilityValue `json:"value"`
}
type InvokeAbility struct {
	CheckBoost *CheckBoostInput   `json:"check_boost,omitempty"`
	Allowance  ResourceID         `json:"allowance,omitempty"`
	HumanRolls []AbilityRollInput `json:"human_rolls,omitempty"`
	Ability    AbilityID          `json:"ability"`
	Arguments  []AbilityArgument  `json:"arguments"`
}

// The request and outer Operation identify the invocation occurrence. Step is
// an authored body index, never a source location or a flattened operation index.
type AbilityOrigin struct {
	Path    []int     `json:"path,omitempty"`
	Ability AbilityID `json:"ability"`
	Step    int       `json:"step"`
}

// AbilityError supplies semantic context to the compiler without carrying any
// source data. Unwrap preserves ordinary errors.As(..., *Fault) consumers.
type AbilityError struct {
	Path       []int
	Definition AbilityID
	Step       int
	Operand    string
	Parameter  ParameterID
	Err        error
}

func (e *AbilityError) Error() string {
	s := fmt.Sprintf("%v; ability %q step %d operand %q parameter %q", e.Err, e.Definition, e.Step, e.Operand, e.Parameter)
	if e.Path != nil {
		s += fmt.Sprintf(" path %v", e.Path)
	}
	return s
}
func (e *AbilityError) Unwrap() error { return e.Err }
func abilityError(id AbilityID, step int, operand string, parameter ParameterID, err error) error {
	if err == nil {
		return nil
	}
	return &AbilityError{Definition: id, Step: step, Operand: operand, Parameter: parameter, Err: err}
}
func abilityFault(code, field, expected, actual string) error {
	return fail(code, "", field, expected, actual, "invalid ability "+field)
}
func argumentKind(k ArgumentKind) bool {
	return k == CharacterArgument || k == ItemArgument || k == IntegerArgument || k == ConditionIDArgument || k == ConditionRefArgument
}
func (v AbilityValue) kind() (ArgumentKind, error) {
	kind := ArgumentKind("")
	count := 0
	for _, x := range []struct {
		yes  bool
		kind ArgumentKind
	}{{v.Character != nil, CharacterArgument}, {v.Item != nil, ItemArgument}, {v.Integer != nil, IntegerArgument}, {v.ConditionID != nil, ConditionIDArgument}, {v.ConditionRef != nil, ConditionRefArgument}} {
		if x.yes {
			count++
			kind = x.kind
		}
	}
	if count != 1 {
		return "", abilityFault("E_IR", "value", "exactly one typed argument value", "invalid sum")
	}
	return kind, nil
}
func (b Blueprint) checkAbilityValue(v AbilityValue, want ArgumentKind) error {
	k, e := v.kind()
	if e != nil {
		return e
	}
	if k != want {
		return abilityFault("E_TYPE", "value", string(want), string(k))
	}
	switch k {
	case CharacterArgument:
		return b.CheckRef(Ref{CharacterKind, string(*v.Character)})
	case ItemArgument:
		return b.CheckRef(Ref{ItemKind, string(*v.Item)})
	case ConditionRefArgument:
		if !validID(string(*v.ConditionRef)) {
			return abilityFault("E_ID", "condition_ref", "valid existing instance ID", string(*v.ConditionRef))
		}
	case ConditionIDArgument:
		if !validID(string(*v.ConditionID)) {
			return abilityFault("E_ID", "condition_id", "valid creation ID", string(*v.ConditionID))
		}
	}
	return nil
}

type templateOperand struct {
	field string
	value AbilityOperand
	kind  ArgumentKind
}

func (s AbilityStep) operands() ([]templateOperand, error) {
	n := 0
	if s.AdjustRelationship != nil {
		n++
	}
	if s.Consume != nil {
		n++
	}
	if s.RemoveCondition != nil {
		n++
	}
	if s.Place != nil {
		n++
	}
	if s.RollResource != nil {
		n++
	}
	if s.AdvanceTime != nil {
		n++
	}
	if s.Check != nil {
		n++
	}
	if s.Travel != nil {
		n++
	}
	if s.Claim != nil {
		n++
	}
	if s.Equip != nil {
		n++
	}
	if s.Unequip != nil {
		n++
	}
	if s.Damage != nil {
		n++
	}
	if s.Heal != nil {
		n++
	}
	if s.Expand != nil {
		n++
	}
	if s.Restore != nil {
		n++
	}
	if s.Spend != nil {
		n++
	}
	if s.ApplyCondition != nil {
		n++
	}
	if s.Transfer != nil {
		n++
	}
	if n != 1 {
		return nil, abilityFault("E_IR", "step", "one damage, heal, spend, apply condition, transfer, equip or unequip", "invalid sum")
	}
	if v := s.AdjustRelationship; v != nil {
		return []templateOperand{{"from", v.From, CharacterArgument}, {"to", v.To, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.Consume; v != nil {
		return []templateOperand{{"item", v.Item, ItemArgument}, {"owner", v.Owner, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.RemoveCondition; v != nil {
		return []templateOperand{{"instance", v.Instance, ConditionRefArgument}, {"target", v.Target, CharacterArgument}}, nil
	}
	if s.Place != nil {
		return placeOperands(*s.Place)
	}
	if r := s.RollResource; r != nil {
		return []templateOperand{{"target", r.Target, CharacterArgument}, {"caster", r.Caster, CharacterArgument}}, nil
	}
	if s.AdvanceTime != nil {
		return []templateOperand{{"ticks", s.AdvanceTime.Ticks, IntegerArgument}}, nil
	}
	if v := s.Check; v != nil {
		return checkOperands(*v)
	}
	if s.Travel != nil {
		return nil, nil
	}
	if s.Claim != nil {
		return []templateOperand{{"recipient", s.Claim.Recipient, CharacterArgument}}, nil
	}
	if v := s.Equip; v != nil {
		if v.Mode != "" && !validID(string(v.Mode)) {
			return nil, abilityFault("E_ID", "mode", "valid literal equipment mode", string(v.Mode))
		}
		return []templateOperand{{"item", v.Item, ItemArgument}, {"wearer", v.Wearer, CharacterArgument}}, nil
	}
	if v := s.Unequip; v != nil {
		return []templateOperand{{"item", v.Item, ItemArgument}}, nil
	}
	if v := s.Damage; v != nil {
		return []templateOperand{{"target", v.Target, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.Heal; v != nil {
		return []templateOperand{{"target", v.Target, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.Expand; v != nil {
		if v.Formula != nil {
			if v.Amount.Parameter != "" || v.Amount.Literal != nil {
				return nil, abilityFault("E_IR", "amount", "one amount or formula", "both present")
			}
			return []templateOperand{{"target", v.Target, CharacterArgument}, {"formula_source", v.Formula.Source, CharacterArgument}}, nil
		}
		return []templateOperand{{"target", v.Target, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.Restore; v != nil {
		return []templateOperand{{"target", v.Target, CharacterArgument}}, nil
	}
	if v := s.Spend; v != nil {
		return []templateOperand{{"payer", v.Payer, CharacterArgument}, {"amount", v.Amount, IntegerArgument}}, nil
	}
	if v := s.Transfer; v != nil {
		return []templateOperand{{"item", v.Item, ItemArgument}, {"from", v.From, CharacterArgument}, {"to", v.To, CharacterArgument}}, nil
	}
	v := s.ApplyCondition
	out := []templateOperand{{"instance", v.Instance, ConditionIDArgument}, {"target", v.Target, CharacterArgument}}
	if v.Anchor != nil {
		out = append(out, templateOperand{"anchor", *v.Anchor, ConditionIDArgument})
	}
	if v.Magnitude != nil {
		out = append(out, templateOperand{"magnitude", *v.Magnitude, IntegerArgument})
	}
	return out, nil
}

// CheckAbilityDefinition checks facts known at declaration time. Missing
// parameter-dependent resources/stats are checked only when actual values bind.
func (b Blueprint) CheckAbilityDefinition(d AbilityDefinition) error {
	fault := func(step int, field string, p ParameterID, e error) error {
		return abilityError(d.ID, step, field, p, e)
	}
	if !validID(string(d.ID)) || !utf8.ValidString(d.Name) {
		return fault(-1, "identity", "", abilityFault("E_ID", "identity", "valid ID and UTF-8 name", "invalid"))
	}
	if (len(d.Steps) == 0 && (d.Action == nil || d.Action.Phase != EncounterPhase)) || len(d.Steps) > MaxOperations {
		return fault(-1, "steps", "", abilityFault("E_LIMIT", "steps", "1..128 body steps", fmt.Sprint(len(d.Steps))))
	}
	parameters := map[ParameterID]ArgumentKind{}
	for _, p := range d.Parameters {
		if !validID(string(p.ID)) {
			return fault(-1, "parameter", p.ID, abilityFault("E_ID", "parameter", "valid parameter ID", string(p.ID)))
		}
		if _, ok := parameters[p.ID]; ok {
			return fault(-1, "parameter", p.ID, abilityFault("E_DUPLICATE", "parameter", "unique local parameter", string(p.ID)))
		}
		if !argumentKind(p.Kind) {
			return fault(-1, "parameter_kind", p.ID, abilityFault("E_TYPE", "parameter_kind", "character, item, int, condition_id or condition_ref", string(p.Kind)))
		}
		parameters[p.ID] = p.Kind
	}
	if e := b.checkActionPolicy(d, parameters); e != nil {
		return e
	}
	checks, rolls := 0, 0
	for i, s := range d.Steps {
		operands, e := s.operands()
		if e != nil {
			return fault(i, "step", "", e)
		}
		for _, o := range operands {
			v := o.value
			if (v.Parameter != "") == (v.Literal != nil) {
				return fault(i, o.field, v.Parameter, abilityFault("E_IR", "operand", "exactly one parameter or literal", "invalid sum"))
			}
			if v.Parameter != "" {
				k, ok := parameters[v.Parameter]
				if !ok {
					return fault(i, o.field, v.Parameter, abilityFault("E_PARAMETER", "parameter", "declared local parameter", string(v.Parameter)))
				}
				if k != o.kind {
					return fault(i, o.field, v.Parameter, abilityFault("E_TYPE", "parameter", string(o.kind), string(k)))
				}
			} else if e := b.checkAbilityValue(*v.Literal, o.kind); e != nil {
				return fault(i, o.field, "", e)
			}
		}

		if v := s.AdjustRelationship; v != nil {
			df := b.relationDefinition(v.Kind)
			if df == nil {
				return fault(i, "relation_kind", "", fail("E_RELATION_KIND", string(v.Kind), "relation_kind", "declared bounded relation kind", "missing", "adjustment requires authored relation bounds"))
			}
			if err := CheckRelationDefinition(*df); err != nil {
				return fault(i, "relation_kind", "", err)
			}
			if v.Amount.Literal != nil && *v.Amount.Literal.Integer == 0 {
				return fault(i, "amount", "", abilityFault("E_AMOUNT", "amount", "nonzero signed int64", "zero"))
			}
		}
		if v := s.RemoveCondition; v != nil {
			if conditionDefinition(&b, v.Type) == nil {
				return fault(i, "type", "", conditionFault("E_CONDITION_TYPE", string(v.Type), "condition_type", "declared literal Condition type", "missing"))
			}
		}
		if s.Place != nil {
			if err := b.checkPlaceTemplate(d, i, *s.Place); err != nil {
				return err
			}
		}
		if s.RollResource != nil {
			rolls++
			if rolls > 1 || checks > 0 {
				return fault(i, "roll", "", abilityFault("E_LIMIT", "roll", "one top-level rolled consequence without Check", "multiple/branching"))
			}
			if err := b.checkRollTemplate(d, i, *s.RollResource); err != nil {
				return err
			}
		}
		if s.Check != nil {
			if rolls > 0 {
				return fault(i, "check", "", abilityFault("E_LIMIT", "check", "no Check with rolled Resource", "present"))
			}
			checks++
			if checks > 1 {
				return fault(i, "check", "", abilityFault("E_LIMIT", "check", "at most one check", "multiple"))
			}
			if e := b.checkCheckTemplate(d, i, *s.Check); e != nil {
				return e
			}
		}
		if x := s.AdvanceTime; x != nil {
			if b.Clock == nil {
				return fault(i, "ticks", "", clockFault("", "advance", "Clock present", "absent"))
			}
			if x.Ticks.Literal != nil && *x.Ticks.Literal.Integer <= 0 {
				return fault(i, "ticks", "", clockFault("", "ticks", "positive int64", "nonpositive"))
			}
		}
		if s.Travel != nil {
			if e := b.CheckRef(Ref{LocationKind, string(s.Travel.To)}); e != nil {
				return fault(i, "destination", "", e)
			}
		}
		if s.Claim != nil && b.objectiveDefinition(s.Claim.Objective) == nil {
			return fault(i, "objective", "", abilityFault("E_OBJECTIVE", "objective", "declared objective", string(s.Claim.Objective)))
		}
		if v := s.Equip; v != nil && v.Item.Literal != nil {
			if err := b.checkEquipMode(Equip{Item: *v.Item.Literal.Item, Mode: v.Mode}); err != nil {
				return fault(i, "mode", "", err)
			}
		}
		if c := s.Consume; c != nil && c.Amount.Literal != nil && *c.Amount.Literal.Integer <= 0 {
			return fault(i, "amount", "", abilityFault("E_AMOUNT", "quantity", "positive int64", "nonpositive"))
		}
		if h := s.Damage; h != nil {
			if !validID(string(h.Resource)) {
				return fault(i, "resource", "", abilityFault("E_ID", "resource", "valid resource ID", string(h.Resource)))
			}
			if h.Amount.Literal != nil && *h.Amount.Literal.Integer <= 0 {
				return fault(i, "amount", "", abilityFault("E_AMOUNT", "amount", "positive int64", "nonpositive"))
			}
			if h.Target.Literal != nil {
				if e := b.CheckResource(*h.Target.Literal.Character, h.Resource); e != nil {
					return fault(i, "target", "", e)
				}
			}
		}
		if h := s.Restore; h != nil {
			if !validID(string(h.Resource)) {
				return fault(i, "resource", "", abilityFault("E_ID", "resource", "valid resource ID", string(h.Resource)))
			}
			if h.Target.Literal != nil {
				if e := b.CheckResource(*h.Target.Literal.Character, h.Resource); e != nil {
					return fault(i, "target", "", e)
				}
			}
		}
		if h := s.Expand; h != nil {
			if h.Formula != nil {
				if err := b.checkCapacityFormula(d, i, *h); err != nil {
					return err
				}
			}
			if !validID(string(h.Resource)) {
				return fault(i, "resource", "", abilityFault("E_ID", "resource", "valid resource ID", string(h.Resource)))
			}
			if h.Amount.Literal != nil && *h.Amount.Literal.Integer <= 0 {
				return fault(i, "amount", "", abilityFault("E_AMOUNT", "amount", "positive int64", "nonpositive"))
			}
			if h.Target.Literal != nil {
				if e := b.CheckResource(*h.Target.Literal.Character, h.Resource); e != nil {
					field := "target"
					if f, ok := e.(*Fault); ok && f.Code == "E_RESOURCE" {
						field = "resource"
					}
					return fault(i, field, "", e)
				}
			}
		}
		if h := s.Heal; h != nil {
			if !validID(string(h.Resource)) {
				return fault(i, "resource", "", abilityFault("E_ID", "resource", "valid resource ID", string(h.Resource)))
			}
			if h.Amount.Literal != nil && *h.Amount.Literal.Integer <= 0 {
				return fault(i, "amount", "", abilityFault("E_AMOUNT", "amount", "positive int64", "nonpositive"))
			}
			if h.Target.Literal != nil {
				if e := b.CheckResource(*h.Target.Literal.Character, h.Resource); e != nil {
					return fault(i, "target", "", e)
				}
			}
		}
		if s := s.Spend; s != nil {
			if !validID(string(s.Resource)) {
				return fault(i, "resource", "", abilityFault("E_ID", "resource", "valid resource ID", string(s.Resource)))
			}
			if s.Amount.Literal != nil && *s.Amount.Literal.Integer <= 0 {
				return fault(i, "amount", "", abilityFault("E_AMOUNT", "amount", "positive int64", "nonpositive"))
			}
			if s.Payer.Literal != nil {
				if e := b.CheckResource(*s.Payer.Literal.Character, s.Resource); e != nil {
					return fault(i, "payer", "", e)
				}
			}
		}
		if c := s.ApplyCondition; c != nil {
			if c.ClockDuration != nil && (c.Duration != nil || b.Clock == nil || *c.ClockDuration == 0) {
				return fault(i, "clock_duration", "", clockFault("", "duration", "positive clock duration, no round duration, Clock present", "invalid"))
			}
			if c.Duration != nil && *c.Duration == 0 {
				return fault(i, "duration", "", abilityFault("E_DURATION", "duration", "positive round count", "zero"))
			}
			df := conditionDefinition(&b, c.Type)
			if df == nil {
				return fault(i, "type", "", conditionFault("E_CONDITION_TYPE", string(c.Type), "condition_type", "declared condition type", "missing"))
			}
			if e := CheckConditionDefinition(*df); e != nil {
				return fault(i, "type", "", e)
			}
			if c.Source != nil {
				if e := b.CheckRef(*c.Source); e != nil {
					return fault(i, "source", "", e)
				}
			}
			if c.Target.Literal != nil && df.EndsAtMinimum != nil {
				if e := b.CheckResource(*c.Target.Literal.Character, *df.EndsAtMinimum); e != nil {
					return fault(i, "target", "", e)
				}
			}
			uses := false
			for _, m := range df.Modifiers {
				uses = uses || m.Magnitude
				if c.Target.Literal != nil {
					if e := b.checkModifierTarget(*c.Target.Literal.Character, m); e != nil {
						return fault(i, "target", "", e)
					}
				}
			}
			if uses != (c.Magnitude != nil) {
				return fault(i, "magnitude", "", conditionFault("E_CONDITION_AMOUNT", string(c.Type), "magnitude", fmt.Sprintf("present=%t", uses), fmt.Sprintf("present=%t", c.Magnitude != nil)))
			}
		}
	}
	return nil
}
func abilityDefinition(b *Blueprint, id AbilityID) *AbilityDefinition {
	for i := range b.AbilityDefinitions {
		if b.AbilityDefinitions[i].ID == id {
			return &b.AbilityDefinitions[i]
		}
	}
	return nil
}
func validateAbilities(b *Blueprint) error {
	seen := map[AbilityID]bool{}
	for _, d := range b.AbilityDefinitions {
		if seen[d.ID] {
			return abilityError(d.ID, -1, "identity", "", abilityFault("E_DUPLICATE", "identity", "unique Ability ID", string(d.ID)))
		}
		seen[d.ID] = true
		if e := b.CheckAbilityDefinition(d); e != nil {
			return e
		}
	}
	return nil
}

// bindAbility performs structural substitution, never evaluation of source or
// mutable mechanical preconditions. Execute checks/applies each bound primitive.
func (b Blueprint) bindAbility(v InvokeAbility) ([]boundStep, error) {
	fault := func(p ParameterID, e error) ([]boundStep, error) {
		return nil, abilityError(v.Ability, -1, "argument", p, e)
	}
	d := abilityDefinition(&b, v.Ability)
	if d == nil {
		return fault("", abilityFault("E_ABILITY", "ability", "declared Ability", string(v.Ability)))
	}
	if e := b.CheckAbilityDefinition(*d); e != nil {
		return nil, e
	}
	params := map[ParameterID]ArgumentKind{}
	for _, p := range d.Parameters {
		params[p.ID] = p.Kind
	}
	args := map[ParameterID]AbilityValue{}
	for _, a := range v.Arguments {
		k, ok := params[a.Name]
		if !ok {
			return fault(a.Name, abilityFault("E_ARGUMENT", "argument", "declared parameter", string(a.Name)))
		}
		if _, ok := args[a.Name]; ok {
			return fault(a.Name, abilityFault("E_DUPLICATE", "argument", "one binding per parameter", string(a.Name)))
		}
		if e := b.checkAbilityValue(a.Value, k); e != nil {
			return fault(a.Name, e)
		}
		args[a.Name] = a.Value
	}
	for _, p := range d.Parameters {
		if _, ok := args[p.ID]; !ok {
			return fault(p.ID, abilityFault("E_ARGUMENT", "argument", "required binding", "missing"))
		}
	}
	steps := make([]boundStep, 0, len(d.Steps))
	for i, s := range d.Steps {
		step := boundStep{Step: i}
		if s.Expand != nil && s.Expand.Formula != nil {
			h := cloneExpandTemplate(s.Expand)
			target, source := operandValue(h.Target, args), operandValue(h.Formula.Source, args)
			h.Target, h.Formula.Source = AbilityOperand{Literal: &target}, AbilityOperand{Literal: &source}
			step.CapacityFormula = h
		} else if s.RollResource != nil {
			r := cloneResourceRoll(s.RollResource)
			target, caster := operandValue(r.Target, args), operandValue(r.Caster, args)
			r.Target, r.Caster = AbilityOperand{Literal: &target}, AbilityOperand{Literal: &caster}
			step.RollResource = r
		} else if s.Check != nil {
			c := *s.Check
			c.Modifier = bindCheckValue(c.Modifier, args)
			c.Threshold = bindCheckValue(c.Threshold, args)
			step.Check = &boundCheck{Template: c}
			for _, p := range c.Success {
				step.Check.Success = append(step.Check.Success, bindPrimitive(p.Step(), args))
			}
			for _, p := range c.Failure {
				step.Check.Failure = append(step.Check.Failure, bindPrimitive(p.Step(), args))
			}
		} else {
			step.Operation = bindPrimitive(s, args)
		}
		steps = append(steps, step)
	}
	if err := b.checkRollInputs(v, steps); err != nil {
		return nil, err
	}
	return steps, nil
}
func operandValue(o AbilityOperand, args map[ParameterID]AbilityValue) AbilityValue {
	if o.Literal != nil {
		return *o.Literal
	}
	return args[o.Parameter]
}
func bindPrimitive(s AbilityStep, args map[ParameterID]AbilityValue) Operation {
	value := func(o AbilityOperand) AbilityValue { return operandValue(o, args) }
	var o Operation
	if x := s.AdjustRelationship; x != nil {
		o.AdjustRelationship = &AdjustRelationship{From: Ref{CharacterKind, string(*value(x.From).Character)}, Kind: x.Kind, To: Ref{CharacterKind, string(*value(x.To).Character)}, Amount: *value(x.Amount).Integer}
	}
	if x := s.Consume; x != nil {
		o.Consume = &ConsumeItem{Item: *value(x.Item).Item, Owner: *value(x.Owner).Character, Amount: *value(x.Amount).Integer}
	}
	if x := s.RemoveCondition; x != nil {
		o.RemoveCondition = &RemoveCondition{Instance: *value(x.Instance).ConditionRef, Target: *value(x.Target).Character, Type: x.Type}
	}
	if x := s.Place; x != nil {
		o.Place = &PlaceItem{Item: *value(x.Item).Item, Handler: *value(x.Handler).Character, From: bindCustody(x.From, args), To: bindCustody(x.To, args)}
	}
	if x := s.AdvanceTime; x != nil {
		o.AdvanceTime = &AdvanceTime{Ticks: *value(x.Ticks).Integer}
	}
	if x := s.Equip; x != nil {
		o.Equip = &Equip{Item: *value(x.Item).Item, Wearer: *value(x.Wearer).Character, Mode: x.Mode}
	}
	if x := s.Unequip; x != nil {
		o.Unequip = &Unequip{Item: *value(x.Item).Item}
	}
	if h := s.Damage; h != nil {
		o.Damage = &ResourceOp{Character: *value(h.Target).Character, Resource: h.Resource, Amount: *value(h.Amount).Integer}
	}
	if h := s.Heal; h != nil {
		o.Heal = &ResourceOp{Character: *value(h.Target).Character, Resource: h.Resource, Amount: *value(h.Amount).Integer}
	}
	if h := s.Expand; h != nil {
		o.Expand = &ResourceOp{Character: *value(h.Target).Character, Resource: h.Resource, Amount: *value(h.Amount).Integer}
	}
	if h := s.Restore; h != nil {
		o.Restore = &RestoreResource{*value(h.Target).Character, h.Resource}
	}
	if s := s.Spend; s != nil {
		o.Spend = &ResourceOp{Character: *value(s.Payer).Character, Resource: s.Resource, Amount: *value(s.Amount).Integer}
	}
	if x := s.Transfer; x != nil {
		o.Transfer = &Transfer{*value(x.Item).Item, *value(x.From).Character, *value(x.To).Character}
	}
	if c := s.ApplyCondition; c != nil {
		o.ApplyCondition = &ApplyCondition{Instance: *value(c.Instance).ConditionID, Type: c.Type, Target: *value(c.Target).Character, Source: copyPtr(c.Source), Duration: copyPtr(c.Duration), ClockDuration: copyPtr(c.ClockDuration)}
		if c.Anchor != nil {
			o.ApplyCondition.Anchor = copyPtr(value(*c.Anchor).ConditionID)
		}
		if c.Magnitude != nil {
			o.ApplyCondition.Magnitude = copyPtr(value(*c.Magnitude).Integer)
		}
	}
	if s.Travel != nil {
		o.Travel = copyPtr(s.Travel)
	}
	if s.Claim != nil {
		o.Claim = &ClaimObjective{Objective: s.Claim.Objective, Recipient: *value(s.Claim.Recipient).Character}
	}
	return o
}

func (b Blueprint) abilityStepError(v InvokeAbility, step int, err error) error {
	if err == nil {
		return nil
	}
	d := abilityDefinition(&b, v.Ability)
	s := d.Steps[step]
	return templateError(v.Ability, step, nil, s, err)
}
func templateError(id AbilityID, step int, path []int, s AbilityStep, err error) error {
	field := "step"
	var operand AbilityOperand
	if x := s.AdjustRelationship; x != nil {
		field, operand = "relation_kind", AbilityOperand{}
		if f, ok := err.(*Fault); ok && f.Code == "E_AMOUNT" {
			field, operand = "amount", x.Amount
		}
	}
	if x := s.Consume; x != nil {
		field, operand = "item", x.Item
		if f, ok := err.(*Fault); ok {
			if f.Code == "E_AMOUNT" {
				field, operand = "amount", x.Amount
			}
			if f.Code == "E_OWNER" {
				field, operand = "owner", x.Owner
			}
		}
	}
	if x := s.RemoveCondition; x != nil {
		field, operand = "instance", x.Instance
		if f, ok := err.(*Fault); ok && f.Field == "target" {
			field, operand = "target", x.Target
		}
	}
	if x := s.Place; x != nil {
		field, operand = "item", x.Item
		if f, ok := err.(*Fault); ok {
			switch f.Field {
			case "from":
				field, operand = "from", x.From.Subject
			case "to":
				field, operand = "to", x.To.Subject
			case "handler":
				field, operand = "handler", x.Handler
			}
		}
	}
	if x := s.AdvanceTime; x != nil {
		field, operand = "ticks", x.Ticks
	}
	if x := s.Equip; x != nil {
		field, operand = "wearer", x.Wearer
		if f, ok := err.(*Fault); ok && f.Code == "E_WORN" {
			field, operand = "item", x.Item
		}
		if f, ok := err.(*Fault); ok && f.Code == "E_EQUIPMENT" && f.Field == "mode" {
			field, operand = "mode", AbilityOperand{}
		}
	}
	if x := s.Unequip; x != nil {
		field, operand = "item", x.Item
	}
	// Keep both static and candidate-state failures tied to the responsible
	// operand of the closed primitive forms.
	if h := s.Damage; h != nil {
		field = "target"
		operand = h.Target
		if f, ok := err.(*Fault); ok && f.Code == "E_AMOUNT" {
			field = "amount"
			operand = h.Amount
		}
	}
	if h := s.Heal; h != nil {
		field = "target"
		operand = h.Target
		if f, ok := err.(*Fault); ok && f.Code == "E_AMOUNT" {
			field = "amount"
			operand = h.Amount
		}
	}
	if h := s.Expand; h != nil {
		field, operand = "target", h.Target
		if f, ok := err.(*Fault); ok && (f.Code == "E_AMOUNT" || f.Code == "E_OVERFLOW") {
			field, operand = "amount", h.Amount
		}
	}
	if h := s.Restore; h != nil {
		field, operand = "target", h.Target
	}
	if s := s.Spend; s != nil {
		field, operand = "payer", s.Payer
		if f, ok := err.(*Fault); ok && f.Code == "E_AMOUNT" {
			field, operand = "amount", s.Amount
		}
	}
	if c := s.ApplyCondition; c != nil {
		field = "target"
		operand = c.Target
		if f, ok := err.(*Fault); ok && f.Field == "anchor" && c.Anchor != nil {
			field, operand = "anchor", *c.Anchor
		}
		if f, ok := err.(*Fault); ok && f.Code == "E_DUPLICATE" && (f.Field == "condition_instance" || f.Field == "effect") {
			field = "instance"
			operand = c.Instance
		}
	}
	if x := s.Transfer; x != nil {
		field = "from"
		operand = x.From
		if f, ok := err.(*Fault); ok && (f.Code == "E_NO_CHANGE" || f.Code == "E_RULE") {
			field = "to"
			operand = x.To
		}
	}
	if x := s.Claim; x != nil {
		field, operand = "recipient", x.Recipient
	}
	e := abilityError(id, step, field, operand.Parameter, err).(*AbilityError)
	e.Path = append([]int(nil), path...)
	return e
}
func (b Blueprint) checkInvocation(v InvokeAbility) error {
	steps, e := b.bindAbility(v)
	if e != nil {
		return e
	}
	d := abilityDefinition(&b, v.Ability)
	if v.Allowance != "" {
		if err := b.checkSelectedAllowance(v.Allowance, v.Ability, false); err != nil {
			return abilityError(v.Ability, -1, "allowance", "", err)
		}
	}
	if e := b.checkBoundPolicyStatic(*d, v); e != nil {
		return e
	}

	for i, s := range steps {
		if s.CapacityFormula != nil {
			if err := b.checkCapacityFormula(*d, i, *s.CapacityFormula); err != nil {
				return err
			}
			continue
		}
		if s.RollResource != nil {
			if err := b.checkRollTemplate(*d, i, *s.RollResource); err != nil {
				return err
			}
			continue
		}
		if s.Check == nil {
			if e := b.CheckOperationStatic(s.Operation); e != nil {
				return b.abilityStepError(v, i, e)
			}
			continue
		}

		for _, x := range []struct {
			field    string
			value    CheckValue
			original CheckValue
		}{{"modifier", s.Check.Template.Modifier, d.Steps[i].Check.Modifier}, {"threshold", s.Check.Template.Threshold, d.Steps[i].Check.Threshold}} {
			if x.value.Stat != nil {
				if e := b.CheckStat(*x.value.Stat.Character.Literal.Character, x.value.Stat.Stat); e != nil {
					return abilityError(v.Ability, i, x.field, x.original.Stat.Character.Parameter, e)
				}
			}
		}
		for bi, branch := range [][]Operation{s.Check.Success, s.Check.Failure} {
			for j, o := range branch {
				if e := b.CheckOperationStatic(o); e != nil {
					p := d.Steps[i].Check.Success
					if bi == 1 {
						p = d.Steps[i].Check.Failure
					}
					return templateError(v.Ability, i, []int{bi, j}, p[j].Step(), e)
				}
			}
		}
	}
	return nil
}
func abilityOriginStep(b *Blueprint, v *AbilityOrigin) (*AbilityStep, error) {
	if v == nil {
		return nil, nil
	}
	d := abilityDefinition(b, v.Ability)
	if d == nil || v.Step < 0 || v.Step >= len(d.Steps) {
		return nil, abilityFault("E_PROVENANCE", "origin", "Condition-application body step", "invalid")
	}
	s := d.Steps[v.Step]
	if v.Path != nil {
		if len(v.Path) != 2 || s.Check == nil || v.Path[0] < 0 || v.Path[0] > 1 || v.Path[1] < 0 {
			return nil, abilityFault("E_PROVENANCE", "origin", "valid branch path", "invalid")
		}
		branch := s.Check.Success
		if v.Path[0] == 1 {
			branch = s.Check.Failure
		}
		if v.Path[1] >= len(branch) {
			return nil, abilityFault("E_PROVENANCE", "origin", "valid branch index", "invalid")
		}
		s = branch[v.Path[1]].Step()
	}
	return &s, nil
}

func checkAbilityOrigin(b *Blueprint, v *AbilityOrigin) error {
	step, err := abilityOriginStep(b, v)
	if err != nil {
		return err
	}
	if step != nil && step.ApplyCondition == nil {
		return abilityFault("E_PROVENANCE", "origin", "Condition-application body step", "invalid")
	}
	return nil
}

// A zero-health record is caused by a resource debit, not Condition application.
// Both records resolve the same immutable body/branch path before checking kind.
func checkLifeAbilityOrigin(b *Blueprint, v *AbilityOrigin) error {
	step, err := abilityOriginStep(b, v)
	if err != nil {
		return err
	}
	if step != nil && step.Damage == nil && step.Spend == nil && (step.RollResource == nil || step.RollResource.Kind != "damage") {
		return abilityFault("E_PROVENANCE", "origin", "resource-debit body step", "invalid")
	}
	return nil
}

func cloneAbilityValue(v AbilityValue) AbilityValue {
	return AbilityValue{Character: copyPtr(v.Character), Item: copyPtr(v.Item), Integer: copyPtr(v.Integer), ConditionID: copyPtr(v.ConditionID), ConditionRef: copyPtr(v.ConditionRef)}
}
func cloneAbilityOperand(v AbilityOperand) AbilityOperand {
	if v.Literal != nil {
		x := cloneAbilityValue(*v.Literal)
		v.Literal = &x
	}
	return v
}

// CanonicalInvocation gives both text and direct typed frontends the same
// detached, named-binding identity. It sorts only; duplicates still reject.
func CanonicalInvocation(v InvokeAbility) InvokeAbility {
	v.CheckBoost = cloneCheckBoostInput(v.CheckBoost)
	rolls := make([]AbilityRollInput, 0, len(v.HumanRolls))
	for _, r := range v.HumanRolls {
		rolls = append(rolls, AbilityRollInput{Step: r.Step, Faces: append([]int64(nil), r.Faces...), Engine: r.Engine})
	}
	if len(rolls) == 0 {
		rolls = nil
	}
	v.HumanRolls = rolls
	args := make([]AbilityArgument, len(v.Arguments))
	for i, a := range v.Arguments {
		args[i] = AbilityArgument{a.Name, cloneAbilityValue(a.Value)}
	}
	sort.SliceStable(args, func(i, j int) bool { return args[i].Name < args[j].Name })
	v.Arguments = args
	return v
}
func cloneInvocation(v *InvokeAbility) *InvokeAbility {
	if v == nil {
		return nil
	}
	out := CanonicalInvocation(*v)
	return &out
}
func cloneAbilityDefinitions(ds []AbilityDefinition) []AbilityDefinition {
	if ds == nil {
		return nil
	}
	out := make([]AbilityDefinition, len(ds))
	for i, d := range ds {
		out[i] = d
		out[i].Action = cloneActionPolicy(d.Action)
		if d.Parameters != nil {
			out[i].Parameters = append([]AbilityParameter{}, d.Parameters...)
		}
		if d.Steps != nil {
			out[i].Steps = make([]AbilityStep, len(d.Steps))
		}
		for j, s := range d.Steps {
			var v AbilityStep
			if x := s.AdjustRelationship; x != nil {
				v.AdjustRelationship = &AdjustRelationshipTemplate{From: cloneAbilityOperand(x.From), Kind: x.Kind, To: cloneAbilityOperand(x.To), Amount: cloneAbilityOperand(x.Amount)}
			}
			if x := s.RemoveCondition; x != nil {
				v.RemoveCondition = &ConditionRemovalTemplate{Instance: cloneAbilityOperand(x.Instance), Target: cloneAbilityOperand(x.Target), Type: x.Type}
			}
			v.Place = clonePlaceTemplate(s.Place)
			v.RollResource = cloneResourceRoll(s.RollResource)
			if x := s.AdvanceTime; x != nil {
				v.AdvanceTime = &AdvanceTimeTemplate{Ticks: cloneAbilityOperand(x.Ticks)}
			}
			v.Travel = copyPtr(s.Travel)
			if s.Claim != nil {
				v.Claim = &ClaimTemplate{Objective: s.Claim.Objective, Recipient: cloneAbilityOperand(s.Claim.Recipient)}
			}
			v.Check = cloneCheckTemplate(s.Check)
			if x := s.Equip; x != nil {
				v.Equip = &EquipTemplate{Item: cloneAbilityOperand(x.Item), Wearer: cloneAbilityOperand(x.Wearer), Mode: x.Mode}
			}
			if x := s.Unequip; x != nil {
				v.Unequip = &UnequipTemplate{Item: cloneAbilityOperand(x.Item)}
			}
			if h := s.Damage; h != nil {
				v.Damage = &DamageTemplate{cloneAbilityOperand(h.Target), h.Resource, cloneAbilityOperand(h.Amount)}
			}
			if h := s.Heal; h != nil {
				v.Heal = &HealTemplate{cloneAbilityOperand(h.Target), h.Resource, cloneAbilityOperand(h.Amount)}
			}
			if h := s.Expand; h != nil {
				v.Expand = cloneExpandTemplate(h)
			}
			if h := s.Restore; h != nil {
				v.Restore = &RestoreTemplate{cloneAbilityOperand(h.Target), h.Resource}
			}
			if s := s.Spend; s != nil {
				v.Spend = &SpendTemplate{cloneAbilityOperand(s.Payer), s.Resource, cloneAbilityOperand(s.Amount)}
			}
			if x := s.Consume; x != nil {
				v.Consume = &ConsumeTemplate{Item: cloneAbilityOperand(x.Item), Owner: cloneAbilityOperand(x.Owner), Amount: cloneAbilityOperand(x.Amount)}
			}
			if x := s.Transfer; x != nil {
				v.Transfer = &TransferTemplate{cloneAbilityOperand(x.Item), cloneAbilityOperand(x.From), cloneAbilityOperand(x.To)}
			}
			if c := s.ApplyCondition; c != nil {
				v.ApplyCondition = &ConditionTemplate{Instance: cloneAbilityOperand(c.Instance), Type: c.Type, Target: cloneAbilityOperand(c.Target), Source: copyPtr(c.Source), Duration: copyPtr(c.Duration), ClockDuration: copyPtr(c.ClockDuration)}
				if c.Anchor != nil {
					a := cloneAbilityOperand(*c.Anchor)
					v.ApplyCondition.Anchor = &a
				}
				if c.Magnitude != nil {
					m := cloneAbilityOperand(*c.Magnitude)
					v.ApplyCondition.Magnitude = &m
				}
			}
			out[i].Steps[j] = v
		}
	}
	return out
}
