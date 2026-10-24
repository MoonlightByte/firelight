package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestItemArchiveFeatureTagCompleteHistoryAndOriginalReceipt(t *testing.T) {
	for _, mode := range []string{"item", "description", "consume-first", "birth-first", "character-first", "character-last"} {
		t.Run(mode, func(t *testing.T) {
			b := saveTestBlueprint()
			q := int64(2)
			b.Items = []core.Item{{ID: "stock", Owner: "hero", Quantity: &q}}
			if mode == "description" {
				b.Items[0].Description = "Original cloth"
			}
			w, e := core.NewWorld(b)
			if e != nil {
				t.Fatal(e)
			}
			s := SessionState{Campaign: "item", Next: 1}
			consume := core.Operation{Consume: &core.ConsumeItem{Item: "stock", Owner: "hero", Amount: 1}}
			character := core.Operation{CreateCharacter: &core.Character{ID: "new-person", Location: "camp"}}
			if mode == "consume-first" {
				saveTestCommit(t, w, &s, consume)
			}
			if mode == "character-first" {
				saveTestCommit(t, w, &s, character)
			}
			var birth core.Batch
			if mode != "description" {
				birth = saveTestCommit(t, w, &s, core.Operation{CreateItem: &core.Item{ID: "new-item", Name: "Cloth", Owner: "hero"}})
			}
			if mode == "birth-first" {
				saveTestCommit(t, w, &s, consume)
			}
			if mode == "character-last" {
				saveTestCommit(t, w, &s, character)
			}
			data, info := saveTestEncode(t, w.ExportCheckpoint(), s)
			if info.Semantics != ItemCreationSemanticsVersion {
				t.Fatal(info)
			}
			loaded, e := Read(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(loaded.World.ExportCheckpoint(), w.ExportCheckpoint()) {
				t.Fatal("item facts/history")
			}
			encoded, _ := saveTestEncode(t, loaded.World.ExportCheckpoint(), loaded.Session)
			if !bytes.Equal(encoded, data) {
				t.Fatal("roundtrip")
			}
			if mode != "description" {
				before := loaded.World.ExportCheckpoint()
				receipt, e := loaded.World.Execute(birth.Request)
				if e != nil || !reflect.DeepEqual(receipt, birth) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
					t.Fatal("receipt", e)
				}
			}
			for _, tag := range []string{SemanticsVersion, CreationSemanticsVersion, InventorySemanticsVersion} {
				bad := bytes.ReplaceAll(data, []byte(ItemCreationSemanticsVersion), []byte(tag))
				if got, e := Read(bytes.NewReader(bad)); e == nil || got != nil {
					t.Fatal("downgrade")
				}
			}
		})
	}
}
