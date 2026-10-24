package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func allowanceSource(t *testing.T) string {
	s := authoredRollSource(t)
	s = strings.Replace(s, `resource "hp"`, `resource "extra" = 0 min 0 max 1; resource "hp"`, 1)
	return strings.Replace(s, `action "rolled-care";`, `action "rolled-care"; allowance "extra" weapon true { action "rolled-care"; };`, 1)
}

func TestBattleAllowanceSourceAndPositions(t *testing.T) {
	s := allowanceSource(t)
	b, ds := compiler.CompileWorld("allowance", s)
	if len(ds) != 0 || len(b.Battle.Rules.Allowances) != 1 || b.Battle.Rules.Allowances[0].Actions[0] != "rolled-care" {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileWorld("other", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source metadata changed semantic identity", ds)
	}
	for _, tc := range []struct{ from, to, at string }{
		{`allowance "extra"`, `allowance "missing"`, `"missing"`},
		{`weapon true { action "rolled-care"; }`, `weapon true { action "unknown"; }`, `"unknown"`},
		{`weapon true { action "rolled-care"; }`, `weapon true { action "rolled-care"; action "rolled-care"; }`, `"rolled-care"`},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		_, ds := compiler.CompileWorld("bad", bad)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal("allowance declaration use site", tc, ds)
		}
	}
	source := `invoke "rolled-care" allowance "missing" { "actor" = character "tess"; "target" = character "tess"; };`
	_, ds = compiler.CompileActions("bad-invoke", source, b)
	if len(ds) != 1 || ds[0].Position.Column != strings.Index(source, `"missing"`)+1 {
		t.Fatal("invocation allowance position", ds)
	}
}
