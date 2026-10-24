package core

import "fmt"

// CapacityFormula is an immutable, bounded recipe for the requested growth.
// Binding resolves its references; evaluation reads the ordered candidate.
type CapacityFormula struct {
	Source  AbilityOperand `json:"source"`
	Recipe  ScalarRecipe   `json:"recipe"`
	Minimum int64          `json:"minimum"`
}

type CapacityFormulaResult struct {
	Source  CharacterID       `json:"source"`
	Raw     int64             `json:"raw"`
	Minimum int64             `json:"minimum"`
	Recipe  RecipeExplanation `json:"recipe"`
}

func cloneExpandTemplate(v *ExpandTemplate) *ExpandTemplate {
	if v == nil {
		return nil
	}
	o := *v
	o.Target, o.Amount = cloneAbilityOperand(v.Target), cloneAbilityOperand(v.Amount)
	if v.Formula != nil {
		x := *v.Formula
		x.Source, x.Recipe = cloneAbilityOperand(x.Source), cloneRecipe(x.Recipe)
		o.Formula = &x
	}
	return &o
}

func cloneCapacityChange(v *CapacityChange) *CapacityChange {
	o := copyPtr(v)
	if o != nil && v.Formula != nil {
		o.Formula = copyPtr(v.Formula)
		o.Formula.Recipe = cloneRecipeExplanation(v.Formula.Recipe)
	}
	return o
}

func (b Blueprint) checkCapacityFormula(d AbilityDefinition, step int, v ExpandTemplate) error {
	f := v.Formula
	original := d.Steps[step].Expand
	fault := func(field string, err error) error {
		parameter := ParameterID("")
		if f, ok := err.(*Fault); ok && f.Code == "E_STAT" {
			parameter = original.Formula.Source.Parameter
		}
		if field == "target" || field == "resource" {
			parameter = original.Target.Parameter
		}
		return abilityError(d.ID, step, field, parameter, err)
	}
	if f.Minimum <= 0 {
		return fault("formula_minimum", abilityFault("E_AMOUNT", "minimum", "positive int64 floor", fmt.Sprint(f.Minimum)))
	}
	if len(f.Recipe.Terms) > 8 {
		return fault("formula", abilityFault("E_LIMIT", "terms", "at most 8 recipe terms", "too many"))
	}
	for i, term := range f.Recipe.Terms {
		field := fmt.Sprintf("formula.term.%d", i)
		if err := b.checkEquipmentTerms(string(d.ID), []StatTerm{term}); err != nil {
			if f, ok := err.(*Fault); ok && (f.Field == "requires" || f.Field == "unless" || f.Field == "unless_slots") {
				field += "." + f.Field
			}
			return fault(field, err)
		}
		if f.Source.Literal != nil {
			if err := b.CheckStat(*f.Source.Literal.Character, term.Stat); err != nil {
				return fault(field, err)
			}
		}
	}
	if v.Target.Literal != nil {
		if err := b.CheckResource(*v.Target.Literal.Character, v.Resource); err != nil {
			return fault("resource", err)
		}
	}
	return nil
}

// Runtime and read-only input simulation call this same resolver. It performs
// no random draw and feeds the existing maximum-only Resource transition.
func (b *Blueprint) applyCapacityFormula(v ExpandTemplate) (Event, error) {
	f := v.Formula
	source := *f.Source.Literal.Character
	raw, explanation, err := evaluateRecipe(b, source, f.Recipe)
	if err != nil {
		return Event{}, err
	}
	amount := raw
	if amount < f.Minimum {
		amount = f.Minimum
	}
	event, err := expandResource(b, ResourceOp{Character: *v.Target.Literal.Character, Resource: v.Resource, Amount: amount})
	if err != nil {
		return Event{}, err
	}
	event.Capacity.Formula = &CapacityFormulaResult{Source: source, Raw: raw, Minimum: f.Minimum, Recipe: explanation}
	return event, nil
}
