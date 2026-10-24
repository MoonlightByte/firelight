package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

const bufferSource = `rules { transfer unequips; wear any; }
location "hall" named "Hall";
character "a" named "A" at "hall" {
 resource "hp" = 20 min 0 max 20;
 resource "temp" = 5 min 0 max 100;
 buffer "temp" before "hp";
}`

func TestBufferSyntaxAndDiagnostics(t *testing.T) {
	b, ds := CompileWorld("buffer", bufferSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, source := range []string{`grant "a" resource "temp" by 9;`, `damage "a" resource "hp" by 8;`, `restore "a" resource "temp";`} {
		ops, ds := CompileActions("action", source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		w, e := core.NewWorld(b)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); e != nil {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct{ source, code, needle string }{{`grant "hall" resource "temp" by 1;`, "E_TYPE", `"hall"`}, {`grant "a" resource "missing" by 1;`, "E_RESOURCE", `"missing"`}, {`grant "a" resource "temp" by 0;`, "E_AMOUNT", "0"}, {`grant "a" resource "temp" by 9223372036854775808;`, "E_INTEGER", "9223372036854775808"}} {
		_, ds := CompileActions("invalid", tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.needle)+1 {
			t.Fatal(tc, ds)
		}
	}
	for _, tc := range []struct {
		clause    string
		line, col int
	}{{`buffer "temp" before "missing";`, 6, 23}, {`buffer "missing" before "hp";`, 6, 9}, {"buffer \"temp\" before \"hp\";\n buffer \"temp\" before \"hp\";", 7, 9}} {
		source := strings.Replace(bufferSource, `buffer "temp" before "hp";`, tc.clause, 1)
		_, ds := CompileWorld("invalid", source)
		if len(ds) != 1 || ds[0].Code != "E_BUFFER" || ds[0].Line != tc.line || ds[0].Column != tc.col {
			t.Fatal(tc, ds)
		}
	}
}
func TestBufferCreatedCharacterAndAbilityComposition(t *testing.T) {
	b, ds := CompileWorld("world", bufferSource+`
ability "hit" named "Hit" { damage "a" resource "hp" by 8; }
ability "clear" named "Clear" { restore "a" resource "temp"; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	for i, source := range []string{`invoke "hit" {};`, `invoke "clear" {};`} {
		ops, ds := CompileActions("action", source, w.Snapshot().Blueprint)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		_, e = w.Execute(core.Request{ID: source, ExpectedRevision: core.Revision(i), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
		if e != nil {
			t.Fatal(e)
		}
	}
	if w.Snapshot().Characters[0].Resources["hp"].Current != 17 {
		t.Fatal("Ability bypassed buffer")
	}
	source := `create character "b" named "B" at "hall" { resource "hp" = 20 min 0 max 20; resource "temp" = 5 min 0 max 10; buffer "temp" before "hp"; };`
	ops, ds := CompileActions("create", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	_, e = w.Execute(core.Request{ID: "birth", ExpectedRevision: 2, Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if e != nil || len(w.Snapshot().Characters[1].Buffers) != 1 {
		t.Fatal(e)
	}
	source = strings.Replace(source, `before "hp"`, `before "missing"`, 1)
	_, ds = CompileActions("bad-create", source, b)
	if len(ds) != 1 || ds[0].Code != "E_BUFFER" || ds[0].Column != strings.Index(source, `"missing"`)+1 {
		t.Fatal(ds)
	}
	if _, ds = CompileWorld("unsupported", bufferSource+` ability "grant" named "Grant" { grant "a" resource "temp" by 1; }`); len(ds) == 0 {
		t.Fatal("unreviewed Ability grant")
	}
}
