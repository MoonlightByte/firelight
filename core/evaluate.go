package core

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"reflect"
)

// evaluateRequest stages one complete transaction. Execute is its sole publisher;
// Preview uses the same path on detached values and never installs a result.
func evaluateRequest(initial Blueprint, revision Revision, r Request) (Blueprint, Batch, error) {
	candidate := cloneBlueprint(initial)
	batch := Batch{Revision: revision + 1, Request: r, Events: []Event{}}
	work := 0
	random := requestRandom{}
	appendEvents := func(events []Event, index int, origin *AbilityOrigin) error {
		if len(batch.Events)+len(events) > 2*MaxOperations {
			return fail("E_LIMIT", r.ID, "events", "at most 256 events", "expanded limit", "invocation consequences exceed event bound")
		}
		for _, e := range events {
			e.Revision = batch.Revision
			e.ID = fmt.Sprintf("event:%d:%d", batch.Revision, len(batch.Events)+1)
			e.RequestID = r.ID
			e.Actor = r.Actor
			e.Operation = index
			e.AbilityOrigin = cloneAbilityOrigin(origin)
			batch.Events = append(batch.Events, e)
		}
		return nil
	}
	for index, o := range r.Operations {
		if e := o.checkShape(); e != nil {
			return Blueprint{}, Batch{}, e
		}
		steps := []boundStep{{Operation: o}}
		var definition *AbilityDefinition
		var started EncounterID
		var battleBefore *BattleEncounter
		if o.Invoke != nil {
			var e error
			steps, e = candidate.bindAbility(*o.Invoke)
			if e != nil {
				return Blueprint{}, Batch{}, e
			}
			// Static facts are checked for both branches; mutable mechanics only execute
			// along the chosen branch. Neither binding nor checking reserves creation IDs.
			if e = candidate.checkInvocation(*o.Invoke); e != nil {
				return Blueprint{}, Batch{}, e
			}
			definition = abilityDefinition(&candidate, o.Invoke.Ability)
			if e = candidate.checkAction(*o.Invoke, r.Actor); e != nil {
				return Blueprint{}, Batch{}, e
			}
			if definition.Action != nil {
				if p := candidate.activeEncounter(); p != nil {
					started = p.ID
				}
				if p := candidate.activeBattle(); p != nil {
					before := cloneBattleEncounter(*p)
					battleBefore = &before
				}
			}
		}
		inputs := conditionInputCursor{Values: o.ConditionRolls}
		ctx := applicationContext{conditionInput: &inputs, random: &random, work: &work, revision: batch.Revision, cause: ConditionCause{RequestID: r.ID, Operation: index, Actor: r.Actor}}
		if battleBefore != nil {
			es, err := candidate.payBattleAction(*o.Invoke, definition, ctx)
			if err != nil {
				return Blueprint{}, Batch{}, err
			}
			if err := appendEvents(es, index, nil); err != nil {
				return Blueprint{}, Batch{}, err
			}
		}
		primitive := func(op Operation, step int, path []int, template AbilityStep) error {
			work++
			if work > MaxOperations {
				return fail("E_LIMIT", r.ID, "operations", "at most 128 expanded primitives", fmt.Sprint(work), "expanded transaction limit exceeded")
			}
			child := ctx
			if o.Invoke != nil {
				child.cause.AbilityOrigin = &AbilityOrigin{Ability: o.Invoke.Ability, Step: step, Path: append([]int(nil), path...)}
			}
			err := candidate.CheckOperationStatic(op)
			var es []Event
			if err == nil {
				es, err = applyOperation(&candidate, op, child)
			}
			if err != nil {
				if o.Invoke != nil {
					err = templateError(o.Invoke.Ability, step, path, template, err)
				}
				return err
			}
			return appendEvents(es, index, child.cause.AbilityOrigin)
		}
		for i, s := range steps {
			var template AbilityStep
			if definition != nil {
				template = definition.Steps[i]
			}
			if s.CapacityFormula != nil {
				work++
				if work > MaxOperations {
					return Blueprint{}, Batch{}, abilityFault("E_LIMIT", "work", "at most 128", "capacity formula exceeded")
				}
				event, err := candidate.applyCapacityFormula(*s.CapacityFormula)
				if err != nil {
					return Blueprint{}, Batch{}, abilityError(o.Invoke.Ability, i, "formula", template.Expand.Formula.Source.Parameter, err)
				}
				origin := &AbilityOrigin{Ability: o.Invoke.Ability, Step: i}
				if err := appendEvents([]Event{event}, index, origin); err != nil {
					return Blueprint{}, Batch{}, err
				}
				continue
			}
			if s.RollResource != nil {
				work++
				if work > MaxOperations {
					return Blueprint{}, Batch{}, abilityFault("E_LIMIT", "work", "at most 128", "rolled resource exceeded")
				}
				child := ctx
				child.cause.AbilityOrigin = &AbilityOrigin{Ability: o.Invoke.Ability, Step: i}
				es, err := candidate.resolveResourceRoll(*o.Invoke, i, *s.RollResource, child)
				if err != nil {
					return Blueprint{}, Batch{}, abilityError(o.Invoke.Ability, i, "roll", "", err)
				}
				if err := appendEvents(es, index, child.cause.AbilityOrigin); err != nil {
					return Blueprint{}, Batch{}, err
				}
				continue
			}
			if s.Check == nil {
				if e := primitive(s.Operation, i, nil, template); e != nil {
					return Blueprint{}, Batch{}, e
				}
				continue
			}
			work++
			if work > MaxOperations {
				return Blueprint{}, Batch{}, abilityFault("E_LIMIT", "work", "at most 128", "check exceeded")
			}
			var result CheckResult
			var e error
			if s.Check.Template.ActorRoll {
				result, e = candidate.resolveActorCheck(*o.Invoke, i, s.Check.Template, ctx)
			} else {
				result, e = resolveCheck(&candidate, s.Check.Template, &random, definition.Action != nil)
			}
			if e != nil {
				return Blueprint{}, Batch{}, abilityError(o.Invoke.Ability, i, "check", "", e)
			}
			origin := &AbilityOrigin{Ability: o.Invoke.Ability, Step: i}
			if result.Boost != nil && result.Boost.Paid > 0 {
				payment := ResourceOp{Character: CharacterID(r.Actor.ID), Resource: result.Boost.Policy.Resource, Amount: result.Boost.Paid}
				if e = primitive(Operation{Spend: &payment}, i, nil, template); e != nil {
					return Blueprint{}, Batch{}, e
				}
			}
			if e = appendEvents([]Event{{Kind: CheckResolved, Check: &result}}, index, origin); e != nil {
				return Blueprint{}, Batch{}, e
			}
			branch := s.Check.Failure
			authored := template.Check.Failure
			bi := 1
			if result.Success {
				branch = s.Check.Success
				authored = template.Check.Success
				bi = 0
			}
			for j, op := range branch {
				if e := primitive(op, i, []int{bi, j}, authored[j].Step()); e != nil {
					return Blueprint{}, Batch{}, e
				}
			}
		}
		if inputs.Next != len(inputs.Values) {
			if candidate.Battle == nil {
				return Blueprint{}, Batch{}, rollFault(string(inputs.Values[inputs.Next].Condition), "faces", "faces only for a triggered check", "unused faces", "no damage check took these faces")
			}
			return Blueprint{}, Batch{}, battleFault("condition rolls", "only consumed human evidence", "unused response")
		}
		if started != "" {
			es, e := candidate.finishTurn(started, ctx, &work)
			if e != nil {
				return Blueprint{}, Batch{}, e
			}
			if e = appendEvents(es, index, nil); e != nil {
				return Blueprint{}, Batch{}, e
			}
		}
		if o.Invoke != nil {
			terminal := Event{Kind: AbilityInvoked, Ability: cloneInvocation(o.Invoke)}
			if battleBefore != nil {
				es, result, e := candidate.finishBattleAction(*battleBefore, definition.Action, o.Invoke.Allowance, ctx)
				if e != nil {
					return Blueprint{}, Batch{}, e
				}
				if e = appendEvents(es, index, nil); e != nil {
					return Blueprint{}, Batch{}, e
				}
				terminal.BattleAction = result
			}
			if e := appendEvents([]Event{terminal}, index, nil); e != nil {
				return Blueprint{}, Batch{}, e
			}
		}
	}
	if e := Validate(candidate); e != nil {
		return Blueprint{}, Batch{}, e
	}
	if e := validateConditionRevisions(&candidate, batch.Revision); e != nil {
		return Blueprint{}, Batch{}, e
	}
	if e := candidate.checkLifeRevisions(batch.Revision); e != nil {
		return Blueprint{}, Batch{}, e
	}
	normalize(&candidate)
	return candidate, batch, nil
}

// PCG advances only this request's cloned candidate. All underlying calls count,
// including rejection samples inside the standard library's unbiased mapper.
type randomExhausted struct{}
type requestRandom struct{ used uint64 }
type countedPCG struct {
	source  *rand.PCG
	calls   *uint64
	request *requestRandom
}

func (c *countedPCG) Uint64() uint64 {
	if c.request.used == 256 || *c.calls == math.MaxUint64 {
		panic(randomExhausted{})
	}
	c.request.used++
	*c.calls++
	return c.source.Uint64()
}
func drawStateDie(state *RandomState, sides uint64, request *requestRandom) (draw uint64, err error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(randomExhausted); !ok {
				panic(p)
			}
			draw = 0
			err = fail("E_LIMIT", "", "random", "at most 256 raw calls and incrementable counter", "exhausted", "random work budget exhausted")
		}
	}()
	p := rand.NewPCG(0, 0)
	if err = p.UnmarshalBinary(state.Bytes); err != nil {
		return 0, err
	}
	source := &countedPCG{source: p, calls: &state.Calls, request: request}
	draw = rand.New(source).Uint64N(sides) + 1
	state.Bytes, err = p.MarshalBinary()
	return draw, err
}

// drawStateWords draws raw words, such as an item seed, under the same budget.
func drawStateWords(state *RandomState, n int, request *requestRandom) (words []uint64, err error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(randomExhausted); !ok {
				panic(p)
			}
			words = nil
			err = fail("E_LIMIT", "", "random", "at most 256 raw calls and incrementable counter", "exhausted", "random work budget exhausted")
		}
	}()
	p := rand.NewPCG(0, 0)
	if err = p.UnmarshalBinary(state.Bytes); err != nil {
		return nil, err
	}
	source := &countedPCG{source: p, calls: &state.Calls, request: request}
	for range n {
		words = append(words, source.Uint64())
	}
	state.Bytes, err = p.MarshalBinary()
	return words, err
}
func checkValue(b *Blueprint, v CheckValue) (int64, error) {
	if v.Literal != nil {
		return *v.Literal, nil
	}
	x, e := explain(b, *v.Stat.Character.Literal.Character, v.Stat.Stat)
	return x.Effective, e
}
func resolveCheck(b *Blueprint, c CheckTemplate, r *requestRandom, optedIn bool) (CheckResult, error) {
	result := CheckResult{Check: c.ID, Sides: c.Sides}
	var state *RandomState
	if b.Adventure != nil {
		state = &b.Adventure.State.Random
	} else if optedIn && b.Battle != nil {
		state = &b.Battle.State.Random
	} else {
		return result, adventureFault("check", "Adventure random state", "legacy world")
	}
	var e error
	result.Modifier, e = checkValue(b, c.Modifier)
	if e != nil {
		return result, e
	}
	result.Threshold, e = checkValue(b, c.Threshold)
	if e != nil {
		return result, e
	}
	result.CallsBefore = state.Calls
	result.Draw, e = drawStateDie(state, c.Sides, r)
	if e != nil {
		return result, e
	}
	result.CallsAfter = state.Calls
	return completeCheck(result)
}

func completeCheck(result CheckResult) (CheckResult, error) {
	sum := new(big.Int).Add(new(big.Int).SetUint64(result.Draw), big.NewInt(result.Modifier))
	if !sum.IsInt64() {
		return result, fail("E_OVERFLOW", result.Check, "check", "int64 total", sum.String(), "check total exceeds numeric domain")
	}
	result.Total = sum.Int64()
	result.Success = result.Total >= result.Threshold
	return result, nil
}

// Preview checks a complete request against a detached candidate. It returns no
// roll, private candidate values or reservation; it is not an authorization API.
func (w *World) Preview(input Request) error {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	r := cloneRequest(input)
	if len(r.Operations) == 0 || len(r.Operations) > MaxOperations {
		return abilityFault("E_LIMIT", "operations", "1..128", "invalid")
	}
	if !validID(r.ID) {
		return fail("E_ID", r.ID, "request", "valid request ID", "invalid", "request ID is required")
	}
	if index, ok := w.authority.receipts[r.ID]; ok {
		if reflect.DeepEqual(w.authority.history[index].Request, r) {
			return nil
		}
		return fail("E_REQUEST_REUSE", r.ID, "request", "identical retry", "different content", "request ID already committed with different content")
	}
	if r.ExpectedRevision != w.authority.state.Revision {
		return fail("E_STALE", r.ID, "revision", fmt.Sprint(w.authority.state.Revision), fmt.Sprint(r.ExpectedRevision), "request was planned against a different revision")
	}
	if e := w.authority.state.CheckRef(r.Actor); e != nil {
		return e
	}
	if w.authority.state.Revision == Revision(math.MaxUint64) {
		return fail("E_OVERFLOW", r.ID, "revision", "incrementable revision", "maximum", "world revision exhausted")
	}
	_, batch, e := evaluateRequest(w.authority.state.Blueprint, w.authority.state.Revision, r)
	if e != nil {
		return e
	}
	_, e = w.authority.admitBatch(batch)
	return e
}
