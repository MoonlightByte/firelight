//go:build linux

package nativesave

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAllocationForkPreservesSelectedHistoryAndRetiresNamespace(t *testing.T) {
	w, session := saveTestFixture(t)
	s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
	first, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"phase":"completed-first"}`))
	if e != nil {
		t.Fatal(e)
	}
	original := w.ExportCheckpoint()
	receipt := original.History[0]
	saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	second, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"phase":"completed-second"}`))
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.ReserveFork("load-one", second.Generation, first.Generation)
	if e != nil {
		t.Fatal(e)
	}
	fork, e := s.ForkJoint(r, []byte(`{"phase":"new-run"}`))
	if e != nil {
		t.Fatal(e)
	}
	if fork.Head != 3 {
		t.Fatal("fork mechanical basis")
	}
	loaded, e := s.LoadJoint()
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Session.Allocation != r.Allocation || loaded.Session.Next != 1 || loaded.Session.LastAttempt != nil || !reflect.DeepEqual(original, loaded.World.ExportCheckpoint()) {
		t.Fatal("fork changed original world/request truth")
	}
	again, e := loaded.World.Execute(receipt.Request)
	if e != nil || !reflect.DeepEqual(receipt, again) {
		t.Fatal("fork lost inherited receipt")
	}
	data, e := os.ReadFile(filepath.Join(s.dir, "HEAD"))
	if e != nil {
		t.Fatal(e)
	}
	var head manifest
	if e = decodeHead(data, &head); e != nil {
		t.Fatal(e)
	}
	if head.Previous != second.Generation {
		t.Fatal("fork previous misidentified as named source")
	}
	if _, e = s.ForkJoint(r, []byte(`{}`)); e == nil {
		t.Fatal("duplicate delayed fork accepted")
	}
	change := loaded.Session
	change.Allocation = "another"
	if _, e = s.SaveJoint(loaded.World.ExportCheckpoint(), change, []byte(`{}`)); e == nil {
		t.Fatal("ordinary save changed allocation")
	}
	r2, e := s.ReserveFork("load-two", fork.Generation, first.Generation)
	if e != nil {
		t.Fatal(e)
	}
	if r2.Allocation == r.Allocation {
		t.Fatal("allocation reused")
	}
	if _, e = os.Stat(filepath.Join(s.dir, allocationName(r.Allocation))); e != nil {
		t.Fatal("discarded future allocation not retained")
	}
	forged := r2
	forged.Control = "different"
	if _, e = s.ForkJoint(forged, []byte(`{}`)); e == nil {
		t.Fatal("changed reserved control accepted")
	}
}
func TestAllocationForkFailureDoesNotInstallCandidateBasis(t *testing.T) {
	for _, stage := range []string{"attachment-publish", "head-publish", "head-directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			w, session := saveTestFixture(t)
			s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
			first, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":3}`))
			if e != nil {
				t.Fatal(e)
			}
			saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
			second, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":4}`))
			if e != nil {
				t.Fatal(e)
			}
			r, e := s.ReserveFork("load", second.Generation, first.Generation)
			if e != nil {
				t.Fatal(e)
			}
			selected, prefix, next, allocation := *s.selected, s.prefixDigest, s.next, s.allocation
			s.ops.before = func(v string) error {
				if v == stage {
					return fmt.Errorf("injected %s", v)
				}
				return nil
			}
			if _, e = s.ForkJoint(r, []byte(`{"at":3,"fork":true}`)); e == nil {
				t.Fatal("missing failure")
			}
			if *s.selected != selected || s.prefixDigest != prefix || s.next != next || s.allocation != allocation {
				t.Fatal("failed fork installed candidate basis")
			}
			s.ops.before = nil
			// Retry the exact still-owned control, without claiming reread is durability.
			result, e := s.ForkJoint(r, []byte(`{"at":3,"fork":true}`))
			if e != nil || result.Head != 3 {
				t.Fatalf("exact fork retry %v", e)
			}
			loaded, e := s.LoadJoint()
			if e != nil || loaded.Session.Allocation != r.Allocation || !bytes.Equal(loaded.Attachment, []byte(`{"at":3,"fork":true}`)) {
				t.Fatal("fork recovery mixed worlds", e)
			}
		})
	}
}
