package core_test

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func authoredEncounterFixture() core.Blueprint {
	b := battleFixture()
	c := b.Characters[0]
	c.ID = "c"
	b.Characters = append(b.Characters, c)
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "c"}, To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", Strength: 1})
	b.Battle.Rules.EncounterDefinitions = []core.BattleEncounterDefinition{{ID: "gate", Name: "The guarded door", Location: "room", ProtectedPositions: []core.BattlePosition{{X: 0, Y: 0}, {X: 0, Y: 1}}, Opposition: []core.EncounterOpponent{{Character: "b", Position: core.BattlePosition{X: 1, Y: 0}}}}}
	return b
}

func TestAuthoredEncounterFormationOrderedIdentityAndReplay(t *testing.T) {
	b := authoredEncounterFixture()
	w := gearWorld(t, b)
	offered := w.Snapshot().Blueprint.EncounterOffers()
	want := core.BeginBattle{ID: "gate", Location: "room", Participants: []core.BattleAdmission{
		{Character: "a", Side: core.ProtectedSide, Controller: core.HumanController, Position: core.BattlePosition{X: 0, Y: 0}, TieRank: 0},
		{Character: "c", Side: core.ProtectedSide, Controller: core.AgentController, Position: core.BattlePosition{X: 0, Y: 1}, TieRank: 1},
		{Character: "b", Side: core.OppositionSide, Controller: core.AgentController, Position: core.BattlePosition{X: 1, Y: 0}, TieRank: 2},
	}}
	if len(offered) != 1 || offered[0].Unavailable != nil || !reflect.DeepEqual(offered[0].Intent, &want) {
		t.Fatal("authored formation differs from fixed oracle", offered)
	}
	for _, kind := range []string{"order", "omitted", "position", "tie", "controller", "id", "location"} {
		t.Run(kind, func(t *testing.T) {
			intent := *w.Snapshot().Blueprint.EncounterOffers()[0].Intent
			intent.Participants[0].Initiative = gearPtr(int64(15))
			switch kind {
			case "order":
				intent.Participants[1], intent.Participants[2] = intent.Participants[2], intent.Participants[1]
			case "omitted":
				intent.Participants = append(intent.Participants[:1], intent.Participants[2:]...)
				intent.Participants[1].TieRank = 1
			case "position":
				intent.Participants[1].Position.Y = 2
			case "tie":
				intent.Participants[1].TieRank, intent.Participants[2].TieRank = 2, 1
			case "controller":
				intent.Participants[1].Controller = core.HumanController
			case "id":
				intent.ID = "invented"
			case "location":
				intent.Location = "missing"
			}
			battleReject(t, w, core.Request{ID: "bad:" + kind, ExpectedRevision: 0, Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{{BeginBattle: &intent}}})
		})
	}
	want.Participants[0].Initiative = gearPtr(int64(15))
	receipt := battleExec(t, w, "begin", "a", core.Operation{BeginBattle: &want})
	if w.Revision() != 1 || len(receipt.Events) != 1 || receipt.Events[0].Kind != core.BattleStarted || w.Snapshot().Battle.State.Random.Calls != 2 {
		t.Fatal("one committed encounter or recorded draws changed", receipt)
	}
	if !reflect.DeepEqual(receipt.Events[0].BattleStarted.Intent, want) {
		t.Fatal("original admission intent lost")
	}
	if offers := w.Snapshot().Blueprint.EncounterOffers(); offers[0].Intent != nil || offers[0].Unavailable == nil {
		t.Fatal("active encounter offered again")
	}
	cp := w.ExportCheckpoint()
	restored, e := core.Replay(cp.Genesis, cp.History)
	if e != nil {
		t.Fatal(e)
	}
	again, e := restored.Execute(receipt.Request)
	if e != nil || !reflect.DeepEqual(again, receipt) || !reflect.DeepEqual(cp, restored.ExportCheckpoint()) {
		t.Fatal("replay or retry changed original encounter", e)
	}
}

func TestAuthoredEncounterAvailabilityDoesNotInvalidateWorld(t *testing.T) {
	b := authoredEncounterFixture()
	b.Locations = append(b.Locations, core.Location{ID: "away", Name: "Elsewhere"})
	w := gearWorld(t, b)
	battleExec(t, w, "move-opponent", "a", core.Operation{Move: &core.Move{Character: "b", To: "away"}})
	offer := w.Snapshot().Blueprint.EncounterOffers()[0]
	if offer.Intent != nil || offer.Unavailable == nil || offer.Definition.Opposition[0].Character != "b" {
		t.Fatal("absent opponent lost identity or remained available")
	}
	battleExec(t, w, "return", "a", core.Operation{Move: &core.Move{Character: "b", To: "room"}})
	battleExec(t, w, "recruit-opponent", "a", core.Operation{Relate: &core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "b"}, To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", Strength: 1}})
	if offer := w.Snapshot().Blueprint.EncounterOffers()[0]; offer.Intent != nil || offer.Unavailable == nil {
		t.Fatal("opponent now in party silently substituted/omitted")
	}
	// Current membership is an availability rule, not definition validity. The
	// unrelated committed actions above must remain replayable at this new party.
	cp := w.ExportCheckpoint()
	if _, e := core.Replay(cp.Genesis, cp.History); e != nil {
		t.Fatal(e)
	}
}

func TestAuthoredEncounterDefinitionsAndOffersDetached(t *testing.T) {
	b := authoredEncounterFixture()
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	b.Battle.Rules.EncounterDefinitions[0].ProtectedPositions[0].X = 999
	b.Battle.Rules.EncounterDefinitions[0].Opposition[0].Character = "changed"
	snapshot := w.Snapshot()
	first := snapshot.Blueprint.EncounterOffers()
	second := snapshot.Blueprint.EncounterOffers()
	first[0].Definition.Opposition[0].Character = "changed"
	first[0].Definition.ProtectedPositions[0].X = 999
	first[0].Intent.Participants[0].Character = "changed"
	snapshot.Battle.Rules.EncounterDefinitions[0].Opposition[0].Position.X = 900
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) || !reflect.DeepEqual(second, w.Snapshot().Blueprint.EncounterOffers()) {
		t.Fatal("nested encounter records alias")
	}
}

func TestAuthoredEncounterDefinitionNegatives(t *testing.T) {
	for _, kind := range []string{"id", "name", "location", "opponent", "duplicate", "square", "coordinate", "empty", "capacity"} {
		t.Run(kind, func(t *testing.T) {
			b := authoredEncounterFixture()
			d := &b.Battle.Rules.EncounterDefinitions[0]
			switch kind {
			case "id":
				d.ID = ""
			case "name":
				d.Name = ""
			case "location":
				d.Location = "a"
			case "opponent":
				d.Opposition[0].Character = "room"
			case "duplicate":
				b.Battle.Rules.EncounterDefinitions = append(b.Battle.Rules.EncounterDefinitions, *d)
			case "square":
				d.Opposition[0].Position = d.ProtectedPositions[0]
			case "coordinate":
				d.ProtectedPositions[0].Y = 1025
			case "empty":
				d.Opposition = nil
			case "capacity":
				d.ProtectedPositions = make([]core.BattlePosition, 16)
			}
			if w, e := core.NewWorld(b); e == nil || w != nil {
				t.Fatal("illegal authored declaration accepted")
			}
		})
	}
}
