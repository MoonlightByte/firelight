package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func chargesFixture(t *testing.T) Blueprint {
	t.Helper()
	b := catalogFixture(t)
	initial := int64(10)
	b.ItemDefinitions = append(b.ItemDefinitions,
		ItemDefinition{ID: "wand", Name: "Wand", Kind: "wand", Magical: true, Charges: &ChargeRule{Max: 7}},
		ItemDefinition{ID: "cube", Name: "Cube", Kind: "wondrous", Magical: true, Charges: &ChargeRule{Max: 36, Initial: &initial}})
	b.Items = append(b.Items,
		Item{ID: "held", From: "wand", Owner: "a", Custody: &Ref{CharacterKind, "a"}, Charges: &ItemCharges{Current: 3}},
		Item{ID: "worn", From: "wand", Owner: "a", Wearer: "a", Charges: &ItemCharges{Current: 7, Max: 7}})
	return b
}

func expend(id ItemID, owner CharacterID, n int64) Operation {
	return Operation{ExpendCharges: &ExpendCharges{Item: id, Owner: owner, Amount: n}}
}

func chargesFaultIs(t *testing.T, err error, want Fault) {
	t.Helper()
	var got *Fault
	if !errors.As(err, &got) || got.Code != want.Code || got.Subject != want.Subject || got.Field != want.Field || got.Expected != want.Expected || got.Actual != want.Actual {
		t.Fatalf("want %+v, got %v", want, err)
	}
}

func chargesOf(t *testing.T, w *World, id ItemID) ItemCharges {
	t.Helper()
	s := w.Snapshot()
	i := item(&s.Blueprint, id)
	if i == nil || i.Charges == nil {
		t.Fatalf("%s has no charges", id)
	}
	return *i.Charges
}

func TestChargesDeriveSpendViewReplay(t *testing.T) {
	b := chargesFixture(t)
	original, _ := json.Marshal(b)
	w := catalogWorld(t, b)
	if after, _ := json.Marshal(b); string(after) != string(original) {
		t.Fatal("construction changed caller input")
	}
	if c := chargesOf(t, w, "held"); c != (ItemCharges{Current: 3, Max: 7}) {
		t.Fatal("genesis max not derived", c)
	}
	create := &Item{ID: "fresh", From: "cube", Owner: "a", Custody: &Ref{CharacterKind, "a"}}
	r, err := w.Execute(inventoryRequest("create", 0, Operation{CreateItem: create}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Events[0].Item.Charges == nil || *r.Events[0].Item.Charges != (ItemCharges{Current: 10, Max: 36}) || r.Events[0].ItemIntent.Charges != nil || create.Charges != nil {
		t.Fatal("creation must derive the initial count without expanding intent", r.Events[0])
	}
	partly := &Item{ID: "found", From: "wand", Owner: "a", Charges: &ItemCharges{Current: 2}}
	if _, err := w.Execute(inventoryRequest("found", 1, Operation{CreateItem: partly})); err != nil || chargesOf(t, w, "found") != (ItemCharges{Current: 2, Max: 7}) {
		t.Fatal("explicit creation count", err)
	}
	r, err = w.Execute(inventoryRequest("spend", 2, expend("held", "a", 3), expend("worn", "a", 2)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || r.Events[0].Kind != ChargesExpended || *r.Events[0].Charges != (ChargeExpenditure{Item: "held", Owner: "a", Requested: 3, Before: 3, After: 0}) || r.Events[1].Charges.After != 5 {
		t.Fatal(r.Events)
	}
	if c := chargesOf(t, w, "held"); c.Current != 0 {
		t.Fatal("exhausted item must keep its identity at zero", c)
	}
	_, err = w.Execute(inventoryRequest("empty", 3, expend("held", "a", 1)))
	chargesFaultIs(t, err, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "at least 1 charges", Actual: "0"})
	if _, err := w.Execute(inventoryRequest("give", 3, Operation{Transfer: &Transfer{Item: "worn", From: "a", To: "b"}})); err != nil || chargesOf(t, w, "worn").Current != 5 {
		t.Fatal("transfer must keep charges", err)
	}
	view, err := w.ItemChargesView([]ItemID{"worn", "fresh", "held", "worn"})
	if err != nil || len(view) != 4 || view[0] != (ItemChargesProjection{Revision: 4, Item: "worn", Owner: "b", Max: 7, Current: 5, Available: 5}) || view[1].Available != 10 || view[2].Available != 0 || view[3] != view[0] {
		t.Fatal(view, err)
	}
	partial, err := w.ItemChargesView([]ItemID{"held", "stock", "worn"})
	chargesFaultIs(t, err, Fault{Code: "E_CHARGES", Subject: "stock", Field: "id", Expected: "charged item", Actual: "stock"})
	if len(partial) != 1 || partial[0].Item != "held" {
		t.Fatal("a failed lookup keeps earlier results", partial)
	}
	replayed, err := Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	h := w.History()
	h[2].Events[0].Charges.After = 1
	if _, err := Replay(b, h); err == nil {
		t.Fatal("tampered charge event replayed")
	}
	if w.History()[2].Events[0].Charges.After != 0 {
		t.Fatal("history aliases the caller's charge record")
	}
	if r, err := w.Execute(inventoryRequest("spend", 2, expend("held", "a", 3), expend("worn", "a", 2))); err != nil || r.Revision != 3 {
		t.Fatal("retry must return the original receipt", r, err)
	}
	// Definitions are detached: neither the caller's genesis nor a view can
	// change the initial count a later creation derives.
	*b.ItemDefinitions[3].Charges.Initial = 1
	defs, _ := w.ItemDefinitionsView([]ItemDefinitionID{"cube"})
	*defs[0].Charges.Initial = 2
	s := w.Snapshot()
	*s.ItemDefinitions[1].Charges.Initial = 3
	if _, err := w.Execute(inventoryRequest("again", 4, Operation{CreateItem: &Item{ID: "second", From: "cube", Owner: "a"}})); err != nil || chargesOf(t, w, "second").Current != 10 {
		t.Fatal("definition initial aliased", err)
	}
}

func TestChargesExactRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Blueprint)
		want   Fault
	}{
		{"no rule", func(b *Blueprint) {
			b.Items = append(b.Items, Item{ID: "x", From: "torch", Owner: "a", Charges: &ItemCharges{Current: 1}})
		},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges", Expected: "no charges for this definition", Actual: "present"}},
		{"uncatalogued", func(b *Blueprint) {
			b.Items = append(b.Items, Item{ID: "x", Name: "Stick", Owner: "a", Charges: &ItemCharges{Current: 1}})
		},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges", Expected: "item from a charged definition", Actual: "uncatalogued item"}},
		{"above max", func(b *Blueprint) { b.Items[len(b.Items)-1].Charges = &ItemCharges{Current: 8} },
			Fault{Code: "E_CHARGES", Subject: "worn", Field: "charges.current", Expected: "0 to 7", Actual: "8"}},
		{"negative", func(b *Blueprint) { b.Items[len(b.Items)-1].Charges = &ItemCharges{Current: -1} },
			Fault{Code: "E_CHARGES", Subject: "worn", Field: "charges.current", Expected: "0 to 7", Actual: "-1"}},
		{"other max", func(b *Blueprint) { b.Items[len(b.Items)-1].Charges = &ItemCharges{Current: 1, Max: 6} },
			Fault{Code: "E_CHARGES", Subject: "worn", Field: "charges.max", Expected: "7", Actual: "6"}},
		{"stack", func(b *Blueprint) { b.Items[len(b.Items)-2].Quantity = inventoryInt(2) },
			Fault{Code: "E_CHARGES", Subject: "held", Field: "quantity", Expected: "at most one unit", Actual: "2"}},
		{"container", func(b *Blueprint) { b.Items[len(b.Items)-2].Container = true; b.Items[len(b.Items)-2].Custody = nil },
			Fault{Code: "E_CHARGES", Subject: "held", Field: "container", Expected: "an item, not a container", Actual: "container"}},
		{"recoverable", func(b *Blueprint) {
			b.Rules.RecoveryNumerator, b.Rules.RecoveryDenominator = 1, 2
			b.Items[len(b.Items)-2].Recoverable = 1
		}, Fault{Code: "E_CHARGES", Subject: "held", Field: "recoverable", Expected: "no recoverable count", Actual: "1"}},
		{"max zero", func(b *Blueprint) { b.ItemDefinitions[2].Charges.Max = 0 },
			Fault{Code: "E_CATALOG", Subject: "wand", Field: "charges.max", Expected: "1 to 1000", Actual: "0"}},
		{"max bound", func(b *Blueprint) { b.ItemDefinitions[2].Charges.Max = 1001 },
			Fault{Code: "E_CATALOG", Subject: "wand", Field: "charges.max", Expected: "1 to 1000", Actual: "1001"}},
		{"initial above", func(b *Blueprint) { b.ItemDefinitions[3].Charges.Initial = inventoryInt(37) },
			Fault{Code: "E_CATALOG", Subject: "cube", Field: "charges.initial", Expected: "0 to the maximum", Actual: "37"}},
		{"initial negative", func(b *Blueprint) { b.ItemDefinitions[3].Charges.Initial = inventoryInt(-1) },
			Fault{Code: "E_CATALOG", Subject: "cube", Field: "charges.initial", Expected: "0 to the maximum", Actual: "-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := chargesFixture(t)
			tc.change(&b)
			_, err := NewWorld(b)
			chargesFaultIs(t, err, tc.want)
			if verr := Validate(b); verr == nil {
				t.Fatal("Validate accepted", tc.name)
			}
		})
	}

	b := chargesFixture(t)
	b.Rules.RecoveryNumerator, b.Rules.RecoveryDenominator = 1, 2
	b.Items = append(b.Items, Item{ID: "elsewhere", From: "wand", Owner: "a", Custody: &Ref{CharacterKind, "b"}, Charges: &ItemCharges{Current: 4}})
	w := catalogWorld(t, b)
	before := w.ExportCheckpoint()
	for _, tc := range []struct {
		name string
		ops  []Operation
		want Fault
	}{
		{"owner", []Operation{expend("worn", "b", 1)}, Fault{Code: "E_OWNER", Subject: "worn", Field: "owner", Expected: "a", Actual: "b"}},
		{"custody", []Operation{expend("elsewhere", "a", 1)}, Fault{Code: "E_CUSTODY", Subject: "elsewhere", Field: "custody"}},
		{"uncharged", []Operation{{ExpendCharges: &ExpendCharges{Item: "stock", Owner: "a", Amount: 1}}}, Fault{Code: "E_CHARGES", Subject: "stock", Field: "charges", Expected: "charged item", Actual: "none"}},
		{"zero", []Operation{expend("held", "a", 0)}, Fault{Code: "E_AMOUNT", Subject: "held", Field: "charges", Expected: "positive int64", Actual: "nonpositive"}},
		{"too many", []Operation{expend("held", "a", 4)}, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "at least 4 charges", Actual: "3"}},
		{"split", []Operation{{Split: &SplitItem{Item: "held", Created: "half", Amount: 1, Handler: "a"}}}, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "uncharged stock", Actual: "charged item"}},
		{"add", []Operation{{Add: &AddItem{Item: "held", Owner: "a", Amount: 1}}}, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "uncharged stock", Actual: "charged item"}},
		{"recoverable", []Operation{{Consume: &ConsumeItem{Item: "held", Owner: "a", Amount: 1, Recoverable: true}}}, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "uncharged stock", Actual: "charged item"}},
		{"create stack", []Operation{{CreateItem: &Item{ID: "pile", From: "wand", Owner: "a", Quantity: inventoryInt(2)}}}, Fault{Code: "E_CHARGES", Subject: "pile", Field: "quantity", Expected: "at most one unit", Actual: "2"}},
		{"create container", []Operation{{CreateItem: &Item{ID: "box", From: "wand", Owner: "a", Container: true, Custody: &Ref{LocationKind, "hall"}}}}, Fault{Code: "E_CHARGES", Subject: "box", Field: "container", Expected: "an item, not a container", Actual: "container"}},
		{"rollback", []Operation{expend("held", "a", 2), consume(9, "a")}, Fault{Code: "E_QUANTITY", Subject: "stock", Field: "quantity", Expected: "at least 9 units", Actual: "3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.Execute(inventoryRequest(tc.name, 0, tc.ops...))
			var got *Fault
			if !errors.As(err, &got) || got.Code != tc.want.Code || got.Subject != tc.want.Subject || got.Field != tc.want.Field ||
				(tc.want.Expected != "" && (got.Expected != tc.want.Expected || got.Actual != tc.want.Actual)) {
				t.Fatalf("want %+v, got %v", tc.want, err)
			}
			if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("refusal changed the world")
			}
		})
	}

	// Physical consumption is separate from charges: the unit goes, the identity
	// and count stay, and nothing can spend or resurrect it.
	if _, err := w.Execute(inventoryRequest("burn", 0, Operation{Consume: &ConsumeItem{Item: "held", Owner: "a", Amount: 1}})); err != nil {
		t.Fatal(err)
	}
	if c := chargesOf(t, w, "held"); c.Current != 3 {
		t.Fatal("consumption changed charges", c)
	}
	_, err := w.Execute(inventoryRequest("ghost", 1, expend("held", "a", 1)))
	chargesFaultIs(t, err, Fault{Code: "E_QUANTITY", Subject: "held", Field: "quantity", Expected: "one physical unit", Actual: "0"})
	_, err = w.Execute(inventoryRequest("revive", 1, Operation{Add: &AddItem{Item: "held", Owner: "a", Amount: 1}}))
	chargesFaultIs(t, err, Fault{Code: "E_CHARGES", Subject: "held", Field: "charges", Expected: "uncharged stock", Actual: "charged item"})
	if v, err := w.ItemChargesView([]ItemID{"held"}); err != nil || v[0].Current != 3 || v[0].Available != 0 {
		t.Fatal("a consumed item has nothing available", v, err)
	}
}

func TestChargesFeatureFreeBytes(t *testing.T) {
	w, _ := inventoryWorld(t, inventoryInt(3))
	if _, err := w.Execute(inventoryRequest("legacy", 0, consume(1, "a"))); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(w.ExportCheckpoint())
	for _, marker := range []string{`"charges"`, `"expend_charges"`, "ChargesExpended"} {
		if strings.Contains(string(raw), marker) {
			t.Fatal("unused charges field changed legacy encoding", marker)
		}
	}
	cw := catalogWorld(t, catalogFixture(t))
	raw, _ = json.Marshal(cw.ExportCheckpoint())
	if strings.Contains(string(raw), `"charges"`) {
		t.Fatal("a catalog without charge rules gained a charges field")
	}
}
