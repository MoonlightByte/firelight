package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestInventoryTagsMixedCreationHistoryAndRetry(t *testing.T) {
	for _, order := range []string{"quantity-genesis", "consume-create", "create-consume"} {
		t.Run(order, func(t *testing.T) {
			b := saveTestBlueprint()
			b.Clock = &core.Clock{Unit: "tick"}
			b.Items = []core.Item{{ID: "stock", Owner: "hero"}}
			if order == "quantity-genesis" {
				b.Items[0].Quantity = saveTestPtr(int64(1))
			}
			w, e := core.NewWorld(b)
			if e != nil {
				t.Fatal(e)
			}
			s := SessionState{Campaign: "stock", Next: 1}
			initial, info := saveTestEncode(t, w.ExportCheckpoint(), s)
			expected := ClockSemanticsVersion
			if order == "quantity-genesis" {
				expected = InventorySemanticsVersion
			}
			if info.Semantics != expected {
				t.Fatal(info)
			}
			loaded, e := Read(bytes.NewReader(initial))
			if e != nil {
				t.Fatal(e)
			}
			again, _ := saveTestEncode(t, loaded.World.ExportCheckpoint(), loaded.Session)
			if !bytes.Equal(initial, again) {
				t.Fatal("initial byte roundtrip")
			}
			birth := core.Operation{CreateCharacter: &core.Character{ID: "new", Location: "camp"}}
			debit := core.Operation{Consume: &core.ConsumeItem{Item: "stock", Owner: "hero", Amount: 1}}
			var receipt core.Batch
			if order == "create-consume" {
				saveTestCommit(t, w, &s, birth)
				_, creationInfo := saveTestEncode(t, w.ExportCheckpoint(), s)
				if creationInfo.Semantics != CreationSemanticsVersion {
					t.Fatal(creationInfo)
				}
				receipt = saveTestCommit(t, w, &s, debit)
			} else {
				receipt = saveTestCommit(t, w, &s, debit)
				saveTestCommit(t, w, &s, birth)
			}
			raw, info := saveTestEncode(t, w.ExportCheckpoint(), s)
			if info.Semantics != InventorySemanticsVersion {
				t.Fatal(info)
			}
			loaded, e = Read(bytes.NewReader(raw))
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
				t.Fatal("history loss")
			}
			before := loaded.World.ExportCheckpoint()
			retry, e := loaded.World.Execute(receipt.Request)
			if e != nil || !reflect.DeepEqual(retry, receipt) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
				t.Fatal("retry debit", e)
			}
			for _, tag := range []string{ClockSemanticsVersion, CreationSemanticsVersion, EquipmentSemanticsVersion, SemanticsVersion} {
				corrupt := bytes.ReplaceAll(raw, []byte(InventorySemanticsVersion), []byte(tag))
				if bad, e := Read(bytes.NewReader(corrupt)); e == nil || bad != nil {
					t.Fatal("downgraded tag returned world", tag)
				}
			}
		})
	}
}
