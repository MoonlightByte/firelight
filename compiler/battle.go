package compiler

import (
	"fmt"
	"github.com/MoonlightByte/firelight/core"
)

type battleNode struct {
	Encounters                                                                                                 []encounterDefinitionNode
	Ranged                                                                                                     *rangedPolicyNode
	Start, Version, Health, Defense, Initiative, Root, Membership, Strength                                    token
	Sides, Miss, Critical, Multiplier, GridUnit, Square, ClockUnit, Ticks, SeedA, SeedB, Protected, Opposition token
	Rolls                                                                                                      []battleRollNode
	Damage                                                                                                     []battleDamageNode
	Armor                                                                                                      []battleArmorNode
	Actions                                                                                                    []token
	TurnResources                                                                                              []token
	Allowances                                                                                                 []battleAllowanceNode
	HitConditions                                                                                              []weaponHitConditionNode
	ConditionChecks                                                                                            []conditionCheckNode
	Routes                                                                                                     []travelRouteNode
	LocationSpans                                                                                              []locationSpanNode
}
type locationSpanNode struct{ Location, Maximum token }
type battleAllowanceNode struct {
	Resource, Weapon token
	Actions          []token
}
type weaponHitConditionNode struct{ ID, Definition, Mode, Attack, Requires, Condition token }
type travelRouteNode struct{ From, To, Ticks, Victory token }
type battleRollNode struct{ Condition, On, Direction token }
type battleDamageNode struct{ Condition, Kind, Treatment token }
type battleArmorNode struct{ Definition, Mode, Training, Stat token }
type conditionCheckNode struct{ Start, Condition, Resource, Stat, Sides, Minimum, Divisor, Maximum token }
type battleParticipantNode struct {
	Character, Side, Controller, X, Y, Tie token
	Initiative                             *token
}
type battleActionNode struct {
	Ammunition                                                      *token
	Recovery                                                        []ammunitionCreationNode
	HitConditionRule, HitConditionInstance                          *token
	Allowance                                                       *token
	Verb, ID, Location, Round, Cursor, Actor, Grant, Target, Finish token
	Participants                                                    []battleParticipantNode
	Human                                                           bool
	Attack, Damage                                                  []token
}

func (p *parser) battle(start token) battleNode {
	n := battleNode{Start: start, Version: p.str()}
	p.expect("{")
	p.expect("health")
	n.Health = p.str()
	p.expect(";")
	p.expect("defense")
	n.Defense = p.str()
	p.expect(";")
	p.expect("initiative")
	n.Initiative = p.str()
	p.expect(";")
	p.expect("party")
	n.Root = p.str()
	p.expect("relation")
	n.Membership = p.str()
	p.expect("strength")
	n.Strength = p.num()
	p.expect(";")
	p.expect("roll")
	p.expect("sides")
	n.Sides = p.num()
	p.expect("miss")
	n.Miss = p.num()
	p.expect("critical")
	n.Critical = p.num()
	p.expect("multiply")
	n.Multiplier = p.num()
	p.expect(";")
	p.expect("grid")
	n.GridUnit = p.str()
	p.expect("square")
	n.Square = p.num()
	p.expect(";")
	p.expect("clock")
	n.ClockUnit = p.str()
	p.expect("round")
	n.Ticks = p.num()
	p.expect(";")
	p.expect("seed")
	n.SeedA = p.num()
	n.SeedB = p.num()
	p.expect(";")
	p.expect("zero")
	p.expect("protected")
	n.Protected = p.choice("stable_down", "dead")
	p.expect("opposition")
	n.Opposition = p.choice("stable_down", "dead")
	p.expect(";")
	for p.peek().Text != "}" && p.err == nil {
		if p.isWord("encounter") {
			n.Encounters = append(n.Encounters, p.encounterDefinition())
			continue
		}
		if p.isWord("ranged") {
			v := p.rangedPolicy()
			if n.Ranged != nil {
				p.err = diag(v.Start.Pos, "E_DUPLICATE", "one ranged policy", "ranged", "duplicate ranged policy")
			} else {
				n.Ranged = v
			}
			continue
		}
		if p.isWord("hit_condition") {
			p.take()
			v := weaponHitConditionNode{ID: p.str()}
			p.expect("definition")
			v.Definition = p.str()
			p.expect("mode")
			v.Mode = p.str()
			p.expect("attack")
			v.Attack = p.str()
			p.expect("requires")
			v.Requires = p.str()
			p.expect("apply")
			v.Condition = p.str()
			p.expect(";")
			n.HitConditions = append(n.HitConditions, v)
			continue
		}
		if p.isWord("allowance") {
			p.take()
			v := battleAllowanceNode{Resource: p.str()}
			p.expect("weapon")
			v.Weapon = p.choice("true", "false")
			p.expect("{")
			for p.peek().Text != "}" && p.err == nil {
				p.expect("action")
				v.Actions = append(v.Actions, p.str())
				p.expect(";")
			}
			p.expect("}")
			p.expect(";")
			n.Allowances = append(n.Allowances, v)
			continue
		}
		if p.isWord("location_span") {
			p.take()
			v := locationSpanNode{Location: p.str()}
			p.expect("maximum")
			v.Maximum = p.num()
			n.LocationSpans = append(n.LocationSpans, v)
			p.expect(";")
			continue
		}
		if p.isWord("condition_check") {
			n.ConditionChecks = append(n.ConditionChecks, p.conditionCheck())
			continue
		}
		// Preserve historical expected-choice diagnostics for the old vocabulary.
		if p.isWord("turn_restore") {
			p.take()
			n.TurnResources = append(n.TurnResources, p.str())
			p.expect(";")
			continue
		}
		switch p.choice("roll_condition", "damage_condition", "untrained", "action", "route").Text {
		case "route":
			v := travelRouteNode{From: p.str()}
			p.expect("to")
			v.To = p.str()
			p.expect("ticks")
			v.Ticks = p.num()
			p.expect("victory")
			v.Victory = p.choice("required", "optional")
			n.Routes = append(n.Routes, v)
		case "action":
			n.Actions = append(n.Actions, p.str())
		case "roll_condition":
			v := battleRollNode{Condition: p.str()}
			p.expect("on")
			v.On = p.choice("attacker", "target")
			v.Direction = p.choice("advantage", "disadvantage")
			n.Rolls = append(n.Rolls, v)
		case "damage_condition":
			v := battleDamageNode{Condition: p.str()}
			p.expect("kind")
			v.Kind = p.str()
			v.Treatment = p.choice("resistance", "vulnerability", "immunity")
			n.Damage = append(n.Damage, v)
		case "untrained":
			v := battleArmorNode{Definition: p.str()}
			p.expect("mode")
			v.Mode = p.str()
			p.expect("requires")
			v.Training = p.str()
			p.expect("stat")
			v.Stat = p.str()
			n.Armor = append(n.Armor, v)
		}
		p.expect(";")
	}
	p.expect("}")
	return n
}
func (p *parser) battleAction() battleActionNode {
	n := battleActionNode{Verb: p.choice("begin", "attack", "end", "recover"), ID: p.str()}
	if n.Verb.Text == "recover" {
		p.expect("by")
		n.Actor = p.str()
		p.expect("{")
		for p.peek().Text != "}" && p.err == nil {
			p.expect("ammunition")
			origin := p.str()
			p.expect("as")
			n.Recovery = append(n.Recovery, ammunitionCreationNode{Origin: origin, Created: p.str()})
			p.expect(";")
		}
		p.expect("}")
		return n
	}
	if n.Verb.Text == "begin" {
		p.expect("at")
		n.Location = p.str()
		p.expect("{")
		for p.peek().Text != "}" && p.err == nil {
			p.expect("participant")
			v := battleParticipantNode{Character: p.str()}
			p.expect("side")
			v.Side = p.choice("protected", "opposition")
			p.expect("control")
			v.Controller = p.choice("human", "agent")
			p.expect("at")
			v.X = p.num()
			v.Y = p.num()
			p.expect("tie")
			v.Tie = p.num()
			if p.isWord("initiative") {
				p.take()
				t := p.num()
				v.Initiative = &t
			}
			p.expect(";")
			n.Participants = append(n.Participants, v)
		}
		p.expect("}")
		return n
	}
	p.expect("round")
	n.Round = p.num()
	p.expect("cursor")
	n.Cursor = p.num()
	p.expect("by")
	n.Actor = p.str()
	if n.Verb.Text == "attack" {
		p.expect("using")
		n.Grant = p.str()
		p.expect("target")
		n.Target = p.str()
		p.expect("finish")
		n.Finish = p.choice("true", "false")
		if p.isWord("ammunition") {
			p.take()
			id := p.str()
			n.Ammunition = &id
		}
		if p.isWord("allowance") {
			p.take()
			id := p.str()
			n.Allowance = &id
		}
		if p.isWord("condition") {
			p.take()
			rule := p.str()
			p.expect("as")
			instance := p.str()
			n.HitConditionRule, n.HitConditionInstance = &rule, &instance
		}
		if p.peek().Text == "{" {
			p.take()
			n.Human = true
			for p.peek().Text != "}" && p.err == nil {
				purpose := p.choice("attack", "damage")
				v := p.num()
				if purpose.Text == "attack" {
					n.Attack = append(n.Attack, v)
				} else {
					n.Damage = append(n.Damage, v)
				}
				p.expect(";")
			}
			p.expect("}")
		}
	}
	return n
}
func (a *analyzer) battleNumber(t token, lo, hi int64) int64 {
	v := a.number(t)
	if v < lo || v > hi {
		a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: fmt.Sprintf("%d..%d", lo, hi), Actual: t.Text, Message: "battle integer outside range"}, t)
	}
	return v
}
func (a *analyzer) battleCondition(b core.Blueprint, t token) {
	a.requireEquipmentID(t)
	for _, d := range b.ConditionDefinitions {
		if string(d.ID) == t.Text {
			return
		}
	}
	a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: "declared Condition type", Actual: t.Text, Message: "battle policy references missing Condition"}, t)
}
func (a *analyzer) battle(b core.Blueprint, n battleNode) *core.Battle {
	a.checkRef(b, core.CharacterKind, n.Root)
	for _, t := range []token{n.Health, n.Defense, n.Initiative, n.Membership, n.GridUnit} {
		a.requireEquipmentID(t)
	}
	if n.Version.Text != core.BattleVersion {
		a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: core.BattleVersion, Actual: n.Version.Text, Message: "unknown Battle rule version"}, n.Version)
	}
	if b.Clock == nil || b.Clock.Unit != n.ClockUnit.Text {
		a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: "declared matching Clock unit", Actual: n.ClockUnit.Text, Message: "battle requires shared Clock"}, n.ClockUnit)
	}
	r := core.BattleRules{Version: n.Version.Text, Health: core.ResourceID(n.Health.Text), Defense: core.StatID(n.Defense.Text), Initiative: core.StatID(n.Initiative.Text), PartyRoot: core.CharacterID(n.Root.Text), Membership: core.RelationKind(n.Membership.Text), MembershipStrength: a.number(n.Strength), DieSides: a.battleNumber(n.Sides, 2, 1000), GridUnit: n.GridUnit.Text, SquareSize: a.battleNumber(n.Square, 1, 1000000), ClockUnit: n.ClockUnit.Text, RoundTicks: a.battleNumber(n.Ticks, 1, 1<<63-1), Seed: [2]uint64{a.unsignedLiteral(n.SeedA, false), a.unsignedLiteral(n.SeedB, false)}, ProtectedOutcome: core.LifeOutcome(n.Protected.Text), OppositionOutcome: core.LifeOutcome(n.Opposition.Text)}
	r.MissFace = a.battleNumber(n.Miss, 1, r.DieSides)
	r.Ranged = a.rangedPolicy(b, n.Ranged)
	r.CriticalFace = a.battleNumber(n.Critical, 1, r.DieSides)
	r.CriticalMultiplier = a.battleNumber(n.Multiplier, 1, 8)
	seenChecks := map[[2]string]bool{}
	for i, v := range n.ConditionChecks {
		a.battleCondition(b, v.Condition)
		p := core.ConditionCheckPolicy{Condition: core.ConditionTypeID(v.Condition.Text), Resource: core.ResourceID(v.Resource.Text), Stat: core.StatID(v.Stat.Text), Sides: uint64(a.battleNumber(v.Sides, 2, 1000)), Minimum: a.battleNumber(v.Minimum, 1, 1<<63-1), Divisor: a.battleNumber(v.Divisor, 1, 1<<63-1), Maximum: a.battleNumber(v.Maximum, 1, 1<<63-1)}
		key := [2]string{v.Condition.Text, v.Resource.Text}
		if seenChecks[key] || i >= 16 {
			a.checkAt(coreEquipmentError("", "condition check", "at most 16 unique Condition/Resource policies", "duplicate or exceeded"), v.Condition)
		}
		if p.Maximum < p.Minimum {
			a.checkAt(coreEquipmentError("", "maximum", "at least threshold minimum", v.Maximum.Text), v.Maximum)
		}
		seenChecks[key] = true
		r.ConditionChecks = append(r.ConditionChecks, p)
	}
	seenResources := map[string]bool{}
	for i, id := range n.TurnResources {
		declared := false
		for _, c := range b.Characters {
			_, ok := c.Resources[core.ResourceID(id.Text)]
			declared = declared || ok
		}
		if i >= 8 || seenResources[id.Text] || !declared || id.Text == n.Health.Text {
			a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: "at most 8 unique existing non-health turn Resources", Actual: id.Text, Message: "invalid turn Resource declaration"}, id)
		}
		seenResources[id.Text] = true
		r.TurnResources = append(r.TurnResources, core.ResourceID(id.Text))
	}
	seenRoutes := map[[2]string]bool{}
	seenHitConditions := map[string]bool{}
	for i, v := range n.HitConditions {
		a.requireEquipmentID(v.ID)
		if i >= 32 || seenHitConditions[v.ID.Text] {
			a.checkAt(coreEquipmentError("", "hit condition", "at most 32 unique policies", v.ID.Text), v.ID)
		}
		seenHitConditions[v.ID.Text] = true
		rule := core.WeaponHitCondition{ID: v.ID.Text, Definition: core.EquipmentDefinitionID(v.Definition.Text), Mode: core.EquipmentModeID(v.Mode.Text), Attack: v.Attack.Text, Requires: core.ConditionTypeID(v.Requires.Text), Condition: core.ConditionTypeID(v.Condition.Text)}
		err := b.CheckWeaponHitCondition(rule)
		position := v.ID
		if fault, ok := err.(*core.Fault); ok {
			switch fault.Field {
			case "definition":
				position = v.Definition
			case "mode":
				position = v.Mode
			case "attack":
				position = v.Attack
			case "requires":
				position = v.Requires
			case "condition":
				position = v.Condition
			}
		}
		a.checkAt(err, position)
		r.HitConditions = append(r.HitConditions, rule)
	}
	for i, v := range n.Allowances {
		a.requireEquipmentID(v.Resource)
		declared := false
		for _, c := range b.Characters {
			if resource, ok := c.Resources[core.ResourceID(v.Resource.Text)]; ok {
				declared = true
				if resource.Minimum != 0 {
					a.checkAt(coreEquipmentError("", "allowance", "Resource minimum0", v.Resource.Text), v.Resource)
				}
			}
		}
		if i >= 8 || seenResources[v.Resource.Text] || !declared || v.Resource.Text == n.Health.Text || v.Weapon.Text == "false" && len(v.Actions) == 0 {
			a.checkAt(coreEquipmentError("", "allowance", "at most 8 unique existing non-health Resources admitting actions, without turn_restore", v.Resource.Text), v.Resource)
		}
		seenResources[v.Resource.Text] = true
		allowance := core.BattleAllowance{Resource: core.ResourceID(v.Resource.Text), Weapon: v.Weapon.Text == "true"}
		for _, id := range v.Actions {
			allowance.Actions = append(allowance.Actions, core.AbilityID(id.Text))
		}
		r.Allowances = append(r.Allowances, allowance)
	}
	seenSpans := map[string]bool{}
	for i, v := range n.LocationSpans {
		a.checkRef(b, core.LocationKind, v.Location)
		if seenSpans[v.Location.Text] || i >= 128 {
			a.checkAt(coreEquipmentError("", "location span", "at most 128 unique Locations", "duplicate or exceeded"), v.Location)
		}
		seenSpans[v.Location.Text] = true
		r.LocationSpans = append(r.LocationSpans, core.LocationSpan{Location: core.LocationID(v.Location.Text), Maximum: a.battleNumber(v.Maximum, 1, 1<<63-1)})
	}
	for i, v := range n.Routes {
		a.checkRef(b, core.LocationKind, v.From)
		a.checkRef(b, core.LocationKind, v.To)
		pair := [2]string{v.From.Text, v.To.Text}
		if v.From.Text == v.To.Text {
			a.checkAt(coreEquipmentError("", "route", "different destination", v.To.Text), v.To)
		}
		if seenRoutes[pair] {
			a.checkAt(coreEquipmentError("", "route", "unique directed route", "duplicate"), v.From)
		}
		if i == 64 {
			a.checkAt(coreEquipmentError("", "routes", "at most 64 routes", "exceeded"), v.From)
		}
		seenRoutes[pair] = true
		r.Routes = append(r.Routes, core.TravelRoute{From: core.LocationID(v.From.Text), To: core.LocationID(v.To.Text), Ticks: a.battleNumber(v.Ticks, 1, 1<<63-1), RequiresVictory: v.Victory.Text == "required"})
	}
	for _, id := range n.Actions {
		r.Actions = append(r.Actions, core.AbilityID(id.Text))
	}
	if r.MissFace == r.CriticalFace {
		a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: "different miss and critical faces", Actual: n.Critical.Text, Message: "conflicting natural-face rules"}, n.Critical)
	}
	seenRoll := map[core.BattleRollPolicy]bool{}
	for _, v := range n.Rolls {
		a.battleCondition(b, v.Condition)
		p := core.BattleRollPolicy{Condition: core.ConditionTypeID(v.Condition.Text), On: v.On.Text, Direction: v.Direction.Text}
		if seenRoll[p] {
			a.checkAt(coreEquipmentError("", "policy", "unique roll policy", "duplicate"), v.Condition)
		}
		seenRoll[p] = true
		r.RollPolicies = append(r.RollPolicies, p)
	}
	seenDamage := map[core.BattleDamagePolicy]bool{}
	for _, v := range n.Damage {
		a.battleCondition(b, v.Condition)
		a.requireEquipmentID(v.Kind)
		p := core.BattleDamagePolicy{Condition: core.ConditionTypeID(v.Condition.Text), Kind: v.Kind.Text, Treatment: v.Treatment.Text}
		if seenDamage[p] {
			a.checkAt(coreEquipmentError("", "policy", "unique damage policy", "duplicate"), v.Condition)
		}
		seenDamage[p] = true
		r.DamagePolicies = append(r.DamagePolicies, p)
	}
	seenArmor := map[core.BattleArmorPolicy]bool{}
	for _, v := range n.Armor {
		a.battleCondition(b, v.Training)
		a.requireEquipmentID(v.Stat)
		definition, mode := false, false
		if b.Equipment != nil {
			for _, d := range b.Equipment.Definitions {
				if string(d.ID) == v.Definition.Text {
					definition = true
					for _, m := range d.Modes {
						mode = mode || string(m.ID) == v.Mode.Text
					}
				}
			}
		}
		if !definition {
			a.checkAt(coreEquipmentError("", "definition", "declared definition", v.Definition.Text), v.Definition)
		} else if !mode {
			a.checkAt(coreEquipmentError("", "mode", "declared mode", v.Mode.Text), v.Mode)
		}
		p := core.BattleArmorPolicy{Definition: core.EquipmentDefinitionID(v.Definition.Text), Mode: core.EquipmentModeID(v.Mode.Text), Training: core.ConditionTypeID(v.Training.Text), AttackStat: core.StatID(v.Stat.Text)}
		if seenArmor[p] {
			a.checkAt(coreEquipmentError("", "policy", "unique armor policy", "duplicate"), v.Definition)
		}
		seenArmor[p] = true
		r.ArmorPolicies = append(r.ArmorPolicies, p)
	}
	if len(n.Rolls) > 16 {
		a.checkAt(coreEquipmentError("", "capacity", "at most 16 roll policies", "exceeded"), n.Rolls[16].Condition)
	}
	if len(n.Damage) > 16 {
		a.checkAt(coreEquipmentError("", "capacity", "at most 16 damage policies", "exceeded"), n.Damage[16].Condition)
	}
	if len(n.Armor) > 16 {
		a.checkAt(coreEquipmentError("", "capacity", "at most 16 armor policies", "exceeded"), n.Armor[16].Definition)
	}
	declarations := b
	declarations.Battle = &core.Battle{Rules: r}
	r.EncounterDefinitions = a.encounterDefinitions(declarations, n.Encounters)
	return &core.Battle{Rules: r}
}

// Battle is lowered before Ability declarations. Resolve this namespace only
// after the complete declaration table exists, retaining the actual use site.
func (a *analyzer) battleActionReferences(b core.Blueprint, n battleNode) {
	seen := map[string]bool{}
	for i, id := range n.Actions {
		if i >= 128 {
			a.checkAt(coreEquipmentError("", "actions", "at most 128 actions", "exceeded"), id)
		}
		if seen[id.Text] {
			a.checkAt(&core.Fault{Code: "E_DUPLICATE", Expected: "unique Battle action", Actual: id.Text, Message: "duplicate Battle action"}, id)
		}
		seen[id.Text] = true
		found := false
		for _, d := range b.AbilityDefinitions {
			if string(d.ID) == id.Text {
				found = d.Action != nil
				break
			}
		}
		if !found {
			a.checkAt(&core.Fault{Code: "E_ABILITY", Expected: "declared opted-in Ability", Actual: id.Text, Message: "Battle action requires an authored Action policy"}, id)
		}
	}
	for _, allowance := range n.Allowances {
		used := map[string]bool{}
		for i, id := range allowance.Actions {
			eligible := false
			for _, d := range b.AbilityDefinitions {
				if string(d.ID) == id.Text {
					eligible = seen[id.Text] && d.Action != nil && d.Action.TurnCost == nil && d.Action.Phase != core.Exploration
				}
			}
			if i >= 128 || used[id.Text] || !eligible {
				a.checkAt(&core.Fault{Code: "E_BATTLE", Expected: "unique registered ordinary Battle action (at most 128)", Actual: id.Text, Message: "invalid allowance action"}, id)
			}
			used[id.Text] = true
		}
	}
}
func (a *analyzer) battleAction(b core.Blueprint, n battleActionNode) core.Operation {
	o := core.Operation{}
	a.requireEquipmentID(n.ID)
	if n.Verb.Text == "recover" {
		o.RecoverAmmunition = a.recoverAmmunition(b, n)
	} else if n.Verb.Text == "begin" {
		v := &core.BeginBattle{ID: core.EncounterID(n.ID.Text), Location: core.LocationID(n.Location.Text)}
		a.checkRef(b, core.LocationKind, n.Location)
		ids := map[string]bool{}
		ranks := map[int]bool{}
		positions := map[core.BattlePosition]bool{}
		for _, p := range n.Participants {
			a.checkRef(b, core.CharacterKind, p.Character)
			x := core.BattleAdmission{Character: core.CharacterID(p.Character.Text), Side: core.BattleSide(p.Side.Text), Controller: core.BattleController(p.Controller.Text), Position: core.BattlePosition{X: a.battleNumber(p.X, -1024, 1024), Y: a.battleNumber(p.Y, -1024, 1024)}, TieRank: int(a.battleNumber(p.Tie, 0, int64(len(n.Participants)-1)))}
			if p.Initiative != nil {
				limit := int64(1000)
				if b.Battle != nil {
					limit = b.Battle.Rules.DieSides
				}
				value := a.battleNumber(*p.Initiative, 1, limit)
				x.Initiative = &value
			}
			if ids[p.Character.Text] {
				a.checkAt(coreEquipmentError("", "participant", "unique Character", "duplicate"), p.Character)
			}
			ids[p.Character.Text] = true
			if ranks[x.TieRank] {
				a.checkAt(coreEquipmentError("", "tie", "unique rank", "duplicate"), p.Tie)
			}
			ranks[x.TieRank] = true
			if positions[x.Position] {
				a.checkAt(coreEquipmentError("", "position", "unique occupied square", "duplicate"), p.X)
			}
			positions[x.Position] = true
			v.Participants = append(v.Participants, x)
		}
		o.BeginBattle = v
	} else {
		a.checkRef(b, core.CharacterKind, n.Actor)
		w := core.BattleWindow{Encounter: core.EncounterID(n.ID.Text), Round: uint64(a.battleNumber(n.Round, 1, 1<<63-1)), Cursor: int(a.battleNumber(n.Cursor, 0, 15)), Actor: core.CharacterID(n.Actor.Text)}
		if n.Verb.Text == "end" {
			o.EndBattleTurn = &core.EndBattleTurn{BattleWindow: w}
		} else {
			a.checkRef(b, core.CharacterKind, n.Target)
			a.requireEquipmentID(n.Grant)
			v := &core.WeaponAttack{BattleWindow: w, Grant: core.EquipmentGrantID(n.Grant.Text), Target: core.CharacterID(n.Target.Text), FinishTurn: n.Finish.Text == "true"}
			if n.Ammunition != nil {
				a.checkRef(b, core.ItemKind, *n.Ammunition)
				v.Ammunition = core.ItemID(n.Ammunition.Text)
			}
			if n.HitConditionRule != nil {
				a.requireEquipmentID(*n.HitConditionRule)
				a.requireEquipmentID(*n.HitConditionInstance)
				v.HitCondition = &core.WeaponConditionChoice{Rule: n.HitConditionRule.Text, Instance: core.ConditionInstanceID(n.HitConditionInstance.Text)}
			}
			if n.Allowance != nil {
				a.requireEquipmentID(*n.Allowance)
				v.Allowance = core.ResourceID(n.Allowance.Text)
			}
			if n.Human {
				v.Human = &core.HumanAttackRolls{}
				limit := int64(1000)
				if b.Battle != nil {
					limit = b.Battle.Rules.DieSides
				}
				for _, t := range n.Attack {
					v.Human.Attack = append(v.Human.Attack, a.battleNumber(t, 1, limit))
				}
				for _, t := range n.Damage {
					v.Human.Damage = append(v.Human.Damage, a.battleNumber(t, 1, 1000))
				}
				if len(n.Attack) > 2 {
					a.checkAt(coreEquipmentError("", "dice", "at most 2 attack faces", "exceeded"), n.Attack[2])
				}
				if len(n.Damage) > 200 {
					a.checkAt(coreEquipmentError("", "dice", "at most 200 damage faces", "exceeded"), n.Damage[200])
				}
			}
			o.WeaponAttack = v
		}
	}
	err := b.CheckOperationStatic(o)
	position := n.ID
	if f, ok := err.(*core.Fault); ok && f.Field == "allowance" && n.Allowance != nil {
		position = *n.Allowance
	}
	if f, ok := err.(*core.Fault); ok && n.HitConditionRule != nil {
		if f.Field == "hit_condition.rule" {
			position = *n.HitConditionRule
		}
		if f.Field == "condition_instance" {
			position = *n.HitConditionInstance
		}
	}
	a.checkAt(err, position)
	return o
}
