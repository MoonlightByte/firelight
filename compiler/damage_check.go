package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"unicode"
)

// damageFacesNode is `faces "<instance>" <face> [<face>]` on a damage action:
// the player's dice for the check that damage triggers on that instance.
type damageFacesNode struct {
	Key, Instance token
	Faces         []token
}

// conditionCheck parses `condition_check "<type>" after_damage resource
// "<resource>" stat "<stat>" sides <n> minimum <n> divide <n> maximum <n>;`,
// in a Battle's rules or, outside Battle, in the world.
func (p *parser) conditionCheck() conditionCheckNode {
	v := conditionCheckNode{Start: p.take(), Condition: p.str()}
	p.expect("after_damage")
	p.expect("resource")
	v.Resource = p.str()
	p.expect("stat")
	v.Stat = p.str()
	p.expect("sides")
	v.Sides = p.num()
	p.expect("minimum")
	v.Minimum = p.num()
	p.expect("divide")
	v.Divisor = p.num()
	p.expect("maximum")
	v.Maximum = p.num()
	p.expect(";")
	return v
}

func (p *parser) damageFaces() []damageFacesNode {
	var out []damageFacesNode
	for p.isWord("faces") && p.err == nil {
		v := damageFacesNode{Key: p.take(), Instance: p.str()}
		v.Faces = append(v.Faces, p.num())
		for p.peek().Kind == integer && p.err == nil {
			v.Faces = append(v.Faces, p.num())
		}
		out = append(out, v)
	}
	return out
}

func ruleFault(field, expected, actual, message string) *core.Fault {
	return &core.Fault{Code: "E_RULE", Field: field, Expected: expected, Actual: actual, Message: message}
}

// worldConditionChecks lowers the damage-triggered checks a world declares
// outside Battle.
func (a *analyzer) worldConditionChecks(b *core.Blueprint, n worldNode) {
	if len(n.ConditionChecks) > 0 && n.Battle != nil {
		a.checkAt(ruleFault("condition_checks", "no Battle, whose rules declare their own", "Battle", "world condition checks with a Battle"), n.ConditionChecks[0].Start)
	}
	seen := map[[2]string]bool{}
	for i, v := range n.ConditionChecks {
		if i >= 16 {
			a.checkAt(&core.Fault{Code: "E_LIMIT", Field: "condition_checks", Expected: "at most 16", Actual: "more", Message: "too many condition checks"}, v.Start)
		}
		declared := false
		for _, d := range b.ConditionDefinitions {
			declared = declared || string(d.ID) == v.Condition.Text
		}
		if !declared {
			a.checkAt(&core.Fault{Code: "E_CONDITION_TYPE", Field: "condition_check", Expected: "declared Condition type", Actual: v.Condition.Text, Message: "condition check names a missing type"}, v.Condition)
		}
		a.requireCheckID(v.Resource, "resource")
		a.requireCheckID(v.Stat, "stat")
		key := [2]string{v.Condition.Text, v.Resource.Text}
		if seen[key] {
			a.checkAt(&core.Fault{Code: "E_DUPLICATE", Field: "condition_check", Expected: "one check per Condition type and Resource", Actual: v.Condition.Text, Message: "duplicate condition check"}, v.Condition)
		}
		seen[key] = true
		p := core.ConditionCheckPolicy{Condition: core.ConditionTypeID(v.Condition.Text), Resource: core.ResourceID(v.Resource.Text), Stat: core.StatID(v.Stat.Text), Sides: uint64(a.number(v.Sides)), Minimum: a.number(v.Minimum), Divisor: a.number(v.Divisor), Maximum: a.number(v.Maximum)}
		switch {
		case a.number(v.Sides) < 2 || p.Sides > 1000:
			a.checkAt(ruleFault("sides", "2..1000", v.Sides.Text, "invalid check die"), v.Sides)
		case p.Minimum < 1:
			a.checkAt(ruleFault("minimum", "positive DC minimum", v.Minimum.Text, "invalid check DC"), v.Minimum)
		case p.Divisor < 1:
			a.checkAt(ruleFault("divide", "positive divisor", v.Divisor.Text, "invalid check DC"), v.Divisor)
		case p.Maximum < p.Minimum:
			a.checkAt(ruleFault("maximum", "at least the minimum", v.Maximum.Text, "invalid check DC"), v.Maximum)
		}
		b.ConditionChecks = append(b.ConditionChecks, p)
	}
	// A checked type's instances need the policy's Resource and stat.
	for _, c := range n.Conditions {
		for _, p := range b.ConditionChecks {
			if string(p.Condition) == c.Type.Text {
				a.checkAt(b.CheckResource(core.CharacterID(c.Target.Text), p.Resource), c.Target)
				a.checkAt(b.CheckStat(core.CharacterID(c.Target.Text), p.Stat), c.Target)
			}
		}
	}
}

// requireCheckID refuses an empty or control-character identity at its token.
func (a *analyzer) requireCheckID(t token, field string) {
	bad := strings.TrimSpace(t.Text) == ""
	for _, r := range t.Text {
		bad = bad || unicode.IsControl(r)
	}
	if bad {
		a.checkAt(&core.Fault{Code: "E_ID", Field: field, Expected: "valid explicit identity", Actual: t.Text, Message: "invalid condition check " + field}, t)
	}
}

// damageFaces lowers a damage action's faces to ordered condition rolls. In
// a Battle world they come from the trusted host instead.
func (a *analyzer) damageFaces(b core.Blueprint, target token, nodes []damageFacesNode) []core.ConditionRollInput {
	var out []core.ConditionRollInput
	for _, v := range nodes {
		if b.Battle != nil {
			a.checkAt(ruleFault("faces", "a world without Battle", "Battle", "Battle check faces come from the host"), v.Key)
		}
		if len(v.Faces) > 2 {
			a.checkAt(&core.Fault{Code: "E_ROLL", Field: "faces", Expected: "at most 2 faces", Actual: v.Key.Text, Message: "too many check faces"}, v.Key)
		}
		for _, t := range v.Faces {
			face := a.number(t)
			if face < 1 || face > 1000 {
				a.checkAt(&core.Fault{Code: "E_ROLL", Field: "faces", Expected: "faces in 1..1000", Actual: t.Text, Message: "check face outside any die"}, t)
			}
			out = append(out, core.ConditionRollInput{Condition: core.ConditionInstanceID(v.Instance.Text), Target: core.CharacterID(target.Text), Face: face})
		}
	}
	return out
}
