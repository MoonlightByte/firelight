package compiler

import "github.com/MoonlightByte/firelight/core"

type placeNode struct {
	Item, Handler token
	From, To      refNode
}
type custodyEndpointNode struct {
	Kind    token
	Subject abilityOperandNode
}
type placeTemplateNode struct {
	Item, Handler abilityOperandNode
	From, To      custodyEndpointNode
}

func (p *parser) place() placeNode {
	v := placeNode{Item: p.str()}
	p.expect("from")
	v.From = p.ref()
	p.expect("to")
	v.To = p.ref()
	p.expect("by")
	v.Handler = p.str()
	return v
}
func (p *parser) custodyEndpoint() custodyEndpointNode {
	v := custodyEndpointNode{Kind: p.choice("character", "item", "location")}
	if v.Kind.Text == "location" {
		v.Subject.Value = p.str()
	} else {
		v.Subject = p.abilityOperand(false)
	}
	return v
}
func (p *parser) placeTemplate() *placeTemplateNode {
	v := &placeTemplateNode{Item: p.abilityOperand(false)}
	p.expect("from")
	v.From = p.custodyEndpoint()
	p.expect("to")
	v.To = p.custodyEndpoint()
	p.expect("by")
	v.Handler = p.abilityOperand(false)
	return v
}
func (a *analyzer) custodyEndpoint(v custodyEndpointNode) core.CustodyEndpoint {
	out := core.CustodyEndpoint{Kind: core.EntityKind(v.Kind.Text)}
	if out.Kind == core.LocationKind {
		out.Location = core.LocationID(v.Subject.Value.Text)
	} else {
		k := core.CharacterArgument
		if out.Kind == core.ItemKind {
			k = core.ItemArgument
		}
		out.Subject = a.operand(v.Subject, k)
	}
	return out
}
func (a *analyzer) placeTemplate(v *placeTemplateNode) *core.PlaceTemplate {
	return &core.PlaceTemplate{Item: a.operand(v.Item, core.ItemArgument), Handler: a.operand(v.Handler, core.CharacterArgument), From: a.custodyEndpoint(v.From), To: a.custodyEndpoint(v.To)}
}
func (v placeTemplateNode) operandToken(field string) token {
	switch field {
	case "handler":
		return v.Handler.Value
	case "from":
		return v.From.Subject.Value
	case "to":
		return v.To.Subject.Value
	default:
		return v.Item.Value
	}
}
