package compiler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/MoonlightByte/firelight/core"
)

// saveNode is `save "<source>" profile stat "<stat>" dc <n> [damage
// <c>d<s>[<+b>|<-b>] kind "<kind>" success half|none] against "<target>"
// [advantage|disadvantage] [faces <a> [<b>]] [added "<type>" <f>...]...
// [damage <n>] [faces "<instance>" <f> ...]`.
type saveNode struct {
	Verb, Source, Stat, DC, Target          token
	Count, Sides, Kind, Success             *token
	Bonus, Mode, FacesKey, Damage, DamageAt *token
	Faces                                   []token
	Added                                   []addedFacesNode
	CheckFaces                              []damageFacesNode
}

func (p *parser) save(verb token) saveNode {
	n := saveNode{Verb: verb, Source: p.str()}
	p.expect("profile")
	p.expect("stat")
	n.Stat = p.str()
	p.expect("dc")
	n.DC = p.num()
	if p.isWord("damage") {
		p.take()
		count := p.num()
		n.Count = &count
		sides := p.peek()
		if p.err == nil && (sides.Kind != word || !diceSides(sides.Text)) {
			p.err = diag(sides.Pos, "E_PARSE", "dice sides such as d8", sides.Text, "expected dice sides")
		}
		p.take()
		n.Sides = &sides
		if t := p.peek(); t.Kind == integer && (strings.HasPrefix(t.Text, "+") || strings.HasPrefix(t.Text, "-")) {
			b := p.take()
			n.Bonus = &b
		}
		p.expect("kind")
		kind := p.str()
		n.Kind = &kind
		p.expect("success")
		success := p.choice("half", "none")
		n.Success = &success
	}
	p.expect("against")
	n.Target = p.str()
	if p.isWord("advantage") || p.isWord("disadvantage") {
		v := p.take()
		n.Mode = &v
	}
	// `faces <a>` are the save's; `faces "<instance>"` are check faces.
	if p.isWord("faces") && p.index+1 < len(p.tokens) && p.tokens[p.index+1].Kind == integer {
		key := p.take()
		n.FacesKey = &key
		for p.peek().Kind == integer && p.err == nil {
			n.Faces = append(n.Faces, p.num())
		}
	}
	n.Added = p.addedFaces()
	if p.isWord("damage") {
		at := p.take()
		n.DamageAt = &at
		v := p.num()
		n.Damage = &v
	}
	n.CheckFaces = p.damageFaces()
	return n
}

func (a *analyzer) save(b core.Blueprint, n saveNode) core.Operation {
	v := core.Save{Source: core.CharacterID(n.Source.Text), Target: core.CharacterID(n.Target.Text), Profile: core.SaveProfile{Stat: core.StatID(n.Stat.Text), DC: a.number(n.DC)}}
	if n.Count != nil {
		sides, err := strconv.ParseInt(n.Sides.Text[1:], 10, 64)
		if err != nil {
			a.checkAt(&core.Fault{Code: "E_INTEGER", Field: "damage sides", Expected: "signed 64-bit integer", Actual: n.Sides.Text, Message: "integer literal exceeds the numeric domain"}, *n.Sides)
		}
		d := core.SaveDamage{Count: a.number(*n.Count), Sides: sides, Kind: n.Kind.Text, Success: n.Success.Text}
		if n.Bonus != nil {
			d.Bonus = a.number(*n.Bonus)
		}
		v.Profile.Damage = &d
	}
	if n.Mode != nil {
		v.Mode = core.RollMode(n.Mode.Text)
	}
	for _, t := range n.Faces {
		v.Faces = append(v.Faces, a.number(t))
	}
	v.AddedFaces = a.addedFaces(n.Added)
	if n.Damage != nil {
		d := a.number(*n.Damage)
		v.Damage = &d
	}
	a.checkRef(b, core.CharacterKind, n.Source)
	a.checkRef(b, core.CharacterKind, n.Target)
	for _, f := range a.damageFaces(b, n.Target, n.CheckFaces) {
		v.CheckFaces = append(v.CheckFaces, core.AttackCheckFace{Condition: f.Condition, Face: f.Face})
	}
	o := core.Operation{Save: &v}
	err := b.CheckOperationStatic(o)
	position := n.Verb
	var f *core.Fault
	if errors.As(err, &f) {
		switch {
		case f.Code == "E_STAT":
			position = n.Stat
		case f.Code == "E_RESOURCE":
			position = n.Target
		case f.Field == "damage dice":
			position = *n.Count
		case f.Field == "damage sides":
			position = *n.Sides
		case f.Field == "damage kind":
			position = *n.Kind
		case f.Field == "added faces":
			if t, ok := addedFacesAt(n.Added, f); ok {
				position = t
			}
		case f.Field == "damage" && n.DamageAt != nil:
			position = *n.DamageAt
		case f.Field == "faces" && n.FacesKey != nil:
			position = *n.FacesKey
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
