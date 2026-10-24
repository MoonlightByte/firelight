package core_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// unarmedType is an Unarmed Strike granted by a Condition type: proficiency
// plus Strength to hit, 1 plus Strength bludgeoning. For a (Strength 16) that
// is +5 and 4; for b (Strength 12) +3 and 2.
func unarmedType(id core.ConditionTypeID) core.ConditionDefinition {
	str := core.StatTerm{Stat: "strength", Offset: -10, Divisor: 2}
	return core.ConditionDefinition{ID: id, Instances: core.Unique, Attacks: []core.EquipmentAttack{{
		ID: "strike", Name: "Unarmed Strike", Melee: &core.MeleeProfile{Reach: 5, Stat: "strength"},
		ToHit:  core.ScalarRecipe{Terms: []core.StatTerm{str, {Stat: "proficiency", Divisor: 1}}},
		Damage: core.DamageGrant{Kind: "bludgeoning", Bonus: core.ScalarRecipe{Base: 1, Terms: []core.StatTerm{str}}},
	}}}
}

// unarmedFixture is the Battle fixture where a has the Unarmed Strike type and
// b does not.
func unarmedFixture() core.Blueprint {
	b := battleFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions, unarmedType("unarmed"))
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-unarmed", Type: "unarmed", Target: "a"})
	return b
}

func conditionGrant(t *testing.T, w *core.World, id core.CharacterID) (core.AttackGrant, int) {
	t.Helper()
	v, err := w.EquipmentView(id)
	if err != nil {
		t.Fatal(err)
	}
	var out core.AttackGrant
	found := 0
	for _, g := range v.Attacks {
		if g.Origin.Condition != "" {
			out = g
			found++
		}
	}
	return out, found
}

// A character on whom the type is present has its attacks beside its items',
// evaluated for that character, with a Condition origin and no item.
func TestConditionAttackGrants(t *testing.T) {
	w := gearWorld(t, unarmedFixture())
	v, err := w.EquipmentView("a")
	if err != nil || len(v.Attacks) != 2 {
		t.Fatal(v.Attacks, err)
	}
	g, n := conditionGrant(t, w, "a")
	if n != 1 || g.Origin != (core.EquipmentOrigin{Condition: "unarmed", Member: "strike"}) || !strings.HasPrefix(string(g.ID), "condition:grant:") || g.Name != "Unarmed Strike" || g.ToHit != 5 || g.DamageBonus != 4 || g.DamageCount != 0 || g.Melee == nil || g.Melee.Reach != 5 {
		t.Fatalf("%+v", g)
	}
	raw, _ := json.Marshal(g.Origin)
	if string(raw) != `{"condition":"unarmed","member":"strike"}` {
		t.Fatal(string(raw))
	}
	// An item's origin keeps its four fields.
	for _, x := range v.Attacks {
		if x.Origin.Item != "" {
			raw, _ := json.Marshal(x.Origin)
			if string(raw) != `{"item":"sword","definition":"longsword","mode":"one","member":"slash"}` {
				t.Fatal(string(raw))
			}
		}
	}
	if _, n := conditionGrant(t, w, "b"); n != 0 {
		t.Fatal("b has no Unarmed Strike")
	}
	// The same type on b is b's grant, with its own numbers and identity.
	b := unarmedFixture()
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "b-unarmed", Type: "unarmed", Target: "b"})
	w = gearWorld(t, b)
	ga, _ := conditionGrant(t, w, "a")
	gb, n := conditionGrant(t, w, "b")
	if n != 1 || gb.ID == ga.ID || gb.ToHit != 3 || gb.DamageBonus != 2 {
		t.Fatalf("%+v %+v", ga, gb)
	}
}

// Presence is the engine's: an instance, an including type or a held floor,
// and the grant comes and goes with it.
func TestConditionAttackPresence(t *testing.T) {
	t.Run("includes", func(t *testing.T) {
		b := battleFixture()
		b.ConditionDefinitions = append(b.ConditionDefinitions, unarmedType("unarmed"), core.ConditionDefinition{ID: "monk", Instances: core.Unique, Includes: []core.ConditionTypeID{"unarmed"}})
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "b-monk", Type: "monk", Target: "b"})
		w := gearWorld(t, b)
		if g, n := conditionGrant(t, w, "b"); n != 1 || g.Origin.Condition != "unarmed" {
			t.Fatal(g, n)
		}
	})
	t.Run("held at the minimum", func(t *testing.T) {
		b := gearFixture()
		hp := core.ResourceID("hp")
		held := unarmedType("down")
		held.HeldAtMinimum = &hp
		b.ConditionDefinitions = append(b.ConditionDefinitions, held)
		b.Characters[1].Resources["hp"] = core.Resource{Current: 0, Minimum: 0, Maximum: 10}
		// No instance on b: the floor alone holds the type.
		var kept []core.ConditionInstance
		for _, c := range b.Conditions {
			if c.Target != "b" {
				kept = append(kept, c)
			}
		}
		b.Conditions = kept
		w := gearWorld(t, b)
		if _, n := conditionGrant(t, w, "a"); n != 0 {
			t.Fatal("a is not at its minimum")
		}
		if g, n := conditionGrant(t, w, "b"); n != 1 || g.Origin.Condition != "down" {
			t.Fatal(g, n)
		}
	})
	t.Run("applied and removed", func(t *testing.T) {
		b := battleFixture()
		b.ConditionDefinitions = append(b.ConditionDefinitions, unarmedType("unarmed"))
		w := gearWorld(t, b)
		gearExecute(t, w, "apply", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "a-unarmed", Type: "unarmed", Target: "a"}})
		if _, n := conditionGrant(t, w, "a"); n != 1 {
			t.Fatal("not granted")
		}
		gearExecute(t, w, "remove", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "a-unarmed", Target: "a"}})
		if _, n := conditionGrant(t, w, "a"); n != 0 {
			t.Fatal("still granted")
		}
	})
}

// Battle takes a Condition grant as it takes an item's, by human and agent
// alike, and refuses one the actor does not have.
func TestConditionAttackInBattle(t *testing.T) {
	b := unarmedFixture()
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "b-unarmed", Type: "unarmed", Target: "b"})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	ga, _ := conditionGrant(t, w, "a")
	gb, _ := conditionGrant(t, w, "b")
	strike := func(actor, target core.CharacterID, grant core.EquipmentGrantID, faces []int64) core.Operation {
		op := battleAttack(t, w, actor, target, faces, nil, true)
		op.WeaponAttack.Grant = grant
		return op
	}
	// b's grant is not a's.
	if err := battleReject(t, w, gearRequest(w, "stolen", strike("a", "b", gb.ID, []int64{15}))); !strings.Contains(err.Error(), "grant") {
		t.Fatal(err)
	}
	r := battleExec(t, w, "strike", "a", strike("a", "b", ga.ID, []int64{15}))
	e := r.Events[len(r.Events)-1].WeaponAttack
	if e.Grant.ID != ga.ID || e.Grant.Origin.Condition != "unarmed" || !e.Roll.Hit || e.Roll.Requested != 4 || len(e.Roll.Damage) != 0 {
		t.Fatalf("%+v", e)
	}
	if hp := w.Snapshot().Characters[1].Resources["hp"].Current; hp != 26 {
		t.Fatalf("b hp %d", hp)
	}
	calls := w.Snapshot().Battle.State.Random.Calls
	r = battleExec(t, w, "agent", "b", strike("b", "a", gb.ID, nil))
	if e := r.Events[len(r.Events)-1].WeaponAttack; e.Grant.ID != gb.ID || w.Snapshot().Battle.State.Random.Calls != calls+1 {
		t.Fatalf("%+v", e)
	}
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("replay", err)
	}
}

// A grant ends with its type: once the instance is removed, the grant it gave
// is refused.
func TestConditionAttackEndsWithItsType(t *testing.T) {
	w := gearWorld(t, unarmedFixture())
	ga, _ := conditionGrant(t, w, "a")
	gearExecute(t, w, "remove", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "a-unarmed", Target: "a"}})
	battleExec(t, w, "begin", "a", battleBegin())
	op := battleAttack(t, w, "a", "b", []int64{15}, nil, true)
	op.WeaponAttack.Grant = ga.ID
	if err := battleReject(t, w, gearRequest(w, "gone", op)); !strings.Contains(err.Error(), "grant") {
		t.Fatal(err)
	}
}

// A ranged attack from a Condition type shoots the actor's ammunition like a
// bow's.
func TestConditionAttackRanged(t *testing.T) {
	b := rangedFixture()
	b.Items[0].Wearer = ""
	b.Items[0].Equipment.Mode = ""
	spit := core.ConditionDefinition{ID: "spitter", Instances: core.Unique, Attacks: []core.EquipmentAttack{{ID: "spit", Name: "Spit", Ranged: &core.RangedProfile{Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionOrigin: "arrows"}, ToHit: core.ScalarRecipe{Base: 4}, Damage: core.DamageGrant{Count: 1, Sides: 6, Kind: "acid", Bonus: core.ScalarRecipe{Base: 2}}}}}
	b.ConditionDefinitions = append(b.ConditionDefinitions, spit)
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-spitter", Type: "spitter", Target: "a"})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", rangedBegin(40))
	g, n := conditionGrant(t, w, "a")
	if n != 1 || g.Ranged == nil {
		t.Fatal(g, n)
	}
	op := shot(t, w, []int64{15}, []int64{3}, false, "arrows")
	op.WeaponAttack.Grant = g.ID
	r := battleExec(t, w, "spit", "a", op)
	if roll := r.Events[len(r.Events)-1].WeaponAttack.Roll; !roll.Hit || roll.Requested != 5 || stockCount(t, w, "arrows") != 4 {
		t.Fatalf("%+v", roll)
	}
	// Without the ranged rules the world is refused, as for a ranged item:
	// here the spit is the only ranged attack.
	b.Battle.Rules.Ranged = nil
	bow := &b.Equipment.Definitions[len(b.Equipment.Definitions)-1]
	bow.Modes[0].Attacks = nil
	if _, err := core.NewWorld(b); err == nil || !strings.Contains(err.Error(), "ranged") {
		t.Fatal(err)
	}
	b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].Ranged = nil
	b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].Melee = &core.MeleeProfile{Reach: 5, Stat: "dexterity"}
	if _, err := core.NewWorld(b); err != nil {
		t.Fatal(err)
	}
}

// A Battle hit Condition names an item's attack, so it cannot ride a
// Condition grant.
func TestConditionAttackHitConditionPolicy(t *testing.T) {
	b := unarmedFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "sapped", Instances: core.Coexist})
	b.Battle.Rules.HitConditions = []core.WeaponHitCondition{{ID: "sap", Definition: "longsword", Mode: "one", Attack: "slash", Requires: "martial", Condition: "sapped"}}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	g, _ := conditionGrant(t, w, "a")
	op := battleAttack(t, w, "a", "b", []int64{15}, nil, true)
	op.WeaponAttack.Grant = g.ID
	op.WeaponAttack.HitCondition = &core.WeaponConditionChoice{Rule: "sap", Instance: "fresh"}
	if err := battleReject(t, w, gearRequest(w, "sap", op)); !strings.Contains(err.Error(), "hit_condition.rule") {
		t.Fatal(err)
	}
	// The sword's slash takes it.
	op = battleAttack(t, w, "a", "b", []int64{15}, []int64{3}, true)
	v, _ := w.EquipmentView("a")
	for _, x := range v.Attacks {
		if x.Origin.Item == "sword" {
			op.WeaponAttack.Grant = x.ID
		}
	}
	op.WeaponAttack.HitCondition = &core.WeaponConditionChoice{Rule: "sap", Instance: "fresh"}
	battleExec(t, w, "sword", "a", op)
}

func TestConditionAttackValidation(t *testing.T) {
	attack := unarmedType("unarmed").Attacks[0]
	for _, tc := range []struct {
		name   string
		change func(*core.Blueprint)
		field  string
	}{
		{"no Equipment rules", func(b *core.Blueprint) { b.Equipment = nil; b.Items = nil; b.Battle = nil }, "attacks"},
		{"nine attacks", func(b *core.Blueprint) {
			d := &b.ConditionDefinitions[len(b.ConditionDefinitions)-1]
			for i := 0; len(d.Attacks) < 9; i++ {
				x := attack
				x.ID = "strike-" + string(rune('a'+i))
				d.Attacks = append(d.Attacks, x)
			}
		}, "attacks"},
		{"a duplicate attack", func(b *core.Blueprint) {
			d := &b.ConditionDefinitions[len(b.ConditionDefinitions)-1]
			d.Attacks = append(d.Attacks, attack)
		}, "attack"},
		{"bad dice", func(b *core.Blueprint) {
			b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].Damage.Count = 1
		}, "attack"},
		{"bad reach", func(b *core.Blueprint) {
			b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].Melee.Reach = 0
		}, "melee"},
		{"an undeclared requirement", func(b *core.Blueprint) {
			b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].ToHit.Terms[1].Requires = "missing"
		}, "requires"},
		{"a derived input", func(b *core.Blueprint) {
			b.ConditionDefinitions[len(b.ConditionDefinitions)-1].Attacks[0].Damage.Bonus.Terms[0].Stat = "defense"
		}, "input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := unarmedFixture()
			tc.change(&b)
			_, err := core.NewWorld(b)
			f, ok := err.(*core.Fault)
			if !ok || f.Code != "E_EQUIPMENT" || f.Field != tc.field || f.Subject != "unarmed" {
				t.Fatalf("%v", err)
			}
		})
	}
	// Eight attacks are allowed.
	b := unarmedFixture()
	d := &b.ConditionDefinitions[len(b.ConditionDefinitions)-1]
	for i := 0; len(d.Attacks) < 8; i++ {
		x := attack
		x.ID = "strike-" + string(rune('a'+i))
		d.Attacks = append(d.Attacks, x)
	}
	if w := gearWorld(t, b); w == nil {
		t.Fatal("eight attacks refused")
	}
}

// Views and snapshots are detached from the world's Condition attacks.
func TestConditionAttackDetached(t *testing.T) {
	w := gearWorld(t, unarmedFixture())
	s := w.Snapshot()
	for i := range s.ConditionDefinitions {
		if len(s.ConditionDefinitions[i].Attacks) > 0 {
			a := &s.ConditionDefinitions[i].Attacks[0]
			a.ToHit.Terms[0].Divisor = 1
			a.Melee.Reach = 99
			a.Damage.Bonus.Base = 50
		}
	}
	if g, _ := conditionGrant(t, w, "a"); g.ToHit != 5 || g.DamageBonus != 4 || g.Melee.Reach != 5 {
		t.Fatalf("%+v", g)
	}
}

// Battle's critical-dice bound covers a Condition type's attacks as an item's.
func TestConditionAttackCriticalDice(t *testing.T) {
	// 200 / 3: at most 66 dice before a critical triples them.
	for _, count := range []int64{66, 67} {
		b := battleFixture()
		b.Battle.Rules.CriticalMultiplier = 3
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "claws", Instances: core.Unique, Attacks: []core.EquipmentAttack{{ID: "rake", Name: "Rake", Melee: &core.MeleeProfile{Reach: 5, Stat: "strength"}, Damage: core.DamageGrant{Count: count, Sides: 4, Kind: "slashing"}}}})
		err := core.Validate(b)
		var f *core.Fault
		if count == 66 && err != nil || count == 67 && (!errors.As(err, &f) || f.Field != "critical dice" || f.Actual != "rake") {
			t.Fatal(count, err)
		}
	}
}
