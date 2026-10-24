//go:build linux

package nativesave

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestJointSaveResumeAndNamedReadDoesNotSelect(t *testing.T) {
	root := saveTestRoot(t)
	w, session := saveTestFixture(t)
	s := saveTestStore(t, root, session.Campaign, true)
	cp := w.ExportCheckpoint()
	body := []byte(`{"narrative":"before"}`)
	first, e := s.SaveJoint(cp, session, body)
	if e != nil {
		t.Fatal(e)
	}
	body[2] = 'X'
	a, e := s.LoadJoint()
	if e != nil || string(a.Attachment) != `{"narrative":"before"}` {
		t.Fatalf("joint load %v %+v", e, a)
	}
	if a.Session.Attachment == nil || a.World.Revision() != 3 {
		t.Fatal("joint association")
	}
	a.Attachment[2] = 'X'
	a.Session.Attachment.Name = "bad"
	a, e = s.LoadJoint()
	if e != nil || string(a.Attachment) != `{"narrative":"before"}` {
		t.Fatal("returned attachment aliases")
	}
	saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	second, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"narrative":"after"}`))
	if e != nil {
		t.Fatal(e)
	}
	named, e := s.ReadNamedJoint(first.Generation)
	if e != nil || named.World.Revision() != 3 || string(named.Attachment) != `{"narrative":"before"}` {
		t.Fatal("named view", e)
	}
	if _, e = s.SaveJoint(cp, session, []byte(`{}`)); e == nil {
		t.Fatal("named inspection silently changed selected mechanical basis")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	resumed := saveTestStore(t, root, session.Campaign, false)
	current, e := resumed.LoadJoint()
	if e != nil || current.Generation != second.Generation || string(current.Attachment) != `{"narrative":"after"}` || !reflect.DeepEqual(current.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("resume association", e)
	}
	if _, e = resumed.ReadNamedJoint("../" + first.Generation); e == nil {
		t.Fatal("named traversal")
	}
}

func TestJointFailureBoundariesNeverMixSelectedComponents(t *testing.T) {
	stages := []string{"attachment-create", "attachment-write", "attachment-flush", "attachment-sync", "attachment-close", "attachment-publish", "attachment-directory-sync", "archive-create", "generation-directory-sync", "head-publish", "head-directory-sync"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			root := saveTestRoot(t)
			w, session := saveTestFixture(t)
			s := saveTestStore(t, root, session.Campaign, true)
			first, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":3}`))
			if e != nil {
				t.Fatal(e)
			}
			saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
			s.ops.before = func(current string) error {
				if current == stage {
					return fmt.Errorf("injected %s", stage)
				}
				return nil
			}
			_, e = s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":4}`))
			var st *StageError
			if !errors.As(e, &st) || st.Stage != stage {
				t.Fatalf("wrong error %v", e)
			}
			if st.Uncertain != (stage == "head-publish" || stage == "head-directory-sync") {
				t.Fatalf("publication uncertainty %+v", st)
			}
			s.ops.before = nil
			// Reading establishes consistency only, not durable acknowledgement.
			current, e := s.LoadJoint()
			if e != nil {
				t.Fatal(e)
			}
			if stage == "head-directory-sync" {
				if current.World.Revision() != 4 || string(current.Attachment) != `{"at":4}` {
					t.Fatal("new HEAD mixed components")
				}
			} else {
				if current.Generation != first.Generation || current.World.Revision() != 3 || string(current.Attachment) != `{"at":3}` {
					t.Fatal("old HEAD mixed components")
				}
			}
			// Only a new successful Save clears this caller's uncertainty.
			if _, e = s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":4}`)); e != nil {
				t.Fatal(e)
			}
			current, e = s.LoadJoint()
			if e != nil || current.World.Revision() != 4 || string(current.Attachment) != `{"at":4}` {
				t.Fatal("retry save", e)
			}
		})
	}
}
func TestJointMissingTamperedOrSymlinkAttachmentFailsClosed(t *testing.T) {
	for _, mode := range []string{"missing", "same-size corrupt", "oversized", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := saveTestRoot(t)
			w, session := saveTestFixture(t)
			s := saveTestStore(t, root, session.Campaign, true)
			if _, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"at":3}`)); e != nil {
				t.Fatal(e)
			}
			loaded, e := s.LoadJoint()
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(s.dir, loaded.Session.Attachment.Name)
			switch mode {
			case "missing":
				e = os.Remove(path)
			case "same-size corrupt":
				e = os.WriteFile(path, []byte(`{"at":4}`), 0600)
			case "oversized":
				e = os.WriteFile(path, bytes.Repeat([]byte("x"), MaxAttachmentBytes+1), 0600)
			case "symlink":
				target := filepath.Join(s.dir, "other")
				if e = os.Rename(path, target); e == nil {
					e = os.Symlink(target, path)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if v, e := s.LoadJoint(); e == nil || v != nil {
				t.Fatal("joint open returned partial world")
			}
			// Mechanical archive compatibility is distinct from complete shell resume.
			if v, e := s.Load(); e != nil || v.World.Revision() != 3 {
				t.Fatalf("mechanical archive changed %v", e)
			}
		})
	}
}
func TestJointBoundsAndLegacyArchiveDistinction(t *testing.T) {
	w, session := saveTestFixture(t)
	s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
	if _, e := s.Save(w.ExportCheckpoint(), session); e != nil {
		t.Fatal(e)
	}
	if v, e := s.LoadJoint(); e == nil || v != nil {
		t.Fatal("fabricated missing narrative continuation")
	}
	for _, body := range [][]byte{nil, bytes.Repeat([]byte("x"), MaxAttachmentBytes+1)} {
		if _, e := s.SaveJoint(w.ExportCheckpoint(), session, body); e == nil {
			t.Fatal("invalid size accepted")
		}
	}
	for _, ref := range []*AttachmentRef{
		{Name: "../host", SHA256: "x", Bytes: 1},
		{Name: "host-" + fmt.Sprintf("%064d", 0) + ".json", SHA256: fmt.Sprintf("%064d", 0), Bytes: 0},
	} {
		session.Attachment = ref
		var b bytes.Buffer
		if _, e := Write(&b, w.ExportCheckpoint(), session); e == nil {
			t.Fatal("malformed reference archived")
		}
	}
}
