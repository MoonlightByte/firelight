package compiler

// These syntax nodes retain literals and positions. They contain no runtime
// objects and cannot mutate a world. Lowering is a separate phase.
type namedNode struct{ ID, Name token }
type statNode struct{ ID, Value token }
type resourceNode struct{ ID, Current, Minimum, Maximum token }
type bufferNode struct{ Pool, Target token }

// traitNode is `resist|vulnerable|immune "<kind>";` in a character.
type traitNode struct{ Key, Kind token }
type characterNode struct {
	Buffers []bufferNode
	Traits  []traitNode
	namedNode
	Location  token
	Stats     []statNode
	Resources []resourceNode
}
type itemNode struct {
	namedNode
	From                *token
	Custody             *refNode
	Container           *token
	Description         *token
	Owner, Wearer       *token
	Quantity            *token
	Recoverable         *token
	Charges, ChargesMax *token
	ChargesSince        *token
	ChargesSeed         []token
	Definition, Mode    *token
}
type refNode struct{ Kind, ID token }

// recoveryNode is the rules block's `recovery <numerator> of <denominator>;`.
type recoveryNode struct{ Start, Numerator, Denominator token }
type effectNode struct {
	ID                         token
	Source                     refNode
	Mode, Target, Stat, Amount token
}
type relationNode struct {
	From     refNode
	Kind     token
	To       refNode
	Strength token
}
type worldNode struct {
	ItemDefinitions          []itemDefinitionNode
	RelationDefinitions      []relationDefinitionNode
	Battle                   *battleNode
	Map                      *mapNode
	Occupants                []occupantNode
	Quests                   []questNode
	Journal                  []journalNode
	JournalNext              *token
	Attacks                  *attacksNode
	DamageTraits             []damageTraitNode
	Clock                    *clockNode
	Dice                     *diceNode
	ConditionChecks          []conditionCheckNode
	Equipment                *equipmentNode
	Adventure                *adventureNode
	Abilities                []abilityNode
	ConditionDefinitions     []conditionDefinitionNode
	Conditions               []conditionNode
	Recovery                 *recoveryNode
	Transfer, Wear, RulesEnd token
	Locations                []namedNode
	Characters               []characterNode
	Items                    []itemNode
	Effects                  []effectNode
	Relations                []relationNode
}
type actionNode struct {
	Occupant                                  *occupantNode
	OccupantAction                            *occupantActionNode
	MapEdit                                   *mapEditNode
	QuestAction                               *questActionNode
	JournalAction                             *journalActionNode
	Attack                                    *attackNode
	Save                                      *saveNode
	Split                                     *stockNode
	Place                                     *placeNode
	Battle                                    *battleActionNode
	Item                                      *itemNode
	Character                                 *characterNode
	Mode                                      *token
	Invocation                                *invocationNode
	Condition                                 *conditionNode
	StatCheck                                 *statCheckNode
	DamageFaces                               []damageFacesNode
	DamageKind                                *token
	Stat                                      *token
	Recoverable                               *token
	Verb, Subject, From, To, Resource, Amount token
	Effect                                    effectNode
	Relation                                  relationNode
}
