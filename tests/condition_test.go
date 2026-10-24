package tests

import (
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func conditionFixture(t testing.TB) core.Blueprint {
	t.Helper()
	src, e := os.ReadFile("../examples/condition-world.nql")
	if e != nil {
		t.Fatal(e)
	}
	b, ds := compiler.CompileWorld("condition-world.nql", string(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}
func bareConditions(t testing.TB) core.Blueprint { b := conditionFixture(t); b.Effects = nil; return b }
func ptr[T any](v T) *T                          { return &v }
func applyC(id, typ string, target core.CharacterID, amount *int64) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: core.ConditionInstanceID(id), Type: core.ConditionTypeID(typ), Target: target, Magnitude: amount}}
}
func removeC(id string, target core.CharacterID) core.Operation {
	return core.Operation{RemoveCondition: &core.RemoveCondition{Instance: core.ConditionInstanceID(id), Target: target}}
}
func typedRequest(w *core.World, id string, ops ...core.Operation) core.Request {
	return core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops}
}
func rejectC(t testing.TB, w *core.World, r core.Request) {
	t.Helper()
	s, h := w.Snapshot(), w.History()
	if _, e := w.Execute(r); e == nil {
		t.Fatal("invalid Condition request accepted", r)
	}
	unchanged(t, w, s, h)
}
func commitC(t testing.TB, w *core.World, r core.Request) core.Batch {
	t.Helper()
	b, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func conditionByID(t testing.TB, w *core.World, id core.ConditionInstanceID) core.ConditionInstance {
	t.Helper()
	for _, c := range w.Snapshot().Conditions {
		if c.ID == id {
			return c
		}
	}
	t.Fatal("missing condition", id)
	return core.ConditionInstance{}
}
func TestConditionCoexistenceProvenanceAndExactRemoval(t *testing.T) {
	b := bareConditions(t)
	w := world(t, b)
	a := execute(t, w, "a", `apply condition "a" of "blessing" to "char:ada" from character "char:bea" magnitude 2;`)
	execute(t, w, "b", `apply condition "b" of "blessing" to "char:ada" magnitude 3 from location "loc:shrine";`)
	stat(t, w, "char:ada", "defense", 15)
	c := conditionByID(t, w, "a")
	if c.Type != "blessing" || c.Target != "char:ada" || c.Source.Kind != core.CharacterKind || c.Source.ID != "char:bea" || *c.Magnitude != 2 || c.AppliedRevision != 1 || c.Cause.RequestID != "a" || c.Cause.Operation != 0 || c.Cause.Actor.ID != "char:ada" {
		t.Fatal("application provenance", c)
	}
	ev := a.Events[0]
	if ev.Kind != core.ConditionApplied || ev.Condition.Before != nil || ev.Condition.BeforeEffects != nil || len(ev.Condition.AfterEffects) != 1 || ev.Condition.AfterEffects[0].Amount != 2 || ev.Condition.AfterEffects[0].ConditionOrigin.Instance != "a" {
		t.Fatal("typed apply event", ev)
	}
	ex, _ := w.Explain("char:ada", "defense")
	if len(ex.Contributions) != 2 || ex.Contributions[0].ConditionOrigin == nil || ex.Contributions[1].ConditionOrigin == nil {
		t.Fatal("lost effect provenance", ex)
	}
	removed := execute(t, w, "remove-a", `remove condition "a" from "char:ada";`)
	stat(t, w, "char:ada", "defense", 13)
	if len(w.Snapshot().Conditions) != 1 || conditionByID(t, w, "b").Source.ID != "loc:shrine" || !reflect.DeepEqual(removed.Events[0].Condition.Before, &c) || removed.Events[0].Condition.After != nil || removed.Events[0].Condition.BeforeEffects[0].Amount != 2 {
		t.Fatal("exact removal facts")
	}
	// Identical contributors falsify removal by source, type, target or amount.
	w = world(t, b)
	execute(t, w, "twins", `apply condition "a" of "blessing" to "char:ada" from character "char:bea" magnitude 2; apply condition "b" of "blessing" to "char:ada" from character "char:bea" magnitude 2; apply effect "decoy" from character "char:bea" on character "char:ada" stat "defense" add 2;`)
	stat(t, w, "char:ada", "defense", 16)
	execute(t, w, "remove", `remove condition "a" from "char:ada";`)
	stat(t, w, "char:ada", "defense", 14)
	s := w.Snapshot()
	if len(s.Conditions) != 1 || s.Conditions[0].ID != "b" || len(s.Effects) != 1 || s.Effects[0].ID != "decoy" || len(s.ConditionEffects) != 1 || s.ConditionEffects[0].ConditionOrigin.Instance != "b" {
		t.Fatal("removal destroyed sibling/decoy")
	}
}
func TestConditionAuthoredPolicyAndLifetime(t *testing.T) {
	b := bareConditions(t)
	for _, policy := range []core.InstancePolicy{core.Coexist, core.Unique} {
		b.ConditionDefinitions[0].Instances = policy
		w := world(t, b)
		commitC(t, w, typedRequest(w, "first", applyC("a", "blessing", "char:ada", ptr(int64(2)))))
		r := typedRequest(w, "second", applyC("b", "blessing", "char:ada", ptr(int64(3))))
		if policy == core.Unique {
			rejectC(t, w, r)
		} else {
			commitC(t, w, r)
			stat(t, w, "char:ada", "defense", 15)
		}
		commitC(t, w, typedRequest(w, "other-target", applyC("other", "blessing", "char:bea", ptr(int64(1)))))
	}
	w := world(t, b)
	commitC(t, w, typedRequest(w, "a", applyC("a", "unique-mark", "char:ada", nil)))
	rejectC(t, w, typedRequest(w, "reverse", applyC("b", "unique-mark", "char:ada", nil), removeC("a", "char:ada")))
	commitC(t, w, typedRequest(w, "replace", removeC("a", "char:ada"), applyC("b", "unique-mark", "char:ada", nil)))
	rejectC(t, w, typedRequest(w, "retired", applyC("a", "marked", "char:bea", nil)))
	rejectC(t, w, typedRequest(w, "cycle", applyC("cycle", "marked", "char:ada", nil), removeC("cycle", "char:ada"), applyC("cycle", "marked", "char:ada", nil)))
	commitC(t, w, typedRequest(w, "cycle", applyC("cycle", "marked", "char:ada", nil))) // failure reserved neither request nor instance
	one := commitC(t, w, typedRequest(w, "ephemeral", applyC("ephemeral", "marked", "char:ada", nil), removeC("ephemeral", "char:ada")))
	if len(one.Events) != 2 || one.Events[0].Condition.AfterEffects != nil || one.Events[1].Condition.BeforeEffects != nil {
		t.Fatal("nonmodifier event representation")
	}
	rejectC(t, w, typedRequest(w, "recycle", applyC("ephemeral", "marked", "char:bea", nil)))
}
func TestConditionNonModifierAndUnrelatedFacts(t *testing.T) {
	w := world(t, conditionFixture(t))
	before := w.Snapshot()
	for i, source := range []string{"", ` from item "item:venom"`, ` from location "loc:shrine"`, ` from character "char:cy"`} {
		id := fmt.Sprintf("m%d", i)
		execute(t, w, id, fmt.Sprintf(`apply condition %q of "marked" to "char:ada"%s;`, id, source))
		c := conditionByID(t, w, core.ConditionInstanceID(id))
		if (source == "") != (c.Source == nil) {
			t.Fatal("optional source inferred")
		}
	}
	stat(t, w, "char:ada", "defense", 12)
	for i := 0; i < 4; i++ {
		execute(t, w, fmt.Sprintf("r%d", i), fmt.Sprintf(`remove condition "m%d" from "char:ada";`, i))
	}
	after := w.Snapshot()
	if after.Revision != 8 || len(after.Conditions) != 0 || len(after.ConditionEffects) != 0 || len(after.RetiredConditionIDs) != 4 {
		t.Fatal("nonmodifier lifecycle")
	}
	after.Revision = before.Revision
	after.Conditions = before.Conditions
	after.RetiredConditionIDs = before.RetiredConditionIDs
	if !reflect.DeepEqual(after, before) {
		t.Fatal("condition mutated unrelated canonical facts")
	}
}
func TestConditionEquipmentCompositionAndWearRegression(t *testing.T) {
	for _, conditionFirst := range []bool{true, false} {
		w := world(t, conditionFixture(t))
		execute(t, w, "bless", `apply condition "b" of "blessing" to "char:ada" magnitude 3;`)
		stat(t, w, "char:ada", "defense", 15)
		rejectC(t, w, request(t, w, "steal-wear", `equip "item:shield" on "char:bea";`))
		if conditionFirst {
			execute(t, w, "r", `remove condition "b" from "char:ada";`)
			stat(t, w, "char:ada", "defense", 12)
			execute(t, w, "u", `unequip "item:shield";`)
		} else {
			execute(t, w, "u", `unequip "item:shield";`)
			stat(t, w, "char:ada", "defense", 13)
			execute(t, w, "r", `remove condition "b" from "char:ada";`)
		}
		stat(t, w, "char:ada", "defense", 10)
		execute(t, w, "equip", `equip "item:shield" on "char:bea";`)
		stat(t, w, "char:bea", "defense", 12)
	}
	w := world(t, bareConditions(t))
	execute(t, w, "fixed", `apply condition "s" of "steady" to "char:ada";`)
	stat(t, w, "char:ada", "defense", 13)
	stat(t, w, "char:ada", "resolve", 9)
	b := bareConditions(t)
	*b.ConditionDefinitions[3].Modifiers[0].Fixed = 7
	w = world(t, b)
	execute(t, w, "same-binary", `apply condition "s" of "steady" to "char:ada";`)
	stat(t, w, "char:ada", "defense", 17)
}
func TestConditionInvalidIRRollbackAndContinuation(t *testing.T) {
	bads := []core.Operation{
		applyC("missing-type", "missing", "char:ada", nil), applyC("bad-target", "marked", "loc:hall", nil), applyC("missing-target", "marked", "char:missing", nil), applyC("", "marked", "char:ada", nil), applyC("bad\nID", "marked", "char:ada", nil), applyC("missing-stat", "incompatible", "char:ada", nil), applyC("missing-amount", "blessing", "char:ada", nil), applyC("unused-amount", "marked", "char:ada", ptr(int64(1))), applyC("overflow", "blessing", "char:ada", ptr(int64(math.MaxInt64))), removeC("missing", "char:ada"), removeC("existing", "char:bea"), applyC("existing", "marked", "char:ada", nil),
	}
	for _, source := range []core.Ref{{Kind: core.CharacterKind, ID: "item:venom"}, {Kind: core.ItemKind, ID: "no-item"}, {Kind: core.ConditionKind, ID: "existing"}} {
		o := applyC("source", "marked", "char:ada", nil)
		o.ApplyCondition.Source = ptr(source)
		bads = append(bads, o)
	}
	for i, o := range bads {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			w := world(t, conditionFixture(t))
			commitC(t, w, typedRequest(w, "seed", applyC("existing", "marked", "char:ada", nil)))
			r := typedRequest(w, "failed", core.Operation{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 2}}, applyC("tentative", "marked", "char:ada", nil), removeC("existing", "char:ada"), o)
			// Keep the existing instance active for duplicate/wrong-target cases.
			r.Operations[2] = core.Operation{Move: &core.Move{Character: "char:ada", To: "loc:shrine"}}
			rejectC(t, w, r)
			commitC(t, w, typedRequest(w, "failed", applyC("tentative", "marked", "char:ada", nil)))
		})
	}
	w := world(t, bareConditions(t))
	o := applyC("both", "marked", "char:ada", nil)
	o.RemoveCondition = &core.RemoveCondition{Instance: "x", Target: "char:ada"}
	rejectC(t, w, typedRequest(w, "bad-sum", o))
}
func TestConditionNumericFinalCandidateAndTransferProjection(t *testing.T) {
	b := bareConditions(t)
	b.Characters[0].Stats["defense"] = math.MaxInt64
	w := world(t, b)
	r := typedRequest(w, "cancel", applyC("positive", "blessing", "char:ada", ptr(int64(1))), applyC("negative", "blessing", "char:ada", ptr(int64(-1))))
	commitC(t, w, r)
	stat(t, w, "char:ada", "defense", math.MaxInt64)
	rejectC(t, w, typedRequest(w, "overflow-removal", removeC("negative", "char:ada")))
	commitC(t, w, typedRequest(w, "remove-both", removeC("negative", "char:ada"), removeC("positive", "char:ada")))
	replay, e := core.Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(replay.Snapshot(), w.Snapshot()) {
		t.Fatal("transient overflow replay", e)
	}
	b = conditionFixture(t)
	b.Characters[0].Stats["defense"] = math.MaxInt64
	b.Effects[0].Amount = -1
	w = world(t, b)
	commitC(t, w, typedRequest(w, "positive", applyC("positive", "blessing", "char:ada", ptr(int64(1)))))
	p, e := w.TransferView("item:shield", "char:cy")
	if e != nil {
		t.Fatal(e)
	}
	decision, e := p.Assess()
	if e != nil || decision.Allowed {
		t.Fatal("projection missed condition-dependent overflow")
	}
	rejectC(t, w, request(t, w, "transfer", `transfer "item:shield" from "char:bea" to "char:cy";`))
	delete(*p.WearerTotals, "defense")
	if p.Validate() == nil {
		t.Fatal("under-inclusion undetected")
	}
}
func TestConditionReplayReceiptsAndCorruption(t *testing.T) {
	b := bareConditions(t)
	w := world(t, b)
	r1 := commitC(t, w, typedRequest(w, "R1", applyC("bless", "blessing", "char:ada", ptr(int64(2)))))
	r2 := commitC(t, w, typedRequest(w, "R2", applyC("mark", "marked", "char:ada", nil)))
	r3 := commitC(t, w, typedRequest(w, "R3", removeC("bless", "char:ada")))
	stat(t, w, "char:ada", "defense", 10)
	s := w.Snapshot()
	if s.Revision != 3 || len(s.Conditions) != 1 || s.Conditions[0].ID != "mark" || len(s.ConditionEffects) != 0 || !reflect.DeepEqual(s.RetiredConditionIDs, []core.ConditionInstanceID{"bless"}) {
		t.Fatal("replay fixed final oracle")
	}
	roundtrip := func() []core.Batch {
		encoded, e := json.Marshal(w.History())
		if e != nil {
			t.Fatal(e)
		}
		var h []core.Batch
		if e = json.Unmarshal(encoded, &h); e != nil {
			t.Fatal(e)
		}
		return h
	}
	rebuilt, e := core.Replay(b, roundtrip())
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(rebuilt.Snapshot(), s) || !reflect.DeepEqual(rebuilt.History(), w.History()) {
		t.Fatal("replay differs")
	}
	for _, receipt := range []core.Batch{r1, r2, r3} {
		again := commitC(t, rebuilt, receipt.Request)
		if !reflect.DeepEqual(again, receipt) {
			t.Fatal("lost exact receipt")
		}
	}
	unchanged(t, rebuilt, s, w.History())
	conflict := r1.Request
	conflict.Operations = []core.Operation{applyC("other", "marked", "char:ada", nil)}
	rejectC(t, rebuilt, conflict)
	rejectC(t, rebuilt, typedRequest(rebuilt, "retired", applyC("bless", "marked", "char:ada", nil)))
	commitC(t, rebuilt, typedRequest(rebuilt, "R4", applyC("zero", "blessing", "char:ada", ptr(int64(0))), removeC("zero", "char:ada")))
	again, e := core.Replay(b, rebuilt.History())
	if e != nil || !reflect.DeepEqual(again.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("continuation replay", e)
	}
	mutations := []func([]core.Batch){
		func(h []core.Batch) { h[0].Events[0].Condition.After.Type = "marked" }, func(h []core.Batch) {
			h[0].Events[0].Condition.After.Source = &core.Ref{Kind: core.ItemKind, ID: "item:venom"}
		}, func(h []core.Batch) { h[0].Events[0].Condition.After.Cause.RequestID = "forged" }, func(h []core.Batch) { h[0].Events[0].Condition.After.Cause.Operation = 1 }, func(h []core.Batch) { h[0].Events[0].Condition.After.Cause.Actor.ID = "char:bea" }, func(h []core.Batch) { h[0].Events[0].Condition.After.AppliedRevision = 2 }, func(h []core.Batch) { h[0].Events[0].Condition.AfterEffects[0].Amount = 9 }, func(h []core.Batch) { h[0].Events[0].Condition.AfterEffects[0].ConditionOrigin.Instance = "mark" }, func(h []core.Batch) { h[0].Events[0].Condition.AfterEffects = nil }, func(h []core.Batch) {
			h[0].Events[0].Condition.AfterEffects = append(h[0].Events[0].Condition.AfterEffects, h[0].Events[0].Condition.AfterEffects[0])
		}, func(h []core.Batch) { h[1].Events[0].Condition.AfterEffects = []core.Effect{} }, func(h []core.Batch) { h[2].Events[0].Condition.Before.Target = "char:bea" }, func(h []core.Batch) { h[2].Events[0].Condition = nil }, func(h []core.Batch) { h[1].Events = nil }, func(h []core.Batch) { h[0].Request.Operations[0].ApplyCondition.Magnitude = ptr(int64(3)) },
	}
	for i, mutate := range mutations {
		h := roundtrip()
		mutate(h)
		rw, e := core.Replay(b, h)
		if e == nil || rw != nil {
			t.Fatalf("corrupt log %d reconstructed", i)
		}
	}
	prefix, e := core.Replay(b, w.History()[:2])
	if e != nil || prefix.Revision() != 2 {
		t.Fatal("valid historical prefix rejected", e)
	}
	stat(t, prefix, "char:ada", "defense", 12)
}
func TestConditionProjectionFreshnessAndHiddenProvenance(t *testing.T) {
	b := bareConditions(t)
	b.Locations = append(b.Locations, core.Location{ID: "hidden-source-sentinel", Name: "Hidden"})
	b.Characters = append(b.Characters, core.Character{ID: "hidden-actor-sentinel", Name: "Hidden", Location: "loc:hall"})
	w := world(t, b)
	old, _ := w.CharacterStatus("char:ada")
	stale := typedRequest(w, "stale", applyC("stale", "marked", "char:ada", nil))
	op := applyC("public-instance", "blessing", "char:ada", ptr(int64(3)))
	op.ApplyCondition.Source = &core.Ref{Kind: core.LocationKind, ID: "hidden-source-sentinel"}
	r := typedRequest(w, "hidden-request-sentinel", op)
	r.Actor.ID = "hidden-actor-sentinel"
	commitC(t, w, r)
	now, _ := w.CharacterStatus("char:ada")
	transfer, _ := w.TransferView("item:shield", "char:cy")
	full, _ := json.Marshal(w.Snapshot())
	statusJSON, _ := json.Marshal(now)
	transferJSON, _ := json.Marshal(transfer)
	for _, sentinel := range []string{"hidden-source-sentinel", "hidden-actor-sentinel", "hidden-request-sentinel"} {
		if !strings.Contains(string(full), sentinel) || strings.Contains(string(statusJSON), sentinel) || strings.Contains(string(transferJSON), sentinel) {
			t.Fatal("hidden provenance control failed", sentinel)
		}
	}
	if old.Revision != 0 || len(old.Conditions) != 0 || now.Revision != 1 || len(now.Conditions) != 1 || now.Conditions[0].Instance != "public-instance" || !strings.Contains(string(statusJSON), `"modifier":"ward"`) {
		t.Fatal("projection freshness/provenance")
	}
	rejectC(t, w, stale)
	execute(t, w, "remove", `remove condition "public-instance" from "char:ada";`)
	end, _ := w.CharacterStatus("char:ada")
	if old.Stats[0].Stat != "defense" || old.Stats[0].Effective != 10 || now.Stats[0].Effective != 13 || end.Stats[0].Effective != 10 {
		t.Fatal("projected/historical defense should remain 10 -> 13 -> 10")
	}
	if end.Revision != 2 || len(end.Conditions) != 0 || len(now.Conditions) != 1 {
		t.Fatal("historical projection changed")
	}
	now.Conditions[0].Instance = "forged"
	now.ActiveEffects[0].Amount = 99
	now.Stats[0].Contributions[0].ConditionOrigin.Instance = "forged"
	stat(t, w, "char:ada", "defense", 10)
}
func TestConditionConcurrentPublicationAndRetries(t *testing.T) {
	for _, identical := range []bool{false, true} {
		w := world(t, bareConditions(t))
		copyHandle := *w
		var count atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id := fmt.Sprint(i)
				if identical {
					id = "same"
				}
				r := core.Request{ID: id, ExpectedRevision: 0, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{applyC(id, "blessing", "char:ada", ptr(int64(2)))}}
				handle := w
				if i%2 == 0 {
					handle = &copyHandle
				}
				if _, e := handle.Execute(r); e == nil {
					count.Add(1)
				}
				_ = handle.Snapshot()
				_, _ = handle.CharacterStatus("char:ada")
				_ = handle.History()
			}(i)
		}
		wg.Wait()
		want := int32(1)
		if identical {
			want = 20
		}
		if count.Load() != want || w.Revision() != 1 || len(w.History()) != 1 || len(w.Snapshot().Conditions) != 1 {
			t.Fatal("concurrent apply corrupted authority")
		}
		stat(t, w, "char:ada", "defense", 12)
		id := string(w.Snapshot().Conditions[0].ID)
		r := typedRequest(w, "remove", removeC(id, "char:ada"))
		count.Store(0)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, e := copyHandle.Execute(r); e == nil {
					count.Add(1)
				}
			}()
		}
		wg.Wait()
		if count.Load() != 20 || w.Revision() != 2 || len(w.History()) != 2 || len(w.Snapshot().Conditions) != 0 {
			t.Fatal("concurrent remove duplicated")
		}
	}
}
