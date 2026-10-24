package tests

import (
	"errors"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// A worn item changes mode by unequip and equip ... mode in one request
// (docs/GRAMMAR.md "Changing a worn item's mode"), on the attack options
// Fighter: a Longsword held in one hand, the Shield worn or carried.
func regrip(t *testing.T, w *core.World, id, src string) (core.Batch, error) {
	t.Helper()
	s := w.Snapshot()
	ops, ds := compiler.CompileActions("regrip.nql", src, s.Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return w.Execute(core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "fighter"}, Operations: ops})
}

// swordGrip is the sword's mode and the hands used, from the equipment view,
// and the modes attack options marks worn.
func swordGrip(t *testing.T, w *core.World) (core.EquipmentModeID, int64, []core.EquipmentModeID) {
	t.Helper()
	e, err := w.EquipmentView("fighter")
	if err != nil {
		t.Fatal(err)
	}
	var mode core.EquipmentModeID
	for _, i := range e.Items {
		if i.Item.ID == "fighter-sword" {
			mode = i.Item.Equipment.Mode
		}
	}
	var hands int64
	for _, s := range e.Slots {
		if s.Slot == "hand" {
			hands = s.Used
		}
	}
	var worn []core.EquipmentModeID
	for _, o := range attackOptions(t, w, "fighter").Options {
		if o.Item == "fighter-sword" && o.Worn {
			worn = append(worn, o.Mode)
		}
	}
	return mode, hands, worn
}

func TestRegrip(t *testing.T) {
	w := optionsWorldWith(t, [2]string{
		`item "fighter-shield" named "Shield" { owner "fighter"; wearer "fighter"; definition "shield"; mode "held"; }`,
		`item "fighter-shield" named "Shield" { owner "fighter"; custody character "fighter"; definition "shield"; }`})
	b, err := regrip(t, w, "two", `unequip "fighter-sword"; equip "fighter-sword" on "fighter" mode "two-handed";`)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Events) != 2 || b.Events[0].Kind != core.ItemUnequipped || b.Events[1].Kind != core.ItemEquipped ||
		*b.Events[0].Wearing != (core.WearingChange{ModeBefore: "held", Item: "fighter-sword", Before: "fighter", Reason: "explicit"}) ||
		*b.Events[1].Wearing != (core.WearingChange{ModeAfter: "two-handed", RequestedMode: "two-handed", Item: "fighter-sword", After: "fighter", Reason: "explicit"}) {
		t.Fatalf("%+v", b.Events)
	}
	if mode, hands, worn := swordGrip(t, w); mode != "two-handed" || hands != 2 || !reflect.DeepEqual(worn, []core.EquipmentModeID{"two-handed"}) {
		t.Fatal(mode, hands, worn)
	}
	e, _ := w.EquipmentView("fighter")
	if len(e.Attacks) != 1 || e.Attacks[0].Origin.Mode != "two-handed" || e.Attacks[0].DamageSides != 10 {
		t.Fatalf("%+v", e.Attacks)
	}

	// Back to one hand.
	if _, err := regrip(t, w, "one", `unequip "fighter-sword"; equip "fighter-sword" on "fighter" mode "held";`); err != nil {
		t.Fatal(err)
	}
	if mode, hands, worn := swordGrip(t, w); mode != "held" || hands != 1 || !reflect.DeepEqual(worn, []core.EquipmentModeID{"held"}) {
		t.Fatal(mode, hands, worn)
	}

	// Replay regenerates the same state and history.
	cp := w.ExportCheckpoint()
	again, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(again.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(again.History(), w.History()) {
		t.Fatal("replay differs", err)
	}
}

// Each refusal names its fault and leaves the world untouched.
func TestRegripRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, src                      string
		code, subject, field, exp, act string
	}{
		// The worn Shield fills the other hand.
		{"shield worn", `unequip "fighter-sword"; equip "fighter-sword" on "fighter" mode "two-handed";`,
			"E_EQUIPMENT", "fighter", "capacity.hand", "2", "3"},
		{"no unequip", `equip "fighter-sword" on "fighter" mode "two-handed";`,
			"E_WORN", "fighter-sword", "wearer", "unworn item", "fighter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := optionsWorldWith(t)
			before, history := w.Snapshot(), w.History()
			_, err := regrip(t, w, "x", tc.src)
			var f *core.Fault
			if !errors.As(err, &f) || f.Code != tc.code || f.Subject != tc.subject || f.Field != tc.field || f.Expected != tc.exp || f.Actual != tc.act {
				t.Fatalf("%+v", err)
			}
			unchanged(t, w, before, history)
		})
	}
}

// An undeclared mode is refused by the compiler, at the mode's token.
func TestRegripUndeclaredMode(t *testing.T) {
	w := optionsWorldWith(t)
	_, ds := compiler.CompileActions("regrip.nql", `unequip "fighter-sword"; equip "fighter-sword" on "fighter" mode "versatile";`, w.Snapshot().Blueprint)
	if len(ds) != 1 || ds[0].Code != "E_EQUIPMENT" || ds[0].Line != 1 || ds[0].Column != 66 || ds[0].Expected != "declared mode" || ds[0].Actual != `"versatile" (versatile)` {
		t.Fatalf("%+v", ds)
	}
}
