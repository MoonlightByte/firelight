package core

func (b *Blueprint) checkBattleAction(v InvokeAbility, actor Ref) error {
	d := abilityDefinition(b, v.Ability)
	args := map[ParameterID]AbilityValue{}
	for _, a := range v.Arguments {
		args[a.Name] = a.Value
	}
	value := func(o AbilityOperand) AbilityValue { return operandValue(o, args) }
	control := *value(d.Action.Actor).Character
	reject := func(field, expected, actual string) error {
		return abilityError(d.ID, -1, field, "", fail("E_ELIGIBILITY", string(control), field, expected, actual, "authored action is not eligible"))
	}
	if actor.Kind != CharacterKind || actor.ID != string(control) {
		return reject("actor", string(control), actor.ID)
	}
	granted := false
	for _, id := range b.Battle.Rules.Actions {
		granted = granted || id == v.Ability
	}
	if !granted {
		return reject("grant", "declared Battle action", string(v.Ability))
	}
	if !b.battleAble(control) {
		return reject("actor", "standing actor", "down or dead")
	}
	e := b.activeBattle()
	if v.Allowance != "" && e == nil {
		return reject("allowance", "active Battle turn", "exploration")
	}
	if d.Action.Phase == EncounterPhase && e == nil || d.Action.Phase == Exploration && e != nil {
		return reject("phase", string(d.Action.Phase), "wrong phase")
	}
	if e != nil {
		current := e.Roster[e.Cursor].Admission.Character
		if current != control {
			return reject("turn", string(current), string(control))
		}
		if d.Action.TurnCost == nil && v.Allowance == "" && e.ActionUsed {
			return reject("action", "unspent action", "already used")
		}
		if character(b, control).Location != e.Location {
			return reject("location", string(e.Location), string(character(b, control).Location))
		}
	}
	return b.checkActionRequirements(d, value, reject)
}

// Both execution and human input preview apply this exact candidate prefix.
// It is outside indexed body steps, preserving roll indices and body provenance.
func (b *Blueprint) payBattleAction(v InvokeAbility, d *AbilityDefinition, ctx applicationContext) ([]Event, error) {
	if v.Allowance != "" {
		es, err := b.spendAllowance(v.Allowance, CharacterID(ctx.cause.Actor.ID), ctx)
		if err != nil {
			return nil, abilityError(d.ID, -1, "allowance", "", err)
		}
		return es, nil
	}
	if d.Action == nil || d.Action.TurnCost == nil || b.activeBattle() == nil {
		return nil, nil
	}
	c := d.Action.TurnCost
	op := Operation{Spend: &ResourceOp{Character: CharacterID(ctx.cause.Actor.ID), Resource: c.Resource, Amount: c.Amount}}
	err := battleWork(ctx)
	var events []Event
	if err == nil {
		err = b.CheckOperationStatic(op)
	}
	if err == nil {
		events, err = applyOperation(b, op, ctx)
	}
	if err != nil {
		return nil, abilityError(d.ID, -1, "turn_cost.resource", "", err)
	}
	return events, nil
}

func (b *Blueprint) finishBattleAction(before BattleEncounter, policy *ActionPolicy, allowance ResourceID, ctx applicationContext) ([]Event, *BattleActionResult, error) {
	e := b.battleEncounter(before.ID)
	finish := true
	if policy.TurnCost == nil {
		if allowance == "" {
			e.ActionUsed = true
		}
		if policy.Finish != nil {
			finish = *policy.Finish || !b.battleAble(e.Roster[e.Cursor].Admission.Character)
		}
	} else {
		// Never free a previously spent standard action. A downed actor cannot
		// retain the active window even if its authored action leaves it open.
		finish = policy.TurnCost.Finish || !b.battleAble(e.Roster[e.Cursor].Admission.Character)
	}
	events, err := advanceBattle(b, e, finish, ctx)
	if err != nil {
		return nil, nil, err
	}
	return events, &BattleActionResult{Before: before, After: cloneBattleEncounter(*e)}, nil
}
