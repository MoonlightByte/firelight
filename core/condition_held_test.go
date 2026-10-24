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

func boundFixture() core.Blueprint {
	at := func(id core.ConditionModifierID, stat core.StatID, bound core.ModifierBound, amount *int64) core.ConditionModifier {
		return core.ConditionModifier{ID: id, Stat: stat, Bound: bound, Fixed: amount, Magnitude: amount == nil}
	}
	return core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}},
		Characters: []core.Character{{ID: "a", Name: "A", Location: "room", Stats: map[core.StatID]int64{"speed": 30, "strength": 16}}},
		ConditionDefinitions: []core.ConditionDefinition{
			{ID: "grappled", Instances: core.Coexist, Modifiers: []core.ConditionModifier{at("speed", "speed", core.AtMost, gearPtr(int64(0)))}},
			{ID: "boots", Instances: core.Unique, Modifiers: []core.ConditionModifier{at("speed", "speed", core.AtLeast, gearPtr(int64(40)))}},
			{ID: "haste", Instances: core.Unique, Modifiers: []core.ConditionModifier{{ID: "speed", Stat: "speed", Fixed: gearPtr(int64(30))}}},
			{ID: "gauntlets", Instances: core.Unique, Modifiers: []core.ConditionModifier{at("strength", "strength", core.AtLeast, gearPtr(int64(19)))}},
			{ID: "belt", Instances: core.Coexist, Modifiers: []core.ConditionModifier{at("strength", "strength", core.AtLeast, nil)}},
			{ID: "slow", Instances: core.Coexist, ModifierSelection: core.LatestModifiers, Modifiers: []core.ConditionModifier{at("speed", "speed", core.AtMost, nil)}},
		}}
}

// replayAndSave rebuilds the world from its history and through a native
// archive; both must equal the live checkpoint.
func replayAndSave(t *testing.T, b core.Blueprint, w *core.World) {
	t.Helper()
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(rebuilt.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "held", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("archive", err)
	}
}

func applyOp(id core.ConditionInstanceID, kind core.ConditionTypeID, target core.CharacterID, magnitude *int64) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: kind, Target: target, Magnitude: magnitude}}
}

func removeOp(id core.ConditionInstanceID, target core.CharacterID) core.Operation {
	return core.Operation{RemoveCondition: &core.RemoveCondition{Instance: id, Target: target}}
}

// bounded checks a total and which bound instances held it.
func bounded(t *testing.T, w *core.World, stat core.StatID, want int64, applied ...core.ConditionInstanceID) core.StatExplanation {
	t.Helper()
	x, err := w.Explain("a", stat)
	if err != nil || x.Effective != want {
		t.Fatalf("%s want %d got %+v %v", stat, want, x, err)
	}
	var got []core.ConditionInstanceID
	for _, b := range x.Bounds {
		if b.Applied {
			got = append(got, b.Instance)
		}
	}
	if !reflect.DeepEqual(got, applied) {
		t.Fatalf("%s applied bounds %v, want %v: %+v", stat, got, applied, x.Bounds)
	}
	return x
}

func TestConditionBoundsFloorsCeilingsAndSelection(t *testing.T) {
	b := boundFixture()
	w := gearWorld(t, b)
	bounded(t, w, "strength", 16)

	// The highest floor sets the score; a floor below the score does nothing.
	gearExecute(t, w, "gauntlets", applyOp("gauntlets", "gauntlets", "a", nil))
	x := bounded(t, w, "strength", 19, "gauntlets")
	if len(x.Contributions) != 0 || x.Base != 16 || x.Bounds[0] != (core.StatBound{Instance: "gauntlets", Type: "gauntlets", Modifier: "strength", Bound: core.AtLeast, Amount: 19, Applied: true}) {
		t.Fatalf("a bound is not an additive contribution: %+v", x)
	}
	gearExecute(t, w, "belts", applyOp("belt-hill", "belt", "a", gearPtr(int64(21))), applyOp("belt-weak", "belt", "a", gearPtr(int64(15))))
	bounded(t, w, "strength", 21, "belt-hill")
	gearExecute(t, w, "unbelt", removeOp("belt-hill", "a"))
	bounded(t, w, "strength", 19, "gauntlets")

	// Additive modifiers apply first; a ceiling then wins over a floor.
	gearExecute(t, w, "haste", applyOp("haste", "haste", "a", nil), applyOp("boots", "boots", "a", nil))
	bounded(t, w, "speed", 60)
	gearExecute(t, w, "grab", applyOp("grab-1", "grappled", "a", nil), applyOp("grab-2", "grappled", "a", nil))
	bounded(t, w, "speed", 0, "grab-1", "grab-2")
	gearExecute(t, w, "release-one", removeOp("grab-1", "a"))
	bounded(t, w, "speed", 0, "grab-2")
	gearExecute(t, w, "release", removeOp("grab-2", "a"), removeOp("haste", "a"))
	bounded(t, w, "speed", 40, "boots")

	// Latest selection suppresses a whole instance, bounds included.
	gearExecute(t, w, "slow-1", applyOp("slow-1", "slow", "a", gearPtr(int64(10))))
	gearExecute(t, w, "slow-2", applyOp("slow-2", "slow", "a", gearPtr(int64(20))))
	x = bounded(t, w, "speed", 20, "slow-2")
	if len(x.Bounds) != 2 || x.Bounds[1].Instance != "slow-2" {
		t.Fatalf("suppressed bound listed: %+v", x.Bounds)
	}
	gearExecute(t, w, "slow-end", removeOp("slow-2", "a"))
	bounded(t, w, "speed", 10, "slow-1")

	replayAndSave(t, b, w)
}

// Totals use exact arithmetic before the int64 check, so a ceiling can bring
// an out-of-range sum back into range.
func TestConditionBoundsExactArithmetic(t *testing.T) {
	b := boundFixture()
	b.Characters[0].Stats["speed"] = math.MaxInt64
	b.Effects = []core.Effect{{ID: "wind", Source: core.Ref{Kind: core.CharacterKind, ID: "a"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "a"}, Stat: "speed", Amount: 5}}
	if _, err := core.NewWorld(b); err == nil {
		t.Fatal("overflow accepted without a bound")
	}
	b.Conditions = []core.ConditionInstance{{ID: "grab", Type: "grappled", Target: "a"}}
	bounded(t, gearWorld(t, b), "speed", 0, "grab")
}

func TestConditionBoundAndHeldDefinitionRefusals(t *testing.T) {
	hp := gearPtr(core.ResourceID("hp"))
	zero := gearPtr(int64(0))
	for _, tc := range []struct {
		d           core.ConditionDefinition
		code, field string
		actual      string
	}{
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "m", Stat: "speed", Bound: "exactly", Fixed: zero}}}, "E_RULE", "bound", "exactly"},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "m", ResourceMaximum: "hp", Bound: core.AtMost, Fixed: gearPtr(int64(5))}}}, "E_RULE", "bound", "hp"},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, HeldAtMinimum: gearPtr(core.ResourceID(" "))}, "E_ID", "held_at_minimum", " "},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, HeldAtMinimum: hp, EndsAtMinimum: hp}, "E_RULE", "held_at_minimum", "hp"},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, HeldAtMinimum: hp, Modifiers: []core.ConditionModifier{{ID: "m", Stat: "speed", Fixed: zero}}}, "E_RULE", "held_at_minimum", "m"},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, HeldAtMinimum: hp, Modifiers: []core.ConditionModifier{{ID: "m", Stat: "speed", Bound: core.AtMost, Magnitude: true}}}, "E_RULE", "held_at_minimum", "m"},
		{core.ConditionDefinition{ID: "x", Instances: core.Coexist, HeldAtMinimum: hp, Modifiers: []core.ConditionModifier{{ID: "m", ResourceMaximum: "hp", Fixed: gearPtr(int64(5))}}}, "E_RULE", "held_at_minimum", "m"},
	} {
		var f *core.Fault
		if err := core.CheckConditionDefinition(tc.d); !errors.As(err, &f) || f.Code != tc.code || f.Field != tc.field || f.Actual != tc.actual {
			t.Errorf("%+v: %v", tc.d, err)
		}
	}
}

func heldFixture() core.Blueprint {
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}}}
	for _, id := range []core.CharacterID{"a", "b"} {
		b.Characters = append(b.Characters, core.Character{ID: id, Name: string(id), Location: "room", Stats: map[core.StatID]int64{"speed": 30, "defense": 10, "one": 1, "awake": 0}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 10, Maximum: 10}}})
	}
	// A Character without the Resource is never held.
	b.Characters = append(b.Characters, core.Character{ID: "c", Name: "c", Location: "room", Stats: map[core.StatID]int64{"speed": 30}})
	b.ConditionDefinitions = []core.ConditionDefinition{
		{ID: "unconscious", Instances: core.Coexist, HeldAtMinimum: gearPtr(core.ResourceID("hp")), Modifiers: []core.ConditionModifier{{ID: "speed", Stat: "speed", Bound: core.AtMost, Fixed: gearPtr(int64(0))}}},
		{ID: "focus", Instances: core.Coexist, EndsOnCondition: gearPtr(core.ConditionTypeID("unconscious")), Modifiers: []core.ConditionModifier{{ID: "defense", Stat: "defense", Fixed: gearPtr(int64(2))}}},
	}
	// Presence tests see held types: awake is 1 unless unconscious.
	b.Equipment = &core.EquipmentRules{Version: core.EquipmentVersion, Slots: []core.SlotDefinition{{ID: "body", Capacity: 1}}, Derived: []core.DerivedStatRule{{Stat: "awake", Terms: []core.StatTerm{{Stat: "one", Divisor: 1, Unless: "unconscious"}}}}}
	return b
}

func held(t *testing.T, w *core.World, id core.CharacterID, speed, awake int64, types ...core.ConditionTypeID) {
	t.Helper()
	p, err := w.CharacterStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	var got []core.ConditionTypeID
	for _, h := range p.Held {
		if h.Resource != "hp" {
			t.Fatalf("held resource %+v", h)
		}
		got = append(got, h.Type)
	}
	if !reflect.DeepEqual(got, types) {
		t.Fatalf("%s held %v, want %v", id, got, types)
	}
	for _, s := range p.Stats {
		if s.Stat == "speed" && s.Effective != speed || s.Stat == "awake" && s.Effective != awake {
			t.Fatalf("%s %s = %d, want speed %d awake %d", id, s.Stat, s.Effective, speed, awake)
		}
	}
}

func TestConditionHeldAtMinimum(t *testing.T) {
	b := heldFixture()
	w := gearWorld(t, b)
	held(t, w, "a", 30, 1)
	gearExecute(t, w, "focus", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "a-focus", Type: "focus", Target: "a"}})
	assertDefense(t, w, "a", 12)

	// Reaching 0 HP holds the type and ends what it ends, before the damage
	// event; no held event is published.
	down := gearExecute(t, w, "down", core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 15}})
	if len(down.Events) != 2 || down.Events[0].Kind != core.ConditionRemoved || down.Events[0].Condition.Before.ID != "a-focus" || *down.Events[0].Condition.MinimumResource != "hp" || down.Events[1].Kind != core.DamageApplied {
		t.Fatalf("down events %+v", down.Events)
	}
	held(t, w, "a", 0, 0, "unconscious")
	assertDefense(t, w, "a", 10)
	x, _ := w.Explain("a", "speed")
	if len(x.Bounds) != 1 || x.Bounds[0] != (core.StatBound{HeldAtMinimum: "hp", Type: "unconscious", Modifier: "speed", Bound: core.AtMost, Amount: 0, Applied: true}) {
		t.Fatalf("held bound %+v", x.Bounds)
	}

	// Nothing that ends on the held type can start while it holds.
	before := w.ExportCheckpoint()
	_, err := w.Execute(gearRequest(w, "refocus", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "a-focus-2", Type: "focus", Target: "a"}}))
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_CONDITION" || f.Field != "ends_on_condition" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("focus while held", err)
	}

	// Any healing ends it, with no event of its own.
	up := gearExecute(t, w, "up", core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}})
	if len(up.Events) != 1 || up.Events[0].Kind != core.HealingApplied {
		t.Fatalf("up events %+v", up.Events)
	}
	held(t, w, "a", 30, 1)

	// An instance of the type works above 0 HP (knocked out at 1 HP) and
	// stays until removed; the held type adds nothing on top.
	gearExecute(t, w, "knockout", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "b-out", Type: "unconscious", Target: "b"}})
	held(t, w, "b", 0, 0)
	gearExecute(t, w, "b-down", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 10}})
	held(t, w, "b", 0, 0, "unconscious")
	x, _ = w.Explain("b", "speed")
	if len(x.Bounds) != 2 || x.Bounds[0].HeldAtMinimum != "hp" || x.Bounds[1].Instance != "b-out" {
		t.Fatalf("both bounds listed %+v", x.Bounds)
	}
	gearExecute(t, w, "b-up", core.Operation{Heal: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 1}})
	held(t, w, "b", 0, 0)
	gearExecute(t, w, "b-wake", removeOp("b-out", "b"))
	held(t, w, "b", 30, 1)
	held(t, w, "c", 30, 0)

	replayAndSave(t, b, w)

	// Genesis obeys the same law: a Character at 0 HP cannot hold focus.
	bad := heldFixture()
	bad.Characters[0].Resources["hp"] = core.Resource{Current: 0, Maximum: 10}
	bad.Conditions = []core.ConditionInstance{{ID: "a-focus", Type: "focus", Target: "a"}}
	if _, err := core.NewWorld(bad); !errors.As(err, &f) || f.Code != "E_CONDITION" || f.Field != "ends_on_condition" {
		t.Fatal("genesis focus at 0 HP", err)
	}
	bad.Conditions = nil
	held(t, gearWorld(t, bad), "a", 0, 0, "unconscious")
}
