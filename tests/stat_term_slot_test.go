package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// Slot-gated recipe terms (docs/GRAMMAR.md "Slot-gated recipe terms"): SRD
// 5.2.1 Unarmored Defense. Every expected number is worked by hand.

const slotTermPrelude = "rules { transfer unequips; wear any; }\nlocation \"yard\" named \"Yard\";\n"

// slotTermExtra adds body armor, a grammar-case armor whose own recipe gates
// Dexterity on the shield slot, a held stick, and a Barbarian with a shield.
const slotTermExtra = ` definition "leather" named "Leather Armor" {
  description "Body armor.";
  default "worn";
  mode "worn" { occupy "body" by 1; base stat "defense" { constant 11; term stat "dexterity" offset -10 divide 2; } }
 }
 definition "test-leather" named "Test Leather" {
  description "A grammar case.";
  default "worn";
  mode "worn" { occupy "body" by 1; base stat "defense" { constant 11; term stat "dexterity" offset -10 divide 2 unless slot "shield"; } }
 }
 definition "stick" named "Stick" { description "Held."; default "held"; mode "held" { occupy "hand" by 1; } }
 definition "shield"`

const slotTermBo = `character "bo" named "Bo" at "yard" { stat "dexterity" = 16; stat "wisdom" = 10; stat "constitution" = 14; stat "defense" = 10; }
condition "bo:unarmored" of "feature:unarmored-defense:barbarian" to "bo";
condition "bo:shield" of "training:shield" to "bo";
item "bo-shield" named "Shield" { owner "bo"; wearer "bo"; definition "shield"; mode "held"; }
`

const tessShield = `item "tess-shield" named "Shield" { owner "tess"; wearer "tess"; definition "shield"; mode "held"; }`

func slotTermGuide(t *testing.T) string {
	t.Helper()
	guide, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Slot-gated recipe terms\n\n```nql\n")
	if !ok {
		t.Fatal("the guide has no slot-gated term example")
	}
	example, _, _ := strings.Cut(after, "```")
	return example
}

// slotTermSource is the guide's example with the extra definitions and Bo,
// then each replacement in order.
func slotTermSource(t *testing.T, edits ...[2]string) string {
	t.Helper()
	s := slotTermPrelude + slotTermGuide(t) + slotTermBo
	for _, r := range append([][2]string{{` definition "shield"`, slotTermExtra}}, edits...) {
		if !strings.Contains(s, r[0]) {
			t.Fatal("no", r[0])
		}
		s = strings.Replace(s, r[0], r[1], 1)
	}
	return s
}

func slotTermWorld(t *testing.T, edits ...[2]string) *core.World {
	t.Helper()
	b, ds := compiler.CompileWorld("slot-term.nql", slotTermSource(t, edits...))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return world(t, b)
}

func wear(id, def, mode string) string {
	return fmt.Sprintf(`item "tess-%s" named "%s" { owner "tess"; wearer "tess"; definition "%s"; mode "%s"; }`, def, def, def, mode)
}

// Tess: Dex 16, Wis 16, a Monk trained with shields. Bo: Dex 16, Con 14, a
// Barbarian with a shield.
func TestSlotTermUnarmoredDefense(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits [][2]string
		id    core.CharacterID
		want  int64
	}{
		{"monk unarmored: 10 + 3 + 3", [][2]string{{tessShield, ""}}, "tess", 16},
		{"monk with a shield: 10 + 3 + 2", nil, "tess", 15},
		{"monk with an untrained shield: 10 + 3", [][2]string{{`condition "tess:shield" of "training:shield" to "tess";`, ""}}, "tess", 13},
		{"monk in leather: 11 + 3", [][2]string{{tessShield, wear("leather", "leather", "worn")}}, "tess", 14},
		{"monk in leather with a shield: 11 + 3 + 2", [][2]string{{tessShield, tessShield + "\n" + wear("leather", "leather", "worn")}}, "tess", 16},
		{"monk holding a stick: 10 + 3 + 3", [][2]string{{tessShield, wear("stick", "stick", "held")}}, "tess", 16},
		{"barbarian with a shield: 10 + 3 + 2 + 2", nil, "bo", 17},
		{"item recipe gate, no shield: 11 + 3", [][2]string{{tessShield, wear("test-leather", "test-leather", "worn")}}, "tess", 14},
		{"item recipe gate, with a shield: 11 + 2", [][2]string{{tessShield, tessShield + "\n" + wear("test-leather", "test-leather", "worn")}}, "tess", 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stat(t, slotTermWorld(t, tc.edits...), tc.id, "defense", tc.want)
		})
	}
}

// The explanation reports the Wisdom term inactive while the shield is worn,
// and a returned explanation is detached from the world.
func TestSlotTermExplanation(t *testing.T) {
	bare := slotTermWorld(t, [2]string{tessShield, ""})
	e, err := bare.Explain("tess", "defense")
	if err != nil || e.Recipe == nil || !e.Recipe.Terms[1].Active {
		t.Fatal("unarmored Wisdom term", e, err)
	}
	w := slotTermWorld(t)
	kept := slotTermWorld(t).Snapshot()
	e, err = w.Explain("tess", "defense")
	if err != nil || e.Recipe == nil {
		t.Fatal(e, err)
	}
	term := e.Recipe.Terms[1]
	if term.Active || term.Exact != "0" || !reflect.DeepEqual(term.Term.UnlessSlots, []core.SlotID{"body", "shield"}) {
		t.Fatal("shielded Wisdom term", term)
	}
	term.Term.UnlessSlots[0] = "hand"
	again, err := w.Explain("tess", "defense")
	if err != nil || again.Recipe.Terms[1].Term.UnlessSlots[0] != "body" || again.Effective != 15 || !reflect.DeepEqual(w.Snapshot(), kept) {
		t.Fatal("an explanation changed the world", again, err)
	}
}

// A term without a slot serializes as before: no unless_slots key.
func TestSlotTermOldShape(t *testing.T) {
	b, ds := compiler.CompileWorld("world.nql", srdStatsFile(t, "world.nql"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	raw, err := json.Marshal(b)
	if err != nil || strings.Contains(string(raw), "unless_slots") {
		t.Fatal("a world without slot gates gained unless_slots", err)
	}
	g, ds := compiler.CompileWorld("slot-term.nql", slotTermSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	raw, err = json.Marshal(g)
	if err != nil || !strings.Contains(string(raw), `"unless_slots":["body","shield"]`) {
		t.Fatal("the gated term lost its slots", err)
	}
}

func TestSlotTermValidation(t *testing.T) {
	monk := `requires "feature:unarmored-defense:monk" unless slot "body" unless slot "shield";`
	nine := ` slot "s1" capacity 1;
 slot "s2" capacity 1;
 slot "s3" capacity 1;
 slot "s4" capacity 1;
 slot "s5" capacity 1;
 slot "s6" capacity 1;
 slot "shield" capacity 1;`
	for _, tc := range []struct{ to, token, code string }{
		{`requires "feature:unarmored-defense:monk" unless slot "body" unless slot "cloak";`, `"cloak"`, "E_EQUIPMENT"},
		{`requires "feature:unarmored-defense:monk" unless slot "body" unless slot "body";`, `"body";`, "E_EQUIPMENT"},
		{`requires "feature:unarmored-defense:monk" unless slot "hand" unless slot "body" unless slot "shield" unless slot "s1" unless slot "s2" unless slot "s3" unless slot "s4" unless slot "s5" unless slot "s6";`, `"s6"`, "E_EQUIPMENT"},
		{`requires "feature:unarmored-defense:monk" unless slot;`, `;`, "E_PARSE"},
		{`requires "feature:unarmored-defense:monk" unless slot "body" unless "training:shield";`, `"training:shield"`, "E_PARSE"},
	} {
		s := strings.Replace(slotTermSource(t), monk, tc.to, 1)
		s = strings.Replace(s, ` slot "shield" capacity 1;`, nine, 1)
		_, ds := compiler.CompileWorld("bad.nql", s)
		at := strings.Index(s, tc.to) + strings.LastIndex(tc.to, tc.token)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc.to, ds)
		}
	}

	// Eight unique declared slots are the bound, accepted.
	eight := `requires "feature:unarmored-defense:monk" unless slot "hand" unless slot "body" unless slot "shield" unless slot "s1" unless slot "s2" unless slot "s3" unless slot "s4" unless slot "s5";`
	s := strings.Replace(strings.Replace(slotTermSource(t), monk, eight, 1), ` slot "shield" capacity 1;`, nine, 1)
	if _, ds := compiler.CompileWorld("eight.nql", s); len(ds) != 0 {
		t.Fatal("eight slots", ds)
	}

	// A Blueprint that skips the compiler meets the same rules.
	b, ds := compiler.CompileWorld("slot-term.nql", slotTermSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		slots  []core.SlotID
		actual string
	}{
		{[]core.SlotID{"cloak"}, "cloak"},
		{[]core.SlotID{"body", "body"}, "body"},
		{[]core.SlotID{"hand", "body", "shield", "hand", "body", "shield", "hand", "body", "shield"}, "9"},
	} {
		bad := b
		bad.Equipment = new(core.EquipmentRules)
		*bad.Equipment = *b.Equipment
		bad.Equipment.Derived = append([]core.DerivedStatRule(nil), b.Equipment.Derived...)
		terms := append([]core.StatTerm(nil), bad.Equipment.Derived[0].Terms...)
		terms[1].UnlessSlots = tc.slots
		bad.Equipment.Derived[0].Terms = terms
		_, err := core.NewWorld(bad)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Field != "unless_slots" || f.Actual != tc.actual {
			t.Fatal(tc.slots, err)
		}
	}
	if _, err := core.NewWorld(b); err != nil {
		t.Fatal(err)
	}
	ok := b
	ok.Equipment = new(core.EquipmentRules)
	*ok.Equipment = *b.Equipment
	ok.Equipment.Slots = append(append([]core.SlotDefinition(nil), b.Equipment.Slots...),
		core.SlotDefinition{ID: "s1", Capacity: 1}, core.SlotDefinition{ID: "s2", Capacity: 1}, core.SlotDefinition{ID: "s3", Capacity: 1},
		core.SlotDefinition{ID: "s4", Capacity: 1}, core.SlotDefinition{ID: "s5", Capacity: 1})
	ok.Equipment.Derived = append([]core.DerivedStatRule(nil), b.Equipment.Derived...)
	terms := append([]core.StatTerm(nil), ok.Equipment.Derived[0].Terms...)
	terms[1].UnlessSlots = []core.SlotID{"hand", "body", "shield", "s1", "s2", "s3", "s4", "s5"}
	ok.Equipment.Derived[0].Terms = terms
	if _, err := core.NewWorld(ok); err != nil {
		t.Fatal("eight slots", err)
	}
}
