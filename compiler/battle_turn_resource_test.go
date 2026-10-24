package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func turnResourceSource(t *testing.T) string {
	s := authoredRollSource(t)
	s = strings.Replace(s, `resource "hp"`, "resource \"turn:bonus\" = 0 min 0 max 1;\n resource \"hp\"", 1)
	s = strings.Replace(s, `action "rolled-care";`, "turn_restore \"turn:bonus\";\n action \"rolled-care\";", 1)
	return strings.Replace(s, `phase either;`, `phase either turn_cost resource "turn:bonus" by 1 finish false;`, 1)
}

func TestBattleTurnResourceSourceAndPositions(t *testing.T) {
	s := turnResourceSource(t)
	b, ds := compiler.CompileWorld("allowance", s)
	if len(ds) != 0 || !reflect.DeepEqual(b.Battle.Rules.TurnResources, []core.ResourceID{"turn:bonus"}) || b.AbilityDefinitions[0].Action.TurnCost.Amount != 1 || b.AbilityDefinitions[0].Action.TurnCost.Finish {
		t.Fatal("typed turn policy", ds)
	}
	other, ds := compiler.CompileWorld("other-name", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("diagnostic metadata changed authored identity", ds)
	}
	for _, tc := range []struct{ from, to, at, code string }{
		{`turn_restore "turn:bonus"`, `turn_restore "missing"`, `"missing"`, "E_BATTLE"},
		{`turn_restore "turn:bonus"`, `turn_restore "hp"`, `"hp"`, "E_BATTLE"},
		{`by 1 finish`, `by 0 finish`, "0", "E_AMOUNT"},
		{`turn_cost resource "turn:bonus"`, `turn_cost resource "missing"`, `"missing"`, "E_RULE"},
		{`finish false`, `finish maybe`, "maybe", "E_PARSE"},
		{`turn_restore "turn:bonus";`, "turn_restore \"turn:bonus\";\n turn_restore \"turn:bonus\";", "\n turn_restore", "E_BATTLE"},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		if tc.at == "\n turn_restore" {
			at += len("\n turn_restore ")
		}
		_, ds := compiler.CompileWorld("bad", bad)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Source != "bad" || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatalf("%s: %v", tc.to, ds)
		}
	}
}
