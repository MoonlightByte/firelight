package core_test

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

// checkFixture derives check and save stats from pools, as the SRD block does.
// sneak reads bonus:checks only while trained, so a mode on the pool reaches
// it only then.
func checkFixture() core.Blueprint {
	roll := func(id core.ConditionModifierID, stat core.StatID, mode core.RollMode) core.ConditionModifier {
		return core.ConditionModifier{ID: id, Stat: stat, Roll: mode}
	}
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}}, Dice: &core.Dice{Seed: [2]uint64{7, 11}}}
	b.Characters = []core.Character{{ID: "a", Name: "A", Location: "room", Stats: map[core.StatID]int64{"dexterity": 14, "bonus:checks": 0, "bonus:saves": 0, "check:dex": 0, "save:dex": 0, "sneak": 0, "flat": 3}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 10, Maximum: 10}}}}
	b.ConditionDefinitions = []core.ConditionDefinition{
		{ID: "poisoned", Instances: core.Coexist, Modifiers: []core.ConditionModifier{roll("checks", "bonus:checks", core.Disadvantage)}},
		{ID: "blessed", Instances: core.Coexist, ModifierSelection: core.LatestModifiers, Modifiers: []core.ConditionModifier{roll("checks", "bonus:checks", core.Advantage)}},
		{ID: "restrained", Instances: core.Coexist, Modifiers: []core.ConditionModifier{roll("save", "save:dex", core.Disadvantage)}},
		{ID: "paralyzed", Instances: core.Coexist, Modifiers: []core.ConditionModifier{roll("save", "save:dex", core.AutoFail)}},
		{ID: "unconscious", Instances: core.Coexist, HeldAtMinimum: gearPtr(core.ResourceID("hp")), Modifiers: []core.ConditionModifier{roll("save", "save:dex", core.AutoFail)}},
		{ID: "trained", Instances: core.Unique},
	}
	dex := core.StatTerm{Stat: "dexterity", Offset: -10, Divisor: 2}
	b.Equipment = &core.EquipmentRules{Version: core.EquipmentVersion, Slots: []core.SlotDefinition{{ID: "body", Capacity: 1}}, Derived: []core.DerivedStatRule{
		{Stat: "check:dex", Terms: []core.StatTerm{dex, {Stat: "bonus:checks", Divisor: 1}}},
		{Stat: "save:dex", Terms: []core.StatTerm{dex, {Stat: "bonus:saves", Divisor: 1}}},
		{Stat: "sneak", Terms: []core.StatTerm{dex, {Stat: "bonus:checks", Divisor: 1, Requires: "trained"}}},
	}}
	return b
}

func checkOp(stat core.StatID, dc *int64, mode core.RollMode, faces ...int64) core.Operation {
	return core.Operation{StatCheck: &core.StatCheck{Character: "a", Stat: stat, Sides: 20, DC: dc, Mode: mode, Faces: faces}}
}

func execute(t *testing.T, w *core.World, id string, ops ...core.Operation) core.Batch {
	t.Helper()
	batch, err := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil {
		t.Fatal(id, err)
	}
	return batch
}

func refused(t *testing.T, w *core.World, code, expected string, ops ...core.Operation) {
	t.Helper()
	before := w.Snapshot()
	_, err := w.Execute(core.Request{ID: "refused", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != code || expected != "" && f.Expected != expected {
		t.Fatalf("want %s %q, got %v", code, expected, err)
	}
	if !reflect.DeepEqual(before, w.Snapshot()) {
		t.Fatal("refused check changed the world")
	}
}

// rolls checks a stat's net mode and the types of its sources.
func rolls(t *testing.T, w *core.World, stat core.StatID, want core.RollMode, types ...core.ConditionTypeID) {
	t.Helper()
	x, err := w.Explain("a", stat)
	if err != nil {
		t.Fatal(err)
	}
	var got []core.ConditionTypeID
	for _, s := range x.RollSources {
		got = append(got, s.Type)
	}
	if x.Roll != want || !reflect.DeepEqual(got, types) || len(x.Contributions) != 0 || len(x.Bounds) != 0 {
		t.Fatalf("%s want %q %v, got %+v", stat, want, types, x)
	}
}

func checked(t *testing.T, batch core.Batch) core.StatCheckResult {
	t.Helper()
	e := batch.Events[len(batch.Events)-1]
	if e.Kind != core.StatCheckResolved || e.StatCheck == nil {
		t.Fatalf("%+v", e)
	}
	return *e.StatCheck
}

func TestRollModesFromConditions(t *testing.T) {
	b := checkFixture()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	rolls(t, w, "check:dex", "")
	execute(t, w, "poison", applyOp("a:poison", "poisoned", "a", nil))
	// The pool's mode reaches every stat whose active terms read it.
	rolls(t, w, "bonus:checks", core.Disadvantage, "poisoned")
	rolls(t, w, "check:dex", core.Disadvantage, "poisoned")
	rolls(t, w, "save:dex", "")
	rolls(t, w, "sneak", "")
	rolls(t, w, "flat", "")
	x, _ := w.Explain("a", "check:dex")
	if want := []core.RollSource{{Instance: "a:poison", Type: "poisoned", Modifier: "checks", Stat: "bonus:checks", Roll: core.Disadvantage}}; !reflect.DeepEqual(x.RollSources, want) || x.Effective != 2 {
		t.Fatalf("%+v", x)
	}
	if s := w.Snapshot(); len(s.ConditionEffects) != 0 {
		t.Fatal("a roll mode generated an Effect", s.ConditionEffects)
	}
	execute(t, w, "train", applyOp("a:trained", "trained", "a", nil))
	rolls(t, w, "sneak", core.Disadvantage, "poisoned")
	// Advantage and disadvantage cancel; latest selection lists one blessing.
	execute(t, w, "bless", applyOp("a:bless:1", "blessed", "a", nil), applyOp("a:bless:2", "blessed", "a", nil))
	rolls(t, w, "check:dex", "", "blessed", "poisoned")
	x, _ = w.Explain("a", "check:dex")
	if x.RollSources[0].Instance != "a:bless:2" {
		t.Fatalf("latest selection %+v", x.RollSources)
	}
	// Any fail is an automatic failure, whatever else is in force.
	execute(t, w, "hold", applyOp("a:rope", "restrained", "a", nil))
	rolls(t, w, "save:dex", core.Disadvantage, "restrained")
	execute(t, w, "freeze", applyOp("a:freeze", "paralyzed", "a", nil))
	rolls(t, w, "save:dex", core.AutoFail, "paralyzed", "restrained")
	execute(t, w, "thaw", removeOp("a:freeze", "a"))
	// A held type's modes name its Resource and sort first, as its bounds do.
	execute(t, w, "knockout", core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 10}})
	rolls(t, w, "save:dex", core.AutoFail, "unconscious", "restrained")
	x, _ = w.Explain("a", "save:dex")
	if x.RollSources[0] != (core.RollSource{HeldAtMinimum: "hp", Type: "unconscious", Modifier: "save", Stat: "save:dex", Roll: core.AutoFail}) {
		t.Fatalf("%+v", x.RollSources)
	}
	replayAndSave(t, b, w)
}

func TestStatCheckResolution(t *testing.T) {
	b := checkFixture()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	dc := gearPtr(int64(12))
	r := checked(t, execute(t, w, "plain", checkOp("check:dex", dc, "", 10)))
	if r.Kept != 10 || r.Bonus != 2 || *r.Total != 12 || !*r.Success || *r.Margin != 0 || r.Drawn || r.Roll != "" || !reflect.DeepEqual(r.Faces, []int64{10}) {
		t.Fatalf("%+v", r)
	}
	r = checked(t, execute(t, w, "advantage", checkOp("check:dex", dc, core.Advantage, 5, 17)))
	if r.Kept != 17 || *r.Total != 19 || *r.Margin != 7 || r.Roll != core.Advantage {
		t.Fatalf("%+v", r)
	}
	r = checked(t, execute(t, w, "disadvantage", checkOp("check:dex", dc, core.Disadvantage, 17, 5)))
	if r.Kept != 5 || *r.Total != 7 || *r.Success || *r.Margin != -5 {
		t.Fatalf("%+v", r)
	}
	// Without a DC the check reports its total only.
	r = checked(t, execute(t, w, "total", checkOp("check:dex", nil, "", 9)))
	if *r.Total != 11 || r.Success != nil || r.Margin != nil {
		t.Fatalf("%+v", r)
	}
	// Supplied faces must match the net mode.
	execute(t, w, "poison", applyOp("a:poison", "poisoned", "a", nil))
	refused(t, w, "E_ROLL", "2 faces (disadvantage)", checkOp("check:dex", dc, "", 15))
	refused(t, w, "E_ROLL", "1 face (normal)", checkOp("check:dex", dc, core.Advantage, 15, 3))
	r = checked(t, execute(t, w, "cancel", checkOp("check:dex", dc, core.Advantage, 15)))
	if r.Kept != 15 || r.Roll != "" || len(r.Sources) != 1 {
		t.Fatalf("%+v", r)
	}
	// An automatic failure takes no faces and draws none.
	execute(t, w, "freeze", applyOp("a:freeze", "paralyzed", "a", nil))
	refused(t, w, "E_ROLL", "0 faces (fail)", checkOp("save:dex", dc, "", 20))
	calls := w.Snapshot().Dice.State.Calls
	r = checked(t, execute(t, w, "fail", checkOp("save:dex", dc, core.Advantage)))
	if r.Roll != core.AutoFail || r.Success == nil || *r.Success || r.Total != nil || r.Margin != nil || r.Faces != nil || r.Kept != 0 || w.Snapshot().Dice.State.Calls != calls {
		t.Fatalf("%+v", r)
	}
	r = checked(t, execute(t, w, "fail-total", checkOp("save:dex", nil, "")))
	if r.Roll != core.AutoFail || r.Success != nil || r.Total != nil {
		t.Fatalf("%+v", r)
	}
	// Engine rolls come from the world's dice: two faces with disadvantage.
	r = checked(t, execute(t, w, "drawn", checkOp("check:dex", dc, "")))
	if !r.Drawn || len(r.Faces) != 2 || r.Kept != min(r.Faces[0], r.Faces[1]) || *r.Total != r.Kept+2 || r.Intent.Faces != nil || w.Snapshot().Dice.State.Calls <= calls {
		t.Fatalf("%+v", r)
	}
	for _, f := range r.Faces {
		if f < 1 || f > 20 {
			t.Fatalf("face %d", f)
		}
	}
	// The same seed draws the same faces.
	again, _ := core.NewWorld(b)
	execute(t, again, "poison", applyOp("a:poison", "poisoned", "a", nil))
	if x := checked(t, execute(t, again, "drawn", checkOp("check:dex", dc, ""))); !reflect.DeepEqual(x.Faces, r.Faces) {
		t.Fatal("dice are not reproducible", x.Faces, r.Faces)
	}
	replayAndSave(t, b, w)
}

func TestStatCheckRefusals(t *testing.T) {
	b := checkFixture()
	b.Characters[0].Stats["huge"] = math.MaxInt64
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	with := func(f func(*core.StatCheck)) core.Operation {
		o := checkOp("check:dex", nil, "", 10)
		f(o.StatCheck)
		return o
	}
	for _, tc := range []struct {
		code string
		op   core.Operation
	}{
		{"E_STAT", checkOp("missing", nil, "", 10)},
		{"E_REFERENCE", with(func(v *core.StatCheck) { v.Character = "nobody" })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Sides = 1 })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Sides = 1001 })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Mode = core.AutoFail })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Faces = []int64{1, 2, 3} })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Faces = []int64{0} })},
		{"E_ROLL", with(func(v *core.StatCheck) { v.Faces = []int64{21} })},
		{"E_OVERFLOW", checkOp("huge", nil, "", 1)},
		{"E_OVERFLOW", checkOp("flat", gearPtr(int64(math.MinInt64)), "", 20)},
	} {
		refused(t, w, tc.code, "", tc.op)
	}
	// Without faces a world needs a random source.
	b.Dice = nil
	w, _ = core.NewWorld(b)
	refused(t, w, "E_ROLL", "1 face (normal) or a world random source", checkOp("check:dex", nil, ""))
	if len(w.History()) != 0 {
		t.Fatal("refusals committed")
	}
}

func TestDiceRandomSource(t *testing.T) {
	// Battle's random state serves a Battle world, and Dice cannot join it.
	bb := battleFixture()
	w, err := core.NewWorld(bb)
	if err != nil {
		t.Fatal(err)
	}
	calls := w.Snapshot().Battle.State.Random.Calls
	if r := checked(t, execute(t, w, "battle", core.Operation{StatCheck: &core.StatCheck{Character: "a", Stat: "initiative", Sides: 20}})); !r.Drawn || w.Snapshot().Battle.State.Random.Calls != calls+1 {
		t.Fatalf("%+v", r)
	}
	bb.Dice = &core.Dice{}
	var f *core.Fault
	if _, err := core.NewWorld(bb); !errors.As(err, &f) || f.Code != "E_DICE" || f.Field != "dice" {
		t.Fatal(err)
	}
	// Genesis accepts zero or the exact initial continuation.
	b := checkFixture()
	w, _ = core.NewWorld(b)
	initial := w.Snapshot().Dice
	if initial.State.Version != core.RandomVersion || len(initial.State.Bytes) != 20 || initial.State.Calls != 0 {
		t.Fatalf("%+v", initial)
	}
	b.Dice = initial
	if _, err := core.NewWorld(b); err != nil {
		t.Fatal(err)
	}
	execute(t, w, "draw", checkOp("flat", nil, ""))
	b.Dice = w.Snapshot().Dice
	if _, err := core.NewWorld(b); !errors.As(err, &f) || f.Code != "E_DICE" || f.Field != "genesis" {
		t.Fatal(err)
	}
	b.Dice = &core.Dice{State: core.RandomState{Version: "other", Bytes: initial.State.Bytes}}
	if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_DICE" || f.Field != "state" {
		t.Fatal(err)
	}
}

func TestRollModifierDefinitionRefusals(t *testing.T) {
	for _, tc := range []struct {
		field string
		m     core.ConditionModifier
		held  bool
	}{
		{"roll", core.ConditionModifier{ID: "m", Stat: "s", Roll: "sometimes"}, false},
		{"roll", core.ConditionModifier{ID: "m", Stat: "s", Roll: core.Advantage, Bound: core.AtMost, Fixed: gearPtr(int64(0))}, false},
		{"amount", core.ConditionModifier{ID: "m", Stat: "s", Roll: core.Advantage, Fixed: gearPtr(int64(1))}, false},
		{"amount", core.ConditionModifier{ID: "m", Stat: "s", Roll: core.Advantage, Magnitude: true}, false},
		{"bound", core.ConditionModifier{ID: "m", ResourceMaximum: "hp", Roll: core.Advantage}, false},
		{"held_at_minimum", core.ConditionModifier{ID: "m", Stat: "s", Fixed: gearPtr(int64(1))}, true},
	} {
		d := core.ConditionDefinition{ID: "t", Instances: core.Coexist, Modifiers: []core.ConditionModifier{tc.m}}
		if tc.held {
			d.HeldAtMinimum = gearPtr(core.ResourceID("hp"))
		}
		var f *core.Fault
		if err := core.CheckConditionDefinition(d); !errors.As(err, &f) || f.Field != tc.field {
			t.Errorf("%+v: want field %s, got %v", tc.m, tc.field, err)
		}
	}
	held := core.ConditionDefinition{ID: "t", Instances: core.Coexist, HeldAtMinimum: gearPtr(core.ResourceID("hp")), Modifiers: []core.ConditionModifier{{ID: "m", Stat: "s", Roll: core.AutoFail}}}
	if err := core.CheckConditionDefinition(held); err != nil {
		t.Fatal(err)
	}
}
