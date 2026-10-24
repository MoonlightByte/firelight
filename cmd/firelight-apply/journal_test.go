package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// The journal guide's examples come from its example files: the departure
// event, the view, the actions and the declaration a host writes back.
func TestJournalGuideExamples(t *testing.T) {
	guide := readFile(t, "../../docs/JOURNAL.md")
	var shown []any
	for _, block := range strings.Split(guide, "```json\n")[1:] {
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(block, "```", 2)[0]), &v); err != nil {
			t.Fatal(err)
		}
		shown = append(shown, v)
	}
	if len(shown) != 2 {
		t.Fatalf("%d examples in the guide", len(shown))
	}
	world := readFile(t, "../../examples/occupants/world.nql")
	trip := readFile(t, "../../examples/journal/trip.nql")
	if !strings.Contains(guide, "```nql\n"+trip+"```") {
		t.Fatal("the guide's actions are not trip.nql")
	}
	ada := core.Ref{Kind: core.CharacterKind, ID: "char:ada"}
	r := apply(input{World: world, Actions: trip, Actor: &ada, Request: "turn:13"})
	if !r.OK {
		t.Fatal(r.Error, r.Fault, r.Diagnostics)
	}
	if got := asJSON(t, r.Receipt.Events[0].Journal); !reflect.DeepEqual(got, shown[0]) {
		t.Fatalf("event:\nengine %v\nguide  %v", got, shown[0])
	}
	v := apply(input{Checkpoint: r.Checkpoint, Journal: &core.JournalFilter{Participant: "char:bram"}})
	if got := asJSON(t, v.Journal); !v.OK || !reflect.DeepEqual(got, shown[1]) {
		t.Fatalf("view:\nengine %v\nguide  %v", got, shown[1])
	}
	// The declaration a host writes back is the departure entry, with the
	// next seq after the trip.
	declared := strings.SplitN(strings.SplitN(guide, "```nql\njournal ", 2)[1], "```", 2)[0]
	d := apply(input{World: world + "journal " + declared, Journal: &core.JournalFilter{}})
	if !d.OK || d.Journal.Next != 3 || len(d.Journal.Entries) != 1 || !reflect.DeepEqual(d.Journal.Entries[0], v.Journal.Entries[0]) {
		t.Fatalf("declared %s %+v", d.Error, d.Journal)
	}
}

// asJSON is v as the response carries it.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
