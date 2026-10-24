package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func catalogFixture(t *testing.T) Blueprint {
	t.Helper()
	_, b := inventoryWorld(t, inventoryInt(3))
	b.ItemDefinitions = []ItemDefinition{
		{ID: "torch", Name: "Torch", Description: "A bundle of dry reeds.", Kind: "gear", Subtype: "light", Consumable: true, Price: &ItemPrice{Unit: "copper", Amount: 1, UnitsPerLot: 1}},
		{ID: "a", Name: "Torch", Kind: "gear"}, // independent namespace and duplicate name
	}
	return b
}

func catalogWorld(t *testing.T, b Blueprint) *World {
	t.Helper()
	w, err := NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestCatalogDerivationIntentRetryReplayAndIsolation(t *testing.T) {
	b := catalogFixture(t)
	b.Items = append(b.Items, Item{ID: "declared", From: "torch", Owner: "a", Quantity: inventoryInt(0)})
	originalBytes, _ := json.Marshal(b)
	if err := Validate(b); err != nil {
		t.Fatal(err)
	}
	w := catalogWorld(t, b)
	afterBytes, _ := json.Marshal(b)
	if string(originalBytes) != string(afterBytes) {
		t.Fatal("validation/construction changed input")
	}
	initial := w.Snapshot()
	if d := item(&initial.Blueprint, "declared"); d.Name != "Torch" || d.Description != b.ItemDefinitions[0].Description || *d.Quantity != 0 {
		t.Fatal(d)
	}
	i := &Item{ID: "new", From: "torch", Owner: "a", Quantity: inventoryInt(4), Custody: &Ref{CharacterKind, "a"}}
	r := inventoryRequest("catalog-birth", 0, Operation{CreateItem: i})
	original := CanonicalRequest(r)
	receipt, err := w.Execute(r)
	if err != nil {
		t.Fatal(err)
	}
	e := receipt.Events[0]
	if !reflect.DeepEqual(receipt.Request, original) || !reflect.DeepEqual(e.ItemIntent, original.Operations[0].CreateItem) || e.Item.Name != "Torch" || e.Item.Description != "A bundle of dry reeds." {
		t.Fatal(receipt)
	}
	if i.Name != "" || i.Description != "" || i.Equipment != nil {
		t.Fatal("expanded caller intent")
	}
	b.ItemDefinitions[0].Name = "caller"
	b.ItemDefinitions[0].Price.Amount = 100
	*i.Quantity = 100
	e.ItemIntent.Custody.ID = "caller"
	*e.ItemIntent.Quantity = 100
	e.Item.Name = "caller"
	view, err := w.ItemDefinitionsView([]ItemDefinitionID{"torch", "a", "torch"})
	if err != nil || len(view) != 3 || view[0].Revision != 1 || view[0].ID != "torch" || view[1].ID != "a" || view[0].Price.Amount != 1 {
		t.Fatal(view, err)
	}
	view[0].Price.Amount = 200
	s := w.Snapshot()
	s.ItemDefinitions[1].Price.Amount = 300
	cp := w.ExportCheckpoint()
	cp.Genesis.ItemDefinitions[1].Price.Amount = 400
	cp.State.ItemDefinitions[1].Price.Amount = 500
	cp.History[0].Events[0].ItemIntent.From = "a"
	held := w.ExportCheckpoint()
	if held.State.ItemDefinitions[1].Price.Amount != 1 || held.History[0].Events[0].ItemIntent.From != "torch" || *held.History[0].Events[0].ItemIntent.Quantity != 4 {
		t.Fatal("alias")
	}
	rebuilt, err := Replay(held.Genesis, w.History())
	if err != nil || !reflect.DeepEqual(held, rebuilt.ExportCheckpoint()) {
		t.Fatal("replay", err)
	}
	for _, v := range []*World{w, rebuilt} {
		again, err := v.Execute(original)
		if err != nil || !reflect.DeepEqual(again, held.History[0]) || !reflect.DeepEqual(held, v.ExportCheckpoint()) {
			t.Fatal("retry", err)
		}
		changed := CanonicalRequest(original)
		changed.Operations[0].CreateItem.Name = "Torch"
		creationReject(t, v, changed, "E_REQUEST_REUSE")
	}
	for _, change := range []func(*Event){
		func(e *Event) { e.ItemIntent = nil },
		func(e *Event) { e.ItemIntent.Name = "Torch" },
		func(e *Event) { e.ItemIntent.From = "a" },
		func(e *Event) { e.Item.Name = "wrong" },
		func(e *Event) { e.Item.From = "a" },
		func(e *Event) { e.ItemIntent.Quantity = nil },
	} {
		h := w.History()
		change(&h[0].Events[0])
		if got, err := Replay(held.Genesis, h); err == nil || got != nil {
			t.Fatal("tampered catalog birth accepted")
		}
	}
	if _, err := NewWorld(held.State.Blueprint); err != nil {
		t.Fatal("resolved snapshot rejected", err)
	}
	partial, err := w.ItemDefinitionsView([]ItemDefinitionID{"a", "missing", "torch"})
	if len(partial) != 1 || err == nil {
		t.Fatal(partial, err)
	}
	if !reflect.DeepEqual(err, &Fault{Code: "E_CATALOG", Subject: "missing", Field: "id", Expected: "declared item definition", Actual: "missing", Message: "invalid item catalog id"}) {
		t.Fatal("lookup fault names the queried id", err)
	}
}

func TestCatalogExactRefusalsAndRollback(t *testing.T) {
	b := catalogFixture(t)
	for _, tc := range []struct {
		field, expected, actual string
		change                  func(*Item)
	}{
		{"from", "declared item definition", "missing", func(i *Item) { i.From = "missing" }},
		{"name", "Torch", "Lantern", func(i *Item) { i.Name = "Lantern" }},
		{"description", "A bundle of dry reeds.", "changed", func(i *Item) { i.Description = "changed" }},
		{"equipment", "", "other", func(i *Item) { i.Equipment = &EquipmentInstance{Definition: "other"} }},
		{"equipment", "absent for this definition", "present", func(i *Item) { i.Equipment = &EquipmentInstance{} }},
	} {
		t.Run(tc.field+tc.actual, func(t *testing.T) {
			i := Item{ID: "new", From: "torch", Owner: "a"}
			tc.change(&i)
			w := catalogWorld(t, b)
			before := w.ExportCheckpoint()
			_, err := w.Execute(inventoryRequest("reject", 0, Operation{CreateItem: &i}))
			want := &Fault{Code: "E_CATALOG", Subject: "new", Field: tc.field, Expected: tc.expected, Actual: tc.actual, Message: "invalid item catalog " + tc.field}
			var got *Fault
			if !errors.As(err, &got) || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal(got, want)
			}
			b := catalogFixture(t)
			b.Items = append(b.Items, i)
			if _, err := NewWorld(b); err == nil {
				t.Fatal("invalid genesis accepted")
			}
		})
	}
	w := catalogWorld(t, b)
	creationReject(t, w, inventoryRequest("rollback", 0, Operation{CreateItem: &Item{ID: "new", From: "torch", Owner: "a"}}, Operation{Consume: &ConsumeItem{Item: "new", Owner: "a", Amount: 2}}), "E_QUANTITY")
	if _, err := w.Execute(inventoryRequest("rollback", 0, Operation{CreateItem: &Item{ID: "new", From: "torch", Name: "Torch", Description: "A bundle of dry reeds.", Owner: "a"}})); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogStockCustodyAndLegacyBytes(t *testing.T) {
	b := catalogFixture(t)
	b.Items = append(b.Items, Item{ID: "torches", From: "torch", Owner: "a", Quantity: inventoryInt(5), Custody: &Ref{CharacterKind, "a"}})
	w := catalogWorld(t, b)
	ops := []Operation{
		{Split: &SplitItem{Item: "torches", Created: "part", Amount: 2, Handler: "a"}},
		{Transfer: &Transfer{Item: "part", From: "a", To: "b"}},
		{Place: &PlaceItem{Item: "part", Handler: "a", From: Ref{CharacterKind, "a"}, To: Ref{CharacterKind, "b"}}},
		{Consume: &ConsumeItem{Item: "part", Owner: "b", Amount: 2}},
		{Add: &AddItem{Item: "part", Owner: "b", Amount: 1}},
	}
	if _, err := w.Execute(inventoryRequest("stock", 0, ops...)); err != nil {
		t.Fatal(err)
	}
	s := w.Snapshot()
	i := item(&s.Blueprint, "part")
	if i.From != "torch" || i.SplitFrom != "torches" || i.Name != "Torch" || *i.Quantity != 1 || i.Owner != "b" || *i.Custody != (Ref{CharacterKind, "b"}) {
		t.Fatal(i)
	}
	if _, err := Replay(b, w.History()); err != nil {
		t.Fatal(err)
	}
	legacy, _ := inventoryWorld(t, nil)
	r, err := legacy.Execute(inventoryRequest("legacy", 0, Operation{CreateItem: &Item{ID: "new", Name: "Handmade"}}))
	if err != nil || r.Events[0].ItemIntent != nil {
		t.Fatal(r, err)
	}
	raw, _ := json.Marshal(legacy.ExportCheckpoint())
	for _, marker := range []string{"item_definitions", "item_intent", `"from":`} {
		if strings.Contains(string(raw), marker) {
			t.Fatal("unused field changed legacy encoding", marker)
		}
	}
}

func TestCatalogDefinitionBounds(t *testing.T) {
	for _, tc := range []struct {
		field  string
		change func(*ItemDefinition)
	}{
		{"id", func(d *ItemDefinition) { d.ID = "" }},
		{"id", func(d *ItemDefinition) { d.ID = ItemDefinitionID(strings.Repeat("i", 257)) }},
		{"id", func(d *ItemDefinition) { d.ID = "bad\n" }},
		{"name", func(d *ItemDefinition) { d.Name = " " }},
		{"name", func(d *ItemDefinition) { d.Name = strings.Repeat("n", 4097) }},
		{"name", func(d *ItemDefinition) { d.Name = string([]byte{255}) }},
		{"description", func(d *ItemDefinition) { d.Description = strings.Repeat("d", 4097) }},
		{"description", func(d *ItemDefinition) { d.Description = string([]byte{255}) }},
		{"kind", func(d *ItemDefinition) { d.Kind = "" }},
		{"kind", func(d *ItemDefinition) { d.Kind = strings.Repeat("k", 257) }},
		{"subtype", func(d *ItemDefinition) { d.Subtype = "bad\n" }},
		{"subtype", func(d *ItemDefinition) { d.Subtype = strings.Repeat("s", 257) }},
		{"equipment", func(d *ItemDefinition) { d.Equipment = "missing" }},
		{"price.unit", func(d *ItemDefinition) { d.Price.Unit = "" }},
		{"price.unit", func(d *ItemDefinition) { d.Price.Unit = strings.Repeat("u", 257) }},
		{"price.amount", func(d *ItemDefinition) { d.Price.Amount = -1 }},
		{"price.units_per_lot", func(d *ItemDefinition) { d.Price.UnitsPerLot = 0 }},
		{"price.units_per_lot", func(d *ItemDefinition) { d.Price.UnitsPerLot = -1 }},
	} {
		b := catalogFixture(t)
		tc.change(&b.ItemDefinitions[0])
		var f *Fault
		if err := Validate(b); !errors.As(err, &f) || f.Code != "E_CATALOG" || f.Field != tc.field {
			t.Fatalf("%s: %v", tc.field, err)
		}
	}
	b := catalogFixture(t)
	b.ItemDefinitions = append(b.ItemDefinitions, b.ItemDefinitions[0])
	if err := Validate(b); err == nil {
		t.Fatal("duplicate definition")
	}
	b = catalogFixture(t)
	d := &b.ItemDefinitions[0]
	d.ID = ItemDefinitionID(strings.Repeat("i", 256))
	d.Name, d.Description = strings.Repeat("n", 4096), strings.Repeat("d", 4096)
	d.Kind, d.Subtype, d.Price.Unit = strings.Repeat("k", 256), strings.Repeat("s", 256), strings.Repeat("u", 256)
	d.Price.Amount, d.Price.UnitsPerLot = math.MaxInt64, math.MaxInt64
	if err := Validate(b); err != nil {
		t.Fatal("boundary", err)
	}
	d.Price.Amount = 0
	if err := Validate(b); err != nil {
		t.Fatal("free price", err)
	}
	b = catalogFixture(t)
	for len(b.ItemDefinitions) < MaxWorldItemDefinitions {
		b.ItemDefinitions = append(b.ItemDefinitions, ItemDefinition{ID: ItemDefinitionID(fmt.Sprint(len(b.ItemDefinitions))), Name: "Supply", Kind: "gear"})
	}
	if _, err := NewWorld(b); err != nil {
		t.Fatal("definition cap", err)
	}
	b.ItemDefinitions = append(b.ItemDefinitions, ItemDefinition{ID: "overflow", Name: "Supply", Kind: "gear"})
	if err := Validate(b); err == nil {
		t.Fatal("definition cap exceeded")
	}
	b = catalogFixture(t)
	for len(b.Items) < MaxWorldItems {
		b.Items = append(b.Items, Item{ID: ItemID(fmt.Sprint(len(b.Items))), From: "torch", Quantity: inventoryInt(0)})
	}
	w := catalogWorld(t, b)
	creationReject(t, w, inventoryRequest("capacity", 0, Operation{CreateItem: &Item{ID: "one-more", From: "torch"}}), "E_CATALOG")
}

func TestCatalogArchiveAdmissionAndAllEntityCaps(t *testing.T) {
	for _, change := range []func(*Blueprint){
		func(b *Blueprint) { b.Characters = make([]Character, MaxWorldCharacters+1) },
		func(b *Blueprint) { b.Locations = make([]Location, MaxWorldLocations+1) },
		func(b *Blueprint) {
			b.ConditionDefinitions = make([]ConditionDefinition, MaxWorldConditionDefinitions+1)
		},
		func(b *Blueprint) { b.Conditions = make([]ConditionInstance, MaxWorldConditions+1) },
	} {
		b := catalogFixture(t)
		change(&b)
		var f *Fault
		if err := Validate(b); !errors.As(err, &f) || f.Code != "E_CATALOG" || f.Field != "capacity" {
			t.Fatal("catalog world escaped existing entity caps", err)
		}
	}
	b := catalogFixture(t)
	b.Items = append(b.Items, Item{ID: ItemID(strings.Repeat("i", 257))})
	if w, err := NewWorld(b); err == nil || w != nil {
		t.Fatal("catalog-only world skipped archive string admission")
	}
	b = catalogFixture(t)
	for len(b.ItemDefinitions) < MaxWorldItemDefinitions {
		b.ItemDefinitions = append(b.ItemDefinitions, ItemDefinition{ID: ItemDefinitionID(fmt.Sprint(len(b.ItemDefinitions))), Name: "Large synthetic entry", Description: strings.Repeat("x", 4096), Kind: "gear"})
	}
	if w, err := NewWorld(b); err == nil || w != nil {
		t.Fatal("catalog-only world skipped genesis record admission")
	}
	// The bound is on the whole expanded birth batch, not just intent/source.
	b = catalogFixture(t)
	b.ItemDefinitions[0].Description, b.ItemDefinitions[0].Name = strings.Repeat("d", 4096), strings.Repeat("n", 4096)
	w := catalogWorld(t, b)
	ops := make([]Operation, MaxOperations)
	for n := range ops {
		ops[n] = Operation{CreateItem: &Item{ID: ItemID(fmt.Sprint(n)), From: "torch"}}
	}
	before := w.ExportCheckpoint()
	if _, err := w.Execute(inventoryRequest("oversize", 0, ops...)); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("oversize batch published", err)
	}
}
