package core_test

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestCreatedEquipmentUsesExistingEvaluatorAndClock(t *testing.T) {
	b := clockFixture()
	w := gearWorld(t, b)
	i := &core.Item{ID: "new-chain", Name: "Chain shirt", Description: "Individual blue trim", Owner: "a", Quantity: gearPtr(int64(1)), Equipment: &core.EquipmentInstance{Definition: "chain-shirt"}}
	// Birth alone cannot bypass occupied slots or donning time. Late failure
	// rolls back the provisional birth and all earlier clock changes.
	gearReject(t, w, "occupied", core.Operation{CreateItem: i}, wear(i.ID, "a", ""))
	gearReject(t, w, "worn-birth", core.Operation{CreateItem: &core.Item{ID: "bad", Owner: "a", Wearer: "a", Equipment: &core.EquipmentInstance{Definition: "chain-shirt", Mode: "worn"}}})
	r := gearRequest(w, "birth-load", core.Operation{CreateItem: i}, unwear("leather"), wear(i.ID, "a", ""))
	original := core.CanonicalRequest(r)
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	want := []core.EventKind{core.ItemCreated, core.TimeAdvanced, core.ItemUnequipped, core.TimeAdvanced, core.ItemEquipped}
	if len(receipt.Events) != len(want) {
		t.Fatal(receipt.Events)
	}
	for j, k := range want {
		if receipt.Events[j].Kind != k {
			t.Fatal(j, receipt.Events[j])
		}
	}
	if w.Snapshot().Clock.Tick != 360 {
		t.Fatal("creation bypassed equipment duration")
	}
	gearDefense(t, w, "a", 17)
	if receipt.Events[0].Item.Equipment.Mode != "" || receipt.Events[0].Item.Wearer != "" {
		t.Fatal("birth changed to postimage")
	}
	i.Equipment.Definition = "shield"
	*i.Quantity = 8
	receipt.Events[0].Item.Equipment.Mode = "caller"
	receipt.Request.Operations[0].CreateItem.Equipment.Definition = "caller"
	snap := w.Snapshot()
	snap.Items[len(snap.Items)-1].Equipment.Definition = "caller"
	view, e := w.EquipmentView("a")
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range view.Items {
		if x.Item.ID == "new-chain" {
			x.Item.Equipment.Definition = "caller"
			*x.Item.Quantity = 88
		}
	}
	history := w.History()
	history[0].Events[0].Item.Equipment.Definition = "caller"
	cp := w.ExportCheckpoint()
	cp.History[0].Request.Operations[0].CreateItem.Equipment.Mode = "caller"
	fixed, ok := w.Receipt("birth-load")
	if !ok || !reflect.DeepEqual(fixed.Request, original) || fixed.Events[0].Item.Equipment.Definition != "chain-shirt" || fixed.Events[0].Item.Equipment.Mode != "" {
		t.Fatal("nested birth alias")
	}
	gearDefense(t, w, "a", 17)
	rebuilt, e := core.Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
		t.Fatal(e)
	}
	retry, e := rebuilt.Execute(original)
	if e != nil || !reflect.DeepEqual(retry, fixed) || rebuilt.Snapshot().Clock.Tick != 360 {
		t.Fatal("retry redonned", e)
	}
	// Weapon grants also come from existing authored definitions, never text.
	gearExecute(t, w, "weapon", core.Operation{CreateItem: &core.Item{ID: "new-sword", Name: "Sword", Owner: "b", Equipment: &core.EquipmentInstance{Definition: "longsword"}}}, wear("new-sword", "b", "two"))
	gearAttack(t, w, "b", 3, 10, 1)
}

func TestCreatedItemsKeepNativeCountIncludingDepletedIdentities(t *testing.T) {
	b := clockFixture()
	b.Items = append(b.Items, core.Item{ID: "depleted", Quantity: gearPtr(int64(0))})
	for len(b.Items) < core.MaxWorldItems-1 {
		b.Items = append(b.Items, core.Item{ID: core.ItemID(fmt.Sprint(len(b.Items)))})
	}
	w := gearWorld(t, b)
	gearExecute(t, w, "last-slot", core.Operation{CreateItem: &core.Item{ID: "last", Owner: "a"}})
	if len(w.Snapshot().Items) != core.MaxWorldItems {
		t.Fatal("wrong capacity")
	}
	err := battleReject(t, w, gearRequest(w, "over-capacity", core.Operation{CreateItem: &core.Item{ID: "overflow", Owner: "a"}}))
	var f *core.Fault
	if !errors.As(err, &f) || f.Field != "capacity" {
		t.Fatal("expected item capacity", err)
	}
	gearReject(t, w, "recycled", core.Operation{CreateItem: &core.Item{ID: "depleted", Owner: "a"}})
	// Rejection did not reserve this request ID; an existing operation continues.
	gearExecute(t, w, "over-capacity", unwear("shield"))
}
