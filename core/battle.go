package core

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"reflect"
	"sort"
)

func battleFault(field, expected, actual string) error {
	return fail("E_BATTLE", "", field, expected, actual, "invalid battle "+field)
}
func lifeOutcome(v LifeOutcome) bool { return v == StableDown || v == Dead }
func (b *Blueprint) battleParty(id CharacterID) bool {
	if b.Battle == nil {
		return false
	}
	r := b.Battle.Rules
	if id == r.PartyRoot {
		return true
	}
	for _, rel := range b.Relationships {
		if rel.From == (Ref{CharacterKind, string(id)}) && rel.To == (Ref{CharacterKind, string(r.PartyRoot)}) && rel.Kind == r.Membership && rel.Strength == r.MembershipStrength {
			return true
		}
	}
	return false
}
func (b *Blueprint) battleEncounter(id EncounterID) *BattleEncounter {
	if b.Battle != nil {
		for i := range b.Battle.State.Encounters {
			if b.Battle.State.Encounters[i].ID == id {
				return &b.Battle.State.Encounters[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) activeBattle() *BattleEncounter {
	if b.Battle != nil {
		for i := range b.Battle.State.Encounters {
			if b.Battle.State.Encounters[i].Status == Active {
				return &b.Battle.State.Encounters[i]
			}
		}
	}
	return nil
}
func battleParticipant(e *BattleEncounter, id CharacterID) *BattleParticipant {
	if e != nil {
		for i := range e.Roster {
			if e.Roster[i].Admission.Character == id {
				return &e.Roster[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) battleLife(id CharacterID) *BattleLife {
	if b.Battle != nil {
		for i := range b.Battle.State.Life {
			if b.Battle.State.Life[i].Character == id {
				return &b.Battle.State.Life[i]
			}
		}
	}
	return nil
}
func (b *Blueprint) battleAble(id CharacterID) bool {
	c := character(b, id)
	if b.Battle == nil || c == nil || b.battleLife(id) != nil {
		return false
	}
	r, ok := c.Resources[b.Battle.Rules.Health]
	return ok && r.Current > r.Minimum
}
func (b *Blueprint) initializeBattle() error {
	if b.Battle == nil {
		return nil
	}
	if err := b.checkBattleRules(); err != nil {
		return err
	}
	if err := b.checkBattleBirths(); err != nil {
		return err
	}
	p := rand.NewPCG(b.Battle.Rules.Seed[0], b.Battle.Rules.Seed[1])
	raw, err := p.MarshalBinary()
	if err != nil {
		return err
	}
	expected := BattleState{Random: RandomState{Version: RandomVersion, Bytes: raw}}
	if !reflect.DeepEqual(b.Battle.State, BattleState{}) && !reflect.DeepEqual(b.Battle.State, expected) {
		return battleFault("genesis", "zero or exact initial continuation", "historical state")
	}
	b.Battle.State = expected
	return nil
}
func (b *Blueprint) checkBattleBirths() error {
	for _, c := range b.Characters {
		if err := b.checkBattleBirth(c); err != nil {
			return err
		}
	}
	return nil
}
func (b *Blueprint) checkBattleBirth(c Character) error {
	if b.Battle != nil {
		if r, ok := c.Resources[b.Battle.Rules.Health]; ok && r.Current <= r.Minimum {
			return battleFault("creation health", "above minimum; no zero-health genesis/creation semantics", string(c.ID))
		}
	}
	return nil
}
func (b *Blueprint) checkBattleRules() error {
	if err := b.checkBattleEncounterDefinitions(); err != nil {
		return err
	}
	if err := b.checkRangedPolicy(); err != nil {
		return err
	}
	if b.Battle == nil {
		return nil
	}
	if err := b.checkAllowanceRules(); err != nil {
		return err
	}
	if err := b.checkHitConditionRules(); err != nil {
		return err
	}
	if err := b.checkConditionCheckRules(); err != nil {
		return err
	}
	if err := b.checkLocationSpans(); err != nil {
		return err
	}
	r := b.Battle.Rules
	if len(r.TurnResources) > 8 {
		return battleFault("turn resources", "at most 8 turn Resources", "exceeded")
	}
	seenResources := map[ResourceID]bool{}
	for _, id := range r.TurnResources {
		declared := false
		for _, c := range b.Characters {
			_, ok := c.Resources[id]
			declared = declared || ok
		}
		if !validID(string(id)) || id == r.Health || seenResources[id] || !declared {
			return battleFault("turn resource", "unique existing non-health Resource", string(id))
		}
		seenResources[id] = true
	}
	if len(r.Actions) > 128 {
		return battleFault("actions", "at most 128 declared actions", "exceeded")
	}
	if err := b.checkTravelRoutes(); err != nil {
		return err
	}
	seenActions := map[AbilityID]bool{}
	for _, id := range r.Actions {
		d := abilityDefinition(b, id)
		if seenActions[id] || d == nil || d.Action == nil {
			return battleFault("action", "unique declared opted-in Ability", string(id))
		}
		seenActions[id] = true
	}
	if b.Adventure != nil || b.Clock == nil || b.Equipment == nil {
		return battleFault("profile", "Clock and Equipment, no Alpha Adventure", "incompatible")
	}
	if r.Version != BattleVersion || !validID(string(r.Health)) || !validID(string(r.Defense)) || !validID(string(r.Initiative)) || !validID(string(r.Membership)) || !validID(r.GridUnit) || r.ClockUnit != b.Clock.Unit {
		return battleFault("rules", "known version and explicit valid selectors/units", "invalid")
	}
	if err := b.CheckRef(Ref{CharacterKind, string(r.PartyRoot)}); err != nil {
		return err
	}
	if r.DieSides < 2 || r.DieSides > 1000 || r.MissFace < 1 || r.MissFace > r.DieSides || r.CriticalFace < 1 || r.CriticalFace > r.DieSides || r.MissFace == r.CriticalFace || r.CriticalMultiplier < 1 || r.CriticalMultiplier > 8 || r.SquareSize < 1 || r.SquareSize > 1000000 || r.RoundTicks < 1 || !lifeOutcome(r.ProtectedOutcome) || !lifeOutcome(r.OppositionOutcome) {
		return battleFault("law", "bounded explicit roll/grid/time/life law", "invalid")
	}
	if len(r.RollPolicies) > 16 || len(r.DamagePolicies) > 16 || len(r.ArmorPolicies) > 16 {
		return battleFault("policies", "at most 16 of each policy", "exceeded")
	}
	rolls := map[BattleRollPolicy]bool{}
	for _, p := range r.RollPolicies {
		if conditionDefinition(b, p.Condition) == nil || (p.On != "attacker" && p.On != "target") || (p.Direction != "advantage" && p.Direction != "disadvantage") || rolls[p] {
			return battleFault("roll policy", "unique typed declared Condition policy", fmt.Sprint(p))
		}
		rolls[p] = true
	}
	damage := map[BattleDamagePolicy]bool{}
	for _, p := range r.DamagePolicies {
		if conditionDefinition(b, p.Condition) == nil || !validID(p.Kind) || (p.Treatment != "resistance" && p.Treatment != "vulnerability" && p.Treatment != "immunity") || damage[p] {
			return battleFault("damage policy", "unique typed declared Condition policy", fmt.Sprint(p))
		}
		damage[p] = true
	}
	armor := map[BattleArmorPolicy]bool{}
	for _, p := range r.ArmorPolicies {
		if equipmentMode(b.equipmentDefinition(p.Definition), p.Mode) == nil || conditionDefinition(b, p.Training) == nil || !validID(string(p.AttackStat)) || armor[p] {
			return battleFault("armor policy", "unique declared mode/training and attack stat", fmt.Sprint(p))
		}
		armor[p] = true
	}
	for _, d := range b.Equipment.Definitions {
		for _, m := range d.Modes {
			for _, a := range m.Attacks {
				if (a.Melee != nil || a.Ranged != nil) && maxDamageCount(a) > 200/r.CriticalMultiplier {
					return battleFault("critical dice", "at most 200 expanded damage dice", a.ID)
				}
			}
		}
	}
	for _, t := range b.ConditionDefinitions {
		for _, a := range t.Attacks {
			if (a.Melee != nil || a.Ranged != nil) && maxDamageCount(a) > 200/r.CriticalMultiplier {
				return battleFault("critical dice", "at most 200 expanded damage dice", a.ID)
			}
		}
	}
	return nil
}
func (b *Blueprint) validateBattle() error {
	if b.Battle == nil {
		return nil
	}
	if err := b.checkBattleRules(); err != nil {
		return err
	}
	s := b.Battle.State
	if err := b.checkAmmunitionHistory(); err != nil {
		return err
	}
	if reflect.DeepEqual(s, BattleState{}) {
		return b.checkBattleBirths()
	} // declaration IR; NewWorld initializes
	p := rand.NewPCG(0, 0)
	if s.Random.Version != RandomVersion || p.UnmarshalBinary(s.Random.Bytes) != nil {
		return battleFault("random", "valid versioned PCG continuation", "invalid")
	}
	if len(s.Encounters) > 16 || len(s.Life) > 64 {
		return battleFault("capacity", "at most 16 encounters and 64 life outcomes", "exceeded")
	}
	lives := map[CharacterID]bool{}
	for _, l := range s.Life {
		if lives[l.Character] || !lifeOutcome(l.Outcome) || l.AppliedRevision == 0 || !validID(l.Cause.RequestID) || l.Cause.Operation < 0 || l.Cause.Operation >= MaxOperations {
			return battleFault("life", "unique caused zero-health outcome", "invalid")
		}
		lives[l.Character] = true
		if err := b.CheckResource(l.Character, b.Battle.Rules.Health); err != nil {
			return err
		}
		c := character(b, l.Character)
		r := c.Resources[b.Battle.Rules.Health]
		if r.Current != r.Minimum {
			return battleFault("life health", "resource at minimum", string(l.Character))
		}
		if err := b.CheckRef(l.Cause.Actor); err != nil {
			return err
		}
		if err := checkLifeAbilityOrigin(b, l.Cause.AbilityOrigin); err != nil {
			return err
		}
	}
	for _, c := range b.Characters {
		if r, ok := c.Resources[b.Battle.Rules.Health]; ok && r.Current == r.Minimum && !lives[c.ID] {
			return battleFault("life health", "recorded meaning for zero-health Character", string(c.ID))
		}
	}
	ids := map[EncounterID]bool{}
	active := 0
	for i := range s.Encounters {
		e := &s.Encounters[i]
		if !validID(string(e.ID)) || ids[e.ID] || len(e.Roster) < 2 || len(e.Roster) > 16 || e.Round == 0 || e.Cursor < 0 || e.Cursor >= len(e.Roster) || (e.Status != Active && e.Status != Victory && e.Status != Defeat) {
			return battleFault("encounter", "bounded unique structurally valid encounter", string(e.ID))
		}
		ids[e.ID] = true
		if err := b.CheckRef(Ref{LocationKind, string(e.Location)}); err != nil {
			return err
		}
		chars := map[CharacterID]bool{}
		ranks := map[int]bool{}
		pos := map[BattlePosition]bool{}
		sides := map[BattleSide]bool{}
		human := 0
		for j, v := range e.Roster {
			a := v.Admission
			if chars[a.Character] || ranks[a.TieRank] || pos[a.Position] || a.TieRank < 0 || a.TieRank >= len(e.Roster) || !validBattlePosition(a.Position) || (a.Side != ProtectedSide && a.Side != OppositionSide) || (a.Controller != HumanController && a.Controller != AgentController) {
				return battleFault("roster", "unique valid participants/ranks/squares", "invalid")
			}
			chars[a.Character] = true
			ranks[a.TieRank] = true
			pos[a.Position] = true
			sides[a.Side] = true
			if err := b.CheckRef(Ref{CharacterKind, string(a.Character)}); err != nil {
				return err
			}
			if v.Result.Face < 1 || v.Result.Face > b.Battle.Rules.DieSides || v.Result.CallsAfter < v.Result.CallsBefore || v.Result.CallsAfter > s.Random.Calls {
				return battleFault("initiative", "valid recorded face/counters", "invalid")
			}
			total := new(big.Int).Add(big.NewInt(v.Result.Face), big.NewInt(v.Result.Modifier))
			if !total.IsInt64() || total.Int64() != v.Result.Total {
				return battleFault("initiative", "exact recorded total", "invalid")
			}
			if a.Controller == HumanController {
				human++
				if a.Character != b.Battle.Rules.PartyRoot || a.Side != ProtectedSide || a.Initiative == nil || *a.Initiative != v.Result.Face || v.Result.CallsBefore != v.Result.CallsAfter {
					return battleFault("human initiative", "root supplied face, no RNG", "invalid")
				}
			} else if a.Initiative != nil || v.Result.CallsAfter <= v.Result.CallsBefore || v.Result.CallsAfter-v.Result.CallsBefore > 256 {
				return battleFault("agent initiative", "engine draw, no supplied face", "invalid")
			}
			if j > 0 {
				prior := e.Roster[j-1]
				if prior.Result.Total < v.Result.Total || (prior.Result.Total == v.Result.Total && prior.Admission.TieRank > a.TieRank) {
					return battleFault("order", "initiative descending, accepted tie rank ascending", "invalid")
				}
			}
			// Terminal rosters are history; current facts may legitimately have changed.
			if e.Status == Active {
				if character(b, a.Character).Location != e.Location {
					return battleFault("location", "active admitted location", string(a.Character))
				}
				if err := b.CheckResource(a.Character, b.Battle.Rules.Health); err != nil {
					return err
				}
			}
		}
		if human != 1 || len(sides) != 2 {
			return battleFault("roster", "both sides and exactly one root human", "invalid")
		}
		if err := b.checkFormationSpan(e.Location, pos); err != nil {
			return err
		}
		if e.Status == Active {
			active++
			if !b.battleAble(e.Roster[e.Cursor].Admission.Character) || b.battleOutcome(e) != "" {
				return battleFault("window", "able actor and two standing sides", "inconsistent")
			}
		}
	}
	if active > 1 {
		return battleFault("active", "one active encounter", "multiple")
	}
	return nil
}
func validBattlePosition(p BattlePosition) bool {
	return p.X >= -1024 && p.X <= 1024 && p.Y >= -1024 && p.Y <= 1024
}
func (b *Blueprint) checkBattleStatic(o Operation) error {
	if b.Battle == nil {
		return battleFault("profile", "authored Battle rules", "absent")
	}
	if v := o.BeginBattle; v != nil {
		if !validID(string(v.ID)) || len(v.Participants) < 2 || len(v.Participants) > 16 {
			return battleFault("begin", "valid ID and 2..16 participants", "invalid")
		}
		if err := b.CheckRef(Ref{LocationKind, string(v.Location)}); err != nil {
			return err
		}
		for _, a := range v.Participants {
			if err := b.CheckRef(Ref{CharacterKind, string(a.Character)}); err != nil {
				return err
			}
			if !validBattlePosition(a.Position) || (a.Side != ProtectedSide && a.Side != OppositionSide) || (a.Controller != HumanController && a.Controller != AgentController) || a.TieRank < 0 || a.TieRank >= len(v.Participants) {
				return battleFault("participant", "typed side/control/bounded position/rank", "invalid")
			}
			if a.Initiative != nil && (*a.Initiative < 1 || *a.Initiative > b.Battle.Rules.DieSides) {
				return battleFault("initiative", "in-range raw face", "invalid")
			}
		}
		return nil
	}
	var w BattleWindow
	if o.WeaponAttack != nil {
		v := o.WeaponAttack
		if err := b.checkWeaponConditionStatic(*v); err != nil {
			return err
		}
		if v.Allowance != "" {
			if err := b.checkSelectedAllowance(v.Allowance, "", true); err != nil {
				return err
			}
		}
		w = v.BattleWindow
		if !validID(string(v.Grant)) {
			return battleFault("grant", "current derived grant ID", "invalid")
		}
		if err := b.CheckRef(Ref{CharacterKind, string(v.Target)}); err != nil {
			return err
		}
		if v.Ammunition != "" {
			if err := b.CheckRef(Ref{ItemKind, string(v.Ammunition)}); err != nil {
				return err
			}
		}
		if v.Human != nil && (len(v.Human.Attack) > 2 || len(v.Human.Damage) > 200) {
			return battleFault("dice", "at most 2 attack and 200 damage faces", "exceeded")
		}
	} else {
		w = o.EndBattleTurn.BattleWindow
	}
	if !validID(string(w.Encounter)) || w.Round == 0 || w.Cursor < 0 || w.Cursor >= 16 {
		return battleFault("window", "valid encounter,positive round,cursor0..15", "invalid")
	}
	return b.CheckRef(Ref{CharacterKind, string(w.Actor)})
}
func (b *Blueprint) checkBegin(v BeginBattle, actor Ref) error {
	if err := b.checkBattleStatic(Operation{BeginBattle: &v}); err != nil {
		return err
	}
	if b.activeBattle() != nil || b.battleEncounter(v.ID) != nil || len(b.Battle.State.Encounters) >= 16 {
		return battleFault("begin", "unused encounter ID, no active battle, capacity available", string(v.ID))
	}
	if actor != (Ref{CharacterKind, string(b.Battle.Rules.PartyRoot)}) {
		return battleFault("actor", "party root attribution", actor.ID)
	}
	if err := b.checkAuthoredBegin(v); err != nil {
		return err
	}
	ids := map[CharacterID]bool{}
	ranks := map[int]bool{}
	positions := map[BattlePosition]bool{}
	sides := map[BattleSide]bool{}
	for _, a := range v.Participants {
		if ids[a.Character] || ranks[a.TieRank] || positions[a.Position] {
			return battleFault("roster", "unique Character, tie rank and square", "duplicate")
		}
		ids[a.Character] = true
		ranks[a.TieRank] = true
		positions[a.Position] = true
		sides[a.Side] = true
		if b.battleParty(a.Character) != (a.Side == ProtectedSide) || (a.Character == b.Battle.Rules.PartyRoot) != (a.Controller == HumanController) {
			return battleFault("roster", "canonical party side and root human control", string(a.Character))
		}
		if character(b, a.Character).Location != v.Location || !b.battleAble(a.Character) {
			return battleFault("participant", "present able Character", string(a.Character))
		}
		for _, s := range []StatID{b.Battle.Rules.Defense, b.Battle.Rules.Initiative} {
			if err := b.CheckStat(a.Character, s); err != nil {
				return err
			}
		}
		if a.Controller == AgentController && a.Initiative != nil {
			return battleFault("initiative", "agent face generated by runtime", "supplied")
		}
	}
	if err := b.checkFormationSpan(v.Location, positions); err != nil {
		return err
	}
	if len(sides) != 2 {
		return battleFault("roster", "two admitted sides", "missing")
	}
	for _, c := range b.Characters {
		if b.battleParty(c.ID) && !ids[c.ID] {
			return battleFault("party roster", "complete current party participation", string(c.ID))
		}
	}
	return nil
}
func (b *Blueprint) checkBattleWindow(w BattleWindow, actor Ref) (*BattleEncounter, error) {
	e := b.battleEncounter(w.Encounter)
	if e == nil || e.Status != Active || e.Round != w.Round || e.Cursor != w.Cursor || e.Roster[e.Cursor].Admission.Character != w.Actor || actor != (Ref{CharacterKind, string(w.Actor)}) || !b.battleAble(w.Actor) || character(b, w.Actor).Location != e.Location {
		return nil, battleFault("window", "current able actor/window and matching attribution", "stale or ineligible")
	}
	return e, nil
}
func (b *Blueprint) battleOutcome(e *BattleEncounter) EncounterStatus {
	p, o := false, false
	for _, v := range e.Roster {
		if b.battleAble(v.Admission.Character) {
			if v.Admission.Side == ProtectedSide {
				p = true
			} else {
				o = true
			}
		}
	}
	if !p {
		return Defeat
	}
	if !o {
		return Victory
	}
	return ""
}
func (b *Blueprint) checkLifeRevisions(revision Revision) error {
	if b.Battle != nil {
		for _, l := range b.Battle.State.Life {
			if l.AppliedRevision > revision {
				return battleFault("life revision", "at most world revision", "future")
			}
		}
	}
	return nil
}

// Resource transitions own life consequences; no independently writable HP copy
// or generic life setter exists. Unknown nonparty zero crossings are rejected.
func battleResourceLife(b *Blueprint, c CharacterID, res ResourceID, before, after Resource, healing bool, ctx applicationContext) (*LifeChange, error) {
	if b.Battle == nil || res != b.Battle.Rules.Health {
		return nil, nil
	}
	old := cloneBattleLife(b.battleLife(c))
	if old != nil && old.Outcome == Dead && healing {
		return nil, battleFault("healing", "living Character; resurrection unsupported", string(c))
	}
	if after.Current > after.Minimum {
		if old == nil {
			return nil, nil
		}
		if old.Outcome != StableDown {
			return nil, battleFault("life", "dead resource stays at minimum", string(c))
		}
		for i, l := range b.Battle.State.Life {
			if l.Character == c {
				b.Battle.State.Life = append(b.Battle.State.Life[:i], b.Battle.State.Life[i+1:]...)
				break
			}
		}
		return &LifeChange{Before: old}, nil
	}
	if old != nil {
		return nil, nil
	}
	var outcome LifeOutcome
	if p := battleParticipant(b.activeBattle(), c); p != nil {
		outcome = b.Battle.Rules.OppositionOutcome
		if p.Admission.Side == ProtectedSide {
			outcome = b.Battle.Rules.ProtectedOutcome
		}
	} else if b.battleParty(c) {
		outcome = b.Battle.Rules.ProtectedOutcome
	}
	if outcome == "" {
		return nil, battleFault("zero health", "active admitted side or current protected membership", string(c))
	}
	l := BattleLife{Character: c, Outcome: outcome, AppliedRevision: ctx.revision, Cause: *cloneConditionCause(&ctx.cause)}
	b.Battle.State.Life = append(b.Battle.State.Life, l)
	sort.Slice(b.Battle.State.Life, func(i, j int) bool { return b.Battle.State.Life[i].Character < b.Battle.State.Life[j].Character })
	return &LifeChange{After: cloneBattleLife(&l)}, nil
}
