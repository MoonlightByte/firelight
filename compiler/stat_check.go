package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"strings"
)

type diceNode struct{ Start, SeedA, SeedB token }

// statCheckNode is `check "<character>" stat "<stat>" die <sides> [against
// <dc>] [advantage|disadvantage] [faces <face>...] [added "<type>"
// <face>...]...`.
type statCheckNode struct {
	Character, Stat, Sides token
	DC, Mode, FacesKey     *token
	Faces                  []token
	Added                  []addedFacesNode
}

// addedFacesNode is `added "<condition type>" <face>...`: the faces of one
// added die.
type addedFacesNode struct {
	Key, Type token
	Faces     []token
}

func (p *parser) addedFaces() []addedFacesNode {
	var out []addedFacesNode
	for p.isWord("added") && p.err == nil {
		n := addedFacesNode{Key: p.take(), Type: p.str()}
		n.Faces = append(n.Faces, p.num())
		for p.peek().Kind == integer && p.err == nil {
			n.Faces = append(n.Faces, p.num())
		}
		out = append(out, n)
	}
	return out
}

func (a *analyzer) addedFaces(nodes []addedFacesNode) []core.AddedFaces {
	var out []core.AddedFaces
	for _, n := range nodes {
		g := core.AddedFaces{Type: core.ConditionTypeID(n.Type.Text)}
		for _, t := range n.Faces {
			g.Faces = append(g.Faces, a.number(t))
		}
		out = append(out, g)
	}
	return out
}

// addedFacesAt places an added-faces fault at the type or face it names, or
// at the first clause.
func addedFacesAt(nodes []addedFacesNode, f *core.Fault) (token, bool) {
	for _, n := range nodes {
		if n.Type.Text == f.Actual {
			return n.Type, true
		}
		for _, t := range n.Faces {
			if strings.HasPrefix(f.Expected, "faces in") && t.Text == f.Actual {
				return t, true
			}
		}
	}
	if len(nodes) > 0 {
		return nodes[0].Key, true
	}
	return token{}, false
}

func (p *parser) dice(start token) diceNode {
	n := diceNode{Start: start}
	p.expect("seed")
	n.SeedA = p.num()
	n.SeedB = p.num()
	p.expect(";")
	return n
}
func (a *analyzer) dice(n diceNode) *core.Dice {
	return &core.Dice{Seed: [2]uint64{a.unsignedLiteral(n.SeedA, false), a.unsignedLiteral(n.SeedB, false)}}
}

func (p *parser) statCheck() statCheckNode {
	n := statCheckNode{Character: p.str()}
	p.expect("stat")
	n.Stat = p.str()
	p.expect("die")
	n.Sides = p.num()
	if p.isWord("against") {
		p.take()
		v := p.num()
		n.DC = &v
	}
	if p.isWord("advantage") || p.isWord("disadvantage") {
		v := p.take()
		n.Mode = &v
	}
	if p.isWord("faces") {
		key := p.take()
		n.FacesKey = &key
		n.Faces = append(n.Faces, p.num())
		for p.peek().Kind == integer && p.err == nil {
			n.Faces = append(n.Faces, p.num())
		}
	}
	n.Added = p.addedFaces()
	p.expect(";")
	return n
}
func (a *analyzer) statCheck(b core.Blueprint, n statCheckNode) core.Operation {
	v := core.StatCheck{Character: core.CharacterID(n.Character.Text), Stat: core.StatID(n.Stat.Text), Sides: a.unsignedLiteral(n.Sides, true)}
	if n.DC != nil {
		dc := a.number(*n.DC)
		v.DC = &dc
	}
	if n.Mode != nil {
		v.Mode = core.RollMode(n.Mode.Text)
	}
	for _, t := range n.Faces {
		v.Faces = append(v.Faces, a.number(t))
	}
	v.Added = a.addedFaces(n.Added)
	a.checkRef(b, core.CharacterKind, n.Character)
	o := core.Operation{StatCheck: &v}
	err := b.CheckOperationStatic(o)
	position := n.Stat
	var f *core.Fault
	if errors.As(err, &f) {
		switch f.Field {
		case "sides":
			position = n.Sides
		case "faces":
			position = *n.FacesKey
		case "added":
			if t, ok := addedFacesAt(n.Added, f); ok {
				position = t
			}
		}
	}
	a.checkAt(err, position)
	return o
}
