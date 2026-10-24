package compiler_test

import (
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const xpSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "xp" = 0; stat "level" = 1; stat "next" = 0; }
equipment "nql-equipment-v1" {
 derive stat "next" { term stat "level" offset 0 divide 1 step 1 = 300 step 2 = 900 step 3 = 0; }
}
`

func TestStatAdjustSourceAndAction(t *testing.T) {
	b, ds := compiler.CompileWorld("xp", xpSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if want := []core.TermStep{{At: 1, Value: 300}, {At: 2, Value: 900}, {At: 3, Value: 0}}; !reflect.DeepEqual(b.Equipment.Derived[0].Terms[0].Steps, want) {
		t.Fatalf("%+v", b.Equipment.Derived[0])
	}
	ops, ds := compiler.CompileActions("act", `adjust "a" stat "xp" by 300;
adjust "a" stat "level" by -1;`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if !reflect.DeepEqual(*ops[0].AdjustStat, core.AdjustStat{Character: "a", Stat: "xp", Amount: 300}) || !reflect.DeepEqual(*ops[1].AdjustStat, core.AdjustStat{Character: "a", Stat: "level", Amount: -1}) || ops[0].AdjustRelationship != nil {
		t.Fatalf("%+v", ops)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops[:1]}); err != nil {
		t.Fatal(err)
	}
	if x, err := w.Explain("a", "xp"); err != nil || x.Base != 300 {
		t.Fatal(x, err)
	}
}

func TestStatAdjustDiagnosticSites(t *testing.T) {
	var many strings.Builder
	for i := range core.MaxTermSteps + 1 {
		fmt.Fprintf(&many, "step %d = 1 ", i)
	}
	with := func(old, new string) string { return strings.Replace(xpSource, old, new, 1) }
	for _, tc := range []struct{ source, at, code string }{
		{with("step 2 = 900", "step 1 = 900"), "1 = 900", "E_EQUIPMENT"},
		{with("step 3 = 0", "step 2 = 0"), "2 = 0", "E_EQUIPMENT"},
		{with("divide 1 step", "divide 1 ceiling 5 step"), "step 1", "E_EQUIPMENT"},
		{with("step 1 = 300 step 2 = 900 step 3 = 0", many.String()), fmt.Sprintf("step %d", core.MaxTermSteps), "E_EQUIPMENT"},
		{with("step 1 = 300", "step 1 300"), "300", "E_PARSE"},
		{with("step 1 = 300", "step = 300"), "= 300", "E_PARSE"},
	} {
		_, ds := compiler.CompileWorld("bad", tc.source)
		index := strings.Index(tc.source, tc.at)
		line, col := strings.Count(tc.source[:index], "\n")+1, index-strings.LastIndex(tc.source[:index], "\n")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at %d:%d, got %+v", tc.at, tc.code, line, col, ds)
		}
	}
	b, ds := compiler.CompileWorld("xp", xpSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, at, code string }{
		{`adjust "a" stat "xp" by 0;`, "0;", "E_AMOUNT"},
		{`adjust "a" stat "gold" by 1;`, `"gold"`, "E_STAT"},
		{`adjust "a" stat "next" by 1;`, `"next"`, "E_RULE"},
		{`adjust "z" stat "xp" by 1;`, `"z"`, "E_REFERENCE"},
		{`adjust "a" "xp" by 1;`, `"xp"`, "E_PARSE"},
		{`adjust "a" stat "xp";`, ";", "E_PARSE"},
		{`adjust stat "xp" by 1;`, "stat", "E_PARSE"},
	} {
		_, ds := compiler.CompileActions("act", tc.source, b)
		col := strings.Index(tc.source, tc.at) + 1
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != 1 || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at 1:%d, got %+v", tc.source, tc.code, col, ds)
		}
	}
}
