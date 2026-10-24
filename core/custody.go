package core

import "fmt"

// PlaceItem changes physical custody of a complete stock record. It neither
// transfers ownership nor supplies host authorization/consent.
type PlaceItem struct {
	Item    ItemID      `json:"item"`
	From    Ref         `json:"from"`
	To      Ref         `json:"to"`
	Handler CharacterID `json:"handler"`
}
type CustodyChange struct {
	Item    ItemID      `json:"item"`
	Before  Ref         `json:"before"`
	After   Ref         `json:"after"`
	Handler CharacterID `json:"handler"`
}

func custodyFault(i ItemID, expected, actual, message string) error {
	return fail("E_CUSTODY", string(i), "custody", expected, actual, message)
}
func carriedBy(i Item, id CharacterID) bool {
	return i.Custody != nil && *i.Custody == (Ref{CharacterKind, string(id)})
}
func (b Blueprint) checkPlace(v PlaceItem) error {
	for _, e := range []struct {
		field string
		ref   Ref
	}{
		{"item", Ref{ItemKind, string(v.Item)}}, {"handler", Ref{CharacterKind, string(v.Handler)}}, {"from", v.From}, {"to", v.To},
	} {
		var err error
		if e.field == "from" || e.field == "to" {
			err = b.CheckCustodyRef(e.ref)
		} else {
			err = b.CheckRef(e.ref)
		}
		if f, ok := err.(*Fault); ok {
			v := *f
			v.Field = e.field
			return &v
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// CheckCustodyRef resolves a typed endpoint, including a declared container.
// It does not infer possession, check distance or authorize a transfer.
func (b Blueprint) CheckCustodyRef(r Ref) error {
	return checkCustodyRef(r, b.KindOf, func(id ItemID) *Item { return item(&b, id) })
}
func checkCustodyRef(r Ref, kindOf func(string) (EntityKind, bool), itemOf func(ItemID) *Item) error {
	if err := checkRef(r, kindOf); err != nil {
		return err
	}
	if r.Kind == ItemKind {
		c := itemOf(ItemID(r.ID))
		if !c.Container || c.Custody == nil || c.Custody.Kind != LocationKind {
			return custodyFault(c.ID, "location-bound container", r.ID, "item is not a location-bound container")
		}
		return checkRef(*c.Custody, kindOf)
	}
	return nil
}
func (b Blueprint) checkItemCustody(i Item) error { return checkItemCustody(i, b.CheckCustodyRef) }
func checkItemCustody(i Item, custodyRef func(Ref) error) error {
	if i.Container && (itemQuantity(i) != 1 || i.Wearer != "" || i.Equipment != nil || i.Custody == nil || i.Custody.Kind != LocationKind) {
		return custodyFault(i.ID, "one unworn location-bound container without equipment", "invalid container", "invalid container declaration")
	}
	if i.Custody == nil {
		return nil
	} // Historical unknown facts stay unknown.
	if err := custodyRef(*i.Custody); err != nil {
		return err
	}
	if i.Wearer != "" && !carriedBy(i, i.Wearer) {
		return custodyFault(i.ID, "custody at wearer", fmt.Sprint(*i.Custody), "known custody disagrees with wearer")
	}
	return nil
}
func custodyLocation(b *Blueprint, r Ref) LocationID {
	switch r.Kind {
	case CharacterKind:
		return character(b, CharacterID(r.ID)).Location
	case ItemKind:
		return LocationID(item(b, ItemID(r.ID)).Custody.ID)
	default:
		return LocationID(r.ID)
	}
}
func placeItem(b *Blueprint, v PlaceItem) ([]Event, error) {
	i := item(b, v.Item)
	if i.Container {
		return nil, custodyFault(i.ID, "movable item", "location-bound container", "container remains anchored to its location")
	}
	if i.Custody == nil || *i.Custody != v.From {
		return nil, custodyFault(i.ID, fmt.Sprint(v.From), fmt.Sprint(i.Custody), "stated source does not match known custody")
	}
	if v.From == v.To {
		return nil, noChange(string(i.ID))
	}
	if i.Wearer != "" {
		return nil, fail("E_WORN", string(i.ID), "wearer", "unworn item", string(i.Wearer), "explicit unequip required before placement")
	}
	if itemQuantity(*i) <= 0 {
		return nil, quantityFault(*i, "positive stock", "depleted item cannot be placed")
	}
	at := character(b, v.Handler).Location
	if custodyLocation(b, v.From) != at || custodyLocation(b, v.To) != at {
		return nil, custodyFault(i.ID, "both endpoints at handler location", string(at), "placement cannot cross locations")
	}
	i.Custody = copyPtr(&v.To)
	return []Event{{Kind: ItemPlaced, Custody: &CustodyChange{Item: i.ID, Before: v.From, After: v.To, Handler: v.Handler}}}, nil
}

// ItemsAt is a detached direct-custody view. It does not recursively include
// container contents or imply permission to take a visible item.
type CustodyProjection struct {
	Revision Revision `json:"revision"`
	At       Ref      `json:"at"`
	Items    []Item   `json:"items"`
}

func (w *World) ItemsAt(at Ref) (CustodyProjection, error) {
	s := w.Snapshot()
	if err := s.Blueprint.CheckCustodyRef(at); err != nil {
		return CustodyProjection{}, err
	}
	p := CustodyProjection{Revision: s.Revision, At: at, Items: []Item{}}
	for _, i := range s.Items {
		if i.Custody != nil && *i.Custody == at {
			p.Items = append(p.Items, *cloneItem(&i))
		}
	}
	return p, nil
}
