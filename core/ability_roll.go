package core

import (
	"fmt"
	"math/big"
)

// RollResourceTemplate is an authored dice consequence. It feeds the existing
// positive-only Resource operation; zero is recorded without a Resource change.
// Expansion instead requires an authored positive floor and changes maximum only.
// A heal may declare a positive floor too; damage declares none.
// It is top-level only: this release admits one group and no branching Check.
type RollResourceTemplate struct {
	Kind     string         `json:"kind"`              // heal, damage or expand; not spell/class rules
	Minimum  int64          `json:"minimum,omitempty"` // required for expand, optional for heal
	Target   AbilityOperand `json:"target"`
	Resource ResourceID     `json:"resource"`
	Count    int64          `json:"count"`
	Sides    int64          `json:"sides"`
	Caster   AbilityOperand `json:"caster"`
	Bonus    ScalarRecipe   `json:"bonus"`
}

// AbilityRollInput answers one rolled step: a human's raw faces, or Engine,
// the human's choice that NQL draws the step as it would for an agent.
type AbilityRollInput struct {
	Step   int     `json:"step"`
	Faces  []int64 `json:"faces"`
	Engine bool    `json:"engine,omitempty"`
}

type ResourceRollResult struct {
	Kind        string            `json:"kind"`
	Minimum     int64             `json:"minimum,omitempty"`
	Target      CharacterID       `json:"target"`
	Resource    ResourceID        `json:"resource"`
	Source      BattleController  `json:"source"`
	Faces       []int64           `json:"faces"`
	Sides       int64             `json:"sides"`
	Caster      CharacterID       `json:"caster"`
	Bonus       int64             `json:"bonus"`
	BonusRecipe RecipeExplanation `json:"bonus_recipe"`
	Requested   int64             `json:"requested"`
	Actual      string            `json:"actual"`
	CallsBefore uint64            `json:"calls_before"`
	CallsAfter  uint64            `json:"calls_after"`
	EngineDrawn bool              `json:"engine_drawn,omitempty"`
}

const ResourceRollResolved EventKind = "ResourceRollResolved"

func cloneResourceRoll(v *RollResourceTemplate) *RollResourceTemplate {
	if v == nil {
		return nil
	}
	r := *v
	r.Target, r.Caster, r.Bonus = cloneAbilityOperand(v.Target), cloneAbilityOperand(v.Caster), cloneRecipe(v.Bonus)
	return &r
}

func (b *Blueprint) checkRollTemplate(d AbilityDefinition, step int, v RollResourceTemplate) error {
	fault := func(field, expected, actual string) error {
		return abilityError(d.ID, step, field, "", abilityFault("E_ROLL", field, expected, actual))
	}
	if b.Battle == nil || d.Action == nil {
		return fault("roll", "native Battle and Action policy", "absent")
	}
	if v.Kind != "heal" && v.Kind != "damage" && v.Kind != "expand" {
		return fault("kind", "heal, damage or expand", v.Kind)
	}
	if v.Kind == "expand" && v.Minimum <= 0 {
		return fault("minimum", "positive expansion floor", fmt.Sprint(v.Minimum))
	}
	if v.Kind == "heal" && v.Minimum < 0 {
		return fault("minimum", "positive heal floor or none", fmt.Sprint(v.Minimum))
	}
	if v.Kind == "damage" && v.Minimum != 0 {
		return fault("minimum", "absent for damage", fmt.Sprint(v.Minimum))
	}
	if !validID(string(v.Resource)) {
		return fault("resource", "valid Resource ID", string(v.Resource))
	}
	if v.Count < 1 || v.Count > 100 {
		return fault("count", "1..100 dice", fmt.Sprint(v.Count))
	}
	if v.Sides < 2 || v.Sides > 1000 {
		return fault("sides", "2..1000 sides", fmt.Sprint(v.Sides))
	}
	if err := b.checkEquipmentTerms(string(d.ID), v.Bonus.Terms); err != nil {
		return abilityError(d.ID, step, "bonus", "", err)
	}
	if v.Target.Literal != nil {
		if err := b.CheckResource(*v.Target.Literal.Character, v.Resource); err != nil {
			return abilityError(d.ID, step, "target", "", err)
		}
	}
	if v.Caster.Literal != nil {
		if err := b.checkTermInputs(*v.Caster.Literal.Character, v.Bonus.Terms); err != nil {
			return abilityError(d.ID, step, "caster", "", err)
		}
	}
	return nil
}

func (b *Blueprint) checkRollInputs(v InvokeAbility, steps []boundStep) error {
	if input := v.CheckBoost; input != nil {
		if input.Step < 0 || input.Step >= len(steps) || steps[input.Step].Check == nil || steps[input.Step].Check.Template.Boost == nil {
			return abilityError(v.Ability, input.Step, "boost", "", abilityFault("E_ROLL", "boost", "declared boost Check index", "invalid"))
		}
		if input.Face != nil && (!input.Use || *input.Face < 1 || uint64(*input.Face) > steps[input.Step].Check.Template.Boost.Sides) {
			return abilityError(v.Ability, input.Step, "boost", "", abilityFault("E_ROLL", "boost", "use with one in-range bonus face, or decline without face", "invalid"))
		}
	}
	if len(v.HumanRolls) > 1 {
		return abilityError(v.Ability, -1, "rolls", "", abilityFault("E_ROLL", "rolls", "at most one face group", "multiple"))
	}
	for _, input := range v.HumanRolls {
		if input.Step < 0 || input.Step >= len(steps) {
			return abilityError(v.Ability, input.Step, "rolls", "", abilityFault("E_ROLL", "step", "rolled-resource body index", fmt.Sprint(input.Step)))
		}
		s := steps[input.Step]
		count, sides := int64(0), int64(0)
		if s.RollResource != nil {
			count, sides = s.RollResource.Count, s.RollResource.Sides
		} else if s.Check != nil && s.Check.Template.ActorRoll {
			count, sides = 1, int64(s.Check.Template.Sides)
		} else {
			return abilityError(v.Ability, input.Step, "rolls", "", abilityFault("E_ROLL", "step", "rolled-resource or actor-roll Check body index", fmt.Sprint(input.Step)))
		}
		if input.Engine && len(input.Faces) != 0 {
			return abilityError(v.Ability, input.Step, "rolls", "", abilityFault("E_ROLL", "faces", "engine or faces, not both", "both"))
		}
		if len(input.Faces) > int(count) {
			return abilityError(v.Ability, input.Step, "rolls", "", abilityFault("E_ROLL", "faces", "no extra faces", "too many"))
		}
		for _, f := range input.Faces {
			if f < 1 || f > sides {
				return abilityError(v.Ability, input.Step, "rolls", "", abilityFault("E_ROLL", "face", fmt.Sprintf("1..%d", sides), fmt.Sprint(f)))
			}
		}
	}
	return nil
}

func (b *Blueprint) abilityRollController(actor Ref) (BattleController, error) {
	if b.Battle == nil || actor.Kind != CharacterKind {
		return "", battleFault("roll", "native Character actor", "invalid")
	}
	if p := battleParticipant(b.activeBattle(), CharacterID(actor.ID)); p != nil {
		return p.Admission.Controller, nil
	}
	if actor.ID == string(b.Battle.Rules.PartyRoot) {
		return HumanController, nil
	}
	return AgentController, nil
}

func (b *Blueprint) resolveResourceRoll(v InvokeAbility, step int, r RollResourceTemplate, ctx applicationContext) ([]Event, error) {
	controller, err := b.abilityRollController(ctx.cause.Actor)
	if err != nil {
		return nil, err
	}
	out := ResourceRollResult{Kind: r.Kind, Minimum: r.Minimum, Target: *r.Target.Literal.Character, Resource: r.Resource, Caster: *r.Caster.Literal.Character, Source: controller, Sides: r.Sides, Actual: "0", CallsBefore: b.Battle.State.Random.Calls}
	// A human may leave this step to NQL: it is drawn as for an agent.
	engine := controller == HumanController && len(v.HumanRolls) == 1 && v.HumanRolls[0].Step == step && v.HumanRolls[0].Engine
	if controller == HumanController && !engine {
		if len(v.HumanRolls) != 1 || v.HumanRolls[0].Step != step || len(v.HumanRolls[0].Faces) != int(r.Count) {
			err := abilityFault("E_ROLL", "faces", fmt.Sprintf("%d supplied human faces", r.Count), "incomplete")
			if len(v.HumanRolls) == 0 {
				return nil, &InputRequired{Need: BattleInputRequirements{Stage: "ability", Count: int(r.Count), Sides: r.Sides, AbilityStep: copyPtr(&step)}, Err: err}
			}
			return nil, err
		}
		out.Faces = append([]int64(nil), v.HumanRolls[0].Faces...)
	} else {
		if controller != HumanController && len(v.HumanRolls) != 0 {
			return nil, abilityFault("E_ROLL", "faces", "no NPC roll input", "present")
		}
		for range r.Count {
			face, err := drawStateDie(&b.Battle.State.Random, uint64(r.Sides), ctx.random)
			if err != nil {
				return nil, err
			}
			out.Faces = append(out.Faces, int64(face))
		}
	}
	if engine {
		out.EngineDrawn = true
	}
	out.CallsAfter = b.Battle.State.Random.Calls
	out.Bonus, out.BonusRecipe, err = evaluateRecipe(b, out.Caster, r.Bonus)
	if err != nil {
		return nil, err
	}
	total := big.NewInt(out.Bonus)
	for _, face := range out.Faces {
		total.Add(total, big.NewInt(face))
	}
	if total.Sign() < 0 {
		total.SetInt64(0)
	}
	if !total.IsInt64() {
		return nil, fail("E_OVERFLOW", string(out.Target), "rolled resource", "int64 amount", total.String(), "rolled amount exceeds numeric domain")
	}
	out.Requested = total.Int64()
	if (r.Kind == "expand" || r.Kind == "heal") && out.Requested < r.Minimum {
		out.Requested = r.Minimum
	}
	before := character(b, out.Target).Resources[r.Resource]
	var events []Event
	if out.Requested > 0 {
		operation := Operation{}
		resource := &ResourceOp{Character: out.Target, Resource: r.Resource, Amount: out.Requested}
		if r.Kind == "expand" {
			operation.Expand = resource
		} else if r.Kind == "heal" {
			operation.Heal = resource
		} else {
			operation.Damage = resource
		}
		events, err = applyOperation(b, operation, ctx)
		if err != nil {
			return nil, err
		}
	} else if _, err = battleResourceLife(b, out.Target, r.Resource, before, before, r.Kind == "heal", ctx); err != nil {
		return nil, err
	}
	after := character(b, out.Target).Resources[r.Resource]
	delta := new(big.Int).Sub(big.NewInt(after.Current), big.NewInt(before.Current))
	if r.Kind == "expand" {
		delta.Sub(big.NewInt(after.Maximum), big.NewInt(before.Maximum))
	}
	out.Actual = delta.Abs(delta).String()
	return append(events, Event{Kind: ResourceRollResolved, ResourceRoll: &out}), nil
}

// The query checks a detached candidate prefix without rolling or publishing.
// A pending question is for one exact complete group, never a model total.
func (b *Blueprint) abilityInput(v InvokeAbility, actor Ref) (BattleInputRequirements, error) {
	out := BattleInputRequirements{Stage: "complete"}
	if err := b.checkInvocation(v); err != nil {
		return out, err
	}
	if err := b.checkAction(v, actor); err != nil {
		return out, err
	}
	controller, err := b.abilityRollController(actor)
	if err != nil {
		return out, err
	}
	if controller != HumanController {
		return out, battleFault("input query", "human Ability actor", "agent")
	}
	steps, err := b.bindAbility(v)
	if err != nil {
		return out, err
	}
	work := 0
	ctx := applicationContext{work: &work, random: &requestRandom{}, revision: 1, cause: ConditionCause{RequestID: "input-query", Actor: actor}}
	if _, err := b.payBattleAction(v, abilityDefinition(b, v.Ability), ctx); err != nil {
		return out, err
	}
	for i, s := range steps {
		if err := battleWork(ctx); err != nil {
			return out, abilityError(v.Ability, i, "work", "", err)
		}
		if s.CapacityFormula != nil {
			if _, err := b.applyCapacityFormula(*s.CapacityFormula); err != nil {
				return out, abilityError(v.Ability, i, "formula", abilityDefinition(b, v.Ability).Steps[i].Expand.Formula.Source.Parameter, err)
			}
			continue
		}
		if s.RollResource != nil {
			if len(v.HumanRolls) != 0 && v.HumanRolls[0].Engine {
				return out, nil // answered: NQL draws it at execution
			}
			faces := []int64(nil)
			if len(v.HumanRolls) != 0 {
				faces = v.HumanRolls[0].Faces
			}
			if len(faces) != 0 && len(faces) != int(s.RollResource.Count) {
				return out, abilityFault("E_ROLL", "faces", "one complete group or none", "partial")
			}
			if len(faces) == 0 {
				out.Stage, out.Count, out.Sides, out.AbilityStep = "ability", int(s.RollResource.Count), s.RollResource.Sides, &i
			}
			return out, nil
		}
		ctx.cause.AbilityOrigin = &AbilityOrigin{Ability: v.Ability, Step: i}
		if _, err := applyOperation(b, s.Operation, ctx); err != nil {
			return out, templateError(v.Ability, i, nil, abilityDefinition(b, v.Ability).Steps[i], err)
		}
	}
	return out, battleFault("input query", "Ability with rolled Resource consequence", "none")
}
