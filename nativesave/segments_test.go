//go:build linux

package nativesave

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func countMembers(t *testing.T, s *Store, kind string) int {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(s.dir, "history-"+kind+"-*.nql"))
	if e != nil {
		t.Fatal(e)
	}
	return len(paths)
}

func TestSegmentedContinuationReceiptAndMixedFormats(t *testing.T) {
	w, session := saveTestFixture(t)
	s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
	cp := w.ExportCheckpoint()
	old := cp.History[0]
	legacy, e := s.SaveJoint(cp, session, []byte(`{"old":true}`))
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.SaveWorldJoint(w, session, []byte(`{"old":false}`))
	if e != nil {
		t.Fatal(e)
	}
	if first.Head != 3 || s.selected.Info.Format != SegmentedFormatVersion || countMembers(t, s, "batch") != 3 || countMembers(t, s, "genesis") != 1 {
		t.Fatal("wrong initial member set")
	}
	for i := 0; i < 3; i++ {
		if _, e := s.SaveWorldJoint(w, session, []byte(fmt.Sprintf(`{"prose":%d}`, i))); e != nil {
			t.Fatal(e)
		}
	}
	if countMembers(t, s, "batch") != 3 {
		t.Fatal("prose save generated history")
	}
	if _, e := w.Execute(core.Request{ID: "rejected", ExpectedRevision: w.Revision(), Actor: old.Request.Actor, Operations: []core.Operation{{Spend: &core.ResourceOp{Character: "hero", Resource: "focus", Amount: 1}}}}); e == nil {
		t.Fatal("exhausted spend succeeded")
	}
	if _, e := s.SaveWorldJoint(w, session, []byte(`{"rejected":true}`)); e != nil {
		t.Fatal(e)
	}
	if countMembers(t, s, "batch") != 3 {
		t.Fatal("rejection generated history")
	}
	saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	if _, e := s.SaveWorldJoint(w, session, []byte(`{"prose":4}`)); e != nil {
		t.Fatal(e)
	}
	if countMembers(t, s, "batch") != 4 {
		t.Fatal("one action did not create exactly one member")
	}
	loaded, e := s.LoadJoint()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("reconstruction changed complete history")
	}
	before := loaded.World.ExportCheckpoint()
	receipt, e := loaded.World.Execute(old.Request)
	if e != nil || !reflect.DeepEqual(receipt, old) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("historical request executed again", e)
	}
	if before.State.Characters[0].Resources["focus"].Current != 0 || len(before.State.Conditions) != 0 || !reflect.DeepEqual(before.State.RetiredConditionIDs, []core.ConditionInstanceID{"old-genesis", "paid-guard"}) {
		t.Fatal("fixed condition/resource truth lost")
	}
	// Cold binding after reconstruction reuses and reacknowledges old members.
	synced := 0
	s.ops.before = func(stage string) error {
		if stage == "member-resync" {
			synced++
		}
		return nil
	}
	if _, e := s.SaveWorldJoint(loaded.World, loaded.Session, loaded.Attachment); e != nil {
		t.Fatal(e)
	}
	if synced != 5 {
		t.Fatal("cold resume did not resync all dependencies", synced)
	}
	s.ops.before = nil
	named, e := s.ReadNamedJoint(legacy.Generation)
	if e != nil || named.Info.Format != FormatVersion || !reflect.DeepEqual(named.World.ExportCheckpoint(), cp) {
		t.Fatal("legacy generation no longer readable", e)
	}
	if _, e := s.SaveJoint(w.ExportCheckpoint(), session, []byte(`{"legacy":2}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveWorldJoint(w, session, []byte(`{"linked":2}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.LoadJoint(); e != nil {
		t.Fatal(e)
	}
}

func TestSegmentedPublicationFailures(t *testing.T) {
	for _, stage := range []string{"member-create", "member-write", "member-flush", "member-sync", "member-close", "member-publish", "member-directory-sync", "attachment-create", "attachment-write", "attachment-flush", "attachment-sync", "attachment-close", "attachment-publish", "attachment-directory-sync", "archive-create", "archive-write", "archive-flush", "archive-sync", "archive-close", "generation-publish", "generation-directory-sync", "head-create", "head-write", "head-flush", "head-sync", "head-close", "head-publish", "head-directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			w, session := saveTestFixture(t)
			s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
			first, e := s.SaveWorldJoint(w, session, []byte(`{"v":1}`))
			if e != nil {
				t.Fatal(e)
			}
			cache := s.incremental
			saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
			want := w.ExportCheckpoint()
			failure := errors.New("injected " + stage)
			s.ops.before = func(at string) error {
				if at == stage {
					return failure
				}
				return nil
			}
			result, e := s.SaveWorldJoint(w, session, []byte(`{"v":2}`))
			var phase *StageError
			if !errors.Is(e, failure) || !errors.As(e, &phase) || result.Generation != "" || phase.Uncertain != (stage == "head-publish" || stage == "head-directory-sync") {
				t.Fatal("wrong publication failure classification", e)
			}
			if s.incremental != cache || s.selected.Generation != first.Generation || !reflect.DeepEqual(want, w.ExportCheckpoint()) {
				t.Fatal("failure advanced cache or mutated World")
			}
			s.ops.before = nil
			prior, e := s.ReadNamedJoint(first.Generation)
			if e != nil || prior.World.Revision() != 3 {
				t.Fatal("old immutable generation damaged", e)
			}
			loaded, e := s.LoadJoint()
			if e != nil {
				t.Fatal(e)
			}
			head := core.Revision(3)
			if stage == "head-directory-sync" {
				head = 4
			}
			if loaded.World.Revision() != head {
				t.Fatal("mixed selected revision", loaded.World.Revision())
			}
			if _, e := s.SaveWorldJoint(w, session, []byte(`{"v":2}`)); e != nil {
				t.Fatal("failed suffix could not be acknowledged", e)
			}
			loaded, e = s.LoadJoint()
			if e != nil || !reflect.DeepEqual(want, loaded.World.ExportCheckpoint()) {
				t.Fatal("retry lost history", e)
			}
		})
	}
}

func TestSegmentedCapturedReceiptMembership(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			w, session := saveTestFixture(t)
			s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
			first, e := s.SaveWorldJoint(w, session, []byte(`{}`))
			if e != nil {
				t.Fatal(e)
			}
			future := fmt.Sprintf("%s:request:%d", session.Campaign, session.Next)
			request := core.Request{ID: future, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "hero"}, Operations: []core.Operation{{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}}}}
			session.Next++
			session.LastAttempt = &AttemptRef{RequestID: future, Committed: committed}
			s.ops.before = func(stage string) error {
				if stage == "suffix-captured" {
					_, e := w.Execute(request)
					return e
				}
				return nil
			}
			result, e := s.SaveWorldJoint(w, session, []byte(`{}`))
			s.ops.before = nil
			if committed {
				if e == nil || result.Generation != "" || s.selected.Generation != first.Generation {
					t.Fatal("future receipt entered past capture", e)
				}
			} else {
				if e != nil {
					t.Fatal("later commit incorrectly changed captured noncommit", e)
				}
				l, e := s.LoadJoint()
				if e != nil || l.World.Revision() != 3 || l.Session.LastAttempt.Committed {
					t.Fatal("snapshot raced receipt membership", e)
				}
			}
		})
	}
}

func TestSegmentedRejectsBadBasisAndAllocation(t *testing.T) {
	w, session := saveTestFixture(t)
	s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
	first, e := s.SaveWorldJoint(w, session, []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	other, otherSession := saveTestFixture(t)
	if _, e := s.SaveWorldJoint(other, otherSession, []byte(`{}`)); e == nil {
		t.Fatal("warm cursor accepted foreign authority")
	}
	session.Next--
	if _, e := s.SaveWorldJoint(w, session, []byte(`{}`)); e == nil {
		t.Fatal("allocation went backwards")
	}
	session.Next++
	session.LastAttempt = &AttemptRef{RequestID: "absent", Committed: true}
	if _, e := s.SaveWorldJoint(w, session, []byte(`{}`)); e == nil {
		t.Fatal("missing receipt accepted")
	}
	if s.selected.Generation != first.Generation {
		t.Fatal("invalid save published")
	}
	// Load clears the authority binding, but the selected prefix still governs.
	if _, e := s.LoadJoint(); e != nil {
		t.Fatal(e)
	}
	divergent, e := core.NewWorld(saveTestBlueprint())
	if e != nil {
		t.Fatal(e)
	}
	divergentSession := SessionState{Campaign: session.Campaign, Next: 1}
	for i := 0; i < 3; i++ {
		saveTestCommit(t, divergent, &divergentSession, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	}
	if _, e := s.SaveWorldJoint(divergent, divergentSession, []byte(`{}`)); e == nil {
		t.Fatal("cold bind accepted different history under identical genesis")
	}
	bad := saveTestBlueprint()
	bad.Characters[0].Name = "different"
	foreign, e := core.NewWorld(bad)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		saveTestCommit(t, foreign, &otherSession, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	}
	if _, e := s.SaveWorldJoint(foreign, otherSession, []byte(`{}`)); e == nil {
		t.Fatal("cold bind accepted unrelated genesis/history")
	}
}

func TestSegmentedCorruptionDoesNotReturnWorld(t *testing.T) {
	cases := []string{"missing", "edited", "symlink", "oversize-member", "bad-link", "short-chain", "long-chain", "state", "semantics", "count", "bytes", "path", "extra"}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			w, session := saveTestFixture(t)
			s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
			result, e := s.SaveWorldJoint(w, session, []byte(`{}`))
			if e != nil {
				t.Fatal(e)
			}
			d := s.incremental.descriptor
			path := filepath.Join(s.dir, memberName("batch", d.Tail))
			switch tc {
			case "missing":
				e = os.Remove(path)
			case "edited":
				var raw []byte
				raw, e = os.ReadFile(path)
				if e == nil {
					raw[len(raw)-2] ^= 1
					e = os.WriteFile(path, raw, 0600)
				}
			case "symlink":
				target := filepath.Join(s.dir, "other")
				var raw []byte
				raw, e = os.ReadFile(path)
				if e == nil {
					e = os.WriteFile(target, raw, 0600)
				}
				if e == nil {
					e = os.Remove(path)
				}
				if e == nil {
					e = os.Symlink(target, path)
				}
			case "oversize-member":
				e = os.WriteFile(path, bytes.Repeat([]byte{'x'}, MaxRecordBytes+257), 0600)
			case "bad-link":
				raw := []byte("../not-a-member\n{\"kind\":\"batch\"}\n")
				d.Tail, e = s.saveMember("batch", raw, d.Meta.Head)
			case "short-chain":
				d.Tail = ""
			case "long-chain":
				d.Meta.Count--
				d.Meta.Head--
			case "state":
				d.StateDigest = strings.Repeat("0", 64)
			case "semantics":
				d.Meta.Semantics = BattleSemanticsVersion
			case "count":
				d.Meta.Count = core.MaxArchiveBatches + 1
				d.Meta.Head = core.Revision(d.Meta.Count)
			case "bytes":
				d.RecordBytes++
			case "path":
				d.Genesis = "../../foreign"
			case "extra":
			}
			if e != nil {
				t.Fatal(e)
			}
			if tc == "bad-link" || tc == "short-chain" || tc == "long-chain" || tc == "state" || tc == "semantics" || tc == "count" || tc == "bytes" || tc == "path" || tc == "extra" {
				raw, e := json.Marshal(d)
				if e != nil {
					t.Fatal(e)
				}
				raw = append(raw, '\n')
				if tc == "extra" {
					raw = append(raw, 'x')
				}
				if e := os.WriteFile(filepath.Join(s.dir, result.Generation), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			// Named inspection bypasses HEAD hash equality: dependency/replay validation
			// itself must reject, not merely notice that a descriptor was edited.
			if l, e := s.ReadNamedJoint(result.Generation); e == nil || l != nil {
				t.Fatal("corrupt history returned a World", tc, e)
			}
		})
	}
}

func TestSegmentedWarmSaveAllocationsIgnoreRetainedHistory(t *testing.T) {
	var counts []float64
	for _, count := range []int{64, 512} {
		w, session := saveTestFixture(t)
		for int(w.Revision()) < count {
			saveTestCommit(t, w, &session, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
		}
		s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
		if _, e := s.SaveWorldJoint(w, session, []byte(`{}`)); e != nil {
			t.Fatal(e)
		}
		var failure error
		n := testing.AllocsPerRun(3, func() { _, failure = s.SaveWorldJoint(w, session, []byte(`{}`)) })
		if failure != nil {
			t.Fatal(failure)
		}
		counts = append(counts, n)
		t.Logf("history=%d warm save allocations=%g", count, n)
	}
	if counts[1] > counts[0]+100 {
		t.Fatal("warm save still scans/clones retained history", counts)
	}
}

func TestSegmentedColdResyncMustAcknowledgeDependencies(t *testing.T) {
	w, session := saveTestFixture(t)
	s := saveTestStore(t, saveTestRoot(t), session.Campaign, true)
	first, e := s.SaveWorldJoint(w, session, []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	l, e := s.LoadJoint()
	if e != nil {
		t.Fatal(e)
	}
	failure := errors.New("cannot sync readable dependency")
	s.ops.before = func(stage string) error {
		if stage == "member-resync" {
			return failure
		}
		return nil
	}
	if result, e := s.SaveWorldJoint(l.World, l.Session, l.Attachment); !errors.Is(e, failure) || result.Generation != "" || s.incremental != nil || s.selected.Generation != first.Generation {
		t.Fatal("readability became durability", e)
	}
	s.ops.before = nil
	if _, e := s.SaveWorldJoint(l.World, l.Session, l.Attachment); e != nil {
		t.Fatal(e)
	}
}

func TestSegmentedAdmittedNearLimitBatchRemainsSaveable(t *testing.T) {
	// Build a legal batch at the existing exact JSON admission bound. The linked
	// envelope must not steal bytes from the established logical record allowance.
	r := core.Request{ID: "near-limit", Actor: core.Ref{Kind: core.CharacterKind, ID: "hero"}}
	for i := 0; i < 15; i++ {
		c := &core.Character{ID: core.CharacterID(fmt.Sprintf("new:%d", i)), Name: "A", Location: "camp", Stats: map[core.StatID]int64{}, Resources: map[core.ResourceID]core.Resource{}}
		for j := 0; j < 128; j++ {
			c.Stats[core.StatID(fmt.Sprintf("%03d", j)+strings.Repeat("s", 237))] = 1
		}
		r.Operations = append(r.Operations, core.Operation{CreateCharacter: c})
	}
	w, e := core.NewWorld(saveTestBlueprint())
	if e != nil {
		t.Fatal(e)
	}
	initial, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(initial)
	if e != nil {
		t.Fatal(e)
	}
	gap := MaxRecordBytes - 64 - len(raw)
	if gap < 0 || gap > 15*4095*2 {
		t.Fatal("near-boundary fixture cannot fill remaining names", gap)
	}
	if gap%2 != 0 {
		t.Fatal("near-limit fixture requires an even name-padding gap", gap)
	}
	for _, op := range r.Operations {
		n := min(gap/2, 4095)
		op.CreateCharacter.Name += strings.Repeat("n", n)
		gap -= 2 * n
	}
	if gap != 0 {
		t.Fatal("unfilled record")
	}
	w, e = core.NewWorld(saveTestBlueprint())
	if e != nil {
		t.Fatal(e)
	}
	b, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = json.Marshal(b)
	if e != nil || len(raw) != MaxRecordBytes-64 {
		t.Fatal("fixture missed exact record bound", len(raw), e)
	}
	s := saveTestStore(t, saveTestRoot(t), "near", true)
	if _, e := s.SaveWorldJoint(w, SessionState{Campaign: "near", Next: 1}, []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	l, e := s.LoadJoint()
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), l.World.ExportCheckpoint()) {
		t.Fatal("near-limit linked batch lost", e)
	}
}
