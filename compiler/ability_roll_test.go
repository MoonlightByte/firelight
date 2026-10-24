package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func authoredRollSource(t *testing.T) string {
	return strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, "zero protected stable_down opposition dead;\n action \"rolled-care\";", 1) + `
ability "rolled-care" named "Recorded healing" {
 parameter "actor" character;
 parameter "target" character;
 action actor param "actor" phase either;
 spend param "actor" resource "hp" by 1;
 roll heal param "target" resource "hp" dice 2 sides 8 bonus from param "actor" {
  constant 0;
  term stat "strength" offset -10 divide 2;
 };
}
`
}

func TestAbilityRollCompilerTypedFacesAndPositions(t *testing.T) {
	b, ds := compiler.CompileWorld("rolled", authoredRollSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "rolled-care" { "actor" = character "tess"; "target" = character "tess"; roll 1 { face 4; face 5; }; };`
	ops, ds := compiler.CompileActions("first", source, b)
	if len(ds) != 0 || len(ops[0].Invoke.HumanRolls) != 1 || !reflect.DeepEqual(ops[0].Invoke.HumanRolls[0].Faces, []int64{4, 5}) {
		t.Fatal(ops, ds)
	}
	other, ds := compiler.CompileActions("different", "\n"+source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("source metadata changed roll identity", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Execute(core.Request{ID: "cast", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(source, "face 5", "face 9", 1)
	_, ds = compiler.CompileActions("bad-face", bad, b)
	if len(ds) != 1 || ds[0].Code != "E_ROLL" || ds[0].Position.Column != strings.Index(bad, "9;")+1 {
		t.Fatal("wrong face position", ds)
	}
	for _, tc := range []struct{ from, to, token string }{{"dice 2", "dice 0", "0"}, {"sides 8", "sides 1", "1"}, {"roll heal", "roll spend", "spend"}} {
		s := strings.Replace(authoredRollSource(t), tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad-declaration", s)
		at := strings.Index(s, tc.to) + strings.LastIndex(tc.to, tc.token)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal("wrong declaration position", ds)
		}
	}
}

// An Ability reads no worn equipment: a roll bonus term with a slot gate is
// refused, though the world declares the slot. A bonus fault points at the
// bonus recipe, as for its requires and unless.
func TestAbilityRollRefusesSlotGate(t *testing.T) {
	plain := " {\n  constant 0;\n  term stat \"strength\" offset -10 divide 2;\n };\n}\n"
	s := authoredRollSource(t)
	if !strings.HasSuffix(s, plain) {
		t.Fatal("the roll bonus recipe moved")
	}
	s = strings.TrimSuffix(s, plain) + strings.Replace(plain, "divide 2;", `divide 2 unless slot "hand";`, 1)
	_, ds := compiler.CompileWorld("rolled-slot", s)
	at := strings.LastIndex(s, "constant 0") + len("constant ")
	if len(ds) != 1 || ds[0].Code != "E_EQUIPMENT" || ds[0].Expected != "no slot gate in an Ability recipe" || ds[0].Actual != `"0" (hand)` ||
		ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
		t.Fatal(ds)
	}
}
