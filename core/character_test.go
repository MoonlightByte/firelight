package core

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func creationFixture() Blueprint {
	return Blueprint{Rules: Rules{Transfer: Unequips, Wear: WearAny}, Clock: &Clock{Unit: "tick"},
		Locations:            []Location{{ID: "hall"}, {ID: "street"}},
		Characters:           []Character{{ID: "actor", Name: "Mira", Location: "hall", Stats: map[StatID]int64{"x": math.MaxInt64}}},
		Items:                []Item{{ID: "sword", Owner: "actor"}},
		ConditionDefinitions: []ConditionDefinition{{ID: "training", Instances: Unique}, {ID: "cover", Instances: Coexist, Modifiers: []ConditionModifier{{ID: "bonus", Stat: "defense", Magnitude: true}}}}}
}
func creationRequest(id string, rev Revision, ops ...Operation) Request {
	return Request{ID: id, ExpectedRevision: rev, Actor: Ref{CharacterKind, "actor"}, Operations: ops}
}
func newNPC() *Character {
	return &Character{ID: "npc", Name: "Mira", Location: "hall", Stats: map[StatID]int64{"defense": 10}, Resources: map[ResourceID]Resource{"hp": {Current: 8, Maximum: 12}}}
}
func creationWorld(t *testing.T, b Blueprint) *World {
	t.Helper()
	w, err := NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func creationReject(t *testing.T, w *World, r Request, code string) {
	t.Helper()
	before := w.ExportCheckpoint()
	oldReceipt, hadReceipt := w.Receipt(r.ID)
	_, err := w.Execute(r)
	var f *Fault
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("failed creation changed authority")
	}
	if after, ok := w.Receipt(r.ID); ok != hadReceipt || !reflect.DeepEqual(after, oldReceipt) {
		t.Fatal("failure changed receipt truth")
	}
}
func TestCreateCharacterCompositionHistoryAndDetachment(t *testing.T) {
	b := creationFixture()
	w := creationWorld(t, b)
	c := newNPC()
	magnitude := int64(3)
	r := creationRequest("birth", 0,
		Operation{CreateCharacter: c},
		Operation{ApplyCondition: &ApplyCondition{Instance: "trained", Type: "training", Target: "npc"}},
		Operation{ApplyCondition: &ApplyCondition{Instance: "covered", Type: "cover", Target: "npc", Magnitude: &magnitude, Source: &Ref{CharacterKind, "actor"}}},
		Operation{Relate: &Relationship{From: Ref{CharacterKind, "npc"}, Kind: "party-member", To: Ref{CharacterKind, "actor"}, Strength: 1}},
		Operation{Damage: &ResourceOp{Character: "npc", Resource: "hp", Amount: 3}},
		Operation{Move: &Move{Character: "npc", To: "street"}})
	original := CanonicalRequest(r)
	receipt, err := w.Execute(r)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []EventKind{CharacterCreated, ConditionApplied, ConditionApplied, RelationshipChanged, DamageApplied, EntityMoved}
	if receipt.Revision != 1 || len(receipt.Events) != 6 {
		t.Fatal(receipt)
	}
	for i, kind := range kinds {
		if receipt.Events[i].Kind != kind || receipt.Events[i].Operation != i {
			t.Fatal(receipt.Events)
		}
	}
	if receipt.Events[0].Character.Resources["hp"].Current != 8 || receipt.Events[0].Character.Location != "hall" {
		t.Fatal("creation event lost initial facts")
	}
	snap := w.Snapshot()
	npc := character(&snap.Blueprint, "npc")
	if npc == nil || npc.Resources["hp"].Current != 5 || npc.Location != "street" || len(snap.Characters) != 2 || len(snap.Conditions) != 2 || len(snap.Relationships) != 1 {
		t.Fatal(snap)
	}
	x, err := w.Explain("npc", "defense")
	if err != nil || x.Effective != 13 || len(x.Contributions) != 1 || x.Contributions[0].ConditionOrigin.Instance != "covered" {
		t.Fatal(x, err)
	}
	if snap.Conditions[0].Cause.RequestID != "birth" || snap.Conditions[0].AppliedRevision != 1 {
		t.Fatal("lost provenance")
	}
	// Every input/output owns its nested maps; mutating one cannot rewrite another.
	c.Stats["defense"] = -99
	c.Resources["hp"] = Resource{}
	receipt.Events[0].Character.Stats["defense"] = -98
	receipt.Request.Operations[0].CreateCharacter.Resources["hp"] = Resource{}
	npc.Stats["defense"] = -97
	h := w.History()
	h[0].Events[0].Character.Resources["hp"] = Resource{}
	cp := w.ExportCheckpoint()
	cp.History[0].Request.Operations[0].CreateCharacter.Stats["defense"] = -96
	fixed, _ := w.Receipt("birth")
	if fixed.Events[0].Character.Stats["defense"] != 10 || fixed.Events[0].Character.Resources["hp"].Current != 8 || !reflect.DeepEqual(fixed.Request, original) {
		t.Fatal("aliased history")
	}
	if _, err := w.Execute(creationRequest("later", 1, Operation{RemoveCondition: &RemoveCondition{Instance: "covered", Target: "npc"}}, Operation{Transfer: &Transfer{Item: "sword", From: "actor", To: "npc"}})); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("replay mismatch", err)
	}
	for _, world := range []*World{w, rebuilt} {
		before := world.ExportCheckpoint()
		retry, e := world.Execute(original)
		if e != nil || !reflect.DeepEqual(retry, fixed) || !reflect.DeepEqual(before, world.ExportCheckpoint()) {
			t.Fatal("historical retry changed truth", e)
		}
		changed := CanonicalRequest(original)
		changed.Operations[0].CreateCharacter.Name = "Changed"
		creationReject(t, world, changed, "E_REQUEST_REUSE")
	}
}

func TestCreateCharacterRejectionAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		alter      func(*Character)
	}{
		{"character collision", "E_DUPLICATE", func(c *Character) { c.ID = "actor" }},
		{"item collision", "E_DUPLICATE", func(c *Character) { c.ID = "sword" }},
		{"location collision", "E_DUPLICATE", func(c *Character) { c.ID = "hall" }},
		{"identity", "E_ID", func(c *Character) { c.ID = "\n" }},
		{"UTF8 name", "E_ID", func(c *Character) { c.Name = "\xff" }},
		{"wrong location", "E_TYPE", func(c *Character) { c.Location = "sword" }},
		{"missing location", "E_REFERENCE", func(c *Character) { c.Location = "missing" }},
		{"stat ID", "E_ID", func(c *Character) { c.Stats[""] = 1 }},
		{"resource ID", "E_ID", func(c *Character) { c.Resources[""] = Resource{} }},
		{"bounds", "E_BOUNDS", func(c *Character) { c.Resources["hp"] = Resource{Current: 20, Maximum: 12} }},
		{"stat capacity", "E_LIMIT", func(c *Character) {
			for i := 0; i < 128; i++ {
				c.Stats[StatID(fmt.Sprint(i))] = 0
			}
		}},
		{"resource capacity", "E_LIMIT", func(c *Character) {
			for i := 0; i < 32; i++ {
				c.Resources[ResourceID(fmt.Sprint(i))] = Resource{}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := creationWorld(t, creationFixture())
			c := newNPC()
			tc.alter(c)
			creationReject(t, w, creationRequest("r", 0, Operation{CreateCharacter: c}), tc.code)
			if _, e := w.Execute(creationRequest("r", 0, Operation{CreateCharacter: newNPC()})); e != nil {
				t.Fatal("continuation", e)
			}
		})
	}
	w := creationWorld(t, creationFixture())
	creationReject(t, w, creationRequest("mixed", 0, Operation{CreateCharacter: newNPC(), Heal: &ResourceOp{}}), "E_IR")
	creationReject(t, w, creationRequest("dup", 0, Operation{CreateCharacter: newNPC()}, Operation{CreateCharacter: newNPC()}), "E_DUPLICATE")
	creationReject(t, w, creationRequest("late", 0, Operation{CreateCharacter: newNPC()}, Operation{ApplyCondition: &ApplyCondition{Instance: "training", Type: "training", Target: "npc"}}, Operation{Transfer: &Transfer{Item: "sword", From: "npc", To: "actor"}}), "E_OWNER")
	creationReject(t, w, creationRequest("early", 0, Operation{Heal: &ResourceOp{Character: "npc", Resource: "hp", Amount: 1}}, Operation{CreateCharacter: newNPC()}), "E_REFERENCE")
	r := creationRequest("self", 0, Operation{CreateCharacter: newNPC()})
	r.Actor = Ref{CharacterKind, "npc"}
	creationReject(t, w, r, "E_REFERENCE")
}

func TestCreationPreservesFinalNumericValidation(t *testing.T) {
	for _, target := range []CharacterID{"actor", "npc"} {
		for _, cancel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%v", target, cancel), func(t *testing.T) {
				b := creationFixture()
				c := newNPC()
				c.Stats["x"] = math.MaxInt64
				plus := Operation{Apply: &Effect{ID: "plus", Source: Ref{CharacterKind, "actor"}, Target: Ref{CharacterKind, string(target)}, Mode: OnCharacter, Stat: "x", Amount: 1}}
				minus := Operation{Apply: &Effect{ID: "minus", Source: Ref{CharacterKind, "actor"}, Target: Ref{CharacterKind, string(target)}, Mode: OnCharacter, Stat: "x", Amount: -1}}
				ops := []Operation{plus, {CreateCharacter: c}}
				if target == "npc" {
					ops = []Operation{{CreateCharacter: c}, plus}
				}
				if cancel {
					ops = append(ops, minus)
				}
				w := creationWorld(t, b)
				r := creationRequest("numeric", 0, ops...)
				if !cancel {
					creationReject(t, w, r, "E_OVERFLOW")
					return
				}
				if _, e := w.Execute(r); e != nil {
					t.Fatal(e)
				}
				x, e := w.Explain(target, "x")
				if e != nil || x.Effective != math.MaxInt64 {
					t.Fatal(x, e)
				}
				if _, e := Replay(b, w.History()); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
	// A newly created derived value can also be repaired by a later contribution.
	b := creationFixture()
	b.Equipment = &EquipmentRules{Version: EquipmentVersion, Derived: []DerivedStatRule{{Stat: "x", Terms: []StatTerm{{Stat: "input", Divisor: 1}}}}}
	b.Characters[0].Stats["input"] = 0
	c := newNPC()
	c.Stats["x"] = math.MaxInt64
	c.Stats["input"] = 1
	w := creationWorld(t, b)
	if _, e := w.Execute(creationRequest("derived", 0, Operation{CreateCharacter: c}, Operation{Apply: &Effect{ID: "minus", Source: Ref{CharacterKind, "actor"}, Target: Ref{CharacterKind, "npc"}, Mode: OnCharacter, Stat: "x", Amount: -1}})); e != nil {
		t.Fatal(e)
	}
}

func TestCreationBoundariesAndReplayCorruption(t *testing.T) {
	b := creationFixture()
	w := creationWorld(t, b)
	c := newNPC()
	c.Stats = map[StatID]int64{}
	c.Resources = map[ResourceID]Resource{}
	for i := 0; i < 128; i++ {
		c.Stats[StatID(fmt.Sprint(i))] = math.MinInt64
	}
	for i := 0; i < 32; i++ {
		c.Resources[ResourceID(fmt.Sprint(i))] = Resource{Minimum: math.MinInt64, Current: math.MaxInt64, Maximum: math.MaxInt64}
	}
	if _, e := w.Execute(creationRequest("max", 0, Operation{CreateCharacter: c})); e != nil {
		t.Fatal(e)
	}
	for _, tamper := range []func([]Batch){
		func(h []Batch) { h[0].Events[0].Character.Stats["0"] = 1 },
		func(h []Batch) { h[0].Request.Operations[0].CreateCharacter.Stats["0"] = 1 },
		func(h []Batch) { h[0].Events[0].Character = nil },
		func(h []Batch) { h[0].Events[0].Resource = &ResourceChange{} },
		func(h []Batch) { h[0].Events[0].Kind = EntityMoved },
	} {
		h := w.History()
		tamper(h)
		got, e := Replay(b, h)
		if e == nil || got != nil {
			t.Fatal("corrupted creation replayed")
		}
	}
	b = creationFixture()
	for i := 0; i < MaxWorldCharacters-1; i++ {
		c := newNPC()
		c.ID = CharacterID(fmt.Sprintf("existing:%d", i))
		b.Characters = append(b.Characters, *c)
	}
	w = creationWorld(t, b)
	creationReject(t, w, creationRequest("over-world-limit", 0, Operation{CreateCharacter: newNPC()}), "E_CLOCK")
	// Old genesis field maps are not newly restricted by creation-only limits.
	b = creationFixture()
	for i := 0; i < 130; i++ {
		b.Characters[0].Stats[StatID(fmt.Sprint(i))] = 1
	}
	creationWorld(t, b)
	// Map canonicalization is identical for nil and empty creation input.
	r := creationRequest("empty", 0, Operation{CreateCharacter: &Character{ID: "empty", Location: "hall"}})
	canonical := CanonicalRequest(r)
	if canonical.Operations[0].CreateCharacter.Stats == nil || canonical.Operations[0].CreateCharacter.Resources == nil {
		t.Fatal("noncanonical empty maps")
	}
}

func TestConcurrentCharacterCreation(t *testing.T) {
	w := creationWorld(t, creationFixture())
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := newNPC()
			c.ID = CharacterID(fmt.Sprint(i))
			_, e := w.Execute(creationRequest(fmt.Sprint(i), 0, Operation{CreateCharacter: c}))
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			var f *Fault
			if !errors.As(e, &f) || f.Code != "E_STALE" {
				t.Fatal(e)
			}
		}
	}
	if success != 1 || w.Revision() != 1 || len(w.Snapshot().Characters) != 2 {
		t.Fatal("concurrent birth publication")
	}
}
