package compiler

import (
	"strconv"

	"github.com/MoonlightByte/firelight/core"
)

// journalNode is a declared journal entry, stated back as a receipt wrote
// it: `journal "<id>" <kind> seq <n> at "<place>" [tick <n>] [with "<id>"
// ...] [text "<text>"];`.
type journalNode struct {
	Start, ID, Kind, Seq, Place token
	Tick                        *token
	With                        []token
	Text                        *token
}

// journalActionNode records an entry: `record journal "<id>" <kind> [at
// "<place>"] [with [party] ["<id>" ...]] [text "<text>"];`.
type journalActionNode struct {
	Verb, ID, Kind token
	Place          *token
	Party          bool
	With           []token
	Text           *token
}

var journalKindWords = []string{"departure", "arrival", "event", "discovery", "note"}

// journal parses a declaration after the keyword: an entry, or
// `next <n>`.
func (p *parser) journal(start token, w *worldNode) {
	if p.isWord("next") {
		p.take()
		n := p.num()
		if w.JournalNext != nil {
			p.err = diag(start.Pos, "E_DUPLICATE", "one journal next", start.Text, "duplicate journal next")
		}
		w.JournalNext = &n
		p.expect(";")
		return
	}
	n := journalNode{Start: start, ID: p.str(), Kind: p.choice(journalKindWords...)}
	p.expect("seq")
	n.Seq = p.num()
	p.expect("at")
	n.Place = p.str()
	if p.isWord("tick") {
		p.take()
		t := p.num()
		n.Tick = &t
	}
	if p.isWord("with") {
		p.take()
		n.With = append(n.With, p.str())
		for p.peek().Kind == quoted {
			n.With = append(n.With, p.str())
		}
	}
	n.Text = p.journalText()
	p.expect(";")
	w.Journal = append(w.Journal, n)
}

// journalText parses an optional `text "<text>"`.
func (p *parser) journalText() *token {
	if !p.isWord("text") {
		return nil
	}
	p.take()
	t := p.str()
	return &t
}

// journalAction parses the rest of `record journal ...`.
func (p *parser) journalAction(verb token) journalActionNode {
	p.expect("journal")
	n := journalActionNode{Verb: verb, ID: p.str(), Kind: p.choice(journalKindWords...)}
	if p.isWord("at") {
		p.take()
		t := p.str()
		n.Place = &t
	}
	if p.isWord("with") {
		p.take()
		if p.isWord("party") {
			p.take()
			n.Party = true
		}
		for p.peek().Kind == quoted {
			n.With = append(n.With, p.str())
		}
		if !n.Party && len(n.With) == 0 {
			p.expect("party")
		}
	}
	n.Text = p.journalText()
	return n
}

func (a *analyzer) unsigned(t token) uint64 {
	v, err := strconv.ParseUint(t.Text, 10, 64)
	if err != nil && a.err == nil {
		a.err = diag(t.Pos, "E_INTEGER", "unsigned 64-bit integer", t.Text, "value outside the unsigned 64-bit domain")
	}
	return v
}

// journal lowers the declared entries. Next is the declared one, or one
// past the last entry.
func (a *analyzer) journal(n worldNode) *core.Journal {
	if len(n.Journal) == 0 && n.JournalNext == nil {
		return nil
	}
	j := &core.Journal{Next: 1, Entries: []core.JournalEntry{}}
	for _, v := range n.Journal {
		e := core.JournalEntry{ID: core.JournalEntryID(v.ID.Text), Kind: core.JournalKind(v.Kind.Text), Seq: a.unsigned(v.Seq), Place: core.LocationID(v.Place.Text), Text: textOf(v.Text)}
		if v.Tick != nil {
			t := a.unsigned(*v.Tick)
			e.Tick = &t
		}
		for _, t := range v.With {
			e.Participants = append(e.Participants, t.Text)
		}
		j.Entries = append(j.Entries, e)
		if e.Seq < ^uint64(0) {
			j.Next = e.Seq + 1
		}
	}
	if n.JournalNext != nil {
		j.Next = a.unsigned(*n.JournalNext)
	}
	return j
}

// journalWorldOperand places a journal fault on the declaration it names.
func journalWorldOperand(f *core.Fault, n worldNode) (token, bool) {
	if f.Code != "E_JOURNAL" && !(f.Code == "E_DUPLICATE" && f.Field == "journal") {
		return token{}, false
	}
	if f.Field == "next" || f.Field == "entries" {
		if n.JournalNext != nil {
			return *n.JournalNext, true
		}
		if len(n.Journal) > 0 {
			return n.Journal[len(n.Journal)-1].Start, true
		}
	}
	var found *journalNode
	for i := range n.Journal {
		if n.Journal[i].ID.Text == f.Subject {
			found = &n.Journal[i]
		}
	}
	if found == nil {
		return token{}, false
	}
	switch f.Field {
	case "seq":
		return found.Seq, true
	case "place":
		return found.Place, true
	case "text":
		if found.Text != nil {
			return *found.Text, true
		}
	case "participants":
		// A name given twice is placed on its second mention.
		at := -1
		for i, t := range found.With {
			if t.Text == f.Actual {
				at = i
			}
		}
		if at >= 0 {
			return found.With[at], true
		}
	}
	return found.ID, true
}

// journalAction lowers and checks `record journal`.
func (a *analyzer) journalAction(catalog core.Blueprint, n journalActionNode) core.Operation {
	v := core.RecordJournal{ID: core.JournalEntryID(n.ID.Text), Kind: core.JournalKind(n.Kind.Text), Party: n.Party, Text: textOf(n.Text)}
	if n.Place != nil {
		p := core.LocationID(n.Place.Text)
		v.Place = &p
	}
	for _, t := range n.With {
		v.With = append(v.With, t.Text)
	}
	o := core.Operation{Journal: &v}
	err := catalog.CheckOperationStatic(o)
	position := n.Verb
	if f, ok := err.(*core.Fault); ok {
		switch {
		case f.Field == "reference" && n.Place != nil:
			position = *n.Place
		case f.Field == "text" && n.Text != nil:
			position = *n.Text
		case f.Field == "id":
			position = n.ID
		case f.Field == "participants":
			for _, t := range n.With {
				if t.Text == f.Actual {
					position = t
				}
			}
		}
	}
	a.checkAt(err, position)
	return o
}
