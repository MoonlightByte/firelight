package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestAddedStockArchiveAndHistoricalRetry(t *testing.T) {
	b := saveTestBlueprint()
	b.Clock = &core.Clock{Unit: "tick"}
	b.Items = []core.Item{{ID: "stock", Owner: "hero"}}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	s := SessionState{Campaign: "add", Next: 1}
	receipt := saveTestCommit(t, w, &s, core.Operation{Add: &core.AddItem{Item: "stock", Owner: "hero", Amount: 10}})
	raw, info := saveTestEncode(t, w.ExportCheckpoint(), s)
	if info.Semantics != InventorySemanticsVersion {
		t.Fatal(info)
	}
	loaded, e := Read(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	before := loaded.World.ExportCheckpoint()
	retry, e := loaded.World.Execute(receipt.Request)
	if e != nil || !reflect.DeepEqual(receipt, retry) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("archive/retry", e)
	}
}
