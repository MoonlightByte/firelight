package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestBufferArchiveAndHistoricalRetries(t *testing.T) {
	b := saveTestBlueprint()
	b.Characters[0].Resources["temp"] = core.Resource{Current: 5, Maximum: 100}
	b.Characters[0].Buffers = []core.ResourceBuffer{{Pool: "temp", Target: "hp"}}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	s := SessionState{Campaign: "buffer", Next: 1}
	receipts := []core.Batch{saveTestCommit(t, w, &s, core.Operation{Grant: &core.ResourceOp{Character: "hero", Resource: "temp", Amount: 9}}), saveTestCommit(t, w, &s, core.Operation{Damage: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 12}}), saveTestCommit(t, w, &s, core.Operation{Restore: &core.RestoreResource{Character: "hero", Resource: "temp"}})}
	raw, _ := saveTestEncode(t, w.ExportCheckpoint(), s)
	loaded, e := Read(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	before := loaded.World.ExportCheckpoint()
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("archive facts")
	}
	for _, r := range receipts {
		retry, e := loaded.World.Execute(r.Request)
		if e != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
			t.Fatal("archive retry", e)
		}
	}
}
