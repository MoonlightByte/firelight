package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/livestate"
)

var liveActor = map[string]any{"kind": "character", "id": "char:a"}

// liveDoc is a document as a host stores it: through JSON.
func liveDoc(t *testing.T, d *livestate.Document) map[string]any {
	t.Helper()
	if d == nil {
		t.Fatal("no live state")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	return generic
}

// withoutMap is a world source with its map block taken out.
func withoutMap(src string) string {
	i := strings.Index(src, "map {")
	j := strings.Index(src[i:], "}\n")
	return src[:i] + src[i+j+2:]
}

func liveOccupant(t *testing.T, d *livestate.Document, id core.OccupantID) core.Occupant {
	t.Helper()
	for _, o := range d.Occupants {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("occupant %s missing from %+v", id, d.Occupants)
	return core.Occupant{}
}

// The documents in docs/LIVE_STATE.md are what the engine writes for the
// occupants example and the quest example, field for field, checksums and
// digests included.
func TestLiveStateGuideExamples(t *testing.T) {
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	guide := read("../../docs/LIVE_STATE.md")
	var shown []any
	for _, block := range strings.Split(guide, "```json\n")[1:] {
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(block, "```", 2)[0]), &v); err != nil {
			t.Fatal(err)
		}
		shown = append(shown, v)
	}
	if len(shown) != 4 {
		t.Fatalf("%d examples in the guide", len(shown))
	}
	call := func(doc map[string]any) map[string]any {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if code := run(bytes.NewReader(raw), &out); code != 0 {
			t.Fatalf("exit %d: %s", code, out.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	world := read("../../examples/occupants/world.nql")
	first := call(map[string]any{"world": world, "new_live_state": true})
	if !reflect.DeepEqual(first["live_state"], shown[0]) {
		t.Fatalf("start:\nengine %v\nguide  %v", first["live_state"], shown[0])
	}
	second := call(map[string]any{"world": world, "live_state": shown[0], "actions": read("../../examples/occupants/fight.nql"), "actor": map[string]any{"kind": "character", "id": "char:ada"}, "request": "turn:12"})
	if !reflect.DeepEqual(second["live_state"], shown[1]) {
		t.Fatalf("after the fight:\nengine %v\nguide  %v", second["live_state"], shown[1])
	}
	third := call(map[string]any{"world": world, "live_state": shown[1], "actions": read("../../examples/occupants/trip.nql"), "actor": map[string]any{"kind": "character", "id": "char:ada"}, "request": "turn:13"})
	if !reflect.DeepEqual(third["live_state"], shown[2]) {
		t.Fatalf("after the trip:\nengine %v\nguide  %v", third["live_state"], shown[2])
	}
	// The trip's time is the host's to write back: 28,800 to 30,000.
	events := third["receipt"].(map[string]any)["events"].([]any)
	if time := events[2].(map[string]any)["time"].(map[string]any); time["before"] != 28800.0 || time["after"] != 30000.0 {
		t.Fatalf("trip time %v", time)
	}
	quests := read("../../examples/quests/world.nql")
	start := call(map[string]any{"world": quests, "new_live_state": true})
	fourth := call(map[string]any{"world": quests, "live_state": start["live_state"], "actions": read("../../examples/quests/start.nql"), "actor": map[string]any{"kind": "character", "id": "char:ada"}, "request": "turn:1"})
	if !reflect.DeepEqual(fourth["live_state"], shown[3]) {
		t.Fatalf("quests:\nengine %v\nguide  %v", fourth["live_state"], shown[3])
	}
}

// A host that writes a fresh world each call starts a document from the
// seeds, passes it back, and gets the occupants as play left them.
func TestLiveStateHostFlow(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": occupantWorld, "new_live_state": true, "locations": []string{"loc:ford"}})
	d := g.LiveState
	if !g.OK || g.Phase != "ready" || d == nil || d.Format != "nql-live-state" || d.Version != 5 || d.Revision != 0 || len(d.Requests) != 0 || !strings.HasPrefix(d.Checksum, "sha256:") {
		t.Fatalf("start: ok %v phase %s error %q live %+v", g.OK, g.Phase, g.Error, d)
	}
	if want := []core.LocationID{"loc:camp", "loc:ford", "loc:mill"}; !reflect.DeepEqual(d.Places, want) || len(d.Occupants) != 2 || d.Occupants[0].ID != "occ:mira" || d.Occupants[1].Range == nil {
		t.Fatalf("seeded %+v", d)
	}
	if len(g.Locations) != 1 || len(g.Locations[0].Present) != 1 || !*g.Locations[0].Hazard {
		t.Fatalf("ford %+v", g.Locations)
	}

	// A fight changes the wolves; the request is kept at revision 1.
	fight := map[string]any{"world": occupantWorld, "live_state": liveDoc(t, d), "actions": `count occupant "occ:wolves" as 3; resolve occupant "occ:wolves" as departed by 2 because "smelled smoke";`, "actor": liveActor, "request": "host:fight", "locations": []string{"loc:ford"}}
	_, r := invoke(t, fight)
	if !r.OK || r.Phase != "executed" || r.Receipt == nil || r.Historical || r.LiveState == nil {
		t.Fatalf("fight: ok %v phase %s error %q fault %+v", r.OK, r.Phase, r.Error, r.Fault)
	}
	d = r.LiveState
	if d.Revision != 1 || len(d.Requests) != 1 || d.Requests[0].ID != "host:fight" || d.Requests[0].Revision != 1 || len(d.Requests[0].Digest) != 64 {
		t.Fatalf("after the fight %+v", d)
	}
	wolves := liveOccupant(t, d, "occ:wolves")
	if *wolves.Count != 1 || !reflect.DeepEqual(wolves.Tally, []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 2, Reason: "smelled smoke"}}) {
		t.Fatalf("wolves %+v", wolves)
	}

	// The source changes: the wolves' seed and a new seed at the ford are
	// ignored, since the document holds the ford; a new place is taken with
	// its seed. A call without a request keeps the revision.
	changed := strings.Replace(occupantWorld, "creatures 2 to 4;", "creatures 7;", 1) +
		`occupant "occ:bears" named "Bears" at "loc:ford" { creatures 2; attitude hostile; }
location "loc:keep" named "Keep";
occupant "occ:keep/rats" named "Rats" at "loc:keep" { creatures 5; attitude hostile; }
`
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "locations": []string{"loc:ford", "loc:keep"}})
	if !r.OK || r.LiveState == nil {
		t.Fatalf("new module: ok %v error %q fault %+v", r.OK, r.Error, r.Fault)
	}
	if !reflect.DeepEqual(liveOccupant(t, r.LiveState, "occ:wolves"), wolves) || len(r.LiveState.Occupants) != 3 || r.LiveState.Revision != 1 || len(r.LiveState.Places) != 4 {
		t.Fatalf("after the new module %+v", r.LiveState)
	}
	if ford, keep := r.Locations[0], r.Locations[1]; len(ford.Present) != 1 || ford.Present[0].ID != "occ:wolves" || len(keep.Present) != 1 || keep.Present[0].ID != "occ:keep/rats" {
		t.Fatalf("views %+v", r.Locations)
	}
	d = r.LiveState

	// A retry after a crash is answered as already applied: no receipt, no
	// change, the views of now.
	fight["world"], fight["live_state"] = changed, liveDoc(t, d)
	_, r = invoke(t, fight)
	if !r.OK || r.Phase != "executed" || !r.Historical || r.Receipt != nil || r.Checkpoint == nil || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("retry: ok %v phase %s historical %v receipt %v error %q live %+v", r.OK, r.Phase, r.Historical, r.Receipt != nil, r.Error, r.LiveState)
	}
	if len(r.Locations) != 1 || *r.Locations[0].Present[0].Count != 1 {
		t.Fatalf("retry view %+v", r.Locations)
	}
	// The same ID with other content is refused, as the core refuses it.
	reuse := map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `set occupant "occ:wolves" attitude indifferent;`, "actor": liveActor, "request": "host:fight"}
	_, r = invoke(t, reuse)
	if r.OK || r.Phase != "execute" || r.Fault == nil || r.Fault.Code != "E_REQUEST_REUSE" || r.Fault.Subject != "host:fight" || r.LiveState != nil || r.Checkpoint != nil {
		t.Fatalf("reuse: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	// So is the same ID from another actor.
	reuse["actions"], reuse["actor"] = fight["actions"], map[string]any{"kind": "character", "id": "char:b"}
	_, r = invoke(t, reuse)
	if r.OK || r.Fault == nil || r.Fault.Code != "E_REQUEST_REUSE" {
		t.Fatalf("other actor: ok %v fault %+v", r.OK, r.Fault)
	}

	// Time is kept: a wait moves the document's clock, and its retry is
	// answered without moving it again.
	wait := map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `advance time by 5;`, "actor": liveActor, "request": "host:wait"}
	_, r = invoke(t, wait)
	if !r.OK || r.Receipt == nil || r.LiveState.Revision != 2 || r.LiveState.Clock.Tick != d.Clock.Tick+5 || r.LiveState.Requests[1].ID != "host:wait" || !reflect.DeepEqual(r.LiveState.Occupants, d.Occupants) {
		t.Fatalf("wait: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	wait["live_state"] = liveDoc(t, r.LiveState)
	_, again := invoke(t, wait)
	if !again.OK || !again.Historical || again.Receipt != nil || !reflect.DeepEqual(again.LiveState, r.LiveState) {
		t.Fatalf("wait again: ok %v historical %v live %+v", again.OK, again.Historical, again.LiveState)
	}
	// A trip is kept: the party stands at the ford, which is visited.
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `travel party to "loc:ford";`, "actor": liveActor, "request": "host:go"})
	if !r.OK || r.Receipt == nil || r.LiveState.Revision != 2 || !reflect.DeepEqual(r.LiveState.Characters, []livestate.Character{{ID: "char:a", Location: "loc:ford"}}) || r.LiveState.Requests[1].ID != "host:go" {
		t.Fatalf("travel: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	// Without a map the characters are the source's: a move is not kept.
	mapless := withoutMap(changed)
	_, n := invoke(t, map[string]any{"world": mapless, "new_live_state": true})
	_, m := invoke(t, map[string]any{"world": mapless, "live_state": liveDoc(t, n.LiveState), "actions": `move "char:a" to "loc:ford";`, "actor": liveActor, "request": "host:walk"})
	if !m.OK || m.Receipt == nil || n.LiveState.Characters != nil || n.LiveState.Map != nil || !reflect.DeepEqual(m.LiveState, n.LiveState) {
		t.Fatalf("move without a map: ok %v error %q live %+v", m.OK, m.Error, m.LiveState)
	}
	// The source's next place for the character is ignored.
	moved := strings.Replace(changed, `character "char:a" named "Ada" at "loc:camp"`, `character "char:a" named "Ada" at "loc:mill"`, 1)
	_, r2 := invoke(t, map[string]any{"world": moved, "live_state": liveDoc(t, r.LiveState), "locations": []string{"loc:ford"}})
	if !r2.OK || len(r2.Locations[0].Characters) != 1 || !reflect.DeepEqual(r2.LiveState, r.LiveState) {
		t.Fatalf("held character: ok %v error %q locations %+v", r2.OK, r2.Error, r2.Locations)
	}

	// A failing view after a retry keeps the answer and the document.
	fight["locations"] = []string{"loc:ford", "loc:nowhere"}
	_, r = invoke(t, fight)
	if !r.OK || !r.Historical || r.ViewError == nil || r.ViewError.View != "locations" || !reflect.DeepEqual(r.LiveState, d) || len(r.Locations) != 1 {
		t.Fatalf("retry with a failing view: ok %v view error %+v", r.OK, r.ViewError)
	}
	// A failing view after a commit keeps the commit in the document.
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `set occupant "occ:mira" attitude indifferent;`, "actor": liveActor, "request": "host:talk", "locations": []string{"loc:nowhere"}})
	if !r.OK || r.ViewError == nil || r.LiveState == nil || r.LiveState.Revision != 2 || liveOccupant(t, r.LiveState, "occ:mira").Attitude != core.AttitudeIndifferent {
		t.Fatalf("commit with a failing view: ok %v view error %+v live %+v", r.OK, r.ViewError, r.LiveState)
	}
	// Without a request a failing view fails the call, with no document.
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "locations": []string{"loc:nowhere"}})
	if r.OK || r.Phase != "locations" || r.LiveState != nil {
		t.Fatalf("read-only view failure: ok %v phase %s", r.OK, r.Phase)
	}
	// A refused request returns no document; the host keeps its own.
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `count occupant "occ:wolves" as 2;`, "actor": liveActor, "request": "host:recount"})
	if r.OK || r.Phase != "execute" || r.Fault == nil || r.Fault.Code != "E_OCCUPANT" || r.LiveState != nil || r.Checkpoint != nil {
		t.Fatalf("refused: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	_, r = invoke(t, map[string]any{"world": changed, "live_state": liveDoc(t, d), "actions": `count occupant "occ:nobody" as 2;`, "actor": liveActor, "request": "host:recount"})
	if r.OK || r.Phase != "compile" || r.LiveState != nil {
		t.Fatalf("compile refusal: ok %v phase %s", r.OK, r.Phase)
	}
}

// The document keeps the last 64 requests that changed an occupant. An older
// one is no longer recognised and applies again.
func TestLiveStateRecentRequests(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": occupantWorld, "new_live_state": true})
	d := g.LiveState
	attitudes := []string{"friendly", "indifferent"}
	for i := 1; i <= livestate.MaxRequests+1; i++ {
		_, r := invoke(t, map[string]any{"world": occupantWorld, "live_state": liveDoc(t, d), "actions": `set occupant "occ:mira" attitude ` + attitudes[i%2] + `;`, "actor": liveActor, "request": fmt.Sprintf("host:%d", i)})
		if !r.OK || r.LiveState == nil {
			t.Fatalf("request %d: ok %v error %q", i, r.OK, r.Error)
		}
		d = r.LiveState
	}
	if d.Revision != 65 || len(d.Requests) != 64 || d.Requests[0].ID != "host:2" || d.Requests[0].Revision != 2 || d.Requests[63].ID != "host:65" || d.Requests[63].Revision != 65 {
		t.Fatalf("window: revision %d requests %d first %+v last %+v", d.Revision, len(d.Requests), d.Requests[0], d.Requests[len(d.Requests)-1])
	}
	// host:2 set friendly; it is still known, so nothing changes.
	_, r := invoke(t, map[string]any{"world": occupantWorld, "live_state": liveDoc(t, d), "actions": `set occupant "occ:mira" attitude friendly;`, "actor": liveActor, "request": "host:2"})
	if !r.OK || !r.Historical || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("known: ok %v historical %v error %q", r.OK, r.Historical, r.Error)
	}
	// host:1 set indifferent; it fell out of the window, so it runs again:
	// Mira is indifferent after host:65, so it is refused as no change.
	_, r = invoke(t, map[string]any{"world": occupantWorld, "live_state": liveDoc(t, d), "actions": `set occupant "occ:mira" attitude indifferent;`, "actor": liveActor, "request": "host:1"})
	if r.OK || r.Fault == nil || r.Fault.Code != "E_NO_CHANGE" {
		t.Fatalf("forgotten: ok %v fault %+v", r.OK, r.Fault)
	}
	_, r = invoke(t, map[string]any{"world": occupantWorld, "live_state": liveDoc(t, d), "actions": `set occupant "occ:mira" attitude hostile;`, "actor": liveActor, "request": "host:1"})
	if !r.OK || r.Historical || r.LiveState.Revision != 66 || r.LiveState.Requests[63].ID != "host:1" || r.LiveState.Requests[0].ID != "host:3" {
		t.Fatalf("forgotten, applied again: ok %v error %q", r.OK, r.Error)
	}
}

func TestLiveStateRefusals(t *testing.T) {
	world := occupantWorld + `location "loc:keep" named "Keep";` + "\n"
	_, g := invoke(t, map[string]any{"world": world, "new_live_state": true})
	_, f := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, g.LiveState), "actions": `resolve occupant "occ:mira" as departed because "went home"; create occupant "occ:guide" named "Guide" at "loc:camp" { person; attitude friendly; };`, "actor": liveActor, "request": "host:1"})
	if !f.OK {
		t.Fatalf("setup: %q", f.Error)
	}
	d := f.LiveState

	for name, doc := range map[string]map[string]any{
		"both":                   {"world": world, "live_state": liveDoc(t, d), "new_live_state": true},
		"with a checkpoint":      {"checkpoint": checkpointDoc(t, g.Checkpoint), "live_state": liveDoc(t, d)},
		"new with a checkpoint":  {"checkpoint": checkpointDoc(t, g.Checkpoint), "new_live_state": true},
		"without a world":        {"live_state": liveDoc(t, d)},
		"new without a world":    {"new_live_state": true},
		"with world, checkpoint": {"world": world, "checkpoint": checkpointDoc(t, g.Checkpoint), "live_state": liveDoc(t, d)},
	} {
		if _, r := invoke(t, doc); r.OK || r.Phase != "input" || r.LiveState != nil {
			t.Fatalf("%s: ok %v phase %s", name, r.OK, r.Phase)
		}
	}

	refused := func(name string, edit func(m map[string]any), world, field, actual string) {
		t.Helper()
		m := liveDoc(t, d)
		edit(m)
		_, r := invoke(t, map[string]any{"world": world, "live_state": m, "locations": []string{"loc:camp"}})
		if r.OK || r.Phase != "live_state" || r.Fault == nil || r.Fault.Field != field || r.Fault.Actual != actual || r.LiveState != nil || r.Checkpoint != nil || r.Locations != nil {
			t.Fatalf("%s: ok %v phase %s fault %+v", name, r.OK, r.Phase, r.Fault)
		}
	}
	none := func(map[string]any) {}
	refused("format", func(m map[string]any) { m["format"] = "nql-checkpoint" }, world, "format", "nql-checkpoint")
	refused("version", func(m map[string]any) { m["version"] = 6 }, world, "version", "6")
	refused("no checksum", func(m map[string]any) { delete(m, "checksum") }, world, "checksum", "absent")
	refused("hand edit", func(m map[string]any) {
		m["occupants"].([]any)[0].(map[string]any)["attitude"] = "hostile"
	}, world, "checksum", "mismatch")
	refused("forgotten request", func(m map[string]any) { m["requests"] = []any{} }, world, "checksum", "mismatch")
	refused("place gone", none, occupantWorld, "place", "not declared")
	partyless := strings.Replace(world, `character "char:a" named "Ada" at "loc:camp" { }`, "", 1)
	partyless = strings.Replace(partyless, ` party "char:a";`, "", 1)
	refused("party member gone", none, partyless, "party", "not declared")
	// A world without the map is no refusal: it holds the map (TestLiveStateHeldMap).
	mapless := withoutMap(world)
	if _, r := invoke(t, map[string]any{"world": mapless, "live_state": liveDoc(t, d)}); !r.OK || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("map gone: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	refused("character moved by hand", func(m map[string]any) {
		m["characters"].([]any)[0].(map[string]any)["location"] = "loc:ford"
	}, world, "checksum", "mismatch")

	// The source decides whose fault genesis refuses: a world without a
	// clock cannot hold the document's occupants.
	clockless := `rules { transfer unequips; wear any; }
location "loc:camp" named "Camp";
location "loc:ford" named "Ford";
location "loc:mill" named "Mill";
location "loc:keep" named "Keep";
character "char:a" named "Ada" at "loc:camp" { }
`
	_, n := invoke(t, map[string]any{"world": mapless, "new_live_state": true})
	_, r := invoke(t, map[string]any{"world": clockless, "live_state": liveDoc(t, n.LiveState)})
	if n.LiveState == nil || n.LiveState.Map != nil || r.OK || r.Phase != "live_state" || r.Fault == nil || r.Fault.Code != "E_OCCUPANT" || r.Fault.Field != "clock" {
		t.Fatalf("clockless: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	// A new place's seed that takes an ID in play is the document's conflict.
	dup := world + `location "loc:hut" named "Hut";
occupant "occ:guide" named "Guide" at "loc:hut" { person; attitude friendly; }
`
	_, r = invoke(t, map[string]any{"world": dup, "live_state": liveDoc(t, d)})
	if r.OK || r.Phase != "live_state" || r.Fault == nil || r.Fault.Code != "E_DUPLICATE" || r.Fault.Subject != "occ:guide" {
		t.Fatalf("duplicate: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	// A source genesis refuses on its own is the source's fault.
	defer func(f func(core.Blueprint) (*core.World, error)) { newWorld = f }(newWorld)
	calls := 0
	newWorld = func(core.Blueprint) (*core.World, error) {
		calls++
		return nil, &core.Fault{Code: "E_TEST", Message: "refused"}
	}
	_, r = invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, d)})
	if r.OK || r.Phase != "genesis" || r.Fault == nil || r.Fault.Code != "E_TEST" || calls != 2 {
		t.Fatalf("source fault: ok %v phase %s fault %+v calls %d", r.OK, r.Phase, r.Fault, calls)
	}
	// Without live state genesis is tried once.
	calls = 0
	if _, r = invoke(t, map[string]any{"world": world}); r.Phase != "genesis" || calls != 1 {
		t.Fatalf("plain genesis: phase %s calls %d", r.Phase, calls)
	}

	// A field the engine does not write is not a document.
	m := liveDoc(t, d)
	m["note"] = "x"
	if code, r := invoke(t, map[string]any{"world": world, "live_state": m}); code != 2 || r.Phase != "input" {
		t.Fatalf("unknown field: code %d phase %s", code, r.Phase)
	}
}

// The same calls give the same answers and the same places whether the host
// keeps the checkpoint or writes a fresh world and passes the live state.
func TestLiveStateMatchesKeptWorld(t *testing.T) {
	places := []string{"loc:camp", "loc:ford", "loc:mill"}
	ids := []string{"occ:wolves", "occ:mira", "occ:n0", "occ:n1", "occ:n2"}
	outcomes := []string{"defeated", "fled", "departed", "joined", "died"}
	reasons := []string{"", "fire", "gold"}
	names := []string{"Ann", "Bo", "Cy", "Di"}
	attitudes := []string{"friendly", "indifferent", "hostile"}
	rng := rand.New(rand.NewSource(44))
	pick := func(list []string) string { return list[rng.Intn(len(list))] }
	by := func() string {
		if rng.Intn(2) == 0 {
			return ""
		}
		return fmt.Sprintf(" by %d", rng.Intn(4))
	}
	statement := func() string {
		id := pick(ids)
		switch rng.Intn(7) {
		case 0:
			body := []string{"person;", fmt.Sprintf("creatures %d;", 1+rng.Intn(4)), "creatures;", "creatures 1 to 3;"}[rng.Intn(4)]
			return fmt.Sprintf(`create occupant %q named %q at %q { %s attitude %s; };`, id, pick(names), pick(places), body, pick(attitudes))
		case 1:
			return fmt.Sprintf(`move occupant %q to %q;`, id, pick(places))
		case 2:
			return fmt.Sprintf(`count occupant %q as %d;`, id, rng.Intn(5))
		case 3:
			outcome, why := pick(outcomes), pick(reasons)
			if outcome == "departed" && why == "" && rng.Intn(4) > 0 {
				why = "fire"
			}
			if why != "" {
				why = fmt.Sprintf(" because %q", why)
			}
			return fmt.Sprintf(`resolve occupant %q as %s%s%s;`, id, outcome, by(), why)
		case 4:
			return fmt.Sprintf(`set occupant %q attitude %s;`, id, pick(attitudes))
		case 5:
			return fmt.Sprintf(`rename occupant %q to %q;`, id, pick(names))
		}
		why := pick(reasons)
		if why != "" {
			why = fmt.Sprintf(" %q", why)
		}
		return fmt.Sprintf(`return occupant %q from %s%s%s;`, id, pick(outcomes), why, by())
	}
	answers := map[string]int{}
	type sent struct{ id, actions string }
	for round := 0; round < 150; round++ {
		a := apply(input{World: occupantWorld, NewLiveState: true})
		b := apply(input{World: occupantWorld})
		if !a.OK || !b.OK {
			t.Fatal(a.Error, b.Error)
		}
		doc, cp := a.LiveState, b.Checkpoint
		var kept, refused []sent
		for step := 0; step < 40; step++ {
			s := sent{id: fmt.Sprintf("r%d", step)}
			for k := 0; k < 1+rng.Intn(3); k++ {
				s.actions += statement()
			}
			switch n := rng.Intn(10); {
			case n == 0 && len(kept) > 0:
				// An exact retry of a recent request.
				s = kept[max(0, len(kept)-livestate.MaxRequests)+rng.Intn(min(len(kept), livestate.MaxRequests))]
			case n == 1 && len(kept) > 0:
				// A kept ID with other content.
				s.id = kept[rng.Intn(len(kept))].id
			case n == 2 && len(refused) > 0:
				// A refused request's ID, free again.
				s.id = refused[rng.Intn(len(refused))].id
			}
			// The live document goes through JSON as a host stores it.
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var stored livestate.Document
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
			actor := core.Ref{Kind: core.CharacterKind, ID: "char:a"}
			ra := apply(input{World: occupantWorld, LiveState: &stored, Actions: s.actions, Actor: &actor, Request: s.id, Locations: []core.LocationID{"loc:camp", "loc:ford", "loc:mill"}})
			rb := apply(input{Checkpoint: cp, Actions: s.actions, Actor: &actor, Request: s.id, Locations: []core.LocationID{"loc:camp", "loc:ford", "loc:mill"}})
			where := fmt.Sprintf("round %d step %d request %s %q", round, step, s.id, s.actions)
			if ra.OK != rb.OK || ra.Phase != rb.Phase || ra.Historical != rb.Historical || ra.Error != rb.Error || !reflect.DeepEqual(ra.Fault, rb.Fault) || !reflect.DeepEqual(ra.Diagnostics, rb.Diagnostics) || ra.ViewError != nil || rb.ViewError != nil {
				t.Fatalf("%s:\nlive ok %v phase %s historical %v error %q fault %+v diagnostics %+v\nkept ok %v phase %s historical %v error %q fault %+v diagnostics %+v", where, ra.OK, ra.Phase, ra.Historical, ra.Error, ra.Fault, ra.Diagnostics, rb.OK, rb.Phase, rb.Historical, rb.Error, rb.Fault, rb.Diagnostics)
			}
			for i := range ra.Locations {
				ra.Locations[i].Revision = 0
			}
			for i := range rb.Locations {
				rb.Locations[i].Revision = 0
			}
			if !reflect.DeepEqual(ra.Locations, rb.Locations) {
				t.Fatalf("%s: places differ\nlive %+v\nkept %+v", where, ra.Locations, rb.Locations)
			}
			switch {
			case !ra.OK:
				answers["refused "+ra.Phase]++
				if ra.LiveState != nil {
					t.Fatalf("%s: refused with a document", where)
				}
				refused = append(refused, s)
				continue
			case ra.Historical:
				answers["already applied"]++
				if !reflect.DeepEqual(ra.LiveState, doc) {
					t.Fatalf("%s: a retry changed the document", where)
				}
			default:
				answers["applied"]++
				var liveEvents, keptEvents []core.OccupantChange
				for _, e := range ra.Receipt.Events {
					liveEvents = append(liveEvents, *e.Occupant)
				}
				for _, e := range rb.Receipt.Events {
					keptEvents = append(keptEvents, *e.Occupant)
				}
				if !reflect.DeepEqual(liveEvents, keptEvents) {
					t.Fatalf("%s: events differ", where)
				}
				if ra.LiveState.Revision != doc.Revision+1 || ra.LiveState.Requests[len(ra.LiveState.Requests)-1].ID != s.id {
					t.Fatalf("%s: request not kept: %+v", where, ra.LiveState.Requests)
				}
			}
			if ra.OK && !ra.Historical {
				kept = append(kept, s)
			}
			doc, cp = ra.LiveState, rb.Checkpoint
		}
		// Every occupant record is the same.
		final := apply(input{Checkpoint: cp, IncludeState: true})
		if !reflect.DeepEqual(doc.Occupants, append([]core.Occupant{}, final.Checkpoint.State.Occupants...)) {
			t.Fatalf("round %d: occupants differ\nlive %+v\nkept %+v", round, doc.Occupants, final.Checkpoint.State.Occupants)
		}
	}
	t.Logf("answers %v", answers)
	for _, k := range []string{"applied", "already applied", "refused execute", "refused compile"} {
		if answers[k] < 50 {
			t.Fatalf("too few %q: %v", k, answers)
		}
	}
}

// liveMapWorld is a map of four places and two characters, its clock at any
// tick.
func liveMapWorld(clock int64) string {
	return fmt.Sprintf(`rules { transfer unequips; wear any; }
clock "minute" at %d;
location "loc:camp" named "Camp";
location "loc:ford" named "Ford";
location "loc:mill" named "Mill";
location "loc:keep" named "Keep";
character "char:a" named "Ada" at "loc:camp" { }
character "char:b" named "Bo" at "loc:camp" { }
occupant "occ:wolves" named "Wolves" at "loc:ford" { creatures 2 to 4; attitude hostile; }
occupant "occ:mira" named "Mira" at "loc:camp" { person; attitude friendly; }
map {
 route "loc:camp" to "loc:ford" ticks 10 both;
 route "loc:ford" to "loc:mill" ticks 5 both;
 route "loc:mill" to "loc:keep" ticks 7 both;
 route "loc:camp" to "loc:keep" ticks 30;
 party "char:a";
 visited "loc:ford";
 hazard "loc:mill";
}
`, clock)
}

// Trips, moves and map edits give the same answers, places, map views and
// final state whether the host keeps the checkpoint or writes a fresh world
// and passes the live state. The fresh world's clock says any tick: the
// document's clock is the one that runs.
func TestLiveStateMapMatchesKeptWorld(t *testing.T) {
	places := []string{"loc:camp", "loc:ford", "loc:mill", "loc:keep"}
	chars := []string{"char:a", "char:b"}
	rng := rand.New(rand.NewSource(45))
	noise := rand.New(rand.NewSource(7))
	pick := func(list []string) string { return list[rng.Intn(len(list))] }
	route := func() string {
		r := [][2]string{{"loc:camp", "loc:ford"}, {"loc:ford", "loc:camp"}, {"loc:ford", "loc:mill"}, {"loc:mill", "loc:ford"}, {"loc:mill", "loc:keep"}, {"loc:keep", "loc:mill"}, {"loc:camp", "loc:keep"}, {"loc:keep", "loc:camp"}}[rng.Intn(8)]
		return fmt.Sprintf("%q to %q", r[0], r[1])
	}
	statement := func() string {
		switch rng.Intn(13) {
		case 0, 1, 2:
			return fmt.Sprintf(`travel party to %q;`, pick(places))
		case 3:
			return fmt.Sprintf(`move %q to %q;`, pick(chars), pick(places))
		case 4:
			return fmt.Sprintf(`join party %q;`, pick(chars))
		case 5:
			return fmt.Sprintf(`leave party %q;`, pick(chars))
		case 6:
			return fmt.Sprintf(`close route %s;`, route())
		case 7:
			return fmt.Sprintf(`open route %s;`, route())
		case 8:
			return fmt.Sprintf(`mark hazard %q;`, pick(places))
		case 9:
			return fmt.Sprintf(`clear hazard %q;`, pick(places))
		case 10:
			return fmt.Sprintf(`advance time by %d;`, 1+rng.Intn(9))
		case 11:
			return fmt.Sprintf(`mark visited %q;`, pick(places))
		}
		return []string{`resolve occupant "occ:wolves" as fled;`, `return occupant "occ:wolves" from fled;`, `set occupant "occ:mira" attitude hostile;`, `set occupant "occ:mira" attitude friendly;`}[rng.Intn(4)]
	}
	zero := func(events []core.Event) []core.Event {
		out := append([]core.Event(nil), events...)
		for i := range out {
			out[i].ID, out[i].Revision = "", 0
		}
		return out
	}
	answers := map[string]int{}
	type sent struct{ id, actions string }
	for round := 0; round < 120; round++ {
		clock := int64(0)
		a := apply(input{World: liveMapWorld(clock), NewLiveState: true})
		b := apply(input{World: liveMapWorld(clock)})
		if !a.OK || !b.OK {
			t.Fatal(a.Error, b.Error)
		}
		doc, cp := a.LiveState, b.Checkpoint
		var kept, refused []sent
		for step := 0; step < 40; step++ {
			s := sent{id: fmt.Sprintf("r%d", step)}
			for k := 0; k < 1+rng.Intn(3); k++ {
				s.actions += statement()
			}
			switch n := rng.Intn(10); {
			case n == 0 && len(kept) > 0:
				s = kept[max(0, len(kept)-livestate.MaxRequests)+rng.Intn(min(len(kept), livestate.MaxRequests))]
			case n == 1 && len(kept) > 0:
				s.id = kept[rng.Intn(len(kept))].id
			case n == 2 && len(refused) > 0:
				s.id = refused[rng.Intn(len(refused))].id
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var stored livestate.Document
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
			actor := core.Ref{Kind: core.CharacterKind, ID: "char:a"}
			views := []core.LocationID{"loc:camp", "loc:ford", "loc:mill", "loc:keep"}
			ra := apply(input{World: liveMapWorld(noise.Int63n(2) * noise.Int63n(1e6)), LiveState: &stored, Actions: s.actions, Actor: &actor, Request: s.id, Locations: views, Map: []core.CharacterID{"char:a", "char:b"}})
			rb := apply(input{Checkpoint: cp, Actions: s.actions, Actor: &actor, Request: s.id, Locations: views, Map: []core.CharacterID{"char:a", "char:b"}})
			where := fmt.Sprintf("round %d step %d request %s %q", round, step, s.id, s.actions)
			if ra.OK != rb.OK || ra.Phase != rb.Phase || ra.Historical != rb.Historical || ra.Error != rb.Error || !reflect.DeepEqual(ra.Fault, rb.Fault) || !reflect.DeepEqual(ra.Diagnostics, rb.Diagnostics) || ra.ViewError != nil || rb.ViewError != nil {
				t.Fatalf("%s:\nlive ok %v phase %s historical %v error %q fault %+v\nkept ok %v phase %s historical %v error %q fault %+v", where, ra.OK, ra.Phase, ra.Historical, ra.Error, ra.Fault, rb.OK, rb.Phase, rb.Historical, rb.Error, rb.Fault)
			}
			for _, r := range []*output{&ra, &rb} {
				for i := range r.Locations {
					r.Locations[i].Revision = 0
				}
				for i := range r.Map {
					r.Map[i].Revision = 0
				}
			}
			if !reflect.DeepEqual(ra.Locations, rb.Locations) || !reflect.DeepEqual(ra.Map, rb.Map) {
				t.Fatalf("%s: views differ\nlive %+v %+v\nkept %+v %+v", where, ra.Locations, ra.Map, rb.Locations, rb.Map)
			}
			switch {
			case !ra.OK:
				answers["refused "+ra.Phase]++
				refused = append(refused, s)
				continue
			case ra.Historical:
				answers["already applied"]++
				if !reflect.DeepEqual(ra.LiveState, doc) {
					t.Fatalf("%s: a retry changed the document", where)
				}
			default:
				if !reflect.DeepEqual(zero(ra.Receipt.Events), zero(rb.Receipt.Events)) {
					t.Fatalf("%s: events differ\nlive %+v\nkept %+v", where, ra.Receipt.Events, rb.Receipt.Events)
				}
				if last := ra.LiveState.Requests; ra.LiveState.Revision == doc.Revision+1 && len(last) > 0 && last[len(last)-1].ID == s.id {
					answers["applied, kept"]++
					kept = append(kept, s)
				} else {
					t.Fatalf("%s: document changed without keeping the request", where)
				}
			}
			// An ID that applied is never free again; one the document does not
			// keep is the host's to keep unique.
			for i := len(refused) - 1; i >= 0; i-- {
				if refused[i].id == s.id {
					refused = append(refused[:i], refused[i+1:]...)
				}
			}
			doc, cp = ra.LiveState, rb.Checkpoint
		}
		// Every place, character and map list is the same.
		final := apply(input{Checkpoint: cp, IncludeState: true})
		st := final.Checkpoint.State
		var chars []livestate.Character
		for _, c := range st.Characters {
			chars = append(chars, livestate.Character{ID: c.ID, Location: c.Location})
		}
		sort.Slice(chars, func(i, j int) bool { return chars[i].ID < chars[j].ID })
		m := st.Map
		sorted := func(ids []core.LocationID) []core.LocationID {
			out := append([]core.LocationID{}, ids...)
			sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
			return out
		}
		want := &livestate.Map{Party: append([]core.CharacterID{}, m.Party...), Visited: sorted(m.Visited), Hazards: sorted(m.Hazards), Closed: append([]core.MapLink{}, m.Closed...)}
		if !reflect.DeepEqual(doc.Occupants, append([]core.Occupant{}, st.Occupants...)) || !reflect.DeepEqual(doc.Characters, chars) || !reflect.DeepEqual(doc.Map, want) || !reflect.DeepEqual(doc.Clock, st.Clock) {
			t.Fatalf("round %d: state differs\nlive %+v %+v %+v\nkept %+v %+v %+v", round, doc.Characters, doc.Map, doc.Clock, chars, want, st.Clock)
		}
	}
	t.Logf("answers %v", answers)
	for _, k := range []string{"applied, kept", "already applied", "refused execute", "refused compile"} {
		if answers[k] < 30 {
			t.Fatalf("too few %q: %v", k, answers)
		}
	}
}

// A world that stops declaring a map, as an older host's would, holds the
// document's characters and map unchanged and ignores them; the next world
// with the map takes them back as they were.
func TestLiveStateHeldMap(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": mapWorld, "new_live_state": true})
	_, r := invoke(t, map[string]any{"world": mapWorld, "live_state": liveDoc(t, g.LiveState), "actions": `travel party to "loc:ford";`, "actor": liveActor, "request": "host:1"})
	if !r.OK || r.LiveState.Revision != 1 {
		t.Fatalf("travel: ok %v error %q", r.OK, r.Error)
	}
	d := r.LiveState
	if !reflect.DeepEqual(d.Characters, []livestate.Character{{ID: "char:a", Location: "loc:ford"}}) || !reflect.DeepEqual(d.Map.Visited, []core.LocationID{"loc:camp", "loc:ford"}) {
		t.Fatalf("setup %+v %+v", d.Characters, d.Map)
	}

	mapless := withoutMap(mapWorld)
	_, v := invoke(t, map[string]any{"world": mapless, "live_state": liveDoc(t, d), "locations": []string{"loc:camp"}})
	if !v.OK || !reflect.DeepEqual(v.LiveState, d) || len(v.Locations) != 1 || len(v.Locations[0].Characters) != 1 {
		t.Fatalf("view without a map: ok %v phase %s error %q", v.OK, v.Phase, v.Error)
	}
	// Without a map a move is the source's business: the document keeps the
	// held position and records no request.
	_, m := invoke(t, map[string]any{"world": mapless, "live_state": liveDoc(t, d), "actions": `move "char:a" to "loc:mill";`, "actor": liveActor, "request": "host:2"})
	if !m.OK || m.LiveState.Revision != 1 || !reflect.DeepEqual(m.LiveState.Characters, d.Characters) || !reflect.DeepEqual(m.LiveState.Map, d.Map) || len(m.LiveState.Requests) != 1 {
		t.Fatalf("move without a map: ok %v error %q state %+v", m.OK, m.Error, m.LiveState)
	}

	// The map is declared again: Ada stands at the ford, as the document
	// holds, not at the camp the source says, and the visits are kept.
	_, back := invoke(t, map[string]any{"world": mapWorld, "live_state": liveDoc(t, m.LiveState), "map": []string{"char:a"}})
	if !back.OK || len(back.Map) != 1 || back.Map[0].Location.ID != "loc:ford" || !reflect.DeepEqual(back.LiveState.Map, d.Map) || !reflect.DeepEqual(back.LiveState.Characters, d.Characters) {
		t.Fatalf("map again: ok %v error %q map %+v", back.OK, back.Error, back.Map)
	}
}

// A place new to the document takes the source's hazard on it and the
// closures of the routes that touch it, as it takes its seeds. From then on
// the document holds them and the source's lines are ignored.
func TestLiveStateNewPlaceMapState(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": mapWorld, "new_live_state": true})
	joined := strings.Replace(mapWorld, `location "loc:mill" named "Mill";`, `location "loc:mill" named "Mill";
location "loc:keep" named "Keep";
location "loc:gate" named "Gate";`, 1)
	joined = strings.Replace(joined, ` hazard "loc:mill";`, ` hazard "loc:mill";
 route "loc:mill" to "loc:keep" ticks 5 both;
 route "loc:keep" to "loc:gate" ticks 5 both;
 hazard "loc:keep";
 closed "loc:mill" to "loc:keep";
 closed "loc:keep" to "loc:gate";
 visited "loc:keep";`, 1)

	// The call that introduces the keep and the gate takes their hazard and
	// closures, but not the visited mark.
	_, v := invoke(t, map[string]any{"world": joined, "live_state": liveDoc(t, g.LiveState), "map": []string{"char:a"}})
	if !v.OK {
		t.Fatalf("join: phase %s error %q", v.Phase, v.Error)
	}
	d := v.LiveState
	if !reflect.DeepEqual(d.Places, []core.LocationID{"loc:camp", "loc:ford", "loc:gate", "loc:keep", "loc:mill"}) ||
		!reflect.DeepEqual(d.Map.Hazards, []core.LocationID{"loc:keep", "loc:mill"}) ||
		!reflect.DeepEqual(d.Map.Closed, []core.MapLink{{From: "loc:keep", To: "loc:gate"}, {From: "loc:mill", To: "loc:keep"}}) ||
		!reflect.DeepEqual(d.Map.Visited, g.LiveState.Map.Visited) || d.Revision != 0 {
		t.Fatalf("joined document %+v %+v", d.Places, d.Map)
	}

	// In that call the hazard is already marked.
	_, again := invoke(t, map[string]any{"world": joined, "live_state": liveDoc(t, g.LiveState), "actions": `mark hazard "loc:keep";`, "actor": liveActor, "request": "join:1"})
	if again.OK || again.Fault == nil || again.Fault.Code != "E_NO_CHANGE" {
		t.Fatalf("mark in the joining call: ok %v fault %+v", again.OK, again.Fault)
	}

	// Cleared in play, the hazard stays cleared although the source still
	// declares it, and a retry is answered as applied.
	clear := map[string]any{"world": joined, "live_state": liveDoc(t, d), "actions": `clear hazard "loc:keep";`, "actor": liveActor, "request": "host:1"}
	_, c := invoke(t, clear)
	if !c.OK || !reflect.DeepEqual(c.LiveState.Map.Hazards, []core.LocationID{"loc:mill"}) {
		t.Fatalf("clear: ok %v error %q map %+v", c.OK, c.Error, c.LiveState)
	}
	_, n := invoke(t, map[string]any{"world": joined, "live_state": liveDoc(t, c.LiveState), "map": []string{"char:a"}})
	if !n.OK || !reflect.DeepEqual(n.LiveState, c.LiveState) {
		t.Fatalf("next call: ok %v error %q map %+v", n.OK, n.Error, n.LiveState)
	}
	clear["live_state"] = liveDoc(t, c.LiveState)
	_, r := invoke(t, clear)
	if !r.OK || !r.Historical || !reflect.DeepEqual(r.LiveState, c.LiveState) {
		t.Fatalf("retry: ok %v historical %v error %q", r.OK, r.Historical, r.Error)
	}
}

// A place marked visited is held like one reached by travel, and a retry of
// the mark is answered as applied.
func TestLiveStateMarkVisited(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": mapWorld, "new_live_state": true})
	mark := map[string]any{"world": mapWorld, "live_state": liveDoc(t, g.LiveState), "actions": `mark visited "loc:mill";`, "actor": liveActor, "request": "host:1"}
	_, r := invoke(t, mark)
	if !r.OK || r.LiveState.Revision != 1 || !reflect.DeepEqual(r.LiveState.Map.Visited, []core.LocationID{"loc:mill"}) || !reflect.DeepEqual(r.LiveState.Characters, g.LiveState.Characters) {
		t.Fatalf("mark: ok %v error %q state %+v", r.OK, r.Error, r.LiveState)
	}
	mark["live_state"] = liveDoc(t, r.LiveState)
	_, again := invoke(t, mark)
	if !again.OK || !again.Historical || !reflect.DeepEqual(again.LiveState, r.LiveState) {
		t.Fatalf("retry: ok %v historical %v error %q", again.OK, again.Historical, again.Error)
	}
}

// A route declared only in some calls keeps its closure: a call that does not
// declare it leaves the document as it was, and declared again the route is
// still closed until a request opens it.
func TestLiveStateHeldClosure(t *testing.T) {
	gated := strings.Replace(mapWorld, ` hazard "loc:mill";`, ` hazard "loc:mill";
 route "loc:camp" to "loc:mill" ticks 3 both;`, 1)
	_, g := invoke(t, map[string]any{"world": mapWorld, "new_live_state": true})
	_, c := invoke(t, map[string]any{"world": gated, "live_state": liveDoc(t, g.LiveState), "actions": `close route "loc:camp" to "loc:mill";`, "actor": liveActor, "request": "bar:1"})
	if !c.OK || !reflect.DeepEqual(c.LiveState.Map.Closed, []core.MapLink{{From: "loc:camp", To: "loc:mill"}}) {
		t.Fatalf("close: ok %v error %q state %+v", c.OK, c.Error, c.LiveState)
	}
	_, n := invoke(t, map[string]any{"world": mapWorld, "live_state": liveDoc(t, c.LiveState), "map": []string{"char:a"}})
	if !n.OK || !reflect.DeepEqual(n.LiveState, c.LiveState) {
		t.Fatalf("without the route: ok %v error %q state %+v", n.OK, n.Error, n.LiveState)
	}
	_, back := invoke(t, map[string]any{"world": gated, "live_state": liveDoc(t, n.LiveState), "map": []string{"char:a"}})
	closed := false
	for _, e := range back.Map[0].Exits {
		if e.To.ID == "loc:mill" {
			closed = e.Closed
		}
	}
	if !back.OK || !closed || !reflect.DeepEqual(back.LiveState, c.LiveState) {
		t.Fatalf("declared again: ok %v error %q exits %+v", back.OK, back.Error, back.Map)
	}
	// A retry names the route, so it is compiled against a world that
	// declares it; without the route it is refused before it is looked up.
	retry := map[string]any{"world": mapWorld, "live_state": liveDoc(t, n.LiveState), "actions": `close route "loc:camp" to "loc:mill";`, "actor": liveActor, "request": "bar:1"}
	if _, r := invoke(t, retry); r.OK || r.Phase != "compile" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "E_MAP" {
		t.Fatalf("retry without the route: ok %v phase %s diagnostics %+v", r.OK, r.Phase, r.Diagnostics)
	}
	retry["world"] = gated
	if _, r := invoke(t, retry); !r.OK || !r.Historical || !reflect.DeepEqual(r.LiveState, c.LiveState) {
		t.Fatalf("retry with the route: ok %v historical %v error %q", r.OK, r.Historical, r.Error)
	}
	_, o := invoke(t, map[string]any{"world": gated, "live_state": liveDoc(t, n.LiveState), "actions": `open route "loc:camp" to "loc:mill";`, "actor": liveActor, "request": "open:1"})
	if !o.OK || len(o.LiveState.Map.Closed) != 0 {
		t.Fatalf("open: ok %v error %q state %+v", o.OK, o.Error, o.LiveState)
	}
}
