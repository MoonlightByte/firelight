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

func TestM10DamageTypedSumsAndBindingContext(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*core.DamageTemplate)
	}{
		{"empty target", "E_IR", func(d *core.DamageTemplate) { d.Target = core.AbilityOperand{} }},
		{"empty amount", "E_IR", func(d *core.DamageTemplate) { d.Amount = core.AbilityOperand{} }},
		{"both target forms", "E_IR", func(d *core.DamageTemplate) {
			d.Target.Literal = &core.AbilityValue{Character: ptr(core.CharacterID("char:lyra"))}
		}},
		{"both amount forms", "E_IR", func(d *core.DamageTemplate) { d.Amount.Literal = &core.AbilityValue{Integer: ptr(int64(1))} }},
		{"dual target value", "E_IR", func(d *core.DamageTemplate) {
			d.Target = core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:lyra")), Integer: ptr(int64(1))}}
		}},
		{"dual amount value", "E_IR", func(d *core.DamageTemplate) {
			d.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(int64(1)), Character: ptr(core.CharacterID("char:lyra"))}}
		}},
		{"empty literal", "E_IR", func(d *core.DamageTemplate) { d.Amount = core.AbilityOperand{Literal: &core.AbilityValue{}} }},
		{"wrong target kind", "E_TYPE", func(d *core.DamageTemplate) {
			d.Target = core.AbilityOperand{Literal: &core.AbilityValue{Item: ptr(core.ItemID("item:token"))}}
		}},
		{"wrong target parameter", "E_TYPE", func(d *core.DamageTemplate) { d.Target.Parameter = "amount" }},
		{"unknown parameter", "E_PARAMETER", func(d *core.DamageTemplate) { d.Amount.Parameter = "absent" }},
		{"empty selector", "E_ID", func(d *core.DamageTemplate) { d.Resource = "" }},
		{"zero literal", "E_AMOUNT", func(d *core.DamageTemplate) {
			d.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(int64(0))}}
		}},
		{"negative literal", "E_AMOUNT", func(d *core.DamageTemplate) {
			d.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptr(int64(-1))}}
		}},
		{"missing literal resource", "E_RESOURCE", func(d *core.DamageTemplate) {
			d.Target = core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:lyra"))}}
			d.Resource = "missing"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, m10Source(t)+m10ParamDefinition)
			tc.change(b.AbilityDefinitions[1].Steps[0].Damage)
			before := contractBytes(t, b)
			w, err := core.NewWorld(b)
			var f *core.Fault
			if w != nil || !errors.As(err, &f) || f.Code != tc.code {
				t.Fatal(w, err, tc.code)
			}
			contractEqual(t, before, b)
		})
	}
	for _, kind := range []string{"empty", "heal", "spend", "transfer", "condition"} {
		t.Run("sum/"+kind, func(t *testing.T) {
			b := m10Fixture(t)
			s := &b.AbilityDefinitions[0].Steps[1]
			switch kind {
			case "empty":
				*s = core.AbilityStep{}
			case "heal":
				s.Heal = &core.HealTemplate{}
			case "spend":
				s.Spend = &core.SpendTemplate{}
			case "transfer":
				s.Transfer = &core.TransferTemplate{}
			case "condition":
				s.ApplyCondition = &core.ConditionTemplate{}
			}
			w, err := core.NewWorld(b)
			var f *core.Fault
			if w != nil || !errors.As(err, &f) || f.Code != "E_IR" {
				t.Fatal(w, err)
			}
		})
	}
	for _, tc := range []struct {
		name, code, field, param string
		step                     int
		mutate                   func(*core.InvokeAbility)
	}{
		{"unknown ability", "E_ABILITY", "argument", "", -1, func(v *core.InvokeAbility) { v.Ability = "missing" }},
		{"missing argument", "E_ARGUMENT", "argument", "amount", -1, func(v *core.InvokeAbility) { v.Arguments = v.Arguments[1:] }},
		{"extra argument", "E_ARGUMENT", "argument", "extra", -1, func(v *core.InvokeAbility) { v.Arguments = append(v.Arguments, m10Int("extra", 2)) }},
		{"duplicate argument", "E_DUPLICATE", "argument", "amount", -1, func(v *core.InvokeAbility) { v.Arguments = append(v.Arguments, v.Arguments[0]) }},
		{"wrong kind", "E_TYPE", "argument", "target", -1, func(v *core.InvokeAbility) {
			v.Arguments[1].Value = core.AbilityValue{Item: ptr(core.ItemID("item:token"))}
		}},
		{"unknown character", "E_REFERENCE", "argument", "target", -1, func(v *core.InvokeAbility) { *v.Arguments[1].Value.Character = "missing" }},
		{"location as character", "E_TYPE", "argument", "target", -1, func(v *core.InvokeAbility) { *v.Arguments[1].Value.Character = "loc:shrine" }},
		{"zero", "E_AMOUNT", "amount", "amount", 0, func(v *core.InvokeAbility) { *v.Arguments[0].Value.Integer = 0 }},
		{"negative", "E_AMOUNT", "amount", "amount", 0, func(v *core.InvokeAbility) { *v.Arguments[0].Value.Integer = -1 }},
		{"dual argument value", "E_IR", "argument", "amount", -1, func(v *core.InvokeAbility) { v.Arguments[0].Value.Character = ptr(core.CharacterID("char:lyra")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, m10Source(t)+m10ParamDefinition)
			w := world(t, b)
			op := m10Param(4)
			tc.mutate(op.Invoke)
			err := costReject(t, w, costRequest(w, "retry", op), tc.code)
			var ae *core.AbilityError
			if !errors.As(err, &ae) || ae.Step != tc.step || ae.Operand != tc.field || string(ae.Parameter) != tc.param {
				t.Fatal(err)
			}
			encoded := string(contractBytes(t, err))
			for _, key := range []string{`"source"`, `"line"`, `"column"`} {
				if strings.Contains(encoded, key) {
					t.Fatal("invented source", encoded)
				}
			}
			commitC(t, w, costRequest(w, "retry", m10Param(4)))
		})
	}
	t.Run("bound resource body context", func(t *testing.T) {
		b := m10Compile(t, m10Source(t)+m10ParamDefinition)
		delete(costCharacter(&b, "char:lyra").Resources, "hp")
		// Both targets are parameters; resource existence is checked when they bind.
		w := world(t, b)
		err := costReject(t, w, costRequest(w, "missing", m10Param(4)), "E_RESOURCE")
		var ae *core.AbilityError
		if !errors.As(err, &ae) || ae.Step != 0 || ae.Operand != "target" || ae.Parameter != "target" {
			t.Fatal(err)
		}
	})
	for _, s := range []string{`{"amount":9223372036854775808}`, `{"amount":-9223372036854775809}`} {
		var op core.ResourceOp
		if json.Unmarshal([]byte(s), &op) == nil {
			t.Fatal("out-of-range serialized direct amount")
		}
	}
	for _, s := range []string{`{"literal":{"integer":9223372036854775808}}`, `{"literal":{"integer":-9223372036854775809}}`} {
		var operand core.AbilityOperand
		if json.Unmarshal([]byte(s), &operand) == nil {
			t.Fatal("out-of-range serialized template amount")
		}
	}
}

func TestM10ConditionCauseCannotPointAtDamage(t *testing.T) {
	b := m10Fixture(t)
	b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Literal: &core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("new"))}}, Type: "condition:guarded", Target: core.AbilityOperand{Parameter: "target"}}})
	b.Conditions = []core.ConditionInstance{{ID: "new", Type: "condition:guarded", Target: "char:lyra", AppliedRevision: 1, Cause: &core.ConditionCause{RequestID: "cause", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, AbilityOrigin: &core.AbilityOrigin{Ability: "ability:focused-strike", Step: 2}}}}
	if err := core.Validate(b); err != nil {
		t.Fatal("positive origin", err)
	}
	b.Conditions[0].Cause.AbilityOrigin.Step = 1
	err := core.Validate(b)
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_PROVENANCE" || f.Field != "origin" {
		t.Fatal("damage accepted as creation origin", err)
	}
}

func TestM10ReplayContradictionsAndValidPrefixes(t *testing.T) {
	b, w := m10Journey(t)
	original := contractBytes(t, w.Snapshot())
	genesis := contractBytes(t, b)
	history := contractBytes(t, w.History())
	for _, tc := range []struct {
		name   string
		change func([]core.Batch)
	}{
		{"requested", func(h []core.Batch) { h[0].Events[1].Resource.Requested = 5 }},
		{"target", func(h []core.Batch) { h[0].Events[1].Resource.Character = "char:orin" }},
		{"selector", func(h []core.Batch) { h[0].Events[1].Resource.Resource = "focus" }},
		{"before current", func(h []core.Batch) { h[0].Events[1].Resource.Before.Current = 7 }},
		{"before minimum", func(h []core.Batch) { h[0].Events[1].Resource.Before.Minimum = -1 }},
		{"before maximum", func(h []core.Batch) { h[0].Events[1].Resource.Before.Maximum = 21 }},
		{"after current", func(h []core.Batch) { h[0].Events[1].Resource.After.Current = 3 }},
		{"after minimum", func(h []core.Batch) { h[0].Events[1].Resource.After.Minimum = -1 }},
		{"after maximum", func(h []core.Batch) { h[0].Events[1].Resource.After.Maximum = 21 }},
		{"event kind", func(h []core.Batch) { h[0].Events[1].Kind = core.HealingApplied }},
		{"event order", func(h []core.Batch) { h[0].Events[0], h[0].Events[1] = h[0].Events[1], h[0].Events[0] }},
		{"origin step", func(h []core.Batch) { h[0].Events[1].AbilityOrigin.Step = 0 }},
		{"origin ability", func(h []core.Batch) { h[0].Events[1].AbilityOrigin.Ability = "other" }},
		{"missing origin", func(h []core.Batch) { h[0].Events[1].AbilityOrigin = nil }},
		{"terminal origin", func(h []core.Batch) {
			h[0].Events[2].AbilityOrigin = &core.AbilityOrigin{Ability: "ability:focused-strike", Step: 1}
		}},
		{"outer group", func(h []core.Batch) { h[0].Events[1].Operation = 1 }},
		{"terminal ability", func(h []core.Batch) { h[0].Events[2].Ability.Ability = "other" }},
		{"terminal bindings", func(h []core.Batch) { *h[0].Events[2].Ability.Arguments[1].Value.Character = "char:orin" }},
		{"retained request", func(h []core.Batch) { *h[0].Request.Operations[0].Invoke.Arguments[1].Value.Character = "char:orin" }},
		{"event actor", func(h []core.Batch) { h[0].Events[1].Actor.ID = "char:orin" }},
		{"request actor", func(h []core.Batch) { h[0].Request.Actor.ID = "char:orin" }},
		{"event revision", func(h []core.Batch) { h[0].Events[1].Revision = 2 }},
		{"batch revision", func(h []core.Batch) { h[0].Revision = 2 }},
		{"request revision", func(h []core.Batch) { h[0].Request.ExpectedRevision = 1 }},
		{"event id", func(h []core.Batch) { h[0].Events[1].ID = "other" }},
		{"late R6", func(h []core.Batch) { h[5].Events[0].Resource.After.Current = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := w.History()
			tc.change(h)
			input := contractBytes(t, h)
			rebuilt, err := core.Replay(b, h)
			if rebuilt != nil || err == nil {
				t.Fatal("contradiction accepted", rebuilt, err)
			}
			contractEqual(t, input, h)
			contractEqual(t, genesis, b)
			contractEqual(t, original, w.Snapshot())
			contractEqual(t, history, w.History())
		})
	}
	wrong := m10Compile(t, strings.Replace(m10Source(t), `resource "hp" by 4;`, `resource "hp" by 5;`, 1))
	h := w.History()[:1]
	if rebuilt, err := core.Replay(wrong, h); rebuilt != nil || err == nil {
		t.Fatal("changed definition accepted", err)
	}
	focuses := []int64{3, 1, 4, 2, 2, 0, 0}
	hps := []int64{6, 2, 2, 0, 5, 5, 4}
	for i := 0; i <= 6; i++ {
		r, err := core.Replay(b, w.History()[:i])
		if err != nil {
			t.Fatal(err)
		}
		costFacts(t, r, focuses[i], hps[i], 10, core.Revision(i))
		for _, batch := range r.History() {
			contractEqual(t, contractBytes(t, batch), commitC(t, r, batch.Request))
		}
	}
	alternative := world(t, wrong)
	commitC(t, alternative, costRequest(alternative, "alternative", m10Strike()))
	costFacts(t, alternative, 1, 1, 10, 1)
	costReplay(t, wrong, alternative)
}

func TestM10DamageDetachedInputsOutputsAndReceipts(t *testing.T) {
	source := m10Source(t) + `ability "literal" named "Literal" { damage "char:lyra" resource "hp" by 1; }`
	b := m10Compile(t, source)
	second := m10Compile(t, source)
	secondBytes := contractBytes(t, second)
	w := world(t, b)
	initial := contractBytes(t, w.Snapshot())
	sibling := w.Snapshot()
	siblingBytes := contractBytes(t, sibling)
	// Mutate both new literal/value pointer depths and the outer template after NewWorld.
	*b.AbilityDefinitions[1].Steps[0].Damage.Target.Literal.Character = "char:orin"
	*b.AbilityDefinitions[1].Steps[0].Damage.Amount.Literal.Integer = 9
	b.AbilityDefinitions[0].Steps[1].Damage.Resource = "missing"
	b.AbilityDefinitions[1].Steps[0].Damage = nil
	contractEqual(t, initial, w.Snapshot())
	contractEqual(t, siblingBytes, sibling)
	contractEqual(t, secondBytes, second)
	view := w.Snapshot()
	*view.AbilityDefinitions[1].Steps[0].Damage.Target.Literal.Character = "char:tess"
	view.AbilityDefinitions[1].Steps[0].Damage.Amount.Literal = &core.AbilityValue{Integer: ptr(int64(99))}
	view.AbilityDefinitions[0].Steps[1].Damage.Target.Parameter = "other"
	contractEqual(t, initial, w.Snapshot())
	contractEqual(t, siblingBytes, sibling)
	text := `invoke "ability:focused-strike" { "payer" = character "char:orin"; "target" = character "char:lyra"; };`
	ops := m10Actions(t, second, text)
	otherOps := m10Actions(t, second, text)
	otherBytes := contractBytes(t, otherOps)
	r := costRequest(w, "detached", ops...)
	got := commitC(t, w, r)
	receipt := w.History()[0]
	receiptBytes := contractBytes(t, receipt)
	state := contractBytes(t, w.Snapshot())
	history := contractBytes(t, w.History())
	*ops[0].Invoke.Arguments[0].Value.Character = "char:tess"
	*r.Operations[0].Invoke.Arguments[1].Value.Character = "char:orin"
	contractEqual(t, otherBytes, otherOps)
	contractEqual(t, secondBytes, second)
	terminalBefore := contractBytes(t, got.Events[2].Ability)
	*got.Request.Operations[0].Invoke.Arguments[1].Value.Character = "char:orin"
	contractEqual(t, terminalBefore, got.Events[2].Ability)
	requestBeforeTerminalEdit := contractBytes(t, got.Request)
	got.Events[0].Resource.Before.Current = 999
	got.Events[1].Resource.After.Current = 999
	got.Events[1].AbilityOrigin.Step = 99
	*got.Events[2].Ability.Arguments[1].Value.Character = "char:tess"
	contractEqual(t, requestBeforeTerminalEdit, got.Request)
	contractEqual(t, state, w.Snapshot())
	contractEqual(t, history, w.History())
	contractEqual(t, receiptBytes, receipt)
	detachedHistory := w.History()
	detachedHistory[0].Events[1].Resource.Requested = 99
	*detachedHistory[0].Events[2].Ability.Arguments[0].Value.Character = "char:tess"
	contractEqual(t, history, w.History())
	contractEqual(t, receiptBytes, commitC(t, w, receipt.Request))
	status, _ := w.CharacterStatus("char:lyra")
	anotherStatus, _ := w.CharacterStatus("char:lyra")
	statusBytes := contractBytes(t, anotherStatus)
	status.Resources["hp"] = m10Resource(999, 1000)
	contractEqual(t, state, w.Snapshot())
	contractEqual(t, statusBytes, anotherStatus)
	// Historical and reconstructed receipts at an unaffordable later world.
	genesis, late := m10Journey(t)
	original := late.History()[0]
	oldBytes := contractBytes(t, original)
	lateState := contractBytes(t, late.Snapshot())
	lateHistory := contractBytes(t, late.History())
	rebuilt := costReplay(t, genesis, late)
	for _, current := range []*core.World{late, rebuilt} {
		ret := commitC(t, current, original.Request)
		another := commitC(t, current, original.Request)
		ret.Events[1].Resource.Requested = 999
		ret.Events[1].Resource.Before.Minimum = -9
		*ret.Events[2].Ability.Arguments[0].Value.Character = "char:tess"
		*ret.Request.Operations[0].Invoke.Arguments[0].Value.Character = "char:tess"
		contractEqual(t, oldBytes, another)
		contractEqual(t, oldBytes, commitC(t, current, original.Request))
		contractEqual(t, lateState, current.Snapshot())
		contractEqual(t, lateHistory, current.History())
	}
	// Copied handles intentionally share authority, unlike value records.
	handle := *late
	commitC(t, &handle, costRequest(late, "shared", m10Damage(1)))
	costFacts(t, late, 0, 3, 10, 7)
}

func TestM10DamageConcurrencyAndWorkBounds(t *testing.T) {
	t.Run("same request", func(t *testing.T) {
		w := world(t, m10Fixture(t))
		r := costRequest(w, "parallel", m10Strike())
		handle := *w
		var wg sync.WaitGroup
		results := make([]core.Batch, 24)
		errs := make([]error, 24)
		for i := range results {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results[i], errs[i] = handle.Execute(r) }(i)
		}
		wg.Wait()
		for i, e := range errs {
			if e != nil {
				t.Fatal(e)
			}
			contractEqual(t, contractBytes(t, w.History()[0]), results[i])
		}
		costFacts(t, w, 1, 2, 10, 1)
		if len(w.History()) != 1 {
			t.Fatal("duplicate commits")
		}
	})
	t.Run("competing then funds", func(t *testing.T) {
		b := m10Fixture(t)
		costBalance(&b, 2, 0, 5)
		w := world(t, b)
		rs := []core.Request{costRequest(w, "a", m10Strike()), costRequest(w, "b", m10Strike())}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		results := make([]core.Batch, 2)
		for i := range rs {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results[i], errs[i] = w.Execute(rs[i]) }(i)
		}
		wg.Wait()
		wins, stale := 0, 0
		for i, e := range errs {
			if e == nil {
				wins++
				continue
			}
			var f *core.Fault
			if !errors.As(e, &f) || f.Code != "E_STALE" || !reflect.DeepEqual(results[i], core.Batch{}) {
				t.Fatal(e, results[i])
			}
			stale++
			rs[i].ExpectedRevision = w.Revision()
			costReject(t, w, rs[i], "E_INSUFFICIENT_RESOURCE")
		}
		if wins != 1 || stale != 1 {
			t.Fatal(wins, stale)
		}
		costFacts(t, w, 0, 2, 10, 1)
		costReplay(t, b, w)
	})
	base := m10Source(t) + `ability "single" named "Single" { damage "char:lyra" resource "hp" by 1; }
 ability "double" named "Double" { damage "char:lyra" resource "hp" by 1; damage "char:lyra" resource "hp" by 1; }`
	for _, tc := range []struct {
		name, ability string
		n, events     int
	}{{"128 single", "single", 128, 256}, {"64 double", "double", 64, 192}} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, base)
			costCharacter(&b, "char:lyra").Resources["hp"] = m10Resource(200, 300)
			w := world(t, b)
			ops := make([]core.Operation, tc.n)
			for i := range ops {
				ops[i] = m10Invoke(core.AbilityID(tc.ability))
			}
			got := commitC(t, w, costRequest(w, "boundary", ops...))
			costFacts(t, w, 3, 72, 10, 1)
			if len(got.Events) != tc.events {
				t.Fatal(len(got.Events))
			}
			costReplay(t, b, w)
		})
	}
	for _, tc := range []struct {
		name, ability string
		n             int
		extra         bool
	}{{"outer", "single", 129, false}, {"expanded", "double", 64, true}} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, base)
			w := world(t, b)
			ops := make([]core.Operation, tc.n)
			for i := range ops {
				ops[i] = m10Invoke(core.AbilityID(tc.ability))
			}
			if tc.extra {
				ops = append(ops, m10Invoke("single"))
			}
			err := costReject(t, w, costRequest(w, "bound", ops...), "E_LIMIT")
			var f *core.Fault
			errors.As(err, &f)
			if f.Field != "operations" {
				t.Fatal(err)
			}
			commitC(t, w, costRequest(w, "bound", m10Invoke("single")))
			costFacts(t, w, 3, 5, 10, 1)
		})
	}
	t.Run("257 events within operation bounds", func(t *testing.T) {
		b := m10Compile(t, base+`ability "pass" named "HandOff" { parameter "item" item; transfer param "item" from "char:orin" to "char:lyra"; }`)
		ops := []core.Operation{m10Invoke("single")}
		for i := 0; i < 85; i++ {
			id := core.ItemID(fmt.Sprintf("worn:%d", i))
			b.Items = append(b.Items, core.Item{ID: id, Name: "Worn", Owner: "char:orin", Wearer: "char:orin"})
			ops = append(ops, m10Invoke("pass", core.AbilityArgument{Name: "item", Value: core.AbilityValue{Item: ptr(id)}}))
		}
		w := world(t, b)
		err := costReject(t, w, costRequest(w, "events", ops...), "E_LIMIT")
		var f *core.Fault
		errors.As(err, &f)
		if f.Field != "events" {
			t.Fatal(err)
		}
		commitC(t, w, costRequest(w, "events", m10Invoke("single")))
		costFacts(t, w, 3, 5, 10, 1)
	})
	if core.MaxOperations != 128 || core.MaxReplayBatches != 1024 || compiler.MaxSourceBytes != 1<<20 {
		t.Fatal("accepted bound changed")
	}
}
