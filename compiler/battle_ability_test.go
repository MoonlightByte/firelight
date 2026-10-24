package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"strings"
	"testing"
)

func TestBattleAuthoredActionReferencesAndSourcePositions(t *testing.T) {
	definition := `
ability "battle-wait" named "Wait" {
 parameter "actor" character;
 action actor param "actor" phase encounter;
}
`
	source := strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, "zero protected stable_down opposition dead;\n action \"battle-wait\";", 1) + definition
	b, ds := compiler.CompileWorld("forward", source)
	if len(ds) != 0 || len(b.Battle.Rules.Actions) != 1 || b.Battle.Rules.Actions[0] != "battle-wait" {
		t.Fatal("forward Ability namespace", ds)
	}
	for _, tc := range []struct{ name, source, use, code string }{
		{"missing", strings.Replace(source, `action "battle-wait";`, `action "missing";`, 1), `"missing"`, "E_ABILITY"},
		{"duplicate", strings.Replace(source, `action "battle-wait";`, "action \"battle-wait\";\n action \"battle-wait\";", 1), "\n action \"battle-wait\";\n", "E_DUPLICATE"},
		{"unopted", strings.Replace(source, `action actor param "actor" phase encounter;`, `heal param "actor" resource "hp" by 1;`, 1), `"battle-wait";`, "E_ABILITY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld(tc.name, tc.source)
			at := strings.Index(tc.source, tc.use)
			if tc.name == "duplicate" {
				at = strings.LastIndex(tc.source, tc.use) + len("\n action ")
			}
			line, column := strings.Count(tc.source[:at], "\n")+1, at-strings.LastIndex(tc.source[:at], "\n")
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Source != tc.name || ds[0].Position.Line != line || ds[0].Position.Column != column {
				t.Fatalf("want %s at%d:%d got%+v", tc.code, line, column, ds)
			}
		})
	}
}
