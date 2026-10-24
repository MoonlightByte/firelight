package core_test

import (
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

func TestConditionSelectionWholeInstanceAndIndependentTypes(t *testing.T) {
	b := selectionFixture()
	for i := range b.Characters {
		b.Characters[i].Stats["resolve"] = 10
	}
	b.ConditionDefinitions[1].Modifiers = append(b.ConditionDefinitions[1].Modifiers, core.ConditionModifier{ID: "resolve", Stat: "resolve", Magnitude: true})
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "other", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "defense", Stat: "defense", Fixed: gearPtr(int64(3))}}})
	w := gearWorld(t, b)
	gearExecute(t, w, "overlap",
		core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "older", Type: "boon", Target: "b", Magnitude: gearPtr(int64(5))}},
		core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "newer", Type: "boon", Target: "b", Magnitude: gearPtr(int64(2))}},
		core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "different", Type: "other", Target: "b"}})
	assertDefense(t, w, "b", 17) // base10 + shield2 + latest2 + unrelated3
	x, err := w.Explain("b", "resolve")
	if err != nil || x.Effective != 12 || len(x.Suppressed) != 1 || x.Suppressed[0].By != "newer" {
		t.Fatal("whole instance selected, not strongest/per-stat", x, err)
	}

	// Suppression cannot hide an illegal revealed value from final validation.
	b = selectionFixture()
	b.Effects = nil
	b.Characters[1].Stats["defense"] = math.MaxInt64
	b.Conditions = []core.ConditionInstance{
		{ID: "a", Type: "boon", Target: "b", Magnitude: gearPtr(int64(1))},
		{ID: "z", Type: "boon", Target: "b", Magnitude: gearPtr(int64(-1))},
	}
	w = gearWorld(t, b)
	assertDefense(t, w, "b", math.MaxInt64-1)
	battleReject(t, w, gearRequest(w, "would-overflow", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "z", Target: "b"}}))
	assertDefense(t, w, "b", math.MaxInt64-1)
}

func TestConditionSelectionCheckUsesSameEffectiveValue(t *testing.T) {
	b := conditionCheckFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "save-boon", Instances: core.Coexist, ModifierSelection: core.LatestModifiers, Modifiers: []core.ConditionModifier{{ID: "save", Stat: "save", Magnitude: true}}})
	b.Conditions = append(b.Conditions,
		core.ConditionInstance{ID: "old-save", Type: "save-boon", Target: "a", Magnitude: gearPtr(int64(5))},
		core.ConditionInstance{ID: "new-save", Type: "save-boon", Target: "a", Magnitude: gearPtr(int64(1))})
	// Genesis ties use ID order, so use explicit a/z IDs for the intended winner.
	b.Conditions[len(b.Conditions)-2].ID = "a-save"
	b.Conditions[len(b.Conditions)-1].ID = "z-save"
	w := gearWorld(t, b)
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "b"}, conditionDamage("a", 1))
	if err != nil || need.Condition == nil || need.Condition.Modifier != 3 {
		t.Fatal("save preview bypassed selection", need, err)
	}
	r := battleExec(t, w, "save", "b", conditionDamage("a", 1, 6))
	if r.Events[0].ConditionCheck.Roll.Success {
		t.Fatal("base2 + selected1 + face6 must fail DC10", r)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("selected check replay", err)
	}
}

func selectionFixture() core.Blueprint {
	b := maintenanceFixture()
	b.ConditionDefinitions[1].ModifierSelection = core.LatestModifiers
	return b
}

func TestConditionSelectionOverlapRemovalAndHistoricalTruth(t *testing.T) {
	for _, removeWinner := range []bool{false, true} {
		b := selectionFixture()
		w := gearWorld(t, b)
		first := gearExecute(t, w, "z-earlier", anchorOp("z-root", "a"), dependentOp("z-old", "z-root", "b", 2))
		gearExecute(t, w, "a-later", anchorOp("a-root", "c"), dependentOp("a-new", "a-root", "b", 2))
		assertDefense(t, w, "b", 14)
		x, err := w.Explain("b", "defense")
		if err != nil || len(x.Suppressed) != 1 || x.Suppressed[0].ConditionOrigin.Instance != "z-old" || x.Suppressed[0].By != "a-new" || x.Suppressed[0].Amount != 2 {
			t.Fatal("complete suppression provenance", x, err)
		}
		status, err := w.CharacterStatus("b")
		if err != nil || len(status.Conditions) != 2 || len(status.ActiveEffects) != 2 {
			t.Fatal("presence versus active equipment/Condition effects", status, err)
		}
		before := w.ExportCheckpoint()
		x.Suppressed[0].ConditionOrigin.Instance = "forged"
		x.Suppressed[0].By = "forged"
		status.Stats[0].Suppressed[0].ConditionOrigin.Instance = "forged"
		if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatal("suppression projection alias")
		}
		root, target, remaining := core.ConditionInstanceID("z-root"), core.CharacterID("a"), core.ConditionInstanceID("a-new")
		if removeWinner {
			root, target, remaining = "a-root", "c", "z-old"
		}
		gearExecute(t, w, "remove-one", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: root, Target: target}})
		assertDefense(t, w, "b", 14)
		x, _ = w.Explain("b", "defense")
		if len(x.Suppressed) != 0 {
			t.Fatal("removed contribution still suppresses", x)
		}
		found := false
		for _, c := range x.Contributions {
			if c.ConditionOrigin != nil {
				found = c.ConditionOrigin.Instance == remaining
			}
		}
		if !found {
			t.Fatal("wrong remaining contributor", x)
		}
		if removeWinner {
			root, target = "z-root", "a"
		} else {
			root, target = "a-root", "c"
		}
		gearExecute(t, w, "remove-rest", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: root, Target: target}})
		assertDefense(t, w, "b", 12)
		rebuilt, err := core.Replay(b, w.History())
		if err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("selection replay", err)
		}
		before = rebuilt.ExportCheckpoint()
		retry, err := rebuilt.Execute(first.Request)
		if err != nil || !reflect.DeepEqual(retry, first) || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
			t.Fatal("selection retry reapplied expired contribution", err)
		}
	}
}

func TestConditionSelectionCausalOrderAcrossOperationBodyAndBranch(t *testing.T) {
	for _, mode := range []string{"operation", "body", "branch"} {
		b := battleFixture()
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "select", Instances: core.Coexist, ModifierSelection: core.LatestModifiers, Modifiers: []core.ConditionModifier{{ID: "bonus", Stat: "defense", Magnitude: true}}})
		var ops []core.Operation
		var steps []core.AbilityStep
		for i := 0; i < 13; i++ {
			id, amount, target := core.ConditionInstanceID(fmt.Sprintf("instance:%d", i)), int64(i), core.CharacterID("b")
			ops = append(ops, core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: "select", Target: target, Magnitude: &amount}})
			value := core.AbilityOperand{Literal: &core.AbilityValue{Integer: &amount}}
			steps = append(steps, core.AbilityStep{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Literal: &core.AbilityValue{ConditionID: &id}}, Type: "select", Target: core.AbilityOperand{Literal: &core.AbilityValue{Character: &target}}, Magnitude: &value}})
		}
		if mode != "operation" {
			if mode == "branch" {
				var branch []core.AbilityPrimitive
				for _, step := range steps {
					branch = append(branch, step.Primitive())
				}
				steps = []core.AbilityStep{{Check: &core.CheckTemplate{ID: "always", Sides: 2, Modifier: core.CheckValue{Literal: gearPtr(int64(0))}, Threshold: core.CheckValue{Literal: gearPtr(int64(0))}, Success: branch}}}
			}
			b.AbilityDefinitions = []core.AbilityDefinition{{ID: "select-many", Action: &core.ActionPolicy{Actor: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}, Phase: core.Exploration}, Steps: steps}}
			b.Battle.Rules.Actions = []core.AbilityID{"select-many"}
			ops = []core.Operation{{Invoke: &core.InvokeAbility{Ability: "select-many"}}}
		}
		w := gearWorld(t, b)
		gearExecute(t, w, "same-request", ops...)
		x, err := w.Explain("b", "defense")
		if err != nil || len(x.Suppressed) != 12 {
			t.Fatal(mode, x, err)
		}
		for _, suppressed := range x.Suppressed {
			if suppressed.By != "instance:12" {
				t.Fatal("numeric causal order", mode, suppressed)
			}
		}
		rebuilt, err := core.Replay(b, w.History())
		if err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("causal replay", mode, err)
		}
	}
}

func TestConditionSelectionGenesisTieExpiryAndRollback(t *testing.T) {
	b := selectionFixture()
	b.Conditions = []core.ConditionInstance{{ID: "a", Type: "boon", Target: "b", Magnitude: gearPtr(int64(2))}, {ID: "z", Type: "boon", Target: "b", Magnitude: gearPtr(int64(3))}}
	w := gearWorld(t, b)
	assertDefense(t, w, "b", 15)
	x, _ := w.Explain("b", "defense")
	if len(x.Suppressed) != 1 || x.Suppressed[0].By != "z" {
		t.Fatal("explicit genesis ID tie", x)
	}
	b = selectionFixture()
	w = gearWorld(t, b)
	gearExecute(t, w, "older", anchorOp("old", "a"), dependentOp("old-boon", "old", "b", 2))
	root := anchorOp("new", "c")
	root.ApplyCondition.ClockDuration = gearPtr(uint64(1))
	gearExecute(t, w, "newer", root, dependentOp("new-boon", "new", "b", 3))
	assertDefense(t, w, "b", 15)
	before := w.ExportCheckpoint()
	battleReject(t, w, gearRequest(w, "late", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "late-boon", Type: "boon", Target: "b", Magnitude: gearPtr(int64(9))}}, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 99}}))
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("late selection leaked")
	}
	gearExecute(t, w, "expire", core.Operation{AdvanceTime: &core.AdvanceTime{Ticks: 1}})
	assertDefense(t, w, "b", 14)
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("expiry selection replay", err)
	}
	b.ConditionDefinitions[1].ModifierSelection = "unknown"
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("unknown selection")
	}
}
