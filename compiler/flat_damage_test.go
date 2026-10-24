package compiler_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// An equipment attack with `flat` lowers to no dice, and a Battle attack
// with it takes its attack face alone.
func TestFlatDamageEquipmentAttack(t *testing.T) {
	const dice = `damage "slashing" dice 1 sides 8 {`
	s := strings.Replace(battleSource(t), dice, `damage "bludgeoning" flat {`, 1)
	// Tess acts first.
	s = strings.Replace(s, `stat "initiative" = 2;`, `stat "initiative" = 30;`, 1)
	b, ds := compiler.CompileWorld("flat.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	got := b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage
	want := core.DamageGrant{Kind: "bludgeoning", Bonus: core.ScalarRecipe{Terms: []core.StatTerm{{Stat: "strength", Offset: -10, Divisor: 2}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.EquipmentView("tess")
	if err != nil || len(view.Attacks) != 1 || view.Attacks[0].DamageCount != 0 || view.Attacks[0].DamageSides != 0 || view.Attacks[0].DamageBonus != 3 {
		t.Fatal(view, err)
	}
	tess := core.Ref{Kind: core.CharacterKind, ID: "tess"}
	run := func(id, source string) (core.Batch, error) {
		ops, ds := compiler.CompileActions(id, source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		return w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: tess, Operations: ops})
	}
	begin, err := os.ReadFile("../examples/native_melee/begin.nql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run("begin", string(begin)); err != nil {
		t.Fatal(err)
	}
	swing := func(rolls string) string {
		return fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true { %s };`, view.Attacks[0].ID, rolls)
	}
	if e := w.Snapshot().Battle.State.Encounters[0]; e.Round != 1 || e.Cursor != 0 || e.Roster[0].Admission.Character != "tess" {
		t.Fatalf("%+v", e)
	}
	if _, err := run("extra", swing("attack 20; damage 1;")); err == nil || !strings.Contains(err.Error(), "damage dice") {
		t.Fatal("a flat attack took a damage face", err)
	}
	r, err := run("strike", swing("attack 20;"))
	if err != nil {
		t.Fatal(err)
	}
	roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
	if !roll.Critical || roll.Requested != 3 || len(roll.Damage) != 0 {
		t.Fatalf("%+v", roll)
	}
	for _, tc := range []struct{ to, token, code string }{
		{`damage "slashing" flat dice 1 sides 8 {`, `dice`, "E_PARSE"},
		{`damage "slashing" flat;`, `;`, "E_PARSE"},
		{`damage "slashing" dice 0 sides 8 {`, `0`, "E_EQUIPMENT"},
		{`damage "slashing" dice 1 sides 0 {`, `0`, "E_EQUIPMENT"},
	} {
		bad := strings.Replace(battleSource(t), dice, tc.to, 1)
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.token)
		_, ds := compiler.CompileWorld("bad.nql", bad)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal(tc.to, ds)
		}
	}
}

// `flat` is `0d0`: the same profile, the same event.
func TestFlatDamageAttackProfile(t *testing.T) {
	text, err := os.ReadFile("../examples/attacks/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("world.nql", string(text))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var profiles []core.AttackProfile
	for _, s := range []string{`flat+4`, `0d0+4`} {
		ops, ds := compiler.CompileActions("a.nql", `attack "char:ada" profile to_hit 5 damage `+s+` kind "bludgeoning" against "occ:valley/B03/goblin-1" faces 20 critical damage 0;`, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		profiles = append(profiles, ops[0].Attack.Profile)
	}
	if want := (core.AttackProfile{ToHit: 5, DamageBonus: 4, DamageKind: "bludgeoning"}); profiles[0] != want || profiles[1] != want {
		t.Fatalf("%+v", profiles)
	}
}
