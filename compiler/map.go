package compiler

import (
	"strconv"

	"github.com/MoonlightByte/firelight/core"
)

type mapNode struct {
	Start                   token
	Routes                  []mapRouteNode
	Party, Visited, Hazards []token
	Closed                  []mapLinkNode
}
type mapRouteNode struct {
	From, To, Ticks token
	Both            bool
}
type mapLinkNode struct{ From, To token }

func coreMapError(id, field, expected, actual string) error {
	return &core.Fault{Code: "E_MAP", Subject: id, Field: field, Expected: expected, Actual: actual, Message: "invalid map " + field}
}

// worldMap parses the body of a world's `map` block.
func (p *parser) worldMap(start token) mapNode {
	n := mapNode{Start: start}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		switch p.choice("route", "party", "visited", "hazard", "closed").Text {
		case "route":
			v := mapRouteNode{From: p.str()}
			p.expect("to")
			v.To = p.str()
			p.expect("ticks")
			v.Ticks = p.num()
			if p.isWord("both") {
				p.take()
				v.Both = true
			}
			n.Routes = append(n.Routes, v)
		case "party":
			n.Party = append(n.Party, p.str())
		case "visited":
			n.Visited = append(n.Visited, p.str())
		case "hazard":
			n.Hazards = append(n.Hazards, p.str())
		case "closed":
			v := mapLinkNode{From: p.str()}
			p.expect("to")
			v.To = p.str()
			n.Closed = append(n.Closed, v)
		}
		p.expect(";")
	}
	p.expect("}")
	return n
}

// worldMap lowers a map block, refusing each bad entry at its position.
// `both` declares the route in each direction with the same ticks.
func (a *analyzer) worldMap(b core.Blueprint, n mapNode) *core.WorldMap {
	m := &core.WorldMap{Routes: []core.MapRoute{}}
	if b.Clock == nil {
		a.checkAt(coreMapError("", "clock", "declared clock", "absent"), n.Start)
	}
	if b.Battle != nil || b.Adventure != nil {
		a.checkAt(coreMapError("", "travel model", "map without battle or adventure", "both"), n.Start)
	}
	refs := core.NewRefs(&b)
	routes := map[core.MapLink]bool{}
	add := func(from, to token, ticks int64) {
		link := core.MapLink{From: core.LocationID(from.Text), To: core.LocationID(to.Text)}
		if from.Text == to.Text {
			a.checkAt(coreMapError(from.Text, "route", "different endpoints", to.Text), to)
		}
		if routes[link] {
			a.checkAt(coreMapError(from.Text, "route", "unique direction", "duplicate"), from)
		}
		if len(m.Routes) == core.MaxMapRoutes {
			a.checkAt(coreMapError("", "routes", "at most 32768 routes", "exceeded"), from)
		}
		routes[link] = true
		m.Routes = append(m.Routes, core.MapRoute{From: link.From, To: link.To, Ticks: ticks})
	}
	for _, v := range n.Routes {
		a.checkAt(refs.CheckRef(core.Ref{Kind: core.LocationKind, ID: v.From.Text}), v.From)
		a.checkAt(refs.CheckRef(core.Ref{Kind: core.LocationKind, ID: v.To.Text}), v.To)
		ticks := a.number(v.Ticks)
		if ticks < 1 || ticks > core.MaxRouteTicks {
			a.checkAt(coreMapError(v.From.Text, "route ticks", "1..1000000000", strconv.FormatInt(ticks, 10)), v.Ticks)
		}
		add(v.From, v.To, ticks)
		if v.Both {
			add(v.To, v.From, ticks)
		}
	}
	party := map[string]bool{}
	for _, t := range n.Party {
		a.checkAt(refs.CheckRef(core.Ref{Kind: core.CharacterKind, ID: t.Text}), t)
		if party[t.Text] {
			a.checkAt(coreMapError(t.Text, "party", "unique member", "duplicate"), t)
		}
		party[t.Text] = true
		m.Party = append(m.Party, core.CharacterID(t.Text))
	}
	for _, list := range []struct {
		field string
		ids   []token
		out   *[]core.LocationID
	}{{"visited", n.Visited, &m.Visited}, {"hazard", n.Hazards, &m.Hazards}} {
		seen := map[string]bool{}
		for _, t := range list.ids {
			a.checkAt(refs.CheckRef(core.Ref{Kind: core.LocationKind, ID: t.Text}), t)
			if seen[t.Text] {
				a.checkAt(coreMapError(t.Text, list.field, "unique location", "duplicate"), t)
			}
			seen[t.Text] = true
			*list.out = append(*list.out, core.LocationID(t.Text))
		}
	}
	closed := map[core.MapLink]bool{}
	for _, v := range n.Closed {
		link := core.MapLink{From: core.LocationID(v.From.Text), To: core.LocationID(v.To.Text)}
		if !routes[link] {
			a.checkAt(coreMapError(v.From.Text, "closed", "declared route", v.From.Text+" -> "+v.To.Text), v.From)
		}
		if closed[link] {
			a.checkAt(coreMapError(v.From.Text, "closed", "unique route", "duplicate"), v.From)
		}
		closed[link] = true
		m.Closed = append(m.Closed, link)
	}
	return m
}
