package core

import (
	"fmt"
	"io"
)

// MaxArchiveBatches bounds complete native history reconstruction. Replay keeps
// its separate, smaller per-call bound and accepts valid history prefixes.
const MaxArchiveBatches = 8192

// Checkpoint is a detached observation of one committed revision. Genesis and
// History reconstruct the world; State is an audit observation, not replay input.
type Checkpoint struct {
	Genesis Blueprint
	Head    Revision
	History []Batch
	State   Snapshot
}

// CheckpointCursor identifies an immutable history prefix of one authority.
// It is process-local, not a serialized proof, capability to mutate, or receipt.
// The zero value starts an export from genesis; copying a World preserves it.
type CheckpointCursor struct {
	authority *authority
	head      Revision
}

// CheckpointDelta is a detached observation at Head. History contains only the
// batches after Base. Genesis is included only when exporting from a zero cursor.
// Public fields are observations, not trusted caller-supplied replay certificates.
type CheckpointDelta struct {
	Genesis *Blueprint
	Base    Revision
	Head    Revision
	History []Batch
	State   Snapshot
}

// ExportSince captures a coherent suffix and audit state under one read lock.
// Storage obtains this directly from a live World, then performs I/O after the
// lock is released. No callback or borrowed canonical collection is exposed.
func (w *World) ExportSince(cursor CheckpointCursor) (CheckpointDelta, CheckpointCursor, error) {
	if w == nil || w.authority == nil {
		return CheckpointDelta{}, CheckpointCursor{}, fmt.Errorf("incremental export requires a live World")
	}
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	if (cursor.authority != nil && cursor.authority != w.authority) || cursor.head > w.authority.state.Revision {
		return CheckpointDelta{}, CheckpointCursor{}, fmt.Errorf("checkpoint cursor does not belong to this World history")
	}
	out := CheckpointDelta{Base: cursor.head, Head: w.authority.state.Revision}
	if cursor.authority == nil {
		if cursor.head != 0 {
			return CheckpointDelta{}, CheckpointCursor{}, fmt.Errorf("invalid zero checkpoint cursor")
		}
		b := cloneBlueprint(w.authority.genesis)
		out.Genesis = &b
	}
	out.History = make([]Batch, len(w.authority.history)-int(cursor.head))
	for i, batch := range w.authority.history[int(cursor.head):] {
		out.History[i] = cloneBatch(batch)
	}
	b := cloneBlueprint(w.authority.state.Blueprint)
	out.State = Snapshot{Revision: out.Head, Blueprint: b, ConditionEffects: derivedConditionEffects(&b)}
	return out, CheckpointCursor{authority: w.authority, head: out.Head}, nil
}

// ExportCheckpoint captures genesis, complete receipts, and current state under
// one read lock. Encoding, storage, and other host work happen after it returns.
func (w *World) ExportCheckpoint() Checkpoint {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()

	history := make([]Batch, len(w.authority.history))
	for i, batch := range w.authority.history {
		history[i] = cloneBatch(batch)
	}
	b := cloneBlueprint(w.authority.state.Blueprint)
	return Checkpoint{
		Genesis: cloneBlueprint(w.authority.genesis),
		Head:    w.authority.state.Revision,
		History: history,
		State: Snapshot{
			Revision:         w.authority.state.Revision,
			Blueprint:        b,
			ConditionEffects: derivedConditionEffects(&b),
		},
	}
}

// BatchReader supplies one complete typed batch on each successful Next call.
// io.EOF means there are no more batches; every other error aborts reconstruction.
// A reader may reuse its returned storage on the following call. Readers own
// byte, allocation, and decoding limits before handing records to the core.
type BatchReader interface {
	Next() (Batch, error)
}

// ReplayArchive reconstructs a new, private World from the complete selected
// history. Unlike Replay, it rejects a valid prefix when count or head promises
// more history. No World is returned until all batches and final EOF validate.
// Reader calls occur outside World locks and never receive the private World.
func ReplayArchive(initial Blueprint, head Revision, count int, input BatchReader) (*World, error) {
	if count < 0 || count > MaxArchiveBatches {
		return nil, fail("E_LIMIT", "", "archive", "0..8192 batches", fmt.Sprint(count), "archive batch limit exceeded")
	}
	if head != Revision(count) {
		return nil, fail("E_REPLAY", "", "head", fmt.Sprint(count), fmt.Sprint(head), "archive head disagrees with complete batch count")
	}
	if input == nil {
		return nil, fail("E_REPLAY", "", "archive", "batch reader", "nil", "archive reader is required")
	}
	w, err := NewWorld(initial)
	if err != nil {
		return nil, err
	}
	for i := 0; i < count; i++ {
		record, err := input.Next()
		if err == io.EOF {
			return nil, fail("E_REPLAY", "", "count", fmt.Sprint(count), fmt.Sprint(i), "archive ended before the declared batch count")
		}
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		if err := replayBatch(w, record); err != nil {
			return nil, err
		}
	}
	if _, err := input.Next(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		return nil, fail("E_REPLAY", "", "count", fmt.Sprint(count), "extra batch", "archive contains more than the declared batch count")
	}
	if w.Revision() != head {
		return nil, fail("E_REPLAY", "", "head", fmt.Sprint(head), fmt.Sprint(w.Revision()), "reconstructed archive head disagrees")
	}
	return w, nil
}
