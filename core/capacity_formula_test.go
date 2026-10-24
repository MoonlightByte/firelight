package core_test

import (
	"bytes"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func capacityFormulaFixture() core.Blueprint {
	b := battleAbilityFixture()
	b.Characters[0].Stats["constitution"] = 12
	b.Characters[1].Resources["hp"] = core.Resource{Current: 5, Maximum: 10}
	b.AbilityDefinitions[0].Action.Phase = core.Exploration
	b.AbilityDefinitions[0].Action.Requirements = nil
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Expand: &core.ExpandTemplate{
		Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp",
		Formula: &core.CapacityFormula{Source: core.AbilityOperand{Parameter: "actor"}, Minimum: 1,
			Recipe: core.ScalarRecipe{Base: 4, Terms: []core.StatTerm{{Stat: "constitution", Offset: -10, Divisor: 2, Ceiling: gearPtr(int64(6))}}}},
	}}}
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "con-gain", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "con", Stat: "constitution", Fixed: gearPtr(int64(1))}}})
	return b
}

func TestCapacityFormulaCandidateProvenanceReplayAndOwnership(t *testing.T) {
	b := capacityFormulaFixture()
	b.Characters[0].Stats["constitution"] = 11
	w := gearWorld(t, b)
	gain := core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "con-1", Type: "con-gain", Target: "a"}}
	invoke := battleInvoke("care", "a", "b")
	gearReject(t, w, "late", gain, invoke, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 4}})
	request := gearRequest(w, "gain", gain, invoke)
	before := w.ExportCheckpoint()
	if err := w.Preview(request); err != nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("formula preview", err)
	}
	r, err := w.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	x := r.Events[1].Capacity
	if x.After != (core.Resource{Current: 5, Maximum: 15}) || x.Requested != 5 || x.Formula.Source != "a" || x.Formula.Raw != 5 || x.Formula.Minimum != 1 || x.Formula.Recipe.Terms[0].Input != 12 || x.Formula.Recipe.Terms[0].Exact != "1" || r.Events[1].AbilityOrigin.Step != 0 {
		t.Fatal("fixed ordered formula oracle", x)
	}
	gearExecute(t, w, "lose-gain", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "con-1", Target: "a"}})
	cp := w.ExportCheckpoint()
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, cp, nativesave.SessionState{Campaign: "formula", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("formula historical retry/replay", err)
	}
	for _, forge := range []func(*core.CapacityChange){
		func(v *core.CapacityChange) { v.Formula.Raw++ },
		func(v *core.CapacityChange) { v.Formula.Source = "b" },
		func(v *core.CapacityChange) { v.Formula.Recipe.Terms[0].Input++ },
		func(v *core.CapacityChange) { v.Formula = nil },
	} {
		history := w.History()
		forge(history[0].Events[1].Capacity)
		if partial, err := core.Replay(cp.Genesis, history); err == nil || partial != nil {
			t.Fatal("forged formula history accepted")
		}
	}
	*b.AbilityDefinitions[0].Steps[0].Expand.Formula.Recipe.Terms[0].Ceiling = 99
	snapshot := w.Snapshot()
	snapshot.AbilityDefinitions[0].Steps[0].Expand.Formula.Source.Parameter = "forged"
	*x.Formula.Recipe.Terms[0].Term.Ceiling = 98
	retry.Events[1].Capacity.Formula.Recipe.Terms[0].Term.Stat = "forged"
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("nested formula alias")
	}
}

func TestCapacityFormulaFloorAndOverflow(t *testing.T) {
	b := capacityFormulaFixture()
	b.Characters[0].Stats["constitution"] = 1
	w := gearWorld(t, b)
	r := gearExecute(t, w, "minimum", battleInvoke("care", "a", "b"))
	x := r.Events[0].Capacity
	if x.Requested != 1 || x.Formula.Raw != -1 || x.After != (core.Resource{Current: 5, Maximum: 11}) {
		t.Fatal("positive floor", x)
	}
	for _, rawOverflow := range []bool{false, true} {
		b := capacityFormulaFixture()
		if rawOverflow {
			b.AbilityDefinitions[0].Steps[0].Expand.Formula.Recipe.Base = math.MaxInt64
		} else {
			b.Characters[1].Resources["hp"] = core.Resource{Current: 5, Maximum: math.MaxInt64}
		}
		w := gearWorld(t, b)
		err := battleReject(t, w, gearRequest(w, "overflow", battleInvoke("care", "a", "b")))
		var fault *core.Fault
		if !errors.As(err, &fault) || fault.Code != "E_OVERFLOW" {
			t.Fatal(err)
		}
	}
}

func TestCapacityFormulaShapeAndBoundSelectors(t *testing.T) {
	for _, mutate := range []func(*core.ExpandTemplate){
		func(v *core.ExpandTemplate) {
			v.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(2))}}
		},
		func(v *core.ExpandTemplate) { v.Formula.Minimum = 0 },
		func(v *core.ExpandTemplate) {
			v.Formula.Source = core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}
		},
		func(v *core.ExpandTemplate) { v.Formula.Recipe.Terms[0].Divisor = 0 },
		func(v *core.ExpandTemplate) { v.Formula.Recipe.Terms = make([]core.StatTerm, 9) },
	} {
		b := capacityFormulaFixture()
		mutate(b.AbilityDefinitions[0].Steps[0].Expand)
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("illegal formula definition accepted")
		}
	}
	b := capacityFormulaFixture()
	b.AbilityDefinitions[0].Steps[0].Expand.Formula.Recipe.Terms[0].Stat = "missing"
	w := gearWorld(t, b)
	err := battleReject(t, w, gearRequest(w, "missing", battleInvoke("care", "a", "b")))
	var ae *core.AbilityError
	if !errors.As(err, &ae) || ae.Parameter != "actor" || ae.Operand != "formula.term.0" {
		t.Fatal("bound formula diagnostic", err)
	}
	b = capacityFormulaFixture()
	primitive := core.AbilityPrimitive{Expand: b.AbilityDefinitions[0].Steps[0].Expand}
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Check: &core.CheckTemplate{ID: "not-supported", Sides: 20, Modifier: core.CheckValue{Literal: gearPtr(int64(0))}, Threshold: core.CheckValue{Literal: gearPtr(int64(0))}, Success: []core.AbilityPrimitive{primitive}}}}
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("formula accepted in unimplemented branch")
	}
}

func TestCapacityFormulaBeforeHumanRollIsPrivate(t *testing.T) {
	b := rolledCareFixture()
	b.Characters[0].Stats["constitution"] = 12
	b.AbilityDefinitions[0].Steps[0] = capacityFormulaFixture().AbilityDefinitions[0].Steps[0]
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	cp := w.ExportCheckpoint()
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
	if err != nil || need.Stage != "ability" || *need.AbilityStep != 1 || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("input simulation leaked growth", need, err)
	}
	r := battleExec(t, w, "grow-heal", "a", rolledCare("a", "a", 4, 5))
	if r.Events[0].Capacity.After != (core.Resource{Current: 5, Maximum: 35}) || r.Events[1].Resource.After.Current != 17 {
		t.Fatal("growth and roll ordering", r)
	}
}
