package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

func battleSource(t *testing.T) string {
	t.Helper()
	s, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	return string(s)
}
func TestBattleSourceDefinitionAndTypedInputs(t *testing.T) {
	source := battleSource(t)
	b, ds := compiler.CompileWorld("battle.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.Battle == nil || b.Battle.Rules.CriticalMultiplier != 2 || b.Equipment.Definitions[0].Modes[0].Attacks[0].Melee.Reach != 5 {
		t.Fatal(b)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := os.ReadFile("../examples/native_melee/begin.nql")
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("begin", string(begin), b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	r, err := w.Execute(core.Request{ID: "begin", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 1 || r.Events[0].Kind != core.BattleStarted {
		t.Fatal(r)
	}
	for _, source := range []string{`battle attack "gate-encounter" round 1 cursor 0 by "tess" using "opaque-grant" target "raider" finish true {};`, `battle attack "gate-encounter" round 1 cursor 0 by "tess" using "opaque-grant" target "raider" finish false { attack 20; damage 4; damage 5; };`, `battle attack "gate-encounter" round 1 cursor 0 by "raider" using "opaque-grant" target "tess" finish true;`, `battle end "gate-encounter" round 1 cursor 0 by "tess";`} {
		left, ds := compiler.CompileActions("first", source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		right, ds := compiler.CompileActions("different-file", "\n\n"+source, b)
		if len(ds) != 0 || !reflect.DeepEqual(left, right) {
			t.Fatal("source location altered semantic intent", ds)
		}
	}
}
func TestBattleCompilerPositionsAndBounds(t *testing.T) {
	b, ds := compiler.CompileWorld("world", battleSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	cases := []struct{ source, token string }{
		{`battle begin "new" at "gate" { participant "gate" side protected control human at 0 0 tie 0; participant "raider" side opposition control agent at 1 0 tie 1; };`, `"gate" side`},
		{`battle attack "gate-encounter" round -1 cursor 0 by "tess" using "grant" target "raider" finish true;`, `-1`},
		{`battle attack "gate-encounter" round 1 cursor 0 by "tess" using "grant" target "gate" finish true;`, `"gate" finish`},
		{`battle attack "gate-encounter" round 1 cursor 0 by "tess" using "grant" target "raider" finish true { attack 21; };`, `21`},
		{`battle begin "new" at "gate" { participant "tess" side protected control human at 1025 0 tie 0; participant "raider" side opposition control agent at 1 0 tie 1; };`, `1025`},
	}
	for _, tc := range cases {
		_, ds := compiler.CompileActions("bad.nql", "\n"+tc.source, b)
		if len(ds) != 1 || ds[0].Position.Source != "bad.nql" || ds[0].Position.Line != 2 || ds[0].Position.Column != strings.Index(tc.source, tc.token)+1 {
			t.Fatalf("%s: %+v", tc.source, ds)
		}
	}
	for _, tc := range []struct{ from, to, token string }{
		{`miss 1 critical 20`, `miss 1 critical 1`, `1`},
		{`melee reach 5`, `melee reach 0`, `0`},
		{`roll_condition "advantage"`, `roll_condition "missing"`, `"missing"`},
		{`party "tess" relation`, `party "gate" relation`, `"gate"`},
	} {
		s := strings.Replace(battleSource(t), tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad-world", s)
		if len(ds) != 1 || ds[0].Position.Line <= 1 || ds[0].Position.Column <= 0 {
			t.Fatal(ds)
		}
	}
}
func FuzzBattleCompiler(f *testing.F) {
	f.Add(`battle attack "gate" round 1 cursor 0 by "a" using "g" target "b" finish true { attack 20; damage 4; damage 5; };`)
	f.Add(`battle begin "x" at "room" { participant "a" side protected control human at 0 0 tie 0 initiative 15; };`)
	f.Fuzz(func(t *testing.T, s string) {
		compiler.CompileActions("fuzz", s, core.Blueprint{})
		compiler.CompileWorld("fuzz", s)
	})
}

func TestBattleWearerReferenceKeepsItsUseSite(t *testing.T) {
	source := strings.Replace(battleSource(t), `wearer "tess"`, `wearer "gate"`, 1)
	at := strings.Index(source, `wearer "gate"`) + len("wearer ")
	line := strings.Count(source[:at], "\n") + 1
	column := at - strings.LastIndex(source[:at], "\n")
	_, ds := compiler.CompileWorld("wrong-wearer", source)
	if len(ds) != 1 || ds[0].Code != "E_TYPE" || ds[0].Position.Line != line || ds[0].Position.Column != column {
		t.Fatalf("expected wearer%d:%d; got%+v", line, column, ds)
	}
}
