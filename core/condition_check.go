package core

import (
	"encoding/json"
	"fmt"
	"sort"
)

const MaxConditionEvidenceBytes = 64 << 10

// ConditionCheckPolicy is an immutable damage consequence in the Battle profile.
// It names no game-specific condition and owns no parallel resource or RNG state.
type ConditionCheckPolicy struct {
	Condition ConditionTypeID `json:"condition"`
	Resource  ResourceID      `json:"resource"`
	Stat      StatID          `json:"stat"`
	Sides     uint64          `json:"sides"`
	Minimum   int64           `json:"minimum"`
	Divisor   int64           `json:"divisor"`
	Maximum   int64           `json:"maximum"`
}

// ConditionRollInput is ordered human evidence for one outer operation. It is
// not an executable variant, a new Condition identity, or a chosen check total.
type ConditionRollInput struct {
	Condition ConditionInstanceID `json:"condition"`
	Target    CharacterID         `json:"target"`
	Face      int64               `json:"face"`
}

type ConditionCheckResult struct {
	Condition ConditionInstance    `json:"condition"`
	Policy    ConditionCheckPolicy `json:"policy"`
	Damage    int64                `json:"damage"`
	Source    BattleController     `json:"source"`
	Roll      CheckResult          `json:"roll"`
}

// ConditionInputNeed describes a private candidate, never committed damage.
type ConditionInputNeed struct {
	Condition ConditionInstanceID `json:"condition"`
	Target    CharacterID         `json:"target"`
	Index     int                 `json:"index"`
	Damage    int64               `json:"damage"`
	Threshold int64               `json:"threshold"`
	Modifier  int64               `json:"modifier"`
}

// InputRequired also preserves the ordinary Fault for direct Execute callers.
// The trusted host uses its typed need to stage a durable human question.
type InputRequired struct {
	Need BattleInputRequirements
	Err  error
}

func (e *InputRequired) Error() string { return e.Err.Error() }
func (e *InputRequired) Unwrap() error { return e.Err }

type conditionInputCursor struct {
	Values []ConditionRollInput
	Next   int
}

func (b *Blueprint) checkConditionCheckRules() error {
	if len(b.Battle.Rules.ConditionChecks) > 16 {
		return battleFault("condition checks", "at most 16", "exceeded")
	}
	if p, bad := invalidConditionCheck(b, b.Battle.Rules.ConditionChecks); bad {
		return battleFault("condition check", "unique declared Condition/Resource and bounded explicit check law", fmt.Sprint(p))
	}
	return nil
}

// invalidConditionCheck finds a policy that is not a unique pair of declared
// Condition type and Resource with a bounded check law.
func invalidConditionCheck(b *Blueprint, policies []ConditionCheckPolicy) (ConditionCheckPolicy, bool) {
	seen := map[[2]string]bool{}
	for _, p := range policies {
		key := [2]string{string(p.Condition), string(p.Resource)}
		if conditionDefinition(b, p.Condition) == nil || !validID(string(p.Resource)) || !validID(string(p.Stat)) || p.Sides < 2 || p.Sides > 1000 || p.Minimum < 1 || p.Maximum < p.Minimum || p.Divisor < 1 || seen[key] {
			return p, true
		}
		seen[key] = true
	}
	return ConditionCheckPolicy{}, false
}

// conditionChecks are the Battle's policies in a Battle world and the world's
// own otherwise; a world cannot declare both.
func (b *Blueprint) conditionChecks() []ConditionCheckPolicy {
	if b.Battle != nil {
		return b.Battle.Rules.ConditionChecks
	}
	return b.ConditionChecks
}

func checkConditionCheckTarget(l lookups, kind ConditionTypeID, target CharacterID) error {
	for _, p := range l.blueprint().conditionChecks() {
		if p.Condition == kind {
			if err := checkResource(l, target, p.Resource); err != nil {
				return err
			}
			if err := checkStat(l, target, p.Stat); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkConditionRollInputs(o Operation) error {
	if len(o.ConditionRolls) > MaxOperations {
		return battleFault("condition rolls", "at most 128", "exceeded")
	}
	if len(o.ConditionRolls) != 0 && o.Damage == nil && o.WeaponAttack == nil && o.Invoke == nil {
		return battleFault("condition rolls", "Damage, WeaponAttack or Invoke evidence", "wrong operation")
	}
	for _, v := range o.ConditionRolls {
		if !validID(string(v.Condition)) || !validID(string(v.Target)) || v.Face < 1 || v.Face > 1000 {
			return battleFault("condition rolls", "valid instance/target and raw face1..1000", "invalid")
		}
	}
	data, _ := json.Marshal(o.ConditionRolls)
	if len(data) > MaxConditionEvidenceBytes {
		return battleFault("condition rolls", "at most 64KiB encoded human evidence", "exceeded")
	}
	return nil
}

func resolveDamageConditions(b *Blueprint, target CharacterID, resource ResourceID, damage int64, ctx applicationContext) ([]Event, error) {
	if damage <= 0 {
		return nil, nil
	}
	if b.Battle == nil {
		return resolveDamageChecks(b, target, resource, damage, ctx)
	}
	if len(b.Battle.Rules.ConditionChecks) == 0 {
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
		} // A previous failed root check may remove dependents.
		c := b.Conditions[i]
		for _, policy := range b.Battle.Rules.ConditionChecks {
			if policy.Condition != c.Type || policy.Resource != resource {
				continue
			}
			if err := battleWork(ctx); err != nil {
				return nil, err
			}
			modifier, err := explain(b, target, policy.Stat)
			if err != nil {
				return nil, err
			}
			threshold := max(policy.Minimum, min(policy.Maximum, damage/policy.Divisor))
			source, err := b.abilityRollController(Ref{CharacterKind, string(target)})
			if err != nil {
				return nil, err
			}
			roll := CheckResult{Check: string(id), Sides: policy.Sides, Modifier: modifier.Effective, Threshold: threshold, CallsBefore: b.Battle.State.Random.Calls}
			if source == HumanController {
				cursor := ctx.conditionInput
				index := 0
				if cursor != nil {
					index = cursor.Next
				}
				if cursor == nil || index == len(cursor.Values) {
					var supplied []ConditionRollInput
					if cursor != nil {
						supplied = cursor.Values
					}
					prospective := append(append([]ConditionRollInput(nil), supplied...), ConditionRollInput{Condition: id, Target: target, Face: int64(policy.Sides)})
					encoded, _ := json.Marshal(prospective)
					if len(encoded) > MaxConditionEvidenceBytes {
						return nil, battleFault("condition rolls", "at most 64KiB encoded human evidence", "next response exceeds limit")
					}
					need := BattleInputRequirements{Stage: "condition", Count: 1, Sides: int64(policy.Sides), Condition: &ConditionInputNeed{Condition: id, Target: target, Index: index, Damage: damage, Threshold: threshold, Modifier: modifier.Effective}}
					return nil, &InputRequired{Need: need, Err: battleFault("condition roll", "supplied raw human save face", "missing")}
				}
				v := cursor.Values[index]
				if v.Condition != id || v.Target != target || v.Face < 1 || uint64(v.Face) > policy.Sides {
					return nil, battleFault("condition roll", "next exact instance/target and bounded raw face", "mismatch")
				}
				cursor.Next++
				roll.Draw = uint64(v.Face)
			} else {
				roll.Draw, err = drawStateDie(&b.Battle.State.Random, policy.Sides, ctx.random)
				if err != nil {
					return nil, err
				}
			}
			roll.CallsAfter = b.Battle.State.Random.Calls
			roll, err = completeCheck(roll)
			if err != nil {
				return nil, err
			}
			events = append(events, Event{Kind: ConditionCheckResolved, ConditionCheck: &ConditionCheckResult{Condition: *cloneCondition(&c), Policy: policy, Damage: damage, Source: source, Roll: roll}})
			if !roll.Success {
				if err := conditionWork(ctx); err != nil {
					return nil, err
				}
				es, err := removeConditionGroup(b, c, ctx)
				if err != nil {
					return nil, err
				}
				events = append(events, es...)
			}
			break // Rule keys are unique for this type and Resource.
		}
	}
	return events, nil
}
