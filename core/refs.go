package core

// Refs answers reference checks against one Blueprint by lookup instead of a
// scan of every entity. A pass that checks every item of a world near its
// entity caps would otherwise take time quadratic in the entity count. The
// Blueprint must not change while its Refs is in use.
type Refs struct {
	b     *Blueprint
	kinds map[string]EntityKind
	items map[ItemID]int
}

// NewRefs indexes b. A repeated ID keeps KindOf's answer: characters, then
// items, then locations, and the first declaration within a kind.
func NewRefs(b *Blueprint) Refs {
	x := Refs{b: b, kinds: make(map[string]EntityKind, len(b.Characters)+len(b.Items)+len(b.Locations)), items: make(map[ItemID]int, len(b.Items))}
	add := func(id string, k EntityKind) {
		if _, ok := x.kinds[id]; !ok {
			x.kinds[id] = k
		}
	}
	for i := range b.Characters {
		add(string(b.Characters[i].ID), CharacterKind)
	}
	for i := range b.Items {
		add(string(b.Items[i].ID), ItemKind)
		if _, ok := x.items[b.Items[i].ID]; !ok {
			x.items[b.Items[i].ID] = i
		}
	}
	for i := range b.Locations {
		add(string(b.Locations[i].ID), LocationKind)
	}
	return x
}

// KindOf is Blueprint.KindOf by lookup.
func (x Refs) KindOf(id string) (EntityKind, bool) {
	k, ok := x.kinds[id]
	return k, ok
}

// CheckRef is Blueprint.CheckRef by lookup.
func (x Refs) CheckRef(r Ref) error { return checkRef(r, x.KindOf) }

// CheckCustodyRef is Blueprint.CheckCustodyRef by lookup.
func (x Refs) CheckCustodyRef(r Ref) error { return checkCustodyRef(r, x.KindOf, x.item) }

func (x Refs) item(id ItemID) *Item {
	if i, ok := x.items[id]; ok {
		return &x.b.Items[i]
	}
	return nil
}
