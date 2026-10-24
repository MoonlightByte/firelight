package core

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func inventoryInt(v int64) *int64 { return &v }
func inventoryWorld(t *testing.T, q *int64) (*World, Blueprint) {
	t.Helper()
	b := Blueprint{Rules: Rules{Transfer: Unequips, Wear: WearAny}, Locations: []Location{{ID: "hall"}},
		Characters: []Character{{ID: "a", Name: "Same", Location: "hall", Stats: map[StatID]int64{"guard": 10}, Resources: map[ResourceID]Resource{"hp": {Current: 8, Minimum: 0, Maximum: 10}}}, {ID: "b", Name: "Same", Location: "hall"}},
		Items:      []Item{{ID: "stock", Name: "Supplies", Owner: "a", Quantity: q}, {ID: "other", Name: "Supplies", Owner: "a"}},
		Effects:    []Effect{{ID: "provenance", Source: Ref{ItemKind, "stock"}, Mode: OnCharacter, Target: Ref{CharacterKind, "a"}, Stat: "guard", Amount: 2}}}
	w, e := NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return w, b
}
func inventoryRequest(id string, rev Revision, ops ...Operation) Request {
	return Request{ID: id, ExpectedRevision: rev, Actor: Ref{CharacterKind, "a"}, Operations: ops}
}
func consume(n int64, owner CharacterID) Operation {
	return Operation{Consume: &ConsumeItem{Item: "stock", Owner: owner, Amount: n}}
}
func inventoryStock(w *World) Item { s := w.Snapshot(); return *item(&s.Blueprint, "stock") }
func inventoryCode(t *testing.T, err error, code string) {
	t.Helper()
	var f *Fault
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("want %s: %v", code, err)
	}
}
func TestInventorySequenceReceiptsReplayAndProvenance(t *testing.T) {
	w, genesis := inventoryWorld(t, inventoryInt(3))
	r1 := inventoryRequest("one", 0, consume(1, "a"))
	one, e := w.Execute(r1)
	if e != nil {
		t.Fatal(e)
	}
	if *inventoryStock(w).Quantity != 2 || len(one.Events) != 1 || one.Events[0].Kind != ItemConsumed {
		t.Fatal(one)
	}
	expected := ConsumptionChange{Item: "stock", Owner: "a", Requested: 1, Before: inventoryInt(3), After: 2}
	if !reflect.DeepEqual(one.Events[0].Consumption, &expected) {
		t.Fatal(one.Events)
	}
	r2 := inventoryRequest("handoff", 1, Operation{Transfer: &Transfer{Item: "stock", From: "a", To: "b"}}, consume(1, "b"))
	two, e := w.Execute(r2)
	if e != nil {
		t.Fatal(e)
	}
	if *inventoryStock(w).Quantity != 1 || inventoryStock(w).Owner != "b" || two.Events[0].Kind != ItemTransferred || two.Events[1].Kind != ItemConsumed {
		t.Fatal(two)
	}
	r3 := inventoryRequest("last", 2, consume(1, "b"))
	if _, e = w.Execute(r3); e != nil {
		t.Fatal(e)
	}
	if *inventoryStock(w).Quantity != 0 {
		t.Fatal("depletion")
	}
	explanation, e := w.Explain("a", "guard")
	if e != nil || explanation.Effective != 12 {
		t.Fatal(explanation, e)
	}
	before := w.Snapshot()
	retry, e := w.Execute(r1)
	if e != nil || !reflect.DeepEqual(retry, one) || !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("retry", e)
	}
	rebuilt, e := Replay(genesis, w.History())
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, rebuilt.Snapshot()) {
		t.Fatal("reconstruction")
	}
	retry, e = rebuilt.Execute(r1)
	if e != nil || !reflect.DeepEqual(one, retry) {
		t.Fatal("historical receipt", e)
	}
	for _, op := range []Operation{consume(1, "b"), {Transfer: &Transfer{Item: "stock", From: "b", To: "a"}}, {Equip: &Equip{Item: "stock", Wearer: "b"}}} {
		_, e = w.Execute(inventoryRequest("unavailable", 3, op))
		inventoryCode(t, e, "E_QUANTITY")
		if !reflect.DeepEqual(before, w.Snapshot()) {
			t.Fatal("depleted mutation")
		}
	}
	if item(&before.Blueprint, "other").Owner != "a" || len(before.Items) != 2 || len(before.Effects) != 1 {
		t.Fatal("unrelated facts")
	}
}
func TestInventoryAtomicFailureAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		op   Operation
		code string
	}{
		{consume(0, "a"), "E_AMOUNT"}, {consume(-1, "a"), "E_AMOUNT"}, {consume(4, "a"), "E_QUANTITY"},
		{consume(1, "b"), "E_OWNER"}, {consume(1, "missing"), "E_REFERENCE"},
		{Operation{Equip: &Equip{Item: "stock", Wearer: "a"}}, "E_QUANTITY"},
		{Operation{Consume: &ConsumeItem{Item: "a", Owner: "a", Amount: 1}}, "E_TYPE"},
		{Operation{Consume: &ConsumeItem{Item: "stock", Owner: "a", Amount: 1}, Unequip: &Unequip{Item: "stock"}}, "E_IR"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			w, _ := inventoryWorld(t, inventoryInt(3))
			before := w.Snapshot()
			_, e := w.Execute(inventoryRequest("bad", 0, tc.op))
			inventoryCode(t, e, tc.code)
			if !reflect.DeepEqual(before, w.Snapshot()) || len(w.History()) != 0 {
				t.Fatal("partial commit")
			}
		})
	}
	w, _ := inventoryWorld(t, inventoryInt(3))
	before := w.Snapshot()
	_, e := w.Execute(inventoryRequest("reuse", 0, consume(1, "a"), consume(3, "a")))
	inventoryCode(t, e, "E_QUANTITY")
	if !reflect.DeepEqual(before, w.Snapshot()) || len(w.History()) != 0 {
		t.Fatal("late rollback")
	}
	if _, e = w.Execute(inventoryRequest("reuse", 0, consume(3, "a"))); e != nil {
		t.Fatal("failure reserved request", e)
	}
	w, _ = inventoryWorld(t, inventoryInt(math.MaxInt64))
	if _, e = w.Execute(inventoryRequest("max", 0, consume(math.MaxInt64, "a"))); e != nil || *inventoryStock(w).Quantity != 0 {
		t.Fatal(e)
	}
}
func TestInventorySingletonValidationAndProjection(t *testing.T) {
	w, b := inventoryWorld(t, nil)
	old, e := json.Marshal(inventoryStock(w))
	if e != nil {
		t.Fatal(e)
	}
	if string(old) != `{"id":"stock","name":"Supplies","owner":"a","wearer":""}` {
		t.Fatal(string(old))
	}
	if _, e = w.TransferView("stock", "b"); e != nil {
		t.Fatal("old view", e)
	}
	receipt, e := w.Execute(inventoryRequest("singleton", 0, consume(1, "a")))
	if e != nil || receipt.Events[0].Consumption.Before != nil || *inventoryStock(w).Quantity != 0 {
		t.Fatal(receipt, e)
	}
	_, e = w.TransferView("stock", "b")
	inventoryCode(t, e, "E_PROJECTION")
	for _, q := range []int64{-1, 0, 2} {
		malformed := cloneBlueprint(b)
		i := item(&malformed, "stock")
		i.Quantity = inventoryInt(q)
		if q >= 0 {
			i.Wearer = "a"
		}
		_, e = NewWorld(malformed)
		inventoryCode(t, e, "E_QUANTITY")
	}
	i := item(&b, "stock")
	i.Wearer = "a"
	w, e = NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	_, e = w.Execute(inventoryRequest("worn", 0, consume(1, "a")))
	inventoryCode(t, e, "E_WORN")
}
func TestInventoryDetachmentAndTamper(t *testing.T) {
	q := inventoryInt(3)
	w, b := inventoryWorld(t, q)
	*q = 999
	if *inventoryStock(w).Quantity != 3 {
		t.Fatal("input alias")
	}
	*q = 3
	request := inventoryRequest("one", 0, consume(1, "a"))
	receipt, e := w.Execute(request)
	if e != nil {
		t.Fatal(e)
	}
	request.Operations[0].Consume.Amount = 999
	*receipt.Events[0].Consumption.Before = 999
	stock := inventoryStock(w)
	*stock.Quantity = 999
	history := w.History()
	*history[0].Events[0].Consumption.Before = 998
	if *inventoryStock(w).Quantity != 2 || *w.History()[0].Events[0].Consumption.Before != 3 || w.History()[0].Request.Operations[0].Consume.Amount != 1 {
		t.Fatal("detachment")
	}
	for _, change := range []func(*Batch){
		func(b *Batch) { *b.Events[0].Consumption.Before = 2 }, func(b *Batch) { b.Events[0].Consumption.After = 0 },
		func(b *Batch) { b.Events[0].Consumption.Owner = "b" }, func(b *Batch) { b.Request.Operations[0].Consume.Amount = 2 },
		func(b *Batch) { b.Events[0].Resource = &ResourceChange{} },
	} {
		bad := w.History()
		change(&bad[0])
		out, e := Replay(b, bad)
		if e == nil || out != nil {
			t.Fatal("accepted tampering")
		}
	}
}
func TestInventoryConcurrentAdmissionAndWorkBound(t *testing.T) {
	w, _ := inventoryWorld(t, inventoryInt(3))
	var wg sync.WaitGroup
	var lock sync.Mutex
	success := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := w.Execute(inventoryRequest("same", 0, consume(1, "a")))
			if e == nil {
				lock.Lock()
				success++
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 8 || w.Revision() != 1 || *inventoryStock(w).Quantity != 2 || len(w.History()) != 1 {
		t.Fatal("duplicate concurrent publication")
	}
	ops := make([]Operation, MaxOperations+1)
	for i := range ops {
		ops[i] = consume(1, "a")
	}
	before := w.Snapshot()
	_, e := w.Execute(inventoryRequest("limit", 1, ops...))
	inventoryCode(t, e, "E_LIMIT")
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("work limit mutation")
	}
}

func TestInventoryDepletionAndDeliveryLifecycle(t *testing.T) {
	for _, phase := range []string{"before-claim", "before-delivery", "after-delivery"} {
		t.Run(phase, func(t *testing.T) {
			b := alphaFixture([2]uint64{1, 2})
			character(&b, "enemy").Resources["hp"] = Resource{Current: 0, Maximum: 6}
			w := alphaWorld(t, b)
			alphaCommit(t, w, alphaRequest(w, "victory", "a", "pass", "", ""))
			debit := func() {
				alphaCommit(t, w, Request{ID: "debit", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{Consume: &ConsumeItem{Item: "relic", Owner: inventoryRelicOwner(w), Amount: 1}}}})
			}
			claim := func() Request {
				return Request{ID: "claim", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{Claim: &ClaimObjective{Objective: "goal", Recipient: "a"}}}}
			}
			if phase == "before-claim" {
				debit()
				before := w.Snapshot()
				_, e := w.Execute(claim())
				inventoryCode(t, e, "E_QUANTITY")
				if !reflect.DeepEqual(before, w.Snapshot()) {
					t.Fatal("depleted claim")
				}
				return
			}
			alphaCommit(t, w, claim())
			if phase == "before-delivery" {
				debit()
			}
			alphaCommit(t, w, Request{ID: "home", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{Travel: &PartyTravel{To: "home"}}}})
			if phase == "after-delivery" {
				debit()
			}
			progress := w.Snapshot().Adventure.State.Objectives[0]
			if !progress.Claimed || progress.Completed != (phase == "after-delivery") {
				t.Fatal(progress)
			}
			rebuilt, e := Replay(b, w.History())
			if e != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
				t.Fatal("delivery replay", e)
			}
		})
	}
}
func inventoryRelicOwner(w *World) CharacterID {
	s := w.Snapshot()
	return item(&s.Blueprint, "relic").Owner
}
