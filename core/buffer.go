package core

import "fmt"

// ResourceBuffer is an immutable, character-local damage routing declaration.
type ResourceBuffer struct {
	Pool   ResourceID `json:"pool"`
	Target ResourceID `json:"target"`
}

// BufferChange records absorption alongside the target's ordinary resource change.
type BufferChange struct {
	Resource ResourceID `json:"resource"`
	Before   Resource   `json:"before"`
	After    Resource   `json:"after"`
	Absorbed int64      `json:"absorbed"`
}

func (c Character) bufferPool(id ResourceID) bool {
	for _, v := range c.Buffers {
		if v.Pool == id {
			return true
		}
	}
	return false
}
func bufferRule(c CharacterID, id ResourceID, message string) error {
	return fail("E_RULE", string(c), string(id), "operation compatible with buffer policy", "buffer pool", message)
}
func (b Blueprint) checkBuffers(c Character) error {
	pools, targets := map[ResourceID]bool{}, map[ResourceID]bool{}
	for _, v := range c.Buffers {
		for _, id := range []ResourceID{v.Pool, v.Target} {
			if _, ok := c.Resources[id]; !ok {
				return fail("E_BUFFER", string(c.ID), string(id), "declared resource", "missing", "buffer endpoint does not exist")
			}
		}
		if v.Pool == v.Target || pools[v.Pool] || targets[v.Target] || pools[v.Target] || targets[v.Pool] {
			return fail("E_BUFFER", string(c.ID), string(v.Pool), "distinct disjoint pool and target pairs", "ambiguous relation", "duplicate, chained or cyclic buffer relation")
		}
		if c.Resources[v.Pool].Minimum != 0 {
			return fail("E_BUFFER", string(c.ID), string(v.Pool), "pool minimum zero", fmt.Sprint(c.Resources[v.Pool].Minimum), "buffer minimum must be zero")
		}
		if b.Battle != nil {
			reserved := []ResourceID{b.Battle.Rules.Health}
			reserved = append(reserved, b.Battle.Rules.TurnResources...)
			for _, a := range b.Battle.Rules.Allowances {
				if a.Resource == v.Target {
					return fail("E_BUFFER", string(c.ID), string(v.Target), "target independent of Battle allowance resources", "reserved resource", "buffer would absorb automatic allowance clearing")
				}
				reserved = append(reserved, a.Resource)
			}
			for _, id := range reserved {
				if id == v.Pool {
					return fail("E_BUFFER", string(c.ID), string(v.Pool), "pool independent of Battle health/turn resources", "reserved resource", "buffer conflicts with Battle resource lifecycle")
				}
			}
		}
		pools[v.Pool], targets[v.Target] = true, true
	}
	return nil
}
func grantResource(b *Blueprint, v ResourceOp) ([]Event, error) {
	c := character(b, v.Character)
	if !c.bufferPool(v.Resource) {
		return nil, bufferRule(v.Character, v.Resource, "grant requires a declared buffer pool")
	}
	before := c.Resources[v.Resource]
	after := before
	if v.Amount > before.Maximum {
		return nil, fail("E_BOUNDS", string(v.Character), string(v.Resource), "grant <= declared maximum", fmt.Sprint(v.Amount), "grant exceeds buffer representation bound")
	}
	after.Current = max(before.Current, v.Amount)
	c.Resources[v.Resource] = after
	return []Event{{Kind: ResourceGranted, Resource: &ResourceChange{Character: v.Character, Resource: v.Resource, Requested: v.Amount, Before: before, After: after}}}, nil
}
