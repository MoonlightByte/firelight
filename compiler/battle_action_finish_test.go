package compiler_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func TestBattleActionExplicitFinishSource(t *testing.T) {
	raw, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw) + `
ability "care" named "Care without ending turn" {
 action actor "tess" phase either finish false;
 heal "tess" resource "hp" by 1;
}
`
	b, ds := compiler.CompileWorld("finish", source)
	if len(ds) != 0 || b.AbilityDefinitions[0].Action.Finish == nil || *b.AbilityDefinitions[0].Action.Finish {
		t.Fatal(ds)
	}
	for _, s := range []string{
		strings.Replace(source, "finish false", "finish maybe", 1),
		strings.Replace(source, "finish false", "finish false finish true", 1),
	} {
		if _, ds := compiler.CompileWorld("bad-finish", s); len(ds) == 0 {
			t.Fatal("invalid finish clause accepted")
		}
	}
	_, ds = compiler.CompileWorld("no-battle", `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { resource "hp" = 1 min 0 max 2; }
ability "care" named "Care" {
 action actor "a" phase either finish false;
 heal "a" resource "hp" by 1;
}`)
	if len(ds) != 1 || ds[0].Position.Line != 5 || ds[0].Position.Column != 39 || ds[0].Code != "E_RULE" {
		t.Fatal("finish source occurrence", ds)
	}
}
