package main

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

const mapWorld = `rules { transfer unequips; wear any; }
clock "minute" at 0;
location "loc:camp" named "Camp";
location "loc:ford" named "Ford";
location "loc:mill" named "Mill";
character "char:a" named "Ada" at "loc:camp" { }
map {
 route "loc:camp" to "loc:ford" ticks 10 both;
 route "loc:ford" to "loc:mill" ticks 5 both;
 party "char:a";
 hazard "loc:mill";
}
`

// The map view reads where a character can go; a travel request moves the
// party, and the returned checkpoint carries position, time and visits.
func TestMapViewAndTravel(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": mapWorld, "map": []string{"char:a"}})
	if !g.OK || g.Phase != "ready" || len(g.Map) != 1 {
		t.Fatalf("view: ok %v phase %s error %q map %+v", g.OK, g.Phase, g.Error, g.Map)
	}
	if d := g.Map[0].Destinations; len(d) != 1 || d[0].To.ID != "loc:ford" || d[0].Ticks != 10 {
		t.Fatalf("destinations before any visit %+v", d)
	}

	doc := map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": `travel party to "loc:ford";`, "actor": actor, "request": "host:go", "map": []string{"char:a"}}
	_, r := invoke(t, doc)
	if !r.OK || r.Phase != "executed" || r.Revision != 1 || r.Receipt == nil {
		t.Fatalf("travel: ok %v phase %s error %q", r.OK, r.Phase, r.Error)
	}
	events := r.Receipt.Events
	want := core.Trip{From: "loc:camp", Stops: []core.LocationID{"loc:ford"}, Ticks: 10, Visited: []core.LocationID{"loc:camp", "loc:ford"}}
	if trip := events[len(events)-1].Trip; trip == nil || !reflect.DeepEqual(*trip, want) {
		t.Fatalf("trip %+v, want %+v", trip, want)
	}
	m := r.Map[0]
	if m.Location.ID != "loc:ford" || m.Clock.Tick != 10 || len(m.Destinations) != 2 || m.Destinations[1].To.ID != "loc:mill" || m.Destinations[1].HaltsAt != nil {
		t.Fatalf("view after travel %+v", m)
	}
	if v := r.Checkpoint.Genesis.Map.Visited; len(v) != 0 {
		t.Fatalf("genesis map changed: %v", v)
	}

	// A historical retry repeats the receipt and does not travel again.
	doc["checkpoint"] = checkpointDoc(t, r.Checkpoint)
	_, retry := invoke(t, doc)
	if !retry.OK || !retry.Historical || retry.Revision != 1 || retry.Map[0].Clock.Tick != 10 {
		t.Fatalf("retry: ok %v historical %v rev %d", retry.OK, retry.Historical, retry.Revision)
	}

	// The state, when asked for, holds the visits.
	doc["include_state"] = true
	_, s := invoke(t, doc)
	if !s.OK || s.Checkpoint.State.Map == nil || !reflect.DeepEqual(s.Checkpoint.State.Map.Visited, want.Visited) {
		t.Fatalf("state: ok %v map %+v", s.OK, s.Checkpoint.State.Map)
	}
}

func TestMapViewNeedsAMap(t *testing.T) {
	_, r := invoke(t, map[string]any{"world": testWorld, "map": []string{"char:a"}})
	if r.OK || r.Fault == nil || r.Fault.Code != "E_MAP" || r.Fault.Field != "map" || r.Map != nil {
		t.Fatalf("map view of a world without a map: ok %v fault %+v", r.OK, r.Fault)
	}
	// After a commit the failing view is named and the commit kept.
	_, r = invoke(t, map[string]any{"world": mapWorld, "actions": `travel party to "loc:ford";`, "actor": map[string]any{"kind": "character", "id": "char:a"}, "request": "host:go", "map": []string{"char:a", "char:nobody"}})
	if !r.OK || r.ViewError == nil || r.ViewError.View != "map" || len(r.Map) != 1 {
		t.Fatalf("view error after travel: ok %v view error %+v map %d", r.OK, r.ViewError, len(r.Map))
	}
}
