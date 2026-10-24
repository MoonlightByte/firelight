package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

const ammoSource = `rules { transfer unequips; wear any; recovery 1 of 2; }
location "field" named "Field";
character "a" named "A" at "field" { }
item "arrows" named "Arrows" { owner "a"; custody character "a"; quantity 20; recoverable 3; }
`

func TestRecoverySourceAndActions(t *testing.T) {
	b, ds := compiler.CompileWorld("ammo", ammoSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.Rules.RecoveryNumerator != 1 || b.Rules.RecoveryDenominator != 2 || b.Items[0].Recoverable != 3 {
		t.Fatalf("%+v %+v", b.Rules, b.Items)
	}
	ops, ds := compiler.CompileActions("act", `consume "arrows" from "a" by 2 recoverable;
consume "arrows" from "a" by 1;
recover "arrows" from "a";
recover "arrows" from "a" by 0;`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	zero := int64(0)
	if !reflect.DeepEqual(*ops[0].Consume, core.ConsumeItem{Item: "arrows", Owner: "a", Amount: 2, Recoverable: true}) || ops[1].Consume.Recoverable ||
		!reflect.DeepEqual(*ops[2].RecoverItem, core.RecoverItem{Item: "arrows", Owner: "a"}) || !reflect.DeepEqual(*ops[3].RecoverItem, core.RecoverItem{Item: "arrows", Owner: "a", Amount: &zero}) {
		t.Fatalf("%+v", ops)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	// 20 - 2 - 1 = 17 with 3 + 2 = 5 spent; half of 5 is 2.
	batch, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops[:3]})
	if err != nil {
		t.Fatal(err)
	}
	if v := batch.Events[2].ItemRecovery; v.Spent != 5 || v.Allowed != 2 || v.After != 19 {
		t.Fatalf("%+v", v)
	}
}

func TestRecoveryDiagnosticSites(t *testing.T) {
	with := func(old, new string) string { return strings.Replace(ammoSource, old, new, 1) }
	melee, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	battle := strings.Replace(string(melee), "wear any; }", "wear any; recovery 1 of 2; }", 1)
	container := `item "chest" named "Chest" { container true; custody location "field"; recoverable 1; }` + "\n"
	for _, tc := range []struct{ source, at, code string }{
		{with("recovery 1 of 2;", "recovery 1 of 2; recovery 1 of 2;"), "recovery 1 of 2; }", "E_DUPLICATE"},
		{with("recovery 1 of 2", "recovery 3 of 2"), "recovery", "E_RULE"},
		{with("recovery 1 of 2", "recovery 1 of 0"), "recovery", "E_RULE"},
		{with("recovery 1 of 2", "recovery -1 of 2"), "recovery", "E_RULE"},
		{with("recovery 1 of 2", "recovery 1 2"), "2;", "E_PARSE"},
		{battle, "recovery 1 of 2", "E_RULE"},
		{with("recoverable 3", "recoverable -1"), "-1", "E_QUANTITY"},
		{with("recoverable 3;", "recoverable 3; recoverable 3;"), "recoverable 3; }", "E_DUPLICATE"},
		{with(" recovery 1 of 2;", ""), "3; }", "E_RULE"},
		{ammoSource + container, "1; }\n", "E_RULE"},
	} {
		_, ds := compiler.CompileWorld("bad", tc.source)
		index := strings.Index(tc.source, tc.at)
		if tc.code == "E_DUPLICATE" || tc.source == ammoSource+container {
			index = strings.LastIndex(tc.source, tc.at)
		}
		line, col := strings.Count(tc.source[:index], "\n")+1, index-strings.LastIndex(tc.source[:index], "\n")
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != line || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at %d:%d, got %+v", tc.at, tc.code, line, col, ds)
		}
	}
	b, ds := compiler.CompileWorld("ammo", ammoSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	plain, ds := compiler.CompileWorld("plain", strings.Replace(with(" recovery 1 of 2;", ""), " recoverable 3;", "", 1))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		source, at, code string
		world            core.Blueprint
	}{
		{`consume "arrows" from "a" by 1 recoverable;`, "recoverable", "E_RULE", plain},
		{`recover "arrows" from "a";`, "recover", "E_RULE", plain},
		{`recover "arrows" from "a" by -1;`, "-1", "E_AMOUNT", b},
		{`recover "z" from "a";`, `"z"`, "E_REFERENCE", b},
		{`recover "arrows" from "z";`, `"z"`, "E_REFERENCE", b},
		{`recover "arrows" "a";`, `"a"`, "E_PARSE", b},
		{`recover "arrows" from "a" by;`, ";", "E_PARSE", b},
		{`consume "arrows" from "a" by 1 recoverable recoverable;`, "recoverable;", "E_PARSE", b},
		{`create item "new" named "New" { owner "a"; custody character "a"; quantity 2; recoverable -1; };`, "-1", "E_QUANTITY", b},
		{`create item "new" named "New" { owner "a"; custody character "a"; quantity 2; recoverable 1; };`, "1; }", "E_RULE", plain},
	} {
		_, ds := compiler.CompileActions("act", tc.source, tc.world)
		col := strings.Index(tc.source, tc.at) + 1
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != 1 || ds[0].Position.Column != col {
			t.Errorf("%s: want %s at 1:%d, got %+v", tc.source, tc.code, col, ds)
		}
	}
}
