package compiler

import (
	"strings"
	"unicode"

	"github.com/MoonlightByte/firelight/core"
)

// occupantNode is an occupant declaration, in a world or a create action.
type occupantNode struct {
	namedNode
	Start, Location token
	Kind            token
	Count, Min, Max *token
	Attitude, Type  *token
	Aliases         []token
	Tally           []tallyNode
}
type tallyNode struct {
	Outcome, Count token
	Reason         *token
}

// occupantActionNode changes one occupant: move, count, resolve, set (an
// attitude), rename or return.
type occupantActionNode struct {
	Verb, Occupant token
	To             token // move's location, rename's name
	Count          *token
	Outcome        token
	Attitude       token
	Reason         *token // resolve's because, return's entry
}

// mapEditNode changes the map in play: join or leave the party, close or
// open a route, mark or clear a hazard.
type mapEditNode struct {
	Verb, Noun, Subject, To token
}

var outcomeWords = []string{"defeated", "fled", "surrendered", "departed", "joined", "died"}
var attitudeWords = []string{"friendly", "indifferent", "hostile"}

// occupant parses `"<id>" named "<name>" at "<location>" { ... }` after the
// keyword start.
func (p *parser) occupant(start token) occupantNode {
	n := occupantNode{Start: start, namedNode: p.named()}
	p.expect("at")
	n.Location = p.str()
	p.expect("{")
	once := func(key token, seen bool) bool {
		if seen {
			p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, key.Text, "duplicate occupant field")
		}
		return !seen
	}
	for p.peek().Text != "}" && p.err == nil {
		key := p.choice("person", "creatures", "attitude", "type", "alias", "tally")
		switch key.Text {
		case "person", "creatures":
			if n.Kind.Text != "" {
				p.err = diag(key.Pos, "E_DUPLICATE", "one person or creatures", key.Text, "duplicate occupant field")
				break
			}
			n.Kind = key
			if key.Text == "creatures" && p.peek().Kind == integer {
				c := p.num()
				n.Count = &c
				if p.isWord("to") {
					p.take()
					m := p.num()
					n.Min, n.Max, n.Count = n.Count, &m, nil
				}
			}
		case "attitude":
			if once(key, n.Attitude != nil) {
				v := p.choice(attitudeWords...)
				n.Attitude = &v
			}
		case "type":
			if once(key, n.Type != nil) {
				v := p.str()
				n.Type = &v
			}
		case "alias":
			n.Aliases = append(n.Aliases, p.str())
		case "tally":
			v := tallyNode{Outcome: p.choice(outcomeWords...)}
			v.Count = p.num()
			v.Reason = p.because()
			n.Tally = append(n.Tally, v)
		}
		p.expect(";")
	}
	p.expect("}")
	return n
}

// because parses an optional `because "<reason>"`.
func (p *parser) because() *token {
	if !p.isWord("because") {
		return nil
	}
	p.take()
	r := p.str()
	return &r
}

// by parses an optional `by <n>`.
func (p *parser) by() *token {
	if !p.isWord("by") {
		return nil
	}
	p.take()
	c := p.num()
	return &c
}

// occupantAction parses the rest of count, resolve, set, rename and return.
func (p *parser) occupantAction(verb token) occupantActionNode {
	p.expect("occupant")
	n := occupantActionNode{Verb: verb, Occupant: p.str()}
	switch verb.Text {
	case "count":
		p.expect("as")
		c := p.num()
		n.Count = &c
	case "resolve":
		p.expect("as")
		n.Outcome = p.choice(outcomeWords...)
		n.Count = p.by()
		n.Reason = p.because()
	case "return":
		p.expect("from")
		n.Outcome = p.choice(outcomeWords...)
		if p.peek().Kind == quoted {
			r := p.str()
			n.Reason = &r
		}
		n.Count = p.by()
	case "set":
		p.expect("attitude")
		n.Attitude = p.choice(attitudeWords...)
	case "rename":
		p.expect("to")
		n.To = p.str()
	}
	return n
}

// mapEdit parses the rest of join, leave, close, open, mark and clear.
// mark takes hazard or visited.
func (p *parser) mapEdit(verb token) mapEditNode {
	n := mapEditNode{Verb: verb}
	switch verb.Text {
	case "join", "leave":
		p.expect("party")
		n.Subject = p.str()
	case "close", "open":
		p.expect("route")
		n.Subject = p.str()
		p.expect("to")
		n.To = p.str()
	case "mark":
		n.Noun = p.choice("hazard", "visited")
		n.Subject = p.str()
	default:
		p.expect("hazard")
		n.Subject = p.str()
	}
	return n
}

// occupant lowers a declaration. The attitude defaults to indifferent; a
// person is present until a tally says how it left.
func (a *analyzer) occupant(n occupantNode) core.Occupant {
	o := core.Occupant{ID: core.OccupantID(n.ID.Text), Name: n.Name.Text, Location: core.LocationID(n.Location.Text), Kind: core.OccupantKind(n.Kind.Text), Attitude: core.AttitudeIndifferent}
	if n.Attitude != nil {
		o.Attitude = core.Attitude(n.Attitude.Text)
	}
	if n.Type != nil {
		o.Type = n.Type.Text
	}
	for _, t := range n.Aliases {
		o.Aliases = append(o.Aliases, t.Text)
	}
	for _, t := range n.Tally {
		o.Tally = append(o.Tally, core.OccupantTally{Outcome: core.Outcome(t.Outcome.Text), Count: a.number(t.Count), Reason: reasonText(t.Reason)})
	}
	switch {
	case n.Kind.Text == "person":
		count := int64(1)
		if len(n.Tally) > 0 {
			count = 0
		}
		o.Count = &count
	case n.Count != nil:
		count := a.number(*n.Count)
		o.Count = &count
	case n.Min != nil:
		o.Range = &core.CountRange{Min: a.number(*n.Min), Max: a.number(*n.Max)}
	}
	return o
}

func reasonText(t *token) string {
	if t == nil {
		return ""
	}
	return t.Text
}

// badReason reports a reason the engine refuses: absent for a departure,
// blank, or too long.
func badReason(outcome token, reason *token) bool {
	if reason == nil {
		return outcome.Text == "departed"
	}
	return invalidText(reason.Text) || len(reason.Text) > core.MaxReasonBytes
}

func invalidText(s string) bool {
	bad := strings.TrimSpace(s) == ""
	for _, r := range s {
		bad = bad || unicode.IsControl(r)
	}
	return bad
}

// occupantOperand is the token an occupant fault is about.
func occupantOperand(f *core.Fault, n occupantNode) token {
	switch f.Field {
	case "name":
		return n.Name
	case "attitude":
		if n.Attitude != nil {
			return *n.Attitude
		}
	case "type":
		if n.Type != nil {
			return *n.Type
		}
	case "aliases":
		if len(n.Aliases) > core.MaxOccupantAliases {
			return n.Aliases[core.MaxOccupantAliases]
		}
	case "alias":
		seen := map[string]bool{n.Name.Text: true}
		for _, t := range n.Aliases {
			if seen[t.Text] || invalidText(t.Text) {
				return t
			}
			seen[t.Text] = true
		}
	case "tally", "create":
		seen := map[[2]string]bool{}
		for _, t := range n.Tally {
			key := [2]string{t.Outcome.Text, reasonText(t.Reason)}
			if f.Expected == "one entry per outcome and reason" && seen[key] {
				return t.Outcome
			}
			seen[key] = true
			if f.Expected == "1..1000000000" && t.Count.Text == f.Actual {
				return t.Count
			}
		}
		if f.Expected != "one entry per outcome and reason" && f.Expected != "1..1000000000" && len(n.Tally) > 0 {
			return n.Tally[0].Outcome
		}
	case "reason":
		for _, t := range n.Tally {
			if badReason(t.Outcome, t.Reason) {
				if t.Reason != nil {
					return *t.Reason
				}
				return t.Outcome
			}
		}
		if n.Count != nil {
			return *n.Count
		}
	case "count":
		if n.Count != nil {
			return *n.Count
		}
		if n.Kind.Text != "" {
			return n.Kind
		}
	case "range":
		if n.Min != nil {
			return *n.Min
		}
	case "kind":
		if n.Kind.Text != "" {
			return n.Kind
		}
	case "clock":
		return n.Start
	}
	return n.ID
}

// occupantWorldOperand places a whole-world occupant fault.
func occupantWorldOperand(f *core.Fault, n worldNode) (token, bool) {
	if len(n.Occupants) == 0 {
		return token{}, false
	}
	switch {
	case f.Code == "E_OCCUPANT" && f.Field == "clock":
		return n.Occupants[0].Start, true
	case f.Code == "E_OCCUPANT" && f.Field == "occupants" && len(n.Occupants) > core.MaxWorldOccupants:
		return n.Occupants[core.MaxWorldOccupants].Start, true
	case f.Code == "E_OCCUPANT", f.Code == "E_DUPLICATE" && f.Field == "occupant":
		for _, v := range n.Occupants {
			if v.ID.Text == f.Subject {
				return occupantOperand(f, v), true
			}
		}
	}
	return token{}, false
}

// createOccupant lowers and checks `create occupant`.
func (a *analyzer) createOccupant(catalog core.Blueprint, n occupantNode) core.Operation {
	o := a.occupant(n)
	op := core.Operation{Occupant: &core.OccupantOp{Create: &o}}
	err := catalog.CheckOperationStatic(op)
	if f, ok := err.(*core.Fault); ok && f.Field == "reference" {
		a.checkAt(err, n.Location)
	} else if ok {
		a.checkAt(err, occupantOperand(f, n))
	}
	return op
}

// occupantAction lowers and checks a change to one occupant.
func (a *analyzer) occupantAction(catalog core.Blueprint, n occupantActionNode) core.Operation {
	id := core.OccupantID(n.Occupant.Text)
	op := core.OccupantOp{}
	switch n.Verb.Text {
	case "move":
		op.Move = &core.MoveOccupant{Occupant: id, To: core.LocationID(n.To.Text)}
	case "count":
		op.Count = &core.CountOccupant{Occupant: id, Count: a.number(*n.Count)}
	case "resolve":
		op.Resolve = &core.ResolveOccupant{Occupant: id, Outcome: core.Outcome(n.Outcome.Text), Amount: a.amount(n.Count), Reason: reasonText(n.Reason)}
	case "return":
		op.Return = &core.ReturnOccupant{Occupant: id, Outcome: core.Outcome(n.Outcome.Text), Amount: a.amount(n.Count), Reason: reasonText(n.Reason)}
	case "set":
		op.Attitude = &core.SetAttitude{Occupant: id, Attitude: core.Attitude(n.Attitude.Text)}
	case "rename":
		op.Rename = &core.RenameOccupant{Occupant: id, Name: n.To.Text}
	}
	o := core.Operation{Occupant: &op}
	err := catalog.CheckOperationStatic(o)
	position := n.Verb
	if f, ok := err.(*core.Fault); ok {
		switch {
		case f.Field == "occupant":
			position = n.Occupant
		case f.Field == "reference", f.Field == "name":
			position = n.To
		case f.Code == "E_AMOUNT":
			position = *n.Count
		case f.Field == "reason" && n.Reason != nil:
			position = *n.Reason
		case f.Field == "reason":
			position = n.Outcome
		}
	}
	a.checkAt(err, position)
	return o
}

// amount lowers an optional `by <n>`.
func (a *analyzer) amount(t *token) *int64 {
	if t == nil {
		return nil
	}
	n := a.number(*t)
	return &n
}

// mapEdit lowers and checks a change to the map.
func (a *analyzer) mapEdit(catalog core.Blueprint, n mapEditNode) core.Operation {
	e := core.MapEdit{}
	character, location := core.CharacterID(n.Subject.Text), core.LocationID(n.Subject.Text)
	link := core.MapLink{From: location, To: core.LocationID(n.To.Text)}
	switch n.Verb.Text {
	case "join":
		e.JoinParty = &character
	case "leave":
		e.LeaveParty = &character
	case "close":
		e.CloseRoute = &link
	case "open":
		e.OpenRoute = &link
	case "mark":
		if n.Noun.Text == "visited" {
			e.MarkVisited = &location
		} else {
			e.MarkHazard = &location
		}
	case "clear":
		e.ClearHazard = &location
	}
	o := core.Operation{MapEdit: &e}
	err := catalog.CheckOperationStatic(o)
	position := n.Subject
	if f, ok := err.(*core.Fault); ok && f.Field == "map" {
		position = n.Verb
	}
	a.checkAt(err, position)
	return o
}
