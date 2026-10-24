package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestAddStockSourceAndTypedIdentity(t *testing.T) {
	b, ds := CompileWorld("stock.nql", inventorySource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.Items[0].Quantity == nil || *b.Items[0].Quantity != 3 {
		t.Fatal(b.Items)
	}
	text := `add "stock" to "a" by 1;`
	ops, ds := CompileActions("one.nql", text, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := CompileActions("elsewhere.nql", "\n // different source\n"+text, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) || ops[0].Add.Amount != 1 {
		t.Fatal(ops, other, ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); e != nil {
		t.Fatal(e)
	}
	if *w.Snapshot().Items[0].Quantity != 4 || *b.Items[0].Quantity != 3 {
		t.Fatal("compiler/input mutation")
	}
	for _, tc := range []struct {
		text, code string
		column     int
	}{
		{`add "a" to "a" by 1;`, "E_TYPE", 5},
		{`add "stock" to "stock" by 1;`, "E_TYPE", 16},
		{`add "stock" to "a" by 0;`, "E_AMOUNT", 23},
		{`add "stock" to "a" by 9223372036854775808;`, "E_INTEGER", 23},
	} {
		_, ds = CompileActions("invalid.nql", tc.text, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != 1 || ds[0].Column != tc.column {
			t.Fatalf("%s: %+v", tc.text, ds)
		}
	}
}
