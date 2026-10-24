package livestate

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func count(n int64) *int64 { return &n }

// sealed is a document the engine could have written: two places, two
// occupants and three recent requests.
func sealed() Document {
	d := Document{Format: Format, Version: Version, Revision: 3,
		Places: []core.LocationID{"loc:camp", "loc:ford"},
		Occupants: []core.Occupant{
			{ID: "occ:mira", Name: "Mira", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(1), Attitude: core.AttitudeFriendly},
			{ID: "occ:wolves", Name: "Wolves", Location: "loc:ford", Kind: core.OccupantCreatures, Count: count(1), Attitude: core.AttitudeHostile, Tally: []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 2, Reason: "smoke"}}},
		},
		Requests: []Request{{ID: "r1", Revision: 1, Digest: strings.Repeat("a", 64)}, {ID: "r2", Revision: 2, Digest: strings.Repeat("b", 64)}, {ID: "r3", Revision: 3, Digest: strings.Repeat("0", 64)}},
	}
	d.Checksum = checksum(d)
	return d
}

// mapped is sealed with a map: three characters, two of them the party.
func mapped() Document {
	d := sealed()
	d.Places = append(d.Places, "loc:mill")
	d.Characters = []Character{{ID: "char:a", Location: "loc:ford"}, {ID: "char:b", Location: "loc:ford"}, {ID: "char:c", Location: "loc:camp"}}
	d.Map = &Map{
		Party:   []core.CharacterID{"char:a", "char:b"},
		Visited: []core.LocationID{"loc:camp", "loc:ford"},
		Hazards: []core.LocationID{"loc:mill"},
		Closed:  []core.MapLink{{From: "loc:camp", To: "loc:ford"}, {From: "loc:ford", To: "loc:mill"}},
	}
	d.Checksum = checksum(d)
	return d
}

func text(s string) *string { return &s }

// quested is sealed with three held quests: one started with a note, one
// unstarted, and one completed ahead of the unstarted one.
func quested() Document {
	d := sealed()
	tick := uint64(40)
	d.Quests = []Quest{
		{ID: "quest:herbs", Status: core.StatusStarted, Log: []core.QuestEntry{{Kind: core.EntryStarted, Text: text("Mira needs feverfew"), Tick: &tick}, {Kind: core.EntryNote, Text: text("found some")}}},
		{ID: "quest:mill", Status: core.StatusUnstarted},
		{ID: "quest:tower", Status: core.StatusCompleted, Log: []core.QuestEntry{{Kind: core.EntryCompleted, Ahead: []core.QuestID{"quest:mill"}, Reason: text("slipped past")}}},
	}
	d.Checksum = checksum(d)
	return d
}

// clocked is sealed with a held clock.
func clocked() Document {
	d := sealed()
	d.Clock = &core.Clock{Unit: "second", Tick: 900}
	d.Checksum = checksum(d)
	return d
}

func wantFault(t *testing.T, err error, code, field, actual string) {
	t.Helper()
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != code || f.Field != field || f.Actual != actual {
		t.Fatalf("got %#v, want %s %s %q", err, code, field, actual)
	}
}

func TestCheck(t *testing.T) {
	d := sealed()
	if err := Check(&d); err != nil {
		t.Fatal(err)
	}
	// A document read back from JSON checks the same.
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back Document
	if err := json.Unmarshal(raw, &back); err != nil || Check(&back) != nil {
		t.Fatalf("round trip: %v %v", err, Check(&back))
	}
	// So do one with a map, one with quests, and version 1 and 2 documents.
	v2 := mapped()
	v2.Version = 2
	v2.Checksum = checksum(v2)
	for _, v := range []Document{mapped(), quested(), clocked(), v2, sealed()} {
		if v.Map == nil && v.Quests == nil && v.Clock == nil {
			v.Version = 1
			v.Checksum = checksum(v)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var back Document
		if err := json.Unmarshal(raw, &back); err != nil || Check(&back) != nil || !reflect.DeepEqual(back, v) {
			t.Fatalf("version %d round trip: %v %v", v.Version, err, Check(&back))
		}
	}
	// Empty lists and absent ones are the same document.
	e := Document{Format: Format, Version: 1}
	e.Checksum = checksum(e)
	m := Document{Format: Format, Version: 2, Map: &Map{}}
	m.Checksum = checksum(m)
	q := Document{Format: Format, Version: 3}
	q.Checksum = checksum(q)
	c := Document{Format: Format, Version: 4, Clock: &core.Clock{Unit: "second"}}
	c.Checksum = checksum(c)
	for _, text := range []string{
		`{"format":"nql-live-state","version":1,"revision":0,"places":[],"occupants":[],"requests":[],"checksum":"` + e.Checksum + `"}`,
		`{"format":"nql-live-state","version":1,"revision":0,"places":null,"occupants":null,"requests":null,"checksum":"` + e.Checksum + `"}`,
		`{"format":"nql-live-state","version":2,"revision":0,"places":[],"occupants":[],"characters":[],"map":{"party":[],"visited":[],"hazards":[],"closed":[]},"requests":[],"checksum":"` + m.Checksum + `"}`,
		`{"format":"nql-live-state","version":2,"revision":0,"places":null,"occupants":null,"map":{"party":null,"visited":null,"hazards":null,"closed":null},"requests":null,"checksum":"` + m.Checksum + `"}`,
		`{"format":"nql-live-state","version":3,"revision":0,"places":[],"occupants":[],"quests":[],"requests":[],"checksum":"` + q.Checksum + `"}`,
		`{"format":"nql-live-state","version":3,"revision":0,"places":[],"occupants":[],"quests":null,"requests":[],"checksum":"` + q.Checksum + `"}`,
		`{"format":"nql-live-state","version":4,"revision":0,"places":[],"occupants":[],"clock":{"unit":"second","tick":0},"requests":[],"checksum":"` + c.Checksum + `"}`,
	} {
		var v Document
		if err := json.Unmarshal([]byte(text), &v); err != nil || Check(&v) != nil {
			t.Fatalf("%s: %v %v", text, err, Check(&v))
		}
	}
	// Closures sort by where they start, then where they end.
	v := mapped()
	v.Map.Closed = []core.MapLink{{From: "loc:camp", To: "loc:mill"}, {From: "loc:ford", To: "loc:camp"}}
	v.Checksum = checksum(v)
	if err := Check(&v); err != nil {
		t.Fatal(err)
	}
	// The map is part of the checksum, and so are the quests.
	v = mapped()
	v.Map.Visited = v.Map.Visited[:1]
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")
	v = quested()
	*v.Quests[0].Log[1].Text = "found none"
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")

	refused := func(name string, edit func(*Document), field, actual string) {
		t.Helper()
		v := sealed()
		edit(&v)
		v.Checksum = checksum(v)
		err := Check(&v)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != field || f.Actual != actual {
			t.Fatalf("%s: got %#v, want %s %q", name, err, field, actual)
		}
	}
	refused("format", func(v *Document) { v.Format = "nql" }, "format", "nql")
	refused("version 0", func(v *Document) { v.Version = 0 }, "version", "0")
	refused("version 6", func(v *Document) { v.Version = 6 }, "version", "6")
	refused("places unsorted", func(v *Document) { v.Places[0], v.Places[1] = v.Places[1], v.Places[0] }, "places", "loc:camp")
	refused("place twice", func(v *Document) { v.Places[1] = "loc:camp" }, "places", "loc:camp")
	refused("blank place", func(v *Document) { v.Places = append([]core.LocationID{" "}, v.Places...) }, "places", " ")
	refused("occupants unsorted", func(v *Document) { v.Occupants[0], v.Occupants[1] = v.Occupants[1], v.Occupants[0] }, "occupants", "occ:mira")
	refused("occupant twice", func(v *Document) { v.Occupants[1].ID = "occ:mira" }, "occupants", "occ:mira")
	refused("occupant elsewhere", func(v *Document) { v.Occupants[1].Location = "loc:mill" }, "location", "loc:mill")
	refused("revision at the maximum", func(v *Document) { v.Revision = math.MaxUint64 }, "revision", "maximum")
	refused("fewer requests", func(v *Document) { v.Requests = v.Requests[1:] }, "requests", "2")
	refused("more requests", func(v *Document) { v.Revision = 2 }, "requests", "3")
	refused("request twice", func(v *Document) { v.Requests[2].ID = "r1" }, "request", "r1")
	refused("blank request", func(v *Document) { v.Requests[0].ID = "\t" }, "request", "\t")
	refused("request revision", func(v *Document) { v.Requests[1].Revision = 3 }, "revision", "3")
	refused("first revision", func(v *Document) { v.Requests[0].Revision = 0 }, "revision", "0")
	refused("short digest", func(v *Document) { v.Requests[0].Digest = "abcd" }, "digest", "abcd")
	refused("long digest", func(v *Document) { v.Requests[0].Digest += "aa" }, "digest", strings.Repeat("a", 66))
	refused("upper digest", func(v *Document) { v.Requests[0].Digest = strings.Repeat("A", 64) }, "digest", strings.Repeat("A", 64))
	refused("not hex", func(v *Document) { v.Requests[0].Digest = strings.Repeat("g", 64) }, "digest", strings.Repeat("g", 64))

	// The characters and the map.
	mapRefused := func(name string, edit func(*Document), field, actual string) {
		t.Helper()
		v := mapped()
		edit(&v)
		v.Checksum = checksum(v)
		var f *core.Fault
		if err := Check(&v); !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != field || f.Actual != actual {
			t.Fatalf("%s: got %#v, want %s %q", name, err, field, actual)
		}
	}
	mapRefused("version 1 with a map", func(v *Document) { v.Version = 1 }, "version", "1")
	mapRefused("version 1 with characters", func(v *Document) { v.Version, v.Map = 1, nil }, "version", "1")
	mapRefused("version 1 with a map alone", func(v *Document) { v.Version, v.Characters, v.Map.Party = 1, nil, nil }, "version", "1")
	mapRefused("characters without a map", func(v *Document) { v.Map = nil }, "characters", "no map")
	mapRefused("characters unsorted", func(v *Document) { v.Characters[0], v.Characters[1] = v.Characters[1], v.Characters[0] }, "characters", "char:a")
	mapRefused("character twice", func(v *Document) { v.Characters[1].ID = "char:a" }, "characters", "char:a")
	mapRefused("blank character", func(v *Document) {
		v.Characters = append([]Character{{ID: " ", Location: "loc:camp"}}, v.Characters...)
	}, "characters", " ")
	mapRefused("character elsewhere", func(v *Document) { v.Characters[2].Location = "loc:keep" }, "location", "loc:keep")
	mapRefused("party unsorted", func(v *Document) { v.Map.Party[0], v.Map.Party[1] = v.Map.Party[1], v.Map.Party[0] }, "party", "char:a")
	mapRefused("party twice", func(v *Document) { v.Map.Party[1] = "char:a" }, "party", "char:a")
	mapRefused("party member not held", func(v *Document) { v.Map.Party = append(v.Map.Party, "char:d") }, "party", "char:d")
	mapRefused("visited unsorted", func(v *Document) { v.Map.Visited[0], v.Map.Visited[1] = v.Map.Visited[1], v.Map.Visited[0] }, "visited", "loc:camp")
	mapRefused("visited twice", func(v *Document) { v.Map.Visited[1] = "loc:camp" }, "visited", "loc:camp")
	mapRefused("visited not held", func(v *Document) { v.Map.Visited = append(v.Map.Visited, "loc:keep") }, "visited", "loc:keep")
	mapRefused("hazard twice", func(v *Document) { v.Map.Hazards = append(v.Map.Hazards, "loc:mill") }, "hazards", "loc:mill")
	mapRefused("hazard not held", func(v *Document) { v.Map.Hazards = []core.LocationID{"loc:keep"} }, "hazards", "loc:keep")
	mapRefused("closed unsorted", func(v *Document) { v.Map.Closed[0], v.Map.Closed[1] = v.Map.Closed[1], v.Map.Closed[0] }, "closed", "loc:camp -> loc:ford")
	mapRefused("closed twice", func(v *Document) { v.Map.Closed[1] = v.Map.Closed[0] }, "closed", "loc:camp -> loc:ford")
	mapRefused("closed to a place not held", func(v *Document) { v.Map.Closed[1].To = "loc:keep" }, "closed", "loc:ford -> loc:keep")
	mapRefused("closed from a place not held", func(v *Document) { v.Map.Closed[1].From = "loc:keep" }, "closed", "loc:keep -> loc:mill")
	mapRefused("closed by the end", func(v *Document) {
		v.Map.Closed = []core.MapLink{{From: "loc:ford", To: "loc:mill"}, {From: "loc:ford", To: "loc:camp"}}
	}, "closed", "loc:ford -> loc:camp")

	// The quests.
	questRefused := func(name string, edit func(*Document), field, actual string) {
		t.Helper()
		v := quested()
		edit(&v)
		v.Checksum = checksum(v)
		var f *core.Fault
		if err := Check(&v); !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != field || f.Actual != actual {
			t.Fatalf("%s: got %#v, want %s %q", name, err, field, actual)
		}
	}
	questRefused("version 2 with quests", func(v *Document) { v.Version = 2 }, "version", "2")
	questRefused("version 1 with quests", func(v *Document) { v.Version = 1 }, "version", "1")
	questRefused("quests unsorted", func(v *Document) { v.Quests[1], v.Quests[2] = v.Quests[2], v.Quests[1] }, "quests", "quest:mill")
	questRefused("quest twice", func(v *Document) { v.Quests[2].ID = "quest:mill" }, "quests", "quest:mill")
	questRefused("blank quest", func(v *Document) { v.Quests = append([]Quest{{ID: " "}}, v.Quests...) }, "quests", " ")
	questRefused("control in a quest", func(v *Document) { v.Quests[2].ID = "quest:\x00" }, "quests", "quest:\x00")

	// The clock. It is part of the checksum.
	v = clocked()
	v.Clock.Tick++
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")
	clockRefused := func(name string, edit func(*Document), field, actual string) {
		t.Helper()
		v := clocked()
		edit(&v)
		v.Checksum = checksum(v)
		var f *core.Fault
		if err := Check(&v); !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != field || f.Actual != actual {
			t.Fatalf("%s: got %#v, want %s %q", name, err, field, actual)
		}
	}
	clockRefused("version 3 with a clock", func(v *Document) { v.Version = 3 }, "version", "3")
	clockRefused("version 1 with a clock", func(v *Document) { v.Version = 1 }, "version", "1")
	clockRefused("blank unit", func(v *Document) { v.Clock.Unit = " " }, "clock", " ")
	clockRefused("no unit", func(v *Document) { v.Clock.Unit = "" }, "clock", "")
	clockRefused("control in a unit", func(v *Document) { v.Clock.Unit = "sec\x00" }, "clock", "sec\x00")

	// A full window ends at the revision.
	v = sealed()
	v.Revision, v.Requests = 100, nil
	for i := uint64(37); i <= 100; i++ {
		v.Requests = append(v.Requests, Request{ID: "r" + strconv.FormatUint(i, 10), Revision: i, Digest: strings.Repeat("c", 64)})
	}
	v.Checksum = checksum(v)
	if err := Check(&v); err != nil {
		t.Fatal(err)
	}
	v.Requests = append([]Request{{ID: "r36", Revision: 36, Digest: strings.Repeat("c", 64)}}, v.Requests...)
	v.Checksum = checksum(v)
	wantFault(t, Check(&v), "E_LIVE_STATE", "requests", "65")

	// Damage is found before anything else.
	v = sealed()
	v.Occupants[0].Name = "Mara"
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")
	v = sealed()
	v.Checksum = ""
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "absent")
	v = sealed()
	v.Checksum = strings.ToUpper(v.Checksum)
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")
}

func TestMerge(t *testing.T) {
	d := sealed()
	b := core.Blueprint{
		Locations: []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:keep"}},
		Occupants: []core.Occupant{
			{ID: "occ:wolves", Location: "loc:ford", Name: "Many Wolves"},
			{ID: "occ:bears", Location: "loc:camp"},
			{ID: "occ:rats", Location: "loc:keep"},
		},
	}
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	var got []core.OccupantID
	for _, o := range b.Occupants {
		got = append(got, o.ID)
	}
	if want := []core.OccupantID{"occ:mira", "occ:wolves", "occ:rats"}; !reflect.DeepEqual(got, want) || b.Occupants[1].Name != "Wolves" {
		t.Fatalf("merged %v %+v", got, b.Occupants)
	}
	// Nothing anywhere stays nothing, so a world without occupants encodes
	// as it does without live state.
	b = core.Blueprint{Locations: []core.Location{{ID: "loc:camp"}}}
	if err := Merge(&b, &Document{Occupants: []core.Occupant{}}); err != nil || b.Occupants != nil {
		t.Fatalf("empty: %v %+v", err, b.Occupants)
	}
	b = core.Blueprint{Locations: []core.Location{{ID: "loc:camp"}}, Occupants: []core.Occupant{{ID: "occ:x", Location: "loc:camp"}}}
	if err := Merge(&b, &Document{Places: []core.LocationID{"loc:camp"}, Occupants: []core.Occupant{}}); err != nil || b.Occupants != nil {
		t.Fatalf("all held: %v %+v", err, b.Occupants)
	}
	// A place the world no longer declares is named.
	b = core.Blueprint{Locations: []core.Location{{ID: "loc:camp"}, {ID: "loc:keep"}}, Occupants: []core.Occupant{{ID: "occ:x", Location: "loc:keep"}}}
	err := Merge(&b, &d)
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Subject != "loc:ford" || f.Field != "place" || f.Actual != "not declared" || len(b.Occupants) != 1 {
		t.Fatalf("place gone: %#v %+v", err, b.Occupants)
	}
}

// mapWorld is a world with a map for mapped(): the source puts everyone at
// the camp, declares a new character and drops char:c.
func mapWorld() core.Blueprint {
	return core.Blueprint{
		Locations: []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:keep"}, {ID: "loc:mill"}},
		// The source's closure is the source's: the document's replace it.
		Characters: []core.Character{{ID: "char:a", Location: "loc:camp"}, {ID: "char:b", Location: "loc:camp"}, {ID: "char:new", Location: "loc:keep"}},
		Map: &core.WorldMap{
			Routes: []core.MapRoute{{From: "loc:camp", To: "loc:ford", Ticks: 5}, {From: "loc:ford", To: "loc:camp", Ticks: 5}},
			Party:  []core.CharacterID{"char:new"},
			Closed: []core.MapLink{{From: "loc:ford", To: "loc:camp"}},
		},
	}
}

func TestMergeMap(t *testing.T) {
	d := mapped()
	b := mapWorld()
	source := b
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	// Held characters stand where the document says; a new one where the
	// source says; one the world no longer declares is gone.
	want := []core.Character{{ID: "char:a", Location: "loc:ford"}, {ID: "char:b", Location: "loc:ford"}, {ID: "char:new", Location: "loc:keep"}}
	if !reflect.DeepEqual(b.Characters, want) {
		t.Fatalf("characters %+v", b.Characters)
	}
	// The map state is the document's; a closure of a route the world no
	// longer declares is held out of the world; the routes are the source's.
	m := b.Map
	if !reflect.DeepEqual(m.Party, d.Map.Party) || !reflect.DeepEqual(m.Visited, d.Map.Visited) || !reflect.DeepEqual(m.Hazards, d.Map.Hazards) ||
		!reflect.DeepEqual(m.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}}) || len(m.Routes) != 2 {
		t.Fatalf("map %+v", m)
	}
	// The source itself is untouched: it may still be needed to place a fault.
	if source.Characters[0].Location != "loc:camp" || !reflect.DeepEqual(source.Map.Party, []core.CharacterID{"char:new"}) || len(source.Map.Closed) != 1 {
		t.Fatalf("source changed %+v %+v", source.Characters, source.Map)
	}
	// Changing the merged lists leaves the document alone.
	m.Party[0], m.Visited[0], m.Hazards[0] = "char:x", "loc:x", "loc:x"
	if d.Map.Party[0] != "char:a" || d.Map.Visited[0] != "loc:camp" || d.Map.Hazards[0] != "loc:mill" {
		t.Fatalf("document changed %+v", d.Map)
	}

	// A party member the world no longer declares is refused.
	b = mapWorld()
	b.Characters = b.Characters[1:]
	d = mapped()
	var f *core.Fault
	if err := Merge(&b, &d); !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Subject != "char:a" || f.Field != "party" || f.Expected != "a character the world declares" || f.Actual != "not declared" {
		t.Fatalf("party member gone: %#v", err)
	}
	// A world that declares no map holds the document's characters and map
	// and ignores them: its characters stand where its source says, even
	// without a party member the document holds, and its occupants still
	// come from the document.
	b = mapWorld()
	b.Map = nil
	b.Characters = b.Characters[1:]
	if err := Merge(&b, &d); err != nil || b.Map != nil || !reflect.DeepEqual(b.Characters, mapWorld().Characters[1:]) || !reflect.DeepEqual(b.Occupants, d.Occupants) {
		t.Fatalf("map gone: %v %+v %+v", err, b.Characters, b.Map)
	}
	if !reflect.DeepEqual(d, mapped()) {
		t.Fatalf("held document changed %+v", d)
	}
	// A document without a map leaves the source's characters and map: a
	// world that declares a map takes its state from the source once.
	for _, v := range []Document{sealed(), {Version: 1}} {
		b = mapWorld()
		b.Locations = append(b.Locations, core.Location{ID: "loc:ford"})
		if err := Merge(&b, &v); err != nil || !reflect.DeepEqual(b.Characters, mapWorld().Characters) || !reflect.DeepEqual(b.Map, mapWorld().Map) {
			t.Fatalf("no map: %v %+v %+v", err, b.Characters, b.Map)
		}
	}
}

// newPlaceWorld is mapWorld with the keep and a new gate joined by routes,
// and the source's hazards, closures and visited marks on held and new
// places, some stated twice.
func newPlaceWorld() core.Blueprint {
	b := mapWorld()
	b.Locations = append(b.Locations, core.Location{ID: "loc:gate"})
	m := b.Map
	m.Routes = append(m.Routes,
		core.MapRoute{From: "loc:ford", To: "loc:keep", Ticks: 5},
		core.MapRoute{From: "loc:keep", To: "loc:gate", Ticks: 5},
		core.MapRoute{From: "loc:gate", To: "loc:keep", Ticks: 5})
	m.Hazards = []core.LocationID{"loc:camp", "loc:keep", "loc:mill", "loc:keep"}
	m.Closed = []core.MapLink{{From: "loc:ford", To: "loc:camp"}, {From: "loc:ford", To: "loc:keep"}, {From: "loc:keep", To: "loc:gate"}, {From: "loc:keep", To: "loc:gate"}}
	m.Visited = []core.LocationID{"loc:keep", "loc:mill"}
	return b
}

func TestMergeNewPlaceMapState(t *testing.T) {
	d := mapped()
	b := newPlaceWorld()
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	// The keep and the gate are new: the keep's hazard is taken once, and
	// the closures that touch either, the one from the held ford included.
	// The held camp's hazard and the held ford-camp closure are the
	// document's, and no visited mark is taken.
	m := b.Map
	if !reflect.DeepEqual(m.Hazards, []core.LocationID{"loc:mill", "loc:keep"}) {
		t.Fatalf("hazards %v", m.Hazards)
	}
	if !reflect.DeepEqual(m.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}, {From: "loc:ford", To: "loc:keep"}, {From: "loc:keep", To: "loc:gate"}}) {
		t.Fatalf("closed %v", m.Closed)
	}
	if !reflect.DeepEqual(m.Visited, d.Map.Visited) {
		t.Fatalf("visited %v", m.Visited)
	}
	if !reflect.DeepEqual(d, mapped()) {
		t.Fatalf("document changed %+v", d)
	}

	// Once the document holds the keep and the gate, the source's lines on
	// them are ignored: a hazard cleared and a route opened in play stay so.
	d = mapped()
	d.Places = append(d.Places, "loc:gate", "loc:keep")
	sort.Slice(d.Places, func(i, j int) bool { return d.Places[i] < d.Places[j] })
	b = newPlaceWorld()
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Map.Hazards, d.Map.Hazards) || !reflect.DeepEqual(b.Map.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}}) {
		t.Fatalf("held places took the source: %v %v", b.Map.Hazards, b.Map.Closed)
	}
}

// questWorld declares the tower and the mill of quested() and a new well,
// each with a log of its own, and no longer declares the herbs.
func questWorld() core.Blueprint {
	return core.Blueprint{Locations: []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}, Quests: []core.Quest{
		{ID: "quest:mill", Name: "Mill", Status: core.StatusStarted, Log: []core.QuestEntry{{Kind: core.EntryStarted}}},
		{ID: "quest:tower", Name: "Tower", Requires: []core.QuestID{"quest:mill"}, Status: core.StatusFailed, Log: []core.QuestEntry{{Kind: core.EntryFailed}}},
		{ID: "quest:well", Name: "Well", Status: core.StatusStarted, Log: []core.QuestEntry{{Kind: core.EntryStarted, Text: text("dry")}}},
	}}
}

func TestMergeQuests(t *testing.T) {
	d := quested()
	b := questWorld()
	source := questWorld()
	b.Quests = source.Quests
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	// Held quests take the document's status and log, and keep the source's
	// name and requirements; a new one keeps the source's; one the world no
	// longer declares is not in the world.
	want := []core.Quest{
		{ID: "quest:mill", Name: "Mill", Status: core.StatusUnstarted},
		{ID: "quest:tower", Name: "Tower", Requires: []core.QuestID{"quest:mill"}, Status: core.StatusCompleted, Log: d.Quests[2].Log},
		questWorld().Quests[2],
	}
	if !reflect.DeepEqual(b.Quests, want) {
		t.Fatalf("quests %+v", b.Quests)
	}
	// The source itself is untouched, and changing the merged log leaves the
	// document alone.
	if !reflect.DeepEqual(source.Quests, questWorld().Quests) {
		t.Fatalf("source changed %+v", source.Quests)
	}
	b.Quests[1].Log[0].Kind = core.EntryFailed
	if d.Quests[2].Log[0].Kind != core.EntryCompleted {
		t.Fatalf("document changed %+v", d.Quests[2])
	}
	// A document without quests leaves the source's: a world takes its
	// quests from the source once.
	for _, v := range []Document{sealed(), {Version: 1}} {
		b = questWorld()
		if err := Merge(&b, &v); err != nil || !reflect.DeepEqual(b.Quests, questWorld().Quests) {
			t.Fatalf("no quests: %v %+v", err, b.Quests)
		}
	}
}

func TestNextQuests(t *testing.T) {
	d := quested()
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}
	s.Occupants = d.Occupants
	s.Quests = questWorld().Quests
	s.Quests[0].Status, s.Quests[0].Log = core.StatusUnstarted, []core.QuestEntry{}
	n := Next(d, s, "", "")
	// The declared quests as they are now, the herbs as they were.
	want := []Quest{
		d.Quests[0],
		{ID: "quest:mill", Status: core.StatusUnstarted},
		{ID: "quest:tower", Status: core.StatusFailed, Log: s.Quests[1].Log},
		{ID: "quest:well", Status: core.StatusStarted, Log: s.Quests[2].Log},
	}
	if n.Version != Version || !reflect.DeepEqual(n.Quests, want) || Check(&n) != nil {
		t.Fatalf("quests %+v %v", n.Quests, Check(&n))
	}
	// An empty log is left out, never null.
	raw, err := json.Marshal(n)
	if err != nil || !strings.Contains(string(raw), `{"id":"quest:mill","status":"unstarted"}`) {
		t.Fatalf("empty log %s %v", raw, err)
	}
	// The snapshot's logs are not shared with the document.
	n.Quests[2].Log[0].Kind = core.EntryStarted
	if s.Quests[1].Log[0].Kind != core.EntryFailed {
		t.Fatalf("snapshot shared %+v", s.Quests[1])
	}
	// A held quest stays held through calls that do not declare it.
	s.Quests = nil
	n = Next(quested(), s, "", "")
	if again := Next(n, s, "r4", strings.Repeat("d", 64)); !reflect.DeepEqual(again.Quests, quested().Quests) || Check(&again) != nil {
		t.Fatalf("kept %+v", again.Quests)
	}
	// No quests anywhere, no quests in the document.
	raw, err = json.Marshal(Next(sealed(), s, "", ""))
	if err != nil || strings.Contains(string(raw), `"quests"`) {
		t.Fatalf("no quests %s %v", raw, err)
	}
}

func TestNext(t *testing.T) {
	d := sealed()
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:keep"}}
	s.Occupants = d.Occupants
	n := Next(d, s, "", "")
	if n.Revision != 3 || !reflect.DeepEqual(n.Requests, d.Requests) || len(n.Places) != 3 || n.Checksum != checksum(n) || Check(&n) != nil {
		t.Fatalf("no request %+v", n)
	}
	// The same state gives the same document.
	s.Locations = s.Locations[:2]
	if again := Next(d, s, "", ""); !reflect.DeepEqual(again, d) {
		t.Fatalf("unchanged %+v\n%+v", again, d)
	}
	n = Next(d, s, "r4", strings.Repeat("d", 64))
	if n.Revision != 4 || len(n.Requests) != 4 || n.Requests[3] != (Request{ID: "r4", Revision: 4, Digest: strings.Repeat("d", 64)}) || Check(&n) != nil {
		t.Fatalf("one request %+v", n)
	}
	// The window keeps the last 64.
	for i := 5; i <= 70; i++ {
		n = Next(n, s, "r"+strconv.Itoa(i), strings.Repeat("d", 64))
	}
	if n.Revision != 70 || len(n.Requests) != MaxRequests || n.Requests[0].ID != "r7" || n.Requests[0].Revision != 7 || Check(&n) != nil {
		t.Fatalf("window %d %+v", len(n.Requests), n.Requests[0])
	}
	// An empty world gives empty lists, never null.
	raw, err := json.Marshal(Next(Document{}, core.Snapshot{}, "", ""))
	if err != nil || !strings.Contains(string(raw), `"places":[],"occupants":[],"requests":[]`) {
		t.Fatalf("empty %s %v", raw, err)
	}
	// Version 1 to 3 documents come back as the current version.
	for _, old := range []int{1, 2, 3} {
		v := sealed()
		v.Version = old
		if n := Next(v, s, "", ""); n.Version != Version || n.Map != nil || n.Characters != nil || n.Quests != nil || n.Clock != nil || Check(&n) != nil {
			t.Fatalf("upgrade from %d %+v", old, n)
		}
	}
}

func TestNextMap(t *testing.T) {
	d := sealed()
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:mill"}}
	s.Occupants = d.Occupants
	s.Characters = []core.Character{{ID: "char:c", Location: "loc:camp"}, {ID: "char:a", Location: "loc:ford"}}
	s.Map = &core.WorldMap{
		Routes:  []core.MapRoute{{From: "loc:camp", To: "loc:ford", Ticks: 5}},
		Party:   []core.CharacterID{"char:c", "char:a"},
		Visited: []core.LocationID{"loc:ford", "loc:camp"},
		Hazards: []core.LocationID{"loc:mill", "loc:ford"},
		Closed:  []core.MapLink{{From: "loc:ford", To: "loc:camp"}, {From: "loc:camp", To: "loc:ford"}},
	}
	n := Next(d, s, "", "")
	want := &Map{
		Party:   []core.CharacterID{"char:a", "char:c"},
		Visited: []core.LocationID{"loc:camp", "loc:ford"},
		Hazards: []core.LocationID{"loc:ford", "loc:mill"},
		Closed:  []core.MapLink{{From: "loc:camp", To: "loc:ford"}, {From: "loc:ford", To: "loc:camp"}},
	}
	if n.Version != Version || !reflect.DeepEqual(n.Characters, []Character{{ID: "char:a", Location: "loc:ford"}, {ID: "char:c", Location: "loc:camp"}}) || !reflect.DeepEqual(n.Map, want) || Check(&n) != nil {
		t.Fatalf("map %+v %+v %v", n.Characters, n.Map, Check(&n))
	}
	// The snapshot's lists are not shared with the document.
	if s.Map.Party[0] != "char:c" || s.Characters[0].ID != "char:c" {
		t.Fatalf("snapshot sorted in place %+v", s.Map)
	}
	// An empty map gives empty lists, never null.
	s.Characters, s.Map = nil, &core.WorldMap{}
	raw, err := json.Marshal(Next(d, s, "", ""))
	if err != nil || !strings.Contains(string(raw), `"map":{"party":[],"visited":[],"hazards":[],"closed":[]}`) || strings.Contains(string(raw), "characters") {
		t.Fatalf("empty map %s %v", raw, err)
	}

	// A world without a map keeps what the document holds of one, as it was,
	// wherever its own characters stand.
	held := mapped()
	s = core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:keep"}, {ID: "loc:mill"}}
	s.Occupants = held.Occupants
	s.Characters = []core.Character{{ID: "char:a", Location: "loc:keep"}}
	n = Next(held, s, "r4", strings.Repeat("c", 64))
	if !reflect.DeepEqual(n.Characters, held.Characters) || !reflect.DeepEqual(n.Map, held.Map) || n.Revision != 4 || Check(&n) != nil {
		t.Fatalf("held map %+v %+v %d %v", n.Characters, n.Map, n.Revision, Check(&n))
	}
	// The kept lists are copies.
	n.Characters[0].Location, n.Map.Party[0], n.Map.Visited[0], n.Map.Hazards[0], n.Map.Closed[0].From = "loc:x", "char:x", "loc:x", "loc:x", "loc:x"
	if !reflect.DeepEqual(held, mapped()) {
		t.Fatalf("previous document changed %+v %+v", held.Characters, held.Map)
	}
	// An empty held map stays a map with empty lists.
	empty := sealed()
	empty.Map = &Map{}
	empty.Checksum = checksum(empty)
	raw, err = json.Marshal(Next(empty, s, "", ""))
	if err != nil || !strings.Contains(string(raw), `"map":{"party":[],"visited":[],"hazards":[],"closed":[]}`) || strings.Contains(string(raw), "characters") {
		t.Fatalf("empty held map %s %v", raw, err)
	}
	// A document without a map still gets none from a world without one.
	if n := Next(sealed(), s, "", ""); n.Map != nil || n.Characters != nil {
		t.Fatalf("no map %+v %+v", n.Characters, n.Map)
	}
}

// A closure of a route the world does not declare is held: the world does
// not get it, and the next document keeps it. A closure of a declared route
// is the world's, so one play opened is gone.
func TestNextHeldClosures(t *testing.T) {
	d := mapped()
	b := mapWorld()
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Map.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}}) {
		t.Fatalf("world closures %v", b.Map.Closed)
	}
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}, {ID: "loc:keep"}, {ID: "loc:mill"}}
	s.Occupants = d.Occupants
	s.Characters = []core.Character{{ID: "char:a", Location: "loc:ford"}}
	s.Map = &core.WorldMap{Routes: b.Map.Routes, Party: []core.CharacterID{"char:a"}, Closed: []core.MapLink{{From: "loc:camp", To: "loc:ford"}}}
	n := Next(d, s, "", "")
	if want := []core.MapLink{{From: "loc:camp", To: "loc:ford"}, {From: "loc:ford", To: "loc:mill"}}; !reflect.DeepEqual(n.Map.Closed, want) || Check(&n) != nil {
		t.Fatalf("closed %v %v", n.Map.Closed, Check(&n))
	}
	// Opened in play: the declared route's closure is gone, the held one stays.
	s.Map.Closed = nil
	if n := Next(d, s, "", ""); !reflect.DeepEqual(n.Map.Closed, []core.MapLink{{From: "loc:ford", To: "loc:mill"}}) {
		t.Fatalf("opened %v", n.Map.Closed)
	}
	// A held closure that sorts before the world's keeps the list sorted.
	early := mapped()
	early.Map.Closed = []core.MapLink{{From: "loc:camp", To: "loc:mill"}, {From: "loc:ford", To: "loc:camp"}}
	early.Checksum = checksum(early)
	s.Map.Closed = []core.MapLink{{From: "loc:ford", To: "loc:camp"}}
	if n := Next(early, s, "", ""); !reflect.DeepEqual(n.Map.Closed, early.Map.Closed) || Check(&n) != nil {
		t.Fatalf("order %v %v", n.Map.Closed, Check(&n))
	}
	// Declared again, the held closure is the world's once more.
	b = mapWorld()
	b.Map.Routes = append(b.Map.Routes, core.MapRoute{From: "loc:ford", To: "loc:mill", Ticks: 5})
	d = mapped()
	if err := Merge(&b, &d); err != nil || !reflect.DeepEqual(b.Map.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}, {From: "loc:ford", To: "loc:mill"}}) {
		t.Fatalf("declared again %v %v", err, b.Map.Closed)
	}
}

// TestHeldMapRoundTrip runs a document through a world with a map, one
// without and the first again: the map state comes back as it was, and the
// characters stand where the document says, not where either source says.
func TestHeldMapRoundTrip(t *testing.T) {
	d := mapped()
	snap := func(b core.Blueprint) core.Snapshot {
		var s core.Snapshot
		s.Locations, s.Occupants, s.Characters, s.Map = b.Locations, b.Occupants, b.Characters, b.Map
		return s
	}
	plain := mapWorld()
	plain.Map = nil
	plain.Characters = []core.Character{{ID: "char:a", Location: "loc:mill"}, {ID: "char:b", Location: "loc:mill"}}
	if err := Merge(&plain, &d); err != nil {
		t.Fatal(err)
	}
	between := Next(d, snap(plain), "", "")
	if err := Check(&between); err != nil || !reflect.DeepEqual(between.Map, d.Map) || !reflect.DeepEqual(between.Characters, d.Characters) {
		t.Fatalf("between %v %+v %+v", err, between.Characters, between.Map)
	}
	b := mapWorld()
	if err := Merge(&b, &between); err != nil {
		t.Fatal(err)
	}
	after := Next(between, snap(b), "", "")
	want := mapped()
	if !reflect.DeepEqual(after.Map.Party, want.Map.Party) || !reflect.DeepEqual(after.Map.Visited, want.Map.Visited) || !reflect.DeepEqual(after.Map.Hazards, want.Map.Hazards) {
		t.Fatalf("map after %+v", after.Map)
	}
	if b.Characters[0].Location != "loc:ford" || b.Characters[1].Location != "loc:ford" {
		t.Fatalf("characters after %+v", b.Characters)
	}
}

func TestMergeClock(t *testing.T) {
	world := func(c *core.Clock) core.Blueprint {
		return core.Blueprint{Locations: []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}, Clock: c}
	}
	// A held clock runs the world at its tick, whatever the source says.
	d := clocked()
	b := world(&core.Clock{Unit: "second", Tick: 0})
	if err := Merge(&b, &d); err != nil || *b.Clock != (core.Clock{Unit: "second", Tick: 900}) || d.Clock.Tick != 900 {
		t.Fatalf("held %v %+v %+v", err, b.Clock, d.Clock)
	}
	// A later source tick is ignored too: the clock has one writer.
	b = world(&core.Clock{Unit: "second", Tick: 5000})
	if err := Merge(&b, &d); err != nil || b.Clock.Tick != 900 {
		t.Fatalf("later source %v %+v", err, b.Clock)
	}
	// Ticks in another unit mean something else.
	b = world(&core.Clock{Unit: "minute", Tick: 15})
	err := Merge(&b, &d)
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != "clock" || f.Expected != "unit second" || f.Actual != "unit minute" {
		t.Fatalf("unit %#v", err)
	}
	// A world without a clock leaves the held one alone.
	b = world(nil)
	if err := Merge(&b, &d); err != nil || b.Clock != nil || d.Clock.Tick != 900 {
		t.Fatalf("no world clock %v %+v", err, b.Clock)
	}
	// A document without a clock takes the source's, which Next then holds.
	for _, old := range []int{1, 2, 3, 4} {
		d := sealed()
		d.Version = old
		b = world(&core.Clock{Unit: "second", Tick: 28800})
		if err := Merge(&b, &d); err != nil || *b.Clock != (core.Clock{Unit: "second", Tick: 28800}) {
			t.Fatalf("version %d %v %+v", old, err, b.Clock)
		}
	}
	// A declared condition's ticks count from the held tick. The source's
	// own conditions are left as they were.
	lasting := []core.ConditionInstance{{ID: "c:timed", ClockLifetime: &core.ClockLifetime{AppliedTick: 100, ExpiresTick: 110}}, {ID: "c:lasting"}}
	b = world(&core.Clock{Unit: "second", Tick: 100})
	b.Conditions = lasting
	if err := Merge(&b, &d); err != nil || *b.Conditions[0].ClockLifetime != (core.ClockLifetime{AppliedTick: 900, ExpiresTick: 910}) || b.Conditions[1].ClockLifetime != nil || lasting[0].ClockLifetime.AppliedTick != 100 {
		t.Fatalf("rebased %v %+v", err, b.Conditions)
	}
	// One that would end past the last tick is refused.
	late := clocked()
	late.Clock.Tick = math.MaxUint64 - 5
	b = world(&core.Clock{Unit: "second", Tick: 100})
	b.Conditions = lasting
	err = Merge(&b, &late)
	if !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != "clock" || f.Subject != "c:timed" || f.Actual != "overflow" {
		t.Fatalf("overflow %#v", err)
	}
	// Without a held clock, the source's tick and lifetimes stand.
	d = sealed()
	b = world(&core.Clock{Unit: "second", Tick: 100})
	b.Conditions = lasting
	if err := Merge(&b, &d); err != nil || b.Clock.Tick != 100 || *b.Conditions[0].ClockLifetime != (core.ClockLifetime{AppliedTick: 100, ExpiresTick: 110}) {
		t.Fatalf("first %v %+v", err, b.Conditions)
	}

	// Neither has one.
	d = sealed()
	b = world(nil)
	if err := Merge(&b, &d); err != nil || b.Clock != nil {
		t.Fatalf("none %v %+v", err, b.Clock)
	}
}

func TestNextClock(t *testing.T) {
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}
	s.Occupants = sealed().Occupants
	// The next document holds the world's clock after the call, a copy.
	s.Clock = &core.Clock{Unit: "second", Tick: 1200}
	n := Next(clocked(), s, "", "")
	if *n.Clock != *s.Clock || n.Clock == s.Clock || Check(&n) != nil {
		t.Fatalf("world clock %+v", n.Clock)
	}
	// The first one comes from the world.
	n = Next(sealed(), s, "", "")
	if *n.Clock != *s.Clock || n.Version != Version || Check(&n) != nil {
		t.Fatalf("first clock %+v", n)
	}
	// A world without a clock carries the held one.
	s.Clock = nil
	d := clocked()
	n = Next(d, s, "", "")
	if *n.Clock != *d.Clock || n.Clock == d.Clock || n.Checksum != d.Checksum {
		t.Fatalf("held clock %+v", n)
	}
	// Neither: none, and the JSON names none.
	raw, err := json.Marshal(Next(sealed(), s, "", ""))
	if err != nil || strings.Contains(string(raw), `"clock"`) {
		t.Fatalf("no clock %s %v", raw, err)
	}
}

// A clock held across a world without one comes back when the world declares
// one again, at the held tick.
func TestClockRoundTrip(t *testing.T) {
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}
	s.Occupants = sealed().Occupants
	d := clocked()
	b := core.Blueprint{Locations: s.Locations}
	if err := Merge(&b, &d); err != nil {
		t.Fatal(err)
	}
	d = Next(d, s, "", "")
	b = core.Blueprint{Locations: s.Locations, Clock: &core.Clock{Unit: "second"}}
	if err := Merge(&b, &d); err != nil || b.Clock.Tick != 900 {
		t.Fatalf("again %v %+v", err, b.Clock)
	}
}

func TestChanges(t *testing.T) {
	batch := func(e core.Event) core.Batch { return core.Batch{Events: []core.Event{{Kind: "Other"}, e}} }
	occupant := batch(core.Event{Occupant: &core.OccupantChange{}})
	moved := batch(core.Event{Movement: &core.MovementChange{}})
	trip := batch(core.Event{Trip: &core.Trip{}})
	edit := batch(core.Event{MapEdit: &core.MapEdit{}})
	quest := batch(core.Event{Quest: &core.QuestChange{}})
	timed := batch(core.Event{Time: &core.TimeChange{}})
	other := batch(core.Event{Kind: "Other"})
	for _, c := range []struct {
		name               string
		b                  core.Batch
		withMap, withClock bool
		want               bool
	}{
		{"occupant", occupant, false, false, true}, {"occupant with a map", occupant, true, false, true},
		{"move", moved, false, true, false}, {"move with a map", moved, true, false, true},
		{"trip", trip, false, true, false}, {"trip with a map", trip, true, false, true},
		{"map edit", edit, false, true, false}, {"map edit with a map", edit, true, false, true},
		{"quest", quest, false, false, true}, {"quest with a map", quest, true, false, true},
		{"time", timed, true, false, false}, {"time with a clock", timed, false, true, true},
		{"other", other, true, true, false}, {"nothing", core.Batch{}, true, true, false},
	} {
		if got := Changes(c.b, c.withMap, c.withClock); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestFindAndDigest(t *testing.T) {
	d := sealed()
	if r, ok := d.Find("r2"); !ok || r.Revision != 2 {
		t.Fatalf("find %+v %v", r, ok)
	}
	if _, ok := d.Find("r9"); ok {
		t.Fatal("found an unknown request")
	}
	a := core.Ref{Kind: core.CharacterKind, ID: "char:a"}
	ops := []core.Operation{{Occupant: &core.OccupantOp{Attitude: &core.SetAttitude{Occupant: "occ:mira", Attitude: core.AttitudeHostile}}}}
	x := Digest(a, ops)
	if len(x) != 64 || !validDigest(x) || x != Digest(a, ops) {
		t.Fatalf("digest %q", x)
	}
	other := []core.Operation{{Occupant: &core.OccupantOp{Attitude: &core.SetAttitude{Occupant: "occ:mira", Attitude: core.AttitudeFriendly}}}}
	if Digest(a, other) == x || Digest(core.Ref{Kind: core.CharacterKind, ID: "char:b"}, ops) == x || Digest(a, append(ops, ops...)) == x {
		t.Fatal("different content, same digest")
	}
}
