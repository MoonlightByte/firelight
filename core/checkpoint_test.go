package core

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"testing"
)

func checkpointTestPtr[T any](v T) *T { return &v }

func checkpointTestBlueprint() Blueprint {
	return Blueprint{
		Rules:     Rules{Transfer: Unequips, Wear: WearAny},
		Locations: []Location{{ID: "loc:camp"}},
		Characters: []Character{
			{ID: "char:z", Location: "loc:camp", Stats: map[StatID]int64{"defense": 10}},
			{ID: "char:a", Location: "loc:camp", Stats: map[StatID]int64{"defense": 10}, Resources: map[ResourceID]Resource{
				"hp": {Current: 6, Minimum: 0, Maximum: 10}, "focus": {Current: 3, Minimum: 0, Maximum: 3},
			}},
		},
		ConditionDefinitions: []ConditionDefinition{{ID: "guard", Instances: Unique, Modifiers: []ConditionModifier{{ID: "defense", Stat: "defense", Fixed: checkpointTestPtr(int64(1))}}}},
		Conditions:           []ConditionInstance{},
		RetiredConditionIDs:  []ConditionInstanceID{"retired:genesis"},
		AbilityDefinitions: []AbilityDefinition{{
			ID: "paid-guard", Parameters: []AbilityParameter{{ID: "target", Kind: CharacterArgument}, {ID: "instance", Kind: ConditionIDArgument}},
			Steps: []AbilityStep{
				{Spend: &SpendTemplate{Payer: AbilityOperand{Parameter: "target"}, Resource: "focus", Amount: AbilityOperand{Literal: &AbilityValue{Integer: checkpointTestPtr(int64(2))}}}},
				{ApplyCondition: &ConditionTemplate{Instance: AbilityOperand{Parameter: "instance"}, Type: "guard", Target: AbilityOperand{Parameter: "target"}, Source: &Ref{Kind: CharacterKind, ID: "char:z"}}},
			},
		}},
	}
}

func checkpointTestWorld(t *testing.T, b Blueprint) *World {
	t.Helper()
	w, err := NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func checkpointTestCommit(t *testing.T, w *World, id string, op Operation) Batch {
	t.Helper()
	batch, err := w.Execute(Request{ID: id, ExpectedRevision: w.Revision(), Actor: Ref{Kind: CharacterKind, ID: "char:a"}, Operations: []Operation{op}})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func checkpointTestPaidGuard(t *testing.T, w *World) Batch {
	t.Helper()
	return checkpointTestCommit(t, w, "paid", Operation{Invoke: &InvokeAbility{Ability: "paid-guard", Arguments: []AbilityArgument{
		{Name: "target", Value: AbilityValue{Character: checkpointTestPtr(CharacterID("char:a"))}},
		{Name: "instance", Value: AbilityValue{ConditionID: checkpointTestPtr(ConditionInstanceID("guard:paid"))}},
	}}})
}

func checkpointTestJourney(t *testing.T) (*World, Batch) {
	t.Helper()
	w := checkpointTestWorld(t, checkpointTestBlueprint())
	first := checkpointTestPaidGuard(t, w)
	checkpointTestCommit(t, w, "remove", Operation{RemoveCondition: &RemoveCondition{Instance: "guard:paid", Target: "char:a"}})
	checkpointTestCommit(t, w, "empty-focus", Operation{Spend: &ResourceOp{Character: "char:a", Resource: "focus", Amount: 1}})
	return w, first
}

type checkpointSliceReader struct {
	records   []Batch
	index     int
	calls     int
	failureAt int
	failure   error
}

func (r *checkpointSliceReader) Next() (Batch, error) {
	r.calls++
	if r.failure != nil && r.index == r.failureAt {
		return Batch{}, r.failure
	}
	if r.index == len(r.records) {
		return Batch{}, io.EOF
	}
	record := r.records[r.index]
	r.index++
	return record, nil
}

func TestCheckpointExportDetachedGenesisAndState(t *testing.T) {
	b := checkpointTestBlueprint()
	w := checkpointTestWorld(t, b)
	initial := w.ExportCheckpoint()
	if initial.Head != 0 || initial.State.Revision != 0 || initial.History == nil || len(initial.History) != 0 || !reflect.DeepEqual(initial.Genesis, initial.State.Blueprint) {
		t.Fatal("genesis checkpoint does not contain the complete normalized zero-revision world")
	}
	if initial.Genesis.Characters[0].ID != "char:a" || initial.Genesis.Conditions == nil {
		t.Fatal("genesis was not normalized, or empty Condition shape was lost")
	}
	b.Characters[0].Stats["defense"] = 900
	*b.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = 99
	*b.ConditionDefinitions[0].Modifiers[0].Fixed = 99
	b.RetiredConditionIDs[0] = "changed"
	if !reflect.DeepEqual(initial, w.ExportCheckpoint()) {
		t.Fatal("constructor inputs alias retained genesis or live state")
	}
	checkpointTestPaidGuard(t, w)
	want := w.ExportCheckpoint()
	if want.Head != 1 || len(want.History) != 1 || want.State.Revision != 1 || !reflect.DeepEqual(want.Genesis, initial.Genesis) || len(want.State.Conditions) != 1 || len(want.State.ConditionEffects) != 1 {
		t.Fatal("export confused immutable genesis with current state or omitted Condition audit effects")
	}
	if want.State.Characters[0].Resources["focus"].Current != 1 || want.Genesis.Characters[0].Resources["focus"].Current != 3 {
		t.Fatal("fixed payment/genesis oracle failed")
	}
	got := w.ExportCheckpoint()
	got.Genesis.Characters[0].Stats["defense"] = 400
	*got.Genesis.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = 99
	*got.Genesis.ConditionDefinitions[0].Modifiers[0].Fixed = 99
	got.Genesis.RetiredConditionIDs[0] = "changed"
	if !reflect.DeepEqual(got.State, want.State) {
		t.Fatal("genesis and audit state share writable data")
	}
	got.State.Conditions[0].Cause.Actor.ID = "changed"
	got.State.ConditionEffects[0].ConditionOrigin.Instance = "changed"
	got.State.Characters[0].Resources["focus"] = Resource{}
	*got.History[0].Request.Operations[0].Invoke.Arguments[0].Value.ConditionID = "changed"
	got.History[0].Events[0].Resource.Before.Current = 99
	got.History[0].Events[1].Condition.After.Cause.Actor.ID = "changed"
	if !reflect.DeepEqual(want, w.ExportCheckpoint()) {
		t.Fatal("exported genesis, audit state, request or event aliases the live authority")
	}
}

func TestReplayArchiveRestoresReceiptsAndLifetimeReservations(t *testing.T) {
	w, first := checkpointTestJourney(t)
	cp := w.ExportCheckpoint()
	reader := &checkpointSliceReader{records: cp.History}
	rebuilt, err := ReplayArchive(cp.Genesis, cp.Head, len(cp.History), reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != len(cp.History)+1 || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("complete reconstruction or final EOF check disagrees with export")
	}
	before := rebuilt.ExportCheckpoint()
	retry, err := rebuilt.Execute(first.Request)
	if err != nil || !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("historical paid receipt did new work after funds were exhausted and its Condition removed", err)
	}
	if got := before.State.RetiredConditionIDs; !reflect.DeepEqual(got, []ConditionInstanceID{"guard:paid", "retired:genesis"}) {
		t.Fatal("initial and later lifetime reservations were not preserved", got)
	}
	changed := cloneRequest(first.Request)
	*changed.Operations[0].Invoke.Arguments[0].Value.ConditionID = "other"
	_, err = rebuilt.Execute(changed)
	var fault *Fault
	if !errors.As(err, &fault) || fault.Code != "E_REQUEST_REUSE" {
		t.Fatal("changed historical intent did not conflict", err)
	}
	_, err = rebuilt.Execute(Request{ID: "reuse-retired", ExpectedRevision: rebuilt.Revision(), Actor: Ref{Kind: CharacterKind, ID: "char:a"}, Operations: []Operation{{ApplyCondition: &ApplyCondition{Instance: "guard:paid", Type: "guard", Target: "char:a"}}}})
	if !errors.As(err, &fault) || fault.Code != "E_DUPLICATE" || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("retired identity was available after reconstruction", err)
	}
	cp.Genesis.Characters[0].Resources["focus"] = Resource{}
	cp.History[0].Events[0].Resource.After.Current = 100
	if !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("archive reader inputs alias reconstructed authority")
	}
	checkpointTestCommit(t, rebuilt, "continue", Operation{Heal: &ResourceOp{Character: "char:a", Resource: "hp", Amount: 1}})
	continued := rebuilt.ExportCheckpoint()
	again, err := ReplayArchive(continued.Genesis, continued.Head, len(continued.History), &checkpointSliceReader{records: continued.History})
	if err != nil || !reflect.DeepEqual(continued, again.ExportCheckpoint()) {
		t.Fatal("reconstructed world cannot continue and reconstruct again", err)
	}
}

func TestReplayArchiveRejectsIncompleteAndContradictoryInput(t *testing.T) {
	w, _ := checkpointTestJourney(t)
	source := w.ExportCheckpoint()
	for _, tc := range []struct {
		name  string
		head  Revision
		count int
	}{
		{"negative count", 0, -1},
		{"oversize count", MaxArchiveBatches + 1, MaxArchiveBatches + 1},
		{"wrong head", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &checkpointSliceReader{records: source.History}
			got, err := ReplayArchive(source.Genesis, tc.head, tc.count, reader)
			if err == nil || got != nil || reader.calls != 0 {
				t.Fatal("invalid metadata read input or returned a World", err)
			}
		})
	}
	if got, err := ReplayArchive(source.Genesis, source.Head, len(source.History), nil); err == nil || got != nil {
		t.Fatal("nil archive reader accepted")
	}
	readFailure := errors.New("injected archive read failure")
	for _, tc := range []struct {
		name string
		edit func(*Checkpoint, *checkpointSliceReader)
	}{
		{"valid prefix truncated", func(_ *Checkpoint, r *checkpointSliceReader) { r.records = r.records[:2] }},
		{"extra batch", func(_ *Checkpoint, r *checkpointSliceReader) { r.records = append(r.records, r.records[2]) }},
		{"late before fact", func(_ *Checkpoint, r *checkpointSliceReader) { r.records[2].Events[0].Resource.Before.Current++ }},
		{"retained intent contradiction", func(_ *Checkpoint, r *checkpointSliceReader) { r.records[2].Request.Operations[0].Spend.Amount++ }},
		{"duplicate request", func(_ *Checkpoint, r *checkpointSliceReader) { r.records[2].Request.ID = r.records[0].Request.ID }},
		{"late group index", func(_ *Checkpoint, r *checkpointSliceReader) { r.records[2].Events[0].Operation++ }},
		{"missing event payload", func(_ *Checkpoint, r *checkpointSliceReader) { r.records[2].Events[0].Resource = nil }},
		{"late read failure", func(_ *Checkpoint, r *checkpointSliceReader) { r.failureAt, r.failure = 2, readFailure }},
		{"EOF read failure", func(_ *Checkpoint, r *checkpointSliceReader) { r.failureAt, r.failure = 3, readFailure }},
		{"EOF with another error", func(_ *Checkpoint, r *checkpointSliceReader) {
			r.failureAt, r.failure = 3, errors.Join(io.EOF, readFailure)
		}},
		{"changed definition", func(cp *Checkpoint, _ *checkpointSliceReader) {
			*cp.Genesis.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = 3
		}},
		{"invalid genesis", func(cp *Checkpoint, _ *checkpointSliceReader) { cp.Genesis.Characters[0].Location = "missing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := w.ExportCheckpoint()
			reader := &checkpointSliceReader{records: cp.History}
			tc.edit(&cp, reader)
			got, err := ReplayArchive(cp.Genesis, cp.Head, len(cp.History), reader)
			if err == nil || got != nil {
				t.Fatal("invalid archive exposed a complete or partial World", err)
			}
			if reader.failure != nil && !errors.Is(err, readFailure) {
				t.Fatal("reader failure was hidden", err)
			}
			if !reflect.DeepEqual(source, w.ExportCheckpoint()) {
				t.Fatal("failed reconstruction changed another live World")
			}
		})
	}
	zero := checkpointTestWorld(t, checkpointTestBlueprint()).ExportCheckpoint()
	rebuilt, err := ReplayArchive(zero.Genesis, 0, 0, &checkpointSliceReader{})
	if err != nil || !reflect.DeepEqual(zero, rebuilt.ExportCheckpoint()) {
		t.Fatal("empty complete archive rejected", err)
	}
}

func TestReplayArchiveCapacityPreservesEarlyReceipt(t *testing.T) {
	w, first := checkpointTestJourney(t)
	var at4096 Snapshot
	for w.Revision() < MaxArchiveBatches {
		checkpointTestCommit(t, w, fmt.Sprintf("capacity:%d", w.Revision()), Operation{Heal: &ResourceOp{Character: "char:a", Resource: "hp", Amount: 1}})
		if w.Revision() == 4096 {
			at4096 = w.Snapshot()
		}
	}
	cp := w.ExportCheckpoint()
	for _, count := range []int{4096, MaxArchiveBatches} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rebuilt, err := ReplayArchive(cp.Genesis, Revision(count), count, &checkpointSliceReader{records: cp.History[:count]})
			if err != nil {
				t.Fatal(err)
			}
			want := cp.State
			if count == 4096 {
				want = at4096
			}
			if !reflect.DeepEqual(want, rebuilt.Snapshot()) || !reflect.DeepEqual(cp.History[:count], rebuilt.History()) {
				t.Fatal("capacity reconstruction lost state or original history")
			}
			retry, err := rebuilt.Execute(first.Request)
			if err != nil || !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(want, rebuilt.Snapshot()) || rebuilt.Revision() != Revision(count) {
				t.Fatal("earliest receipt changed or executed again", err)
			}
		})
	}
	if MaxReplayBatches != 1024 {
		t.Fatal("legacy replay bound changed")
	}
	if prefix, err := Replay(cp.Genesis, cp.History[:1024]); err != nil || prefix.Revision() != 1024 {
		t.Fatal("legacy 1024-batch valid prefix rejected", err)
	}
	if got, err := Replay(cp.Genesis, cp.History[:1025]); err == nil || got != nil {
		t.Fatal("legacy 1025-batch input accepted")
	}
}

func TestCheckpointConcurrentExportsReconstructTheirOwnHead(t *testing.T) {
	w := checkpointTestWorld(t, checkpointTestBlueprint())
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ {
			_, err := w.Execute(Request{ID: fmt.Sprintf("concurrent:%d", i), ExpectedRevision: Revision(i), Actor: Ref{Kind: CharacterKind, ID: "char:a"}, Operations: []Operation{{Heal: &ResourceOp{Character: "char:a", Resource: "hp", Amount: 1}}}})
			if err != nil {
				done <- err
				return
			}
			runtime.Gosched()
		}
		done <- nil
	}()
	for i := 0; i < 64; i++ {
		cp := w.ExportCheckpoint()
		if cp.Head != cp.State.Revision || cp.Head != Revision(len(cp.History)) || cp.Genesis.Characters[0].Resources["hp"].Current != 6 {
			t.Fatal("export combined different revisions or mutated genesis")
		}
		wantHP := int64(6 + cp.Head)
		if wantHP > 10 {
			wantHP = 10
		}
		if cp.State.Characters[0].Resources["hp"].Current != wantHP {
			t.Fatal("exported state does not match the fixed revision/HP oracle")
		}
		rebuilt, err := ReplayArchive(cp.Genesis, cp.Head, len(cp.History), &checkpointSliceReader{records: cp.History})
		if err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
			t.Fatal("concurrent export does not reconstruct its own state and receipts", err)
		}
		runtime.Gosched()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
