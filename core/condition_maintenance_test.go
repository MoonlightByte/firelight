package core_test

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"reflect"
	"testing"
)

func maintenanceFixture() core.Blueprint {
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}}, Clock: &core.Clock{Unit: "seconds"}}
	for _, id := range []core.CharacterID{"a", "b", "c"} {
		b.Characters = append(b.Characters, core.Character{ID: id, Name: string(id), Location: "room", Stats: map[core.StatID]int64{"defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 10, Maximum: 10}}})
	}
	b.Items = []core.Item{{ID: "shield", Name: "Shield", Owner: "b", Wearer: "b"}}
	b.Effects = []core.Effect{{ID: "shield-bonus", Source: core.Ref{Kind: core.ItemKind, ID: "shield"}, Mode: core.WhileWorn, Target: core.Ref{Kind: core.ItemKind, ID: "shield"}, Stat: "defense", Amount: 2}}
	b.ConditionDefinitions = []core.ConditionDefinition{
		{ID: "maintained", Instances: core.Replace, EndsAtMinimum: gearPtr(core.ResourceID("hp"))},
		{ID: "boon", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "defense", Stat: "defense", Magnitude: true}}},
	}
	return b
}

func anchorOp(id core.ConditionInstanceID, target core.CharacterID) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: "maintained", Target: target}}
}
func dependentOp(id, anchor core.ConditionInstanceID, target core.CharacterID, amount int64) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: "boon", Target: target, Anchor: gearPtr(anchor), Magnitude: &amount}}
}
func assertDefense(t *testing.T, w *core.World, id core.CharacterID, value int64) {
	t.Helper()
	s, err := w.Explain(id, "defense")
	if err != nil || s.Effective != value {
		t.Fatalf("%s defense want%d got%+v %v", id, value, s, err)
	}
}

func TestConditionMaintenanceReplacementRemovalReplay(t *testing.T) {
	b := maintenanceFixture()
	w := gearWorld(t, b)
	first := gearExecute(t, w, "first", anchorOp("a-root", "a"), dependentOp("a-b", "a-root", "b", 2), dependentOp("a-c", "a-root", "c", 2), anchorOp("b-root", "b"), dependentOp("b-b", "b-root", "b", 3))
	assertDefense(t, w, "b", 17)
	assertDefense(t, w, "c", 12)
	replaced := gearExecute(t, w, "replace", anchorOp("a-new", "a"), dependentOp("a-new-b", "a-new", "b", 2))
	if len(replaced.Events) != 5 || replaced.Events[0].Condition.Before.ID != "a-b" || replaced.Events[1].Condition.Before.ID != "a-c" || replaced.Events[2].Condition.Before.ID != "a-root" || *replaced.Events[2].Condition.ReplacedBy != "a-new" || replaced.Events[3].Kind != core.ConditionApplied {
		t.Fatal("replacement group order/provenance", replaced)
	}
	assertDefense(t, w, "b", 17)
	assertDefense(t, w, "c", 10)
	removed := gearExecute(t, w, "remove", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "a-new", Target: "a"}})
	if len(removed.Events) != 2 || *removed.Events[0].Condition.AnchorRemoved != "a-new" || removed.Events[1].Condition.Before.ID != "a-new" {
		t.Fatal("explicit root terminal", removed)
	}
	assertDefense(t, w, "b", 15)
	before := w.ExportCheckpoint()
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("replay", err)
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "maintained", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, world := range []*core.World{w, rebuilt, loaded.World} {
		for _, old := range []core.Batch{first, replaced, removed} {
			r, err := world.Execute(old.Request)
			if err != nil || !reflect.DeepEqual(r, old) || !reflect.DeepEqual(before, world.ExportCheckpoint()) {
				t.Fatal("retry reapplied group", err)
			}
		}
	}
	// Both canonical anchor references and nested receipt annotations detach.
	*first.Request.Operations[1].ApplyCondition.Anchor = "forged"
	*replaced.Events[0].Condition.AnchorRemoved = "forged"
	*removed.Events[0].Condition.Before.Anchor = "forged"
	*b.ConditionDefinitions[0].EndsAtMinimum = "forged"
	snap := w.Snapshot()
	for _, c := range snap.Conditions {
		if c.Anchor != nil {
			*c.Anchor = "forged"
		}
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("maintenance alias")
	}
	bad := w.History()
	bad[1].Events[0].Condition.AnchorRemoved = nil
	if partial, err := core.Replay(before.Genesis, bad); err == nil || partial != nil {
		t.Fatal("corrupt cascade accepted")
	}
	bad = w.History()
	bad[1].Events = bad[1].Events[1:]
	if partial, err := core.Replay(before.Genesis, bad); err == nil || partial != nil {
		t.Fatal("missing cascade accepted")
	}
}

func TestConditionMaintenanceFloorExpiryAndRollback(t *testing.T) {
	for _, ending := range []string{"floor", "expiry"} {
		t.Run(ending, func(t *testing.T) {
			b := maintenanceFixture()
			w := gearWorld(t, b)
			root := anchorOp("a-root", "a")
			root.ApplyCondition.ClockDuration = gearPtr(uint64(20))
			child := dependentOp("z-child", "a-root", "b", 2)
			child.ApplyCondition.ClockDuration = gearPtr(uint64(20))
			gearExecute(t, w, "apply", root, child, anchorOp("other-root", "b"), dependentOp("other-child", "other-root", "b", 3))
			before := w.ExportCheckpoint()
			for _, ops := range [][]core.Operation{
				{anchorOp("new", "a"), {Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 11}}},
				{anchorOp("a-root", "a")},
				{dependentOp("missing", "absent", "b", 2)},
				{dependentOp("self", "self", "b", 2)},
				{dependentOp("chain", "z-child", "c", 2)},
			} {
				if _, err := w.Execute(gearRequest(w, "rejected", ops...)); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
					t.Fatal("invalid maintenance published", err)
				}
			}
			var result core.Batch
			if ending == "floor" {
				result = gearExecute(t, w, "end", core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 10}}, core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}})
				if len(result.Events) != 4 || result.Events[2].Kind != core.DamageApplied || *result.Events[1].Condition.MinimumResource != "hp" {
					t.Fatal("debit terminal/release", result)
				}
			} else {
				result = gearExecute(t, w, "end", core.Operation{AdvanceTime: &core.AdvanceTime{Ticks: 20}})
				if len(result.Events) != 3 || result.Events[2].Kind != core.TimeAdvanced || result.Events[1].Condition.ClockExpiration.Deadline != 20 || result.Events[0].Condition.ClockExpiration != nil {
					t.Fatal("expiry root annotation", result)
				}
			}
			assertDefense(t, w, "b", 15)
			if len(w.Snapshot().Conditions) != 2 {
				t.Fatal("unrelated maintainer lost")
			}
			if _, err := core.Replay(b, w.History()); err != nil {
				t.Fatal(err)
			}
		})
	}
	b := maintenanceFixture()
	b.Characters[0].Resources["hp"] = core.Resource{Maximum: 10}
	w := gearWorld(t, b)
	if _, err := w.Execute(gearRequest(w, "floor", anchorOp("root", "a"))); err == nil || w.Revision() != 0 {
		t.Fatal("floor application accepted")
	}
}

func TestConditionMaintenanceWorkBound(t *testing.T) {
	b := maintenanceFixture()
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "root", Type: "maintained", Target: "a"})
	for i := 0; i < 127; i++ {
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprintf("child:%03d", i)), Type: "boon", Target: "b", Anchor: gearPtr(core.ConditionInstanceID("root")), Magnitude: gearPtr(int64(0))})
	}
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	if _, err := w.Execute(gearRequest(w, "over", anchorOp("new", "a"))); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("replacement exceeds128 work")
	}
	r := gearExecute(t, w, "at-bound", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "root", Target: "a"}})
	if len(r.Events) != 128 || len(w.Snapshot().RetiredConditionIDs) != 128 {
		t.Fatal("cascade exact work boundary")
	}
}
