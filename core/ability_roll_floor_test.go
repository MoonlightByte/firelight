package core_test

// 7b-engine commit A (kit agents/kw/plan-7b-engine-r0.md): a heal floor on a
// rolled Resource step.

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func flooredCareFixture(minimum int64) core.Blueprint {
	b := rolledCareFixture()
	r := b.AbilityDefinitions[0].Steps[1].RollResource
	r.Count, r.Bonus, r.Minimum = 1, core.ScalarRecipe{Base: -5}, minimum
	return b
}

func resourceRoll(t *testing.T, r core.Batch) *core.ResourceRollResult {
	t.Helper()
	for _, e := range r.Events {
		if e.ResourceRoll != nil {
			return e.ResourceRoll
		}
	}
	t.Fatal("no ResourceRollResolved event", r)
	return nil
}

func TestRolledHealFloorRaisesOnlyALowerTotal(t *testing.T) {
	for _, tc := range []struct {
		name               string
		minimum, face      int64
		requested, hpAfter int64
	}{
		{"floor raises a clamped zero", 1, 1, 1, 6},
		{"floor raises a positive total below it", 3, 7, 3, 8},
		{"floor leaves a higher total", 2, 8, 3, 8},
		{"no floor keeps the paid zero", 0, 1, 0, 5},
	} {
		w := gearWorld(t, flooredCareFixture(tc.minimum))
		r := battleExec(t, w, tc.name, "a", rolledCare("a", "a", tc.face))
		roll := resourceRoll(t, r)
		if roll.Requested != tc.requested || roll.Minimum != tc.minimum || w.Snapshot().Characters[0].Resources["hp"].Current != tc.hpAfter || w.Snapshot().Characters[0].Resources["slot:1"].Current != 1 {
			t.Fatal(tc.name, roll, w.Snapshot().Characters[0].Resources)
		}
		if rebuilt, err := core.Replay(flooredCareFixture(tc.minimum), w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("floor replay", tc.name, err)
		}
	}
}

func TestRolledFloorIsRefusedOnDamageAndBelowOne(t *testing.T) {
	b := flooredCareFixture(1)
	b.AbilityDefinitions[0].Steps[1].RollResource.Kind = "damage"
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("damage floor accepted")
	}
	if _, err := core.NewWorld(flooredCareFixture(-1)); err == nil {
		t.Fatal("negative heal floor accepted")
	}
	b = flooredCareFixture(0)
	b.AbilityDefinitions[0].Steps[1].RollResource.Kind = "damage"
	if _, err := core.NewWorld(b); err != nil {
		t.Fatal("damage without a floor refused", err)
	}
}
