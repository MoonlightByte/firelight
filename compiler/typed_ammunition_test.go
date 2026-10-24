package compiler_test

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

// guideTyped is the grammar guide's typed ammunition clause and declarations.
func guideTyped(t *testing.T) (clause, declarations string) {
	t.Helper()
	guide, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Ammunition named by item type\n\n")
	if !ok {
		t.Fatal("the guide has no typed ammunition section")
	}
	var blocks []string
	for range 2 {
		_, after, _ = strings.Cut(after, "```nql\n")
		var block string
		block, after, _ = strings.Cut(after, "```")
		blocks = append(blocks, block)
	}
	return strings.TrimSpace(blocks[0]), blocks[1]
}

// typedSource is rangedSource with the guide's typed bow and Tess's arrows.
func typedSource(t *testing.T) string {
	clause, declarations := guideTyped(t)
	s := rangedSource(t)
	for _, r := range [][2]string{
		{`ranged normal 80 maximum 320 stat "dexterity" ammunition "arrows";`, clause},
		{`item "arrows" named "Arrows" { quantity 5; owner "tess"; custody character "tess"; }
`, declarations},
	} {
		if !strings.Contains(s, r[0]) {
			t.Fatal("no", r[0])
		}
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	return s
}

// The guide's typed bow lowers to an item type, and a Battle shot takes
// Tess's arrows and records them under their own stock.
func TestTypedAmmunitionGuideExample(t *testing.T) {
	s := typedSource(t)
	// Tess acts first.
	s = strings.Replace(s, `stat "initiative" = 2;`, `stat "initiative" = 30;`, 1)
	b, ds := compiler.CompileWorld("typed.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := &core.RangedProfile{Stat: "dexterity", Normal: 150, Maximum: 600, AmmunitionType: "srd:arrow"}
	if got := b.Equipment.Definitions[0].Modes[0].Attacks[0].Ranged; !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.EquipmentView("tess")
	if err != nil || len(view.Attacks) != 1 {
		t.Fatal(view, err)
	}
	for _, i := range view.Items {
		if i.Item.ID == "tess:arrows" && i.AmmunitionOrigin != "tess:arrows" {
			t.Fatalf("%+v", i)
		}
	}
	tess := core.Ref{Kind: core.CharacterKind, ID: "tess"}
	run := func(id, source string) core.Batch {
		ops, ds := compiler.CompileActions(id, source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		r, err := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: tess, Operations: ops})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	begin, err := os.ReadFile("../examples/native_melee/begin.nql")
	if err != nil {
		t.Fatal(err)
	}
	run("begin", string(begin))
	// The raider stands next to Tess: two faces at disadvantage, a miss.
	r := run("shot", fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true ammunition "tess:arrows" { attack 15; attack 2; };`, view.Attacks[0].ID))
	if r.Events[0].Kind != core.ItemConsumed || r.Events[0].Consumption.Item != "tess:arrows" || r.Events[len(r.Events)-1].WeaponAttack.Roll.Hit {
		t.Fatalf("%+v", r.Events)
	}
	e := w.Snapshot().Battle.State.Encounters[0]
	if !reflect.DeepEqual(e.Ammunition, []core.ActorAmmunition{{Actor: "tess", Stocks: []core.AmmunitionStock{{Origin: "tess:arrows", Spent: 1}}}}) {
		t.Fatalf("%+v", e.Ammunition)
	}
	// A save keeps the typed profile and the shot.
	var buffer bytes.Buffer
	info, err := nativesave.Write(&buffer, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "typed", Next: 1})
	if err != nil || info.Semantics != nativesave.ItemCatalogSemanticsVersion {
		t.Fatal(info, err)
	}
	loaded, err := nativesave.Read(&buffer)
	if err != nil || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("round trip", err)
	}
	if again, err := loaded.World.Execute(r.Request); err != nil || !reflect.DeepEqual(again, r) {
		t.Fatal("archive lost the shot", err)
	}
}

func TestTypedAmmunitionCompilerPositions(t *testing.T) {
	s := typedSource(t)
	for _, tc := range []struct{ from, to, at, code string }{
		{`ammunition from "srd:arrow"`, `ammunition from "missing"`, `"missing"`, "E_EQUIPMENT"},
		{`ammunition from "srd:arrow"`, `ammunition from "tess:arrows"`, `"tess:arrows"`, "E_EQUIPMENT"},
		{`ammunition from "srd:arrow"`, `ammunition from "blade"`, `"blade"`, "E_EQUIPMENT"},
		{`ammunition from "srd:arrow"`, `ammunition "srd:arrow"`, `"srd:arrow"`, "E_REFERENCE"},
		{`ammunition from "srd:arrow"`, `ammunition from;`, `;`, "E_PARSE"},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1) + `item type "blade" named "Blade" { kind "weapon"; equipment "longsword"; }
`
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		_, ds := compiler.CompileWorld("bad-typed", bad)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatalf("%+v: %+v", tc, ds)
		}
	}
}
