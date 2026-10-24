package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestItemCreationGrammarSequentialTypesAndDetachedCatalog(t *testing.T) {
	b, ds := CompileWorld("world.nql", `rules { transfer unequips; wear any; } location "hall" named "Hall"; character "a" named "A" at "hall" {} item "old" named "Old" { description "Original"; owner "a"; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	reserved := make([]core.Item, 1, 8)
	reserved[0] = b.Items[0]
	b.Items = reserved
	before := b.Items[:cap(b.Items)]
	copyBefore := append([]core.Item(nil), before...)
	source := `create item "new" named "Old" { description "New\nblue ribbon"; owner "a"; quantity 3; }; consume "new" from "a" by 1;`
	ops, ds := CompileActions("action.nql", source, b)
	if len(ds) != 0 || len(ops) != 2 {
		t.Fatal(ops, ds)
	}
	if !reflect.DeepEqual(before, copyBefore) {
		t.Fatal("mutated caller backing storage")
	}
	if ops[0].CreateItem.Description != "New\nblue ribbon" || *ops[0].CreateItem.Quantity != 3 {
		t.Fatal(ops)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	r, e := w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if e != nil || r.Events[0].Kind != core.ItemCreated {
		t.Fatal(r, e)
	}
	for _, tc := range []struct {
		source, code string
		line, column int
	}{
		{`create item "a" named "X" {};`, "E_DUPLICATE", 1, 13},
		{"create item \"new\" named \"X\" {\n owner \"hall\";\n};", "E_TYPE", 2, 8},
		{"create item \"new\" named \"X\" {\n quantity 0;\n};", "E_QUANTITY", 2, 11},
		{"create item \"new\" named \"X\" {\n wearer \"a\";\n};", "E_RULE", 2, 9},
		{`consume "later" from "a" by 1; create item "later" named "X" { owner "a"; };`, "E_REFERENCE", 1, 9},
	} {
		_, ds := CompileActions("bad.nql", tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != tc.line || ds[0].Column != tc.column || ds[0].Source != "bad.nql" {
			t.Fatal(tc, ds)
		}
	}
	for _, source := range []string{`create item "x" named "X" { description "a"; description "b"; };`, `create item "x" named "X" {} garbage`, `create item "x" named "X" { quantity 9223372036854775808; };`} {
		if _, ds := CompileActions("bad.nql", source, b); len(ds) == 0 {
			t.Fatal(source)
		}
	}
	if _, ds := CompileActions("corrected.nql", source, b); len(ds) != 0 {
		t.Fatal(ds)
	}
}

func FuzzItemCreation(f *testing.F) {
	for _, s := range []string{
		`create item "new" named "Ribbon" { description "A stitched bridge"; owner "a"; quantity 1; };`,
		`create item "new" named "" {}; consume "new" from "a" by 1;`,
		`create item "new" named "x" { quantity 9223372036854775808; };`,
		"create item \"new\" named \"\xff\" {};",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "hall"}}, Characters: []core.Character{{ID: "a", Location: "hall", Stats: map[core.StatID]int64{}, Resources: map[core.ResourceID]core.Resource{}}}}
		CompileActions("fuzz-item.nql", source, b)
	})
}
