package compiler

import (
	"encoding/json"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const abilityCatalog = `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" { stat "defense" = 10; resource "hp" = 10 min 0 max 20; }
character "n" named "N" at "l" {}
item "i" named "I" { owner "c"; }
condition type "t" { instances coexist; modifier "ward" stat "defense" add 3; }
`
const abilityDefinitionSource = `ability "aid" named "Aid" {
 parameter "target" character;
 parameter "amount" int;
 parameter "instance" condition_id;
 heal param "target" resource "hp" by param "amount";
 apply condition param "instance" of "t" to param "target";
}
`
const invokeSource = `invoke "aid" {
 "target" = character "c";
 "amount" = int 4;
 "instance" = condition_id "one";
};`

func TestAbilityPunctuationParameterNamesAreData(t *testing.T) {
	b, ds := CompileWorld("punctuation.nql", abilityCatalog+`ability "a" named "A" { parameter "}" int; heal "c" resource "hp" by param "}"; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	one := int64(1)
	want := []core.Operation{{Invoke: &core.InvokeAbility{Ability: "a", Arguments: []core.AbilityArgument{{Name: "}", Value: core.AbilityValue{Integer: &one}}}}}}
	for _, source := range []string{`invoke "a" { "}" = int 1; };`, `invoke "a" { "\u007d" = int 1; };`} {
		ops, ds := CompileActions("punctuation-action.nql", source, b)
		if len(ds) != 0 || !reflect.DeepEqual(ops, want) {
			t.Errorf("quoted punctuation must equal directly typed binding: %v, %v", ops, ds)
			continue
		}
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.Execute(core.Request{ID: "punctuation", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
		if err != nil {
			t.Fatal(err)
		}
		status, err := w.CharacterStatus("c")
		if err != nil || status.Resources["hp"].Current != 11 || status.Revision != 1 {
			t.Fatal("punctuation invocation outcome", status, err)
		}
	}
}

func TestAbilityDefinitionDiagnosticOccurrences(t *testing.T) {
	for _, tc := range []struct {
		name, source, code string
		line, column       int
	}{
		{"duplicate Ability", `ability "a" named "A" { heal "c" resource "hp" by 1; }
ability "a" named "B" { heal "c" resource "hp" by 1; }`, "E_DUPLICATE", 8, 9},
		{"duplicate parameter", `ability "a" named "A" {
 parameter "x" character;
 parameter "x" character;
 heal param "x" resource "hp" by 1;
}`, "E_DUPLICATE", 9, 12},
		{"unknown body parameter", `ability "a" named "A" {
 parameter "target" character;
 heal param "missing" resource "hp" by 1;
}`, "E_PARAMETER", 9, 13},
		{"wrong body parameter kind", `ability "a" named "A" {
 parameter "target" int;
 heal param "target" resource "hp" by 1;
}`, "E_TYPE", 9, 13},
		{"nonpositive known literal", `ability "a" named "A" {
 parameter "target" character;
 heal param "target" resource "hp" by 0;
}`, "E_AMOUNT", 9, 39},
		{"invalid resource selector", `ability "a" named "A" {
 parameter "target" character;
 heal param "target" resource "" by 1;
}`, "E_ID", 9, 31},
		{"unknown Condition type", `ability "a" named "A" {
 parameter "target" character;
 apply condition "new" of "missing" to param "target";
}`, "E_CONDITION_TYPE", 9, 27},
		{"empty body", `ability "a" named "A" {
}`, "E_LIMIT", 8, 1},
		{"nested invocation", `ability "a" named "A" {
 invoke "a" {};
}`, "E_PARSE", 8, 2},
		{"unsupported primitive", `ability "a" named "A" {
 move "c" to "l";
}`, "E_PARSE", 8, 2},
		{"loop", `ability "a" named "A" {
 while true {}
}`, "E_PARSE", 8, 2},
		{"expression", `ability "a" named "A" { heal "c" resource "hp" by 1 +2; }`, "E_PARSE", 7, 53},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ds := CompileWorld("ability-definition.nql", abilityCatalog+tc.source)
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != tc.line || ds[0].Column != tc.column || ds[0].Source != "ability-definition.nql" {
				t.Fatalf("want %s %d:%d got %v", tc.code, tc.line, tc.column, ds)
			}
			if !reflect.DeepEqual(b, core.Blueprint{}) {
				t.Fatal("partial declaration IR escaped")
			}
		})
	}
}
func TestAbilityInvocationDiagnosticsAndHonestContext(t *testing.T) {
	b, ds := CompileWorld("catalog.nql", abilityCatalog+abilityDefinitionSource)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		name, source, code string
		line, column       int
	}{
		{"unknown Ability", `invoke "missing" {};`, "E_ABILITY", 1, 8},
		{"missing argument", "invoke \"aid\" {\n};", "E_ARGUMENT", 2, 1},
		{"extra argument", "invoke \"aid\" {\n \"extra\" = int 1;\n};", "E_ARGUMENT", 2, 2},
		{"duplicate binding", "invoke \"aid\" {\n \"target\" = character \"c\";\n \"target\" = character \"c\";\n};", "E_DUPLICATE", 3, 2},
		{"wrong kind", strings.Replace(invokeSource, `character "c"`, `item "i"`, 1), "E_TYPE", 2, 18},
		{"wrong entity type", strings.Replace(invokeSource, `character "c"`, `character "i"`, 1), "E_TYPE", 2, 23},
		{"bound resource missing", strings.Replace(invokeSource, `character "c"`, `character "n"`, 1), "E_RESOURCE", 2, 23},
		{"bad signed amount", strings.Replace(invokeSource, "int 4", "int 0", 1), "E_AMOUNT", 3, 17},
		{"integer overflow", strings.Replace(invokeSource, "int 4", "int 9223372036854775808", 1), "E_INTEGER", 3, 17},
		{"bad creation ID", strings.Replace(invokeSource, `condition_id "one"`, `condition_id ""`, 1), "E_ID", 4, 28},
		{"CRLF and Unicode", "// heading\r\n\tinvoke \"aid\" { \"target\" = character \"é\"; \"amount\" = int 1; \"instance\" = condition_id \"one\"; };", "E_REFERENCE", 2, 38},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, ds := CompileActions("invoke.nql", tc.source, b)
			if ops != nil || len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != tc.line || ds[0].Column != tc.column || ds[0].Source != "invoke.nql" {
				t.Fatalf("want %s %d:%d got %v", tc.code, tc.line, tc.column, ds)
			}
			if ds[0].Expected == "" || ds[0].Actual == "" || ds[0].Message == "" {
				t.Fatal("incomplete diagnostic")
			}
		})
	}
	// A catalog alone has no world-source positions. A known literal error is
	// honestly attributed to the invocation, with semantic definition/step detail.
	b.AbilityDefinitions[0].Steps[0].Heal.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptrCompiler(int64(0))}}
	_, ds = CompileActions("different-source.nql", invokeSource, b)
	if len(ds) != 1 || ds[0].Line != 1 || ds[0].Column != 8 || !strings.Contains(ds[0].Message, `step 0 operand "amount"`) {
		t.Fatal("invented definition source", ds)
	}
	err := b.CheckOperationStatic(core.Operation{Invoke: &core.InvokeAbility{Ability: "aid"}})
	var f *core.Fault
	var context *core.AbilityError
	if !errors.As(err, &f) || !errors.As(err, &context) || context.Step != 0 || context.Operand != "amount" {
		t.Fatal("typed context", err)
	}
	encoded, _ := json.Marshal(context)
	for _, s := range []string{`"source"`, `"line"`, `"column"`} {
		if strings.Contains(string(encoded), s) {
			t.Fatal("typed error invented source")
		}
	}
}
func ptrCompiler[T any](v T) *T { return &v }

func TestAbilityForwardNamespacesAndParameterOccurrence(t *testing.T) {
	source := `ability "c" named "Aid" {
 heal param "c" resource "hp" by 1;
 apply condition param "new" of "c" to param "c";
 parameter "c" character;
 parameter "new" condition_id;
}
rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" { stat "defense" = 10; resource "hp" = 10 min 0 max 20; }
condition type "c" { instances coexist; modifier "c" stat "defense" add 1; }
condition "c" of "c" to "c";
`
	b, ds := CompileWorld("forward.nql", source)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	ops, ds := CompileActions("use.nql", `invoke "c" { "new" = condition_id "new"; "c" = character "c"; };`, b)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	_, e = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
	if e != nil {
		t.Fatal(e)
	}
	ex, e := w.Explain("c", "defense")
	if e != nil || ex.Effective != 12 || len(ex.Contributions) != 2 {
		t.Fatal("namespace mechanics", e)
	}
	// Same parameter used by both body steps; step1 missing-stat error must select
	// the actual target binding after step0's resource check succeeds.
	delete(b.Characters[0].Stats, "defense")
	b.Conditions = nil
	_, ds = CompileActions("use.nql", `invoke "c" { "new" = condition_id "new"; "c" = character "c"; };`, b)
	if len(ds) != 1 || ds[0].Code != "E_STAT" || ds[0].Line != 1 || ds[0].Column != 58 || !strings.Contains(ds[0].Message, "step 1") {
		t.Fatal("wrong repeated parameter occurrence", ds)
	}
}
func TestAbilitySourceIdentityCatalogDetachmentAndContinuation(t *testing.T) {
	b, ds := CompileWorld("world", abilityCatalog+abilityDefinitionSource)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	before, _ := json.Marshal(b)
	first, ds := CompileActions("first.nql", invokeSource, b)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	second, ds := CompileActions("other.nql", "// intent\r\n\tinvoke \"aid\" { \"instance\" = condition_id \"one\"; \"amount\" = int +04; \"target\" = character \"\\u0063\"; };", b)
	if len(ds) > 0 || !reflect.DeepEqual(first, second) {
		t.Fatal("source metadata/order changed invocation identity", ds)
	}
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("compiler mutated catalog")
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	r := core.Request{ID: "same", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: first}
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	_, bad := CompileActions("broken", `invoke "aid" { "amount" = int ; };`, b)
	if len(bad) != 1 {
		t.Fatal("malformed source accepted")
	}
	heal, ds := CompileActions("valid", `heal "c" resource "hp" by 1;`, b)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	_, e = w.Execute(core.Request{ID: "later", ExpectedRevision: 1, Actor: r.Actor, Operations: heal})
	if e != nil {
		t.Fatal("continuation", e)
	}
	r.Operations = second
	again, e := w.Execute(r)
	if e != nil || !reflect.DeepEqual(again, receipt) || w.Revision() != 2 {
		t.Fatal("late equivalent-source retry", e)
	}
	rw, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	again, e = rw.Execute(r)
	if e != nil || !reflect.DeepEqual(again, receipt) {
		t.Fatal("reconstructed retry", e)
	}
	secondBytes, _ := json.Marshal(second)
	*first[0].Invoke.Arguments[0].Value.Integer = 99
	b.AbilityDefinitions[0].Parameters[0].ID = "changed"
	after, _ = json.Marshal(second)
	if string(secondBytes) != string(after) {
		t.Fatal("IR/canonical input aliases")
	}
}
func FuzzAbilityCompiler(f *testing.F) {
	f.Add(abilityCatalog + abilityDefinitionSource)
	f.Add(invokeSource)
	f.Add(`ability "x" named "X" { invoke "x" {}; }`)
	f.Add("\xff")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 8192 {
			return
		}
		b, _ := CompileWorld("catalog", abilityCatalog+abilityDefinitionSource)
		_, worldDS := CompileWorld("fuzz", s)
		_, actionDS := CompileActions("fuzz", s, b)
		for _, d := range append(worldDS, actionDS...) {
			if d.Source != "fuzz" || d.Line < 1 || d.Column < 1 || d.Code == "" {
				t.Fatal("invalid diagnostic", d)
			}
		}
	})
}
