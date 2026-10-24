package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestCatalogEquipmentDerivationAndPaymentAtomicity(t *testing.T) {
	b := clockFixture()
	b.ItemDefinitions = []core.ItemDefinition{{ID: "chain-shirt", Name: "Chain shirt", Description: "Linked metal rings.", Kind: "armor", Equipment: "chain-shirt", Price: &core.ItemPrice{Unit: "focus", Amount: 2, UnitsPerLot: 1}}}
	i := &core.Item{ID: "catalog-chain", From: "chain-shirt", Owner: "a", Quantity: gearPtr(int64(1))}
	w := gearWorld(t, b)
	gearReject(t, w, "occupied", core.Operation{CreateItem: i}, wear(i.ID, "a", ""))
	gearReject(t, w, "conflict", core.Operation{CreateItem: &core.Item{ID: "bad", From: "chain-shirt", Equipment: &core.EquipmentInstance{Definition: "shield"}}})
	gearReject(t, w, "mode-at-birth", core.Operation{CreateItem: &core.Item{ID: "bad", From: "chain-shirt", Equipment: &core.EquipmentInstance{Mode: "worn"}}})
	// Reuse a real Resource, without defining a price evaluator.
	b.Characters[0].Resources["coins"] = core.Resource{Current: 2, Minimum: 0, Maximum: 10}
	w = gearWorld(t, b)
	pay := core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "coins", Amount: 2}}
	gearReject(t, w, "paid-but-occupied", pay, core.Operation{CreateItem: i}, wear(i.ID, "a", ""))
	gearReject(t, w, "insufficient", core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "coins", Amount: 3}}, core.Operation{CreateItem: i})
	r := gearRequest(w, "buy", pay, core.Operation{CreateItem: i}, unwear("leather"), wear(i.ID, "a", ""))
	original := core.CanonicalRequest(r)
	receipt, err := w.Execute(r)
	if err != nil {
		t.Fatal(err)
	}
	gearDefense(t, w, "a", 17)
	if w.Snapshot().Clock.Tick != 360 || w.Snapshot().Characters[0].Resources["coins"].Current != 0 {
		t.Fatal("existing mechanics changed")
	}
	birth := receipt.Events[1]
	if birth.Item.Equipment.Definition != "chain-shirt" || birth.Item.Equipment.Mode != "" || birth.ItemIntent.Equipment != nil || !reflect.DeepEqual(receipt.Request, original) {
		t.Fatal(birth)
	}
	loaded, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.ExportCheckpoint()) {
		t.Fatal(err)
	}
	for _, equal := range []bool{false, true} {
		declared := w.Snapshot().Blueprint
		for n := range declared.Items {
			v := &declared.Items[n]
			if v.ID != i.ID {
				continue
			}
			if !equal {
				v.Name, v.Description, v.Equipment.Definition = "", "", ""
			}
		}
		worn := gearWorld(t, declared)
		gearDefense(t, worn, "a", 17)
		if !reflect.DeepEqual(w.Snapshot().Blueprint, worn.Snapshot().Blueprint) {
			t.Fatal("worn source/snapshot round-trip")
		}
	}
}
