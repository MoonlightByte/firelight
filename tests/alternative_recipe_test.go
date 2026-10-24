package tests

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// Gated alternative recipes on a derived stat (docs/GRAMMAR.md "Alternative
// recipes"): SRD 5.2.1 Mage Armor, base AC 13 + Dexterity while the target
// wears no armor. The stat takes the eligible recipe with the highest total
// (base plus active terms), so it never adds to Unarmored Defense. Every
// expected number is worked by hand.

const recipeTypes = `condition type "effect:ma1" { instances unique; }
condition type "effect:ma2" { instances unique; }
condition type "effect:sof" { instances unique; modifier "m0" stat "defense" add 2; }
condition type "feature:unarmored-defense:monk" { instances unique; }
`

const recipeChars = `character "maren" named "Maren" at "yard" { stat "dexterity" = 13; stat "wisdom" = 10; stat "defense" = 10; }
character "ilse" named "Ilse" at "yard" { stat "dexterity" = 15; stat "wisdom" = 10; stat "defense" = 10; }
character "tess" named "Tess" at "yard" { stat "dexterity" = 14; stat "wisdom" = 18; stat "defense" = 10; }
condition "tess:monk" of "feature:unarmored-defense:monk" to "tess";
`

// recipeSource builds the world: the defense derive with Unarmored Defense
// (Wisdom, unless body or shield) and the given recipe clauses, chain mail and
// a shield, then the extra lines (Condition instances and items).
func recipeSource(recipes, extra string) string {
	return "rules { transfer unequips; wear any; }\nlocation \"yard\" named \"Yard\";\n" + recipeTypes + recipeChars +
		fmt.Sprintf(`equipment %q {
 slot "hand" capacity 2;
 slot "body" capacity 1;
 slot "shield" capacity 1;
 derive stat "defense" { term stat "dexterity" offset -10 divide 2; term stat "wisdom" offset -10 divide 2 requires "feature:unarmored-defense:monk" unless slot "body" unless slot "shield";%s }
 definition "chain" named "Chain Mail" { description "Body armor."; default "worn"; mode "worn" { occupy "body" by 1; base stat "defense" { constant 16; } } }
 definition "shield" named "Shield" { description "Shield."; default "held"; mode "held" { occupy "hand" by 1; occupy "shield" by 1; modifier "defense" stat "defense" add 2; } }
}
`, core.EquipmentVersion, recipes) + extra
}

const mageArmor1 = ` recipe "ac-set:ma1" base 13 requires "effect:ma1" unless slot "body" { term stat "dexterity" offset -10 divide 2; }`
const mageArmor2 = ` recipe "ac-set:ma2" base 13 requires "effect:ma2" unless slot "body" { term stat "dexterity" offset -10 divide 2; }`

func recipeWorld(t *testing.T, recipes, extra string) (*core.World, core.Blueprint) {
	t.Helper()
	b, ds := compiler.CompileWorld("recipe.nql", recipeSource(recipes, extra))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return world(t, b), b
}

func cast(id, effect, to string) string {
	return fmt.Sprintf("condition %q of %q to %q;\n", id, effect, to)
}

func worn(id, def, mode, who string) string {
	return fmt.Sprintf("item %q named %q { owner %q; wearer %q; definition %q; mode %q; }\n", id, def, who, who, def, mode)
}

// 1. Parse: a recipe compiles; malformed clauses are refused.
func TestRecipeParse(t *testing.T) {
	recipeWorld(t, mageArmor1, "")
	for _, tc := range []struct{ recipe, code string }{
		{` recipe "ac-set:ma1" requires "effect:ma1" { term stat "dexterity" offset -10 divide 2; }`, "E_PARSE"},
		{` recipe "ac-set:ma1" base "13" requires "effect:ma1" { term stat "dexterity" offset -10 divide 2; }`, "E_PARSE"},
		{` recipe "ac-set:ma1" base 13 requires "effect:ma1" while slot "body" { term stat "dexterity" offset -10 divide 2; }`, "E_PARSE"},
		{` recipe "ac-set:ma1" base 13 { term stat "dexterity" offset -10 divide 2; }`, "E_PARSE"},
		{mageArmor1 + mageArmor1, "E_DUPLICATE"},
		{` recipe "ac-set:ma1" base 13 requires "effect:nope" { term stat "dexterity" offset -10 divide 2; }`, "E_EQUIPMENT"},
		{` recipe "ac-set:ma1" base 13 requires "effect:ma1" unless slot "cloak" { term stat "dexterity" offset -10 divide 2; }`, "E_EQUIPMENT"},
		{` recipe "ac-set:ma1" base 13 requires "effect:ma1" { term stat "defense" offset -10 divide 2; }`, "E_EQUIPMENT"},
	} {
		_, ds := compiler.CompileWorld("recipe.nql", recipeSource(tc.recipe, ""))
		if len(ds) == 0 || ds[0].Code != tc.code {
			t.Fatalf("%s: want %s, got %v", tc.recipe, tc.code, ds)
		}
	}
}

// 2. Eligibility: the recipe counts only with its Condition and without body armor.
func TestRecipeEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		id          core.CharacterID
		want        int64
	}{
		{"no Mage Armor: 10 + 1", "", "maren", 11},
		{"Mage Armor: 13 + 1", cast("m1", "effect:ma1", "maren"), "maren", 14},
		{"Mage Armor in chain mail: 16", cast("m1", "effect:ma1", "maren") + worn("mail", "chain", "worn", "maren"), "maren", 16},
		{"another's Mage Armor does not count", cast("i1", "effect:ma1", "ilse"), "maren", 11},
		{"two casters, one recipe, own Dexterity: 13 + 1", cast("m1", "effect:ma1", "maren") + cast("i1", "effect:ma1", "ilse"), "maren", 14},
		{"two casters, one recipe, own Dexterity: 13 + 2", cast("m1", "effect:ma1", "maren") + cast("i1", "effect:ma1", "ilse"), "ilse", 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := recipeWorld(t, mageArmor1, tc.extra)
			stat(t, w, tc.id, "defense", tc.want)
		})
	}
	// Two recipes, one per effect: each character takes the one it holds.
	w, _ := recipeWorld(t, mageArmor1+mageArmor2, cast("m1", "effect:ma1", "maren")+cast("i2", "effect:ma2", "ilse"))
	stat(t, w, "maren", "defense", 14)
	stat(t, w, "ilse", "defense", 15)
}

// 3. The highest total wins, and alternatives never add: totals, not bases, rank.
func TestRecipeHighestTotal(t *testing.T) {
	// Tess, Monk, Dex 14 / Wis 18: Unarmored Defense 10 + 2 + 4 = 16 beats
	// Mage Armor 13 + 2 = 15, though Mage Armor's base is higher.
	w, _ := recipeWorld(t, mageArmor1, cast("t1", "effect:ma1", "tess"))
	stat(t, w, "tess", "defense", 16)
	e, err := w.Explain("tess", "defense")
	if err != nil || e.Recipe.Name != "" || e.Recipe.Base != 10 {
		t.Fatal("Unarmored Defense should be chosen", e.Recipe, err)
	}
	// Dex 14 / Wis 10: Mage Armor 13 + 2 = 15 beats 10 + 2 + 0 = 12.
	b, ds := compiler.CompileWorld("recipe.nql", strings.Replace(recipeSource(mageArmor1, cast("t1", "effect:ma1", "tess")), `stat "wisdom" = 18`, `stat "wisdom" = 10`, 1))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	stat(t, world(t, b), "tess", "defense", 15)
	// Never additive, and gated out by body armor: chain mail is 16 alone.
	w, _ = recipeWorld(t, mageArmor1, cast("t1", "effect:ma1", "tess")+worn("mail", "chain", "worn", "tess"))
	stat(t, w, "tess", "defense", 16)
	e, err = w.Explain("tess", "defense")
	if err != nil || e.Recipe.Name != "" || e.Recipe.Origin == nil || e.Recipe.Candidates[1].Eligible {
		t.Fatal("chain mail should be chosen, Mage Armor ineligible", e.Recipe, err)
	}
}

// 4. Ties keep the default, then the earlier recipe.
func TestRecipeTies(t *testing.T) {
	// Maren, Dex 13: a base-10 recipe ties the default (10 + 1).
	tie := ` recipe "ac-set:ma1" base 10 requires "effect:ma1" { term stat "dexterity" offset -10 divide 2; }`
	w, _ := recipeWorld(t, tie, cast("m1", "effect:ma1", "maren"))
	e, err := w.Explain("maren", "defense")
	if err != nil || e.Effective != 11 || e.Recipe.Name != "" {
		t.Fatal("a tie keeps the default", e.Recipe, err)
	}
	// Two equal recipes: the earlier is chosen.
	w, _ = recipeWorld(t, mageArmor1+mageArmor2, cast("m1", "effect:ma1", "maren")+cast("m2", "effect:ma2", "maren"))
	e, err = w.Explain("maren", "defense")
	if err != nil || e.Effective != 14 || e.Recipe.Name != "ac-set:ma1" {
		t.Fatal("a tie keeps the earlier recipe", e.Recipe, err)
	}
}

// 5. Modifiers add once to the chosen recipe: a shield and Shield of Faith, in either order.
func TestRecipeModifiers(t *testing.T) {
	ma, sof := cast("m1", "effect:ma1", "maren"), cast("s1", "effect:sof", "maren")
	for _, tc := range []struct {
		name, extra string
		want        int64
	}{
		{"Mage Armor and a shield: 14 + 2", ma + worn("sh", "shield", "held", "maren"), 16},
		{"Mage Armor then Shield of Faith: 14 + 2", ma + sof, 16},
		{"Shield of Faith then Mage Armor: 14 + 2", sof + ma, 16},
		{"Shield of Faith alone: 11 + 2", sof, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := recipeWorld(t, mageArmor1, tc.extra)
			stat(t, w, "maren", "defense", tc.want)
		})
	}
}

// 6. The explanation names the chosen recipe and lists every candidate.
func TestRecipeExplanation(t *testing.T) {
	w, _ := recipeWorld(t, mageArmor1+mageArmor2, cast("m1", "effect:ma1", "maren"))
	e, err := w.Explain("maren", "defense")
	if err != nil {
		t.Fatal(err)
	}
	r := e.Recipe
	if r.Name != "ac-set:ma1" || r.Requires != "effect:ma1" || r.Base != 13 || r.Origin != nil || len(r.Terms) != 1 || r.Terms[0].Exact != "1" {
		t.Fatal("chosen recipe", r)
	}
	want := []core.RecipeCandidate{{Eligible: true, Exact: "11"}, {Name: "ac-set:ma1", Eligible: true, Exact: "14"}, {Name: "ac-set:ma2"}}
	if !reflect.DeepEqual(r.Candidates, want) {
		t.Fatal("candidates", r.Candidates)
	}
	raw, _ := json.Marshal(r)
	if !strings.Contains(string(raw), `"name":"ac-set:ma1","requires":"effect:ma1","candidates":[{"eligible":true,"exact":"11"},`) {
		t.Fatal("explanation JSON", string(raw))
	}
}

// 7. A world without recipes serializes and explains exactly as before.
func TestRecipeOldShape(t *testing.T) {
	for _, src := range []string{srdStatsFile(t, "world.nql"), recipeSource("", cast("m1", "effect:ma1", "maren"))} {
		b, ds := compiler.CompileWorld("world.nql", src)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		raw, err := json.Marshal(b)
		if err != nil || strings.Contains(string(raw), `"recipes"`) {
			t.Fatal("a world without recipes gained a recipes key", err)
		}
	}
	w, _ := recipeWorld(t, "", cast("m1", "effect:ma1", "maren"))
	e, err := w.Explain("maren", "defense")
	if err != nil || e.Effective != 11 {
		t.Fatal(e, err)
	}
	raw, _ := json.Marshal(e)
	for _, key := range []string{`"candidates"`, `"name"`, `"requires":"effect`} {
		if strings.Contains(string(raw), key) {
			t.Fatal("an explanation without recipes gained", key, string(raw))
		}
	}
}

// 8. A world with a live recipe replays to the same head after donning armor.
func TestRecipeReplay(t *testing.T) {
	w, b := recipeWorld(t, mageArmor1, cast("m1", "effect:ma1", "maren")+`item "mail" named "Chain Mail" { owner "maren"; definition "chain"; }`+"\n")
	stat(t, w, "maren", "defense", 14)
	don := core.Request{ID: "don", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "maren"}, Operations: []core.Operation{{Equip: &core.Equip{Item: "mail", Wearer: "maren"}}}}
	commitC(t, w, don)
	stat(t, w, "maren", "defense", 16)
	rw, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(rw.History(), w.History()) || !reflect.DeepEqual(rw.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	stat(t, rw, "maren", "defense", 16)
}
