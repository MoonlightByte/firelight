package tests

import (
	"errors"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"strings"
	"testing"
)

// Negative Resource maximum modifiers (drains) such as Life Drain. Big's
// maximum is 5 below the int64 maximum; Bea's declared 13/13 already includes
// her genesis drain of 7.
const resourceDrainWorld = `rules { transfer unequips; wear any; }
clock "second" at 0;
location "loc:hall" named "Hall";
character "char:ada" named "Ada" at "loc:hall" {
 stat "defense" = 10;
 resource "hp" = 20 min 0 max 20;
 resource "slots" = 1 min 0 max 1;
 resource "big" = 0 min 0 max 9223372036854775802;
}
character "char:bea" named "Bea" at "loc:hall" {
 stat "defense" = 10;
 resource "hp" = 13 min 0 max 13;
}
condition type "spell:aid" { instances replace; modifier "hp" resource "hp" maximum add magnitude; }
condition type "curse:drain" { instances coexist; modifier "hp" resource "hp" maximum add magnitude; }
condition type "curse:wither" { instances replace; modifier "hp" resource "hp" maximum add -4; modifier "slot" resource "slots" maximum add 1; modifier "weak" stat "defense" add -1; }
condition type "curse:self" { instances unique; ends_at_minimum "hp"; modifier "hp" resource "hp" maximum add magnitude; }
condition type "curse:mixed" { instances unique; modifier "a" resource "hp" maximum add 2; modifier "b" resource "hp" maximum add magnitude; }
condition type "curse:big" { instances coexist; modifier "b" resource "big" maximum add magnitude; }
condition type "boon:big" { instances unique; modifier "b" resource "big" maximum add 7; }
condition type "state:focus" { instances unique; ends_at_minimum "hp"; }
condition "drain:bea" of "curse:drain" to "char:bea" magnitude -7;
`

func resourceDrainFixture(t testing.TB) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("resource-drain.nql", resourceDrainWorld)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func TestConditionResourceDrainApplyAndEnding(t *testing.T) {
	b := resourceDrainFixture(t)
	w := world(t, b)

	// Life Drain: the damage comes first, then the maximum drops and clamps current.
	execute(t, w, "hit", `damage "char:ada" resource "hp" by 5;`)
	r := execute(t, w, "drain", `apply condition "drain:1" of "curse:drain" to "char:ada" magnitude -8 ticks 60;`)
	wantKinds(t, r, core.ConditionApplied)
	if got := r.Events[0].Condition.Resources; len(got) != 1 || got[0] != (core.ConditionResourceChange{Modifier: "hp", Resource: "hp", Amount: -8,
		Before: core.Resource{Current: 15, Minimum: 0, Maximum: 20}, After: core.Resource{Current: 12, Minimum: 0, Maximum: 12}}) {
		t.Fatalf("drain record %+v", got)
	}
	wantResource(t, w, "char:ada", "hp", 12, 12)

	// The ending gives the maximum back and leaves current for the host to heal.
	r = execute(t, w, "rest", `advance time by 60;`)
	wantKinds(t, r, core.ConditionRemoved, core.TimeAdvanced)
	if got := r.Events[0].Condition.Resources; r.Events[0].Condition.ClockExpiration == nil || len(got) != 1 || got[0].Amount != -8 ||
		got[0].Before != (core.Resource{Current: 12, Minimum: 0, Maximum: 12}) || got[0].After != (core.Resource{Current: 12, Minimum: 0, Maximum: 20}) {
		t.Fatalf("ending record %+v", r.Events[0].Condition)
	}
	wantResource(t, w, "char:ada", "hp", 12, 20)

	// A drain that stays above current leaves current alone, and a declared
	// genesis drain gives back only its own amount.
	execute(t, w, "small", `apply condition "drain:2" of "curse:drain" to "char:ada" magnitude -3;`)
	wantResource(t, w, "char:ada", "hp", 12, 17)
	execute(t, w, "lift", `remove condition "drain:bea" from "char:bea";`)
	wantResource(t, w, "char:bea", "hp", 13, 20)
	costReplay(t, b, w)
}

func TestConditionResourceDrainRoomExcludesRaises(t *testing.T) {
	b := resourceDrainFixture(t)
	w := world(t, b)
	var f *core.Fault

	// With nothing raising it, a drain may take the room above the minimum and no more.
	costReject(t, w, typedRequest(w, "too-much", applyC("d:0", "curse:drain", "char:ada", ptr(int64(-21)))), "E_CONDITION_AMOUNT")

	// Aid holds 5 of 25, so 20 is the most a drain can take.
	execute(t, w, "aid", `apply condition "aid:1" of "spell:aid" to "char:ada" magnitude 5; heal "char:ada" resource "hp" by 5;`)
	err := costReject(t, w, typedRequest(w, "past-aid", applyC("d:1", "curse:drain", "char:ada", ptr(int64(-21)))), "E_CONDITION_AMOUNT")
	if !errors.As(err, &f) || f.Field != "resource_maximum" || f.Actual != "drain=21 maximum=25 minimum=0 raises=5" {
		t.Fatalf("room fault %+v", f)
	}

	// Aid, a drain, Aid ends, the drain ends: exactly the base maximum again.
	execute(t, w, "drain", `apply condition "d:2" of "curse:drain" to "char:ada" magnitude -15;`)
	wantResource(t, w, "char:ada", "hp", 10, 10)
	execute(t, w, "aid-ends", `remove condition "aid:1" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 5, 5)
	execute(t, w, "drain-ends", `remove condition "d:2" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 5, 20)

	// The other order: Aid raises the drained maximum, and each ending takes back its own.
	execute(t, w, "drain-2", `apply condition "d:3" of "curse:drain" to "char:ada" magnitude -15;`)
	execute(t, w, "aid-2", `apply condition "aid:2" of "spell:aid" to "char:ada" magnitude 5; heal "char:ada" resource "hp" by 5;`)
	wantResource(t, w, "char:ada", "hp", 10, 10)
	execute(t, w, "drain-ends-2", `remove condition "d:3" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 10, 25)
	execute(t, w, "aid-ends-2", `remove condition "aid:2" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 10, 20)

	// Exactly the room is allowed: max HP to 0 while no raise is active.
	execute(t, w, "to-zero", `apply condition "d:4" of "curse:drain" to "char:ada" magnitude -20;`)
	wantResource(t, w, "char:ada", "hp", 0, 0)
	costReplay(t, b, w)
}

func TestConditionResourceDrainFloor(t *testing.T) {
	b := resourceDrainFixture(t)
	w := world(t, b)

	// A drain runs before its instance joins, so it cannot end itself: one
	// that would end at the minimum it causes, or whose anchor that floor
	// would end, is refused and nothing changes.
	var f *core.Fault
	err := costReject(t, w, typedRequest(w, "self", applyC("self:1", "curse:self", "char:ada", ptr(int64(-20)))), "E_CONDITION")
	if !errors.As(err, &f) || f.Field != "ends_at_minimum" {
		t.Fatalf("self-floor fault %+v", f)
	}
	execute(t, w, "focus", `apply condition "focus:1" of "state:focus" to "char:ada";`)
	anchored := applyC("d:1", "curse:drain", "char:ada", ptr(int64(-20)))
	anchored.ApplyCondition.Anchor = ptr(core.ConditionInstanceID("focus:1"))
	err = costReject(t, w, typedRequest(w, "anchored", anchored), "E_CONDITION")
	if !errors.As(err, &f) || f.Field != "anchor" {
		t.Fatalf("anchor fault %+v", f)
	}

	// A drain to the minimum is a debit: floor Conditions end first and the
	// applied event, which replay reads the operation from, stays last.
	r := execute(t, w, "empty", `apply condition "d:2" of "curse:drain" to "char:ada" magnitude -20;`)
	wantKinds(t, r, core.ConditionRemoved, core.ConditionApplied)
	if c := r.Events[0].Condition; c.Before.ID != "focus:1" || c.MinimumResource == nil || *c.MinimumResource != "hp" {
		t.Fatalf("floor release %+v", c)
	}
	wantResource(t, w, "char:ada", "hp", 0, 0)

	// A self-ending drain that leaves room above the minimum is an ordinary apply.
	execute(t, w, "restore", `remove condition "d:2" from "char:ada"; heal "char:ada" resource "hp" by 20;`)
	execute(t, w, "self-ok", `apply condition "self:2" of "curse:self" to "char:ada" magnitude -19;`)
	wantResource(t, w, "char:ada", "hp", 1, 1)
	costReplay(t, b, w)
}

func TestConditionResourceDrainStackReplaceAndSigns(t *testing.T) {
	b := resourceDrainFixture(t)
	w := world(t, b)

	// Coexisting drains stack; each ending gives back its own amount.
	execute(t, w, "two", `apply condition "d:1" of "curse:drain" to "char:ada" magnitude -3; apply condition "d:2" of "curse:drain" to "char:ada" magnitude -4;`)
	wantResource(t, w, "char:ada", "hp", 13, 13)
	execute(t, w, "one", `remove condition "d:1" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 13, 16)

	// One type may drain one Resource and raise another; records keep modifier ID order.
	r := execute(t, w, "wither", `apply condition "w:1" of "curse:wither" to "char:ada";`)
	if got := r.Events[0].Condition.Resources; len(got) != 2 || got[0].Modifier != "hp" || got[0].Amount != -4 || got[1].Modifier != "slot" || got[1].Amount != 1 {
		t.Fatalf("wither records %+v", got)
	}
	wantResource(t, w, "char:ada", "hp", 12, 12)
	wantResource(t, w, "char:ada", "slots", 1, 2)
	stat(t, w, "char:ada", "defense", 9)

	// Replacing reverses the old instance before the new one drains.
	r = execute(t, w, "wither-again", `apply condition "w:2" of "curse:wither" to "char:ada";`)
	wantKinds(t, r, core.ConditionRemoved, core.ConditionApplied)
	if old := r.Events[0].Condition.Resources; len(old) != 2 || old[0].After != (core.Resource{Current: 12, Minimum: 0, Maximum: 16}) {
		t.Fatalf("replaced records %+v", old)
	}
	wantResource(t, w, "char:ada", "hp", 12, 12)
	wantResource(t, w, "char:ada", "slots", 1, 2)

	// One instance may not raise and drain the same Resource; the compiler
	// reports it at the magnitude.
	costReject(t, w, typedRequest(w, "mixed", applyC("m:1", "curse:mixed", "char:ada", ptr(int64(-1)))), "E_CONDITION_AMOUNT")
	if _, ds := compiler.CompileActions("action.nql", `apply condition "m:1" of "curse:mixed" to "char:ada" magnitude -1;`, w.Snapshot().Blueprint); len(ds) != 1 || ds[0].Code != "E_CONDITION_AMOUNT" || !strings.Contains(ds[0].Expected, "one sign") {
		t.Fatalf("mixed diagnostics %+v", ds)
	}
	execute(t, w, "same-sign", `apply condition "m:2" of "curse:mixed" to "char:ada" magnitude 1;`)
	wantResource(t, w, "char:ada", "hp", 12, 15)
	costReplay(t, b, w)
}

func TestConditionResourceDrainOverflow(t *testing.T) {
	b := resourceDrainFixture(t)
	w := world(t, b)
	var f *core.Fault

	// A drain does not make headroom: its ending needs the amount back.
	execute(t, w, "drain", `apply condition "d:1" of "curse:big" to "char:ada" magnitude -10;`)
	err := costReject(t, w, typedRequest(w, "boon", applyC("b:1", "boon:big", "char:ada", nil)), "E_OVERFLOW")
	if !errors.As(err, &f) || f.Expected != "maximum + amount + active drains fits int64" || f.Actual != "maximum=9223372036854775792 amount=7 drains=10" {
		t.Fatalf("raise fault %+v", f)
	}
	err = costReject(t, w, request(t, w, "grow", `expand "char:ada" resource "big" by 6;`), "E_OVERFLOW")
	if !errors.As(err, &f) || f.Expected != "maximum + requested + active drains fits int64" {
		t.Fatalf("expand fault %+v", f)
	}
	execute(t, w, "grow-5", `expand "char:ada" resource "big" by 5;`)
	execute(t, w, "lift", `remove condition "d:1" from "char:ada";`)
	wantResource(t, w, "char:ada", "big", 0, math.MaxInt64)
	costReplay(t, b, w)

	// A genesis whose declared drains could not be given back is refused.
	src := strings.Replace(resourceDrainWorld, `condition "drain:bea"`, `condition "big:ada" of "curse:big" to "char:ada" magnitude -6;
condition "drain:bea"`, 1)
	if _, ds := compiler.CompileWorld("bad.nql", src); len(ds) != 1 || ds[0].Code != "E_OVERFLOW" {
		t.Fatalf("genesis diagnostics %+v", ds)
	}
	bad := resourceDrainFixture(t)
	bad.Conditions = append(bad.Conditions, core.ConditionInstance{ID: "big:ada", Type: "curse:big", Target: "char:ada", Magnitude: ptr(int64(-6))})
	if _, err := core.NewWorld(bad); !errors.As(err, &f) || f.Code != "E_OVERFLOW" || f.Actual != "maximum=9223372036854775802 drains=6" {
		t.Fatalf("genesis fault %v", err)
	}
}
