package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const inventorySource = `rules { transfer unequips; wear any; }
location "hall" named "Hall";
character "a" named "A" at "hall" {}
item "stock" named "Rations" {
  quantity 3;
  owner "a";
}`

func TestInventorySourceAndTypedIdentity(t *testing.T) {
	b, ds := CompileWorld("stock.nql", inventorySource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.Items[0].Quantity == nil || *b.Items[0].Quantity != 3 {
		t.Fatal(b.Items)
	}
	text := `consume "stock" from "a" by 1;`
	ops, ds := CompileActions("one.nql", text, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := CompileActions("elsewhere.nql", "\n // different source\n"+text, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) || ops[0].Consume.Amount != 1 {
		t.Fatal(ops, other, ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); e != nil {
		t.Fatal(e)
	}
	if *w.Snapshot().Items[0].Quantity != 2 || *b.Items[0].Quantity != 3 {
		t.Fatal("compiler/input mutation")
	}
	for _, tc := range []struct {
		text, code string
		column     int
	}{
		{`consume "a" from "a" by 1;`, "E_TYPE", 9},
		{`consume "stock" from "stock" by 1;`, "E_TYPE", 22},
		{`consume "stock" from "a" by 0;`, "E_AMOUNT", 29},
		{`consume "stock" from "a" by 9223372036854775808;`, "E_INTEGER", 29},
	} {
		_, ds = CompileActions("invalid.nql", tc.text, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != 1 || ds[0].Column != tc.column {
			t.Fatalf("%s: %+v", tc.text, ds)
		}
	}
}
func TestInventoryDeclarationDiagnosticsAndAbilityBoundary(t *testing.T) {
	for _, tc := range []struct {
		text, code   string
		line, column int
	}{
		{strings.Replace(inventorySource, "quantity 3", "quantity -1", 1), "E_QUANTITY", 5, 12},
		{strings.Replace(inventorySource, "quantity 3;", "quantity 3;\n  quantity 1;", 1), "E_DUPLICATE", 6, 3},
		{strings.Replace(inventorySource, `owner "a";`, `wearer "a";`, 1), "E_QUANTITY", 5, 12},
	} {
		_, ds := CompileWorld("bad.nql", tc.text)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != tc.line || ds[0].Column != tc.column {
			t.Fatalf("want %s %d:%d: %+v", tc.code, tc.line, tc.column, ds)
		}
	}
	_, ds := CompileWorld("unsupported.nql", inventorySource+` ability "divide" named "Divide" { split "stock" as "part" by 1; }`)
	if len(ds) == 0 {
		t.Fatal("unreviewed Ability expansion")
	}
}
func FuzzInventorySource(f *testing.F) {
	for _, s := range []string{`consume "stock" from "a" by 1;`, `consume "stock" from "a" by -9223372036854775808;`, "\xff", `consume "\\\"" from "a" by 1;`} {
		f.Add(s)
	}
	b, ds := CompileWorld("fixture.nql", inventorySource)
	if len(ds) != 0 {
		f.Fatal(ds)
	}
	f.Fuzz(func(t *testing.T, s string) { CompileActions("fuzz.nql", s, b) })
}
