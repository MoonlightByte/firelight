package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strconv"
	"strings"
	"unicode"
)

type equipmentNode struct {
	Start, Version token
	Slots          []slotNode
	Derived        []derivedNode
	Definitions    []equipmentDefinitionNode
}
type slotNode struct{ ID, Capacity token }
type termNode struct {
	Stat, Offset, Divisor     token
	Ceiling, Requires, Unless *token
	Steps                     []stepNode
	UnlessSlots               []token
}
type stepNode struct{ Start, At, Value token }
type derivedNode struct {
	Stat    token
	Terms   []termNode
	Recipes []alternativeNode
}

// alternativeNode is `recipe "<name>" base <n> requires "<type>"
// {unless slot "<slot>"} { <terms> }` inside a derive.
type alternativeNode struct {
	Start, Name, Base, Requires token
	UnlessSlots                 []token
	Terms                       []termNode
}
type recipeNode struct {
	Base  token
	Terms []termNode
}
type equipmentDefinitionNode struct {
	namedNode
	Description, Default token
	Modes                []equipmentModeNode
}
type equipmentModeNode struct {
	EquipTicks, UnequipTicks *token
	ID                       token
	Slots                    []slotUseNode
	Modifiers                []equipmentModifierNode
	Overrides                []overrideNode
	Attacks                  []equipmentAttackNode
}
type slotUseNode struct{ Slot, Amount token }
type equipmentModifierNode struct {
	ID, Stat, Amount token
	Requires         *token
}
type overrideNode struct {
	Stat   token
	Recipe recipeNode
}
type equipmentAttackNode struct {
	Ranged            *rangedProfileNode
	Thrown            *thrownProfileNode
	Reach, AttackStat *token
	namedNode
	ToHit, Bonus       recipeNode
	Kind, Count, Sides token
	Flat               *token
	Gate               gateNode
	Upgrades           []upgradeNode
}

// gateNode is `[requires "<type>"] {unless slot "<slot>"}`.
type gateNode struct {
	Requires *token
	Unless   []token
}

// upgradeNode is `or dice <c> sides <s> requires "<type>" {unless slot "<slot>"}`.
type upgradeNode struct {
	Or, Count, Sides token
	Gate             gateNode
}

func (p *parser) equipment(start token) equipmentNode {
	n := equipmentNode{Start: start, Version: p.str()}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		switch p.choice("slot", "derive", "definition").Text {
		case "slot":
			id := p.str()
			p.expect("capacity")
			n.Slots = append(n.Slots, slotNode{id, p.num()})
			p.expect(";")
		case "derive":
			p.expect("stat")
			id := p.str()
			p.expect("{")
			d := derivedNode{Stat: id}
			for p.peek().Text != "}" && p.err == nil {
				if p.isWord("recipe") {
					d.Recipes = append(d.Recipes, p.alternative())
				} else {
					d.Terms = append(d.Terms, p.term())
				}
			}
			n.Derived = append(n.Derived, d)
			p.expect("}")
		case "definition":
			n.Definitions = append(n.Definitions, p.equipmentDefinition())
		}
	}
	p.expect("}")
	return n
}
func (p *parser) terms() []termNode {
	var out []termNode
	for p.peek().Text != "}" && p.err == nil {
		out = append(out, p.term())
	}
	return out
}
func (p *parser) alternative() alternativeNode {
	n := alternativeNode{Start: p.take(), Name: p.str()}
	p.expect("base")
	n.Base = p.num()
	p.expect("requires")
	n.Requires = p.str()
	for p.isWord("unless") && p.err == nil {
		p.take()
		p.expect("slot")
		n.UnlessSlots = append(n.UnlessSlots, p.str())
	}
	p.expect("{")
	n.Terms = p.terms()
	p.expect("}")
	return n
}
func (p *parser) term() termNode {
	p.expect("term")
	p.expect("stat")
	n := termNode{Stat: p.str()}
	p.expect("offset")
	n.Offset = p.num()
	p.expect("divide")
	n.Divisor = p.num()
	if p.isWord("ceiling") {
		p.take()
		v := p.num()
		n.Ceiling = &v
	}
	for p.isWord("step") && p.err == nil {
		s := stepNode{Start: p.take(), At: p.num()}
		p.expect("=")
		s.Value = p.num()
		n.Steps = append(n.Steps, s)
	}
	if p.isWord("requires") {
		p.take()
		v := p.str()
		n.Requires = &v
	}
	// One excluding type, then any slot gates: unless slot "<slot>".
	if p.isWord("unless") {
		p.take()
		if p.isWord("slot") {
			p.take()
			n.UnlessSlots = append(n.UnlessSlots, p.str())
		} else {
			v := p.str()
			n.Unless = &v
		}
	}
	for p.isWord("unless") && p.err == nil {
		p.take()
		p.expect("slot")
		n.UnlessSlots = append(n.UnlessSlots, p.str())
	}
	p.expect(";")
	return n
}
func (p *parser) recipe() recipeNode {
	p.expect("{")
	p.expect("constant")
	n := recipeNode{Base: p.num()}
	p.expect(";")
	n.Terms = p.terms()
	p.expect("}")
	return n
}
func (p *parser) equipmentDefinition() equipmentDefinitionNode {
	n := equipmentDefinitionNode{namedNode: p.named()}
	p.expect("{")
	// Required singleton declarations in fixed order keep omissions unambiguous.
	p.expect("description")
	n.Description = p.str()
	p.expect(";")
	p.expect("default")
	n.Default = p.str()
	p.expect(";")
	for p.peek().Text != "}" && p.err == nil {
		p.expect("mode")
		m := equipmentModeNode{ID: p.str()}
		p.expect("{")
		for p.peek().Text != "}" && p.err == nil {
			if p.isWord("equip_ticks") || p.isWord("unequip_ticks") {
				key := p.take()
				v := p.num()
				dest := &m.EquipTicks
				if key.Text == "unequip_ticks" {
					dest = &m.UnequipTicks
				}
				if *dest != nil {
					p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, key.Text, "duplicate mode timing")
				} else {
					*dest = &v
				}
				p.expect(";")
				continue
			}
			switch p.choice("occupy", "modifier", "base", "attack").Text {
			case "occupy":
				id := p.str()
				p.expect("by")
				m.Slots = append(m.Slots, slotUseNode{id, p.num()})
				p.expect(";")
			case "modifier":
				v := equipmentModifierNode{ID: p.str()}
				p.expect("stat")
				v.Stat = p.str()
				p.expect("add")
				v.Amount = p.num()
				if p.isWord("requires") {
					p.take()
					x := p.str()
					v.Requires = &x
				}
				p.expect(";")
				m.Modifiers = append(m.Modifiers, v)
			case "base":
				p.expect("stat")
				id := p.str()
				m.Overrides = append(m.Overrides, overrideNode{id, p.recipe()})
			case "attack":
				m.Attacks = append(m.Attacks, p.equipmentAttack())
			}
		}
		p.expect("}")
		n.Modes = append(n.Modes, m)
	}
	p.expect("}")
	return n
}

// equipmentAttack is `"<id>" named "<name>" { hit <recipe> damage "<kind>"
// dice <c> sides <s> | flat <recipe> [melee reach <n> stat "<s>"; | ranged
// ...] }` after `attack`, in an equipment mode or a Condition type.
func (p *parser) equipmentAttack() equipmentAttackNode {
	a := equipmentAttackNode{namedNode: p.named()}
	a.Gate = p.attackGate(false)
	p.expect("{")
	p.expect("hit")
	a.ToHit = p.recipe()
	p.expect("damage")
	a.Kind = p.str()
	if p.isWord("flat") {
		t := p.take()
		a.Flat = &t
	} else {
		p.expect("dice")
		a.Count = p.num()
		p.expect("sides")
		a.Sides = p.num()
	}
	a.Bonus = p.recipe()
	for p.isWord("or") && p.err == nil {
		u := upgradeNode{Or: p.take()}
		p.expect("dice")
		u.Count = p.num()
		p.expect("sides")
		u.Sides = p.num()
		u.Gate = p.attackGate(true)
		a.Upgrades = append(a.Upgrades, u)
	}
	if p.isWord("melee") {
		p.take()
		p.expect("reach")
		reach := p.num()
		p.expect("stat")
		stat := p.str()
		a.Reach = &reach
		a.AttackStat = &stat
		p.expect(";")
	} else if p.isWord("ranged") {
		a.Ranged = p.rangedProfile()
		a.AttackStat = &a.Ranged.Stat
	} else if p.isWord("thrown") {
		a.Thrown = p.thrownProfile()
		a.AttackStat = &a.Thrown.Stat
	}
	p.expect("}")
	return a
}

// attackGate parses a gate; an upgrade's must start with requires.
func (p *parser) attackGate(required bool) gateNode {
	var g gateNode
	if required || p.isWord("requires") {
		p.expect("requires")
		t := p.str()
		g.Requires = &t
	}
	for p.isWord("unless") && p.err == nil {
		p.take()
		p.expect("slot")
		g.Unless = append(g.Unless, p.str())
	}
	return g
}
func coreEquipmentError(id, field, expected, actual string) error {
	return &core.Fault{Code: "E_EQUIPMENT", Subject: id, Field: field, Expected: expected, Actual: actual, Message: "invalid equipment " + field}
}
func (a *analyzer) requireEquipmentID(t token) {
	bad := strings.TrimSpace(t.Text) == ""
	for _, r := range t.Text {
		bad = bad || unicode.IsControl(r)
	}
	if bad {
		a.checkAt(coreEquipmentError(t.Text, "identity", "valid explicit identity", t.Text), t)
	}
}
func (a *analyzer) equipmentUnique(seen map[string]bool, t token) {
	a.requireEquipmentID(t)
	if seen[t.Text] {
		a.checkAt(&core.Fault{Code: "E_DUPLICATE", Subject: t.Text, Field: "equipment", Expected: "unique local identity", Actual: t.Text, Message: "duplicate equipment declaration"}, t)
	}
	seen[t.Text] = true
}
func (a *analyzer) terms(nodes []termNode) []core.StatTerm {
	var out []core.StatTerm
	for _, n := range nodes {
		a.requireEquipmentID(n.Stat)
		t := core.StatTerm{Stat: core.StatID(n.Stat.Text), Offset: a.number(n.Offset), Divisor: a.number(n.Divisor)}
		if t.Divisor <= 0 {
			a.checkAt(coreEquipmentError(n.Stat.Text, "divisor", "positive integer", n.Divisor.Text), n.Divisor)
		}
		if n.Ceiling != nil {
			v := a.number(*n.Ceiling)
			t.Ceiling = &v
		}
		for i, s := range n.Steps {
			step := core.TermStep{At: a.number(s.At), Value: a.number(s.Value)}
			switch {
			case i == core.MaxTermSteps:
				a.checkAt(coreEquipmentError(n.Stat.Text, "steps", "at most 32", strconv.Itoa(len(n.Steps))), s.Start)
			case i == 0 && n.Ceiling != nil:
				a.checkAt(coreEquipmentError(n.Stat.Text, "steps", "steps or a ceiling, not both", "ceiling"), s.Start)
			case i > 0 && step.At <= t.Steps[i-1].At:
				a.checkAt(coreEquipmentError(n.Stat.Text, "steps", "strictly increasing step inputs", s.At.Text), s.At)
			}
			t.Steps = append(t.Steps, step)
		}
		if n.Requires != nil {
			a.requireEquipmentID(*n.Requires)
			t.Requires = core.ConditionTypeID(n.Requires.Text)
		}
		if n.Unless != nil {
			a.requireEquipmentID(*n.Unless)
			t.Unless = core.ConditionTypeID(n.Unless.Text)
		}
		seen := map[string]bool{}
		for i, s := range n.UnlessSlots {
			a.requireEquipmentID(s)
			if i == core.MaxUnlessSlots || seen[s.Text] {
				a.checkAt(coreEquipmentError(n.Stat.Text, "unless_slots", "at most 8 unique slots", s.Text), s)
			}
			seen[s.Text] = true
			t.UnlessSlots = append(t.UnlessSlots, core.SlotID(s.Text))
		}
		out = append(out, t)
	}
	return out
}
func (a *analyzer) recipe(n recipeNode) core.ScalarRecipe {
	return core.ScalarRecipe{Base: a.number(n.Base), Terms: a.terms(n.Terms)}
}
func (a *analyzer) equipment(n equipmentNode) *core.EquipmentRules {
	out := &core.EquipmentRules{Version: n.Version.Text}
	if n.Version.Text != core.EquipmentVersion {
		a.checkAt(coreEquipmentError("", "version", core.EquipmentVersion, n.Version.Text), n.Version)
	}
	seen := map[string]bool{}
	for _, s := range n.Slots {
		a.equipmentUnique(seen, s.ID)
		capacity := a.number(s.Capacity)
		if capacity <= 0 {
			a.checkAt(coreEquipmentError(s.ID.Text, "capacity", "positive integer", s.Capacity.Text), s.Capacity)
		}
		out.Slots = append(out.Slots, core.SlotDefinition{ID: core.SlotID(s.ID.Text), Capacity: capacity})
	}
	seen = map[string]bool{}
	for _, d := range n.Derived {
		a.equipmentUnique(seen, d.Stat)
		rule := core.DerivedStatRule{Stat: core.StatID(d.Stat.Text), Terms: a.terms(d.Terms)}
		names := map[string]bool{}
		for _, r := range d.Recipes {
			a.equipmentUnique(names, r.Name)
			a.requireEquipmentID(r.Name)
			a.requireEquipmentID(r.Requires)
			v := core.AlternativeRecipe{Name: r.Name.Text, Base: a.number(r.Base), Requires: core.ConditionTypeID(r.Requires.Text), Terms: a.terms(r.Terms)}
			slots := map[string]bool{}
			for i, x := range r.UnlessSlots {
				a.requireEquipmentID(x)
				if i == core.MaxUnlessSlots || slots[x.Text] {
					a.checkAt(coreEquipmentError(r.Name.Text, "unless_slots", "at most 8 unique slots", x.Text), x)
				}
				slots[x.Text] = true
				v.UnlessSlots = append(v.UnlessSlots, core.SlotID(x.Text))
			}
			rule.Recipes = append(rule.Recipes, v)
		}
		out.Derived = append(out.Derived, rule)
	}
	seen = map[string]bool{}
	for _, d := range n.Definitions {
		a.equipmentUnique(seen, d.ID)
		a.requireEquipmentID(d.Default)
		v := core.EquipmentDefinition{ID: core.EquipmentDefinitionID(d.ID.Text), Name: d.Name.Text, Description: d.Description.Text, DefaultMode: core.EquipmentModeID(d.Default.Text)}
		modes := map[string]bool{}
		for _, m := range d.Modes {
			a.equipmentUnique(modes, m.ID)
			mode := core.EquipmentMode{ID: core.EquipmentModeID(m.ID.Text)}
			if m.EquipTicks != nil {
				mode.EquipTicks = a.number(*m.EquipTicks)
				a.checkModeTicks(*m.EquipTicks, mode.EquipTicks)
			}
			if m.UnequipTicks != nil {
				mode.UnequipTicks = a.number(*m.UnequipTicks)
				a.checkModeTicks(*m.UnequipTicks, mode.UnequipTicks)
			}
			uses := map[string]bool{}
			for _, s := range m.Slots {
				a.equipmentUnique(uses, s.Slot)
				amount := a.number(s.Amount)
				if amount <= 0 {
					a.checkAt(coreEquipmentError(s.Slot.Text, "occupancy", "positive use", s.Amount.Text), s.Amount)
				}
				mode.Slots = append(mode.Slots, core.SlotUse{Slot: core.SlotID(s.Slot.Text), Amount: amount})
			}
			mods := map[string]bool{}
			for _, x := range m.Modifiers {
				a.equipmentUnique(mods, x.ID)
				a.requireEquipmentID(x.Stat)
				v := core.EquipmentModifier{ID: x.ID.Text, Stat: core.StatID(x.Stat.Text), Amount: a.number(x.Amount)}
				if x.Requires != nil {
					a.requireEquipmentID(*x.Requires)
					v.Requires = core.ConditionTypeID(x.Requires.Text)
				}
				mode.Modifiers = append(mode.Modifiers, v)
			}
			overrides := map[string]bool{}
			for _, x := range m.Overrides {
				a.equipmentUnique(overrides, x.Stat)
				mode.Overrides = append(mode.Overrides, core.StatBaseOverride{Stat: core.StatID(x.Stat.Text), Recipe: a.recipe(x.Recipe)})
			}
			attacks := map[string]bool{}
			for _, x := range m.Attacks {
				mode.Attacks = append(mode.Attacks, a.equipmentAttack(attacks, x))
			}
			v.Modes = append(v.Modes, mode)
		}
		if !modes[d.Default.Text] {
			a.checkAt(coreEquipmentError(d.ID.Text, "default_mode", "declared mode", d.Default.Text), d.Default)
		}
		out.Definitions = append(out.Definitions, v)
	}
	return out
}

// equipmentAttack lowers one attack of an equipment mode or a Condition type,
// unique among its owner's attacks in seen.
func (a *analyzer) equipmentAttack(seen map[string]bool, x equipmentAttackNode) core.EquipmentAttack {
	a.equipmentUnique(seen, x.ID)
	a.requireEquipmentID(x.Kind)
	var count, sides int64
	if x.Flat == nil {
		count, sides = a.number(x.Count), a.number(x.Sides)
		if count < 1 || count > 100 {
			a.checkAt(coreEquipmentError(x.ID.Text, "dice", "1..100", x.Count.Text), x.Count)
		}
		if sides < 2 || sides > 1000 {
			a.checkAt(coreEquipmentError(x.ID.Text, "sides", "2..1000", x.Sides.Text), x.Sides)
		}
	}
	var melee *core.MeleeProfile
	var ranged *core.RangedProfile
	var thrown *core.ThrownProfile
	if x.Ranged != nil {
		ranged = a.rangedProfile(x.Ranged)
	}
	if x.Thrown != nil {
		thrown = a.thrownProfile(x.Thrown)
	}
	if x.Reach != nil {
		a.requireEquipmentID(*x.AttackStat)
		melee = &core.MeleeProfile{Reach: a.battleNumber(*x.Reach, 1, 1000000), Stat: core.StatID(x.AttackStat.Text)}
	}
	out := core.EquipmentAttack{Ranged: ranged, Melee: melee, Thrown: thrown, ID: x.ID.Text, Name: x.Name.Text, ToHit: a.recipe(x.ToHit), Damage: core.DamageGrant{Count: count, Sides: sides, Kind: x.Kind.Text, Bonus: a.recipe(x.Bonus)}, AttackGate: a.attackGate(x.ID.Text, x.Gate)}
	for i, u := range x.Upgrades {
		if i == core.MaxDamageUpgrades {
			a.checkAt(coreEquipmentError(x.ID.Text, "upgrades", "at most 8", u.Or.Text), u.Or)
		}
		v := core.DamageUpgrade{Count: a.number(u.Count), Sides: a.number(u.Sides), AttackGate: a.attackGate(x.ID.Text, u.Gate)}
		if v.Count < 1 || v.Count > 100 {
			a.checkAt(coreEquipmentError(x.ID.Text, "upgrade", "1..100", u.Count.Text), u.Count)
		}
		if v.Sides < 2 || v.Sides > 1000 {
			a.checkAt(coreEquipmentError(x.ID.Text, "upgrade", "2..1000", u.Sides.Text), u.Sides)
		}
		out.Damage.Upgrades = append(out.Damage.Upgrades, v)
	}
	return out
}

// attackGate lowers a gate. Its type and slots are resolved in equipmentRefs.
func (a *analyzer) attackGate(id string, g gateNode) core.AttackGate {
	var out core.AttackGate
	if g.Requires != nil {
		a.requireEquipmentID(*g.Requires)
		out.Requires = core.ConditionTypeID(g.Requires.Text)
	}
	seen := map[string]bool{}
	for i, t := range g.Unless {
		a.requireEquipmentID(t)
		if i == core.MaxUnlessSlots || seen[t.Text] {
			a.checkAt(coreEquipmentError(id, "unless_slots", "at most 8 unique slots", t.Text), t)
		}
		seen[t.Text] = true
		out.UnlessSlots = append(out.UnlessSlots, core.SlotID(t.Text))
	}
	return out
}

// Resolve immutable uses in their AST namespace before whole-world validation.
func (a *analyzer) equipmentRefs(b core.Blueprint, n worldNode) {
	if n.Equipment == nil {
		return
	}
	if b.Clock == nil {
		for _, d := range n.Equipment.Definitions {
			for _, m := range d.Modes {
				for _, t := range []*token{m.EquipTicks, m.UnequipTicks} {
					if t != nil && a.number(*t) != 0 {
						a.checkAt(coreEquipmentError(d.ID.Text, "time", "Clock present", t.Text), *t)
					}
				}
			}
		}
	}
	rules := n.Equipment
	slots, derived, types := map[string]bool{}, map[string]bool{}, map[string]bool{}
	definitions := map[string]map[string]bool{}
	for _, s := range rules.Slots {
		slots[s.ID.Text] = true
	}
	for _, d := range rules.Derived {
		derived[d.Stat.Text] = true
	}
	for _, d := range b.ConditionDefinitions {
		types[string(d.ID)] = true
	}
	checkType := func(t *token) {
		if t != nil && !types[t.Text] {
			a.checkAt(coreEquipmentError(t.Text, "requires", "declared Condition type", t.Text), *t)
		}
	}
	checkTerms := func(ts []termNode) {
		for _, t := range ts {
			if derived[t.Stat.Text] {
				a.checkAt(coreEquipmentError(t.Stat.Text, "input", "non-derived stat", t.Stat.Text), t.Stat)
			}
			checkType(t.Requires)
			if t.Unless != nil && (!types[t.Unless.Text] || t.Requires != nil && t.Requires.Text == t.Unless.Text) {
				a.checkAt(coreEquipmentError(t.Unless.Text, "unless", "declared Condition type other than requires", t.Unless.Text), *t.Unless)
			}
			for _, s := range t.UnlessSlots {
				if !slots[s.Text] {
					a.checkAt(coreEquipmentError(s.Text, "unless_slots", "declared slot", s.Text), s)
				}
			}
		}
	}
	checkGate := func(g gateNode) {
		checkType(g.Requires)
		for _, t := range g.Unless {
			if !slots[t.Text] {
				a.checkAt(coreEquipmentError(t.Text, "unless_slots", "declared slot", t.Text), t)
			}
		}
	}
	checkAttack := func(v equipmentAttackNode) {
		if v.Ranged != nil {
			a.rangedAmmunition(b, v.Ranged)
		}
		checkGate(v.Gate)
		for _, u := range v.Upgrades {
			checkGate(u.Gate)
		}
		checkTerms(v.ToHit.Terms)
		checkTerms(v.Bonus.Terms)
	}
	for _, d := range rules.Derived {
		checkTerms(d.Terms)
		for _, r := range d.Recipes {
			checkType(&r.Requires)
			for _, x := range r.UnlessSlots {
				if !slots[x.Text] {
					a.checkAt(coreEquipmentError(x.Text, "unless_slots", "declared slot", x.Text), x)
				}
			}
			checkTerms(r.Terms)
		}
	}
	for _, d := range rules.Definitions {
		definitions[d.ID.Text] = map[string]bool{}
		for _, m := range d.Modes {
			definitions[d.ID.Text][m.ID.Text] = true
			for _, s := range m.Slots {
				if !slots[s.Slot.Text] {
					a.checkAt(coreEquipmentError(d.ID.Text, "occupancy", "declared slot", s.Slot.Text), s.Slot)
				}
			}
			for _, v := range m.Modifiers {
				checkType(v.Requires)
			}
			for _, v := range m.Overrides {
				if !derived[v.Stat.Text] {
					a.checkAt(coreEquipmentError(d.ID.Text, "override", "declared derived stat", v.Stat.Text), v.Stat)
				}
				checkTerms(v.Recipe.Terms)
			}
			for _, v := range m.Attacks {
				checkAttack(v)
			}
		}
	}
	for _, d := range n.ConditionDefinitions {
		for _, v := range d.Attacks {
			checkAttack(v)
		}
	}
	for _, i := range n.Items {
		if i.Wearer != nil && i.Definition != nil {
			for _, d := range rules.Definitions {
				if d.ID.Text != i.Definition.Text {
					continue
				}
				mode := d.Default.Text
				if i.Mode != nil {
					mode = i.Mode.Text
				}
				for _, m := range d.Modes {
					if m.ID.Text != mode {
						continue
					}
					for _, attack := range m.Attacks {
						if attack.AttackStat != nil {
							if err := b.CheckRef(core.Ref{Kind: core.CharacterKind, ID: i.Wearer.Text}); err != nil {
								a.checkAt(err, *i.Wearer)
								continue
							}
							a.checkAt(b.CheckStat(core.CharacterID(i.Wearer.Text), core.StatID(attack.AttackStat.Text)), *attack.AttackStat)
						}
					}
				}
			}
		}
		if i.Definition != nil {
			a.requireEquipmentID(*i.Definition)
			modes, ok := definitions[i.Definition.Text]
			if !ok {
				a.checkAt(coreEquipmentError(i.ID.Text, "definition", "declared equipment definition", i.Definition.Text), *i.Definition)
			}
			if i.Mode != nil {
				a.requireEquipmentID(*i.Mode)
				if !modes[i.Mode.Text] {
					a.checkAt(coreEquipmentError(i.ID.Text, "mode", "declared mode", i.Mode.Text), *i.Mode)
				}
			}
		}
	}
}

// conditionAttackOperand positions a whole-world equipment fault about a
// Condition type's attacks. The compiler reports an attack's own faults at its
// tokens; the absent rules are at the first attack, the ninth attack at
// itself, and the rest at the type.
func conditionAttackOperand(f *core.Fault, n worldNode) (token, bool) {
	for _, d := range n.ConditionDefinitions {
		if len(d.Attacks) == 0 {
			continue
		}
		if f.Subject == d.ID.Text {
			switch {
			case f.Field == "attacks" && f.Expected == "Equipment rules":
				return d.Attacks[0].ID, true
			case f.Field == "attacks" && len(d.Attacks) > core.MaxConditionAttacks:
				return d.Attacks[core.MaxConditionAttacks].ID, true
			}
			return d.ID, true
		}
	}
	return token{}, false
}
func equipmentWorldOperand(f *core.Fault, n worldNode) (token, bool) {
	if f.Code == "E_EQUIPMENT" {
		for _, i := range n.Items {
			if i.ID.Text == f.Subject && i.Definition != nil {
				if f.Field == "mode" && i.Mode != nil {
					return *i.Mode, true
				}
				return *i.Definition, true
			}
		}
		if t, ok := conditionAttackOperand(f, n); ok {
			return t, true
		}
		if n.Equipment == nil {
			return token{}, false
		}
		for _, d := range n.Equipment.Definitions {
			if d.ID.Text == f.Subject {
				return d.ID, true
			}
		}
		for _, d := range n.Equipment.Derived {
			if d.Stat.Text == f.Subject {
				return d.Stat, true
			}
			for _, r := range d.Recipes {
				if r.Name.Text == f.Subject {
					return r.Name, true
				}
			}
		}
		return n.Equipment.Start, true
	}
	if n.Equipment == nil {
		return token{}, false
	}
	if f.Code == "E_STAT" {
		declared := map[string]bool{}
		for _, c := range n.Characters {
			if c.ID.Text == f.Subject {
				for _, stat := range c.Stats {
					declared[stat.ID.Text] = true
				}
			}
		}
		for _, d := range n.Equipment.Derived {
			if !declared[d.Stat.Text] {
				continue
			}
			for _, term := range d.Terms {
				if term.Stat.Text == f.Field {
					return term.Stat, true
				}
			}
			for _, r := range d.Recipes {
				for _, term := range r.Terms {
					if term.Stat.Text == f.Field {
						return term.Stat, true
					}
				}
			}
		}
		for _, i := range n.Items {
			if i.Wearer == nil || i.Wearer.Text != f.Subject || i.Definition == nil || i.Mode == nil {
				continue
			}
			for _, d := range n.Equipment.Definitions {
				if d.ID.Text != i.Definition.Text {
					continue
				}
				for _, m := range d.Modes {
					if m.ID.Text != i.Mode.Text {
						continue
					}
					for _, v := range m.Modifiers {
						if v.Stat.Text == f.Field {
							return v.Stat, true
						}
					}
					for _, v := range m.Overrides {
						if v.Stat.Text == f.Field {
							return v.Stat, true
						}
						for _, x := range v.Recipe.Terms {
							if x.Stat.Text == f.Field {
								return x.Stat, true
							}
						}
					}
					for _, v := range m.Attacks {
						for _, ts := range [][]termNode{v.ToHit.Terms, v.Bonus.Terms} {
							for _, x := range ts {
								if x.Stat.Text == f.Field {
									return x.Stat, true
								}
							}
						}
					}
				}
			}
		}
	}
	return token{}, false
}
