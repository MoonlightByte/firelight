package core_test

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// flatFixture is the Battle fixture with an Unarmed Strike: no dice, 1 plus
// the Strength modifier, so 4 for a (Strength 16) and 2 for b (12).
func flatFixture() core.Blueprint {
	b := battleFixture()
	str := core.StatTerm{Stat: "strength", Offset: -10, Divisor: 2}
	for i := range b.Equipment.Definitions[0].Modes {
		a := &b.Equipment.Definitions[0].Modes[i].Attacks[0]
		a.ToHit = core.ScalarRecipe{Base: 100}
		a.Damage = core.DamageGrant{Kind: "bludgeoning", Bonus: core.ScalarRecipe{Base: 1, Terms: []core.StatTerm{str}}}
	}
	return b
}

// A flat grant asks a human for no damage faces, and an agent draws none:
// the damage is the bonus, on a critical too.
func TestFlatDamageBattleGrant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		face      int64
		hit, crit bool
		requested int64
	}{
		{"hit", 15, true, false, 4}, {"critical", 20, true, true, 4}, {"natural miss", 1, false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := gearWorld(t, flatFixture())
			view, err := w.EquipmentView("a")
			if err != nil || len(view.Attacks) != 1 || view.Attacks[0].DamageCount != 0 || view.Attacks[0].DamageSides != 0 || view.Attacks[0].DamageBonus != 4 {
				t.Fatalf("grant %+v %v", view, err)
			}
			battleExec(t, w, "begin", "a", battleBegin())
			before := w.ExportCheckpoint()
			human := core.Ref{Kind: core.CharacterKind, ID: "a"}
			need, err := w.BattleInput(w.Revision(), human, battleAttack(t, w, "a", "b", nil, nil, true))
			if err != nil || need.Stage != "attack" || need.Count != 1 || need.Sides != 20 {
				t.Fatal(need, err)
			}
			need, err = w.BattleInput(w.Revision(), human, battleAttack(t, w, "a", "b", []int64{tc.face}, nil, true))
			if err != nil || need.Stage != "complete" || need.Count != 0 || need.Provisional == nil || need.Provisional.Hit != tc.hit {
				t.Fatal(need, err)
			}
			battleReject(t, w, gearRequest(w, "extra", battleAttack(t, w, "a", "b", []int64{tc.face}, []int64{1}, true)))
			if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("input changed the world")
			}
			calls := w.Snapshot().Battle.State.Random.Calls
			r := battleExec(t, w, "strike", "a", battleAttack(t, w, "a", "b", []int64{tc.face}, nil, true))
			roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
			if roll.Hit != tc.hit || roll.Critical != tc.crit || roll.Requested != tc.requested || len(roll.Damage) != 0 || w.Snapshot().Battle.State.Random.Calls != calls {
				t.Fatalf("human %+v", roll)
			}
			// The agent's turn: one draw, the attack face, and no damage dice.
			calls = w.Snapshot().Battle.State.Random.Calls
			r = battleExec(t, w, "agent", "b", battleAttack(t, w, "b", "a", nil, nil, true))
			roll = r.Events[len(r.Events)-1].WeaponAttack.Roll
			// The fixture's seed draws a hit for b.
			if !roll.Hit || roll.Requested != 2 || len(roll.Damage) != 0 || w.Snapshot().Battle.State.Random.Calls != calls+1 {
				t.Fatalf("agent %+v calls %d then %d", roll, calls, w.Snapshot().Battle.State.Random.Calls)
			}
			cp := w.ExportCheckpoint()
			rebuilt, err := core.Replay(cp.Genesis, cp.History)
			if err != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
				t.Fatal("replay", err)
			}
		})
	}
}

// Only count and sides both 0 are flat; either alone is refused as before.
func TestFlatDamageGrantBounds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, sides int64
		ok           bool
	}{
		{"flat", 0, 0, true}, {"no dice of 6 sides", 0, 6, false}, {"1 die of 0 sides", 1, 0, false},
		{"-1 dice of 0 sides", -1, 0, false}, {"0 dice of -1 sides", 0, -1, false}, {"0 dice of 1 side", 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := gearFixture()
			b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage.Count = tc.count
			b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage.Sides = tc.sides
			w, err := core.NewWorld(b)
			if (err == nil) != tc.ok || (w != nil) != tc.ok {
				t.Fatalf("world %v", err)
			}
			if f, isFault := err.(*core.Fault); !tc.ok && (!isFault || f.Field != "attack" || f.Expected != "unique ID, valid name/kind, 1..100 dice of 2..1000 sides") {
				t.Fatalf("%+v", err)
			}
		})
	}
}
