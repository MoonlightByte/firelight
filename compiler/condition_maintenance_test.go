package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const maintenanceSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "defense" = 10; resource "hp" = 4 min 0 max 9; }
condition type "root" { instances replace; ends_at_minimum "hp"; }
condition type "boon" { instances coexist; modifier "ward" stat "defense" add 2; }
ability "ward" named "Ward" {
 parameter "actor" character;
 parameter "root" condition_id;
 parameter "child" condition_id;
 spend param "actor" resource "hp" by 1;
 apply condition param "root" of "root" to param "actor";
 apply condition param "child" of "boon" to param "actor" anchor param "root";
}`

func TestConditionMaintenanceSourceInvocationAndLocations(t *testing.T) {
	b, ds := compiler.CompileWorld("maintenance", maintenanceSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "ward" { "actor" = character "a"; "root" = condition_id "r"; "child" = condition_id "c"; };`
	ops, ds := compiler.CompileActions("one", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileActions("two", "// other source\n"+source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("source changed command identity", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 4 || r.Events[2].Condition.After.Anchor == nil || *r.Events[2].Condition.After.Anchor != "r" || r.Events[2].Condition.After.Cause.AbilityOrigin.Step != 2 {
		t.Fatal("authored anchor provenance", r)
	}
	stat, _ := w.Explain("a", "defense")
	if stat.Effective != 12 {
		t.Fatal(stat)
	}
	remove, ds := compiler.CompileActions("remove", `remove condition "r" from "a";`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, err = w.Execute(core.Request{ID: "remove", ExpectedRevision: 1, Actor: r.Request.Actor, Operations: remove}); err != nil {
		t.Fatal(err)
	}
	before := w.ExportCheckpoint()
	old, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(old, r) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("historical anchor retry", err)
	}
	b.AbilityDefinitions[0].Steps[2].ApplyCondition.Anchor.Parameter = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested anchor operand alias")
	}
}

func TestConditionMaintenanceDiagnosticSites(t *testing.T) {
	for _, tc := range []struct{ source, needle string }{
		{strings.Replace(maintenanceSource, `ends_at_minimum "hp"`, `ends_at_minimum ""`, 1), `""`},
		{strings.Replace(maintenanceSource, `anchor param "root"`, `anchor param "actor"`, 1), `anchor param "actor"`},
		{maintenanceSource + "\n" + `condition "child" of "boon" to "a" anchor "absent";`, `"absent"`},
	} {
		_, ds := compiler.CompileWorld("bad-anchor", tc.source)
		index := strings.LastIndex(tc.source, tc.needle)
		if strings.HasPrefix(tc.needle, "anchor param ") {
			index += len("anchor param ")
		}
		line := strings.Count(tc.source[:index], "\n") + 1
		col := index - strings.LastIndex(tc.source[:index], "\n")
		if len(ds) != 1 || ds[0].Position.Source != "bad-anchor" || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Fatal("wrong declaration/use site", line, col, ds)
		}
	}
	// Forward references at genesis resolve against the complete declaration set.
	_, ds := compiler.CompileWorld("forward", maintenanceSource+"\n"+`condition "child" of "boon" to "a" anchor "parent"; condition "parent" of "root" to "a";`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	b, ds := compiler.CompileWorld("ok", maintenanceSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	bad := `apply condition "c" of "boon" to "a" anchor "";`
	_, ds = compiler.CompileActions("bad-use", bad, b)
	if len(ds) != 1 || ds[0].Position.Column != strings.Index(bad, `""`)+1 {
		t.Fatal(ds)
	}
}

func TestConditionMaintenanceMissingFloorResourcePosition(t *testing.T) {
	// Resource names can equal structural field names. Report the target that
	// lacks the immutable Resource, never the instance or magnitude argument.
	for _, resource := range []string{"missing", "magnitude"} {
		world := maintenanceSource + "\ncondition type \"floor\" { instances unique; ends_at_minimum \"" + resource + "\"; }"
		b, ds := compiler.CompileWorld("floor-world", world)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		action := `apply condition "new" of "floor" to "a";`
		_, ds = compiler.CompileActions("floor-action", action, b)
		if len(ds) != 1 || ds[0].Code != "E_RESOURCE" || ds[0].Position.Column != strings.Index(action, `"a"`)+1 {
			t.Fatal("missing floor Resource must identify target", ds)
		}
		declaration := `condition "new" of "floor" to "a";`
		_, ds = compiler.CompileWorld("floor-genesis", world+"\n"+declaration)
		if len(ds) != 1 || ds[0].Code != "E_RESOURCE" || ds[0].Position.Line != 15 || ds[0].Position.Column != strings.Index(declaration, `"a"`)+1 {
			t.Fatal("genesis missing floor Resource must identify target", ds)
		}
	}
}
