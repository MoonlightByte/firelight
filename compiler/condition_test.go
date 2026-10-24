package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

const conditionCatalog = `rules { transfer unequips; wear any; }
location "l" named "L";
character "c" named "C" at "l" { stat "defense" = 10; }
condition type "bless" { instances coexist; modifier "ward" stat "defense" add magnitude; }
condition type "marked" { instances unique; }
condition type "bad-stat" { instances coexist; modifier "bad" stat "missing" add 1; }
`

func TestConditionCompilerDiagnosticsAndContinuation(t *testing.T) {
	b, ds := CompileWorld("catalog", conditionCatalog)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ src, code, operand string }{
		{`apply condition "a" of "bless" to "c" magnitude 9223372036854775808;`, "E_INTEGER", "9223372036854775808"},
		{`apply condition "a" of "missing" to "c";`, "E_CONDITION_TYPE", `"missing"`},
		{`apply condition "" of "marked" to "c";`, "E_ID", `""`},
		{`apply condition "a" of "marked" to "l";`, "E_TYPE", `"l"`},
		{`apply condition "a" of "marked" to "c" from character "l";`, "E_TYPE", `"l"`},
		{`apply condition "a" of "bad-stat" to "c";`, "E_STAT", `"c"`},
		{`apply condition "a" of "marked" to "c" magnitude 2;`, "E_CONDITION_AMOUNT", "2"},
		{`apply condition "a" of "bless" to "c";`, "E_CONDITION_AMOUNT", `"a"`},
		{`remove condition "a" from "l";`, "E_TYPE", `"l"`},
		{`remove condition "" from "c";`, "E_ID", `""`},
	} {
		src := "\n  " + tc.src
		_, ds := CompileActions("bad.nql", src, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != 2 || ds[0].Column != strings.Index(tc.src, tc.operand)+3 || ds[0].Source != "bad.nql" || ds[0].Expected == "" || ds[0].Actual == "" {
			t.Errorf("%s: %+v", tc.src, ds)
		}
	}
	malformed := []string{`apply condition "a" of "marked" to "c" from character "c" from location "l";`, `apply condition "a" of "bless" to "c" magnitude 1 magnitude 2;`, `expire condition "a";`, `apply condition "a" of "marked" to "c" from condition "b";`, `remove condition "a";`, `apply condition "a" of "marked" to "c"`, `apply condition "\uD800" of "marked" to "c";`}
	for _, src := range malformed {
		if ops, ds := CompileActions("malformed", src, b); len(ds) == 0 || ops != nil {
			t.Fatal("malformed condition accepted", src)
		}
	}
	for _, src := range []string{
		`condition type "x" {}`, `condition type "x" { instances coexist; instances unique; }`, `condition type "x" { instances unknown; }`, `condition type "x" { instances coexist; modifier "a" stat "defense" add 1; modifier "a" stat "defense" add 2; }`, `condition type "marked" { instances coexist; }`, `condition "a" of "marked" to "c"; condition "a" of "marked" to "c";`, `condition type "x" { instances coexist; modifier "" stat "defense" add 1; }`, `condition type "x" { instances coexist; modifier "a" stat "" add 1; }`, `condition type "x" { instances coexist; modifier "a" stat "defense" add 9223372036854775808; }`,
	} {
		if _, ds := CompileWorld("bad-world", conditionCatalog+src); len(ds) == 0 {
			t.Fatal("malformed declaration accepted", src)
		}
	}
	ops, ds := CompileActions("valid", `apply condition "a" of "bless" to "c" magnitude -9223372036854775808 from location "l"; remove condition "a" from "c";`, b)
	if len(ds) > 0 || len(ops) != 2 || *ops[0].ApplyCondition.Magnitude != -9223372036854775808 {
		t.Fatal("continuation/int boundary", ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	ops, ds = CompileActions("valid", `apply condition "a" of "marked" to "c";`, b)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, e = w.Execute(core.Request{ID: "valid-after-malformed", Actor: core.Ref{Kind: core.CharacterKind, ID: "c"}, Operations: ops}); e != nil {
		t.Fatal(e)
	}
}
func FuzzConditionCompiler(f *testing.F) {
	for _, src := range []string{conditionCatalog, conditionCatalog + `condition "a" of "marked" to "c";`, `apply condition "a" of "bless" to "c" from location "l" magnitude 2;`, `remove condition "a" from "c";`, `condition type "x" { instances coexist; modifier "x" stat "defense" add -9223372036854775808; }`, `apply condition "\uD800"`, "apply condition \xff"} {
		f.Add(src)
	}
	b, ds := CompileWorld("catalog", conditionCatalog)
	if len(ds) > 0 {
		f.Fatal(ds)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > MaxSourceBytes+1 {
			return
		}
		out, ds := CompileWorld("fuzz", src)
		if len(ds) == 0 {
			if _, e := core.NewWorld(out); e != nil {
				t.Fatal("invalid compiled world", e)
			}
		}
		ops, ds := CompileActions("fuzz", src, b)
		if len(ds) == 0 {
			for _, o := range ops {
				if e := b.CheckOperationStatic(o); e != nil {
					t.Fatal("invalid compiled IR", e)
				}
			}
		}
		for _, d := range ds {
			if d.Source != "fuzz" || d.Line < 1 || d.Column < 1 || d.Code == "" {
				t.Fatal("invalid diagnostic", d)
			}
		}
	})
}

func TestConditionDeclarationDiagnosticUseSites(t *testing.T) {
	prefix := "rules { transfer unequips; wear any; }\nlocation \"l\" named \"L\";\ncharacter \"second\" named \"C\" at \"l\" { stat \"defense\" = 10; }\n"
	for _, tc := range []struct {
		src, code    string
		line, column int
	}{
		{prefix + "condition type \"x\" {\n instances coexist;\n modifier \"ward\" stat \"defense\" add 1;\n modifier \"ward\" stat \"defense\" add 2;\n}", "E_DUPLICATE", 7, 11},
		{prefix + "condition type \"x\" { instances unique; }\ncondition \"first\" of \"x\" to \"second\";\ncondition \"second\" of \"x\" to \"second\";", "E_RULE", 6, 11},
	} {
		_, ds := CompileWorld("positions.nql", tc.src)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != tc.line || ds[0].Column != tc.column {
			t.Errorf("wanted %s at %d:%d; got %+v", tc.code, tc.line, tc.column, ds)
		}
	}
}
