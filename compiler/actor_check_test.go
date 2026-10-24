package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"os"
	"strings"
	"testing"
)

func TestActorCheckSourceAndPosition(t *testing.T) {
	s := battleSource(t)
	s = strings.Replace(s, `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead; action "skill";`, 1)
	s += `ability "skill" named "Skill" {
 action actor "tess" phase either;
 check "skill" die 20 actor_roll plus stat "tess" "strength" against 20 {
 success {} failure {}
 }
}`
	b, ds := compiler.CompileWorld("native", s)
	if len(ds) != 0 || !b.AbilityDefinitions[len(b.AbilityDefinitions)-1].Steps[0].Check.ActorRoll {
		t.Fatal(ds)
	}
	ops, ds := compiler.CompileActions("check", `invoke "skill" { roll 0 { face 7; }; };`, b)
	if len(ds) != 0 || ops[0].Invoke.HumanRolls[0].Faces[0] != 7 {
		t.Fatal(ds)
	}
	// The same opt-in without a native Action must point at its exact keyword.
	bad := strings.Replace(s, ` action actor "tess" phase either;`, "", 1)
	at := strings.Index(bad, "actor_roll")
	_, ds = compiler.CompileWorld("bad", bad)
	if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
		t.Fatal("actor_roll source site", ds)
	}
}

func TestActorCheckDoesNotChangeAlphaRollPolicy(t *testing.T) {
	data, err := os.ReadFile("../examples/alpha/adventure.nql")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), "die 20 plus", "die 20 actor_roll plus", 1)
	at := strings.Index(source, "actor_roll")
	_, ds := compiler.CompileWorld("alpha", source)
	if len(ds) != 1 || ds[0].Code != "E_RULE" || ds[0].Position.Line != strings.Count(source[:at], "\n")+1 {
		t.Fatal("Alpha must reject opt-in at the declaration site", ds)
	}
}
