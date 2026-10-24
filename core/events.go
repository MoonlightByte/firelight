package core

import (
	"fmt"
	"reflect"
)

type EventKind string

const (
	AmmunitionRecovered    EventKind = "AmmunitionRecovered"
	ResourceExpanded       EventKind = "ResourceExpanded"
	ItemSplit              EventKind = "ItemSplit"
	ItemPlaced             EventKind = "ItemPlaced"
	ConditionCheckResolved EventKind = "ConditionCheckResolved"
	ItemCreated            EventKind = "ItemCreated"
	ItemAdded              EventKind = "ItemAdded"
	ItemConsumed           EventKind = "ItemConsumed"
	CharacterCreated       EventKind = "CharacterCreated"
	AbilityInvoked         EventKind = "AbilityInvoked"
	ConditionApplied       EventKind = "ConditionApplied"
	ConditionRemoved       EventKind = "ConditionRemoved"
	ItemTransferred        EventKind = "ItemTransferred"
	ItemEquipped           EventKind = "ItemEquipped"
	ItemUnequipped         EventKind = "ItemUnequipped"
	DamageApplied          EventKind = "DamageApplied"
	HealingApplied         EventKind = "HealingApplied"
	ResourceGranted        EventKind = "ResourceGranted"
	ResourceSpent          EventKind = "ResourceSpent"
	ResourceRestored       EventKind = "ResourceRestored"
	EffectApplied          EventKind = "EffectApplied"
	EffectRemoved          EventKind = "EffectRemoved"
	EffectExpired          EventKind = "EffectExpired"
	EntityMoved            EventKind = "EntityMoved"
	StatAdjusted           EventKind = "StatAdjusted"
	ItemRecovered          EventKind = "ItemRecovered"
	ChargesExpended        EventKind = "ChargesExpended"
	RelationshipDecayed    EventKind = "RelationshipDecayed"
	RelationshipAdjusted   EventKind = "RelationshipAdjusted"
	RelationshipChanged    EventKind = "RelationshipChanged"
)

type ConsumptionChange struct {
	Item        ItemID      `json:"item"`
	Owner       CharacterID `json:"owner"`
	Requested   int64       `json:"requested"`
	Before      *int64      `json:"before"`
	After       int64       `json:"after"`
	Recoverable bool        `json:"recoverable,omitempty"`
}
type OwnershipChange struct {
	Item   ItemID      `json:"item"`
	Before CharacterID `json:"before"`
	After  CharacterID `json:"after"`
}
type WearingChange struct {
	ModeBefore    EquipmentModeID `json:"mode_before,omitempty"`
	ModeAfter     EquipmentModeID `json:"mode_after,omitempty"`
	RequestedMode EquipmentModeID `json:"requested_mode,omitempty"`
	Item          ItemID          `json:"item"`
	Before        CharacterID     `json:"before"`
	After         CharacterID     `json:"after"`
	Reason        string          `json:"reason"`
}
type ResourceChange struct {
	Buffer    *BufferChange  `json:"buffer,omitempty"`
	Life      *LifeChange    `json:"life,omitempty"`
	Character CharacterID    `json:"character"`
	Resource  ResourceID     `json:"resource"`
	Requested int64          `json:"requested"`
	Treated   *DamageTreated `json:"treated,omitempty"`
	Before    Resource       `json:"before"`
	After     Resource       `json:"after"`
}
type RestorationChange struct {
	Life      *LifeChange `json:"life,omitempty"`
	Character CharacterID `json:"character"`
	Resource  ResourceID  `json:"resource"`
	Before    Resource    `json:"before"`
	After     Resource    `json:"after"`
}
type EffectChange struct {
	Before *Effect `json:"before"`
	After  *Effect `json:"after"`
}
type MovementChange struct {
	Character CharacterID `json:"character"`
	Before    LocationID  `json:"before"`
	After     LocationID  `json:"after"`
}
type RelationshipChange struct {
	Decay      *RelationDecayChange `json:"decay,omitempty"`
	Adjustment *int64               `json:"adjustment,omitempty"`
	Before     *Relationship        `json:"before"`
	After      Relationship         `json:"after"`
}
type Event struct {
	AmmunitionRecovery *AmmunitionRecovery   `json:"ammunition_recovery,omitempty"`
	Capacity           *CapacityChange       `json:"capacity,omitempty"`
	StockSplit         *StockSplit           `json:"stock_split,omitempty"`
	Custody            *CustodyChange        `json:"custody,omitempty"`
	ConditionCheck     *ConditionCheckResult `json:"condition_check,omitempty"`
	Restoration        *RestorationChange    `json:"restoration,omitempty"`
	ResourceRoll       *ResourceRollResult   `json:"resource_roll,omitempty"`
	BattleAction       *BattleActionResult   `json:"battle_action,omitempty"`
	BattleStarted      *BattleStartedResult  `json:"battle_started,omitempty"`
	WeaponAttack       *WeaponAttackResult   `json:"weapon_attack,omitempty"`
	BattleTurn         *BattleTurnResult     `json:"battle_turn,omitempty"`
	Item               *Item                 `json:"item,omitempty"`
	ItemIntent         *Item                 `json:"item_intent,omitempty"`
	Addition           *ConsumptionChange    `json:"addition,omitempty"`
	Consumption        *ConsumptionChange    `json:"consumption,omitempty"`
	ItemRecovery       *ItemRecovery         `json:"item_recovery,omitempty"`
	Charges            *ChargeExpenditure    `json:"charges,omitempty"`
	Character          *Character            `json:"character,omitempty"`
	Time               *TimeChange           `json:"time,omitempty"`
	StatCheck          *StatCheckResult      `json:"stat_check,omitempty"`
	StatAdjustment     *StatAdjustment       `json:"stat_adjustment,omitempty"`
	Check              *CheckResult          `json:"check,omitempty"`
	Encounter          *EncounterChange      `json:"encounter,omitempty"`
	Objective          *ObjectiveChange      `json:"objective,omitempty"`
	Travel             *PartyTravel          `json:"travel,omitempty"`
	Trip               *Trip                 `json:"trip,omitempty"`
	Occupant           *OccupantChange       `json:"occupant,omitempty"`
	MapEdit            *MapEdit              `json:"map_edit,omitempty"`
	Quest              *QuestChange          `json:"quest,omitempty"`
	Journal            *JournalChange        `json:"journal,omitempty"`
	Attack             *AttackResult         `json:"attack,omitempty"`
	Save               *SaveResult           `json:"save,omitempty"`
	Claim              *ClaimObjective       `json:"claim,omitempty"`
	AbilityOrigin      *AbilityOrigin        `json:"ability_origin,omitempty"`
	Ability            *InvokeAbility        `json:"ability_invoked,omitempty"`
	Condition          *ConditionChange      `json:"condition,omitempty"`
	ID                 string                `json:"id"`
	Revision           Revision              `json:"revision"`
	RequestID          string                `json:"request_id"`
	Actor              Ref                   `json:"actor"`
	Operation          int                   `json:"operation"`
	Kind               EventKind             `json:"kind"`
	Ownership          *OwnershipChange      `json:"ownership,omitempty"`
	Wearing            *WearingChange        `json:"wearing,omitempty"`
	Resource           *ResourceChange       `json:"resource,omitempty"`
	Effect             *EffectChange         `json:"effect,omitempty"`
	Movement           *MovementChange       `json:"movement,omitempty"`
	Relationship       *RelationshipChange   `json:"relationship,omitempty"`
}

// Batch is both a structured receipt and one atomic committed history unit.
// Request is retained for exact idempotency comparison after reconstruction.
type Batch struct {
	Revision Revision `json:"revision"`
	Request  Request  `json:"request"`
	Events   []Event  `json:"events"`
}

func cloneEvent(e Event) Event {
	e.ItemIntent = cloneItem(e.ItemIntent)
	e.Capacity = cloneCapacityChange(e.Capacity)
	e.StockSplit = cloneStockSplit(e.StockSplit)
	e.Custody = copyPtr(e.Custody)
	if e.ConditionCheck != nil {
		v := *e.ConditionCheck
		v.Condition = *cloneCondition(&v.Condition)
		e.ConditionCheck = &v
	}
	e.Restoration = copyPtr(e.Restoration)
	if e.Restoration != nil {
		e.Restoration.Life = cloneLifeChange(e.Restoration.Life)
	}
	if e.ResourceRoll != nil {
		v := *e.ResourceRoll
		v.Faces = append([]int64(nil), v.Faces...)
		v.BonusRecipe = cloneRecipeExplanation(v.BonusRecipe)
		e.ResourceRoll = &v
	}
	if e.BattleAction != nil {
		e.BattleAction = &BattleActionResult{Before: cloneBattleEncounter(e.BattleAction.Before), After: cloneBattleEncounter(e.BattleAction.After)}
	}
	if e.BattleStarted != nil {
		v := *e.BattleStarted
		v.Intent = *cloneBegin(&v.Intent)
		v.After = cloneBattleEncounter(v.After)
		e.BattleStarted = &v
	}
	if e.WeaponAttack != nil {
		v := *e.WeaponAttack
		v.Intent = *cloneWeaponAttack(&v.Intent)
		v.Grant = cloneAttackGrant(v.Grant)
		v.Roll = cloneWeaponRoll(v.Roll)
		v.Before = cloneBattleEncounter(v.Before)
		v.After = cloneBattleEncounter(v.After)
		e.WeaponAttack = &v
	}
	if e.BattleTurn != nil {
		v := *e.BattleTurn
		v.Before = cloneBattleEncounter(v.Before)
		v.After = cloneBattleEncounter(v.After)
		e.BattleTurn = &v
	}

	e.Item = cloneItem(e.Item)
	e.AmmunitionRecovery = cloneAmmunitionRecovery(e.AmmunitionRecovery)
	e.Character = cloneCharacter(e.Character)
	if e.Addition != nil {
		e.Addition = copyPtr(e.Addition)
		e.Addition.Before = copyPtr(e.Addition.Before)
	}
	if e.Consumption != nil {
		e.Consumption = copyPtr(e.Consumption)
		e.Consumption.Before = copyPtr(e.Consumption.Before)
	}
	e.Time = copyPtr(e.Time)
	e.StatCheck = cloneStatCheckResult(e.StatCheck)
	e.StatAdjustment = copyPtr(e.StatAdjustment)
	e.ItemRecovery = cloneItemRecovery(e.ItemRecovery)
	e.Charges = cloneChargeExpenditure(e.Charges)
	e.AbilityOrigin = cloneAbilityOrigin(e.AbilityOrigin)
	e.Check = cloneCheckResult(e.Check)
	e.Travel = copyPtr(e.Travel)
	if e.Trip != nil {
		v := *e.Trip
		v.Stops = append([]LocationID(nil), v.Stops...)
		v.Visited = append([]LocationID(nil), v.Visited...)
		e.Trip = &v
	}
	e.Occupant = cloneOccupantChange(e.Occupant)
	e.MapEdit = cloneMapEdit(e.MapEdit)
	e.Quest = cloneQuestChange(e.Quest)
	e.Journal = cloneJournalChange(e.Journal)
	e.Attack = cloneAttackResult(e.Attack)
	e.Save = cloneSaveResult(e.Save)
	e.Claim = copyPtr(e.Claim)
	e.Objective = copyPtr(e.Objective)
	if e.Encounter != nil {
		x := *e.Encounter
		x.Boundary = copyPtr(e.Encounter.Boundary)
		e.Encounter = &x
	}
	e.Ability = cloneInvocation(e.Ability)
	if e.Condition != nil {
		v := e.Condition
		e.Condition = &ConditionChange{ExpectedType: v.ExpectedType, BattleEnding: copyPtr(v.BattleEnding), Before: cloneCondition(v.Before), After: cloneCondition(v.After), BeforeEffects: cloneEffects(v.BeforeEffects), AfterEffects: cloneEffects(v.AfterEffects), Expiration: copyPtr(v.Expiration), ClockExpiration: copyPtr(v.ClockExpiration), AnchorRemoved: copyPtr(v.AnchorRemoved), ReplacedBy: copyPtr(v.ReplacedBy), MinimumResource: copyPtr(v.MinimumResource), CheckFailed: v.CheckFailed, EndingCondition: copyPtr(v.EndingCondition), Resources: append([]ConditionResourceChange(nil), v.Resources...)}
	}
	e.Ownership = copyPtr(e.Ownership)
	e.Wearing = copyPtr(e.Wearing)
	e.Resource = copyPtr(e.Resource)
	if e.Resource != nil {
		e.Resource.Life = cloneLifeChange(e.Resource.Life)
		e.Resource.Buffer = copyPtr(e.Resource.Buffer)
		e.Resource.Treated = cloneDamageTreated(e.Resource.Treated)
	}
	e.Movement = copyPtr(e.Movement)
	if e.Effect != nil {
		e.Effect = &EffectChange{cloneEffect(e.Effect.Before), cloneEffect(e.Effect.After)}
	}
	if e.Relationship != nil {
		e.Relationship = &RelationshipChange{Decay: copyPtr(e.Relationship.Decay), Adjustment: copyPtr(e.Relationship.Adjustment), Before: copyPtr(e.Relationship.Before), After: e.Relationship.After}
	}
	return e
}
func cloneBatch(b Batch) Batch {
	b.Request = cloneRequest(b.Request)
	events := make([]Event, len(b.Events))
	for i, e := range b.Events {
		events[i] = cloneEvent(e)
	}
	b.Events = events
	return b
}

// operationFromEvent uses only a group's terminal semantic event. A transfer's
// preceding rule-driven unequip is regenerated and compared, never skipped.
func operationFromEvent(e Event) (Operation, error) {
	o := Operation{}
	switch {
	case e.Kind == ItemSplit && e.StockSplit != nil:
		v := e.StockSplit
		o.Split = &SplitItem{Item: v.Before.ID, Created: v.Created.ID, Amount: v.Requested, Handler: v.Handler}
	case e.Kind == ItemPlaced && e.Custody != nil:
		v := e.Custody
		o.Place = &PlaceItem{Item: v.Item, From: v.Before, To: v.After, Handler: v.Handler}
	case e.Kind == ResourceExpanded && e.Capacity != nil:
		v := e.Capacity
		o.Expand = &ResourceOp{Character: v.Character, Resource: v.Resource, Amount: v.Requested}
	case e.Kind == ResourceRestored && e.Restoration != nil:
		o.Restore = &RestoreResource{Character: e.Restoration.Character, Resource: e.Restoration.Resource}
	case e.Kind == BattleStarted && e.BattleStarted != nil:
		o.BeginBattle = cloneBegin(&e.BattleStarted.Intent)
	case e.Kind == WeaponAttackResolved && e.WeaponAttack != nil:
		o.WeaponAttack = cloneWeaponAttack(&e.WeaponAttack.Intent)
	case e.Kind == BattleTurnEnded && e.BattleTurn != nil:
		o.EndBattleTurn = copyPtr(&e.BattleTurn.Intent)
	case e.Kind == ItemCreated && e.Item != nil:
		o.CreateItem = cloneItem(e.Item)
		if e.ItemIntent != nil {
			o.CreateItem = cloneItem(e.ItemIntent)
		}
	case e.Kind == AmmunitionRecovered && e.AmmunitionRecovery != nil:
		o.RecoverAmmunition = cloneRecoverAmmunition(&e.AmmunitionRecovery.Intent)
	case e.Kind == ItemAdded && e.Addition != nil:
		v := e.Addition
		o.Add = &AddItem{Item: v.Item, Owner: v.Owner, Amount: v.Requested}
	case e.Kind == ItemConsumed && e.Consumption != nil:
		v := e.Consumption
		o.Consume = &ConsumeItem{Item: v.Item, Owner: v.Owner, Amount: v.Requested, Recoverable: v.Recoverable}
	case e.Kind == ChargesExpended && e.Charges != nil:
		v := e.Charges
		o.ExpendCharges = &ExpendCharges{Item: v.Item, Owner: v.Owner, Amount: v.Requested}
	case e.Kind == ItemRecovered && e.ItemRecovery != nil:
		v := e.ItemRecovery
		o.RecoverItem = &RecoverItem{Item: v.Item, Owner: v.Owner, Amount: copyPtr(v.Requested)}
	case e.Kind == CharacterCreated && e.Character != nil:
		o.CreateCharacter = cloneCharacter(e.Character)
	case e.Kind == TimeAdvanced && e.Time != nil:
		o.AdvanceTime = &AdvanceTime{Ticks: e.Time.Requested}
	case e.Kind == StatCheckResolved && e.StatCheck != nil:
		o.StatCheck = cloneStatCheck(&e.StatCheck.Intent)
	case e.Kind == StatAdjusted && e.StatAdjustment != nil:
		v := e.StatAdjustment
		o.AdjustStat = &AdjustStat{Character: v.Character, Stat: v.Stat, Amount: v.Amount}
	case e.Kind == PartyTraveled && e.Travel != nil:
		o.Travel = copyPtr(e.Travel)
	case occupantEvent(e.Kind) && e.Occupant != nil:
		o.Occupant = cloneOccupantOp(&e.Occupant.Intent)
	case mapEditEvent(e.Kind) && e.MapEdit != nil:
		o.MapEdit = cloneMapEdit(e.MapEdit)
	case questEvent(e.Kind) && e.Quest != nil:
		o.Quest = cloneQuestOp(&e.Quest.Intent)
	case e.Kind == JournalRecorded && e.Journal != nil:
		o.Journal = cloneRecordJournal(&e.Journal.Intent)
	case e.Kind == AttackScored && e.Attack != nil:
		o.Attack = cloneAttack(&e.Attack.Intent)
	case e.Kind == SaveScored && e.Save != nil:
		o.Save = cloneSave(&e.Save.Intent)
	case (e.Kind == ObjectiveClaimed || e.Kind == ObjectiveCompleted) && e.Claim != nil:
		o.Claim = copyPtr(e.Claim)
	case e.Kind == AbilityInvoked && e.Ability != nil:
		o.Invoke = cloneInvocation(e.Ability)
	case e.Kind == ConditionApplied && e.Condition != nil && e.Condition.After != nil:
		v := conditionIntent(*e.Condition.After)
		o.ApplyCondition = &v
	case e.Kind == ConditionRemoved && e.Condition != nil && e.Condition.Before != nil:
		v := e.Condition.Before
		o.RemoveCondition = &RemoveCondition{Instance: v.ID, Target: v.Target, Type: e.Condition.ExpectedType}
	case e.Kind == ItemTransferred && e.Ownership != nil:
		v := e.Ownership
		o.Transfer = &Transfer{v.Item, v.Before, v.After}
	case e.Kind == ItemEquipped && e.Wearing != nil:
		v := e.Wearing
		o.Equip = &Equip{Item: v.Item, Wearer: v.After, Mode: v.RequestedMode}
	case e.Kind == ItemUnequipped && e.Wearing != nil:
		o.Unequip = &Unequip{e.Wearing.Item}
	case (e.Kind == DamageApplied || e.Kind == HealingApplied || e.Kind == ResourceSpent || e.Kind == ResourceGranted) && e.Resource != nil:
		v := e.Resource
		r := &ResourceOp{Character: v.Character, Resource: v.Resource, Amount: v.Requested}
		if e.Kind == DamageApplied {
			// A treated Damage replays as asked: its kind and amount, before traits.
			if v.Treated != nil {
				r.Amount, r.Kind = v.Treated.Amount, v.Treated.Kind
			}
			o.Damage = r
		} else if e.Kind == ResourceGranted {
			o.Grant = r
		} else if e.Kind == ResourceSpent {
			o.Spend = r
		} else {
			o.Heal = r
		}
	case e.Kind == EffectApplied && e.Effect != nil && e.Effect.After != nil:
		o.Apply = cloneEffect(e.Effect.After)
	case (e.Kind == EffectRemoved || e.Kind == EffectExpired) && e.Effect != nil && e.Effect.Before != nil:
		v := &EffectOp{e.Effect.Before.ID}
		if e.Kind == EffectRemoved {
			o.Remove = v
		} else {
			o.Expire = v
		}
	case e.Kind == EntityMoved && e.Movement != nil:
		v := e.Movement
		o.Move = &Move{v.Character, v.After}
	case e.Kind == RelationshipAdjusted && e.Relationship != nil && e.Relationship.Adjustment != nil && *e.Relationship.Adjustment != 0 && e.Relationship.Decay == nil:
		v := e.Relationship
		o.AdjustRelationship = &AdjustRelationship{From: v.After.From, Kind: v.After.Kind, To: v.After.To, Amount: *v.Adjustment}
	case e.Kind == RelationshipChanged && e.Relationship != nil && e.Relationship.Adjustment == nil && e.Relationship.Decay == nil:
		o.Relate = copyPtr(&e.Relationship.After)
	default:
		return o, fail("E_REPLAY", "", "event", "supported typed event", string(e.Kind), "missing or invalid event payload")
	}
	return o, nil
}

// Replay reconstructs a NEW world from committed event facts. It derives intent
// from event payloads, reruns the same transition semantics, and compares the
// complete trace including before facts. Retained requests cannot override events.
// No partially reconstructed world is returned on any failure.
func Replay(initial Blueprint, history []Batch) (*World, error) {
	if len(history) > MaxReplayBatches {
		return nil, fail("E_LIMIT", "", "replay", "at most 1024 batches", fmt.Sprint(len(history)), "replay batch limit exceeded")
	}
	w, err := NewWorld(initial)
	if err != nil {
		return nil, err
	}
	for _, record := range history {
		if err := replayBatch(w, record); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// replayBatch is shared by the bounded prefix and complete archive constructors.
// Its World is private to reconstruction, so a failure cannot expose partial
// history. Event-derived intent and the entire generated Batch must both agree.
func replayBatch(w *World, record Batch) error {
	if len(record.Events) == 0 || len(record.Events) > 2*MaxOperations {
		return fail("E_REPLAY", "", "events", "1..256 events", fmt.Sprint(len(record.Events)), "invalid replay batch size")
	}
	if record.Revision != w.Revision()+1 || record.Request.ExpectedRevision != w.Revision() {
		return fail("E_REPLAY", "", "revision", "contiguous revision", fmt.Sprint(record.Revision), "invalid replay revision")
	}
	if _, exists := w.authority.receipts[record.Request.ID]; exists {
		return fail("E_REPLAY", record.Request.ID, "request", "unique committed request", "duplicate", "request repeated in replay")
	}
	ops := []Operation{}
	for start := 0; start < len(record.Events); {
		index := len(ops)
		end := start
		if record.Events[start].Operation != index {
			return fail("E_REPLAY", "", "operation", "contiguous operation indexes", "invalid index", "invalid event grouping")
		}
		for end < len(record.Events) && record.Events[end].Operation == index {
			end++
		}
		o, e := operationFromEvent(record.Events[end-1])
		if e != nil {
			return e
		}
		for _, event := range record.Events[start:end] {
			if event.Kind == ConditionCheckResolved && event.ConditionCheck != nil && event.ConditionCheck.Source == HumanController {
				c := event.ConditionCheck
				o.ConditionRolls = append(o.ConditionRolls, ConditionRollInput{Condition: c.Condition.ID, Target: c.Condition.Target, Face: int64(c.Roll.Draw)})
			}
			// A damage-triggered check outside Battle took its supplied faces
			// from the operation's condition rolls; an Attack's or a Save's are in
			// its intent.
			if c := event.StatCheck; event.Kind == StatCheckResolved && c != nil && c.Condition != nil && o.Attack == nil && o.Save == nil {
				for _, face := range c.Intent.Faces {
					o.ConditionRolls = append(o.ConditionRolls, ConditionRollInput{Condition: *c.Condition, Target: c.Intent.Character, Face: face})
				}
			}
		}
		// A treated Damage's unused faces followed the ones its checks took.
		if r := record.Events[end-1].Resource; o.Damage != nil && r != nil && r.Treated != nil {
			for _, f := range r.Treated.UnusedFaces {
				o.ConditionRolls = append(o.ConditionRolls, ConditionRollInput{Condition: f.Condition, Target: o.Damage.Character, Face: f.Face})
			}
		}
		ops = append(ops, o)
		start = end
	}
	r := Request{ID: record.Request.ID, ExpectedRevision: w.Revision(), Actor: record.Request.Actor, Operations: ops}
	if !reflect.DeepEqual(r, record.Request) {
		return fail("E_REPLAY", r.ID, "request", "event-derived request", "mismatch", "retained request disagrees with event facts")
	}
	actual, e := w.Execute(r)
	if e != nil {
		return fmt.Errorf("replay: %w", e)
	}
	if !reflect.DeepEqual(actual, record) {
		return fail("E_REPLAY", r.ID, "events", "exact semantic trace", "mismatch", "event before/after facts, metadata, or rule trace disagree")
	}
	return nil
}
