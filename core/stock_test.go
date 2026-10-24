package core_test

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func splitOp(source, created core.ItemID, amount int64, handler core.CharacterID) core.Operation {
	return core.Operation{Split: &core.SplitItem{Item: source, Created: created, Amount: amount, Handler: handler}}
}

func TestStockPartialPlacementRollbackReplayAndOwnership(t *testing.T) {
	b := custodyFixture()
	b.Items[2].Quantity, b.Items[2].Description = gearPtr(int64(6)), "Six prayer leaves; not magic."
	w := gearWorld(t, b)
	from, chest := core.Ref{Kind: core.CharacterKind, ID: "b"}, core.Ref{Kind: core.ItemKind, ID: "chest"}
	first := gearExecute(t, w, "store-two", splitOp("scroll", "stored", 2, "b"), placeOp("stored", from, chest, "b"))
	if *custodyItem(w, "scroll").Quantity != 4 || *custodyItem(w, "stored").Quantity != 2 || custodyItem(w, "stored").Owner != "a" || custodyItem(w, "stored").Description != b.Items[2].Description {
		t.Fatal("split lost quantity or original properties")
	}
	if len(first.Events) != 2 || first.Events[0].Kind != core.ItemSplit || *first.Events[0].StockSplit.Created.Custody != from || first.Events[1].Kind != core.ItemPlaced {
		t.Fatal("split event is not its own before-placement truth", first.Events)
	}
	gearReject(t, w, "late", splitOp("stored", "returned", 1, "b"), placeOp("returned", chest, from, "b"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 99}})
	gearExecute(t, w, "late", splitOp("stored", "returned", 1, "b"), placeOp("returned", chest, core.Ref{Kind: core.CharacterKind, ID: "a"}, "b"))
	if *custodyItem(w, "scroll").Quantity != 4 || *custodyItem(w, "stored").Quantity != 1 || *custodyItem(w, "returned").Quantity != 1 {
		t.Fatal("partial retrieval merged identities")
	}
	if custodyItem(w, "stored").SplitFrom != "scroll" || custodyItem(w, "returned").SplitFrom != "stored" {
		t.Fatal("lost immediate split ancestry")
	}
	gearReject(t, w, "forged-birth", core.Operation{CreateItem: &core.Item{ID: "forged", SplitFrom: "scroll", Owner: "a"}})
	gearExecute(t, w, "consume", core.Operation{Consume: &core.ConsumeItem{Item: "returned", Owner: "a", Amount: 1}})
	gearReject(t, w, "recycle", splitOp("scroll", "returned", 1, "b"))
	cp := w.ExportCheckpoint()
	var data bytes.Buffer
	if _, err := nativesave.Write(&data, cp, nativesave.SessionState{Campaign: "stock", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&data)
	if err != nil || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("split archive", err)
	}
	retry, err := loaded.World.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(retry, first) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("split retried", err)
	}
	for _, corrupt := range []func(*core.StockSplit){
		func(v *core.StockSplit) { *v.Before.Quantity = 99 },
		func(v *core.StockSplit) { v.Created.Description = "forged" },
		func(v *core.StockSplit) { v.After.Custody.ID = "a" },
		func(v *core.StockSplit) { v.Created.SplitFrom = "shield" },
	} {
		h := w.History()
		corrupt(h[0].Events[0].StockSplit)
		if partial, err := core.Replay(cp.Genesis, h); err == nil || partial != nil {
			t.Fatal("forged split history")
		}
	}
	first.Events[0].StockSplit.Created.Custody.ID = "forged"
	*first.Events[0].StockSplit.Before.Quantity = 100
	first.Request.Operations[0].Split.Amount = 99
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("split record aliases world")
	}
}

func TestStockIllegalBoundariesAndEquipmentDefinitionReuse(t *testing.T) {
	for _, parent := range []core.ItemID{"scroll", "room", "missing"} {
		b := custodyFixture()
		b.Items[2].SplitFrom = parent
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid stock ancestry", parent)
		}
	}
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Items[2].Custody = nil },
		func(b *core.Blueprint) {
			b.Characters[1].Location = "remote"
			b.Items[2].Custody = gearPtr(core.Ref{Kind: core.CharacterKind, ID: "a"})
		},
		func(b *core.Blueprint) {
			b.Effects = append(b.Effects, core.Effect{ID: "stock-effect", Source: core.Ref{Kind: core.ItemKind, ID: "scroll"}, Target: core.Ref{Kind: core.ItemKind, ID: "scroll"}, Mode: core.WhileWorn, Stat: "defense", Amount: 1})
		},
	} {
		b := custodyFixture()
		mutate(&b)
		w := gearWorld(t, b)
		gearReject(t, w, "no-split", splitOp("scroll", "new", 1, "b"))
	}
	w := gearWorld(t, custodyFixture())
	for _, op := range []core.Operation{
		splitOp("scroll", "new", 0, "b"), splitOp("scroll", "new", -1, "b"), splitOp("scroll", "new", 3, "b"), splitOp("scroll", "new", 4, "b"),
		splitOp("scroll", "scroll", 1, "b"), splitOp("scroll", "room", 1, "b"), splitOp("scroll", "", 1, "b"), splitOp("scroll", "new", 1, "room"),
		splitOp("chest", "new", 1, "b"), splitOp("shield", "new", 1, "b"),
		{Split: splitOp("scroll", "new", 1, "b").Split, Unequip: &core.Unequip{Item: "shield"}},
	} {
		gearReject(t, w, "bad", op)
	}
	b := custodyFixture()
	b.Items[2].Quantity = gearPtr(int64(math.MaxInt64))
	w = gearWorld(t, b)
	gearExecute(t, w, "maximum", splitOp("scroll", "one", 1, "b"))
	if *custodyItem(w, "scroll").Quantity != math.MaxInt64-1 {
		t.Fatal("quantity wrapped")
	}
	b = gearFixture()
	b.Items[0].Quantity = gearPtr(int64(2))
	b.Items[0].Custody = gearPtr(core.Ref{Kind: core.CharacterKind, ID: "b"})
	w = gearWorld(t, b)
	gearExecute(t, w, "two-weapons", splitOp("sword", "new-sword", 1, "b"), wear("new-sword", "b", "one"), wear("sword", "b", "one"))
	view, err := w.EquipmentView("b")
	if err != nil || len(view.Attacks) != 2 || view.Attacks[0].ID == view.Attacks[1].ID || custodyItem(w, "new-sword").Equipment.Definition != "longsword" {
		t.Fatal(view, err)
	}
	before := w.ExportCheckpoint()
	h := w.History()
	h[0].Events[0].StockSplit.Created.Equipment.Definition = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("equipment split alias")
	}
}
