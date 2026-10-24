package nativesave

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// pagedSaveWorld returns saveTestBlueprint plus pages whose exported genesis
// encodes to exactly size bytes, and one committed request.
func pagedSaveWorld(t *testing.T, size int) (*core.World, SessionState) {
	t.Helper()
	b := saveTestBlueprint()
	for n := 0; n < size/4096; n++ {
		b.Items = append(b.Items, core.Item{ID: core.ItemID(fmt.Sprintf("page:%05d", n)), Name: "Page", Owner: "hero", Description: strings.Repeat("d", 4000)})
	}
	build := func() (*core.World, int) {
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(w.ExportCheckpoint().Genesis)
		if err != nil {
			t.Fatal(err)
		}
		return w, len(raw)
	}
	_, got := build()
	for i, gap := 0, size-got; gap > 0; i++ {
		add := min(gap, 4096-len(b.Items[i].Description))
		b.Items[i].Description += strings.Repeat("d", add)
		gap -= add
	}
	w, got := build()
	if got != size {
		t.Fatal("fixture missed the genesis size", got, size)
	}
	s := SessionState{Campaign: "paged", Next: 1}
	saveTestCommit(t, w, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	return w, s
}

// A genesis record up to 16 MiB minus framing saves and loads through both
// physical formats; one byte more is refused before anything is written.
func TestGenesisRecordAtBoundSavesAndLoads(t *testing.T) {
	limit := 16<<20 - 1024 // the documented bound, not the constant
	w, s := pagedSaveWorld(t, limit)
	if limit <= MaxRecordBytes {
		t.Fatal("bound is not above the batch record bound")
	}
	data, info := saveTestEncode(t, w.ExportCheckpoint(), s)
	loaded, err := Read(bytes.NewReader(data))
	if err != nil || loaded.Info != info || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("bound genesis lost in the archive", err)
	}
	store := saveTestStore(t, saveTestRoot(t), s.Campaign, true)
	if _, err := store.SaveWorldJoint(w, s, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	joint, err := store.LoadJoint()
	if err != nil || !reflect.DeepEqual(joint.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("bound genesis lost in the linked store", err)
	}
	over, s := pagedSaveWorld(t, limit+1)
	var output bytes.Buffer
	if _, err := Write(&output, over.ExportCheckpoint(), s); err == nil || output.Len() != 0 {
		t.Fatal("archive accepted a genesis over the bound", err, output.Len())
	}
	fresh := saveTestStore(t, saveTestRoot(t), s.Campaign, true)
	if _, err := fresh.SaveWorldJoint(over, s, []byte(`{}`)); err == nil {
		t.Fatal("linked store accepted a genesis over the bound")
	}
}

// Read admits a genesis line up to the genesis bound, not the batch bound, and
// refuses a longer one before decoding it.
func TestReadGenesisLineBound(t *testing.T) {
	w, s := saveTestFixture(t)
	data, _ := saveTestEncode(t, w.ExportCheckpoint(), s)
	lines := bytes.SplitAfter(data, []byte{'\n'})
	for _, tc := range []struct {
		length  int
		limited bool
	}{{MaxRecordBytes + 1, false}, {16 << 20, false}, {16<<20 + 1, true}} {
		archive := append(append([]byte{}, lines[0]...), bytes.Repeat([]byte{'x'}, tc.length-1)...)
		archive = append(archive, '\n')
		_, err := Read(bytes.NewReader(archive))
		if err == nil || strings.Contains(err.Error(), "record limit exceeded") != tc.limited {
			t.Fatalf("genesis line of %d bytes: %v", tc.length, err)
		}
	}
}

// A linked genesis member may hold the genesis bound; a larger file is refused
// on its size before it is read.
func TestGenesisMemberSizeBound(t *testing.T) {
	w, s := saveTestFixture(t)
	store := saveTestStore(t, saveTestRoot(t), s.Campaign, true)
	if _, err := store.SaveWorldJoint(w, s, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	hash := store.incremental.descriptor.Genesis
	path := filepath.Join(store.dir, memberName("genesis", hash))
	for _, tc := range []struct {
		length int
		size   bool
	}{{MaxRecordBytes + 1, false}, {16 << 20, false}, {16<<20 + 1, true}} {
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, tc.length), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := store.readMember("genesis", hash)
		if err == nil || strings.Contains(err.Error(), "invalid history member size") != tc.size {
			t.Fatalf("genesis member of %d bytes: %v", tc.length, err)
		}
	}
}
