package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strconv"
	"strings"
)

// Alpha adds a finite declaration/statement vocabulary. Syntax nodes retain
// occurrences; the core IR contains no source data or executable expressions.
type adventureNode struct {
	Start, End, Health token
	Party              []token
	Links              []linkNode
	Grants             []grantNode
	Encounters         []encounterNode
	Objectives         []objectiveNode
	Initial            *token
	Seed               *[2]token
}
type linkNode struct{ From, To token }
type grantNode struct {
	Character token
	Abilities []token
}
type encounterNode struct {
	ID, Location token
	Order        []participantNode
}
type participantNode struct{ Character, Team token }
type objectiveNode struct{ ID, Item, Custodian, Victory, Destination token }
type actionPolicyNode struct {
	Verb, Phase token
	Actor       abilityOperandNode
	TurnCost    *turnCostNode
	Finish      *token
}
type turnCostNode struct{ Resource, Amount, Finish token }
type requirementNode struct {
	Location, Stat, Minimum, Maximum                                               token
	Verb, Kind, Resource, Condition, Distance, Unit, Slot, Count, Definition, Mode token
	Subject, Other                                                                 abilityOperandNode
}
type checkValueNode struct {
	Literal   *token
	Character abilityOperandNode
	Stat      token
}
type checkNode struct {
	Boost               *checkBoostNode
	ActorRoll           *token
	ID, Sides           token
	Modifier, Threshold checkValueNode
	Success, Failure    []abilityStepNode
}
type checkBoostNode struct{ Start, Condition, Resource, Cost, Sides token }

func (p *parser) isWord(s string) bool { return p.peek().Kind == word && p.peek().Text == s }

// Recognize the opt-in forms without rewriting established diagnostics for
// malformed legacy forms, including their expected-choice wording.
func (p *parser) abilityVerb(metadata bool) token {
	for _, s := range []string{"travel", "claim", "advance", "restore", "place", "expand", "remove", "consume", "adjust"} {
		if p.isWord(s) {
			return p.take()
		}
	}
	if metadata {
		for _, s := range []string{"action", "require", "check", "roll"} {
			if p.isWord(s) {
				return p.take()
			}
		}
		return p.choice("parameter", "damage", "heal", "spend", "apply", "transfer", "equip", "unequip")
	}
	return p.choice("damage", "heal", "spend", "apply", "transfer", "equip", "unequip")
}

func (p *parser) adventure(start token) adventureNode {
	n := adventureNode{Start: start}
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		key := p.choice("party", "health", "link", "grant", "encounter", "objective", "initial", "seed")
		switch key.Text {
		case "party":
			n.Party = append(n.Party, p.str())
		case "health":
			if n.Health.Pos.Line != 0 {
				p.err = diag(key.Pos, "E_DUPLICATE", "one health selector", key.Text, "duplicate adventure health")
				break
			}
			n.Health = p.str()
		case "link":
			v := linkNode{From: p.str()}
			p.expect("to")
			v.To = p.str()
			n.Links = append(n.Links, v)
		case "grant":
			v := grantNode{Character: p.str()}
			p.expect("{")
			for p.peek().Text != "}" && p.err == nil {
				p.expect("ability")
				v.Abilities = append(v.Abilities, p.str())
				p.expect(";")
			}
			p.expect("}")
			n.Grants = append(n.Grants, v)
			continue
		case "encounter":
			v := encounterNode{ID: p.str()}
			p.expect("at")
			v.Location = p.str()
			p.expect("{")
			for p.peek().Text != "}" && p.err == nil {
				p.expect("participant")
				c := p.str()
				p.expect("team")
				v.Order = append(v.Order, participantNode{c, p.choice("party", "hostile")})
				p.expect(";")
			}
			p.expect("}")
			n.Encounters = append(n.Encounters, v)
			continue
		case "objective":
			v := objectiveNode{ID: p.str()}
			p.expect("item")
			v.Item = p.str()
			p.expect("custodian")
			v.Custodian = p.str()
			p.expect("victory")
			v.Victory = p.str()
			p.expect("destination")
			v.Destination = p.str()
			n.Objectives = append(n.Objectives, v)
		case "initial":
			if n.Initial != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one initial encounter", key.Text, "duplicate initial encounter")
				break
			}
			p.expect("encounter")
			v := p.str()
			n.Initial = &v
		case "seed":
			if n.Seed != nil {
				p.err = diag(key.Pos, "E_DUPLICATE", "one seed pair", key.Text, "duplicate adventure seed")
				break
			}
			v := [2]token{p.num(), p.num()}
			n.Seed = &v
		}
		p.expect(";")
	}
	n.End = p.expect("}")
	if p.err == nil && n.Health.Pos.Line == 0 {
		p.err = diag(n.End.Pos, "E_PARSE", "health selector", "}", "adventure requires a health selector")
	}
	if p.err == nil && n.Seed == nil {
		p.err = diag(n.End.Pos, "E_PARSE", "seed pair", "}", "adventure requires a seed pair")
	}
	return n
}

func (p *parser) requirement(verb token) requirementNode {
	kind := p.peek()
	if p.isWord("carried_item") || p.isWord("within") || p.isWord("free_slot") || p.isWord("worn_training") || p.isWord("at") || p.isWord("stat_between") {
		p.take()
	} else {
		kind = p.choice("alive", "colocated", "allied", "hostile", "distinct", "has", "lacks", "party_owned")
	}
	r := requirementNode{Verb: verb, Kind: kind}
	r.Subject = p.abilityOperand(false)
	switch r.Kind.Text {
	case "carried_item":
		p.expect("by")
		r.Other = p.abilityOperand(false)
	case "at":
		p.expect("location")
		r.Location = p.str()
	case "stat_between":
		p.expect("stat")
		r.Stat = p.str()
		p.expect("min")
		r.Minimum = p.num()
		p.expect("max")
		r.Maximum = p.num()
	case "alive":
		p.expect("resource")
		r.Resource = p.str()
	case "colocated", "allied", "hostile", "distinct", "within":
		p.expect("with")
		r.Other = p.abilityOperand(false)
		if r.Kind.Text == "within" {
			p.expect("distance")
			r.Distance = p.num()
			p.expect("unit")
			r.Unit = p.str()
		}
	case "has", "lacks":
		p.expect("condition")
		r.Condition = p.str()
	case "free_slot":
		p.expect("slot")
		r.Slot = p.str()
		p.expect("count")
		r.Count = p.num()
	case "worn_training":
		p.expect("definition")
		r.Definition = p.str()
		p.expect("mode")
		r.Mode = p.str()
		p.expect("condition")
		r.Condition = p.str()
	}
	return r
}

func (p *parser) checkValue() checkValueNode {
	if p.isWord("stat") {
		p.take()
		return checkValueNode{Character: p.abilityOperand(false), Stat: p.str()}
	}
	v := p.num()
	return checkValueNode{Literal: &v}
}
func (p *parser) check() checkNode {
	n := checkNode{ID: p.str()}
	p.expect("die")
	n.Sides = p.num()
	if p.isWord("actor_roll") {
		t := p.take()
		n.ActorRoll = &t
	}
	p.expect("plus")
	n.Modifier = p.checkValue()
	p.expect("against")
	n.Threshold = p.checkValue()
	p.expect("{")
	if p.isWord("boost") {
		v := checkBoostNode{Start: p.take()}
		p.expect("condition")
		v.Condition = p.str()
		p.expect("resource")
		v.Resource = p.str()
		p.expect("cost")
		v.Cost = p.num()
		p.expect("die")
		v.Sides = p.num()
		p.expect("on_success")
		p.expect(";")
		n.Boost = &v
	}
	p.expect("success")
	n.Success = p.checkBranch()
	p.expect("failure")
	n.Failure = p.checkBranch()
	p.expect("}")
	return n
}
func (p *parser) checkBranch() []abilityStepNode {
	p.expect("{")
	// A written empty branch is an explicit empty sequence in the Alpha IR.
	out := []abilityStepNode{}
	for p.peek().Text != "}" && p.err == nil {
		out = append(out, p.abilityStep(p.abilityVerb(false)))
	}
	p.expect("}")
	return out
}

func (a *analyzer) unsignedLiteral(t token, positive bool) uint64 {
	v := a.number(t)
	if a.err == nil && (v < 0 || positive && v == 0) {
		expected := "nonnegative signed 64-bit literal"
		if positive {
			expected = "positive signed 64-bit literal"
		}
		a.err = diag(t.Pos, "E_AMOUNT", expected, t.Text, "literal is outside the permitted domain")
	}
	return uint64(v)
}
func (a *analyzer) adventure(n adventureNode) *core.Adventure {
	d := core.AdventureDefinition{Health: core.ResourceID(n.Health.Text)}
	for _, c := range n.Party {
		d.Party = append(d.Party, core.CharacterID(c.Text))
	}
	for _, l := range n.Links {
		d.Links = append(d.Links, core.LocationLink{From: core.LocationID(l.From.Text), To: core.LocationID(l.To.Text)})
	}
	for _, g := range n.Grants {
		v := core.AbilityGrant{Character: core.CharacterID(g.Character.Text), Abilities: []core.AbilityID{}}
		for _, id := range g.Abilities {
			v.Abilities = append(v.Abilities, core.AbilityID(id.Text))
		}
		d.Grants = append(d.Grants, v)
	}
	for _, e := range n.Encounters {
		v := core.EncounterDefinition{ID: core.EncounterID(e.ID.Text), Location: core.LocationID(e.Location.Text), Order: []core.Participant{}}
		for _, p := range e.Order {
			v.Order = append(v.Order, core.Participant{Character: core.CharacterID(p.Character.Text), Team: core.Team(p.Team.Text)})
		}
		d.Encounters = append(d.Encounters, v)
	}
	for _, o := range n.Objectives {
		d.Objectives = append(d.Objectives, core.ObjectiveDefinition{ID: core.ObjectiveID(o.ID.Text), Item: core.ItemID(o.Item.Text), Custodian: core.CharacterID(o.Custodian.Text), Victory: core.EncounterID(o.Victory.Text), Destination: core.LocationID(o.Destination.Text)})
	}
	if n.Initial != nil {
		d.InitialEncounter = core.EncounterID(n.Initial.Text)
	}
	if n.Seed != nil {
		for i, t := range n.Seed {
			d.Seed[i] = a.unsignedLiteral(t, false)
		}
	}
	return &core.Adventure{Definition: d}
}
func (a *analyzer) actionPolicy(n abilityNode) *core.ActionPolicy {
	if n.Action == nil {
		if len(n.Requirements) > 0 && a.err == nil {
			a.err = diag(n.Requirements[0].Verb.Pos, "E_ACTION", "action policy", "require", "requirements need an explicit action policy")
		}
		return nil
	}
	out := &core.ActionPolicy{Actor: a.operand(n.Action.Actor, core.CharacterArgument), Phase: core.ActionPhase(n.Action.Phase.Text), Requirements: []core.Requirement{}}
	if c := n.Action.TurnCost; c != nil {
		out.TurnCost = &core.TurnCost{Resource: core.ResourceID(c.Resource.Text), Amount: a.number(c.Amount), Finish: c.Finish.Text == "true"}
	}
	if n.Action.Finish != nil {
		finish := n.Action.Finish.Text == "true"
		out.Finish = &finish
	}
	for _, r := range n.Requirements {
		kind := core.RequirementKind(r.Kind.Text)
		subjectKind := core.CharacterArgument
		switch r.Kind.Text {
		case "alive":
			kind = core.AboveMinimum
		case "has":
			kind = core.HasCondition
		case "lacks":
			kind = core.LacksCondition
		case "party_owned", "carried_item":
			subjectKind = core.ItemArgument
		}
		v := core.Requirement{Kind: kind, Subject: a.operand(r.Subject, subjectKind), Resource: core.ResourceID(r.Resource.Text), Condition: core.ConditionTypeID(r.Condition.Text)}
		v.Location = core.LocationID(r.Location.Text)
		if kind == core.StatBetween {
			v.StatRange = &core.StatRange{Stat: core.StatID(r.Stat.Text), Minimum: a.number(r.Minimum), Maximum: a.number(r.Maximum)}
		}
		if kind == core.WithinRange {
			v.Distance = &core.DistanceLimit{Maximum: a.number(r.Distance), Unit: r.Unit.Text}
		}
		if kind == core.FreeSlot {
			v.Slot = &core.SlotUse{Slot: core.SlotID(r.Slot.Text), Amount: a.number(r.Count)}
		}
		if kind == core.WornTraining {
			v.Worn = &core.WornSelector{Definition: core.EquipmentDefinitionID(r.Definition.Text), Mode: core.EquipmentModeID(r.Mode.Text)}
		}
		if r.Other.Value.Pos.Line != 0 {
			x := a.operand(r.Other, core.CharacterArgument)
			v.Other = &x
		}
		out.Requirements = append(out.Requirements, v)
	}
	return out
}
func (a *analyzer) checkValue(n checkValueNode) core.CheckValue {
	if n.Literal != nil {
		v := a.number(*n.Literal)
		return core.CheckValue{Literal: &v}
	}
	return core.CheckValue{Stat: &core.StatOperand{Character: a.operand(n.Character, core.CharacterArgument), Stat: core.StatID(n.Stat.Text)}}
}
func (a *analyzer) checkTemplate(n checkNode) *core.CheckTemplate {
	out := &core.CheckTemplate{ID: n.ID.Text, Sides: a.unsignedLiteral(n.Sides, true), Modifier: a.checkValue(n.Modifier), Threshold: a.checkValue(n.Threshold), Success: []core.AbilityPrimitive{}, Failure: []core.AbilityPrimitive{}}
	out.ActorRoll = n.ActorRoll != nil
	if v := n.Boost; v != nil {
		out.Boost = &core.CheckBoostPolicy{Condition: core.ConditionTypeID(v.Condition.Text), Resource: core.ResourceID(v.Resource.Text), Cost: a.number(v.Cost), Sides: a.unsignedLiteral(v.Sides, true)}
	}
	for _, s := range n.Success {
		out.Success = append(out.Success, a.abilityStep(s).Primitive())
	}
	for _, s := range n.Failure {
		out.Failure = append(out.Failure, a.abilityStep(s).Primitive())
	}
	return out
}

func (n checkValueNode) operandToken() token {
	if n.Literal != nil {
		return *n.Literal
	}
	return n.Character.Value
}
func (n checkNode) operandToken(field string) token {
	switch field {
	case "boost":
		if n.Boost != nil {
			return n.Boost.Start
		}
	case "actor_roll":
		if n.ActorRoll != nil {
			return *n.ActorRoll
		}
	case "sides":
		return n.Sides
	case "modifier":
		return n.Modifier.operandToken()
	case "threshold":
		return n.Threshold.operandToken()
	case "modifier_stat":
		return n.Modifier.Stat
	case "threshold_stat":
		return n.Threshold.Stat
	}
	return n.ID
}
func (n abilityNode) actionOperandToken(field string) (token, bool) {
	if n.Action == nil {
		return token{}, false
	}
	switch field {
	case "actor":
		return n.Action.Actor.Value, true
	case "phase":
		return n.Action.Phase, true
	case "finish":
		if n.Action.Finish != nil {
			return *n.Action.Finish, true
		}
	case "turn_cost.resource":
		if n.Action.TurnCost != nil {
			return n.Action.TurnCost.Resource, true
		}
	case "turn_cost.amount":
		if n.Action.TurnCost != nil {
			return n.Action.TurnCost.Amount, true
		}
	case "action":
		return n.Action.Verb, true
	case "requirements":
		if len(n.Requirements) > 16 {
			return n.Requirements[16].Verb, true
		}
		return n.Action.Verb, true
	}
	parts := strings.Split(field, ".")
	if len(parts) != 3 || parts[0] != "requirement" {
		return token{}, false
	}
	i, err := strconv.Atoi(parts[1])
	if err != nil || i < 0 || i >= len(n.Requirements) {
		return token{}, false
	}
	r := n.Requirements[i]
	switch parts[2] {
	case "stat":
		return r.Stat, true
	case "minimum":
		return r.Minimum, true
	case "maximum":
		return r.Maximum, true
	case "location":
		return r.Location, true
	case "subject":
		return r.Subject.Value, true
	case "other":
		return r.Other.Value, true
	case "resource":
		return r.Resource, true
	case "condition":
		return r.Condition, true
	case "distance":
		return r.Distance, true
	case "unit":
		return r.Unit, true
	case "slot":
		return r.Slot, true
	case "count":
		return r.Count, true
	case "definition":
		return r.Definition, true
	case "mode":
		return r.Mode, true
	}
	return r.Kind, true
}

// These are immutable declaration/reference checks. Runtime remains the only
// evaluator of team eligibility, control, travel, claim and mutable conditions.
func (a *analyzer) adventureReferences(b core.Blueprint, n adventureNode) {
	unique := func(tokens []token, label string) {
		seen := map[string]bool{}
		for _, t := range tokens {
			if seen[t.Text] && a.err == nil {
				a.err = diag(t.Pos, "E_DUPLICATE", "unique "+label, t.Text, "duplicate "+label)
			}
			seen[t.Text] = true
		}
	}
	unique(n.Party, "party character")
	for _, c := range n.Party {
		a.checkRef(b, core.CharacterKind, c)
		a.checkAt(b.CheckResource(core.CharacterID(c.Text), core.ResourceID(n.Health.Text)), n.Health)
	}
	for _, l := range n.Links {
		a.checkRef(b, core.LocationKind, l.From)
		a.checkRef(b, core.LocationKind, l.To)
	}
	abilities := map[string]bool{}
	for _, d := range b.AbilityDefinitions {
		abilities[string(d.ID)] = true
	}
	var grantCharacters, encounterIDs, objectiveIDs []token
	for _, g := range n.Grants {
		grantCharacters = append(grantCharacters, g.Character)
		a.checkRef(b, core.CharacterKind, g.Character)
		a.checkAt(b.CheckResource(core.CharacterID(g.Character.Text), core.ResourceID(n.Health.Text)), n.Health)
		unique(g.Abilities, "granted ability")
		for _, id := range g.Abilities {
			if !abilities[id.Text] && a.err == nil {
				a.err = diag(id.Pos, "E_ABILITY", "declared Ability", id.Text, "unknown granted Ability")
			}
		}
	}
	unique(grantCharacters, "grant character")
	encounters := map[string]bool{}
	for _, e := range n.Encounters {
		encounterIDs = append(encounterIDs, e.ID)
		encounters[e.ID.Text] = true
		a.checkRef(b, core.LocationKind, e.Location)
		var participants []token
		for _, p := range e.Order {
			participants = append(participants, p.Character)
			a.checkRef(b, core.CharacterKind, p.Character)
			a.checkAt(b.CheckResource(core.CharacterID(p.Character.Text), core.ResourceID(n.Health.Text)), n.Health)
		}
		unique(participants, "encounter participant")
	}
	unique(encounterIDs, "encounter identity")
	checkEncounter := func(t token) {
		if !encounters[t.Text] && a.err == nil {
			a.err = diag(t.Pos, "E_ENCOUNTER", "declared encounter", t.Text, "unknown encounter reference")
		}
	}
	if n.Initial != nil {
		checkEncounter(*n.Initial)
	}
	for _, o := range n.Objectives {
		objectiveIDs = append(objectiveIDs, o.ID)
		a.checkRef(b, core.ItemKind, o.Item)
		a.checkRef(b, core.CharacterKind, o.Custodian)
		a.checkRef(b, core.LocationKind, o.Destination)
		checkEncounter(o.Victory)
	}
	unique(objectiveIDs, "objective identity")
}

func adventureOperand(f *core.Fault, n adventureNode) token {
	// Resolve core cross-record faults against explicit Alpha authoring sites.
	if f.Field == "health" {
		return n.Health
	}
	if f.Code == "E_ADVENTURE" {
		switch f.Field {
		case "grant":
			for _, g := range n.Grants {
				for _, id := range g.Abilities {
					if id.Text == f.Actual {
						return id
					}
				}
			}
		case "encounter":
			for _, e := range n.Encounters {
				if e.ID.Text == f.Actual {
					return e.ID
				}
			}
		case "participant", "team":
			party := map[string]bool{}
			for _, id := range n.Party {
				party[id.Text] = true
			}
			for _, e := range n.Encounters {
				for _, p := range e.Order {
					if p.Character.Text == f.Actual && party[p.Character.Text] != (p.Team.Text == "party") {
						return p.Team
					}
				}
			}
		case "objective":
			for _, o := range n.Objectives {
				if o.ID.Text == f.Actual {
					return o.ID
				}
			}
		case "link":
			seen := map[[2]string]bool{}
			for _, l := range n.Links {
				key := [2]string{l.From.Text, l.To.Text}
				if seen[key] || l.From.Text == l.To.Text {
					return l.To
				}
				seen[key] = true
			}
		}
	}
	for _, e := range n.Encounters {
		if e.ID.Text != f.Subject {
			continue
		}
		if f.Field == "location" {
			return e.Location
		}
		return e.ID
	}
	for _, o := range n.Objectives {
		if o.ID.Text != f.Subject {
			continue
		}
		switch f.Field {
		case "item":
			return o.Item
		case "custodian":
			return o.Custodian
		case "victory":
			return o.Victory
		case "destination":
			return o.Destination
		}
		return o.ID
	}
	if f.Field == "initial_encounter" && n.Initial != nil {
		return *n.Initial
	}
	// No matching operand is better than inventing a declaration location.
	return n.Start
}
