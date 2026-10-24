package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const clockSource = `rules { transfer unequips; wear any; }
clock "seconds" at 0;
location "room" named "Room";
character "a" named "A" at "room" { stat "defense" = 10; }
condition type "guarded" { instances coexist; modifier "guard" stat "defense" add 3; }
condition "genesis" of "guarded" to "a" ticks 120;
ability "wait" named "Wait" { parameter "ticks" int; advance time by param "ticks"; }
ability "guard" named "Guard" { parameter "id" condition_id; apply condition param "id" of "guarded" to "a" ticks 60; }
`

func TestClockSourceLifetimeAndAbility(t *testing.T) {
	b, ds := CompileWorld("clock.nql", clockSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.Clock.Tick != 0 || b.Conditions[0].ClockLifetime.ExpiresTick != 120 {
		t.Fatal("clock lowering")
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	execute := func(id, source string) core.Batch {
		t.Helper()
		ops, ds := CompileActions("action.nql", source, w.Snapshot().Blueprint)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		r, e := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	execute("direct", `apply condition "direct" of "guarded" to "a" ticks 10; advance time by 10;`)
	execute("ability", `invoke "guard" { "id" = condition_id "ability-guard"; }; invoke "wait" { "ticks" = int 60; };`)
	if len(w.Snapshot().Conditions) != 1 || w.Snapshot().Clock.Tick != 70 {
		t.Fatal("clock application/expiry")
	}
	h := w.History()
	v, e := core.Replay(b, h)
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), v.ExportCheckpoint()) {
		t.Fatalf("replay %v", e)
	}
	// Duration pointers retained in both body and direct operations are detached.
	*b.AbilityDefinitions[1].Steps[0].ApplyCondition.ClockDuration = 500
	if *w.Snapshot().AbilityDefinitions[0].Steps[0].ApplyCondition.ClockDuration != 60 {
		t.Fatal("definition aliased")
	}
	ops, ds := CompileActions("negative.nql", `invoke "wait" { "ticks" = int -1; };`, w.Snapshot().Blueprint)
	if len(ds) == 0 {
		_, e = w.Execute(core.Request{ID: "negative", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
		var context *core.AbilityError
		if !errors.As(e, &context) || context.Operand != "ticks" {
			t.Fatalf("missing semantic context: %v", e)
		}
	}
	execute("continue", `advance time by 50;`)
	if len(w.Snapshot().Conditions) != 0 {
		t.Fatal("continuation")
	}
}
func TestClockEquipmentAuthoredTiming(t *testing.T) {
	source := equipmentSource(t) + "\nclock \"seconds\" at 0;\n"
	source = strings.Replace(source, `mode "worn" {`, `mode "worn" { equip_ticks 60; unequip_ticks 60;`, 1)
	b, ds := CompileWorld("gear-clock.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	found := false
	for _, d := range b.Equipment.Definitions {
		for _, m := range d.Modes {
			if m.EquipTicks == 60 && m.UnequipTicks == 60 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("timing discarded")
	}
	_, ds = CompileWorld("no-clock.nql", strings.Replace(source, "clock \"seconds\" at 0;", "", 1))
	if len(ds) != 1 || ds[0].Code != "E_EQUIPMENT" || ds[0].Position.Line == 1 {
		t.Fatalf("missing timing site %+v", ds)
	}
}
func TestClockPositionedInvalidSource(t *testing.T) {
	for _, tc := range []struct {
		old, new, code string
		line           int
	}{
		{`clock "seconds" at 0;`, `clock "seconds" at -1;`, "E_AMOUNT", 2},
		{`ticks 120`, `ticks 0`, "E_AMOUNT", 6},
		{`advance time by param "ticks"`, `advance time by -1`, "E_CLOCK", 7},
		{`ticks 60`, `ticks 60 duration 1`, "E_CLOCK", 8},
		{`clock "seconds" at 0;`, `clock "seconds" at 18446744073709551615;`, "E_INTEGER", 2},
	} {
		t.Run(tc.new, func(t *testing.T) {
			b, ds := CompileWorld("invalid.nql", strings.Replace(clockSource, tc.old, tc.new, 1))
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Source != "invalid.nql" || ds[0].Position.Line != tc.line || !reflect.DeepEqual(b, core.Blueprint{}) {
				t.Fatalf("%+v partial %+v", ds, b)
			}
		})
	}
	b, ds := CompileWorld("clock.nql", clockSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, s := range []string{`advance time by 0;`, `advance time by -1;`, `advance time by 9223372036854775808;`, `apply condition "x" of "guarded" to "a" ticks 0;`} {
		ops, ds := CompileActions("bad.nql", s, b)
		if len(ds) != 1 || len(ops) != 0 || ds[0].Position.Line != 1 || ds[0].Position.Column <= 1 {
			t.Fatalf("invalid action %s %+v", s, ds)
		}
	}
}
func FuzzClockSource(f *testing.F) {
	f.Add(clockSource)
	f.Add("clock \"seconds\" at 18446744073709551615;")
	f.Add("clock \"\xff\" at -1;")
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 8192 {
			t.Skip()
		}
		b, ds := CompileWorld("fuzz-clock", source)
		if len(ds) == 0 {
			if _, e := core.NewWorld(b); e != nil {
				t.Fatalf("accepted invalid IR: %v", e)
			}
		}
	})
}
