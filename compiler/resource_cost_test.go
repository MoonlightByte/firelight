package compiler

import (
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const spendDefinitionSource = `ability "pay" named "Pay" {
 parameter "payer" character;
 parameter "amount" int;
 spend param "payer" resource "hp" by param "amount";
}
`
const spendInvocationSource = `invoke "pay" {
 "payer" = character "c";
 "amount" = int 2;
};`

// The marker fixes the intended occurrence in each input independently of the
// compiler's own position selection; it is removed before compilation.
func spendMarkedPosition(t *testing.T, marked string) (string, int, int) {
	t.Helper()
	at := strings.Index(marked, "«")
	if at < 0 || strings.Count(marked, "«") != 1 {
		t.Fatal("one oracle marker required")
	}
	prefix := marked[:at]
	line := strings.Count(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
	return strings.Replace(marked, "«", "", 1), line, column
}
func TestSpendCompilerOccurrencesAndHonestContext(t *testing.T) {
	b, ds := CompileWorld("catalog", abilityCatalog+spendDefinitionSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		name, source, code, context string
		world                       bool
	}{
		{"direct absent payer", `spend «"absent" resource "hp" by 1;`, "E_REFERENCE", "", false},
		{"direct wrong payer kind", `spend «"l" resource "hp" by 1;`, "E_TYPE", "", false},
		{"direct missing resource", `spend "c" resource «"missing" by 1;`, "E_RESOURCE", "", false},
		{"direct zero", `spend "c" resource "hp" by «0;`, "E_AMOUNT", "", false},
		{"direct negative", `spend "c" resource "hp" by «-1;`, "E_AMOUNT", "", false},
		{"direct overflow", `spend "c" resource "hp" by «9223372036854775808;`, "E_INTEGER", "", false},
		{"direct missing amount", `spend "c" resource "hp" by «;`, "E_PARSE", "", false},
		{"literal payer", abilityCatalog + `ability "a" named "A" { spend «"i" resource "hp" by 1; }`, "E_TYPE", `step 0 operand "payer"`, true},
		{"literal resource obligation", abilityCatalog + `ability "a" named "A" { spend «"n" resource "hp" by 1; }`, "E_RESOURCE", `step 0 operand "payer"`, true},
		{"selector identity", abilityCatalog + `ability "a" named "A" { spend "c" resource «"" by 1; }`, "E_ID", `step 0 operand "resource"`, true},
		{"literal amount", abilityCatalog + `ability "a" named "A" { spend "c" resource "hp" by «-1; }`, "E_AMOUNT", `step 0 operand "amount"`, true},
		{"body integer overflow", abilityCatalog + `ability "a" named "A" { spend "c" resource "hp" by «9223372036854775808; }`, "E_INTEGER", "", true},
		{"body unknown parameter", abilityCatalog + `ability "a" named "A" { spend param «"payer" resource "hp" by 1; }`, "E_PARAMETER", `step 0 operand "payer"`, true},
		{"body wrong parameter kind", abilityCatalog + `ability "a" named "A" { parameter "payer" int; spend param «"payer" resource "hp" by 1; }`, "E_TYPE", `step 0 operand "payer"`, true},
		{"missing argument", "invoke \"pay\" { \"amount\" = int 2;\n«};", "E_ARGUMENT", `step -1 operand "argument"`, false},
		{"duplicate argument", `invoke "pay" { "payer" = character "c"; «"payer" = character "c"; "amount" = int 2; };`, "E_DUPLICATE", "", false},
		{"extra argument", `invoke "pay" { «"extra" = int 2; };`, "E_ARGUMENT", `step -1 operand "argument"`, false},
		{"wrong argument kind", `invoke "pay" { "payer" = item «"i"; "amount" = int 2; };`, "E_TYPE", `step -1 operand "argument"`, false},
		{"wrong argument reference", `invoke "pay" { "payer" = character «"l"; "amount" = int 2; };`, "E_TYPE", `step -1 operand "argument"`, false},
		{"bound missing resource", `invoke "pay" { "payer" = character «"n"; "amount" = int 2; };`, "E_RESOURCE", `step 0 operand "payer"`, false},
		{"bound zero", `invoke "pay" { "payer" = character "c"; "amount" = int «0; };`, "E_AMOUNT", `step 0 operand "amount"`, false},
		{"bound negative", `invoke "pay" { "payer" = character "c"; "amount" = int «-2; };`, "E_AMOUNT", `step 0 operand "amount"`, false},
		{"bound overflow", `invoke "pay" { "payer" = character "c"; "amount" = int «9223372036854775808; };`, "E_INTEGER", "", false},
		{"UTF8 CRLF", "// é\r\n\tinvoke \"pay\" { \"payer\" = character «\"é\"; \"amount\" = int 2; };", "E_REFERENCE", `step -1 operand "argument"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, line, column := spendMarkedPosition(t, tc.source)
			var ds []Diagnostic
			if tc.world {
				_, ds = CompileWorld("spend-world.nql", s)
			} else {
				ops, d := CompileActions("spend-use.nql", s, b)
				ds = d
				if ops != nil {
					t.Fatal("invalid source produced operations")
				}
			}
			file := "spend-use.nql"
			if tc.world {
				file = "spend-world.nql"
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
	b.AbilityDefinitions[0].Steps[0].Spend.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptrCompiler(int64(0))}}
	_, ds = CompileActions("other-file.nql", spendInvocationSource, b)
	if len(ds) != 1 || ds[0].Line != 1 || ds[0].Column != 8 || ds[0].Source != "other-file.nql" || !strings.Contains(ds[0].Message, `step 0 operand "amount"`) {
		t.Fatal(ds)
	}
}

func TestSpendCompilerSourceFreeIdentityAndSignedInt(t *testing.T) {
	source := abilityCatalog + `ability "spend" named "é" {
 parameter "}" int;
 parameter "c" character;
 spend param "c" resource "hp" by param "}";
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
	input := []string{`invoke "spend" { "}" = int 2; "c" = character "c"; };`, "// same typed intent\r\n invoke \"spend\" { \"c\" = character \"\\u0063\"; \"\\u007d\" = int +02; };"}
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
	if ops, ds := CompileActions("bad", `spend "c" resource "hp" by ;`, b); ops != nil || len(ds) != 1 {
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
	// Literal payer/amount body and direct syntax both lower to the same mechanic.
	literal, ds := CompileWorld("literal", abilityCatalog+`ability "literal" named "Literal" { spend "c" resource "hp" by 2; }`)
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
	if err != nil || got.Events[0].Resource.After.Current != 8 || got.Events[0].Kind != core.ResourceSpent {
		t.Fatal(got, err)
	}
}

func FuzzSpendCompiler(f *testing.F) {
	for _, s := range []string{spendInvocationSource, `spend "c" resource "hp" by 2;`, abilityCatalog + spendDefinitionSource, `spend "c" resource "hp" by -9223372036854775808;`, "spend \"\xff\""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		b, ds := CompileWorld("fixture", abilityCatalog+spendDefinitionSource)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		a, da := CompileActions("fuzz", s, b)
		z, dz := CompileActions("fuzz", s, b)
		if !reflect.DeepEqual(a, z) || !reflect.DeepEqual(da, dz) {
			t.Fatal("nondeterministic compiler")
		}
		_, _ = CompileWorld("fuzz-world", s)
	})
}
