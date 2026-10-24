package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestConditionCheckAuthoredPolicyAndPositions(t *testing.T) {
	s := strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead;
 condition_check "maintained" after_damage resource "hp" stat "defense" sides 20 minimum 10 divide 2 maximum 30;`, 1) + `
condition type "maintained" { instances replace; ends_at_minimum "hp"; }
condition "casting" of "maintained" to "tess";
`
	b, ds := compiler.CompileWorld("save-policy", s)
	if len(ds) != 0 || len(b.Battle.Rules.ConditionChecks) != 1 || b.Battle.Rules.ConditionChecks[0].Maximum != 30 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileWorld("different", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source changed semantic policy", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	op, ds := compiler.CompileActions("hit", `damage "tess" resource "hp" by 1;`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "raider"}, op[0])
	if err != nil || need.Stage != "condition" || need.Condition.Condition != "casting" || need.Condition.Threshold != 10 {
		t.Fatal(need, err)
	}
	for _, tc := range []struct{ from, to, token string }{
		{`condition_check "maintained"`, `condition_check "missing"`, `"missing"`},
		{`sides 20 minimum`, `sides 1 minimum`, `1`},
		{`divide 2 maximum`, `divide 0 maximum`, `0`},
		{`maximum 30;`, `maximum 9;`, `9`},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.token)
		_, ds := compiler.CompileWorld("bad-save", bad)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal(tc.to, ds)
		}
	}
}
