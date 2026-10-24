package core

import "sort"

// These are authored equipment consequences, not built-in weapon masteries.
type WeaponHitCondition struct {
	ID         string                `json:"id"`
	Definition EquipmentDefinitionID `json:"definition"`
	Mode       EquipmentModeID       `json:"mode"`
	Attack     string                `json:"attack"`
	Requires   ConditionTypeID       `json:"requires"`
	Condition  ConditionTypeID       `json:"condition"`
}
type WeaponConditionChoice struct {
	Rule     string              `json:"rule"`
	Instance ConditionInstanceID `json:"instance"`
}
type BattleConditionEnding struct {
	Window  BattleWindow `json:"window"`
	Trigger string       `json:"trigger"` // source_turn, attack, terminal
}

func (b *Blueprint) hitConditionRule(id string) *WeaponHitCondition {
	if b.Battle != nil {
		for i := range b.Battle.Rules.HitConditions {
			if b.Battle.Rules.HitConditions[i].ID == id {
				return &b.Battle.Rules.HitConditions[i]
			}
		}
	}
	return nil
}

func (b *Blueprint) checkHitConditionRules() error {
	if len(b.Battle.Rules.HitConditions) > 32 {
		return battleFault("hit conditions", "at most 32", "exceeded")
	}
	seen := map[string]bool{}
	for _, r := range b.Battle.Rules.HitConditions {
		if !validID(r.ID) || seen[r.ID] {
			return battleFault("hit condition", "unique valid policy ID", r.ID)
		}
		seen[r.ID] = true
		if err := b.CheckWeaponHitCondition(r); err != nil {
			return err
		}
	}
	return nil
}

// CheckWeaponHitCondition is shared by source lowering and typed rule validation.
func (b *Blueprint) CheckWeaponHitCondition(r WeaponHitCondition) error {
	d := b.equipmentDefinition(r.Definition)
	if d == nil {
		return battleFault("definition", "declared equipment", string(r.Definition))
	}
	m := equipmentMode(d, r.Mode)
	if m == nil {
		return battleFault("mode", "declared equipment mode", string(r.Mode))
	}
	found := false
	for _, a := range m.Attacks {
		found = found || a.ID == r.Attack && a.Melee != nil
	}
	if !found {
		return battleFault("attack", "executable equipment attack", r.Attack)
	}
	if conditionDefinition(b, r.Requires) == nil {
		return battleFault("requires", "declared training Condition", string(r.Requires))
	}
	result := conditionDefinition(b, r.Condition)
	if result == nil {
		return battleFault("condition", "declared consequence Condition", string(r.Condition))
	}
	for _, modifier := range result.Modifiers {
		if modifier.Magnitude {
			return battleFault("condition", "fixed consequence without magnitude argument", string(r.Condition))
		}
	}
	return nil
}

func (b *Blueprint) checkWeaponConditionStatic(v WeaponAttack) error {
	if v.HitCondition == nil {
		return nil
	}
	r := b.hitConditionRule(v.HitCondition.Rule)
	if r == nil {
		return battleFault("hit_condition.rule", "declared weapon hit condition", v.HitCondition.Rule)
	}
	return b.CheckConditionStatic(ApplyCondition{Instance: v.HitCondition.Instance, Type: r.Condition, Target: v.Target, Source: &Ref{CharacterKind, string(v.Actor)}})
}

func (b *Blueprint) checkWeaponCondition(v WeaponAttack, grant AttackGrant) error {
	if v.HitCondition == nil {
		return nil
	}
	r := b.hitConditionRule(v.HitCondition.Rule)
	if r == nil || r.Definition != grant.Origin.Definition || r.Mode != grant.Origin.Mode || r.Attack != grant.Origin.Member || !b.hasCondition(v.Actor, r.Requires) {
		return battleFault("hit_condition.rule", "selected current grant and required training", v.HitCondition.Rule)
	}
	id := v.HitCondition.Instance
	if conditionIndex(b, id) >= 0 {
		return conditionFault("E_DUPLICATE", string(id), "condition_instance", "unused lifetime identity", "active")
	}
	for _, retired := range b.RetiredConditionIDs {
		if retired == id {
			return conditionFault("E_DUPLICATE", string(id), "condition_instance", "unused lifetime identity", "retired")
		}
	}
	return nil
}

func endBattleConditions(b *Blueprint, e *BattleEncounter, trigger string, ctx applicationContext) ([]Event, error) {
	actor := e.Roster[e.Cursor].Admission.Character
	var ids []ConditionInstanceID
	for _, c := range b.Conditions {
		d := conditionDefinition(b, c.Type)
		if trigger == "source_turn" && d.UntilSourceTurn && c.Source != nil && c.Source.Kind == CharacterKind && c.Source.ID == string(actor) ||
			trigger == "attack" && d.EndsAfterAttack && c.Target == actor ||
			trigger == "terminal" && (d.UntilSourceTurn || d.EndsAfterAttack) {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var events []Event
	for _, id := range ids {
		i := conditionIndex(b, id)
		if i < 0 {
			continue
		}
		if err := conditionWork(ctx); err != nil {
			return nil, err
		}
		es, err := removeConditionGroup(b, b.Conditions[i], ctx)
		if err != nil {
			return nil, err
		}
		es[len(es)-1].Condition.BattleEnding = &BattleConditionEnding{Window: BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: actor}, Trigger: trigger}
		events = append(events, es...)
	}
	return events, nil
}
