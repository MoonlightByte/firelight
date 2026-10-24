package core

import "fmt"

type boundStep struct {
	CapacityFormula *ExpandTemplate
	RollResource    *RollResourceTemplate
	Operation       Operation
	Check           *boundCheck
	Step            int
}
type boundCheck struct {
	Template         CheckTemplate
	Success, Failure []Operation
}

func bindCheckValue(v CheckValue, args map[ParameterID]AbilityValue) CheckValue {
	v = cloneCheckValue(v)
	if v.Stat != nil {
		x := operandValue(v.Stat.Character, args)
		v.Stat.Character = AbilityOperand{Literal: &x}
	}
	return v
}
func checkOperands(c CheckTemplate) ([]templateOperand, error) {
	out := []templateOperand{}
	for _, v := range []struct {
		name  string
		value CheckValue
	}{{"modifier", c.Modifier}, {"threshold", c.Threshold}} {
		if (v.value.Literal != nil) == (v.value.Stat != nil) {
			return nil, abilityFault("E_IR", v.name, "one literal or stat", "invalid sum")
		}
		if v.value.Stat != nil {
			out = append(out, templateOperand{v.name, v.value.Stat.Character, CharacterArgument})
		}
	}
	return out, nil
}
func (b Blueprint) checkCheckTemplate(d AbilityDefinition, index int, c CheckTemplate) error {
	fault := func(field string, e error) error { return abilityError(d.ID, index, field, "", e) }
	if c.ActorRoll && (b.Battle == nil || d.Action == nil) {
		return fault("actor_roll", abilityFault("E_RULE", "actor_roll", "native Battle Action", "absent"))
	}
	if err := b.checkBoostPolicy(d, c); err != nil {
		return fault("boost", err)
	}
	if !validID(c.ID) {
		return fault("check", abilityFault("E_ID", "check", "valid check ID", c.ID))
	}
	if c.Sides < 2 || c.Sides > 1000 {
		return fault("sides", abilityFault("E_BOUNDS", "sides", "2..1000", fmt.Sprint(c.Sides)))
	}
	for _, v := range []struct {
		name  string
		value CheckValue
	}{{"modifier", c.Modifier}, {"threshold", c.Threshold}} {
		if x := v.value.Stat; x != nil {
			if !validID(string(x.Stat)) {
				return fault(v.name, abilityFault("E_ID", "stat", "valid Stat ID", string(x.Stat)))
			}
			if x.Character.Literal != nil {
				if e := b.CheckStat(*x.Character.Literal.Character, x.Stat); e != nil {
					return fault(v.name, e)
				}
			}
		}
	}
	// Complete authored work is bounded before binding or candidate evaluation.
	extra := 0
	if c.Boost != nil {
		extra = 2
	} // bonus draw/decision and optional exact payment
	if len(d.Steps)+len(c.Success)+len(c.Failure)+extra > MaxOperations {
		return fault("check", abilityFault("E_LIMIT", "steps", "at most 128 total authored steps", "too many"))
	}
	for bi, branch := range [][]AbilityPrimitive{c.Success, c.Failure} {
		if len(branch) == 0 {
			continue
		}
		sub := d
		sub.Action = nil
		sub.Steps = make([]AbilityStep, len(branch))
		for i, p := range branch {
			if p.Expand != nil && p.Expand.Formula != nil {
				err := abilityError(d.ID, index, "formula", "", abilityFault("E_LIMIT", "formula", "top-level capacity formula", "Check branch")).(*AbilityError)
				err.Path = []int{bi, i}
				return err
			}
			sub.Steps[i] = p.Step()
		}
		if e := b.CheckAbilityDefinition(sub); e != nil {
			if a, ok := e.(*AbilityError); ok {
				a.Path = []int{bi, a.Step}
				a.Step = index
			}
			return e
		}
	}
	return nil
}
func cloneCheckTemplate(c *CheckTemplate) *CheckTemplate {
	if c == nil {
		return nil
	}
	out := *c
	out.Boost = copyPtr(c.Boost)
	out.Modifier = cloneCheckValue(c.Modifier)
	out.Threshold = cloneCheckValue(c.Threshold)
	clone := func(ps []AbilityPrimitive) []AbilityPrimitive {
		if ps == nil {
			return nil
		}
		d := AbilityDefinition{Steps: make([]AbilityStep, len(ps))}
		for i, p := range ps {
			d.Steps[i] = p.Step()
		}
		x := cloneAbilityDefinitions([]AbilityDefinition{d})[0]
		out := make([]AbilityPrimitive, len(ps))
		for i, s := range x.Steps {
			out[i] = s.Primitive()
		}
		return out
	}
	out.Success = clone(c.Success)
	out.Failure = clone(c.Failure)
	return &out
}
func (b Blueprint) checkActionPolicy(d AbilityDefinition, params map[ParameterID]ArgumentKind) error {
	if d.Action == nil {
		return nil
	}
	a := d.Action
	fault := func(field string, o AbilityOperand, e error) error {
		return abilityError(d.ID, -1, field, o.Parameter, e)
	}
	operand := func(field string, o AbilityOperand, k ArgumentKind) error {
		if (o.Parameter != "") == (o.Literal != nil) {
			return fault(field, o, abilityFault("E_IR", field, "one parameter or literal", "invalid sum"))
		}
		if o.Parameter != "" {
			actual, ok := params[o.Parameter]
			if !ok {
				return fault(field, o, abilityFault("E_PARAMETER", field, "declared parameter", string(o.Parameter)))
			}
			if actual != k {
				return fault(field, o, abilityFault("E_TYPE", field, string(k), string(actual)))
			}
			return nil
		}
		return fault(field, o, b.checkAbilityValue(*o.Literal, k))
	}
	if e := operand("actor", a.Actor, CharacterArgument); e != nil {
		return e
	}
	if a.Phase != Exploration && a.Phase != EncounterPhase && a.Phase != EitherPhase {
		return fault("phase", AbilityOperand{}, abilityFault("E_RULE", "phase", "exploration, encounter or either", string(a.Phase)))
	}
	if a.Finish != nil && (b.Battle == nil || a.TurnCost != nil) {
		return fault("finish", AbilityOperand{}, abilityFault("E_RULE", "finish", "ordinary Battle action without turn_cost", "incompatible policy"))
	}
	if c := a.TurnCost; c != nil {
		declared := false
		if b.Battle != nil {
			for _, id := range b.Battle.Rules.TurnResources {
				declared = declared || id == c.Resource
			}
		}
		if !declared || !validID(string(c.Resource)) {
			return fault("turn_cost.resource", AbilityOperand{}, abilityFault("E_RULE", "turn_cost.resource", "declared Battle turn Resource", string(c.Resource)))
		}
		if c.Amount <= 0 {
			return fault("turn_cost.amount", AbilityOperand{}, abilityFault("E_AMOUNT", "turn_cost.amount", "positive exact turn cost", fmt.Sprint(c.Amount)))
		}
	}
	if len(a.Requirements) > 16 {
		return fault("requirements", AbilityOperand{}, abilityFault("E_LIMIT", "requirements", "at most 16", "too many"))
	}
	for i, r := range a.Requirements {
		prefix := fmt.Sprintf("requirement.%d.", i)
		kind := CharacterArgument
		if r.Kind == PartyOwned || r.Kind == CarriedItem {
			kind = ItemArgument
		}
		if e := operand(prefix+"subject", r.Subject, kind); e != nil {
			return e
		}
		pair := r.Kind == CarriedItem || r.Kind == Colocated || r.Kind == Allied || r.Kind == Hostile || r.Kind == Distinct || r.Kind == WithinRange
		resource := r.Kind == AboveMinimum
		if resource && !validID(string(r.Resource)) {
			return fault(prefix+"resource", r.Subject, abilityFault("E_ID", "resource", "valid resource ID", string(r.Resource)))
		}
		condition := r.Kind == HasCondition || r.Kind == LacksCondition || r.Kind == WornTraining
		if !pair && !resource && !condition && r.Kind != PartyOwned && r.Kind != FreeSlot && r.Kind != AtLocation && r.Kind != StatBetween {
			return fault(prefix+"kind", r.Subject, abilityFault("E_RULE", "requirement", "supported predicate", string(r.Kind)))
		}
		if (r.Other != nil) != pair || (r.Resource != "") != resource || (r.Condition != "") != condition || (r.Distance != nil) != (r.Kind == WithinRange) || (r.Slot != nil) != (r.Kind == FreeSlot) || (r.Worn != nil) != (r.Kind == WornTraining) || (r.Location != "") != (r.Kind == AtLocation) || (r.StatRange != nil) != (r.Kind == StatBetween) {
			return fault(prefix+"kind", r.Subject, abilityFault("E_IR", "requirement", "exact fields for predicate", "invalid shape"))
		}
		if bounds := r.StatRange; bounds != nil {
			if !validID(string(bounds.Stat)) {
				return fault(prefix+"stat", r.Subject, abilityFault("E_ID", "stat", "valid stat ID", string(bounds.Stat)))
			}
			if bounds.Minimum > bounds.Maximum {
				return fault(prefix+"maximum", r.Subject, abilityFault("E_RANGE", "maximum", "at least minimum", fmt.Sprint(bounds.Maximum)))
			}
			if r.Subject.Literal != nil {
				if err := b.CheckStat(*r.Subject.Literal.Character, bounds.Stat); err != nil {
					return fault(prefix+"stat", r.Subject, err)
				}
			}
		}
		if r.Kind == AtLocation {
			if err := b.CheckRef(Ref{LocationKind, string(r.Location)}); err != nil {
				return fault(prefix+"location", r.Subject, err)
			}
		}
		if r.Worn != nil {
			definition := b.equipmentDefinition(r.Worn.Definition)
			if definition == nil {
				return fault(prefix+"definition", r.Subject, abilityFault("E_EQUIPMENT", "definition", "declared equipment definition", string(r.Worn.Definition)))
			}
			if equipmentMode(definition, r.Worn.Mode) == nil {
				return fault(prefix+"mode", r.Subject, abilityFault("E_EQUIPMENT", "mode", "declared equipment mode", string(r.Worn.Mode)))
			}
		}
		if r.Slot != nil {
			found := false
			if b.Equipment != nil {
				for _, slot := range b.Equipment.Slots {
					found = found || slot.ID == r.Slot.Slot
				}
			}
			if !found {
				return fault(prefix+"slot", r.Subject, abilityFault("E_SLOT", "slot", "declared equipment slot", string(r.Slot.Slot)))
			}
			if r.Slot.Amount <= 0 {
				return fault(prefix+"count", r.Subject, abilityFault("E_AMOUNT", "count", "positive free capacity", fmt.Sprint(r.Slot.Amount)))
			}
		}
		if r.Distance != nil {
			if r.Distance.Maximum < 0 {
				return fault(prefix+"distance", r.Subject, abilityFault("E_RANGE", "distance", "nonnegative distance", fmt.Sprint(r.Distance.Maximum)))
			}
			if b.Battle == nil || r.Distance.Unit != b.Battle.Rules.GridUnit {
				return fault(prefix+"unit", r.Subject, abilityFault("E_RANGE", "unit", "authored Battle grid unit", r.Distance.Unit))
			}
		}
		if pair {
			if e := operand(prefix+"other", *r.Other, CharacterArgument); e != nil {
				return e
			}
		}
		if resource && r.Subject.Literal != nil {
			if e := b.CheckResource(*r.Subject.Literal.Character, r.Resource); e != nil {
				return fault(prefix+"resource", r.Subject, e)
			}
		}
		if condition && conditionDefinition(&b, r.Condition) == nil {
			return fault(prefix+"condition", r.Subject, abilityFault("E_CONDITION_TYPE", "condition", "declared type", string(r.Condition)))
		}
	}
	return nil
}

func (b Blueprint) checkBoundPolicyStatic(d AbilityDefinition, v InvokeAbility) error {
	if d.Action == nil {
		return nil
	}
	args := map[ParameterID]AbilityValue{}
	for _, a := range v.Arguments {
		args[a.Name] = a.Value
	}
	for i, r := range d.Action.Requirements {
		if r.Kind == StatBetween {
			if err := b.CheckStat(*operandValue(r.Subject, args).Character, r.StatRange.Stat); err != nil {
				return abilityError(d.ID, -1, fmt.Sprintf("requirement.%d.stat", i), r.Subject.Parameter, err)
			}
		}
		if r.Kind == AboveMinimum {
			if e := b.CheckResource(*operandValue(r.Subject, args).Character, r.Resource); e != nil {
				return abilityError(d.ID, -1, fmt.Sprintf("requirement.%d.resource", i), r.Subject.Parameter, e)
			}
		}
	}
	return nil
}
