package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

const encounterDeclaration = `encounter "encounter:gate" named "Gate guard" at "gate" {
 protected at 0 0;
 protected at 0 1;
 opposition "raider" at 1 0;
};`

func encounterSource(t *testing.T, declaration string) string {
	t.Helper()
	return strings.Replace(battleSource(t), "zero protected stable_down opposition dead;", "zero protected stable_down opposition dead;\n"+declaration, 1)
}
func TestAuthoredEncounterCompilerAndMultipleChoices(t *testing.T) {
	s := encounterSource(t, encounterDeclaration+"\n"+strings.Replace(encounterDeclaration, "encounter:gate", "encounter:other", 1))
	b, ds := compiler.CompileWorld("authored.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	offers := w.Snapshot().Blueprint.EncounterOffers()
	if len(offers) != 2 || offers[0].Intent == nil || offers[1].Intent == nil || offers[0].Definition.ID != "encounter:gate" || offers[1].Definition.ID != "encounter:other" || offers[0].Definition.Name != offers[1].Definition.Name {
		t.Fatal("same-label distinct choices lost", offers)
	}
}
func TestAuthoredEncounterCompilerExactPositions(t *testing.T) {
	for _, tc := range []struct {
		label, from, to, needle string
		last                    bool
	}{
		{"wrong location", `at "gate" {`, `at "tess" {`, `"tess" {`, false},
		{"wrong opponent", `opposition "raider"`, `opposition "gate"`, `"gate" at`, false},
		{"missing opponent", `opposition "raider"`, `opposition "absent"`, `"absent"`, false},
		{"out of range x", "protected at 0 0", "protected at 1025 0", "1025", false},
		{"out of range y", "opposition \"raider\" at 1 0", "opposition \"raider\" at 1 -1025", "-1025", false},
		{"duplicate square", "opposition \"raider\" at 1 0", "opposition \"raider\" at 0 0", "0 0;", true},
		{"duplicate opponent", "opposition \"raider\" at 1 0;", "opposition \"raider\" at 1 0;\n opposition \"raider\" at 2 0;", `"raider" at 2`, false},
		{"empty label", `named "Gate guard"`, `named ""`, `""`, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			declaration := strings.Replace(encounterDeclaration, tc.from, tc.to, 1)
			source := encounterSource(t, declaration)
			at := strings.Index(source, tc.needle)
			if tc.last {
				at = strings.LastIndex(source, tc.needle)
			}
			if at < 0 {
				t.Fatal("bad position oracle")
			}
			line := strings.Count(source[:at], "\n") + 1
			column := at - strings.LastIndex(source[:at], "\n")
			_, ds := compiler.CompileWorld("bad-encounter", source)
			if len(ds) != 1 || ds[0].Position.Source != "bad-encounter" || ds[0].Position.Line != line || ds[0].Position.Column != column {
				t.Fatalf("expected %d:%d; got %+v", line, column, ds)
			}
		})
	}
	duplicated := encounterSource(t, encounterDeclaration+"\n"+encounterDeclaration)
	at := strings.LastIndex(duplicated, `"encounter:gate"`)
	_, ds := compiler.CompileWorld("duplicate", duplicated)
	if len(ds) != 1 || ds[0].Position.Line != strings.Count(duplicated[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(duplicated[:at], "\n") {
		t.Fatal("duplicate did not identify second declaration", ds)
	}
}
