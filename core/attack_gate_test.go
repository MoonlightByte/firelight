package core_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// gateFixture is battleFixture with a Monk's Martial Arts: the "monk" type on
// Ada grants a Dexterity unarmed attack while Ada has "ma" and wears no armor
// or shield, rolling d6, d8 or d10 by tier, and an attack whose upgrade does
// not win a tie. Her sword gains a pommel strike, gated on "ma" alone, whose
// d12 upgrade is shut by body armor.
func gateFixture() core.Blueprint {
	b := battleFixture()
	for _, id := range []core.ConditionTypeID{"ma", "ma-d8", "ma-d10"} {
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: id, Instances: core.Unique})
	}
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "ki", Instances: core.Unique, Includes: []core.ConditionTypeID{"ma"}})
	dex := core.ScalarRecipe{Terms: []core.StatTerm{{Stat: "dexterity", Offset: -10, Divisor: 2}}}
	unarmored := []core.SlotID{"body", "shield"}
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "monk", Instances: core.Unique, Attacks: []core.EquipmentAttack{
		{ID: "fist", Name: "Unarmed Strike", Melee: &core.MeleeProfile{Reach: 5, Stat: "dexterity"}, ToHit: dex, AttackGate: core.AttackGate{Requires: "ma", UnlessSlots: unarmored},
			Damage: core.DamageGrant{Count: 1, Sides: 6, Kind: "bludgeoning", Bonus: dex, Upgrades: []core.DamageUpgrade{
				{Count: 1, Sides: 8, AttackGate: core.AttackGate{Requires: "ma-d8", UnlessSlots: unarmored}},
				{Count: 1, Sides: 10, AttackGate: core.AttackGate{Requires: "ma-d10", UnlessSlots: unarmored}},
			}}},
		{ID: "tie", Name: "Tie", Damage: core.DamageGrant{Count: 1, Sides: 8, Kind: "bludgeoning", Upgrades: []core.DamageUpgrade{{Count: 2, Sides: 4, AttackGate: core.AttackGate{Requires: "ma"}}}}},
	}})
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-monk", Type: "monk", Target: "a"}, core.ConditionInstance{ID: "a-ma", Type: "ma", Target: "a"})
	for i, m := range b.Equipment.Definitions[0].Modes {
		if m.ID == b.Items[0].Equipment.Mode {
			b.Equipment.Definitions[0].Modes[i].Attacks = append(m.Attacks, core.EquipmentAttack{ID: "pommel", Name: "Pommel", Melee: &core.MeleeProfile{Reach: 5, Stat: "strength"}, AttackGate: core.AttackGate{Requires: "ma"},
				Damage: core.DamageGrant{Count: 1, Sides: 4, Kind: "bludgeoning", Upgrades: []core.DamageUpgrade{{Count: 1, Sides: 12, AttackGate: core.AttackGate{Requires: "ma", UnlessSlots: []core.SlotID{"body"}}}}}})
		}
	}
	return b
}

// wearOn puts Ada's unworn item id on her in its default mode.
func wearOn(b *core.Blueprint, id core.ItemID) {
	for i := range b.Items {
		if b.Items[i].ID == id {
			b.Items[i].Wearer = "a"
			for _, d := range b.Equipment.Definitions {
				if d.ID == b.Items[i].Equipment.Definition {
					b.Items[i].Equipment.Mode = d.DefaultMode
				}
			}
		}
	}
}

// listed is Ada's grants by member: "<count>d<sides>".
func listed(t *testing.T, w *core.World) map[string]string {
	t.Helper()
	v, err := w.EquipmentView("a")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, g := range v.Attacks {
		out[g.Origin.Member] = fmt.Sprintf("%dd%d", g.DamageCount, g.DamageSides)
	}
	return out
}

func TestAttackGateListing(t *testing.T) {
	without := func(id core.ConditionTypeID) func(*core.Blueprint) {
		return func(b *core.Blueprint) {
			b.Conditions = slices.DeleteFunc(b.Conditions, func(c core.ConditionInstance) bool { return c.Type == id })
		}
	}
	with := func(ids ...core.ConditionTypeID) func(*core.Blueprint) {
		return func(b *core.Blueprint) {
			for _, id := range ids {
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID("a-" + string(id)), Type: id, Target: "a"})
			}
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*core.Blueprint)
		want map[string]string
	}{
		{"Martial Arts", func(*core.Blueprint) {}, map[string]string{"slash": "1d8", "pommel": "1d12", "fist": "1d6", "tie": "1d8"}},
		{"no Martial Arts", without("ma"), map[string]string{"slash": "1d8", "tie": "1d8"}},
		{"Martial Arts by inclusion", func(b *core.Blueprint) { without("ma")(b); with("ki")(b) }, map[string]string{"slash": "1d8", "pommel": "1d12", "fist": "1d6", "tie": "1d8"}},
		{"d8 tier", with("ma-d8"), map[string]string{"slash": "1d8", "pommel": "1d12", "fist": "1d8", "tie": "1d8"}},
		{"d8 and d10 tiers", with("ma-d8", "ma-d10"), map[string]string{"slash": "1d8", "pommel": "1d12", "fist": "1d10", "tie": "1d8"}},
		{"d10 tier alone", with("ma-d10"), map[string]string{"slash": "1d8", "pommel": "1d12", "fist": "1d10", "tie": "1d8"}},
		{"armor", func(b *core.Blueprint) { wearOn(b, "leather") }, map[string]string{"slash": "1d8", "pommel": "1d4", "tie": "1d8"}},
		{"shield", func(b *core.Blueprint) { wearOn(b, "shield") }, map[string]string{"slash": "1d8", "pommel": "1d12", "tie": "1d8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := gateFixture()
			tc.edit(&b)
			if got := listed(t, gearWorld(t, b)); !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got)
			}
		})
	}
}

// Battle rolls the chosen die and refuses a shut attack's grant.
func TestAttackGateBattle(t *testing.T) {
	b := gateFixture()
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-d8", Type: "ma-d8", Target: "a"})
	w := gearWorld(t, b)
	var fist core.EquipmentGrantID
	v, _ := w.EquipmentView("a")
	for _, g := range v.Attacks {
		if g.Origin.Member == "fist" {
			fist = g.ID
		}
	}
	battleExec(t, w, "begin", "a", battleBegin())
	strike := func(w *core.World, damage int64) core.Operation {
		op := battleAttack(t, w, "a", "b", []int64{19}, []int64{damage}, true)
		op.WeaponAttack.Grant = fist
		return op
	}
	// A d8 face of 7 is only legal on the upgraded die.
	r := battleExec(t, w, "strike", "a", strike(w, 7))
	if roll := r.Events[len(r.Events)-1].WeaponAttack.Roll; !roll.Hit || !reflect.DeepEqual(roll.Damage, []int64{7}) {
		t.Fatalf("%+v", roll)
	}
	b.Conditions = slices.DeleteFunc(b.Conditions, func(c core.ConditionInstance) bool { return c.Type == "ma" })
	shut := gearWorld(t, b)
	battleExec(t, shut, "begin", "a", battleBegin())
	err := battleReject(t, shut, gearRequest(shut, "strike", strike(shut, 3)))
	var f *core.Fault
	if !errors.As(err, &f) || f.Field != "grant" || f.Actual != string(fist) {
		t.Fatal(err)
	}
}

func TestAttackGateValidation(t *testing.T) {
	if err := core.Validate(gateFixture()); err != nil {
		t.Fatal(err)
	}
	fist := func(b *core.Blueprint) *core.EquipmentAttack {
		return &b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0]
	}
	nine := make([]core.SlotID, 9)
	for _, tc := range []struct {
		name, field, actual string
		edit                func(*core.Blueprint)
	}{
		{"an undeclared type", "requires", "missing", func(b *core.Blueprint) { fist(b).Requires = "missing" }},
		{"an upgrade with no type", "requires", "", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].Requires = "" }},
		{"an upgrade's undeclared type", "requires", "missing", func(b *core.Blueprint) { fist(b).Damage.Upgrades[1].Requires = "missing" }},
		{"an undeclared slot", "unless_slots", "back", func(b *core.Blueprint) { fist(b).UnlessSlots = []core.SlotID{"back"} }},
		{"a slot twice", "unless_slots", "body", func(b *core.Blueprint) { fist(b).UnlessSlots = []core.SlotID{"body", "body"} }},
		{"an upgrade's undeclared slot", "unless_slots", "back", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].UnlessSlots = []core.SlotID{"back"} }},
		{"nine slots", "unless_slots", "9", func(b *core.Blueprint) { fist(b).UnlessSlots = nine }},
		{"nine upgrades", "upgrades", "fist", func(b *core.Blueprint) {
			for len(fist(b).Damage.Upgrades) < 9 {
				fist(b).Damage.Upgrades = append(fist(b).Damage.Upgrades, core.DamageUpgrade{Count: 1, Sides: 4, AttackGate: core.AttackGate{Requires: "ma"}})
			}
		}},
		{"no dice", "upgrade", "fist", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].Count = 0 }},
		{"101 dice", "upgrade", "fist", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].Count = 101 }},
		{"one side", "upgrade", "fist", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].Sides = 1 }},
		{"1001 sides", "upgrade", "fist", func(b *core.Blueprint) { fist(b).Damage.Upgrades[0].Sides = 1001 }},
		{"an upgrade past the critical bound", "critical dice", "fist", func(b *core.Blueprint) {
			b.Battle.Rules.CriticalMultiplier = 3
			fist(b).Damage.Upgrades[0].Count = 67
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := gateFixture()
			tc.edit(&b)
			var f *core.Fault
			if err := core.Validate(b); !errors.As(err, &f) || f.Field != tc.field || f.Actual != tc.actual {
				t.Fatalf("%+v", err)
			}
		})
	}
	// The 66 dice that triple to 198 stay inside the bound.
	b := gateFixture()
	b.Battle.Rules.CriticalMultiplier = 3
	fist(&b).Damage.Upgrades[0].Count = 66
	if err := core.Validate(b); err != nil {
		t.Fatal(err)
	}
}

// A snapshot's gates and upgrades are detached from the world's.
func TestAttackGateDetached(t *testing.T) {
	w := gearWorld(t, gateFixture())
	// A separate world keeps the expected definitions out of any alias.
	before, kept := listed(t, w), gearWorld(t, gateFixture()).Snapshot()
	s := w.Snapshot()
	for i := range s.ConditionDefinitions {
		if s.ConditionDefinitions[i].ID == "monk" {
			a := &s.ConditionDefinitions[i].Attacks[0]
			a.UnlessSlots[0] = "hand"
			a.Damage.Upgrades[0].Requires = "ma"
			a.Damage.Upgrades[1].UnlessSlots[0] = "hand"
		}
	}
	if got := listed(t, w); !reflect.DeepEqual(got, before) || !reflect.DeepEqual(w.Snapshot().ConditionDefinitions, kept.ConditionDefinitions) {
		t.Fatal("alias", got)
	}
}

// Random gates, tiers and worn slots against an independent model of the
// listing: an attack is listed while its type is present and no worn item
// fills a slot it names; it rolls the largest admitted die, ties keeping the
// earlier.
func TestAttackGateRandomModel(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	types := []core.ConditionTypeID{"t1", "t2", "t3"}
	slots := []core.SlotID{"body", "shield", "hand"}
	occupies := map[core.ItemID][]core.SlotID{"sword": {"hand"}, "leather": {"body"}, "shield": {"hand", "shield"}}
	gate := func(required bool) core.AttackGate {
		var g core.AttackGate
		if required || r.IntN(2) == 0 {
			g.Requires = types[r.IntN(len(types))]
		}
		for _, s := range slots {
			if r.IntN(3) == 0 {
				g.UnlessSlots = append(g.UnlessSlots, s)
			}
		}
		return g
	}
	for round := range 300 {
		b := battleFixture()
		for _, id := range types {
			b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: id, Instances: core.Unique})
		}
		var attacks []core.EquipmentAttack
		for n := range 1 + r.IntN(8) {
			a := core.EquipmentAttack{ID: fmt.Sprint("a", n), Name: "A", AttackGate: gate(false), Damage: core.DamageGrant{Count: 1 + r.Int64N(3), Sides: []int64{4, 6, 8, 10, 12}[r.IntN(5)], Kind: "bludgeoning"}}
			for range r.IntN(4) {
				a.Damage.Upgrades = append(a.Damage.Upgrades, core.DamageUpgrade{Count: 1 + r.Int64N(3), Sides: []int64{4, 6, 8, 10, 12}[r.IntN(5)], AttackGate: gate(true)})
			}
			attacks = append(attacks, a)
		}
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "pool", Instances: core.Unique, Attacks: attacks})
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-pool", Type: "pool", Target: "a"})
		present := map[core.ConditionTypeID]bool{}
		for _, id := range types {
			if r.IntN(2) == 0 {
				present[id] = true
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID("a-" + string(id)), Type: id, Target: "a"})
			}
		}
		filled := map[core.SlotID]bool{"hand": true}
		for _, id := range []core.ItemID{"leather", "shield"} {
			if r.IntN(2) == 0 {
				wearOn(&b, id)
				for _, s := range occupies[id] {
					filled[s] = true
				}
			}
		}
		admits := func(g core.AttackGate) bool {
			return (g.Requires == "" || present[g.Requires]) && !slices.ContainsFunc(g.UnlessSlots, func(s core.SlotID) bool { return filled[s] })
		}
		want := map[string]string{"slash": "1d8"}
		for _, a := range attacks {
			if !admits(a.AttackGate) {
				continue
			}
			best := [2]int64{a.Damage.Count, a.Damage.Sides}
			for _, u := range a.Damage.Upgrades {
				if admits(u.AttackGate) && u.Count*u.Sides > best[0]*best[1] {
					best = [2]int64{u.Count, u.Sides}
				}
			}
			want[a.ID] = fmt.Sprintf("%dd%d", best[0], best[1])
		}
		if got := listed(t, gearWorld(t, b)); !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: got %v want %v", round, got, want)
		}
	}
}
