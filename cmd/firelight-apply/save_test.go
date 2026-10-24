package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// The save guide's actions are the example files, and its results are the
// engine's: one save, a list of two, and a save scored first.
func TestSaveGuideExamples(t *testing.T) {
	guide := readFile(t, "../../docs/SAVES.md")
	var shown []any
	for _, block := range strings.Split(guide, "```json\n")[1:] {
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(block, "```", 2)[0]), &v); err != nil {
			t.Fatal(err)
		}
		shown = append(shown, v)
	}
	if len(shown) != 3 {
		t.Fatalf("%d examples in the guide", len(shown))
	}
	world := readFile(t, "../../examples/saves/world.nql")
	ada := core.Ref{Kind: core.CharacterKind, ID: "char:ada"}
	for i, file := range []string{"sacred-flame.nql", "burning-hands.nql", "save-first.nql"} {
		actions := actionLines(t, "../../examples/saves/"+file)
		if !strings.Contains(guide, "```nql\n"+actions+"\n```") {
			t.Fatalf("the guide does not show %s", file)
		}
		r := apply(input{World: world, Actions: actions, Actor: &ada, Request: "save:1"})
		if !r.OK || len(r.Saves) != strings.Count(actions, "\n")+1 {
			t.Fatal(file, r.Error, r.Fault, r.Diagnostics)
		}
		var got any = asJSON(t, r.Saves[0])
		if len(r.Saves) > 1 {
			got = asJSON(t, r.Saves)
		}
		if !reflect.DeepEqual(got, shown[i]) {
			t.Fatalf("%s:\nengine %v\nguide  %v", file, got, shown[i])
		}
	}
}
