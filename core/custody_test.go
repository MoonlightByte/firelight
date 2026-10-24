package core_test

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"reflect"
	"sync"
	"testing"
)

func custodyFixture() core.Blueprint {
	b := maintenanceFixture()
	b.Rules.Transfer = core.KeepsWorn
	b.Items[0].Owner = "a"
	b.Items[0].Custody = gearPtr(core.Ref{Kind: core.CharacterKind, ID: "b"})
	b.Items = append(b.Items,
		core.Item{ID: "chest", Name: "Chest", Container: true, Custody: gearPtr(core.Ref{Kind: core.LocationKind, ID: "room"})},
		core.Item{ID: "scroll", Name: "Prayer", Owner: "a", Quantity: gearPtr(int64(3)), Custody: gearPtr(core.Ref{Kind: core.CharacterKind, ID: "b"})})
	b.Locations = append(b.Locations, core.Location{ID: "remote"})
	return b
}
func placeOp(id core.ItemID, from, to core.Ref, handler core.CharacterID) core.Operation {
	return core.Operation{Place: &core.PlaceItem{Item: id, From: from, To: to, Handler: handler}}
}
func custodyItem(w *core.World, id core.ItemID) core.Item {
	for _, i := range w.Snapshot().Items {
		if i.ID == id {
			return i
		}
	}
	panic("missing fixture item")
}
func TestCustodyOwnershipWearingRollbackAndReplay(t *testing.T) {
	b := custodyFixture()
	w := gearWorld(t, b)
	from, to := core.Ref{Kind: core.CharacterKind, ID: "b"}, core.Ref{Kind: core.CharacterKind, ID: "c"}
	gearExecute(t, w, "ownership", core.Operation{Transfer: &core.Transfer{Item: "shield", From: "a", To: "c"}})
	i := custodyItem(w, "shield")
	if i.Owner != "c" || i.Wearer != "b" || *i.Custody != from {
		t.Fatal("ownership rewrote physical facts", i)
	}
	battleReject(t, w, gearRequest(w, "worn", placeOp("shield", from, to, "b")))
	ops := []core.Operation{{Unequip: &core.Unequip{Item: "shield"}}, placeOp("shield", from, to, "b"), {Equip: &core.Equip{Item: "shield", Wearer: "c"}}}
	late := append(append([]core.Operation(nil), ops...), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 99}})
	battleReject(t, w, gearRequest(w, "late", late...))
	first := gearExecute(t, w, "give", ops...)
	if len(first.Events) != 3 || first.Events[1].Kind != core.ItemPlaced || *custodyItem(w, "shield").Custody != to {
		t.Fatal(first)
	}
	assertDefense(t, w, "b", 10)
	assertDefense(t, w, "c", 12)
	stored := core.Ref{Kind: core.ItemKind, ID: "chest"}
	gearExecute(t, w, "store", placeOp("scroll", from, stored, "b"))
	view, err := w.ItemsAt(stored)
	if err != nil || len(view.Items) != 1 || view.Items[0].Owner != "a" || *view.Items[0].Quantity != 3 {
		t.Fatal(view, err)
	}
	battleReject(t, w, gearRequest(w, "stored-consume", core.Operation{Consume: &core.ConsumeItem{Item: "scroll", Owner: "a", Amount: 1}}))
	gearExecute(t, w, "retrieve", placeOp("scroll", stored, from, "b"))
	if now, err := w.ItemsAt(stored); err != nil || len(now.Items) != 0 || len(view.Items) != 1 {
		t.Fatal("historical container view", now, err)
	}
	borrowed, err := w.EquipmentView("b")
	if err != nil || len(borrowed.Items) != 1 || borrowed.Items[0].Item.ID != "scroll" {
		t.Fatal("unworn borrower cannot see held item", borrowed, err)
	}
	before := w.ExportCheckpoint()
	view.Items[0].Custody.ID = "forged"
	borrowed.Items[0].Item.Custody.ID = "forged"
	b.Items[0].Custody.ID = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("custody aliases canonical state")
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "custody", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("custody reconstruction", err)
	}
	r, err := loaded.World.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(first, r) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("retry moved item again", err)
	}
	for _, corrupt := range []func(*core.Event){
		func(e *core.Event) { e.Custody.Before.ID = "a" },
		func(e *core.Event) { e.Custody.Handler = "a" },
		func(e *core.Event) { e.Custody = nil },
	} {
		h := w.History()
		corrupt(&h[1].Events[1])
		if partial, err := core.Replay(before.Genesis, h); err == nil || partial != nil {
			t.Fatal("corrupt custody history accepted")
		}
	}
}

func TestCustodyIllegalStatesAndUnknownCompatibility(t *testing.T) {
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Items[0].Custody.Kind = core.LocationKind },
		func(b *core.Blueprint) { b.Items[0].Custody.ID = "c" },
		func(b *core.Blueprint) { b.Items[1].Custody = nil },
		func(b *core.Blueprint) { b.Items[1].Custody = gearPtr(core.Ref{Kind: core.ItemKind, ID: "chest"}) },
		func(b *core.Blueprint) { b.Items[1].Quantity = gearPtr(int64(2)) },
		func(b *core.Blueprint) { b.Items[1].Wearer = "a" },
		func(b *core.Blueprint) { b.Items[2].Custody = gearPtr(core.Ref{Kind: core.ItemKind, ID: "shield"}) },
	} {
		b := custodyFixture()
		mutate(&b)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid custody accepted", b.Items)
		}
	}
	b := custodyFixture()
	b.Characters[2].Location = "remote"
	w := gearWorld(t, b)
	from, chest := core.Ref{Kind: core.CharacterKind, ID: "b"}, core.Ref{Kind: core.ItemKind, ID: "chest"}
	for _, op := range []core.Operation{
		placeOp("scroll", from, chest, "c"),
		placeOp("scroll", from, core.Ref{Kind: core.CharacterKind, ID: "c"}, "b"),
		placeOp("scroll", core.Ref{Kind: core.CharacterKind, ID: "a"}, chest, "b"),
		placeOp("scroll", from, core.Ref{Kind: core.ItemKind, ID: "shield"}, "b"),
		placeOp("scroll", from, core.Ref{Kind: core.LocationKind, ID: "b"}, "b"),
		placeOp("chest", core.Ref{Kind: core.LocationKind, ID: "room"}, from, "b"),
		{Consume: &core.ConsumeItem{Item: "chest", Owner: "a", Amount: 1}},
		{Equip: &core.Equip{Item: "scroll", Wearer: "a"}},
		{Place: placeOp("scroll", from, chest, "b").Place, Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}},
	} {
		battleReject(t, w, gearRequest(w, "invalid", op))
	}
	gearExecute(t, w, "valid-after-errors", placeOp("scroll", from, chest, "b"))
	// Existing worlds never acquire custody as a side effect of equipment/ownership.
	b = maintenanceFixture()
	w = gearWorld(t, b)
	gearExecute(t, w, "old", core.Operation{Transfer: &core.Transfer{Item: "shield", From: "b", To: "a"}}, core.Operation{Equip: &core.Equip{Item: "shield", Wearer: "c"}})
	if custodyItem(w, "shield").Custody != nil {
		t.Fatal("invented historical custody")
	}
	battleReject(t, w, gearRequest(w, "unknown", placeOp("shield", core.Ref{Kind: core.CharacterKind, ID: "c"}, core.Ref{Kind: core.LocationKind, ID: "room"}, "c")))
}

func TestCustodyCompetingRequestsAndDepletion(t *testing.T) {
	b := custodyFixture()
	w := gearWorld(t, b)
	from := core.Ref{Kind: core.CharacterKind, ID: "b"}
	to := core.Ref{Kind: core.ItemKind, ID: "chest"}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, id := range []string{"one", "two"} {
		request := gearRequest(w, id, placeOp("scroll", from, to, "b"))
		request.ExpectedRevision = 0
		group.Add(1)
		go func() { defer group.Done(); _, err := w.Execute(request); results <- err }()
	}
	group.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 || w.Revision() != 1 || len(w.History()) != 1 || *custodyItem(w, "scroll").Custody != to {
		t.Fatal("competing custody publication", wins)
	}
	// Creation of an explicitly depleted record is illegal, but exhaustion keeps
	// its ID/custody and cannot then be used as movable stock.
	b = custodyFixture()
	b.Items[2].Quantity = gearPtr(int64(0))
	w = gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "depleted", placeOp("scroll", from, to, "b")))
}
