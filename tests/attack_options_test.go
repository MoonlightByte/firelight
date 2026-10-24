package tests

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// The attack options view (docs/ATTACKS.md "Attack options"): every attacking
// mode of every item a Character wears or carries itself. A Fighter (Str 16,
// proficiency 2) in Chain Mail with a Shield, a Longsword in one hand and 8
// Javelins carried: to hit 3 + 2 = 5, damage bonus 3.
const optionsWorld = `rules { transfer unequips; wear any; }
location "yard" named "Yard";
location "camp" named "Camp";
character "fighter" named "Fighter" at "yard" { stat "strength" = 16; stat "dexterity" = 12; stat "proficiency" = 2; stat "defense" = 10; }
character "squire" named "Squire" at "yard" { stat "strength" = 10; stat "dexterity" = 14; stat "proficiency" = 2; stat "defense" = 10; }
condition type "martial" { instances unique; }
condition type "monk" { instances unique; }
condition type "monk-d8" { instances unique; }
condition "fighter:martial" of "martial" to "fighter";
equipment "nql-equipment-v1" {
 slot "hand" capacity 2;
 slot "body" capacity 1;
 slot "shield" capacity 1;
 derive stat "defense" { term stat "dexterity" offset -10 divide 2; }
 definition "longsword" named "Longsword" {
  description "Versatile.";
  default "held";
  mode "held" {
   occupy "hand" by 1;
   attack "melee-strength" named "Longsword (Strength)" {
    hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1 requires "martial"; }
    damage "slashing" dice 1 sides 8 { constant 0; term stat "strength" offset -10 divide 2; }
    melee reach 5 stat "strength";
   }
  }
  mode "two-handed" {
   occupy "hand" by 2;
   attack "melee-strength" named "Longsword (Strength, two-handed)" {
    hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1 requires "martial"; }
    damage "slashing" dice 1 sides 10 { constant 0; term stat "strength" offset -10 divide 2; }
    melee reach 5 stat "strength";
   }
  }
 }
 definition "javelin" named "Javelin" {
  description "Thrown.";
  default "held";
  mode "held" {
   occupy "hand" by 1;
   attack "melee-strength" named "Javelin (Strength)" {
    hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1 requires "martial"; }
    damage "piercing" dice 1 sides 6 { constant 0; term stat "strength" offset -10 divide 2; }
    melee reach 5 stat "strength";
   }
   attack "thrown-strength" named "Javelin (thrown, Strength)" {
    hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1 requires "martial"; }
    damage "piercing" dice 1 sides 6 { constant 0; term stat "strength" offset -10 divide 2; }
    thrown normal 30 maximum 120 stat "strength";
   }
  }
 }
 definition "staff" named "Quarterstaff" {
  description "A Monk weapon.";
  default "held";
  mode "held" {
   occupy "hand" by 1;
   attack "melee-dexterity" named "Quarterstaff (Dexterity, Martial Arts)" requires "monk" unless slot "shield" {
    hit { constant 0; term stat "dexterity" offset -10 divide 2; term stat "proficiency" offset 0 divide 1; }
    damage "bludgeoning" dice 1 sides 6 { constant 0; term stat "dexterity" offset -10 divide 2; }
     or dice 1 sides 8 requires "monk-d8" unless slot "shield"
    melee reach 5 stat "dexterity";
   }
  }
 }
 definition "gauntlet" named "Spiked Gauntlet" {
  description "Worn, no slot.";
  default "worn";
  mode "worn" {
   attack "punch" named "Gauntlet punch" {
    hit { constant 0; term stat "strength" offset -10 divide 2; }
    damage "piercing" dice 1 sides 4 { constant 0; term stat "strength" offset -10 divide 2; }
    melee reach 5 stat "strength";
   }
  }
 }
 definition "shield" named "Shield" { description "Shield."; default "held";
  mode "held" { occupy "hand" by 1; occupy "shield" by 1; modifier "shield" stat "defense" add 2; } }
 definition "chain" named "Chain Mail" { description "Heavy armor."; default "worn";
  mode "worn" { occupy "body" by 1; base stat "defense" { constant 16; } } }
}
item "fighter-mail" named "Chain Mail" { owner "fighter"; wearer "fighter"; definition "chain"; mode "worn"; }
item "fighter-shield" named "Shield" { owner "fighter"; wearer "fighter"; definition "shield"; mode "held"; }
item "fighter-sword" named "Longsword" { owner "fighter"; wearer "fighter"; definition "longsword"; mode "held"; }
item "fighter-javelins" named "Javelins" { owner "fighter"; custody character "fighter"; definition "javelin"; quantity 8; }
`

func optionsWorldWith(t *testing.T, edits ...[2]string) *core.World {
	t.Helper()
	src := optionsWorld
	for _, e := range edits {
		if e[0] == "" {
			src += e[1] + "\n"
			continue
		}
		if !strings.Contains(src, e[0]) {
			t.Fatal("no", e[0])
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	b, ds := compiler.CompileWorld("options.nql", src)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return world(t, b)
}

func attackOptions(t *testing.T, w *core.World, id core.CharacterID) core.AttackOptionsProjection {
	t.Helper()
	v, err := w.AttackOptionsView(id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// optionKey is item/mode -> (worn, slots, grant summaries).
func optionSummary(v core.AttackOptionsProjection) map[string]string {
	out := map[string]string{}
	for _, o := range v.Options {
		var parts []string
		for _, g := range o.Grants {
			kind := "melee"
			if g.Ranged != nil {
				kind = "ranged"
			} else if g.Thrown != nil {
				kind = "thrown"
			}
			parts = append(parts, g.Name+" "+kind+" "+itoa(g.ToHit)+" "+itoa(g.DamageCount)+"d"+itoa(g.DamageSides)+"+"+itoa(g.DamageBonus))
		}
		slots := ""
		for _, s := range o.Slots {
			slots += string(s.Slot) + itoa(s.Amount)
		}
		worn := "carried"
		if o.Worn {
			worn = "worn"
		}
		out[string(o.Item)+"/"+string(o.Mode)] = worn + " " + slots + " [" + strings.Join(parts, "; ") + "]"
	}
	return out
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// The worked example of plan and guide: both Longsword modes, the carried
// Javelins with melee and thrown, and nothing for the armor or the shield.
func TestAttackOptionsFighter(t *testing.T) {
	v := attackOptions(t, optionsWorldWith(t), "fighter")
	if v.Character.ID != "fighter" || v.Character.Name != "Fighter" || v.Revision != 0 {
		t.Fatalf("%+v", v)
	}
	want := map[string]string{
		"fighter-javelins/held":    "carried hand1 [Javelin (thrown, Strength) thrown 5 1d6+3; Javelin (Strength) melee 5 1d6+3]",
		"fighter-sword/held":       "worn hand1 [Longsword (Strength) melee 5 1d8+3]",
		"fighter-sword/two-handed": "carried hand2 [Longsword (Strength, two-handed) melee 5 1d10+3]",
	}
	if got := optionSummary(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	// Order: by item, then the definition's modes.
	var order []string
	for _, o := range v.Options {
		order = append(order, string(o.Item)+"/"+string(o.Mode))
	}
	if !reflect.DeepEqual(order, []string{"fighter-javelins/held", "fighter-sword/held", "fighter-sword/two-handed"}) {
		t.Fatal(order)
	}
	// Grants within an option are in grant ID order, as in the equipment view.
	for _, o := range v.Options {
		for k := 1; k < len(o.Grants); k++ {
			if o.Grants[k-1].ID >= o.Grants[k].ID {
				t.Fatal("grant order", o.Grants)
			}
		}
	}
	raw, err := json.Marshal(v)
	if err != nil || !strings.HasPrefix(string(raw), `{"character":{"id":"fighter","name":"Fighter"},"revision":0,"options":[{"item":"fighter-javelins","definition":"javelin","mode":"held","slots":[{"slot":"hand","amount":1}],"worn":false,"grants":[`) {
		t.Fatal(string(raw), err)
	}

	// The guide shows this response, with each grants list elided.
	guide, err := os.ReadFile("../docs/ATTACKS.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Attack options\n")
	_, block, ok2 := strings.Cut(after, "```text\n")
	block, _, ok3 := strings.Cut(block, "```")
	if !ok || !ok2 || !ok3 {
		t.Fatal("the guide has no attack options example")
	}
	var shown, engine any
	if err := json.Unmarshal([]byte("{"+strings.ReplaceAll(block, "[...]", "[]")+"}"), &shown); err != nil {
		t.Fatal(err)
	}
	for k := range v.Options {
		v.Options[k].Grants = []core.AttackGrant{}
	}
	raw, _ = json.Marshal(map[string]any{"attack_options": []core.AttackOptionsProjection{v}})
	if err := json.Unmarshal(raw, &engine); err != nil || !reflect.DeepEqual(shown, engine) {
		t.Fatalf("guide %v\nengine %v", shown, engine)
	}
}

// The worn option is the equipment view for that item, byte for byte, in the
// fixture and in every example world that wears an attacking item.
func TestAttackOptionsWornMatchesEquipment(t *testing.T) {
	compared := 0
	check := func(name string, w *core.World) {
		s := w.Snapshot()
		for _, c := range s.Blueprint.Characters {
			e, err := w.EquipmentView(c.ID)
			if err != nil {
				t.Fatal(name, err)
			}
			v := attackOptions(t, w, c.ID)
			for _, o := range v.Options {
				if !o.Worn {
					continue
				}
				var listed []core.AttackGrant
				for _, g := range e.Attacks {
					if g.Origin.Item == o.Item {
						listed = append(listed, g)
					}
				}
				if listed == nil {
					listed = []core.AttackGrant{}
				}
				a, _ := json.Marshal(o.Grants)
				b, _ := json.Marshal(listed)
				if string(a) != string(b) {
					t.Fatalf("%s %s %s: options %s, equipment %s", name, c.ID, o.Item, a, b)
				}
				compared++
			}
		}
	}
	check("fixture", optionsWorldWith(t))
	files, _ := filepath.Glob("../examples/*/*.nql")
	more, _ := filepath.Glob("../examples/*.nql")
	for _, f := range append(files, more...) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b, ds := compiler.CompileWorld(filepath.Base(f), string(src))
		if len(ds) != 0 || b.Equipment == nil {
			continue
		}
		w, err := core.NewWorld(b)
		if err != nil {
			continue
		}
		check(f, w)
	}
	if compared < 4 {
		t.Fatal("too few worn options compared:", compared)
	}
}

// Worn or carried by the Character itself; never mere ownership.
func TestAttackOptionsCustody(t *testing.T) {
	w := optionsWorldWith(t,
		[2]string{"", `item "camp-sword" named "Camp sword" { owner "fighter"; custody location "camp"; definition "longsword"; }`},
		[2]string{"", `item "lent-javelin" named "Lent javelin" { owner "fighter"; custody character "squire"; definition "javelin"; }`},
		[2]string{"", `item "chest" named "Chest" { container true; custody location "camp"; }`},
		[2]string{"", `item "chest-sword" named "Chest sword" { owner "fighter"; custody item "chest"; definition "longsword"; }`},
		[2]string{"", `item "borrowed-gauntlet" named "Gauntlet" { owner "squire"; wearer "fighter"; definition "gauntlet"; mode "worn"; }`},
	)
	got := optionSummary(attackOptions(t, w, "fighter"))
	for _, absent := range []string{"camp-sword/held", "lent-javelin/held", "chest-sword/held"} {
		if _, ok := got[absent]; ok {
			t.Fatal("listed", absent)
		}
	}
	if got["borrowed-gauntlet/worn"] != "worn  [Gauntlet punch melee 3 1d4+3]" {
		t.Fatal("worn but not owned:", got)
	}
	// A mode with no slots reports an empty list, not null.
	if raw, _ := json.Marshal(attackOptions(t, w, "fighter")); !strings.Contains(string(raw), `"mode":"worn","slots":[],"worn":true`) {
		t.Fatal(string(raw))
	}
	squire := optionSummary(attackOptions(t, w, "squire"))
	if squire["lent-javelin/held"] != "carried hand1 [Javelin (Strength) melee 0 1d6+0; Javelin (thrown, Strength) thrown 0 1d6+0]" {
		t.Fatal("carrier:", squire)
	}
	// A Character with no attacking item has an empty list, not null.
	bare := optionsWorldWith(t)
	v := attackOptions(t, bare, "squire")
	raw, _ := json.Marshal(v)
	if len(v.Options) != 0 || !strings.HasSuffix(string(raw), `"options":[]}`) {
		t.Fatal(string(raw))
	}
}

// Gates and die upgrades read the Character as it stands now.
func TestAttackOptionsGates(t *testing.T) {
	staff := `item "squire-staff" named "Quarterstaff" { owner "squire"; custody character "squire"; definition "staff"; }`
	monk := `condition "squire:monk" of "monk" to "squire";`
	d8 := `condition "squire:monk-d8" of "monk-d8" to "squire";`
	shieldCarried := `item "squire-shield" named "Shield" { owner "squire"; custody character "squire"; definition "shield"; }`
	shieldWorn := `item "squire-shield" named "Shield" { owner "squire"; wearer "squire"; definition "shield"; mode "held"; }`
	for _, tc := range []struct {
		name  string
		extra []string
		want  string
	}{
		{"no requires: shut", []string{staff}, "carried hand1 []"},
		{"open", []string{staff, monk}, "carried hand1 [Quarterstaff (Dexterity, Martial Arts) melee 4 1d6+2]"},
		{"upgrade die", []string{staff, monk, d8}, "carried hand1 [Quarterstaff (Dexterity, Martial Arts) melee 4 1d8+2]"},
		{"shield carried, not worn", []string{staff, monk, shieldCarried}, "carried hand1 [Quarterstaff (Dexterity, Martial Arts) melee 4 1d6+2]"},
		{"shield worn: shut", []string{staff, monk, d8, shieldWorn}, "carried hand1 []"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var edits [][2]string
			for _, e := range tc.extra {
				edits = append(edits, [2]string{"", e})
			}
			v := attackOptions(t, optionsWorldWith(t, edits...), "squire")
			if got := optionSummary(v); got["squire-staff/held"] != tc.want {
				t.Fatal(got)
			}
			// A shut mode is still listed, with an empty list, not null.
			if raw, _ := json.Marshal(v); strings.HasSuffix(tc.want, "[]") && !strings.Contains(string(raw), `"worn":false,"grants":[]}`) {
				t.Fatal(string(raw))
			}
		})
	}
}

// Slots are reported, not enforced: the two-handed mode is listed while the
// shield holds the other hand, with the mode's slot use verbatim.
func TestAttackOptionsSlotsReported(t *testing.T) {
	v := attackOptions(t, optionsWorldWith(t), "fighter")
	for _, o := range v.Options {
		if o.Item == "fighter-sword" && o.Mode == "two-handed" {
			if o.Worn || !reflect.DeepEqual(o.Slots, []core.SlotUse{{Slot: "hand", Amount: 2}}) || len(o.Grants) != 1 {
				t.Fatalf("%+v", o)
			}
			return
		}
	}
	t.Fatal("no two-handed option")
}

// A hypothetical mode's grant has the ID the same member gets once the item is
// worn in that mode. Only a single unit may be worn, so the worn Javelins are
// the same item ID with no quantity: the grant ID does not read the quantity.
func TestAttackOptionsGrantIDs(t *testing.T) {
	ids := func(w *core.World, item core.ItemID, mode core.EquipmentModeID) []core.EquipmentGrantID {
		var out []core.EquipmentGrantID
		for _, o := range attackOptions(t, w, "fighter").Options {
			if o.Item == item && o.Mode == mode {
				for _, g := range o.Grants {
					out = append(out, g.ID)
				}
			}
		}
		return out
	}
	worn := func(w *core.World, item core.ItemID) []core.EquipmentGrantID {
		e, err := w.EquipmentView("fighter")
		if err != nil {
			t.Fatal(err)
		}
		var out []core.EquipmentGrantID
		for _, g := range e.Attacks {
			if g.Origin.Item == item {
				out = append(out, g.ID)
			}
		}
		return out
	}
	base := optionsWorldWith(t)
	two := optionsWorldWith(t,
		[2]string{`item "fighter-shield" named "Shield" { owner "fighter"; wearer "fighter"; definition "shield"; mode "held"; }`, `item "fighter-shield" named "Shield" { owner "fighter"; custody character "fighter"; definition "shield"; }`},
		[2]string{`definition "longsword"; mode "held"; }`, `definition "longsword"; mode "two-handed"; }`})
	if a, b := ids(base, "fighter-sword", "two-handed"), worn(two, "fighter-sword"); len(a) != 1 || !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
	thrown := optionsWorldWith(t,
		[2]string{`item "fighter-shield" named "Shield" { owner "fighter"; wearer "fighter"; definition "shield"; mode "held"; }`, `item "fighter-shield" named "Shield" { owner "fighter"; custody character "fighter"; definition "shield"; }`},
		[2]string{`custody character "fighter"; definition "javelin"; quantity 8;`, `wearer "fighter"; definition "javelin"; mode "held";`})
	if a, b := ids(base, "fighter-javelins", "held"), worn(thrown, "fighter-javelins"); len(a) != 2 || !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
}

// The view is detached and read-only.
func TestAttackOptionsReadOnly(t *testing.T) {
	w := optionsWorldWith(t)
	before := w.Snapshot()
	history := w.History()
	v := attackOptions(t, w, "fighter")
	v.Options[0].Slots[0].Amount = 9
	v.Options[0].Grants[0].Thrown.Normal = 99
	v.Options[0].Grants[0].HitRecipe.Terms[0].Term.Offset = 7
	unchanged(t, w, before, history)
	again := attackOptions(t, w, "fighter")
	if again.Options[0].Slots[0].Amount != 1 || again.Options[0].Grants[0].Thrown.Normal != 30 || again.Options[0].Grants[0].HitRecipe.Terms[0].Term.Offset != -10 {
		t.Fatal("the view aliases the world", again.Options[0])
	}
}

func TestAttackOptionsUnknownCharacter(t *testing.T) {
	w := optionsWorldWith(t)
	_, want := w.EquipmentView("nobody")
	_, err := w.AttackOptionsView("nobody")
	var f, g *core.Fault
	if !errors.As(err, &f) || !errors.As(want, &g) || *f != *g {
		t.Fatal(err, want)
	}
}
