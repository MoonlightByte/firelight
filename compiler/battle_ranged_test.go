package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

func rangedSource(t *testing.T) string {
	t.Helper()
	s := strings.ReplaceAll(battleSource(t), `melee reach 5 stat "strength";`, `ranged normal 80 maximum 320 stat "dexterity" ammunition "arrows";`)
	s = strings.Replace(s, `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead;
 ranged close 5 recover 1 of 2 ticks 60 {
  ignore_threat "blind";
  concealed "hidden";
 }`, 1)
	return s + `
item "arrows" named "Arrows" { quantity 5; owner "tess"; custody character "tess"; }
condition type "blind" { instances coexist; }
condition type "hidden" { instances coexist; }
`
}

func TestRangedCompilerDefinitionIntentAndSourceIdentity(t *testing.T) {
	s := rangedSource(t)
	b, ds := compiler.CompileWorld("ranged", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := &core.RangedProfile{Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionOrigin: "arrows"}
	attack := b.Equipment.Definitions[0].Modes[0].Attacks[0]
	if attack.Melee != nil || !reflect.DeepEqual(attack.Ranged, want) {
		t.Fatalf("profile: %+v", attack)
	}
	policy := &core.BattleRangedPolicy{CloseDistance: 5, IgnoreThreat: []core.ConditionTypeID{"blind"}, Concealed: []core.ConditionTypeID{"hidden"}, RecoveryNumerator: 1, RecoveryDenominator: 2, RecoveryTicks: 60}
	if !reflect.DeepEqual(b.Battle.Rules.Ranged, policy) {
		t.Fatalf("policy: %+v", b.Battle.Rules.Ranged)
	}
	other, ds := compiler.CompileWorld("elsewhere", "\n\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source location changed definition", ds)
	}
	for _, tc := range []struct {
		source string
		want   core.Operation
	}{
		{`battle attack "fight" round 1 cursor 0 by "tess" using "grant" target "raider" finish true ammunition "arrows";`, core.Operation{WeaponAttack: &core.WeaponAttack{BattleWindow: core.BattleWindow{Encounter: "fight", Round: 1, Cursor: 0, Actor: "tess"}, Grant: "grant", Target: "raider", FinishTurn: true, Ammunition: "arrows"}}},
		{`battle recover "fight" by "tess" { ammunition "arrows" as "recovered"; };`, core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "fight", Actor: "tess", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}}},
		{`battle recover "fight" by "tess" {};`, core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "fight", Actor: "tess"}}},
	} {
		ops, ds := compiler.CompileActions("intent", tc.source, b)
		if len(ds) != 0 || !reflect.DeepEqual(ops, []core.Operation{tc.want}) {
			t.Fatalf("%s: %+v %+v", tc.source, ops, ds)
		}
		other, ds := compiler.CompileActions("elsewhere", "\n\n"+tc.source, b)
		if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
			t.Fatal("source location changed action identity", ds)
		}
	}
}

func TestRangedCompilerDiagnosticUseSites(t *testing.T) {
	s := rangedSource(t)
	for _, tc := range []struct{ from, to, at string }{
		{`normal 80`, `normal 0`, `0`},
		{`maximum 320`, `maximum 79`, `79`},
		{`maximum 320`, `maximum 1000001`, `1000001`},
		{`ammunition "arrows"`, `ammunition "gate"`, `"gate"`},
		{`ammunition "arrows"`, `ammunition "missing"`, `"missing"`},
		{`ammunition "arrows"`, `ammunition "tess-sword"`, `"tess-sword"`},
		{`recover 1 of 2`, `recover 3 of 2`, `3`},
		{`recover 1 of 2`, `recover 1 of 0`, `0`},
		{`ticks 60`, `ticks 0`, `0`},
		{`ignore_threat "blind";`, `ignore_threat "missing";`, `"missing"`},
		{`concealed "hidden";`, `concealed "hidden"; concealed "hidden";`, `"hidden"`},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		_, ds := compiler.CompileWorld("bad-ranged", bad)
		if len(ds) != 1 || ds[0].Position.Source != "bad-ranged" || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatalf("%+v: %+v", tc, ds)
		}
	}
	b, ds := compiler.CompileWorld("valid-ranged", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, at string }{
		{`battle attack "fight" round 1 cursor 0 by "tess" using "grant" target "raider" finish true ammunition "gate";`, `"gate"`},
		{`battle recover "fight" by "gate" {};`, `"gate"`},
		{`battle recover "fight" by "tess" { ammunition "gate" as "new"; };`, `"gate"`},
		{`battle recover "fight" by "tess" { ammunition "tess-sword" as "new"; };`, `"tess-sword"`},
		{`battle recover "fight" by "tess" { ammunition "arrows" as "tess"; };`, `"tess";`},
		{`battle recover "fight" by "tess" { ammunition "arrows" as "one"; ammunition "arrows" as "two"; };`, `"arrows" as "two"`},
	} {
		_, ds := compiler.CompileActions("bad-intent", "\n"+tc.source, b)
		if len(ds) != 1 || ds[0].Position.Source != "bad-intent" || ds[0].Position.Line != 2 || ds[0].Position.Column != strings.LastIndex(tc.source, tc.at)+1 {
			t.Fatalf("%s: %+v", tc.source, ds)
		}
	}
}
