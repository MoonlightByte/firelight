package core_test

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

// ammoFixture is a shooter, a with arrows and bolts, beside b, in a world that
// recovers half of what was spent, rounded down.
func ammoFixture() core.Blueprint {
	twenty, ten := int64(20), int64(10)
	a := core.Ref{Kind: core.CharacterKind, ID: "a"}
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny, RecoveryNumerator: 1, RecoveryDenominator: 2}, Locations: []core.Location{{ID: "field", Name: "Field"}}}
	b.Characters = []core.Character{{ID: "a", Name: "A", Location: "field"}, {ID: "b", Name: "B", Location: "field"}}
	b.Items = []core.Item{{ID: "arrows", Name: "Arrows", Owner: "a", Custody: &a, Quantity: &twenty}, {ID: "bolts", Name: "Bolts", Owner: "a", Custody: &a, Quantity: &ten}}
	return b
}

func consumeOp(item core.ItemID, amount int64, recoverable bool) core.Operation {
	return core.Operation{Consume: &core.ConsumeItem{Item: item, Owner: "a", Amount: amount, Recoverable: recoverable}}
}

func recoverOp(item core.ItemID, amount *int64) core.Operation {
	return core.Operation{RecoverItem: &core.RecoverItem{Item: item, Owner: "a", Amount: amount}}
}

// stock checks an item's quantity and recoverable count.
func stock(t *testing.T, w *core.World, id core.ItemID, quantity, recoverable int64) {
	t.Helper()
	for _, i := range w.Snapshot().Items {
		if i.ID == id {
			if i.Quantity == nil || *i.Quantity != quantity || i.Recoverable != recoverable {
				t.Fatalf("%s = %+v, want %d recoverable %d", id, i, quantity, recoverable)
			}
			return
		}
	}
	t.Fatal("no item", id)
}

func TestRecoverItem(t *testing.T) {
	w, err := core.NewWorld(ammoFixture())
	if err != nil {
		t.Fatal(err)
	}
	batch := execute(t, w, "shoot", consumeOp("arrows", 5, true))
	if c := batch.Events[0].Consumption; batch.Events[0].Kind != core.ItemConsumed || !c.Recoverable || c.After != 15 {
		t.Fatalf("%+v", batch.Events)
	}
	stock(t, w, "arrows", 15, 5)
	// Plain consumption spends nothing recoverable.
	execute(t, w, "snap", consumeOp("arrows", 2, false))
	stock(t, w, "arrows", 13, 5)
	execute(t, w, "shoot again", consumeOp("arrows", 2, true))
	stock(t, w, "arrows", 11, 7)
	// Half of 7, rounded down.
	batch = execute(t, w, "gather", recoverOp("arrows", nil))
	eleven := int64(11)
	if e := batch.Events; len(e) != 1 || e[0].Kind != core.ItemRecovered || !reflect.DeepEqual(*e[0].ItemRecovery, core.ItemRecovery{Item: "arrows", Owner: "a", Spent: 7, Allowed: 3, Before: &eleven, After: 14}) {
		t.Fatalf("%+v", e[0].ItemRecovery)
	}
	stock(t, w, "arrows", 14, 0)
	// The agent may take less, and 0 forfeits the share; the count resets.
	execute(t, w, "bolts", consumeOp("bolts", 3, true))
	zero := int64(0)
	batch = execute(t, w, "fled", recoverOp("bolts", &zero))
	if v := batch.Events[0].ItemRecovery; v.Allowed != 1 || *v.Requested != 0 || v.After != 7 {
		t.Fatalf("%+v", v)
	}
	stock(t, w, "bolts", 7, 0)
	// A depleted stock keeps its identity and recovers into it.
	execute(t, w, "empty", consumeOp("bolts", 7, true))
	stock(t, w, "bolts", 0, 7)
	two := int64(2)
	execute(t, w, "some", recoverOp("bolts", &two))
	stock(t, w, "bolts", 2, 0)
	// One spent unit has no half to recover.
	execute(t, w, "one", consumeOp("bolts", 1, true))
	execute(t, w, "lost", recoverOp("bolts", nil))
	stock(t, w, "bolts", 1, 0)
	replayAndSave(t, ammoFixture(), w)
}

func TestRecoverItemRefusals(t *testing.T) {
	b := ammoFixture()
	one, max := int64(1), int64(math.MaxInt64)
	b.Items = append(b.Items,
		core.Item{ID: "full", Name: "Full", Owner: "a", Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}, Quantity: &max, Recoverable: 4},
		core.Item{ID: "quiver", Name: "Quiver", Owner: "a", Wearer: "a", Quantity: &one, Recoverable: 2},
		core.Item{ID: "cache", Name: "Cache", Owner: "a", Custody: &core.Ref{Kind: core.LocationKind, ID: "field"}, Quantity: &one, Recoverable: 2},
		core.Item{ID: "worn out", Name: "Worn out", Owner: "a", Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}, Quantity: &one, Recoverable: math.MaxInt64},
	)
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	minus, three := int64(-1), int64(3)
	refused(t, w, "E_QUANTITY", "spent recoverable units", recoverOp("arrows", nil))
	refused(t, w, "E_AMOUNT", "nonnegative int64", recoverOp("arrows", &minus))
	refused(t, w, "E_REFERENCE", "", recoverOp("sling", nil))
	refused(t, w, "E_OWNER", "", core.Operation{RecoverItem: &core.RecoverItem{Item: "arrows", Owner: "b"}})
	refused(t, w, "E_WORN", "unworn item", recoverOp("quiver", nil))
	refused(t, w, "E_CUSTODY", "", recoverOp("cache", nil))
	refused(t, w, "E_QUANTITY", "sum within int64", recoverOp("full", nil))
	refused(t, w, "E_QUANTITY", "recoverable count within int64", consumeOp("worn out", 1, true))
	refused(t, w, "E_IR", "", core.Operation{RecoverItem: &core.RecoverItem{Item: "arrows", Owner: "a"}, Consume: &core.ConsumeItem{Item: "arrows", Owner: "a", Amount: 1}})
	execute(t, w, "shoot", consumeOp("arrows", 5, true))
	refused(t, w, "E_QUANTITY", "at most 2 units", recoverOp("arrows", &three))
	// A world without the rule recovers nothing.
	b = ammoFixture()
	b.Rules.RecoveryNumerator, b.Rules.RecoveryDenominator = 0, 0
	if w, err = core.NewWorld(b); err != nil {
		t.Fatal(err)
	}
	refused(t, w, "E_RULE", "world recovery rule", consumeOp("arrows", 1, true))
	refused(t, w, "E_RULE", "world recovery rule", recoverOp("arrows", nil))
	execute(t, w, "plain", consumeOp("arrows", 1, false))
}

func TestRecoveryRuleValidation(t *testing.T) {
	for _, tc := range []struct {
		name, code, field, expected string
		edit                        func(*core.Blueprint)
	}{
		{"above one", "E_RULE", "recovery", "", func(b *core.Blueprint) { b.Rules.RecoveryNumerator = 3 }},
		{"negative", "E_RULE", "recovery", "", func(b *core.Blueprint) { b.Rules.RecoveryNumerator = -1 }},
		{"no denominator", "E_RULE", "recovery", "", func(b *core.Blueprint) { b.Rules.RecoveryDenominator = 0 }},
		{"large denominator", "E_RULE", "recovery", "", func(b *core.Blueprint) { b.Rules.RecoveryDenominator = 1001 }},
		{"negative count", "E_QUANTITY", "recoverable", "nonnegative int64", func(b *core.Blueprint) { b.Items[0].Recoverable = -1 }},
		{"count without rule", "E_RULE", "recoverable", "world recovery rule", func(b *core.Blueprint) {
			b.Rules.RecoveryNumerator, b.Rules.RecoveryDenominator = 0, 0
			b.Items[0].Recoverable = 1
		}},
		{"container", "E_RULE", "recoverable", "stock, not a container", func(b *core.Blueprint) {
			b.Items[0].Container, b.Items[0].Quantity, b.Items[0].Custody, b.Items[0].Recoverable = true, nil, &core.Ref{Kind: core.LocationKind, ID: "field"}, 1
		}},
	} {
		b := ammoFixture()
		tc.edit(&b)
		_, err := core.NewWorld(b)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != tc.code || f.Field != tc.field || tc.expected != "" && f.Expected != tc.expected {
			t.Fatal(tc.name, err)
		}
	}
	// Battle keeps its own ammunition record.
	battle := turnResourceFixture()
	battle.Rules.RecoveryNumerator, battle.Rules.RecoveryDenominator = 1, 2
	var f *core.Fault
	if _, err := core.NewWorld(battle); !errors.As(err, &f) || f.Code != "E_RULE" || f.Field != "recovery" || f.Expected != "a world without Battle" {
		t.Fatal(err)
	}
	// 0 of n is a rule: everything spent is lost.
	b := ammoFixture()
	b.Rules.RecoveryNumerator = 0
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, w, "shoot", consumeOp("arrows", 5, true))
	execute(t, w, "gone", recoverOp("arrows", nil))
	stock(t, w, "arrows", 15, 0)
	// A created stock may carry a count.
	four := int64(4)
	execute(t, w, "found", core.Operation{CreateItem: &core.Item{ID: "found", Name: "Found", Owner: "a", Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}, Quantity: &four, Recoverable: 2}})
	refused(t, w, "E_QUANTITY", "nonnegative int64", core.Operation{CreateItem: &core.Item{ID: "bad", Name: "Bad", Owner: "a", Quantity: &four, Recoverable: -2}})
}

// Split leaves the count on the original stock; transfer carries it, and the
// new owner recovers once the stock is in their custody.
func TestRecoverableSplitAndTransfer(t *testing.T) {
	w, err := core.NewWorld(ammoFixture())
	if err != nil {
		t.Fatal(err)
	}
	execute(t, w, "shoot", consumeOp("arrows", 4, true))
	batch := execute(t, w, "share", core.Operation{Split: &core.SplitItem{Item: "arrows", Created: "arrows:b", Amount: 6, Handler: "a"}})
	if s := batch.Events[0].StockSplit; s.Created.Recoverable != 0 || s.After.Recoverable != 4 {
		t.Fatalf("%+v", s)
	}
	stock(t, w, "arrows", 10, 4)
	stock(t, w, "arrows:b", 6, 0)
	a, b := core.Ref{Kind: core.CharacterKind, ID: "a"}, core.Ref{Kind: core.CharacterKind, ID: "b"}
	execute(t, w, "hand over", core.Operation{Transfer: &core.Transfer{Item: "arrows", From: "a", To: "b"}})
	stock(t, w, "arrows", 10, 4)
	refused(t, w, "E_CUSTODY", "", core.Operation{RecoverItem: &core.RecoverItem{Item: "arrows", Owner: "b"}})
	execute(t, w, "pass", core.Operation{Place: &core.PlaceItem{Item: "arrows", From: a, To: b, Handler: "a"}})
	execute(t, w, "gather", core.Operation{RecoverItem: &core.RecoverItem{Item: "arrows", Owner: "b"}})
	stock(t, w, "arrows", 12, 0)
	replayAndSave(t, ammoFixture(), w)
}

// The caller's request, receipt and history copies are detached from the world.
func TestRecoverItemDetachment(t *testing.T) {
	w, err := core.NewWorld(ammoFixture())
	if err != nil {
		t.Fatal(err)
	}
	execute(t, w, "shoot", consumeOp("arrows", 6, true))
	amount, one := int64(1), int64(1)
	request := core.Request{ID: "gather", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{recoverOp("arrows", &amount)}}
	receipt, err := w.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	*request.Operations[0].RecoverItem.Amount = 3
	*receipt.Events[0].ItemRecovery.Requested = 3
	*receipt.Events[0].ItemRecovery.Before = 99
	history := w.History()
	*history[1].Request.Operations[0].RecoverItem.Amount = 2
	*history[1].Events[0].ItemRecovery.Requested = 2
	*history[1].Events[0].ItemRecovery.Before = 98
	h := w.History()[1]
	fourteen := int64(14)
	if *h.Request.Operations[0].RecoverItem.Amount != 1 || !reflect.DeepEqual(*h.Events[0].ItemRecovery, core.ItemRecovery{Item: "arrows", Owner: "a", Requested: &one, Spent: 6, Allowed: 3, Before: &fourteen, After: 15}) {
		t.Fatalf("%+v", h.Events[0].ItemRecovery)
	}
	replayAndSave(t, ammoFixture(), w)
}
