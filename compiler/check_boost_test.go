package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"strings"
	"testing"
)

func TestCheckBoostGrammarAndDiagnosticSite(t *testing.T) {
	source := strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead; action "skill";`, 1) + `
condition type "grant" { instances unique; }
ability "skill" named "Skill" {
 action actor "tess" phase either;
 check "try" die 20 actor_roll plus 3 against 10 {
  boost condition "grant" resource "hp" cost 1 die 10 on_success;
  success {} failure {}
 }
}`
	b, ds := compiler.CompileWorld("skill", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, input := range []string{`boost 0 use face 1;`, `boost 0 decline;`, `boost 0 use;`} {
		ops, ds := compiler.CompileActions("choice", `invoke "skill" { roll 0 { face 6; }; `+input+` };`, b)
		if len(ds) != 0 || ops[0].Invoke.CheckBoost == nil {
			t.Fatal(input, ds)
		}
	}
	for _, input := range []string{`boost 0 decline face 1;`, `boost 1 use face 1;`, `boost 0 use face 11;`, `boost 0 use; boost 0 decline;`} {
		_, ds := compiler.CompileActions("choice", `invoke "skill" { `+input+` };`, b)
		if len(ds) != 1 || ds[0].Position.Line != 1 {
			t.Fatal("malformed choice", input, ds)
		}
	}
	bad := strings.Replace(source, "cost 1", "cost 0", 1)
	at := strings.Index(bad, "boost condition")
	_, ds = compiler.CompileWorld("bad", bad)
	if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 {
		t.Fatal("boost declaration site", ds)
	}
}
