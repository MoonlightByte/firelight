package compiler

import (
	"strconv"

	"github.com/MoonlightByte/firelight/core"
)

// questNode is a quest declaration: its requirements, the quest it sits
// under, and the log that states its progress back.
type questNode struct {
	namedNode
	Start    token
	Requires []token
	SideOf   *token
	Entries  []questEntryNode
}

// questEntryNode is one declared log line.
type questEntryNode struct {
	Kind   token
	Text   *token
	Ahead  []token
	Reason *token
	Tick   *token
}

// questActionNode changes one quest: start, note, complete, fail or reopen.
type questActionNode struct {
	Verb, Quest token
	Ahead       *token // complete's `ahead because` reason
	Text        *token // the impact
	Reason      *token // reopen's because
}

// quest parses `"<id>" named "<name>"`, then `;` or a body, after the
// keyword start.
func (p *parser) quest(start token) questNode {
	n := questNode{Start: start, namedNode: p.named()}
	if p.peek().Text == ";" {
		p.take()
		return n
	}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		key := p.choice("requires", "side", "started", "note", "completed", "failed", "reopened")
		switch key.Text {
		case "requires":
			n.Requires = append(n.Requires, p.str())
		case "side":
			p.expect("of")
			v := p.str()
			if n.SideOf != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one side of", key.Text, "duplicate quest field")
				break
			}
			n.SideOf = &v
		default:
			n.Entries = append(n.Entries, p.questEntry(key))
		}
		p.expect(";")
	}
	p.expect("}")
	return n
}

// questEntry parses the rest of one log line:
// `started|failed [impact "<text>"]`, `note impact "<text>"`,
// `completed [ahead "<id>"... because "<reason>"] [impact "<text>"]` or
// `reopened because "<reason>"`, each with an optional `at <tick>`.
func (p *parser) questEntry(kind token) questEntryNode {
	e := questEntryNode{Kind: kind}
	switch kind.Text {
	case "reopened":
		p.expect("because")
		r := p.str()
		e.Reason = &r
	case "note":
		p.expect("impact")
		t := p.str()
		e.Text = &t
	default:
		if kind.Text == "completed" && p.isWord("ahead") {
			p.take()
			e.Ahead = append(e.Ahead, p.str())
			for p.peek().Kind == quoted {
				e.Ahead = append(e.Ahead, p.str())
			}
			p.expect("because")
			r := p.str()
			e.Reason = &r
		}
		e.Text = p.impact()
	}
	if p.isWord("at") {
		p.take()
		t := p.num()
		e.Tick = &t
	}
	return e
}

// impact parses an optional `impact "<text>"`.
func (p *parser) impact() *token {
	if !p.isWord("impact") {
		return nil
	}
	p.take()
	t := p.str()
	return &t
}

// questAction parses the rest of start, note, complete, fail and reopen.
func (p *parser) questAction(verb token) questActionNode {
	p.expect("quest")
	n := questActionNode{Verb: verb, Quest: p.str()}
	switch verb.Text {
	case "reopen":
		p.expect("because")
		r := p.str()
		n.Reason = &r
	case "note":
		p.expect("impact")
		t := p.str()
		n.Text = &t
	default:
		if verb.Text == "complete" && p.isWord("ahead") {
			p.take()
			p.expect("because")
			r := p.str()
			n.Ahead = &r
		}
		n.Text = p.impact()
	}
	return n
}

func textOf(t *token) *string {
	if t == nil {
		return nil
	}
	s := t.Text
	return &s
}

// quest lowers a declaration. Its status is the one its log ends in.
func (a *analyzer) quest(n questNode) core.Quest {
	q := core.Quest{ID: core.QuestID(n.ID.Text), Name: n.Name.Text}
	if n.SideOf != nil {
		q.SideOf = core.QuestID(n.SideOf.Text)
	}
	for _, t := range n.Requires {
		q.Requires = append(q.Requires, core.QuestID(t.Text))
	}
	for _, v := range n.Entries {
		e := core.QuestEntry{Kind: core.QuestEntryKind(v.Kind.Text), Text: textOf(v.Text), Reason: textOf(v.Reason)}
		for _, t := range v.Ahead {
			e.Ahead = append(e.Ahead, core.QuestID(t.Text))
		}
		if v.Tick != nil {
			tick, err := strconv.ParseUint(v.Tick.Text, 10, 64)
			if err != nil && a.err == nil {
				a.err = diag(v.Tick.Pos, "E_INTEGER", "unsigned 64-bit tick", v.Tick.Text, "tick exceeds the clock's domain")
			}
			e.Tick = &tick
		}
		q.Log = append(q.Log, e)
	}
	q.Status = core.QuestStatusOf(q.Log)
	return q
}

// questOperand is the token a quest fault is about.
func questOperand(f *core.Fault, n questNode) token {
	switch f.Field {
	case "name":
		return n.Name
	case "side_of":
		if n.SideOf != nil {
			return *n.SideOf
		}
	case "requires":
		if f.Expected == "at most 16 requirements" {
			return n.Requires[core.MaxQuestRequirements]
		}
		if t, ok := questToken(f, n.Requires); ok {
			return t
		}
	case "ahead":
		for _, e := range n.Entries {
			if len(e.Ahead) > core.MaxQuestRequirements {
				return e.Ahead[core.MaxQuestRequirements]
			}
			if t, ok := questToken(f, e.Ahead); ok {
				return t
			}
		}
	case "text", "reason":
		for _, e := range n.Entries {
			t := e.Text
			if f.Field == "reason" {
				t = e.Reason
			}
			if t != nil && (invalidText(t.Text) || len(t.Text) > core.MaxQuestTextBytes) {
				return *t
			}
		}
	case "log":
		if len(n.Entries) > core.MaxQuestLog {
			return n.Entries[core.MaxQuestLog].Kind
		}
	}
	return n.ID
}

// questToken finds the quest a fault names in a list, or for a repeat,
// the repeat.
func questToken(f *core.Fault, ts []token) (token, bool) {
	seen := false
	for _, t := range ts {
		if t.Text != f.Actual {
			continue
		}
		if seen || f.Expected != "each quest once" {
			return t, true
		}
		seen = true
	}
	return token{}, false
}

// questWorldOperand places a whole-world quest fault.
func questWorldOperand(f *core.Fault, n worldNode) (token, bool) {
	if len(n.Quests) == 0 {
		return token{}, false
	}
	switch {
	case f.Code == "E_QUEST" && f.Field == "quests" && len(n.Quests) > core.MaxWorldQuests:
		return n.Quests[core.MaxWorldQuests].Start, true
	case f.Code == "E_QUEST", f.Code == "E_DUPLICATE" && f.Field == "quest":
		for _, v := range n.Quests {
			if v.ID.Text == f.Subject {
				return questOperand(f, v), true
			}
		}
	}
	return token{}, false
}

// questAction lowers and checks a change to one quest.
func (a *analyzer) questAction(catalog core.Blueprint, n questActionNode) core.Operation {
	id := core.QuestID(n.Quest.Text)
	op := core.QuestOp{}
	step := &core.QuestStep{Quest: id, Text: textOf(n.Text)}
	switch n.Verb.Text {
	case "start":
		op.Start = step
	case "note":
		op.Note = step
	case "fail":
		op.Fail = step
	case "complete":
		op.Complete = &core.CompleteQuest{Quest: id, Ahead: textOf(n.Ahead), Text: textOf(n.Text)}
	case "reopen":
		op.Reopen = &core.ReopenQuest{Quest: id, Reason: textOf(n.Reason)}
	}
	o := core.Operation{Quest: &op}
	err := catalog.CheckOperationStatic(o)
	position := n.Verb
	if f, ok := err.(*core.Fault); ok {
		switch {
		case f.Field == "reference":
			position = n.Quest
		case f.Field == "text" && n.Text != nil:
			position = *n.Text
		case f.Field == "reason" && n.Ahead != nil:
			position = *n.Ahead
		case f.Field == "reason" && n.Reason != nil:
			position = *n.Reason
		}
	}
	a.checkAt(err, position)
	return o
}
