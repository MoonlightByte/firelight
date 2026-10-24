package core

import (
	"fmt"
	"math/big"
)

// CapacityChange records permanent growth separately from a current-value
// mutation. Expanding capacity never restores or heals a spent Resource.
type CapacityChange struct {
	Formula   *CapacityFormulaResult `json:"formula,omitempty"`
	Character CharacterID            `json:"character"`
	Resource  ResourceID             `json:"resource"`
	Requested int64                  `json:"requested"`
	Before    Resource               `json:"before"`
	After     Resource               `json:"after"`
}

func expandResource(b *Blueprint, v ResourceOp) (Event, error) {
	c := character(b, v.Character)
	if c.bufferPool(v.Resource) {
		return Event{}, bufferRule(v.Character, v.Resource, "buffer pools cannot be expanded")
	}
	before := c.Resources[v.Resource]
	maximum := new(big.Int).Add(big.NewInt(before.Maximum), big.NewInt(v.Amount))
	if !maximum.IsInt64() {
		return Event{}, fail("E_OVERFLOW", string(v.Character), string(v.Resource), "maximum + requested fits int64", fmt.Sprintf("maximum=%d requested=%d", before.Maximum, v.Amount), "resource capacity cannot represent the requested growth")
	}
	// Active Condition drains give their amount back when they end.
	if _, drains := conditionMaximumTotals(b, v.Character, v.Resource); drains > headroom(maximum.Int64()) {
		return Event{}, fail("E_OVERFLOW", string(v.Character), string(v.Resource), "maximum + requested + active drains fits int64", fmt.Sprintf("maximum=%d requested=%d drains=%d", before.Maximum, v.Amount, drains), "resource capacity cannot represent the requested growth")
	}
	after := before
	after.Maximum = maximum.Int64()
	c.Resources[v.Resource] = after
	return Event{Kind: ResourceExpanded, Capacity: &CapacityChange{Character: v.Character, Resource: v.Resource, Requested: v.Amount, Before: before, After: after}}, nil
}
