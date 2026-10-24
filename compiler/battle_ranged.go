package compiler

import "github.com/MoonlightByte/firelight/core"

// rangedProfileNode's Origin is an item, or an item type after From.
type rangedProfileNode struct {
	Normal, Maximum, Stat, Origin token
	From                          *token
}
type thrownProfileNode struct{ Normal, Maximum, Stat token }
type rangedPolicyNode struct {
	Start, Close, Numerator, Denominator, Ticks token
	Ignore, Concealed                           []token
}
type ammunitionCreationNode struct{ Origin, Created token }

func (p *parser) rangedProfile() *rangedProfileNode {
	p.expect("ranged")
	p.expect("normal")
	n := &rangedProfileNode{Normal: p.num()}
	p.expect("maximum")
	n.Maximum = p.num()
	p.expect("stat")
	n.Stat = p.str()
	p.expect("ammunition")
	if p.isWord("from") {
		t := p.take()
		n.From = &t
	}
	n.Origin = p.str()
	p.expect(";")
	return n
}

// thrownProfile is `thrown normal <n> maximum <m> stat "<s>";`.
func (p *parser) thrownProfile() *thrownProfileNode {
	p.expect("thrown")
	p.expect("normal")
	n := &thrownProfileNode{Normal: p.num()}
	p.expect("maximum")
	n.Maximum = p.num()
	p.expect("stat")
	n.Stat = p.str()
	p.expect(";")
	return n
}
func (p *parser) rangedPolicy() *rangedPolicyNode {
	n := &rangedPolicyNode{Start: p.take()}
	p.expect("close")
	n.Close = p.num()
	p.expect("recover")
	n.Numerator = p.num()
	p.expect("of")
	n.Denominator = p.num()
	p.expect("ticks")
	n.Ticks = p.num()
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		kind := p.choice("ignore_threat", "concealed")
		id := p.str()
		p.expect(";")
		if kind.Text == "ignore_threat" {
			n.Ignore = append(n.Ignore, id)
		} else {
			n.Concealed = append(n.Concealed, id)
		}
	}
	p.expect("}")
	return n
}
func (a *analyzer) rangedProfile(n *rangedProfileNode) *core.RangedProfile {
	a.requireEquipmentID(n.Stat)
	p := &core.RangedProfile{Stat: core.StatID(n.Stat.Text)}
	p.Normal, p.Maximum = a.ranges(n.Normal, n.Maximum)
	if n.From != nil {
		p.AmmunitionType = core.ItemDefinitionID(n.Origin.Text)
	} else {
		p.AmmunitionOrigin = core.ItemID(n.Origin.Text)
	}
	return p
}
func (a *analyzer) thrownProfile(n *thrownProfileNode) *core.ThrownProfile {
	a.requireEquipmentID(n.Stat)
	p := &core.ThrownProfile{Stat: core.StatID(n.Stat.Text)}
	p.Normal, p.Maximum = a.ranges(n.Normal, n.Maximum)
	return p
}

// ranges lowers a ranged or thrown profile's normal and maximum range.
func (a *analyzer) ranges(normal, maximum token) (int64, int64) {
	n, m := a.battleNumber(normal, 1, 1000000), a.battleNumber(maximum, 1, 1000000)
	if m < n {
		a.checkAt(coreEquipmentError("", "range", "maximum >= normal", maximum.Text), maximum)
	}
	return n, m
}
func (a *analyzer) rangedPolicy(b core.Blueprint, n *rangedPolicyNode) *core.BattleRangedPolicy {
	if n == nil {
		return nil
	}
	p := &core.BattleRangedPolicy{CloseDistance: a.battleNumber(n.Close, 0, 1000000), RecoveryDenominator: a.battleNumber(n.Denominator, 1, 1000), RecoveryTicks: a.battleNumber(n.Ticks, 1, 1<<63-1)}
	p.RecoveryNumerator = a.battleNumber(n.Numerator, 0, p.RecoveryDenominator)
	for i, list := range [][]token{n.Ignore, n.Concealed} {
		seen := map[string]bool{}
		for j, t := range list {
			a.battleCondition(b, t)
			if j >= 16 || seen[t.Text] {
				a.checkAt(coreEquipmentError("", "ranged policy", "at most 16 unique Conditions", t.Text), t)
			}
			seen[t.Text] = true
			if i == 0 {
				p.IgnoreThreat = append(p.IgnoreThreat, core.ConditionTypeID(t.Text))
			} else {
				p.Concealed = append(p.Concealed, core.ConditionTypeID(t.Text))
			}
		}
	}
	return p
}

// rangedAmmunition checks a ranged profile's ammunition: an original plain
// stock, or a declared item type without equipment.
func (a *analyzer) rangedAmmunition(b core.Blueprint, n *rangedProfileNode) {
	if n.From == nil {
		a.ammunitionOrigin(b, n.Origin)
		return
	}
	for _, d := range b.ItemDefinitions {
		if string(d.ID) == n.Origin.Text && d.Equipment == "" {
			return
		}
	}
	a.checkAt(coreEquipmentError(n.Origin.Text, "ammunition type", "declared item type without equipment", n.Origin.Text), n.Origin)
}
func (a *analyzer) ammunitionOrigin(b core.Blueprint, origin token) {
	a.checkRef(b, core.ItemKind, origin)
	for _, i := range b.Items {
		if string(i.ID) == origin.Text && (i.Quantity == nil || i.Equipment != nil || i.Container || i.SplitFrom != "") {
			a.checkAt(coreEquipmentError(string(i.ID), "ammunition origin", "original plain stock with explicit quantity", origin.Text), origin)
		}
	}
}
func (a *analyzer) recoverAmmunition(b core.Blueprint, n battleActionNode) *core.RecoverAmmunition {
	a.checkRef(b, core.CharacterKind, n.Actor)
	v := &core.RecoverAmmunition{Encounter: core.EncounterID(n.ID.Text), Actor: core.CharacterID(n.Actor.Text)}
	origins, ids := map[string]bool{}, map[string]bool{}
	for i, c := range n.Recovery {
		a.ammunitionOrigin(b, c.Origin)
		a.requireEquipmentID(c.Created)
		if i >= 64 || origins[c.Origin.Text] {
			a.checkAt(coreEquipmentError("", "recovery origin", "at most 64 unique origins", c.Origin.Text), c.Origin)
		}
		_, exists := b.KindOf(c.Created.Text)
		if ids[c.Created.Text] || exists {
			a.checkAt(&core.Fault{Code: "E_DUPLICATE", Expected: "fresh unique item ID", Actual: c.Created.Text, Message: "duplicate recovery identity"}, c.Created)
		}
		origins[c.Origin.Text], ids[c.Created.Text] = true, true
		v.Create = append(v.Create, core.AmmunitionCreation{Origin: core.ItemID(c.Origin.Text), Created: core.ItemID(c.Created.Text)})
	}
	return v
}
