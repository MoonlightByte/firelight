package compiler_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// guideUnarmed is the grammar guide's Condition attack example.
func guideUnarmed(t *testing.T) string {
	t.Helper()
	guide, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Attacks from Condition types\n\n```nql\n")
	if !ok {
		t.Fatal("the guide has no Condition attack example")
	}
	block, _, _ := strings.Cut(after, "```")
	return block
}

// The guide's Unarmed Strike lowers into its type, grants Tess an attack, and
// a Battle strike takes it.
func TestConditionAttackGuideExample(t *testing.T) {
	s := battleSource(t) + guideUnarmed(t)
	// Tess acts first.
	s = strings.Replace(s, `stat "initiative" = 2;`, `stat "initiative" = 30;`, 1)
	b, ds := compiler.CompileWorld("unarmed.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var def *core.ConditionDefinition
	for i := range b.ConditionDefinitions {
		if b.ConditionDefinitions[i].ID == "srd:unarmed-strike" {
			def = &b.ConditionDefinitions[i]
		}
	}
	if def == nil || len(def.Attacks) != 1 || def.Attacks[0].ID != "unarmed" || def.Attacks[0].Melee.Reach != 5 || def.Attacks[0].Damage.Count != 0 || def.Attacks[0].Damage.Bonus.Base != 1 {
		t.Fatalf("%+v", def)
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
		if g.Origin.Condition == "srd:unarmed-strike" {
			grant = g
		}
	}
	// Strength 16 and proficiency 2: +5 to hit, 1 + 3 = 4 bludgeoning.
	if grant.ToHit != 5 || grant.DamageBonus != 4 || grant.Origin.Member != "unarmed" {
		t.Fatalf("%+v", view.Attacks)
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
	r := run("strike", fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true { attack 15; };`, grant.ID))
	if e := r.Events[len(r.Events)-1].WeaponAttack; e.Grant.ID != grant.ID || !e.Roll.Hit || e.Roll.Requested != 4 {
		t.Fatalf("%+v", e)
	}
}

func TestConditionAttackCompilerPositions(t *testing.T) {
	example := guideUnarmed(t)
	attack := `
 attack "kick" named "Kick" {
  hit { constant 0; }
  damage "bludgeoning" flat { constant 1; }
  melee reach 5 stat "strength";
 }
`
	nine := ""
	for i := range 9 {
		nine += strings.Replace(attack, `"kick"`, fmt.Sprintf(`"kick-%d"`, i), 1)
	}
	world := func(t *testing.T) string { return battleSource(t) }
	for _, tc := range []struct {
		name, from, to, token, code string
		base                        func(*testing.T) string
	}{
		{"a duplicate attack", ` melee reach 5 stat "strength";
 }
}`, ` melee reach 5 stat "strength";
 }` + strings.Replace(attack, `"kick"`, `"unarmed"`, 1) + `}`, `"unarmed" named "Kick"`, "E_DUPLICATE", world},
		{"nine attacks", ` instances unique;
`, ` instances unique;
` + nine, `"kick-8"`, "E_EQUIPMENT", world},
		{"bad dice", `damage "bludgeoning" flat {`, `damage "bludgeoning" dice 0 sides 4 {`, `0 sides`, "E_EQUIPMENT", world},
		{"an undeclared requirement", `offset 0 divide 1; }`, `offset 0 divide 1 requires "missing"; }`, `"missing"`, "E_EQUIPMENT", world},
		{"a derived input", `term stat "proficiency"`, `term stat "defense"`, `"defense"`, "E_EQUIPMENT", world},
		{"no equipment", "", "", `"unarmed" named`, "E_EQUIPMENT", func(t *testing.T) string {
			text, err := os.ReadFile("../examples/attacks/world.nql")
			if err != nil {
				t.Fatal(err)
			}
			return strings.Replace(string(text), `character "char:ada"`, `character "tess" named "Tess" at "loc:valley/B03" { stat "strength" = 10; }
character "char:ada"`, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(example, tc.from, tc.to, 1)
			if changed == example && tc.from != "" {
				t.Fatal("no change")
			}
			src := tc.base(t) + changed
			at := len(tc.base(t)) + strings.Index(changed, tc.token)
			_, ds := compiler.CompileWorld("bad.nql", src)
			if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(src[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(src[:at], "\n") {
				t.Fatalf("%+v", ds)
			}
		})
	}
}
