package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestExplainGolden pins every stat explanation, Character status and
// equipment view of every Character after each request, on worlds with
// latest selection, held and included types and requires gates. b43e75b,
// which scanned the world for every explanation, wrote the golden files; the
// derivations that replaced the scans must give the same answers.
// NQL_EXPLAIN_GOLDEN=1 rewrites them from the build under test.
func TestExplainGolden(t *testing.T) {
	for _, tc := range []struct {
		name, world string
		actor       core.CharacterID
		steps       []string // each a request after the previous one
		scenarios   []string // each a request on the genesis world
	}{
		{name: "selection", world: "testdata/explain/selection.nql", actor: "char:a", steps: []string{
			`apply condition "a:boon:3" of "boon" to "char:a" magnitude 7;`,
			`apply condition "a:boon:4" of "boon" to "char:a" magnitude 1; apply condition "a:boon:5" of "boon" to "char:a" magnitude 4; apply condition "a:hex:3" of "hex" to "char:a";`,
			`apply condition "a:stun" of "stunned" to "char:a";`,
			`remove condition "a:boon:5" from "char:a"; remove condition "a:hex:3" from "char:a";`,
			`damage "char:c" resource "hp" by 99;`,
			`unequip "item:buckler:a"; equip "item:buckler:b" on "char:b";`,
			`apply effect "fx:late" from character "char:b" on character "char:a" stat "defense" add 4; apply condition "b:boon:2" of "boon" to "char:b" magnitude 9;`,
			`heal "char:b" resource "hp" by 5; remove condition "a:stun" from "char:a";`,
		}},
		{name: "srd-stats", world: "../examples/srd-stats/world.nql", actor: "char:fighter", scenarios: []string{
			"../examples/srd-stats/conditions.nql",
			"../examples/srd-stats/concentration.nql",
			"../examples/srd-stats/checks.nql",
		}},
		{name: "condition-world", world: "../examples/condition-world.nql", actor: "char:ada", scenarios: []string{
			"../examples/condition-journey.nql",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.world)
			if err != nil {
				t.Fatal(err)
			}
			b, ds := compiler.CompileWorld(filepath.Base(tc.world), string(src))
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			var log bytes.Buffer
			w := explainWorld(t, b)
			pinExplanations(t, &log, "genesis", w)
			for i, step := range tc.steps {
				explainRequest(t, w, fmt.Sprintf("step:%d", i+1), tc.actor, step)
				pinExplanations(t, &log, fmt.Sprintf("step %d", i+1), w)
			}
			for _, path := range tc.scenarios {
				actions, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				w := explainWorld(t, b)
				explainRequest(t, w, filepath.Base(path), tc.actor, string(actions))
				pinExplanations(t, &log, filepath.Base(path), w)
			}
			golden := filepath.Join("testdata", "explain", tc.name+".jsonl")
			if os.Getenv("NQL_EXPLAIN_GOLDEN") != "" {
				if err := os.WriteFile(golden, log.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			got := bytes.Split(log.Bytes(), []byte("\n"))
			lines := bytes.Split(want, []byte("\n"))
			for i := 0; i < len(got) || i < len(lines); i++ {
				if i >= len(got) || i >= len(lines) || !bytes.Equal(got[i], lines[i]) {
					var g, l []byte
					if i < len(got) {
						g = got[i]
					}
					if i < len(lines) {
						l = lines[i]
					}
					t.Fatalf("%s line %d:\n got %s\nwant %s", golden, i+1, g, l)
				}
			}
		})
	}
}

func explainWorld(t *testing.T, b core.Blueprint) *core.World {
	t.Helper()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func explainRequest(t *testing.T, w *core.World, id string, actor core.CharacterID, actions string) {
	t.Helper()
	ops, ds := compiler.CompileActions(id, actions, w.Snapshot().Blueprint)
	if len(ds) > 0 {
		t.Fatal(id, ds)
	}
	if _, err := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: string(actor)}, Operations: ops}); err != nil {
		t.Fatal(id, err)
	}
}

// pinExplanations writes one JSON line per view: every stat of every
// Character, then its status, less the explanations it repeats, its
// equipment view, and each item's transfer view in a world without
// equipment rules.
func pinExplanations(t *testing.T, log *bytes.Buffer, at string, w *core.World) {
	t.Helper()
	line := func(v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		log.Write(raw)
		log.WriteByte('\n')
	}
	failure := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	s := w.Snapshot()
	for _, c := range s.Characters {
		stats := make([]string, 0, len(c.Stats))
		for stat := range c.Stats {
			stats = append(stats, string(stat))
		}
		sort.Strings(stats)
		explained := []core.StatExplanation{}
		for _, stat := range stats {
			x, err := w.Explain(c.ID, core.StatID(stat))
			line(map[string]any{"at": at, "character": c.ID, "stat": stat, "explain": x, "error": failure(err)})
			explained = append(explained, x)
		}
		// A status explains every stat at once; it must match the lines above.
		status, err := w.CharacterStatus(c.ID)
		if err == nil {
			a, _ := json.Marshal(status.Stats)
			b, _ := json.Marshal(explained)
			if !bytes.Equal(a, b) {
				t.Fatalf("%s %s: status explains\n%s\nper stat\n%s", at, c.ID, a, b)
			}
		}
		status.Stats = nil
		line(map[string]any{"at": at, "character": c.ID, "status": status, "error": failure(err)})
		equipment, err := w.EquipmentView(c.ID)
		line(map[string]any{"at": at, "character": c.ID, "equipment": equipment, "error": failure(err)})
		if s.Equipment == nil {
			for _, i := range s.Items {
				transfer, err := w.TransferView(i.ID, c.ID)
				line(map[string]any{"at": at, "item": i.ID, "to": c.ID, "transfer": transfer, "error": failure(err)})
			}
		}
	}
}
