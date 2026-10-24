package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func hitConditionSource(t *testing.T) string {
	s := battleSource(t)
	s = strings.Replace(s, `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead;
 hit_condition "sap" definition "longsword" mode "one" attack "slash" requires "martial" apply "sapped";`, 1)
	return s + `condition type "sapped" { instances coexist; until_source_turn; ends_after_attack; }`
}
func TestWeaponHitConditionSource(t *testing.T) {
	s := hitConditionSource(t)
	b, ds := compiler.CompileWorld("hit", s)
	if len(ds) != 0 || len(b.Battle.Rules.HitConditions) != 1 || !b.ConditionDefinitions[len(b.ConditionDefinitions)-1].UntilSourceTurn {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileWorld("other", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source identity", ds)
	}
	action := `battle attack "gate-encounter" round 1 cursor 0 by "tess" using "grant" target "raider" finish true condition "sap" as "new" {};`
	ops, ds := compiler.CompileActions("intent", action, b)
	if len(ds) != 0 || ops[0].WeaponAttack.HitCondition.Instance != "new" {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ from, to, at string }{
		{`definition "longsword" mode`, `definition "missing" mode`, `"missing"`},
		{`mode "one" attack`, `mode "missing" attack`, `"missing"`},
		{`attack "slash" requires`, `attack "missing" requires`, `"missing"`},
		{`apply "sapped"`, `apply "missing"`, `"missing"`},
		{`until_source_turn;`, `until_source_turn; until_source_turn;`, `until_source_turn;`},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		_, ds := compiler.CompileWorld("bad", bad)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal("wrong source occurrence", tc, ds)
		}
	}
}
