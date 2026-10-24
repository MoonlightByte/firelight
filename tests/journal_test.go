package tests

import (
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"strings"
	"testing"
)

// The map world of world_map_test.go with its occupants and a quest: Ada
// and Bea, the party, at camp at minute 100; Cy at the keep.
func journalFixture(t testing.TB, extra string) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("journal.nql", mapPlaces+occupantRoster+`quest "quest:a" named "A";`+"\n"+extra+"map {\n"+mapRoutes+"}\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func journalOf(w *core.World) core.Journal {
	if j := w.Snapshot().Journal; j != nil {
		return *j
	}
	return core.Journal{}
}

func journalEvent(t testing.TB, b core.Batch, i int) core.JournalChange {
	t.Helper()
	if i >= len(b.Events) || b.Events[i].Kind != core.JournalRecorded || b.Events[i].Journal == nil {
		t.Fatalf("event %d of %+v", i, b.Events)
	}
	return *b.Events[i].Journal
}

func journalRequest(w *core.World, id string, actor core.Ref, ops ...core.RecordJournal) core.Request {
	r := core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: actor}
	for i := range ops {
		r.Operations = append(r.Operations, core.Operation{Journal: &ops[i]})
	}
	return r
}

var ada = core.Ref{Kind: core.CharacterKind, ID: "char:ada"}

// declareJournal states a journal back as declarations.
func declareJournal(j core.Journal) string {
	var s strings.Builder
	for _, e := range j.Entries {
		fmt.Fprintf(&s, "journal %q %s seq %d at %q", e.ID, e.Kind, e.Seq, e.Place)
		if e.Tick != nil {
			fmt.Fprintf(&s, " tick %d", *e.Tick)
		}
		if len(e.Participants) > 0 {
			s.WriteString(" with")
			for _, p := range e.Participants {
				fmt.Fprintf(&s, " %q", p)
			}
		}
		if e.Text != nil {
			fmt.Fprintf(&s, " text %q", *e.Text)
		}
		s.WriteString(";\n")
	}
	fmt.Fprintf(&s, "journal next %d;\n", j.Next)
	return s.String()
}

func TestJournalRecord(t *testing.T) {
	b := journalFixture(t, "")
	w := world(t, b)
	if w.Snapshot().Journal != nil {
		t.Fatal("a world that records nothing has no journal")
	}

	// The party expands; a hand-listed name that is no declared character
	// or occupant is left out and reported, once.
	r1 := execute(t, w, "r1", `record journal "j:1" departure with party "occ:mira" "occ:ghost" "occ:ghost" "loc:ford" text "set out";`)
	c := journalEvent(t, r1, 0)
	want := core.JournalEntry{Seq: 1, ID: "j:1", Kind: "departure", Tick: tick(100), Place: "loc:camp", Participants: []string{"char:ada", "char:bea", "occ:mira"}, Text: text("set out")}
	if len(r1.Events) != 1 || !reflect.DeepEqual(c.Entry, want) || c.Dropped != nil || !reflect.DeepEqual(c.DroppedParticipants, []string{"loc:ford", "occ:ghost"}) {
		t.Fatalf("departure %+v", c)
	}

	// Action order sets the tick, and the actor's place is derived after
	// the trip.
	r2 := execute(t, w, "r2", `travel party to "loc:ford"; record journal "j:2" arrival with party;`)
	c = journalEvent(t, r2, len(r2.Events)-1)
	if want := (core.JournalEntry{Seq: 2, ID: "j:2", Kind: "arrival", Tick: tick(110), Place: "loc:ford", Participants: []string{"char:ada", "char:bea"}}); !reflect.DeepEqual(c.Entry, want) || c.DroppedParticipants != nil {
		t.Fatalf("arrival %+v", c)
	}

	// Each kind; a named place; one with nobody.
	execute(t, w, "r3", `record journal "j:3" event at "loc:mill" with "char:cy"; record journal "j:4" discovery at "loc:keep"; record journal "j:5" note;`)
	// A place actor's entry is at that place.
	if _, err := w.Execute(journalRequest(w, "r4", core.Ref{Kind: core.LocationKind, ID: "loc:gate"}, core.RecordJournal{ID: "j:6", Kind: "note"})); err != nil {
		t.Fatal(err)
	}
	j := journalOf(w)
	if j.Next != 7 || len(j.Entries) != 6 {
		t.Fatalf("journal %+v", j)
	}
	for i, p := range []core.LocationID{"loc:camp", "loc:ford", "loc:mill", "loc:keep", "loc:ford", "loc:gate"} {
		e := j.Entries[i]
		if e.Seq != uint64(i+1) || e.Place != p || *e.Tick != []uint64{100, 110, 110, 110, 110, 110}[i] || string(e.Kind) != []string{"departure", "arrival", "event", "discovery", "note", "note"}[i] {
			t.Fatalf("entry %d: %+v", i, e)
		}
	}
	if !reflect.DeepEqual(j.Entries[2].Participants, []string{"char:cy"}) || j.Entries[3].Participants != nil {
		t.Fatalf("participants %+v", j.Entries)
	}
	rebuilt := costReplay(t, b, w)
	if !reflect.DeepEqual(journalOf(rebuilt), j) {
		t.Fatal("replay differs")
	}

	// Stated back as declarations, the journal is the same, and recording
	// goes on from the same seq.
	again := world(t, journalFixture(t, declareJournal(j)))
	if !reflect.DeepEqual(journalOf(again), j) {
		t.Fatalf("declared\n%+v\nwant\n%+v", journalOf(again), j)
	}
	r := execute(t, again, "r5", `record journal "j:7" note;`)
	if c := journalEvent(t, r, 0); c.Entry.Seq != 7 {
		t.Fatalf("next seq %+v", c)
	}

	// Without a clock an entry has no tick.
	plain, ds := compiler.CompileWorld("plain.nql", `rules { transfer unequips; wear any; }
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" { }
`)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pw := world(t, plain)
	execute(t, pw, "p1", `record journal "j:1" note with "char:ada";`)
	if e := journalOf(pw).Entries[0]; e.Tick != nil || e.Place != "loc:camp" {
		t.Fatalf("no clock %+v", e)
	}
}

func TestJournalRefusalsLeaveNoTrace(t *testing.T) {
	many := ""
	for i := 0; i < 15; i++ {
		many += fmt.Sprintf("occupant \"occ:o%02d\" named \"O\" at \"loc:camp\" { person; }\n", i)
	}
	b := journalFixture(t, many+`item "item:rope" named "Rope" { owner "char:ada"; }`+"\n")
	w := world(t, b)
	execute(t, w, "r1", `record journal "j:1" note;`)
	before, rev := journalOf(w), w.Revision()
	long := strings.Repeat("t", core.MaxJournalTextBytes+1)
	fifteen := []string{}
	for i := 0; i < 15; i++ {
		fifteen = append(fifteen, fmt.Sprintf("occ:o%02d", i))
	}
	seventeen := append(append([]string{}, fifteen...), "char:cy", "occ:mira")
	place := func(s string) *core.LocationID { p := core.LocationID(s); return &p }
	for _, tc := range []struct {
		name                  string
		actor                 core.Ref
		ops                   []core.RecordJournal
		code, field, expected string
		actual                string
	}{
		{"a used entry ID", ada, []core.RecordJournal{{ID: "j:1", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "j:1"},
		{"a character's ID", ada, []core.RecordJournal{{ID: "char:ada", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "char:ada"},
		{"a place's ID", ada, []core.RecordJournal{{ID: "loc:camp", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "loc:camp"},
		{"an occupant's ID", ada, []core.RecordJournal{{ID: "occ:mira", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "occ:mira"},
		{"a quest's ID", ada, []core.RecordJournal{{ID: "quest:a", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "quest:a"},
		{"twice in one request", ada, []core.RecordJournal{{ID: "j:2", Kind: "note"}, {ID: "j:2", Kind: "note"}}, "E_DUPLICATE", "journal", "unique entry ID", "j:2"},
		{"an undeclared place", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Place: place("loc:nowhere")}}, "E_REFERENCE", "reference", "location", "missing"},
		{"a character as the place", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Place: place("char:cy")}}, "E_TYPE", "reference", "location", "character"},
		{"an unknown kind", ada, []core.RecordJournal{{ID: "j:2", Kind: "diary"}}, "E_JOURNAL", "kind", "departure, arrival, event, discovery or note", "diary"},
		{"a blank ID", ada, []core.RecordJournal{{ID: " ", Kind: "note"}}, "E_JOURNAL", "id", "valid entry ID", "invalid"},
		{"blank text", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Text: text(" ")}}, "E_JOURNAL", "text", "valid text of at most 512 bytes", "invalid"},
		{"a newline", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Text: text("left\nat dawn")}}, "E_JOURNAL", "text", "valid text of at most 512 bytes", "control character"},
		{"a tab", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Text: text("left\tat dawn")}}, "E_JOURNAL", "text", "valid text of at most 512 bytes", "control character"},
		{"long text", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", Text: &long}}, "E_JOURNAL", "text", "valid text of at most 512 bytes", "513 bytes"},
		{"17 listed", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", With: seventeen}}, "E_JOURNAL", "participants", "at most 16 participants", "17"},
		{"a blank participant", ada, []core.RecordJournal{{ID: "j:2", Kind: "note", With: []string{" "}}}, "E_JOURNAL", "participants", "valid IDs", " "},
		{"an actor at no place", core.Ref{Kind: core.ItemKind, ID: "item:rope"}, []core.RecordJournal{{ID: "j:2", Kind: "note"}}, "E_JOURNAL", "place", "a place, or an actor standing at one", "item"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.Execute(journalRequest(w, "x", tc.actor, tc.ops...))
			wantQuestFault(t, err, tc.code, tc.field, tc.expected, tc.actual)
			if !reflect.DeepEqual(journalOf(w), before) || w.Revision() != rev {
				t.Fatal("a refusal left a trace")
			}
		})
	}
	// Sixteen is the bound, not a refusal.
	if _, err := w.Execute(journalRequest(w, "r2", ada, core.RecordJournal{ID: "j:2", Kind: "note", With: append(fifteen, "char:cy")})); err != nil {
		t.Fatal(err)
	}
	// Past 16 with the party, the party is kept, then the listed names in
	// order; the rest are left out and reported, never refused.
	r, err := w.Execute(journalRequest(w, "r4", ada, core.RecordJournal{ID: "j:4", Kind: "note", Party: true, With: append([]string{"occ:ghost"}, fifteen...)}))
	if err != nil {
		t.Fatal(err)
	}
	c := journalEvent(t, r, 0)
	if want := append([]string{"char:ada", "char:bea"}, fifteen[:14]...); !reflect.DeepEqual(c.Entry.Participants, want) || !reflect.DeepEqual(c.DroppedParticipants, []string{"occ:ghost", "occ:o14"}) {
		t.Fatalf("past the cap %+v %v", c.Entry.Participants, c.DroppedParticipants)
	}
	// 512 bytes is the bound, not a refusal.
	if _, err := w.Execute(journalRequest(w, "r3", ada, core.RecordJournal{ID: "j:3", Kind: "note", Text: text(long[1:])})); err != nil {
		t.Fatal(err)
	}

	// `party` needs a map.
	plain, ds := compiler.CompileWorld("plain.nql", `rules { transfer unequips; wear any; }
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" { }
`)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	pw := world(t, plain)
	_, err = pw.Execute(journalRequest(pw, "x", ada, core.RecordJournal{ID: "j:1", Kind: "note", Party: true}))
	wantQuestFault(t, err, "E_JOURNAL", "party", "a world map", "absent")
}

func TestJournalFull(t *testing.T) {
	var declared strings.Builder
	for i := 1; i <= core.MaxJournalEntries; i++ {
		fmt.Fprintf(&declared, "journal \"j:old/%d\" note seq %d at \"loc:camp\";\n", i, i)
	}
	b := journalFixture(t, declared.String())
	w := world(t, b)
	if j := journalOf(w); len(j.Entries) != core.MaxJournalEntries || j.Next != core.MaxJournalEntries+1 {
		t.Fatalf("declared %d next %d", len(j.Entries), j.Next)
	}

	// A full journal drops its oldest entry; the event carries it.
	c := journalEvent(t, execute(t, w, "r1", `record journal "j:new" note;`), 0)
	if c.Entry.Seq != 1025 || c.Dropped == nil || c.Dropped.ID != "j:old/1" || c.Dropped.Seq != 1 {
		t.Fatalf("drop %+v", c)
	}
	// A dropped ID is free again, and seq never repeats.
	c = journalEvent(t, execute(t, w, "r2", `record journal "j:old/1" note;`), 0)
	if c.Entry.Seq != 1026 || c.Dropped == nil || c.Dropped.ID != "j:old/2" {
		t.Fatalf("second drop %+v", c)
	}
	j := journalOf(w)
	if len(j.Entries) != core.MaxJournalEntries || j.Entries[0].Seq != 3 || j.Entries[len(j.Entries)-1].Seq != 1026 || j.Next != 1027 {
		t.Fatalf("after drops: %d entries, first %d, next %d", len(j.Entries), j.Entries[0].Seq, j.Next)
	}
	if !reflect.DeepEqual(journalOf(costReplay(t, b, w)), j) {
		t.Fatal("replay differs")
	}
	// After drops, the declarations need `journal next`; they state it.
	if !reflect.DeepEqual(journalOf(world(t, journalFixture(t, declareJournal(j)))), j) {
		t.Fatal("declared after drops")
	}

	// The last seq is never taken.
	last := world(t, journalFixture(t, fmt.Sprintf("journal next %d;\n", uint64(math.MaxUint64))))
	_, err := last.Execute(journalRequest(last, "x", ada, core.RecordJournal{ID: "j:1", Kind: "note"}))
	wantQuestFault(t, err, "E_OVERFLOW", "journal", "an unused seq", "maximum")
}

func TestJournalView(t *testing.T) {
	w := world(t, journalFixture(t, ""))
	empty, err := w.JournalView(core.JournalFilter{})
	if err != nil || empty.Next != 1 || empty.Entries == nil || len(empty.Entries) != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
	execute(t, w, "r1", `record journal "j:1" departure with party text "out";`)
	execute(t, w, "r2", `travel party to "loc:ford"; record journal "j:2" arrival with party "occ:wolves";`)
	execute(t, w, "r3", `record journal "j:3" discovery with "char:ada"; record journal "j:4" note at "loc:keep" with "char:cy";`)
	seqs := func(f core.JournalFilter) []uint64 {
		t.Helper()
		p, err := w.JournalView(f)
		if err != nil {
			t.Fatal(err)
		}
		if p.Next != 5 || p.Revision != w.Revision() {
			t.Fatalf("view %+v", p)
		}
		out := []uint64{}
		for _, e := range p.Entries {
			out = append(out, e.Seq)
		}
		return out
	}
	since := uint64(110)
	for _, tc := range []struct {
		name string
		f    core.JournalFilter
		want []uint64
	}{
		{"all", core.JournalFilter{}, []uint64{1, 2, 3, 4}},
		{"by id", core.JournalFilter{IDs: []core.JournalEntryID{"j:3", "j:1", "j:9"}}, []uint64{1, 3}},
		{"after", core.JournalFilter{After: 2}, []uint64{3, 4}},
		{"after the last", core.JournalFilter{After: 4}, []uint64{}},
		{"since", core.JournalFilter{Since: &since}, []uint64{2, 3, 4}},
		{"place", core.JournalFilter{Place: "loc:ford"}, []uint64{2, 3}},
		{"participant", core.JournalFilter{Participant: "char:bea"}, []uint64{1, 2}},
		{"an occupant", core.JournalFilter{Participant: "occ:wolves"}, []uint64{2}},
		{"kinds", core.JournalFilter{Kinds: []core.JournalKind{"note", "departure"}}, []uint64{1, 4}},
		{"limit", core.JournalFilter{Limit: 3}, []uint64{1, 2, 3}},
		{"a page", core.JournalFilter{After: 1, Limit: 2}, []uint64{2, 3}},
		{"the last page", core.JournalFilter{After: 3, Limit: 2}, []uint64{4}},
		{"every filter", core.JournalFilter{After: 1, Since: &since, Place: "loc:ford", Participant: "char:ada", Kinds: []core.JournalKind{"discovery"}}, []uint64{3}},
	} {
		if got := seqs(tc.f); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	_, err = w.JournalView(core.JournalFilter{Kinds: []core.JournalKind{"diary"}})
	wantQuestFault(t, err, "E_JOURNAL", "kind", "departure, arrival, event, discovery or note", "diary")
	_, err = w.JournalView(core.JournalFilter{Limit: -1})
	wantQuestFault(t, err, "E_JOURNAL", "limit", "0 to 1024", "-1")
	_, err = w.JournalView(core.JournalFilter{Limit: core.MaxJournalEntries + 1})
	wantQuestFault(t, err, "E_JOURNAL", "limit", "0 to 1024", "1025")

	// The view is a copy.
	p, _ := w.JournalView(core.JournalFilter{})
	p.Entries[0].Participants[0], *p.Entries[0].Text, *p.Entries[0].Tick = "x", "x", 9
	if e := journalOf(w).Entries[0]; e.Participants[0] != "char:ada" || *e.Text != "out" || *e.Tick != 100 {
		t.Fatal("the view aliases the world")
	}
	s := w.Snapshot()
	s.Journal.Entries[0].Participants[0] = "x"
	if journalOf(w).Entries[0].Participants[0] != "char:ada" {
		t.Fatal("the snapshot aliases the world")
	}
}

func TestJournalCompileRefusals(t *testing.T) {
	head := strings.Count(mapPlaces+occupantRoster, "\n") + 2 // the body's first line
	many := ""
	for i := 0; i <= core.MaxJournalParticipants; i++ {
		many += fmt.Sprintf(" \"occ:o%02d\"", i)
	}
	for _, tc := range []struct {
		name, body string
		line       int
		at         string
		code       string
		expected   string
	}{
		{"seq out of order", "journal \"j:a\" note seq 2 at \"loc:camp\";\njournal \"j:b\" note seq 2 at \"loc:camp\";", 2, `2`, "E_JOURNAL", "increasing, from 1 and below next"},
		{"seq 0", `journal "j:a" note seq 0 at "loc:camp";`, 1, `0`, "E_JOURNAL", "increasing, from 1 and below next"},
		{"next below a seq", "journal \"j:a\" note seq 5 at \"loc:camp\";\njournal next 5;", 1, `5`, "E_JOURNAL", "increasing, from 1 and below next"},
		{"next 0", `journal next 0;`, 1, `0`, "E_JOURNAL", "at least 1"},
		{"next twice", "journal next 3;\njournal next 4;", 2, `journal`, "E_DUPLICATE", "one journal next"},
		{"two of one ID", "journal \"j:a\" note seq 1 at \"loc:camp\";\njournal \"j:a\" note seq 2 at \"loc:camp\";", 2, `"j:a"`, "E_DUPLICATE", "unique entry ID"},
		{"a character's ID", `journal "char:ada" note seq 1 at "loc:camp";`, 1, `"char:ada"`, "E_DUPLICATE", "unique entry ID"},
		{"a quest's ID", `journal "quest:a" note seq 1 at "loc:camp";`, 1, `"quest:a"`, "E_DUPLICATE", "unique entry ID"},
		{"a participant twice", `journal "j:a" note seq 1 at "loc:camp" with "char:ada" "char:ada";`, 1, `"char:ada"`, "E_JOURNAL", "valid IDs, each once"},
		{"17 participants", `journal "j:a" note seq 1 at "loc:camp" with` + many + `;`, 1, `"j:a"`, "E_JOURNAL", "at most 16 participants"},
		{"long text", `journal "j:a" note seq 1 at "loc:camp" text "` + strings.Repeat("t", core.MaxJournalTextBytes+1) + `";`, 1, `"t`, "E_JOURNAL", "valid text of at most 512 bytes"},
		{"an unknown kind", `journal "j:a" diary seq 1 at "loc:camp";`, 1, `diary`, "E_PARSE", ""},
		{"no place", `journal "j:a" note seq 1;`, 1, `;`, "E_PARSE", ""},
		{"seq too large", `journal "j:a" note seq 18446744073709551616 at "loc:camp";`, 1, `18446744073709551616`, "E_INTEGER", ""},
		{"a negative tick", `journal "j:a" note seq 1 at "loc:camp" tick -1;`, 1, `-1`, "E_INTEGER", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld("journal.nql", mapPlaces+occupantRoster+`quest "quest:a" named "A";`+"\n"+tc.body+"\nmap {\n"+mapRoutes+"}\n")
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			line := strings.Split(tc.body, "\n")[tc.line-1]
			wantLine, column := head+tc.line-1, strings.LastIndex(line, tc.at)+1
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != wantLine || d.Column != column {
				t.Fatalf("got %#v, want %s %q at %d:%d", d, tc.code, tc.expected, wantLine, column)
			}
		})
	}

	// History is not checked against the world: a declared entry may name
	// a place and people the world no longer declares.
	journalFixture(t, `journal "j:a" note seq 1 at "loc:gone" with "char:gone" "occ:gone";`+"\n")

	// Too many entries.
	var all strings.Builder
	for i := 1; i <= core.MaxJournalEntries+1; i++ {
		fmt.Fprintf(&all, "journal \"j:%d\" note seq %d at \"loc:camp\";\n", i, i)
	}
	_, ds := compiler.CompileWorld("journal.nql", mapPlaces+all.String())
	if len(ds) != 1 || ds[0].Code != "E_JOURNAL" || ds[0].Expected != "at most 1024 entries" {
		t.Fatalf("too many entries: %#v", ds)
	}
}

func TestJournalActionCompileRefusals(t *testing.T) {
	b := journalFixture(t, "")
	for _, tc := range []struct {
		name, src, at, code, expected string
	}{
		{"an undeclared place", `record journal "j:a" note at "loc:nowhere";`, `"loc:nowhere"`, "E_REFERENCE", "location"},
		{"long text", `record journal "j:a" note text "` + strings.Repeat("t", core.MaxJournalTextBytes+1) + `";`, `"t`, "E_JOURNAL", "valid text of at most 512 bytes"},
		{"a blank participant", `record journal "j:a" note with " ";`, `" "`, "E_JOURNAL", "valid IDs"},
		{"a newline in the text", `record journal "j:a" note text "left\nat dawn";`, `"left`, "E_JOURNAL", "valid text of at most 512 bytes"},
		{"a blank ID", `record journal " " note;`, `" "`, "E_JOURNAL", "valid entry ID"},
		{"an unknown kind", `record journal "j:a" diary;`, `diary`, "E_PARSE", ""},
		{"with nothing", `record journal "j:a" note with;`, `;`, "E_PARSE", ""},
		{"a tick", `record journal "j:a" note tick 4;`, `tick`, "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("action.nql", tc.src, b)
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != 1 || d.Column != strings.LastIndex(tc.src, tc.at)+1 {
				t.Fatalf("got %#v, want %s %q at column %d", d, tc.code, tc.expected, strings.LastIndex(tc.src, tc.at)+1)
			}
		})
	}
	plain, ds := compiler.CompileWorld("plain.nql", `rules { transfer unequips; wear any; }
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" { }
`)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	_, ds = compiler.CompileActions("action.nql", `record journal "j:a" note with party;`, plain)
	if len(ds) != 1 || ds[0].Code != "E_JOURNAL" || ds[0].Expected != "a world map" {
		t.Fatalf("party without a map: %#v", ds)
	}
}
