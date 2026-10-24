package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

func TestLexerPositionsEscapesAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		src       string
		code      string
		line, col int
	}{
		{"\n  @", "E_TOKEN", 2, 3},
		{"// hi\n\xff", "E_UTF8", 2, 1},
		{"// \xff", "E_UTF8", 1, 4},
		{"\"x\xff\"", "E_UTF8", 1, 3},
		{"\"\\\xff\"", "E_UTF8", 1, 3},
		{`"\x41"`, "E_ESCAPE", 1, 1},
		{`"\uD800"`, "E_ESCAPE", 1, 1},
		{`"\uDC00"`, "E_ESCAPE", 1, 1},
		{`"\uD800\u0041"`, "E_ESCAPE", 1, 1},
		{`"unterminated`, "E_STRING", 1, 1},
	} {
		_, d := lex("test.nql", tc.src, MaxSourceBytes)
		if d == nil || d.Code != tc.code || d.Line != tc.line || d.Column != tc.col {
			t.Fatalf("%q: %+v", tc.src, d)
		}
	}
	ts, d := lex("unicode", `"\uD83D\uDE00" "é" "\\uD800"`, MaxSourceBytes)
	if d != nil || ts[0].Text != "😀" || ts[1].Text != "é" || ts[2].Text != `\uD800` {
		t.Fatalf("valid JSON escape semantics: %v %+v", ts, d)
	}
	_, d = lex("large", strings.Repeat(" ", MaxSourceBytes), MaxSourceBytes)
	if d != nil {
		t.Fatal("exact source boundary rejected", d)
	}
	_, d = lex("large", strings.Repeat(" ", MaxSourceBytes+1), MaxSourceBytes)
	if d == nil || d.Code != "E_LIMIT" {
		t.Fatal("oversized source accepted")
	}
}
func TestWorldDuplicateAndForwardReferenceDiagnostics(t *testing.T) {
	valid := `rules { transfer unequips; wear any; }
character "c" named "C" at "l" { stat "x" = -9223372036854775808; resource "r" = 0 min -1 max 1; }
item "i" named "I" { owner "c"; }
location "l" named "L";`
	if _, ds := CompileWorld("forward", valid); len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, src := range []string{
		valid + `location "c" named "Collision";`,
		strings.Replace(valid, `stat "x" = -9223372036854775808;`, `stat "x" = 1; stat "x" = 2;`, 1),
		strings.Replace(valid, `owner "c";`, `owner "c"; owner "c";`, 1),
		strings.Replace(valid, `wear any;`, `wear any; wear owner_only;`, 1),
		strings.Replace(valid, `resource "r" = 0 min -1 max 1;`, `resource "r" = 0 min -1 max 1; resource "r" = 0 min 0 max 1;`, 1),
	} {
		_, ds := CompileWorld("dup.nql", src)
		if len(ds) != 1 || ds[0].Code != "E_DUPLICATE" {
			t.Fatalf("duplicate accepted/misdiagnosed: %+v", ds)
		}
	}
}
func FuzzCompiler(f *testing.F) {
	for _, s := range []string{"", `rules { transfer unequips; wear any; }`, `move "x" to "y";`, `"\uD800"`, string([]byte{0xff, 0xfe}), `damage "c" resource "hp" by 9223372036854775808;`, `"\"`, "//\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxSourceBytes+1 {
			return
		}
		b, ds := CompileWorld("fuzz", s)
		if len(ds) == 0 {
			if e := core.Validate(b); e != nil {
				t.Fatalf("compiler produced invalid blueprint: %v", e)
			}
		}
		_, ds = CompileActions("fuzz", s, core.Blueprint{})
		for _, d := range ds {
			if d.Line < 1 || d.Column < 1 || d.Source != "fuzz" || d.Code == "" {
				t.Fatalf("invalid diagnostic: %+v", d)
			}
		}
	})
}

func TestReferenceUseSiteDiagnostics(t *testing.T) {
	for _, field := range []string{"owner", "wearer"} {
		src := `rules { transfer unequips; wear any; } item "i" named "I" { ` + field + ` ""; }`
		_, ds := CompileWorld("empty.nql", src)
		if len(ds) != 1 || ds[0].Code != "E_REFERENCE" || ds[0].Column != strings.Index(src, `""`)+1 {
			t.Errorf("explicit empty %s accepted or mispositioned: %+v", field, ds)
		}
	}
	missing := "rules { transfer unequips; wear any; }\n\ncharacter \"c\" named \"C\" at \"loc:typo\" {}"
	_, ds := CompileWorld("missing.nql", missing)
	if len(ds) != 1 || ds[0].Line != 3 || ds[0].Column != 28 || !strings.Contains(ds[0].Actual, "loc:typo") {
		t.Errorf("missing reference use site lost: %+v", ds)
	}
	wrong := "rules { transfer unequips; wear any; }\nlocation \"target\" named \"Place\";\ncharacter \"c\" named \"C\" at \"target\" {}\nitem \"i\" named \"I\" { owner \"target\"; }"
	_, ds = CompileWorld("type.nql", wrong)
	if len(ds) != 1 || ds[0].Code != "E_TYPE" || ds[0].Line != 4 || ds[0].Column != 28 || !strings.Contains(ds[0].Actual, "target") {
		t.Errorf("wrong-kind use site lost: %+v", ds)
	}
	b := core.Blueprint{Characters: []core.Character{{ID: "c", Stats: map[core.StatID]int64{"defense": 1}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 1, Minimum: 0, Maximum: 2}}}}}
	for _, src := range []string{`damage "c" resource "mana_typo" by 1;`, `apply effect "e" from character "c" on character "c" stat "mana_typo" add 1;`} {
		_, ds = CompileActions("action.nql", src, b)
		if len(ds) != 1 || ds[0].Column != strings.Index(src, `"mana_typo"`)+1 || !strings.Contains(ds[0].Actual, "mana_typo") {
			t.Errorf("missing field use site lost: %+v", ds)
		}
	}
}

func TestEffectIdentityDiagnosticOperands(t *testing.T) {
	prefix := `rules { transfer unequips; wear any; } location "l" named "L"; character "c" named "C" at "l" { stat "defense" = 1; } `
	b, ds := CompileWorld("catalog", prefix)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, body := range []string{`effect "" from character "c" on character "c" stat "defense" add 1;`, `effect "effect:valid" from character "c" on character "c" stat "" add 1;`} {
		for _, action := range []bool{false, true} {
			src := prefix + body
			var ds []Diagnostic
			if action {
				src = "apply " + body
				_, ds = CompileActions("effect.nql", src, b)
			} else {
				_, ds = CompileWorld("effect.nql", src)
			}
			if len(ds) != 1 || ds[0].Code != "E_ID" || ds[0].Column != strings.Index(src, `""`)+1 || !strings.HasPrefix(ds[0].Actual, `""`) {
				t.Errorf("action=%v, wrong invalid effect operand: %+v", action, ds)
			}
		}
	}
}
