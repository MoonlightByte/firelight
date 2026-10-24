package compiler_test

// 7b-engine commit A (kit agents/kw/plan-7b-engine-r0.md): `min N` on a rolled
// heal, with exact diagnostic positions.

import (
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

func rolledCareStep(t *testing.T, b core.Blueprint) *core.RollResourceTemplate {
	t.Helper()
	for _, d := range b.AbilityDefinitions {
		if d.ID == "rolled-care" {
			for _, s := range d.Steps {
				if s.RollResource != nil {
					return s.RollResource
				}
			}
		}
	}
	t.Fatal("rolled-care has no rolled step")
	return nil
}

func diagnosticAt(t *testing.T, source, token, code string) {
	t.Helper()
	_, ds := compiler.CompileWorld("floor", source)
	at := strings.Index(source, token) + strings.LastIndex(token, strings.Fields(token)[len(strings.Fields(token))-1])
	if len(ds) != 1 || ds[0].Code != code || ds[0].Position.Line != strings.Count(source[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(source[:at], "\n") {
		t.Fatal("diagnostic", token, ds)
	}
}

func TestRolledHealFloorSource(t *testing.T) {
	floored := strings.Replace(authoredRollSource(t), "dice 2 sides 8 bonus", "dice 2 sides 8 min 1 bonus", 1)
	b, ds := compiler.CompileWorld("floor", floored)
	if len(ds) != 0 || rolledCareStep(t, b).Minimum != 1 {
		t.Fatal("heal floor", ds)
	}
	plain, ds := compiler.CompileWorld("plain", authoredRollSource(t))
	if len(ds) != 0 || rolledCareStep(t, plain).Minimum != 0 {
		t.Fatal("no floor", ds)
	}
	diagnosticAt(t, strings.Replace(authoredRollSource(t), "dice 2 sides 8 bonus", "dice 2 sides 8 min 0 bonus", 1), "sides 8 min 0", "E_ROLL")
	damage := strings.Replace(authoredRollSource(t), "roll heal", "roll damage", 1)
	diagnosticAt(t, strings.Replace(damage, "dice 2 sides 8 bonus", "dice 2 sides 8 min 2 bonus", 1), "sides 8 min 2", "E_ROLL")
}
