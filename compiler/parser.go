package compiler

type parser struct {
	tokens []token
	index  int
	err    *Diagnostic
}

func (p *parser) peek() token { return p.tokens[p.index] }
func (p *parser) take() token {
	t := p.peek()
	if t.Kind != eof {
		p.index++
	}
	return t
}
func (p *parser) literal(k tokenKind, expected string) token {
	t := p.peek()
	if p.err != nil {
		return t
	}
	if t.Kind != k {
		p.err = diag(t.Pos, "E_PARSE", expected, t.Text, "expected "+expected)
		return t
	}
	return p.take()
}
func (p *parser) expect(s string) token {
	t := p.peek()
	if p.err != nil {
		return t
	}
	if t.Text != s || t.Kind == quoted {
		p.err = diag(t.Pos, "E_PARSE", s, t.Text, "expected "+s)
		return t
	}
	return p.take()
}
func (p *parser) choice(values ...string) token {
	t := p.peek()
	if p.err != nil {
		return t
	}
	for _, s := range values {
		if t.Kind == word && t.Text == s {
			return p.take()
		}
	}
	p.err = diag(t.Pos, "E_PARSE", joinChoices(values), t.Text, "unexpected construct")
	return t
}
func joinChoices(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " or "
		}
		out += v
	}
	return out
}
func (p *parser) str() token       { return p.literal(quoted, "quoted string") }
func (p *parser) num() token       { return p.literal(integer, "signed decimal integer") }
func (p *parser) ref() refNode     { return refNode{p.choice("character", "item", "location"), p.str()} }
func (p *parser) named() namedNode { id := p.str(); p.expect("named"); return namedNode{id, p.str()} }
func (p *parser) effect() effectNode {
	e := effectNode{ID: p.str()}
	p.expect("from")
	e.Source = p.ref()
	p.expect("on")
	e.Mode = p.choice("character", "worn")
	e.Target = p.str()
	p.expect("stat")
	e.Stat = p.str()
	p.expect("add")
	e.Amount = p.num()
	return e
}
func (p *parser) relation() relationNode {
	r := relationNode{From: p.ref()}
	r.Kind = p.str()
	r.To = p.ref()
	p.expect("=")
	r.Strength = p.num()
	return r
}
func (p *parser) world() worldNode {
	w := worldNode{}
	rulesSeen := false
	for p.peek().Kind != eof && p.err == nil {
		if p.isWord("battle") {
			start := p.take()
			if w.Battle != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one battle block", start.Text, "duplicate battle rules")
				break
			}
			n := p.battle(start)
			w.Battle = &n
			continue
		}
		if p.isWord("map") {
			start := p.take()
			if w.Map != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one map block", start.Text, "duplicate map")
				break
			}
			n := p.worldMap(start)
			w.Map = &n
			continue
		}
		if p.isWord("clock") {
			start := p.take()
			if w.Clock != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one clock declaration", start.Text, "duplicate clock")
				break
			}
			n := p.clock()
			w.Clock = &n
			continue
		}
		if p.isWord("dice") {
			start := p.take()
			if w.Dice != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one dice declaration", start.Text, "duplicate dice")
				break
			}
			n := p.dice(start)
			w.Dice = &n
			continue
		}
		if p.isWord("equipment") {
			start := p.take()
			if w.Equipment != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one equipment block", start.Text, "duplicate equipment rules")
				break
			}
			n := p.equipment(start)
			w.Equipment = &n
			continue
		}
		if p.isWord("adventure") {
			start := p.take()
			if w.Adventure != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one adventure block", start.Text, "duplicate adventure block")
				break
			}
			n := p.adventure(start)
			w.Adventure = &n
			continue
		}
		if p.isWord("condition_check") {
			w.ConditionChecks = append(w.ConditionChecks, p.conditionCheck())
			continue
		}
		if p.isWord("occupant") {
			start := p.take()
			w.Occupants = append(w.Occupants, p.occupant(start))
			continue
		}
		if p.isWord("quest") {
			start := p.take()
			w.Quests = append(w.Quests, p.quest(start))
			continue
		}
		if p.isWord("journal") {
			p.journal(p.take(), &w)
			continue
		}
		if p.isWord("damage") {
			w.DamageTraits = append(w.DamageTraits, p.damageTrait(p.take()))
			continue
		}
		if p.isWord("attacks") {
			start := p.take()
			if w.Attacks != nil {
				p.err = diag(start.Pos, "E_DUPLICATE", "one attacks block", start.Text, "duplicate attacks law")
				break
			}
			n := p.attacks(start)
			w.Attacks = &n
			continue
		}
		if p.isWord("relation_kind") {
			p.take()
			w.RelationDefinitions = append(w.RelationDefinitions, p.relationDefinition())
			continue
		}
		v := p.choice("rules", "location", "character", "item", "effect", "relation", "condition", "ability")
		switch v.Text {
		case "ability":
			w.Abilities = append(w.Abilities, p.ability())
		case "condition":
			if p.peek().Kind == word && p.peek().Text == "type" {
				p.take()
				w.ConditionDefinitions = append(w.ConditionDefinitions, p.conditionDefinition())
			} else {
				w.Conditions = append(w.Conditions, p.condition())
				p.expect(";")
			}
		case "rules":
			if rulesSeen {
				p.err = diag(v.Pos, "E_DUPLICATE", "one rules block", "repeated rules", "duplicate rules block")
				break
			}
			rulesSeen = true
			p.expect("{")
			for p.peek().Text != "}" && p.err == nil {
				key := p.choice("transfer", "wear", "recovery")
				switch key.Text {
				case "recovery":
					if w.Recovery != nil {
						p.err = diag(key.Pos, "E_DUPLICATE", "one recovery rule", "repeated rule", "duplicate rule")
						break
					}
					r := recoveryNode{Start: key, Numerator: p.num()}
					p.expect("of")
					r.Denominator = p.num()
					w.Recovery = &r
				case "transfer":
					if w.Transfer.Text != "" {
						p.err = diag(key.Pos, "E_DUPLICATE", "one transfer policy", "repeated policy", "duplicate rule")
						break
					}
					w.Transfer = p.choice("keeps_worn", "unequips")
				case "wear":
					if w.Wear.Text != "" {
						p.err = diag(key.Pos, "E_DUPLICATE", "one wear policy", "repeated policy", "duplicate rule")
						break
					}
					w.Wear = p.choice("any", "owner_only")
				}
				p.expect(";")
			}
			w.RulesEnd = p.expect("}")
		case "location":
			w.Locations = append(w.Locations, p.named())
			p.expect(";")
		case "character":
			c := p.character()
			w.Characters = append(w.Characters, c)
		case "item":
			if p.isWord("type") {
				p.take()
				w.ItemDefinitions = append(w.ItemDefinitions, p.itemDefinition())
			} else {
				w.Items = append(w.Items, p.item())
			}
		case "effect":
			w.Effects = append(w.Effects, p.effect())
			p.expect(";")
		case "relation":
			w.Relations = append(w.Relations, p.relation())
			p.expect(";")
		}
	}
	return w
}
func (p *parser) actions() []actionNode {
	out := []actionNode{}
	for p.peek().Kind != eof && p.err == nil {
		if p.isWord("place") {
			a := actionNode{Verb: p.take()}
			v := p.place()
			a.Place = &v
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("split") {
			a := actionNode{Verb: p.take()}
			v := stockNode{Item: p.str()}
			p.expect("quantity")
			v.Amount = p.num()
			p.expect("into")
			v.Created = p.str()
			p.expect("by")
			v.Handler = p.str()
			p.expect(";")
			a.Split = &v
			out = append(out, a)
			continue
		}
		if p.isWord("expand") {
			a := actionNode{Verb: p.take(), Subject: p.str()}
			p.expect("resource")
			a.Resource = p.str()
			p.expect("by")
			a.Amount = p.num()
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("restore") {
			a := actionNode{Verb: p.take(), Subject: p.str()}
			p.expect("resource")
			a.Resource = p.str()
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("travel") {
			a := actionNode{Verb: p.take()}
			p.expect("party")
			p.expect("to")
			a.To = p.str()
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("battle") {
			a := actionNode{Verb: p.take()}
			n := p.battleAction()
			a.Battle = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("advance") {
			a := actionNode{Verb: p.take()}
			p.expect("time")
			p.expect("by")
			a.Amount = p.num()
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("check") {
			a := actionNode{Verb: p.take()}
			v := p.statCheck()
			a.StatCheck = &v
			out = append(out, a)
			continue
		}
		if p.isWord("recover") {
			a := actionNode{Verb: p.take(), Subject: p.str()}
			p.expect("from")
			a.From = p.str()
			if p.isWord("by") {
				p.take()
				a.Amount = p.num()
			}
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("adjust") {
			a := actionNode{Verb: p.take()}
			if p.peek().Kind == quoted {
				a.Subject = p.str()
				p.expect("stat")
				stat := p.str()
				a.Stat = &stat
			} else {
				p.expect("relation")
				a.Relation = relationNode{From: p.ref(), Kind: p.str(), To: p.ref()}
			}
			p.expect("by")
			a.Amount = p.num()
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("count") || p.isWord("resolve") || p.isWord("set") || p.isWord("rename") || p.isWord("return") {
			a := actionNode{Verb: p.take()}
			n := p.occupantAction(a.Verb)
			a.OccupantAction = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("start") || p.isWord("note") || p.isWord("complete") || p.isWord("fail") || p.isWord("reopen") {
			a := actionNode{Verb: p.take()}
			n := p.questAction(a.Verb)
			a.QuestAction = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("attack") {
			a := actionNode{Verb: p.take()}
			n := p.attack(a.Verb)
			a.Attack = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("save") {
			a := actionNode{Verb: p.take()}
			n := p.save(a.Verb)
			a.Save = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("record") {
			a := actionNode{Verb: p.take()}
			n := p.journalAction(a.Verb)
			a.JournalAction = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		if p.isWord("join") || p.isWord("leave") || p.isWord("close") || p.isWord("open") || p.isWord("mark") || p.isWord("clear") {
			a := actionNode{Verb: p.take()}
			n := p.mapEdit(a.Verb)
			a.MapEdit = &n
			p.expect(";")
			out = append(out, a)
			continue
		}
		a := actionNode{Verb: p.choice("create", "add", "consume", "expend", "transfer", "equip", "unequip", "damage", "heal", "spend", "grant", "apply", "remove", "expire", "move", "relate", "invoke")}
		switch a.Verb.Text {
		case "create":
			if p.isWord("occupant") {
				n := p.occupant(p.take())
				a.Occupant = &n
				break
			}
			kind := p.choice("character", "item")
			if kind.Text == "item" {
				i := p.item()
				a.Item = &i
			} else {
				c := p.character()
				a.Character = &c
			}
		case "invoke":
			v := p.invocation()
			a.Invocation = &v
		case "add":
			a.Subject = p.str()
			p.expect("to")
			a.From = p.str()
			p.expect("by")
			a.Amount = p.num()
		case "consume":
			a.Subject = p.str()
			p.expect("from")
			a.From = p.str()
			p.expect("by")
			a.Amount = p.num()
			if p.isWord("recoverable") {
				t := p.take()
				a.Recoverable = &t
			}
		case "expend":
			a.Amount = p.num()
			p.expect("charges")
			p.expect("of")
			a.Subject = p.str()
			p.expect("by")
			a.From = p.str()
		case "transfer":
			a.Subject = p.str()
			p.expect("from")
			a.From = p.str()
			p.expect("to")
			a.To = p.str()
		case "equip":
			a.Subject = p.str()
			p.expect("on")
			a.To = p.str()
			if p.isWord("mode") {
				p.take()
				v := p.str()
				a.Mode = &v
			}
		case "unequip":
			a.Subject = p.str()
		case "damage", "heal", "spend", "grant":
			a.Subject = p.str()
			p.expect("resource")
			a.Resource = p.str()
			p.expect("by")
			a.Amount = p.num()
			if a.Verb.Text == "damage" {
				if p.isWord("kind") {
					p.take()
					k := p.str()
					a.DamageKind = &k
				}
				a.DamageFaces = p.damageFaces()
			}
		case "apply":
			if p.choice("effect", "condition").Text == "condition" {
				c := p.condition()
				a.Condition = &c
			} else {
				a.Effect = p.effect()
			}
		case "remove":
			if p.choice("effect", "condition").Text == "condition" {
				c := conditionNode{ID: p.str()}
				if p.isWord("of") {
					p.take()
					c.Type = p.str()
				}
				p.expect("from")
				c.Target = p.str()
				a.Condition = &c
			} else {
				a.Subject = p.str()
			}
		case "expire":
			p.expect("effect")
			a.Subject = p.str()
		case "move":
			if p.isWord("occupant") {
				p.take()
				n := occupantActionNode{Verb: a.Verb, Occupant: p.str()}
				p.expect("to")
				n.To = p.str()
				a.OccupantAction = &n
				break
			}
			a.Subject = p.str()
			p.expect("to")
			a.To = p.str()
		case "relate":
			a.Relation = p.relation()
		}
		p.expect(";")
		out = append(out, a)
	}
	if len(out) == 0 && p.err == nil {
		p.err = diag(p.peek().Pos, "E_PARSE", "one or more actions", "end of input", "empty transaction")
	}
	return out
}

func (p *parser) character() characterNode {
	c := characterNode{namedNode: p.named()}
	p.expect("at")
	c.Location = p.str()
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		if p.isWord("resist") || p.isWord("vulnerable") || p.isWord("immune") {
			c.Traits = append(c.Traits, traitNode{Key: p.take(), Kind: p.str()})
			p.expect(";")
			continue
		}
		if p.isWord("buffer") {
			p.take()
			v := bufferNode{Pool: p.str()}
			p.expect("before")
			v.Target = p.str()
			p.expect(";")
			c.Buffers = append(c.Buffers, v)
			continue
		}
		key := p.choice("stat", "resource")
		id := p.str()
		p.expect("=")
		n := p.num()
		if key.Text == "stat" {
			c.Stats = append(c.Stats, statNode{id, n})
		} else {
			p.expect("min")
			lo := p.num()
			p.expect("max")
			hi := p.num()
			c.Resources = append(c.Resources, resourceNode{id, n, lo, hi})
		}
		p.expect(";")
	}
	p.expect("}")
	return c
}

func (p *parser) item() itemNode {
	i := itemNode{namedNode: namedNode{ID: p.str()}}
	if p.isWord("from") {
		p.take()
		v := p.str()
		i.From = &v
	} else {
		// Preserve the historical diagnostic for malformed non-catalog items.
		p.expect("named")
		i.Name = p.str()
	}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		if p.isWord("custody") {
			key := p.take()
			if i.Custody != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one custody", key.Text, "duplicate item field")
				break
			}
			v := p.ref()
			i.Custody = &v
			p.expect(";")
			continue
		}
		if p.isWord("container") {
			key := p.take()
			if i.Container != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one container field", key.Text, "duplicate item field")
				break
			}
			v := p.choice("true", "false")
			i.Container = &v
			p.expect(";")
			continue
		}
		if p.isWord("charges") {
			key := p.take()
			if i.Charges != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one charges", key.Text, "duplicate item field")
				break
			}
			v := p.num()
			i.Charges = &v
			if p.isWord("of") {
				p.take()
				m := p.num()
				i.ChargesMax = &m
			}
			if p.isWord("since") {
				p.take()
				t := p.num()
				i.ChargesSince = &t
			}
			if p.isWord("seed") {
				p.take()
				i.ChargesSeed = []token{p.num(), p.num()}
			}
			p.expect(";")
			continue
		}
		if p.isWord("quantity") || p.isWord("recoverable") {
			key := p.take()
			value := p.num()
			dest := &i.Quantity
			if key.Text == "recoverable" {
				dest = &i.Recoverable
			}
			if *dest != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, key.Text, "duplicate item field")
			} else {
				*dest = &value
			}
			p.expect(";")
			continue
		}
		if p.isWord("definition") || p.isWord("mode") || p.isWord("description") {
			key := p.take()
			if i.From != nil && key.Text != "mode" {
				p.err = diag(key.Pos, "E_PARSE", "instance field for item from a definition", key.Text, "catalog fields come from the item definition")
				break
			}
			value := p.str()
			dest := &i.Definition
			if key.Text == "mode" {
				dest = &i.Mode
			} else if key.Text == "description" {
				dest = &i.Description
			}
			if *dest != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, key.Text, "duplicate item field")
			} else {
				*dest = &value
			}
			p.expect(";")
			continue
		}
		key := p.choice("owner", "wearer")
		id := p.str()
		dest := &i.Owner
		if key.Text == "wearer" {
			dest = &i.Wearer
		}
		if *dest != nil {
			p.err = diag(key.Pos, "E_DUPLICATE", "one "+key.Text, "repeated field", "duplicate item field")
		} else {
			*dest = &id
		}
		p.expect(";")
	}
	p.expect("}")
	return i
}
