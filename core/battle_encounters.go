package core

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// BattleEncounterDefinition is immutable authored admission, not a second live roster.
// All current party members fill the protected slots, root first then sorted IDs.
type BattleEncounterDefinition struct {
	ID                 EncounterID         `json:"id"`
	Name               string              `json:"name"`
	Location           LocationID          `json:"location"`
	ProtectedPositions []BattlePosition    `json:"protected_positions"`
	Opposition         []EncounterOpponent `json:"opposition"`
}
type EncounterOpponent struct {
	Character CharacterID    `json:"character"`
	Position  BattlePosition `json:"position"`
}
type EncounterOffer struct {
	Definition  BattleEncounterDefinition `json:"definition"`
	Intent      *BeginBattle              `json:"intent,omitempty"`
	Unavailable *Fault                    `json:"unavailable,omitempty"`
}

func cloneBattleEncounterDefinition(d BattleEncounterDefinition) BattleEncounterDefinition {
	d.ProtectedPositions = append([]BattlePosition(nil), d.ProtectedPositions...)
	d.Opposition = append([]EncounterOpponent(nil), d.Opposition...)
	return d
}
func encounterFault(id EncounterID, field, expected, actual string) error {
	return fail("E_ENCOUNTER", string(id), field, expected, actual, "invalid or unavailable authored encounter")
}

// CheckBattleEncounterDefinition validates only immutable declarations. Moving or
// recruiting a named opponent must change availability, not invalidate the World.
func (b Blueprint) CheckBattleEncounterDefinition(d BattleEncounterDefinition) error {
	if !validID(string(d.ID)) {
		return encounterFault(d.ID, "id", "valid encounter identity", string(d.ID))
	}
	if d.Name == "" || len(d.Name) > 4096 || !utf8.ValidString(d.Name) {
		return encounterFault(d.ID, "name", "nonempty UTF-8 label up to 4096 bytes", d.Name)
	}
	if err := b.CheckRef(Ref{LocationKind, string(d.Location)}); err != nil {
		return encounterFault(d.ID, "location", "declared Location", string(d.Location))
	}
	if len(d.ProtectedPositions) < 1 || len(d.Opposition) < 1 || len(d.ProtectedPositions)+len(d.Opposition) > 16 {
		return encounterFault(d.ID, "formation", "protected and opposition slots totaling2..16", "invalid count")
	}
	seen := map[BattlePosition]bool{}
	checkPosition := func(p BattlePosition, field string) error {
		if p.X < -1024 || p.X > 1024 {
			return encounterFault(d.ID, field+".x", "coordinate -1024..1024", fmt.Sprint(p.X))
		}
		if p.Y < -1024 || p.Y > 1024 {
			return encounterFault(d.ID, field+".y", "coordinate -1024..1024", fmt.Sprint(p.Y))
		}
		if seen[p] {
			return encounterFault(d.ID, field+".x", "unique authored square", "duplicate")
		}
		seen[p] = true
		return nil
	}
	for i, p := range d.ProtectedPositions {
		if err := checkPosition(p, fmt.Sprintf("protected[%d]", i)); err != nil {
			return err
		}
	}
	ids := map[CharacterID]bool{}
	for i, p := range d.Opposition {
		field := fmt.Sprintf("opposition[%d]", i)
		if err := b.CheckRef(Ref{CharacterKind, string(p.Character)}); err != nil {
			return encounterFault(d.ID, field+".character", "declared Character", string(p.Character))
		}
		if ids[p.Character] {
			return encounterFault(d.ID, field+".character", "unique opponent", "duplicate")
		}
		ids[p.Character] = true
		if err := checkPosition(p.Position, field); err != nil {
			return err
		}
	}
	if b.Battle != nil {
		if err := b.checkFormationSpan(d.Location, seen); err != nil {
			return encounterFault(d.ID, "formation", "within authored location span", err.Error())
		}
	}
	return nil
}
func (b *Blueprint) checkBattleEncounterDefinitions() error {
	if b.Battle == nil {
		return nil
	}
	if len(b.Battle.Rules.EncounterDefinitions) > 16 {
		return encounterFault("", "definitions", "at most 16 definitions", "exceeded")
	}
	seen := map[EncounterID]bool{}
	for _, d := range b.Battle.Rules.EncounterDefinitions {
		if seen[d.ID] {
			return encounterFault(d.ID, "id", "unique encounter definition", "duplicate")
		}
		seen[d.ID] = true
		if err := b.CheckBattleEncounterDefinition(d); err != nil {
			return err
		}
	}
	return nil
}
func (b *Blueprint) encounterFormation(d BattleEncounterDefinition) (BeginBattle, error) {
	root := b.Battle.Rules.PartyRoot
	var others []CharacterID
	for _, c := range b.Characters {
		if c.ID != root && b.battleParty(c.ID) {
			others = append(others, c.ID)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i] < others[j] })
	party := append([]CharacterID{root}, others...)
	if len(party) > len(d.ProtectedPositions) {
		return BeginBattle{}, encounterFault(d.ID, "party", "room for every current party member", "formation too small")
	}
	intent := BeginBattle{ID: d.ID, Location: d.Location}
	for i, id := range party {
		controller := AgentController
		if id == root {
			controller = HumanController
		}
		intent.Participants = append(intent.Participants, BattleAdmission{Character: id, Side: ProtectedSide, Controller: controller, Position: d.ProtectedPositions[i], TieRank: i})
	}
	for _, p := range d.Opposition {
		intent.Participants = append(intent.Participants, BattleAdmission{Character: p.Character, Side: OppositionSide, Controller: AgentController, Position: p.Position, TieRank: len(intent.Participants)})
	}
	return intent, nil
}
func (b *Blueprint) checkAuthoredBegin(v BeginBattle) error {
	if len(b.Battle.Rules.EncounterDefinitions) == 0 {
		return nil
	} // retained direct-operation contract
	for _, d := range b.Battle.Rules.EncounterDefinitions {
		if d.ID != v.ID {
			continue
		}
		want, err := b.encounterFormation(d)
		if err != nil {
			return err
		}
		if want.Location != v.Location || len(want.Participants) != len(v.Participants) {
			return encounterFault(v.ID, "formation", "complete authored formation", "different location or roster")
		}
		for i, got := range v.Participants {
			// Input validation still checks face bounds and prohibits agent-supplied
			// initiative. Preserve order: it determines which agent receives each draw.
			if got.Controller == HumanController {
				got.Initiative = nil
			}
			if got != want.Participants[i] {
				return encounterFault(v.ID, "formation", "exact ordered authored admissions", "different participant, order, square or tie rank")
			}
		}
		return nil
	}
	return encounterFault(v.ID, "id", "declared encounter", "unknown")
}

// EncounterOffers derives detached admission proposals from this snapshot.
// Its result is never authoritative input: Execute rechecks against current facts.
func (b Blueprint) EncounterOffers() []EncounterOffer {
	if b.Battle == nil {
		return nil
	}
	var out []EncounterOffer
	for _, d := range b.Battle.Rules.EncounterDefinitions {
		offer := EncounterOffer{Definition: cloneBattleEncounterDefinition(d)}
		intent, err := b.encounterFormation(d)
		if err == nil {
			err = b.checkBegin(intent, Ref{CharacterKind, string(b.Battle.Rules.PartyRoot)})
		}
		if err == nil {
			offer.Intent = &intent
		} else if f, ok := err.(*Fault); ok {
			copy := *f
			offer.Unavailable = &copy
		} else {
			offer.Unavailable = &Fault{Code: "E_ENCOUNTER", Subject: string(d.ID), Message: err.Error()}
		}
		out = append(out, offer)
	}
	return out
}
