package core

import (
	"fmt"
	"math/big"
	"sort"
)

// CheckBoostPolicy offers one optional bonus after failure. Cost is exact and
// payable only when that bonus changes the Check into success. The actor owns
// both the required Condition and Resource; this is not a generic reaction.
type CheckBoostPolicy struct {
	Condition ConditionTypeID `json:"condition"`
	Resource  ResourceID      `json:"resource"`
	Cost      int64           `json:"cost"`
	Sides     uint64          `json:"sides"`
}
type CheckBoostInput struct {
	Step int    `json:"step"`
	Use  bool   `json:"use"`
	Face *int64 `json:"face,omitempty"`
}
type CheckBoostResult struct {
	Policy    CheckBoostPolicy      `json:"policy"`
	Grants    []ConditionInstanceID `json:"grants"`
	BaseTotal int64                 `json:"base_total"`
	Used      bool                  `json:"used"`
	Draw      uint64                `json:"draw"`
	Paid      int64                 `json:"paid"`
}

// This is provisional input context, never a committed Check or snapshot.
type CheckBoostNeed struct {
	Base   CheckResult      `json:"base"`
	Policy CheckBoostPolicy `json:"policy"`
}

func cloneCheckBoostInput(v *CheckBoostInput) *CheckBoostInput {
	if v == nil {
		return nil
	}
	out := *v
	out.Face = copyPtr(v.Face)
	return &out
}
func cloneCheckResult(v *CheckResult) *CheckResult {
	if v == nil {
		return nil
	}
	out := *v
	if v.Boost != nil {
		out.Boost = copyPtr(v.Boost)
		out.Boost.Grants = append([]ConditionInstanceID(nil), v.Boost.Grants...)
	}
	return &out
}
func (b Blueprint) checkBoostPolicy(d AbilityDefinition, c CheckTemplate) error {
	p := c.Boost
	if p == nil {
		return nil
	}
	fault := func(expected, actual string) error { return abilityFault("E_RULE", "boost", expected, actual) }
	if !c.ActorRoll {
		return fault("actor_roll Check", "automatic Check")
	}
	if conditionDefinition(&b, p.Condition) == nil {
		return fault("declared granting Condition", string(p.Condition))
	}
	if !validID(string(p.Resource)) || p.Cost <= 0 || p.Sides < 2 || p.Sides > 1000 {
		return fault("valid Resource, positive cost, 2..1000 sides", fmt.Sprint(*p))
	}
	if d.Action != nil && d.Action.Actor.Literal != nil {
		return b.CheckResource(*d.Action.Actor.Literal.Character, p.Resource)
	}
	return nil
}

func (b *Blueprint) resolveCheckBoost(v InvokeAbility, step int, c CheckTemplate, out CheckResult, ctx applicationContext) (CheckResult, error) {
	p, input := c.Boost, v.CheckBoost
	fault := func(expected, actual string) error { return abilityFault("E_ROLL", "boost", expected, actual) }
	if p == nil {
		return out, nil
	}
	if out.Source == AgentController && input != nil && input.Face != nil {
		return out, fault("no supplied NPC bonus face", "present")
	}
	if out.Success {
		if out.Source == HumanController && input != nil {
			return out, fault("no bonus input on base success", "present")
		}
		return out, nil // NPC's declared contingency is unused, not a bonus roll.
	}
	actor := CharacterID(ctx.cause.Actor.ID)
	grants := []ConditionInstanceID{}
	for _, grant := range b.Conditions {
		if grant.Target == actor && grant.Type == p.Condition {
			grants = append(grants, grant.ID)
		}
	}
	eligible := len(grants) != 0
	if eligible {
		if err := b.CheckResource(actor, p.Resource); err != nil {
			return out, err
		}
		r := character(b, actor).Resources[p.Resource]
		available := new(big.Int).Sub(big.NewInt(r.Current), big.NewInt(r.Minimum))
		eligible = available.Cmp(big.NewInt(p.Cost)) >= 0
	}
	if !eligible {
		if input != nil {
			return out, fault("eligible actor grant and exact cost before bonus input", "unavailable")
		}
		return out, nil
	}
	if input == nil {
		if out.Source == AgentController {
			return out, nil
		} // no declared contingency
		return out, &InputRequired{Need: BattleInputRequirements{Stage: "check_boost", Count: 1, Sides: int64(p.Sides), AbilityStep: copyPtr(&step), CheckBoost: &CheckBoostNeed{Base: out, Policy: *p}}, Err: fault("explicit use or decline", "absent")}
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i] < grants[j] })
	out.Boost = &CheckBoostResult{Policy: *p, Grants: grants, BaseTotal: out.Total, Used: input.Use}
	if !input.Use {
		return out, nil
	}
	*ctx.work++
	if *ctx.work > MaxOperations {
		return out, abilityFault("E_LIMIT", "work", "at most 128", "check boost exceeded")
	}
	if out.Source == HumanController {
		if input.Face == nil {
			return out, fault("one raw human bonus face", "absent")
		}
		out.Boost.Draw = uint64(*input.Face)
	} else {
		var err error
		out.Boost.Draw, err = drawStateDie(&b.Battle.State.Random, p.Sides, ctx.random)
		if err != nil {
			return out, err
		}
	}
	// Freeze this comparison before payment: spending may end a stat-contributing
	// Condition. That consequence cannot retroactively change the accepted Check.
	total := new(big.Int).Add(big.NewInt(out.Total), new(big.Int).SetUint64(out.Boost.Draw))
	if !total.IsInt64() {
		return out, fail("E_OVERFLOW", c.ID, "boost", "int64 total", total.String(), "boosted Check total exceeds numeric domain")
	}
	out.Total = total.Int64()
	out.Success = out.Total >= out.Threshold
	if out.Success {
		out.Boost.Paid = p.Cost
	}
	out.CallsAfter = b.Battle.State.Random.Calls
	return out, nil
}
