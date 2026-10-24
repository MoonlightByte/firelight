package core

import (
	"fmt"
	"math"
	"math/big"
)

// MaxRecoveryDenominator bounds the world's recovery share, as Battle bounds
// its own.
const MaxRecoveryDenominator = 1000

// RecoverItem returns part of what a stock has spent since its last recovery,
// the share the world's recovery rule allows: the SRD recovers half the
// ammunition used in a fight, rounded down. Amount states less than the share,
// 0 forfeiting it; without it the stock takes the whole share. Either way the
// stock's spent count starts again from 0.
type RecoverItem struct {
	Item   ItemID      `json:"item"`
	Owner  CharacterID `json:"owner"`
	Amount *int64      `json:"amount,omitempty"`
}

// ItemRecovery records one recovery: the spent count it closes, the share the
// rule allows from it, and the stock before and after.
type ItemRecovery struct {
	Item      ItemID      `json:"item"`
	Owner     CharacterID `json:"owner"`
	Requested *int64      `json:"requested,omitempty"`
	Spent     int64       `json:"spent"`
	Allowed   int64       `json:"allowed"`
	Before    *int64      `json:"before"`
	After     int64       `json:"after"`
}

func cloneRecoverItem(v *RecoverItem) *RecoverItem {
	if v == nil {
		return nil
	}
	out := *v
	out.Amount = copyPtr(v.Amount)
	return &out
}
func cloneItemRecovery(v *ItemRecovery) *ItemRecovery {
	if v == nil {
		return nil
	}
	out := *v
	out.Requested, out.Before = copyPtr(v.Requested), copyPtr(v.Before)
	return &out
}

func (b Blueprint) recoveryRule() bool { return b.Rules.RecoveryDenominator != 0 }

func (b Blueprint) checkRecoveryRule() error {
	r := b.Rules
	if r.RecoveryNumerator == 0 && r.RecoveryDenominator == 0 {
		return nil
	}
	if r.RecoveryDenominator < 1 || r.RecoveryDenominator > MaxRecoveryDenominator || r.RecoveryNumerator < 0 || r.RecoveryNumerator > r.RecoveryDenominator {
		return fail("E_RULE", "", "recovery", "numerator of denominator, 0 <= numerator <= denominator, 1 <= denominator <= 1000", fmt.Sprintf("%d of %d", r.RecoveryNumerator, r.RecoveryDenominator), "invalid recovery rule")
	}
	// A Battle keeps its own ammunition record; one world has one recovery.
	if b.Battle != nil {
		return fail("E_RULE", "", "recovery", "a world without Battle", "battle", "Battle recovers ammunition with battle recover")
	}
	return nil
}

func noRecoveryRule(subject ItemID, field string) error {
	return fail("E_RULE", string(subject), field, "world recovery rule", "none", "the world declares no recovery rule")
}

func (b Blueprint) checkRecoverable(i Item) error {
	if i.Recoverable == 0 {
		return nil
	}
	if i.Recoverable < 0 {
		return fail("E_QUANTITY", string(i.ID), "recoverable", "nonnegative int64", fmt.Sprint(i.Recoverable), "recoverable count cannot be negative")
	}
	if !b.recoveryRule() {
		return noRecoveryRule(i.ID, "recoverable")
	}
	if i.Container {
		return fail("E_RULE", string(i.ID), "recoverable", "stock, not a container", "container", "a container spends no stock")
	}
	return nil
}

func (b Blueprint) checkRecoverItem(v RecoverItem) error {
	if !b.recoveryRule() {
		return noRecoveryRule(v.Item, "recovery")
	}
	if v.Amount != nil && *v.Amount < 0 {
		return fail("E_AMOUNT", string(v.Item), "quantity", "nonnegative int64", fmt.Sprint(*v.Amount), "recovery amount cannot be negative")
	}
	for _, r := range []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.Owner)}} {
		if err := b.CheckRef(r); err != nil {
			return err
		}
	}
	return nil
}

// recoveryShare is floor(spent * numerator / denominator); it never exceeds
// spent, so it fits int64.
func (b Blueprint) recoveryShare(spent int64) int64 {
	n := new(big.Int).Mul(big.NewInt(spent), big.NewInt(b.Rules.RecoveryNumerator))
	return n.Quo(n, big.NewInt(b.Rules.RecoveryDenominator)).Int64()
}

func recoverItem(b *Blueprint, v RecoverItem) ([]Event, error) {
	i := item(b, v.Item)
	if i.Owner != v.Owner {
		return nil, fail("E_OWNER", string(i.ID), "owner", string(i.Owner), string(v.Owner), "stated owner is not the current owner")
	}
	if i.Container || (i.Custody != nil && !carriedBy(*i, v.Owner)) {
		return nil, custodyFault(i.ID, "stock carried by stated owner", fmt.Sprint(i.Custody), "known custody does not permit recovery")
	}
	if i.Wearer != "" {
		return nil, fail("E_WORN", string(i.ID), "wearer", "unworn item", string(i.Wearer), "explicit unequip required before recovery")
	}
	if i.Recoverable == 0 {
		return nil, fail("E_QUANTITY", string(i.ID), "recoverable", "spent recoverable units", "0", "the stock has spent nothing recoverable")
	}
	allowed := b.recoveryShare(i.Recoverable)
	amount := allowed
	if v.Amount != nil {
		if *v.Amount > allowed {
			return nil, fail("E_QUANTITY", string(i.ID), "recoverable", fmt.Sprintf("at most %d units", allowed), fmt.Sprint(*v.Amount), "recovery exceeds the rule's share")
		}
		amount = *v.Amount
	}
	if amount > math.MaxInt64-itemQuantity(*i) {
		return nil, quantityFault(*i, "sum within int64", "stock recovery would overflow")
	}
	change := &ItemRecovery{Item: i.ID, Owner: i.Owner, Requested: copyPtr(v.Amount), Spent: i.Recoverable, Allowed: allowed,
		Before: copyPtr(i.Quantity), After: itemQuantity(*i) + amount}
	i.Quantity = copyPtr(&change.After)
	i.Recoverable = 0
	return []Event{{Kind: ItemRecovered, ItemRecovery: change}}, nil
}
