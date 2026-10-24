package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/livestate"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// questViews is the quests view with its revisions taken out: a fresh world
// and a kept one count revisions differently.
func questViews(ps []core.QuestProjection) []core.QuestProjection {
	out := append([]core.QuestProjection(nil), ps...)
	for i := range out {
		out[i].Revision = 0
	}
	return out
}

// A host that writes a fresh world for each call passes the live state, and
// its quests stay as play left them, whatever the source says of them.
func TestLiveStateQuests(t *testing.T) {
	world := readFile(t, "../../examples/quests/world.nql")
	start := readFile(t, "../../examples/quests/start.nql")
	ahead := readFile(t, "../../examples/quests/ahead.nql")
	actor := map[string]any{"kind": "character", "id": "char:ada"}
	all := []string{"quest:valley/herbs", "quest:valley/mill", "quest:valley/tower"}
	at := func(clock int) string {
		return strings.Replace(world, "at 28800;", fmt.Sprintf("at %d;", clock), 1)
	}

	// A new document holds every declared quest as the source states it.
	_, g := invoke(t, map[string]any{"world": world, "new_live_state": true})
	if d := g.LiveState; !g.OK || d == nil || d.Version != 5 || len(d.Quests) != 3 || d.Quests[0].Status != core.StatusStarted || *d.Quests[0].Log[0].Tick != 27000 || d.Quests[1].Status != core.StatusUnstarted || d.Quests[1].Log != nil || d.Quests[2].Status != core.StatusUnstarted {
		t.Fatalf("start: ok %v error %q live %+v", g.OK, g.Error, g.LiveState)
	}

	// The same two requests on a kept checkpoint and through the live state.
	_, k1 := invoke(t, map[string]any{"world": world, "actions": start, "actor": actor, "request": "turn:1"})
	_, k2 := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, k1.Checkpoint), "actions": ahead, "actor": actor, "request": "turn:2", "quests": all})
	_, r1 := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, g.LiveState), "actions": start, "actor": actor, "request": "turn:1"})
	if !r1.OK || r1.LiveState.Revision != 1 || len(r1.LiveState.Requests) != 1 || r1.LiveState.Requests[0].ID != "turn:1" {
		t.Fatalf("turn 1: ok %v error %q fault %+v live %+v", r1.OK, r1.Error, r1.Fault, r1.LiveState)
	}
	// The host writes nothing back for quests; the source still says the
	// mill is unstarted.
	_, r2 := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, r1.LiveState), "actions": ahead, "actor": actor, "request": "turn:2", "quests": all})
	if !r2.OK || !k2.OK || r2.LiveState.Revision != 2 || !reflect.DeepEqual(questViews(r2.Quests), questViews(k2.Quests)) || !reflect.DeepEqual(r2.Receipt.Events[1].Quest, k2.Receipt.Events[1].Quest) {
		t.Fatalf("turn 2:\nlive %+v\nkept %+v", r2.Quests, k2.Quests)
	}
	d := r2.LiveState
	// ahead.nql advanced the clock: the host writes it back from the receipt.
	world = at(32400)

	// A retry is answered as already applied; other content under its ID is
	// refused.
	_, again := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, d), "actions": ahead, "actor": actor, "request": "turn:2", "quests": all})
	if !again.OK || !again.Historical || again.Receipt != nil || !reflect.DeepEqual(again.LiveState, d) || !reflect.DeepEqual(questViews(again.Quests), questViews(k2.Quests)) {
		t.Fatalf("retry: ok %v historical %v error %q fault %+v", again.OK, again.Historical, again.Error, again.Fault)
	}
	_, reuse := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, d), "actions": `fail quest "quest:valley/mill";`, "actor": actor, "request": "turn:2"})
	if reuse.OK || reuse.Fault == nil || reuse.Fault.Code != "E_REQUEST_REUSE" || reuse.LiveState != nil {
		t.Fatalf("reuse: ok %v fault %+v", reuse.OK, reuse.Fault)
	}

	// The source's log for a held quest is ignored.
	failed := strings.Replace(world, `{ requires "quest:valley/mill"; }`, `{ requires "quest:valley/mill"; failed impact "too late"; }`, 1)
	_, v := invoke(t, map[string]any{"world": failed, "live_state": liveDoc(t, d), "quests": all})
	if !v.OK || !reflect.DeepEqual(questViews(v.Quests), questViews(k2.Quests)) || !reflect.DeepEqual(v.LiveState, d) {
		t.Fatalf("source log: ok %v error %q fault %+v quests %+v", v.OK, v.Error, v.Fault, v.Quests)
	}

	// A world that stops declaring held quests keeps them in the document,
	// unchanged, and the engine ignores them: an action or a view naming one
	// is refused as unknown.
	only := strings.Split(world, `quest "quest:valley/tower"`)[0]
	_, o := invoke(t, map[string]any{"world": only, "live_state": liveDoc(t, d), "actions": `note quest "quest:valley/mill" impact "the wheel turns";`, "actor": actor, "request": "turn:3", "quests": []string{"quest:valley/mill"}})
	if !o.OK || o.LiveState.Revision != 3 || !reflect.DeepEqual(o.LiveState.Quests[0], d.Quests[0]) || !reflect.DeepEqual(o.LiveState.Quests[2], d.Quests[2]) || len(o.Quests[0].Log) != 2 {
		t.Fatalf("only the mill: ok %v error %q fault %+v live %+v", o.OK, o.Error, o.Fault, o.LiveState)
	}
	_, u := invoke(t, map[string]any{"world": only, "live_state": liveDoc(t, o.LiveState), "actions": `reopen quest "quest:valley/tower" because "the beacon went out";`, "actor": actor, "request": "turn:4"})
	if u.OK || u.Phase != "compile" || len(u.Diagnostics) != 1 || u.Diagnostics[0].Code != "E_REFERENCE" || u.LiveState != nil {
		t.Fatalf("undeclared action: ok %v phase %s diagnostics %+v", u.OK, u.Phase, u.Diagnostics)
	}
	_, u = invoke(t, map[string]any{"world": only, "live_state": liveDoc(t, o.LiveState), "quests": []string{"quest:valley/tower"}})
	if u.OK || u.Phase != "quests" || u.Fault == nil || u.Fault.Code != "E_REFERENCE" {
		t.Fatalf("undeclared view: ok %v phase %s fault %+v", u.OK, u.Phase, u.Fault)
	}
	// Declared again, they are as play left them.
	_, back := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, o.LiveState), "quests": all})
	if !back.OK || back.Quests[2].Status != core.StatusCompleted || !reflect.DeepEqual(back.Quests[2].Log, k2.Quests[2].Log) || !reflect.DeepEqual(back.Quests[0].Log, k2.Quests[0].Log) || len(back.Quests[1].Log) != 2 {
		t.Fatalf("declared again: ok %v error %q quests %+v", back.OK, back.Error, back.Quests)
	}

	// A held log that names, as completed ahead of it, a quest the world no
	// longer declares is the document's to place: declaring that quest again
	// is the way back.
	alone := `rules { transfer unequips; wear any; }
clock "second" at 32400;
location "loc:valley/B01" named "Trailhead";
location "loc:valley/B02" named "Ranger Outpost";
location "loc:valley/B03" named "Old Mill";
location "loc:valley/B04" named "Watchtower";
character "char:ada" named "Ada" at "loc:valley/B01" { }
quest "quest:valley/tower" named "Warn the Watchtower";
`
	_, a := invoke(t, map[string]any{"world": alone, "live_state": liveDoc(t, o.LiveState)})
	if a.OK || a.Phase != "live_state" || a.Fault == nil || a.Fault.Code != "E_QUEST" || a.Fault.Subject != "quest:valley/tower" || a.Fault.Field != "ahead" || a.Fault.Actual != "quest:valley/mill" || a.LiveState != nil {
		t.Fatalf("ahead of a quest not declared: ok %v phase %s fault %+v", a.OK, a.Phase, a.Fault)
	}
	_, a = invoke(t, map[string]any{"world": alone + `quest "quest:valley/mill" named "Clear the Old Mill";` + "\n", "live_state": liveDoc(t, o.LiveState)})
	if !a.OK {
		t.Fatalf("mill declared again: phase %s fault %+v", a.Phase, a.Fault)
	}

	// A quest the document does not hold yet, such as one from a newly
	// installed module, takes the source's log and is held from then on.
	well := world + `quest "quest:hills/well" named "The Dry Well" { started impact "the well ran dry" at 30000; }` + "\n"
	_, n := invoke(t, map[string]any{"world": well, "live_state": liveDoc(t, o.LiveState), "actions": `note quest "quest:hills/well" impact "a rope is missing";`, "actor": actor, "request": "turn:5", "quests": []string{"quest:hills/well"}})
	if !n.OK || len(n.LiveState.Quests) != 4 || n.LiveState.Quests[0].ID != "quest:hills/well" || len(n.LiveState.Quests[0].Log) != 2 || len(n.Quests[0].Log) != 2 {
		t.Fatalf("new quest: ok %v error %q fault %+v live %+v", n.OK, n.Error, n.Fault, n.LiveState)
	}
}

// liveQuestWorld is five quests with requirements and a side quest, its clock
// at any tick.
func liveQuestWorld(clock int64) string {
	return fmt.Sprintf(`rules { transfer unequips; wear any; }
clock "minute" at %d;
location "loc:camp" named "Camp";
character "char:a" named "Ada" at "loc:camp" { }
quest "quest:a" named "A";
quest "quest:b" named "B" { requires "quest:a"; }
quest "quest:c" named "C" { requires "quest:a"; requires "quest:b"; }
quest "quest:d" named "D" { side of "quest:a"; started impact "begun" at 0; }
quest "quest:e" named "E";
`, clock)
}

// Quest actions give the same answers, events, views and final quests
// whether the host keeps the checkpoint or writes a fresh world and passes
// the live state. The fresh world's clock says any tick: the document's clock
// is the one that runs.
func TestLiveStateQuestsMatchKeptWorld(t *testing.T) {
	quests := []string{"quest:a", "quest:b", "quest:c", "quest:d", "quest:e"}
	texts := []string{"", ` impact "went well"`, ` impact "lost a day"`}
	rng := rand.New(rand.NewSource(46))
	noise := rand.New(rand.NewSource(8))
	pick := func(list []string) string { return list[rng.Intn(len(list))] }
	// Notes go to two quests and half the status changes to one, so logs
	// fill: notes are dropped, and a log of status changes alone is full.
	statement := func() string {
		q := pick(quests)
		if rng.Intn(2) == 0 {
			q = "quest:e"
		}
		switch rng.Intn(14) {
		case 0, 1:
			return fmt.Sprintf(`start quest %q%s;`, q, pick(texts))
		case 2, 3, 4:
			return fmt.Sprintf(`note quest %q impact "step %d";`, pick([]string{"quest:a", "quest:d"}), rng.Intn(9))
		case 5, 6:
			return fmt.Sprintf(`complete quest %q%s;`, q, pick(texts))
		case 7:
			return fmt.Sprintf(`complete quest %q ahead because "rushed"%s;`, q, pick(texts))
		case 8:
			return fmt.Sprintf(`fail quest %q%s;`, q, pick(texts))
		case 9, 10:
			return fmt.Sprintf(`reopen quest %q because "again";`, q)
		case 11:
			return `start quest "quest:z";`
		}
		return fmt.Sprintf(`advance time by %d;`, 1+rng.Intn(9))
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
	for round := 0; round < 60; round++ {
		clock := int64(0)
		a := apply(input{World: liveQuestWorld(clock), NewLiveState: true})
		b := apply(input{World: liveQuestWorld(clock)})
		if !a.OK || !b.OK {
			t.Fatal(a.Error, b.Error)
		}
		doc, cp := a.LiveState, b.Checkpoint
		var kept, refused []sent
		for step := 0; step < 150; step++ {
			// Mostly one action, so long logs build up.
			s := sent{id: fmt.Sprintf("r%d", step), actions: statement()}
			if rng.Intn(4) == 0 {
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
			views := []core.QuestID{"quest:a", "quest:b", "quest:c", "quest:d", "quest:e"}
			ra := apply(input{World: liveQuestWorld(noise.Int63n(2) * noise.Int63n(1e6)), LiveState: &stored, Actions: s.actions, Actor: &actor, Request: s.id, Quests: views})
			rb := apply(input{Checkpoint: cp, Actions: s.actions, Actor: &actor, Request: s.id, Quests: views})
			where := fmt.Sprintf("round %d step %d request %s %q", round, step, s.id, s.actions)
			if ra.OK != rb.OK || ra.Phase != rb.Phase || ra.Historical != rb.Historical || ra.Error != rb.Error || !reflect.DeepEqual(ra.Fault, rb.Fault) || !reflect.DeepEqual(ra.Diagnostics, rb.Diagnostics) || ra.ViewError != nil || rb.ViewError != nil {
				t.Fatalf("%s:\nlive ok %v phase %s historical %v error %q fault %+v diagnostics %+v\nkept ok %v phase %s historical %v error %q fault %+v diagnostics %+v", where, ra.OK, ra.Phase, ra.Historical, ra.Error, ra.Fault, ra.Diagnostics, rb.OK, rb.Phase, rb.Historical, rb.Error, rb.Fault, rb.Diagnostics)
			}
			if !reflect.DeepEqual(questViews(ra.Quests), questViews(rb.Quests)) {
				t.Fatalf("%s: views differ\nlive %+v\nkept %+v", where, ra.Quests, rb.Quests)
			}
			switch {
			case !ra.OK:
				key := "refused " + ra.Phase
				if ra.Fault != nil {
					key += " " + ra.Fault.Code
					if ra.Fault.Code == "E_QUEST" {
						key += " " + ra.Fault.Field
					}
				}
				answers[key]++
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
				if !reflect.DeepEqual(zero(ra.Receipt.Events), zero(rb.Receipt.Events)) {
					t.Fatalf("%s: events differ\nlive %+v\nkept %+v", where, ra.Receipt.Events, rb.Receipt.Events)
				}
				for _, e := range ra.Receipt.Events {
					if e.Quest != nil && e.Quest.Dropped != nil {
						answers["note dropped"]++
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
		// Every quest is the same.
		final := apply(input{Checkpoint: cp, IncludeState: true})
		var want []livestate.Quest
		for _, q := range final.Checkpoint.State.Quests {
			want = append(want, livestate.Quest{ID: q.ID, Status: q.Status, Log: q.Log})
		}
		if !reflect.DeepEqual(doc.Quests, want) || !reflect.DeepEqual(doc.Clock, final.Checkpoint.State.Clock) {
			t.Fatalf("round %d: quests differ\nlive %+v %+v\nkept %+v %+v", round, doc.Quests, doc.Clock, want, final.Checkpoint.State.Clock)
		}
	}
	t.Logf("answers %v", answers)
	for _, k := range []string{"applied, kept", "already applied", "refused compile", "refused execute E_NO_CHANGE", "refused execute E_REQUEST_REUSE", "refused execute E_QUEST status", "refused execute E_QUEST requires", "refused execute E_QUEST ahead", "refused execute E_QUEST log", "note dropped"} {
		if answers[k] < 20 {
			t.Fatalf("too few %q: %v", k, answers)
		}
	}
}
