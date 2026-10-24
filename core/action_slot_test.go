package core_test

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func TestActionFreeSlotUsesCanonicalLoadout(t *testing.T) {
	b := battleAbilityFixture()
	b.AbilityDefinitions[0].Action.Requirements = append(b.AbilityDefinitions[0].Action.Requirements,
		core.Requirement{Kind: core.FreeSlot, Subject: core.AbilityOperand{Parameter: "actor"}, Slot: &core.SlotUse{Slot: "hand", Amount: 1}})
	b.Items[0].Equipment.Mode = "two"
	w := gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "occupied", battleInvoke("care", "a", "a")))
	view, err := w.EquipmentView("a")
	if err != nil || view.Slots[0].Used != 2 {
		t.Fatal("loadout projection disagrees", view, err)
	}
	r := gearExecute(t, w, "make-room", unwear("sword"), battleInvoke("care", "a", "a"))
	if w.Snapshot().Characters[0].Resources["focus"].Current != 1 || len(r.Events) != 4 {
		t.Fatal("ordered equipment/action composition", r)
	}
	cp := w.ExportCheckpoint()
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("free slot history", err)
	}
	b.AbilityDefinitions[0].Action.Requirements[2].Slot.Amount = 2
	s := w.Snapshot()
	s.AbilityDefinitions[0].Action.Requirements[2].Slot.Slot = "forged"
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("slot pointer aliases authority")
	}
	for _, slot := range []core.SlotUse{{Slot: "missing", Amount: 1}, {Slot: "hand", Amount: 0}} {
		b.AbilityDefinitions[0].Action.Requirements[2].Slot = &slot
		if bad, err := core.NewWorld(b); bad != nil || err == nil {
			t.Fatal("invalid slot rule accepted")
		}
	}
}

func TestActionWornTrainingIsConditionalOnActualWearer(t *testing.T) {
	b := battleAbilityFixture()
	b.AbilityDefinitions[0].Action.Requirements = append(b.AbilityDefinitions[0].Action.Requirements,
		core.Requirement{Kind: core.WornTraining, Subject: core.AbilityOperand{Parameter: "actor"}, Condition: "guarded", Worn: &core.WornSelector{Definition: "chain-shirt", Mode: "worn"}})
	b.Items[3].Wearer = "a"
	b.Items[3].Equipment.Mode = "worn"
	w := gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "untrained", battleInvoke("care", "a", "a")))
	gearExecute(t, w, "remove-armor", unwear("chain"), battleInvoke("care", "a", "a"))
	if w.Snapshot().Characters[0].Resources["focus"].Current != 1 {
		t.Fatal("unworn equipment blocked action")
	}
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "qualified", Type: "guarded", Target: "a"})
	w = gearWorld(t, b)
	gearExecute(t, w, "trained", battleInvoke("care", "a", "a"))
	before := w.ExportCheckpoint()
	b.AbilityDefinitions[0].Action.Requirements[2].Worn.Mode = "forged"
	s := w.Snapshot()
	s.AbilityDefinitions[0].Action.Requirements[2].Worn.Definition = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("worn selector aliases")
	}
	if invalid, err := core.NewWorld(b); invalid != nil || err == nil {
		t.Fatal("unknown mode accepted")
	}
}
