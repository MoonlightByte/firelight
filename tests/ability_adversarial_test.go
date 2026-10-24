package tests

import (
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func literalCharacter(id core.CharacterID) core.AbilityOperand {
	return core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(id)}}
}
func literalItem(id core.ItemID) core.AbilityOperand {
	return core.AbilityOperand{Literal: &core.AbilityValue{Item: ptr(id)}}
}
func literalInt(v int64) core.AbilityOperand {
	return core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(v)}}
}
func literalCreation(id core.ConditionInstanceID) core.AbilityOperand {
	return core.AbilityOperand{Literal: &core.AbilityValue{ConditionID: ptr(id)}}
}
func pulse() core.AbilityDefinition {
	return core.AbilityDefinition{ID: "pulse", Name: "Pulse", Steps: []core.AbilityStep{{Heal: &core.HealTemplate{Target: literalCharacter("char:ada"), Resource: "hp", Amount: literalInt(1)}}}}
}
func invoke(id core.AbilityID) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: id}}
}

func TestAbilityTypedDefinitionRejections(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*core.Blueprint)
	}{
		{"duplicate definition", func(b *core.Blueprint) { b.AbilityDefinitions = append(b.AbilityDefinitions, b.AbilityDefinitions[0]) }},
		{"invalid ID", func(b *core.Blueprint) { b.AbilityDefinitions[0].ID = "" }},
		{"invalid name", func(b *core.Blueprint) { b.AbilityDefinitions[0].Name = "\xff" }},
		{"empty body", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps = nil }},
		{"oversized body", func(b *core.Blueprint) {
			d := &b.AbilityDefinitions[0]
			for len(d.Steps) <= core.MaxOperations {
				d.Steps = append(d.Steps, d.Steps[0])
			}
		}},
		{"duplicate parameter", func(b *core.Blueprint) {
			d := &b.AbilityDefinitions[0]
			d.Parameters = append(d.Parameters, d.Parameters[0])
		}},
		{"empty parameter", func(b *core.Blueprint) { b.AbilityDefinitions[0].Parameters[0].ID = "" }},
		{"unknown kind", func(b *core.Blueprint) { b.AbilityDefinitions[0].Parameters[0].Kind = "location" }},
		{"missing operand", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Target = core.AbilityOperand{} }},
		{"operand sum", func(b *core.Blueprint) {
			b.AbilityDefinitions[0].Steps[0].Heal.Target.Literal = literalCharacter("char:ada").Literal
		}},
		{"unknown parameter", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Target.Parameter = "missing" }},
		{"wrong operand kind", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Target.Parameter = "amount" }},
		{"literal wrong kind", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Target = literalItem("item:shield") }},
		{"literal missing entity", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Target = literalCharacter("missing") }},
		{"known invalid amount parameter target", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Amount = literalInt(0) }},
		{"invalid resource label", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0].Heal.Resource = "" }},
		{"literal target missing resource", func(b *core.Blueprint) {
			s := b.AbilityDefinitions[0].Steps[0].Heal
			s.Target = literalCharacter("char:ada")
			s.Resource = "missing"
		}},
		{"unknown Condition type", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[1].ApplyCondition.Type = "missing" }},
		{"source wrong kind", func(b *core.Blueprint) {
			b.AbilityDefinitions[0].Steps[1].ApplyCondition.Source = &core.Ref{Kind: core.CharacterKind, ID: "loc:shrine"}
		}},
		{"literal target missing stat", func(b *core.Blueprint) {
			delete(b.Characters[0].Stats, "defense")
			b.Effects = nil
			b.AbilityDefinitions[0].Steps[1].ApplyCondition.Target = literalCharacter("char:ada")
		}},
		{"forbidden magnitude", func(b *core.Blueprint) {
			b.AbilityDefinitions[0].Steps[1].ApplyCondition.Magnitude = ptr(literalInt(1))
		}},
		{"required magnitude", func(b *core.Blueprint) {
			m := &b.ConditionDefinitions[0].Modifiers[0]
			m.Fixed = nil
			m.Magnitude = true
		}},
		{"step sum", func(b *core.Blueprint) { s := &b.AbilityDefinitions[0].Steps[0]; s.Transfer = &core.TransferTemplate{} }},
		{"empty step", func(b *core.Blueprint) { b.AbilityDefinitions[0].Steps[0] = core.AbilityStep{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := abilityFixture(t)
			tc.mutate(&b)
			before := contractBytes(t, b)
			w, e := core.NewWorld(b)
			if e == nil || w != nil {
				t.Fatal("invalid definition published")
			}
			contractEqual(t, before, b)
		})
	}
	// A detached, unvalidated catalog must not make direct invocation checking panic.
	b := abilityFixture(t)
	b.AbilityDefinitions[0].Steps[0].Heal.Target.Literal = &core.AbilityValue{}
	if b.CheckOperationStatic(aid("one", 4)) == nil {
		t.Fatal("forged catalog accepted")
	}
}
func TestAbilityExpandedWorkAndEventBounds(t *testing.T) {
	b := abilityFixture(t)
	b.AbilityDefinitions = append(b.AbilityDefinitions, pulse())
	w := world(t, b)
	ops := make([]core.Operation, 128)
	for i := range ops {
		ops[i] = invoke("pulse")
	}
	receipt := commitC(t, w, typedRequest(w, "limit", ops...))
	if len(receipt.Events) != 256 || w.Revision() != 1 || receipt.Events[255].Operation != 127 {
		t.Fatal("exact step/event boundary")
	}
	if rw, e := core.Replay(b, w.History()); e != nil || !reflect.DeepEqual(rw.History(), w.History()) {
		t.Fatal("limit replay", e)
	}
	for _, transfers := range []int{127, 128} {
		b := abilityFixture(t)
		d := core.AbilityDefinition{ID: "limit", Name: "Limit"}
		for i := 0; i < transfers; i++ {
			id := core.ItemID(fmt.Sprintf("limit:%03d", i))
			b.Items = append(b.Items, core.Item{ID: id, Owner: "char:ada", Wearer: "char:ada"})
			d.Steps = append(d.Steps, core.AbilityStep{Transfer: &core.TransferTemplate{Item: literalItem(id), From: literalCharacter("char:ada"), To: literalCharacter("char:bea")}})
		}
		if transfers == 127 {
			d.Steps = append(d.Steps, pulse().Steps[0])
		}
		b.AbilityDefinitions = append(b.AbilityDefinitions, d, pulse())
		w := world(t, b)
		r := typedRequest(w, "limit", invoke("limit"))
		if transfers == 127 {
			receipt := commitC(t, w, r)
			if len(receipt.Events) != 256 {
				t.Fatal("127 transfers + heal + terminal")
			}
		} else {
			abilityReject(t, w, r, "E_LIMIT")
			hp(t, w, 10)
			commitC(t, w, typedRequest(w, "limit", invoke("pulse")))
		}
	}
	b = abilityFixture(t)
	d := pulse()
	d.ID = "many"
	for len(d.Steps) < 128 {
		d.Steps = append(d.Steps, pulse().Steps[0])
	}
	b.AbilityDefinitions = append(b.AbilityDefinitions, d)
	w = world(t, b)
	abilityReject(t, w, typedRequest(w, "expanded", invoke("many"), core.Operation{Heal: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 1}}), "E_LIMIT")
	receipt = commitC(t, w, typedRequest(w, "expanded", invoke("many")))
	if len(receipt.Events) != 129 {
		t.Fatal("128 heals + terminal")
	}
}

func TestAbilityReplayCorruptionAndPositiveLimits(t *testing.T) {
	b := abilityFixture(t)
	w := world(t, b)
	commitC(t, w, typedRequest(w, "aid", aid("one", 4)))
	commitC(t, w, typedRequest(w, "transfer", handoff("item:shield", "char:bea", "char:cy")))
	commitC(t, w, typedRequest(w, "remove", removeC("one", "char:ada")))
	state, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	for _, tc := range []struct {
		name   string
		mutate func([]core.Batch)
	}{
		{"terminal definition", func(h []core.Batch) { h[0].Events[2].Ability.Ability = "handoff" }},
		{"terminal argument", func(h []core.Batch) { *h[0].Events[2].Ability.Arguments[0].Value.Integer = 5 }},
		{"retained binding", func(h []core.Batch) { *h[0].Request.Operations[0].Invoke.Arguments[0].Value.Integer = 5 }},
		{"step origin", func(h []core.Batch) { h[0].Events[0].AbilityOrigin.Step = 1 }},
		{"definition origin", func(h []core.Batch) { h[0].Events[1].AbilityOrigin.Ability = "handoff" }},
		{"missing origin", func(h []core.Batch) { h[0].Events[0].AbilityOrigin = nil }},
		{"terminal origin", func(h []core.Batch) { h[0].Events[2].AbilityOrigin = &core.AbilityOrigin{Ability: "aid", Step: 1} }},
		{"created identity", func(h []core.Batch) { h[0].Events[1].Condition.After.ID = "other" }},
		{"Condition creation step", func(h []core.Batch) { h[0].Events[1].Condition.After.Cause.AbilityOrigin.Step = 0 }},
		{"consequence", func(h []core.Batch) { h[0].Events[1].Condition.AfterEffects[0].Amount++ }},
		{"event ordering", func(h []core.Batch) { h[0].Events[0], h[0].Events[1] = h[0].Events[1], h[0].Events[0] }},
		{"outer grouping", func(h []core.Batch) { h[0].Events[1].Operation = 1 }},
		{"missing terminal", func(h []core.Batch) { h[0].Events = h[0].Events[:2] }},
		{"rule event missing", func(h []core.Batch) { h[1].Events = h[1].Events[1:] }},
		{"late revision", func(h []core.Batch) { h[2].Revision++ }},
		{"late actor", func(h []core.Batch) { h[2].Events[0].Actor.ID = "char:bea" }},
		{"late prior cause", func(h []core.Batch) { h[2].Events[0].Condition.Before.Cause.AbilityOrigin.Ability = "handoff" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := w.History()
			tc.mutate(h)
			bb, hb := contractBytes(t, b), contractBytes(t, h)
			rw, e := core.Replay(b, h)
			if e == nil || rw != nil {
				t.Fatal("inconsistent history accepted")
			}
			contractEqual(t, bb, b)
			contractEqual(t, hb, h)
			contractEqual(t, state, w.Snapshot())
			contractEqual(t, history, w.History())
		})
	}
	changed := abilityFixture(t)
	*changed.ConditionDefinitions[0].Modifiers[0].Fixed = 4
	if rw, e := core.Replay(changed, w.History()); e == nil || rw != nil {
		t.Fatal("necessarily different definition accepted")
	}
	changed = abilityFixture(t)
	changed.AbilityDefinitions[0].Steps[0].Heal.Amount = literalInt(1)
	if rw, e := core.Replay(changed, w.History()); e == nil || rw != nil {
		t.Fatal("necessarily different body accepted")
	}
	for n := 0; n <= 3; n++ {
		rw, e := core.Replay(b, w.History()[:n])
		if e != nil || rw.Revision() != core.Revision(n) {
			t.Fatal("valid prefix", n, e)
		}
	}
	other := world(t, b)
	commitC(t, other, typedRequest(other, "coherent", aid("different", 2)))
	if rw, e := core.Replay(b, other.History()); e != nil || !reflect.DeepEqual(rw.History(), other.History()) {
		t.Fatal("coherent alternative", e)
	}
	// Binding data is not executable source, even when it resembles statements.
	id := `fresh"; heal "char:ada" resource "hp" by 20;`
	commitC(t, other, typedRequest(other, "data", aid(id, 1)))
	hp(t, other, 13)
}

func tamperAbilityValue(v *core.AbilityValue) {
	if v.Character != nil {
		*v.Character = "tampered"
	}
	if v.Item != nil {
		*v.Item = "tampered"
	}
	if v.Integer != nil {
		*v.Integer = 99
	}
	if v.ConditionID != nil {
		*v.ConditionID = "tampered"
	}
}
func tamperOperand(v *core.AbilityOperand) {
	if v.Literal != nil {
		tamperAbilityValue(v.Literal)
	}
	v.Parameter = "tampered"
}
func tamperDefinitions(ds []core.AbilityDefinition) {
	for i := range ds {
		d := &ds[i]
		d.Name = "tampered"
		for j := range d.Parameters {
			d.Parameters[j].ID = "tampered"
		}
		for j := range d.Steps {
			s := &d.Steps[j]
			if h := s.Heal; h != nil {
				tamperOperand(&h.Target)
				tamperOperand(&h.Amount)
				h.Resource = "tampered"
			}
			if c := s.ApplyCondition; c != nil {
				tamperOperand(&c.Instance)
				tamperOperand(&c.Target)
				if c.Magnitude != nil {
					tamperOperand(c.Magnitude)
				}
				if c.Source != nil {
					c.Source.ID = "tampered"
				}
				c.Type = "tampered"
			}
			if x := s.Transfer; x != nil {
				tamperOperand(&x.Item)
				tamperOperand(&x.From)
				tamperOperand(&x.To)
			}
		}
	}
}
func tamperInvocation(v *core.InvokeAbility) {
	if v == nil {
		return
	}
	v.Ability = "tampered"
	for i := range v.Arguments {
		v.Arguments[i].Name = "tampered"
		tamperAbilityValue(&v.Arguments[i].Value)
	}
}
func tamperAbilityBatch(b *core.Batch) {
	for i := range b.Request.Operations {
		tamperInvocation(b.Request.Operations[i].Invoke)
	}
	for i := range b.Events {
		e := &b.Events[i]
		tamperInvocation(e.Ability)
		if e.AbilityOrigin != nil {
			e.AbilityOrigin.Step = 99
		}
		if e.Condition != nil {
			for _, c := range []*core.ConditionInstance{e.Condition.Before, e.Condition.After} {
				if c != nil && c.Cause != nil && c.Cause.AbilityOrigin != nil {
					c.Cause.AbilityOrigin.Step = 99
				}
			}
		}
	}
}
func TestAbilityNestedDetachmentAndProjectionFreshness(t *testing.T) {
	b := abilityFixture(t)
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "variable", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "v", Stat: "defense", Magnitude: true}}})
	literals := pulse()
	literals.ID = "literals"
	literals.Steps = append(literals.Steps, core.AbilityStep{ApplyCondition: &core.ConditionTemplate{Instance: literalCreation("literal"), Target: literalCharacter("char:ada"), Type: "variable", Source: ptr(core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}), Magnitude: ptr(literalInt(2))}}, core.AbilityStep{Transfer: &core.TransferTemplate{Item: literalItem("item:parcel"), From: literalCharacter("char:ada"), To: literalCharacter("char:bea")}})
	b.AbilityDefinitions = append(b.AbilityDefinitions, literals)
	w := world(t, b)
	initial := contractBytes(t, w.Snapshot())
	tamperDefinitions(b.AbilityDefinitions)
	contractEqual(t, initial, w.Snapshot())
	genesis := w.Snapshot().Blueprint
	old, _ := w.CharacterStatus("char:ada")
	r := typedRequest(w, "aid", aid("one", 4))
	first := commitC(t, w, r)
	firstBytes := contractBytes(t, first)
	now, _ := w.CharacterStatus("char:ada")
	if old.Resources["hp"].Current != 10 || now.Resources["hp"].Current != 14 || old.Stats[0].Effective != 12 || now.Stats[0].Effective != 15 || old.Revision != 0 || now.Revision != 1 {
		t.Fatal("numeric projection freshness")
	}
	projection := string(contractBytes(t, now))
	for _, hidden := range []string{"ability_definitions", "ability_origin", "loc:shrine", "arguments"} {
		if strings.Contains(projection, hidden) {
			t.Fatal("narrow projection leaked", hidden)
		}
	}
	// Within the SAME receipt, terminal intent and retained request are independent.
	tamperInvocation(first.Events[2].Ability)
	contractEqual(t, contractBytes(t, core.CanonicalInvocation(*r.Operations[0].Invoke)), *first.Request.Operations[0].Invoke)
	tamperInvocation(r.Operations[0].Invoke)
	contractEqual(t, firstBytes, commitC(t, w, w.History()[0].Request))
	commitC(t, w, typedRequest(w, "remove", removeC("one", "char:ada")))
	state, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	inputHistory := w.History()
	gb, hb := contractBytes(t, genesis), contractBytes(t, inputHistory)
	rebuilt, e := core.Replay(genesis, inputHistory)
	if e != nil {
		t.Fatal(e)
	}
	contractEqual(t, gb, genesis)
	contractEqual(t, hb, inputHistory)
	tamperDefinitions(genesis.AbilityDefinitions)
	for i := range inputHistory {
		tamperAbilityBatch(&inputHistory[i])
	}
	for _, world := range []*core.World{w, rebuilt} {
		contractEqual(t, state, world.Snapshot())
		contractEqual(t, history, world.History())
		a, z := world.Snapshot(), world.Snapshot()
		want := contractBytes(t, z)
		tamperDefinitions(a.AbilityDefinitions)
		contractEqual(t, want, z)
		h1, h2 := world.History(), world.History()
		for i := range h1 {
			tamperAbilityBatch(&h1[i])
		}
		contractEqual(t, history, h2)
		for _, receipt := range h2 {
			r1 := commitC(t, world, receipt.Request)
			r2 := commitC(t, world, receipt.Request)
			want := contractBytes(t, r2)
			tamperAbilityBatch(&r1)
			contractEqual(t, want, r2)
			contractEqual(t, want, commitC(t, world, receipt.Request))
		}
		contractEqual(t, state, world.Snapshot())
		contractEqual(t, history, world.History())
	}
	// Active persistent cause is independently detached too.
	commitC(t, rebuilt, typedRequest(rebuilt, "new", aid("two", 1)))
	snap, sibling := rebuilt.Snapshot(), rebuilt.Snapshot()
	before := contractBytes(t, sibling)
	snap.Conditions[0].Cause.AbilityOrigin.Step = 99
	contractEqual(t, before, sibling)
	contractEqual(t, before, rebuilt.Snapshot())
}

func FuzzAbilitySequence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 0, 2})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 12 {
			return
		}
		b := abilityFixture(t)
		w := world(t, b)
		expectedHP := int64(10)
		active := map[string]bool{}
		for i, v := range data {
			id := fmt.Sprintf("c%d", i)
			requestID := fmt.Sprintf("r%d", i)
			switch v % 3 {
			case 0:
				commitC(t, w, typedRequest(w, requestID, aid(id, 1)))
				expectedHP++
				if expectedHP > 20 {
					expectedHP = 20
				}
				active[id] = true
			case 1:
				abilityReject(t, w, typedRequest(w, requestID, aid(id, 0)), "E_AMOUNT")
			case 2:
				h := w.History()
				if len(h) > 0 {
					receipt := h[0]
					contractEqual(t, contractBytes(t, receipt), commitC(t, w, receipt.Request))
				}
			}
			hp(t, w, expectedHP)
			stat(t, w, "char:ada", "defense", 12+3*int64(len(active)))
		}
		encoded := contractBytes(t, w.History())
		var history []core.Batch
		if e := json.Unmarshal(encoded, &history); e != nil {
			t.Fatal(e)
		}
		rw, e := core.Replay(b, history)
		if e != nil || !reflect.DeepEqual(rw.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(rw.History(), w.History()) {
			t.Fatal("sequence replay", e)
		}
	})
}
