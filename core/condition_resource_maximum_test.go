package core_test

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"testing"
)

// Battle life is driven by the health minimum, so a stored maximum modifier on
// Battle health is refused at application and at genesis.
func TestConditionResourceMaximumRefusesBattleHealth(t *testing.T) {
	five := int64(5)
	aid := core.ConditionDefinition{ID: "spell:aid", Instances: core.Unique, Modifiers: []core.ConditionModifier{{ID: "hp", ResourceMaximum: "hp", Fixed: &five}}}
	b := battleFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions, aid)
	w := gearWorld(t, b)
	_, err := w.Execute(core.Request{ID: "aid", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{{ApplyCondition: &core.ApplyCondition{Instance: "aid:a", Type: "spell:aid", Target: "a"}}}})
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_RULE" || f.Field != "resource_maximum" {
		t.Fatalf("apply on Battle health: %v", err)
	}
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "aid:a", Type: "spell:aid", Target: "a"})
	if _, err := core.NewWorld(b); !errors.As(err, &f) || f.Code != "E_RULE" {
		t.Fatalf("genesis on Battle health: %v", err)
	}
}
