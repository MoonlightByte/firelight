package core

func (b *Blueprint) checkAllowanceRules() error {
	if len(b.Battle.Rules.Allowances) > 8 {
		return battleFault("allowances", "at most 8 allowances", "exceeded")
	}
	seen := map[ResourceID]bool{}
	for _, a := range b.Battle.Rules.Allowances {
		if !validID(string(a.Resource)) || seen[a.Resource] || a.Resource == b.Battle.Rules.Health {
			return battleFault("allowance", "unique non-health Resource", string(a.Resource))
		}
		seen[a.Resource] = true
		for _, id := range b.Battle.Rules.TurnResources {
			if id == a.Resource {
				return battleFault("allowance", "Resource without turn_restore", string(id))
			}
		}
		declared := false
		for _, c := range b.Characters {
			if r, ok := c.Resources[a.Resource]; ok {
				declared = true
				if r.Minimum != 0 {
					return battleFault("allowance", "Resource minimum0", string(a.Resource))
				}
			}
		}
		if !declared || !a.Weapon && len(a.Actions) == 0 || len(a.Actions) > 128 {
			return battleFault("allowance", "existing Resource admitting weapons and/or at most 128 actions", string(a.Resource))
		}
		seenActions := map[AbilityID]bool{}
		for _, id := range a.Actions {
			d := abilityDefinition(b, id)
			granted := false
			for _, registered := range b.Battle.Rules.Actions {
				granted = granted || id == registered
			}
			if seenActions[id] || !granted || d == nil || d.Action == nil || d.Action.Phase == Exploration || d.Action.TurnCost != nil {
				return battleFault("allowance action", "unique registered ordinary Battle action", string(id))
			}
			seenActions[id] = true
		}
	}
	return nil
}

// Immutable admission only. Affordability belongs to shared Spend on the candidate.
func (b *Blueprint) checkSelectedAllowance(id ResourceID, action AbilityID, weapon bool) error {
	if b.Battle != nil && validID(string(id)) {
		for _, a := range b.Battle.Rules.Allowances {
			if a.Resource != id {
				continue
			}
			if weapon && a.Weapon {
				return nil
			}
			if !weapon {
				for _, allowed := range a.Actions {
					if allowed == action {
						return nil
					}
				}
			}
		}
	}
	return battleFault("allowance", "explicitly admitted action allowance", string(id))
}

func (b *Blueprint) spendAllowance(id ResourceID, actor CharacterID, ctx applicationContext) ([]Event, error) {
	op := Operation{Spend: &ResourceOp{Character: actor, Resource: id, Amount: 1}}
	if err := battleWork(ctx); err != nil {
		return nil, err
	}
	if err := b.CheckOperationStatic(op); err != nil {
		return nil, err
	}
	return applyOperation(b, op, ctx)
}

func clearBattleAllowances(b *Blueprint, actor CharacterID, ctx applicationContext) ([]Event, error) {
	var events []Event
	for _, a := range b.Battle.Rules.Allowances {
		r, ok := character(b, actor).Resources[a.Resource]
		if !ok || r.Current == 0 {
			continue
		}
		if err := battleWork(ctx); err != nil {
			return nil, err
		}
		// Rules require minimum0: current is a positive int64 and exact Damage
		// to that floor reuses Condition/resource consequences and typed events.
		op := Operation{Damage: &ResourceOp{Character: actor, Resource: a.Resource, Amount: r.Current}}
		if err := b.CheckOperationStatic(op); err != nil {
			return nil, err
		}
		es, err := applyOperation(b, op, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
	}
	return events, nil
}
