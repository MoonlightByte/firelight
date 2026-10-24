package tests

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const questPlaces = `rules { transfer unequips; wear any; }
clock "minute" at 1440;
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" { }
`

// A chain: B requires A, C requires A and B; S is a side quest of A.
const questChain = `quest "quest:a" named "A";
quest "quest:b" named "B" { requires "quest:a"; }
quest "quest:c" named "C" { requires "quest:b"; requires "quest:a"; }
quest "quest:s" named "S" { side of "quest:a"; }
`

func questFixture(t testing.TB, quests string) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("quests.nql", questPlaces+quests)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func questOf(t testing.TB, w *core.World, id core.QuestID) core.Quest {
	t.Helper()
	for _, q := range w.Snapshot().Quests {
		if q.ID == id {
			return q
		}
	}
	t.Fatalf("quest %s missing", id)
	return core.Quest{}
}

func text(s string) *string { return &s }

func tick(n uint64) *uint64 { return &n }

func questRequest(w *core.World, id string, ops ...core.QuestOp) core.Request {
	r := core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}}
	for i := range ops {
		r.Operations = append(r.Operations, core.Operation{Quest: &ops[i]})
	}
	return r
}

func wantQuestFault(t testing.TB, err error, code, field, expected, actual string) {
	t.Helper()
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != code || f.Field != field || f.Expected != expected || f.Actual != actual {
		t.Fatalf("got %#v, want %s %s %q %q", err, code, field, expected, actual)
	}
}

func TestQuestDeclarations(t *testing.T) {
	b := questFixture(t, questChain)
	want := []core.Quest{
		{ID: "quest:a", Name: "A", Status: core.StatusUnstarted},
		{ID: "quest:b", Name: "B", Requires: []core.QuestID{"quest:a"}, Status: core.StatusUnstarted},
		{ID: "quest:c", Name: "C", Requires: []core.QuestID{"quest:a", "quest:b"}, Status: core.StatusUnstarted},
		{ID: "quest:s", Name: "S", SideOf: "quest:a", Status: core.StatusUnstarted},
	}
	if got := world(t, b).Snapshot().Quests; !reflect.DeepEqual(got, want) {
		t.Fatalf("quests\n%+v\nwant\n%+v", got, want)
	}

	// A declared log states progress back; the status is the one it ends in.
	b = questFixture(t, questChain+`quest "quest:d" named "D" {
  requires "quest:c"; requires "quest:b";
  started impact "set out" at 5;
  note impact "found the map";
  completed ahead "quest:c" "quest:b" because "the keep came first" impact "done" at 9;
  reopened because "it was a decoy";
  failed;
}
`)
	d := questOf(t, world(t, b), "quest:d")
	wantD := core.Quest{ID: "quest:d", Name: "D", Requires: []core.QuestID{"quest:b", "quest:c"}, Status: core.StatusFailed, Log: []core.QuestEntry{
		{Kind: core.EntryStarted, Text: text("set out"), Tick: tick(5)},
		{Kind: core.EntryNote, Text: text("found the map")},
		{Kind: core.EntryCompleted, Text: text("done"), Ahead: []core.QuestID{"quest:b", "quest:c"}, Reason: text("the keep came first"), Tick: tick(9)},
		{Kind: core.EntryReopened, Reason: text("it was a decoy")},
		{Kind: core.EntryFailed},
	}}
	if !reflect.DeepEqual(d, wantD) {
		t.Fatalf("declared log\n%+v\nwant\n%+v", d, wantD)
	}

	// Quests are kept in ID order, whatever the source order.
	if got := world(t, questFixture(t, `quest "quest:z" named "Z"; quest "quest:m" named "M"; quest "quest:a" named "A";`+"\n")).Snapshot().Quests; got[0].ID != "quest:a" || got[1].ID != "quest:m" || got[2].ID != "quest:z" {
		t.Fatalf("order %+v", got)
	}

	// The short form and an empty body are the same quest. Quests need no
	// clock, and then a log has no ticks.
	one := questFixture(t, `quest "quest:x" named "X";`+"\n").Quests
	two := questFixture(t, `quest "quest:x" named "X" { }`+"\n").Quests
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("forms %+v %+v", one, two)
	}
	plain, ds := compiler.CompileWorld("plain.nql", strings.Replace(questPlaces, "clock \"minute\" at 1440;\n", "", 1)+questChain)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, plain)
	if _, err := w.Execute(questRequest(w, "start", core.QuestOp{Start: &core.QuestStep{Quest: "quest:a"}})); err != nil {
		t.Fatal(err)
	}
	if got := questOf(t, w, "quest:a").Log; len(got) != 1 || got[0].Tick != nil {
		t.Fatalf("no clock, log %+v", got)
	}
}

// Every action against every status: the move it makes, or the exact refusal.
func TestQuestTransitions(t *testing.T) {
	src := `quest "quest:u" named "U";
quest "quest:s" named "S" { started; }
quest "quest:d" named "D" { completed; }
quest "quest:f" named "F" { failed; }
`
	type outcome struct {
		status   core.QuestStatus
		event    core.EventKind
		entry    core.QuestEntryKind
		code     string // a refusal
		expected string
	}
	ops := map[string]func(core.QuestID) core.QuestOp{
		"start": func(id core.QuestID) core.QuestOp {
			return core.QuestOp{Start: &core.QuestStep{Quest: id, Text: text("go")}}
		},
		"note": func(id core.QuestID) core.QuestOp {
			return core.QuestOp{Note: &core.QuestStep{Quest: id, Text: text("go")}}
		},
		"complete": func(id core.QuestID) core.QuestOp {
			return core.QuestOp{Complete: &core.CompleteQuest{Quest: id, Text: text("go")}}
		},
		"fail": func(id core.QuestID) core.QuestOp {
			return core.QuestOp{Fail: &core.QuestStep{Quest: id, Text: text("go")}}
		},
		"reopen": func(id core.QuestID) core.QuestOp {
			return core.QuestOp{Reopen: &core.ReopenQuest{Quest: id, Reason: text("go")}}
		},
	}
	ok := func(s core.QuestStatus, e core.EventKind, k core.QuestEntryKind) outcome {
		return outcome{status: s, event: e, entry: k}
	}
	same := outcome{code: "E_NO_CHANGE"}
	refuse := func(expected string) outcome { return outcome{code: "E_QUEST", expected: expected} }
	table := map[string]map[core.QuestID]outcome{
		"start":    {"quest:u": ok(core.StatusStarted, core.QuestStarted, core.EntryStarted), "quest:s": same, "quest:d": refuse("unstarted"), "quest:f": refuse("unstarted")},
		"note":     {"quest:u": refuse("started"), "quest:s": ok(core.StatusStarted, core.QuestNoted, core.EntryNote), "quest:d": refuse("started"), "quest:f": refuse("started")},
		"complete": {"quest:u": ok(core.StatusCompleted, core.QuestCompleted, core.EntryCompleted), "quest:s": ok(core.StatusCompleted, core.QuestCompleted, core.EntryCompleted), "quest:d": same, "quest:f": refuse("unstarted or started")},
		"fail":     {"quest:u": ok(core.StatusFailed, core.QuestFailed, core.EntryFailed), "quest:s": ok(core.StatusFailed, core.QuestFailed, core.EntryFailed), "quest:d": refuse("unstarted or started"), "quest:f": same},
		"reopen":   {"quest:u": refuse("completed or failed"), "quest:s": refuse("completed or failed"), "quest:d": ok(core.StatusStarted, core.QuestReopened, core.EntryReopened), "quest:f": ok(core.StatusStarted, core.QuestReopened, core.EntryReopened)},
	}
	for verb, row := range table {
		for id, want := range row {
			t.Run(verb+" "+string(id), func(t *testing.T) {
				b := questFixture(t, src)
				w := world(t, b)
				before, history := w.Snapshot(), w.History()
				old := questOf(t, w, id)
				batch, err := w.Execute(questRequest(w, "r", ops[verb](id)))
				if want.code != "" {
					if want.code == "E_NO_CHANGE" {
						wantQuestFault(t, err, "E_NO_CHANGE", "operation", "state transition", "unchanged")
					} else {
						wantQuestFault(t, err, "E_QUEST", "status", want.expected, string(old.Status))
					}
					unchanged(t, w, before, history)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				q := questOf(t, w, id)
				last := q.Log[len(q.Log)-1]
				wantEntry := core.QuestEntry{Kind: want.entry, Tick: tick(1440)}
				if verb == "reopen" {
					wantEntry.Reason = text("go")
				} else {
					wantEntry.Text = text("go")
				}
				if q.Status != want.status || len(q.Log) != len(old.Log)+1 || !reflect.DeepEqual(last, wantEntry) {
					t.Fatalf("after %+v", q)
				}
				if len(batch.Events) != 1 || batch.Events[0].Kind != want.event || batch.Events[0].Quest == nil ||
					!reflect.DeepEqual(batch.Events[0].Quest.Before, old) || !reflect.DeepEqual(batch.Events[0].Quest.After, q) || batch.Events[0].Quest.Dropped != nil {
					t.Fatalf("events %+v", batch.Events)
				}
				costReplay(t, b, w)
			})
		}
	}
}

func TestQuestRequirements(t *testing.T) {
	b := questFixture(t, questChain)
	w := world(t, b)
	complete := func(id core.QuestID, ahead *string) core.QuestOp {
		return core.QuestOp{Complete: &core.CompleteQuest{Quest: id, Ahead: ahead}}
	}
	view := func(ids ...core.QuestID) []core.QuestProjection {
		t.Helper()
		p, err := w.QuestsView(ids)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Completing names every open requirement; starting and failing are
	// never gated.
	_, err := w.Execute(questRequest(w, "early", complete("quest:c", nil)))
	wantQuestFault(t, err, "E_QUEST", "requires", "completed requirements", "quest:a, quest:b")
	if _, err := w.Execute(questRequest(w, "start c", core.QuestOp{Start: &core.QuestStep{Quest: "quest:c"}})); err != nil {
		t.Fatal(err)
	}
	if p := view("quest:a", "quest:c"); !reflect.DeepEqual(p[1].Open, []core.QuestID{"quest:a", "quest:b"}) || p[1].Ready || !p[0].Ready || len(p[0].Open) != 0 {
		t.Fatalf("view %+v", p)
	}

	// In one request, in order: A, then B, then C.
	if _, err := w.Execute(questRequest(w, "chain", complete("quest:a", nil), complete("quest:b", nil))); err != nil {
		t.Fatal(err)
	}
	if p := view("quest:c"); len(p[0].Open) != 0 || !p[0].Ready {
		t.Fatalf("ready %+v", p)
	}
	_, err = w.Execute(questRequest(w, "nothing ahead", complete("quest:c", text("why"))))
	wantQuestFault(t, err, "E_QUEST", "ahead", "an open requirement", "none")

	// Reopening a requirement opens it again for the quests that need it,
	// without touching them.
	if _, err := w.Execute(questRequest(w, "reopen a", core.QuestOp{Reopen: &core.ReopenQuest{Quest: "quest:a", Reason: text("a decoy")}})); err != nil {
		t.Fatal(err)
	}
	if p := view("quest:b", "quest:c"); p[0].Status != core.StatusCompleted || !reflect.DeepEqual(p[0].Open, []core.QuestID{"quest:a"}) || !reflect.DeepEqual(p[1].Open, []core.QuestID{"quest:a"}) {
		t.Fatalf("reopened %+v", p)
	}

	// A failed requirement is open; ahead records the open ones and why.
	if _, err := w.Execute(questRequest(w, "fail a", core.QuestOp{Fail: &core.QuestStep{Quest: "quest:a"}})); err != nil {
		t.Fatal(err)
	}
	_, err = w.Execute(questRequest(w, "after a failure", complete("quest:c", nil)))
	wantQuestFault(t, err, "E_QUEST", "requires", "completed requirements", "quest:a")
	batch, err := w.Execute(questRequest(w, "ahead", core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:c", Ahead: text("the keep came first"), Text: text("won anyway")}}))
	if err != nil {
		t.Fatal(err)
	}
	c := questOf(t, w, "quest:c")
	if want := (core.QuestEntry{Kind: core.EntryCompleted, Text: text("won anyway"), Ahead: []core.QuestID{"quest:a"}, Reason: text("the keep came first"), Tick: tick(1440)}); c.Status != core.StatusCompleted || !reflect.DeepEqual(c.Log[len(c.Log)-1], want) {
		t.Fatalf("ahead %+v", c)
	}
	if !reflect.DeepEqual(batch.Events[0].Quest.Intent, core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:c", Ahead: text("the keep came first"), Text: text("won anyway")}}) {
		t.Fatalf("intent %+v", batch.Events[0].Quest.Intent)
	}
	costReplay(t, b, w)
}

func TestQuestLog(t *testing.T) {
	b := questFixture(t, `quest "quest:x" named "X";`+"\n")
	w := world(t, b)
	note := func(s string) core.QuestOp {
		return core.QuestOp{Note: &core.QuestStep{Quest: "quest:x", Text: text(s)}}
	}
	if _, err := w.Execute(questRequest(w, "start", core.QuestOp{Start: &core.QuestStep{Quest: "quest:x"}})); err != nil {
		t.Fatal(err)
	}
	// Time passes between entries; each entry has the tick it was made at.
	for i := 1; i < core.MaxQuestLog; i++ {
		if _, err := w.Execute(questRequest(w, fmt.Sprint("note ", i), note(fmt.Sprint("n", i)))); err != nil {
			t.Fatal(err)
		}
	}
	execute(t, w, "wait", `advance time by 60;`)
	// The log is full: a new note drops the oldest note, never the start.
	batch, err := w.Execute(questRequest(w, "note 16", note("n16")))
	if err != nil {
		t.Fatal(err)
	}
	x := questOf(t, w, "quest:x")
	if len(x.Log) != core.MaxQuestLog || x.Log[0].Kind != core.EntryStarted || *x.Log[1].Text != "n2" || *x.Log[15].Text != "n16" || *x.Log[15].Tick != 1500 {
		t.Fatalf("log %+v", x.Log)
	}
	if d := batch.Events[0].Quest.Dropped; d == nil || !reflect.DeepEqual(*d, core.QuestEntry{Kind: core.EntryNote, Text: text("n1"), Tick: tick(1440)}) {
		t.Fatalf("dropped %+v", d)
	}
	// A status change drops the oldest note too.
	if _, err := w.Execute(questRequest(w, "done", core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:x"}})); err != nil {
		t.Fatal(err)
	}
	if x := questOf(t, w, "quest:x"); len(x.Log) != core.MaxQuestLog || *x.Log[1].Text != "n3" || x.Log[15].Kind != core.EntryCompleted {
		t.Fatalf("log %+v", x.Log)
	}
	costReplay(t, b, w)

	// Sixteen status changes fill a log for good: nothing more is taken.
	full := `quest "quest:y" named "Y" { started;`
	for i := 1; i < core.MaxQuestLog; i += 2 {
		full += ` completed; reopened because "again";`
	}
	full = strings.TrimSuffix(full, ` reopened because "again";`) + " }\n"
	w = world(t, questFixture(t, full))
	y := questOf(t, w, "quest:y")
	if len(y.Log) != core.MaxQuestLog || y.Status != core.StatusCompleted {
		t.Fatalf("full log %d %s", len(y.Log), y.Status)
	}
	before, history := w.Snapshot(), w.History()
	_, err = w.Execute(questRequest(w, "seventeenth", core.QuestOp{Reopen: &core.ReopenQuest{Quest: "quest:y", Reason: text("once more")}}))
	wantQuestFault(t, err, "E_QUEST", "log", "at most 16 status changes", "full")
	unchanged(t, w, before, history)
}

func TestQuestTexts(t *testing.T) {
	w := world(t, questFixture(t, questChain+`quest "quest:d" named "D" { completed; }`+"\n"))
	long, longest := strings.Repeat("t", core.MaxQuestTextBytes+1), strings.Repeat("t", core.MaxQuestTextBytes)
	for _, tc := range []struct {
		name                          string
		op                            core.QuestOp
		code, field, expected, actual string
	}{
		{"impact too long", core.QuestOp{Start: &core.QuestStep{Quest: "quest:a", Text: &long}}, "E_QUEST", "text", "valid text of at most 512 bytes", "513 bytes"},
		{"blank impact", core.QuestOp{Fail: &core.QuestStep{Quest: "quest:a", Text: text(" ")}}, "E_QUEST", "text", "valid text of at most 512 bytes", "invalid"},
		{"empty impact", core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:a", Text: text("")}}, "E_QUEST", "text", "valid text of at most 512 bytes", "invalid"},
		{"a note without text", core.QuestOp{Note: &core.QuestStep{Quest: "quest:a"}}, "E_QUEST", "text", "text for a note", "absent"},
		{"blank ahead", core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:b", Ahead: text("\t")}}, "E_QUEST", "reason", "valid text of at most 512 bytes", "invalid"},
		{"reopen without a reason", core.QuestOp{Reopen: &core.ReopenQuest{Quest: "quest:d"}}, "E_QUEST", "reason", "a reason for reopen", "absent"},
		{"reopen reason too long", core.QuestOp{Reopen: &core.ReopenQuest{Quest: "quest:d", Reason: &long}}, "E_QUEST", "reason", "valid text of at most 512 bytes", "513 bytes"},
		{"unknown quest", core.QuestOp{Start: &core.QuestStep{Quest: "quest:zz"}}, "E_REFERENCE", "reference", "quest", "missing"},
		{"two changes", core.QuestOp{Start: &core.QuestStep{Quest: "quest:a"}, Fail: &core.QuestStep{Quest: "quest:a"}}, "E_IR", "quest", "exactly one quest change", "invalid sum"},
		{"no change", core.QuestOp{}, "E_IR", "quest", "exactly one quest change", "invalid sum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, history := w.Snapshot(), w.History()
			_, err := w.Execute(questRequest(w, tc.name, tc.op))
			wantQuestFault(t, err, tc.code, tc.field, tc.expected, tc.actual)
			unchanged(t, w, before, history)
		})
	}
	if _, err := w.Execute(questRequest(w, "longest", core.QuestOp{Start: &core.QuestStep{Quest: "quest:a", Text: &longest}})); err != nil {
		t.Fatal(err)
	}
}

// Records the grammar cannot write are refused by the engine.
func TestQuestRecordRefusals(t *testing.T) {
	base := questFixture(t, questChain)
	for _, tc := range []struct {
		name                          string
		change                        func(q *core.Quest)
		code, field, expected, actual string
	}{
		{"status", func(q *core.Quest) { q.Status = "done" }, "E_QUEST", "status", "unstarted, started, completed or failed", "done"},
		{"status against the log", func(q *core.Quest) { q.Status = core.StatusStarted }, "E_QUEST", "status", "unstarted, as the log ends", "started"},
		{"unknown entry", func(q *core.Quest) {
			q.Log = []core.QuestEntry{{Kind: "won"}}
		}, "E_QUEST", "log", "started, note, completed, failed or reopened", "won"},
		{"ahead on a start", func(q *core.Quest) {
			q.Status, q.Log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryStarted, Ahead: []core.QuestID{"quest:b"}}}
		}, "E_QUEST", "ahead", "ahead only for completed", "started"},
		{"reason on a start", func(q *core.Quest) {
			q.Status, q.Log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryStarted, Reason: text("why")}}
		}, "E_QUEST", "reason", "a reason only for reopened or ahead", "started"},
		{"ahead without a reason", func(q *core.Quest) {
			q.Status, q.Log = core.StatusCompleted, []core.QuestEntry{{Kind: core.EntryCompleted, Ahead: []core.QuestID{"quest:b"}}}
		}, "E_QUEST", "reason", "a reason for ahead", "absent"},
		{"a reason without ahead", func(q *core.Quest) {
			q.Status, q.Log = core.StatusCompleted, []core.QuestEntry{{Kind: core.EntryCompleted, Reason: text("why")}}
		}, "E_QUEST", "reason", "a reason only with ahead", "present"},
		{"a note without text", func(q *core.Quest) {
			q.Status, q.Log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryStarted}, {Kind: core.EntryNote}}
		}, "E_QUEST", "text", "text for a note", "absent"},
		{"reopened without a reason", func(q *core.Quest) {
			q.Status, q.Log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryFailed}, {Kind: core.EntryReopened}}
		}, "E_QUEST", "reason", "a reason for reopened", "absent"},
		{"text on reopened", func(q *core.Quest) {
			q.Status, q.Log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryCompleted}, {Kind: core.EntryReopened, Reason: text("r"), Text: text("t")}}
		}, "E_QUEST", "text", "no text for reopened", "present"},
		{"a long ahead", func(q *core.Quest) {
			e := core.QuestEntry{Kind: core.EntryCompleted, Reason: text("r")}
			for i := 0; i <= core.MaxQuestRequirements; i++ {
				e.Ahead = append(e.Ahead, "quest:b")
			}
			q.Status, q.Log = core.StatusCompleted, []core.QuestEntry{e}
		}, "E_QUEST", "ahead", "at most 16 requirements", "17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := base
			b.Quests = append([]core.Quest(nil), base.Quests...)
			tc.change(&b.Quests[0])
			_, err := core.NewWorld(b)
			wantQuestFault(t, err, tc.code, tc.field, tc.expected, tc.actual)
		})
	}
}

// A world at the limits: 4,096 quests, each requiring the 16 before it.
// Each requirement is searched for cycles once, so this compiles at once.
func TestQuestLimits(t *testing.T) {
	var src strings.Builder
	for i := 0; i < core.MaxWorldQuests; i++ {
		fmt.Fprintf(&src, "quest \"quest:%04d\" named \"Q\" {", i)
		for j := max(0, i-core.MaxQuestRequirements); j < i; j++ {
			fmt.Fprintf(&src, " requires \"quest:%04d\";", j)
		}
		src.WriteString(" }\n")
	}
	w := world(t, questFixture(t, src.String()))
	last := core.QuestID(fmt.Sprintf("quest:%04d", core.MaxWorldQuests-1))
	ps, err := w.QuestsView([]core.QuestID{"quest:0000", last})
	if err != nil || !ps[0].Ready || len(ps[1].Open) != core.MaxQuestRequirements || ps[1].Ready {
		t.Fatalf("view %+v %v", ps, err)
	}
	if _, err := w.Execute(questRequest(w, "skip", core.QuestOp{Complete: &core.CompleteQuest{Quest: last, Ahead: text("a shortcut")}})); err != nil {
		t.Fatal(err)
	}
	if q := questOf(t, w, last); len(q.Log[0].Ahead) != core.MaxQuestRequirements {
		t.Fatalf("ahead %+v", q.Log[0])
	}
}

// IDs the compiler refuses first are refused by the engine too.
func TestQuestIDsAreUnique(t *testing.T) {
	src := questChain + `occupant "occ:x" named "O" at "loc:camp" { person; }` + "\n"
	base := questFixture(t, src)
	for _, id := range []core.QuestID{"quest:b", "loc:camp", "char:ada"} {
		b := base
		b.Quests = append(append([]core.Quest(nil), base.Quests...), core.Quest{ID: id, Name: "Q", Status: core.StatusUnstarted})
		_, err := core.NewWorld(b)
		wantQuestFault(t, err, "E_DUPLICATE", "quest", "unique quest ID", string(id))
	}
	// An occupant and a quest never share an ID, declared or created.
	b := base
	b.Quests = append(append([]core.Quest(nil), base.Quests...), core.Quest{ID: "occ:x", Name: "Q", Status: core.StatusUnstarted})
	_, err := core.NewWorld(b)
	wantQuestFault(t, err, "E_DUPLICATE", "occupant", "unique occupant ID", "occ:x")
	b = base
	b.Occupants = append(append([]core.Occupant(nil), base.Occupants...), base.Occupants[0])
	b.Occupants[1].ID = "quest:a"
	_, err = core.NewWorld(b)
	wantQuestFault(t, err, "E_DUPLICATE", "occupant", "unique occupant ID", "quest:a")
	src = `create occupant "quest:a" named "O" at "loc:camp" { person; };`
	if _, ds := compiler.CompileActions("actions.nql", src, base); len(ds) != 1 || ds[0].Code != "E_DUPLICATE" || ds[0].Column != strings.Index(src, `"quest:a"`)+1 {
		t.Fatalf("create: %#v", ds)
	}
	w := world(t, base)
	create := base.Occupants[0]
	create.ID = "quest:a"
	_, err = w.Execute(core.Request{ID: "create", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{{Occupant: &core.OccupantOp{Create: &create}}}})
	wantQuestFault(t, err, "E_DUPLICATE", "occupant", "unique occupant ID", "quest:a")
}

func TestQuestCompileRefusals(t *testing.T) {
	head := strings.Count(questPlaces, "\n") + 1 // the quests' first line
	requires, entries := "", ""
	for i := 0; i <= core.MaxQuestRequirements; i++ {
		requires += fmt.Sprintf(` requires "quest:r%02d";`, i)
	}
	for i := 0; i < core.MaxQuestLog; i++ {
		entries += ` note impact "n";`
	}
	many := ""
	for i := 0; i <= core.MaxQuestRequirements; i++ {
		many += fmt.Sprintf("quest \"quest:r%02d\" named \"R\";\n", i)
	}
	for _, tc := range []struct {
		name, body string
		line       int    // line within the body
		at         string // its last occurrence on the line marks the column
		code       string
		expected   string
	}{
		{"unknown requirement", `quest "quest:x" named "X" { requires "quest:zz"; }`, 1, `"quest:zz"`, "E_QUEST", "a declared quest"},
		{"requires itself", `quest "quest:x" named "X" { requires "quest:x"; }`, 1, `"quest:x"`, "E_QUEST", "another quest"},
		{"requires twice", questChain + `quest "quest:x" named "X" { requires "quest:a"; requires "quest:a"; }`, 5, `"quest:a"`, "E_QUEST", "each quest once"},
		{"a cycle", "quest \"quest:x\" named \"X\" { requires \"quest:y\"; }\nquest \"quest:y\" named \"Y\" { requires \"quest:z\"; }\nquest \"quest:z\" named \"Z\" { requires \"quest:x\"; }", 3, `"quest:x"`, "E_QUEST", "no cycle"},
		{"too many requirements", many + `quest "quest:x" named "X" {` + requires + ` }`, core.MaxQuestRequirements + 2, `"quest:r16"`, "E_QUEST", "at most 16 requirements"},
		{"unknown side of", `quest "quest:x" named "X" { side of "quest:zz"; }`, 1, `"quest:zz"`, "E_QUEST", "a declared quest"},
		{"side of itself", `quest "quest:x" named "X" { side of "quest:x"; }`, 1, `"quest:x"`, "E_QUEST", "another quest"},
		{"two side of", questChain + `quest "quest:x" named "X" { side of "quest:a"; side of "quest:b"; }`, 5, `side`, "E_DUPLICATE", "one side of"},
		{"note before a start", `quest "quest:x" named "X" { note impact "n"; }`, 1, `"quest:x"`, "E_QUEST", "a log the actions allow"},
		{"completed twice", `quest "quest:x" named "X" { completed; completed; }`, 1, `"quest:x"`, "E_QUEST", "a log the actions allow"},
		{"started after a failure", `quest "quest:x" named "X" { failed; started; }`, 1, `"quest:x"`, "E_QUEST", "a log the actions allow"},
		{"reopened while open", `quest "quest:x" named "X" { started; reopened because "r"; }`, 1, `"quest:x"`, "E_QUEST", "a log the actions allow"},
		{"too long a log", `quest "quest:x" named "X" { started;` + entries + ` }`, 1, `note`, "E_QUEST", "at most 16 entries"},
		{"blank impact", `quest "quest:x" named "X" { started impact " "; }`, 1, `" "`, "E_QUEST", "valid text of at most 512 bytes"},
		{"long impact", `quest "quest:x" named "X" { started impact "` + strings.Repeat("t", core.MaxQuestTextBytes+1) + `"; }`, 1, `"t`, "E_QUEST", "valid text of at most 512 bytes"},
		{"blank reason", `quest "quest:x" named "X" { completed; reopened because ""; }`, 1, `""`, "E_QUEST", "valid text of at most 512 bytes"},
		{"unknown ahead", questChain + `quest "quest:x" named "X" { completed ahead "quest:zz" because "r"; }`, 5, `"quest:zz"`, "E_QUEST", "a declared quest"},
		{"ahead twice", questChain + `quest "quest:x" named "X" { completed ahead "quest:a" "quest:a" because "r"; }`, 5, `"quest:a"`, "E_QUEST", "each quest once"},
		{"ahead without because", `quest "quest:x" named "X" { completed ahead "quest:x"; }`, 1, `;`, "E_PARSE", ""},
		{"a reason for a start", `quest "quest:x" named "X" { started because "r"; }`, 1, `because`, "E_PARSE", ""},
		{"ahead on a start", `quest "quest:x" named "X" { started ahead "quest:x" because "r"; }`, 1, `ahead`, "E_PARSE", ""},
		{"too many ahead", many + `quest "quest:x" named "X" { completed ahead` + strings.NewReplacer(" requires", "", ";", "").Replace(requires) + ` because "r"; }`, core.MaxQuestRequirements + 2, `"quest:r16"`, "E_QUEST", "at most 16 requirements"},
		{"blank ID", `quest " " named "X";`, 1, `" "`, "E_QUEST", "valid quest ID"},
		{"tick too large", `quest "quest:x" named "X" { started at 18446744073709551616; }`, 1, `18446744073709551616`, "E_INTEGER", ""},
		{"negative tick", `quest "quest:x" named "X" { started at -1; }`, 1, `-1`, "E_INTEGER", ""},
		{"blank name", `quest "quest:x" named " ";`, 1, `" "`, "E_QUEST", "valid name"},
		{"a place's ID", `quest "loc:camp" named "X";`, 1, `"loc:camp"`, "E_DUPLICATE", "unique canonical ID"},
		{"two of one ID", "quest \"quest:x\" named \"X\";\nquest \"quest:x\" named \"Y\";", 2, `"quest:x"`, "E_DUPLICATE", "unique canonical ID"},
		{"an occupant's ID", `occupant "occ:x" named "O" at "loc:camp" { person; }` + "\n" + `quest "occ:x" named "X";`, 2, `"occ:x"`, "E_DUPLICATE", "unique canonical ID"},
		{"unknown entry", `quest "quest:x" named "X" { won; }`, 1, `won`, "E_PARSE", ""},
		{"missing brace", `quest "quest:x" named "X" { started;`, 1, ``, "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld("quests.nql", questPlaces+tc.body+"\n")
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			text := strings.Split(tc.body, "\n")[tc.line-1]
			line, column := head+tc.line-1, strings.LastIndex(text, tc.at)+1
			if tc.at == "" { // the end of the source
				line, column = head+tc.line, 1
			}
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != line || d.Column != column {
				t.Fatalf("got %#v, want %s %q at %d:%d", d, tc.code, tc.expected, line, column)
			}
		})
	}

	// A log refusal names the entry and the status before it.
	_, ds := compiler.CompileWorld("quests.nql", questPlaces+`quest "quest:x" named "X" { started; note impact "n"; failed; failed; }`+"\n")
	if len(ds) != 1 || ds[0].Actual != `"quest:x" (failed after failed)` {
		t.Fatalf("log: %#v", ds)
	}

	// Too many quests.
	var all strings.Builder
	for i := 0; i <= core.MaxWorldQuests; i++ {
		fmt.Fprintf(&all, "quest \"quest:%d\" named \"Q\";\n", i)
	}
	_, ds = compiler.CompileWorld("quests.nql", questPlaces+all.String())
	if len(ds) != 1 || ds[0].Code != "E_QUEST" || ds[0].Expected != "at most 4096 quests" || ds[0].Line != head+core.MaxWorldQuests || ds[0].Column != 1 {
		t.Fatalf("too many quests: %#v", ds)
	}
}

func TestQuestActionCompileRefusals(t *testing.T) {
	catalog := questFixture(t, questChain+`quest "quest:d" named "D" { completed; }`+"\n")
	for _, tc := range []struct {
		name, src, at, code, expected string
	}{
		{"unknown quest", `start quest "quest:zz";`, `"quest:zz"`, "E_REFERENCE", "quest"},
		{"a place", `start quest "loc:camp";`, `"loc:camp"`, "E_REFERENCE", "quest"},
		{"blank impact", `start quest "quest:a" impact "";`, `""`, "E_QUEST", "valid text of at most 512 bytes"},
		{"long note", `note quest "quest:a" impact "` + strings.Repeat("t", core.MaxQuestTextBytes+1) + `";`, `"t`, "E_QUEST", "valid text of at most 512 bytes"},
		{"blank ahead", `complete quest "quest:b" ahead because " " impact "x";`, `" "`, "E_QUEST", "valid text of at most 512 bytes"},
		{"blank reopen", `reopen quest "quest:d" because "";`, `""`, "E_QUEST", "valid text of at most 512 bytes"},
		{"note without impact", `note quest "quest:a";`, `;`, "E_PARSE", ""},
		{"reopen without because", `reopen quest "quest:d";`, `;`, "E_PARSE", ""},
		{"ahead on a start", `start quest "quest:a" ahead because "r";`, `ahead`, "E_PARSE", ""},
		{"impact before ahead", `complete quest "quest:b" impact "x" ahead because "r";`, `ahead`, "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("actions.nql", tc.src, catalog)
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			column := strings.LastIndex(tc.src, tc.at) + 1
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != 1 || d.Column != column {
				t.Fatalf("got %#v, want %s %q at 1:%d", d, tc.code, tc.expected, column)
			}
		})
	}

	// The actions lower to the operations they name.
	ops, ds := compiler.CompileActions("actions.nql", `start quest "quest:a" impact "go";
note quest "quest:a" impact "map";
complete quest "quest:c" ahead because "skip" impact "won";
fail quest "quest:b";
reopen quest "quest:d" because "decoy";
`, catalog)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	want := []core.QuestOp{
		{Start: &core.QuestStep{Quest: "quest:a", Text: text("go")}},
		{Note: &core.QuestStep{Quest: "quest:a", Text: text("map")}},
		{Complete: &core.CompleteQuest{Quest: "quest:c", Ahead: text("skip"), Text: text("won")}},
		{Fail: &core.QuestStep{Quest: "quest:b"}},
		{Reopen: &core.ReopenQuest{Quest: "quest:d", Reason: text("decoy")}},
	}
	if len(ops) != len(want) {
		t.Fatalf("ops %+v", ops)
	}
	for i := range want {
		if ops[i].Quest == nil || !reflect.DeepEqual(*ops[i].Quest, want[i]) {
			t.Fatalf("op %d %+v", i, ops[i])
		}
	}
}

func TestQuestsView(t *testing.T) {
	w := world(t, questFixture(t, questChain))
	execute(t, w, "go", `start quest "quest:a" impact "set out";`)
	ps, err := w.QuestsView([]core.QuestID{"quest:c", "quest:a", "quest:zz", "quest:b"})
	wantQuestFault(t, err, "E_REFERENCE", "reference", "quest", "missing")
	if len(ps) != 2 || ps[0].ID != "quest:c" || ps[1].ID != "quest:a" || ps[0].Revision != 1 {
		t.Fatalf("views before the unknown quest %+v", ps)
	}
	// The view is a copy.
	ps[1].Log[0].Text = text("changed")
	ps[0].Requires[0] = "quest:x"
	if q := questOf(t, w, "quest:a"); *q.Log[0].Text != "set out" || questOf(t, w, "quest:c").Requires[0] != "quest:a" {
		t.Fatalf("view aliases the world %+v", q)
	}
	op := core.QuestOp{Complete: &core.CompleteQuest{Quest: "quest:c", Ahead: text("skip")}}
	if _, err := w.Execute(questRequest(w, "ahead", op)); err != nil {
		t.Fatal(err)
	}
	op.Complete.Quest = "quest:b"
	ps, _ = w.QuestsView([]core.QuestID{"quest:c"})
	ps[0].Log[0].Ahead[0] = "quest:x"
	history := w.History()
	if q := questOf(t, w, "quest:c"); q.Log[0].Ahead[0] != "quest:a" || history[len(history)-1].Events[0].Quest.Intent.Complete.Quest != "quest:c" {
		t.Fatalf("aliased %+v", q)
	}
	// A side quest is ready at once; the view never reads side of.
	ps, err = w.QuestsView([]core.QuestID{"quest:s"})
	if err != nil || !ps[0].Ready || ps[0].SideOf != "quest:a" || len(ps[0].Open) != 0 {
		t.Fatalf("side quest %+v %v", ps, err)
	}
}

// declareQuest writes a record as a declaration, as docs/QUESTS.md tells a
// host that rebuilds the world per call.
func declareQuest(q core.Quest) string {
	s := fmt.Sprintf("quest %q named %q {", q.ID, q.Name)
	for _, r := range q.Requires {
		s += fmt.Sprintf(" requires %q;", r)
	}
	if q.SideOf != "" {
		s += fmt.Sprintf(" side of %q;", q.SideOf)
	}
	for _, e := range q.Log {
		s += " " + string(e.Kind)
		if e.Kind == core.EntryReopened {
			s += fmt.Sprintf(" because %q", *e.Reason)
		}
		if len(e.Ahead) > 0 {
			s += " ahead"
			for _, a := range e.Ahead {
				s += fmt.Sprintf(" %q", a)
			}
			s += fmt.Sprintf(" because %q", *e.Reason)
		}
		if e.Text != nil {
			s += fmt.Sprintf(" impact %q", *e.Text)
		}
		if e.Tick != nil {
			s += fmt.Sprintf(" at %d", *e.Tick)
		}
		s += ";"
	}
	return s + " }"
}

// questModel is a quest as the model sees it.
type questModel struct {
	status   core.QuestStatus
	requires []core.QuestID
	log      []core.QuestEntry
}

func TestQuestsMatchModel(t *testing.T) {
	rng := rand.New(rand.NewSource(47))
	texts := []string{"a", "b", "c", " ", strings.Repeat("t", core.MaxQuestTextBytes+1)}
	pick := func() *string {
		switch rng.Intn(12) {
		case 0:
			return nil
		case 1:
			return text(texts[3+rng.Intn(2)])
		}
		return text(texts[rng.Intn(3)])
	}
	good := func(s *string) bool { return s == nil || *s != " " && len(*s) <= core.MaxQuestTextBytes }
	results := map[string]int{}
	for round := 0; round < 150; round++ {
		// A random acyclic set of requirements: each quest may require
		// quests with lower numbers.
		n := 2 + rng.Intn(6)
		src := ""
		model := map[core.QuestID]*questModel{}
		var ids []core.QuestID
		for i := 0; i < n; i++ {
			id := core.QuestID(fmt.Sprintf("quest:%d", i))
			ids = append(ids, id)
			m := &questModel{status: core.StatusUnstarted}
			src += fmt.Sprintf("quest %q named \"Q\" {", id)
			for j := 0; j < i; j++ {
				if rng.Intn(3) == 0 {
					r := core.QuestID(fmt.Sprintf("quest:%d", j))
					m.requires = append(m.requires, r)
					src += fmt.Sprintf(" requires %q;", r)
				}
			}
			if rng.Intn(4) == 0 {
				src += fmt.Sprintf(" side of \"quest:%d\";", (i+1+rng.Intn(n-1))%n)
			}
			// Some quests come with a long declared log, so logs fill.
			if rng.Intn(3) == 0 {
				m.status, m.log = core.StatusStarted, []core.QuestEntry{{Kind: core.EntryStarted}}
				src += " started;"
				for k := rng.Intn(core.MaxQuestLog); k > 0; k-- {
					e := core.QuestEntry{Kind: core.EntryNote, Text: text("n")}
					switch {
					case m.status == core.StatusCompleted:
						e = core.QuestEntry{Kind: core.EntryReopened, Reason: text("r")}
						m.status = core.StatusStarted
						src += ` reopened because "r";`
					case rng.Intn(2) == 0:
						e = core.QuestEntry{Kind: core.EntryCompleted}
						m.status = core.StatusCompleted
						src += " completed;"
					default:
						src += ` note impact "n";`
					}
					m.log = append(m.log, e)
				}
			}
			src += " }\n"
			model[id] = m
		}
		b := questFixture(t, src)
		w := world(t, b)
		clock := uint64(1440)
		for step := 0; step < 50; step++ {
			if rng.Intn(8) == 0 {
				execute(t, w, fmt.Sprint(round, "-", step, "-wait"), `advance time by 7;`)
				clock += 7
				continue
			}
			next := map[core.QuestID]*questModel{}
			for k, v := range model {
				c := *v
				c.log = append([]core.QuestEntry(nil), v.log...)
				next[k] = &c
			}
			var ops []core.QuestOp
			dropped := 0
			code, field := "", ""
			refuse := func(c, f string) {
				if code == "" {
					code, field = c, f
				}
			}
			// Each operation is checked, then applied, in turn.
			type pending struct {
				id core.QuestID
				op core.QuestOp
			}
			var todo []pending
			for k := 0; k < 1+rng.Intn(3); k++ {
				id := ids[rng.Intn(len(ids))]
				if rng.Intn(25) == 0 {
					id = "quest:none"
				}
				op := core.QuestOp{}
				verb := rng.Intn(5)
				if m := model[id]; m != nil && rng.Intn(3) > 0 {
					// Mostly an action the status allows.
					valid := map[core.QuestStatus][]int{
						core.StatusUnstarted: {0, 2, 3},
						core.StatusStarted:   {1, 1, 2, 3},
						core.StatusCompleted: {4},
						core.StatusFailed:    {4},
					}[m.status]
					verb = valid[rng.Intn(len(valid))]
				}
				switch verb {
				case 0:
					op.Start = &core.QuestStep{Quest: id, Text: pick()}
				case 1:
					op.Note = &core.QuestStep{Quest: id, Text: pick()}
				case 2:
					op.Complete = &core.CompleteQuest{Quest: id, Text: pick()}
					if rng.Intn(2) == 0 {
						op.Complete.Ahead = pick()
						if op.Complete.Ahead == nil {
							op.Complete.Ahead = text("why")
						}
					}
				case 3:
					op.Fail = &core.QuestStep{Quest: id, Text: pick()}
				default:
					op.Reopen = &core.ReopenQuest{Quest: id, Reason: pick()}
				}
				ops = append(ops, op)
				todo = append(todo, pending{id, op})
			}
			for _, p := range todo {
				op := p.op
				switch {
				case next[p.id] == nil:
					refuse("E_REFERENCE", "reference")
				case op.Note != nil && op.Note.Text == nil:
					refuse("E_QUEST", "text")
				case op.Reopen != nil && op.Reopen.Reason == nil:
					refuse("E_QUEST", "reason")
				case op.Complete != nil && !good(op.Complete.Ahead):
					refuse("E_QUEST", "reason")
				case op.Reopen != nil && !good(op.Reopen.Reason):
					refuse("E_QUEST", "reason")
				case op.Start != nil && !good(op.Start.Text), op.Note != nil && !good(op.Note.Text), op.Complete != nil && !good(op.Complete.Text), op.Fail != nil && !good(op.Fail.Text):
					refuse("E_QUEST", "text")
				}
				if code != "" {
					break
				}
				m := next[p.id]
				e := core.QuestEntry{Tick: tick(clock)}
				status := m.status
				switch {
				case op.Start != nil:
					switch m.status {
					case core.StatusStarted:
						refuse("E_NO_CHANGE", "operation")
					case core.StatusUnstarted:
						e.Kind, e.Text, status = core.EntryStarted, op.Start.Text, core.StatusStarted
					default:
						refuse("E_QUEST", "status")
					}
				case op.Note != nil:
					if m.status != core.StatusStarted {
						refuse("E_QUEST", "status")
					}
					e.Kind, e.Text = core.EntryNote, op.Note.Text
				case op.Complete != nil:
					var open []core.QuestID
					for _, r := range m.requires {
						if next[r].status != core.StatusCompleted {
							open = append(open, r)
						}
					}
					switch {
					case m.status == core.StatusCompleted:
						refuse("E_NO_CHANGE", "operation")
					case m.status == core.StatusFailed:
						refuse("E_QUEST", "status")
					case op.Complete.Ahead == nil && len(open) > 0:
						refuse("E_QUEST", "requires")
					case op.Complete.Ahead != nil && len(open) == 0:
						refuse("E_QUEST", "ahead")
					}
					e.Kind, e.Text, status = core.EntryCompleted, op.Complete.Text, core.StatusCompleted
					if op.Complete.Ahead != nil {
						e.Ahead, e.Reason = open, op.Complete.Ahead
					}
				case op.Fail != nil:
					switch m.status {
					case core.StatusFailed:
						refuse("E_NO_CHANGE", "operation")
					case core.StatusCompleted:
						refuse("E_QUEST", "status")
					}
					e.Kind, e.Text, status = core.EntryFailed, op.Fail.Text, core.StatusFailed
				default:
					if m.status != core.StatusCompleted && m.status != core.StatusFailed {
						refuse("E_QUEST", "status")
					}
					e.Kind, e.Reason, status = core.EntryReopened, op.Reopen.Reason, core.StatusStarted
				}
				if code != "" {
					break
				}
				if len(m.log) >= core.MaxQuestLog {
					at := -1
					for i, x := range m.log {
						if x.Kind == core.EntryNote {
							at = i
							break
						}
					}
					if at < 0 {
						refuse("E_QUEST", "log")
						break
					}
					m.log = append(m.log[:at], m.log[at+1:]...)
					dropped++
				}
				m.log, m.status = append(m.log, e), status
			}
			_, err := w.Execute(questRequest(w, fmt.Sprint(round, "-", step), ops...))
			if code == "" {
				if err != nil {
					t.Fatalf("round %d step %d: %v for %+v", round, step, err, ops)
				}
				model = next
				results["applied"]++
				results["dropped"] += dropped
			} else {
				var f *core.Fault
				if !errors.As(err, &f) || f.Code != code || f.Field != field {
					t.Fatalf("round %d step %d: got %v, want %s %s for %+v", round, step, err, code, field, ops)
				}
				results[code+" "+field]++
			}
			// Every quest, and the view, match the model.
			ps, err := w.QuestsView(ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range ps {
				m := model[p.ID]
				if p.Status != m.status || len(p.Log) != len(m.log) || len(m.log) > 0 && !reflect.DeepEqual(p.Log, m.log) {
					t.Fatalf("round %d step %d: %s\n%+v\nwant %s %+v", round, step, p.ID, p.Quest, m.status, m.log)
				}
				open := []core.QuestID{}
				for _, r := range m.requires {
					if model[r].status != core.StatusCompleted {
						open = append(open, r)
					}
				}
				ready := len(open) == 0 && (m.status == core.StatusUnstarted || m.status == core.StatusStarted)
				if !reflect.DeepEqual(p.Open, open) || p.Ready != ready {
					t.Fatalf("round %d step %d: %s open %v ready %v, want %v %v", round, step, p.ID, p.Open, p.Ready, open, ready)
				}
			}
		}
		costReplay(t, b, w)
		// A host that rebuilds the world writes the records back as
		// declarations, and gets the same quests.
		src = ""
		for _, q := range w.Snapshot().Quests {
			src += declareQuest(q) + "\n"
		}
		if back := questFixture(t, src).Quests; !reflect.DeepEqual(back, w.Snapshot().Quests) {
			t.Fatalf("round %d: written back\n%+v\nwant\n%+v", round, back, w.Snapshot().Quests)
		}
	}
	keys := make([]string, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %d", k, results[k])
	}
	for _, k := range []string{"applied", "dropped", "E_NO_CHANGE operation", "E_QUEST status", "E_QUEST requires", "E_QUEST ahead", "E_QUEST log", "E_QUEST text", "E_QUEST reason", "E_REFERENCE reference"} {
		if results[k] == 0 {
			t.Errorf("no %s", k)
		}
	}
}

// Requirement cycles, against a brute-force search on random graphs.
func TestQuestCyclesMatchBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(4747))
	cycles, acyclic := 0, 0
	for round := 0; round < 400; round++ {
		n := 1 + rng.Intn(7)
		edges := make([][]bool, n)
		src := ""
		for i := range edges {
			edges[i] = make([]bool, n)
			src += fmt.Sprintf("quest \"quest:%d\" named \"Q\" {", i)
			for j := 0; j < n; j++ {
				if i != j && rng.Intn(4) == 0 {
					edges[i][j] = true
					src += fmt.Sprintf(" requires \"quest:%d\";", j)
				}
			}
			src += " }\n"
		}
		// reach[i][j]: j can be reached from i by one or more requirements.
		reach := make([][]bool, n)
		for i := range reach {
			reach[i] = append([]bool(nil), edges[i]...)
		}
		for k := 0; k < n; k++ {
			for i := 0; i < n; i++ {
				for j := 0; j < n; j++ {
					reach[i][j] = reach[i][j] || reach[i][k] && reach[k][j]
				}
			}
		}
		cyclic := false
		for i := 0; i < n; i++ {
			cyclic = cyclic || reach[i][i]
		}
		_, ds := compiler.CompileWorld("cycles.nql", questPlaces+src)
		got := len(ds) == 1 && ds[0].Code == "E_QUEST" && ds[0].Expected == "no cycle"
		if got != cyclic || !cyclic && len(ds) > 0 {
			t.Fatalf("round %d: cyclic %v, diagnostics %v\n%s", round, cyclic, ds, src)
		}
		if cyclic {
			cycles++
		} else {
			acyclic++
		}
	}
	if cycles == 0 || acyclic == 0 {
		t.Fatalf("%d cyclic and %d acyclic graphs", cycles, acyclic)
	}
	t.Logf("%d cyclic, %d acyclic", cycles, acyclic)
}
