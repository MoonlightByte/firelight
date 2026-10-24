package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

var occupantWorld = strings.Replace(mapWorld, ` party "char:a";`, ` party "char:a"; visited "loc:ford";`, 1) + `occupant "occ:wolves" named "Wolves" at "loc:ford" { creatures 2 to 4; attitude hostile; }
occupant "occ:mira" named "Mira" at "loc:camp" { person; attitude friendly; }
`

// A host reads who is at a place, changes occupants and the map by typed
// actions, and carries the result in the checkpoint it passes back.
func TestLocationsViewAndOccupantActions(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": occupantWorld, "locations": []string{"loc:ford", "loc:camp"}})
	if !g.OK || g.Phase != "ready" || len(g.Locations) != 2 {
		t.Fatalf("view: ok %v phase %s error %q locations %+v", g.OK, g.Phase, g.Error, g.Locations)
	}
	ford := g.Locations[0]
	if ford.Location.ID != "loc:ford" || len(ford.Present) != 1 || ford.Present[0].Range == nil || !*ford.Hazard || !*ford.Visited {
		t.Fatalf("ford %+v", ford)
	}
	if camp := g.Locations[1]; len(camp.Characters) != 1 || camp.Characters[0].ID != "char:a" || len(camp.Present) != 1 || *camp.Hazard {
		t.Fatalf("camp %+v", camp)
	}

	// The wolves at the ford stop a trip to the mill there.
	doc := map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": `travel party to "loc:mill";`, "actor": actor, "request": "host:go"}
	_, r := invoke(t, doc)
	if !r.OK || r.Receipt == nil {
		t.Fatalf("travel: ok %v error %q", r.OK, r.Error)
	}
	if trip := r.Receipt.Events[len(r.Receipt.Events)-1].Trip; trip == nil || !trip.Halted || trip.Stops[0] != "loc:ford" {
		t.Fatalf("trip %+v", trip)
	}

	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `count occupant "occ:wolves" as 3; resolve occupant "occ:wolves" as defeated by 2; resolve occupant "occ:wolves" as fled; move occupant "occ:mira" to "loc:ford"; clear hazard "loc:mill";`, "actor": actor, "request": "host:fight", "locations": []string{"loc:ford"}}
	_, r = invoke(t, doc)
	if !r.OK || r.Phase != "executed" || r.Revision != 2 {
		t.Fatalf("fight: ok %v phase %s error %q fault %+v", r.OK, r.Phase, r.Error, r.Fault)
	}
	kinds := []core.EventKind{}
	for _, e := range r.Receipt.Events {
		kinds = append(kinds, e.Kind)
	}
	if want := []core.EventKind{core.OccupantCounted, core.OccupantResolved, core.OccupantResolved, core.OccupantMoved, core.HazardCleared}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("events %v, want %v", kinds, want)
	}
	ford = r.Locations[0]
	tally := []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 2}, {Outcome: core.OutcomeFled, Count: 1}}
	if len(ford.Present) != 1 || ford.Present[0].ID != "occ:mira" || len(ford.Resolved) != 1 || !reflect.DeepEqual(ford.Resolved[0].Tally, tally) || *ford.Hazard {
		t.Fatalf("ford after the fight %+v", ford)
	}

	// The next call replays both requests from the checkpoint; the trip goes on.
	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `travel party to "loc:mill";`, "actor": actor, "request": "host:on", "include_state": true}
	_, r = invoke(t, doc)
	if !r.OK || r.Revision != 3 {
		t.Fatalf("on: ok %v error %q", r.OK, r.Error)
	}
	if got := r.Checkpoint.State.Occupants; len(got) != 2 || got[1].ID != "occ:wolves" || *got[1].Count != 0 || got[0].Location != "loc:ford" {
		t.Fatalf("state occupants %+v", got)
	}

	// An unknown place fails the view after the commit, which stands.
	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `set occupant "occ:mira" attitude indifferent;`, "actor": actor, "request": "host:talk", "locations": []string{"loc:ford", "loc:nowhere"}}
	_, r = invoke(t, doc)
	if !r.OK || r.Revision != 4 || r.ViewError == nil || r.ViewError.View != "locations" || len(r.Locations) != 1 {
		t.Fatalf("view error: ok %v view error %+v", r.OK, r.ViewError)
	}
	cp := r.Checkpoint
	// A refused change names its reason and commits nothing.
	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `resolve occupant "occ:wolves" as fled;`, "actor": actor, "request": "host:again"}
	_, r = invoke(t, doc)
	if r.OK || r.Fault == nil || r.Fault.Code != "E_OCCUPANT" || r.Fault.Field != "resolve" || r.Fault.Actual != "none" || r.Revision != 4 {
		t.Fatalf("refusal: ok %v fault %+v revision %d", r.OK, r.Fault, r.Revision)
	}

	// A departure says why. The reason rides in the checkpoint, and the next
	// call brings Mira back from that entry.
	doc = map[string]any{"checkpoint": checkpointDoc(t, cp), "actions": `resolve occupant "occ:mira" as departed because "went for help";`, "actor": actor, "request": "host:leave"}
	_, r = invoke(t, doc)
	if !r.OK || r.Revision != 5 {
		t.Fatalf("leave: ok %v error %q fault %+v", r.OK, r.Error, r.Fault)
	}
	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `return occupant "occ:mira" from departed "went for help";`, "actor": actor, "request": "host:back", "locations": []string{"loc:ford"}}
	_, r = invoke(t, doc)
	if !r.OK || r.Revision != 6 || len(r.Receipt.Events) != 1 || r.Receipt.Events[0].Kind != core.OccupantReturned {
		t.Fatalf("back: ok %v error %q fault %+v", r.OK, r.Error, r.Fault)
	}
	raw, err := json.Marshal(r.Receipt.Events[0].Occupant.Before.Tally)
	if err != nil || string(raw) != `[{"outcome":"departed","count":1,"reason":"went for help"}]` {
		t.Fatalf("tally %s %v", raw, err)
	}
	if ford := r.Locations[0]; len(ford.Present) != 1 || ford.Present[0].ID != "occ:mira" || ford.Present[0].Tally != nil {
		t.Fatalf("ford %+v", ford)
	}
	if v := r.Receipt.Events[0].Occupant.Intent.Return; v == nil || v.Outcome != core.OutcomeDeparted || v.Reason != "went for help" {
		t.Fatalf("return intent %+v", v)
	}
	// The next call replays the return from the checkpoint.
	doc = map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `set occupant "occ:mira" attitude friendly;`, "actor": actor, "request": "host:thanks", "locations": []string{"loc:ford"}}
	_, r = invoke(t, doc)
	if !r.OK || r.Revision != 7 || len(r.Locations[0].Present) != 1 {
		t.Fatalf("after the return: ok %v error %q fault %+v", r.OK, r.Error, r.Fault)
	}
}
