package core

// Battle is the native, explicitly authored weapon encounter profile. Alpha's
// Adventure profile remains separate and cannot coexist in the same World.
type Battle struct {
	Rules BattleRules `json:"rules"`
	State BattleState `json:"state"`
}

const BattleVersion = "nql-battle-v1"

type BattleSide string
type BattleController string
type LifeOutcome string

const (
	ProtectedSide   BattleSide       = "protected"
	OppositionSide  BattleSide       = "opposition"
	HumanController BattleController = "human"
	AgentController BattleController = "agent"
	StableDown      LifeOutcome      = "stable_down"
	Dead            LifeOutcome      = "dead"
)

type BattleRules struct {
	EncounterDefinitions []BattleEncounterDefinition `json:"encounter_definitions,omitempty"`
	Ranged               *BattleRangedPolicy         `json:"ranged,omitempty"`
	LocationSpans        []LocationSpan              `json:"location_spans,omitempty"`
	ConditionChecks      []ConditionCheckPolicy      `json:"condition_checks,omitempty"`
	Routes               []TravelRoute               `json:"routes,omitempty"`
	TurnResources        []ResourceID                `json:"turn_resources,omitempty"`
	Allowances           []BattleAllowance           `json:"allowances,omitempty"`
	HitConditions        []WeaponHitCondition        `json:"hit_conditions,omitempty"`
	// Actions names the immutable opted-in vocabulary. Authored requirements
	// (for example a known-feature Condition) decide who may use each action.
	Actions            []AbilityID          `json:"actions,omitempty"`
	Version            string               `json:"version"`
	Health             ResourceID           `json:"health"`
	Defense            StatID               `json:"defense"`
	Initiative         StatID               `json:"initiative"`
	PartyRoot          CharacterID          `json:"party_root"`
	Membership         RelationKind         `json:"membership"`
	MembershipStrength int64                `json:"membership_strength"`
	DieSides           int64                `json:"die_sides"`
	MissFace           int64                `json:"miss_face"`
	CriticalFace       int64                `json:"critical_face"`
	CriticalMultiplier int64                `json:"critical_multiplier"`
	GridUnit           string               `json:"grid_unit"`
	SquareSize         int64                `json:"square_size"`
	ClockUnit          string               `json:"clock_unit"`
	RoundTicks         int64                `json:"round_ticks"`
	Seed               [2]uint64            `json:"seed"`
	ProtectedOutcome   LifeOutcome          `json:"protected_outcome"`
	OppositionOutcome  LifeOutcome          `json:"opposition_outcome"`
	RollPolicies       []BattleRollPolicy   `json:"roll_policies"`
	DamagePolicies     []BattleDamagePolicy `json:"damage_policies"`
	ArmorPolicies      []BattleArmorPolicy  `json:"armor_policies"`
}

// BattleAllowance pays one selected ordinary action from a canonical Resource.
// Its balance is cleared at the actor's turn boundaries, never a second counter.
type BattleAllowance struct {
	Resource ResourceID  `json:"resource"`
	Weapon   bool        `json:"weapon"`
	Actions  []AbilityID `json:"actions,omitempty"`
}

// LocationSpan is an authored upper bound on separation in GridUnit. It does
// not supply exact positions, visibility, or a character's movement allowance.
type LocationSpan struct {
	Location LocationID `json:"location"`
	Maximum  int64      `json:"maximum"`
}

// TravelRoute is immutable authored topology. Current party membership and
// locations remain relationship/Character facts, not a second route state.
type TravelRoute struct {
	From            LocationID `json:"from"`
	To              LocationID `json:"to"`
	Ticks           int64      `json:"ticks"`
	RequiresVictory bool       `json:"requires_victory"`
}
type BattleRollPolicy struct {
	Condition ConditionTypeID `json:"condition"`
	On        string          `json:"on"`        // attacker or target
	Direction string          `json:"direction"` // advantage or disadvantage
}
type BattleDamagePolicy struct {
	Condition ConditionTypeID `json:"condition"`
	Kind      string          `json:"kind"`
	Treatment string          `json:"treatment"` // resistance, vulnerability or immunity
}
type BattleArmorPolicy struct {
	Definition EquipmentDefinitionID `json:"definition"`
	Mode       EquipmentModeID       `json:"mode"`
	Training   ConditionTypeID       `json:"training"`
	AttackStat StatID                `json:"attack_stat"`
}

// MeleeProfile enables a described attack for this bounded open-grid executor.
// The controlling stat is authored, never guessed from a recipe's first term.
type MeleeProfile struct {
	Reach int64  `json:"reach"`
	Stat  StatID `json:"stat"`
}
type BattlePosition struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}
type BattleAdmission struct {
	Character  CharacterID      `json:"character"`
	Side       BattleSide       `json:"side"`
	Controller BattleController `json:"controller"`
	Position   BattlePosition   `json:"position"`
	TieRank    int              `json:"tie_rank"`
	Initiative *int64           `json:"initiative,omitempty"`
}
type BattleInitiative struct {
	Face        int64  `json:"face"`
	Modifier    int64  `json:"modifier"`
	Total       int64  `json:"total"`
	CallsBefore uint64 `json:"calls_before"`
	CallsAfter  uint64 `json:"calls_after"`
}
type BattleParticipant struct {
	Admission BattleAdmission  `json:"admission"`
	Result    BattleInitiative `json:"result"`
}
type BattleEncounter struct {
	Ammunition []ActorAmmunition   `json:"ammunition,omitempty"`
	ID         EncounterID         `json:"id"`
	Location   LocationID          `json:"location"`
	Roster     []BattleParticipant `json:"roster"` // initiative order, not party membership
	Status     EncounterStatus     `json:"status"`
	Round      uint64              `json:"round"`
	Cursor     int                 `json:"cursor"`
	ActionUsed bool                `json:"action_used"`
}
type BattleState struct {
	Random     RandomState       `json:"random"`
	Encounters []BattleEncounter `json:"encounters"`
	Life       []BattleLife      `json:"life"`
}
type BattleLife struct {
	Character       CharacterID    `json:"character"`
	Outcome         LifeOutcome    `json:"outcome"`
	AppliedRevision Revision       `json:"applied_revision"`
	Cause           ConditionCause `json:"cause"`
}
type LifeChange struct {
	Before *BattleLife `json:"before"`
	After  *BattleLife `json:"after"`
}
type BeginBattle struct {
	ID           EncounterID       `json:"id"`
	Location     LocationID        `json:"location"`
	Participants []BattleAdmission `json:"participants"`
}
type BattleWindow struct {
	Encounter EncounterID `json:"encounter"`
	Round     uint64      `json:"round"`
	Cursor    int         `json:"cursor"`
	Actor     CharacterID `json:"actor"`
}
type HumanAttackRolls struct {
	Attack []int64 `json:"attack"`
	Damage []int64 `json:"damage"`
}
type WeaponAttack struct {
	BattleWindow
	Ammunition   ItemID                 `json:"ammunition,omitempty"`
	HitCondition *WeaponConditionChoice `json:"hit_condition,omitempty"`
	Allowance    ResourceID             `json:"allowance,omitempty"`
	Grant        EquipmentGrantID       `json:"grant"`
	Target       CharacterID            `json:"target"`
	FinishTurn   bool                   `json:"finish_turn"`
	Human        *HumanAttackRolls      `json:"human,omitempty"`
}
type EndBattleTurn struct{ BattleWindow }
type BattleRollCause struct {
	Rule      string              `json:"rule,omitempty"`
	Character CharacterID         `json:"character,omitempty"`
	Condition ConditionInstanceID `json:"condition,omitempty"`
	Equipment *EquipmentOrigin    `json:"equipment,omitempty"`
	Direction string              `json:"direction"`
}
type BattleDamageCause struct {
	Condition ConditionInstanceID `json:"condition"`
	Treatment string              `json:"treatment"`
}
type WeaponRollResult struct {
	Source       BattleController    `json:"source"`
	Attack       []int64             `json:"attack"`
	Selected     int64               `json:"selected"`
	Modifier     int64               `json:"modifier"`
	Total        int64               `json:"total"`
	Defense      int64               `json:"defense"`
	Hit          bool                `json:"hit"`
	Critical     bool                `json:"critical"`
	Causes       []BattleRollCause   `json:"causes"`
	Damage       []int64             `json:"damage"`
	DamageBonus  int64               `json:"damage_bonus"`
	RawDamage    string              `json:"raw_damage"`
	DamageKind   string              `json:"damage_kind"`
	DamageCauses []BattleDamageCause `json:"damage_causes"`
	Requested    int64               `json:"requested"`
	Actual       string              `json:"actual"` // exact difference can span the int64 resource domain
	CallsBefore  uint64              `json:"calls_before"`
	CallsAfter   uint64              `json:"calls_after"`
}
type BattleStartedResult struct {
	Intent BeginBattle     `json:"intent"`
	After  BattleEncounter `json:"after"`
}
type WeaponAttackResult struct {
	Intent WeaponAttack     `json:"intent"`
	Grant  AttackGrant      `json:"grant"`
	Roll   WeaponRollResult `json:"roll"`
	Before BattleEncounter  `json:"before"`
	After  BattleEncounter  `json:"after"`
}
type BattleTurnResult struct {
	Intent EndBattleTurn   `json:"intent"`
	Before BattleEncounter `json:"before"`
	After  BattleEncounter `json:"after"`
}

// BattleActionResult belongs to the terminal AbilityInvoked event. The original
// invocation remains the request/replay intent; its body has no child commits.
type BattleActionResult struct {
	Before BattleEncounter `json:"before"`
	After  BattleEncounter `json:"after"`
}

const (
	BattleStarted        EventKind = "BattleStarted"
	WeaponAttackResolved EventKind = "WeaponAttackResolved"
	BattleTurnEnded      EventKind = "BattleTurnEnded"
)

// Complete means input is complete, never that an outcome has committed.
type BattleInputRequirements struct {
	CheckBoost  *CheckBoostNeed     `json:"check_boost,omitempty"`
	Condition   *ConditionInputNeed `json:"condition,omitempty"`
	AbilityStep *int                `json:"ability_step,omitempty"`
	Revision    Revision            `json:"revision"`
	Stage       string              `json:"stage"`
	Count       int                 `json:"count"`
	Sides       int64               `json:"sides"`
	Provisional *WeaponRollResult   `json:"provisional,omitempty"`
}

func cloneBegin(v *BeginBattle) *BeginBattle {
	if v == nil {
		return nil
	}
	out := *v
	if v.Participants != nil {
		out.Participants = append([]BattleAdmission{}, v.Participants...)
		for i := range out.Participants {
			out.Participants[i].Initiative = copyPtr(v.Participants[i].Initiative)
		}
	}
	return &out
}
func cloneWeaponAttack(v *WeaponAttack) *WeaponAttack {
	if v == nil {
		return nil
	}
	out := *v
	out.HitCondition = copyPtr(v.HitCondition)
	if v.Human != nil {
		out.Human = &HumanAttackRolls{Attack: cloneFaces(v.Human.Attack), Damage: cloneFaces(v.Human.Damage)}
	}
	return &out
}
func cloneFaces(v []int64) []int64 {
	if v == nil {
		return nil
	}
	return append([]int64{}, v...)
}
func cloneBattleLife(v *BattleLife) *BattleLife {
	if v == nil {
		return nil
	}
	out := *v
	out.Cause = *cloneConditionCause(&v.Cause)
	return &out
}
func cloneLifeChange(v *LifeChange) *LifeChange {
	if v == nil {
		return nil
	}
	return &LifeChange{cloneBattleLife(v.Before), cloneBattleLife(v.After)}
}
func cloneBattleEncounter(v BattleEncounter) BattleEncounter {
	v.Ammunition = cloneActorAmmunition(v.Ammunition)
	if v.Roster != nil {
		out := make([]BattleParticipant, len(v.Roster))
		copy(out, v.Roster)
		for i := range out {
			out[i].Admission.Initiative = copyPtr(out[i].Admission.Initiative)
		}
		v.Roster = out
	}
	return v
}
func cloneBattle(v *Battle) *Battle {
	if v == nil {
		return nil
	}
	out := *v
	out.Rules.Ranged = cloneRangedPolicy(v.Rules.Ranged)
	out.Rules.Actions = append([]AbilityID(nil), v.Rules.Actions...)
	out.Rules.TurnResources = append([]ResourceID(nil), v.Rules.TurnResources...)
	out.Rules.Allowances = append([]BattleAllowance(nil), v.Rules.Allowances...)
	out.Rules.HitConditions = append([]WeaponHitCondition(nil), v.Rules.HitConditions...)
	for i := range out.Rules.Allowances {
		out.Rules.Allowances[i].Actions = append([]AbilityID(nil), v.Rules.Allowances[i].Actions...)
	}
	out.Rules.EncounterDefinitions = append([]BattleEncounterDefinition(nil), v.Rules.EncounterDefinitions...)
	for i, d := range out.Rules.EncounterDefinitions {
		out.Rules.EncounterDefinitions[i] = cloneBattleEncounterDefinition(d)
	}
	out.Rules.LocationSpans = append([]LocationSpan(nil), v.Rules.LocationSpans...)
	out.Rules.ConditionChecks = append([]ConditionCheckPolicy(nil), v.Rules.ConditionChecks...)
	out.Rules.RollPolicies = append([]BattleRollPolicy(nil), v.Rules.RollPolicies...)
	out.Rules.Routes = append([]TravelRoute(nil), v.Rules.Routes...)
	out.Rules.DamagePolicies = append([]BattleDamagePolicy(nil), v.Rules.DamagePolicies...)
	out.Rules.ArmorPolicies = append([]BattleArmorPolicy(nil), v.Rules.ArmorPolicies...)
	out.State.Random.Bytes = append([]byte(nil), v.State.Random.Bytes...)
	if v.State.Encounters != nil {
		out.State.Encounters = make([]BattleEncounter, len(v.State.Encounters))
		for i, e := range v.State.Encounters {
			out.State.Encounters[i] = cloneBattleEncounter(e)
		}
	}
	if v.State.Life != nil {
		out.State.Life = make([]BattleLife, len(v.State.Life))
		for i, l := range v.State.Life {
			out.State.Life[i] = *cloneBattleLife(&l)
		}
	}
	return &out
}
func cloneAttackGrant(v AttackGrant) AttackGrant {
	v.Ranged = copyPtr(v.Ranged)
	v.Melee = copyPtr(v.Melee)
	v.HitRecipe = cloneRecipeExplanation(v.HitRecipe)
	v.BonusRecipe = cloneRecipeExplanation(v.BonusRecipe)
	return v
}
func cloneRecipeExplanation(v RecipeExplanation) RecipeExplanation {
	v.Origin = copyPtr(v.Origin)
	if v.Terms != nil {
		v.Terms = append([]TermExplanation{}, v.Terms...)
		for i := range v.Terms {
			v.Terms[i].Term = cloneStatTerm(v.Terms[i].Term)
		}
	}
	return v
}
func cloneWeaponRoll(v WeaponRollResult) WeaponRollResult {
	v.Attack = cloneFaces(v.Attack)
	v.Damage = cloneFaces(v.Damage)
	if v.Causes != nil {
		v.Causes = append([]BattleRollCause{}, v.Causes...)
		for i := range v.Causes {
			v.Causes[i].Equipment = copyPtr(v.Causes[i].Equipment)
		}
	}
	if v.DamageCauses != nil {
		v.DamageCauses = append([]BattleDamageCause{}, v.DamageCauses...)
	}
	return v
}
