package tests

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

const goblin = "occ:valley/B03/goblin-1"

// attackWorld is the attack example world, a map and no Battle, with extra
// declarations.
func attackWorld(t testing.TB, extra string) (core.Blueprint, *core.World) {
	t.Helper()
	text, err := os.ReadFile("../examples/attacks/world.nql")
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

// attackRun compiles actions against the world and executes them as one
// request.
func attackRun(t testing.TB, w *core.World, id, actions string) (core.Batch, error) {
	t.Helper()
	ops, ds := compiler.CompileActions("actions.nql", actions, w.Snapshot().Blueprint)
	if len(ds) > 0 {
		t.Fatalf("%s: %v", actions, ds)
	}
	return w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: ada, Operations: ops})
}

func scored(b core.Batch) []core.AttackResult {
	var out []core.AttackResult
	for _, e := range b.Events {
		if e.Kind == core.AttackScored {
			out = append(out, *e.Attack)
		}
	}
	return out
}

func eventKinds(b core.Batch) []core.EventKind {
	var out []core.EventKind
	for _, e := range b.Events {
		out = append(out, e.Kind)
	}
	return out
}

func currentOf(w *core.World, id core.CharacterID, r core.ResourceID) int64 {
	for _, c := range w.Snapshot().Characters {
		if c.ID == id {
			return c.Resources[r].Current
		}
	}
	return -1
}

func swing(by, target, profile, rest string) string {
	return fmt.Sprintf("attack %q profile %s against %q %s;", by, profile, target, rest)
}

func faultOf(t testing.TB, err error) *core.Fault {
	t.Helper()
	var f *core.Fault
	if !errors.As(err, &f) {
		t.Fatalf("not a fault: %v", err)
	}
	return f
}

func TestAttackLaw(t *testing.T) {
	text, err := os.ReadFile("../examples/attacks/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	law := `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 2; }`
	if !strings.Contains(string(text), law) {
		t.Fatal("the example world's law changed")
	}
	for _, tc := range []struct {
		name, law, at, code, field string
	}{
		{"die 2", `attacks { defense "ac"; health "hp"; die 2; miss 1; critical 2; critical dice 2; }`, "", "", ""},
		{"die 1000", `attacks { defense "ac"; health "hp"; die 1000; miss 1; critical 1000; critical dice 2; }`, "", "", ""},
		{"die 1", `attacks { defense "ac"; health "hp"; die 1; miss 1; critical 1; critical dice 2; }`, "1", "E_ATTACK", "die"},
		{"die 1001", `attacks { defense "ac"; health "hp"; die 1001; miss 1; critical 20; critical dice 2; }`, "1001", "E_ATTACK", "die"},
		{"miss 0", `attacks { defense "ac"; health "hp"; die 20; miss 0; critical 20; critical dice 2; }`, "0", "E_ATTACK", "miss"},
		{"miss above the die", `attacks { defense "ac"; health "hp"; die 20; miss 21; critical 20; critical dice 2; }`, "21", "E_ATTACK", "miss"},
		{"critical 0", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 0; critical dice 2; }`, "0", "E_ATTACK", "critical"},
		{"critical above the die", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 21; critical dice 2; }`, "21", "E_ATTACK", "critical"},
		{"critical is the miss face", `attacks { defense "ac"; health "hp"; die 20; miss 20; critical 20; critical dice 2; }`, "20", "E_ATTACK", "critical"},
		{"critical dice 1", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 1; }`, "", "", ""},
		{"critical dice 8", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 8; }`, "", "", ""},
		{"critical dice 0", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 0; }`, "0", "E_ATTACK", "critical dice"},
		{"critical dice 9", `attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 9; }`, "9", "E_ATTACK", "critical dice"},
		{"blank defense", `attacks { defense " "; health "hp"; die 20; miss 1; critical 20; critical dice 2; }`, " ", "E_ATTACK", "defense"},
		{"blank health", `attacks { defense "ac"; health ""; die 20; miss 1; critical 20; critical dice 2; }`, "", "E_ATTACK", "health"},
		{"two laws", law + "\n" + law, "attacks", "E_DUPLICATE", ""},
		{"out of order", `attacks { health "hp"; defense "ac"; die 20; miss 1; critical 20; critical dice 2; }`, "health", "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld("world.nql", strings.Replace(string(text), law, tc.law, 1))
			if tc.code == "" {
				if len(ds) > 0 {
					t.Fatal(ds)
				}
				return
			}
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.HasPrefix(ds[0].Actual, shownAt(tc.code, tc.at)) || ds[0].Line != 5 && tc.code != "E_DUPLICATE" {
				t.Fatalf("%+v", ds)
			}
			if tc.field != "" && !strings.Contains(ds[0].Message, tc.field) {
				t.Fatalf("%+v", ds)
			}
		})
	}
	// Battle keeps its own law.
	battle, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	_, ds := compiler.CompileWorld("world.nql", string(battle)+law+"\n")
	if len(ds) != 1 || ds[0].Code != "E_ATTACK" || !strings.HasPrefix(ds[0].Actual, `"attacks"`) {
		t.Fatalf("%+v", ds)
	}
}

func TestAttackScoring(t *testing.T) {
	for _, tc := range []struct {
		rest                string
		toHit, kept, total  int64
		hit, critical, miss bool
	}{
		{"faces 17", 4, 17, 21, true, false, false},
		{"faces 11", 4, 11, 15, true, false, false}, // the total equals the defense
		{"faces 10", 4, 10, 14, false, false, false},
		{"faces 1", 100, 1, 101, false, false, true},
		{"faces 20", -100, 20, -80, true, true, false},
		{"faces 2", 13, 2, 15, true, false, false},
		{"advantage faces 3 18", 0, 18, 18, true, false, false},
		{"advantage faces 18 3", 0, 18, 18, true, false, false},
		{"advantage faces 7 7", 0, 7, 7, false, false, false},
		{"disadvantage faces 3 18", 0, 3, 3, false, false, false},
		{"disadvantage faces 18 3", 0, 3, 3, false, false, false},
		{"disadvantage faces 20 1", 50, 1, 51, false, false, true},
		{"advantage faces 1 20", -50, 20, -30, true, true, false},
	} {
		t.Run(tc.rest, func(t *testing.T) {
			_, w := attackWorld(t, "")
			b, err := attackRun(t, w, "r1", swing("char:ada", goblin, fmt.Sprintf(`to_hit %d damage 1d8 kind "piercing"`, tc.toHit), tc.rest))
			if err != nil {
				t.Fatal(err)
			}
			s := scored(b)
			if len(b.Events) != 1 || len(s) != 1 {
				t.Fatalf("%v", eventKinds(b))
			}
			r := s[0]
			if r.Kept != tc.kept || r.Total != tc.total || r.Defense != 15 || r.Hit != tc.hit || r.Critical != tc.critical || r.NaturalMiss != tc.miss || r.Damage != nil || r.DamageRolled != nil || len(r.Unused) != 0 {
				t.Fatalf("%+v", r)
			}
			if currentOf(w, goblin, "hp") != 7 {
				t.Fatal("score-only applied damage")
			}
		})
	}
}

// The defense is the target's effective value: a Condition that raises it
// turns a hit into a miss.
func TestAttackEffectiveDefense(t *testing.T) {
	_, w := attackWorld(t, "condition type \"spell:shield\" { instances unique; modifier \"ac\" stat \"ac\" add 5; }\ncondition \"goblin:shield\" of \"spell:shield\" to \""+goblin+"\";\n")
	b, err := attackRun(t, w, "r1", swing("char:ada", goblin, `to_hit 4 damage 1d8 kind "piercing"`, "faces 11 damage 3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := scored(b)[0]; r.Defense != 20 || r.Hit || r.Damage != nil || !reflect.DeepEqual(r.Unused, []string{"damage"}) {
		t.Fatalf("%+v", r)
	}
}

func TestAttackDamage(t *testing.T) {
	ten := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		name, profile, rest string
		requested           *int64
		rolled              int64
		count               int64
		raw                 string
		unused              []string
		hpAfter             int64
		code, field, expect string
	}{
		{"lowest total", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 damage 1", ten(1), 1, 1, "1", nil, 6, "", "", ""},
		{"highest total", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 damage 6", ten(6), 6, 1, "6", nil, 1, "", "", ""},
		{"total below the dice", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 damage 0", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "1..6"},
		{"total above the dice", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 damage 7", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "1..6"},
		{"critical lowest", `to_hit 0 damage 1d6 kind "slashing"`, "faces 20 critical damage 2", ten(2), 2, 2, "2", nil, 5, "", "", ""},
		{"critical highest", `to_hit 0 damage 1d6 kind "slashing"`, "faces 20 critical damage 12", ten(12), 12, 2, "12", nil, 0, "", "", ""},
		{"critical below", `to_hit 0 damage 1d6 kind "slashing"`, "faces 20 critical damage 1", nil, 0, 0, "", nil, 7, "E_ATTACK", "critical_damage", "2..12"},
		{"critical above", `to_hit 0 damage 1d6 kind "slashing"`, "faces 20 critical damage 13", nil, 0, 0, "", nil, 7, "E_ATTACK", "critical_damage", "2..12"},
		{"both on a hit", `to_hit 0 damage 1d6+2 kind "slashing"`, "faces 17 damage 3 critical damage 9", ten(5), 3, 1, "5", []string{"critical_damage"}, 2, "", "", ""},
		{"both on a critical", `to_hit 0 damage 1d6+2 kind "slashing"`, "faces 20 damage 3 critical damage 4", ten(6), 4, 2, "6", []string{"damage"}, 1, "", "", ""},
		{"both on a miss", `to_hit 0 damage 1d6+2 kind "slashing"`, "faces 2 damage 3 critical damage 9", nil, 0, 0, "", []string{"damage", "critical_damage"}, 7, "", "", ""},
		{"an unused total out of range", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 damage 3 critical damage 999", ten(3), 3, 1, "3", []string{"critical_damage"}, 4, "", "", ""},
		{"a miss with an absurd total", `to_hit 0 damage 1d6 kind "slashing"`, "faces 1 damage 999", nil, 0, 0, "", []string{"damage"}, 7, "", "", ""},
		{"a hit with only the critical total", `to_hit 0 damage 1d6 kind "slashing"`, "faces 17 critical damage 9", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "damage"},
		{"a critical with only the hit total", `to_hit 0 damage 1d6 kind "slashing"`, "faces 20 damage 3", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "critical_damage"},
		{"a negative raw damage is 0", `to_hit 0 damage 1d6-5 kind "slashing"`, "faces 17 damage 2", ten(0), 2, 1, "-3", nil, 7, "", "", ""},
		{"a bonus is added once on a critical", `to_hit 0 damage 2d4+3 kind "slashing"`, "faces 20 critical damage 4", ten(7), 4, 4, "7", nil, 0, "", "", ""},
		{"flat damage", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 17 damage 0", ten(4), 0, 0, "4", nil, 3, "", "", ""},
		{"flat damage on a critical", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 20 critical damage 0", ten(4), 0, 0, "4", nil, 3, "", "", ""},
		{"flat damage with both totals", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 20 damage 0 critical damage 0", ten(4), 0, 0, "4", []string{"damage"}, 3, "", "", ""},
		{"flat damage scored only", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 17", nil, 0, 0, "", nil, 7, "", "", ""},
		{"flat damage without a bonus", `to_hit 0 damage flat kind "bludgeoning"`, "faces 17 damage 0", ten(0), 0, 0, "0", nil, 7, "", "", ""},
		{"a negative flat damage is 0", `to_hit 0 damage flat-2 kind "bludgeoning"`, "faces 17 damage 0", ten(0), 0, 0, "-2", nil, 7, "", "", ""},
		{"a flat total above 0", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 17 damage 1", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "0..0"},
		{"a flat critical total above 0", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 20 critical damage 1", nil, 0, 0, "", nil, 7, "E_ATTACK", "critical_damage", "0..0"},
		{"a flat hit with only the critical total", `to_hit 0 damage flat+4 kind "bludgeoning"`, "faces 17 critical damage 0", nil, 0, 0, "", nil, 7, "E_ATTACK", "damage", "damage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := attackWorld(t, "")
			b, err := attackRun(t, w, "r1", swing("char:ada", goblin, tc.profile, tc.rest))
			if tc.code != "" {
				f := faultOf(t, err)
				if f.Code != tc.code || f.Field != tc.field || f.Expected != tc.expect || w.Revision() != 0 || currentOf(w, goblin, "hp") != 7 {
					t.Fatalf("%+v revision %d", f, w.Revision())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := scored(b)[0]
			if !reflect.DeepEqual(r.Damage, tc.requested) || !reflect.DeepEqual(r.Unused, tc.unused) || r.RawDamage != tc.raw || r.DamageCount != tc.count {
				t.Fatalf("%+v", r)
			}
			if tc.requested != nil && *r.DamageRolled != tc.rolled {
				t.Fatalf("%+v", r)
			}
			if got := currentOf(w, goblin, "hp"); got != tc.hpAfter {
				t.Fatalf("hp %d", got)
			}
			// Damage is applied by the Damage operation, before the scored
			// event that closes the operation.
			want := []core.EventKind{core.AttackScored}
			if tc.requested != nil && *tc.requested > 0 {
				want = []core.EventKind{core.DamageApplied, core.AttackScored}
				if got := b.Events[0].Resource; got.Requested != *tc.requested || got.Resource != "hp" || got.Character != goblin {
					t.Fatalf("%+v", got)
				}
			}
			if !reflect.DeepEqual(eventKinds(b), want) {
				t.Fatalf("%v", eventKinds(b))
			}
		})
	}
}

// A swing's damage takes the buffer first, reaches the minimum, ends the
// Conditions that end there, and triggers the checks a Damage triggers, with
// the host's faces.
func TestAttackDamagePath(t *testing.T) {
	var b core.Batch
	_, w := attackWorld(t, "condition type \"state:maintaining\" { instances unique; ends_at_minimum \"hp\"; }\ncondition \"goblin:maintaining\" of \"state:maintaining\" to \""+goblin+"\";\n")
	// A check that runs without faces in a world without dice is refused.
	_, err := attackRun(t, w, "r0", swing(goblin, "char:ada", `to_hit 4 damage 1d8+2 kind "slashing"`, `faces 17 damage 5`))
	if f := faultOf(t, err); f.Code != "E_ROLL" || f.Subject != "char:ada:concentration" || w.Revision() != 0 {
		t.Fatal(f)
	}
	// 1d8+2 rolled 5: 7 damage; 5 temp hp first, then 2 hp. 7 / 2 is below 10,
	// so the check is DC 10; face 7 + 2 = 9 fails and the concentration ends.
	b, err = attackRun(t, w, "r1", swing(goblin, "char:ada", `to_hit 4 damage 1d8+2 kind "slashing"`, `faces 17 damage 5 faces "char:ada:concentration" 7`))
	if err != nil {
		t.Fatal(err)
	}
	if got := eventKinds(b); !reflect.DeepEqual(got, []core.EventKind{core.StatCheckResolved, core.ConditionRemoved, core.DamageApplied, core.AttackScored}) {
		t.Fatalf("%v", got)
	}
	check := b.Events[0].StatCheck
	if *check.Intent.DC != 10 || check.Kept != 7 || *check.Success || check.Damage != 7 {
		t.Fatalf("%+v", check)
	}
	if !b.Events[1].Condition.CheckFailed || b.Events[1].Condition.Before.ID != "char:ada:concentration" {
		t.Fatalf("%+v", b.Events[1].Condition)
	}
	if d := b.Events[2].Resource; d.Buffer == nil || d.Buffer.Absorbed != 5 || d.After.Current != 22 || currentOf(w, "char:ada", "temp-hp") != 0 {
		t.Fatalf("%+v", d)
	}
	// Check faces no check takes are unused, never refused: on a miss, and
	// on a hit after the concentration has ended.
	unused := []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 7}}
	for i, faces := range []string{"faces 2", "faces 15"} {
		b, err = attackRun(t, w, fmt.Sprint("r2-", i), swing(goblin, "char:ada", `to_hit 4 damage 1d8+2 kind "slashing"`, faces+` damage 5 faces "char:ada:concentration" 7`))
		if err != nil {
			t.Fatal(err)
		}
		if r := scored(b)[0]; !reflect.DeepEqual(r.UnusedFaces, unused) || (i == 1) != (r.Damage != nil) {
			t.Fatalf("%+v", r)
		}
	}
	if currentOf(w, "char:ada", "hp") != 15 {
		t.Fatal(currentOf(w, "char:ada", "hp"))
	}
	// A critical to 0 hp ends the Condition that ends at the minimum.
	b, err = attackRun(t, w, "r3", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind "piercing"`, `faces 20 critical damage 8`))
	if err != nil {
		t.Fatal(err)
	}
	if got := eventKinds(b); !reflect.DeepEqual(got, []core.EventKind{core.ConditionRemoved, core.DamageApplied, core.AttackScored}) || currentOf(w, goblin, "hp") != 0 {
		t.Fatalf("%v", got)
	}
}

// Several attacks in one request apply in order and atomically: the second
// sees the first's damage, a swing at 0 hp scores and changes nothing, and
// one refused swing refuses the request.
func TestAttackMultiattack(t *testing.T) {
	_, w := attackWorld(t, "")
	claw := `to_hit 3 damage 1d4+1 kind "slashing"`
	b, err := attackRun(t, w, "r1", swing("char:ada", goblin, claw, "faces 15 damage 4")+"\n"+swing("char:ada", goblin, claw, "faces 12 damage 3")+"\n"+swing("char:ada", goblin, claw, "faces 18 damage 1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := eventKinds(b); !reflect.DeepEqual(got, []core.EventKind{core.DamageApplied, core.AttackScored, core.DamageApplied, core.AttackScored, core.DamageApplied, core.AttackScored}) {
		t.Fatalf("%v", got)
	}
	if b.Events[0].Resource.After.Current != 2 || b.Events[2].Resource.Before.Current != 2 || b.Events[2].Resource.After.Current != 0 {
		t.Fatalf("%+v %+v", b.Events[0].Resource, b.Events[2].Resource)
	}
	if r := b.Events[4].Resource; r.Requested != 2 || r.Before.Current != 0 || r.After.Current != 0 {
		t.Fatalf("%+v", r)
	}
	for i, e := range b.Events {
		if e.Operation != i/2 {
			t.Fatalf("event %d operation %d", i, e.Operation)
		}
	}
	// Prerolled faces for every swing: the first swing breaks the
	// concentration, so the second's face is unused.
	_, w = attackWorld(t, "")
	scimitar := `to_hit 4 damage 1d6+2 kind "slashing"`
	b, err = attackRun(t, w, "r1", swing(goblin, "char:ada", scimitar, `faces 17 damage 6 faces "char:ada:concentration" 3`)+"\n"+swing(goblin, "char:ada", scimitar, `faces 18 damage 6 faces "char:ada:concentration" 19`))
	if err != nil {
		t.Fatal(err)
	}
	if s := scored(b); s[0].UnusedFaces != nil || !reflect.DeepEqual(s[1].UnusedFaces, []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 19}}) || currentOf(w, "char:ada", "hp") != 13 {
		t.Fatalf("%+v", s)
	}
	_, w = attackWorld(t, "")
	_, err = attackRun(t, w, "r1", swing("char:ada", goblin, claw, "faces 15 damage 4")+"\n"+swing("char:ada", goblin, claw, "faces 15 damage 9"))
	if f := faultOf(t, err); f.Code != "E_ATTACK" || w.Revision() != 0 || currentOf(w, goblin, "hp") != 7 {
		t.Fatal(f)
	}
}

// Every refusal leaves the world as it was.
func TestAttackRefusalsLeaveNoTrace(t *testing.T) {
	b0, _ := attackWorld(t, "character \"char:ghost\" named \"Ghost\" at \"loc:valley/B03\" { resource \"hp\" = 3 min 0 max 3; }\ncharacter \"char:wisp\" named \"Wisp\" at \"loc:valley/B03\" { stat \"ac\" = 3; }\n")
	good := core.Attack{Attacker: "char:ada", Target: goblin, Profile: core.AttackProfile{ToHit: 4, DamageCount: 1, DamageSides: 6, DamageKind: "slashing"}, Faces: []int64{17}}
	with := func(f func(*core.Attack)) core.Operation {
		v := good
		v.Faces = append([]int64(nil), good.Faces...)
		f(&v)
		return core.Operation{Attack: &v}
	}
	total := func(n int64) *int64 { return &n }
	// Without the law, the example's damage traits go too: they need it.
	noLaw := b0
	noLaw.Attacks, noLaw.DamageTraits = nil, nil
	noLaw.Characters = append([]core.Character(nil), b0.Characters...)
	for i := range noLaw.Characters {
		noLaw.Characters[i].DamageTraits = nil
	}
	for _, tc := range []struct {
		name  string
		b     core.Blueprint
		op    core.Operation
		code  string
		field string
	}{
		{"no law", noLaw, with(func(*core.Attack) {}), "E_ATTACK", "law"},
		{"an unknown attacker", b0, with(func(v *core.Attack) { v.Attacker = "char:nobody" }), "E_REFERENCE", ""},
		{"an unknown target", b0, with(func(v *core.Attack) { v.Target = "char:nobody" }), "E_REFERENCE", ""},
		{"a target without the defense stat", b0, with(func(v *core.Attack) { v.Target = "char:ghost" }), "E_STAT", "ac"},
		{"a target without health", b0, with(func(v *core.Attack) { v.Target = "char:wisp" }), "E_RESOURCE", "hp"},
		{"two faces without a mode", b0, with(func(v *core.Attack) { v.Faces = []int64{3, 4} }), "E_ATTACK", "faces"},
		{"one face with a mode", b0, with(func(v *core.Attack) { v.Mode = core.Advantage }), "E_ATTACK", "faces"},
		{"no faces", b0, with(func(v *core.Attack) { v.Faces = nil }), "E_ATTACK", "faces"},
		{"face 0", b0, with(func(v *core.Attack) { v.Faces = []int64{0} }), "E_ATTACK", "faces"},
		{"face 21", b0, with(func(v *core.Attack) { v.Faces = []int64{21} }), "E_ATTACK", "faces"},
		{"an automatic failure mode", b0, with(func(v *core.Attack) { v.Mode = core.AutoFail; v.Faces = []int64{3, 4} }), "E_ATTACK", "mode"},
		{"0 dice", b0, with(func(v *core.Attack) { v.Profile.DamageCount = 0 }), "E_ATTACK", "damage dice"},
		{"-1 dice of 0 sides", b0, with(func(v *core.Attack) { v.Profile.DamageCount, v.Profile.DamageSides = -1, 0 }), "E_ATTACK", "damage dice"},
		{"0 dice of -1 sides", b0, with(func(v *core.Attack) { v.Profile.DamageCount, v.Profile.DamageSides = 0, -1 }), "E_ATTACK", "damage dice"},
		{"0 dice of 1 side", b0, with(func(v *core.Attack) { v.Profile.DamageCount, v.Profile.DamageSides = 0, 1 }), "E_ATTACK", "damage dice"},
		{"a flat total of 1", b0, with(func(v *core.Attack) { v.Profile.DamageCount, v.Profile.DamageSides, v.Damage = 0, 0, total(1) }), "E_ATTACK", "damage"},
		{"a flat blank kind", b0, with(func(v *core.Attack) { v.Profile.DamageCount, v.Profile.DamageSides, v.Profile.DamageKind = 0, 0, "" }), "E_ATTACK", "damage kind"},
		{"65 dice", b0, with(func(v *core.Attack) { v.Profile.DamageCount = 65 }), "E_ATTACK", "damage dice"},
		{"0 sides", b0, with(func(v *core.Attack) { v.Profile.DamageSides = 0 }), "E_ATTACK", "damage sides"},
		{"1001 sides", b0, with(func(v *core.Attack) { v.Profile.DamageSides = 1001 }), "E_ATTACK", "damage sides"},
		{"a blank kind", b0, with(func(v *core.Attack) { v.Profile.DamageKind = " " }), "E_ATTACK", "damage kind"},
		{"a total out of range", b0, with(func(v *core.Attack) { v.Damage = total(7) }), "E_ATTACK", "damage"},
		{"a hit with only the other total", b0, with(func(v *core.Attack) { v.CriticalDamage = total(4) }), "E_ATTACK", "damage"},
		{"a total past int64", b0, with(func(v *core.Attack) { v.Profile.ToHit = math.MaxInt64 }), "E_OVERFLOW", "attack total"},
		{"damage past int64", b0, with(func(v *core.Attack) { v.Damage = total(6); v.Profile.DamageBonus = math.MaxInt64 }), "E_OVERFLOW", "damage"},
		{"a check face off any die", b0, with(func(v *core.Attack) {
			v.CheckFaces = []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 1001}}
		}), "E_ATTACK", "check faces"},
		{"a blank check instance", b0, with(func(v *core.Attack) { v.CheckFaces = []core.AttackCheckFace{{Condition: " ", Face: 3}} }), "E_ATTACK", "check faces"},
		{"129 check faces", b0, with(func(v *core.Attack) {
			v.CheckFaces = make([]core.AttackCheckFace, 129)
			for i := range v.CheckFaces {
				v.CheckFaces[i] = core.AttackCheckFace{Condition: "char:ada:concentration", Face: 3}
			}
		}), "E_ATTACK", "check faces"},
		{"condition rolls beside the attack", b0, func() core.Operation {
			o := with(func(v *core.Attack) { v.Target = "char:ada" })
			o.ConditionRolls = []core.ConditionRollInput{{Condition: "char:ada:concentration", Target: "char:ada", Face: 9}}
			return o
		}(), "E_BATTLE", "condition rolls"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := core.NewWorld(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			before := w.Snapshot()
			_, err = w.Execute(core.Request{ID: "r1", ExpectedRevision: 0, Actor: ada, Operations: []core.Operation{tc.op}})
			f := faultOf(t, err)
			if f.Code != tc.code || tc.field != "" && f.Field != tc.field {
				t.Fatalf("%+v", f)
			}
			if !reflect.DeepEqual(w.Snapshot(), before) || len(w.History()) != 0 {
				t.Fatal("a refusal left a trace")
			}
		})
	}
}

func TestAttackCompileRefusals(t *testing.T) {
	_, w := attackWorld(t, "character \"char:ghost\" named \"Ghost\" at \"loc:valley/B03\" { resource \"hp\" = 3 min 0 max 3; }\n")
	for _, tc := range []struct {
		name, actions, at, code string
	}{
		{"0 dice", swing("char:ada", goblin, `to_hit 0 damage 0d6 kind "slashing"`, "faces 3"), "0", "E_ATTACK"},
		{"0 sides", swing("char:ada", goblin, `to_hit 0 damage 1d0 kind "slashing"`, "faces 3"), "d0", "E_ATTACK"},
		{"flat with dice", swing("char:ada", goblin, `to_hit 0 damage flat 1d6 kind "slashing"`, "faces 3"), "1", "E_PARSE"},
		{"an unsigned flat bonus", swing("char:ada", goblin, `to_hit 0 damage flat 4 kind "slashing"`, "faces 3"), "4", "E_PARSE"},
		{"flat as dice sides", swing("char:ada", goblin, `to_hit 0 damage 1flat kind "slashing"`, "faces 3"), "flat", "E_PARSE"},
		{"a flat blank kind", swing("char:ada", goblin, `to_hit 0 damage flat+4 kind ""`, "faces 3"), "", "E_ATTACK"},
		{"sides past int64", swing("char:ada", goblin, `to_hit 0 damage 1d99999999999999999999 kind "slashing"`, "faces 3"), "d99999999999999999999", "E_INTEGER"},
		{"not dice sides", swing("char:ada", goblin, `to_hit 0 damage 1x6 kind "slashing"`, "faces 3"), "x6", "E_PARSE"},
		{"an unsigned bonus", swing("char:ada", goblin, `to_hit 0 damage 1d6 2 kind "slashing"`, "faces 3"), "2", "E_PARSE"},
		{"a blank kind", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind ""`, "faces 3"), "", "E_ATTACK"},
		{"an unknown target", swing("char:ada", "char:nobody", `to_hit 0 damage 1d6 kind "slashing"`, "faces 3"), "char:nobody", "E_REFERENCE"},
		{"a target without the defense stat", swing("char:ada", "char:ghost", `to_hit 0 damage 1d6 kind "slashing"`, "faces 3"), "char:ghost", "E_STAT"},
		{"one face with a mode", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind "slashing"`, "advantage faces 3"), "faces", "E_ATTACK"},
		{"face 21", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind "slashing"`, "advantage faces 3 21"), "21", "E_ATTACK"},
		{"no faces", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind "slashing"`, "damage 3"), "damage", "E_PARSE"},
		{"three check faces", swing(goblin, "char:ada", `to_hit 0 damage 1d6 kind "slashing"`, `faces 3 damage 3 faces "char:ada:concentration" 1 2 3`), "faces", "E_ROLL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("actions.nql", tc.actions, w.Snapshot().Blueprint)
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.HasPrefix(ds[0].Actual, shownAt(tc.code, tc.at)) {
				t.Fatalf("%+v", ds)
			}
		})
	}
}

// shownAt is a diagnostic's actual text for the token at the fault: quoted
// when the analyzer positioned it, bare when the parser stopped there.
func shownAt(code, at string) string {
	if code == "E_PARSE" || code == "E_DUPLICATE" {
		return at
	}
	return strconv.Quote(at)
}

// History replays from the event intents, check faces included, and every
// value handed in or out is detached.
func TestAttackReplayAndDetachment(t *testing.T) {
	b0, w := attackWorld(t, "")
	for i, a := range []string{
		swing(goblin, "char:ada", `to_hit 4 damage 1d6+2 kind "slashing"`, `faces 17 damage 4 critical damage 7 faces "char:ada:concentration" 12`),
		swing("char:ada", goblin, `to_hit 5 damage 1d8+3 kind "piercing"`, `advantage faces 4 20 damage 2 critical damage 9`),
		swing(goblin, "char:ada", `to_hit 4 damage 1d6+2 kind "slashing"`, `disadvantage faces 19 1 damage 6`),
		swing(goblin, "char:ada", `to_hit 4 damage 1d6+2 kind "slashing"`, `faces 12`),
		// Prerolls for a Multiattack: the first swing breaks the
		// concentration, so the second swing's face is unused.
		swing(goblin, "char:ada", `to_hit 4 damage 1d6+2 kind "slashing"`, `faces 16 damage 6 faces "char:ada:concentration" 2`) + "\n" +
			swing(goblin, "char:ada", `to_hit 4 damage 1d6+2 kind "slashing"`, `faces 15 damage 1 faces "char:ada:concentration" 18`),
	} {
		if _, err := attackRun(t, w, fmt.Sprint("r", i), a); err != nil {
			t.Fatal(err)
		}
	}
	h := w.History()
	last := scored(h[len(h)-1])
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
	ops, _ := compiler.CompileActions("a.nql", swing("char:ada", goblin, `to_hit 0 damage 1d6 kind "slashing"`, "advantage faces 2 3 damage 3"), w.Snapshot().Blueprint)
	b, err := w.Execute(core.Request{ID: "r9", ExpectedRevision: w.Revision(), Actor: ada, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	ops[0].Attack.Faces[0] = 19
	*ops[0].Attack.Damage = 5
	s := scored(b)
	s[0].Intent.Faces[1] = 20
	*s[0].Intent.Damage = 6
	s[0].Unused = append(s[0].Unused, "x")
	b.Events[len(b.Events)-1].Attack.Intent.Faces[0] = 20
	h = w.History()
	got := h[len(h)-1].Events[len(h[len(h)-1].Events)-1].Attack
	if !reflect.DeepEqual(got.Intent.Faces, []int64{2, 3}) || *got.Intent.Damage != 3 || !reflect.DeepEqual(got.Unused, []string{"damage"}) || !reflect.DeepEqual(h[len(h)-1].Request.Operations[0].Attack.Faces, []int64{2, 3}) {
		t.Fatalf("%+v", got)
	}
}

// modelAttack is an independent statement of the attack law: one swing
// against a defense, with the law of the example world.
type modelOutcome struct {
	refused            bool
	kept, total        int64
	hit, crit, natMiss bool
	damage             int64
	scoredDamage       bool
	unused             []string
}

func modelAttack(a core.Attack, defense int64) modelOutcome {
	const die, missFace, critFace, critDice = 20, 1, 20, 2
	var out modelOutcome
	p := a.Profile
	flat := p.DamageCount == 0 && p.DamageSides == 0
	switch {
	case a.Mode == "" && len(a.Faces) != 1, a.Mode != "" && len(a.Faces) != 2:
		return modelOutcome{refused: true}
	case !flat && (p.DamageCount < 1 || p.DamageCount > 64 || p.DamageSides < 1 || p.DamageSides > 1000):
		return modelOutcome{refused: true}
	}
	for _, f := range a.Faces {
		if f < 1 || f > die {
			return modelOutcome{refused: true}
		}
	}
	out.kept = a.Faces[0]
	if len(a.Faces) == 2 {
		hi, lo := a.Faces[0], a.Faces[1]
		if lo > hi {
			hi, lo = lo, hi
		}
		out.kept = map[core.RollMode]int64{core.Advantage: hi, core.Disadvantage: lo}[a.Mode]
	}
	out.total = out.kept + a.Profile.ToHit
	out.natMiss = out.kept == missFace
	out.crit = out.kept == critFace
	out.hit = !out.natMiss && (out.crit || out.total >= defense)
	clauses := []struct {
		name string
		v    *int64
		crit bool
	}{{"damage", a.Damage, false}, {"critical_damage", a.CriticalDamage, true}}
	var used *int64
	for _, c := range clauses {
		if c.v == nil {
			continue
		}
		if out.hit && c.crit == out.crit {
			used = c.v
		} else {
			out.unused = append(out.unused, c.name)
		}
	}
	if !out.hit || used == nil && len(out.unused) == 0 {
		return out
	}
	if used == nil {
		return modelOutcome{refused: true}
	}
	dice := a.Profile.DamageCount
	if out.crit {
		dice *= critDice
	}
	if *used < dice || *used > dice*a.Profile.DamageSides {
		return modelOutcome{refused: true}
	}
	out.scoredDamage = true
	out.damage = max(0, *used+a.Profile.DamageBonus)
	return out
}

// The engine against the model on thousands of random typed attacks, in one
// world, so every swing also sees the earlier ones' damage.
func TestAttackRandomAgainstModel(t *testing.T) {
	_, w := attackWorld(t, "character \"char:t1\" named \"T1\" at \"loc:valley/B03\" { stat \"ac\" = 1; resource \"hp\" = 1000000000 min 0 max 1000000000; resist \"fire\"; }\ncharacter \"char:t15\" named \"T15\" at \"loc:valley/B03\" { stat \"ac\" = 15; resource \"hp\" = 1000000000 min 0 max 1000000000; vulnerable \"fire\"; resist \"cold\"; }\ncharacter \"char:t30\" named \"T30\" at \"loc:valley/B03\" { stat \"ac\" = 30; resource \"hp\" = 1000000000 min 0 max 1000000000; immune \"poison\"; }\ncondition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";\ncondition \"t15:ward\" of \"spell:ward\" to \"char:t15\";\n")
	// The targets' treatments by kind, own and held: resistance, vulnerability, immunity.
	traits := map[core.CharacterID]map[string][3]bool{"char:t1": {"fire": {true, false, false}}, "char:t15": {"fire": {true, true, false}, "cold": {true, false, false}}, "char:t30": {"poison": {false, false, true}}}
	kinds := []string{"slashing", "fire", "cold", "poison"}
	targets := map[core.CharacterID]int64{"char:t1": 1, "char:t15": 15, "char:t30": 30}
	health := map[core.CharacterID]int64{"char:t1": 1e9, "char:t15": 1e9, "char:t30": 1e9}
	ids := []core.CharacterID{"char:t1", "char:t15", "char:t30"}
	rng := rand.New(rand.NewPCG(7, 11))
	tally := map[string]int{}
	for i := range 4000 {
		a := core.Attack{Attacker: "char:ada", Target: ids[rng.IntN(3)], Profile: core.AttackProfile{ToHit: rng.Int64N(31) - 10, DamageCount: rng.Int64N(4) + 1, DamageSides: rng.Int64N(12) + 1, DamageBonus: rng.Int64N(13) - 6, DamageKind: kinds[rng.IntN(4)]}}
		// Flat damage, no dice, and now and then a lone 0, which is not.
		switch rng.IntN(32) {
		case 0, 1, 2, 3:
			a.Profile.DamageCount, a.Profile.DamageSides = 0, 0
		case 4:
			a.Profile.DamageCount = 0
		case 5:
			a.Profile.DamageSides = 0
		}
		flat := a.Profile.DamageCount == 0 && a.Profile.DamageSides == 0
		face := func() int64 {
			if rng.IntN(50) == 0 {
				return []int64{0, 21}[rng.IntN(2)]
			}
			return rng.Int64N(20) + 1
		}
		a.Mode = []core.RollMode{"", core.Advantage, core.Disadvantage}[rng.IntN(3)]
		a.Faces = []int64{face()}
		if a.Mode != "" || rng.IntN(60) == 0 {
			a.Faces = append(a.Faces, face())
		}
		total := func(dice int64) *int64 {
			v := dice
			if span := dice*a.Profile.DamageSides - dice + 1; span > 0 {
				v += rng.Int64N(span)
			}
			if rng.IntN(25) == 0 {
				v = []int64{dice - 1, dice*a.Profile.DamageSides + 1}[rng.IntN(2)]
			}
			return &v
		}
		if rng.IntN(4) != 0 {
			a.Damage = total(a.Profile.DamageCount)
		}
		if rng.IntN(3) == 0 {
			a.CriticalDamage = total(2 * a.Profile.DamageCount)
		}
		want := modelAttack(a, targets[a.Target])
		if tr := traits[a.Target][a.Profile.DamageKind]; tr[0] {
			want.damage /= 2
		}
		if tr := traits[a.Target][a.Profile.DamageKind]; tr[1] {
			want.damage *= 2
		}
		if tr := traits[a.Target][a.Profile.DamageKind]; tr[2] {
			want.damage = 0
		}
		b, err := w.Execute(core.Request{ID: fmt.Sprint("r", i), ExpectedRevision: w.Revision(), Actor: ada, Operations: []core.Operation{{Attack: &a}}})
		if want.refused {
			tally["refused"]++
			if err == nil {
				t.Fatalf("%d: %+v accepted, model refuses", i, a)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%d: %+v: %v", i, a, err)
		}
		got := scored(b)[0]
		if got.Kept != want.kept || got.Total != want.total || got.Hit != want.hit || got.Critical != want.crit || got.NaturalMiss != want.natMiss || !reflect.DeepEqual(got.Unused, want.unused) || (got.Damage != nil) != want.scoredDamage {
			t.Fatalf("%d: %+v\nengine %+v\nmodel  %+v", i, a, got, want)
		}
		if want.scoredDamage {
			if *got.Damage != want.damage {
				t.Fatalf("%d: damage %d, model %d", i, *got.Damage, want.damage)
			}
			health[a.Target] -= want.damage
			tally["damage"]++
			if flat {
				tally["flat"]++
			}
			if traits[a.Target][a.Profile.DamageKind] != [3]bool{} {
				tally["treated"]++
			}
		}
		switch {
		case want.crit:
			tally["critical"]++
		case want.hit:
			tally["hit"]++
		default:
			tally["miss"]++
		}
		if currentOf(w, a.Target, "hp") != health[a.Target] {
			t.Fatalf("%d: hp %d, model %d", i, currentOf(w, a.Target, "hp"), health[a.Target])
		}
	}
	for _, k := range []string{"refused", "damage", "flat", "treated", "critical", "hit", "miss"} {
		if tally[k] < 40 {
			t.Fatalf("too few %s: %v", k, tally)
		}
	}
	t.Logf("%v", tally)
}
