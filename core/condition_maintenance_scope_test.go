package core

import (
	"reflect"
	"testing"
)

func TestConditionMaintenanceRoundExpiry(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	w := alphaWorld(t, b)
	root := ConditionInstanceID("a-root")
	alphaCommit(t, w, Request{ID: "apply", Actor: Ref{Kind: CharacterKind, ID: "a"}, Operations: []Operation{
		{ApplyCondition: &ApplyCondition{Instance: root, Type: "marked", Target: "a", Duration: alphaPtr(uint64(1))}},
		{ApplyCondition: &ApplyCondition{Instance: "z-child", Type: "guarded", Target: "b", Duration: alphaPtr(uint64(1)), Anchor: &root}},
	}})
	var last Batch
	for _, actor := range []CharacterID{"a", "b", "c", "enemy"} {
		last = alphaCommit(t, w, alphaRequest(w, "pass-"+string(actor), actor, "pass", "", ""))
	}
	var removed []Event
	for _, e := range last.Events {
		if e.Kind == ConditionRemoved {
			removed = append(removed, e)
		}
	}
	if len(removed) != 2 || removed[0].Condition.Before.ID != "z-child" || removed[0].Condition.Expiration != nil || removed[1].Condition.Before.ID != root || removed[1].Condition.Expiration.Encounter != "enc" {
		t.Fatal("round expiry must annotate root, not an already-cascaded child", removed)
	}
	if len(w.Snapshot().Conditions) != 0 || len(w.Snapshot().RetiredConditionIDs) != 2 {
		t.Fatal("round lifecycle")
	}
	state := w.ExportCheckpoint()
	replay, err := Replay(state.Genesis, w.History())
	if err != nil || !reflect.DeepEqual(state, replay.ExportCheckpoint()) {
		t.Fatal("round cascade replay", err)
	}
}
