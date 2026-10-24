package compiler

import (
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"testing"
)

func TestAlphaEquipBodyOperandForms(t *testing.T) {
	for _, item := range []string{`"i"`, `param "it"`} {
		for _, wearer := range []string{`"c"`, `param "wear"`} {
			t.Run(item+"/"+wearer, func(t *testing.T) {
				src := abilityCatalog + fmt.Sprintf(`ability "load" named "Load" {
 parameter "it" item;
 parameter "wear" character;
 equip %s on %s;
}
ability "off" named "Off" { unequip "i"; }
`, item, wearer)
				b, ds := CompileWorld("loadout.nql", src)
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				w, err := core.NewWorld(b)
				if err != nil {
					t.Fatal(err)
				}
				ops, ds := CompileActions("load.nql", `invoke "load" { "it" = item "i"; "wear" = character "c"; };`, b)
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				first, err := w.Execute(core.Request{ID: "load", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
				if err != nil || len(first.Events) != 2 || first.Events[0].Kind != core.ItemEquipped || first.Events[0].AbilityOrigin.Step != 0 {
					t.Fatal(first, err)
				}
				if w.Snapshot().Items[0].Wearer != "c" {
					t.Fatal("effect-free item lost wearing fact")
				}
				ops, ds = CompileActions("off.nql", `invoke "off" {};`, b)
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				last, err := w.Execute(core.Request{ID: "off", ExpectedRevision: 1, Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops})
				if err != nil || last.Events[0].Kind != core.ItemUnequipped || w.Snapshot().Items[0].Wearer != "" {
					t.Fatal(last, err)
				}
			})
		}
	}
}

func TestAlphaLoadoutDefinitionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		source, code string
		line, column int
	}{
		{`ability "load" named "Load" {
 equip "l" on "c";
}`, "E_TYPE", 8, 8},
		{`ability "load" named "Load" {
 equip "i" on "l";
}`, "E_TYPE", 8, 15},
		{`ability "load" named "Load" {
 parameter "who" character;
 unequip param "who";
}`, "E_TYPE", 9, 16},
	} {
		_, ds := CompileWorld("bad-loadout.nql", abilityCatalog+tc.source)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "bad-loadout.nql" || ds[0].Line != tc.line || ds[0].Column != tc.column {
			t.Fatalf("want %s %d:%d, got %v", tc.code, tc.line, tc.column, ds)
		}
	}
}
