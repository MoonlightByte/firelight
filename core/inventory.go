package core

import (
	"fmt"
	"math"
)

// ConsumeItem debits physical stock exactly. Ownership is a precondition, not
// authorization or proof of physical custody. Depletion retains the entity ID.
type ConsumeItem struct {
	Item   ItemID      `json:"item"`
	Owner  CharacterID `json:"owner"`
	Amount int64       `json:"amount"`
	// Recoverable adds the units to the stock's recoverable count, for
	// ammunition that can be picked up again after the fight.
	Recoverable bool `json:"recoverable,omitempty"`
}

// AddItem credits existing physical stock without changing identity or ownership.
type AddItem struct {
	Item   ItemID      `json:"item"`
	Owner  CharacterID `json:"owner"`
	Amount int64       `json:"amount"`
}

func itemQuantity(i Item) int64 {
	if i.Quantity == nil {
		return 1
	}
	return *i.Quantity
}
func quantityFault(i Item, expected, message string) error {
	return fail("E_QUANTITY", string(i.ID), "quantity", expected, fmt.Sprint(itemQuantity(i)), message)
}
func checkItemQuantity(i Item) error {
	if itemQuantity(i) < 0 {
		return quantityFault(i, "nonnegative int64", "item quantity cannot be negative")
	}
	if i.Wearer != "" && itemQuantity(i) != 1 {
		return quantityFault(i, "exactly one worn unit", "only a single unit may be worn")
	}
	return nil
}
func consumeItem(b *Blueprint, v ConsumeItem) ([]Event, error) {
	i := item(b, v.Item)
	if i.Container || (i.Custody != nil && !carriedBy(*i, v.Owner)) {
		return nil, custodyFault(i.ID, "stock carried by stated owner", fmt.Sprint(i.Custody), "known custody does not permit consumption")
	}
	if i.Owner != v.Owner {
		return nil, fail("E_OWNER", string(i.ID), "owner", string(i.Owner), string(v.Owner), "stated owner is not the current owner")
	}
	if i.Wearer != "" {
		return nil, fail("E_WORN", string(i.ID), "wearer", "unworn item", string(i.Wearer), "explicit unequip required before consumption")
	}
	if itemQuantity(*i) < v.Amount {
		return nil, quantityFault(*i, fmt.Sprintf("at least %d units", v.Amount), "insufficient stock for exact consumption")
	}
	if v.Recoverable && i.Charges != nil {
		return nil, chargedStockFault(*i, "consumed as recoverable stock")
	}
	if v.Recoverable && i.Recoverable > math.MaxInt64-v.Amount {
		return nil, fail("E_QUANTITY", string(i.ID), "recoverable", "recoverable count within int64", fmt.Sprint(i.Recoverable), "recoverable count would overflow")
	}
	change := &ConsumptionChange{Item: i.ID, Owner: i.Owner, Requested: v.Amount,
		Before: copyPtr(i.Quantity), After: itemQuantity(*i) - v.Amount, Recoverable: v.Recoverable}
	i.Quantity = copyPtr(&change.After)
	if v.Recoverable {
		i.Recoverable += v.Amount
	}
	return []Event{{Kind: ItemConsumed, Consumption: change}}, nil
}

func addItem(b *Blueprint, v AddItem) ([]Event, error) {
	i := item(b, v.Item)
	if i.Charges != nil {
		return nil, chargedStockFault(*i, "added to")
	}
	if i.Owner != v.Owner {
		return nil, fail("E_OWNER", string(i.ID), "owner", string(i.Owner), string(v.Owner), "stated owner is not the current owner")
	}
	if i.Container || (i.Custody != nil && !carriedBy(*i, v.Owner)) {
		return nil, custodyFault(i.ID, "stock carried by stated owner", fmt.Sprint(i.Custody), "known custody does not permit addition")
	}
	if i.Wearer != "" {
		return nil, fail("E_WORN", string(i.ID), "wearer", "unworn item", string(i.Wearer), "explicit unequip required before addition")
	}
	if v.Amount > math.MaxInt64-itemQuantity(*i) {
		return nil, quantityFault(*i, "sum within int64", "stock addition would overflow")
	}
	change := &ConsumptionChange{Item: i.ID, Owner: i.Owner, Requested: v.Amount,
		Before: copyPtr(i.Quantity), After: itemQuantity(*i) + v.Amount}
	i.Quantity = copyPtr(&change.After)
	return []Event{{Kind: ItemAdded, Addition: change}}, nil
}
