package core

import "math/big"

type CharacterSummary struct {
	ID   CharacterID `json:"id"`
	Name string      `json:"name"`
}
type ItemSummary struct {
	ID   ItemID `json:"id"`
	Name string `json:"name"`
}

// A present fact with an empty Value means known-unowned/unworn. A nil fact
// means missing information. The distinction makes under-inclusion testable.
type CharacterFact struct {
	Value CharacterID `json:"value"`
}
type TransferProjection struct {
	Revision        *Revision         `json:"revision"`
	Item            *ItemSummary      `json:"item"`
	Owner           *CharacterFact    `json:"owner"`
	Wearer          *CharacterFact    `json:"wearer"`
	Recipient       *CharacterSummary `json:"recipient"`
	Rules           *Rules            `json:"rules"`
	RequiredEffects []EffectID        `json:"required_effects"`
	Effects         *[]Effect         `json:"effects"`
	WearerTotals    *map[StatID]int64 `json:"wearer_totals"`
}

func (w *World) TransferView(id ItemID, to CharacterID) (TransferProjection, error) {
	s := w.Snapshot()
	b := &s.Blueprint
	if b.Equipment != nil {
		return TransferProjection{}, fail("E_PROJECTION", string(id), "equipment", "EquipmentView and shared Preview", "legacy fixed-effect transfer view", "legacy transfer projection is insufficient for equipment recipes")
	}
	if err := b.CheckRef(Ref{ItemKind, string(id)}); err != nil {
		return TransferProjection{}, err
	}
	if err := b.CheckRef(Ref{CharacterKind, string(to)}); err != nil {
		return TransferProjection{}, err
	}
	i := item(b, id)
	if i.Quantity != nil {
		return TransferProjection{}, fail("E_PROJECTION", string(id), "quantity", "stock-aware view and shared Preview", "legacy transfer view", "legacy transfer projection lacks quantity facts")
	}
	recipient := character(b, to)
	effects := []Effect{}
	required := []EffectID{}
	totals := map[StatID]int64{}
	d := newDerivation(b)
	for _, e := range b.Effects {
		if e.Mode == WhileWorn && e.Target.ID == string(id) {
			effects = append(effects, e)
			required = append(required, e.ID)
			if i.Wearer != "" {
				ex, err := d.explain(i.Wearer, e.Stat)
				if err != nil {
					return TransferProjection{}, err
				}
				totals[e.Stat] = ex.Effective
			}
		}
	}
	return TransferProjection{&s.Revision, &ItemSummary{i.ID, i.Name}, &CharacterFact{i.Owner}, &CharacterFact{i.Wearer}, &CharacterSummary{recipient.ID, recipient.Name}, &b.Rules, required, &effects, &totals}, nil
}
func (p TransferProjection) Validate() error {
	missing := func(field string) error {
		return fail("E_PROJECTION", "", field, "required mechanical fact", "missing or inconsistent", "insufficient transfer projection")
	}
	if p.Revision == nil {
		return missing("revision")
	}
	if p.Item == nil || !validID(string(p.Item.ID)) {
		return missing("item")
	}
	if p.Owner == nil {
		return missing("owner")
	}
	if p.Wearer == nil {
		return missing("wearer")
	}
	if p.Recipient == nil || !validID(string(p.Recipient.ID)) {
		return missing("recipient")
	}
	if p.Rules == nil {
		return missing("rules")
	}
	if (p.Rules.Transfer != KeepsWorn && p.Rules.Transfer != Unequips) || (p.Rules.Wear != WearAny && p.Rules.Wear != OwnerOnly) {
		return missing("rules")
	}
	if p.Effects == nil {
		return missing("effects")
	}
	if p.WearerTotals == nil {
		return missing("wearer_totals")
	}
	if len(p.RequiredEffects) != len(*p.Effects) {
		return missing("effects")
	}
	seen := map[EffectID]bool{}
	for index, e := range *p.Effects {
		if !validID(string(e.ID)) || !validID(string(e.Stat)) || seen[e.ID] || p.RequiredEffects[index] != e.ID || e.Mode != WhileWorn || e.Target != (Ref{ItemKind, string(p.Item.ID)}) {
			return missing("effects")
		}
		seen[e.ID] = true
		if p.Wearer.Value != "" {
			if _, ok := (*p.WearerTotals)[e.Stat]; !ok {
				return missing("wearer_totals." + string(e.Stat))
			}
		}
	}
	return nil
}

type TransferAssessment struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// Assess evaluates a standalone transfer, not a later equip or a compound
// request. This is planning information; Execute never accepts it as authority.
func (p TransferProjection) Assess() (TransferAssessment, error) {
	if err := p.Validate(); err != nil {
		return TransferAssessment{}, err
	}
	deny := func(reason string) (TransferAssessment, error) { return TransferAssessment{false, reason}, nil }
	if p.Owner.Value == "" {
		return deny("item has no owner to transfer from")
	}
	if p.Owner.Value == p.Recipient.ID {
		return deny("recipient is already owner")
	}
	if p.Rules.Transfer == KeepsWorn && p.Rules.Wear == OwnerOnly && p.Wearer.Value != "" && p.Wearer.Value != p.Recipient.ID {
		return deny("transfer would violate owner_only")
	}
	if p.Rules.Transfer == Unequips && p.Wearer.Value != "" {
		totals := map[StatID]*big.Int{}
		for _, e := range *p.Effects {
			if totals[e.Stat] == nil {
				totals[e.Stat] = big.NewInt((*p.WearerTotals)[e.Stat])
			}
			totals[e.Stat].Sub(totals[e.Stat], big.NewInt(e.Amount))
		}
		for _, stat := range keys(totals) {
			if !totals[stat].IsInt64() {
				return deny("unequip would overflow stat " + string(stat))
			}
		}
	}
	return TransferAssessment{true, "standalone transfer satisfies projected mechanics"}, nil
}

type CharacterStatusProjection struct {
	Buffers       []ResourceBuffer        `json:"buffers,omitempty"`
	Clock         *Clock                  `json:"clock,omitempty"`
	Conditions    []ConditionSummary      `json:"conditions,omitempty"`
	Held          []HeldCondition         `json:"held_conditions,omitempty"`
	Revision      Revision                `json:"revision"`
	Character     CharacterSummary        `json:"character"`
	Location      Location                `json:"location"`
	Resources     map[ResourceID]Resource `json:"resources"`
	Stats         []StatExplanation       `json:"stats"`
	ActiveEffects []Effect                `json:"active_effects"`
}

func (w *World) CharacterStatus(id CharacterID) (CharacterStatusProjection, error) {
	s := w.Snapshot()
	b := &s.Blueprint
	if err := b.CheckRef(Ref{CharacterKind, string(id)}); err != nil {
		return CharacterStatusProjection{}, err
	}
	c := character(b, id)
	p := CharacterStatusProjection{Buffers: c.Buffers, Clock: copyPtr(b.Clock), Revision: s.Revision, Character: CharacterSummary{c.ID, c.Name}, Resources: c.Resources, Stats: []StatExplanation{}, ActiveEffects: []Effect{}}
	for _, l := range b.Locations {
		if l.ID == c.Location {
			p.Location = l
			break
		}
	}
	d := newDerivation(b)
	for _, stat := range keys(c.Stats) {
		e, err := d.explain(id, stat)
		if err != nil {
			return CharacterStatusProjection{}, err
		}
		p.Stats = append(p.Stats, e)
	}
	for _, c := range b.Conditions {
		if c.Target == id {
			p.Conditions = append(p.Conditions, ConditionSummary{Instance: c.ID, Type: c.Type, ClockLifetime: copyPtr(c.ClockLifetime)})
		}
	}
	p.Held = d.heldConditions(id)
	for _, e := range d.allEffects() {
		if d.activeFor(e, id) {
			p.ActiveEffects = append(p.ActiveEffects, e)
		}
	}
	return p, nil
}
