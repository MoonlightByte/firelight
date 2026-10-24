package core

import (
	"math"
	"reflect"
	"testing"
)

func bufferFixture(t *testing.T) Blueprint {
	_, b := inventoryWorld(t, nil)
	b.Characters[0].Resources = map[ResourceID]Resource{"hp": {Current: 20, Maximum: 20}, "temp": {Current: 5, Maximum: math.MaxInt64}}
	b.Characters[0].Buffers = []ResourceBuffer{{Pool: "temp", Target: "hp"}}
	return b
}
func bufferWorld(t *testing.T, b Blueprint) *World {
	t.Helper()
	w, e := NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func bufferOp(kind string, n int64) Operation {
	v := &ResourceOp{Character: "a", Resource: "temp", Amount: n}
	switch kind {
	case "grant":
		return Operation{Grant: v}
	case "heal":
		return Operation{Heal: v}
	case "spend":
		return Operation{Spend: v}
	case "expand":
		return Operation{Expand: v}
	case "restore":
		return Operation{Restore: &RestoreResource{Character: "a", Resource: "temp"}}
	default:
		v.Resource = "hp"
		return Operation{Damage: v}
	}
}
func TestBufferDamageAndReplay(t *testing.T) {
	for _, tc := range []struct{ amount, hp, pool, absorbed int64 }{{3, 20, 2, 3}, {5, 20, 0, 5}, {8, 17, 0, 5}, {30, 0, 0, 5}, {math.MaxInt64, 0, 0, 5}} {
		b := bufferFixture(t)
		w := bufferWorld(t, b)
		r, e := w.Execute(inventoryRequest("hit", 0, bufferOp("damage", tc.amount)))
		if e != nil {
			t.Fatal(e)
		}
		c := w.Snapshot().Characters[0]
		v := r.Events[0].Resource
		if len(r.Events) != 1 || r.Events[0].Kind != DamageApplied || v.Requested != tc.amount || v.Before.Current != 20 || v.After.Current != tc.hp || v.Buffer.Before.Current != 5 || v.Buffer.After.Current != tc.pool || v.Buffer.Absorbed != tc.absorbed || c.Resources["hp"].Current != tc.hp || c.Resources["temp"].Current != tc.pool {
			t.Fatal(r, c)
		}
		rebuilt, e := Replay(b, w.History())
		if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
			t.Fatal("replay", e)
		}
		before := rebuilt.ExportCheckpoint()
		retry, e := rebuilt.Execute(r.Request)
		if e != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
			t.Fatal("retry", e)
		}
		for _, change := range []func(*Batch){func(b *Batch) { b.Events[0].Resource.Buffer.Absorbed++ }, func(b *Batch) { b.Events[0].Resource.Buffer.Before.Current++ }, func(b *Batch) { b.Events[0].Resource.Buffer = nil }} {
			history := w.History()
			change(&history[0])
			if out, e := Replay(b, history); e == nil || out != nil {
				t.Fatal("tampered buffer accepted")
			}
		}
	}
}
func TestBufferGrantRestoreAndRestrictions(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		n, want int64
	}{{"grant", 3, 5}, {"grant", 5, 5}, {"grant", 9, 9}, {"grant", math.MaxInt64, math.MaxInt64}, {"restore", 0, 0}} {
		w := bufferWorld(t, bufferFixture(t))
		r, e := w.Execute(inventoryRequest("r", 0, bufferOp(tc.kind, tc.n)))
		if e != nil || len(r.Events) != 1 || w.Snapshot().Characters[0].Resources["temp"].Current != tc.want {
			t.Fatal(r, e)
		}
		if tc.kind == "grant" && r.Events[0].Kind != ResourceGranted {
			t.Fatal(r)
		}
	}
	for _, tc := range []struct {
		kind, code string
		n          int64
	}{{"heal", "E_RULE", 1}, {"spend", "E_RULE", 1}, {"expand", "E_RULE", 1}, {"grant", "E_AMOUNT", 0}, {"grant", "E_AMOUNT", -1}} {
		w := bufferWorld(t, bufferFixture(t))
		before := w.ExportCheckpoint()
		_, e := w.Execute(inventoryRequest("r", 0, bufferOp("damage", 8), bufferOp(tc.kind, tc.n)))
		inventoryCode(t, e, tc.code)
		if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("partial publication")
		}
		if _, e = w.Execute(inventoryRequest("r", 0, bufferOp("grant", 9))); e != nil {
			t.Fatal(e)
		}
	}
	b := bufferFixture(t)
	r := b.Characters[0].Resources["temp"]
	r.Maximum = 5
	b.Characters[0].Resources["temp"] = r
	w := bufferWorld(t, b)
	_, e := w.Execute(inventoryRequest("r", 0, bufferOp("grant", 6)))
	inventoryCode(t, e, "E_BOUNDS")
	_, e = w.Execute(inventoryRequest("r", 0, Operation{Grant: &ResourceOp{Character: "a", Resource: "hp", Amount: 1}}))
	inventoryCode(t, e, "E_RULE")
	_, e = w.Execute(inventoryRequest("r", 0, Operation{Grant: &ResourceOp{Character: "a", Resource: "missing", Amount: 1}}))
	inventoryCode(t, e, "E_RESOURCE")
	op := bufferOp("grant", 1)
	op.Heal = &ResourceOp{Character: "a", Resource: "hp", Amount: 1}
	_, e = w.Execute(inventoryRequest("r", 0, op))
	inventoryCode(t, e, "E_IR")
}
func TestBufferInvalidDeclarationsAndDetachment(t *testing.T) {
	for _, relations := range [][]ResourceBuffer{{{"missing", "hp"}}, {{"temp", "missing"}}, {{"temp", "temp"}}, {{"temp", "hp"}, {"temp", "hp"}}, {{"temp", "hp"}, {"hp", "temp"}}} {
		b := bufferFixture(t)
		b.Characters[0].Buffers = relations
		_, e := NewWorld(b)
		inventoryCode(t, e, "E_BUFFER")
	}
	b := bufferFixture(t)
	r := b.Characters[0].Resources["temp"]
	r.Minimum = -1
	b.Characters[0].Resources["temp"] = r
	_, e := NewWorld(b)
	inventoryCode(t, e, "E_BUFFER")
	b = bufferFixture(t)
	w := bufferWorld(t, b)
	before := w.ExportCheckpoint()
	b.Characters[0].Buffers[0].Pool = "forged"
	s := w.Snapshot()
	s.Characters[0].Buffers[0].Target = "forged"
	p, e := w.CharacterStatus("a")
	if e != nil {
		t.Fatal(e)
	}
	p.Buffers[0].Pool = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("buffer alias")
	}
	request := inventoryRequest("r", 0, bufferOp("grant", 9), bufferOp("damage", 10))
	receipt, e := w.Execute(request)
	if e != nil {
		t.Fatal(e)
	}
	canonical := w.ExportCheckpoint()
	request.Operations[0].Grant.Amount = 99
	receipt.Events[1].Resource.Buffer.After.Current = 99
	history := w.History()
	history[0].Events[1].Resource.Buffer.Before.Current = 99
	if !reflect.DeepEqual(canonical, w.ExportCheckpoint()) {
		t.Fatal("receipt alias")
	}
}
func TestBufferMinimumConditionCleanup(t *testing.T) {
	for _, kind := range []string{"damage", "restore"} {
		b := bufferFixture(t)
		pool := ResourceID("temp")
		b.ConditionDefinitions = []ConditionDefinition{{ID: "guard", Instances: Coexist, EndsAtMinimum: &pool}}
		b.Conditions = []ConditionInstance{{ID: "guard-one", Type: "guard", Target: "a"}}
		w := bufferWorld(t, b)
		before := w.ExportCheckpoint()
		_, e := w.Execute(inventoryRequest("r", 0, bufferOp(kind, 8), bufferOp("heal", 1)))
		inventoryCode(t, e, "E_RULE")
		if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("cleanup rollback")
		}
		r, e := w.Execute(inventoryRequest("r", 0, bufferOp(kind, 8)))
		if e != nil || len(r.Events) != 2 || r.Events[0].Kind != ConditionRemoved || *r.Events[0].Condition.MinimumResource != "temp" || len(w.Snapshot().Conditions) != 0 {
			t.Fatal(r, e)
		}
		rebuilt, e := Replay(b, w.History())
		if e != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("cleanup replay", e)
		}
	}
}
func TestBufferOrdinaryHealingAndRestoreDoNotFillPool(t *testing.T) {
	w := bufferWorld(t, bufferFixture(t))
	_, e := w.Execute(inventoryRequest("hit", 0, bufferOp("damage", 8)))
	if e != nil {
		t.Fatal(e)
	}
	_, e = w.Execute(inventoryRequest("heal", 1, Operation{Heal: &ResourceOp{Character: "a", Resource: "hp", Amount: 1}}))
	if e != nil || w.Snapshot().Characters[0].Resources["hp"].Current != 18 || w.Snapshot().Characters[0].Resources["temp"].Current != 0 {
		t.Fatal(e)
	}
	_, e = w.Execute(inventoryRequest("rest", 2, Operation{Restore: &RestoreResource{Character: "a", Resource: "hp"}}))
	if e != nil || w.Snapshot().Characters[0].Resources["hp"].Current != 20 || w.Snapshot().Characters[0].Resources["temp"].Current != 0 {
		t.Fatal(e)
	}
}
