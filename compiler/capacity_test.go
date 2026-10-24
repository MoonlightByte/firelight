package compiler_test

import (
	"errors"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const capacitySource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { resource "hp" = 5 min 0 max 10; }
ability "grow" named "Capacity growth" {
 parameter "target" character;
 parameter "amount" int;
 expand param "target" resource "hp" by param "amount";
 expand "a" resource "hp" by 2;
}`

func TestExpandSourceBindingDiagnosticsAndIdentity(t *testing.T) {
	b, ds := compiler.CompileWorld("growth-world", capacitySource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "grow" { "target" = character "a"; "amount" = int 6; };`
	ops, ds := compiler.CompileActions("one", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileActions("two", "\n  "+source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("source affects intent", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	request := core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}
	r, err := w.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	if r.Events[0].Capacity.After != (core.Resource{Current: 5, Maximum: 16}) || r.Events[1].Capacity.After != (core.Resource{Current: 5, Maximum: 18}) {
		t.Fatal(r)
	}
	request.Operations = other
	retry, err := w.Execute(request)
	if err != nil || !reflect.DeepEqual(r, retry) {
		t.Fatal("position changed retry", err)
	}
	for _, tc := range []struct{ source, token string }{
		{`expand "room" resource "hp" by 1;`, `"room"`},
		{`expand "a" resource "missing" by 1;`, `"missing"`},
		{`expand "a" resource "hp" by 0;`, `0`},
		{`expand "a" resource "hp" by 9223372036854775808;`, `9223372036854775808`},
	} {
		_, ds := compiler.CompileActions("bad-growth", tc.source, b)
		if len(ds) != 1 || ds[0].Position.Source != "bad-growth" || ds[0].Position.Line != 1 || ds[0].Position.Column != strings.Index(tc.source, tc.token)+1 {
			t.Fatal(tc, ds)
		}
	}
	for _, tc := range []struct {
		old, new  string
		line, col int
	}{
		{`expand "a"`, `expand "room"`, 8, 9},
		{`by 2;`, `by 0;`, 8, 30},
		{`expand "a" resource "hp"`, `expand "a" resource "missing"`, 8, 22},
		{`"target" character`, `"target" int`, 7, 15},
	} {
		_, ds := compiler.CompileWorld("bad-definition", strings.Replace(capacitySource, tc.old, tc.new, 1))
		if len(ds) != 1 || ds[0].Position.Line != tc.line || ds[0].Position.Column != tc.col {
			t.Fatal(tc, ds)
		}
	}
	// A forged typed invocation still fails with semantic context, no invented source.
	bad := core.Request{ID: "bad-typed", ExpectedRevision: w.Revision(), Actor: request.Actor, Operations: []core.Operation{{Invoke: &core.InvokeAbility{Ability: "grow", Arguments: []core.AbilityArgument{{Name: "target", Value: core.AbilityValue{Character: ptrCharacter("a")}}, {Name: "amount", Value: core.AbilityValue{Integer: ptrInt(-1)}}}}}}}
	before := w.ExportCheckpoint()
	_, err = w.Execute(bad)
	var ae *core.AbilityError
	if !errors.As(err, &ae) || ae.Definition != "grow" || ae.Step != 0 || ae.Parameter != "amount" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("typed boundary", err)
	}
}
func ptrCharacter(v core.CharacterID) *core.CharacterID { return &v }
func ptrInt(v int64) *int64                             { return &v }

func TestExpandSourceFlatBranches(t *testing.T) {
	definition := `ability "branch-growth" named "Growth" {
 parameter "actor" character;
 parameter "amount" int;
 action actor param "actor" phase exploration;
 check "growth-check" die 20 plus 0 against 0 {
  success { expand param "actor" resource "hp" by param "amount"; }
  failure { expand param "actor" resource "hp" by 2; }
 }
}`
	b, ds := compiler.CompileWorld("branch-world", strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead; action "branch-growth";`, 1)+definition)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	ops, ds := compiler.CompileActions("branch-use", `invoke "branch-growth" { "actor" = character "tess"; "amount" = int 6; };`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "branch", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 3 || r.Events[1].Capacity == nil || r.Events[1].Capacity.Requested != 6 {
		t.Fatal("compiled branch lost original growth", r)
	}
}
