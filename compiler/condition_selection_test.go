package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestConditionModifierSelectionSourceAndDiagnostics(t *testing.T) {
	s := strings.Replace(conditionCatalog, `instances coexist;`, `instances coexist; modifier_selection latest;`, 1)
	b, ds := CompileWorld("selection", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	found := false
	for _, d := range b.ConditionDefinitions {
		if d.ID == "bless" {
			found = d.ModifierSelection == core.LatestModifiers
		}
	}
	if !found {
		t.Fatal("authored selection missing")
	}
	other, ds := CompileWorld("different", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source changed selection", ds)
	}
	for _, tc := range []struct{ replacement, token string }{
		{`modifier_selection unknown;`, `unknown`},
		{`modifier_selection latest; modifier_selection latest;`, `modifier_selection`},
	} {
		bad := strings.Replace(s, `modifier_selection latest;`, tc.replacement, 1)
		at := strings.Index(bad, tc.replacement) + strings.LastIndex(tc.replacement, tc.token)
		_, ds := CompileWorld("bad", bad)
		if len(ds) != 1 || ds[0].Line != strings.Count(bad[:at], "\n")+1 || ds[0].Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
}
