package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// Synthetic sizing control only; this does not substitute for measuring the
// reviewed real content pack and its host call shape.
func BenchmarkCatalogOneShot200(b *testing.B) {
	var world strings.Builder
	world.WriteString(`rules { transfer unequips; wear any; } location "camp" named "Camp"; character "a" named "A" at "camp" { resource "coins" = 20 min 0 max 100; }`)
	for n := 0; n < 200; n++ {
		fmt.Fprintf(&world, `item type "def:%d" named "Synthetic item %d" { description %q; kind "gear"; price "coins" 1 per 1; }`, n, n, strings.Repeat("Synthetic fixture text. ", 8))
	}
	raw, err := json.Marshal(map[string]any{"world": world.String(), "actions": `spend "a" resource "coins" by 1; create item "new" from "def:0" { owner "a"; quantity 1; };`, "request": "one", "actor": map[string]any{"kind": "character", "id": "a"}, "item_definitions": []string{"def:0"}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var out bytes.Buffer
		if code := run(bytes.NewReader(raw), &out); code != 0 {
			b.Fatal(out.String())
		}
	}
	b.ReportMetric(float64(len(raw)), "input-bytes")
}

func TestItemCatalogGuideExamples(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	guide := read("../../docs/ITEM_CATALOG.md")
	blocks := strings.Split(guide, "```nql\n")[1:]
	if len(blocks) != 2 {
		t.Fatal("guide NQL example count", len(blocks))
	}
	world := read("../../examples/item-catalog/world.nql")
	actions := read("../../examples/item-catalog/buy.nql")
	for n, expected := range []string{world, actions} {
		if strings.TrimSpace(strings.SplitN(blocks[n], "```", 2)[0]) != strings.TrimSpace(expected) {
			t.Fatal("guide/example drift", n)
		}
	}
	jsonBlocks := strings.Split(guide, "```json\n")[1:]
	if len(jsonBlocks) != 1 {
		t.Fatal("guide JSON example count", len(jsonBlocks))
	}
	var shown []core.ItemDefinitionProjection
	if err := json.Unmarshal([]byte(strings.SplitN(jsonBlocks[0], "```", 2)[0]), &shown); err != nil {
		t.Fatal(err)
	}
	actor := map[string]any{"kind": "character", "id": "char:ada"}
	call := map[string]any{"world": world, "actions": actions, "actor": actor, "request": "buy:1", "item_definitions": []string{"itemdef:torch"}, "items_at": []any{actor}, "include_state": true}
	_, first := invoke(t, call)
	if !first.OK || !reflect.DeepEqual(first.ItemDefinitions, shown) || first.Receipt == nil {
		t.Fatalf("guide %+v %s %v", first.Diagnostics, first.Error, first.Fault)
	}
	if len(first.Checkpoint.State.Items) != 1 || first.Checkpoint.State.Items[0].From != "itemdef:torch" || first.Checkpoint.State.Items[0].Name != "Reed torch" || *first.Checkpoint.State.Items[0].Quantity != 2 || first.Checkpoint.State.Characters[0].Resources["copper"].Current != 8 {
		t.Fatal(first.Checkpoint.State)
	}
	if len(first.ItemsAt) != 1 || len(first.ItemsAt[0].Items) != 1 || first.ItemsAt[0].Items[0].From != "itemdef:torch" {
		t.Fatal(first.ItemsAt)
	}
	_, readBack := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, first.Checkpoint), "item_definitions": []string{"itemdef:torch"}})
	if !readBack.OK || !reflect.DeepEqual(readBack.ItemDefinitions, shown) {
		t.Fatal("checkpoint definition view", readBack.Error)
	}
	delete(call, "world")
	call["checkpoint"] = checkpointDoc(t, first.Checkpoint)
	_, retry := invoke(t, call)
	if !retry.OK || !retry.Historical || !reflect.DeepEqual(retry.Receipt, first.Receipt) {
		t.Fatal("guide exact retry", retry.Error, retry.Fault, retry.Diagnostics)
	}
	// Sheet-style reconstruction retains identity and stock, without putting
	// inventory in a live-state document or repeating definition-owned text.
	sheet := strings.Replace(world, `= 10 min`, `= 8 min`, 1) + `item "item:torch-7" from "itemdef:torch" { owner "char:ada"; custody character "char:ada"; quantity 2; }`
	_, fresh := invoke(t, map[string]any{"world": sheet, "include_state": true, "item_definitions": []string{"itemdef:torch"}})
	if !fresh.OK || !reflect.DeepEqual(fresh.Checkpoint.State.Blueprint, first.Checkpoint.State.Blueprint) {
		t.Fatal("sheet reconstruction", fresh.Error, fresh.Diagnostics)
	}
	_, missing := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, first.Checkpoint), "item_definitions": []string{"itemdef:torch", "missing"}})
	if missing.OK || missing.Phase != "item_definitions" || missing.Fault == nil || missing.Fault.Code != "E_CATALOG" {
		t.Fatal("missing definition", missing.Error, missing.Fault)
	}
	call["item_definitions"] = []string{"itemdef:torch", "missing"}
	_, committedViewFailure := invoke(t, call)
	if !committedViewFailure.OK || !committedViewFailure.Historical || committedViewFailure.ViewError == nil || committedViewFailure.ViewError.View != "item_definitions" || len(committedViewFailure.ItemDefinitions) != 1 || committedViewFailure.Checkpoint == nil {
		t.Fatal("view failure hid receipt", committedViewFailure)
	}
	_, failed := invoke(t, map[string]any{"world": strings.Replace(world, "= 10 min", "= 1 min", 1), "actions": actions, "actor": actor, "request": "too-poor", "include_state": true})
	if failed.OK || failed.Phase != "execute" || failed.Revision != 0 || failed.Receipt != nil || failed.Checkpoint != nil {
		t.Fatal("failed purchase", failed.Error)
	}
}

func TestItemCatalogLiveStateRetryAfterDefinitionEdit(t *testing.T) {
	_, initial := invoke(t, map[string]any{"world": retryWorld, "new_live_state": true})
	if !initial.OK {
		t.Fatal(initial.Error, initial.Diagnostics)
	}
	actions := `create occupant "occ:peddler" named "Peddler" at "loc:camp" { person; }; create item "item:torch" from "itemdef:torch" { owner "char:a"; quantity 2; };`
	call := map[string]any{"world": retryWorld, "live_state": liveDoc(t, initial.LiveState), "actions": actions, "actor": liveActor, "request": "catalog:live", "include_state": true}
	_, first := invoke(t, call)
	if !first.OK || first.Receipt == nil || len(first.LiveState.Requests) != 1 {
		t.Fatal(first.Error, first.Diagnostics, first.Fault)
	}
	if first.Receipt.Events[1].Item.Name != "Torch" || first.Receipt.Events[1].ItemIntent.Name != "" {
		t.Fatal(first.Receipt)
	}
	// Write only instance facts, then edit authored display data between fresh
	// worlds. The request digest must remain the same original typed intent.
	call["world"] = strings.Replace(retryWorld, `named "Torch" { kind "gear"; }`, `named "Reed torch" { kind "gear"; }`, 1) + `item "item:torch" from "itemdef:torch" { owner "char:a"; quantity 2; }`
	call["live_state"] = liveDoc(t, first.LiveState)
	_, retry := invoke(t, call)
	if !retry.OK || !retry.Historical || retry.Receipt != nil || !reflect.DeepEqual(first.LiveState, retry.LiveState) {
		t.Fatal("definition edit changed retry identity", retry.Error, retry.Diagnostics, retry.Fault)
	}
	if len(retry.Checkpoint.State.Items) != 2 || retry.Checkpoint.State.Items[1].Name != "Reed torch" || *retry.Checkpoint.State.Items[1].Quantity != 2 {
		t.Fatal("retry repeated acquisition or lost derivation", retry.Checkpoint.State.Items)
	}
}
