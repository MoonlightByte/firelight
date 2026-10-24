package compiler

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func equipmentSource(t *testing.T) string {
	t.Helper()
	v, e := os.ReadFile("testdata/equipment.nql")
	if e != nil {
		t.Fatal(e)
	}
	return string(v)
}
func equipmentCompiled(t *testing.T) (core.Blueprint, *core.World) {
	t.Helper()
	b, ds := CompileWorld("equipment.nql", equipmentSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return b, w
}
func TestEquipmentAuthoredCatalogAndAbilityReplay(t *testing.T) {
	b, w := equipmentCompiled(t)
	for _, c := range []struct {
		id  core.CharacterID
		def int64
	}{{"char:a", 15}, {"char:b", 13}} {
		x, e := w.Explain(c.id, "defense")
		if e != nil || x.Effective != c.def {
			t.Fatalf("%+v %v", x, e)
		}
	}
	source := `invoke "handoff-ready" { "item" = item "item:sword:1"; "giver" = character "char:a"; "recipient" = character "char:b"; };`
	ops, ds := CompileActions("handoff.nql", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	r := core.Request{ID: "handoff", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:a"}, Operations: ops}
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	if len(receipt.Events) != 4 || receipt.Events[3].Kind != core.AbilityInvoked || receipt.Events[2].Wearing.ModeAfter != "one" {
		t.Fatalf("bad ordered receipt %+v", receipt)
	}
	v, e := w.EquipmentView("char:b")
	if e != nil || len(v.Attacks) != 1 || v.Attacks[0].ToHit != 3 || v.Attacks[0].DamageBonus != 1 {
		t.Fatalf("bad recipient grant %+v %v", v, e)
	}
	ops, ds = CompileActions("mode.nql", `unequip "item:sword:1"; invoke "ready-two" { "item" = item "item:sword:1"; "wearer" = character "char:b"; };`, w.Snapshot().Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	_, e = w.Execute(core.Request{ID: "two", ExpectedRevision: 1, Actor: r.Actor, Operations: ops})
	if e != nil {
		t.Fatal(e)
	}
	v, _ = w.EquipmentView("char:b")
	if v.Attacks[0].DamageSides != 10 {
		t.Fatal("lost authored mode")
	}
	var buffer bytes.Buffer
	_, e = nativesave.Write(&buffer, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "compiled", Next: 3})
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := nativesave.Read(&buffer)
	if e != nil {
		t.Fatal(e)
	}
	again, e := loaded.World.Execute(r)
	if e != nil || !reflect.DeepEqual(receipt, again) {
		t.Fatal("archive lost original invocation")
	}
}
func TestEquipmentPositionedDiagnostics(t *testing.T) {
	source := equipmentSource(t)
	for _, tc := range []struct{ name, old, replacement, marker, code string }{
		{"division", `divide 2;`, `divide 0;`, `0;`, "E_EQUIPMENT"},
		{"condition", `requires "training:martial"`, `requires "missing"`, `"missing"`, "E_EQUIPMENT"},
		{"unless", `requires "training:martial"`, `requires "training:martial" unless "missing"`, `"missing"`, "E_EQUIPMENT"},
		{"derived cycle", `term stat "dexterity" offset -10 divide 2;`, `term stat "defense" offset -10 divide 2;`, `"defense" offset`, "E_EQUIPMENT"},
		{"slot", `occupy "hand" by 1;`, `occupy "unknown" by 1;`, `"unknown"`, "E_EQUIPMENT"},
		{"default", `default "one";`, `default "missing";`, `"missing"`, "E_EQUIPMENT"},
		{"empty mode", `mode "one";`, `mode "";`, `"";`, "E_EQUIPMENT"},
		{"integer", `capacity 2;`, `capacity 9223372036854775808;`, `9223372036854775808`, "E_INTEGER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(source, tc.old, tc.replacement, 1)
			_, ds := CompileWorld("bad.nql", changed)
			if len(ds) != 1 || ds[0].Code != tc.code {
				t.Fatalf("diagnostics %+v", ds)
			}
			start := strings.Index(changed, tc.replacement)
			offset := start + strings.Index(tc.replacement, tc.marker)
			prefix := changed[:offset]
			line := strings.Count(prefix, "\n") + 1
			col := len(prefix) - strings.LastIndex(prefix, "\n")
			if ds[0].Position.Source != "bad.nql" || ds[0].Position.Line != line || ds[0].Position.Column != col {
				t.Fatalf("want%d:%d got%+v", line, col, ds[0])
			}
		})
	}
	b, w := equipmentCompiled(t)
	before := w.ExportCheckpoint()
	_, ds := CompileActions("mode.nql", `equip "item:sword:2" on "char:b" mode "bad";`, b)
	if len(ds) != 1 || ds[0].Position.Column != 39 {
		t.Fatalf("mode use site %+v", ds)
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("compiler mutated state")
	}
	ops, ds := CompileActions("good.nql", `equip "item:sword:2" on "char:b" mode "two";`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, e := w.Execute(core.Request{ID: "valid", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:b"}, Operations: ops}); e != nil {
		t.Fatal(e)
	}
}
func FuzzEquipmentWorld(f *testing.F) {
	data, e := os.ReadFile("testdata/equipment.nql")
	if e != nil {
		f.Fatal(e)
	}
	f.Add(string(data))
	f.Add(`equipment "nql-equipment-v1" {`)
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 32000 {
			return
		}
		b, ds := CompileWorld("fuzz.nql", text)
		if len(ds) == 0 {
			if _, e := core.NewWorld(b); e != nil {
				t.Fatalf("compiler accepted invalid world: %v", e)
			}
		}
	})
}

func TestEquipmentMissingInputUsesRelevantCharacterRecipe(t *testing.T) {
	source := `rules { transfer unequips; wear any; }
location "l" named "L";
character "a" named "A" at "l" { stat "defense" = 10; stat "strength" = 16; }
character "b" named "B" at "l" { stat "unused" = 0; }
equipment "nql-equipment-v1" {
 derive stat "defense" {
  term stat "strength" offset -10 divide 2;
 }
 definition "d" named "D" {
  description "test";
  default "m";
  mode "m" {
   attack "a" named "A" {
    hit {
     constant 0;
     term stat "strength" offset -10 divide 2;
    }
    damage "force" dice 1 sides 2 { constant 0; }
   }
  }
 }
}
item "i" named "I" { owner "b"; wearer "b"; definition "d"; mode "m"; }
`
	b, ds := CompileWorld("relevant.nql", source)
	if len(ds) != 1 || ds[0].Code != "E_STAT" || ds[0].Position.Line != 16 || ds[0].Position.Column != 16 {
		t.Fatalf("expected attack input at16:16, got%+v", ds)
	}
	if !reflect.DeepEqual(b, core.Blueprint{}) {
		t.Fatal("partial IR escaped")
	}
}
