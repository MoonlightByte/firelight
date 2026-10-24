package nativesave

import (
	"github.com/MoonlightByte/firelight/core"
	"testing"
)

func TestBlueprintEntityLimitsUseWorldConstants(t *testing.T) {
	for _, tc := range []struct {
		limit  int
		resize func(*core.Blueprint, int)
	}{
		{core.MaxWorldCharacters, func(b *core.Blueprint, n int) { b.Characters = make([]core.Character, n) }},
		{core.MaxWorldItems, func(b *core.Blueprint, n int) { b.Items = make([]core.Item, n) }},
		{core.MaxWorldLocations, func(b *core.Blueprint, n int) { b.Locations = make([]core.Location, n) }},
		{core.MaxWorldConditionDefinitions, func(b *core.Blueprint, n int) { b.ConditionDefinitions = make([]core.ConditionDefinition, n) }},
		{core.MaxWorldConditions, func(b *core.Blueprint, n int) { b.Conditions = make([]core.ConditionInstance, n) }},
	} {
		var b core.Blueprint
		tc.resize(&b, tc.limit)
		if err := validateBlueprintLimits(b); err != nil {
			t.Fatal(err)
		}
		tc.resize(&b, tc.limit+1)
		if err := validateBlueprintLimits(b); err == nil {
			t.Fatal("over-limit accepted")
		}
	}
}
