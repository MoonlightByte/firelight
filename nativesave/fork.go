package nativesave

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ForkReservation is durable allocation bookkeeping, not a mechanical event or
// alternate world selector. Keep it even if publication later fails: discarded
// futures must never reuse lifetime identities.
type ForkReservation struct {
	Allocation string `json:"allocation"`
	Control    string `json:"control"`
	Expected   string `json:"expected_generation"`
	Source     string `json:"source_generation"`
}

func allocationName(v string) string { return "allocation-" + v + ".json" }
func validAllocation(v string) bool {
	if len(v) != 36 || v[:4] != "run-" {
		return false
	}
	for _, c := range v[4:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *Store) ReserveFork(control, expected, source string) (ForkReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.checkOpen(); e != nil {
		return ForkReservation{}, e
	}
	if e := validateCampaign(control); e != nil {
		return ForkReservation{}, e
	}
	if s.selected == nil || s.selected.Generation != expected {
		return ForkReservation{}, fmt.Errorf("stale historical-Load control")
	}
	l, e := s.readGeneration(source)
	if e != nil {
		return ForkReservation{}, e
	}
	if l.Info.GenesisDigest != s.selected.Info.GenesisDigest {
		return ForkReservation{}, fmt.Errorf("fork source genesis differs")
	}
	if _, e = s.readAttachment(l.Session.Attachment); e != nil {
		return ForkReservation{}, e
	}
	for i := 0; i < 8; i++ {
		var nonce [16]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			return ForkReservation{}, e
		}
		r := ForkReservation{Allocation: "run-" + hex.EncodeToString(nonce[:]), Control: control, Expected: expected, Source: source}
		name := allocationName(r.Allocation)
		path := filepath.Join(s.dir, name)
		data, e := json.Marshal(r)
		if e != nil {
			return ForkReservation{}, e
		}
		temp, e := s.temporary("allocation", name, s.selected.Info.Head, func(w io.Writer) error { _, e := w.Write(data); return e })
		if e != nil {
			return ForkReservation{}, e
		}
		e = s.stage("allocation-publish", name, s.selected.Info.Head, false, func() error { return os.Link(temp, path) })
		_ = os.Remove(temp)
		if errors.Is(e, os.ErrExist) {
			continue
		}
		if e != nil {
			return ForkReservation{}, e
		}
		if e = s.stage("allocation-directory-sync", name, s.selected.Info.Head, false, func() error { return s.ops.syncDir(s.dir) }); e != nil {
			return ForkReservation{}, e
		}
		return r, nil
	}
	return ForkReservation{}, fmt.Errorf("could not reserve a fresh allocation namespace")
}
func (s *Store) checkForkReservation(r ForkReservation) error {
	if !validAllocation(r.Allocation) {
		return fmt.Errorf("invalid reserved allocation")
	}
	data, e := func() ([]byte, error) {
		f, e := openRegular(filepath.Join(s.dir, allocationName(r.Allocation)))
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(f, MaxHeadBytes+1))
		return data, errors.Join(e, f.Close())
	}()
	if e != nil {
		return e
	}
	expected, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if len(data) > MaxHeadBytes || !bytes.Equal(data, expected) {
		return fmt.Errorf("fork differs from its immutable reservation")
	}
	return nil
}

// ForkJoint intentionally selects an exact historical world, retaining its
// receipts, in a new allocation namespace under the same story lock. The host
// must validate the source adjunct is a completed continuation and supply its
// new idle adjunct. Failure never installs the candidate Store basis in memory;
// uncertain publication requires the caller to freeze and durably resolve it.
func (s *Store) ForkJoint(r ForkReservation, data []byte) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.checkOpen(); e != nil {
		return SaveResult{}, e
	}
	if s.selected == nil || s.selected.Generation != r.Expected {
		return SaveResult{}, fmt.Errorf("stale historical-Load control")
	}
	if e := s.checkForkReservation(r); e != nil {
		return SaveResult{}, e
	}
	source, e := s.readGeneration(r.Source)
	if e != nil {
		return SaveResult{}, e
	}
	if source.Info.GenesisDigest != s.selected.Info.GenesisDigest {
		return SaveResult{}, fmt.Errorf("fork source genesis differs")
	}
	if _, e = s.readAttachment(source.Session.Attachment); e != nil {
		return SaveResult{}, e
	}
	prefix, e := historyPrefixDigest(source.World.History())
	if e != nil {
		return SaveResult{}, e
	}
	// This private Store candidate shares the already-held lifetime lock and IO
	// primitives, not a second live World. Only successful publication installs it.
	candidate := &Store{dir: s.dir, campaign: s.campaign, lock: s.lock, selected: &manifest{Generation: r.Source, Info: source.Info}, prefixDigest: prefix, next: 1, allocation: r.Allocation, forkPrevious: r.Expected, ops: s.ops}
	session := SessionState{Campaign: s.campaign, Allocation: r.Allocation, Next: 1}
	result, e := candidate.saveJointLocked(source.World.ExportCheckpoint(), session, data)
	if e != nil {
		return SaveResult{}, e
	}
	s.incremental = nil
	s.selected = candidate.selected
	s.prefixDigest = candidate.prefixDigest
	s.next = candidate.next
	s.allocation = candidate.allocation
	s.fresh = false
	return result, nil
}
