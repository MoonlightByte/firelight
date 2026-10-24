package core

// Operation is a closed typed sum. Exactly one variant must be present. This
// keeps JSON boundaries typed too; malformed/forged sums fail before publication.
type Operation struct {
	Grant              *ResourceOp          `json:"grant,omitempty"`
	AdjustRelationship *AdjustRelationship  `json:"adjust_relationship,omitempty"`
	AdjustStat         *AdjustStat          `json:"adjust_stat,omitempty"`
	RecoverItem        *RecoverItem         `json:"recover_item,omitempty"`
	RecoverAmmunition  *RecoverAmmunition   `json:"recover_ammunition,omitempty"`
	Expand             *ResourceOp          `json:"expand,omitempty"`
	Split              *SplitItem           `json:"split,omitempty"`
	Place              *PlaceItem           `json:"place,omitempty"`
	ConditionRolls     []ConditionRollInput `json:"condition_rolls,omitempty"`
	BeginBattle        *BeginBattle         `json:"begin_battle,omitempty"`
	WeaponAttack       *WeaponAttack        `json:"weapon_attack,omitempty"`
	EndBattleTurn      *EndBattleTurn       `json:"end_battle_turn,omitempty"`
	CreateItem         *Item                `json:"create_item,omitempty"`
	Add                *AddItem             `json:"add,omitempty"`
	Consume            *ConsumeItem         `json:"consume,omitempty"`
	ExpendCharges      *ExpendCharges       `json:"expend_charges,omitempty"`
	CreateCharacter    *Character           `json:"create_character,omitempty"`
	AdvanceTime        *AdvanceTime         `json:"advance_time,omitempty"`
	StatCheck          *StatCheck           `json:"stat_check,omitempty"`
	Travel             *PartyTravel         `json:"travel,omitempty"`
	Claim              *ClaimObjective      `json:"claim,omitempty"`
	Invoke             *InvokeAbility       `json:"invoke,omitempty"`
	ApplyCondition     *ApplyCondition      `json:"apply_condition,omitempty"`
	RemoveCondition    *RemoveCondition     `json:"remove_condition,omitempty"`
	Transfer           *Transfer            `json:"transfer,omitempty"`
	Equip              *Equip               `json:"equip,omitempty"`
	Unequip            *Unequip             `json:"unequip,omitempty"`
	Damage             *ResourceOp          `json:"damage,omitempty"`
	Heal               *ResourceOp          `json:"heal,omitempty"`
	Spend              *ResourceOp          `json:"spend,omitempty"`
	Restore            *RestoreResource     `json:"restore,omitempty"`
	Apply              *Effect              `json:"apply,omitempty"`
	Remove             *EffectOp            `json:"remove,omitempty"`
	Expire             *EffectOp            `json:"expire,omitempty"`
	Move               *Move                `json:"move,omitempty"`
	Relate             *Relationship        `json:"relate,omitempty"`
	Occupant           *OccupantOp          `json:"occupant,omitempty"`
	MapEdit            *MapEdit             `json:"map_edit,omitempty"`
	Quest              *QuestOp             `json:"quest,omitempty"`
	Journal            *RecordJournal       `json:"journal,omitempty"`
	Attack             *Attack              `json:"attack,omitempty"`
	Save               *Save                `json:"save,omitempty"`
}
type Transfer struct {
	Item ItemID      `json:"item"`
	From CharacterID `json:"from"`
	To   CharacterID `json:"to"`
}
type Equip struct {
	Mode   EquipmentModeID `json:"mode,omitempty"`
	Item   ItemID          `json:"item"`
	Wearer CharacterID     `json:"wearer"`
}
type Unequip struct {
	Item ItemID `json:"item"`
}

// ResourceOp is a resource change. Kind, on Damage only, is the damage kind
// the target's damage traits treat before the Damage applies.
type ResourceOp struct {
	Character CharacterID `json:"character"`
	Resource  ResourceID  `json:"resource"`
	Amount    int64       `json:"amount"`
	Kind      string      `json:"kind,omitempty"`
}

// RestoreResource restores the canonical maximum; it is not a caller-supplied
// postimage or a healing amount (the full range can exceed int64).
type RestoreResource struct {
	Character CharacterID `json:"character"`
	Resource  ResourceID  `json:"resource"`
}
type EffectOp struct {
	Effect EffectID `json:"effect"`
}
type Move struct {
	Character CharacterID `json:"character"`
	To        LocationID  `json:"to"`
}

const MaxOperations = 128
const MaxReplayBatches = 1024

type Request struct {
	ID               string      `json:"id"`
	ExpectedRevision Revision    `json:"expected_revision"`
	Actor            Ref         `json:"actor"`
	Operations       []Operation `json:"operations"`
}

func (o Operation) checkShape() error {
	n := 0
	if o.AdjustRelationship != nil {
		n++
	}
	if o.AdjustStat != nil {
		n++
	}
	if o.RecoverItem != nil {
		n++
	}
	if o.RecoverAmmunition != nil {
		n++
	}
	if o.Place != nil {
		n++
	}
	if o.Split != nil {
		n++
	}
	if o.ExpendCharges != nil {
		n++
	}
	for _, yes := range []bool{o.BeginBattle != nil, o.WeaponAttack != nil, o.EndBattleTurn != nil, o.CreateItem != nil, o.Add != nil, o.Consume != nil, o.CreateCharacter != nil, o.AdvanceTime != nil, o.StatCheck != nil, o.Travel != nil, o.Claim != nil, o.Invoke != nil, o.ApplyCondition != nil, o.RemoveCondition != nil, o.Transfer != nil, o.Equip != nil, o.Unequip != nil, o.Damage != nil, o.Heal != nil, o.Grant != nil, o.Spend != nil, o.Restore != nil, o.Expand != nil, o.Apply != nil, o.Remove != nil, o.Expire != nil, o.Move != nil, o.Relate != nil, o.Occupant != nil, o.MapEdit != nil, o.Quest != nil, o.Journal != nil, o.Attack != nil, o.Save != nil} {
		if yes {
			n++
		}
	}
	if n != 1 {
		return fail("E_IR", "", "operation", "exactly one typed variant", "invalid sum", "operation requires exactly one variant")
	}
	return checkConditionRollInputs(o)
}

// CheckOperationStatic validates syntax-independent immutable types. Mutable
// preconditions and game rules are always checked again by Execute.
func (b Blueprint) CheckOperationStatic(o Operation) error {
	if err := o.checkShape(); err != nil {
		return err
	}
	var refs []Ref
	switch {
	case o.AdjustRelationship != nil:
		return b.checkAdjustRelationship(*o.AdjustRelationship)
	case o.AdjustStat != nil:
		return b.checkAdjustStat(*o.AdjustStat)
	case o.RecoverItem != nil:
		return b.checkRecoverItem(*o.RecoverItem)
	case o.RecoverAmmunition != nil:
		return b.checkRecoverAmmunition(*o.RecoverAmmunition)
	case o.Split != nil:
		return b.checkSplit(*o.Split)
	case o.ExpendCharges != nil:
		return b.checkExpendCharges(*o.ExpendCharges)
	case o.Place != nil:
		return b.checkPlace(*o.Place)
	case o.Restore != nil:
		return b.CheckResource(o.Restore.Character, o.Restore.Resource)
	case o.StatCheck != nil:
		return b.checkStatCheck(*o.StatCheck)
	case o.BeginBattle != nil || o.WeaponAttack != nil || o.EndBattleTurn != nil:
		return b.checkBattleStatic(o)
	case o.CreateItem != nil:
		return b.checkCreateItem(*o.CreateItem)
	case o.Add != nil:
		v := o.Add
		if v.Amount <= 0 {
			return fail("E_AMOUNT", string(v.Item), "quantity", "positive int64", "nonpositive", "addition amount must be positive")
		}
		refs = []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.Owner)}}
	case o.Consume != nil:
		v := o.Consume
		if v.Amount <= 0 {
			return fail("E_AMOUNT", string(v.Item), "quantity", "positive int64", "nonpositive", "consumption amount must be positive")
		}
		if v.Recoverable && !b.recoveryRule() {
			return noRecoveryRule(v.Item, "recoverable")
		}
		refs = []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.Owner)}}
	case o.CreateCharacter != nil:
		return b.checkCreateCharacter(*o.CreateCharacter)
	case o.AdvanceTime != nil:
		return b.checkAdvanceTime(*o.AdvanceTime)
	case o.Travel != nil:
		return b.CheckRef(Ref{LocationKind, string(o.Travel.To)})
	case o.Claim != nil:
		if b.objectiveDefinition(o.Claim.Objective) == nil {
			return abilityFault("E_OBJECTIVE", "objective", "declared objective", string(o.Claim.Objective))
		}
		return b.CheckRef(Ref{CharacterKind, string(o.Claim.Recipient)})
	case o.Invoke != nil:
		return b.checkInvocation(*o.Invoke)
	case o.ApplyCondition != nil:
		return b.CheckConditionStatic(*o.ApplyCondition)
	case o.RemoveCondition != nil:
		v := o.RemoveCondition
		if !validID(string(v.Instance)) {
			return conditionFault("E_ID", string(v.Instance), "condition_instance", "valid instance ID", "invalid")
		}
		if v.Type != "" && conditionDefinition(&b, v.Type) == nil {
			return conditionFault("E_CONDITION_TYPE", string(v.Type), "condition_type", "declared condition type", "missing")
		}
		refs = []Ref{{CharacterKind, string(v.Target)}}
	case o.Transfer != nil:
		v := o.Transfer
		refs = []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.From)}, {CharacterKind, string(v.To)}}
	case o.Equip != nil:
		v := o.Equip
		refs = []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.Wearer)}}
	case o.Unequip != nil:
		refs = []Ref{{ItemKind, string(o.Unequip.Item)}}
	case o.Damage != nil || o.Heal != nil || o.Spend != nil || o.Expand != nil || o.Grant != nil:
		v := o.Damage
		if v == nil {
			v = o.Heal
		}
		if v == nil {
			v = o.Spend
		}
		if v == nil {
			v = o.Expand
		}
		if v == nil {
			v = o.Grant
		}
		if v.Amount <= 0 {
			return fail("E_AMOUNT", string(v.Character), string(v.Resource), "positive int64", "nonpositive", "resource amount must be positive")
		}
		if v.Kind != "" {
			if err := b.checkDamageKind(o.Damage != nil, *v); err != nil {
				return err
			}
		}
		return b.CheckResource(v.Character, v.Resource)
	case o.Apply != nil:
		return b.CheckEffectStatic(*o.Apply)
	case o.Remove != nil || o.Expire != nil:
		v := o.Remove
		if v == nil {
			v = o.Expire
		}
		if !validID(string(v.Effect)) {
			return fail("E_ID", string(v.Effect), "effect", "valid effect ID", "invalid", "invalid effect ID")
		}
	case o.Move != nil:
		v := o.Move
		refs = []Ref{{CharacterKind, string(v.Character)}, {LocationKind, string(v.To)}}
	case o.Relate != nil:
		return b.CheckRelationship(*o.Relate)
	case o.Occupant != nil:
		return b.checkOccupantOp(*o.Occupant)
	case o.MapEdit != nil:
		return b.checkMapEdit(*o.MapEdit)
	case o.Quest != nil:
		return b.checkQuestOp(*o.Quest)
	case o.Journal != nil:
		return b.checkJournalOp(*o.Journal)
	case o.Attack != nil:
		return b.checkAttack(*o.Attack)
	case o.Save != nil:
		return b.checkSave(*o.Save)
	}
	for _, r := range refs {
		if err := b.CheckRef(r); err != nil {
			return err
		}
	}
	if o.Equip != nil {
		return b.checkEquipMode(*o.Equip)
	}
	return nil
}
func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func cloneOperation(o Operation) Operation {
	return Operation{Grant: copyPtr(o.Grant), AdjustRelationship: copyPtr(o.AdjustRelationship), AdjustStat: copyPtr(o.AdjustStat), RecoverItem: cloneRecoverItem(o.RecoverItem), RecoverAmmunition: cloneRecoverAmmunition(o.RecoverAmmunition), Expand: copyPtr(o.Expand), Split: copyPtr(o.Split), Place: copyPtr(o.Place), ConditionRolls: append([]ConditionRollInput(nil), o.ConditionRolls...), BeginBattle: cloneBegin(o.BeginBattle), WeaponAttack: cloneWeaponAttack(o.WeaponAttack), EndBattleTurn: copyPtr(o.EndBattleTurn), CreateItem: cloneItem(o.CreateItem), Add: copyPtr(o.Add), Consume: copyPtr(o.Consume), ExpendCharges: copyPtr(o.ExpendCharges), CreateCharacter: cloneCharacter(o.CreateCharacter), AdvanceTime: copyPtr(o.AdvanceTime), StatCheck: cloneStatCheck(o.StatCheck), Travel: copyPtr(o.Travel), Claim: copyPtr(o.Claim), Invoke: cloneInvocation(o.Invoke), ApplyCondition: cloneApplyCondition(o.ApplyCondition), RemoveCondition: copyPtr(o.RemoveCondition), Transfer: copyPtr(o.Transfer), Equip: copyPtr(o.Equip), Unequip: copyPtr(o.Unequip), Damage: copyPtr(o.Damage), Heal: copyPtr(o.Heal), Spend: copyPtr(o.Spend), Restore: copyPtr(o.Restore), Apply: cloneEffect(o.Apply), Remove: copyPtr(o.Remove), Expire: copyPtr(o.Expire), Move: copyPtr(o.Move), Relate: copyPtr(o.Relate), Occupant: cloneOccupantOp(o.Occupant), MapEdit: cloneMapEdit(o.MapEdit), Quest: cloneQuestOp(o.Quest), Journal: cloneRecordJournal(o.Journal), Attack: cloneAttack(o.Attack), Save: cloneSave(o.Save)}
}
func cloneRequest(r Request) Request {
	ops := make([]Operation, len(r.Operations))
	for i, o := range r.Operations {
		ops[i] = cloneOperation(o)
	}
	r.Operations = ops
	return r
}

// CanonicalRequest detaches caller-owned intent using the exact normalization
// used by Execute and stored receipts. It does not validate or execute it.
func CanonicalRequest(r Request) Request { return cloneRequest(r) }
