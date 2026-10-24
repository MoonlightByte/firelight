package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

const checkSource = `rules { transfer unequips; wear any; }
dice seed 7 11;
location "room" named "Room";
character "a" named "A" at "room" { stat "dexterity" = 14; stat "bonus:checks" = 0; stat "check:dex" = 0; stat "save:dex" = 0; resource "hp" = 10 min 0 max 10; }
condition type "poisoned" { instances coexist; modifier "checks" stat "bonus:checks" roll disadvantage; }
condition type "unconscious" { instances coexist; held_at_minimum "hp"; modifier "save" stat "save:dex" roll fail; }
equipment "nql-equipment-v1" {
 slot "body" capacity 1;
 derive stat "check:dex" { term stat "dexterity" offset -10 divide 2; term stat "bonus:checks" offset 0 divide 1; }
 derive stat "save:dex" { term stat "dexterity" offset -10 divide 2; }
}
`

func TestStatCheckSourceAndAction(t *testing.T) {
	b, ds := compiler.CompileWorld("check", checkSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	hp := core.ResourceID("hp")
	want := []core.ConditionDefinition{
		{ID: "poisoned", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "checks", Stat: "bonus:checks", Roll: core.Disadvantage}}},
		{ID: "unconscious", Instances: core.Coexist, HeldAtMinimum: &hp, Modifiers: []core.ConditionModifier{{ID: "save", Stat: "save:dex", Roll: core.AutoFail}}},
	}
	if !reflect.DeepEqual(b.ConditionDefinitions, want) || !reflect.DeepEqual(b.Dice, &core.Dice{Seed: [2]uint64{7, 11}}) {
		t.Fatalf("lowered %+v %+v", b.ConditionDefinitions, b.Dice)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("act", `apply condition "a:poison" of "poisoned" to "a";
check "a" stat "check:dex" die 20 against 12 advantage faces 5;
check "a" stat "check:dex" die 20 against 12;
damage "a" resource "hp" by 10;
check "a" stat "save:dex" die 20 against 15;
check "a" stat "save:dex" die 20;`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	dc := int64(12)
	if want := (core.StatCheck{Character: "a", Stat: "check:dex", Sides: 20, DC: &dc, Mode: core.Advantage, Faces: []int64{5}}); !reflect.DeepEqual(*ops[1].StatCheck, want) {
		t.Fatalf("%+v", *ops[1].StatCheck)
	}
	batch, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	var got []core.StatCheckResult
	for _, e := range batch.Events {
		if e.StatCheck != nil {
			got = append(got, *e.StatCheck)
		}
	}
	// Advantage cancels the poison; the engine rolls the poisoned check.
	if len(got) != 4 || got[0].Roll != "" || got[0].Kept != 5 || *got[0].Total != 7 || *got[0].Success ||
		got[1].Roll != core.Disadvantage || !got[1].Drawn || len(got[1].Faces) != 2 ||
		got[2].Roll != core.AutoFail || *got[2].Success || got[2].Total != nil || got[3].Success != nil {
		t.Fatalf("%+v", got)
	}
}

func TestStatCheckDiagnosticSites(t *testing.T) {
	adventure, err := os.ReadFile("../examples/alpha/adventure.nql")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ source, at, code string }{
		{strings.Replace(checkSource, "dice seed 7 11;", "dice seed 7 11;\ndice seed 1 2;", 1), "dice seed 1 2", "E_DUPLICATE"},
		{strings.Replace(checkSource, "seed 7 11", "seed 7 -1", 1), "-1", "E_AMOUNT"},
		{strings.Replace(checkSource, "roll disadvantage", "roll sometimes", 1), "sometimes", "E_PARSE"},
		{strings.Replace(checkSource, "roll disadvantage", "roll disadvantage 2", 1), "2;", "E_PARSE"},
		{strings.Replace(checkSource, `modifier "save" stat "save:dex" roll fail`, `modifier "save" stat "save:dex" add 1`, 1), `"save" stat "save:dex" add`, "E_RULE"},
		{string(adventure) + "\ndice seed 1 2;\n", "dice seed 1 2", "E_DICE"},
	} {
		_, ds := compiler.CompileWorld("bad", tc.source)
		index := strings.Index(tc.source, tc.at)
		line, col := strings.Count(tc.source[:index], "\n")+1, index-strings.LastIndex(tc.source[:index], "\n")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at %d:%d, got %+v", tc.at, tc.code, line, col, ds)
		}
	}
	b, ds := compiler.CompileWorld("check", checkSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, at, code string }{
		{`check "nobody" stat "check:dex" die 20;`, `"nobody"`, "E_REFERENCE"},
		{`check "a" stat "missing" die 20;`, `"missing"`, "E_STAT"},
		{`check "a" stat "check:dex" die 1;`, `1;`, "E_ROLL"},
		{`check "a" stat "check:dex" die 0;`, `0;`, "E_AMOUNT"},
		{`check "a" stat "check:dex" die 20 faces 21;`, `faces`, "E_ROLL"},
		{`check "a" stat "check:dex" die 20 faces 1 2 3;`, `faces`, "E_ROLL"},
		{`check "a" stat "check:dex" die 20 fail;`, `fail`, "E_PARSE"},
		{`check "a" stat "check:dex" die 20 faces;`, `;`, "E_PARSE"},
		{`check "a" stat "check:dex" die 20 advantage against 12;`, `against`, "E_PARSE"},
	} {
		_, ds := compiler.CompileActions("act", tc.source, b)
		col := strings.Index(tc.source, tc.at) + 1
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != 1 || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at 1:%d, got %+v", tc.source, tc.code, col, ds)
		}
	}
}
