package tests

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

const (
	spider = "occ:valley/B03/spider-1"
	imp    = "occ:valley/B03/imp-1"
	bandit = "occ:valley/B03/bandit-1"
)

// saveWorld is the save example world, a map and no Battle, with extra
// declarations.
func saveWorld(t testing.TB, extra string) (core.Blueprint, *core.World) {
	t.Helper()
	text, err := os.ReadFile("../examples/saves/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("world.nql", string(text)+extra)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return b, w
}

func saved(b core.Batch) []core.SaveResult {
	var out []core.SaveResult
	for _, e := range b.Events {
		if e.Kind == core.SaveScored {
			out = append(out, *e.Save)
		}
	}
	return out
}

// save is a save action: the profile, then the target, then the rest.
func save(target, profile, rest string) string {
	return fmt.Sprintf("save \"char:eirik\" profile %s against %q %s;", profile, target, rest)
}

const sacredFlame = `stat "save:dexterity" dc 15 damage 2d8 kind "radiant" success none`

// The save is a stat check on the target's stat, with the law's die and the
// target's roll mode; the engine takes the faces the mode needs.
func TestSaveVerdict(t *testing.T) {
	_, w := saveWorld(t, "condition type \"state:paralyzed\" { instances coexist; modifier \"save:dexterity\" stat \"save:dexterity\" roll fail; }\ncondition type \"spell:haste\" { instances coexist; modifier \"save:dexterity\" stat \"save:dexterity\" roll advantage; }\ncharacter \"char:held\" named \"Held\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 9; resource \"hp\" = 20 min 0 max 20; }\ncondition \"char:held:paralyzed\" of \"state:paralyzed\" to \"char:held\";\ncharacter \"char:quick\" named \"Quick\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 0; resource \"hp\" = 20 min 0 max 20; }\ncondition \"char:quick:haste\" of \"spell:haste\" to \"char:quick\";\n")
	for i, tc := range []struct {
		name, target, profile, rest string
		roll                        core.RollMode
		faces, unused               []int64
		kept, total                 int64
		success                     bool
	}{
		{"reaches the DC", spider, `stat "save:dexterity" dc 15`, "faces 12", "", []int64{12}, nil, 12, 15, true},
		{"one short", spider, `stat "save:dexterity" dc 15`, "faces 11", "", []int64{11}, nil, 11, 14, false},
		{"a natural 20 is only a face", spider, `stat "save:dexterity" dc 30`, "faces 20", "", []int64{20}, nil, 20, 23, false},
		{"a natural 1 is only a face", spider, `stat "save:dexterity" dc 3`, "faces 1", "", []int64{1}, nil, 1, 4, true},
		{"a plain roll takes the first face", spider, `stat "save:dexterity" dc 15`, "faces 5 19", "", []int64{5}, []int64{19}, 5, 8, false},
		{"advantage asked", spider, `stat "save:dexterity" dc 15`, "advantage faces 5 19", core.Advantage, []int64{5, 19}, nil, 19, 22, true},
		{"disadvantage asked", spider, `stat "save:dexterity" dc 15`, "disadvantage faces 19 5", core.Disadvantage, []int64{19, 5}, nil, 5, 8, false},
		{"restrained", bandit, `stat "save:dexterity" dc 15`, "faces 18 7", core.Disadvantage, []int64{18, 7}, nil, 7, 8, false},
		{"restrained and advantage cancel", bandit, `stat "save:dexterity" dc 15`, "advantage faces 18 7", "", []int64{18}, []int64{7}, 18, 19, true},
		{"a Condition's advantage", "char:quick", `stat "save:dexterity" dc 15`, "faces 3 15", core.Advantage, []int64{3, 15}, nil, 15, 15, true},
		{"paralyzed fails without faces", "char:held", `stat "save:dexterity" dc 1`, "", core.AutoFail, nil, nil, 0, 0, false},
		{"paralyzed leaves the faces unused", "char:held", `stat "save:dexterity" dc 1`, "advantage faces 20 20", core.AutoFail, nil, []int64{20, 20}, 0, 0, false},
		{"another stat", "char:ada", `stat "save:constitution" dc 12`, "faces 10", "", []int64{10}, nil, 10, 12, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := attackRun(t, w, fmt.Sprint("v", i), save(tc.target, tc.profile, tc.rest))
			if err != nil {
				t.Fatal(err)
			}
			s := saved(b)
			if len(b.Events) != 1 || len(s) != 1 {
				t.Fatalf("%v", eventKinds(b))
			}
			c := s[0].Check
			if c.Roll != tc.roll || !reflect.DeepEqual(c.Faces, tc.faces) || !reflect.DeepEqual(s[0].UnusedSaveFaces, tc.unused) || c.Kept != tc.kept || *c.Success != tc.success || c.Intent.Sides != 20 || *c.Intent.DC != s[0].Intent.Profile.DC || s[0].Effect != "" {
				t.Fatalf("%+v", s[0])
			}
			if tc.roll == core.AutoFail {
				if c.Total != nil || c.Margin != nil {
					t.Fatalf("%+v", c)
				}
			} else if *c.Total != tc.total || *c.Margin != tc.total-*c.Intent.DC {
				t.Fatalf("%+v", c)
			}
		})
	}
	for _, tc := range []struct{ name, target, rest, expected string }{
		{"no face", spider, "", "1 face (normal)"},
		{"one face under disadvantage", bandit, "faces 18", "2 faces (disadvantage)"},
		{"one face with advantage asked", spider, "advantage faces 18", "2 faces (advantage)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rev := w.Revision()
			_, err := attackRun(t, w, "x-"+tc.name, save(tc.target, `stat "save:dexterity" dc 15`, tc.rest))
			if f := faultOf(t, err); f.Code != "E_ROLL" || f.Field != "faces" || f.Expected != tc.expected || w.Revision() != rev {
				t.Fatalf("%+v", f)
			}
		})
	}
}

// The effect on the damage, then the treatments: full on a failure, half or
// none on a success, the bonus before the halving.
func TestSaveEffect(t *testing.T) {
	extra := "character \"char:pale\" named \"Pale\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 0; resource \"hp\" = 100 min 0 max 100; immune \"radiant\"; vulnerable \"fire\"; }\n"
	for _, tc := range []struct {
		name, target, profile, rest string
		effect                      string
		damage                      *int64
		raw                         string
		treatments                  int
		unused                      []string
	}{
		{"failed, full", spider, sacredFlame, "faces 4 damage 13", "full", i64(13), "13", 0, nil},
		{"saved, none", spider, sacredFlame, "faces 19 damage 13", "none", nil, "", 0, []string{"damage"}},
		{"none never checks the total", spider, sacredFlame, "faces 19 damage 99", "none", nil, "", 0, []string{"damage"}},
		{"saved, half rounds down", spider, `stat "save:dexterity" dc 15 damage 3d6 kind "fire" success half`, "faces 19 damage 11", "half", i64(5), "11", 0, nil},
		{"failed, half is full", spider, `stat "save:dexterity" dc 15 damage 3d6 kind "fire" success half`, "faces 2 damage 11", "full", i64(11), "11", 0, nil},
		{"the bonus before the halving", spider, `stat "save:dexterity" dc 15 damage 1d6+3 kind "fire" success half`, "faces 19 damage 4", "half", i64(3), "7", 0, nil},
		{"a negative total is 0", spider, `stat "save:dexterity" dc 15 damage 1d4-5 kind "fire" success half`, "faces 2 damage 2", "full", i64(0), "-3", 0, nil},
		{"halved, then resisted", imp, `stat "save:dexterity" dc 13 damage 4d6 kind "fire" success half`, "faces 12 damage 13", "half", i64(3), "13", 1, nil},
		{"failed, then resisted", imp, `stat "save:dexterity" dc 13 damage 4d6 kind "fire" success half`, "faces 2 damage 13", "full", i64(6), "13", 1, nil},
		{"halved, then doubled", "char:pale", `stat "save:dexterity" dc 15 damage 3d6 kind "fire" success half`, "faces 20 damage 11", "half", i64(10), "11", 1, nil},
		{"immune", "char:pale", sacredFlame, "faces 2 damage 13", "full", i64(0), "13", 1, nil},
		{"scored only", spider, sacredFlame, "faces 4", "full", nil, "", 0, nil},
		{"scored only, saved", spider, `stat "save:dexterity" dc 15 damage 3d6 kind "fire" success half`, "faces 19", "half", nil, "", 0, nil},
		{"no damage profile", spider, `stat "save:dexterity" dc 15`, "faces 4", "", nil, "", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := saveWorld(t, extra)
			hp := currentOf(w, core.CharacterID(tc.target), "hp")
			b, err := attackRun(t, w, "e1", save(tc.target, tc.profile, tc.rest))
			if err != nil {
				t.Fatal(err)
			}
			s := saved(b)[0]
			if s.Effect != tc.effect || !reflect.DeepEqual(s.Damage, tc.damage) || s.RawDamage != tc.raw || len(s.Treatments) != tc.treatments || !reflect.DeepEqual(s.Unused, tc.unused) {
				t.Fatalf("%+v", s)
			}
			var lost int64
			if tc.damage != nil {
				lost = *tc.damage
				if s.DamageRolled == nil || fmt.Sprint(*s.DamageRolled) != strings.Fields(tc.rest)[3] {
					t.Fatalf("%+v", s)
				}
			}
			want := 1
			if lost > 0 {
				want = 2
			}
			if currentOf(w, core.CharacterID(tc.target), "hp") != hp-lost || len(b.Events) != want {
				t.Fatalf("hp %d, %v", currentOf(w, core.CharacterID(tc.target), "hp"), eventKinds(b))
			}
		})
	}
}

func i64(v int64) *int64 { return &v }

// The damage a save applies runs the same Damage: temporary hit points, then
// the hit points, then the concentration check on the final amount, with the
// save's own check faces.
func TestSaveDamagePath(t *testing.T) {
	const fire = `stat "save:dexterity" dc 13 damage 6d6 kind "fire" success half`
	for _, tc := range []struct {
		name, rest     string
		hp             int64
		dc             int64
		concentrating  bool
		unused         []core.AttackCheckFace
		checks, events int
	}{
		{"failed: 26, DC 13", `faces 4 damage 26 faces "char:ada:concentration" 11`, 3, 13, true, nil, 1, 3},
		{"failed: one short of DC 13", `faces 4 damage 26 faces "char:ada:concentration" 10`, 3, 13, false, nil, 1, 4},
		{"saved: 13, DC 10", `faces 15 damage 26 faces "char:ada:concentration" 8`, 16, 10, true, nil, 1, 3},
		{"saved: 3 to temporary hit points still checks", `faces 15 damage 6 faces "char:ada:concentration" 8`, 24, 10, true, nil, 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := saveWorld(t, "")
			b, err := attackRun(t, w, "d1", save("char:ada", fire, tc.rest))
			if err != nil {
				t.Fatal(err)
			}
			s := saved(b)[0]
			if currentOf(w, "char:ada", "hp") != tc.hp || currentOf(w, "char:ada", "temp-hp") != max(0, 5-*s.Damage) || !reflect.DeepEqual(s.UnusedFaces, tc.unused) || len(b.Events) != tc.events {
				t.Fatalf("hp %d, %+v, %v", currentOf(w, "char:ada", "hp"), s, eventKinds(b))
			}
			checks := 0
			for _, e := range b.Events {
				if e.Kind == core.StatCheckResolved {
					checks++
					if *e.StatCheck.Intent.DC != tc.dc {
						t.Fatalf("DC %d", *e.StatCheck.Intent.DC)
					}
				}
			}
			held := false
			for _, c := range w.Snapshot().Blueprint.Conditions {
				held = held || c.ID == "char:ada:concentration"
			}
			if checks != tc.checks || held != tc.concentrating {
				t.Fatalf("%d checks, concentrating %v", checks, held)
			}
		})
	}
	// A none success applies nothing, so every check face is unused.
	_, w := saveWorld(t, "")
	b, err := attackRun(t, w, "d2", save("char:ada", `stat "save:dexterity" dc 5 damage 2d8 kind "radiant" success none`, `faces 15 damage 9 faces "char:ada:concentration" 8`))
	if err != nil {
		t.Fatal(err)
	}
	if s := saved(b)[0]; len(b.Events) != 1 || len(s.UnusedFaces) != 1 || currentOf(w, "char:ada", "temp-hp") != 5 {
		t.Fatalf("%+v", s)
	}
}

// One roll for several targets: each saves on its own, in order, and one
// refusal refuses them all.
func TestSaveSeveralTargets(t *testing.T) {
	text, err := os.ReadFile("../examples/saves/burning-hands.nql")
	if err != nil {
		t.Fatal(err)
	}
	_, w := saveWorld(t, "")
	b, err := attackRun(t, w, "s1", string(text))
	if err != nil {
		t.Fatal(err)
	}
	s := saved(b)
	if len(s) != 2 || *s[0].Damage != 2 || *s[1].Damage != 10 || currentOf(w, imp, "hp") != 8 || currentOf(w, "char:ada", "hp") != 19 {
		t.Fatalf("%+v", s)
	}
	_, w = saveWorld(t, "")
	_, err = attackRun(t, w, "s2", string(text)+"\n"+save(spider, sacredFlame, "faces 2 damage 17"))
	if f := faultOf(t, err); f.Code != "E_SAVE" || f.Field != "damage" || f.Expected != "2..16" || w.Revision() != 0 || currentOf(w, imp, "hp") != 10 || currentOf(w, "char:ada", "temp-hp") != 5 {
		t.Fatalf("%+v", f)
	}
}

// Every refusal handed to the engine directly, each leaving no trace.
func TestSaveRefusals(t *testing.T) {
	_, w := saveWorld(t, "character \"char:ghost\" named \"Ghost\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 0; }\ncharacter \"char:huge\" named \"Huge\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 9223372036854775800; resource \"hp\" = 10 min 0 max 10; vulnerable \"fire\"; }\n")
	fire := func(success string) *core.SaveDamage {
		return &core.SaveDamage{Count: 2, Sides: 6, Kind: "fire", Success: success}
	}
	base := func() core.Save {
		return core.Save{Source: "char:eirik", Target: spider, Profile: core.SaveProfile{Stat: "save:dexterity", DC: 15, Damage: fire("half")}, Faces: []int64{4}, Damage: i64(7)}
	}
	for _, tc := range []struct {
		name                  string
		edit                  func(*core.Save)
		code, field, expected string
	}{
		{"0 dice", func(v *core.Save) { v.Profile.Damage.Count = 0 }, "E_SAVE", "damage dice", "1..64"},
		{"65 dice", func(v *core.Save) { v.Profile.Damage.Count = 65 }, "E_SAVE", "damage dice", "1..64"},
		{"0 sides", func(v *core.Save) { v.Profile.Damage.Sides = 0 }, "E_SAVE", "damage sides", "1..1000"},
		{"1001 sides", func(v *core.Save) { v.Profile.Damage.Sides = 1001 }, "E_SAVE", "damage sides", "1..1000"},
		{"a blank kind", func(v *core.Save) { v.Profile.Damage.Kind = "" }, "E_SAVE", "damage kind", "valid damage kind"},
		{"another effect", func(v *core.Save) { v.Profile.Damage.Success = "quarter" }, "E_SAVE", "success", "half or none"},
		{"full is not a success", func(v *core.Save) { v.Profile.Damage.Success = "full" }, "E_SAVE", "success", "half or none"},
		{"a total without a profile", func(v *core.Save) { v.Profile.Damage = nil }, "E_SAVE", "damage", "a damage profile"},
		{"a fail mode", func(v *core.Save) { v.Mode = core.AutoFail }, "E_SAVE", "mode", "omitted, advantage or disadvantage"},
		{"three faces", func(v *core.Save) { v.Faces = []int64{1, 2, 3} }, "E_SAVE", "faces", "at most 2 faces"},
		{"face 0", func(v *core.Save) { v.Faces = []int64{0} }, "E_SAVE", "faces", "faces in 1..20"},
		{"face 21", func(v *core.Save) { v.Faces = []int64{4, 21} }, "E_SAVE", "faces", "faces in 1..20"},
		{"a check face 0", func(v *core.Save) { v.CheckFaces = []core.AttackCheckFace{{Condition: "x", Face: 0}} }, "E_SAVE", "check faces", "valid instance and face 1..1000"},
		{"129 check faces", func(v *core.Save) { v.CheckFaces = make([]core.AttackCheckFace, 129) }, "E_SAVE", "check faces", "at most 128"},
		{"a total under its dice", func(v *core.Save) { v.Damage = i64(1) }, "E_SAVE", "damage", "2..12"},
		{"a total over its dice", func(v *core.Save) { v.Damage = i64(13) }, "E_SAVE", "damage", "2..12"},
		{"an unknown source", func(v *core.Save) { v.Source = "char:nobody" }, "E_REFERENCE", "reference", "character"},
		{"an unknown target", func(v *core.Save) { v.Target = "char:nobody" }, "E_REFERENCE", "reference", "character"},
		{"an undeclared stat", func(v *core.Save) { v.Profile.Stat = "save:wisdom" }, "E_STAT", "save:wisdom", "declared stat"},
		{"damage to a target without health", func(v *core.Save) { v.Target = "char:ghost" }, "E_RESOURCE", "hp", "declared resource"},
		{"a total past int64", func(v *core.Save) { v.Target = "char:huge"; v.Faces = []int64{20} }, "E_OVERFLOW", "save:dexterity", "int64 check total"},
		{"no face for a plain roll", func(v *core.Save) { v.Faces = nil }, "E_ROLL", "faces", "1 face (normal)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := base()
			tc.edit(&v)
			rev := w.Revision()
			_, err := w.Execute(core.Request{ID: "x-" + tc.name, ExpectedRevision: rev, Actor: ada, Operations: []core.Operation{{Save: &v}}})
			f := faultOf(t, err)
			if f.Code != tc.code || f.Field != tc.field || !strings.HasPrefix(f.Expected, tc.expected) || w.Revision() != rev {
				t.Fatalf("%+v", f)
			}
		})
	}
	// Past int64 after the vulnerability: a failed save on huge damage.
	_, w2 := saveWorld(t, "character \"char:frail\" named \"Frail\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 0; resource \"hp\" = 10 min 0 max 10; vulnerable \"fire\"; }\n")
	v := core.Save{Source: "char:eirik", Target: "char:frail", Profile: core.SaveProfile{Stat: "save:dexterity", DC: 15, Damage: &core.SaveDamage{Count: 1, Sides: 6, Bonus: math.MaxInt64 - 6, Kind: "fire", Success: "half"}}, Faces: []int64{2}, Damage: i64(6)}
	_, err := w2.Execute(core.Request{ID: "o1", ExpectedRevision: 0, Actor: ada, Operations: []core.Operation{{Save: &v}}})
	if f := faultOf(t, err); f.Code != "E_OVERFLOW" || f.Field != "damage" || w2.Revision() != 0 {
		t.Fatalf("%+v", f)
	}
	// Without a damage profile, a target needs no health Resource.
	v = core.Save{Source: "char:eirik", Target: "char:ghost", Profile: core.SaveProfile{Stat: "save:dexterity", DC: 15}, Faces: []int64{2}}
	if _, err := w.Execute(core.Request{ID: "ok1", ExpectedRevision: w.Revision(), Actor: ada, Operations: []core.Operation{{Save: &v}}}); err != nil {
		t.Fatal(err)
	}
	// A world without the attack law refuses a save.
	b, ds := compiler.CompileWorld("w.nql", "rules { transfer unequips; wear any; }\nlocation \"loc:a\" named \"A\";\ncharacter \"char:a\" named \"A\" at \"loc:a\" { stat \"save:dexterity\" = 0; resource \"hp\" = 5 min 0 max 5; }\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	nw, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	v = core.Save{Source: "char:a", Target: "char:a", Profile: core.SaveProfile{Stat: "save:dexterity", DC: 15}, Faces: []int64{2}}
	_, err = nw.Execute(core.Request{ID: "l1", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:a"}, Operations: []core.Operation{{Save: &v}}})
	if f := faultOf(t, err); f.Code != "E_SAVE" || f.Field != "law" || f.Expected != "world attacks law" {
		t.Fatalf("%+v", f)
	}
	if _, ds := compiler.CompileActions("a.nql", "save \"char:a\" profile stat \"save:dexterity\" dc 15 against \"char:a\" faces 2;", nw.Snapshot().Blueprint); len(ds) != 1 || ds[0].Code != "E_SAVE" || !strings.HasPrefix(ds[0].Actual, `"save"`) {
		t.Fatalf("%+v", ds)
	}
}

// Each compile fault is at its token.
func TestSaveCompileRefusals(t *testing.T) {
	_, w := saveWorld(t, "character \"char:ghost\" named \"Ghost\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 0; }\n")
	const dex = `stat "save:dexterity" dc 15`
	for _, tc := range []struct {
		name, actions, at, code string
	}{
		{"an undeclared stat", save(spider, `stat "save:wisdom" dc 15`, "faces 3"), "save:wisdom", "E_STAT"},
		{"0 dice", save(spider, dex+` damage 0d6 kind "fire" success half`, "faces 3"), "0", "E_SAVE"},
		{"0 sides", save(spider, dex+` damage 1d0 kind "fire" success half`, "faces 3"), "d0", "E_SAVE"},
		{"not dice sides", save(spider, dex+` damage 1x6 kind "fire" success half`, "faces 3"), "x6", "E_PARSE"},
		{"a blank kind", save(spider, dex+` damage 1d6 kind "" success half`, "faces 3"), "", "E_SAVE"},
		{"another effect", save(spider, dex+` damage 1d6 kind "fire" success quarter`, "faces 3"), "quarter", "E_PARSE"},
		{"no effect", save(spider, dex+` damage 1d6 kind "fire"`, "faces 3"), "against", "E_PARSE"},
		{"an unknown source", strings.Replace(save(spider, dex, "faces 3"), "char:eirik", "char:nobody", 1), "char:nobody", "E_REFERENCE"},
		{"an unknown target", save("char:nobody", dex, "faces 3"), "char:nobody", "E_REFERENCE"},
		{"a target without health", save("char:ghost", dex+` damage 1d6 kind "fire" success half`, "faces 3"), "char:ghost", "E_RESOURCE"},
		{"three faces", save(spider, dex, "faces 3 4 5"), "faces", "E_SAVE"},
		{"face 21", save(spider, dex, "advantage faces 3 21"), "21", "E_SAVE"},
		{"a total without a profile", save(spider, dex, "faces 3 damage 4"), "damage", "E_SAVE"},
		{"three check faces", save("char:ada", dex+` damage 1d6 kind "fire" success half`, `faces 3 damage 3 faces "char:ada:concentration" 1 2 3`), "faces", "E_ROLL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("actions.nql", tc.actions, w.Snapshot().Blueprint)
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.HasPrefix(ds[0].Actual, shownAt(tc.code, tc.at)) {
				t.Fatalf("%+v", ds)
			}
		})
	}
	// The compiled save carries every clause.
	ops, ds := compiler.CompileActions("a.nql", save("char:ada", dex+` damage 2d6-1 kind "fire" success none`, `disadvantage faces 3 4 damage 5 faces "char:ada:concentration" 7`), w.Snapshot().Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	want := core.Save{Source: "char:eirik", Target: "char:ada", Profile: core.SaveProfile{Stat: "save:dexterity", DC: 15, Damage: &core.SaveDamage{Count: 2, Sides: 6, Bonus: -1, Kind: "fire", Success: "none"}}, Mode: core.Disadvantage, Faces: []int64{3, 4}, Damage: i64(5), CheckFaces: []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 7}}}
	if len(ops) != 1 || !reflect.DeepEqual(*ops[0].Save, want) {
		t.Fatalf("%+v", ops[0].Save)
	}
}

// History replays from the event intents, check faces included, and every
// value handed in or out is detached.
func TestSaveReplayAndDetachment(t *testing.T) {
	b0, w := saveWorld(t, "")
	const fire = `stat "save:dexterity" dc 13 damage 6d6 kind "fire" success half`
	for i, a := range []string{
		save(spider, sacredFlame, "faces 19 damage 13"),
		save(spider, sacredFlame, "faces 3 20 damage 13"),
		save(bandit, sacredFlame, "faces 18 7"),
		save(imp, fire, "faces 12 damage 13"),
		// Two prerolled concentration faces: the first save breaks the
		// concentration, so the second save's face is unused.
		save("char:ada", fire, `faces 2 damage 26 faces "char:ada:concentration" 2`) + "\n" +
			save("char:ada", fire, `faces 2 damage 6 faces "char:ada:concentration" 18`),
		save("char:ada", `stat "save:constitution" dc 10`, "faces 9"),
	} {
		if _, err := attackRun(t, w, fmt.Sprint("r", i), a); err != nil {
			t.Fatal(err)
		}
	}
	h := w.History()
	last := saved(h[len(h)-2])
	if len(last) != 2 || last[0].UnusedFaces != nil || !reflect.DeepEqual(last[1].UnusedFaces, []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 18}}) {
		t.Fatalf("%+v", last)
	}
	r, err := core.Replay(b0, h)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(r.History(), w.History()) {
		t.Fatal("replay differs")
	}
	// The request and the receipt are copies.
	ops, _ := compiler.CompileActions("a.nql", save(spider, fire, "advantage faces 2 3 damage 9"), w.Snapshot().Blueprint)
	b, err := w.Execute(core.Request{ID: "r9", ExpectedRevision: w.Revision(), Actor: ada, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	ops[0].Save.Faces[0] = 19
	*ops[0].Save.Damage = 5
	ops[0].Save.Profile.Damage.Success = "none"
	s := saved(b)
	s[0].Intent.Faces[1] = 20
	*s[0].Intent.Damage = 6
	s[0].Intent.Profile.Damage.Kind = "cold"
	s[0].Check.Faces[0] = 20
	*s[0].Check.Success = true
	b.Events[len(b.Events)-1].Save.Intent.Faces[0] = 20
	b.Events[len(b.Events)-1].Save.Check.Faces[1] = 20
	h = w.History()
	got := h[len(h)-1].Events[len(h[len(h)-1].Events)-1].Save
	if !reflect.DeepEqual(got.Intent.Faces, []int64{2, 3}) || *got.Intent.Damage != 9 || got.Intent.Profile.Damage.Kind != "fire" || got.Intent.Profile.Damage.Success != "half" || !reflect.DeepEqual(got.Check.Faces, []int64{2, 3}) || *got.Check.Success || !reflect.DeepEqual(h[len(h)-1].Request.Operations[0].Save.Faces, []int64{2, 3}) {
		t.Fatalf("%+v", got)
	}
}

// modelSave is an independent statement of the save law with the example
// world's die: the target's mode sources, the faces, the verdict, the
// effect and the damage before the treatments.
type modelSaveOutcome struct {
	refused      bool
	kept, total  int64
	autoFail     bool
	success      bool
	effect       string
	damage       int64
	applied      bool
	unusedFaces  []int64
	unusedDamage bool
}

func modelSave(v core.Save, bonus int64, source core.RollMode) modelSaveOutcome {
	var out modelSaveOutcome
	d := v.Profile.Damage
	if d == nil && v.Damage != nil || len(v.Faces) > 2 {
		return modelSaveOutcome{refused: true}
	}
	for _, f := range v.Faces {
		if f < 1 || f > 20 {
			return modelSaveOutcome{refused: true}
		}
	}
	adv := v.Mode == core.Advantage || source == core.Advantage
	dis := v.Mode == core.Disadvantage || source == core.Disadvantage
	need := 1
	switch {
	case source == core.AutoFail:
		need = 0
	case adv != dis:
		need = 2
	}
	if len(v.Faces) < need {
		return modelSaveOutcome{refused: true}
	}
	out.unusedFaces = v.Faces[need:]
	switch need {
	case 0:
		out.autoFail = true
	case 1:
		out.kept = v.Faces[0]
	case 2:
		out.kept = min(v.Faces[0], v.Faces[1])
		if adv {
			out.kept = max(v.Faces[0], v.Faces[1])
		}
	}
	out.total = out.kept + bonus
	out.success = !out.autoFail && out.total >= v.Profile.DC
	if d == nil {
		return out
	}
	out.effect = "full"
	if out.success {
		out.effect = d.Success
	}
	if v.Damage == nil {
		return out
	}
	if out.effect == "none" {
		out.unusedDamage = true
		return out
	}
	if *v.Damage < d.Count || *v.Damage > d.Count*d.Sides {
		return modelSaveOutcome{refused: true}
	}
	out.applied = true
	out.damage = max(0, *v.Damage+d.Bonus)
	if out.effect == "half" {
		out.damage /= 2
	}
	return out
}

// The engine against the model on thousands of random typed saves, in one
// world, so every save also sees the earlier ones' damage.
func TestSaveRandomAgainstModel(t *testing.T) {
	_, w := saveWorld(t, "condition type \"state:paralyzed\" { instances coexist; modifier \"save:dexterity\" stat \"save:dexterity\" roll fail; }\ncondition type \"spell:haste\" { instances coexist; modifier \"save:dexterity\" stat \"save:dexterity\" roll advantage; }\ncondition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";\n"+
		"character \"char:t1\" named \"T1\" at \"loc:valley/B03\" { stat \"save:dexterity\" = -2; resource \"hp\" = 1000000000 min 0 max 1000000000; resist \"fire\"; }\n"+
		"character \"char:t2\" named \"T2\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 5; resource \"hp\" = 1000000000 min 0 max 1000000000; vulnerable \"fire\"; resist \"cold\"; }\ncondition \"t2:web\" of \"state:restrained\" to \"char:t2\";\ncondition \"t2:ward\" of \"spell:ward\" to \"char:t2\";\n"+
		"character \"char:t3\" named \"T3\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 9; resource \"hp\" = 1000000000 min 0 max 1000000000; immune \"poison\"; }\ncondition \"t3:held\" of \"state:paralyzed\" to \"char:t3\";\n"+
		"character \"char:t4\" named \"T4\" at \"loc:valley/B03\" { stat \"save:dexterity\" = 1; resource \"hp\" = 1000000000 min 0 max 1000000000; vulnerable \"cold\"; }\ncondition \"t4:haste\" of \"spell:haste\" to \"char:t4\";\n")
	ids := []core.CharacterID{"char:t1", "char:t2", "char:t3", "char:t4"}
	bonus := map[core.CharacterID]int64{"char:t1": -2, "char:t2": 5, "char:t3": 9, "char:t4": 1}
	source := map[core.CharacterID]core.RollMode{"char:t2": core.Disadvantage, "char:t3": core.AutoFail, "char:t4": core.Advantage}
	// The targets' treatments by kind, own and held: resistance, vulnerability, immunity.
	traits := map[core.CharacterID]map[string][3]bool{"char:t1": {"fire": {true, false, false}}, "char:t2": {"fire": {true, true, false}, "cold": {true, false, false}}, "char:t3": {"poison": {false, false, true}}, "char:t4": {"cold": {false, true, false}}}
	kinds := []string{"slashing", "fire", "cold", "poison"}
	health := map[core.CharacterID]int64{"char:t1": 1e9, "char:t2": 1e9, "char:t3": 1e9, "char:t4": 1e9}
	rng := rand.New(rand.NewPCG(13, 17))
	tally := map[string]int{}
	for i := range 4000 {
		v := core.Save{Source: "char:eirik", Target: ids[rng.IntN(4)], Profile: core.SaveProfile{Stat: "save:dexterity", DC: rng.Int64N(31) - 5}}
		if rng.IntN(7) != 0 {
			v.Profile.Damage = &core.SaveDamage{Count: rng.Int64N(4) + 1, Sides: rng.Int64N(12) + 1, Bonus: rng.Int64N(13) - 6, Kind: kinds[rng.IntN(4)], Success: []string{"half", "none"}[rng.IntN(2)]}
		}
		v.Mode = []core.RollMode{"", core.Advantage, core.Disadvantage}[rng.IntN(3)]
		for range []int{0, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 3}[rng.IntN(20)] {
			f := rng.Int64N(20) + 1
			if rng.IntN(60) == 0 {
				f = []int64{0, 21}[rng.IntN(2)]
			}
			v.Faces = append(v.Faces, f)
		}
		if d := v.Profile.Damage; d != nil && rng.IntN(4) != 0 {
			total := d.Count + rng.Int64N(d.Count*d.Sides-d.Count+1)
			if rng.IntN(25) == 0 {
				total = []int64{d.Count - 1, d.Count*d.Sides + 1}[rng.IntN(2)]
			}
			v.Damage = &total
		} else if d == nil && rng.IntN(20) == 0 {
			v.Damage = i64(3)
		}
		want := modelSave(v, bonus[v.Target], source[v.Target])
		if want.applied && v.Profile.Damage != nil {
			tr := traits[v.Target][v.Profile.Damage.Kind]
			if tr[0] {
				want.damage /= 2
			}
			if tr[1] {
				want.damage *= 2
			}
			if tr[2] {
				want.damage = 0
			}
		}
		b, err := w.Execute(core.Request{ID: fmt.Sprint("r", i), ExpectedRevision: w.Revision(), Actor: ada, Operations: []core.Operation{{Save: &v}}})
		if want.refused {
			tally["refused"]++
			if err == nil {
				t.Fatalf("%d: %+v accepted, model refuses", i, v)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%d: %+v: %v", i, v, err)
		}
		got := saved(b)[0]
		c := got.Check
		var unusedFaces []int64
		if len(want.unusedFaces) > 0 {
			unusedFaces = want.unusedFaces
		}
		if *c.Success != want.success || (c.Roll == core.AutoFail) != want.autoFail || !want.autoFail && (c.Kept != want.kept || *c.Total != want.total) || got.Effect != want.effect || !reflect.DeepEqual(got.UnusedSaveFaces, unusedFaces) || (got.Damage != nil) != want.applied || (len(got.Unused) == 1) != want.unusedDamage {
			t.Fatalf("%d: %+v\nengine %+v\nmodel  %+v", i, v, got, want)
		}
		if want.applied {
			if *got.Damage != want.damage {
				t.Fatalf("%d: damage %d, model %d", i, *got.Damage, want.damage)
			}
			health[v.Target] -= want.damage
			tally[want.effect]++
			if traits[v.Target][v.Profile.Damage.Kind] != [3]bool{} {
				tally["treated"]++
			}
		}
		switch {
		case want.autoFail:
			tally["automatic failure"]++
		case want.success:
			tally["success"]++
		default:
			tally["failure"]++
		}
		if want.unusedDamage {
			tally["none"]++
		}
		if currentOf(w, v.Target, "hp") != health[v.Target] {
			t.Fatalf("%d: hp %d, model %d", i, currentOf(w, v.Target, "hp"), health[v.Target])
		}
	}
	for _, k := range []string{"refused", "full", "half", "none", "treated", "automatic failure", "success", "failure"} {
		if tally[k] < 40 {
			t.Fatalf("too few %s: %v", k, tally)
		}
	}
	t.Logf("%v", tally)
}
