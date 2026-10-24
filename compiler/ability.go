package compiler

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"strings"
)

type abilityOperandNode struct {
	Parameter bool
	Value     token
}
type abilityParameterNode struct{ ID, Kind token }
type capacityFormulaNode struct {
	Start, Minimum token
	Source         abilityOperandNode
	Recipe         recipeNode
}
type abilityStepNode struct {
	Formula                                         *capacityFormulaNode
	Place                                           *placeTemplateNode
	Roll                                            *rollResourceNode
	Mode                                            *token
	Verb                                            token
	Target, Payer, Amount, Instance, Item, From, To abilityOperandNode
	Resource, Type                                  token
	Source                                          *refNode
	Magnitude                                       *abilityOperandNode
	Anchor                                          *abilityOperandNode
	Duration                                        *token
	ClockDuration                                   *token
	Check                                           *checkNode
	Destination, Objective                          token
}
type abilityNode struct {
	ID, Name, End token
	Parameters    []abilityParameterNode
	Steps         []abilityStepNode
	Action        *actionPolicyNode
	Requirements  []requirementNode
}
type argumentNode struct{ Name, Kind, Value token }
type invocationNode struct {
	Boost     *checkBoostInputNode
	Allowance *token
	Rolls     []abilityRollNode
	ID, End   token
	Arguments []argumentNode
}
type checkBoostInputNode struct {
	Start, Step, Use token
	Face             *token
}

func (p *parser) abilityOperand(numeric bool) abilityOperandNode {
	if p.peek().Kind == word && p.peek().Text == "param" {
		p.take()
		return abilityOperandNode{true, p.str()}
	}
	if numeric {
		return abilityOperandNode{Value: p.num()}
	}
	return abilityOperandNode{Value: p.str()}
}
func (p *parser) ability() abilityNode {
	n := abilityNode{ID: p.str()}
	p.expect("named")
	n.Name = p.str()
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		verb := p.abilityVerb(true)
		if verb.Text == "parameter" {
			n.Parameters = append(n.Parameters, abilityParameterNode{p.str(), p.choice("character", "item", "int", "condition_id", "condition_ref")})
			p.expect(";")
			continue
		}
		if verb.Text == "action" {
			if n.Action != nil {
				p.err = diag(verb.Pos, "E_DUPLICATE", "one action policy", verb.Text, "duplicate action policy")
				break
			}
			p.expect("actor")
			action := actionPolicyNode{Verb: verb, Actor: p.abilityOperand(false)}
			p.expect("phase")
			action.Phase = p.choice("exploration", "encounter", "either")
			if p.isWord("turn_cost") {
				p.take()
				p.expect("resource")
				c := turnCostNode{Resource: p.str()}
				p.expect("by")
				c.Amount = p.num()
				p.expect("finish")
				c.Finish = p.choice("true", "false")
				action.TurnCost = &c
			} else if p.isWord("finish") {
				p.take()
				finish := p.choice("true", "false")
				action.Finish = &finish
			}
			n.Action = &action
			p.expect(";")
			continue
		}
		if verb.Text == "require" {
			n.Requirements = append(n.Requirements, p.requirement(verb))
			p.expect(";")
			continue
		}
		n.Steps = append(n.Steps, p.abilityStep(verb))
	}
	n.End = p.expect("}")
	return n
}
func (p *parser) abilityStep(verb token) abilityStepNode {
	s := abilityStepNode{Verb: verb}
	switch verb.Text {
	case "adjust":
		p.expect("relation")
		p.expect("character")
		s.From = p.abilityOperand(false)
		s.Type = p.str()
		p.expect("character")
		s.To = p.abilityOperand(false)
		p.expect("by")
		s.Amount = p.abilityOperand(true)
	case "consume":
		s.Item = p.abilityOperand(false)
		p.expect("from")
		s.From = p.abilityOperand(false)
		p.expect("by")
		s.Amount = p.abilityOperand(true)
	case "remove":
		p.expect("condition")
		s.Instance = p.abilityOperand(false)
		p.expect("of")
		s.Type = p.str()
		p.expect("from")
		s.Target = p.abilityOperand(false)
	case "place":
		s.Place = p.placeTemplate()
	case "roll":
		r := p.rollResource()
		s.Roll = &r
	case "advance":
		p.expect("time")
		p.expect("by")
		s.Amount = p.abilityOperand(true)
	case "check":
		c := p.check()
		s.Check = &c
		return s
	case "travel":
		p.expect("party")
		p.expect("to")
		s.Destination = p.str()
	case "claim":
		p.expect("objective")
		s.Objective = p.str()
		p.expect("to")
		s.To = p.abilityOperand(false)
	case "equip":
		s.Item = p.abilityOperand(false)
		p.expect("on")
		s.To = p.abilityOperand(false)
		if p.isWord("mode") {
			p.take()
			v := p.str()
			s.Mode = &v
		}
	case "unequip":
		s.Item = p.abilityOperand(false)
	case "spend":
		s.Payer = p.abilityOperand(false)
		p.expect("resource")
		s.Resource = p.str()
		p.expect("by")
		s.Amount = p.abilityOperand(true)
	case "restore":
		s.Target = p.abilityOperand(false)
		p.expect("resource")
		s.Resource = p.str()
	case "heal", "damage", "expand":
		s.Target = p.abilityOperand(false)
		p.expect("resource")
		s.Resource = p.str()
		p.expect("by")
		if verb.Text == "expand" && p.isWord("formula") {
			f := &capacityFormulaNode{Start: p.take()}
			p.expect("from")
			f.Source = p.abilityOperand(false)
			p.expect("min")
			f.Minimum = p.num()
			f.Recipe = p.recipe()
			s.Formula = f
		} else {
			s.Amount = p.abilityOperand(true)
		}
	case "transfer":
		s.Item = p.abilityOperand(false)
		p.expect("from")
		s.From = p.abilityOperand(false)
		p.expect("to")
		s.To = p.abilityOperand(false)
	case "apply":
		p.expect("condition")
		s.Instance = p.abilityOperand(false)
		p.expect("of")
		s.Type = p.str()
		p.expect("to")
		s.Target = p.abilityOperand(false)
		for p.peek().Text != ";" && p.err == nil {
			key := p.peek()
			if p.isWord("anchor") {
				p.take()
				if s.Anchor != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one anchor", key.Text, "duplicate Condition anchor")
					break
				}
				v := p.abilityOperand(false)
				s.Anchor = &v
				continue
			}
			if p.isWord("ticks") {
				p.take()
				if s.ClockDuration != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one clock duration", key.Text, "duplicate condition clock duration")
					break
				}
				v := p.num()
				s.ClockDuration = &v
				continue
			}
			if p.isWord("duration") {
				p.take()
				if s.Duration != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one duration", key.Text, "duplicate condition duration")
					break
				}
				duration := p.num()
				s.Duration = &duration
				continue
			}
			key = p.choice("from", "magnitude")
			if key.Text == "from" {
				if s.Source != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one source", key.Text, "duplicate condition source")
					break
				}
				r := p.ref()
				s.Source = &r
			} else if key.Text == "magnitude" {
				if s.Magnitude != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one magnitude", key.Text, "duplicate condition magnitude")
					break
				}
				m := p.abilityOperand(true)
				s.Magnitude = &m
			}
		}
	}
	p.expect(";")
	return s
}
func (p *parser) invocation() invocationNode {
	n := invocationNode{ID: p.str()}
	if p.isWord("allowance") {
		p.take()
		id := p.str()
		n.Allowance = &id
	}
	p.expect("{")
	for (p.peek().Kind != punctuation || p.peek().Text != "}") && p.err == nil {
		if p.isWord("boost") {
			start := p.take()
			if n.Boost != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one check boost choice", "boost", "duplicate boost choice")
				break
			}
			v := checkBoostInputNode{Start: start, Step: p.num(), Use: p.choice("use", "decline")}
			if p.isWord("face") {
				p.take()
				f := p.num()
				v.Face = &f
			}
			p.expect(";")
			n.Boost = &v
			continue
		}
		if p.isWord("roll") {
			p.take()
			r := abilityRollNode{Step: p.num()}
			if p.isWord("engine") {
				engine := p.take()
				r.Engine = &engine
				p.expect(";")
				n.Rolls = append(n.Rolls, r)
				continue
			}
			p.expect("{")
			for p.peek().Text != "}" && p.err == nil {
				p.expect("face")
				r.Faces = append(r.Faces, p.num())
				p.expect(";")
			}
			p.expect("}")
			p.expect(";")
			n.Rolls = append(n.Rolls, r)
			continue
		}
		a := argumentNode{Name: p.str()}
		p.expect("=")
		a.Kind = p.choice("character", "item", "int", "condition_id", "condition_ref")
		if a.Kind.Text == "int" {
			a.Value = p.num()
		} else {
			a.Value = p.str()
		}
		p.expect(";")
		n.Arguments = append(n.Arguments, a)
	}
	n.End = p.expect("}")
	return n
}
func (a *analyzer) abilityValue(t token, k core.ArgumentKind) core.AbilityValue {
	v := core.AbilityValue{}
	switch k {
	case core.CharacterArgument:
		x := core.CharacterID(t.Text)
		v.Character = &x
	case core.ItemArgument:
		x := core.ItemID(t.Text)
		v.Item = &x
	case core.IntegerArgument:
		x := a.number(t)
		v.Integer = &x
	case core.ConditionRefArgument:
		x := core.ConditionInstanceID(t.Text)
		v.ConditionRef = &x
	case core.ConditionIDArgument:
		x := core.ConditionInstanceID(t.Text)
		v.ConditionID = &x
	}
	return v
}
func (a *analyzer) operand(n abilityOperandNode, k core.ArgumentKind) core.AbilityOperand {
	if n.Parameter {
		return core.AbilityOperand{Parameter: core.ParameterID(n.Value.Text)}
	}
	v := a.abilityValue(n.Value, k)
	return core.AbilityOperand{Literal: &v}
}
func (a *analyzer) abilityDefinition(n abilityNode) core.AbilityDefinition {
	d := core.AbilityDefinition{ID: core.AbilityID(n.ID.Text), Name: n.Name.Text}
	for _, p := range n.Parameters {
		d.Parameters = append(d.Parameters, core.AbilityParameter{ID: core.ParameterID(p.ID.Text), Kind: core.ArgumentKind(p.Kind.Text)})
	}
	d.Action = a.actionPolicy(n)
	for _, s := range n.Steps {
		d.Steps = append(d.Steps, a.abilityStep(s))
	}
	return d
}
func (a *analyzer) abilityStep(s abilityStepNode) core.AbilityStep {
	var out core.AbilityStep
	switch s.Verb.Text {
	case "adjust":
		out.AdjustRelationship = &core.AdjustRelationshipTemplate{From: a.operand(s.From, core.CharacterArgument), Kind: core.RelationKind(s.Type.Text), To: a.operand(s.To, core.CharacterArgument), Amount: a.operand(s.Amount, core.IntegerArgument)}
	case "consume":
		out.Consume = &core.ConsumeTemplate{Item: a.operand(s.Item, core.ItemArgument), Owner: a.operand(s.From, core.CharacterArgument), Amount: a.operand(s.Amount, core.IntegerArgument)}
	case "remove":
		out.RemoveCondition = &core.ConditionRemovalTemplate{Instance: a.operand(s.Instance, core.ConditionRefArgument), Type: core.ConditionTypeID(s.Type.Text), Target: a.operand(s.Target, core.CharacterArgument)}
	case "place":
		out.Place = a.placeTemplate(s.Place)
	case "roll":
		r := s.Roll
		out.RollResource = &core.RollResourceTemplate{Kind: r.Kind.Text, Target: a.operand(r.Target, core.CharacterArgument), Resource: core.ResourceID(r.Resource.Text), Count: a.number(r.Count), Sides: a.number(r.Sides), Caster: a.operand(r.Caster, core.CharacterArgument), Bonus: a.recipe(r.Bonus)}
		if r.Minimum != nil {
			out.RollResource.Minimum = a.number(*r.Minimum)
			if r.Kind.Text == "heal" && out.RollResource.Minimum <= 0 && a.err == nil {
				a.err = diag(r.Minimum.Pos, "E_ROLL", "positive heal floor", r.Minimum.Text, "omit min for no heal floor")
			}
		}
	case "advance":
		out.AdvanceTime = &core.AdvanceTimeTemplate{Ticks: a.operand(s.Amount, core.IntegerArgument)}
	case "check":
		out.Check = a.checkTemplate(*s.Check)
	case "travel":
		out.Travel = &core.PartyTravel{To: core.LocationID(s.Destination.Text)}
	case "claim":
		out.Claim = &core.ClaimTemplate{Objective: core.ObjectiveID(s.Objective.Text), Recipient: a.operand(s.To, core.CharacterArgument)}
	case "equip":
		out.Equip = &core.EquipTemplate{Item: a.operand(s.Item, core.ItemArgument), Wearer: a.operand(s.To, core.CharacterArgument)}
		if s.Mode != nil {
			a.requireEquipmentID(*s.Mode)
			out.Equip.Mode = core.EquipmentModeID(s.Mode.Text)
		}
	case "unequip":
		out.Unequip = &core.UnequipTemplate{Item: a.operand(s.Item, core.ItemArgument)}
	case "spend":
		out.Spend = &core.SpendTemplate{Payer: a.operand(s.Payer, core.CharacterArgument), Resource: core.ResourceID(s.Resource.Text), Amount: a.operand(s.Amount, core.IntegerArgument)}
	case "damage":
		out.Damage = &core.DamageTemplate{Target: a.operand(s.Target, core.CharacterArgument), Resource: core.ResourceID(s.Resource.Text), Amount: a.operand(s.Amount, core.IntegerArgument)}
	case "expand":
		out.Expand = &core.ExpandTemplate{Target: a.operand(s.Target, core.CharacterArgument), Resource: core.ResourceID(s.Resource.Text)}
		if s.Formula != nil {
			f := s.Formula
			out.Expand.Formula = &core.CapacityFormula{Source: a.operand(f.Source, core.CharacterArgument), Minimum: a.number(f.Minimum), Recipe: a.recipe(f.Recipe)}
		} else {
			out.Expand.Amount = a.operand(s.Amount, core.IntegerArgument)
		}
	case "restore":
		out.Restore = &core.RestoreTemplate{Target: a.operand(s.Target, core.CharacterArgument), Resource: core.ResourceID(s.Resource.Text)}
	case "heal":
		out.Heal = &core.HealTemplate{Target: a.operand(s.Target, core.CharacterArgument), Resource: core.ResourceID(s.Resource.Text), Amount: a.operand(s.Amount, core.IntegerArgument)}
	case "transfer":
		out.Transfer = &core.TransferTemplate{Item: a.operand(s.Item, core.ItemArgument), From: a.operand(s.From, core.CharacterArgument), To: a.operand(s.To, core.CharacterArgument)}
	case "apply":
		out.ApplyCondition = &core.ConditionTemplate{Instance: a.operand(s.Instance, core.ConditionIDArgument), Type: core.ConditionTypeID(s.Type.Text), Target: a.operand(s.Target, core.CharacterArgument)}
		if s.Source != nil {
			x := ref(*s.Source)
			out.ApplyCondition.Source = &x
		}
		if s.Anchor != nil {
			x := a.operand(*s.Anchor, core.ConditionIDArgument)
			out.ApplyCondition.Anchor = &x
		}
		if s.Magnitude != nil {
			x := a.operand(*s.Magnitude, core.IntegerArgument)
			out.ApplyCondition.Magnitude = &x
		}
		if s.Duration != nil {
			x := a.unsignedLiteral(*s.Duration, true)
			out.ApplyCondition.Duration = &x
		}
		if s.ClockDuration != nil {
			x := a.unsignedLiteral(*s.ClockDuration, true)
			out.ApplyCondition.ClockDuration = &x
		}
	}
	return out
}
func (s abilityStepNode) operandToken(field string) token {
	if s.Formula != nil {
		switch field {
		case "formula":
			return s.Formula.Start
		case "formula_source":
			return s.Formula.Source.Value
		case "formula_minimum":
			return s.Formula.Minimum
		}
		var index int
		if _, err := fmt.Sscanf(field, "formula.term.%d", &index); err == nil && index >= 0 && index < len(s.Formula.Recipe.Terms) {
			term := s.Formula.Recipe.Terms[index]
			if strings.HasSuffix(field, ".requires") && term.Requires != nil {
				return *term.Requires
			}
			if strings.HasSuffix(field, ".unless") && term.Unless != nil {
				return *term.Unless
			}
			if strings.HasSuffix(field, ".unless_slots") && len(term.UnlessSlots) > 0 {
				return term.UnlessSlots[0]
			}
			return term.Stat
		}
	}
	if s.Place != nil {
		return s.Place.operandToken(field)
	}
	if s.Roll != nil {
		switch field {
		case "kind":
			return s.Roll.Kind
		case "target":
			return s.Roll.Target.Value
		case "resource":
			return s.Roll.Resource
		case "count":
			return s.Roll.Count
		case "sides":
			return s.Roll.Sides
		case "minimum":
			if s.Roll.Minimum != nil {
				return *s.Roll.Minimum
			}
		case "caster":
			return s.Roll.Caster.Value
		case "bonus":
			return s.Roll.Bonus.Base
		}
	}
	if s.Check != nil {
		return s.Check.operandToken(field)
	}
	switch field {
	case "ticks":
		return s.Amount.Value
	case "clock_duration":
		if s.ClockDuration != nil {
			return *s.ClockDuration
		}
	case "duration":
		if s.Duration != nil {
			return *s.Duration
		}
	case "destination":
		return s.Destination
	case "objective":
		return s.Objective
	case "recipient":
		return s.To.Value
	case "payer":
		return s.Payer.Value
	case "target":
		return s.Target.Value
	case "amount":
		return s.Amount.Value
	case "anchor":
		if s.Anchor != nil {
			return s.Anchor.Value
		}
	case "instance":
		return s.Instance.Value
	case "item":
		return s.Item.Value
	case "from", "owner":
		return s.From.Value
	case "to":
		return s.To.Value
	case "wearer":
		return s.To.Value
	case "mode":
		if s.Mode != nil {
			return *s.Mode
		}
		return s.Item.Value
	case "resource":
		return s.Resource
	case "type", "relation_kind":
		return s.Type
	case "source":
		if s.Source != nil {
			return s.Source.ID
		}
	case "magnitude":
		if s.Magnitude != nil {
			return s.Magnitude.Value
		}
	}
	return s.Verb
}
func (a *analyzer) checkAbilityAt(err error, t token) {
	if err == nil || a.err != nil {
		return
	}
	a.checkAt(err, t)
	var context *core.AbilityError
	if errors.As(err, &context) {
		a.err.Message += fmt.Sprintf("; ability %q step %d operand %q parameter %q", context.Definition, context.Step, context.Operand, context.Parameter)
		if len(context.Path) != 0 {
			a.err.Message += fmt.Sprintf(" path %v", context.Path)
		}
	}
}
func (a *analyzer) abilityDeclarations(b *core.Blueprint, nodes []abilityNode) {
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.ID.Text] && a.err == nil {
			a.err = diag(n.ID.Pos, "E_DUPLICATE", "unique Ability ID", n.ID.Text, "duplicate Ability identity")
		}
		seen[n.ID.Text] = true
		b.AbilityDefinitions = append(b.AbilityDefinitions, a.abilityDefinition(n))
	}
	for i, n := range nodes {
		err := b.CheckAbilityDefinition(b.AbilityDefinitions[i])
		position := n.ID
		var context *core.AbilityError
		var fault *core.Fault
		if errors.As(err, &context) && errors.As(err, &fault) {
			if context.Step >= 0 && context.Step < len(n.Steps) {
				step := n.Steps[context.Step]
				if step.Check != nil && len(context.Path) == 2 {
					branch := step.Check.Success
					if context.Path[0] == 1 {
						branch = step.Check.Failure
					}
					if (context.Path[0] == 0 || context.Path[0] == 1) && context.Path[1] >= 0 && context.Path[1] < len(branch) {
						step = branch[context.Path[1]]
					}
				}
				position = step.operandToken(context.Operand)
			} else {
				if t, ok := n.actionOperandToken(context.Operand); ok {
					position = t
				}
				if context.Operand == "steps" {
					position = n.End
				}
				seenParameters := map[string]bool{}
				for _, p := range n.Parameters {
					if context.Operand != "parameter" && context.Operand != "parameter_kind" {
						break
					}
					if p.ID.Text == string(context.Parameter) && (fault.Code != "E_DUPLICATE" || seenParameters[p.ID.Text]) {
						position = p.ID
						if context.Operand == "parameter_kind" {
							position = p.Kind
						}
						break
					}
					seenParameters[p.ID.Text] = true
				}
			}
		}
		a.checkAbilityAt(err, position)
	}
}
func (a *analyzer) invocation(b core.Blueprint, n invocationNode) core.Operation {
	v := core.InvokeAbility{Ability: core.AbilityID(n.ID.Text)}
	if input := n.Boost; input != nil {
		step := a.number(input.Step)
		if step < 0 || step >= core.MaxOperations {
			a.checkAbilityAt(&core.Fault{Code: "E_ROLL", Expected: "body index0..127", Actual: input.Step.Text, Message: "invalid boost body index"}, input.Step)
		}
		v.CheckBoost = &core.CheckBoostInput{Step: int(step), Use: input.Use.Text == "use"}
		if input.Face != nil {
			face := a.number(*input.Face)
			v.CheckBoost.Face = &face
		}
	}
	if n.Allowance != nil {
		a.requireEquipmentID(*n.Allowance)
		v.Allowance = core.ResourceID(n.Allowance.Text)
	}
	seen := map[string]bool{}
	for _, arg := range n.Arguments {
		if seen[arg.Name.Text] && a.err == nil {
			a.err = diag(arg.Name.Pos, "E_DUPLICATE", "one argument per parameter", arg.Name.Text, "duplicate invocation binding")
		}
		seen[arg.Name.Text] = true
		v.Arguments = append(v.Arguments, core.AbilityArgument{Name: core.ParameterID(arg.Name.Text), Value: a.abilityValue(arg.Value, core.ArgumentKind(arg.Kind.Text))})
	}
	for _, r := range n.Rolls {
		step := a.number(r.Step)
		if step < 0 || step >= core.MaxOperations {
			a.checkAbilityAt(&core.Fault{Code: "E_ROLL", Expected: "body index0..127", Actual: r.Step.Text, Message: "invalid roll body index"}, r.Step)
		}
		input := core.AbilityRollInput{Step: int(step), Engine: r.Engine != nil}
		for _, f := range r.Faces {
			input.Faces = append(input.Faces, a.number(f))
		}
		v.HumanRolls = append(v.HumanRolls, input)
	}
	v = core.CanonicalInvocation(v)
	out := core.Operation{Invoke: &v}
	err := b.CheckOperationStatic(out)
	position := n.ID
	var context *core.AbilityError
	var fault *core.Fault
	if errors.As(err, &context) && errors.As(err, &fault) {
		if context.Operand == "boost" && n.Boost != nil {
			position = n.Boost.Start
		}
		if context.Operand == "allowance" && n.Allowance != nil {
			position = *n.Allowance
		}
		if context.Operand == "rolls" && len(n.Rolls) > 0 {
			position = n.Rolls[len(n.Rolls)-1].Step
			for _, r := range n.Rolls {
				if r.Step.Text == fmt.Sprint(context.Step) {
					position = r.Step
					if fault.Field == "face" {
						for _, f := range r.Faces {
							if f.Text == fault.Actual {
								position = f
								break
							}
						}
					}
				}
			}
		}
		if context.Parameter != "" {
			position = n.End
			for _, arg := range n.Arguments {
				if arg.Name.Text == string(context.Parameter) {
					position = arg.Value
					if fault.Code == "E_ARGUMENT" {
						position = arg.Name
					}
					break
				}
			}
		}
	}
	a.checkAbilityAt(err, position)
	return out
}
