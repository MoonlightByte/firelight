package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/livestate"
)

// A departure entry goes in the trip's request. A retry the document keeps
// is answered as applied; one older than the window is refused whole at the
// entry's ID, the trip does not apply twice, and the view by that ID finds
// the entry: the trip is done.
func TestLiveStateJournal(t *testing.T) {
	world := readFile(t, "../../examples/occupants/world.nql")
	ada := core.Ref{Kind: core.CharacterKind, ID: "char:ada"}
	g := apply(input{World: world, NewLiveState: true})
	if !g.OK || g.LiveState.Journal != nil {
		t.Fatalf("start: %+v", g.LiveState)
	}
	trip := `record journal "journal:trip/1" departure with party text "Left for the ford."; travel party to "loc:valley/B02";`
	r := apply(input{World: world, LiveState: g.LiveState, Actions: trip, Actor: &ada, Request: "turn:1"})
	if !r.OK || r.Historical {
		t.Fatalf("trip: %s %+v", r.Error, r.Fault)
	}
	j := r.LiveState.Journal
	if j == nil || j.Next != 2 || len(j.Entries) != 1 || *j.Entries[0].Tick != 28800 || j.Entries[0].Place != "loc:valley/B01" || !reflect.DeepEqual(j.Entries[0].Participants, []string{"char:ada", "char:bram"}) || r.LiveState.Clock.Tick == 28800 {
		t.Fatalf("departure %+v %+v", j, r.LiveState.Clock)
	}
	doc := r.LiveState
	again := apply(input{World: world, LiveState: doc, Actions: trip, Actor: &ada, Request: "turn:1"})
	if !again.OK || !again.Historical || !reflect.DeepEqual(again.LiveState, doc) {
		t.Fatalf("retry: %+v", again)
	}
	// Push the trip out of the window.
	for i := 0; i < livestate.MaxRequests; i++ {
		n := apply(input{World: world, LiveState: doc, Actions: fmt.Sprintf(`record journal "journal:note/%d" note;`, i), Actor: &ada, Request: fmt.Sprintf("turn:n%d", i)})
		if !n.OK {
			t.Fatal(n.Error, n.Fault)
		}
		doc = n.LiveState
	}
	old := apply(input{World: world, LiveState: doc, Actions: trip, Actor: &ada, Request: "turn:1"})
	if old.OK || old.Phase != "execute" || old.Fault == nil || old.Fault.Code != "E_DUPLICATE" || old.Fault.Subject != "journal:trip/1" || old.LiveState != nil {
		t.Fatalf("old retry: ok %v phase %s fault %+v", old.OK, old.Phase, old.Fault)
	}
	// The documented recovery: a view call with that ID.
	v := apply(input{World: world, LiveState: doc, Journal: &core.JournalFilter{IDs: []core.JournalEntryID{"journal:trip/1"}}})
	if !v.OK || v.Journal == nil || len(v.Journal.Entries) != 1 || v.Journal.Entries[0].Seq != 1 || v.Journal.Next != 66 || !reflect.DeepEqual(v.LiveState, doc) {
		t.Fatalf("recovery: %s %+v", v.Error, v.Journal)
	}
	// A held entry's ID stays taken: a world that declares something with
	// it is the document's fault, with nothing applied.
	clash := apply(input{World: world + `quest "journal:trip/1" named "Trip";` + "\n", LiveState: doc, Journal: &core.JournalFilter{}})
	if clash.OK || clash.Phase != "live_state" || clash.Fault == nil || clash.Fault.Code != "E_DUPLICATE" || clash.Fault.Field != "journal" || clash.Fault.Subject != "journal:trip/1" {
		t.Fatalf("clash: ok %v phase %s fault %+v", clash.OK, clash.Phase, clash.Fault)
	}
	// Paging with after.
	var seqs []uint64
	for after := uint64(0); ; {
		p := apply(input{World: world, LiveState: doc, Journal: &core.JournalFilter{After: after, Limit: 20}})
		if !p.OK || p.Journal == nil {
			t.Fatal(p.Error)
		}
		if len(p.Journal.Entries) == 0 {
			break
		}
		for _, e := range p.Journal.Entries {
			seqs = append(seqs, e.Seq)
		}
		after = p.Journal.Entries[len(p.Journal.Entries)-1].Seq
	}
	if len(seqs) != 65 || seqs[0] != 1 || seqs[64] != 65 {
		t.Fatalf("pages %v", seqs)
	}
	// A bad filter is a view failure, after the request.
	bad := apply(input{World: world, LiveState: doc, Journal: &core.JournalFilter{Limit: 2000}})
	if bad.OK || bad.Fault == nil || bad.Fault.Code != "E_JOURNAL" || bad.Fault.Field != "limit" {
		t.Fatalf("bad view %+v", bad)
	}
}

func liveJournalWorld(clock int64, declared int) string {
	var s strings.Builder
	s.WriteString(liveMapWorld(clock))
	for i := 1; i <= declared; i++ {
		fmt.Fprintf(&s, "journal \"j:old/%d\" note seq %d at \"loc:camp\" tick %d with \"char:a\";\n", i, i, i)
	}
	return s.String()
}

// Journal actions give the same answers, events, views and final journal,
// seq included, whether the host keeps the checkpoint or writes a fresh
// world and passes the live state. Half the rounds start from a source that
// declares a journal near the cap, so entries are dropped.
func TestLiveStateJournalMatchesKeptWorld(t *testing.T) {
	places := []string{"loc:camp", "loc:ford", "loc:mill", "loc:keep"}
	names := []string{"char:a", "char:b", "occ:mira", "occ:wolves", "occ:ghost", "loc:camp"}
	kinds := []string{"departure", "arrival", "event", "discovery", "note"}
	rng := rand.New(rand.NewSource(48))
	noise := rand.New(rand.NewSource(9))
	pick := func(list []string) string { return list[rng.Intn(len(list))] }
	fresh := 0
	record := func() string {
		id := ""
		switch rng.Intn(12) {
		case 0:
			id = fmt.Sprintf("j:%d", rng.Intn(fresh+1))
		case 1:
			id = pick([]string{"char:a", "occ:mira", "loc:keep", "j:old/1", "j:old/1015"})
		default:
			fresh++
			id = fmt.Sprintf("j:%d", fresh)
		}
		s := fmt.Sprintf(`record journal %q %s`, id, pick(kinds))
		if rng.Intn(3) == 0 {
			s += fmt.Sprintf(` at %q`, pick(places))
		}
		if rng.Intn(3) > 0 {
			s += " with"
			if rng.Intn(2) == 0 {
				s += " party"
			}
			for k := rng.Intn(3); k >= 0; k-- {
				s += fmt.Sprintf(" %q", pick(names))
			}
		}
		if rng.Intn(2) == 0 {
			s += fmt.Sprintf(` text "step %d"`, rng.Intn(9))
		}
		return s + ";"
	}
	statement := func() string {
		switch rng.Intn(10) {
		case 0, 1:
			return fmt.Sprintf(`travel party to %q;`, pick(places))
		case 2:
			return fmt.Sprintf(`join party %q;`, pick([]string{"char:a", "char:b"}))
		case 3:
			return fmt.Sprintf(`advance time by %d;`, 1+rng.Intn(9))
		}
		return record()
	}
	zero := func(events []core.Event) []core.Event {
		out := append([]core.Event(nil), events...)
		for i := range out {
			out[i].ID, out[i].Revision = "", 0
		}
		return out
	}
	view := func(p *core.JournalProjection) *core.JournalProjection {
		if p == nil {
			return nil
		}
		v := *p
		v.Revision = 0
		return &v
	}
	answers := map[string]int{}
	type sent struct{ id, actions string }
	for round := 0; round < 24; round++ {
		declared := 0
		if round%2 == 1 {
			declared = core.MaxJournalEntries - 4
		}
		a := apply(input{World: liveJournalWorld(0, declared), NewLiveState: true})
		b := apply(input{World: liveJournalWorld(0, declared)})
		if !a.OK || !b.OK {
			t.Fatal(a.Error, b.Error)
		}
		doc, cp := a.LiveState, b.Checkpoint
		var kept, refused []sent
		for step := 0; step < 60; step++ {
			s := sent{id: fmt.Sprintf("r%d", step)}
			for k := 0; k < 1+rng.Intn(3); k++ {
				s.actions += statement()
			}
			switch n := rng.Intn(10); {
			case n == 0 && len(kept) > 0:
				s = kept[rng.Intn(len(kept))]
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
			filter := &core.JournalFilter{After: uint64(rng.Intn(1030)), Limit: rng.Intn(4)}
			if rng.Intn(2) == 0 {
				filter = &core.JournalFilter{Participant: pick(names), Kinds: []core.JournalKind{core.JournalKind(pick(kinds))}}
			}
			// The fresh source says any tick and, after the first call, any
			// journal: the document's are the ones that run.
			ra := apply(input{World: liveJournalWorld(noise.Int63n(2)*noise.Int63n(1e6), noise.Intn(2)*declared), LiveState: &stored, Actions: s.actions, Actor: &actor, Request: s.id, Journal: filter})
			rb := apply(input{Checkpoint: cp, Actions: s.actions, Actor: &actor, Request: s.id, Journal: filter})
			where := fmt.Sprintf("round %d step %d request %s %q", round, step, s.id, s.actions)
			if ra.OK != rb.OK || ra.Phase != rb.Phase || ra.Historical != rb.Historical || ra.Error != rb.Error || !reflect.DeepEqual(ra.Fault, rb.Fault) || !reflect.DeepEqual(ra.Diagnostics, rb.Diagnostics) || ra.ViewError != nil || rb.ViewError != nil {
				t.Fatalf("%s:\nlive ok %v phase %s historical %v error %q fault %+v diagnostics %+v\nkept ok %v phase %s historical %v error %q fault %+v diagnostics %+v", where, ra.OK, ra.Phase, ra.Historical, ra.Error, ra.Fault, ra.Diagnostics, rb.OK, rb.Phase, rb.Historical, rb.Error, rb.Fault, rb.Diagnostics)
			}
			if !reflect.DeepEqual(view(ra.Journal), view(rb.Journal)) {
				t.Fatalf("%s: views differ\nlive %+v\nkept %+v", where, ra.Journal, rb.Journal)
			}
			switch {
			case !ra.OK:
				key := "refused " + ra.Phase
				if ra.Fault != nil {
					key += " " + ra.Fault.Code
				}
				answers[key]++
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
				for _, e := range ra.Receipt.Events {
					if e.Journal != nil {
						answers["recorded"]++
						if e.Journal.Dropped != nil {
							answers["entry dropped"]++
						}
						if len(e.Journal.DroppedParticipants) > 0 {
							answers["participant dropped"]++
						}
					}
				}
				if last := ra.LiveState.Requests; ra.LiveState.Revision == doc.Revision+1 && len(last) > 0 && last[len(last)-1].ID == s.id {
					answers["applied, kept"]++
					kept = append(kept, s)
				} else {
					t.Fatalf("%s: document changed without keeping the request", where)
				}
			}
			for i := len(refused) - 1; i >= 0; i-- {
				if refused[i].id == s.id {
					refused = append(refused[:i], refused[i+1:]...)
				}
			}
			doc, cp = ra.LiveState, rb.Checkpoint
		}
		// The journal is the same, seq included.
		final := apply(input{Checkpoint: cp, IncludeState: true})
		if !reflect.DeepEqual(doc.Journal, final.Checkpoint.State.Journal) {
			t.Fatalf("round %d: journals differ\nlive %+v\nkept %+v", round, doc.Journal, final.Checkpoint.State.Journal)
		}
	}
	t.Logf("answers %v", answers)
	for _, k := range []string{"applied, kept", "already applied", "refused execute E_DUPLICATE", "refused execute E_REQUEST_REUSE", "recorded", "entry dropped", "participant dropped"} {
		if answers[k] < 20 {
			t.Fatalf("too few %q: %v", k, answers)
		}
	}
}
