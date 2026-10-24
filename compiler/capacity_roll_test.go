package compiler_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func TestCapacityRollSourceAndMinimumPosition(t *testing.T) {
	raw, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(raw), "zero protected stable_down opposition dead;", "zero protected stable_down opposition dead; action \"growth\";", 1) + `
ability "growth" named "Recorded growth" {
 parameter "actor" character;
 action actor param "actor" phase exploration;
 roll expand param "actor" resource "hp" dice 1 sides 10 min 1 bonus from param "actor" { constant 2; };
}
`
	b, ds := compiler.CompileWorld("growth", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.AbilityDefinitions[len(b.AbilityDefinitions)-1].Steps[0].RollResource.Minimum != 1 {
		t.Fatal("missing authored minimum")
	}
	for _, tc := range []struct{ from, to, token string }{
		{"min 1 bonus", "min 0 bonus", "0"},
		{"min 1 bonus", "min -1 bonus", "-1"},
		{"min 1 bonus", "min 9223372036854775808 bonus", "9223372036854775808"},
		{"sides 10 min", "sides 1 min", "1"},
	} {
		s := strings.Replace(source, tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad-growth", s)
		at := strings.Index(s, tc.to) + strings.Index(tc.to, tc.token)
		if len(ds) != 1 || ds[0].Position.Source != "bad-growth" || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
	for _, s := range []string{
		strings.Replace(source, " min 1 bonus", " bonus", 1),
		// A heal may now declare a positive floor (7b); damage still may not.
		strings.Replace(source, "roll expand", "roll damage", 1),
	} {
		if _, ds := compiler.CompileWorld("bad-form", s); len(ds) == 0 {
			t.Fatal("expansion minimum missing or attached to damage")
		}
	}
}
