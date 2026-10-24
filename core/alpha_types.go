package core

// Alpha records extend the same Blueprint and candidate, not a separate world.
type EncounterID string
type ObjectiveID string
type Team string

const (
	PartyTeam   Team = "party"
	HostileTeam Team = "hostile"
)

type Adventure struct {
	Definition AdventureDefinition `json:"definition"`
	State      AdventureState      `json:"state"`
}
type AdventureDefinition struct {
	Party            []CharacterID         `json:"party"`
	Health           ResourceID            `json:"health"`
	Links            []LocationLink        `json:"links"`
	Grants           []AbilityGrant        `json:"grants"`
	Encounters       []EncounterDefinition `json:"encounters"`
	Objectives       []ObjectiveDefinition `json:"objectives"`
	InitialEncounter EncounterID           `json:"initial_encounter"`
	Seed             [2]uint64             `json:"seed"`
}
type LocationLink struct {
	From LocationID `json:"from"`
	To   LocationID `json:"to"`
}
type AbilityGrant struct {
	Character CharacterID `json:"character"`
	Abilities []AbilityID `json:"abilities"`
}
type Participant struct {
	Character CharacterID `json:"character"`
	Team      Team        `json:"team"`
}
type EncounterDefinition struct {
	ID       EncounterID   `json:"id"`
	Location LocationID    `json:"location"`
	Order    []Participant `json:"order"`
}
type ObjectiveDefinition struct {
	ID          ObjectiveID `json:"id"`
	Item        ItemID      `json:"item"`
	Custodian   CharacterID `json:"custodian"`
	Victory     EncounterID `json:"victory"`
	Destination LocationID  `json:"destination"`
}
type EncounterStatus string

const (
	Pending EncounterStatus = "pending"
	Active  EncounterStatus = "active"
	Victory EncounterStatus = "victory"
	Defeat  EncounterStatus = "defeat"
)

type EncounterProgress struct {
	ID     EncounterID     `json:"id"`
	Status EncounterStatus `json:"status"`
	Round  uint64          `json:"round"`
	Cursor int             `json:"cursor"`
}
type ObjectiveProgress struct {
	ID        ObjectiveID `json:"id"`
	Claimed   bool        `json:"claimed"`
	Completed bool        `json:"completed"`
}
type AdventureState struct {
	Encounters []EncounterProgress `json:"encounters"`
	Objectives []ObjectiveProgress `json:"objectives"`
	Random     RandomState         `json:"random"`
}
type RandomState struct {
	Version string `json:"version"`
	Bytes   []byte `json:"bytes"`
	Calls   uint64 `json:"calls"`
}

const RandomVersion = "go1.27-pcg-uint64n-v1"

type ActionPhase string

const (
	Exploration    ActionPhase = "exploration"
	EncounterPhase ActionPhase = "encounter"
	EitherPhase    ActionPhase = "either"
)

type ActionPolicy struct {
	Finish       *bool          `json:"finish,omitempty"` // ordinary Battle action only; absent finishes
	Actor        AbilityOperand `json:"actor"`
	Phase        ActionPhase    `json:"phase"`
	Requirements []Requirement  `json:"requirements"`
	TurnCost     *TurnCost      `json:"turn_cost,omitempty"`
}

// TurnCost opts into an authored Resource allowance during Battle turns.
// Outside Battle the body still runs normally, without this turn payment.
type TurnCost struct {
	Resource ResourceID `json:"resource"`
	Amount   int64      `json:"amount"`
	Finish   bool       `json:"finish"`
}
type RequirementKind string

const (
	AboveMinimum   RequirementKind = "above_minimum"
	Colocated      RequirementKind = "colocated"
	Allied         RequirementKind = "allied"
	Hostile        RequirementKind = "hostile"
	Distinct       RequirementKind = "distinct"
	HasCondition   RequirementKind = "has_condition"
	LacksCondition RequirementKind = "lacks_condition"
	PartyOwned     RequirementKind = "party_owned"
	CarriedItem    RequirementKind = "carried_item"
	WithinRange    RequirementKind = "within"
	FreeSlot       RequirementKind = "free_slot"
	WornTraining   RequirementKind = "worn_training"
	AtLocation     RequirementKind = "at"
	StatBetween    RequirementKind = "stat_between"
)

// DistanceLimit uses the authored grid's units. It does not create positions.
type DistanceLimit struct {
	Maximum int64  `json:"maximum"`
	Unit    string `json:"unit"`
}

type WornSelector struct {
	Definition EquipmentDefinitionID `json:"definition"`
	Mode       EquipmentModeID       `json:"mode"`
}

// StatRange compares the existing effective value with inclusive authored bounds.
type StatRange struct {
	Stat    StatID `json:"stat"`
	Minimum int64  `json:"minimum"`
	Maximum int64  `json:"maximum"`
}

// Each kind has a fixed schema, validated by checkActionPolicy.
type Requirement struct {
	StatRange *StatRange      `json:"stat_range,omitempty"`
	Location  LocationID      `json:"location,omitempty"`
	Kind      RequirementKind `json:"kind"`
	Subject   AbilityOperand  `json:"subject"`
	Other     *AbilityOperand `json:"other,omitempty"`
	Resource  ResourceID      `json:"resource,omitempty"`
	Condition ConditionTypeID `json:"condition,omitempty"`
	Distance  *DistanceLimit  `json:"distance,omitempty"`
	Slot      *SlotUse        `json:"slot,omitempty"`
	Worn      *WornSelector   `json:"worn,omitempty"`
}
type StatOperand struct {
	Character AbilityOperand `json:"character"`
	Stat      StatID         `json:"stat"`
}
type CheckValue struct {
	Literal *int64       `json:"literal,omitempty"`
	Stat    *StatOperand `json:"stat,omitempty"`
}
type CheckTemplate struct {
	Boost     *CheckBoostPolicy  `json:"boost,omitempty"`
	ActorRoll bool               `json:"actor_roll,omitempty"`
	ID        string             `json:"id"`
	Sides     uint64             `json:"sides"`
	Modifier  CheckValue         `json:"modifier"`
	Threshold CheckValue         `json:"threshold"`
	Success   []AbilityPrimitive `json:"success"`
	Failure   []AbilityPrimitive `json:"failure"`
}

// Branches are deliberately nonrecursive: they cannot contain another Check.
type AbilityPrimitive struct {
	AdjustRelationship *AdjustRelationshipTemplate `json:"adjust_relationship,omitempty"`
	Consume            *ConsumeTemplate            `json:"consume,omitempty"`
	RemoveCondition    *ConditionRemovalTemplate   `json:"remove_condition,omitempty"`
	Expand             *ExpandTemplate             `json:"expand,omitempty"`
	Place              *PlaceTemplate              `json:"place,omitempty"`
	AdvanceTime        *AdvanceTimeTemplate        `json:"advance_time,omitempty"`
	Equip              *EquipTemplate              `json:"equip,omitempty"`
	Unequip            *UnequipTemplate            `json:"unequip,omitempty"`
	Damage             *DamageTemplate             `json:"damage,omitempty"`
	Restore            *RestoreTemplate            `json:"restore,omitempty"`
	Heal               *HealTemplate               `json:"heal,omitempty"`
	Spend              *SpendTemplate              `json:"spend,omitempty"`
	ApplyCondition     *ConditionTemplate          `json:"apply_condition,omitempty"`
	Transfer           *TransferTemplate           `json:"transfer,omitempty"`
	Travel             *PartyTravel                `json:"travel,omitempty"`
	Claim              *ClaimTemplate              `json:"claim,omitempty"`
}
type PartyTravel struct {
	To LocationID `json:"to"`
}
type ClaimObjective struct {
	Objective ObjectiveID `json:"objective"`
	Recipient CharacterID `json:"recipient"`
}
type ClaimTemplate struct {
	Objective ObjectiveID    `json:"objective"`
	Recipient AbilityOperand `json:"recipient"`
}
type ConditionLifetime struct {
	AppliedRound      uint64      `json:"applied_round"`
	Encounter         EncounterID `json:"encounter"`
	ExpiresAfterRound uint64      `json:"expires_after_round"`
}
type ExpirationOrigin struct {
	Encounter EncounterID `json:"encounter"`
	Round     uint64      `json:"round"`
	Reason    string      `json:"reason"`
}
type CheckResult struct {
	Boost       *CheckBoostResult `json:"boost,omitempty"`
	Source      BattleController  `json:"source,omitempty"`
	Check       string            `json:"check"`
	Sides       uint64            `json:"sides"`
	Draw        uint64            `json:"draw"`
	Modifier    int64             `json:"modifier"`
	Threshold   int64             `json:"threshold"`
	Total       int64             `json:"total"`
	Success     bool              `json:"success"`
	CallsBefore uint64            `json:"calls_before"`
	CallsAfter  uint64            `json:"calls_after"`
	EngineDrawn bool              `json:"engine_drawn,omitempty"`
}
type EncounterChange struct {
	Before   EncounterProgress `json:"before"`
	After    EncounterProgress `json:"after"`
	Boundary *uint64           `json:"boundary,omitempty"`
}
type ObjectiveChange struct {
	Before ObjectiveProgress `json:"before"`
	After  ObjectiveProgress `json:"after"`
}

const (
	CheckResolved      EventKind = "CheckResolved"
	TurnAdvanced       EventKind = "TurnAdvanced"
	EncounterStarted   EventKind = "EncounterStarted"
	EncounterEnded     EventKind = "EncounterEnded"
	PartyTraveled      EventKind = "PartyTraveled"
	ObjectiveClaimed   EventKind = "ObjectiveClaimed"
	ObjectiveCompleted EventKind = "ObjectiveCompleted"
)

func (p AbilityPrimitive) Step() AbilityStep {
	return AbilityStep{AdjustRelationship: p.AdjustRelationship, Consume: p.Consume, RemoveCondition: p.RemoveCondition, Expand: p.Expand, Place: p.Place, AdvanceTime: p.AdvanceTime, Equip: p.Equip, Unequip: p.Unequip, Damage: p.Damage, Heal: p.Heal, Restore: p.Restore, Spend: p.Spend, ApplyCondition: p.ApplyCondition, Transfer: p.Transfer, Travel: p.Travel, Claim: p.Claim}
}
func (s AbilityStep) Primitive() AbilityPrimitive {
	return AbilityPrimitive{AdjustRelationship: s.AdjustRelationship, Consume: s.Consume, RemoveCondition: s.RemoveCondition, Expand: s.Expand, Place: s.Place, AdvanceTime: s.AdvanceTime, Equip: s.Equip, Unequip: s.Unequip, Damage: s.Damage, Heal: s.Heal, Restore: s.Restore, Spend: s.Spend, ApplyCondition: s.ApplyCondition, Transfer: s.Transfer, Travel: s.Travel, Claim: s.Claim}
}
func cloneAbilityOrigin(v *AbilityOrigin) *AbilityOrigin {
	if v == nil {
		return nil
	}
	o := *v
	if v.Path != nil {
		o.Path = append([]int{}, v.Path...)
	}
	return &o
}
func cloneCheckValue(v CheckValue) CheckValue {
	v.Literal = copyPtr(v.Literal)
	if v.Stat != nil {
		x := *v.Stat
		x.Character = cloneAbilityOperand(x.Character)
		v.Stat = &x
	}
	return v
}
func cloneActionPolicy(v *ActionPolicy) *ActionPolicy {
	if v == nil {
		return nil
	}
	o := *v
	o.Actor = cloneAbilityOperand(v.Actor)
	o.TurnCost = copyPtr(v.TurnCost)
	o.Finish = copyPtr(v.Finish)
	if v.Requirements != nil {
		o.Requirements = make([]Requirement, len(v.Requirements))
		for i, r := range v.Requirements {
			o.Requirements[i] = r
			o.Requirements[i].Subject = cloneAbilityOperand(r.Subject)
			o.Requirements[i].Distance = copyPtr(r.Distance)
			o.Requirements[i].Slot = copyPtr(r.Slot)
			o.Requirements[i].Worn = copyPtr(r.Worn)
			o.Requirements[i].StatRange = copyPtr(r.StatRange)
			if r.Other != nil {
				x := cloneAbilityOperand(*r.Other)
				o.Requirements[i].Other = &x
			}
		}
	}
	return &o
}
func cloneAdventure(v *Adventure) *Adventure {
	if v == nil {
		return nil
	}
	o := *v
	d := v.Definition
	if d.Party != nil {
		d.Party = append([]CharacterID{}, d.Party...)
	}
	if d.Links != nil {
		d.Links = append([]LocationLink{}, d.Links...)
	}
	if d.Grants != nil {
		d.Grants = append([]AbilityGrant{}, d.Grants...)
		for i, g := range d.Grants {
			if g.Abilities != nil {
				d.Grants[i].Abilities = append([]AbilityID{}, g.Abilities...)
			}
		}
	}
	if d.Encounters != nil {
		d.Encounters = append([]EncounterDefinition{}, d.Encounters...)
		for i, e := range d.Encounters {
			if e.Order != nil {
				d.Encounters[i].Order = append([]Participant{}, e.Order...)
			}
		}
	}
	if d.Objectives != nil {
		d.Objectives = append([]ObjectiveDefinition{}, d.Objectives...)
	}
	o.Definition = d
	if v.State.Encounters != nil {
		o.State.Encounters = append([]EncounterProgress{}, v.State.Encounters...)
	}
	if v.State.Objectives != nil {
		o.State.Objectives = append([]ObjectiveProgress{}, v.State.Objectives...)
	}
	if v.State.Random.Bytes != nil {
		o.State.Random.Bytes = append([]byte{}, v.State.Random.Bytes...)
	}
	return &o
}
