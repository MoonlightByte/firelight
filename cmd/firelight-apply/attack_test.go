package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// actionLines is an example file without its comments.
func actionLines(t *testing.T, path string) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(readFile(t, path), "\n") {
		if line != "" && !strings.HasPrefix(line, "//") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// The attack guide's actions are the example files, and its five attack
// results and its rider's DamageApplied are the engine's.
func TestAttackGuideExamples(t *testing.T) {
	guide := readFile(t, "../../docs/ATTACKS.md")
	var shown []any
	for _, block := range strings.Split(guide, "```json\n")[1:] {
		var v any
		if err := json.Unmarshal([]byte(strings.SplitN(block, "```", 2)[0]), &v); err != nil {
			t.Fatal(err)
		}
		shown = append(shown, v)
	}
	if len(shown) != 6 {
		t.Fatalf("%d examples in the guide", len(shown))
	}
	world := readFile(t, "../../examples/attacks/world.nql")
	ada := core.Ref{Kind: core.CharacterKind, ID: "char:ada"}
	for i, file := range []string{"swing.nql", "player-score.nql", "player-damage.nql", "traits.nql"} {
		actions := actionLines(t, "../../examples/attacks/"+file)
		if file != "player-damage.nql" && !strings.Contains(guide, "```nql\n"+actions+"\n```") {
			t.Fatalf("the guide does not show %s", file)
		}
		r := apply(input{World: world, Actions: actions, Actor: &ada, Request: "swing:1"})
		if !r.OK || len(r.Attacks) != 1 {
			t.Fatal(file, r.Error, r.Fault, r.Diagnostics)
		}
		if got := asJSON(t, r.Attacks[0]); !reflect.DeepEqual(got, shown[i]) {
			t.Fatalf("%s:\nengine %v\nguide  %v", file, got, shown[i])
		}
	}
	if !strings.Contains(guide, "`critical damage 11` (`player-damage.nql`)") {
		t.Fatal("the guide does not name player-damage.nql")
	}
	rider := actionLines(t, "../../examples/attacks/rider.nql")
	if !strings.Contains(guide, "```nql\n"+rider+"\n```") {
		t.Fatal("the guide does not show rider.nql")
	}
	r := apply(input{World: world, Actions: rider, Actor: &ada, Request: "rider:1"})
	if !r.OK || len(r.Receipt.Events) != 1 {
		t.Fatal(r.Error, r.Fault, r.Diagnostics)
	}
	if got := asJSON(t, r.Receipt.Events[0].Resource); !reflect.DeepEqual(got, shown[4]) {
		t.Fatalf("rider.nql:\nengine %v\nguide  %v", got, shown[4])
	}
	flat := actionLines(t, "../../examples/attacks/flat.nql")
	if !strings.Contains(guide, "```nql\n"+flat+"\n```") {
		t.Fatal("the guide does not show flat.nql")
	}
	r = apply(input{World: world, Actions: flat, Actor: &ada, Request: "strike:1"})
	if !r.OK || len(r.Attacks) != 1 || len(r.Receipt.Events) != 2 {
		t.Fatal(r.Error, r.Fault, r.Diagnostics)
	}
	if got := asJSON(t, r.Attacks[0]); !reflect.DeepEqual(got, shown[5]) {
		t.Fatalf("flat.nql:\nengine %v\nguide  %v", got, shown[5])
	}
	if hp := r.Receipt.Events[0].Resource.After.Current; hp != 3 {
		t.Fatalf("goblin hp %d", hp)
	}
	// When the ward ends, the same swing does 14.
	unwarded := strings.Replace(world, "condition \"occ:valley/B03/skeleton-1:warding\" of \"spell:warding\" to \"occ:valley/B03/skeleton-1\";\n", "", 1)
	if unwarded == world {
		t.Fatal("the ward is not in the world")
	}
	r = apply(input{World: unwarded, Actions: actionLines(t, "../../examples/attacks/traits.nql"), Actor: &ada, Request: "swing:1"})
	if !r.OK || *r.Attacks[0].Damage != 14 || !strings.Contains(guide, "the same swing does 14") {
		t.Fatal(r.Error, r.Fault, r.Attacks)
	}
}

// A host's shape: a stateless per-call world and one call per swing, in
// order, with the host carrying hit points into the next call's world. The
// same swings as one Multiattack request score and apply alike.
func TestAttackOneCallPerSwing(t *testing.T) {
	world := readFile(t, "../../examples/attacks/world.nql")
	ada := core.Ref{Kind: core.CharacterKind, ID: "char:ada"}
	swings := []string{
		`attack "occ:valley/B03/goblin-1" profile to_hit 4 damage 1d6+2 kind "slashing" against "char:ada" faces 17 damage 4 critical damage 7 faces "char:ada:concentration" 12;`,
		`attack "occ:valley/B03/goblin-1" profile to_hit 4 damage 1d6+2 kind "slashing" against "char:ada" advantage faces 3 20 damage 2 critical damage 9 faces "char:ada:concentration" 15;`,
		`attack "occ:valley/B03/goblin-1" profile to_hit 4 damage 1d6+2 kind "slashing" against "char:ada" faces 1 damage 6 critical damage 12;`,
	}
	var each []core.AttackResult
	current := world
	for i, s := range swings {
		r := apply(input{World: current, Actions: s, Actor: &ada, Request: "swing:" + string(rune('1'+i))})
		if !r.OK || len(r.Attacks) != 1 {
			t.Fatal(i, r.Error, r.Fault, r.Diagnostics)
		}
		each = append(each, r.Attacks[0])
		// The host writes the hit points it read back into its next world.
		for _, e := range r.Receipt.Events {
			if e.Kind == core.DamageApplied {
				res := e.Resource
				current = strings.Replace(current, `resource "hp" = 24 min 0 max 24;`, `resource "hp" = `+itoa(res.After.Current)+` min 0 max 24;`, 1)
				current = strings.Replace(current, `resource "temp-hp" = 5 min 0 max 5;`, `resource "temp-hp" = `+itoa(res.Buffer.After.Current)+` min 0 max 5;`, 1)
			}
		}
	}
	if each[0].Damage == nil || *each[0].Damage != 6 || !each[1].Critical || *each[1].Damage != 11 || !each[2].NaturalMiss || each[2].Damage != nil {
		t.Fatalf("%+v", each)
	}
	r := apply(input{World: world, Actions: strings.Join(swings, "\n"), Actor: &ada, Request: "multiattack:1"})
	if !r.OK || !reflect.DeepEqual(r.Attacks, each) {
		t.Fatalf("%v %+v", r.Error, r.Attacks)
	}
	var after int64 = -1
	for _, e := range r.Receipt.Events {
		if e.Kind == core.DamageApplied {
			after = e.Resource.After.Current
		}
	}
	// 6 then 11: 5 temporary hit points, then 1 and 11 of Ada's 24.
	if after != 12 {
		t.Fatalf("hp %d", after)
	}
}

func itoa(v int64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
