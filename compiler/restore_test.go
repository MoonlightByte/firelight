package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

const restoreSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { resource "hp" = 2 min 0 max 9; }
ability "rest" named "Rest" {
 parameter "actor" character;
 restore param "actor" resource "hp";
 restore "a" resource "hp";
}`

func TestRestoreSourceAndPositions(t *testing.T) {
	b, ds := compiler.CompileWorld("rest-world", restoreSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, source := range []string{`restore "a" resource "hp";`, `invoke "rest" { "actor" = character "a"; };`} {
		ops, ds := compiler.CompileActions("rest-action", source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		r, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
		if err != nil || r.Events[0].Restoration.After.Current != 9 {
			t.Fatal(r, err)
		}
	}
	for _, source := range []string{`restore "room" resource "hp";`, `restore "a" resource "missing";`} {
		_, ds := compiler.CompileActions("bad-rest", source, b)
		needle := `"room"`
		if strings.Contains(source, "missing") {
			needle = `"missing"`
		}
		if len(ds) != 1 || ds[0].Position.Source != "bad-rest" || ds[0].Position.Line != 1 || ds[0].Position.Column != strings.Index(source, needle)+1 {
			t.Fatal(ds)
		}
	}
	bad := strings.Replace(restoreSource, `restore "a"`, `restore "room"`, 1)
	_, ds = compiler.CompileWorld("bad-literal", bad)
	if len(ds) != 1 || ds[0].Position.Line != 7 || ds[0].Position.Column != 10 {
		t.Fatal(ds)
	}
	bad = strings.Replace(restoreSource, `"actor" character`, `"actor" int`, 1)
	if _, ds = compiler.CompileWorld("bad-parameter", bad); len(ds) != 1 {
		t.Fatal(ds)
	}
}
