package compiler

import (
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const m10DamageDefinitionSource = `ability "pay" named "Pay" {
 parameter "target" character;
 parameter "amount" int;
 damage param "target" resource "hp" by param "amount";
}
`
const m10DamageInvocationSource = `invoke "pay" {
 "target" = character "c";
 "amount" = int 2;
};`

func TestM10DamageCompilerOccurrencesAndHonestContext(t *testing.T) {
	b, ds := CompileWorld("catalog", abilityCatalog+m10DamageDefinitionSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		name, source, code, context string
		world                       bool
	}{
		{"direct absent target", `damage «"absent" resource "hp" by 1;`, "E_REFERENCE", "", false},
		{"direct wrong target kind", `damage «"l" resource "hp" by 1;`, "E_TYPE", "", false},
		{"direct missing resource", `damage "c" resource «"missing" by 1;`, "E_RESOURCE", "", false},
		{"direct zero", `damage "c" resource "hp" by «0;`, "E_AMOUNT", "", false},
		{"direct negative", `damage "c" resource "hp" by «-1;`, "E_AMOUNT", "", false},
		{"direct overflow", `damage "c" resource "hp" by «9223372036854775808;`, "E_INTEGER", "", false},
		{"direct missing amount", `damage "c" resource "hp" by «;`, "E_PARSE", "", false},
		{"literal target", abilityCatalog + `ability "a" named "A" { damage «"i" resource "hp" by 1; }`, "E_TYPE", `step 0 operand "target"`, true},
		{"literal resource obligation", abilityCatalog + `ability "a" named "A" { damage «"n" resource "hp" by 1; }`, "E_RESOURCE", `step 0 operand "target"`, true},
		{"selector identity", abilityCatalog + `ability "a" named "A" { damage "c" resource «"" by 1; }`, "E_ID", `step 0 operand "resource"`, true},
		{"literal amount", abilityCatalog + `ability "a" named "A" { damage "c" resource "hp" by «-1; }`, "E_AMOUNT", `step 0 operand "amount"`, true},
		{"body integer overflow", abilityCatalog + `ability "a" named "A" { damage "c" resource "hp" by «9223372036854775808; }`, "E_INTEGER", "", true},
		{"body integer underflow", abilityCatalog + `ability "a" named "A" { damage "c" resource "hp" by «-9223372036854775809; }`, "E_INTEGER", "", true},
		{"body unknown parameter", abilityCatalog + `ability "a" named "A" { damage param «"target" resource "hp" by 1; }`, "E_PARAMETER", `step 0 operand "target"`, true},
		{"body wrong parameter kind", abilityCatalog + `ability "a" named "A" { parameter "target" int; damage param «"target" resource "hp" by 1; }`, "E_TYPE", `step 0 operand "target"`, true},
		{"missing argument", "invoke \"pay\" { \"amount\" = int 2;\n«};", "E_ARGUMENT", `step -1 operand "argument"`, false},
		{"duplicate argument", `invoke "pay" { "target" = character "c"; «"target" = character "c"; "amount" = int 2; };`, "E_DUPLICATE", "", false},
		{"extra argument", `invoke "pay" { «"extra" = int 2; };`, "E_ARGUMENT", `step -1 operand "argument"`, false},
		{"wrong argument kind", `invoke "pay" { "target" = item «"i"; "amount" = int 2; };`, "E_TYPE", `step -1 operand "argument"`, false},
		{"wrong argument reference", `invoke "pay" { "target" = character «"l"; "amount" = int 2; };`, "E_TYPE", `step -1 operand "argument"`, false},
		{"bound missing resource", `invoke "pay" { "target" = character «"n"; "amount" = int 2; };`, "E_RESOURCE", `step 0 operand "target"`, false},
		{"bound zero", `invoke "pay" { "target" = character "c"; "amount" = int «0; };`, "E_AMOUNT", `step 0 operand "amount"`, false},
		{"bound negative", `invoke "pay" { "target" = character "c"; "amount" = int «-2; };`, "E_AMOUNT", `step 0 operand "amount"`, false},
		{"bound overflow", `invoke "pay" { "target" = character "c"; "amount" = int «9223372036854775808; };`, "E_INTEGER", "", false},
		{"bound underflow", `invoke "pay" { "target" = character "c"; "amount" = int «-9223372036854775809; };`, "E_INTEGER", "", false},
		{"UTF8 CRLF", "// é\r\n\tinvoke \"pay\" { \"target\" = character «\"é\"; \"amount\" = int 2; };", "E_REFERENCE", `step -1 operand "argument"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, line, column := spendMarkedPosition(t, tc.source)
			var ds []Diagnostic
			if tc.world {
				_, ds = CompileWorld("damage-world.nql", s)
			} else {
				ops, d := CompileActions("damage-use.nql", s, b)
				ds = d
				if ops != nil {
					t.Fatal("invalid source produced operations")
				}
			}
			file := "damage-use.nql"
			if tc.world {
				file = "damage-world.nql"
			}
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != file || ds[0].Line != line || ds[0].Column != column || !strings.Contains(ds[0].Message, tc.context) {
				t.Fatalf("want %s %s %d:%d %s; got %+v", tc.code, file, line, column, tc.context, ds)
			}
			if ds[0].Expected == "" || ds[0].Actual == "" || ds[0].Message == "" {
				t.Fatal("incomplete diagnostic", ds)
			}
		})
	}
	// No declaration AST is available in this edited typed catalog: invocation
	// context is honest; a definition location must not be invented.
	b.AbilityDefinitions[0].Steps[0].Damage.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptrCompiler(int64(0))}}
	_, ds = CompileActions("other-file.nql", m10DamageInvocationSource, b)
	if len(ds) != 1 || ds[0].Line != 1 || ds[0].Column != 8 || ds[0].Source != "other-file.nql" || !strings.Contains(ds[0].Message, `step 0 operand "amount"`) {
		t.Fatal(ds)
	}
}

func TestM10DamageCompilerSourceFreeIdentityAndSignedInt(t *testing.T) {
	source := abilityCatalog + `ability "damage" named "é" {
 parameter "}" int;
 parameter "c" character;
 damage param "c" resource "hp" by param "}";
}
condition type "negative" { instances coexist; modifier "penalty" stat "defense" add magnitude; }
ability "penalty" named "Penalty" {
 parameter "amount" int;
 apply condition "negative:one" of "negative" to "c" magnitude param "amount";
}`
	b, ds := CompileWorld("names.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	input := []string{`invoke "damage" { "}" = int 2; "c" = character "c"; };`, "// same typed intent\r\n invoke \"damage\" { \"c\" = character \"\\u0063\"; \"\\u007d\" = int +02; };"}
	var prior []core.Operation
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	var receipt core.Batch
	for i, s := range input {
		ops, ds := CompileActions([]string{"one.nql", "other.nql"}[i], s, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if i > 0 && !reflect.DeepEqual(prior, ops) {
			t.Fatal("source changed identity")
		}
		prior = ops
		got, err := w.Execute(core.Request{ID: "same", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !reflect.DeepEqual(got, receipt) {
			t.Fatal("retry receipt changed")
		}
		receipt = got
	}
	if w.Revision() != 1 {
		t.Fatal("retry executed twice")
	}
	status, _ := w.CharacterStatus("c")
	if status.Resources["hp"].Current != 8 {
		t.Fatal(status)
	}
	before, _ := json.Marshal(w.Snapshot())
	if ops, ds := CompileActions("bad", `damage "c" resource "hp" by ;`, b); ops != nil || len(ds) != 1 {
		t.Fatal("malformed")
	}
	after, _ := json.Marshal(w.Snapshot())
	if string(before) != string(after) {
		t.Fatal("compiler mutation")
	}
	ops, ds := CompileActions("signed", `invoke "penalty" { "amount" = int -2; };`, b)
	if len(ds) != 0 {
		t.Fatal("int globally narrowed", ds)
	}
	if _, err := w.Execute(core.Request{ID: "signed", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	ex, _ := w.Explain("c", "defense")
	if ex.Effective != 8 {
		t.Fatal("signed Condition magnitude", ex)
	}
	// Literal target/amount body and direct syntax both lower to the same mechanic.
	literal, ds := CompileWorld("literal", abilityCatalog+`ability "literal" named "Literal" { damage "c" resource "hp" by 2; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	lw, err := core.NewWorld(literal)
	if err != nil {
		t.Fatal(err)
	}
	ops, ds = CompileActions("literal-use", `invoke "literal" {};`, literal)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	got, err := lw.Execute(core.Request{ID: "literal", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
	if err != nil || got.Events[0].Resource.After.Current != 8 || got.Events[0].Kind != core.DamageApplied {
		t.Fatal(got, err)
	}
}

func TestM10DamageOperandFormsAndForwardParameters(t *testing.T) {
	for _, tc := range []struct{ target, amount, params, args string }{
		{`"c"`, `2`, ``, ``},
		{`"c"`, `param "amount"`, `parameter "amount" int;`, `"amount" = int 2;`},
		{`param "target"`, `2`, `parameter "target" character;`, `"target" = character "c";`},
		{`param "target"`, `param "amount"`, `parameter "target" character; parameter "amount" int;`, `"amount" = int 2; "target" = character "c";`},
	} {
		source := abilityCatalog + `ability "damage" named "é" { damage ` + tc.target + ` resource "hp" by ` + tc.amount + `; ` + tc.params + ` }`
		b, ds := CompileWorld("four.nql", source)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		ops, ds := CompileActions("four-use.nql", `invoke "damage" {`+tc.args+`};`, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		got, err := w.Execute(core.Request{ID: "forms", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
		if err != nil || len(got.Events) != 2 || got.Events[0].Kind != core.DamageApplied || got.Events[0].Resource.After.Current != 8 {
			t.Fatal(got, err)
		}
	}
}

func TestM10DamageNamespacesRemainDistinct(t *testing.T) {
	source := abilityCatalog + `condition type "c" { instances coexist; }
 ability "c" named "C" { parameter "c" character; damage param "c" resource "hp" by 1; }`
	b, ds := CompileWorld("namespaces.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	ops, ds := CompileActions("same-spelling.nql", `invoke "c" { "c" = character "c"; };`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.Execute(core.Request{ID: "c", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
	if err != nil || got.Events[0].Resource.After.Current != 9 {
		t.Fatal(got, err)
	}
}
