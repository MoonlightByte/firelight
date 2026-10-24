package compiler

import "github.com/MoonlightByte/firelight/core"

type relationDefinitionNode struct {
	Kind, Minimum, Maximum token
	Decay                  *relationDecayNode
}
type relationDecayNode struct{ Start, Period, Numerator, Denominator, Baseline token }

func (p *parser) relationDefinition() relationDefinitionNode {
	n := relationDefinitionNode{Kind: p.str()}
	p.expect("min")
	n.Minimum = p.num()
	p.expect("max")
	n.Maximum = p.num()
	if p.isWord("decay") {
		d := relationDecayNode{Start: p.take()}
		p.expect("every")
		d.Period = p.num()
		p.expect("retain")
		d.Numerator = p.num()
		p.expect("of")
		d.Denominator = p.num()
		p.expect("baseline")
		d.Baseline = p.num()
		n.Decay = &d
	}
	p.expect(";")
	return n
}

func (a *analyzer) relationDefinitions(b *core.Blueprint, nodes []relationDefinitionNode) {
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.Kind.Text] && a.err == nil {
			a.err = diag(n.Kind.Pos, "E_DUPLICATE", "unique relation kind definition", n.Kind.Text, "duplicate relation kind definition")
		}
		seen[n.Kind.Text] = true
		d := core.RelationDefinition{Kind: core.RelationKind(n.Kind.Text), Minimum: a.number(n.Minimum), Maximum: a.number(n.Maximum)}
		if x := n.Decay; x != nil {
			period := a.number(x.Period)
			if period <= 0 {
				a.checkAt(&core.Fault{Code: "E_RELATION_DECAY", Field: "period", Expected: "positive logical period", Actual: x.Period.Text, Message: "invalid relationship decay period"}, x.Period)
			}
			d.Decay = &core.RelationDecay{Period: uint64(period), Numerator: a.number(x.Numerator), Denominator: a.number(x.Denominator), Baseline: a.number(x.Baseline)}
			if b.Clock == nil {
				a.checkAt(&core.Fault{Code: "E_CLOCK", Field: "relationship_decay", Expected: "Clock present", Actual: "absent", Message: "relationship decay requires a Clock"}, x.Start)
			}
		}
		position := n.Minimum
		if d.Maximum < 0 || d.Maximum <= d.Minimum {
			position = n.Maximum
		}
		err := core.CheckRelationDefinition(d)
		if f, ok := err.(*core.Fault); ok {
			if f.Code == "E_ID" {
				position = n.Kind
			}
			if n.Decay != nil {
				switch f.Field {
				case "period":
					position = n.Decay.Period
				case "numerator":
					position = n.Decay.Numerator
				case "denominator":
					position = n.Decay.Denominator
				case "baseline":
					position = n.Decay.Baseline
				}
			}
		}
		a.checkAt(err, position)
		b.RelationDefinitions = append(b.RelationDefinitions, d)
	}
}
