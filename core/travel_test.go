package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

func travelFixture() core.Blueprint {
	b := battleFixture()
	b.Locations = append(b.Locations, core.Location{ID: "road", Name: "Road"})
	b.Battle.Rules.Routes = []core.TravelRoute{{From: "room", To: "road", Ticks: 600}, {From: "road", To: "room", Ticks: 600}}
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "b"}, To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", Strength: 1})
	b.Characters = append(b.Characters, core.Character{ID: "c", Name: "Bystander", Location: "room", Resources: map[core.ResourceID]core.Resource{"hp": {Current: 1, Maximum: 1}}})
	return b
}

func travel(to core.LocationID) core.Operation {
	return core.Operation{Travel: &core.PartyTravel{To: to}}
}

func TestNativeTravelAtomicHistoryAndOwnership(t *testing.T) {
	b := travelFixture()
	w := gearWorld(t, b)
	gearExecute(t, w, "ward", timed("travel-ward", 120))
	before := w.Snapshot()
	r := gearExecute(t, w, "depart", travel("road"))
	s := w.Snapshot()
	if s.Clock.Tick != 600 || s.Characters[0].Location != "road" || s.Characters[1].Location != "road" || s.Characters[2].Location != "room" || !reflect.DeepEqual(before.Items, s.Items) || !reflect.DeepEqual(before.Relationships, s.Relationships) || !reflect.DeepEqual(before.Characters[0].Resources, s.Characters[0].Resources) {
		t.Fatal("travel facts differ from fixed oracle")
	}
	want := []core.EventKind{core.EntityMoved, core.EntityMoved, core.ConditionRemoved, core.TimeAdvanced, core.PartyTraveled}
	if len(r.Events) != len(want) {
		t.Fatal(r.Events)
	}
	for i, kind := range want {
		if r.Events[i].Kind != kind {
			t.Fatal(i, r.Events[i])
		}
	}
	if r.Events[0].Movement.Character != "a" || r.Events[1].Movement.Character != "b" || r.Events[2].Condition.Before.ID != "travel-ward" {
		t.Fatal(r.Events)
	}
	gearExecute(t, w, "return", travel("room"))
	cp := w.ExportCheckpoint()
	historical, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(r, historical) || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("retry changed world", err)
	}
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
		t.Fatal("replay", err)
	}
	historical, err = rebuilt.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(historical, r) || rebuilt.Snapshot().Clock.Tick != 1200 {
		t.Fatal("reconstructed retry", err)
	}
	bad := w.History()
	bad[1].Events[0].Movement.After = "room"
	if partial, err := core.Replay(b, bad); err == nil || partial != nil {
		t.Fatal("corrupt travel returned world")
	}
	// Mutable inputs, snapshots and returned events are detached.
	b.Battle.Rules.Routes[0].Ticks = 1
	s.Battle.Rules.Routes[0].To = "room"
	r.Events[0].Movement.After = "room"
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("alias")
	}
}

func TestNativeTravelRejections(t *testing.T) {
	for _, name := range []string{"missing", "split", "down", "actor", "active", "victory", "late", "clock"} {
		t.Run(name, func(t *testing.T) {
			b := travelFixture()
			if name == "split" {
				b.Characters[1].Location = "road"
			}
			if name == "victory" {
				b.Battle.Rules.Routes[0].RequiresVictory = true
			}
			if name == "clock" {
				b.Clock.Tick = math.MaxUint64 - 10
			}
			if name == "active" {
				b.Relationships = nil
			}
			w := gearWorld(t, b)
			if name == "down" {
				gearExecute(t, w, "injury", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 30}})
			}
			if name == "active" {
				battleExec(t, w, "begin", "a", battleBegin())
			}
			r := gearRequest(w, "travel", travel("road"))
			if name == "missing" {
				r.Operations[0] = travel("room")
			}
			if name == "actor" {
				r.Actor.ID = "b"
			}
			if name == "late" {
				r.Operations = append(r.Operations, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 31}})
			}
			battleReject(t, w, r)
		})
	}
}

func TestNativeTravelVictoryAndDefinitionBounds(t *testing.T) {
	b := travelFixture()
	b.Relationships = nil
	b.Battle.Rules.Routes[0].RequiresVictory = true
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	gearExecute(t, w, "finish", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 30}}, core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 0, Actor: "a"}}})
	gearExecute(t, w, "depart", travel("road"))
	if w.Snapshot().Clock.Tick != 606 {
		t.Fatal(w.Snapshot().Clock)
	}
	for _, change := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Battle.Rules.Routes[0].To = "a" },
		func(b *core.Blueprint) { b.Battle.Rules.Routes[0].Ticks = 0 },
		func(b *core.Blueprint) { b.Battle.Rules.Routes[0].To = "room" },
		func(b *core.Blueprint) {
			b.Battle.Rules.Routes = append(b.Battle.Rules.Routes, b.Battle.Rules.Routes[0])
		},
	} {
		b := travelFixture()
		change(&b)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid route accepted")
		}
	}
}
