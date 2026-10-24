package core

import "fmt"

// SplitItem separates a strict part of one stock. The candidate supplies all
// new Item properties; callers supply intent, never a replacement postimage.
type SplitItem struct {
	Item    ItemID      `json:"item"`
	Created ItemID      `json:"created"`
	Amount  int64       `json:"amount"`
	Handler CharacterID `json:"handler"`
}

type StockSplit struct {
	Before    Item        `json:"before"`
	After     Item        `json:"after"`
	Created   Item        `json:"created"`
	Requested int64       `json:"requested"`
	Handler   CharacterID `json:"handler"`
}

func (b Blueprint) checkItemLineage(i Item) error {
	return checkItemLineage(i, b.KindOf, func(id ItemID) *Item { return item(&b, id) })
}
func checkItemLineage(i Item, kindOf func(string) (EntityKind, bool), itemOf func(ItemID) *Item) error {
	seen := map[ItemID]bool{i.ID: true}
	for parent := i.SplitFrom; parent != ""; parent = itemOf(parent).SplitFrom {
		if seen[parent] {
			return fail("E_REFERENCE", string(i.ID), "split_from", "acyclic stock ancestry", string(parent), "cyclic stock provenance")
		}
		if err := checkRef(Ref{ItemKind, string(parent)}, kindOf); err != nil {
			return err
		}
		seen[parent] = true
	}
	return nil
}

func cloneStockSplit(v *StockSplit) *StockSplit {
	if v == nil {
		return nil
	}
	s := *v
	s.Before, s.After, s.Created = *cloneItem(&v.Before), *cloneItem(&v.After), *cloneItem(&v.Created)
	return &s
}

func (b Blueprint) checkSplit(v SplitItem) error {
	for _, r := range []struct {
		field string
		ref   Ref
	}{
		{"item", Ref{ItemKind, string(v.Item)}}, {"handler", Ref{CharacterKind, string(v.Handler)}},
	} {
		if err := b.CheckRef(r.ref); err != nil {
			if f, ok := err.(*Fault); ok {
				n := *f
				n.Field = r.field
				return &n
			}
			return err
		}
	}
	if !validID(string(v.Created)) {
		return fail("E_ID", string(v.Created), "created", "valid new Item ID", "invalid", "invalid split identity")
	}
	if _, exists := b.KindOf(string(v.Created)); exists {
		return fail("E_DUPLICATE", string(v.Created), "created", "unique canonical ID", string(v.Created), "split requires a fresh identity")
	}
	if v.Amount <= 0 {
		return fail("E_AMOUNT", string(v.Item), "quantity", "positive int64", fmt.Sprint(v.Amount), "split quantity must be positive")
	}
	return nil
}

func splitItem(b *Blueprint, v SplitItem) ([]Event, error) {
	i := item(b, v.Item)
	if i.Container || i.Custody == nil || custodyLocation(b, *i.Custody) != character(b, v.Handler).Location {
		return nil, custodyFault(i.ID, "movable stock with known custody at handler location", fmt.Sprint(i.Custody), "split requires known local stock")
	}
	if i.Charges != nil {
		return nil, chargedStockFault(*i, "split")
	}
	if i.Wearer != "" {
		return nil, fail("E_WORN", string(i.ID), "wearer", "unworn stock", string(i.Wearer), "explicit unequip required before splitting")
	}
	if v.Amount >= itemQuantity(*i) {
		return nil, quantityFault(*i, "strictly more units than requested", "use the original Item for a whole-stock placement")
	}
	for _, e := range b.Effects {
		if e.Target == (Ref{ItemKind, string(i.ID)}) {
			return nil, fail("E_RULE", string(i.ID), "effects", "stock without instance-owned worn effects", string(e.ID), "splitting cannot duplicate an instance-owned effect")
		}
	}
	before := *cloneItem(i)
	created := *cloneItem(i)
	// The spent count stays with the original stock.
	created.ID, created.Quantity, created.SplitFrom, created.Recoverable = v.Created, copyPtr(&v.Amount), v.Item, 0
	if err := b.checkNewItem(created); err != nil {
		return nil, err
	}
	remainder := itemQuantity(*i) - v.Amount // Positive operands and result: no overflow.
	i.Quantity = &remainder
	change := &StockSplit{Before: before, After: *cloneItem(i), Created: *cloneItem(&created), Requested: v.Amount, Handler: v.Handler}
	b.Items = append(b.Items, created)
	return []Event{{Kind: ItemSplit, StockSplit: change}}, nil
}
