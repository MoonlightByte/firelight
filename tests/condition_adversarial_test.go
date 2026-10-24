package tests

import (
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"sort"
	"testing"
)

func TestConditionInitialDataAndDefinitionBounds(t *testing.T) {
	base := func() core.Blueprint {
		b := bareConditions(t)
		b.Conditions = []core.ConditionInstance{{ID: "genesis", Type: "blessing", Target: "char:ada", Source: &core.Ref{Kind: core.ItemKind, ID: "item:venom"}, Magnitude: ptr(int64(2))}}
		b.RetiredConditionIDs = []core.ConditionInstanceID{"reserved"}
		return b
	}
	w := world(t, base())
	stat(t, w, "char:ada", "defense", 12)
	c := conditionByID(t, w, "genesis")
	if c.AppliedRevision != 0 || c.Cause != nil {
		t.Fatal("genesis provenance")
	}
	rejectC(t, w, typedRequest(w, "reserved", applyC("reserved", "marked", "char:ada", nil)))
	execute(t, w, "remove-initial", `remove condition "genesis" from "char:ada";`)
	replay, e := core.Replay(base(), w.History())
	if e != nil || !reflect.DeepEqual(replay.Snapshot(), w.Snapshot()) {
		t.Fatal("initial instance replay", e)
	}
	mutations := []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Conditions[0].ID = "" }, func(b *core.Blueprint) { b.Conditions = append(b.Conditions, b.Conditions[0]) }, func(b *core.Blueprint) { b.Conditions[0].Target = "loc:hall" }, func(b *core.Blueprint) { b.Conditions[0].Source.Kind = core.CharacterKind }, func(b *core.Blueprint) { b.Conditions[0].Type = "unknown" }, func(b *core.Blueprint) { b.Conditions[0].Type = "incompatible" }, func(b *core.Blueprint) { b.Conditions[0].Magnitude = nil }, func(b *core.Blueprint) { b.Conditions[0].Magnitude = ptr(int64(math.MaxInt64)) }, func(b *core.Blueprint) { b.Conditions[0].AppliedRevision = 1 }, func(b *core.Blueprint) {
			b.Conditions[0].Cause = &core.ConditionCause{RequestID: "made-up", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}}
		}, func(b *core.Blueprint) {
			b.Conditions[0].AppliedRevision = 1
			b.Conditions[0].Cause = &core.ConditionCause{RequestID: "made-up", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}}
		}, func(b *core.Blueprint) { b.RetiredConditionIDs = []core.ConditionInstanceID{"genesis"} }, func(b *core.Blueprint) { b.RetiredConditionIDs = []core.ConditionInstanceID{""} }, func(b *core.Blueprint) { b.RetiredConditionIDs = append(b.RetiredConditionIDs, "reserved") }, func(b *core.Blueprint) {
			b.ConditionDefinitions = append(b.ConditionDefinitions, b.ConditionDefinitions[0])
		}, func(b *core.Blueprint) { b.ConditionDefinitions[0].ID = "" }, func(b *core.Blueprint) { b.ConditionDefinitions[0].Instances = "implicit" }, func(b *core.Blueprint) { b.ConditionDefinitions[0].Modifiers[0].ID = "" }, func(b *core.Blueprint) { b.ConditionDefinitions[0].Modifiers[0].Stat = "" }, func(b *core.Blueprint) { b.ConditionDefinitions[0].Modifiers[0].Magnitude = false }, func(b *core.Blueprint) { b.ConditionDefinitions[0].Modifiers[0].Fixed = ptr(int64(2)) }, func(b *core.Blueprint) {
			b.ConditionDefinitions[0].Modifiers = append(b.ConditionDefinitions[0].Modifiers, b.ConditionDefinitions[0].Modifiers[0])
		},
	}
	for i, mutate := range mutations {
		b := base()
		mutate(&b)
		if rw, e := core.NewWorld(b); e == nil || rw != nil {
			t.Fatalf("invalid initial data %d accepted", i)
		}
	}
	for _, count := range []int{16, 17} {
		b := bareConditions(t)
		d := &b.ConditionDefinitions[0]
		d.Modifiers = nil
		for j := 0; j < count; j++ {
			d.Modifiers = append(d.Modifiers, core.ConditionModifier{ID: core.ConditionModifierID(fmt.Sprint(j)), Stat: "defense", Magnitude: true})
		}
		rw, e := core.NewWorld(b)
		if count == 17 {
			if e == nil || rw != nil {
				t.Fatal("17 templates accepted")
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		batch := commitC(t, rw, typedRequest(rw, "bound", applyC("bound", "blessing", "char:ada", ptr(int64(1)))))
		stat(t, rw, "char:ada", "defense", 26)
		if len(batch.Events) != 1 || len(batch.Events[0].Condition.AfterEffects) != 16 {
			t.Fatal("event bound")
		}
	}
}
func TestConditionOwnedEffectsAndIdentityCollisions(t *testing.T) {
	b := bareConditions(t)
	w := world(t, b)
	commitC(t, w, typedRequest(w, "apply", applyC("a", "blessing", "char:ada", ptr(int64(2)))))
	derived := w.Snapshot().ConditionEffects[0]
	if derived.ID != "condition:1:a:4:ward" {
		t.Fatal("generated identity oracle", derived.ID)
	}
	direct := core.Effect{ID: derived.ID, Source: core.Ref{Kind: core.CharacterKind, ID: "char:bea"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Stat: "defense", Amount: 2}
	rejectC(t, w, typedRequest(w, "collision", core.Operation{Apply: &direct}, removeC("a", "char:ada")))
	for _, o := range []core.Operation{{Remove: &core.EffectOp{Effect: derived.ID}}, {Expire: &core.EffectOp{Effect: derived.ID}}} {
		rejectC(t, w, typedRequest(w, "owned", o))
	}
	for i := 0; i < 3; i++ {
		forged := direct
		forged.ID = core.EffectID(fmt.Sprintf("forged%d", i))
		if i != 0 {
			forged.Source = derived.Source
		}
		if i != 1 {
			forged.ConditionOrigin = derived.ConditionOrigin
		}
		rejectC(t, w, typedRequest(w, "forged", core.Operation{Apply: &forged}))
		init := bareConditions(t)
		init.Effects = []core.Effect{forged}
		if _, e := core.NewWorld(init); e == nil {
			t.Fatal("forged initial effect")
		}
	}
	r := typedRequest(w, "actor", applyC("mark", "marked", "char:ada", nil))
	r.Actor = derived.Source
	rejectC(t, w, r)
	relation := core.Relationship{From: derived.Source, Kind: "link", To: direct.Target}
	rejectC(t, w, typedRequest(w, "relation", core.Operation{Relate: &relation}))
	relation.From, relation.To = relation.To, relation.From
	rejectC(t, w, typedRequest(w, "relation2", core.Operation{Relate: &relation}))
	if _, ok := w.Snapshot().Blueprint.KindOf("a"); ok {
		t.Fatal("condition widened entity catalog")
	}
	// No reserved M1 prefix: direct identity is legal in the absence of a collision.
	b.Effects = []core.Effect{direct}
	w = world(t, b)
	rejectC(t, w, typedRequest(w, "reverse", applyC("a", "blessing", "char:ada", ptr(int64(2))), core.Operation{Remove: &core.EffectOp{Effect: direct.ID}}))
	b.Conditions = []core.ConditionInstance{{ID: "a", Type: "blessing", Target: "char:ada", Magnitude: ptr(int64(2))}}
	if _, e := core.NewWorld(b); e == nil {
		t.Fatal("initial collision accepted")
	}
	commitC(t, w, typedRequest(w, "ordered", core.Operation{Remove: &core.EffectOp{Effect: direct.ID}}, applyC("a", "blessing", "char:ada", ptr(int64(2)))))
	commitC(t, w, typedRequest(w, "direct-after", removeC("a", "char:ada"), core.Operation{Apply: &direct}))
	stat(t, w, "char:ada", "defense", 12)
	// Delimiter and UTF-8 identities must not collide under pair encoding.
	b = bareConditions(t)
	b.ConditionDefinitions[0].Modifiers = append(b.ConditionDefinitions[0].Modifiers, core.ConditionModifier{ID: "a:4:ward", Stat: "defense", Magnitude: true}, core.ConditionModifier{ID: "é:λ", Stat: "defense", Magnitude: true})
	w = world(t, b)
	for i, id := range []string{"a", "a:4:ward", "é:λ", "1:a:4:ward"} {
		commitC(t, w, typedRequest(w, fmt.Sprint(i), applyC(id, "blessing", "char:ada", ptr(int64(1)))))
	}
	seen := map[core.EffectID]bool{}
	for _, e := range w.Snapshot().ConditionEffects {
		if seen[e.ID] {
			t.Fatal("generated collision")
		}
		seen[e.ID] = true
	}
	if len(seen) != 12 {
		t.Fatal("missing template effects")
	}
	stat(t, w, "char:ada", "defense", 22)
}
func TestConditionNestedDetachmentAndReceiptTruth(t *testing.T) {
	b := bareConditions(t)
	b.Conditions = []core.ConditionInstance{{ID: "initial", Type: "blessing", Target: "char:ada", Source: &core.Ref{Kind: core.ItemKind, ID: "item:venom"}, Magnitude: ptr(int64(2))}}
	b.RetiredConditionIDs = []core.ConditionInstanceID{"old"}
	w := world(t, b)
	baseline, history := w.Snapshot(), w.History()
	b.ConditionDefinitions[0].Modifiers[0].Stat = "invalid"
	*b.ConditionDefinitions[3].Modifiers[0].Fixed = 99
	b.Conditions[0].Source.ID = "invalid"
	*b.Conditions[0].Magnitude = 99
	b.RetiredConditionIDs[0] = "invalid"
	unchanged(t, w, baseline, history)
	op := applyC("new", "blessing", "char:ada", ptr(int64(3)))
	op.ApplyCondition.Source = &core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}
	r := typedRequest(w, "apply", op)
	receipt := commitC(t, w, r)
	originalRequest := w.History()[0].Request
	originalReceipt := w.History()[0]
	baseline, history = w.Snapshot(), w.History()
	*r.Operations[0].ApplyCondition.Magnitude = 900
	r.Operations[0].ApplyCondition.Source.ID = "forged"
	receipt.Request.Operations[0].ApplyCondition.Source.ID = "forged"
	*receipt.Request.Operations[0].ApplyCondition.Magnitude = 99
	receipt.Events[0].Condition.After.Source.ID = "forged"
	*receipt.Events[0].Condition.After.Magnitude = 99
	receipt.Events[0].Condition.After.Cause.RequestID = "forged"
	receipt.Events[0].Condition.AfterEffects[0].ConditionOrigin.Instance = "forged"
	unchanged(t, w, baseline, history)
	baseline, history = w.Snapshot(), w.History()
	tampered := w.Snapshot()
	tampered.Conditions[0].Source.ID = "forged"
	*tampered.Conditions[0].Magnitude = 99
	tampered.Conditions[1].Cause.Actor.ID = "forged"
	tampered.ConditionEffects[0].ConditionOrigin.Modifier = "forged"
	tampered.ConditionDefinitions[0].Modifiers[0].Stat = "forged"
	for _, d := range tampered.ConditionDefinitions {
		for _, m := range d.Modifiers {
			if m.Fixed != nil {
				*m.Fixed = 99
			}
		}
	}
	tampered.RetiredConditionIDs[0] = "forged"
	h := w.History()
	h[0].Events[0].Condition.After.Cause.Operation = 99
	h[0].Events[0].Condition.AfterEffects[0].Amount = 99
	*h[0].Request.Operations[0].ApplyCondition.Magnitude = 99
	p, _ := w.CharacterStatus("char:ada")
	p.Conditions[0].Type = "forged"
	p.ActiveEffects[0].ConditionOrigin.Instance = "forged"
	p.Stats[0].Contributions[0].ConditionOrigin.Modifier = "forged"
	ex, _ := w.Explain("char:ada", "defense")
	ex.Contributions[0].ConditionOrigin.Instance = "forged"
	unchanged(t, w, baseline, history)
	again := commitC(t, w, originalRequest)
	if !reflect.DeepEqual(again, originalReceipt) {
		t.Fatal("returned aliases changed retained receipt")
	}
	removal := commitC(t, w, typedRequest(w, "remove", removeC("new", "char:ada")))
	saved := w.History()[1]
	removal.Events[0].Condition.Before.Source.ID = "forged"
	removal.Events[0].Condition.Before.Cause.RequestID = "forged"
	removal.Events[0].Condition.BeforeEffects[0].ConditionOrigin.Instance = "forged"
	retry := commitC(t, w, saved.Request)
	if !reflect.DeepEqual(retry, saved) {
		t.Fatal("remove receipt alias")
	}
	stat(t, w, "char:ada", "defense", 12)
}
func TestConditionReplayOrderingAndMixedRollback(t *testing.T) {
	b := bareConditions(t)
	w := world(t, b)
	commitC(t, w, typedRequest(w, "fixed", applyC("fixed", "steady", "char:ada", nil)))
	h := w.History()
	effects := h[0].Events[0].Condition.AfterEffects
	effects[0], effects[1] = effects[1], effects[0]
	if rw, e := core.Replay(b, h); e == nil || rw != nil {
		t.Fatal("reordered consequences accepted")
	}
	rejectC(t, w, typedRequest(w, "mixed", removeC("fixed", "char:ada"), applyC("tentative", "marked", "char:ada", nil), core.Operation{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 3}}, applyC("overflow", "blessing", "char:ada", ptr(int64(math.MaxInt64)))))
	commitC(t, w, typedRequest(w, "mixed", applyC("tentative", "marked", "char:ada", nil)))
	// Initial declarations resolve forward references and retain exact source absence.
	src := `condition "initial" of "marker" to "c"; condition type "marker" { instances coexist; } rules { transfer unequips; wear any; } location "l" named "L"; character "c" named "C" at "l" {}`
	initial, ds := compiler.CompileWorld("forward.nql", src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	rw := world(t, initial)
	if len(rw.Snapshot().Conditions) != 1 || rw.Snapshot().Conditions[0].Source != nil {
		t.Fatal("initial condition declaration")
	}
}

// Independent small model: IDs never recycle; accepted coexist additions add
// their signed magnitude; removals subtract that exact instance contribution.
func FuzzConditionSequence(f *testing.F) {
	for _, seed := range [][]byte{{0, 1, 2, 3, 4, 5, 6, 7}, {255, 0, 2, 8, 4, 9}, {}, {1, 1, 1, 1}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			return
		}
		initial := bareConditions(t)
		w := world(t, initial)
		active := map[string]int64{}
		used := map[string]bool{}
		var revision core.Revision
		var total int64 = 10
		for i, v := range data {
			id := fmt.Sprintf("id%d", v%8)
			amount := int64(v%7) - 3
			add := v&8 == 0
			expected := false
			var o core.Operation
			if add {
				o = applyC(id, "blessing", "char:ada", ptr(amount))
				expected = !used[id]
			} else {
				o = removeC(id, "char:ada")
				_, expected = active[id]
			}
			before, history := w.Snapshot(), w.History()
			r := typedRequest(w, fmt.Sprintf("request%d", i), o)
			batch, err := w.Execute(r)
			if (err == nil) != expected {
				t.Fatalf("model disagrees: byte=%d err=%v", v, err)
			}
			if expected {
				revision++
				if add {
					active[id] = amount
					used[id] = true
					total += amount
				} else {
					total -= active[id]
					delete(active, id)
				}
				retry, e := w.Execute(r)
				if e != nil || !reflect.DeepEqual(batch, retry) {
					t.Fatal("retry changed")
				}
			} else {
				unchanged(t, w, before, history)
			}
			stat(t, w, "char:ada", "defense", total)
			s := w.Snapshot()
			if s.Revision != revision || len(s.Conditions) != len(active) {
				t.Fatal("independent model state mismatch")
			}
			for _, c := range s.Conditions {
				want, ok := active[string(c.ID)]
				if !ok || *c.Magnitude != want {
					t.Fatal("instance mismatch")
				}
			}
		}
		encoded, e := json.Marshal(w.History())
		if e != nil {
			t.Fatal(e)
		}
		var history []core.Batch
		if e = json.Unmarshal(encoded, &history); e != nil {
			t.Fatal(e)
		}
		replay, e := core.Replay(initial, history)
		if e != nil || !reflect.DeepEqual(replay.Snapshot(), w.Snapshot()) {
			t.Fatal("sequence replay mismatch", e)
		}
		var retired []string
		for id := range used {
			if _, ok := active[id]; !ok {
				retired = append(retired, id)
			}
		}
		sort.Strings(retired)
		got := w.Snapshot().RetiredConditionIDs
		if len(got) != len(retired) {
			t.Fatal("retired count")
		}
		for i, id := range retired {
			if string(got[i]) != id {
				t.Fatal("retired identity mismatch")
			}
		}
	})
}
