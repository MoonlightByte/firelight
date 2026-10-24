package tests

import (
	"errors"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"strconv"
	"strings"
	"testing"
)

const resourceMaximumWorld = `rules { transfer unequips; wear any; }
clock "second" at 0;
location "loc:hall" named "Hall";
character "char:ada" named "Ada" at "loc:hall" {
 stat "defense" = 10;
 resource "hp" = 20 min 0 max 20;
 resource "temp" = 0 min 0 max 100;
 resource "slots" = 0 min 0 max 0;
 buffer "temp" before "hp";
}
character "char:bea" named "Bea" at "loc:hall" {
 stat "defense" = 10;
 resource "hp" = 3 min 0 max 3;
 resource "big" = 0 min 0 max 9223372036854775807;
}
condition type "spell:aid" { instances replace; modifier "hp" resource "hp" maximum add magnitude; }
condition type "boon:slot" { instances unique; modifier "slot" resource "slots" maximum add 1; }
condition type "state:focus" { instances unique; ends_at_minimum "slots"; }
condition type "spell:heroes" { instances coexist; modifier "def" stat "defense" add 1; modifier "hp" resource "hp" maximum add 3; }
condition type "state:root" { instances unique; }
condition type "bad:temp" { instances unique; modifier "t" resource "temp" maximum add 5; }
condition type "bad:big" { instances unique; modifier "b" resource "big" maximum add 1; }
condition "aid:bea" of "spell:aid" to "char:bea" magnitude 5;
`

func resourceMaximumFixture(t testing.TB) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("resource-maximum.nql", resourceMaximumWorld)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func resourceOf(t testing.TB, w *core.World, id core.CharacterID, r core.ResourceID) core.Resource {
	t.Helper()
	p, err := w.CharacterStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Resources[r]
}

func wantResource(t testing.TB, w *core.World, id core.CharacterID, r core.ResourceID, current, maximum int64) {
	t.Helper()
	if got := resourceOf(t, w, id, r); got.Current != current || got.Maximum != maximum {
		t.Fatalf("%s.%s = %d/%d, want %d/%d", id, r, got.Current, got.Maximum, current, maximum)
	}
}

func wantKinds(t testing.TB, b core.Batch, kinds ...core.EventKind) {
	t.Helper()
	if len(b.Events) != len(kinds) {
		t.Fatalf("events %+v, want kinds %v", b.Events, kinds)
	}
	for i, k := range kinds {
		if b.Events[i].Kind != k {
			t.Fatalf("event %d is %s, want %s (%+v)", i, b.Events[i].Kind, k, b.Events)
		}
	}
}

func TestConditionResourceMaximumAidHealAndExpiry(t *testing.T) {
	b := resourceMaximumFixture(t)
	w := world(t, b)
	// Genesis declarations are already applied: Bea's declared 3/3 includes Aid.
	wantResource(t, w, "char:bea", "hp", 3, 3)

	// The heal after the apply in the same request sees the raised maximum.
	r := execute(t, w, "aid", `apply condition "aid:1" of "spell:aid" to "char:ada" magnitude 5 ticks 480;
heal "char:ada" resource "hp" by 5;`)
	wantKinds(t, r, core.ConditionApplied, core.HealingApplied)
	raise := r.Events[0].Condition.Resources
	if len(raise) != 1 || raise[0] != (core.ConditionResourceChange{Modifier: "hp", Resource: "hp", Amount: 5,
		Before: core.Resource{Current: 20, Minimum: 0, Maximum: 20}, After: core.Resource{Current: 20, Minimum: 0, Maximum: 25}}) {
		t.Fatalf("apply record %+v", raise)
	}
	if r.Events[0].Condition.AfterEffects != nil {
		t.Fatalf("a Resource maximum generated a stat Effect: %+v", r.Events[0].Condition.AfterEffects)
	}
	wantResource(t, w, "char:ada", "hp", 25, 25)
	stat(t, w, "char:ada", "defense", 10)

	// Expiry lowers the maximum and clamps current in the same removal event.
	r = execute(t, w, "rest", `advance time by 480;`)
	wantKinds(t, r, core.ConditionRemoved, core.TimeAdvanced)
	lower := r.Events[0].Condition
	if lower.ClockExpiration == nil || len(lower.Resources) != 1 || lower.Resources[0].Amount != 5 ||
		lower.Resources[0].Before != (core.Resource{Current: 25, Minimum: 0, Maximum: 25}) ||
		lower.Resources[0].After != (core.Resource{Current: 20, Minimum: 0, Maximum: 20}) {
		t.Fatalf("expiry record %+v", lower)
	}
	wantResource(t, w, "char:ada", "hp", 20, 20)

	// Damage below the base maximum is kept: expiry only clamps from above.
	execute(t, w, "aid-again", `apply condition "aid:2" of "spell:aid" to "char:ada" magnitude 5 ticks 60; damage "char:ada" resource "hp" by 17;`)
	execute(t, w, "expire", `advance time by 60;`)
	wantResource(t, w, "char:ada", "hp", 3, 20)
	costReplay(t, b, w)
}

func TestConditionResourceMaximumReplaceReversesFirst(t *testing.T) {
	b := resourceMaximumFixture(t)
	w := world(t, b)
	execute(t, w, "aid", `apply condition "aid:1" of "spell:aid" to "char:ada" magnitude 5; heal "char:ada" resource "hp" by 5;`)
	r := execute(t, w, "upcast", `apply condition "aid:2" of "spell:aid" to "char:ada" magnitude 10;`)
	wantKinds(t, r, core.ConditionRemoved, core.ConditionApplied)
	old, added := r.Events[0].Condition, r.Events[1].Condition
	if old.ReplacedBy == nil || *old.ReplacedBy != "aid:2" || old.Resources[0].After != (core.Resource{Current: 20, Minimum: 0, Maximum: 20}) {
		t.Fatalf("replaced record %+v", old)
	}
	if added.Resources[0].After != (core.Resource{Current: 20, Minimum: 0, Maximum: 30}) {
		t.Fatalf("replacement record %+v", added)
	}
	wantResource(t, w, "char:ada", "hp", 20, 30)

	// Coexisting instances stack stat and maximum modifiers; each removal takes back its own.
	execute(t, w, "heroes", `apply condition "h:1" of "spell:heroes" to "char:ada"; apply condition "h:2" of "spell:heroes" to "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 20, 36)
	stat(t, w, "char:ada", "defense", 12)
	execute(t, w, "drop", `remove condition "h:1" from "char:ada";`)
	wantResource(t, w, "char:ada", "hp", 20, 33)
	stat(t, w, "char:ada", "defense", 11)
	costReplay(t, b, w)
}

func TestConditionResourceMaximumRemovalNeverRefused(t *testing.T) {
	b := resourceMaximumFixture(t)
	w := world(t, b)

	// An inconsistent genesis maximum stops at the minimum instead of refusing.
	r := execute(t, w, "end-aid", `remove condition "aid:bea" from "char:bea";`)
	if got := r.Events[0].Condition.Resources[0].After; got != (core.Resource{Current: 0, Minimum: 0, Maximum: 0}) {
		t.Fatalf("floored record %+v", got)
	}
	wantResource(t, w, "char:bea", "hp", 0, 0)

	// A clamp that reaches the minimum is a debit: it releases floor Conditions
	// before the terminal removal, and the request still commits.
	execute(t, w, "slot", `apply condition "boon:1" of "boon:slot" to "char:ada"; heal "char:ada" resource "slots" by 1;
apply condition "focus:1" of "state:focus" to "char:ada";`)
	wantResource(t, w, "char:ada", "slots", 1, 1)
	r = execute(t, w, "lose-slot", `remove condition "boon:1" from "char:ada";`)
	wantKinds(t, r, core.ConditionRemoved, core.ConditionRemoved)
	focus, boon := r.Events[0].Condition, r.Events[1].Condition
	if focus.Before.ID != "focus:1" || focus.MinimumResource == nil || *focus.MinimumResource != "slots" || boon.Before.ID != "boon:1" || boon.MinimumResource != nil {
		t.Fatalf("floor release order %+v / %+v", focus, boon)
	}
	wantResource(t, w, "char:ada", "slots", 0, 0)

	// An anchored dependent's maximum on another character is reversed with its root.
	execute(t, w, "anchor", `apply condition "root:1" of "state:root" to "char:ada";
apply condition "h:bea" of "spell:heroes" to "char:bea" anchor "root:1";`)
	wantResource(t, w, "char:bea", "hp", 0, 3)
	r = execute(t, w, "release", `remove condition "root:1" from "char:ada";`)
	wantKinds(t, r, core.ConditionRemoved, core.ConditionRemoved)
	if r.Events[0].Condition.AnchorRemoved == nil || len(r.Events[0].Condition.Resources) != 1 {
		t.Fatalf("anchored reversal %+v", r.Events[0].Condition)
	}
	wantResource(t, w, "char:bea", "hp", 0, 0)
	costReplay(t, b, w)
}

func TestConditionResourceMaximumRefusedAtApply(t *testing.T) {
	b := resourceMaximumFixture(t)
	w := world(t, b)
	for _, c := range []struct {
		name, typ string
		target    core.CharacterID
		magnitude *int64
		code      string
	}{
		{"buffer pool", "bad:temp", "char:ada", nil, "E_RULE"},
		{"missing resource", "bad:temp", "char:bea", nil, "E_RESOURCE"},
		{"zero magnitude", "spell:aid", "char:ada", ptr(int64(0)), "E_CONDITION_AMOUNT"},
		// Negative magnitudes are drains (condition_resource_drain_test.go);
		// only the int64 minimum, whose size does not fit, stays refused.
		{"int64 minimum magnitude", "spell:aid", "char:ada", ptr(int64(math.MinInt64)), "E_CONDITION_AMOUNT"},
		{"overflow", "bad:big", "char:bea", nil, "E_OVERFLOW"},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The core refuses the typed operation; the compiler reports the
			// static cases as diagnostics with the same code.
			costReject(t, w, typedRequest(w, "refused", applyC("x", c.typ, c.target, c.magnitude)), c.code)
			if c.code == "E_OVERFLOW" {
				return
			}
			src := `apply condition "x" of "` + c.typ + `" to "` + string(c.target) + `";`
			if c.magnitude != nil {
				src = strings.Replace(src, ";", " magnitude "+strconv.FormatInt(*c.magnitude, 10)+";", 1)
			}
			_, ds := compiler.CompileActions("action.nql", src, w.Snapshot().Blueprint)
			if len(ds) != 1 || ds[0].Code != c.code {
				t.Fatalf("diagnostics %+v, want one %s", ds, c.code)
			}
		})
	}

	for _, c := range []struct{ name, clause, code string }{
		{"latest selection", `instances coexist; modifier_selection latest; modifier "m" resource "hp" maximum add 1;`, "E_RULE"},
		{"zero amount", `instances unique; modifier "m" resource "hp" maximum add 0;`, "E_CONDITION_AMOUNT"},
		{"int64 minimum amount", `instances unique; modifier "m" resource "hp" maximum add -9223372036854775808;`, "E_CONDITION_AMOUNT"},
		{"raise and drain one Resource", `instances unique; modifier "a" resource "hp" maximum add 2; modifier "b" resource "hp" maximum add -1;`, "E_CONDITION_AMOUNT"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Replace(resourceMaximumWorld, `condition type "state:root" { instances unique; }`, `condition type "state:root" { `+c.clause+` }`, 1)
			_, ds := compiler.CompileWorld("bad.nql", src)
			if len(ds) != 1 || ds[0].Code != c.code {
				t.Fatalf("diagnostics %+v, want one %s", ds, c.code)
			}
		})
	}
	_, ds := compiler.CompileWorld("bad.nql", strings.Replace(resourceMaximumWorld, `resource "slots" maximum add 1;`, `resource "slots" add 1;`, 1))
	if len(ds) != 1 || ds[0].Code != "E_PARSE" {
		t.Fatalf("missing maximum keyword: %+v", ds)
	}
	var f *core.Fault
	if err := core.CheckConditionDefinition(core.ConditionDefinition{ID: "both", Instances: core.Unique, Modifiers: []core.ConditionModifier{{ID: "m", Stat: "defense", ResourceMaximum: "hp", Fixed: ptr(int64(1))}}}); !errors.As(err, &f) || f.Code != "E_IR" {
		t.Fatalf("stat and Resource maximum together: %v", err)
	}
}
