package core_test

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"strings"
	"testing"
)

// xpFixture is a character with a running count, xp, beside its level, and
// three step-table totals: the XP the next level needs, the level the XP has
// earned, and a gated term whose offset and divisor apply before its table.
func xpFixture() core.Blueprint {
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}}}
	b.Characters = []core.Character{{ID: "a", Name: "A", Location: "room", Stats: map[core.StatID]int64{"xp": 0, "level": 1, "next": 0, "earned": 0, "half": 0}}}
	b.ConditionDefinitions = []core.ConditionDefinition{{ID: "trained", Instances: core.Unique}}
	b.Equipment = &core.EquipmentRules{Version: core.EquipmentVersion, Derived: []core.DerivedStatRule{
		{Stat: "next", Terms: []core.StatTerm{{Stat: "level", Divisor: 1, Steps: []core.TermStep{{At: 1, Value: 300}, {At: 2, Value: 900}, {At: 3, Value: 0}}}}},
		{Stat: "earned", Terms: []core.StatTerm{{Stat: "xp", Divisor: 1, Steps: []core.TermStep{{At: 0, Value: 1}, {At: 300, Value: 2}, {At: 900, Value: 3}}}}},
		{Stat: "half", Terms: []core.StatTerm{{Stat: "xp", Offset: 100, Divisor: 2, Steps: []core.TermStep{{At: -50, Value: -7}, {At: 200, Value: 5}}, Requires: "trained"}}},
	}}
	return b
}

func adjustOp(stat core.StatID, amount int64) core.Operation {
	return core.Operation{AdjustStat: &core.AdjustStat{Character: "a", Stat: stat, Amount: amount}}
}

func effective(t *testing.T, w *core.World, stat core.StatID, want int64) core.StatExplanation {
	t.Helper()
	x, err := w.Explain("a", stat)
	if err != nil || x.Effective != want {
		t.Fatalf("%s = %+v %v, want %d", stat, x, err, want)
	}
	return x
}

func TestAdjustStat(t *testing.T) {
	w, err := core.NewWorld(xpFixture())
	if err != nil {
		t.Fatal(err)
	}
	effective(t, w, "next", 300)
	effective(t, w, "earned", 1)
	batch := execute(t, w, "award", adjustOp("xp", 299))
	if e := batch.Events; len(e) != 1 || e[0].Kind != core.StatAdjusted || *e[0].StatAdjustment != (core.StatAdjustment{Character: "a", Stat: "xp", Amount: 299, Before: 0, After: 299}) {
		t.Fatalf("%+v", batch.Events)
	}
	// The declared base moves; the tables read it.
	if x := effective(t, w, "xp", 299); x.Base != 299 {
		t.Fatalf("%+v", x)
	}
	effective(t, w, "earned", 1)
	execute(t, w, "reach", adjustOp("xp", 1))
	x := effective(t, w, "earned", 2)
	if term := x.Recipe.Terms[0]; term.Input != 300 || term.Exact != "2" || len(term.Term.Steps) != 3 {
		t.Fatalf("%+v", x.Recipe)
	}
	// Any non-derived stat adjusts, level included.
	execute(t, w, "level", adjustOp("level", 1))
	effective(t, w, "next", 900)
	execute(t, w, "levels", adjustOp("level", 5))
	effective(t, w, "next", 0)
	// Above the last step the table holds; below the first it is 0.
	execute(t, w, "more", adjustOp("xp", 10000))
	effective(t, w, "earned", 3)
	execute(t, w, "back", adjustOp("xp", -10301))
	effective(t, w, "xp", -1)
	effective(t, w, "earned", 0)
	replayAndSave(t, xpFixture(), w)
}

func TestStepTermOffsetAndGate(t *testing.T) {
	w, err := core.NewWorld(xpFixture())
	if err != nil {
		t.Fatal(err)
	}
	if x := effective(t, w, "half", 0); x.Recipe.Terms[0].Active || x.Recipe.Terms[0].Exact != "0" {
		t.Fatalf("%+v", x.Recipe)
	}
	execute(t, w, "train", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "a:trained", Type: "trained", Target: "a"}})
	// floor((0 + 100) / 2) = 50 reaches the step at -50.
	if x := effective(t, w, "half", -7); !x.Recipe.Terms[0].Active || x.Recipe.Terms[0].Input != 0 || x.Recipe.Terms[0].Exact != "-7" {
		t.Fatalf("%+v", x.Recipe)
	}
	// floor((300 + 100) / 2) = 200 reaches the step at 200.
	execute(t, w, "up", adjustOp("xp", 300))
	effective(t, w, "half", 5)
	// floor((-300 + 100) / 2) = -100 is below the first step.
	execute(t, w, "down", adjustOp("xp", -600))
	effective(t, w, "half", 0)
	replayAndSave(t, xpFixture(), w)
}

func TestAdjustStatRefusals(t *testing.T) {
	w, err := core.NewWorld(xpFixture())
	if err != nil {
		t.Fatal(err)
	}
	refused(t, w, "E_AMOUNT", "nonzero signed int64", adjustOp("xp", 0))
	refused(t, w, "E_STAT", "declared stat", adjustOp("gold", 1))
	refused(t, w, "E_REFERENCE", "", core.Operation{AdjustStat: &core.AdjustStat{Character: "b", Stat: "xp", Amount: 1}})
	// An agent never writes a total.
	refused(t, w, "E_RULE", "non-derived stat", adjustOp("earned", 1))
	refused(t, w, "E_IR", "", core.Operation{AdjustStat: &core.AdjustStat{Character: "a", Stat: "xp", Amount: 1}, Move: &core.Move{Character: "a", To: "room"}})
	// The new base must fit int64, and so must every total that reads it.
	execute(t, w, "max", adjustOp("xp", math.MaxInt64))
	refused(t, w, "E_OVERFLOW", "base + amount fits int64", adjustOp("xp", 1))
	execute(t, w, "min", adjustOp("xp", math.MinInt64))
	execute(t, w, "boost", core.Operation{Apply: &core.Effect{ID: "boost", Source: core.Ref{Kind: core.CharacterKind, ID: "a"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "a"}, Stat: "level", Amount: 10}})
	refused(t, w, "E_OVERFLOW", "int64 effective stat", adjustOp("level", math.MaxInt64-5))
}

func TestStepTermRefusals(t *testing.T) {
	ceiling := int64(3)
	many := make([]core.TermStep, core.MaxTermSteps+1)
	for i := range many {
		many[i] = core.TermStep{At: int64(i), Value: 1}
	}
	for _, tc := range []struct {
		name, expected string
		edit           func(*core.StatTerm)
	}{
		{"limit", "at most 32", func(t *core.StatTerm) { t.Steps = many }},
		{"ceiling", "steps or a ceiling, not both", func(t *core.StatTerm) { t.Ceiling = &ceiling }},
		{"order", "strictly increasing step inputs", func(t *core.StatTerm) { t.Steps[2].At = 300 }},
	} {
		b := xpFixture()
		tc.edit(&b.Equipment.Derived[1].Terms[0])
		_, err := core.NewWorld(b)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Field != "steps" || f.Expected != tc.expected || !strings.Contains(f.Subject, "earned") {
			t.Fatal(tc.name, err)
		}
	}
	// Equal inputs are refused too, and so is a table on an equipment recipe.
	b := xpFixture()
	b.Equipment.Derived[1].Terms[0].Steps[1].At = 0
	var f *core.Fault
	if _, err := core.NewWorld(b); !errors.As(err, &f) || f.Expected != "strictly increasing step inputs" {
		t.Fatal(err)
	}
	b = xpFixture()
	b.Equipment.Slots = []core.SlotDefinition{{ID: "body", Capacity: 1}}
	b.Equipment.Definitions = []core.EquipmentDefinition{{ID: "robe", Name: "Robe", DefaultMode: "worn", Modes: []core.EquipmentMode{{ID: "worn",
		Overrides: []core.StatBaseOverride{{Stat: "next", Recipe: core.ScalarRecipe{Terms: []core.StatTerm{{Stat: "level", Divisor: 1, Ceiling: &ceiling, Steps: []core.TermStep{{At: 1, Value: 1}}}}}}}}}}}
	if _, err := core.NewWorld(b); !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Expected != "steps or a ceiling, not both" {
		t.Fatal(err)
	}
}

// The caller's blueprint, request, receipt, history and explanations are all
// detached from the world.
func TestAdjustStatDetachment(t *testing.T) {
	b := xpFixture()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Equipment.Derived[1].Terms[0].Steps[0].Value = 99
	effective(t, w, "earned", 1)
	request := core.Request{ID: "award", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{adjustOp("xp", 300)}}
	receipt, err := w.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Operations[0].AdjustStat.Amount = 999
	receipt.Events[0].StatAdjustment.After = 999
	history := w.History()
	history[0].Events[0].StatAdjustment.Before = 998
	history[0].Request.Operations[0].AdjustStat.Amount = 997
	x := effective(t, w, "earned", 2)
	x.Recipe.Terms[0].Term.Steps[1].Value = 99
	h := w.History()[0]
	if h.Request.Operations[0].AdjustStat.Amount != 300 || *h.Events[0].StatAdjustment != (core.StatAdjustment{Character: "a", Stat: "xp", Amount: 300, Before: 0, After: 300}) {
		t.Fatalf("%+v", h)
	}
	effective(t, w, "earned", 2)
	replayAndSave(t, xpFixture(), w)
}
