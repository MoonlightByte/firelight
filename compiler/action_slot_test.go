package compiler_test

import (
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func TestActionFreeSlotCompiler(t *testing.T) {
	clause := `require free_slot param "actor" slot "hand" count 1;`
	source := strings.Replace(authoredRollSource(t), "phase either;", "phase either;\n "+clause, 1)
	b, ds := compiler.CompileWorld("free-slot", source)
	if len(ds) != 0 || b.AbilityDefinitions[0].Action.Requirements[0].Slot.Amount != 1 {
		t.Fatal(ds)
	}
	for _, change := range []struct{ from, to string }{{"count 1", "count 0"}, {`slot "hand"`, `slot "missing"`}} {
		bad := strings.Replace(source, clause, strings.Replace(clause, change.from, change.to, 1), 1)
		_, ds := compiler.CompileWorld("bad-slot", bad)
		at := strings.Index(bad, change.to)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 {
			t.Fatal("slot declaration position", ds)
		}
	}
}

func TestActionWornTrainingCompiler(t *testing.T) {
	clause := `require worn_training param "actor" definition "longsword" mode "one" condition "advantage";`
	source := strings.Replace(authoredRollSource(t), "phase either;", "phase either;\n "+clause, 1)
	b, ds := compiler.CompileWorld("worn-training", source)
	if len(ds) != 0 || b.AbilityDefinitions[0].Action.Requirements[0].Worn.Mode != "one" {
		t.Fatal(ds)
	}
	for _, change := range []struct{ from, to string }{{`mode "one"`, `mode "missing"`}, {`definition "longsword"`, `definition "missing"`}} {
		bad := strings.Replace(source, clause, strings.Replace(clause, change.from, change.to, 1), 1)
		_, ds := compiler.CompileWorld("bad-worn", bad)
		at := strings.Index(bad, change.to)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 {
			t.Fatal("worn declaration position", ds)
		}
	}
}
