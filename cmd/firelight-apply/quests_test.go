package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// The quest guide's view comes from its example files, on a kept checkpoint.
func TestQuestGuideExample(t *testing.T) {
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	blocks := strings.Split(read("../../docs/QUESTS.md"), "```json\n")[1:]
	if len(blocks) != 1 {
		t.Fatalf("%d examples in the guide", len(blocks))
	}
	var shown any
	if err := json.Unmarshal([]byte(strings.SplitN(blocks[0], "```", 2)[0]), &shown); err != nil {
		t.Fatal(err)
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
	actor := map[string]any{"kind": "character", "id": "char:ada"}
	quests := []string{"quest:valley/tower", "quest:valley/mill"}
	world := read("../../examples/quests/world.nql")
	first := call(map[string]any{"world": world, "actions": read("../../examples/quests/start.nql"), "actor": actor, "request": "turn:1"})
	second := call(map[string]any{"checkpoint": first["checkpoint"], "actions": read("../../examples/quests/ahead.nql"), "actor": actor, "request": "turn:2", "quests": quests})
	if !reflect.DeepEqual(second["quests"], shown) {
		t.Fatalf("after ahead.nql:\nengine %v\nguide  %v", second["quests"], shown)
	}
	// The next call replays both requests from the checkpoint.
	third := call(map[string]any{"checkpoint": second["checkpoint"], "quests": quests})
	if !reflect.DeepEqual(third["quests"], shown) {
		t.Fatalf("from the checkpoint:\nengine %v\nguide  %v", third["quests"], shown)
	}
}

// A host reads quests, changes them by typed actions, and gets refusals that
// say why.
func TestQuestsViewAndActions(t *testing.T) {
	raw, err := os.ReadFile("../../examples/quests/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	world := string(raw)
	actor := map[string]any{"kind": "character", "id": "char:ada"}
	_, r := invoke(t, map[string]any{"world": world, "actions": `complete quest "quest:valley/tower";`, "actor": actor, "request": "host:early"})
	if r.OK || r.Phase != "execute" || r.Fault == nil || r.Fault.Code != "E_QUEST" || r.Fault.Field != "requires" || r.Fault.Actual != "quest:valley/mill" || r.Checkpoint != nil {
		t.Fatalf("early: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
	_, r = invoke(t, map[string]any{"world": world, "actions": `start quest "quest:valley/nowhere";`, "actor": actor, "request": "host:typo"})
	if r.OK || r.Phase != "compile" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "E_REFERENCE" {
		t.Fatalf("typo: ok %v phase %s diagnostics %+v", r.OK, r.Phase, r.Diagnostics)
	}

	// In order, in one request: the mill, then the tower, then a note on the
	// side quest.
	_, r = invoke(t, map[string]any{"world": world, "actions": `complete quest "quest:valley/mill" impact "the goblins are gone"; complete quest "quest:valley/tower"; note quest "quest:valley/herbs" impact "found feverfew";`, "actor": actor, "request": "host:done", "quests": []string{"quest:valley/tower", "quest:valley/herbs"}})
	if !r.OK || r.Phase != "executed" || r.Revision != 1 || len(r.Quests) != 2 {
		t.Fatalf("done: ok %v phase %s error %q fault %+v", r.OK, r.Phase, r.Error, r.Fault)
	}
	kinds := []core.EventKind{}
	for _, e := range r.Receipt.Events {
		kinds = append(kinds, e.Kind)
	}
	if want := []core.EventKind{core.QuestCompleted, core.QuestCompleted, core.QuestNoted}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("events %v, want %v", kinds, want)
	}
	if tower, herbs := r.Quests[0], r.Quests[1]; tower.Status != core.StatusCompleted || tower.Ready || len(tower.Open) != 0 || herbs.Status != core.StatusStarted || !herbs.Ready || len(herbs.Log) != 2 {
		t.Fatalf("quests %+v", r.Quests)
	}

	// An unknown quest fails a read, and after a commit only the view.
	cp := checkpointDoc(t, r.Checkpoint)
	_, v := invoke(t, map[string]any{"checkpoint": cp, "quests": []string{"quest:valley/nowhere"}})
	if v.OK || v.Phase != "quests" || v.Fault == nil || v.Fault.Code != "E_REFERENCE" {
		t.Fatalf("read: ok %v phase %s fault %+v", v.OK, v.Phase, v.Fault)
	}
	_, v = invoke(t, map[string]any{"checkpoint": cp, "actions": `reopen quest "quest:valley/mill" because "more goblins came";`, "actor": actor, "request": "host:again", "quests": []string{"quest:valley/mill", "quest:valley/nowhere"}})
	if !v.OK || v.Revision != 2 || v.ViewError == nil || v.ViewError.View != "quests" || len(v.Quests) != 1 || v.Quests[0].Status != core.StatusStarted || v.Quests[0].Revision != 2 {
		t.Fatalf("commit with a failing view: ok %v view error %+v quests %+v", v.OK, v.ViewError, v.Quests)
	}
	// The tower's requirement is open again; the tower stays completed.
	_, v = invoke(t, map[string]any{"checkpoint": checkpointDoc(t, v.Checkpoint), "quests": []string{"quest:valley/tower"}})
	if !v.OK || v.Quests[0].Status != core.StatusCompleted || !reflect.DeepEqual(v.Quests[0].Open, []core.QuestID{"quest:valley/mill"}) {
		t.Fatalf("tower %+v", v.Quests)
	}

	// A retry of a committed request is answered from history, unchanged.
	_, again := invoke(t, map[string]any{"checkpoint": cp, "actions": `complete quest "quest:valley/mill" impact "the goblins are gone"; complete quest "quest:valley/tower"; note quest "quest:valley/herbs" impact "found feverfew";`, "actor": actor, "request": "host:done"})
	if !again.OK || !again.Historical || again.Revision != 1 || !reflect.DeepEqual(again.Receipt, r.Receipt) {
		t.Fatalf("retry: ok %v historical %v", again.OK, again.Historical)
	}
}
