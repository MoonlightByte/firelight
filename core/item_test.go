package core

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestItemBirthOrderedFactsReplayAndDetachedReceipts(t *testing.T) {
	w, b := inventoryWorld(t, inventoryInt(3))
	i := &Item{ID: "new", Name: "Supplies", Description: "Blue stitching", Owner: "a", Quantity: inventoryInt(3)}
	r := inventoryRequest("birth", 0, Operation{CreateItem: i}, Operation{Transfer: &Transfer{Item: "new", From: "a", To: "b"}}, Operation{Consume: &ConsumeItem{Item: "new", Owner: "b", Amount: 1}})
	original := CanonicalRequest(r)
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	if receipt.Revision != 1 || len(receipt.Events) != 3 || receipt.Events[0].Kind != ItemCreated || receipt.Events[1].Kind != ItemTransferred || receipt.Events[2].Kind != ItemConsumed {
		t.Fatal(receipt)
	}
	born := receipt.Events[0].Item
	if born.Owner != "a" || *born.Quantity != 3 || born.Description != "Blue stitching" {
		t.Fatal(born)
	}
	s := w.Snapshot()
	now := item(&s.Blueprint, "new")
	if now.Owner != "b" || *now.Quantity != 2 || now.Wearer != "" || len(s.Items) != 3 {
		t.Fatal(s)
	}
	*i.Quantity = 99
	i.Description = "caller"
	*born.Quantity = 98
	receipt.Request.Operations[0].CreateItem.Description = "receipt"
	*now.Quantity = 97
	h := w.History()
	*h[0].Events[0].Item.Quantity = 96
	cp := w.ExportCheckpoint()
	*cp.History[0].Request.Operations[0].CreateItem.Quantity = 95
	fixed, _ := w.Receipt("birth")
	if !reflect.DeepEqual(fixed.Request, original) || fixed.Events[0].Item.Description != "Blue stitching" || *fixed.Events[0].Item.Quantity != 3 {
		t.Fatal("birth record alias")
	}
	if _, e = w.Execute(inventoryRequest("empty", 1, Operation{Consume: &ConsumeItem{Item: "new", Owner: "b", Amount: 2}})); e != nil {
		t.Fatal(e)
	}
	before := w.ExportCheckpoint()
	rebuilt, e := Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("replay", e)
	}
	for _, world := range []*World{w, rebuilt} {
		old, e := world.Execute(original)
		if e != nil || !reflect.DeepEqual(old, fixed) || !reflect.DeepEqual(world.ExportCheckpoint(), before) {
			t.Fatal("retry", e)
		}
		changed := CanonicalRequest(original)
		changed.Operations[0].CreateItem.Description = "different"
		creationReject(t, world, changed, "E_REQUEST_REUSE")
	}
	for _, mutate := range []func([]Batch){
		func(h []Batch) { h[0].Events[0].Item.Description = "forged" },
		func(h []Batch) { h[0].Events[0].Item.Owner = "b" },
		func(h []Batch) { h[0].Events[0].Item.Quantity = nil },
		func(h []Batch) { h[0].Events[0].Resource = &ResourceChange{} },
	} {
		h := w.History()
		mutate(h)
		if got, e := Replay(b, h); e == nil || got != nil {
			t.Fatal("accepted altered birth")
		}
	}
}

func TestItemBirthInvalidFactsAtomicityAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		i    Item
		code string
	}{
		{Item{ID: "a"}, "E_DUPLICATE"}, {Item{ID: "hall"}, "E_DUPLICATE"}, {Item{ID: "stock"}, "E_DUPLICATE"},
		{Item{ID: ""}, "E_ID"}, {Item{ID: "new", Name: string([]byte{255})}, "E_ID"},
		{Item{ID: "new", Description: string([]byte{255})}, "E_TEXT"},
		{Item{ID: "new", Owner: "missing"}, "E_REFERENCE"}, {Item{ID: "new", Owner: "hall"}, "E_TYPE"},
		{Item{ID: "new", Quantity: inventoryInt(0)}, "E_QUANTITY"}, {Item{ID: "new", Quantity: inventoryInt(-1)}, "E_QUANTITY"},
		{Item{ID: "new", Wearer: "a"}, "E_RULE"},
		{Item{ID: "new", Equipment: &EquipmentInstance{Definition: "unknown"}}, "E_EQUIPMENT"},
	} {
		t.Run(tc.code+string(tc.i.ID), func(t *testing.T) {
			w, _ := inventoryWorld(t, inventoryInt(0))
			creationReject(t, w, inventoryRequest("same", 0, Operation{CreateItem: &tc.i}), tc.code)
			if _, e := w.Execute(inventoryRequest("same", 0, Operation{CreateItem: &Item{ID: "good"}})); e != nil {
				t.Fatal(e)
			}
		})
	}
	w, _ := inventoryWorld(t, nil)
	creationReject(t, w, inventoryRequest("shape", 0, Operation{CreateItem: &Item{ID: "new"}, Unequip: &Unequip{Item: "stock"}}), "E_IR")
	creationReject(t, w, inventoryRequest("early", 0, Operation{Transfer: &Transfer{Item: "new", From: "a", To: "b"}}, Operation{CreateItem: &Item{ID: "new", Owner: "a"}}), "E_REFERENCE")
	creationReject(t, w, inventoryRequest("late", 0, Operation{CreateItem: &Item{ID: "new", Owner: "a"}}, Operation{Consume: &ConsumeItem{Item: "new", Owner: "a", Amount: 2}}), "E_QUANTITY")
}

func TestItemBirthSharesFinalArithmeticAndUniqueCondition(t *testing.T) {
	b := creationFixture()
	w := creationWorld(t, b)
	plus := Operation{Apply: &Effect{ID: "plus", Source: Ref{CharacterKind, "actor"}, Mode: OnCharacter, Target: Ref{CharacterKind, "actor"}, Stat: "x", Amount: 1}}
	minus := Operation{Apply: &Effect{ID: "minus", Source: Ref{CharacterKind, "actor"}, Mode: OnCharacter, Target: Ref{CharacterKind, "actor"}, Stat: "x", Amount: -1}}
	birth := Operation{CreateItem: &Item{ID: "ribbon", Owner: "actor", Description: "Plain blue ribbon"}}
	creationReject(t, w, creationRequest("overflow", 0, plus, birth), "E_OVERFLOW")
	mark := Operation{ApplyCondition: &ApplyCondition{Instance: "issued", Type: "training", Target: "actor", Source: &Ref{LocationKind, "hall"}}}
	first, e := w.Execute(creationRequest("claim", 0, plus, birth, minus, mark))
	if e != nil {
		t.Fatal(e)
	}
	x, e := w.Explain("actor", "x")
	if e != nil || x.Effective != math.MaxInt64 {
		t.Fatal(x, e)
	}
	second := Operation{CreateItem: &Item{ID: "ribbon2", Owner: "actor"}}
	creationReject(t, w, creationRequest("again", 1, second, Operation{ApplyCondition: &ApplyCondition{Instance: "issued2", Type: "training", Target: "actor"}}), "E_RULE")
	if _, e = w.Execute(creationRequest("npc", 1, Operation{CreateCharacter: newNPC()}, Operation{CreateItem: &Item{ID: "npc-item", Owner: "npc"}}, Operation{Transfer: &Transfer{Item: "ribbon", From: "actor", To: "npc"}})); e != nil {
		t.Fatal(e)
	}
	creationReject(t, w, creationRequest("again", 2, second, Operation{ApplyCondition: &ApplyCondition{Instance: "issued2", Type: "training", Target: "actor"}}), "E_RULE")
	old, e := w.Execute(first.Request)
	if e != nil || !reflect.DeepEqual(old, first) {
		t.Fatal(e)
	}
}

func TestItemBirthNativeTextCountAndConcurrentAllocation(t *testing.T) {
	b := creationFixture()
	w := creationWorld(t, b)
	i := Item{ID: "large", Name: strings.Repeat("<", 4096), Description: strings.Repeat("<", 4096)}
	if _, e := w.Execute(creationRequest("large", 0, Operation{CreateItem: &i})); e != nil {
		t.Fatal(e)
	}
	tooLarge := i
	tooLarge.ID = "too-large"
	tooLarge.Description += "x"
	before := w.ExportCheckpoint()
	if _, e := w.Execute(creationRequest("bytes", 1, Operation{CreateItem: &tooLarge})); e == nil {
		t.Fatal("accepted oversized native text")
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("partial text publication")
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"r1", "r2"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			_, e := w.Execute(creationRequest(id, 1, Operation{CreateItem: &Item{ID: "race"}}))
			results <- e
		}(id)
	}
	group.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 || w.Revision() != 2 {
		t.Fatal(success, w.Revision())
	}
	// Existing accepted zero genesis remains legal, and description is inert.
	b.Items[0].Quantity = inventoryInt(0)
	b.Items[0].Description = "magical healing food"
	if _, e := NewWorld(b); e != nil {
		t.Fatal(e)
	}
}
