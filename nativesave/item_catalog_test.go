package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestItemCatalogIncrementalTagPrecedence(t *testing.T) {
	for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion, ItemCatalogSemanticsVersion} {
		if combineSemantics(ItemCatalogSemanticsVersion, tag) != ItemCatalogSemanticsVersion || combineSemantics(tag, ItemCatalogSemanticsVersion) != ItemCatalogSemanticsVersion {
			t.Fatal("incremental save downgraded catalog", tag)
		}
	}
}

func TestItemCatalogEmptyRegistryKeepsLegacyArchiveShape(t *testing.T) {
	b := saveTestBlueprint()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	session := SessionState{Campaign: "empty-catalog", Next: 1}
	legacy, info := saveTestEncode(t, w.ExportCheckpoint(), session)
	b.ItemDefinitions = []core.ItemDefinition{}
	w, err = core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if w.Snapshot().ItemDefinitions != nil || b.ItemDefinitions == nil {
		t.Fatal("empty registry must canonicalize without mutating its input")
	}
	data, withEmpty := saveTestEncode(t, w.ExportCheckpoint(), session)
	if !bytes.Equal(legacy, data) || info != withEmpty {
		t.Fatal("unused catalog changed archive bytes or profile")
	}
	loaded, err := Read(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("empty registry round-trip", err)
	}
}

func TestItemCatalogArchiveTagAndIntentRoundTrip(t *testing.T) {
	for _, mode := range []string{"definitions", "genesis", "created", "clock", "equipment", "battle-tag"} {
		t.Run(mode, func(t *testing.T) {
			b := saveTestBlueprint()
			b.ItemDefinitions = []core.ItemDefinition{{ID: "cloth", Name: "Cloth", Description: "Plain fabric.", Kind: "gear", Price: &core.ItemPrice{Unit: "cp", Amount: 5, UnitsPerLot: 2}}}
			if mode == "genesis" {
				b.Items = []core.Item{{ID: "cloth-1", From: "cloth", Owner: "hero"}}
			}
			if mode == "clock" {
				b.Clock = &core.Clock{Unit: "seconds"}
			}
			if mode == "equipment" {
				b.Equipment = &core.EquipmentRules{Version: core.EquipmentVersion}
			}
			if mode == "battle-tag" {
				// Profile selection must not hide catalog fields under an older
				// battle tag. Full mixed battle validation has its own suite.
				b.Battle = &core.Battle{}
				if semanticsFor(b) != ItemCatalogSemanticsVersion {
					t.Fatal("battle masked catalog profile")
				}
				return
			}
			w, err := core.NewWorld(b)
			if err != nil {
				t.Fatal(err)
			}
			session := SessionState{Campaign: "catalog", Next: 1}
			var receipt core.Batch
			if mode == "created" {
				receipt = saveTestCommit(t, w, &session, core.Operation{CreateItem: &core.Item{ID: "new", From: "cloth", Owner: "hero"}})
			}
			data, info := saveTestEncode(t, w.ExportCheckpoint(), session)
			if info.Semantics != ItemCatalogSemanticsVersion {
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
			if mode == "created" {
				r, err := loaded.World.Execute(receipt.Request)
				if err != nil || !reflect.DeepEqual(receipt, r) || r.Events[0].ItemIntent.Name != "" || r.Events[0].Item.Name != "Cloth" {
					t.Fatal(r, err)
				}
			}
			for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion} {
				bad := bytes.ReplaceAll(data, []byte(ItemCatalogSemanticsVersion), []byte(tag))
				if v, err := Read(bytes.NewReader(bad)); err == nil || v != nil {
					t.Fatal("catalog downgrade accepted", tag)
				}
			}
		})
	}
}
