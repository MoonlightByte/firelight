package core

func applyAdventureOperation(b *Blueprint, o Operation, ctx applicationContext) ([]Event, error) {
	if b.Battle != nil && o.Travel != nil {
		return applyNativeTravel(b, *o.Travel, ctx)
	}
	if b.Map != nil && o.Travel != nil {
		return applyMapTravel(b, *o.Travel, ctx)
	}
	if b.Adventure == nil {
		return nil, adventureFault("progression", "Adventure world", "legacy world")
	}
	if o.Travel != nil {
		if b.activeEncounter() != nil {
			return nil, adventureFault("travel", "no active encounter", "active")
		}
		for _, p := range b.Adventure.State.Encounters {
			if p.Status == Defeat {
				return nil, adventureFault("travel", "undefeated party", "defeat")
			}
		}
		party := b.Adventure.Definition.Party
		from := character(b, party[0]).Location
		to := o.Travel.To
		able := false
		for _, id := range party {
			able = able || b.alive(id)
			if character(b, id).Location != from {
				return nil, adventureFault("party location", "colocated companions", "split party")
			}
		}
		if !able {
			return nil, adventureFault("travel", "at least one able companion", "all downed")
		}
		linked := false
		for _, l := range b.Adventure.Definition.Links {
			linked = linked || (l.From == from && l.To == to)
		}
		if !linked {
			return nil, adventureFault("travel", "authored directed link", "not adjacent")
		}
		events := []Event{}
		for _, id := range party {
			if ctx.work != nil {
				*ctx.work++
				if *ctx.work > MaxOperations {
					return nil, abilityFault("E_LIMIT", "work", "at most 128", "travel exceeded")
				}
			}
			es, e := applyOperation(b, Operation{Move: &Move{Character: id, To: to}}, ctx)
			if e != nil {
				return nil, e
			}
			events = append(events, es...)
		}
		for _, d := range b.Adventure.Definition.Encounters {
			p := b.progress(d.ID)
			if d.Location != to || p.Status != Pending {
				continue
			}
			before := *p
			p.Status = Active
			p.Round = 1
			p.Cursor = 0
			for p.Cursor < len(d.Order) {
				if ctx.work != nil {
					*ctx.work++
					if *ctx.work > MaxOperations {
						return nil, abilityFault("E_LIMIT", "work", "at most 128", "encounter activation exceeded")
					}
				}
				if b.alive(d.Order[p.Cursor].Character) {
					break
				}
				p.Cursor++
			}
			if p.Cursor == len(d.Order) {
				return nil, adventureFault("start", "able participant", "none")
			}
			for _, x := range d.Order {
				if character(b, x.Character).Location != to {
					return nil, adventureFault("start", "all participants at encounter location", string(x.Character))
				}
			}
			events = append(events, Event{Kind: EncounterStarted, Encounter: &EncounterChange{Before: before, After: *p}})
		}
		events = append(events, b.completeObjectives()...)
		return append(events, Event{Kind: PartyTraveled, Travel: copyPtr(o.Travel)}), nil
	}
	c := o.Claim
	d := b.objectiveDefinition(c.Objective)
	if d == nil {
		return nil, adventureFault("claim", "declared objective", string(c.Objective))
	}
	var p *ObjectiveProgress
	for i := range b.Adventure.State.Objectives {
		if b.Adventure.State.Objectives[i].ID == d.ID {
			p = &b.Adventure.State.Objectives[i]
		}
	}
	if p == nil || p.Claimed {
		return nil, adventureFault("claim", "unclaimed objective", "already claimed")
	}
	if b.activeEncounter() != nil || b.progress(d.Victory).Status != Victory {
		return nil, adventureFault("claim", "required completed victory", "not victorious")
	}
	if !b.isParty(c.Recipient) || !b.alive(c.Recipient) {
		return nil, adventureFault("recipient", "able party member", string(c.Recipient))
	}
	if character(b, c.Recipient).Location != character(b, d.Custodian).Location {
		return nil, adventureFault("claim location", "recipient at custodian", "different locations")
	}
	if item(b, d.Item).Owner != d.Custodian {
		return nil, adventureFault("custodian", string(d.Custodian), string(item(b, d.Item).Owner))
	}
	if ctx.work != nil {
		*ctx.work++
		if *ctx.work > MaxOperations {
			return nil, abilityFault("E_LIMIT", "work", "at most 128", "claim exceeded")
		}
	}
	es, e := applyOperation(b, Operation{Transfer: &Transfer{Item: d.Item, From: d.Custodian, To: c.Recipient}}, ctx)
	if e != nil {
		return nil, e
	}
	before := *p
	p.Claimed = true
	change := &ObjectiveChange{Before: before, After: *p}
	es = append(es, Event{Kind: ObjectiveClaimed, Objective: change})
	es = append(es, b.completeObjectives()...)
	// The terminal consequence retains the original typed intent even when
	// claiming at the destination also completes the objective immediately.
	es[len(es)-1].Claim = copyPtr(c)
	return es, nil
}
func (b *Blueprint) completeObjectives() []Event {
	events := []Event{}
	for i, d := range b.Adventure.Definition.Objectives {
		p := &b.Adventure.State.Objectives[i]
		if !p.Claimed || p.Completed {
			continue
		}
		stock := item(b, d.Item)
		if itemQuantity(*stock) == 0 {
			continue
		}
		owner := stock.Owner
		if !b.isParty(owner) || character(b, owner).Location != d.Destination {
			continue
		}
		before := *p
		p.Completed = true
		events = append(events, Event{Kind: ObjectiveCompleted, Objective: &ObjectiveChange{Before: before, After: *p}})
	}
	return events
}
