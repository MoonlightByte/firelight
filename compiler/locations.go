package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"sort"
)

// Duplicate entities share a global namespace, but lowering groups declarations
// by kind. Diagnose in source order without changing the emitted Blueprint.
func (a *analyzer) checkEntityDeclarations(n worldNode) {
	ids := entityTokens(n)
	sort.Slice(ids, func(i, j int) bool {
		x, y := ids[i].Pos, ids[j].Pos
		return x.Line < y.Line || (x.Line == y.Line && x.Column < y.Column)
	})
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id.Text] {
			a.err = diag(id.Pos, "E_DUPLICATE", "unique canonical ID", id.Text, "duplicate canonical identity")
			return
		}
		seen[id.Text] = true
	}
}
func entityTokens(n worldNode) []token {
	ids := []token{}
	for _, v := range n.Characters {
		ids = append(ids, v.ID)
	}
	for _, v := range n.Items {
		ids = append(ids, v.ID)
	}
	for _, v := range n.Locations {
		ids = append(ids, v.ID)
	}
	for _, v := range n.Occupants {
		ids = append(ids, v.ID)
	}
	for _, v := range n.Quests {
		ids = append(ids, v.ID)
	}
	return ids
}

// Most faults are checked at an explicit operand while lowering. Whole-world
// validation also checks cross-record invariants. Resolve those faults through
// their code and typed AST context, never through a global spelling lookup.
// Local stat/resource labels are data, not namespace tags.
func (a *analyzer) checkWorld(err error, n worldNode) {
	if err == nil || a.err != nil {
		return
	}
	var f *core.Fault
	if errors.As(err, &f) {
		if t, ok := catalogWorldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if f.Code == "E_CHARGES" {
			for _, i := range n.Items {
				if i.ID.Text == f.Subject {
					a.checkAt(err, chargesItemOperand(f, i))
					return
				}
			}
		}
		if t, ok := equipmentWorldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := worldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := occupantWorldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := questWorldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := journalWorldOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := traitLawOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if t, ok := attacksOperand(f, n); ok {
			a.checkAt(err, t)
			return
		}
		if n.Dice != nil && f.Code == "E_DICE" {
			a.checkAt(err, n.Dice.Start)
			return
		}
		if n.Battle != nil && f.Code == "E_BATTLE" {
			a.checkAt(err, n.Battle.Start)
			return
		}
		if n.Adventure != nil {
			a.checkAt(err, adventureOperand(f, *n.Adventure))
			return
		}
	}
	a.check(err)
}
func worldOperand(f *core.Fault, n worldNode) (token, bool) {
	if f.Code == "E_CONDITION" && (f.Field == "anchor" || f.Field == "ends_at_minimum" || f.Field == "ends_on_condition") {
		for _, c := range n.Conditions {
			if c.ID.Text == f.Subject {
				return conditionOperand(f, c), true
			}
		}
	}
	switch f.Code {
	case "E_CUSTODY":
		for _, i := range n.Items {
			if i.ID.Text == f.Subject {
				if i.Custody != nil {
					return i.Custody.ID, true
				}
				if i.Container != nil {
					return *i.Container, true
				}
				return i.ID, true
			}
		}
	case "E_QUANTITY":
		for _, i := range n.Items {
			if i.ID.Text == f.Subject && f.Field == "recoverable" && i.Recoverable != nil {
				return *i.Recoverable, true
			}
			if i.ID.Text == f.Subject && i.Quantity != nil {
				return *i.Quantity, true
			}
		}
	case "E_RULE":
		switch f.Field {
		case "transfer", "wear":
			t := n.Transfer
			if f.Field == "wear" {
				t = n.Wear
			}
			if t.Pos.Line == 0 {
				t = n.RulesEnd
			}
			return t, t.Pos.Line != 0
		case "wearer":
			for _, v := range n.Items {
				if v.ID.Text == f.Subject && v.Wearer != nil {
					return *v.Wearer, true
				}
			}
		case "recovery":
			if n.Recovery != nil {
				return n.Recovery.Start, true
			}
		case "recoverable":
			for _, v := range n.Items {
				if v.ID.Text == f.Subject && v.Recoverable != nil {
					return *v.Recoverable, true
				}
			}
		case "instances":
			for _, v := range n.Conditions {
				if v.ID.Text == f.Subject {
					return v.ID, true
				}
			}
		}
	case "E_ID":
		switch f.Field {
		case "identity":
			for _, id := range entityTokens(n) {
				if id.Text == f.Subject {
					return id, true
				}
			}
		case "stat", "resource":
			for _, c := range n.Characters {
				if c.ID.Text != f.Subject {
					continue
				}
				if f.Field == "stat" {
					for _, s := range c.Stats {
						if s.ID.Text == f.Actual {
							return s.ID, true
						}
					}
				}
				if f.Field == "resource" {
					for _, r := range c.Resources {
						if r.ID.Text == f.Actual {
							return r.ID, true
						}
					}
				}
			}
		case "relation":
			for _, r := range n.Relations {
				if r.Kind.Text == f.Actual {
					return r.Kind, true
				}
			}
		}
	case "E_BUFFER":
		for _, c := range n.Characters {
			if c.ID.Text == f.Subject {
				for _, v := range c.Buffers {
					if v.Pool.Text == f.Field {
						return v.Pool, true
					}
					if v.Target.Text == f.Field {
						return v.Target, true
					}
				}
			}
		}
	case "E_BOUNDS", "E_OVERFLOW":
		for _, c := range n.Characters {
			if c.ID.Text != f.Subject {
				continue
			}
			if f.Code == "E_BOUNDS" {
				for _, r := range c.Resources {
					if r.ID.Text == f.Field {
						return r.ID, true
					}
				}
			}
			if f.Code == "E_OVERFLOW" {
				for _, s := range c.Stats {
					if s.ID.Text == f.Field {
						return s.ID, true
					}
				}
			}
		}
	case "E_STAT":
		// Static checking already resolved direct targets and Condition targets.
		// The remaining dynamic dependency is an initial item's current wearer.
		for _, e := range n.Effects {
			if e.Mode.Text != "worn" || e.Stat.Text != f.Field {
				continue
			}
			for _, i := range n.Items {
				if i.ID.Text == e.Target.Text && i.Wearer != nil && i.Wearer.Text == f.Subject {
					return e.Stat, true
				}
			}
		}
	case "E_DUPLICATE":
		switch f.Field {
		case "effect":
			var first *token
			for _, e := range n.Effects {
				if e.ID.Text != f.Subject {
					continue
				}
				if first != nil {
					return e.ID, true
				}
				t := e.ID
				first = &t
			}
			// A derived Condition effect may collide with one explicit effect.
			if first != nil {
				return *first, true
			}
		case "relation":
			seen := map[struct {
				from, to core.Ref
				kind     string
			}]bool{}
			for _, r := range n.Relations {
				key := struct {
					from, to core.Ref
					kind     string
				}{ref(r.From), ref(r.To), r.Kind.Text}
				if seen[key] {
					return r.Kind, true
				}
				seen[key] = true
			}
		}
	}
	return token{}, false
}
