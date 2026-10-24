package compiler_test

import (
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

const concentrationSource = checkSource + `condition type "incapacitated" { instances coexist; }
condition type "stunned" { instances coexist; includes "incapacitated"; }
condition type "focus" { instances replace; ends_on_condition "incapacitated"; }
condition_check "focus" after_damage resource "hp" stat "save:dex" sides 20 minimum 10 divide 2 maximum 30;
`

func TestDamageCheckSourceAndAction(t *testing.T) {
	b, ds := compiler.CompileWorld("focus", concentrationSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if want := []core.ConditionCheckPolicy{{Condition: "focus", Resource: "hp", Stat: "save:dex", Sides: 20, Minimum: 10, Divisor: 2, Maximum: 30}}; !reflect.DeepEqual(b.ConditionChecks, want) {
		t.Fatalf("%+v", b.ConditionChecks)
	}
	for _, d := range b.ConditionDefinitions {
		if d.ID == "stunned" && !reflect.DeepEqual(d.Includes, []core.ConditionTypeID{"incapacitated"}) {
			t.Fatalf("%+v", d)
		}
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("act", `apply condition "a:focus" of "focus" to "a";
damage "a" resource "hp" by 4 faces "a:focus" 8;
damage "a" resource "hp" by 1;
apply condition "a:stun" of "stunned" to "a";`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if want := []core.ConditionRollInput{{Condition: "a:focus", Target: "a", Face: 8}}; !reflect.DeepEqual(ops[1].ConditionRolls, want) || ops[2].ConditionRolls != nil {
		t.Fatalf("%+v %+v", ops[1].ConditionRolls, ops[2].ConditionRolls)
	}
	batch, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	// 8 + 2 holds DC 10; the engine rolls the second check; Stunned ends
	// whatever concentration survives.
	var got []core.StatCheckResult
	ended := false
	for _, e := range batch.Events {
		if e.StatCheck != nil {
			got = append(got, *e.StatCheck)
		}
		ended = ended || e.Kind == core.ConditionRemoved
	}
	if len(got) != 2 || got[0].Kept != 8 || !*got[0].Success || *got[0].Condition != "a:focus" || got[0].Damage != 4 || !got[1].Drawn || !ended {
		t.Fatalf("%+v", got)
	}
	// Faces clauses repeat, one per instance, in check order.
	ops, ds = compiler.CompileActions("act", `damage "a" resource "hp" by 4 faces "a:x" 3 17 faces "a:y" 2;`, b)
	if want := []core.ConditionRollInput{{Condition: "a:x", Target: "a", Face: 3}, {Condition: "a:x", Target: "a", Face: 17}, {Condition: "a:y", Target: "a", Face: 2}}; len(ds) != 0 || !reflect.DeepEqual(ops[0].ConditionRolls, want) {
		t.Fatalf("%+v %+v", ds, ops)
	}
}

func TestDamageCheckDiagnosticSites(t *testing.T) {
	melee, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	var many strings.Builder
	for i := range 17 {
		fmt.Fprintf(&many, "condition_check \"focus\" after_damage resource \"r%d\" stat \"save:dex\" sides 20 minimum 10 divide 2 maximum 30;\n", i)
	}
	check := `condition_check "focus" after_damage resource "hp" stat "save:dex" sides 20 minimum 10 divide 2 maximum 30;`
	with := func(old, new string) string { return strings.Replace(concentrationSource, old, new, 1) }
	for _, tc := range []struct{ source, at, code string }{
		{with(`includes "incapacitated"`, `includes "stunned"`), `"stunned";`, "E_CONDITION_TYPE"},
		{with(`includes "incapacitated";`, `includes "incapacitated"; includes "incapacitated";`), `"incapacitated"; }`, "E_CONDITION_TYPE"},
		{with(`includes "incapacitated"`, `includes "missing"`), `"missing"`, "E_CONDITION_TYPE"},
		{concentrationSource + `condition type "worse" { instances coexist; includes "stunned"; }`, `"stunned"; }`, "E_RULE"},
		{with(`includes "incapacitated";`, strings.Repeat(`includes "x"; `, 9)), `"stunned"`, "E_LIMIT"},
		{string(melee) + check, "condition_check", "E_RULE"},
		{concentrationSource + many.String(), `condition_check "focus" after_damage resource "r15"`, "E_LIMIT"},
		{with(`condition_check "focus"`, `condition_check "missing"`), `"missing"`, "E_CONDITION_TYPE"},
		{with(`resource "hp" stat`, `resource " " stat`), `" "`, "E_ID"},
		{with(`stat "save:dex" sides`, `stat "" sides`), `""`, "E_ID"},
		{with("sides 20", "sides 1"), "1 minimum", "E_RULE"},
		{with("sides 20", "sides 1001"), "1001", "E_RULE"},
		{with("minimum 10", "minimum 0"), "0 divide 2 maximum", "E_RULE"},
		{with("divide 2 maximum", "divide 0 maximum"), "0 maximum", "E_RULE"},
		{with("maximum 30", "maximum 9"), "9;", "E_RULE"},
		{concentrationSource + "\n" + check, "\n" + check, "E_DUPLICATE"},
		{with("after_damage", "after_heal"), "after_heal", "E_PARSE"},
		{concentrationSource + `character "c" named "C" at "room" { resource "hp" = 5 min 0 max 5; }
condition "c:focus" of "focus" to "c";`, `"c";`, "E_STAT"},
	} {
		_, ds := compiler.CompileWorld("bad", tc.source)
		index := strings.Index(tc.source, tc.at)
		if tc.at == "\n"+check {
			index = strings.LastIndex(tc.source, `"focus" after_damage`)
		}
		line, col := strings.Count(tc.source[:index], "\n")+1, index-strings.LastIndex(tc.source[:index], "\n")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at %d:%d, got %+v", tc.at, tc.code, line, col, ds)
		}
	}
	b, ds := compiler.CompileWorld("focus", concentrationSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	battle, ds := compiler.CompileWorld("melee", string(melee))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		source, at, code string
		catalog          core.Blueprint
	}{
		{`damage "a" resource "hp" by 4 faces "a:focus" 1 2 3;`, "faces", "E_ROLL", b},
		{`damage "a" resource "hp" by 4 faces "a:focus" 0;`, "0;", "E_ROLL", b},
		{`damage "a" resource "hp" by 4 faces "a:focus" 1001;`, "1001", "E_ROLL", b},
		{`damage "a" resource "hp" by 4 faces "a:focus";`, ";", "E_PARSE", b},
		{`damage "a" resource "hp" by 4 faces 9;`, "9", "E_PARSE", b},
		{`heal "a" resource "hp" by 4 faces "a:focus" 9;`, "faces", "E_PARSE", b},
		{`damage "tess" resource "hp" by 4 faces "tess:focus" 9;`, "faces", "E_RULE", battle},
	} {
		_, ds := compiler.CompileActions("act", tc.source, tc.catalog)
		col := strings.Index(tc.source, tc.at) + 1
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != 1 || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at 1:%d, got %+v", tc.source, tc.code, col, ds)
		}
	}
}
