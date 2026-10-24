package core

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxWorldItemDefinitions = 4096

type ItemDefinitionID string

// ItemPrice is reference data, not a purchase rule or a currency conversion.
// A missing price is unpriced; an amount of zero is explicitly free.
type ItemPrice struct {
	Unit        string `json:"unit"`
	Amount      int64  `json:"amount"`
	UnitsPerLot int64  `json:"units_per_lot"`
}

// ItemDefinition owns display and classification data. Equipment refers to
// the existing equipment rules; neither classification nor price grants rules.
// IDs occupy a separate namespace from entities and equipment definitions.
type ItemDefinition struct {
	ID          ItemDefinitionID      `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Kind        string                `json:"kind"`
	Subtype     string                `json:"subtype,omitempty"`
	Magical     bool                  `json:"magical"`
	Consumable  bool                  `json:"consumable"`
	Equipment   EquipmentDefinitionID `json:"equipment,omitempty"`
	Price       *ItemPrice            `json:"price,omitempty"`
	Charges     *ChargeRule           `json:"charges,omitempty"`
}

func catalogFault(id, field, expected, actual string) error {
	return fail("E_CATALOG", id, field, expected, actual, "invalid item catalog "+field)
}

func cloneItemDefinitions(ds []ItemDefinition) []ItemDefinition {
	// An unused catalog has one canonical shape. Unlike the historical
	// condition arrays, it needs no native empty-presence mask extension.
	if len(ds) == 0 {
		return nil
	}
	out := append([]ItemDefinition{}, ds...)
	for i := range out {
		out[i].Price = copyPtr(out[i].Price)
		out[i].Charges = cloneChargeRule(out[i].Charges)
	}
	return out
}

func catalogID(s string) bool   { return len(s) <= 256 && validID(s) }
func catalogText(s string) bool { return len(s) <= 4096 && utf8.ValidString(s) }

func (b *Blueprint) itemDefinition(id ItemDefinitionID) *ItemDefinition {
	for n := range b.ItemDefinitions {
		if b.ItemDefinitions[n].ID == id {
			return &b.ItemDefinitions[n]
		}
	}
	return nil
}

func (b *Blueprint) catalogIndex() (map[ItemDefinitionID]*ItemDefinition, error) {
	if len(b.ItemDefinitions) > MaxWorldItemDefinitions {
		return nil, catalogFault("", "definitions", "at most 4096 definitions", strconv.Itoa(len(b.ItemDefinitions)))
	}
	index := make(map[ItemDefinitionID]*ItemDefinition, len(b.ItemDefinitions))
	for n := range b.ItemDefinitions {
		d := &b.ItemDefinitions[n]
		id := string(d.ID)
		if !catalogID(id) {
			return nil, catalogFault(id, "id", "valid ID of at most 256 bytes", "invalid")
		}
		if index[d.ID] != nil {
			return nil, catalogFault(id, "id", "unique item definition ID", id)
		}
		index[d.ID] = d
		if !catalogText(d.Name) || strings.TrimSpace(d.Name) == "" {
			return nil, catalogFault(id, "name", "nonblank UTF-8 text of at most 4096 bytes", "invalid")
		}
		if !catalogText(d.Description) {
			return nil, catalogFault(id, "description", "UTF-8 text of at most 4096 bytes", "invalid")
		}
		if !catalogID(d.Kind) {
			return nil, catalogFault(id, "kind", "valid token of at most 256 bytes", "invalid")
		}
		if d.Subtype != "" && !catalogID(d.Subtype) {
			return nil, catalogFault(id, "subtype", "empty or valid token of at most 256 bytes", "invalid")
		}
		if d.Equipment != "" && (!catalogID(string(d.Equipment)) || b.equipmentDefinition(d.Equipment) == nil) {
			return nil, catalogFault(id, "equipment", "declared equipment definition", string(d.Equipment))
		}
		if p := d.Price; p != nil {
			if !catalogID(p.Unit) {
				return nil, catalogFault(id, "price.unit", "valid unit of at most 256 bytes", "invalid")
			}
			if p.Amount < 0 {
				return nil, catalogFault(id, "price.amount", "nonnegative int64", strconv.FormatInt(p.Amount, 10))
			}
			if p.UnitsPerLot <= 0 {
				return nil, catalogFault(id, "price.units_per_lot", "positive int64", strconv.FormatInt(p.UnitsPerLot, 10))
			}
		}
		if err := checkChargeRule(id, d.Charges); err != nil {
			return nil, err
		}
	}
	if len(index) > 0 && (len(b.Characters) > MaxWorldCharacters || len(b.Items) > MaxWorldItems || len(b.Locations) > MaxWorldLocations || len(b.ConditionDefinitions) > MaxWorldConditionDefinitions || len(b.Conditions) > MaxWorldConditions) {
		return nil, catalogFault("", "capacity", "native entity/Condition limits", "exceeded")
	}
	return index, nil
}

// resolveCatalogItem detaches only fields it fills. It never changes submitted
// intent. Empty display/equipment fields derive; equal postimages round-trip.
func resolveCatalogItem(i Item, d *ItemDefinition, fresh bool) (Item, error) {
	if i.From == "" {
		return i, nil
	}
	if !catalogID(string(i.From)) || d == nil {
		return Item{}, catalogFault(string(i.ID), "from", "declared item definition", string(i.From))
	}
	if i.Name != "" && i.Name != d.Name {
		return Item{}, catalogFault(string(i.ID), "name", d.Name, i.Name)
	}
	if i.Description != "" && i.Description != d.Description {
		return Item{}, catalogFault(string(i.ID), "description", d.Description, i.Description)
	}
	i.Name, i.Description = d.Name, d.Description
	if i.Equipment != nil && i.Equipment.Definition != "" && i.Equipment.Definition != d.Equipment {
		return Item{}, catalogFault(string(i.ID), "equipment", string(d.Equipment), string(i.Equipment.Definition))
	}
	if d.Equipment != "" {
		e := EquipmentInstance{Definition: d.Equipment}
		if i.Equipment != nil {
			e.Mode = i.Equipment.Mode
		}
		i.Equipment = &e
	} else if i.Equipment != nil {
		if i.Equipment.Mode != "" {
			return Item{}, catalogFault(string(i.ID), "mode", "no mode for ordinary item", string(i.Equipment.Mode))
		}
		return Item{}, catalogFault(string(i.ID), "equipment", "absent for this definition", "present")
	}
	charges, err := resolveItemCharges(i, d, fresh)
	if err != nil {
		return Item{}, err
	}
	i.Charges = charges
	return i, nil
}

// resolveCatalog returns a shallow Blueprint with detached resolved items.
// Validate can check source IR without mutating the caller's declarations.
func resolveCatalog(b Blueprint) (Blueprint, error) {
	index, err := b.catalogIndex()
	if err != nil {
		return Blueprint{}, err
	}
	var items []Item
	for n, i := range b.Items {
		if i.From == "" {
			continue
		}
		if items == nil {
			items = append([]Item(nil), b.Items...)
		}
		items[n], err = resolveCatalogItem(i, index[i.From], false)
		if err != nil {
			return Blueprint{}, err
		}
	}
	if items != nil {
		b.Items = items
	}
	return b, nil
}

type ItemDefinitionProjection struct {
	Revision Revision `json:"revision"`
	ItemDefinition
}

// ItemDefinitionsView reads exact IDs in request order at one revision. Like
// other multi-ID views, an unknown ID returns prior results and a fault.
func (w *World) ItemDefinitionsView(ids []ItemDefinitionID) ([]ItemDefinitionProjection, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	s := &w.authority.state
	out := make([]ItemDefinitionProjection, 0, len(ids))
	for _, id := range ids {
		n := sort.Search(len(s.ItemDefinitions), func(n int) bool { return s.ItemDefinitions[n].ID >= id })
		if n == len(s.ItemDefinitions) || s.ItemDefinitions[n].ID != id {
			return out, catalogFault(string(id), "id", "declared item definition", string(id))
		}
		d := s.ItemDefinitions[n]
		d.Price = copyPtr(d.Price)
		d.Charges = cloneChargeRule(d.Charges)
		out = append(out, ItemDefinitionProjection{Revision: s.Revision, ItemDefinition: d})
	}
	return out, nil
}
