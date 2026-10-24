package core

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sync"
)

// World is a handle. Copying it preserves one lock and one authority.
type World struct{ authority *authority }

type canonicalState struct {
	Revision Revision
	Blueprint
}

type authority struct {
	mu           sync.RWMutex
	state        canonicalState
	genesis      Blueprint
	archiveBytes uint64
	history      []Batch
	receipts     map[string]int
}

func NewWorld(input Blueprint) (*World, error) {
	b := cloneBlueprint(input)
	var catalogErr error
	b, catalogErr = resolveCatalog(b)
	if catalogErr != nil {
		return nil, catalogErr
	}
	if err := b.initializeBattle(); err != nil {
		return nil, err
	}
	if err := b.initializeAdventure(); err != nil {
		return nil, err
	}
	if err := b.initializeDice(); err != nil {
		return nil, err
	}
	if err := Validate(b); err != nil {
		return nil, err
	}
	if err := validateConditionRevisions(&b, 0); err != nil {
		return nil, err
	}
	normalize(&b)
	size, e := genesisArchiveBytes(b)
	if e != nil {
		return nil, e
	}
	return &World{authority: &authority{archiveBytes: size, genesis: cloneBlueprint(b), state: canonicalState{Blueprint: b}, history: []Batch{}, receipts: map[string]int{}}}, nil
}
func (w *World) Revision() Revision {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	return w.authority.state.Revision
}
func (w *World) Snapshot() Snapshot {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	b := cloneBlueprint(w.authority.state.Blueprint)
	return Snapshot{Revision: w.authority.state.Revision, Blueprint: b, ConditionEffects: derivedConditionEffects(&b)}
}
func (w *World) History() []Batch {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	out := make([]Batch, len(w.authority.history))
	for i, b := range w.authority.history {
		out[i] = cloneBatch(b)
	}
	return out
}

// ReceiptRevision reads successful-request membership without copying an event
// payload. Consumers of an earlier snapshot must also compare to that head.
func (w *World) ReceiptRevision(id string) (Revision, bool) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	i, ok := w.authority.receipts[id]
	if !ok {
		return 0, false
	}
	return w.authority.history[i].Revision, true
}

// ReceiptMetadata retains exact event identities without cloning their payloads.
// It is a detached read, never a replacement for a complete historical receipt.
type ReceiptMetadata struct {
	Revision Revision
	EventIDs []string
}

func (w *World) ReceiptMetadata(id string) (ReceiptMetadata, bool) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	i, ok := w.authority.receipts[id]
	if !ok {
		return ReceiptMetadata{}, false
	}
	b := w.authority.history[i]
	out := ReceiptMetadata{Revision: b.Revision, EventIDs: make([]string, len(b.Events))}
	for j, event := range b.Events {
		out.EventIDs[j] = event.ID
	}
	return out, true
}

// Receipt returns the complete detached historical outcome, if committed. It
// neither evaluates current preconditions nor acknowledges external delivery.
func (w *World) Receipt(id string) (Batch, bool) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	i, ok := w.authority.receipts[id]
	if !ok {
		return Batch{}, false
	}
	return cloneBatch(w.authority.history[i]), true
}
func (w *World) Explain(id CharacterID, stat StatID) (StatExplanation, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	return explain(&w.authority.state.Blueprint, id, stat)
}

// Execute is the only method that publishes live authoritative state. Callers
// must not concurrently mutate inputs during this call (ordinary Go ownership).
// All retained input/output values are detached before publication/return.
func (w *World) Execute(input Request) (Batch, error) {
	if len(input.Operations) == 0 || len(input.Operations) > MaxOperations {
		return Batch{}, fail("E_LIMIT", input.ID, "operations", "1..128 operations", fmt.Sprint(len(input.Operations)), "invalid request size")
	}
	r := cloneRequest(input)
	w.authority.mu.Lock()
	defer w.authority.mu.Unlock()
	if !validID(r.ID) {
		return Batch{}, fail("E_ID", r.ID, "request", "valid request ID", "invalid", "request ID is required")
	}
	if index, ok := w.authority.receipts[r.ID]; ok {
		b := w.authority.history[index]
		if reflect.DeepEqual(b.Request, r) {
			return cloneBatch(b), nil
		}
		return Batch{}, fail("E_REQUEST_REUSE", r.ID, "request", "identical retry", "different content", "request ID already committed with different content")
	}
	if r.ExpectedRevision != w.authority.state.Revision {
		return Batch{}, fail("E_STALE", r.ID, "revision", fmt.Sprint(w.authority.state.Revision), fmt.Sprint(r.ExpectedRevision), "request was planned against a different revision")
	}
	if w.authority.state.Revision == Revision(math.MaxUint64) {
		return Batch{}, fail("E_OVERFLOW", r.ID, "revision", "incrementable revision", "maximum", "world revision exhausted")
	}
	if err := w.authority.state.Blueprint.CheckRef(r.Actor); err != nil {
		return Batch{}, err
	}
	candidate, batch, err := evaluateRequest(w.authority.state.Blueprint, w.authority.state.Revision, r)
	if err != nil {
		return Batch{}, err
	}
	encodedSize, err := w.authority.admitBatch(batch)
	if err != nil {
		return Batch{}, err
	}
	// Prepare detached receipt before the single publication section.
	result := cloneBatch(batch)
	w.authority.state = canonicalState{batch.Revision, candidate}
	w.authority.receipts[r.ID] = len(w.authority.history)
	w.authority.history = append(w.authority.history, batch)
	w.authority.archiveBytes += encodedSize
	return result, nil
}
func noChange(subject string) error {
	return fail("E_NO_CHANGE", subject, "operation", "state transition", "unchanged", "operation would not change this fact")
}
func checkWornStats(b *Blueprint, i Item) error {
	if i.Wearer == "" {
		return nil
	}
	for _, e := range b.Effects {
		if e.Mode == WhileWorn && e.Target.ID == string(i.ID) {
			if err := b.CheckStat(i.Wearer, e.Stat); err != nil {
				return err
			}
		}
	}
	return nil
}
func resourceAfter(r Resource, amount int64, damage bool) Resource {
	v := big.NewInt(r.Current)
	if damage {
		v.Sub(v, big.NewInt(amount))
	} else {
		v.Add(v, big.NewInt(amount))
	}
	if v.Cmp(big.NewInt(r.Minimum)) < 0 {
		r.Current = r.Minimum
	} else if v.Cmp(big.NewInt(r.Maximum)) > 0 {
		r.Current = r.Maximum
	} else {
		r.Current = v.Int64()
	}
	return r
}
func applyOperation(b *Blueprint, o Operation, ctx applicationContext) ([]Event, error) {
	events := []Event{}
	switch {
	case o.RecoverAmmunition != nil:
		return recoverAmmunition(b, *o.RecoverAmmunition, ctx)
	case o.Place != nil:
		return placeItem(b, *o.Place)
	case o.Split != nil:
		return splitItem(b, *o.Split)
	case o.BeginBattle != nil:
		return beginBattle(b, *o.BeginBattle, ctx)
	case o.WeaponAttack != nil:
		return executeWeapon(b, *o.WeaponAttack, ctx)
	case o.EndBattleTurn != nil:
		return endBattleTurn(b, *o.EndBattleTurn, ctx)
	case o.CreateItem != nil:
		i := cloneItem(o.CreateItem)
		if err := b.prepareChargedCreation(i, ctx); err != nil {
			return nil, err
		}
		resolved, err := resolveCatalogItem(*i, b.itemDefinition(i.From), false)
		if err != nil {
			return nil, err
		}
		i = &resolved
		b.Items = append(b.Items, *i)
		e := Event{Kind: ItemCreated, Item: cloneItem(i)}
		if i.From != "" {
			e.ItemIntent = cloneItem(o.CreateItem)
		}
		return []Event{e}, nil
	case o.Add != nil:
		return addItem(b, *o.Add)
	case o.Consume != nil:
		return consumeItem(b, *o.Consume)
	case o.ExpendCharges != nil:
		return expendCharges(b, *o.ExpendCharges)
	case o.CreateCharacter != nil:
		c := cloneCharacter(o.CreateCharacter)
		b.Characters = append(b.Characters, *c)
		return []Event{{Kind: CharacterCreated, Character: cloneCharacter(c)}}, nil
	case o.AdvanceTime != nil:
		return advanceClock(b, *o.AdvanceTime, ctx)
	case o.StatCheck != nil:
		return resolveStatCheck(b, *o.StatCheck, ctx)
	case o.Travel != nil || o.Claim != nil:
		return applyAdventureOperation(b, o, ctx)
	case o.Occupant != nil:
		return applyOccupant(b, *o.Occupant)
	case o.MapEdit != nil:
		return applyMapEdit(b, *o.MapEdit)
	case o.Quest != nil:
		return applyQuest(b, *o.Quest)
	case o.Journal != nil:
		return recordJournal(b, *o.Journal, ctx.cause.Actor)
	case o.Attack != nil:
		return applyAttack(b, *o.Attack, ctx)
	case o.Save != nil:
		return applySave(b, *o.Save, ctx)
	case o.ApplyCondition != nil || o.RemoveCondition != nil:
		return applyConditionOperation(b, o, ctx)
	case o.Transfer != nil:
		v := o.Transfer
		i := item(b, v.Item)
		if itemQuantity(*i) == 0 {
			return nil, quantityFault(*i, "positive stock", "depleted item cannot transfer")
		}
		if i.Owner != v.From {
			return nil, fail("E_OWNER", string(i.ID), "owner", string(i.Owner), string(v.From), "stated owner is not the current owner")
		}
		if v.From == v.To {
			return nil, noChange(string(i.ID))
		}
		if b.Rules.Transfer == KeepsWorn && b.Rules.Wear == OwnerOnly && i.Wearer != "" && i.Wearer != v.To {
			return nil, fail("E_RULE", string(i.ID), "wearer", "recipient under owner_only", string(i.Wearer), "transfer would violate owner_only")
		}
		if b.Rules.Transfer == Unequips && i.Wearer != "" {
			es, err := unequipItem(b, i, "transfer rule", ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, es...)
		}
		events = append(events, Event{Kind: ItemTransferred, Ownership: &OwnershipChange{i.ID, i.Owner, v.To}})
		i.Owner = v.To
		if err := validateEquipmentLoadout(scan{b}); err != nil {
			return nil, err
		}
	case o.Equip != nil:
		v := o.Equip
		i := item(b, v.Item)
		if i.Container || (i.Custody != nil && !carriedBy(*i, v.Wearer)) {
			return nil, custodyFault(i.ID, "custody at proposed wearer", fmt.Sprint(i.Custody), "retrieve or hand over before equipping")
		}
		if itemQuantity(*i) != 1 {
			return nil, quantityFault(*i, "exactly one unit", "only a single unit may be equipped")
		}
		if i.Wearer != "" {
			return nil, fail("E_WORN", string(i.ID), "wearer", "unworn item", string(i.Wearer), "explicit unequip required before equip")
		}
		if b.Rules.Wear == OwnerOnly && i.Owner != v.Wearer {
			return nil, fail("E_RULE", string(i.ID), "wearer", string(i.Owner), string(v.Wearer), "owner_only forbids this wearer")
		}
		i.Wearer = v.Wearer
		if i.Equipment != nil {
			i.Equipment.Mode = v.Mode
			if v.Mode == "" {
				i.Equipment.Mode = b.equipmentDefinition(i.Equipment.Definition).DefaultMode
			}
		}
		if err := checkWornStats(b, *i); err != nil {
			return nil, err
		}
		if err := validateEquipmentLoadout(scan{b}); err != nil {
			return nil, err
		}
		change := &WearingChange{Item: i.ID, After: i.Wearer, Reason: "explicit", RequestedMode: v.Mode}
		var ticks int64
		if i.Equipment != nil {
			change.ModeAfter = i.Equipment.Mode
			if mode := b.wornMode(*i); mode != nil {
				ticks = mode.EquipTicks
			}
		}
		// The new loadout was checked above, but becomes active only after
		// donning time. Expiring Conditions still see the previous loadout.
		if ticks != 0 {
			i.Wearer = ""
			i.Equipment.Mode = ""
			es, err := equipmentTime(b, ticks, ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, es...)
			i.Wearer, i.Equipment.Mode = change.After, change.ModeAfter
			if err := checkWornStats(b, *i); err != nil {
				return nil, err
			}
			if err := validateEquipmentLoadout(scan{b}); err != nil {
				return nil, err
			}
		}
		events = append(events, Event{Kind: ItemEquipped, Wearing: change})
	case o.Unequip != nil:
		i := item(b, o.Unequip.Item)
		if i.Wearer == "" {
			return nil, noChange(string(i.ID))
		}
		es, err := unequipItem(b, i, "explicit", ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, es...)
		if err := validateEquipmentLoadout(scan{b}); err != nil {
			return nil, err
		}
	case o.Grant != nil:
		return grantResource(b, *o.Grant)
	case o.Expand != nil:
		event, err := expandResource(b, *o.Expand)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	case o.Restore != nil:
		v := o.Restore
		c := character(b, v.Character)
		before := c.Resources[v.Resource]
		after := before
		after.Current = before.Maximum
		if c.bufferPool(v.Resource) {
			after.Current = before.Minimum
		}
		c.Resources[v.Resource] = after
		life, err := battleResourceLife(b, v.Character, v.Resource, before, after, true, ctx)
		if err != nil {
			return nil, err
		}
		if c.bufferPool(v.Resource) {
			ended, err := endConditionsAtMinimum(b, v.Character, v.Resource, ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, ended...)
		}
		events = append(events, Event{Kind: ResourceRestored, Restoration: &RestorationChange{Character: v.Character, Resource: v.Resource, Before: before, After: after, Life: life}})
	case o.Spend != nil:
		v := o.Spend
		c := character(b, v.Character)
		if c.bufferPool(v.Resource) {
			return nil, bufferRule(v.Character, v.Resource, "buffer pools cannot be spent")
		}
		before := c.Resources[v.Resource]
		// Sufficiency is local to this ordered candidate step. Exact arithmetic
		// also covers floors whose distance from current exceeds int64.
		remaining := new(big.Int).Sub(big.NewInt(before.Current), big.NewInt(v.Amount))
		if remaining.Cmp(big.NewInt(before.Minimum)) < 0 {
			return nil, fail("E_INSUFFICIENT_RESOURCE", string(v.Character), string(v.Resource),
				fmt.Sprintf("current - requested >= minimum %d", before.Minimum),
				fmt.Sprintf("current=%d minimum=%d requested=%d", before.Current, before.Minimum, v.Amount),
				"the full resource amount cannot be paid")
		}
		after := before
		// Positive amount and sufficiency imply minimum <= remaining <= current.
		after.Current = remaining.Int64()
		c.Resources[v.Resource] = after
		life, err := battleResourceLife(b, v.Character, v.Resource, before, after, false, ctx)
		if err != nil {
			return nil, err
		}
		ended, err := endConditionsAtMinimum(b, v.Character, v.Resource, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, ended...)
		events = append(events, Event{Kind: ResourceSpent, Resource: &ResourceChange{Character: v.Character, Resource: v.Resource, Requested: v.Amount, Before: before, After: after, Life: life}})
	case o.Damage != nil || o.Heal != nil:
		v := o.Damage
		kind := DamageApplied
		if v == nil {
			v = o.Heal
			kind = HealingApplied
		}
		c := character(b, v.Character)
		before := c.Resources[v.Resource]
		if kind == HealingApplied && c.bufferPool(v.Resource) {
			return nil, bufferRule(v.Character, v.Resource, "buffer pools are granted, not healed")
		}
		amount := v.Amount
		var treated *DamageTreated
		if kind == DamageApplied && v.Kind != "" {
			var err error
			if treated, amount, err = treatKindedDamage(b, *v); err != nil {
				return nil, err
			}
			if amount == 0 {
				// Immunity: recorded, and nothing else happens.
				takeUnusedFaces(treated, ctx.conditionInput)
				events = append(events, Event{Kind: kind, Resource: &ResourceChange{Character: v.Character, Resource: v.Resource, Requested: 0, Treated: treated, Before: before, After: before}})
				break
			}
		}
		requested := amount
		var buffer *BufferChange
		if kind == DamageApplied {
			for _, relation := range c.Buffers {
				if relation.Target == v.Resource {
					pool := c.Resources[relation.Pool]
					absorbed := min(pool.Current, amount)
					remaining := pool
					remaining.Current -= absorbed
					buffer = &BufferChange{Resource: relation.Pool, Before: pool, After: remaining, Absorbed: absorbed}
					c.Resources[relation.Pool] = remaining
					amount -= absorbed
					ended, err := endConditionsAtMinimum(b, v.Character, relation.Pool, ctx)
					if err != nil {
						return nil, err
					}
					events = append(events, ended...)
					break
				}
			}
		}
		after := resourceAfter(before, amount, kind == DamageApplied)
		c.Resources[v.Resource] = after
		life, err := battleResourceLife(b, v.Character, v.Resource, before, after, kind == HealingApplied, ctx)
		if err != nil {
			return nil, err
		}
		ended, err := endConditionsAtMinimum(b, v.Character, v.Resource, ctx)
		if err != nil {
			return nil, err
		}
		events = append(events, ended...)
		if kind == DamageApplied {
			actual := new(big.Int).Sub(big.NewInt(before.Current), big.NewInt(after.Current)).Int64()
			if buffer != nil {
				actual += buffer.Absorbed
			}
			es, err := resolveDamageConditions(b, v.Character, v.Resource, actual, ctx)
			if err != nil {
				return nil, err
			}
			events = append(events, es...)
		}
		if treated != nil {
			takeUnusedFaces(treated, ctx.conditionInput)
		}
		events = append(events, Event{Kind: kind, Resource: &ResourceChange{Buffer: buffer, Character: v.Character, Resource: v.Resource, Requested: requested, Treated: treated, Before: before, After: after, Life: life}})
	case o.Apply != nil:
		e := *o.Apply
		if effectExists(b, e.ID) {
			return nil, fail("E_DUPLICATE", string(e.ID), "effect", "unused effect ID", string(e.ID), "effect ID is already active")
		}
		if err := validateEffect(scan{b}, e, true); err != nil {
			return nil, err
		}
		b.Effects = append(b.Effects, e)
		events = append(events, Event{Kind: EffectApplied, Effect: &EffectChange{After: &e}})
	case o.Remove != nil || o.Expire != nil:
		v := o.Remove
		kind := EffectRemoved
		if v == nil {
			v = o.Expire
			kind = EffectExpired
		}
		index := effectIndex(b, v.Effect)
		if index < 0 {
			return nil, fail("E_EFFECT", string(v.Effect), "effect", "existing effect", "missing", "effect does not exist")
		}
		before := b.Effects[index]
		b.Effects = append(b.Effects[:index], b.Effects[index+1:]...)
		events = append(events, Event{Kind: kind, Effect: &EffectChange{Before: &before}})
	case o.Move != nil:
		v := o.Move
		c := character(b, v.Character)
		if c.Location == v.To {
			return nil, noChange(string(c.ID))
		}
		events = append(events, Event{Kind: EntityMoved, Movement: &MovementChange{c.ID, c.Location, v.To}})
		c.Location = v.To
	case o.AdjustRelationship != nil:
		events = append(events, adjustRelationship(b, *o.AdjustRelationship))
	case o.AdjustStat != nil:
		return adjustStat(b, *o.AdjustStat)
	case o.RecoverItem != nil:
		return recoverItem(b, *o.RecoverItem)
	case o.Relate != nil:
		r := *o.Relate
		index := relationIndex(b, r)
		delta := &RelationshipChange{After: r}
		if index >= 0 {
			before := b.Relationships[index]
			if before.Strength == r.Strength {
				return nil, noChange(r.From.ID)
			}
			delta.Before = &before
			b.Relationships[index] = r
		} else {
			b.Relationships = append(b.Relationships, r)
		}
		events = append(events, Event{Kind: RelationshipChanged, Relationship: delta})
	}
	return events, nil
}
