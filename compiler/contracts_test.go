package compiler

import (
	"encoding/json"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const contractCatalog = `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" {
  stat "defense" = 10;
  resource "hp" = 4 min 0 max 5;
}
`

func TestContractWorldDiagnosticOccurrences(t *testing.T) {
	tests := []struct {
		name, src, code, message string
		line, column             int
	}{
		{"cross-kind later location", `rules { transfer unequips; wear any; }
character "x" named "X" at "l" {}
location "l" named "L";
location "x" named "Duplicate";`, "E_DUPLICATE", "identity", 4, 10},
		{"second of three kinds", `rules { transfer unequips; wear any; }
character "x" named "X" at "l" {}
item "x" named "Second" {}
location "x" named "Third";
location "l" named "L";`, "E_DUPLICATE", "identity", 3, 6},
		{"second effect not third", contractCatalog + `effect "e" from character "c" on character "c" stat "defense" add 1;
effect "e" from character "c" on character "c" stat "defense" add 2;
effect "e" from character "c" on character "c" stat "defense" add 3;`, "E_DUPLICATE", "effect", 8, 8},
		{"stat namespace not effect", `rules { transfer unequips; wear any; }
location "l" named "L";
character "same" named "S" at "l" {
  stat "" = 0;
  stat "defense" = 10;
}
effect "same" from character "same" on character "same" stat "defense" add 1;`, "E_ID", "stat", 4, 8},
		{"resource namespace not effect", `rules { transfer unequips; wear any; }
location "l" named "L";
character "same" named "S" at "l" {
  resource "" = 1 min 0 max 5;
  stat "defense" = 10;
}
effect "same" from character "same" on character "same" stat "defense" add 1;`, "E_ID", "resource", 4, 12},
		{"bounded resource declaration", `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" {
  resource "effect" = 6 min 0 max 5;
}`, "E_BOUNDS", "bounds", 4, 12},
		{"complete relation key", contractCatalog + `character "d" named "D" at "l" {}
character "e" named "E" at "l" {}
relation character "c" "trust" character "d" = 1;
relation character "c" "trust" character "e" = 1;
relation character "c" "trust" character "d" = 2;`, "E_DUPLICATE", "relationship", 11, 24},
		{"relation label", contractCatalog + `relation character "c" "" character "c" = 1;`, "E_ID", "relation", 7, 24},
		{"initial worn stat use", contractCatalog + `item "i" named "I" { owner "c"; wearer "c"; }
effect "w" from item "i" on worn "i" stat "missing" add 1;`, "E_STAT", "missing", 8, 43},
		{"numeric field is not namespace", `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" {
  stat "condition_instance" = 9223372036854775807;
}
effect "e" from character "c" on character "c" stat "condition_instance" add 1;
condition type "mark" { instances coexist; }
condition "c" of "mark" to "c";`, "E_OVERFLOW", "condition_instance", 4, 8},
		{"owner-only wearer occurrence", strings.Replace(contractCatalog, "wear any;", "wear owner_only;", 1) + `character "d" named "D" at "l" {}
item "i" named "I" {
  owner "c";
  wearer "d";
}`, "E_RULE", "wearer", 10, 10},
		{"missing policy enclosing rules", `// preface
rules {
 transfer unequips;
}
location "l" named "L";`, "E_RULE", "wear", 4, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := CompileWorld("contract-world.nql", tc.src)
			if len(ds) != 1 {
				t.Fatalf("wanted one diagnostic: %+v", ds)
			}
			d := ds[0]
			if d.Source != "contract-world.nql" || d.Code != tc.code || d.Line != tc.line || d.Column != tc.column || !strings.Contains(d.Message, tc.message) {
				t.Errorf("want %s at %d:%d about %q; got %+v", tc.code, tc.line, tc.column, tc.message, d)
			}
		})
	}
}
func TestContractConditionFieldNamesAreNotNamespaces(t *testing.T) {
	for _, label := range []string{"condition_type", "magnitude", "condition_instance", "effect", "instances"} {
		src := `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" {}
condition type "t" { instances coexist; modifier "m" stat "` + label + `" add magnitude; }
`
		b, ds := CompileWorld("catalog", src)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		for _, worldMode := range []bool{false, true} {
			var ds []Diagnostic
			line, column := 1, 31
			if worldMode {
				_, ds = CompileWorld("field.nql", src+`condition "a" of "t" to "c" magnitude 2;`)
				line, column = 5, 25
			} else {
				_, ds = CompileActions("field.nql", `apply condition "a" of "t" to "c" magnitude 2;`, b)
			}
			if len(ds) != 1 || ds[0].Code != "E_STAT" || ds[0].Source != "field.nql" || ds[0].Line != line || ds[0].Column != column || !strings.Contains(ds[0].Message, label) {
				t.Errorf("label %s world=%v want E_STAT at %d:%d; got %+v", label, worldMode, line, column, ds)
			}
		}
	}
}
func TestContractNamespaceAndEncodingCharacterization(t *testing.T) {
	src := `condition "same" of "same" to "same" magnitude 3;
condition type "same" { instances coexist; modifier "same" stat "same" add magnitude; }
effect "same" from character "same" on character "same" stat "same" add 2;
character "same" named "Same" at "l" { stat "same" = 10; resource "same" = 1 min 0 max 2; }
relation character "same" "same" character "same" = 1;
location "l" named "L";
rules { transfer unequips; wear any; }`
	b, ds := CompileWorld("namespaces.nql", src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	ex, e := w.Explain("same", "same")
	if e != nil || ex.Effective != 15 || len(ex.Contributions) != 2 {
		t.Fatal("valid namespaces/forward references", e, ex)
	}
	catalog, ds := CompileWorld("catalog", contractCatalog)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		src, code    string
		line, column int
	}{
		{"// é\r\n\tdamage \"c\" resource \"missing\" by 1;", "E_RESOURCE", 2, 22},
		{"// first\r\n\tapply effect \"é\" from character \"c\" on character \"c\" stat \"missing\" add 1;", "E_STAT", 2, 60},
		{`apply effect "\u00e9" from character "c" on character "c" stat "missing" add 1;`, "E_STAT", 1, 64},
		{"// first\r\n\t\xff", "E_UTF8", 2, 2},
	} {
		_, ds := CompileActions("encoding.nql", tc.src, catalog)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "encoding.nql" || ds[0].Line != tc.line || ds[0].Column != tc.column {
			t.Errorf("want %s %d:%d got %+v", tc.code, tc.line, tc.column, ds)
		}
	}
}
func TestContractSourceIdentityAndHonestTypedFaults(t *testing.T) {
	b, ds := CompileWorld("catalog", conditionCatalog)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	sources := []string{`apply condition "a" of "bless" to "c" from location "l" magnitude 2; damage "c" resource "hp" by 1;`, "// same intent\r\n\tapply condition \"a\" of \"bless\" to \"\\u0063\" magnitude +02 from location \"l\";\r\ndamage \"c\" resource \"hp\" by 01;"}
	// Add the same resource to the compiler catalog and world genesis.
	b.Characters[0].Resources = map[core.ResourceID]core.Resource{"hp": {Current: 4, Minimum: 0, Maximum: 5}}
	var ops [][]core.Operation
	for i, s := range sources {
		o, ds := CompileActions([]string{"first.nql", "second.nql"}[i], s, b)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		ops = append(ops, o)
	}
	if !reflect.DeepEqual(ops[0], ops[1]) {
		t.Fatal("source location/layout entered typed identity")
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	r := core.Request{ID: "same-intent", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops[0]}
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	_, e = w.Execute(core.Request{ID: "later", ExpectedRevision: 1, Actor: r.Actor, Operations: []core.Operation{{Heal: &core.ResourceOp{Character: "c", Resource: "hp", Amount: 1}}}})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(w.Snapshot())
	history, _ := json.Marshal(w.History())
	r.Operations = ops[1]
	retry, e := w.Execute(r)
	if e != nil || !reflect.DeepEqual(retry, receipt) {
		t.Fatal("equivalent source failed late retry", e)
	}
	after, _ := json.Marshal(w.Snapshot())
	afterHistory, _ := json.Marshal(w.History())
	if string(before) != string(after) || string(history) != string(afterHistory) {
		t.Fatal("retry mutated truth")
	}
	rebuilt, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	retry, e = rebuilt.Execute(r)
	if e != nil || !reflect.DeepEqual(retry, receipt) || rebuilt.Revision() != 2 {
		t.Fatal("replay receipt/source identity", e)
	}
	bad := core.Operation{Damage: &core.ResourceOp{Character: "c", Resource: "missing", Amount: 1}}
	err := b.CheckOperationStatic(bad)
	var fault *core.Fault
	if !errors.As(err, &fault) || fault.Code != "E_RESOURCE" || fault.Subject != "c" || fault.Field != "missing" {
		t.Fatal("typed fault", err)
	}
	encoded, _ := json.Marshal(fault)
	for _, field := range []string{`"source"`, `"line"`, `"column"`} {
		if strings.Contains(string(encoded), field) {
			t.Fatal("typed validation invented a source location")
		}
	}
	for _, s := range []string{`damage "c" resource "missing" by 1;`, `apply condition "bad" of "marked" to ;`} {
		if o, ds := CompileActions("bad.nql", s, w.Snapshot().Blueprint); o != nil || len(ds) != 1 {
			t.Fatal("invalid source accepted")
		}
	}
	_, e = w.Execute(core.Request{ID: "continuation", ExpectedRevision: 2, Actor: r.Actor, Operations: []core.Operation{{Heal: &core.ResourceOp{Character: "c", Resource: "hp", Amount: 1}}}})
	if e != nil || w.Revision() != 3 {
		t.Fatal("valid continuation", e)
	}
}

func TestContractCompilerCatalogAndOutputOwnership(t *testing.T) {
	b, ds := CompileWorld("catalog", conditionCatalog)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	before, _ := json.Marshal(b)
	source := `apply condition "a" of "bless" to "c" from location "l" magnitude 2;`
	first, ds := CompileActions("one", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	second, ds := CompileActions("two", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	secondBytes, _ := json.Marshal(second)
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("compiler mutated borrowed catalog")
	}
	if _, ds = CompileActions("bad", `apply condition "bad" of "unknown" to "c";`, b); len(ds) != 1 {
		t.Fatal("invalid action accepted")
	}
	after, _ = json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("failed compilation mutated catalog")
	}
	*first[0].ApplyCondition.Magnitude = 99
	first[0].ApplyCondition.Source.ID = "tampered"
	b.Characters[0].Stats["defense"] = 99
	b.ConditionDefinitions[0].Modifiers[0].Stat = "tampered"
	after, _ = json.Marshal(second)
	if string(secondBytes) != string(after) {
		t.Fatal("independent IR aliases another result or catalog")
	}
	fresh, ds := CompileWorld("catalog-again", conditionCatalog)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	after, _ = json.Marshal(fresh)
	if string(before) != string(after) {
		t.Fatal("CompileWorld result retained mutable authority")
	}
}
