package core

import "sort"

// An anchor is an existing Condition, not another group/cache authority. Only
// direct dependents are supported; resource laws belong to authored definitions.
func checkConditionMaintenance(x *derivation, c ConditionInstance) error {
	b := x.b
	d := x.definition(c.Type)
	if d.UntilSourceTurn || d.EndsAfterAttack {
		e := b.activeBattle()
		if e == nil || battleParticipant(e, c.Target) == nil || d.UntilSourceTurn && (c.Source == nil || c.Source.Kind != CharacterKind || battleParticipant(e, CharacterID(c.Source.ID)) == nil) {
			return conditionFault("E_DURATION", string(c.ID), "battle lifetime", "active Battle containing target and lifetime source", "absent")
		}
	}
	if err := checkConditionBlocker(x, c); err != nil {
		return err
	}
	if c.Anchor != nil {
		i := x.instance(*c.Anchor)
		if i < 0 || *c.Anchor == c.ID || b.Conditions[i].Anchor != nil {
			return conditionFault("E_CONDITION", string(c.ID), "anchor", "active root Condition, no self-reference or chains", string(*c.Anchor))
		}
	}
	if resource := x.definition(c.Type).EndsAtMinimum; resource != nil {
		r := x.character(c.Target).Resources[*resource]
		if r.Current <= r.Minimum {
			return conditionFault("E_CONDITION", string(c.ID), "ends_at_minimum", "target Resource above minimum", string(*resource))
		}
	}
	return nil
}

func checkConditionBlocker(l lookups, c ConditionInstance) error {
	if blocker := l.definition(c.Type).EndsOnCondition; blocker != nil && l.hasCondition(c.Target, *blocker) {
		return conditionFault("E_CONDITION", string(c.ID), "ends_on_condition", "blocking Condition absent on target", string(*blocker))
	}
	return nil
}

func endConditionsOnApply(b *Blueprint, incoming ConditionInstance, ctx applicationContext) ([]Event, error) {
	var ids []ConditionInstanceID
	for _, c := range b.Conditions {
		trigger := conditionDefinition(b, c.Type).EndsOnCondition
		if c.Target == incoming.Target && trigger != nil && presentAs(conditionDefinition(b, incoming.Type), *trigger) {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var events []Event
	for _, id := range ids {
		i := conditionIndex(b, id)
		if i < 0 { // Already removed with a selected root.
			continue
		}
		if err := conditionWork(ctx); err != nil {
			return nil, err
		}
		ended, err := removeConditionGroup(b, b.Conditions[i], ctx)
		if err != nil {
			return nil, err
		}
		ended[len(ended)-1].Condition.EndingCondition = copyPtr(&incoming.ID)
		events = append(events, ended...)
	}
	return events, nil
}

func conditionWork(ctx applicationContext) error {
	*ctx.work++
	if *ctx.work > MaxOperations {
		return conditionFault("E_LIMIT", "", "work", "at most 128", "Condition removal exceeded")
	}
	return nil
}

// Root removal is already charged by the caller. Dependents are charged here.
// The terminal root event preserves the explicit removal's replay intent.
// A lowered Resource maximum that clamps current to its minimum is a debit:
// it releases floor Conditions like any other, before the root event.
func removeConditionGroup(b *Blueprint, root ConditionInstance, ctx applicationContext) ([]Event, error) {
	var ids []ConditionInstanceID
	for _, c := range b.Conditions {
		if c.Anchor != nil && *c.Anchor == root.ID {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ids = append(ids, root.ID)
	events := []Event{}
	var floors []resourceFloor
	var terminal Event
	for _, id := range ids {
		if id != root.ID {
			if err := conditionWork(ctx); err != nil {
				return nil, err
			}
		}
		i := conditionIndex(b, id)
		c := b.Conditions[i]
		change := &ConditionChange{Before: cloneCondition(&c), BeforeEffects: conditionEffects(b, c)}
		if id != root.ID {
			change.AnchorRemoved = copyPtr(&root.ID)
		}
		b.Conditions = append(b.Conditions[:i], b.Conditions[i+1:]...)
		b.RetiredConditionIDs = append(b.RetiredConditionIDs, id)
		var floored []resourceFloor
		change.Resources, floored = lowerConditionMaxima(b, c)
		floors = append(floors, floored...)
		if id == root.ID {
			terminal = Event{Kind: ConditionRemoved, Condition: change}
		} else {
			events = append(events, Event{Kind: ConditionRemoved, Condition: change})
		}
	}
	for _, f := range floors {
		ended, err := endConditionsAtMinimum(b, f.Character, f.Resource, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, ended...)
	}
	return append(events, terminal), nil
}

func replaceConditions(b *Blueprint, incoming ConditionInstance, ctx applicationContext) ([]Event, error) {
	if conditionDefinition(b, incoming.Type).Instances != Replace {
		return nil, nil
	}
	for _, c := range b.Conditions {
		if c.Type == incoming.Type && c.Target == incoming.Target {
			if err := conditionWork(ctx); err != nil {
				return nil, err
			}
			events, err := removeConditionGroup(b, c, ctx)
			if err != nil {
				return nil, err
			}
			events[len(events)-1].Condition.ReplacedBy = copyPtr(&incoming.ID)
			return events, nil
		}
	}
	return nil, nil
}

// Called by the single Resource evaluator, before its terminal consequence
// event. Even a later heal in the same request cannot restore a released group.
// An instance whose ends_on_condition type is held at this minimum ends too:
// the held type has just begun.
func endConditionsAtMinimum(b *Blueprint, target CharacterID, resource ResourceID, ctx applicationContext) ([]Event, error) {
	r := character(b, target).Resources[resource]
	if r.Current > r.Minimum {
		return nil, nil
	}
	var ids []ConditionInstanceID
	for _, c := range b.Conditions {
		d := conditionDefinition(b, c.Type)
		if c.Target == target && (d.EndsAtMinimum != nil && *d.EndsAtMinimum == resource || endsWhenHeld(b, d, resource)) {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []Event
	for _, id := range ids {
		i := conditionIndex(b, id)
		if i < 0 { // Already removed as another selected root's dependent.
			continue
		}
		if err := conditionWork(ctx); err != nil {
			return nil, err
		}
		es, err := removeConditionGroup(b, b.Conditions[i], ctx)
		if err != nil {
			return nil, err
		}
		es[len(es)-1].Condition.MinimumResource = copyPtr(&resource)
		out = append(out, es...)
	}
	return out, nil
}
