package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestResourceSpendTypedRejectionAndBindingContext(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		op         core.Operation
	}{
		{"no variant", "E_IR", core.Operation{}},
		{"two variants", "E_IR", core.Operation{Spend: spend(2).Spend, Heal: focusHeal(2).Heal}},
		{"zero", "E_AMOUNT", spend(0)}, {"negative", "E_AMOUNT", spend(-1)},
		{"unknown", "E_REFERENCE", core.Operation{Spend: &core.ResourceOp{Character: "missing", Resource: "focus", Amount: 1}}},
		{"wrong kind", "E_TYPE", core.Operation{Spend: &core.ResourceOp{Character: "loc:shrine", Resource: "focus", Amount: 1}}},
		{"missing resource", "E_RESOURCE", core.Operation{Spend: &core.ResourceOp{Character: "char:orin", Resource: "absent", Amount: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := world(t, costFixture(t))
			err := costReject(t, w, costRequest(w, "reuse", tc.op), tc.code)
			encoded := string(contractBytes(t, err))
			for _, forbidden := range []string{`"source"`, `"line"`, `"column"`} {
				if strings.Contains(encoded, forbidden) {
					t.Fatal("invented source", encoded)
				}
			}
			commitC(t, w, costRequest(w, "reuse", spend(1)))
			costFacts(t, w, 2, 6, 10, 1)
		})
	}
	for _, tc := range []struct {
		name, code string
		change     func(*core.SpendTemplate)
	}{
		{"missing payer", "E_IR", func(s *core.SpendTemplate) { s.Payer = core.AbilityOperand{} }},
		{"missing amount", "E_IR", func(s *core.SpendTemplate) { s.Amount = core.AbilityOperand{} }},
		{"both operand forms", "E_IR", func(s *core.SpendTemplate) {
			s.Payer.Literal = &core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}
		}},
		{"both value forms", "E_IR", func(s *core.SpendTemplate) {
			s.Payer = core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:orin")), Integer: ptr(int64(2))}}
		}},
		{"wrong literal kind", "E_TYPE", func(s *core.SpendTemplate) {
			s.Payer = core.AbilityOperand{Literal: &core.AbilityValue{Item: ptr(core.ItemID("item:token"))}}
		}},
		{"wrong parameter kind", "E_TYPE", func(s *core.SpendTemplate) { s.Payer.Parameter = "amount" }},
		{"undeclared parameter", "E_PARAMETER", func(s *core.SpendTemplate) { s.Amount.Parameter = "missing" }},
		{"invalid selector", "E_ID", func(s *core.SpendTemplate) { s.Resource = "" }},
		{"zero literal", "E_AMOUNT", func(s *core.SpendTemplate) {
			s.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(int64(0))}}
		}},
		{"missing literal resource", "E_RESOURCE", func(s *core.SpendTemplate) {
			s.Payer = core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}}
			s.Resource = "missing"
		}},
	} {
		t.Run("template/"+tc.name, func(t *testing.T) {
			b := costFixture(t)
			tc.change(b.AbilityDefinitions[1].Steps[0].Spend)
			got, err := core.NewWorld(b)
			var f *core.Fault
			if got != nil || !errors.As(err, &f) || f.Code != tc.code {
				t.Fatal("NewWorld must reject through clone", tc.code, got, err)
			}
		})
	}
	t.Run("dual template survives clone", func(t *testing.T) {
		b := costFixture(t)
		b.AbilityDefinitions[0].Steps[0].Heal = b.AbilityDefinitions[0].Steps[1].Heal
		w, err := core.NewWorld(b)
		var f *core.Fault
		if w != nil || !errors.As(err, &f) || f.Code != "E_IR" {
			t.Fatal(w, err)
		}
	})
	for _, tc := range []struct {
		name, code, operand, param string
		step                       int
		mutate                     func(*core.InvokeAbility)
	}{
		{"missing binding", "E_ARGUMENT", "argument", "payer", -1, func(v *core.InvokeAbility) { v.Arguments = v.Arguments[1:] }},
		{"extra binding", "E_ARGUMENT", "argument", "extra", -1, func(v *core.InvokeAbility) {
			v.Arguments = append(v.Arguments, core.AbilityArgument{Name: "extra", Value: core.AbilityValue{Integer: ptr(int64(1))}})
		}},
		{"duplicate binding", "E_DUPLICATE", "argument", "payer", -1, func(v *core.InvokeAbility) { v.Arguments = append(v.Arguments, v.Arguments[0]) }},
		{"wrong kind", "E_TYPE", "argument", "payer", -1, func(v *core.InvokeAbility) {
			v.Arguments[0].Value = core.AbilityValue{Item: ptr(core.ItemID("item:token"))}
		}},
		{"missing reference", "E_REFERENCE", "argument", "payer", -1, func(v *core.InvokeAbility) { *v.Arguments[0].Value.Character = "missing" }},
		{"wrong reference kind", "E_TYPE", "argument", "payer", -1, func(v *core.InvokeAbility) { *v.Arguments[0].Value.Character = "loc:shrine" }},
		{"empty value", "E_IR", "argument", "amount", -1, func(v *core.InvokeAbility) { v.Arguments[1].Value = core.AbilityValue{} }},
		{"dual value", "E_IR", "argument", "amount", -1, func(v *core.InvokeAbility) { v.Arguments[1].Value.Character = ptr(core.CharacterID("char:orin")) }},
		{"nonpositive bound", "E_AMOUNT", "amount", "amount", 0, func(v *core.InvokeAbility) { *v.Arguments[1].Value.Integer = 0 }},
	} {
		t.Run("binding/"+tc.name, func(t *testing.T) {
			w := world(t, costFixture(t))
			op := pay(2)
			tc.mutate(op.Invoke)
			err := costReject(t, w, costRequest(w, "bad", op), tc.code)
			var ae *core.AbilityError
			if !errors.As(err, &ae) || ae.Step != tc.step || ae.Operand != tc.operand || string(ae.Parameter) != tc.param {
				t.Fatal(err)
			}
			commitC(t, w, costRequest(w, "bad", pay(2)))
		})
	}
	// Funds are intentionally absent from static validation. Current-candidate
	// resource existence is checked with the payer field, not amount or target.
	b := costFixture(t)
	delete(costCharacter(&b, "char:tess").Resources, "focus")
	w := world(t, b)
	o := pay(2)
	*o.Invoke.Arguments[0].Value.Character = "char:tess"
	err := costReject(t, w, costRequest(w, "resource", o), "E_RESOURCE")
	var ae *core.AbilityError
	if !errors.As(err, &ae) || ae.Step != 0 || ae.Operand != "payer" || ae.Parameter != "payer" {
		t.Fatal(err)
	}
	if err := b.CheckOperationStatic(pay(99)); err != nil {
		t.Fatal("availability is runtime only", err)
	}
}

func TestResourceSpendReplayCorruptionAndConsistentPrefixes(t *testing.T) {
	b, w := costJourney(t)
	original := contractBytes(t, w.History())
	snapshot := contractBytes(t, w.Snapshot())
	genesis := contractBytes(t, b)
	for _, tc := range []struct {
		name   string
		mutate func([]core.Batch)
	}{
		{"amount", func(h []core.Batch) { h[0].Events[0].Resource.Requested = 1 }},
		{"payer", func(h []core.Batch) { h[0].Events[0].Resource.Character = "char:tess" }},
		{"selector", func(h []core.Batch) { h[0].Events[0].Resource.Resource = "hp" }},
		{"before", func(h []core.Batch) { h[0].Events[0].Resource.Before.Current = 4 }},
		{"after", func(h []core.Batch) { h[0].Events[0].Resource.After.Current = 2 }},
		{"bounds", func(h []core.Batch) { h[0].Events[0].Resource.After.Minimum = -1 }},
		{"kind", func(h []core.Batch) { h[0].Events[0].Kind = core.DamageApplied }},
		{"order", func(h []core.Batch) { h[0].Events[0], h[0].Events[1] = h[0].Events[1], h[0].Events[0] }},
		{"origin", func(h []core.Batch) { h[0].Events[0].AbilityOrigin.Step = 1 }},
		{"outer index", func(h []core.Batch) { h[0].Events[0].Operation = 1 }},
		{"request", func(h []core.Batch) { *h[0].Request.Operations[0].Invoke.Arguments[1].Value.Character = "char:tess" }},
		{"terminal binding", func(h []core.Batch) { *h[0].Events[3].Ability.Arguments[1].Value.Character = "char:tess" }},
		{"missing terminal", func(h []core.Batch) { h[0].Events = h[0].Events[:3] }},
		{"direct amount", func(h []core.Batch) { h[4].Events[0].Resource.Requested = 1 }},
		{"direct damaged intent", func(h []core.Batch) { h[4].Events[0].Kind = core.DamageApplied }},
		{"condition origin", func(h []core.Batch) { h[0].Events[2].Condition.After.Cause.AbilityOrigin.Step = 0 }},
		{"late inconsistent removal", func(h []core.Batch) { h[5].Events[0].Condition.Before.ID = "absent" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := w.History()
			tc.mutate(h)
			before := contractBytes(t, h)
			got, err := core.Replay(b, h)
			if got != nil || err == nil {
				t.Fatal("corruption accepted or partial World escaped", got, err)
			}
			contractEqual(t, before, h)
			contractEqual(t, original, w.History())
			contractEqual(t, snapshot, w.Snapshot())
			contractEqual(t, genesis, b)
		})
	}
	changed := costFixture(t)
	*changed.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = 3
	if got, err := core.Replay(changed, w.History()); got != nil || err == nil {
		t.Fatal("different cost accepted old trace")
	}
	for _, n := range []int{0, 1, 3, 6} {
		h := w.History()[:n]
		got, err := core.Replay(b, h)
		if err != nil {
			t.Fatal("valid prefix", n, err)
		}
		if got.Revision() != core.Revision(n) {
			t.Fatal("prefix revision")
		}
		contractEqual(t, contractBytes(t, h), got.History())
		for _, r := range h {
			contractEqual(t, contractBytes(t, r), commitC(t, got, r.Request))
		}
	}
	// A freshly executed alternative amount with matching intent/trace is a
	// consistent different history, not evidence of authenticity/completeness.
	other := world(t, b)
	commitC(t, other, costRequest(other, "alternative", spend(1)))
	costFacts(t, other, 2, 6, 10, 1)
	costReplay(t, b, other)
}

func TestResourceSpendInlineMechanicsAndDistinctHistory(t *testing.T) {
	b := costFixture(t)
	invoked, inline := world(t, b), world(t, b)
	commitC(t, invoked, costRequest(invoked, "invoke", steady("guard:m7:one")))
	commitC(t, inline, costRequest(inline, "inline", spend(2), core.Operation{Heal: &core.ResourceOp{Character: "char:lyra", Resource: "hp", Amount: 4}}, core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "guard:m7:one", Type: "condition:guarded", Target: "char:lyra", Source: &core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}}}))
	for _, w := range []*core.World{invoked, inline} {
		costFacts(t, w, 1, 10, 13, 1)
		costReplay(t, b, w)
	}
	a, z := invoked.Snapshot(), inline.Snapshot()
	if reflect.DeepEqual(a, z) || reflect.DeepEqual(invoked.History(), inline.History()) {
		t.Fatal("erased invocation/cause")
	}
	if a.Conditions[0].Cause.AbilityOrigin.Step != 2 || a.Conditions[0].Cause.Operation != 0 || z.Conditions[0].Cause.AbilityOrigin != nil || z.Conditions[0].Cause.Operation != 2 {
		t.Fatal("cause distinction")
	}
	a.Conditions[0].Cause = nil
	z.Conditions[0].Cause = nil
	// Full mechanical manifest, keeping definitions and every other field.
	contractEqual(t, contractBytes(t, a), z)
	ai, zi := invoked.History()[0], inline.History()[0]
	if len(ai.Events) != 4 || len(zi.Events) != 3 || ai.Request.Operations[0].Invoke == nil || zi.Request.Operations[0].Spend == nil || ai.Events[3].Kind != core.AbilityInvoked || zi.Events[0].AbilityOrigin != nil {
		t.Fatal("event/intent distinction")
	}
}

func TestResourceSpendDetachedBoundariesAndHistoricalReceipts(t *testing.T) {
	b := costFixture(t)
	// Also exercise deeply nested literal payer and amount copies in definitions.
	b.AbilityDefinitions[1].Steps[0].Spend.Payer = core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}}
	b.AbilityDefinitions[1].Steps[0].Spend.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(int64(1))}}
	w := world(t, b)
	initial := contractBytes(t, w.Snapshot())
	*b.AbilityDefinitions[1].Steps[0].Spend.Payer.Literal.Character = "char:tess"
	*b.AbilityDefinitions[1].Steps[0].Spend.Amount.Literal.Integer = 99
	b.AbilityDefinitions[1].Steps[0].Spend.Resource = "hp"
	contractEqual(t, initial, w.Snapshot())
	sibling := w.Snapshot()
	siblingBefore := contractBytes(t, sibling)
	s := w.Snapshot()
	for _, d := range s.AbilityDefinitions {
		if d.ID == "ability:pay" {
			*d.Steps[0].Spend.Payer.Literal.Character = "changed"
			*d.Steps[0].Spend.Amount.Literal.Integer = 50
		}
	}
	contractEqual(t, initial, w.Snapshot())
	contractEqual(t, siblingBefore, sibling)
	input := costRequest(w, "direct", spend(1))
	first := commitC(t, w, input)
	expected := contractBytes(t, first)
	canonical := contractBytes(t, w.Snapshot())
	history := contractBytes(t, w.History())
	input.Operations[0].Spend.Amount = 100
	first.Request.Operations[0].Spend.Amount = 200
	first.Events[0].Resource.After.Current = 999
	contractEqual(t, canonical, w.Snapshot())
	contractEqual(t, history, w.History())
	request := w.History()[0].Request
	a := commitC(t, w, request)
	z := commitC(t, w, request)
	zBefore := contractBytes(t, z)
	a.Events[0].Resource.Before.Current = 999
	a.Request.Operations[0].Spend.Resource = "hp"
	contractEqual(t, zBefore, z)
	contractEqual(t, expected, commitC(t, w, request))
	contractEqual(t, history, w.History())
	h := w.History()
	h[0].Events[0].Resource.Requested = 900
	h[0].Request.Operations[0].Spend.Character = "char:tess"
	contractEqual(t, history, w.History())
	p, _ := w.CharacterStatus("char:orin")
	p2, _ := w.CharacterStatus("char:orin")
	pBefore := contractBytes(t, p2)
	p.Resources["focus"] = core.Resource{Current: 999}
	contractEqual(t, pBefore, p2)
	contractEqual(t, canonical, w.Snapshot())
	// Compiled output is neither a catalog writer nor a retained request writer.
	ops, ds := compiler.CompileActions("one", `spend "char:orin" resource "focus" by 1;`, w.Snapshot().Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	otherOps, ds := compiler.CompileActions("two", `spend "char:orin" resource "focus" by 1;`, w.Snapshot().Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	opExpected := contractBytes(t, otherOps)
	commitC(t, w, costRequest(w, "compiled", ops...))
	ops[0].Spend.Amount = 1000
	contractEqual(t, opExpected, otherOps)
	// Original, late and reconstructed invoked receipts after both guards retire.
	genesis, journey := costJourney(t)
	rebuilt := costReplay(t, genesis, journey)
	for _, live := range []*core.World{journey, rebuilt} {
		before := contractBytes(t, live.Snapshot())
		hs := contractBytes(t, live.History())
		req := live.History()[0].Request
		left, right := commitC(t, live, req), commitC(t, live, req)
		want := contractBytes(t, right)
		*req.Operations[0].Invoke.Arguments[1].Value.Character = "char:tess"
		*left.Request.Operations[0].Invoke.Arguments[1].Value.Character = "changed"
		if *left.Events[3].Ability.Arguments[1].Value.Character != "char:orin" {
			t.Fatal("request and terminal alias")
		}
		*left.Events[3].Ability.Arguments[1].Value.Character = "changed"
		left.Events[0].Resource.After.Current = 88
		left.Events[2].Condition.After.Source.ID = "changed"
		left.Events[2].Condition.After.Cause.AbilityOrigin.Step = 0
		left.Events[2].Condition.AfterEffects[0].ConditionOrigin.Instance = "changed"
		contractEqual(t, want, right)
		contractEqual(t, want, commitC(t, live, live.History()[0].Request))
		contractEqual(t, before, live.Snapshot())
		contractEqual(t, hs, live.History())
	}
}

func TestResourceSpendConcurrencyAndIndependentWorkBounds(t *testing.T) {
	t.Run("same request", func(t *testing.T) {
		w := world(t, costFixture(t))
		r := costRequest(w, "parallel", steady("guard:m7:one"))
		copyHandle := *w
		var wg sync.WaitGroup
		results := make(chan core.Batch, 24)
		errs := make(chan error, 24)
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); got, err := copyHandle.Execute(r); results <- got; errs <- err }()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		want := contractBytes(t, w.History()[0])
		for got := range results {
			contractEqual(t, want, got)
		}
		costFacts(t, w, 1, 10, 13, 1)
	})
	t.Run("revision then funds", func(t *testing.T) {
		b := costFixture(t)
		costBalance(&b, 2, 0, 5)
		w := world(t, b)
		rs := []core.Request{costRequest(w, "a", steady("a")), costRequest(w, "b", steady("b"))}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range rs {
			wg.Add(1)
			go func(i int) { defer wg.Done(); _, errs[i] = w.Execute(rs[i]) }(i)
		}
		wg.Wait()
		success, stale := 0, 0
		for i, err := range errs {
			if err == nil {
				success++
				continue
			}
			var f *core.Fault
			if !errors.As(err, &f) || f.Code != "E_STALE" {
				t.Fatal(err)
			}
			stale++
			rs[i].ExpectedRevision = w.Revision()
			costReject(t, w, rs[i], "E_INSUFFICIENT_RESOURCE")
		}
		if success != 1 || stale != 1 {
			t.Fatal(success, stale)
		}
		costFacts(t, w, 0, 10, 13, 1)
		costReplay(t, b, w)
	})
	t.Run("exact outer step event boundary", func(t *testing.T) {
		b := costFixture(t)
		costBalance(&b, 128, 0, 200)
		w := world(t, b)
		ops := make([]core.Operation, 128)
		for i := range ops {
			ops[i] = pay(1)
		}
		got := commitC(t, w, costRequest(w, "boundary", ops...))
		if len(got.Events) != 256 {
			t.Fatal(len(got.Events))
		}
		costFacts(t, w, 0, 6, 10, 1)
		costReplay(t, b, w)
		other := world(t, b)
		costReject(t, other, costRequest(other, "over", append(ops, pay(1))...), "E_LIMIT")
	})
	t.Run("expanded bound after private spend", func(t *testing.T) {
		b := costFixture(t)
		costBalance(&b, 200, 0, 200)
		d := b.AbilityDefinitions[1]
		d.ID = "twice"
		d.Steps = append(d.Steps, d.Steps[0])
		b.AbilityDefinitions = append(b.AbilityDefinitions, d)
		w := world(t, b)
		ops := make([]core.Operation, 65)
		for i := range ops {
			ops[i] = pay(1)
			ops[i].Invoke.Ability = "twice"
		}
		costReject(t, w, costRequest(w, "expanded", ops...), "E_LIMIT")
		commitC(t, w, costRequest(w, "expanded", pay(1)))
	})
	t.Run("event bound after private spend within other bounds", func(t *testing.T) {
		b := costFixture(t)
		d := core.AbilityDefinition{ID: "pass", Name: "Pass", Parameters: []core.AbilityParameter{{ID: "item", Kind: core.ItemArgument}}, Steps: []core.AbilityStep{{Transfer: &core.TransferTemplate{Item: core.AbilityOperand{Parameter: "item"}, From: core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}}, To: core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:lyra"))}}}}}}
		b.AbilityDefinitions = append(b.AbilityDefinitions, d)
		ops := []core.Operation{pay(1)}
		for i := 0; i < 85; i++ {
			id := core.ItemID(fmt.Sprintf("item:%d", i))
			b.Items = append(b.Items, core.Item{ID: id, Name: "Worn", Owner: "char:orin", Wearer: "char:orin"})
			ops = append(ops, core.Operation{Invoke: &core.InvokeAbility{Ability: "pass", Arguments: []core.AbilityArgument{{Name: "item", Value: core.AbilityValue{Item: ptr(id)}}}}})
		}
		w := world(t, b)
		err := costReject(t, w, costRequest(w, "events", ops...), "E_LIMIT")
		var f *core.Fault
		errors.As(err, &f)
		if f.Field != "events" {
			t.Fatal("did not reach event bound", err)
		}
		commitC(t, w, costRequest(w, "events", pay(1)))
		costFacts(t, w, 2, 6, 10, 1)
	})
}

func FuzzResourceSpendSequence(f *testing.F) {
	for _, seed := range [][]byte{{0, 1, 2, 3, 4, 5}, {3, 3, 0, 7}, {255, 0, 2, 2}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sequence []byte) {
		if len(sequence) > 24 {
			return
		}
		b := costFixture(t)
		w := world(t, b)
		balance := int64(3)
		revision := core.Revision(0)
		for i, v := range sequence {
			amount := int64(v%5 + 1)
			op := spend(amount)
			if v&1 == 1 {
				op = pay(amount)
			}
			r := costRequest(w, fmt.Sprintf("f:%d", i), op)
			if amount > balance {
				costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
			} else {
				got := commitC(t, w, r)
				balance -= amount
				revision++
				contractEqual(t, contractBytes(t, got), commitC(t, w, r))
			}
			costFacts(t, w, balance, 6, 10, revision)
			if v&2 != 0 {
				commitC(t, w, costRequest(w, fmt.Sprintf("h:%d", i), focusHeal(2)))
				balance += 2
				if balance > 5 {
					balance = 5
				}
				revision++
			}
		}
		h := w.History()
		data := contractBytes(t, h)
		var decoded []core.Batch
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := core.Replay(b, decoded)
		if err != nil {
			t.Fatal(err)
		}
		contractEqual(t, contractBytes(t, w.Snapshot()), rebuilt.Snapshot())
		contractEqual(t, data, rebuilt.History())
	})
}
