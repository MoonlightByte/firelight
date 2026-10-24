package core

import (
	"fmt"
	"math"
	"math/big"
	"sort"
)

func battleWork(ctx applicationContext) error {
	*ctx.work++
	if *ctx.work > MaxOperations {
		return fail("E_LIMIT", "", "work", "at most 128", "battle exceeded", "battle work budget exceeded")
	}
	return nil
}
func battleClock(b *Blueprint, ctx applicationContext) ([]Event, error) {
	if err := battleWork(ctx); err != nil {
		return nil, err
	}
	return advanceClock(b, AdvanceTime{Ticks: b.Battle.Rules.RoundTicks}, ctx)
}
func beginBattle(b *Blueprint, v BeginBattle, ctx applicationContext) ([]Event, error) {
	if err := b.checkBegin(v, ctx.cause.Actor); err != nil {
		return nil, err
	}
	for _, a := range v.Participants {
		if a.Controller == HumanController && a.Initiative == nil {
			return nil, &InputRequired{Need: BattleInputRequirements{Stage: "initiative", Count: 1, Sides: b.Battle.Rules.DieSides}, Err: battleFault("initiative", "supplied human raw face", "missing")}
		}
	}
	e := BattleEncounter{ID: v.ID, Location: v.Location, Status: Active, Round: 1}
	for _, a := range v.Participants {
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		result := BattleInitiative{CallsBefore: b.Battle.State.Random.Calls}
		mod, err := explain(b, a.Character, b.Battle.Rules.Initiative)
		if err != nil {
			return nil, err
		}
		result.Modifier = mod.Effective
		if a.Controller == HumanController {
			result.Face = *a.Initiative
		} else {
			draw, err := drawStateDie(&b.Battle.State.Random, uint64(b.Battle.Rules.DieSides), ctx.random)
			if err != nil {
				return nil, err
			}
			result.Face = int64(draw)
		}
		result.CallsAfter = b.Battle.State.Random.Calls
		total := new(big.Int).Add(big.NewInt(result.Face), big.NewInt(result.Modifier))
		if !total.IsInt64() {
			return nil, fail("E_OVERFLOW", string(a.Character), "initiative", "int64", total.String(), "initiative total exceeds numeric domain")
		}
		result.Total = total.Int64()
		a.Initiative = copyPtr(a.Initiative)
		e.Roster = append(e.Roster, BattleParticipant{Admission: a, Result: result})
	}
	sort.Slice(e.Roster, func(i, j int) bool {
		a, z := e.Roster[i], e.Roster[j]
		if a.Result.Total != z.Result.Total {
			return a.Result.Total > z.Result.Total
		}
		return a.Admission.TieRank < z.Admission.TieRank
	})
	b.Battle.State.Encounters = append(b.Battle.State.Encounters, e)
	events, err := restoreTurnResources(b, e.Roster[0].Admission.Character, ctx)
	if err != nil {
		return nil, err
	}
	return append(events, Event{Kind: BattleStarted, BattleStarted: &BattleStartedResult{Intent: *cloneBegin(&v), After: cloneBattleEncounter(e)}}), nil
}

func restoreTurnResources(b *Blueprint, actor CharacterID, ctx applicationContext) ([]Event, error) {
	events, err := clearBattleAllowances(b, actor, ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range b.Battle.Rules.TurnResources {
		if _, exists := character(b, actor).Resources[id]; !exists {
			continue
		}
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		es, err := applyOperation(b, Operation{Restore: &RestoreResource{Character: actor, Resource: id}}, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
	}
	return events, nil
}

// The active round is settled only when wrapping or entering a terminal state.
// The terminal check precedes wrapping, so a last-turn victory never charges twice.
func advanceBattle(b *Blueprint, e *BattleEncounter, finish bool, ctx applicationContext) ([]Event, error) {
	for range e.Roster {
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
	}
	if outcome := b.battleOutcome(e); outcome != "" {
		events, err := endBattleConditions(b, e, "terminal", ctx)
		if err != nil {
			return nil, err
		}
		es, err := clearBattleAllowances(b, e.Roster[e.Cursor].Admission.Character, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
		es, err = battleClock(b, ctx)
		if err != nil {
			return nil, err
		}
		e.Status = outcome
		return append(events, es...), nil
	}
	if !finish {
		return nil, nil
	}
	events, err := clearBattleAllowances(b, e.Roster[e.Cursor].Admission.Character, ctx)
	if err != nil {
		return nil, err
	}
	for n := 0; n < len(e.Roster); n++ {
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		e.Cursor++
		if e.Cursor == len(e.Roster) {
			if e.Round == math.MaxUint64 {
				return nil, battleFault("round", "incrementable round", "maximum")
			}
			es, err := battleClock(b, ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, es...)
			e.Cursor = 0
			e.Round++
		}
		es, err := endBattleConditions(b, e, "source_turn", ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
		if b.battleAble(e.Roster[e.Cursor].Admission.Character) {
			e.ActionUsed = false
			es, err := restoreTurnResources(b, e.Roster[e.Cursor].Admission.Character, ctx)
			return append(events, es...), err
		}
	}
	return nil, battleFault("turn", "standing participant", "none")
}
func executeWeapon(b *Blueprint, v WeaponAttack, ctx applicationContext) ([]Event, error) {
	var events []Event
	if v.Allowance != "" {
		if err := b.checkBattleStatic(Operation{WeaponAttack: &v}); err != nil {
			return nil, err
		}
		if _, err := b.checkBattleWindow(v.BattleWindow, ctx.cause.Actor); err != nil {
			return nil, err
		}
		var err error
		events, err = b.spendAllowance(v.Allowance, v.Actor, ctx)
		if err != nil {
			return nil, err
		}
	}
	p, err := b.prepareWeapon(v, ctx.cause.Actor)
	if err != nil {
		return nil, err
	}
	before := cloneBattleEncounter(*p.encounter)
	if p.roll.Source == HumanController {
		var need BattleInputRequirements
		p, need, err = b.humanWeaponInput(v, ctx.cause.Actor)
		if err != nil {
			return nil, err
		}
		if need.Stage != "complete" {
			return nil, &InputRequired{Need: need, Err: battleFault("dice", fmt.Sprintf("complete human %s faces", need.Stage), "missing")}
		}
	} else {
		faces := make([]int64, p.attackCount)
		for i := range faces {
			draw, err := drawStateDie(&b.Battle.State.Random, uint64(b.Battle.Rules.DieSides), ctx.random)
			if err != nil {
				return nil, err
			}
			faces[i] = int64(draw)
		}
		if err = b.selectWeaponRoll(&p, faces); err != nil {
			return nil, err
		}
		damage := make([]int64, p.damageCount)
		for i := range damage {
			draw, err := drawStateDie(&b.Battle.State.Random, uint64(p.grant.DamageSides), ctx.random)
			if err != nil {
				return nil, err
			}
			damage[i] = int64(draw)
		}
		if err = b.finishWeaponDamage(&p, v.Target, damage); err != nil {
			return nil, err
		}
	}
	p.roll.CallsAfter = b.Battle.State.Random.Calls
	if p.grant.Ranged != nil {
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		op := Operation{Consume: &ConsumeItem{Item: v.Ammunition, Owner: v.Actor, Amount: 1}}
		if err := b.CheckOperationStatic(op); err != nil {
			return nil, err
		}
		es, err := applyOperation(b, op, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
		if err := recordAmmunition(p.encounter, v.Actor, p.origin); err != nil {
			return nil, err
		}
	}
	ended, err := endBattleConditions(b, p.encounter, "attack", ctx)
	if err != nil {
		return nil, err
	}
	events = append(events, ended...)
	if p.roll.Requested > 0 {
		if err = battleWork(ctx); err != nil {
			return nil, err
		}
		op := Operation{Damage: &ResourceOp{Character: v.Target, Resource: b.Battle.Rules.Health, Amount: p.roll.Requested}}
		if err = b.CheckOperationStatic(op); err != nil {
			return nil, err
		}
		es, err := applyOperation(b, op, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
		resource := es[len(es)-1].Resource
		p.roll.Actual = new(big.Int).Sub(big.NewInt(resource.Before.Current), big.NewInt(resource.After.Current)).String()
	}
	if p.roll.Hit && v.HitCondition != nil {
		r := b.hitConditionRule(v.HitCondition.Rule)
		op := Operation{ApplyCondition: &ApplyCondition{Instance: v.HitCondition.Instance, Type: r.Condition, Target: v.Target, Source: &Ref{CharacterKind, string(v.Actor)}}}
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		if err := b.CheckOperationStatic(op); err != nil {
			return nil, err
		}
		es, err := applyOperation(b, op, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
	}
	if v.Allowance == "" {
		p.encounter.ActionUsed = true
	}
	es, err := advanceBattle(b, p.encounter, v.FinishTurn, ctx)
	if err != nil {
		return nil, err
	}
	events = append(events, es...)
	result := &WeaponAttackResult{Intent: *cloneWeaponAttack(&v), Grant: cloneAttackGrant(p.grant), Roll: cloneWeaponRoll(p.roll), Before: before, After: cloneBattleEncounter(*p.encounter)}
	return append(events, Event{Kind: WeaponAttackResolved, WeaponAttack: result}), nil
}
func endBattleTurn(b *Blueprint, v EndBattleTurn, ctx applicationContext) ([]Event, error) {
	e, err := b.checkBattleWindow(v.BattleWindow, ctx.cause.Actor)
	if err != nil {
		return nil, err
	}
	before := cloneBattleEncounter(*e)
	es, err := advanceBattle(b, e, true, ctx)
	if err != nil {
		return nil, err
	}
	return append(es, Event{Kind: BattleTurnEnded, BattleTurn: &BattleTurnResult{Intent: v, Before: before, After: cloneBattleEncounter(*e)}}), nil
}
