package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"math"
)

type conditionModifierNode struct {
	ID, Stat, Resource, Amount token
	Bound, Roll                *token
	Die, DieCount, DieSides    *token
}
type conditionDefinitionNode struct {
	UntilSourceTurn, EndsAfterAttack *token
	ID, Policy                       token
	EndsOnCondition                  *token
	EndsAtMinimum                    *token
	HeldAtMinimum                    *token
	Includes                         []token
	ModifierSelection                *token
	Modifiers                        []conditionModifierNode
	Attacks                          []equipmentAttackNode
}
type conditionNode struct {
	ClockDuration    *token
	Anchor           *token
	ID, Type, Target token
	Source           *refNode
	Magnitude        *token
}

func (p *parser) conditionDefinition() conditionDefinitionNode {
	d := conditionDefinitionNode{ID: p.str()}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		if p.isWord("until_source_turn") || p.isWord("ends_after_attack") {
			key := p.take()
			field := &d.UntilSourceTurn
			if key.Text == "ends_after_attack" {
				field = &d.EndsAfterAttack
			}
			if *field != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one lifecycle clause", key.Text, "duplicate Condition lifecycle")
				break
			}
			*field = &key
			p.expect(";")
			continue
		}
		if p.isWord("ends_on_condition") {
			key := p.take()
			if d.EndsOnCondition != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one ending Condition type", key.Text, "duplicate ending Condition type")
				break
			}
			v := p.str()
			d.EndsOnCondition = &v
			p.expect(";")
			continue
		}
		if p.isWord("includes") {
			p.take()
			d.Includes = append(d.Includes, p.str())
			p.expect(";")
			continue
		}
		if p.isWord("attack") {
			p.take()
			d.Attacks = append(d.Attacks, p.equipmentAttack())
			continue
		}
		if p.isWord("modifier_selection") {
			key := p.take()
			if d.ModifierSelection != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one modifier selection", key.Text, "duplicate modifier selection")
				break
			}
			v := p.choice("latest")
			d.ModifierSelection = &v
			p.expect(";")
			continue
		}
		if p.isWord("ends_at_minimum") || p.isWord("held_at_minimum") {
			key := p.take()
			field := &d.EndsAtMinimum
			if key.Text == "held_at_minimum" {
				field = &d.HeldAtMinimum
			}
			if *field != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one Resource floor", key.Text, "duplicate Condition floor")
				break
			}
			v := p.str()
			*field = &v
			p.expect(";")
			continue
		}
		key := p.choice("instances", "modifier")
		switch key.Text {
		case "instances":
			if d.Policy.Text != "" {
				p.err = diag(key.Pos, "E_DUPLICATE", "one instance policy", key.Text, "duplicate instance policy")
				break
			}
			if p.isWord("replace") {
				d.Policy = p.take()
			} else {
				d.Policy = p.choice("coexist", "unique")
			}
		case "modifier":
			m := conditionModifierNode{ID: p.str()}
			if p.choice("stat", "resource").Text == "resource" {
				m.Resource = p.str()
				p.expect("maximum")
				p.expect("add")
			} else {
				m.Stat = p.str()
				// A stat modifier adds, bounds the total (at least or at most),
				// sets how checks on it roll or adds a die to them.
				switch key := p.choice("add", "at", "roll", "die"); key.Text {
				case "at":
					bound := p.choice("least", "most")
					m.Bound = &bound
				case "roll":
					mode := p.choice("advantage", "disadvantage", "fail")
					m.Roll = &mode
				case "die":
					count, sides := p.num(), p.num()
					m.Die, m.DieCount, m.DieSides = &key, &count, &sides
				}
			}
			if m.Roll == nil && m.Die == nil {
				if p.peek().Kind == word && p.peek().Text == "magnitude" {
					m.Amount = p.take()
				} else {
					m.Amount = p.num()
				}
			}
			d.Modifiers = append(d.Modifiers, m)
		}
		p.expect(";")
	}
	if p.err == nil && d.Policy.Text == "" {
		p.err = diag(p.peek().Pos, "E_RULE", "explicit instances policy", "missing", "condition type requires an instance policy")
	}
	p.expect("}")
	return d
}
func (p *parser) condition() conditionNode {
	c := conditionNode{ID: p.str()}
	p.expect("of")
	c.Type = p.str()
	p.expect("to")
	c.Target = p.str()
	for p.peek().Text != ";" && p.err == nil {
		if p.isWord("anchor") {
			key := p.take()
			if c.Anchor != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one anchor", key.Text, "duplicate Condition anchor")
				break
			}
			v := p.str()
			c.Anchor = &v
			continue
		}
		if p.isWord("ticks") {
			key := p.take()
			if c.ClockDuration != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one clock duration", key.Text, "duplicate condition clock duration")
				break
			}
			v := p.num()
			c.ClockDuration = &v
			continue
		}
		key := p.choice("from", "magnitude")
		switch key.Text {
		case "from":
			if c.Source != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one source", key.Text, "duplicate condition source")
				break
			}
			r := p.ref()
			c.Source = &r
		case "magnitude":
			if c.Magnitude != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one magnitude", key.Text, "duplicate condition magnitude")
				break
			}
			v := p.num()
			c.Magnitude = &v
		}
	}
	return c
}
func (a *analyzer) condition(n conditionNode) core.ApplyCondition {
	v := core.ApplyCondition{Instance: core.ConditionInstanceID(n.ID.Text), Type: core.ConditionTypeID(n.Type.Text), Target: core.CharacterID(n.Target.Text)}
	if n.Anchor != nil {
		id := core.ConditionInstanceID(n.Anchor.Text)
		v.Anchor = &id
	}
	if n.ClockDuration != nil {
		ticks := a.unsignedLiteral(*n.ClockDuration, true)
		v.ClockDuration = &ticks
	}
	if n.Source != nil {
		r := ref(*n.Source)
		v.Source = &r
	}
	if n.Magnitude != nil {
		m := a.number(*n.Magnitude)
		v.Magnitude = &m
	}
	return v
}
func (a *analyzer) conditionRefs(b core.Blueprint, n conditionNode) {
	a.checkRef(b, core.CharacterKind, n.Target)
	if n.Source != nil {
		a.checkRef(b, core.EntityKind(n.Source.Kind.Text), n.Source.ID)
	}
}
func conditionOperand(err error, n conditionNode) token {
	var f *core.Fault
	if errors.As(err, &f) {
		// A missing stat/Resource may spell a structural field. The target
		// lacks that immutable fact; the instance/magnitude is not the use site.
		if f.Code == "E_STAT" || f.Code == "E_RESOURCE" {
			return n.Target
		}
		if f.Code == "E_CLOCK" && n.ClockDuration != nil {
			return *n.ClockDuration
		}
		switch f.Field {
		case "anchor":
			if n.Anchor != nil {
				return *n.Anchor
			}
		case "ends_at_minimum", "ends_on_condition":
			return n.Target
		case "condition_type":
			return n.Type
		case "magnitude":
			if n.Magnitude != nil {
				return *n.Magnitude
			}
			return n.ID
		}
	}
	return n.ID
}
func (a *analyzer) conditionDeclarations(b *core.Blueprint, n worldNode) {
	seen := map[string]bool{}
	for _, d := range n.ConditionDefinitions {
		if seen[d.ID.Text] && a.err == nil {
			a.err = diag(d.ID.Pos, "E_DUPLICATE", "unique condition type", d.ID.Text, "duplicate condition type")
		}
		seen[d.ID.Text] = true
		out := core.ConditionDefinition{ID: core.ConditionTypeID(d.ID.Text), Instances: core.InstancePolicy(d.Policy.Text)}
		out.UntilSourceTurn = d.UntilSourceTurn != nil
		out.EndsAfterAttack = d.EndsAfterAttack != nil
		if n.Battle == nil {
			for _, clause := range []*token{d.UntilSourceTurn, d.EndsAfterAttack} {
				if clause != nil {
					a.checkAt(&core.Fault{Code: "E_RULE", Expected: "Battle profile", Actual: "absent", Message: "Condition attack/turn lifecycle requires Battle"}, *clause)
				}
			}
		}
		if d.EndsOnCondition != nil {
			kind := core.ConditionTypeID(d.EndsOnCondition.Text)
			out.EndsOnCondition = &kind
		}
		if d.ModifierSelection != nil {
			out.ModifierSelection = core.ModifierSelection(d.ModifierSelection.Text)
		}
		if d.EndsAtMinimum != nil {
			resource := core.ResourceID(d.EndsAtMinimum.Text)
			out.EndsAtMinimum = &resource
		}
		if d.HeldAtMinimum != nil {
			resource := core.ResourceID(d.HeldAtMinimum.Text)
			out.HeldAtMinimum = &resource
		}
		for _, t := range d.Includes {
			out.Includes = append(out.Includes, core.ConditionTypeID(t.Text))
		}
		attacks := map[string]bool{}
		for _, x := range d.Attacks {
			out.Attacks = append(out.Attacks, a.equipmentAttack(attacks, x))
		}
		for _, m := range d.Modifiers {
			v := core.ConditionModifier{ID: core.ConditionModifierID(m.ID.Text), Stat: core.StatID(m.Stat.Text), ResourceMaximum: core.ResourceID(m.Resource.Text)}
			if m.Bound != nil {
				v.Bound = core.ModifierBound("at_" + m.Bound.Text)
			}
			if m.Die != nil {
				v.Die = &core.AddedDie{Count: a.number(*m.DieCount), Sides: a.number(*m.DieSides)}
			} else if m.Roll != nil {
				v.Roll = core.RollMode(m.Roll.Text)
			} else if m.Amount.Kind == word {
				v.Magnitude = true
			} else {
				x := a.number(m.Amount)
				v.Fixed = &x
			}
			out.Modifiers = append(out.Modifiers, v)
		}
		err := core.CheckConditionDefinition(out)
		pos := d.ID
		var fault *core.Fault
		if errors.As(err, &fault) {
			if fault.Field == "ends_on_condition" && d.EndsOnCondition != nil {
				pos = *d.EndsOnCondition
			}
			if fault.Field == "ends_at_minimum" && d.EndsAtMinimum != nil {
				pos = *d.EndsAtMinimum
			}
			if fault.Field == "held_at_minimum" && d.HeldAtMinimum != nil {
				pos = *d.HeldAtMinimum
			}
			if t, ok := includedToken(fault, d); ok {
				pos = t
			}
			seenModifiers := map[string]bool{}
			for _, m := range d.Modifiers {
				// A held type's modifier that is neither a fixed bound nor a roll.
				heldModifier := fault.Field == "held_at_minimum" && fault.Expected == "fixed bound or roll modifiers only" && m.ID.Text == fault.Actual
				if heldModifier || fault.Field == "modifier" && ((fault.Code == "E_DUPLICATE" && seenModifiers[m.ID.Text]) || (fault.Code != "E_DUPLICATE" && m.ID.Text == fault.Actual)) {
					pos = m.ID
					break
				}
				if fault.Field == "stat" && m.Stat.Text == fault.Actual {
					pos = m.Stat
					break
				}
				if fault.Field == "resource_maximum" && m.Resource.Text == fault.Actual {
					pos = m.Resource
					break
				}
				if fault.Field == "die" && m.Die != nil && m.ID.Text == fault.Actual {
					pos = *m.Die
					break
				}
				seenModifiers[m.ID.Text] = true
			}
		}
		a.checkAt(err, pos)
		b.ConditionDefinitions = append(b.ConditionDefinitions, out)
	}
	for i, d := range n.ConditionDefinitions {
		err := b.CheckConditionDefinitionReferences(b.ConditionDefinitions[i])
		var fault *core.Fault
		if !errors.As(err, &fault) {
			continue
		}
		if t, ok := includedToken(fault, d); ok {
			a.checkAt(err, t)
		} else if d.EndsOnCondition != nil {
			a.checkAt(err, *d.EndsOnCondition)
		}
	}
	seen = map[string]bool{}
	for _, c := range n.Conditions {
		if seen[c.ID.Text] && a.err == nil {
			a.err = diag(c.ID.Pos, "E_DUPLICATE", "unique condition instance", c.ID.Text, "duplicate condition instance")
		}
		seen[c.ID.Text] = true
		v := a.condition(c)
		a.conditionRefs(*b, c)
		err := b.CheckConditionStatic(v)
		a.checkAt(err, conditionOperand(err, c))
		instance := core.ConditionInstance{ID: v.Instance, Type: v.Type, Target: v.Target, Source: v.Source, Magnitude: v.Magnitude, Anchor: v.Anchor}
		if v.ClockDuration != nil && b.Clock != nil {
			if *v.ClockDuration > math.MaxUint64-b.Clock.Tick {
				if a.err == nil {
					a.err = diag(c.ClockDuration.Pos, "E_CLOCK", "nonoverflowing deadline", c.ClockDuration.Text, "clock lifetime exceeds uint64")
				}
			} else {
				instance.ClockLifetime = &core.ClockLifetime{AppliedTick: b.Clock.Tick, ExpiresTick: b.Clock.Tick + *v.ClockDuration}
			}
		}
		b.Conditions = append(b.Conditions, instance)
	}
}
func (a *analyzer) conditionAction(b core.Blueprint, n actionNode) core.Operation {
	c := *n.Condition
	var o core.Operation
	if n.Verb.Text == "apply" {
		v := a.condition(c)
		o.ApplyCondition = &v
		a.conditionRefs(b, c)
	} else {
		o.RemoveCondition = &core.RemoveCondition{Instance: core.ConditionInstanceID(c.ID.Text), Target: core.CharacterID(c.Target.Text), Type: core.ConditionTypeID(c.Type.Text)}
		a.checkRef(b, core.CharacterKind, c.Target)
	}
	err := b.CheckOperationStatic(o)
	a.checkAt(err, conditionOperand(err, c))
	return o
}

// includedToken is the includes clause a fault names: the last one, which
// for a type named twice is the repeat.
func includedToken(f *core.Fault, d conditionDefinitionNode) (token, bool) {
	var out token
	found := false
	if f.Field == "includes" {
		for _, t := range d.Includes {
			if t.Text == f.Actual {
				out, found = t, true
			}
		}
	}
	return out, found
}
