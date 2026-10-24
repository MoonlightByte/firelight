//go:build linux

package nativesave

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/MoonlightByte/firelight/core"
)

type measuredSaveFile struct {
	syncFile
	bytes *int64
}

func (f *measuredSaveFile) Write(p []byte) (int, error) {
	n, err := f.syncFile.Write(p)
	*f.bytes += int64(n)
	return n, err
}

// A fixed tiny narrative and one fixed-size new event must not rewrite old
// batches. The16KiB oracle concerns physical writes, not flaky wall-clock speed.
// Original code uses the detached-checkpoint path; the selected native-session
// save path is the consumer this regression must exercise after correction.
func TestJointSaveDoesNotRewritePriorHistory(t *testing.T) {
	for _, count := range []int{64, 512} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			w, session := saveTestFixture(t)
			for int(w.Revision()) < count {
				saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
			}
			store := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
			if _, err := store.SaveWorldJoint(w, session, []byte(`{"prose":0}`)); err != nil {
				t.Fatal(err)
			}
			var written int64
			create := store.ops.createTemp
			store.ops.createTemp = func(dir, pattern string) (syncFile, error) {
				f, err := create(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &measuredSaveFile{syncFile: f, bytes: &written}, nil
			}
			for _, appendBatch := range []bool{false, true} {
				if appendBatch {
					saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
				}
				written = 0
				start := time.Now()
				if _, err := store.SaveWorldJoint(w, session, []byte(`{"prose":1}`)); err != nil {
					t.Fatal(err)
				}
				t.Logf("prior batches=%d new batch=%v physical write bytes=%d elapsed=%s", count, appendBatch, written, time.Since(start))
				if written > 16<<10 {
					t.Errorf("small continuation change rewrote prior history: %d bytes >16384", written)
				}
			}
		})
	}
}

var _ io.Writer = (*measuredSaveFile)(nil)
