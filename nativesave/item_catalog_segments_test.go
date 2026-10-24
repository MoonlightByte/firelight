//go:build linux

package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestItemCatalogSegmentedSaveAndDowngradeRefusal(t *testing.T) {
	b := saveTestBlueprint()
	b.ItemDefinitions = []core.ItemDefinition{{ID: "cloth", Name: "Cloth", Kind: "gear"}}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	session := SessionState{Campaign: "catalog", Next: 1}
	s := saveTestStore(t, saveTestRoot(t), "catalog", true)
	for step := 0; step < 3; step++ {
		if step == 1 {
			saveTestCommit(t, w, &session, core.Operation{CreateItem: &core.Item{ID: "cloth-1", From: "cloth", Owner: "hero"}})
		}
		saved, err := s.SaveWorldJoint(w, session, []byte(`{"narrative":"unchanged"}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.selected.Info.Semantics != ItemCatalogSemanticsVersion {
			t.Fatal("saved descriptor downgraded", s.selected.Info)
		}
		loaded, err := s.ReadNamedJoint(saved.Generation)
		if err != nil || loaded.Info.Semantics != ItemCatalogSemanticsVersion || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
			t.Fatal(loaded, err)
		}
		// Exercise descriptor semantics, not an unrelated manifest/hash mismatch.
		// The segmented reader streams through the monolithic replay validator.
		for _, tag := range []string{SemanticsVersion, EquipmentSemanticsVersion, BattleSemanticsVersion, ClockSemanticsVersion, CreationSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion} {
			d := s.incremental.descriptor
			d.Meta.Semantics = tag
			raw, err := encodedRecord(d, maxDescriptorBytes)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := s.readStoredGeneration(bytes.NewReader(raw)); got != nil || err == nil || !strings.Contains(err.Error(), "semantics") {
				t.Fatal("segmented downgrade not refused by semantic validation", tag, got, err)
			}
		}
	}
	loaded, err := s.LoadJoint()
	if err != nil || loaded.Info.Semantics != ItemCatalogSemanticsVersion {
		t.Fatal("cold selected load", loaded, err)
	}
}
