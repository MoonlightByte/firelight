package compiler_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

// gateSource is battleSource with body and shield slots, a shield for Tess,
// and the grammar guide's gated Monk attack.
func gateSource(t *testing.T) string {
	t.Helper()
	guide, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Gated attacks and die upgrades\n\n```nql\n")
	if !ok {
		t.Fatal("the guide has no gated attack example")
	}
	example, _, _ := strings.Cut(after, "```")
	s := battleSource(t)
	for _, r := range [][2]string{
		{` slot "hand" capacity 2;
`, ` slot "hand" capacity 2;
 slot "body" capacity 1;
 slot "shield" capacity 1;
 definition "shield" named "Shield" {
  description "A shield.";
  default "held";
  mode "held" { occupy "hand" by 1; occupy "shield" by 1; }
 }
`},
		{`item "tess-sword"`, `item "tess-shield" named "Shield" { owner "tess"; definition "shield"; }
item "tess-sword"`},
	} {
		if !strings.Contains(s, r[0]) {
			t.Fatal("no", r[0])
		}
		s = strings.Replace(s, r[0], r[1], 1)
	}
	return s + example
}

// The guide's gated attack lowers, lists its d8 tier for Tess, rolls it in
// Battle and survives a save; with her shield on, it is not listed.
func TestAttackGateGuideExample(t *testing.T) {
	s := strings.Replace(gateSource(t), `stat "initiative" = 2;`, `stat "initiative" = 30;`, 1)
	b, ds := compiler.CompileWorld("gate.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var attack core.EquipmentAttack
	for _, d := range b.ConditionDefinitions {
		if d.ID == "monk:unarmed" {
			attack = d.Attacks[0]
		}
	}
	unarmored := []core.SlotID{"body", "shield"}
	want := core.AttackGate{Requires: "feature:martial-arts", UnlessSlots: unarmored}
	upgrades := []core.DamageUpgrade{{Count: 1, Sides: 8, AttackGate: core.AttackGate{Requires: "feature:martial-arts-d8", UnlessSlots: unarmored}}, {Count: 1, Sides: 10, AttackGate: core.AttackGate{Requires: "feature:martial-arts-d10", UnlessSlots: unarmored}}}
	if !reflect.DeepEqual(attack.AttackGate, want) || !reflect.DeepEqual(attack.Damage.Upgrades, upgrades) || attack.Damage.Count != 1 || attack.Damage.Sides != 6 {
		t.Fatalf("%+v", attack)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.EquipmentView("tess")
	if err != nil {
		t.Fatal(err)
	}
	var grant core.AttackGrant
	for _, g := range view.Attacks {
		if g.Origin.Member == "unarmed-dex" {
			grant = g
		}
	}
	// Dexterity 14 and proficiency 2: +4 to hit, 1d8+2 at the d8 tier.
	if grant.ToHit != 4 || grant.DamageCount != 1 || grant.DamageSides != 8 || grant.DamageBonus != 2 {
		t.Fatalf("%+v", view.Attacks)
	}
	tess := core.Ref{Kind: core.CharacterKind, ID: "tess"}
	run := func(w *core.World, id, source string) (core.Batch, error) {
		ops, ds := compiler.CompileActions(id, source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		return w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: tess, Operations: ops})
	}
	begin, err := os.ReadFile("../examples/native_melee/begin.nql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(w, "begin", string(begin)); err != nil {
		t.Fatal(err)
	}
	r, err := run(w, "strike", fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true { attack 15; damage 8; };`, grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if roll := r.Events[len(r.Events)-1].WeaponAttack.Roll; !roll.Hit || !reflect.DeepEqual(roll.Damage, []int64{8}) {
		t.Fatalf("%+v", roll)
	}
	var buffer bytes.Buffer
	if _, err := nativesave.Write(&buffer, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "gate", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&buffer)
	if err != nil || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("round trip", err)
	}
	// With the shield on, the attack is shut: not listed, and refused.
	shielded, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(shielded, "shield", `equip "tess-shield" on "tess";`); err != nil {
		t.Fatal(err)
	}
	view, _ = shielded.EquipmentView("tess")
	for _, g := range view.Attacks {
		if g.ID == grant.ID {
			t.Fatal("a shut attack is listed")
		}
	}
	if _, err := run(shielded, "begin", string(begin)); err != nil {
		t.Fatal(err)
	}
	_, err = run(shielded, "strike", fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true { attack 15; damage 8; };`, grant.ID))
	var f *core.Fault
	if !errors.As(err, &f) || f.Field != "grant" {
		t.Fatal(err)
	}
}

func TestAttackGateCompilerPositions(t *testing.T) {
	s := gateSource(t)
	const head = `requires "feature:martial-arts" unless slot "body" unless slot "shield" {`
	const d8 = `or dice 1 sides 8 requires "feature:martial-arts-d8" unless slot "body" unless slot "shield"`
	nine := strings.Repeat(" or dice 1 sides 4 requires \"feature:martial-arts\"", 8)
	slots := ""
	for i := range 9 {
		slots += fmt.Sprintf(` unless slot "s%d"`, i)
	}
	for _, tc := range []struct {
		name, from, to, at, code string
		declare                  bool
	}{
		{"a mode attack's undeclared type", `attack "slash" named "Longsword slash" {`, `attack "slash" named "Longsword slash" requires "missing" {`, `"missing"`, "E_EQUIPMENT", false},
		{"an undeclared type", head, `requires "missing" unless slot "body" unless slot "shield" {`, `"missing"`, "E_EQUIPMENT", false},
		{"an undeclared slot", head, `requires "feature:martial-arts" unless slot "back" {`, `"back"`, "E_EQUIPMENT", false},
		{"a slot twice", head, `requires "feature:martial-arts" unless slot "body" unless slot "body" {`, `"body" {`, "E_EQUIPMENT", false},
		{"nine slots", head, `requires "feature:martial-arts"` + slots + ` {`, `"s8"`, "E_EQUIPMENT", true},
		{"an upgrade's undeclared type", d8, `or dice 1 sides 8 requires "missing"`, `"missing"`, "E_EQUIPMENT", false},
		{"an upgrade's undeclared slot", d8, `or dice 1 sides 8 requires "feature:martial-arts-d8" unless slot "back"`, `"back"`, "E_EQUIPMENT", false},
		{"an upgrade with no type", d8, `or dice 1 sides 8 unless slot "body"`, `unless`, "E_PARSE", false},
		{"no dice", d8, `or dice 0 sides 8 requires "feature:martial-arts-d8"`, `0`, "E_EQUIPMENT", false},
		{"one side", d8, `or dice 1 sides 1 requires "feature:martial-arts-d8"`, `1 requires`, "E_EQUIPMENT", false},
		{"nine upgrades", d8, d8 + nine, `or dice 1 sides 4 requires "feature:martial-arts"`, "E_EQUIPMENT", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := s
			if tc.declare {
				decl := ""
				for i := range 9 {
					decl += fmt.Sprintf(" slot \"s%d\" capacity 1;\n", i)
				}
				base = strings.Replace(base, ` slot "body" capacity 1;
`, ` slot "body" capacity 1;
`+decl, 1)
			}
			bad := strings.Replace(base, tc.from, tc.to, 1)
			if bad == base {
				t.Fatal("no change")
			}
			at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
			_, ds := compiler.CompileWorld("bad-gate", bad)
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
				t.Fatalf("%+v", ds)
			}
		})
	}
}
