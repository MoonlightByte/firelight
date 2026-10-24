package core

import "sort"

// AttackOption is one attacking mode of an item a Character wears or carries,
// with the grants that mode gives the Character as it stands now.
type AttackOption struct {
	Item       ItemID                `json:"item"`
	Definition EquipmentDefinitionID `json:"definition"`
	Mode       EquipmentModeID       `json:"mode"`
	Slots      []SlotUse             `json:"slots"`
	Worn       bool                  `json:"worn"`
	Grants     []AttackGrant         `json:"grants"`
}

type AttackOptionsProjection struct {
	Character CharacterSummary `json:"character"`
	Revision  Revision         `json:"revision"`
	Options   []AttackOption   `json:"options"`
}

// AttackOptionsView lists, for each item the Character wears or carries
// itself, every mode that declares attacks, and the grants of that mode. It
// is a detached observation, never input to Execute.
//
// Gates, recipes and die upgrades read the Character as it stands: the
// listed mode is not worn for the purpose, so its own slots and modifiers do
// not apply. Slots are reported, not checked against free capacity. An item
// the Character only owns, or that sits in a container, is not listed.
// Options follow the Blueprint's item order, by ID, then the definition's
// modes.
func (w *World) AttackOptionsView(id CharacterID) (AttackOptionsProjection, error) {
	s := w.Snapshot()
	b := &s.Blueprint
	if err := b.CheckRef(Ref{CharacterKind, string(id)}); err != nil {
		return AttackOptionsProjection{}, err
	}
	c := character(b, id)
	out := AttackOptionsProjection{Character: CharacterSummary{c.ID, c.Name}, Revision: s.Revision, Options: []AttackOption{}}
	d := newDerivation(b)
	for _, i := range b.Items {
		if i.Equipment == nil || (i.Wearer != id && !carriedBy(i, id)) {
			continue
		}
		def := b.equipmentDefinition(i.Equipment.Definition)
		if def == nil {
			continue
		}
		for _, m := range def.Modes {
			if len(m.Attacks) == 0 {
				continue
			}
			o := AttackOption{Item: i.ID, Definition: def.ID, Mode: m.ID, Slots: append([]SlotUse{}, m.Slots...),
				Worn: i.Wearer == id && i.Equipment.Mode == m.ID, Grants: []AttackGrant{}}
			for _, a := range m.Attacks {
				grant, ok, err := d.evaluateEquipmentGrant(id, i, m.ID, a)
				if err != nil {
					return AttackOptionsProjection{}, err
				}
				if ok {
					o.Grants = append(o.Grants, grant)
				}
			}
			sort.Slice(o.Grants, func(x, y int) bool { return o.Grants[x].ID < o.Grants[y].ID })
			out.Options = append(out.Options, o)
		}
	}
	return out, nil
}
