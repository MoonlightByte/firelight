package compiler_test

import (
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func TestActionRangeCompiler(t *testing.T) {
	clause := `require within param "actor" with param "target" distance 5 unit "feet";`
	source := strings.Replace(authoredRollSource(t), "phase either;", "phase either;\n "+clause, 1)
	b, ds := compiler.CompileWorld("range", source)
	if len(ds) != 0 || b.AbilityDefinitions[0].Action.Requirements[0].Distance.Maximum != 5 {
		t.Fatal(ds)
	}
	for _, change := range []struct{ from, to string }{{"distance 5", "distance -1"}, {`unit "feet"`, `unit "meters"`}, {`with param "target"`, `with int 3`}} {
		bad := strings.Replace(source, change.from, change.to, 1)
		_, ds := compiler.CompileWorld("bad-range", bad)
		at := strings.Index(bad, change.to)
		if len(ds) != 1 || ds[0].Position.Source != "bad-range" || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 {
			t.Fatal("range diagnostic not on declaration", ds)
		}
	}
}
