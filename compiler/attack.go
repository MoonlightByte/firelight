package compiler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/MoonlightByte/firelight/core"
)

// attacksNode is the world's attack law outside Battle: `attacks { defense
// "<stat>"; health "<resource>"; die <n>; miss <n>; critical <n>; critical
// dice <n>; }`.
type attacksNode struct {
	Start, Defense, Health, Die, Miss, Critical, Multiplier token
}

// attackNode is `attack "<attacker>" profile to_hit <n> damage <c>d<s> |
// flat [<+b>|<-b>] kind "<kind>" against "<target>" [advantage|disadvantage]
// faces <a> [<b>] [damage <n>] [critical damage <m>] [faces "<instance>"
// <f> ...]`.
type attackNode struct {
	Verb, Attacker, ToHit, Count, Sides, Kind, Target, FacesKey token
	Flat, Bonus, Mode, Damage, CriticalDamage                   *token
	Faces                                                       []token
	CheckFaces                                                  []damageFacesNode
}

func (p *parser) attacks(start token) attacksNode {
	n := attacksNode{Start: start}
	p.expect("{")
	p.expect("defense")
	n.Defense = p.str()
	p.expect(";")
	p.expect("health")
	n.Health = p.str()
	p.expect(";")
	p.expect("die")
	n.Die = p.num()
	p.expect(";")
	p.expect("miss")
	n.Miss = p.num()
	p.expect(";")
	p.expect("critical")
	n.Critical = p.num()
	p.expect(";")
	p.expect("critical")
	p.expect("dice")
	n.Multiplier = p.num()
	p.expect(";")
	p.expect("}")
	return n
}

func (p *parser) attack(verb token) attackNode {
	n := attackNode{Verb: verb, Attacker: p.str()}
	p.expect("profile")
	p.expect("to_hit")
	n.ToHit = p.num()
	p.expect("damage")
	if p.isWord("flat") {
		t := p.take()
		n.Flat = &t
	} else {
		n.Count = p.num()
		n.Sides = p.peek()
		if p.err == nil && (n.Sides.Kind != word || !diceSides(n.Sides.Text)) {
			p.err = diag(n.Sides.Pos, "E_PARSE", "dice sides such as d8", n.Sides.Text, "expected dice sides")
		}
		p.take()
	}
	if t := p.peek(); t.Kind == integer && (strings.HasPrefix(t.Text, "+") || strings.HasPrefix(t.Text, "-")) {
		b := p.take()
		n.Bonus = &b
	}
	p.expect("kind")
	n.Kind = p.str()
	p.expect("against")
	n.Target = p.str()
	if p.isWord("advantage") || p.isWord("disadvantage") {
		v := p.take()
		n.Mode = &v
	}
	n.FacesKey = p.expect("faces")
	n.Faces = append(n.Faces, p.num())
	for p.peek().Kind == integer && p.err == nil {
		n.Faces = append(n.Faces, p.num())
	}
	if p.isWord("damage") {
		p.take()
		v := p.num()
		n.Damage = &v
	}
	if p.isWord("critical") {
		p.take()
		p.expect("damage")
		v := p.num()
		n.CriticalDamage = &v
	}
	n.CheckFaces = p.damageFaces()
	return n
}

// diceSides reports whether a word is `d` and decimal digits.
func diceSides(s string) bool {
	if len(s) < 2 || s[0] != 'd' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (a *analyzer) attacks(n attacksNode) *core.AttackLaw {
	return &core.AttackLaw{Defense: core.StatID(n.Defense.Text), Health: core.ResourceID(n.Health.Text), DieSides: a.number(n.Die), MissFace: a.number(n.Miss), CriticalFace: a.number(n.Critical), CriticalMultiplier: a.number(n.Multiplier)}
}

// attacksOperand positions a law fault at its field.
func attacksOperand(f *core.Fault, n worldNode) (token, bool) {
	if n.Attacks == nil || f.Code != "E_ATTACK" {
		return token{}, false
	}
	switch f.Field {
	case "defense":
		return n.Attacks.Defense, true
	case "health":
		return n.Attacks.Health, true
	case "die":
		return n.Attacks.Die, true
	case "miss":
		return n.Attacks.Miss, true
	case "critical":
		return n.Attacks.Critical, true
	case "critical dice":
		return n.Attacks.Multiplier, true
	}
	return n.Attacks.Start, true
}

func (a *analyzer) attack(b core.Blueprint, n attackNode) core.Operation {
	var count, sides int64
	if n.Flat == nil {
		var err error
		count = a.number(n.Count)
		sides, err = strconv.ParseInt(n.Sides.Text[1:], 10, 64)
		if err != nil {
			a.checkAt(&core.Fault{Code: "E_INTEGER", Field: "damage sides", Expected: "signed 64-bit integer", Actual: n.Sides.Text, Message: "integer literal exceeds the numeric domain"}, n.Sides)
		}
	}
	v := core.Attack{Attacker: core.CharacterID(n.Attacker.Text), Target: core.CharacterID(n.Target.Text), Profile: core.AttackProfile{ToHit: a.number(n.ToHit), DamageCount: count, DamageSides: sides, DamageKind: n.Kind.Text}}
	if n.Bonus != nil {
		v.Profile.DamageBonus = a.number(*n.Bonus)
	}
	if n.Mode != nil {
		v.Mode = core.RollMode(n.Mode.Text)
	}
	for _, t := range n.Faces {
		v.Faces = append(v.Faces, a.number(t))
	}
	if n.Damage != nil {
		d := a.number(*n.Damage)
		v.Damage = &d
	}
	if n.CriticalDamage != nil {
		d := a.number(*n.CriticalDamage)
		v.CriticalDamage = &d
	}
	a.checkRef(b, core.CharacterKind, n.Attacker)
	a.checkRef(b, core.CharacterKind, n.Target)
	for _, f := range a.damageFaces(b, n.Target, n.CheckFaces) {
		v.CheckFaces = append(v.CheckFaces, core.AttackCheckFace{Condition: f.Condition, Face: f.Face})
	}
	o := core.Operation{Attack: &v}
	err := b.CheckOperationStatic(o)
	position := n.Verb
	var f *core.Fault
	if errors.As(err, &f) {
		switch {
		case f.Code == "E_STAT" || f.Code == "E_RESOURCE":
			position = n.Target
		case f.Field == "damage dice":
			position = n.Count
		case f.Field == "damage sides":
			position = n.Sides
		case f.Field == "damage kind":
			position = n.Kind
		case f.Field == "mode" && n.Mode != nil:
			position = *n.Mode
		case f.Field == "faces":
			position = n.FacesKey
			for _, t := range n.Faces {
				if strings.HasPrefix(f.Expected, "faces in") && strconv.FormatInt(a.number(t), 10) == f.Actual {
					position = t
					break
				}
			}
		}
	}
	a.checkAt(err, position)
	return o
}

// traitLawOperand positions a character trait in a world without the attack
// law at the character's first trait.
func traitLawOperand(f *core.Fault, n worldNode) (token, bool) {
	if f.Code != "E_ATTACK" || f.Field != "damage trait" || f.Expected != "world attacks law" {
		return token{}, false
	}
	for _, c := range n.Characters {
		if c.ID.Text == f.Subject && len(c.Traits) > 0 {
			return c.Traits[0].Key, true
		}
	}
	return token{}, false
}

// damageTraitNode is `damage trait "<condition type>" resistance |
// vulnerability | immunity "<kind>";` in a world.
type damageTraitNode struct {
	Start, Condition, Treatment, Kind token
}

func (p *parser) damageTrait(start token) damageTraitNode {
	p.expect("trait")
	n := damageTraitNode{Start: start, Condition: p.str(), Treatment: p.choice("resistance", "vulnerability", "immunity"), Kind: p.str()}
	p.expect(";")
	return n
}

var traitTreatments = map[string]string{"resist": "resistance", "vulnerable": "vulnerability", "immune": "immunity"}

// characterTraits lowers a character's own damage traits, each fault at its
// token.
func (a *analyzer) characterTraits(c *core.Character, nodes []traitNode) {
	for _, v := range nodes {
		c.DamageTraits = append(c.DamageTraits, core.DamageTrait{Treatment: traitTreatments[v.Key.Text], Kind: v.Kind.Text})
		probe := core.Character{ID: c.ID, DamageTraits: c.DamageTraits}
		if err := core.CheckDamageTraits(probe); err != nil {
			position := v.Kind
			if f, ok := err.(*core.Fault); ok && strings.HasPrefix(f.Expected, "at most") {
				position = v.Key
			}
			a.checkAt(err, position)
		}
	}
}

// damageTraits lowers the world's policies, each fault at its token.
func (a *analyzer) damageTraits(b *core.Blueprint, n worldNode) {
	for i, v := range n.DamageTraits {
		b.DamageTraits = append(b.DamageTraits, core.DamageTraitPolicy{Condition: core.ConditionTypeID(v.Condition.Text), Treatment: v.Treatment.Text, Kind: v.Kind.Text})
		probe := core.Blueprint{Attacks: b.Attacks, ConditionDefinitions: b.ConditionDefinitions, DamageTraits: b.DamageTraits}
		err := core.CheckDamageTraitPolicies(probe)
		var f *core.Fault
		if !errors.As(err, &f) {
			continue
		}
		position := v.Kind
		switch {
		case f.Expected == "world attacks law" || strings.HasPrefix(f.Expected, "at most"):
			position = n.DamageTraits[i].Start
		case f.Expected == "declared Condition type":
			position = v.Condition
		}
		a.checkAt(err, position)
	}
}
