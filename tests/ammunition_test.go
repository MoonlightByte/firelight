package tests

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

// SRD 5.2.1 ammunition from examples/ammunition (docs/AMMUNITION.md). Every
// expected number is worked by hand from the SRD rule: half the ammunition a
// fight used comes back, rounded down.

// rebuiltArrows is the arrows after fight.nql as a host that rebuilds the
// world per call declares them.
const rebuiltArrows = `item "item:arrows" named "Arrows" { owner "char:ranger"; custody character "char:ranger"; quantity 14; recoverable 6; }`

func ammunitionFile(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("../examples/ammunition/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ammunitionWorld(t testing.TB, source string) (core.Blueprint, *core.World) {
	t.Helper()
	b, ds := compiler.CompileWorld("world.nql", source)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return b, w
}

func ammunitionExecute(t testing.TB, w *core.World, id, name string) core.Batch {
	t.Helper()
	s := w.Snapshot()
	ops, ds := compiler.CompileActions(name, ammunitionFile(t, name), s.Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	b, err := w.Execute(core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ranger"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wantStock reads a stock the way a host does, through items_at.
func wantStock(t testing.TB, w *core.World, owner string, id core.ItemID, quantity, recoverable int64) {
	t.Helper()
	p, err := w.ItemsAt(core.Ref{Kind: core.CharacterKind, ID: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range p.Items {
		if i.ID == id {
			if i.Quantity == nil || *i.Quantity != quantity || i.Recoverable != recoverable {
				t.Fatalf("%s = %+v, want quantity %d recoverable %d", id, i, quantity, recoverable)
			}
			return
		}
	}
	t.Fatalf("%s not at %s", id, owner)
}

func TestAmmunitionFightAndRecovery(t *testing.T) {
	b, w := ammunitionWorld(t, ammunitionFile(t, "world.nql"))
	if b.Rules.RecoveryNumerator != 1 || b.Rules.RecoveryDenominator != 2 {
		t.Fatalf("%+v", b.Rules)
	}
	wantStock(t, w, "char:ranger", "item:arrows", 20, 0)
	fight := ammunitionExecute(t, w, "fight", "fight.nql")
	if c := fight.Events[2].Consumption; c.Recoverable || c.After != 16 {
		t.Fatalf("%+v", c)
	}
	// 20 - 6 arrows; 20 - 3 - 1 bolts, of which the broken one counts nothing.
	wantStock(t, w, "char:ranger", "item:arrows", 14, 6)
	wantStock(t, w, "char:fighter", "item:bolts", 16, 3)
	recovered := ammunitionExecute(t, w, "recover", "recover.nql")
	fourteen, sixteen, zero := int64(14), int64(16), int64(0)
	want := []core.ItemRecovery{
		{Item: "item:arrows", Owner: "char:ranger", Spent: 6, Allowed: 3, Before: &fourteen, After: 17},
		{Item: "item:bolts", Owner: "char:fighter", Requested: &zero, Spent: 3, Allowed: 1, Before: &sixteen, After: 16},
	}
	for i, e := range recovered.Events {
		if e.Kind != core.ItemRecovered || !reflect.DeepEqual(*e.ItemRecovery, want[i]) {
			t.Fatalf("%d: %+v", i, e.ItemRecovery)
		}
	}
	// Half of 6 comes back; the forfeited bolts stay spent. Both counts close.
	wantStock(t, w, "char:ranger", "item:arrows", 17, 0)
	wantStock(t, w, "char:fighter", "item:bolts", 16, 0)
	costReplay(t, b, w)
}

// A host that rebuilds the world per call declares the pending counts and gets
// the same recovery as one that kept the checkpoint.
func TestAmmunitionRebuiltPerCall(t *testing.T) {
	world := ammunitionFile(t, "world.nql")
	for _, edit := range [][2]string{
		{`item "item:arrows" named "Arrows" { owner "char:ranger"; custody character "char:ranger"; quantity 20; }`, rebuiltArrows},
		{`quantity 20; }`, `quantity 16; recoverable 3; }`},
	} {
		if strings.Count(world, edit[0]) != 1 {
			t.Fatal(edit[0])
		}
		world = strings.Replace(world, edit[0], edit[1], 1)
	}
	_, w := ammunitionWorld(t, world)
	wantStock(t, w, "char:ranger", "item:arrows", 14, 6)
	wantStock(t, w, "char:fighter", "item:bolts", 16, 3)
	batch := ammunitionExecute(t, w, "recover", "recover.nql")
	if v := batch.Events[0].ItemRecovery; v.Allowed != 3 || v.After != 17 {
		t.Fatalf("%+v", v)
	}
	wantStock(t, w, "char:ranger", "item:arrows", 17, 0)
	wantStock(t, w, "char:fighter", "item:bolts", 16, 0)
}

func TestAmmunitionGuideMatchesExamples(t *testing.T) {
	guide, err := os.ReadFile("../docs/AMMUNITION.md")
	if err != nil {
		t.Fatal(err)
	}
	examples := ammunitionFile(t, "world.nql") + ammunitionFile(t, "fight.nql") + ammunitionFile(t, "recover.nql") + rebuiltArrows + "\n"
	lines := map[string]bool{}
	for _, l := range strings.Split(examples, "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	blocks := strings.Split(string(guide), "```")
	shown := 0
	for i := 1; i < len(blocks); i += 2 {
		body := strings.TrimLeft(blocks[i], " ")
		if !strings.HasPrefix(body, "nql\n") {
			continue
		}
		for _, l := range strings.Split(strings.TrimPrefix(body, "nql\n"), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if !lines[l] {
				t.Errorf("guide line not in examples/ammunition: %s", l)
			}
			shown++
		}
	}
	if shown == 0 {
		t.Fatal("guide shows no nql")
	}
}
