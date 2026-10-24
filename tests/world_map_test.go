package tests

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A small world map: the quickest way from camp to the gate runs through
// the ford and the mill (45 minutes) rather than straight to the mill (50).
// The keep is one way from the gate; nothing reaches the isle.
const mapPlaces = `rules { transfer unequips; wear any; }
clock "minute" at 100;
location "loc:camp" named "Camp";
location "loc:ford" named "Ford";
location "loc:mill" named "Mill";
location "loc:gate" named "Gate";
location "loc:keep" named "Keep";
location "loc:isle" named "Isle";
character "char:ada" named "Ada" at "loc:camp" { }
character "char:bea" named "Bea" at "loc:camp" { }
character "char:cy" named "Cy" at "loc:keep" { }
condition type "spell:ward" { instances unique; }
`
const mapRoutes = ` route "loc:camp" to "loc:ford" ticks 10 both;
 route "loc:ford" to "loc:mill" ticks 5 both;
 route "loc:camp" to "loc:mill" ticks 20 both;
 route "loc:mill" to "loc:gate" ticks 30 both;
 route "loc:gate" to "loc:keep" ticks 15;
 party "char:ada";
 party "char:bea";
`

func mapFixture(t testing.TB, extra string) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n"+mapRoutes+extra+"}\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func trip(t testing.TB, b core.Batch) core.Trip {
	t.Helper()
	last := b.Events[len(b.Events)-1]
	if last.Kind != core.PartyTraveled || last.Trip == nil {
		t.Fatalf("last event %+v, want PartyTraveled with a trip", last)
	}
	return *last.Trip
}

func wantAt(t testing.TB, w *core.World, at core.LocationID, ids ...core.CharacterID) {
	t.Helper()
	for _, id := range ids {
		for _, c := range w.Snapshot().Characters {
			if c.ID == id && c.Location != at {
				t.Fatalf("%s at %s, want %s", id, c.Location, at)
			}
		}
	}
}

func locs(ids ...string) []core.LocationID {
	out := []core.LocationID{}
	for _, id := range ids {
		out = append(out, core.LocationID(id))
	}
	return out
}

func TestWorldMapTravel(t *testing.T) {
	b := mapFixture(t, ` visited "loc:ford";`+"\n"+` visited "loc:mill";`+"\n")
	w := world(t, b)
	execute(t, w, "ward", `apply condition "ward:1" of "spell:ward" to "char:ada" ticks 30;`)

	// The ward ends on the way, at the tick its 30 minutes run out.
	r := execute(t, w, "go", `travel party to "loc:gate";`)
	wantKinds(t, r, core.EntityMoved, core.EntityMoved, core.ConditionRemoved, core.TimeAdvanced, core.PartyTraveled)
	if got, want := trip(t, r), (core.Trip{From: "loc:camp", Stops: locs("loc:ford", "loc:mill", "loc:gate"), Ticks: 45, Visited: locs("loc:camp", "loc:gate")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	if e := r.Events[2].Condition; e.ClockExpiration == nil || e.Before.ID != "ward:1" {
		t.Fatalf("ward ending %+v", e)
	}
	if got := r.Events[3].Time; got.Before != 100 || got.After != 145 || got.Requested != 45 {
		t.Fatalf("time event %+v", got)
	}
	s := w.Snapshot()
	if s.Clock.Tick != 145 || !reflect.DeepEqual(s.Map.Visited, locs("loc:camp", "loc:ford", "loc:gate", "loc:mill")) || len(s.Conditions) != 0 {
		t.Fatalf("clock %d visited %v conditions %v", s.Clock.Tick, s.Map.Visited, s.Conditions)
	}
	wantAt(t, w, "loc:gate", "char:ada", "char:bea")
	wantAt(t, w, "loc:keep", "char:cy")

	// A one-way route reaches a new place; nothing leads back from it.
	r = execute(t, w, "on", `travel party to "loc:keep";`)
	if got, want := trip(t, r), (core.Trip{From: "loc:gate", Stops: locs("loc:keep"), Ticks: 15, Visited: locs("loc:keep")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	_, err := w.Execute(request(t, w, "back", `travel party to "loc:gate";`))
	wantMapFault(t, err, "travel path", "no route")
	costReplay(t, b, w)
}

func TestWorldMapTimeEvent(t *testing.T) {
	w := world(t, mapFixture(t, ` visited "loc:ford";`+"\n"))
	r := execute(t, w, "go", `travel party to "loc:mill";`)
	wantKinds(t, r, core.EntityMoved, core.EntityMoved, core.TimeAdvanced, core.PartyTraveled)
	if got := r.Events[2].Time; got.Before != 100 || got.After != 115 || got.Requested != 15 {
		t.Fatalf("time event %+v", got)
	}
	// The destination need not be visited; it is afterwards, and so is the
	// place the party left.
	if got := w.Snapshot().Map.Visited; !reflect.DeepEqual(got, locs("loc:camp", "loc:ford", "loc:mill")) {
		t.Fatalf("visited %v", got)
	}
}

func TestWorldMapHazardHalts(t *testing.T) {
	b := mapFixture(t, ` visited "loc:ford";`+"\n"+` visited "loc:mill";`+"\n"+` hazard "loc:ford";`+"\n")
	w := world(t, b)
	r := execute(t, w, "go", `travel party to "loc:gate";`)
	if got, want := trip(t, r), (core.Trip{From: "loc:camp", Stops: locs("loc:ford"), Ticks: 10, Halted: true, Visited: locs("loc:camp")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	wantAt(t, w, "loc:ford", "char:ada", "char:bea")
	if w.Snapshot().Clock.Tick != 110 {
		t.Fatalf("clock %d, want 110", w.Snapshot().Clock.Tick)
	}
	// Leaving a hazard does not stop the party again.
	r = execute(t, w, "on", `travel party to "loc:gate";`)
	if got, want := trip(t, r), (core.Trip{From: "loc:ford", Stops: locs("loc:mill", "loc:gate"), Ticks: 35, Visited: locs("loc:gate")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	costReplay(t, b, w)

	// A hazard at the destination is simply reached.
	w = world(t, mapFixture(t, ` visited "loc:ford";`+"\n"+` hazard "loc:mill";`+"\n"))
	if got := trip(t, execute(t, w, "go", `travel party to "loc:mill";`)); got.Halted || !reflect.DeepEqual(got.Stops, locs("loc:ford", "loc:mill")) {
		t.Fatalf("trip %+v", got)
	}
}

func wantMapFault(t testing.TB, err error, field, actual string) {
	t.Helper()
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_MAP" || f.Field != field || f.Actual != actual {
		t.Fatalf("got %#v, want E_MAP %s with actual %q", err, field, actual)
	}
}

func TestWorldMapRefusalsLeaveNoTrace(t *testing.T) {
	for _, tc := range []struct {
		name, extra, actor, actions, field, actual string
	}{
		{"unvisited stop", ` visited "loc:ford";`, "char:ada", `travel party to "loc:gate";`, "travel path", "unvisited stop"},
		{"closed route", ` visited "loc:ford"; visited "loc:mill"; closed "loc:mill" to "loc:gate";`, "char:ada", `travel party to "loc:gate";`, "travel path", "closed route"},
		{"closed and unvisited", ` visited "loc:ford"; closed "loc:mill" to "loc:gate";`, "char:ada", `travel party to "loc:gate";`, "travel path", "closed route and unvisited stop"},
		{"closed exits", ` closed "loc:camp" to "loc:ford"; closed "loc:camp" to "loc:mill";`, "char:ada", `travel party to "loc:ford";`, "travel path", "closed route"},
		{"way round unvisited", ` closed "loc:camp" to "loc:ford";`, "char:ada", `travel party to "loc:ford";`, "travel path", "unvisited stop"},
		{"no route", ``, "char:ada", `travel party to "loc:isle";`, "travel path", "no route"},
		{"not in party", ``, "char:cy", `travel party to "loc:ford";`, "travel actor", "char:cy"},
		{"split party", ``, "char:ada", `move "char:bea" to "loc:ford"; travel party to "loc:ford";`, "travel party", "loc:ford"},
		{"already there", ``, "char:ada", `travel party to "loc:camp";`, "travel", "loc:camp"},
		{"second travel fails", ``, "char:ada", `travel party to "loc:ford"; travel party to "loc:ford";`, "travel", "loc:ford"},
		{"no party", ``, "char:ada", `travel party to "loc:ford";`, "travel actor", "char:ada"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := mapRoutes
			if tc.name == "no party" {
				routes = strings.Replace(strings.Replace(routes, ` party "char:ada";`+"\n", "", 1), ` party "char:bea";`+"\n", "", 1)
			}
			b, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n"+routes+tc.extra+"\n}\n")
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			w := world(t, b)
			before, history := w.Snapshot(), w.History()
			r := request(t, w, "go", tc.actions)
			r.Actor = core.Ref{Kind: core.CharacterKind, ID: tc.actor}
			_, err := w.Execute(r)
			wantMapFault(t, err, tc.field, tc.actual)
			unchanged(t, w, before, history)
		})
	}

	// Time that cannot pass rejects the whole trip, after the moves.
	b := mapFixture(t, "")
	b.Clock.Tick = math.MaxUint64 - 19
	w := world(t, b)
	before, history := w.Snapshot(), w.History()
	_, err := w.Execute(request(t, w, "go", `travel party to "loc:mill";`))
	if f, ok := err.(*core.Fault); !ok || f.Code != "E_CLOCK" {
		t.Fatalf("travel past the end of the clock: %#v", err)
	}
	unchanged(t, w, before, history)

	// So does a party too large for one request's work: each member's move
	// and the time each count.
	var party strings.Builder
	for i := 0; i < core.MaxOperations; i++ {
		fmt.Fprintf(&party, "character \"char:m%03d\" named \"M\" at \"loc:camp\" { }\n party \"char:m%03d\";\n", i, i)
	}
	members := strings.Split(strings.TrimSpace(party.String()), "\n")
	var chars, list []string
	for i, l := range members {
		if i%2 == 0 {
			chars = append(chars, l)
		} else {
			list = append(list, l)
		}
	}
	b, ds := compiler.CompileWorld("crowd.nql", mapPlaces+strings.Join(chars, "\n")+"\nmap {\n"+mapRoutes+strings.Join(list, "\n")+"\n}\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w = world(t, b)
	before, history = w.Snapshot(), w.History()
	_, err = w.Execute(request(t, w, "go", `travel party to "loc:mill";`))
	if f, ok := err.(*core.Fault); !ok || f.Code != "E_LIMIT" {
		t.Fatalf("travel of %d members: %#v", core.MaxOperations+2, err)
	}
	unchanged(t, w, before, history)
}

// Equal trip times go to fewer stops, then to the smaller stop IDs in
// order, whatever order the routes are declared in.
func TestWorldMapTies(t *testing.T) {
	const places = `rules { transfer unequips; wear any; }
clock "minute" at 0;
location "loc:a" named "A";
location "loc:b" named "B";
location "loc:c" named "C";
location "loc:d" named "D";
location "loc:e" named "E";
location "loc:f" named "F";
character "char:ada" named "Ada" at "loc:a" { }
`
	for _, tc := range []struct {
		routes string
		want   []core.LocationID
	}{
		{`route "loc:a" to "loc:c" ticks 5; route "loc:a" to "loc:b" ticks 5; route "loc:c" to "loc:d" ticks 5; route "loc:b" to "loc:d" ticks 5;`, locs("loc:b", "loc:d")},
		{`route "loc:a" to "loc:c" ticks 5; route "loc:c" to "loc:d" ticks 5; route "loc:a" to "loc:d" ticks 10;`, locs("loc:d")},
		// The first stop decides, though the last stops compare the other way.
		{`route "loc:a" to "loc:c" ticks 1; route "loc:c" to "loc:e" ticks 1; route "loc:e" to "loc:d" ticks 1; route "loc:a" to "loc:b" ticks 1; route "loc:b" to "loc:f" ticks 1; route "loc:f" to "loc:d" ticks 1;`, locs("loc:b", "loc:f", "loc:d")},
		{`route "loc:a" to "loc:b" ticks 1; route "loc:b" to "loc:f" ticks 1; route "loc:f" to "loc:d" ticks 1; route "loc:b" to "loc:e" ticks 1; route "loc:e" to "loc:d" ticks 1;`, locs("loc:b", "loc:e", "loc:d")},
	} {
		src := places + "map {\n" + tc.routes + ` party "char:ada"; visited "loc:b"; visited "loc:c"; visited "loc:e"; visited "loc:f";` + "\n}\n"
		b, ds := compiler.CompileWorld("ties.nql", src)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		if got := trip(t, execute(t, world(t, b), "go", `travel party to "loc:d";`)); !reflect.DeepEqual(got.Stops, tc.want) {
			t.Fatalf("%s: stops %v, want %v", tc.routes, got.Stops, tc.want)
		}
	}
}

func TestWorldMapView(t *testing.T) {
	w := world(t, mapFixture(t, ` visited "loc:ford"; hazard "loc:ford"; closed "loc:camp" to "loc:mill";`+"\n"))
	p, err := w.MapView("char:bea")
	if err != nil {
		t.Fatal(err)
	}
	place := func(id, name string) core.Location { return core.Location{ID: core.LocationID(id), Name: name} }
	ford := core.LocationID("loc:ford")
	want := core.MapProjection{
		Clock:     &core.Clock{Unit: "minute", Tick: 100},
		Character: core.CharacterSummary{ID: "char:bea", Name: "Bea"},
		Location:  place("loc:camp", "Camp"),
		Exits: []core.MapExit{
			{To: place("loc:ford", "Ford"), Ticks: 10, Visited: true, Hazard: true},
			{To: place("loc:mill", "Mill"), Ticks: 20, Closed: true},
		},
		Destinations: []core.MapDestination{
			{To: place("loc:ford", "Ford"), Ticks: 10, Stops: 1, Visited: true},
			{To: place("loc:mill", "Mill"), Ticks: 15, Stops: 2, HaltsAt: &ford},
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("view\n%+v\nwant\n%+v", p, want)
	}
	// The view is the caller's to change.
	p.Clock.Tick, *p.Destinations[1].HaltsAt, p.Exits[0].To.Name = 900, "loc:keep", "Changed"
	if again, _ := w.MapView("char:bea"); !reflect.DeepEqual(again, want) {
		t.Fatalf("view aliases the world: %+v", again)
	}

	if _, err := w.MapView("char:nobody"); err == nil {
		t.Fatal("view of an unknown character")
	}
	b, ds := compiler.CompileWorld("plain.nql", mapPlaces)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	_, err = world(t, b).MapView("char:ada")
	wantMapFault(t, err, "map", "absent")
}

// Declaration order does not change the world: routes and lists are kept
// sorted, so the same map written two ways is the same state.
func TestWorldMapNormalized(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(mapRoutes+` visited "loc:mill"; visited "loc:ford"; hazard "loc:keep"; hazard "loc:gate"; closed "loc:mill" to "loc:camp"; closed "loc:camp" to "loc:ford";`), "\n")
	reversed := append([]string(nil), lines...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	var snaps []core.Snapshot
	for _, body := range [][]string{lines, reversed} {
		b, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n"+strings.Join(body, "\n")+"\n}\n")
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		snaps = append(snaps, world(t, b).Snapshot())
	}
	if !reflect.DeepEqual(snaps[0], snaps[1]) {
		t.Fatalf("declaration order changed the world:\n%+v\n%+v", snaps[0].Map, snaps[1].Map)
	}
	if m := snaps[0].Map; m.Routes[0].From != "loc:camp" || m.Routes[0].To != "loc:ford" || m.Closed[0].To != "loc:ford" || m.Visited[0] != "loc:ford" || m.Hazards[0] != "loc:gate" {
		t.Fatalf("map not sorted: %+v", m)
	}
}

func TestWorldMapCompileRefusals(t *testing.T) {
	head := strings.Count(mapPlaces, "\n") + 1 // the map line
	for _, tc := range []struct {
		name, body string
		line       int    // line within the body, 0 for the map line
		at         string // the last occurrence on that line marks the column
		code       string
		expected   string
	}{
		{"unknown route place", ` route "loc:camp" to "loc:nowhere" ticks 5;`, 1, `"loc:nowhere"`, "E_REFERENCE", ""},
		{"same endpoints", ` route "loc:camp" to "loc:camp" ticks 5;`, 1, `"loc:camp"`, "E_MAP", "different endpoints"},
		{"zero ticks", ` route "loc:camp" to "loc:ford" ticks 0;`, 1, `0`, "E_MAP", "1..1000000000"},
		{"too many ticks", ` route "loc:camp" to "loc:ford" ticks 1000000001;`, 1, `1000000001`, "E_MAP", "1..1000000000"},
		{"duplicate route", " route \"loc:camp\" to \"loc:ford\" ticks 5;\n route \"loc:camp\" to \"loc:ford\" ticks 6;", 2, ` "loc:camp"`, "E_MAP", "unique direction"},
		{"both meets a route", " route \"loc:ford\" to \"loc:camp\" ticks 5;\n route \"loc:camp\" to \"loc:ford\" ticks 5 both;", 2, `"loc:ford"`, "E_MAP", "unique direction"},
		{"route after both", " route \"loc:camp\" to \"loc:ford\" ticks 5 both;\n route \"loc:ford\" to \"loc:camp\" ticks 5;", 2, ` "loc:ford"`, "E_MAP", "unique direction"},
		{"unknown party member", ` party "char:nobody";`, 1, `"char:nobody"`, "E_REFERENCE", ""},
		{"place as party member", ` party "loc:camp";`, 1, `"loc:camp"`, "E_TYPE", ""},
		{"duplicate party member", " party \"char:ada\";\n party \"char:ada\";", 2, `"char:ada"`, "E_MAP", "unique member"},
		{"duplicate visited", " visited \"loc:camp\";\n visited \"loc:camp\";", 2, `"loc:camp"`, "E_MAP", "unique location"},
		{"duplicate hazard", " hazard \"loc:camp\";\n hazard \"loc:camp\";", 2, `"loc:camp"`, "E_MAP", "unique location"},
		{"unknown visited", ` visited "loc:nowhere";`, 1, `"loc:nowhere"`, "E_REFERENCE", ""},
		{"unknown hazard", ` hazard "char:ada";`, 1, `"char:ada"`, "E_TYPE", ""},
		{"closed without route", ` closed "loc:camp" to "loc:gate";`, 1, ` "loc:camp"`, "E_MAP", "declared route"},
		{"closed reverse of one way", " route \"loc:camp\" to \"loc:ford\" ticks 5;\n closed \"loc:ford\" to \"loc:camp\";", 2, ` "loc:ford"`, "E_MAP", "declared route"},
		{"duplicate closed", " route \"loc:camp\" to \"loc:ford\" ticks 5;\n closed \"loc:camp\" to \"loc:ford\";\n closed \"loc:camp\" to \"loc:ford\";", 3, ` "loc:camp"`, "E_MAP", "unique route"},
		{"unknown statement", ` road "loc:camp" to "loc:ford" ticks 5;`, 1, `road`, "E_PARSE", ""},
		{"both twice", ` route "loc:camp" to "loc:ford" ticks 5 both both;`, 1, `both;`, "E_PARSE", ""},
		{"missing ticks", ` route "loc:camp" to "loc:ford";`, 1, `;`, "E_PARSE", ""},
		{"negative ticks", ` route "loc:camp" to "loc:ford" ticks -1;`, 1, `-1`, "E_MAP", "1..1000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n"+tc.body+"\n}\n")
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			text := strings.Split(tc.body, "\n")[tc.line-1]
			line, column := head+tc.line, strings.LastIndex(text, tc.at)+1
			if strings.HasPrefix(tc.at, " ") {
				column++
			}
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != line || d.Column != column {
				t.Fatalf("got %#v, want %s %q at %d:%d", d, tc.code, tc.expected, line, column)
			}
		})
	}

	// Bounds that hold: one tick, the longest route.
	if _, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n route \"loc:camp\" to \"loc:ford\" ticks 1;\n route \"loc:ford\" to \"loc:camp\" ticks 1000000000;\n}\n"); len(ds) > 0 {
		t.Fatal(ds)
	}
	// An empty map is a map with no routes.
	b, ds := compiler.CompileWorld("map.nql", mapPlaces+"map {\n}\n")
	if len(ds) > 0 || b.Map == nil || b.Map.Routes == nil || len(b.Map.Routes) != 0 {
		t.Fatalf("empty map %+v %v", b.Map, ds)
	}

	for _, tc := range []struct {
		name, src, code, expected string
		line, column              int
	}{
		{"second map", mapPlaces + "map {\n}\nmap {\n}\n", "E_DUPLICATE", "one map block", head + 2, 1},
		{"no clock", strings.Replace(mapPlaces, "clock \"minute\" at 100;\n", "", 1) + "map {\n}\n", "E_MAP", "declared clock", head - 1, 1},
	} {
		_, ds := compiler.CompileWorld("map.nql", tc.src)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Expected != tc.expected || ds[0].Line != tc.line || ds[0].Column != tc.column {
			t.Fatalf("%s: got %#v", tc.name, ds)
		}
	}

	for _, src := range travelModelWorlds(t) {
		_, ds = compiler.CompileWorld("other.nql", src+"map {\n}\n")
		if len(ds) != 1 || ds[0].Code != "E_MAP" || ds[0].Expected != "map without battle or adventure" {
			t.Fatalf("map beside another travel model: %#v", ds)
		}
	}
}

// travelModelWorlds are a battle world and an adventure world, each with a
// clock, the two worlds that travel by their own routes.
func travelModelWorlds(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, path := range []string{"../examples/native_melee/world.nql", "../examples/alpha/adventure.nql"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if !strings.Contains(text, "\nclock ") {
			text = strings.Replace(text, "\nlocation ", "\nclock \"minute\" at 0;\nlocation ", 1)
		}
		out = append(out, text)
	}
	return out
}

func TestWorldMapRouteLimit(t *testing.T) {
	var places strings.Builder
	places.WriteString("rules { transfer unequips; wear any; }\nclock \"minute\" at 0;\n")
	const n = 182 // 182 * 181 ordered pairs cover the limit
	for i := 0; i < n; i++ {
		fmt.Fprintf(&places, "location \"loc:%d\" named \"P%d\";\n", i, i)
	}
	routes := func(count int) string {
		var s strings.Builder
		s.WriteString("map {\n")
		for i := 0; i < n && count > 0; i++ {
			for j := 0; j < n && count > 0; j++ {
				if i != j {
					fmt.Fprintf(&s, " route \"loc:%d\" to \"loc:%d\" ticks 1;\n", i, j)
					count--
				}
			}
		}
		return s.String() + "}\n"
	}
	b, ds := compiler.CompileWorld("limit.nql", places.String()+routes(core.MaxMapRoutes))
	if len(ds) > 0 || len(b.Map.Routes) != core.MaxMapRoutes {
		t.Fatalf("routes at the limit: %v", ds)
	}
	_, ds = compiler.CompileWorld("limit.nql", places.String()+routes(core.MaxMapRoutes+1))
	if len(ds) != 1 || ds[0].Code != "E_MAP" || ds[0].Expected != "at most 32768 routes" {
		t.Fatalf("routes past the limit: %#v", ds)
	}
	// The engine refuses the same map handed to it directly.
	b.Map.Routes = append(b.Map.Routes, core.MapRoute{From: "loc:181", To: "loc:180", Ticks: 1})
	if _, err := core.NewWorld(b); err == nil || err.(*core.Fault).Expected != "at most 32768 routes" {
		t.Fatalf("engine accepted %d routes: %v", len(b.Map.Routes), err)
	}
}

// A map handed to the engine directly, as a host checkpoint carries it, is
// checked as strictly as one compiled from source.
func TestWorldMapEngineRefusals(t *testing.T) {
	base := mapFixture(t, "")
	for _, tc := range []struct {
		name     string
		edit     func(m *core.WorldMap, b *core.Blueprint)
		code     string
		expected string
	}{
		{"no clock", func(m *core.WorldMap, b *core.Blueprint) { b.Clock = nil }, "E_MAP", "declared clock"},
		{"zero ticks", func(m *core.WorldMap, b *core.Blueprint) { m.Routes[0].Ticks = 0 }, "E_MAP", "1..1000000000"},
		{"too many ticks", func(m *core.WorldMap, b *core.Blueprint) { m.Routes[0].Ticks = core.MaxRouteTicks + 1 }, "E_MAP", "1..1000000000"},
		{"same endpoints", func(m *core.WorldMap, b *core.Blueprint) { m.Routes[0].To = m.Routes[0].From }, "E_MAP", "different endpoints"},
		{"unknown place", func(m *core.WorldMap, b *core.Blueprint) { m.Routes[0].To = "loc:nowhere" }, "E_REFERENCE", ""},
		{"duplicate route", func(m *core.WorldMap, b *core.Blueprint) { m.Routes = append(m.Routes, m.Routes[0]) }, "E_MAP", "unique direction"},
		{"duplicate party", func(m *core.WorldMap, b *core.Blueprint) { m.Party = append(m.Party, m.Party[0]) }, "E_MAP", "unique member"},
		{"unknown party", func(m *core.WorldMap, b *core.Blueprint) { m.Party = append(m.Party, "char:nobody") }, "E_REFERENCE", ""},
		{"duplicate visited", func(m *core.WorldMap, b *core.Blueprint) { m.Visited = locs("loc:camp", "loc:camp") }, "E_MAP", "unique location"},
		{"unknown hazard", func(m *core.WorldMap, b *core.Blueprint) { m.Hazards = locs("loc:nowhere") }, "E_REFERENCE", ""},
		{"closed without route", func(m *core.WorldMap, b *core.Blueprint) {
			m.Closed = []core.MapLink{{From: "loc:keep", To: "loc:gate"}}
		}, "E_MAP", "declared route"},
		{"duplicate closed", func(m *core.WorldMap, b *core.Blueprint) {
			m.Closed = []core.MapLink{{From: "loc:gate", To: "loc:keep"}, {From: "loc:gate", To: "loc:keep"}}
		}, "E_MAP", "unique route"},
	} {
		b := mapFixture(t, "")
		tc.edit(b.Map, &b)
		_, err := core.NewWorld(b)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != tc.code || tc.expected != "" && f.Expected != tc.expected {
			t.Fatalf("%s: got %#v", tc.name, err)
		}
	}
	if _, err := core.NewWorld(base); err != nil {
		t.Fatal(err)
	}
	for _, src := range travelModelWorlds(t) {
		b, ds := compiler.CompileWorld("other.nql", src)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		b.Map = &core.WorldMap{Routes: []core.MapRoute{}}
		_, err := core.NewWorld(b)
		wantMapFault(t, err, "travel model", "both")
	}
}

// Snapshots, receipts and history are copies: changing one changes nothing
// in the world.
func TestWorldMapDetached(t *testing.T) {
	b := mapFixture(t, ` visited "loc:ford"; hazard "loc:keep"; closed "loc:mill" to "loc:camp";`+"\n")
	w := world(t, b)
	r := execute(t, w, "go", `travel party to "loc:mill";`)
	before, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	s := w.Snapshot()
	s.Map.Routes[0].Ticks, s.Map.Party[0], s.Map.Visited[0], s.Map.Hazards[0], s.Map.Closed[0].To = 900, "char:cy", "loc:isle", "loc:isle", "loc:isle"
	r.Events[len(r.Events)-1].Trip.Stops[0], r.Events[len(r.Events)-1].Trip.Visited[0] = "loc:isle", "loc:isle"
	h := w.History()
	h[0].Events[len(h[0].Events)-1].Trip.Stops[0] = "loc:isle"
	b.Map.Routes[0].Ticks, b.Map.Visited[0] = 900, "loc:isle"
	contractEqual(t, before, w.Snapshot())
	contractEqual(t, history, w.History())
}

// bruteTrip is the travel rule by enumeration: every simple path from start
// to the destination over open routes whose stops before the last may be
// passed through, the quickest, then the shortest, then the smallest stops.
type bruteMap struct {
	routes  map[[2]int]int64
	n       int
	visited []bool
	hazard  []bool
	closed  map[[2]int]bool
}

func (m bruteMap) best(start, to int, openAll, passAll bool) ([]int, int64, bool) {
	var best []int
	var bestTicks int64
	found := false
	seen := make([]bool, m.n)
	var path []int
	var walk func(at int, ticks int64)
	walk = func(at int, ticks int64) {
		if at == to {
			if !found || ticks < bestTicks || ticks == bestTicks && (len(path) < len(best) || len(path) == len(best) && lessInts(path, best)) {
				best, bestTicks, found = append([]int(nil), path...), ticks, true
			}
			return
		}
		if at != start && !passAll && !m.visited[at] {
			return
		}
		for next := 0; next < m.n; next++ {
			r, ok := m.routes[[2]int{at, next}]
			if !ok || seen[next] || next == start || !openAll && m.closed[[2]int{at, next}] {
				continue
			}
			seen[next] = true
			path = append(path, next)
			walk(next, ticks+r)
			path = path[:len(path)-1]
			seen[next] = false
		}
	}
	walk(start, 0)
	return best, bestTicks, found
}

func lessInts(a, z []int) bool {
	for i := range a {
		if a[i] != z[i] {
			// Location IDs "loc:pN" with N one digit compare as N does.
			return a[i] < z[i]
		}
	}
	return false
}

func TestWorldMapMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(39))
	trips, refusals, halts := 0, 0, 0
	for round := 0; round < 1500; round++ {
		n := 2 + rng.Intn(8)
		m := bruteMap{routes: map[[2]int]int64{}, n: n, visited: make([]bool, n), hazard: make([]bool, n), closed: map[[2]int]bool{}}
		var src strings.Builder
		src.WriteString("rules { transfer unequips; wear any; }\nclock \"minute\" at 0;\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&src, "location \"loc:p%d\" named \"P%d\";\n", i, i)
		}
		start := rng.Intn(n)
		fmt.Fprintf(&src, "character \"char:ada\" named \"Ada\" at \"loc:p%d\" { }\nmap {\n party \"char:ada\";\n", start)
		density := 0.2 + 0.4*rng.Float64()
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i != j && rng.Float64() < density {
					ticks := int64(1 + rng.Intn(3))
					m.routes[[2]int{i, j}] = ticks
					fmt.Fprintf(&src, " route \"loc:p%d\" to \"loc:p%d\" ticks %d;\n", i, j, ticks)
					if rng.Float64() < 0.15 {
						m.closed[[2]int{i, j}] = true
						fmt.Fprintf(&src, " closed \"loc:p%d\" to \"loc:p%d\";\n", i, j)
					}
				}
			}
			if rng.Float64() < 0.5 {
				m.visited[i] = true
				fmt.Fprintf(&src, " visited \"loc:p%d\";\n", i)
			}
			if rng.Float64() < 0.25 {
				m.hazard[i] = true
				fmt.Fprintf(&src, " hazard \"loc:p%d\";\n", i)
			}
		}
		src.WriteString("}\n")
		b, ds := compiler.CompileWorld("random.nql", src.String())
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		view, err := world(t, b).MapView("char:ada")
		if err != nil {
			t.Fatal(err)
		}
		destinations := map[core.LocationID]core.MapDestination{}
		for _, d := range view.Destinations {
			destinations[d.To.ID] = d
		}
		for to := 0; to < n; to++ {
			if to == start {
				continue
			}
			id := core.LocationID(fmt.Sprintf("loc:p%d", to))
			path, ticks, ok := m.best(start, to, false, false)
			w := world(t, b)
			batch, err := w.Execute(request(t, w, "go", fmt.Sprintf("travel party to %q;", id)))
			d, listed := destinations[id]
			if !ok {
				if listed {
					t.Fatalf("round %d: view lists unreachable %s", round, id)
				}
				_, _, any := m.best(start, to, true, true)
				_, _, closedOnly := m.best(start, to, false, true)
				_, _, unvisitedOnly := m.best(start, to, true, false)
				want := "closed route and unvisited stop"
				switch {
				case !any:
					want = "no route"
				case closedOnly:
					want = "unvisited stop"
				case unvisitedOnly:
					want = "closed route"
				}
				wantMapFault(t, err, "travel path", want)
				refusals++
				continue
			}
			if err != nil {
				t.Fatalf("round %d: travel to %s refused: %v\n%s", round, id, err, src.String())
			}
			stops, spent, halted := path, ticks, false
			for i, at := range path[:len(path)-1] {
				if m.hazard[at] {
					stops, halted = path[:i+1], true
					spent = 0
					prev := start
					for _, s := range stops {
						spent += m.routes[[2]int{prev, s}]
						prev = s
					}
					break
				}
			}
			want := core.Trip{From: core.LocationID(fmt.Sprintf("loc:p%d", start)), Ticks: spent, Halted: halted}
			for _, s := range stops {
				want.Stops = append(want.Stops, core.LocationID(fmt.Sprintf("loc:p%d", s)))
			}
			for _, at := range []int{start, stops[len(stops)-1]} {
				if !m.visited[at] {
					want.Visited = append(want.Visited, core.LocationID(fmt.Sprintf("loc:p%d", at)))
				}
			}
			if got := trip(t, batch); !reflect.DeepEqual(got, want) {
				t.Fatalf("round %d: trip to %s %+v, want %+v\n%s", round, id, got, want, src.String())
			}
			wantDest := core.MapDestination{To: core.Location{ID: id, Name: fmt.Sprintf("P%d", to)}, Ticks: ticks, Stops: len(path), Visited: m.visited[to]}
			if halted {
				wantDest.HaltsAt = &want.Stops[len(want.Stops)-1]
			}
			if !listed || !reflect.DeepEqual(d, wantDest) {
				t.Fatalf("round %d: view of %s %+v, want %+v", round, id, d, wantDest)
			}
			if w.Snapshot().Clock.Tick != uint64(spent) {
				t.Fatalf("round %d: clock %d, want %d", round, w.Snapshot().Clock.Tick, spent)
			}
			trips++
			if halted {
				halts++
			}
		}
	}
	if trips < 3000 || refusals < 2000 || halts < 300 {
		t.Fatalf("too few cases: %d trips, %d refusals, %d halts", trips, refusals, halts)
	}
	t.Logf("%d trips, %d refusals, %d halts", trips, refusals, halts)
}

// The numbers docs/WORLD_MAP.md quotes, from the example world.
func TestWorldMapExample(t *testing.T) {
	src, err := os.ReadFile("../examples/map/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	actions, err := os.ReadFile("../examples/map/travel.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("world.nql", string(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	p, err := w.MapView("char:ada")
	if err != nil {
		t.Fatal(err)
	}
	type dest struct {
		to      core.LocationID
		ticks   int64
		stops   int
		visited bool
		haltsAt core.LocationID
	}
	var got []dest
	for _, d := range p.Destinations {
		x := dest{d.To.ID, d.Ticks, d.Stops, d.Visited, ""}
		if d.HaltsAt != nil {
			x.haltsAt = *d.HaltsAt
		}
		got = append(got, x)
	}
	want := []dest{{"loc:valley/B02", 300, 1, true, ""}, {"loc:valley/B03", 600, 2, true, ""}, {"loc:valley/B04", 1200, 3, false, "loc:valley/B03"}}
	if !reflect.DeepEqual(got, want) || len(p.Exits) != 2 || p.Exits[1].To.ID != "loc:valley/B03" || p.Exits[1].Ticks != 900 || !p.Exits[1].Hazard {
		t.Fatalf("view %+v %+v", got, p.Exits)
	}

	// The trip toward the watchtower stops at the mill.
	r := execute(t, w, "go", string(actions))
	if got, want := trip(t, r), (core.Trip{From: "loc:valley/B01", Stops: locs("loc:valley/B02", "loc:valley/B03"), Ticks: 600, Halted: true}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	if w.Snapshot().Clock.Tick != 29400 {
		t.Fatalf("clock %d", w.Snapshot().Clock.Tick)
	}

	// The host clears the hazard in its next world; the party goes on.
	next := strings.NewReplacer(`clock "second" at 28800;`, `clock "second" at 29400;`, `at "loc:valley/B01"`, `at "loc:valley/B03"`, ` hazard "loc:valley/B03";`+"\n", "").Replace(string(src))
	b, ds = compiler.CompileWorld("world.nql", next)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w = world(t, b)
	r = execute(t, w, "on", string(actions))
	if got, want := trip(t, r), (core.Trip{From: "loc:valley/B03", Stops: locs("loc:valley/B04"), Ticks: 600, Visited: locs("loc:valley/B04")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	r = execute(t, w, "gate", `travel party to "loc:keep/A01";`)
	if got := trip(t, r); got.Ticks != 1800 || w.Snapshot().Clock.Tick != 31800 {
		t.Fatalf("trip %+v clock %d", got, w.Snapshot().Clock.Tick)
	}
	_, err = w.Execute(request(t, w, "back", `travel party to "loc:valley/B01";`))
	if err != nil {
		t.Fatal(err)
	}
}

// An Ability's travel step travels by the map, as the action does.
func TestWorldMapAbilityTravel(t *testing.T) {
	src := mapPlaces + `ability "ability:ford" named "To the ford" {
 travel party to "loc:ford";
 advance time by 5;
}
map {
` + mapRoutes + "}\n"
	b, ds := compiler.CompileWorld("map.nql", src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	r := execute(t, w, "go", `invoke "ability:ford" { };`)
	wantKinds(t, r, core.EntityMoved, core.EntityMoved, core.TimeAdvanced, core.PartyTraveled, core.TimeAdvanced, core.AbilityInvoked)
	if got := r.Events[3].Trip; got == nil || got.Ticks != 10 || !reflect.DeepEqual(got.Stops, locs("loc:ford")) || w.Snapshot().Clock.Tick != 115 {
		t.Fatalf("trip %+v clock %d", got, w.Snapshot().Clock.Tick)
	}
	wantAt(t, w, "loc:ford", "char:ada", "char:bea")
	costReplay(t, b, w)
}
