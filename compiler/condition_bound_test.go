package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const boundSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "speed" = 30; stat "strength" = 16; resource "hp" = 10 min 0 max 10; }
condition type "grappled" { instances coexist; modifier "speed" stat "speed" at most 0; }
condition type "belt" { instances unique; modifier "strength" stat "strength" at least magnitude; }
condition type "unconscious" { instances coexist; held_at_minimum "hp"; modifier "speed" stat "speed" at most 0; }
`

func TestConditionBoundSourceAndHeldType(t *testing.T) {
	b, ds := compiler.CompileWorld("bound", boundSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	zero := int64(0)
	hp := core.ResourceID("hp")
	want := []core.ConditionDefinition{
		{ID: "grappled", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "speed", Stat: "speed", Bound: core.AtMost, Fixed: &zero}}},
		{ID: "belt", Instances: core.Unique, Modifiers: []core.ConditionModifier{{ID: "strength", Stat: "strength", Bound: core.AtLeast, Magnitude: true}}},
		{ID: "unconscious", Instances: core.Coexist, HeldAtMinimum: &hp, Modifiers: []core.ConditionModifier{{ID: "speed", Stat: "speed", Bound: core.AtMost, Fixed: &zero}}},
	}
	if !reflect.DeepEqual(b.ConditionDefinitions, want) {
		t.Fatalf("lowered %+v", b.ConditionDefinitions)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("act", `apply condition "a:belt" of "belt" to "a" magnitude 21; apply condition "a:grab" of "grappled" to "a"; damage "a" resource "hp" by 10;`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	p, err := w.CharacterStatus("a")
	if err != nil || !reflect.DeepEqual(p.Held, []core.HeldCondition{{Type: "unconscious", Resource: "hp"}}) {
		t.Fatal("held", p.Held, err)
	}
	for _, s := range p.Stats {
		if s.Stat == "strength" && s.Effective != 21 || s.Stat == "speed" && (s.Effective != 0 || len(s.Bounds) != 2) {
			t.Fatalf("%+v", s)
		}
	}
}

func TestConditionBoundDiagnosticSites(t *testing.T) {
	held := `held_at_minimum "hp"; modifier "speed" stat "speed" at most 0;`
	for _, tc := range []struct{ from, to, at, code string }{
		{held, `held_at_minimum "hp"; held_at_minimum "hp"; modifier "speed" stat "speed" at most 0;`, `held_at_minimum "hp"; modifier`, "E_DUPLICATE"},
		{held, `held_at_minimum "hp"; ends_at_minimum "hp"; modifier "speed" stat "speed" at most 0;`, `"hp"; ends_at_minimum`, "E_RULE"},
		{held, `held_at_minimum "hp"; modifier "slow" stat "speed" add -5;`, `"slow"`, "E_RULE"},
		{held, `held_at_minimum "hp"; modifier "slow" stat "speed" at most magnitude;`, `"slow"`, "E_RULE"},
		{held, `held_at_minimum " "; modifier "speed" stat "speed" at most 0;`, `" "`, "E_ID"},
		{`at most 0; }
condition type "belt"`, `at exactly 0; }
condition type "belt"`, `exactly`, "E_PARSE"},
		{`modifier "speed" stat "speed" at most 0; }
condition type "belt"`, `modifier "speed" resource "hp" maximum at most 0; }
condition type "belt"`, `at most 0; }
condition type "belt"`, "E_PARSE"},
	} {
		if strings.Count(boundSource, tc.from) != 1 {
			t.Fatal("fixture", tc.from)
		}
		bad := strings.Replace(boundSource, tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad", bad)
		index := strings.Index(bad, tc.to) + strings.Index(tc.to, tc.at)
		line, col := strings.Count(bad[:index], "\n")+1, index-strings.LastIndex(bad[:index], "\n")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at %d:%d, got %+v", tc.to, tc.code, line, col, ds)
		}
	}
}
