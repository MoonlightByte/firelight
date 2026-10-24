package core

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sort"
)

func adventureFault(field, expected, actual string) error {
	return fail("E_ADVENTURE", "", field, expected, actual, "invalid adventure "+field)
}
func (b *Blueprint) encounterDefinition(id EncounterID) *EncounterDefinition {
	if b.Adventure != nil {
		for i := range b.Adventure.Definition.Encounters {
			if b.Adventure.Definition.Encounters[i].ID == id {
				return &b.Adventure.Definition.Encounters[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) objectiveDefinition(id ObjectiveID) *ObjectiveDefinition {
	if b.Adventure != nil {
		for i := range b.Adventure.Definition.Objectives {
			if b.Adventure.Definition.Objectives[i].ID == id {
				return &b.Adventure.Definition.Objectives[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) progress(id EncounterID) *EncounterProgress {
	if b.Adventure != nil {
		for i := range b.Adventure.State.Encounters {
			if b.Adventure.State.Encounters[i].ID == id {
				return &b.Adventure.State.Encounters[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) activeEncounter() *EncounterProgress {
	if b.Adventure != nil {
		for i := range b.Adventure.State.Encounters {
			if b.Adventure.State.Encounters[i].Status == Active {
				return &b.Adventure.State.Encounters[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) isParty(id CharacterID) bool {
	if b.Adventure != nil {
		for _, p := range b.Adventure.Definition.Party {
			if p == id {
				return true
			}
		}
	}
	return false
}
func (b *Blueprint) alive(id CharacterID) bool {
	c := character(b, id)
	if c == nil || b.Adventure == nil {
		return false
	}
	r, ok := c.Resources[b.Adventure.Definition.Health]
	return ok && r.Current > r.Minimum
}
func (b *Blueprint) initializeAdventure() error {
	if b.Adventure == nil {
		return nil
	}
	if e := b.validateAdventureDefinition(); e != nil {
		return e
	}
	for _, c := range b.Conditions {
		if c.Lifetime != nil {
			return adventureFault("genesis lifetime", "no pre-applied encounter-scoped Condition", string(c.ID))
		}
	}
	d := b.Adventure.Definition
	p := rand.NewPCG(d.Seed[0], d.Seed[1])
	data, e := p.MarshalBinary()
	if e != nil {
		return e
	}
	expected := AdventureState{Encounters: []EncounterProgress{}, Objectives: []ObjectiveProgress{}, Random: RandomState{Version: RandomVersion, Bytes: data}}
	for _, d := range d.Encounters {
		p := EncounterProgress{ID: d.ID, Status: Pending}
		if d.ID == b.Adventure.Definition.InitialEncounter {
			p.Status = Active
			p.Round = 1
			for p.Cursor < len(d.Order) && !b.alive(d.Order[p.Cursor].Character) {
				p.Cursor++
			}
			if p.Cursor == len(d.Order) {
				return adventureFault("initial_encounter", "able participant", "none")
			}
		}
		expected.Encounters = append(expected.Encounters, p)
	}
	for _, d := range d.Objectives {
		expected.Objectives = append(expected.Objectives, ObjectiveProgress{ID: d.ID})
	}
	if !reflect.DeepEqual(b.Adventure.State, AdventureState{}) && !reflect.DeepEqual(b.Adventure.State, expected) {
		return adventureFault("genesis", "zero or exact initial continuation", "historical state")
	}
	b.Adventure.State = expected
	return nil
}
func (b *Blueprint) validateAdventureDefinition() error {
	if b.Adventure == nil {
		return nil
	}
	d := b.Adventure.Definition
	if len(d.Party) == 0 || len(d.Party) > 8 || len(d.Encounters) > 8 || len(d.Objectives) > 16 || len(b.Characters) > 64 || len(b.Items) > 64 || len(b.Locations) > 32 || len(b.ConditionDefinitions) > 64 {
		return adventureFault("limits", "bounded Alpha entities/party/encounters/objectives", "exceeded")
	}
	party := map[CharacterID]bool{}
	for _, id := range d.Party {
		if party[id] {
			return adventureFault("party", "unique members", string(id))
		}
		party[id] = true
		if e := b.CheckResource(id, d.Health); e != nil {
			return e
		}
	}
	links := map[LocationLink]bool{}
	for _, l := range d.Links {
		if links[l] || l.From == l.To {
			return adventureFault("link", "unique directed link between distinct locations", "duplicate/self")
		}
		links[l] = true
		for _, id := range []LocationID{l.From, l.To} {
			if e := b.CheckRef(Ref{LocationKind, string(id)}); e != nil {
				return e
			}
		}
	}
	grants := map[CharacterID]bool{}
	for _, g := range d.Grants {
		if grants[g.Character] {
			return adventureFault("grants", "one record per character", string(g.Character))
		}
		grants[g.Character] = true
		if e := b.CheckResource(g.Character, d.Health); e != nil {
			return e
		}
		seen := map[AbilityID]bool{}
		for _, id := range g.Abilities {
			a := abilityDefinition(b, id)
			if seen[id] || a == nil || a.Action == nil {
				return adventureFault("grant", "unique declared opted-in Ability", string(id))
			}
			seen[id] = true
		}
	}
	encounters := map[EncounterID]bool{}
	locations := map[LocationID]bool{}
	roles := map[CharacterID]Team{}
	for _, e := range d.Encounters {
		if !validID(string(e.ID)) || encounters[e.ID] || locations[e.Location] || len(e.Order) < 2 || len(e.Order) > 16 {
			return adventureFault("encounter", "unique ID/location and 2..16 participants", string(e.ID))
		}
		encounters[e.ID] = true
		locations[e.Location] = true
		if err := b.CheckRef(Ref{LocationKind, string(e.Location)}); err != nil {
			return err
		}
		seen := map[CharacterID]bool{}
		hostiles := 0
		for _, p := range e.Order {
			if seen[p.Character] || (p.Team != PartyTeam && p.Team != HostileTeam) || party[p.Character] != (p.Team == PartyTeam) {
				return adventureFault("participant", "unique coherent team membership", string(p.Character))
			}
			seen[p.Character] = true
			if old, ok := roles[p.Character]; ok && old != p.Team {
				return adventureFault("team", "consistent team", string(p.Character))
			}
			roles[p.Character] = p.Team
			if p.Team == HostileTeam {
				hostiles++
			}
			if err := b.CheckResource(p.Character, d.Health); err != nil {
				return err
			}
		}
		if hostiles == 0 {
			return adventureFault("encounter", "at least one hostile", "none")
		}
		for id := range party {
			if !seen[id] {
				return adventureFault("encounter", "all party participants", string(id))
			}
		}
	}
	if d.InitialEncounter != "" && !encounters[d.InitialEncounter] {
		return adventureFault("initial_encounter", "declared encounter", string(d.InitialEncounter))
	}
	objectives := map[ObjectiveID]bool{}
	items := map[ItemID]bool{}
	for _, o := range d.Objectives {
		if !validID(string(o.ID)) || objectives[o.ID] || items[o.Item] || !encounters[o.Victory] || party[o.Custodian] {
			return adventureFault("objective", "unique objective/item, declared victory and nonparty custodian", string(o.ID))
		}
		objectives[o.ID] = true
		items[o.Item] = true
		for _, r := range []Ref{{ItemKind, string(o.Item)}, {CharacterKind, string(o.Custodian)}, {LocationKind, string(o.Destination)}} {
			if e := b.CheckRef(r); e != nil {
				return e
			}
		}
	}
	return nil
}
func (b *Blueprint) validateAdventure() error {
	if b.Adventure == nil {
		for _, c := range b.Conditions {
			if c.Lifetime != nil {
				return adventureFault("lifetime", "Adventure world", "legacy world")
			}
		}
		return nil
	}
	if e := b.validateAdventureDefinition(); e != nil {
		return e
	}
	a := b.Adventure
	s := a.State
	if reflect.DeepEqual(s, AdventureState{}) { // declaration IR only; constructor initializes before publication
		for _, c := range b.Conditions {
			if c.Lifetime != nil {
				return adventureFault("genesis", "no pre-applied timed conditions", string(c.ID))
			}
		}
		return nil
	}
	if s.Random.Version != RandomVersion || len(s.Random.Bytes) != 20 {
		return adventureFault("random", "versioned20-byte PCG", "invalid")
	}
	p := rand.NewPCG(0, 0)
	if e := p.UnmarshalBinary(s.Random.Bytes); e != nil {
		return e
	}
	if len(s.Encounters) != len(a.Definition.Encounters) || len(s.Objectives) != len(a.Definition.Objectives) {
		return adventureFault("progress", "one record per definition", "mismatch")
	}
	active := 0
	for i, p := range s.Encounters {
		d := a.Definition.Encounters[i]
		if p.ID != d.ID {
			return adventureFault("progress", "definition order/identity", string(p.ID))
		}
		switch p.Status {
		case Pending:
			if p.Round != 0 || p.Cursor != 0 {
				return adventureFault("pending", "zero round/cursor", "nonzero")
			}
		case Active, Victory, Defeat:
			if p.Round == 0 || p.Cursor < 0 || p.Cursor >= len(d.Order) {
				return adventureFault("turn", "positive round/valid cursor", "invalid")
			}
			if p.Status == Active {
				active++
				if !b.alive(d.Order[p.Cursor].Character) {
					return adventureFault("turn", "able current participant", "downed")
				}
				for _, x := range d.Order {
					if character(b, x.Character).Location != d.Location {
						return adventureFault("encounter location", string(d.Location), string(character(b, x.Character).Location))
					}
				}
			}
		default:
			return adventureFault("status", "pending/active/victory/defeat", string(p.Status))
		}
	}
	if active > 1 {
		return adventureFault("active", "at most one encounter", "multiple")
	}
	for i, p := range s.Objectives {
		if p.ID != a.Definition.Objectives[i].ID || p.Completed && !p.Claimed {
			return adventureFault("objective progress", "coherent lifecycle", string(p.ID))
		}
	}
	if len(b.Conditions) > 128 {
		return adventureFault("conditions", "at most 128 active", "too many")
	}
	timed := 0
	for _, c := range b.Conditions {
		if l := c.Lifetime; l != nil {
			timed++
			p := b.progress(l.Encounter)
			if p == nil || p.Status != Active || l.AppliedRound == 0 || l.ExpiresAfterRound < l.AppliedRound || l.ExpiresAfterRound < p.Round {
				return adventureFault("lifetime", "active scope with unexpired round", string(c.ID))
			}
		}
	}
	if timed > 16 {
		return adventureFault("lifetimes", "at most 16 active timed Conditions", "too many")
	}
	return nil
}
func (b *Blueprint) conditionLifetime(duration uint64) (*ConditionLifetime, error) {
	p := b.activeEncounter()
	if p == nil {
		return nil, adventureFault("duration", "active encounter", "none")
	}
	if duration == 0 || duration-1 > math.MaxUint64-p.Round {
		return nil, fail("E_OVERFLOW", "", "duration", "positive representable expiry boundary", fmt.Sprint(duration), "invalid duration or round overflow")
	}
	return &ConditionLifetime{Encounter: p.ID, AppliedRound: p.Round, ExpiresAfterRound: p.Round + duration - 1}, nil
}

// hasCondition sees instances, held types and the types they include alike.
func (b *Blueprint) hasCondition(id CharacterID, t ConditionTypeID) bool {
	for _, c := range b.Conditions {
		if c.Target == id && (c.Type == t || presentAs(conditionDefinition(b, c.Type), t)) {
			return true
		}
	}
	for i := range b.ConditionDefinitions {
		if d := &b.ConditionDefinitions[i]; presentAs(d, t) && heldAt(b, id, d) {
			return true
		}
	}
	return false
}
func (b *Blueprint) team(id CharacterID) Team {
	if b.Battle != nil {
		if p := battleParticipant(b.activeBattle(), id); p != nil {
			return Team(p.Admission.Side)
		}
		if b.battleParty(id) {
			return Team(ProtectedSide)
		}
		return ""
	}
	if b.isParty(id) {
		return PartyTeam
	}
	if p := b.activeEncounter(); p != nil {
		for _, x := range b.encounterDefinition(p.ID).Order {
			if x.Character == id {
				return x.Team
			}
		}
	}
	return ""
}
func (b *Blueprint) checkAction(v InvokeAbility, actor Ref) error {
	d := abilityDefinition(b, v.Ability)
	if d.Action == nil {
		return nil
	}
	if b.Battle != nil {
		return b.checkBattleAction(v, actor)
	}
	if b.Adventure == nil {
		return adventureFault("action", "Adventure world", "none")
	}
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
	for _, g := range b.Adventure.Definition.Grants {
		if g.Character == control {
			for _, id := range g.Abilities {
				granted = granted || id == d.ID
			}
		}
	}
	if !granted {
		return reject("grant", "granted Ability", string(d.ID))
	}
	if !b.alive(control) {
		return reject("actor", "above minimum Health", "downed")
	}
	p := b.activeEncounter()
	for _, state := range b.Adventure.State.Encounters {
		if state.Status == Defeat {
			return reject("phase", "undefeated party", "defeat")
		}
	}
	if d.Action.Phase == EncounterPhase && p == nil || d.Action.Phase == Exploration && p != nil {
		return reject("phase", string(d.Action.Phase), "wrong phase")
	}
	if p != nil {
		encounter := b.encounterDefinition(p.ID)
		current := encounter.Order[p.Cursor].Character
		if control != current {
			return reject("turn", string(current), string(control))
		}
		if character(b, control).Location != encounter.Location {
			return reject("location", string(encounter.Location), string(character(b, control).Location))
		}
	}
	return b.checkActionRequirements(d, value, reject)
}

// Both authored action profiles use the same predicate evaluator. Their phase,
// grant and turn laws remain explicit at the profile boundary.
func (b *Blueprint) checkActionRequirements(d *AbilityDefinition, value func(AbilityOperand) AbilityValue, reject func(string, string, string) error) error {
	for i, r := range d.Action.Requirements {
		good := false
		subject := value(r.Subject)
		if r.Kind == CarriedItem {
			i := item(b, *subject.Item)
			good = carriedBy(*i, *value(*r.Other).Character) && itemQuantity(*i) > 0
		} else if r.Kind == PartyOwned {
			good = b.isParty(item(b, *subject.Item).Owner)
			if b.Battle != nil {
				good = b.battleParty(item(b, *subject.Item).Owner)
			}
		} else {
			id := *subject.Character
			switch r.Kind {
			case AtLocation:
				good = character(b, id).Location == r.Location
			case StatBetween:
				x, err := explain(b, id, r.StatRange.Stat)
				if err != nil {
					return abilityError(d.ID, -1, fmt.Sprintf("requirement.%d.stat", i), r.Subject.Parameter, err)
				}
				good = x.Effective >= r.StatRange.Minimum && x.Effective <= r.StatRange.Maximum
			case AboveMinimum:
				if err := b.CheckResource(id, r.Resource); err != nil {
					return abilityError(d.ID, -1, fmt.Sprintf("requirement.%d.resource", i), r.Subject.Parameter, err)
				}
				x := character(b, id).Resources[r.Resource]
				good = x.Current > x.Minimum
			case HasCondition:
				good = b.hasCondition(id, r.Condition)
			case LacksCondition:
				good = !b.hasCondition(id, r.Condition)
			case FreeSlot:
				for _, slot := range b.equipmentSlots(id) {
					if slot.Slot == r.Slot.Slot {
						good = slot.Capacity-slot.Used >= r.Slot.Amount
					}
				}
			case WornTraining:
				good = true
				for _, item := range b.Items {
					if item.Wearer == id && item.Equipment != nil && item.Equipment.Definition == r.Worn.Definition && item.Equipment.Mode == r.Worn.Mode && !b.hasCondition(id, r.Condition) {
						good = false
					}
				}
			default:
				other := *value(*r.Other).Character
				switch r.Kind {
				case Colocated:
					good = character(b, id).Location == character(b, other).Location
				case WithinRange:
					good = b.withinRange(id, other, r.Distance.Maximum)
				case Distinct:
					good = id != other
				case Allied:
					good = b.team(id) != "" && b.team(id) == b.team(other)
				case Hostile:
					good = b.team(id) != "" && b.team(other) != "" && b.team(id) != b.team(other)
				}
			}
		}
		if !good {
			return reject(fmt.Sprintf("requirement.%d.subject", i), string(r.Kind), "unsatisfied")
		}
	}
	return nil
}
func (b *Blueprint) expireScope(id EncounterID, round uint64, terminal bool, ctx applicationContext, work *int) ([]Event, error) {
	ids := []ConditionInstanceID{}
	for _, c := range b.Conditions {
		if l := c.Lifetime; l != nil && l.Encounter == id && (terminal || l.ExpiresAfterRound <= round) {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	events := []Event{}
	for _, id := range ids {
		if conditionIndex(b, id) < 0 {
			continue
		}
		*work++
		if *work > MaxOperations {
			return nil, abilityFault("E_LIMIT", "work", "at most 128", "expiry exceeded")
		}
		c := b.Conditions[conditionIndex(b, id)]
		es, e := applyConditionOperation(b, Operation{RemoveCondition: &RemoveCondition{Instance: id, Target: c.Target}}, ctx)
		if e != nil {
			return nil, e
		}
		reason := "round end"
		if terminal {
			reason = "encounter end"
		}
		es[len(es)-1].Condition.Expiration = &ExpirationOrigin{Encounter: c.Lifetime.Encounter, Round: round, Reason: reason}
		events = append(events, es...)
	}
	return events, nil
}
func (b *Blueprint) finishTurn(id EncounterID, ctx applicationContext, work *int) ([]Event, error) {
	p := b.progress(id)
	if p == nil || p.Status != Active {
		return nil, nil
	}
	d := b.encounterDefinition(id)
	before := *p
	allParty, allHostiles := true, true
	for _, x := range d.Order {
		*work++
		if *work > MaxOperations {
			return nil, abilityFault("E_LIMIT", "work", "at most 128", "terminal assessment exceeded")
		}
		if b.alive(x.Character) {
			if x.Team == PartyTeam {
				allParty = false
			} else {
				allHostiles = false
			}
		}
	}
	if allParty || allHostiles {
		es, e := b.expireScope(id, p.Round, true, ctx, work)
		if e != nil {
			return nil, e
		}
		if allParty {
			p.Status = Defeat
		} else {
			p.Status = Victory
		}
		return append(es, Event{Kind: EncounterEnded, Encounter: &EncounterChange{Before: before, After: *p}}), nil
	}
	cursor := p.Cursor
	wrapped := false
	for n := 0; n < len(d.Order); n++ {
		*work++
		if *work > MaxOperations {
			return nil, abilityFault("E_LIMIT", "work", "at most 128", "turn exceeded")
		}
		cursor++
		if cursor == len(d.Order) {
			cursor = 0
			wrapped = true
		}
		if b.alive(d.Order[cursor].Character) {
			break
		}
	}
	es := []Event{}
	var boundary *uint64
	if wrapped {
		if p.Round == math.MaxUint64 {
			return nil, fail("E_OVERFLOW", string(id), "round", "incrementable round", "maximum", "round exhausted")
		}
		r := p.Round
		boundary = &r
		var e error
		es, e = b.expireScope(id, r, false, ctx, work)
		if e != nil {
			return nil, e
		}
		p.Round++
	}
	p.Cursor = cursor
	return append(es, Event{Kind: TurnAdvanced, Encounter: &EncounterChange{Before: before, After: *p, Boundary: boundary}}), nil
}
