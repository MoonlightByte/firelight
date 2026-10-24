package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const capacityFormulaSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "constitution" = 12; resource "hp" = 5 min 0 max 10; }
character "b" named "B" at "room" { resource "hp" = 5 min 0 max 10; }
ability "grow" named "Authored capacity formula" {
 parameter "actor" character;
 parameter "target" character;
 expand param "target" resource "hp" by formula from param "actor" min 1 {
  constant 4;
  term stat "constitution" offset -10 divide 2;
 };
}`

func TestCapacityFormulaSourceAndPositions(t *testing.T) {
	b, ds := compiler.CompileWorld("formula", capacityFormulaSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "grow" { "actor" = character "a"; "target" = character "b"; };`
	ops, ds := compiler.CompileActions("first", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileActions("second", "\n "+source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("formula source changed identity", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "formula", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil || r.Events[0].Capacity.Requested != 5 {
		t.Fatal(r, err)
	}
	for _, tc := range []struct{ from, to, token string }{
		{`min 1 {`, `min 0 {`, `0`},
		{`from param "actor"`, `from "room"`, `"room"`},
		{`from param "actor"`, `from param "undeclared"`, `"undeclared"`},
		{`divide 2`, `divide 0`, `0`},
		{`divide 2;`, `divide 2 requires "missing-condition";`, `"missing-condition"`},
		{`divide 2;`, `divide 2 unless "missing-condition";`, `"missing-condition"`},
		{`divide 2;`, `divide 2 unless slot "body";`, `"body"`},
		{`constant 4`, `constant 9223372036854775808`, `9223372036854775808`},
		{`resource "hp" by formula`, `resource "missing" by formula`, `"missing"`},
	} {
		s := strings.Replace(capacityFormulaSource, tc.from, tc.to, 1)
		// Use a literal target to check the Resource at its definition occurrence.
		if strings.Contains(tc.to, "missing") {
			s = strings.Replace(s, `expand param "target"`, `expand "a"`, 1)
		}
		_, ds := compiler.CompileWorld("bad-formula", s)
		at := strings.Index(s, tc.to) + strings.LastIndex(tc.to, tc.token)
		if len(ds) != 1 || ds[0].Position.Source != "bad-formula" || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
	literal := strings.Replace(capacityFormulaSource, `from param "actor"`, `from "a"`, 1)
	literal = strings.Replace(literal, `term stat "constitution"`, `term stat "missing"`, 1)
	_, ds = compiler.CompileWorld("literal-missing", literal)
	if len(ds) != 1 || ds[0].Position.Line != 10 || ds[0].Position.Column != 13 || ds[0].Code != "E_STAT" {
		t.Fatal("formula term occurrence", ds)
	}
	badSource := strings.Replace(source, `"actor" = character "a"`, `"actor" = character "b"`, 1)
	_, ds = compiler.CompileActions("bound-missing", badSource, b)
	if len(ds) != 1 || ds[0].Code != "E_STAT" || ds[0].Position.Column != strings.Index(badSource, `"b"`)+1 {
		t.Fatal("bound formula occurrence", ds)
	}
}
