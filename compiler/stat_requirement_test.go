package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const statRequirementSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "level" = 1; resource "hp" = 5 min 0 max 10; }
character "b" named "B" at "room" { resource "hp" = 5 min 0 max 10; }
ability "grow" named "Growth" {
 parameter "actor" character;
 action actor param "actor" phase exploration;
 require stat_between param "actor" stat "level" min 1 max 1;
 expand param "actor" resource "hp" by 6;
}
adventure {
 party "a";
 health "hp";
 seed 1 2;
 grant "a" { ability "grow"; }
}`

func TestStatRequirementCompilerIdentityAndPositions(t *testing.T) {
	b, ds := compiler.CompileWorld("growth", statRequirementSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	requestSource := `invoke "grow" { "actor" = character "a"; };`
	ops, ds := compiler.CompileActions("first", requestSource, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileActions("second", "\n"+requestSource, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("position changed intent", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "grow", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil || r.Events[0].Capacity.After != (core.Resource{Current: 5, Maximum: 16}) {
		t.Fatal(r, err)
	}
	for _, tc := range []struct{ from, to, token, code string }{
		{`min 1 max 1`, `min 2 max 1`, `1`, "E_RANGE"},
		{`min 1 max 1`, `min -9223372036854775809 max 1`, `-9223372036854775809`, "E_INTEGER"},
		{`min 1 max 1`, `min 1 max 9223372036854775808`, `9223372036854775808`, "E_INTEGER"},
		{`require stat_between param "actor"`, `require stat_between param "missing"`, `"missing"`, "E_PARAMETER"},
		{`require stat_between param "actor"`, `require stat_between "room"`, `"room"`, "E_TYPE"},
		{`require stat_between param "actor" stat "level"`, `require stat_between "a" stat "absent"`, `"absent"`, "E_STAT"},
	} {
		s := strings.Replace(statRequirementSource, tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad-range", s)
		at := strings.Index(s, tc.to) + strings.LastIndex(tc.to, tc.token)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Source != "bad-range" || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
	bad := strings.Replace(requestSource, `character "a"`, `character "b"`, 1)
	_, ds = compiler.CompileActions("missing-bound-stat", bad, b)
	if len(ds) != 1 || ds[0].Code != "E_STAT" || ds[0].Position.Column != strings.Index(bad, `"b"`)+1 {
		t.Fatal("bound selector position", ds)
	}
	wide := strings.Replace(statRequirementSource, `min 1 max 1`, `min -9223372036854775808 max 9223372036854775807`, 1)
	if _, ds := compiler.CompileWorld("endpoints", wide); len(ds) != 0 {
		t.Fatal(ds)
	}
}
