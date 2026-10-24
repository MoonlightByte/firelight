package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func TestItemChargesGuideExamples(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	guide := read("../../docs/ITEM_CHARGES.md")
	blocks := strings.Split(guide, "```nql\n")[1:]
	if len(blocks) != 4 {
		t.Fatal("guide NQL example count", len(blocks))
	}
	world := read("../../examples/item-charges/world.nql")
	actions := read("../../examples/item-charges/use.nql")
	staffWorld := read("../../examples/item-charges/recharge.nql")
	staffActions := read("../../examples/item-charges/spend-staff.nql")
	for n, expected := range []string{world, actions, staffWorld, staffActions} {
		if strings.TrimSpace(strings.SplitN(blocks[n], "```", 2)[0]) != strings.TrimSpace(expected) {
			t.Fatal("guide/example drift", n)
		}
	}
	jsonBlocks := strings.Split(guide, "```json\n")[1:]
	if len(jsonBlocks) != 2 {
		t.Fatal("guide JSON example count", len(jsonBlocks))
	}
	var shown []core.ItemChargesProjection
	if err := json.Unmarshal([]byte(strings.SplitN(jsonBlocks[0], "```", 2)[0]), &shown); err != nil {
		t.Fatal(err)
	}
	actor := map[string]any{"kind": "character", "id": "char:ada"}
	call := map[string]any{"world": world, "actions": actions, "actor": actor, "request": "use:1", "item_charges": []string{"item:wand-1"}, "include_state": true}
	_, first := invoke(t, call)
	if !first.OK || !reflect.DeepEqual(first.ItemCharges, shown) || first.Receipt == nil {
		t.Fatalf("guide %+v %s %v", first.Diagnostics, first.Error, first.Fault)
	}
	if e := first.Receipt.Events; len(e) != 1 || e[0].Kind != core.ChargesExpended || *e[0].Charges != (core.ChargeExpenditure{Item: "item:wand-1", Owner: "char:ada", Requested: 2, Before: 3, After: 1}) {
		t.Fatal(first.Receipt.Events)
	}
	delete(call, "world")
	call["checkpoint"] = checkpointDoc(t, first.Checkpoint)
	_, retry := invoke(t, call)
	if !retry.OK || !retry.Historical || !reflect.DeepEqual(retry.Receipt, first.Receipt) || retry.ItemCharges[0].Current != 1 {
		t.Fatal("guide exact retry", retry.Error, retry.Fault, retry.Diagnostics)
	}
	// Sheet-style reconstruction: the host declares back what the result said.
	sheet := strings.Replace(world, "charges 3 of 7;", "charges 1 of 7;", 1)
	_, fresh := invoke(t, map[string]any{"world": sheet, "include_state": true, "item_charges": []string{"item:wand-1"}})
	if !fresh.OK || !reflect.DeepEqual(fresh.Checkpoint.State.Blueprint, first.Checkpoint.State.Blueprint) || !reflect.DeepEqual(fresh.ItemCharges[0].Available, shown[0].Available) {
		t.Fatal("sheet reconstruction", fresh.Error, fresh.Diagnostics)
	}
	_, refused := invoke(t, map[string]any{"world": sheet, "actions": actions, "actor": actor, "request": "use:2", "include_state": true})
	if refused.OK || refused.Phase != "execute" || refused.Fault == nil || refused.Fault.Code != "E_CHARGES" || refused.Receipt != nil || refused.Checkpoint != nil {
		t.Fatal("insufficient charges", refused.Error, refused.Fault)
	}
	_, missing := invoke(t, map[string]any{"world": sheet, "item_charges": []string{"item:wand-1", "char:ada"}})
	if missing.OK || missing.Phase != "item_charges" || missing.Fault == nil || missing.Fault.Code != "E_CHARGES" {
		t.Fatal("unknown charged item", missing.Error, missing.Fault)
	}
	var staffShown []core.ItemChargesProjection
	if err := json.Unmarshal([]byte(strings.SplitN(jsonBlocks[1], "```", 2)[0]), &staffShown); err != nil {
		t.Fatal(err)
	}
	_, staff := invoke(t, map[string]any{"world": staffWorld, "actions": staffActions, "actor": actor, "request": "use:1", "item_charges": []string{"item:staff-1"}})
	asOf := uint64(120000)
	if !staff.OK || !reflect.DeepEqual(staff.ItemCharges, staffShown) || len(staff.Receipt.Events) != 1 ||
		!reflect.DeepEqual(*staff.Receipt.Events[0].Charges, core.ChargeExpenditure{Item: "item:staff-1", Owner: "char:ada", Requested: 3, Before: 6, After: 3, Clock: core.ChargeClockCurrent, AsOf: &asOf}) {
		t.Fatalf("staff guide %+v %s %v", staff.ItemCharges, staff.Error, staff.Fault)
	}
	call["item_charges"] = []string{"item:wand-1", "missing"}
	_, committedViewFailure := invoke(t, call)
	if !committedViewFailure.OK || !committedViewFailure.Historical || committedViewFailure.ViewError == nil || committedViewFailure.ViewError.View != "item_charges" || len(committedViewFailure.ItemCharges) != 1 {
		t.Fatal("view failure hid receipt", committedViewFailure)
	}
}
