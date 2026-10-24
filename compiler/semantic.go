package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"strconv"
)

type analyzer struct {
	err      *Diagnostic
	fallback Position
}

func (a *analyzer) number(t token) int64 {
	v, err := strconv.ParseInt(t.Text, 10, 64)
	if err != nil && a.err == nil {
		a.err = diag(t.Pos, "E_INTEGER", "signed 64-bit integer", t.Text, "integer literal exceeds the numeric domain")
	}
	return v
}
func ref(n refNode) core.Ref { return core.Ref{Kind: core.EntityKind(n.Kind.Text), ID: n.ID.Text} }
func (a *analyzer) effect(n effectNode) core.Effect {
	kind := core.CharacterKind
	if n.Mode.Text == "worn" {
		kind = core.ItemKind
	}
	return core.Effect{ID: core.EffectID(n.ID.Text), Source: ref(n.Source), Mode: core.Activation(n.Mode.Text), Target: core.Ref{Kind: kind, ID: n.Target.Text}, Stat: core.StatID(n.Stat.Text), Amount: a.number(n.Amount)}
}
func (a *analyzer) relation(n relationNode) core.Relationship {
	return core.Relationship{From: ref(n.From), Kind: core.RelationKind(n.Kind.Text), To: ref(n.To), Strength: a.number(n.Strength)}
}
func (a *analyzer) check(err error) {
	if err == nil || a.err != nil {
		return
	}
	var f *core.Fault
	if errors.As(err, &f) {
		p := a.fallback
		message := f.Message
		if f.Subject != "" {
			message += "; entity " + strconv.Quote(f.Subject)
		}
		if f.Field != "" {
			message += "; field " + strconv.Quote(f.Field)
		}
		a.err = diag(p, f.Code, f.Expected, f.Actual, message)
	} else {
		a.err = diag(a.fallback, "E_SEMANTIC", "valid semantics", "invalid", err.Error())
	}
}

// Keep the particular use-site token until checking is complete. Declaration
// positions alone cannot identify a bad reference or distinguish omission from
// an explicitly authored empty optional reference.
func (a *analyzer) checkAt(err error, t token) {
	if err == nil || a.err != nil {
		return
	}
	a.check(err)
	a.err.Position = t.Pos
	a.err.Actual = strconv.Quote(t.Text) + " (" + a.err.Actual + ")"
}
func (a *analyzer) checkRef(b core.Blueprint, kind core.EntityKind, t token) {
	a.checkAt(b.CheckRef(core.Ref{Kind: kind, ID: t.Text}), t)
}
func (a *analyzer) effectRefs(b core.Blueprint, n effectNode) {
	a.checkRef(b, core.EntityKind(n.Source.Kind.Text), n.Source.ID)
	kind := core.CharacterKind
	if n.Mode.Text == "worn" {
		kind = core.ItemKind
	}
	a.checkRef(b, kind, n.Target)
}
func (a *analyzer) relationRefs(b core.Blueprint, n relationNode) {
	a.checkRef(b, core.EntityKind(n.From.Kind.Text), n.From.ID)
	a.checkRef(b, core.EntityKind(n.To.Kind.Text), n.To.ID)
}

func effectOperand(err error, n effectNode) token {
	var f *core.Fault
	if errors.As(err, &f) && f.Code == "E_ID" && f.Field == "effect" {
		return n.ID
	}
	return n.Stat
}
func newParser(source, text string, limit int) (*parser, []Diagnostic) {
	ts, d := lex(source, text, limit)
	if d != nil {
		return nil, []Diagnostic{*d}
	}
	return &parser{tokens: ts}, nil
}

// CompileWorld returns declaration IR only. It never publishes a world.
func CompileWorld(source, text string) (core.Blueprint, []Diagnostic) {
	p, ds := newParser(source, text, MaxWorldSourceBytes)
	if ds != nil {
		return core.Blueprint{}, ds
	}
	n := p.world()
	if p.err != nil {
		return core.Blueprint{}, []Diagnostic{*p.err}
	}
	a := analyzer{fallback: Position{source, 1, 1}}
	a.checkEntityDeclarations(n)
	b := core.Blueprint{Rules: core.Rules{Transfer: core.TransferPolicy(n.Transfer.Text), Wear: core.WearPolicy(n.Wear.Text)}}
	b.ItemDefinitions = a.itemDefinitions(n.ItemDefinitions)
	if n.Recovery != nil {
		b.Rules.RecoveryNumerator, b.Rules.RecoveryDenominator = a.number(n.Recovery.Numerator), a.number(n.Recovery.Denominator)
	}
	if n.Clock != nil {
		b.Clock = a.clock(*n.Clock)
	}
	if n.Dice != nil {
		b.Dice = a.dice(*n.Dice)
	}
	for _, v := range n.Locations {
		b.Locations = append(b.Locations, core.Location{ID: core.LocationID(v.ID.Text), Name: v.Name.Text})
	}
	for _, v := range n.Characters {
		c := a.character(v)
		b.Characters = append(b.Characters, c)
	}
	for _, v := range n.Items {
		b.Items = append(b.Items, a.item(v))
	}
	for _, v := range n.Effects {
		b.Effects = append(b.Effects, a.effect(v))
	}
	for _, v := range n.Relations {
		b.Relationships = append(b.Relationships, a.relation(v))
	}
	a.relationDefinitions(&b, n.RelationDefinitions)
	a.conditionDeclarations(&b, n)
	a.worldConditionChecks(&b, n)
	if n.Equipment != nil {
		b.Equipment = a.equipment(*n.Equipment)
		a.equipmentRefs(b, n)
	}
	if n.Battle != nil {
		b.Battle = a.battle(b, *n.Battle)
	}
	if n.Adventure != nil {
		b.Adventure = a.adventure(*n.Adventure)
	}
	if n.Map != nil {
		b.Map = a.worldMap(b, *n.Map)
	}
	for _, v := range n.Occupants {
		b.Occupants = append(b.Occupants, a.occupant(v))
	}
	for _, v := range n.Quests {
		b.Quests = append(b.Quests, a.quest(v))
	}
	b.Journal = a.journal(n)
	if n.Attacks != nil {
		b.Attacks = a.attacks(*n.Attacks)
	}
	a.damageTraits(&b, n)
	a.abilityDeclarations(&b, n.Abilities)
	if n.Battle != nil {
		a.battleActionReferences(b, *n.Battle)
	}
	if n.Adventure != nil {
		a.adventureReferences(b, *n.Adventure)
	}
	// One index for these per-entity checks keeps a large world linear.
	refs := core.NewRefs(&b)
	for _, v := range n.Characters {
		a.checkAt(refs.CheckRef(core.Ref{Kind: core.LocationKind, ID: v.Location.Text}), v.Location)
	}
	for _, v := range n.Occupants {
		a.checkAt(refs.CheckRef(core.Ref{Kind: core.LocationKind, ID: v.Location.Text}), v.Location)
	}
	for _, v := range n.Items {
		if v.Owner != nil {
			a.checkAt(refs.CheckRef(core.Ref{Kind: core.CharacterKind, ID: v.Owner.Text}), *v.Owner)
		}
		if v.Wearer != nil {
			a.checkAt(refs.CheckRef(core.Ref{Kind: core.CharacterKind, ID: v.Wearer.Text}), *v.Wearer)
		}
		if v.Custody != nil {
			a.checkAt(refs.CheckCustodyRef(ref(*v.Custody)), v.Custody.ID)
		}
	}
	for index, v := range n.Effects {
		a.effectRefs(b, v)
		err := b.CheckEffectStatic(b.Effects[index])
		a.checkAt(err, effectOperand(err, v))
	}
	for _, v := range n.Relations {
		a.relationRefs(b, v)
		err := b.CheckRelationship(a.relation(v))
		position := v.Kind
		if f, ok := err.(*core.Fault); ok && f.Code == "E_RELATION_BOUNDS" {
			position = v.Strength
		}
		a.checkAt(err, position)
	}
	a.checkWorld(core.Validate(b), n)
	if a.err != nil {
		return core.Blueprint{}, []Diagnostic{*a.err}
	}
	return b, nil
}

// CompileActions type-checks against a detached catalog. Expected revision is a
// host request concern; Execute revalidates this IR against authoritative state.
func CompileActions(source, text string, catalog core.Blueprint) ([]core.Operation, []Diagnostic) {
	return compileActions(source, text, catalog, false)
}

// CompileRetry compiles a request the world has already committed. The items,
// characters and occupants its actions create exist now, so they are left out
// of the catalog first: an exact retry lowers to the operations it lowered to
// the first time, and Execute answers it with the original receipt. Changed
// content is refused there as reuse, not here as a duplicate.
func CompileRetry(source, text string, catalog core.Blueprint) ([]core.Operation, []Diagnostic) {
	return compileActions(source, text, catalog, true)
}

func compileActions(source, text string, catalog core.Blueprint, retry bool) ([]core.Operation, []Diagnostic) {
	p, ds := newParser(source, text, MaxSourceBytes)
	if ds != nil {
		return nil, ds
	}
	nodes := p.actions()
	if p.err != nil {
		return nil, []Diagnostic{*p.err}
	}
	if len(nodes) > core.MaxOperations {
		return nil, []Diagnostic{*diag(nodes[core.MaxOperations].Verb.Pos, "E_LIMIT", "at most 128 operations", "too many operations", "transaction limit exceeded")}
	}
	if retry {
		catalog = withoutCreated(catalog, nodes)
	}
	a := analyzer{}
	ops := make([]core.Operation, 0, len(nodes))
	for _, n := range nodes {
		a.fallback = n.Verb.Pos
		if n.Split != nil {
			o, stub := a.stock(catalog, *n.Split)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			catalog.Items = append(append([]core.Item(nil), catalog.Items...), stub)
			continue
		}
		if n.Place != nil {
			v := n.Place
			o := core.Operation{Place: &core.PlaceItem{Item: core.ItemID(v.Item.Text), Handler: core.CharacterID(v.Handler.Text), From: ref(v.From), To: ref(v.To)}}
			err := catalog.CheckOperationStatic(o)
			position := v.Item
			var f *core.Fault
			if errors.As(err, &f) {
				switch f.Field {
				case "handler":
					position = v.Handler
				case "from":
					position = v.From.ID
				case "to":
					position = v.To.ID
				}
			}
			a.checkAt(err, position)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Item != nil {
			i := a.item(*n.Item)
			if n.Item.Owner != nil {
				a.checkRef(catalog, core.CharacterKind, *n.Item.Owner)
			}
			if n.Item.Custody != nil {
				a.checkAt(catalog.CheckCustodyRef(ref(*n.Item.Custody)), n.Item.Custody.ID)
			}
			err := catalog.CheckOperationStatic(core.Operation{CreateItem: &i})
			position := n.Item.ID
			var fault *core.Fault
			if errors.As(err, &fault) && fault.Code == "E_CHARGES" {
				position = chargesItemOperand(fault, *n.Item)
			} else if errors.As(err, &fault) {
				switch fault.Field {
				case "from":
					if n.Item.From != nil {
						position = *n.Item.From
					}
				case "custody":
					if n.Item.Custody != nil {
						position = n.Item.Custody.ID
					} else if n.Item.Container != nil {
						position = *n.Item.Container
					}
				case "quantity":
					if n.Item.Quantity != nil {
						position = *n.Item.Quantity
					}
				case "recoverable":
					if n.Item.Recoverable != nil {
						position = *n.Item.Recoverable
					}
				case "wearer":
					if n.Item.Wearer != nil {
						position = *n.Item.Wearer
					}
				case "definition":
					if n.Item.Definition != nil {
						position = *n.Item.Definition
					}
				case "mode":
					if n.Item.Mode != nil {
						position = *n.Item.Mode
					}
				case "description":
					if n.Item.Description != nil {
						position = *n.Item.Description
					}
				}
			}
			a.checkAt(err, position)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, core.Operation{CreateItem: &i})
			catalog.Items = append(append([]core.Item(nil), catalog.Items...), i)
			continue
		}
		if n.Character != nil {
			c := a.character(*n.Character)
			a.checkRef(catalog, core.LocationKind, n.Character.Location)
			err := catalog.CheckOperationStatic(core.Operation{CreateCharacter: &c})
			position := n.Character.ID
			var fault *core.Fault
			if errors.As(err, &fault) {
				one := worldNode{Characters: []characterNode{*n.Character}}
				if t, ok := worldOperand(fault, one); ok {
					position = t
				} else if t, ok := traitLawOperand(fault, one); ok {
					position = t
				}
				// An omitted recipe input is diagnosed at this creation's ID;
				// no nonexistent source operand is invented.
			}
			a.checkAt(err, position)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, core.Operation{CreateCharacter: &c})
			// Detach the backing slice even if the caller reserved capacity.
			// This catalog introduces only immutable type facts, not mechanics.
			catalog.Characters = append(append([]core.Character(nil), catalog.Characters...), c)
			continue
		}
		if n.Battle != nil {
			o := a.battleAction(catalog, *n.Battle)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Invocation != nil {
			o := a.invocation(catalog, *n.Invocation)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Condition != nil {
			o := a.conditionAction(catalog, n)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Attack != nil {
			o := a.attack(catalog, *n.Attack)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Save != nil {
			o := a.save(catalog, *n.Save)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.JournalAction != nil {
			o := a.journalAction(catalog, *n.JournalAction)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.QuestAction != nil {
			o := a.questAction(catalog, *n.QuestAction)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.Occupant != nil || n.OccupantAction != nil || n.MapEdit != nil {
			var o core.Operation
			switch {
			case n.Occupant != nil:
				o = a.createOccupant(catalog, *n.Occupant)
				catalog.Occupants = append(append([]core.Occupant(nil), catalog.Occupants...), *o.Occupant.Create)
			case n.OccupantAction != nil:
				o = a.occupantAction(catalog, *n.OccupantAction)
			default:
				o = a.mapEdit(catalog, *n.MapEdit)
			}
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		if n.StatCheck != nil {
			o := a.statCheck(catalog, *n.StatCheck)
			if a.err != nil {
				return nil, []Diagnostic{*a.err}
			}
			ops = append(ops, o)
			continue
		}
		o := core.Operation{}
		switch n.Verb.Text {
		case "travel":
			o.Travel = &core.PartyTravel{To: core.LocationID(n.To.Text)}
		case "advance":
			o.AdvanceTime = &core.AdvanceTime{Ticks: a.number(n.Amount)}
		case "add":
			o.Add = &core.AddItem{Item: core.ItemID(n.Subject.Text), Owner: core.CharacterID(n.From.Text), Amount: a.number(n.Amount)}
		case "expend":
			o.ExpendCharges = &core.ExpendCharges{Item: core.ItemID(n.Subject.Text), Owner: core.CharacterID(n.From.Text), Amount: a.number(n.Amount)}
		case "consume":
			o.Consume = &core.ConsumeItem{Item: core.ItemID(n.Subject.Text), Owner: core.CharacterID(n.From.Text), Amount: a.number(n.Amount), Recoverable: n.Recoverable != nil}
		case "recover":
			o.RecoverItem = &core.RecoverItem{Item: core.ItemID(n.Subject.Text), Owner: core.CharacterID(n.From.Text)}
			if n.Amount.Pos.Line != 0 {
				amount := a.number(n.Amount)
				o.RecoverItem.Amount = &amount
			}
		case "transfer":
			o.Transfer = &core.Transfer{Item: core.ItemID(n.Subject.Text), From: core.CharacterID(n.From.Text), To: core.CharacterID(n.To.Text)}
		case "equip":
			o.Equip = &core.Equip{Item: core.ItemID(n.Subject.Text), Wearer: core.CharacterID(n.To.Text)}
			if n.Mode != nil {
				o.Equip.Mode = core.EquipmentModeID(n.Mode.Text)
				a.requireEquipmentID(*n.Mode)
			}
		case "unequip":
			o.Unequip = &core.Unequip{Item: core.ItemID(n.Subject.Text)}
		case "restore":
			o.Restore = &core.RestoreResource{Character: core.CharacterID(n.Subject.Text), Resource: core.ResourceID(n.Resource.Text)}
		case "damage", "heal", "spend", "grant", "expand":
			v := &core.ResourceOp{Character: core.CharacterID(n.Subject.Text), Resource: core.ResourceID(n.Resource.Text), Amount: a.number(n.Amount)}
			if n.Verb.Text == "damage" {
				if n.DamageKind != nil {
					v.Kind = n.DamageKind.Text
					// An empty kind would read as no kind in IR.
					if v.Kind == "" {
						a.checkAt(&core.Fault{Code: "E_ATTACK", Subject: string(v.Character), Field: "kind", Expected: "valid damage kind", Actual: "", Message: "invalid kind"}, *n.DamageKind)
					}
				}
				o.Damage = v
				o.ConditionRolls = a.damageFaces(catalog, n.Subject, n.DamageFaces)
			} else if n.Verb.Text == "grant" {
				o.Grant = v
			} else if n.Verb.Text == "expand" {
				o.Expand = v
			} else if n.Verb.Text == "spend" {
				o.Spend = v
			} else {
				o.Heal = v
			}
		case "apply":
			e := a.effect(n.Effect)
			o.Apply = &e
		case "remove":
			o.Remove = &core.EffectOp{Effect: core.EffectID(n.Subject.Text)}
		case "expire":
			o.Expire = &core.EffectOp{Effect: core.EffectID(n.Subject.Text)}
		case "move":
			o.Move = &core.Move{Character: core.CharacterID(n.Subject.Text), To: core.LocationID(n.To.Text)}
		case "adjust":
			if n.Stat != nil {
				o.AdjustStat = &core.AdjustStat{Character: core.CharacterID(n.Subject.Text), Stat: core.StatID(n.Stat.Text), Amount: a.number(n.Amount)}
			} else {
				o.AdjustRelationship = &core.AdjustRelationship{From: ref(n.Relation.From), Kind: core.RelationKind(n.Relation.Kind.Text), To: ref(n.Relation.To), Amount: a.number(n.Amount)}
			}
		case "relate":
			r := a.relation(n.Relation)
			o.Relate = &r
		}
		// Reference checks carry their own operand positions, including when the
		// same spelling is used in two operands requiring different kinds.
		position := n.Subject
		switch n.Verb.Text {
		case "travel":
			a.checkRef(catalog, core.LocationKind, n.To)
			position = n.To
		case "advance":
			position = n.Amount
		case "consume", "add", "expend":
			a.checkRef(catalog, core.ItemKind, n.Subject)
			a.checkRef(catalog, core.CharacterKind, n.From)
			position = n.Amount
		case "recover":
			a.checkRef(catalog, core.ItemKind, n.Subject)
			a.checkRef(catalog, core.CharacterKind, n.From)
			position = n.Verb
		case "transfer":
			a.checkRef(catalog, core.ItemKind, n.Subject)
			a.checkRef(catalog, core.CharacterKind, n.From)
			a.checkRef(catalog, core.CharacterKind, n.To)
		case "equip":
			a.checkRef(catalog, core.ItemKind, n.Subject)
			a.checkRef(catalog, core.CharacterKind, n.To)
		case "unequip":
			a.checkRef(catalog, core.ItemKind, n.Subject)
		case "damage", "heal", "spend", "grant", "restore", "expand":
			a.checkRef(catalog, core.CharacterKind, n.Subject)
			position = n.Resource
		case "move":
			a.checkRef(catalog, core.CharacterKind, n.Subject)
			a.checkRef(catalog, core.LocationKind, n.To)
		case "apply":
			a.effectRefs(catalog, n.Effect)
			position = n.Effect.Stat
		case "relate", "adjust":
			if n.Stat != nil {
				a.checkRef(catalog, core.CharacterKind, n.Subject)
				position = *n.Stat
				break
			}
			a.relationRefs(catalog, n.Relation)
			position = n.Relation.Kind
		}
		err := catalog.CheckOperationStatic(o)
		var fault *core.Fault
		if errors.As(err, &fault) {
			if fault.Code == "E_EQUIPMENT" && fault.Field == "mode" && n.Mode != nil {
				position = *n.Mode
			}
			if fault.Code == "E_RELATION_BOUNDS" && n.Verb.Text == "relate" {
				position = n.Relation.Strength
			}
			if fault.Code == "E_AMOUNT" {
				position = n.Amount
			}
			if fault.Field == "kind" && n.DamageKind != nil {
				position = *n.DamageKind
			}
			if fault.Field == "recoverable" && n.Recoverable != nil {
				position = *n.Recoverable
			}
			if n.Verb.Text == "apply" {
				position = effectOperand(err, n.Effect)
			}
		}
		a.checkAt(err, position)
		if a.err != nil {
			return nil, []Diagnostic{*a.err}
		}
		ops = append(ops, o)
	}
	return ops, nil
}

func (a *analyzer) character(v characterNode) core.Character {
	c := core.Character{ID: core.CharacterID(v.ID.Text), Name: v.Name.Text, Location: core.LocationID(v.Location.Text), Stats: map[core.StatID]int64{}, Resources: map[core.ResourceID]core.Resource{}}
	for _, s := range v.Stats {
		id := core.StatID(s.ID.Text)
		if _, ok := c.Stats[id]; ok && a.err == nil {
			a.err = diag(s.ID.Pos, "E_DUPLICATE", "unique stat ID", s.ID.Text, "duplicate character stat")
		}
		c.Stats[id] = a.number(s.Value)
	}
	for _, r := range v.Resources {
		id := core.ResourceID(r.ID.Text)
		if _, ok := c.Resources[id]; ok && a.err == nil {
			a.err = diag(r.ID.Pos, "E_DUPLICATE", "unique resource ID", r.ID.Text, "duplicate character resource")
		}
		c.Resources[id] = core.Resource{Current: a.number(r.Current), Minimum: a.number(r.Minimum), Maximum: a.number(r.Maximum)}
	}
	seenPools, seenTargets := map[string]bool{}, map[string]bool{}
	for _, r := range v.Buffers {
		if (seenPools[r.Pool.Text] || seenTargets[r.Target.Text] || seenPools[r.Target.Text] || seenTargets[r.Pool.Text]) && a.err == nil {
			a.err = diag(r.Pool.Pos, "E_BUFFER", "distinct disjoint pool and target pairs", r.Pool.Text, "duplicate, chained or cyclic buffer relation")
		}
		seenPools[r.Pool.Text], seenTargets[r.Target.Text] = true, true
		c.Buffers = append(c.Buffers, core.ResourceBuffer{Pool: core.ResourceID(r.Pool.Text), Target: core.ResourceID(r.Target.Text)})
	}
	a.characterTraits(&c, v.Traits)
	return c
}

func (a *analyzer) item(v itemNode) core.Item {
	i := core.Item{ID: core.ItemID(v.ID.Text), Name: v.Name.Text}
	if v.From != nil {
		i.From = core.ItemDefinitionID(v.From.Text)
		if i.From == "" {
			a.checkAt(&core.Fault{Code: "E_CATALOG", Subject: v.ID.Text, Field: "from", Expected: "declared item definition", Actual: "", Message: "invalid item catalog from"}, *v.From)
		}
	}
	if v.Custody != nil {
		r := ref(*v.Custody)
		i.Custody = &r
	}
	if v.Container != nil {
		i.Container = v.Container.Text == "true"
	}
	if v.Quantity != nil {
		quantity := a.number(*v.Quantity)
		i.Quantity = &quantity
	}
	if v.Recoverable != nil {
		i.Recoverable = a.number(*v.Recoverable)
	}
	if v.Charges != nil {
		i.Charges = &core.ItemCharges{Current: a.number(*v.Charges)}
		if v.ChargesMax != nil {
			i.Charges.Max = a.number(*v.ChargesMax)
		}
		if v.ChargesSince != nil {
			since := a.tick(*v.ChargesSince)
			i.Charges.AsOf = &since
		}
		if v.ChargesSeed != nil {
			i.Charges.Seed = &[2]uint64{a.tick(v.ChargesSeed[0]), a.tick(v.ChargesSeed[1])}
		}
	}
	if v.Owner != nil {
		i.Owner = core.CharacterID(v.Owner.Text)
	}
	if v.Wearer != nil {
		i.Wearer = core.CharacterID(v.Wearer.Text)
	}
	if v.Definition != nil {
		i.Equipment = &core.EquipmentInstance{Definition: core.EquipmentDefinitionID(v.Definition.Text)}
	}
	if v.Mode != nil {
		if i.Equipment == nil && i.From != "" {
			i.Equipment = &core.EquipmentInstance{}
		}
		if i.Equipment == nil {
			a.checkAt(coreEquipmentError(v.ID.Text, "mode", "item definition required", v.Mode.Text), *v.Mode)
		} else {
			i.Equipment.Mode = core.EquipmentModeID(v.Mode.Text)
		}
	}
	if v.Description != nil {
		i.Description = v.Description.Text
	}
	return i
}
