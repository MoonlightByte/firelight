package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestItemChargesIncrementalTagPrecedence(t *testing.T) {
	for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion, ItemCatalogSemanticsVersion, ItemChargesSemanticsVersion} {
		if combineSemantics(ItemChargesSemanticsVersion, tag) != ItemChargesSemanticsVersion || combineSemantics(tag, ItemChargesSemanticsVersion) != ItemChargesSemanticsVersion {
			t.Fatal("incremental save downgraded charges", tag)
		}
	}
}

func TestItemChargesArchiveTagSpendAndResume(t *testing.T) {
	b := saveTestBlueprint()
	b.ItemDefinitions = []core.ItemDefinition{
		{ID: "cloth", Name: "Cloth", Kind: "gear"},
		{ID: "wand", Name: "Wand", Kind: "wand", Magical: true, Charges: &core.ChargeRule{Max: 7}},
	}
	b.Items = []core.Item{{ID: "wand-1", From: "wand", Owner: "hero", Charges: &core.ItemCharges{Current: 5}}}
	if semanticsFor(b) != ItemChargesSemanticsVersion {
		t.Fatal("charge rules must select the charges profile")
	}
	plain := b
	plain.ItemDefinitions = b.ItemDefinitions[:1]
	plain.Items = nil
	if semanticsFor(plain) != ItemCatalogSemanticsVersion {
		t.Fatal("a catalog without charge rules keeps its profile")
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	session := SessionState{Campaign: "charges", Next: 1}
	receipt := saveTestCommit(t, w, &session, core.Operation{ExpendCharges: &core.ExpendCharges{Item: "wand-1", Owner: "hero", Amount: 2}})
	data, info := saveTestEncode(t, w.ExportCheckpoint(), session)
	if info.Semantics != ItemChargesSemanticsVersion {
		t.Fatal(info)
	}
	loaded, err := Read(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal(err)
	}
	again, _ := saveTestEncode(t, loaded.World.ExportCheckpoint(), loaded.Session)
	if !bytes.Equal(data, again) {
		t.Fatal("archive bytes changed on round-trip")
	}
	if v, err := loaded.World.ItemChargesView([]core.ItemID{"wand-1"}); err != nil || v[0].Current != 3 {
		t.Fatal("resumed charges", v, err)
	}
	r, err := loaded.World.Execute(receipt.Request)
	if err != nil || !reflect.DeepEqual(receipt, r) {
		t.Fatal("retry after resume must return the original receipt", r, err)
	}
	for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion, ItemCatalogSemanticsVersion} {
		bad := bytes.ReplaceAll(data, []byte(ItemChargesSemanticsVersion), []byte(tag))
		if v, err := Read(bytes.NewReader(bad)); err == nil || v != nil {
			t.Fatal("charges downgrade accepted", tag)
		}
	}
}

func TestItemRechargeProfile(t *testing.T) {
	for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion, ItemCatalogSemanticsVersion, ItemChargesSemanticsVersion, ItemRechargeSemanticsVersion} {
		if combineSemantics(ItemRechargeSemanticsVersion, tag) != ItemRechargeSemanticsVersion || combineSemantics(tag, ItemRechargeSemanticsVersion) != ItemRechargeSemanticsVersion {
			t.Fatal("incremental save downgraded recharge", tag)
		}
	}
	b := saveTestBlueprint()
	b.Clock = &core.Clock{Unit: "second", Tick: 100}
	asOf := uint64(20)
	b.ItemDefinitions = []core.ItemDefinition{
		{ID: "rod", Name: "Rod", Kind: "rod", Charges: &core.ChargeRule{Max: 10, Recharge: &core.ChargeRecharge{Every: 30, Dice: &core.ChargeDice{Count: 1, Sides: 6}}}},
		{ID: "wand", Name: "Wand", Kind: "wand", Charges: &core.ChargeRule{Max: 7}},
	}
	b.Items = []core.Item{{ID: "rod-1", From: "rod", Owner: "hero", Charges: &core.ItemCharges{Current: 1, AsOf: &asOf, Seed: &[2]uint64{3, 4}}}}
	for _, rule := range []func(*core.ChargeRule){
		func(r *core.ChargeRule) { r.Recharge = &core.ChargeRecharge{Every: 30, All: true} },
		func(r *core.ChargeRule) { r.InitialDice = &core.ChargeDice{Count: 1, Sides: 6} },
		func(r *core.ChargeRule) { r.Exhausted = &core.ChargeExhausted{Destroy: true} },
	} {
		only := b
		only.Items = nil
		r := core.ChargeRule{Max: 7}
		rule(&r)
		only.ItemDefinitions = []core.ItemDefinition{{ID: "wand", Name: "Wand", Kind: "wand", Charges: &r}}
		if semanticsFor(only) != ItemRechargeSemanticsVersion {
			t.Fatal("each new rule selects the recharge profile", r)
		}
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	session := SessionState{Campaign: "recharge", Next: 1}
	receipt := saveTestCommit(t, w, &session, core.Operation{ExpendCharges: &core.ExpendCharges{Item: "rod-1", Owner: "hero", Amount: 2}})
	data, info := saveTestEncode(t, w.ExportCheckpoint(), session)
	if info.Semantics != ItemRechargeSemanticsVersion {
		t.Fatal(info)
	}
	loaded, err := Read(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal(err)
	}
	if again, _ := saveTestEncode(t, loaded.World.ExportCheckpoint(), loaded.Session); !bytes.Equal(data, again) {
		t.Fatal("archive bytes changed on round-trip")
	}
	before, _ := w.ItemChargesView([]core.ItemID{"rod-1"})
	if v, err := loaded.World.ItemChargesView([]core.ItemID{"rod-1"}); err != nil || !reflect.DeepEqual(v, before) || *v[0].AsOf != 100 {
		t.Fatal("resumed recharge state", v, err)
	}
	if r, err := loaded.World.Execute(receipt.Request); err != nil || !reflect.DeepEqual(receipt, r) {
		t.Fatal("retry after resume must return the original receipt", r, err)
	}
	for _, tag := range []string{ItemCatalogSemanticsVersion, ItemChargesSemanticsVersion} {
		bad := bytes.ReplaceAll(data, []byte(ItemRechargeSemanticsVersion), []byte(tag))
		if v, err := Read(bytes.NewReader(bad)); err == nil || v != nil {
			t.Fatal("recharge downgrade accepted", tag)
		}
	}
}
