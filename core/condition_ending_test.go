package core_test

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"reflect"
	"testing"
)

func endingFixture() core.Blueprint {
	b := maintenanceFixture()
	b.ConditionDefinitions[0].EndsOnCondition = gearPtr(core.ConditionTypeID("blocked"))
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "blocked", Instances: core.Coexist})
	return b
}

func blockOp(id core.ConditionInstanceID) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: "blocked", Target: "a"}}
}

func TestConditionEndingGroupsReceiptsAndDetachment(t *testing.T) {
	b := endingFixture()
	w := gearWorld(t, b)
	first := gearExecute(t, w, "maintain", anchorOp("a-root", "a"), dependentOp("a-child", "a-root", "b", 2), anchorOp("c-root", "c"), dependentOp("c-child", "c-root", "b", 3))
	assertDefense(t, w, "b", 17)
	before := w.ExportCheckpoint()
	if _, err := w.Execute(gearRequest(w, "late-failure", blockOp("fail"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 11}})); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("late failure published an ended group", err)
	}
	ended := gearExecute(t, w, "block", blockOp("block-a"))
	if len(ended.Events) != 3 || ended.Events[0].Condition.Before.ID != "a-child" || ended.Events[1].Condition.Before.ID != "a-root" || *ended.Events[1].Condition.EndingCondition != "block-a" || ended.Events[2].Kind != core.ConditionApplied {
		t.Fatal("group order/ending provenance", ended)
	}
	assertDefense(t, w, "b", 15)
	before = w.ExportCheckpoint()
	if _, err := w.Execute(gearRequest(w, "still-blocked", anchorOp("new-root", "a"))); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("blocked application published", err)
	}
	gearExecute(t, w, "clear", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "block-a", Target: "a"}}, core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}})
	assertDefense(t, w, "b", 15) // Removing the blocker/healing cannot resurrect A.
	before = w.ExportCheckpoint()
	if _, err := w.Execute(gearRequest(w, "reuse", anchorOp("a-root", "a"))); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("ended identity reused", err)
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "ending", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("archive", err)
	}
	for _, old := range []core.Batch{first, ended} {
		got, err := loaded.World.Execute(old.Request)
		if err != nil || !reflect.DeepEqual(old, got) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
			t.Fatal("historical retry", err)
		}
	}
	bad := w.History()
	bad[1].Events[1].Condition.EndingCondition = nil
	if partial, err := core.Replay(before.Genesis, bad); err == nil || partial != nil {
		t.Fatal("forged ending accepted")
	}
	*ended.Events[1].Condition.EndingCondition = "forged"
	*b.ConditionDefinitions[0].EndsOnCondition = "forged"
	snap := w.Snapshot()
	for _, d := range snap.ConditionDefinitions {
		if d.ID == "maintained" {
			*d.EndsOnCondition = "forged"
		}
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested ending alias")
	}
}

func TestConditionEndingOrderingAndLimits(t *testing.T) {
	for _, variant := range []string{"mutual", "cascade-blocker", "removed-anchor"} {
		t.Run(variant, func(t *testing.T) {
			b := endingFixture()
			if variant == "mutual" {
				b.ConditionDefinitions[2].EndsOnCondition = gearPtr(core.ConditionTypeID("maintained"))
			}
			if variant == "cascade-blocker" {
				b.ConditionDefinitions[2].EndsOnCondition = gearPtr(core.ConditionTypeID("boon"))
			}
			w := gearWorld(t, b)
			gearExecute(t, w, "initial", anchorOp("root", "a"), dependentOp("child", "root", "a", 2))
			before := w.ExportCheckpoint()
			op := blockOp("incoming")
			if variant == "removed-anchor" {
				op.ApplyCondition.Anchor = gearPtr(core.ConditionInstanceID("root"))
			}
			if _, err := w.Execute(gearRequest(w, "invalid", op)); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("removal hid invalid incoming state", err)
			}
		})
	}
	for _, variant := range []string{"missing", "self", "genesis-conflict"} {
		b := endingFixture()
		switch variant {
		case "missing":
			b.ConditionDefinitions[0].EndsOnCondition = gearPtr(core.ConditionTypeID("missing"))
		case "self":
			b.ConditionDefinitions[0].EndsOnCondition = gearPtr(core.ConditionTypeID("maintained"))
		case "genesis-conflict":
			b.Conditions = []core.ConditionInstance{{ID: "root", Type: "maintained", Target: "a"}, {ID: "blocker", Type: "blocked", Target: "a"}}
		}
		if w, err := core.NewWorld(b); err == nil || w != nil {
			t.Fatal("invalid ending genesis accepted", variant)
		}
	}
	b := endingFixture()
	b.Conditions = []core.ConditionInstance{{ID: "root", Type: "maintained", Target: "a"}}
	for i := 0; i < 127; i++ {
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprintf("child:%03d", i)), Type: "boon", Target: "b", Anchor: gearPtr(core.ConditionInstanceID("root")), Magnitude: gearPtr(int64(1))})
	}
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	if _, err := w.Execute(gearRequest(w, "over", blockOp("incoming"))); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("ending work overflow published", err)
	}
}
