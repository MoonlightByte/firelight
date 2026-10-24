package core

import "sort"

func (b *Blueprint) checkTravelRoutes() error {
	if len(b.Battle.Rules.Routes) > 64 {
		return battleFault("routes", "at most 64 directed routes", "exceeded")
	}
	seen := map[[2]LocationID]bool{}
	for _, route := range b.Battle.Rules.Routes {
		for _, id := range []LocationID{route.From, route.To} {
			if err := b.CheckRef(Ref{LocationKind, string(id)}); err != nil {
				return err
			}
		}
		pair := [2]LocationID{route.From, route.To}
		if route.From == route.To || route.Ticks <= 0 || seen[pair] {
			return battleFault("route", "distinct endpoints, positive ticks and unique direction", string(route.From)+" -> "+string(route.To))
		}
		seen[pair] = true
	}
	return nil
}

func applyNativeTravel(b *Blueprint, intent PartyTravel, ctx applicationContext) ([]Event, error) {
	r := b.Battle.Rules
	if ctx.cause.Actor != (Ref{CharacterKind, string(r.PartyRoot)}) {
		return nil, battleFault("travel actor", string(r.PartyRoot), ctx.cause.Actor.ID)
	}
	if b.activeBattle() != nil {
		return nil, battleFault("travel", "no active encounter", "active")
	}
	from := character(b, r.PartyRoot).Location
	var route *TravelRoute
	for i := range r.Routes {
		if r.Routes[i].From == from && r.Routes[i].To == intent.To {
			route = &r.Routes[i]
			break
		}
	}
	if route == nil {
		return nil, battleFault("travel route", "authored directed route", string(from)+" -> "+string(intent.To))
	}
	if route.RequiresVictory {
		victorious := false
		for _, e := range b.Battle.State.Encounters {
			victorious = victorious || e.Location == from && e.Status == Victory
		}
		if !victorious {
			return nil, battleFault("travel victory", "completed victory at departure", string(from))
		}
	}
	party := []CharacterID{}
	for _, c := range b.Characters {
		if !b.battleParty(c.ID) {
			continue
		}
		if c.Location != from || !b.battleAble(c.ID) {
			return nil, battleFault("travel party", "colocated standing party members", string(c.ID))
		}
		party = append(party, c.ID)
	}
	sort.Slice(party, func(i, j int) bool { return party[i] < party[j] })
	events := []Event{}
	apply := func(op Operation) error {
		if ctx.work != nil {
			*ctx.work++
			if *ctx.work > MaxOperations {
				return abilityFault("E_LIMIT", "work", "at most 128", "travel exceeded")
			}
		}
		es, err := applyOperation(b, op, ctx)
		if err == nil {
			events = append(events, es...)
		}
		return err
	}
	for _, id := range party {
		if err := apply(Operation{Move: &Move{Character: id, To: intent.To}}); err != nil {
			return nil, err
		}
	}
	if err := apply(Operation{AdvanceTime: &AdvanceTime{Ticks: route.Ticks}}); err != nil {
		return nil, err
	}
	return append(events, Event{Kind: PartyTraveled, Travel: &intent}), nil
}
