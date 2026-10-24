package tests

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

const dummy = "char:dummy"

// traitWorld is the attack example world with a target that has the given
// own traits, and the Condition instances on it that the policies name:
// "spell:ward" (resistance), "hex:frail" (vulnerability), "spell:proof"
// (immunity), "hex:mixed" (immunity, then resistance), each for "slashing".
func traitWorld(t testing.TB, traits string, conditions ...string) *core.World {
	t.Helper()
	extra := fmt.Sprintf("character %q named \"Dummy\" at \"loc:valley/B03\" { stat \"ac\" = 10; resource \"hp\" = 100 min 0 max 100; %s }\n", dummy, traits)
	extra += "condition type \"spell:ward\" { instances coexist; }\ncondition type \"hex:frail\" { instances coexist; }\ncondition type \"spell:proof\" { instances coexist; }\ncondition type \"hex:mixed\" { instances coexist; }\n"
	extra += "damage trait \"spell:ward\" resistance \"slashing\";\ndamage trait \"hex:frail\" vulnerability \"slashing\";\ndamage trait \"spell:proof\" immunity \"slashing\";\ndamage trait \"hex:mixed\" immunity \"slashing\";\ndamage trait \"hex:mixed\" resistance \"slashing\";\n"
	for _, c := range conditions {
		id, kind, _ := strings.Cut(c, "=")
		extra += fmt.Sprintf("condition %q of %q to %q;\n", id, kind, dummy)
	}
	_, w := attackWorld(t, extra)
	return w
}

func TestAttackTraits(t *testing.T) {
	type treat = core.AttackTreatment
	own := func(t string) treat { return treat{Character: dummy, Treatment: t} }
	held := func(id core.ConditionInstanceID, t string) treat { return treat{Condition: id, Treatment: t} }
	for _, tc := range []struct {
		name, traits string
		conditions   []string
		profile      string
		rest         string
		damage       int64
		treatments   []treat
	}{
		{"none", "", nil, "1d8", "faces 15 damage 7", 7, nil},
		{"resistance rounds down", `resist "slashing";`, nil, "1d8", "faces 15 damage 7", 3, []treat{own("resistance")}},
		{"vulnerability", `vulnerable "slashing";`, nil, "1d8", "faces 15 damage 7", 14, []treat{own("vulnerability")}},
		{"immunity", `immune "slashing";`, nil, "1d8", "faces 15 damage 7", 0, []treat{own("immunity")}},
		{"resistance then vulnerability", `vulnerable "slashing"; resist "slashing";`, nil, "1d8", "faces 15 damage 7", 6, []treat{own("resistance"), own("vulnerability")}},
		{"immunity over both", `immune "slashing"; vulnerable "slashing"; resist "slashing";`, nil, "1d8", "faces 15 damage 7", 0, []treat{own("resistance"), own("vulnerability"), own("immunity")}},
		{"another kind", `resist "piercing"; vulnerable "fire";`, nil, "1d8", "faces 15 damage 7", 7, nil},
		{"a Condition's resistance", "", []string{"d:ward=spell:ward"}, "1d8", "faces 15 damage 7", 3, []treat{held("d:ward", "resistance")}},
		{"two resistances halve once", `resist "slashing";`, []string{"d:ward=spell:ward"}, "1d8", "faces 15 damage 7", 3, []treat{own("resistance"), held("d:ward", "resistance")}},
		{"own trait and Condition", `vulnerable "slashing";`, []string{"d:ward=spell:ward"}, "1d8", "faces 15 damage 7", 6, []treat{own("vulnerability"), held("d:ward", "resistance")}},
		{"Conditions by instance", "", []string{"d:z=spell:ward", "d:a=hex:frail", "d:m=spell:proof"}, "1d8", "faces 15 damage 7", 0, []treat{held("d:a", "vulnerability"), held("d:m", "immunity"), held("d:z", "resistance")}},
		{"one Condition's treatments in order", "", []string{"d:x=hex:mixed"}, "1d8", "faces 15 damage 7", 0, []treat{held("d:x", "resistance"), held("d:x", "immunity")}},
		{"floor before vulnerability", `vulnerable "slashing";`, nil, "1d8-5", "faces 15 damage 2", 0, []treat{own("vulnerability")}},
		{"a critical's total", `resist "slashing";`, nil, "1d8+1", "faces 20 critical damage 10", 5, []treat{own("resistance")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := traitWorld(t, tc.traits, tc.conditions...)
			b, err := attackRun(t, w, "r1", swing("char:ada", dummy, `to_hit 0 damage `+tc.profile+` kind "slashing"`, tc.rest))
			if err != nil {
				t.Fatal(err)
			}
			r := scored(b)[0]
			if *r.Damage != tc.damage || !reflect.DeepEqual(r.Treatments, tc.treatments) || currentOf(w, dummy, "hp") != 100-tc.damage {
				t.Fatalf("%+v", r)
			}
			if (tc.damage == 0) != (len(b.Events) == 1) {
				t.Fatalf("%v", eventKinds(b))
			}
		})
	}
	// A miss and a score-only swing meet no treatment.
	w := traitWorld(t, `resist "slashing";`)
	b, err := attackRun(t, w, "r1", swing("char:ada", dummy, `to_hit 0 damage 1d8 kind "slashing"`, "faces 2 damage 7")+"\n"+swing("char:ada", dummy, `to_hit 0 damage 1d8 kind "slashing"`, "faces 15"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range scored(b) {
		if r.Treatments != nil || r.Damage != nil {
			t.Fatalf("%+v", r)
		}
	}
}

// A character created in play carries its traits, and history replays.
func TestAttackTraitsCreatedAndReplayed(t *testing.T) {
	b0, w := attackWorld(t, "condition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";\n")
	if _, err := attackRun(t, w, "r1", `create character "occ:valley/B03/imp" named "Imp" at "loc:valley/B03" { stat "ac" = 13; resource "hp" = 20 min 0 max 20; vulnerable "fire"; immune "poison"; };
apply condition "imp:ward" of "spell:ward" to "occ:valley/B03/imp";`); err != nil {
		t.Fatal(err)
	}
	b, err := attackRun(t, w, "r2", swing("char:ada", "occ:valley/B03/imp", `to_hit 5 damage 2d6+1 kind "fire"`, "faces 13 damage 8")+"\n"+swing("char:ada", "occ:valley/B03/imp", `to_hit 5 damage 1d4 kind "poison"`, "faces 13 damage 3"))
	if err != nil {
		t.Fatal(err)
	}
	s := scored(b)
	if *s[0].Damage != 8 || *s[1].Damage != 0 || currentOf(w, "occ:valley/B03/imp", "hp") != 12 {
		t.Fatalf("%+v", s)
	}
	r, err := core.Replay(b0, w.History())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), w.Snapshot()) {
		t.Fatal("replay differs")
	}
}

func TestAttackTraitRefusals(t *testing.T) {
	many := ""
	for i := 0; i <= core.MaxDamageTraitsPerTreatment; i++ {
		many += fmt.Sprintf(" resist \"k%02d\";", i)
	}
	// The example world declares one policy; these make exactly 17.
	policies := "condition type \"spell:ward\" { instances coexist; }\n"
	for i := 0; i < core.MaxDamageTraitPolicies; i++ {
		policies += fmt.Sprintf("damage trait \"spell:ward\" resistance \"k%02d\";\n", i)
	}
	raw, err := os.ReadFile("../examples/attacks/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, edit, at, code string
		noLaw                bool
	}{
		{"a trait twice", `resist "fire"; resist "fire";`, "fire", "E_ATTACK", false},
		{"33 of one treatment", many, "resist", "E_ATTACK", false},
		{"a blank kind", `immune "";`, "", "E_ATTACK", false},
		{"an unknown trait word", `resists "fire";`, "resists", "E_PARSE", false},
		{"a policy without the law", "POLICY condition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";", "damage", "E_ATTACK", true},
		{"an undeclared Condition type", "POLICY damage trait \"spell:none\" resistance \"fire\";", "spell:none", "E_ATTACK", false},
		{"a policy twice", "POLICY condition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";\ndamage trait \"spell:ward\" resistance \"fire\";", "fire", "E_ATTACK", false},
		{"17 policies", "POLICY " + policies, "damage", "E_ATTACK", false},
		{"an unknown treatment", "POLICY condition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resist \"fire\";", "resist", "E_PARSE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := string(raw)
			if tc.noLaw {
				src = strings.Replace(src, `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 2; }`, "", 1)
				src = strings.Replace(src, "damage trait \"spell:warding\" resistance \"bludgeoning\";\n", "", 1)
			}
			if p, ok := strings.CutPrefix(tc.edit, "POLICY "); ok {
				src += p + "\n"
			} else {
				src += fmt.Sprintf("character \"char:t\" named \"T\" at \"loc:valley/B03\" { stat \"ac\" = 1; %s }\n", tc.edit)
			}
			_, ds := compiler.CompileWorld("world.nql", src)
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.HasPrefix(ds[0].Actual, shownAt(tc.code, tc.at)) {
				t.Fatalf("%+v", ds)
			}
		})
	}
	// Own traits need the attack law too: Battle would not apply them.
	noLaw := "rules { transfer unequips; wear any; }\nlocation \"loc:a\" named \"A\";\ncharacter \"char:a\" named \"A\" at \"loc:a\" { stat \"ac\" = 1;\n immune \"fire\"; }\n"
	if _, ds := compiler.CompileWorld("world.nql", noLaw); len(ds) != 1 || ds[0].Code != "E_ATTACK" || ds[0].Expected != "world attacks law" || !strings.HasPrefix(ds[0].Actual, `"immune"`) || ds[0].Position.Line != 4 {
		t.Fatalf("%+v", ds)
	}
	nb, ds := compiler.CompileWorld("world.nql", "rules { transfer unequips; wear any; }\nlocation \"loc:a\" named \"A\";\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, ds := compiler.CompileActions("a.nql", "create character \"char:b\" named \"B\" at \"loc:a\" { stat \"ac\" = 1; resist \"cold\"; };", nb); len(ds) != 1 || ds[0].Code != "E_ATTACK" || !strings.HasPrefix(ds[0].Actual, `"resist"`) {
		t.Fatalf("%+v", ds)
	}
	// The same checks on a blueprint handed to the engine directly.
	b, _ := attackWorld(t, "")
	bad := b
	bad.Characters = append([]core.Character(nil), b.Characters...)
	bad.Characters[0].DamageTraits = []core.DamageTrait{{Treatment: "absorb", Kind: "fire"}}
	if f := faultOf(t, core.Validate(bad)); f.Code != "E_ATTACK" || f.Field != "damage trait" {
		t.Fatal(f)
	}
	bad = b
	bad.DamageTraits = []core.DamageTraitPolicy{{Condition: "state:concentration", Treatment: "immunity", Kind: " "}}
	if f := faultOf(t, core.Validate(bad)); f.Code != "E_ATTACK" || f.Field != "damage trait" {
		t.Fatal(f)
	}
}

// The receipt's treatments, and a snapshot's traits and policies, are copies.
func TestAttackTraitsDetached(t *testing.T) {
	w := traitWorld(t, `resist "slashing";`, "d:ward=spell:ward")
	attack := swing("char:ada", dummy, `to_hit 0 damage 1d8 kind "slashing"`, "faces 15 damage 7")
	b, err := attackRun(t, w, "r1", attack)
	if err != nil {
		t.Fatal(err)
	}
	b.Events[len(b.Events)-1].Attack.Treatments[0].Treatment = "immunity"
	h := w.History()
	if got := scored(h[len(h)-1])[0].Treatments; got[0].Treatment != "resistance" {
		t.Fatalf("%+v", got)
	}
	s := w.Snapshot()
	for i := range s.Blueprint.Characters {
		for j := range s.Blueprint.Characters[i].DamageTraits {
			s.Blueprint.Characters[i].DamageTraits[j].Kind = "fire"
		}
	}
	for i := range s.Blueprint.DamageTraits {
		s.Blueprint.DamageTraits[i].Kind = "fire"
	}
	b, err = attackRun(t, w, "r2", attack)
	if err != nil {
		t.Fatal(err)
	}
	if r := scored(b)[0]; *r.Damage != 3 || len(r.Treatments) != 2 {
		t.Fatalf("%+v", r)
	}
}
