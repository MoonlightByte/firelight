package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const catalogSource = `rules { transfer unequips; wear any; }
location "hall" named "Hall";
character "a" named "A" at "hall" {}
item type "torch" named "Torch" {
 description "A reed bundle."; kind "gear"; subtype "light";
 magical false; consumable true; price "copper" 1 per 1;
}
`

func TestCatalogSourceAndUnexpandedActions(t *testing.T) {
	b, ds := CompileWorld("world", catalogSource+`item "torch" from "torch" { owner "a"; quantity 0; }`)
	if len(ds) != 0 || len(b.ItemDefinitions) != 1 || b.ItemDefinitions[0].Price.Amount != 1 || b.Items[0].Name != "" || b.Items[0].From != "torch" {
		t.Fatal(b, ds)
	}
	source := `create item "new" from "torch" { owner "a"; quantity 3; custody character "a"; }; split "new" quantity 1 into "part" by "a"; consume "part" from "a" by 1;`
	ops, ds := CompileActions("actions", source, b)
	if len(ds) != 0 || len(ops) != 3 {
		t.Fatal(ops, ds)
	}
	if i := ops[0].CreateItem; i.From != "torch" || i.Name != "" || i.Description != "" || i.Equipment != nil {
		t.Fatal("expanded intent", i)
	}
	b.ItemDefinitions[0].Name = "Reed torch"
	b.ItemDefinitions[0].Price.Amount = 2
	again, ds := CompileActions("actions", source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, again) {
		t.Fatal("definition edit changed action identity", again, ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	again, ds = CompileRetry("retry", source, w.Snapshot().Blueprint)
	if len(ds) != 0 || !reflect.DeepEqual(ops, again) {
		t.Fatal("post-state retry", ds)
	}
}

func TestCatalogDiagnostics(t *testing.T) {
	_, legacy := CompileWorld("legacy", `item "i" unknown "Name" {}`)
	if len(legacy) != 1 || legacy[0].Code != "E_PARSE" || legacy[0].Expected != "named" || legacy[0].Actual != "unknown" || legacy[0].Column != 10 {
		t.Fatal("catalog changed old malformed-item diagnostic", legacy)
	}
	b, ds := CompileWorld("good", catalogSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, code, at string }{
		{`create item "new" from "missing" {};`, "E_CATALOG", `"missing"`},
		{`create item "new" from "" {};`, "E_CATALOG", `""`},
		{`create item "new" from "torch" { description "override"; };`, "E_PARSE", "description"},
		{`create item "new" from "torch" { definition "override"; };`, "E_PARSE", "definition"},
		{`create item "new" from "torch" named "override" {};`, "E_PARSE", "named"},
		{`create item "new" from "torch" { quantity 0; };`, "E_QUANTITY", "0"},
		{`create item "new" from "torch" { mode "held"; };`, "E_CATALOG", `"held"`},
		{`create item "new" from "torch" { quantity 9223372036854775808; };`, "E_INTEGER", "9223372036854775808"},
	} {
		_, ds := CompileActions("bad", tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatal(tc, ds)
		}
	}
	for _, tc := range []struct{ source, code, at string }{
		{`item type "d" named "D" { kind "gear"; price "cp" -1 per 1; }`, "E_CATALOG", "-1"},
		{`item type "d" named "D" { kind "gear"; price "cp" 1 per 0; }`, "E_CATALOG", "0"},
		{`item type "d" named "D" { kind "gear"; equipment "absent"; }`, "E_CATALOG", `"absent"`},
		{`item type "d" named "D" { kind "gear"; equipment ""; }`, "E_CATALOG", `""`},
		{`item type "d" named "D" { kind "gear"; kind "other"; }`, "E_DUPLICATE", `kind "other"`},
		{`item type "d" named "D" { kind "gear"; price "cp" 1 per 1; price "cp" 2 per 1; }`, "E_DUPLICATE", `price "cp" 2`},
		{`item "i" from "missing" {}`, "E_CATALOG", `"missing"`},
		{`item "i" from "torch" {};`, "E_PARSE", ";"},
	} {
		_, ds := CompileWorld("bad", catalogSource+tc.source)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatal(tc, ds)
		}
	}
	duplicate := `item type "torch" named "Again" { kind "gear"; }`
	_, ds = CompileWorld("duplicate", catalogSource+duplicate)
	if len(ds) != 1 || ds[0].Code != "E_CATALOG" || ds[0].Column != 11 {
		t.Fatal(ds)
	}
	collision := `item type "same" named "" { kind "gear"; } item "same" from "same" {}`
	_, ds = CompileWorld("namespace", catalogSource+collision)
	if len(ds) != 1 || ds[0].Code != "E_CATALOG" || ds[0].Column != strings.Index(collision, `""`)+1 {
		t.Fatal("definition fault mapped to an item operand", ds)
	}
}

func TestCatalogEquipmentSourceAndSequentialCreation(t *testing.T) {
	source := equipmentSource(t) + `item type "catalog-sword" named "Sword" { kind "weapon"; equipment "gear:longsword"; }
`
	b, ds := CompileWorld("equipment", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	ops, ds := CompileActions("equip", `create item "new-sword" from "catalog-sword" { owner "char:b"; }; equip "new-sword" on "char:b" mode "two";`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if ops[0].CreateItem.Equipment != nil {
		t.Fatal("expanded equipment")
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Execute(core.Request{ID: "equip", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:b"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	_, ds = CompileWorld("worn", source+`item "new-sword" from "catalog-sword" { owner "char:b"; wearer "char:b"; mode "two"; }`)
	if len(ds) != 0 {
		t.Fatal("mode on source from declaration", ds)
	}
}

func FuzzItemCatalog(f *testing.F) {
	for _, source := range []string{catalogSource, `item type "x" named "X" { price "cp" 1 per 1; }`, `item "x" from "" {}`, `item type "x" named "X" { magical false; magical true; }`} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) { CompileWorld("catalog-fuzz", source) })
}
