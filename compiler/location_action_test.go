package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"strings"
	"testing"
)

func TestLocationRequirementPositions(t *testing.T) {
	clause := `require at param "actor" location "gate";`
	source := strings.Replace(authoredRollSource(t), "phase either;", "phase either;\n "+clause, 1)
	b, ds := compiler.CompileWorld("at-location", source)
	if len(ds) != 0 || b.AbilityDefinitions[0].Action.Requirements[0].Location != "gate" {
		t.Fatal(ds)
	}
	for _, wrong := range []string{"tess", "missing"} {
		bad := strings.Replace(source, clause, strings.Replace(clause, `"gate"`, `"`+wrong+`"`, 1), 1)
		_, ds := compiler.CompileWorld("wrong-place", bad)
		at := strings.Index(bad, `location "`+wrong+`"`) + len("location ")
		line, column := strings.Count(bad[:at], "\n")+1, at-strings.LastIndex(bad[:at], "\n")
		if len(ds) != 1 || ds[0].Position.Source != "wrong-place" || ds[0].Position.Line != line || ds[0].Position.Column != column {
			t.Fatal(ds)
		}
	}
}
