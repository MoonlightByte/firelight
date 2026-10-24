package core

import (
	"fmt"
	"math/big"
)

// AdjustStat changes a non-derived stat's declared base by a signed amount. It
// is for a running count such as experience points: the new base is the fact a
// host stores, and every total that reads the stat follows.
type AdjustStat struct {
	Character CharacterID `json:"character"`
	Stat      StatID      `json:"stat"`
	Amount    int64       `json:"amount"`
}

// StatAdjustment records one declared base before and after an adjustment.
type StatAdjustment struct {
	Character CharacterID `json:"character"`
	Stat      StatID      `json:"stat"`
	Amount    int64       `json:"amount"`
	Before    int64       `json:"before"`
	After     int64       `json:"after"`
}

func (b Blueprint) checkAdjustStat(v AdjustStat) error {
	if v.Amount == 0 {
		return fail("E_AMOUNT", string(v.Character), string(v.Stat), "nonzero signed int64", "zero", "stat adjustment must be nonzero")
	}
	if err := b.CheckStat(v.Character, v.Stat); err != nil {
		return err
	}
	// An agent never writes a total: a derived stat's base stays declared.
	if b.derivedRule(v.Stat) != nil {
		return fail("E_RULE", string(v.Character), string(v.Stat), "non-derived stat", "derived", "a derived total cannot be adjusted")
	}
	return nil
}

func adjustStat(b *Blueprint, v AdjustStat) ([]Event, error) {
	c := character(b, v.Character)
	before := c.Stats[v.Stat]
	after := new(big.Int).Add(big.NewInt(before), big.NewInt(v.Amount))
	if !after.IsInt64() {
		return nil, fail("E_OVERFLOW", string(v.Character), string(v.Stat), "base + amount fits int64", fmt.Sprintf("base=%d amount=%d", before, v.Amount), "stat base cannot represent the adjustment")
	}
	c.Stats[v.Stat] = after.Int64()
	return []Event{{Kind: StatAdjusted, StatAdjustment: &StatAdjustment{Character: v.Character, Stat: v.Stat, Amount: v.Amount, Before: before, After: after.Int64()}}}, nil
}
