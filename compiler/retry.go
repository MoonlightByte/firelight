package compiler

import "github.com/MoonlightByte/firelight/core"

// withoutCreated returns the catalog without the items, characters and
// occupants the actions create, each removed only from its own kind. The
// slices are new, so the caller's catalog is unchanged.
func withoutCreated(b core.Blueprint, nodes []actionNode) core.Blueprint {
	items, characters, occupants := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, n := range nodes {
		switch {
		case n.Item != nil:
			items[n.Item.ID.Text] = true
		case n.Split != nil:
			items[n.Split.Created.Text] = true
		case n.Battle != nil:
			for _, c := range n.Battle.Recovery {
				items[c.Created.Text] = true
			}
		case n.Character != nil:
			characters[n.Character.ID.Text] = true
		case n.Occupant != nil:
			occupants[n.Occupant.ID.Text] = true
		}
	}
	kept := make([]core.Item, 0, len(b.Items))
	for _, i := range b.Items {
		if !items[string(i.ID)] {
			kept = append(kept, i)
		}
	}
	people := make([]core.Character, 0, len(b.Characters))
	for _, c := range b.Characters {
		if !characters[string(c.ID)] {
			people = append(people, c)
		}
	}
	present := make([]core.Occupant, 0, len(b.Occupants))
	for _, o := range b.Occupants {
		if !occupants[string(o.ID)] {
			present = append(present, o)
		}
	}
	b.Items, b.Characters, b.Occupants = kept, people, present
	return b
}
