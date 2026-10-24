package nativesave

import (
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// A Condition type's melee attack is Battle semantics, as an item's is.
func TestConditionAttackSemantics(t *testing.T) {
	b := core.Blueprint{Equipment: &core.EquipmentRules{Version: core.EquipmentVersion}}
	if semanticsFor(b) == BattleSemanticsVersion {
		t.Fatal("no attack is Battle semantics")
	}
	b.ConditionDefinitions = []core.ConditionDefinition{{ID: "unarmed", Instances: core.Unique, Attacks: []core.EquipmentAttack{{ID: "strike", Melee: &core.MeleeProfile{Reach: 5, Stat: "strength"}}}}}
	if got := semanticsFor(b); got != BattleSemanticsVersion {
		t.Fatal(got)
	}
}
