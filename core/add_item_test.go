package core

import (
	"math"
	"reflect"
	"testing"
)

func addStock(n int64, owner CharacterID) Operation {
	return Operation{Add: &AddItem{Item: "stock", Owner: owner, Amount: n}}
}
func TestAddStockLifecycle(t *testing.T) {
	_, b := inventoryWorld(t, inventoryInt(20))
	b.Items[0].Custody = &Ref{CharacterKind, "a"}
	w, e := NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	req := inventoryRequest("add", 0, addStock(10, "a"))
	receipt, e := w.Execute(req)
	if e != nil || *inventoryStock(w).Quantity != 30 || len(receipt.Events) != 1 || receipt.Events[0].Kind != ItemAdded || *receipt.Events[0].Addition.Before != 20 || receipt.Events[0].Addition.After != 30 {
		t.Fatal(receipt, e)
	}
	if _, e = w.Execute(inventoryRequest("empty", 1, consume(30, "a"))); e != nil {
		t.Fatal(e)
	}
	if _, e = w.Execute(inventoryRequest("refill", 2, addStock(5, "a"))); e != nil || *inventoryStock(w).Quantity != 5 {
		t.Fatal(e)
	}
	rebuilt, e := Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("replay")
	}
	before := rebuilt.ExportCheckpoint()
	retry, e := rebuilt.Execute(req)
	if e != nil || !reflect.DeepEqual(receipt, retry) || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("retry", e)
	}
	req.Operations[0].Add.Amount = 9
	_, e = rebuilt.Execute(req)
	inventoryCode(t, e, "E_REQUEST_REUSE")
}
func TestAddStockRejectionsAndRollback(t *testing.T) {
	for _, tc := range []struct {
		op   Operation
		code string
	}{
		{addStock(0, "a"), "E_AMOUNT"}, {addStock(-1, "a"), "E_AMOUNT"}, {addStock(1, "b"), "E_OWNER"}, {addStock(1, "missing"), "E_REFERENCE"},
		{addStock(math.MaxInt64, "a"), "E_QUANTITY"}, {Operation{Add: &AddItem{Item: "a", Owner: "a", Amount: 1}}, "E_TYPE"},
		{Operation{Add: &AddItem{Item: "stock", Owner: "a", Amount: 1}, Consume: &ConsumeItem{Item: "stock", Owner: "a", Amount: 1}}, "E_IR"},
	} {
		w, _ := inventoryWorld(t, inventoryInt(20))
		before := w.ExportCheckpoint()
		_, e := w.Execute(inventoryRequest("r", 0, addStock(10, "a"), tc.op))
		inventoryCode(t, e, tc.code)
		if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("partial commit")
		}
		if _, e = w.Execute(inventoryRequest("r", 0, addStock(1, "a"))); e != nil {
			t.Fatal(e)
		}
	}
	for _, kind := range []string{"worn", "stored", "container", "wrong-owner"} {
		_, b := inventoryWorld(t, inventoryInt(1))
		i := &b.Items[0]
		code := "E_CUSTODY"
		switch kind {
		case "worn":
			i.Wearer = "a"
			code = "E_WORN"
		case "stored":
			i.Custody = &Ref{LocationKind, "hall"}
		case "container":
			i.Container = true
			i.Custody = &Ref{LocationKind, "hall"}
		case "wrong-owner":
			i.Custody = &Ref{CharacterKind, "a"}
			code = "E_OWNER"
		}
		w, e := NewWorld(b)
		if e != nil {
			t.Fatal(e)
		}
		owner := CharacterID("a")
		if kind == "wrong-owner" {
			owner = "b"
		}
		_, e = w.Execute(inventoryRequest("r", 0, addStock(1, owner)))
		inventoryCode(t, e, code)
	}
	w, _ := inventoryWorld(t, inventoryInt(0))
	if _, e := w.Execute(inventoryRequest("max", 0, addStock(math.MaxInt64, "a"))); e != nil || *inventoryStock(w).Quantity != math.MaxInt64 {
		t.Fatal(e)
	}
}
func TestAddStockDetachmentAndTamper(t *testing.T) {
	q := inventoryInt(3)
	w, b := inventoryWorld(t, q)
	*q = 999
	if *inventoryStock(w).Quantity != 3 {
		t.Fatal("input alias")
	}
	*q = 3
	request := inventoryRequest("one", 0, addStock(1, "a"))
	receipt, e := w.Execute(request)
	if e != nil {
		t.Fatal(e)
	}
	request.Operations[0].Add.Amount = 999
	*receipt.Events[0].Addition.Before = 999
	stock := inventoryStock(w)
	*stock.Quantity = 999
	history := w.History()
	*history[0].Events[0].Addition.Before = 998
	if *inventoryStock(w).Quantity != 4 || *w.History()[0].Events[0].Addition.Before != 3 || w.History()[0].Request.Operations[0].Add.Amount != 1 {
		t.Fatal("detachment")
	}
	for _, change := range []func(*Batch){
		func(b *Batch) { *b.Events[0].Addition.Before = 2 }, func(b *Batch) { b.Events[0].Addition.After = 0 },
		func(b *Batch) { b.Events[0].Addition.Owner = "b" }, func(b *Batch) { b.Request.Operations[0].Add.Amount = 2 },
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
