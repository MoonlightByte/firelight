package core

// Recovery identities are intent. Quantities and descriptive facts are derived
// only from recorded encounter use and the original immutable stock lineage.
type AmmunitionCreation struct {
	Origin  ItemID `json:"origin"`
	Created ItemID `json:"created"`
}
type RecoverAmmunition struct {
	Encounter EncounterID          `json:"encounter"`
	Actor     CharacterID          `json:"actor"`
	Create    []AmmunitionCreation `json:"create,omitempty"`
}
type AmmunitionRecovery struct {
	Intent RecoverAmmunition `json:"intent"`
	Before ActorAmmunition   `json:"before"`
	After  ActorAmmunition   `json:"after"`
}

func cloneRecoverAmmunition(v *RecoverAmmunition) *RecoverAmmunition {
	if v == nil {
		return nil
	}
	out := *v
	out.Create = append([]AmmunitionCreation(nil), v.Create...)
	return &out
}
func cloneAmmunitionRecovery(v *AmmunitionRecovery) *AmmunitionRecovery {
	if v == nil {
		return nil
	}
	out := *v
	out.Intent = *cloneRecoverAmmunition(&v.Intent)
	out.Before = cloneActorAmmunition([]ActorAmmunition{v.Before})[0]
	out.After = cloneActorAmmunition([]ActorAmmunition{v.After})[0]
	return &out
}
func (b *Blueprint) checkRecoverAmmunition(v RecoverAmmunition) error {
	if b.Battle == nil || b.Battle.Rules.Ranged == nil || !validID(string(v.Encounter)) || len(v.Create) > 64 {
		return battleFault("ammunition recovery", "ranged rules, encounter ID and at most 64 creation identities", "invalid")
	}
	if err := b.CheckRef(Ref{CharacterKind, string(v.Actor)}); err != nil {
		return err
	}
	origins, ids := map[ItemID]bool{}, map[ItemID]bool{}
	for _, c := range v.Create {
		if !b.ammunitionOrigin(c.Origin) || origins[c.Origin] {
			return battleFault("ammunition recovery origin", "unique original plain stock", string(c.Origin))
		}
		if ids[c.Created] || !validID(string(c.Created)) {
			return battleFault("ammunition recovery", "unique original stocks and fresh valid item IDs", string(c.Created))
		}
		if _, exists := b.KindOf(string(c.Created)); exists {
			return fail("E_DUPLICATE", string(c.Created), "created", "fresh canonical ID", string(c.Created), "recovery identity is already in use")
		}
		origins[c.Origin], ids[c.Created] = true, true
	}
	return nil
}
func recoverAmmunition(b *Blueprint, v RecoverAmmunition, ctx applicationContext) ([]Event, error) {
	if ctx.cause.Actor != (Ref{CharacterKind, string(v.Actor)}) || !b.battleAble(v.Actor) {
		return nil, battleFault("ammunition recovery", "acting able shooter", string(v.Actor))
	}
	var encounter *BattleEncounter
	for i := range b.Battle.State.Encounters {
		e := &b.Battle.State.Encounters[i]
		if e.Status == Active {
			return nil, battleFault("ammunition recovery", "no active encounter", string(e.ID))
		}
		if e.ID == v.Encounter {
			encounter = e
		}
	}
	if encounter == nil || character(b, v.Actor).Location != encounter.Location {
		return nil, battleFault("ammunition recovery", "shooter at recorded terminal encounter location", string(v.Encounter))
	}
	var record *ActorAmmunition
	for i := range encounter.Ammunition {
		if encounter.Ammunition[i].Actor == v.Actor {
			record = &encounter.Ammunition[i]
		}
	}
	if record == nil || record.Claimed {
		return nil, battleFault("ammunition recovery", "unclaimed recorded shots by actor", string(v.Actor))
	}
	before := cloneActorAmmunition([]ActorAmmunition{*record})[0]
	requested := map[ItemID]ItemID{}
	for _, c := range v.Create {
		requested[c.Origin] = c.Created
	}
	var events []Event
	for i := range record.Stocks {
		s := &record.Stocks[i]
		amount := recoveredAmount(b.Battle.Rules.Ranged, s.Spent)
		created := requested[s.Origin]
		if amount == 0 {
			if created != "" {
				return nil, battleFault("ammunition recovery", "no new identity for zero recovery", string(created))
			}
			continue
		}
		if created == "" {
			return nil, battleFault("ammunition recovery", "fresh identity for every positive recovery", string(s.Origin))
		}
		delete(requested, s.Origin)
		origin := item(b, s.Origin)
		op := Operation{CreateItem: &Item{ID: created, Name: origin.Name, Description: origin.Description, Owner: v.Actor, Quantity: &amount, Custody: &Ref{CharacterKind, string(v.Actor)}}}
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
		s.Recovered = created
	}
	if len(requested) != 0 {
		return nil, battleFault("ammunition recovery", "only recorded positive-recovery origins", "extra identity")
	}
	if err := battleWork(ctx); err != nil {
		return nil, err
	}
	es, err := advanceClock(b, AdvanceTime{Ticks: b.Battle.Rules.Ranged.RecoveryTicks}, ctx)
	if err != nil {
		return nil, err
	}
	events = append(events, es...)
	record.Claimed = true
	after := cloneActorAmmunition([]ActorAmmunition{*record})[0]
	return append(events, Event{Kind: AmmunitionRecovered, AmmunitionRecovery: &AmmunitionRecovery{Intent: *cloneRecoverAmmunition(&v), Before: before, After: after}}), nil
}
