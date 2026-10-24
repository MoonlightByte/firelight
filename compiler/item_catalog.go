package compiler

import (
	"strconv"
	"strings"

	"github.com/MoonlightByte/firelight/core"
)

type itemDefinitionNode struct {
	namedNode
	Fields      map[string]token
	Price       *itemPriceNode
	InitialDice *chargeDiceNode
	Recharge    *rechargeNode
	Exhausted   *exhaustedNode
}
type itemPriceNode struct{ Unit, Amount, Units token }
type chargeDiceNode struct {
	Count, Sides token
	Plus         *token
}
type rechargeNode struct {
	Every, Amount token
	At, By        *token
	Dice          *chargeDiceNode
}
type exhaustedNode struct {
	Mode                    token
	Roll, Destroy, RegainAt *token
	Regain                  *chargeDiceNode
}

// chargeDice parses `C sides S [plus B]`.
func (p *parser) chargeDice() chargeDiceNode {
	d := chargeDiceNode{Count: p.num()}
	p.expect("sides")
	d.Sides = p.num()
	if p.isWord("plus") {
		p.take()
		t := p.num()
		d.Plus = &t
	}
	return d
}

func (p *parser) itemDefinition() itemDefinitionNode {
	n := itemDefinitionNode{namedNode: p.named(), Fields: map[string]token{}}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		key := p.choice("description", "kind", "subtype", "magical", "consumable", "equipment", "price", "charges", "initial", "recharge", "exhausted")
		if _, exists := n.Fields[key.Text]; exists {
			p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, key.Text, "duplicate item definition field")
			break
		}
		if key.Text == "price" {
			v := itemPriceNode{Unit: p.str(), Amount: p.num()}
			p.expect("per")
			v.Units = p.num()
			n.Price = &v
			n.Fields[key.Text] = key
		} else if key.Text == "initial" && p.isWord("dice") {
			p.take()
			d := p.chargeDice()
			n.InitialDice = &d
			n.Fields[key.Text] = d.Count
		} else if key.Text == "charges" || key.Text == "initial" {
			n.Fields[key.Text] = p.num()
		} else if key.Text == "recharge" {
			// recharge every P [at O] (all | by N | dice C sides S [plus B])
			p.expect("every")
			r := rechargeNode{Every: p.num()}
			if p.isWord("at") {
				p.take()
				t := p.num()
				r.At = &t
			}
			r.Amount = p.choice("all", "by", "dice")
			if r.Amount.Text == "by" {
				t := p.num()
				r.By = &t
			} else if r.Amount.Text == "dice" {
				d := p.chargeDice()
				r.Dice = &d
			}
			n.Recharge = &r
			n.Fields[key.Text] = key
		} else if key.Text == "exhausted" {
			// exhausted destroy | exhausted roll S [destroy D] [regain R dice C sides S [plus B]]
			x := exhaustedNode{Mode: p.choice("destroy", "roll")}
			if x.Mode.Text == "roll" {
				t := p.num()
				x.Roll = &t
				if p.isWord("destroy") {
					p.take()
					t := p.num()
					x.Destroy = &t
				}
				if p.isWord("regain") {
					p.take()
					t := p.num()
					x.RegainAt = &t
					p.expect("dice")
					d := p.chargeDice()
					x.Regain = &d
				}
			}
			n.Exhausted = &x
			n.Fields[key.Text] = key
		} else if key.Text == "magical" || key.Text == "consumable" {
			n.Fields[key.Text] = p.choice("true", "false")
		} else {
			n.Fields[key.Text] = p.str()
		}
		p.expect(";")
	}
	p.expect("}")
	return n
}

func (a *analyzer) itemDefinitions(ns []itemDefinitionNode) []core.ItemDefinition {
	var out []core.ItemDefinition
	for _, n := range ns {
		d := core.ItemDefinition{ID: core.ItemDefinitionID(n.ID.Text), Name: n.Name.Text,
			Description: n.Fields["description"].Text, Kind: n.Fields["kind"].Text,
			Subtype: n.Fields["subtype"].Text, Magical: n.Fields["magical"].Text == "true",
			Consumable: n.Fields["consumable"].Text == "true", Equipment: core.EquipmentDefinitionID(n.Fields["equipment"].Text)}
		if t, ok := n.Fields["equipment"]; ok && t.Text == "" {
			a.checkAt(&core.Fault{Code: "E_CATALOG", Subject: n.ID.Text, Field: "equipment", Expected: "declared equipment definition", Actual: "", Message: "invalid item catalog equipment"}, t)
		}
		if n.Price != nil {
			d.Price = &core.ItemPrice{Unit: n.Price.Unit.Text, Amount: a.number(n.Price.Amount), UnitsPerLot: a.number(n.Price.Units)}
		}
		if t, ok := n.Fields["charges"]; ok {
			d.Charges = a.chargeRule(t, n)
		} else {
			for _, field := range []string{"initial", "recharge", "exhausted"} {
				if t, ok := n.Fields[field]; ok {
					a.checkAt(&core.Fault{Code: "E_CATALOG", Subject: n.ID.Text, Field: "charges." + field, Expected: "charges declared", Actual: field + " without charges", Message: "invalid item catalog charges." + field}, t)
					break
				}
			}
		}
		out = append(out, d)
	}
	return out
}

func (a *analyzer) chargeDice(n chargeDiceNode) *core.ChargeDice {
	d := &core.ChargeDice{Count: a.number(n.Count), Sides: a.number(n.Sides)}
	if n.Plus != nil {
		d.Plus = a.number(*n.Plus)
	}
	return d
}

func (a *analyzer) chargeRule(max token, n itemDefinitionNode) *core.ChargeRule {
	r := &core.ChargeRule{Max: a.number(max)}
	if n.InitialDice != nil {
		r.InitialDice = a.chargeDice(*n.InitialDice)
	} else if t, ok := n.Fields["initial"]; ok {
		initial := a.number(t)
		r.Initial = &initial
	}
	if v := n.Recharge; v != nil {
		rc := &core.ChargeRecharge{Every: a.tick(v.Every), All: v.Amount.Text == "all"}
		if v.At != nil {
			rc.At = a.tick(*v.At)
		}
		if v.By != nil {
			rc.By = a.number(*v.By)
		}
		if v.Dice != nil {
			rc.Dice = a.chargeDice(*v.Dice)
		}
		r.Recharge = rc
	}
	if v := n.Exhausted; v != nil {
		x := &core.ChargeExhausted{Destroy: v.Mode.Text == "destroy"}
		for _, f := range []struct {
			t    *token
			dest *int64
		}{{v.Roll, &x.Roll}, {v.Destroy, &x.DestroyAtMost}, {v.RegainAt, &x.RegainAtLeast}} {
			if f.t != nil {
				*f.dest = a.number(*f.t)
			}
		}
		if v.Regain != nil {
			x.Regain = a.chargeDice(*v.Regain)
		}
		r.Exhausted = x
	}
	return r
}

// tick reads an unsigned 64-bit tick, period or seed word.
func (a *analyzer) tick(t token) uint64 {
	v, err := strconv.ParseUint(t.Text, 10, 64)
	if err != nil && a.err == nil {
		a.err = diag(t.Pos, "E_INTEGER", "unsigned 64-bit integer", t.Text, "integer literal exceeds the unsigned domain")
	}
	return v
}

// chargeRuleOperand is the source token a charge-rule fault names.
func chargeRuleOperand(field string, n itemDefinitionNode) (token, bool) {
	dice := func(prefix string, d *chargeDiceNode) (token, bool) {
		if d == nil {
			return token{}, false
		}
		switch field {
		case prefix + ".count":
			return d.Count, true
		case prefix + ".sides":
			return d.Sides, true
		case prefix + ".plus":
			if d.Plus != nil {
				return *d.Plus, true
			}
		}
		return token{}, false
	}
	if t, ok := dice("charges.initial", n.InitialDice); ok {
		return t, true
	}
	if r := n.Recharge; r != nil {
		if t, ok := dice("charges.recharge.dice", r.Dice); ok {
			return t, true
		}
		switch field {
		case "charges.recharge.every":
			return r.Every, true
		case "charges.recharge.at":
			if r.At != nil {
				return *r.At, true
			}
		case "charges.recharge.amount":
			return r.Amount, true
		case "charges.recharge.by":
			if r.By != nil {
				return *r.By, true
			}
		}
	}
	if x := n.Exhausted; x != nil {
		if t, ok := dice("charges.exhausted.regain", x.Regain); ok {
			return t, true
		}
		for _, f := range []struct {
			name string
			t    *token
		}{{"charges.exhausted.roll", x.Roll}, {"charges.exhausted.destroy", x.Destroy}, {"charges.exhausted.regain", x.RegainAt}} {
			if field == f.name && f.t != nil {
				return *f.t, true
			}
		}
		if field == "charges.exhausted" || field == "charges.exhausted.regain" {
			return x.Mode, true
		}
	}
	return token{}, false
}

func catalogWorldOperand(f *core.Fault, n worldNode) (token, bool) {
	if f.Code != "E_CATALOG" {
		return token{}, false
	}
	// Source cannot override derived display/equipment fields. The only
	// instance faults here are a missing reference or an ordinary item's mode;
	// definition faults must stay in the definition namespace when IDs match.
	if f.Field == "from" || f.Field == "mode" {
		for _, i := range n.Items {
			if i.ID.Text == f.Subject && i.From != nil {
				if f.Field == "mode" && i.Mode != nil {
					return *i.Mode, true
				}
				return *i.From, true
			}
		}
	}
	// A duplicate is diagnosed at the second declaration, not the first.
	var first *itemDefinitionNode
	for j := range n.ItemDefinitions {
		d := &n.ItemDefinitions[j]
		if d.ID.Text != f.Subject {
			continue
		}
		if first != nil && f.Field == "id" {
			return d.ID, true
		}
		first = d
	}
	if first == nil {
		return token{}, false
	}
	if f.Field == "name" {
		return first.Name, true
	}
	if t, ok := first.Fields[f.Field]; ok {
		return t, true
	}
	if t, ok := chargeRuleOperand(f.Field, *first); ok {
		return t, true
	}
	key := map[string]string{"charges.max": "charges", "charges.initial": "initial"}[f.Field]
	for _, field := range []string{"recharge", "exhausted"} {
		if strings.HasPrefix(f.Field, "charges."+field) {
			key = field
		}
	}
	if t, ok := first.Fields[key]; ok {
		return t, true
	}
	if p := first.Price; p != nil {
		switch f.Field {
		case "price.unit":
			return p.Unit, true
		case "price.amount":
			return p.Amount, true
		case "price.units_per_lot":
			return p.Units, true
		}
	}
	return first.ID, true
}

// chargesItemOperand places an E_CHARGES fault at the item operand it names,
// else at the declared charges, the definition reference or the item ID.
func chargesItemOperand(f *core.Fault, i itemNode) token {
	switch f.Field {
	case "charges.max":
		if i.ChargesMax != nil {
			return *i.ChargesMax
		}
	case "charges.as_of":
		if i.ChargesSince != nil {
			return *i.ChargesSince
		}
	case "charges.seed":
		if i.ChargesSeed != nil {
			return i.ChargesSeed[0]
		}
	case "quantity":
		if i.Quantity != nil {
			return *i.Quantity
		}
	case "container":
		if i.Container != nil {
			return *i.Container
		}
	case "recoverable":
		if i.Recoverable != nil {
			return *i.Recoverable
		}
	}
	if i.Charges != nil {
		return *i.Charges
	}
	if i.From != nil {
		return *i.From
	}
	return i.ID
}
