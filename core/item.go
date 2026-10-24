package core

import "unicode/utf8"

func cloneItem(i *Item) *Item {
	if i == nil {
		return nil
	}
	v := *i
	v.Custody = copyPtr(i.Custody)
	v.Quantity = copyPtr(i.Quantity)
	v.Equipment = copyPtr(i.Equipment)
	v.Charges = cloneItemCharges(i.Charges)
	return &v
}

// Creation checks only local facts. Ordered operations and final effective
// values remain the shared evaluator's responsibility.
func (b Blueprint) checkCreateItem(i Item) error {
	if i.SplitFrom != "" {
		return fail("E_RULE", string(i.ID), "split_from", "provenance assigned by Split", string(i.SplitFrom), "direct creation cannot claim a split ancestor")
	}
	return b.checkNewItem(i)
}

func (b Blueprint) checkNewItem(i Item) error {
	var err error
	i, err = resolveCatalogItem(i, b.itemDefinition(i.From), true)
	if err != nil {
		return err
	}
	if !validID(string(i.ID)) || !utf8.ValidString(i.Name) {
		return fail("E_ID", string(i.ID), "identity", "valid explicit ID and UTF-8 name", "invalid", "invalid identity or name")
	}
	if _, exists := b.KindOf(string(i.ID)); exists {
		return fail("E_DUPLICATE", string(i.ID), "identity", "unique canonical ID", string(i.ID), "duplicate canonical identity")
	}
	if err := b.checkItemLineage(i); err != nil {
		return err
	}
	if !utf8.ValidString(i.Description) {
		return fail("E_TEXT", string(i.ID), "description", "UTF-8 text", "invalid", "invalid item description")
	}
	if itemQuantity(i) <= 0 {
		return quantityFault(i, "positive initial stock", "created item must have positive stock")
	}
	if i.Owner != "" {
		if err := b.CheckRef(Ref{CharacterKind, string(i.Owner)}); err != nil {
			return err
		}
	}
	if i.Wearer != "" {
		return fail("E_RULE", string(i.ID), "wearer", "initially unworn item", string(i.Wearer), "use Equip after creation to apply wearing rules")
	}
	if err := b.checkItemCustody(i); err != nil {
		return err
	}
	if err := b.checkRecoverable(i); err != nil {
		return err
	}
	if err := checkChargedItem(i); err != nil {
		return err
	}
	if i.Equipment != nil {
		if b.Equipment == nil || b.equipmentDefinition(i.Equipment.Definition) == nil {
			return equipmentFault(string(i.ID), "definition", "declared equipment definition", string(i.Equipment.Definition))
		}
		if i.Equipment.Mode != "" {
			return equipmentFault(string(i.ID), "mode", "empty for initially unworn item", string(i.Equipment.Mode))
		}
	}
	return nil
}
