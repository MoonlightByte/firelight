package core

import (
	"errors"
	"fmt"
	"sort"
)

// validateWorldConditionChecks checks the damage-triggered checks a world
// declares outside Battle. A Battle declares its own in its rules.
func validateWorldConditionChecks(b *Blueprint) error {
	if len(b.ConditionChecks) == 0 {
		return nil
	}
	if b.Battle != nil {
		return conditionFault("E_RULE", "", "condition_checks", "no Battle, whose rules declare their own", "Battle")
	}
	if len(b.ConditionChecks) > 16 {
		return conditionFault("E_LIMIT", "", "condition_checks", "at most 16", fmt.Sprint(len(b.ConditionChecks)))
	}
	if p, bad := invalidConditionCheck(b, b.ConditionChecks); bad {
		return conditionFault("E_RULE", string(p.Condition), "condition_check", "unique declared Condition/Resource and bounded check law", fmt.Sprint(p))
	}
	return nil
}

// resolveDamageChecks is the damage-triggered check outside Battle. Each
// instance of a checked type on the damaged Character, in ID order, makes a
// StatCheck on the policy's stat against DC max(minimum, min(maximum, damage /
// divisor)), with the stat's roll modes. The damage operation's condition
// rolls for that instance supply its faces, in order; without them the
// world's random source rolls. Failure ends the instance and its anchored
// group.
func resolveDamageChecks(b *Blueprint, target CharacterID, resource ResourceID, damage int64, ctx applicationContext) ([]Event, error) {
	if len(b.ConditionChecks) == 0 {
		return nil, nil
	}
	var ids []ConditionInstanceID
	for _, c := range b.Conditions {
		if c.Target == target {
			ids = append(ids, c.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var events []Event
	for _, id := range ids {
		i := conditionIndex(b, id)
		if i < 0 {
			continue
		} // A previous failed check may remove dependents.
		c := b.Conditions[i]
		for _, policy := range b.ConditionChecks {
			if policy.Condition != c.Type || policy.Resource != resource {
				continue
			}
			if err := conditionWork(ctx); err != nil {
				return nil, err
			}
			dc := max(policy.Minimum, min(policy.Maximum, damage/policy.Divisor))
			v := StatCheck{Character: target, Stat: policy.Stat, Sides: policy.Sides, DC: &dc}
			if cursor := ctx.conditionInput; cursor != nil {
				for cursor.Next < len(cursor.Values) && len(v.Faces) < 2 && cursor.Values[cursor.Next].Condition == id && cursor.Values[cursor.Next].Target == target {
					v.Faces = append(v.Faces, cursor.Values[cursor.Next].Face)
					cursor.Next++
				}
			}
			err := b.checkStatCheck(v)
			var result StatCheckResult
			if err == nil {
				result, err = statCheck(b, v, ctx)
			}
			var f *Fault
			if errors.As(err, &f) && f.Code == "E_ROLL" {
				return nil, rollFault(string(id), f.Field, f.Expected, f.Actual, fmt.Sprintf("%s: damage check DC %d", f.Message, dc))
			}
			if err != nil {
				return nil, err
			}
			result.Condition, result.Damage = copyPtr(&id), damage
			events = append(events, Event{Kind: StatCheckResolved, StatCheck: &result})
			if !*result.Success {
				if err := conditionWork(ctx); err != nil {
					return nil, err
				}
				es, err := removeConditionGroup(b, c, ctx)
				if err != nil {
					return nil, err
				}
				es[len(es)-1].Condition.CheckFailed = true
				events = append(events, es...)
			}
			break // Policy keys are unique for this type and Resource.
		}
	}
	return events, nil
}
