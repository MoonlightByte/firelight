package core

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCheckpointDeltaAuthoritySuffixAndDetachment(t *testing.T) {
	w := checkpointTestWorld(t, checkpointTestBlueprint())
	initial, cursor, err := w.ExportSince(CheckpointCursor{})
	if err != nil || initial.Genesis == nil || initial.Base != 0 || initial.Head != 0 || initial.State.Revision != 0 || len(initial.History) != 0 {
		t.Fatal("invalid initial window", err)
	}
	first := checkpointTestPaidGuard(t, w)
	alias := *w
	delta, next, err := alias.ExportSince(cursor)
	if err != nil || delta.Genesis != nil || delta.Base != 0 || delta.Head != 1 || !reflect.DeepEqual(delta.History, []Batch{first}) {
		t.Fatal("copied live handle lost its authority/suffix", err)
	}
	want := w.ExportCheckpoint()
	initial.Genesis.Characters[0].Stats["defense"] = 999
	delta.State.Characters[0].Resources["focus"] = Resource{}
	delta.State.Conditions[0].Cause.Actor.ID = "changed"
	delta.History[0].Events[0].Resource.After.Current = 999
	*delta.History[0].Request.Operations[0].Invoke.Arguments[0].Value.ConditionID = "changed"
	if !reflect.DeepEqual(w.ExportCheckpoint(), want) {
		t.Fatal("exported window aliases canonical nested records")
	}
	empty, _, err := w.ExportSince(next)
	if err != nil || len(empty.History) != 0 || empty.Base != 1 || empty.Head != 1 || empty.Genesis != nil {
		t.Fatal("unchanged head reexported history", err)
	}
	other := checkpointTestWorld(t, checkpointTestBlueprint())
	if _, _, err := other.ExportSince(cursor); err == nil {
		t.Fatal("cursor accepted by another authority at matching revision")
	}
	if _, _, err := (*World)(nil).ExportSince(cursor); err == nil {
		t.Fatal("nil authority accepted")
	}
	if revision, ok := w.ReceiptRevision("paid"); !ok || revision != 1 {
		t.Fatal("receipt membership lost")
	}
	if _, ok := w.ReceiptRevision("absent"); ok {
		t.Fatal("missing receipt invented")
	}
	metadata, ok := w.ReceiptMetadata("paid")
	if !ok || metadata.Revision != 1 || len(metadata.EventIDs) != len(first.Events) {
		t.Fatal("receipt metadata incomplete")
	}
	for i, id := range metadata.EventIDs {
		if id != first.Events[i].ID {
			t.Fatal("metadata invented an event identity")
		}
	}
	metadata.EventIDs[0] = "changed"
	if receipt, _ := w.Receipt("paid"); !reflect.DeepEqual(receipt, first) {
		t.Fatal("metadata altered full historical receipt")
	}
}

func TestCheckpointDeltaConcurrentHeadIsCoherent(t *testing.T) {
	w := checkpointTestWorld(t, checkpointTestBlueprint())
	_, cursor, err := w.ExportSince(CheckpointCursor{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ {
			_, err := w.Execute(Request{ID: fmt.Sprint(i), ExpectedRevision: Revision(i), Actor: Ref{Kind: CharacterKind, ID: "char:a"}, Operations: []Operation{{Heal: &ResourceOp{Character: "char:a", Resource: "hp", Amount: 1}}}})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	// Every returned state and suffix must describe the same captured head,
	// regardless of which commits win the read lock.
	for i := 0; i < 64; i++ {
		delta, next, err := w.ExportSince(cursor)
		if err != nil || delta.Head != delta.State.Revision || Revision(len(delta.History)) != delta.Head-delta.Base {
			t.Fatal("incoherent concurrent export", err)
		}
		wantHP := int64(6 + delta.Head)
		if wantHP > 10 {
			wantHP = 10
		}
		if delta.State.Characters[0].Resources["hp"].Current != wantHP {
			t.Fatal("state differs from captured head")
		}
		cursor = next
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	last, _, err := w.ExportSince(cursor)
	if err != nil || last.Head != 64 || last.State.Characters[0].Resources["hp"].Current != 10 {
		t.Fatal("final suffix lost committed state", err)
	}
}
