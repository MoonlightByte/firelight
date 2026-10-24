package compiler

import (
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const createNPCSource = `create character "npc" named "Mira" at "hall" {
  stat "defense" = 10;
  resource "hp" = 8 min 0 max 12;
};`

func creationCatalog() core.Blueprint {
	return core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "hall"}}, Characters: []core.Character{{ID: "actor", Name: "Mira", Location: "hall"}}}
}
func TestCharacterActionSequentialCatalogAndSourceIdentity(t *testing.T) {
	b := creationCatalog()
	backing := make([]core.Character, 4)
	copy(backing, b.Characters)
	backing[1] = core.Character{ID: "sentinel"}
	b.Characters = backing[:1]
	text := createNPCSource + ` heal "npc" resource "hp" by 2; relate character "npc" "party-member" character "actor" = 1;`
	ops, ds := CompileActions("one.nql", text, b)
	if len(ds) != 0 || len(ops) != 3 || ops[0].CreateCharacter.ID != "npc" {
		t.Fatal(ops, ds)
	}
	if backing[1].ID != "sentinel" || len(b.Characters) != 1 {
		t.Fatal("compiler wrote caller backing slice")
	}
	other, ds := CompileActions("elsewhere.nql", "\n // relocated\n"+text, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("source metadata changed intent", ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	r := core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "actor"}, Operations: ops}
	if _, e := w.Execute(r); e != nil {
		t.Fatal(e)
	}
	s := w.Snapshot()
	if s.Characters[1].Resources["hp"].Current != 10 {
		t.Fatal(s)
	}
	ops[0].CreateCharacter.Stats["defense"] = 20
	if other[0].CreateCharacter.Stats["defense"] != 10 {
		t.Fatal("separate compiles alias")
	}
}

func TestCharacterCreationPositionedFailures(t *testing.T) {
	for _, tc := range []struct {
		text, code   string
		line, column int
	}{
		{`heal "npc" resource "hp" by 2; ` + createNPCSource, "E_REFERENCE", 1, 6},
		{strings.Replace(createNPCSource, `"npc"`, `"actor"`, 1), "E_DUPLICATE", 1, 18},
		{strings.Replace(createNPCSource, `at "hall"`, `at "actor"`, 1), "E_TYPE", 1, 40},
		{strings.Replace(createNPCSource, `at "hall"`, `at "missing"`, 1), "E_REFERENCE", 1, 40},
		{strings.Replace(createNPCSource, `stat "defense" = 10;`, `stat "" = 10;`, 1), "E_ID", 2, 8},
		{strings.Replace(createNPCSource, `resource "hp" = 8`, `resource "hp" = 13`, 1), "E_BOUNDS", 3, 12},
		{strings.Replace(createNPCSource, `resource "hp"`, `resource ""`, 1), "E_ID", 3, 12},
		{strings.Replace(createNPCSource, `  stat "defense" = 10;`, `  stat "defense" = 10; stat "defense" = 2;`, 1), "E_DUPLICATE", 2, 29},
		{strings.Replace(createNPCSource, `= 10;`, `= 9223372036854775808;`, 1), "E_INTEGER", 2, 20},
		{createNPCSource + "\n" + createNPCSource, "E_DUPLICATE", 5, 18},
		{createNPCSource + ` damage "npc" resource "absent" by 1;`, "E_RESOURCE", 4, 26},
	} {
		t.Run(fmt.Sprintf("%s/%d/%d", tc.code, tc.line, tc.column), func(t *testing.T) {
			ops, ds := CompileActions("birth.nql", tc.text, creationCatalog())
			if ops != nil || len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "birth.nql" || ds[0].Line != tc.line || ds[0].Column != tc.column {
				t.Fatalf("want %s at %d:%d: %+v", tc.code, tc.line, tc.column, ds)
			}
		})
	}
	for _, text := range []string{strings.TrimSuffix(createNPCSource, ";"), `create item "x";`, strings.Replace(createNPCSource, "stat", "while", 1)} {
		if _, ds := CompileActions("invalid", text, creationCatalog()); len(ds) == 0 || ds[0].Code != "E_PARSE" {
			t.Fatal(ds)
		}
	}
	if _, ds := CompileActions("continuation", createNPCSource, creationCatalog()); len(ds) != 0 {
		t.Fatal(ds)
	}
}

func TestCreationNotAnAbilityOrForwardWorldMutation(t *testing.T) {
	world := `rules { transfer unequips; wear any; } location "hall" named "Hall"; character "actor" named "Mira" at "hall" {}`
	if _, ds := CompileWorld("not-world", world+createNPCSource); len(ds) == 0 {
		t.Fatal("creation accepted as world declaration")
	}
	if _, ds := CompileWorld("not-ability", world+` ability "birth" { `+createNPCSource+` }`); len(ds) == 0 {
		t.Fatal("creation accepted in Ability")
	}
}

func FuzzCharacterActions(f *testing.F) {
	for _, s := range []string{createNPCSource, `create character "\uD800" named "X" at "hall" {};`, strings.Replace(createNPCSource, "10", "-9223372036854775808", 1), "create character \xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxSourceBytes+1 {
			return
		}
		ops, ds := CompileActions("fuzz-creation", s, creationCatalog())
		if len(ds) > 0 {
			for _, d := range ds {
				if d.Line < 1 || d.Column < 1 {
					t.Fatal(d)
				}
			}
			return
		}
		w, e := core.NewWorld(creationCatalog())
		if e != nil {
			t.Fatal(e)
		}
		before := w.ExportCheckpoint()
		_, e = w.Execute(core.Request{ID: "fuzz", Actor: core.Ref{Kind: core.CharacterKind, ID: "actor"}, Operations: ops})
		if e != nil {
			if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("failed source partially published")
			}
			return
		}
		rebuilt, e := core.Replay(before.Genesis, w.History())
		if e != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
			t.Fatal("successful source replay", e)
		}
	})
}
