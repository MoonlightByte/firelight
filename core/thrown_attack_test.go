package core_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// thrownFixture is battleFixture with a thrown attack beside the melee one in
// the mode Ada wears.
func thrownFixture() core.Blueprint {
	b := battleFixture()
	for i, m := range b.Equipment.Definitions[0].Modes {
		if m.ID == b.Items[0].Equipment.Mode {
			b.Equipment.Definitions[0].Modes[i].Attacks = append(m.Attacks, core.EquipmentAttack{ID: "throw", Name: "Throw", Thrown: &core.ThrownProfile{Stat: "strength", Normal: 20, Maximum: 60}, ToHit: core.ScalarRecipe{Base: 4}, Damage: core.DamageGrant{Count: 1, Sides: 4, Kind: "piercing", Bonus: core.ScalarRecipe{Base: 2}}})
		}
	}
	return b
}

func thrownGrant(t *testing.T, w *core.World) core.AttackGrant {
	t.Helper()
	v, err := w.EquipmentView("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range v.Attacks {
		if g.Origin.Member == "throw" {
			return g
		}
	}
	t.Fatalf("no thrown grant: %+v", v.Attacks)
	return core.AttackGrant{}
}

// A thrown attack is listed with its profile and numbers; Battle refuses it.
func TestThrownAttackGrant(t *testing.T) {
	w := gearWorld(t, thrownFixture())
	g := thrownGrant(t, w)
	if g.Melee != nil || g.Ranged != nil || !reflect.DeepEqual(g.Thrown, &core.ThrownProfile{Stat: "strength", Normal: 20, Maximum: 60}) || g.ToHit != 4 || g.DamageCount != 1 || g.DamageSides != 4 || g.DamageBonus != 2 {
		t.Fatalf("%+v", g)
	}
	data, err := json.Marshal(g)
	if err != nil || !strings.HasPrefix(string(data), `{"thrown":{"stat":"strength","normal":20,"maximum":60},"id":`) {
		t.Fatal(string(data), err)
	}
	// The listed profile, the view's definition and the world's are detached.
	view, err := w.EquipmentView("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range view.Attacks {
		if listed.Thrown != nil {
			listed.Thrown.Normal = 97
		}
	}
	for _, i := range view.Items {
		if i.Definition != nil {
			for _, m := range i.Definition.Modes {
				for _, a := range m.Attacks {
					if a.Thrown != nil && a.Thrown.Normal != 20 {
						t.Fatal("the grant aliases the view's definition")
					}
				}
			}
		}
	}
	g.Thrown.Normal = 99
	s := w.Snapshot()
	for _, m := range s.Equipment.Definitions[0].Modes {
		for _, a := range m.Attacks {
			if a.Thrown != nil {
				a.Thrown.Normal = 98
			}
		}
	}
	if again := thrownGrant(t, w); again.Thrown.Normal != 20 {
		t.Fatal("alias", again.Thrown)
	}
	battleExec(t, w, "begin", "a", battleBegin())
	op := battleAttack(t, w, "a", "b", []int64{20}, []int64{4}, true)
	op.WeaponAttack.Grant = g.ID
	err = battleReject(t, w, gearRequest(w, "throw", op))
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_BATTLE" || f.Field != "grant" || f.Actual != string(g.ID) || f.Expected != "currently worn executable melee or ranged grant" {
		t.Fatal(err)
	}
	// A grant the actor does not currently have meets the same refusal.
	op.WeaponAttack.Grant = "equipment:grant:none"
	err = battleReject(t, w, gearRequest(w, "unworn", op))
	if !errors.As(err, &f) || f.Code != "E_BATTLE" || f.Field != "grant" || f.Actual != "equipment:grant:none" || f.Expected != "currently worn executable melee or ranged grant" {
		t.Fatal(err)
	}
}

func TestThrownAttackValidation(t *testing.T) {
	if err := core.Validate(thrownFixture()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		edit        func(*core.Blueprint, *core.EquipmentAttack)
	}{
		{"and melee", "thrown", func(_ *core.Blueprint, a *core.EquipmentAttack) {
			a.Melee = &core.MeleeProfile{Reach: 5, Stat: "strength"}
		}},
		{"and ranged", "thrown", func(b *core.Blueprint, a *core.EquipmentAttack) {
			r := rangedFixture()
			b.Battle.Rules.Ranged, b.ConditionDefinitions = r.Battle.Rules.Ranged, r.ConditionDefinitions
			b.Items = append(b.Items, r.Items[len(r.Items)-1])
			a.Ranged = &core.RangedProfile{Stat: "strength", Normal: 20, Maximum: 60, AmmunitionOrigin: "arrows"}
		}},
		{"no stat", "thrown", func(_ *core.Blueprint, a *core.EquipmentAttack) { a.Thrown.Stat = "" }},
		{"normal 0", "thrown", func(_ *core.Blueprint, a *core.EquipmentAttack) { a.Thrown.Normal = 0 }},
		{"maximum below normal", "thrown", func(_ *core.Blueprint, a *core.EquipmentAttack) { a.Thrown.Maximum = 19 }},
		{"maximum too far", "thrown", func(_ *core.Blueprint, a *core.EquipmentAttack) { a.Thrown.Maximum = 1000001 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := thrownFixture()
			var attack *core.EquipmentAttack
			for i := range b.Equipment.Definitions[0].Modes {
				for j := range b.Equipment.Definitions[0].Modes[i].Attacks {
					if a := &b.Equipment.Definitions[0].Modes[i].Attacks[j]; a.ID == "throw" {
						attack = a
					}
				}
			}
			tc.edit(&b, attack)
			var f *core.Fault
			if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Field != tc.field || f.Actual != "throw" {
				t.Fatal(err)
			}
		})
	}
	// The wearer needs the thrown attack's stat.
	b := thrownFixture()
	for i := range b.Equipment.Definitions[0].Modes {
		for j := range b.Equipment.Definitions[0].Modes[i].Attacks {
			if a := &b.Equipment.Definitions[0].Modes[i].Attacks[j]; a.ID == "throw" {
				a.Thrown.Stat = "missing"
			}
		}
	}
	var f *core.Fault
	if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_STAT" {
		t.Fatalf("%+v", f)
	}
}
