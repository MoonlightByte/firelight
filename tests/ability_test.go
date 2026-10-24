package tests

import (
	"encoding/json"
	"errors"
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

func abilityFixture(t testing.TB) core.Blueprint {
	t.Helper()
	source, e := os.ReadFile("../examples/ability-world.nql")
	if e != nil {
		t.Fatal(e)
	}
	b, ds := compiler.CompileWorld("ability-world.nql", string(source))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return b
}
func aid(id string, amount int64) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "aid", Arguments: []core.AbilityArgument{
		{Name: "target", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:ada"))}},
		{Name: "amount", Value: core.AbilityValue{Integer: ptr(amount)}},
		{Name: "instance", Value: core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID(id))}},
	}}}
}
func handoff(item core.ItemID, from, to core.CharacterID) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "handoff", Arguments: []core.AbilityArgument{
		{Name: "item", Value: core.AbilityValue{Item: ptr(item)}}, {Name: "giver", Value: core.AbilityValue{Character: ptr(from)}}, {Name: "recipient", Value: core.AbilityValue{Character: ptr(to)}},
	}}}
}
func hp(t testing.TB, w *core.World, want int64) {
	t.Helper()
	p, e := w.CharacterStatus("char:ada")
	if e != nil || p.Resources["hp"].Current != want {
		t.Fatal("HP oracle", want, p.Resources, e)
	}
}
func abilityReject(t testing.TB, w *core.World, r core.Request, code string) {
	t.Helper()
	s, h := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	_, e := w.Execute(r)
	var f *core.Fault
	if !errors.As(e, &f) || f.Code != code {
		t.Fatalf("want %s got %v", code, e)
	}
	contractEqual(t, s, w.Snapshot())
	contractEqual(t, h, w.History())
}
func TestAbilityAidLifecycleAndExactHistory(t *testing.T) {
	b := abilityFixture(t)
	w := world(t, b)
	hp(t, w, 10)
	stat(t, w, "char:ada", "defense", 12)
	r := typedRequest(w, "first", aid("one", 4))
	r.Actor.ID = "char:bea"
	first := commitC(t, w, r)
	hp(t, w, 14)
	stat(t, w, "char:ada", "defense", 15)
	if first.Revision != 1 || len(first.Events) != 3 || first.Events[0].Kind != core.HealingApplied || first.Events[1].Kind != core.ConditionApplied || first.Events[2].Kind != core.AbilityInvoked || first.Events[2].AbilityOrigin != nil {
		t.Fatal("Aid event oracle", first)
	}
	for i, e := range first.Events {
		if e.ID != fmt.Sprintf("event:1:%d", i+1) || e.Operation != 0 || e.RequestID != "first" || e.Actor.ID != "char:bea" || e.Revision != 1 {
			t.Fatal("outer event envelope")
		}
		if i < 2 && (e.AbilityOrigin == nil || e.AbilityOrigin.Ability != "aid" || e.AbilityOrigin.Step != i) {
			t.Fatal("body lineage")
		}
	}
	c := conditionByID(t, w, "one")
	if c.Source.ID != "loc:shrine" || c.Cause.Actor.ID != "char:bea" || c.Cause.Operation != 0 || c.Cause.AbilityOrigin.Step != 1 || c.Cause.AbilityOrigin.Ability != "aid" {
		t.Fatal("source/actor/creation lineage", c)
	}
	contractEqual(t, contractBytes(t, first), commitC(t, w, r))
	if w.Revision() != 1 {
		t.Fatal("retry mutated")
	}
	commitC(t, w, typedRequest(w, "second", aid("two", 4)))
	hp(t, w, 18)
	stat(t, w, "char:ada", "defense", 18)
	removed := commitC(t, w, typedRequest(w, "remove-one", removeC("one", "char:ada")))
	hp(t, w, 18)
	stat(t, w, "char:ada", "defense", 15)
	if removed.Events[0].AbilityOrigin != nil || removed.Events[0].Condition.Before.Cause.AbilityOrigin.Step != 1 {
		t.Fatal("direct removal versus original creation cause")
	}
	commitC(t, w, typedRequest(w, "remove-two", removeC("two", "char:ada")))
	hp(t, w, 18)
	stat(t, w, "char:ada", "defense", 12)
	if w.Revision() != 4 || !reflect.DeepEqual(w.Snapshot().RetiredConditionIDs, []core.ConditionInstanceID{"one", "two"}) {
		t.Fatal("lifetime/revision")
	}
	for _, live := range []*core.World{w} {
		contractEqual(t, contractBytes(t, first), commitC(t, live, r))
	}
	rebuilt, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(rebuilt.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(rebuilt.History(), w.History()) {
		t.Fatal("exact Ability-path replay")
	}
	for _, receipt := range w.History() {
		contractEqual(t, contractBytes(t, receipt), commitC(t, rebuilt, receipt.Request))
	}
	if rebuilt.Revision() != 4 {
		t.Fatal("historical retry executed again")
	}
	abilityReject(t, rebuilt, typedRequest(rebuilt, "retired", aid("one", 4)), "E_DUPLICATE")
	commitC(t, rebuilt, typedRequest(rebuilt, "retired", aid("three", 1)))
	hp(t, rebuilt, 19)
	if again, e := core.Replay(b, rebuilt.History()); e != nil || !reflect.DeepEqual(again.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("replay continuation", e)
	}
}

// A04's frozen mechanical comparison manifest. This is NEVER used for exact
// historical reconstruction assertions, which compare complete typed records.
func mechanical(s core.Snapshot) core.Snapshot {
	s.AbilityDefinitions = nil
	for i := range s.Conditions {
		s.Conditions[i].Cause = nil
	}
	return s // includes definitions, AppliedRevision, resources, rules and effects
}
func TestAbilityMechanicalEquivalenceAndAuthoredRules(t *testing.T) {
	for _, policy := range []core.TransferPolicy{core.Unequips, core.KeepsWorn} {
		b := abilityFixture(t)
		b.Rules.Transfer = policy
		ability, inline := world(t, b), world(t, b)
		a := commitC(t, ability, typedRequest(ability, "invoke", aid("one", 4), handoff("item:shield", "char:bea", "char:cy")))
		execute(t, inline, "inline", `heal "char:ada" resource "hp" by 4; apply condition "one" of "blessed" to "char:ada" from location "loc:shrine"; transfer "item:shield" from "char:bea" to "char:cy";`)
		if !reflect.DeepEqual(mechanical(ability.Snapshot()), mechanical(inline.Snapshot())) {
			t.Fatal("mechanics differ", policy)
		}
		if reflect.DeepEqual(ability.History(), inline.History()) || reflect.DeepEqual(ability.Snapshot(), inline.Snapshot()) {
			t.Fatal("invocation identity/cause disappeared")
		}
		if conditionByID(t, inline, "one").Cause.AbilityOrigin != nil || conditionByID(t, ability, "one").Cause.AbilityOrigin == nil {
			t.Fatal("cause paths conflated")
		}
		want := []core.EventKind{core.ItemUnequipped, core.ItemTransferred, core.AbilityInvoked}
		def := int64(13)
		if policy == core.KeepsWorn {
			want = want[1:]
			def = 15
		}
		for i, k := range want {
			e := a.Events[3+i]
			if e.Kind != k || e.Operation != 1 {
				t.Fatal("HandOff order")
			}
			if i < len(want)-1 && (e.AbilityOrigin.Step != 0 || e.AbilityOrigin.Ability != "handoff") {
				t.Fatal("rule event origin")
			}
		}
		hp(t, ability, 14)
		stat(t, ability, "char:ada", "defense", def)
		if ability.Snapshot().Items[1].Owner != "char:cy" {
			t.Fatal("ownership")
		}
		rw, e := core.Replay(b, ability.History())
		if e != nil || !reflect.DeepEqual(rw.History(), ability.History()) {
			t.Fatal("rule-specific replay", e)
		}
	}
}
func TestAbilityRollbackOuterCandidateAndFreshIDs(t *testing.T) {
	b := abilityFixture(t)
	b.ConditionDefinitions[0].Instances = core.Unique
	w := world(t, b)
	commitC(t, w, typedRequest(w, "existing", applyC("existing", "blessed", "char:ada", nil)))
	r := typedRequest(w, "failed", aid("fresh", 4))
	abilityReject(t, w, r, "E_RULE")
	hp(t, w, 10)
	r.Operations = append([]core.Operation{removeC("existing", "char:ada")}, r.Operations...)
	commitC(t, w, r)
	hp(t, w, 14)
	if conditionByID(t, w, "fresh").Cause.Operation != 1 {
		t.Fatal("outer index flattened")
	}
	b = abilityFixture(t)
	w = world(t, b)
	abilityReject(t, w, typedRequest(w, "collision", aid("same", 4), aid("same", 4)), "E_DUPLICATE")
	hp(t, w, 10)
	commitC(t, w, typedRequest(w, "collision", aid("same", 4), aid("different", 4)))
	hp(t, w, 18)
	stat(t, w, "char:ada", "defense", 18)
	if w.Revision() != 1 || len(w.History()[0].Events) != 6 || conditionByID(t, w, "different").Cause.Operation != 1 {
		t.Fatal("two invocation occurrences")
	}
	// A creation ID may share an entity's spelling without referring to it.
	commitC(t, w, typedRequest(w, "namespace", aid("char:ada", 1)))
	w = world(t, b)
	ops := []core.Operation{{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 2}}, aid("a", 4), {Move: &core.Move{Character: "char:ada", To: "loc:shrine"}}}
	commitC(t, w, typedRequest(w, "mixed", ops...))
	hp(t, w, 12)
	if w.Revision() != 1 || conditionByID(t, w, "a").Cause.Operation != 1 || len(w.History()[0].Events) != 5 {
		t.Fatal("mixed outer transaction")
	}
	// Second invocation reads ownership changed by the first, on one candidate.
	commitC(t, w, typedRequest(w, "chain", handoff("item:parcel", "char:ada", "char:bea"), handoff("item:parcel", "char:bea", "char:cy")))
	if w.Snapshot().Items[0].Owner != "char:cy" {
		t.Fatal("stale candidate binding")
	}
	late := typedRequest(w, "late", aid("tentative", 1), core.Operation{Equip: &core.Equip{Item: "item:shield", Wearer: "char:cy"}})
	abilityReject(t, w, late, "E_WORN")
	commitC(t, w, typedRequest(w, "late", aid("tentative", 1)))
}
func TestAbilityFinalCandidateArithmetic(t *testing.T) {
	b := abilityFixture(t)
	b.Effects = nil
	b.Characters[0].Stats["defense"] = math.MaxInt64
	b.ConditionDefinitions[0].Modifiers[0].Fixed = ptr(int64(1))
	w := world(t, b)
	negative := core.Effect{ID: "negative", Source: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Stat: "defense", Amount: -1}
	commitC(t, w, typedRequest(w, "cancel", aid("positive", 1), core.Operation{Apply: &negative}))
	stat(t, w, "char:ada", "defense", math.MaxInt64)
	abilityReject(t, w, typedRequest(w, "overflow", core.Operation{Remove: &core.EffectOp{Effect: "negative"}}), "E_OVERFLOW")
	rw, e := core.Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(rw.History(), w.History()) {
		t.Fatal("outer arithmetic replay", e)
	}
	// A second invocation can be the compensating operation too.
	b.AbilityDefinitions = append(b.AbilityDefinitions, core.AbilityDefinition{ID: "cancel", Name: "Cancel", Steps: []core.AbilityStep{{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Literal: &core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("negative"))}}, Type: "negative", Target: core.AbilityOperand{Literal: &core.AbilityValue{Character: ptr(core.CharacterID("char:ada"))}}}}}})
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "negative", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "n", Stat: "defense", Fixed: ptr(int64(-1))}}})
	w = world(t, b)
	commitC(t, w, typedRequest(w, "two", aid("positive", 1), core.Operation{Invoke: &core.InvokeAbility{Ability: "cancel"}}))
	stat(t, w, "char:ada", "defense", math.MaxInt64)
}
func TestAbilityBindingAndRequestTruth(t *testing.T) {
	b := abilityFixture(t)
	copyDef := b.AbilityDefinitions[0]
	copyDef.ID = "another"
	b.AbilityDefinitions = append(b.AbilityDefinitions, copyDef)
	w := world(t, b)
	r := typedRequest(w, "R", aid("one", 4))
	receipt := commitC(t, w, r)
	r.Operations[0].Invoke.Arguments[0], r.Operations[0].Invoke.Arguments[2] = r.Operations[0].Invoke.Arguments[2], r.Operations[0].Invoke.Arguments[0]
	contractEqual(t, contractBytes(t, receipt), commitC(t, w, r))
	commitC(t, w, typedRequest(w, "later", removeC("one", "char:ada")))
	contractEqual(t, contractBytes(t, receipt), commitC(t, w, r))
	rebuilt, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	contractEqual(t, contractBytes(t, receipt), commitC(t, rebuilt, r))
	for _, mode := range []string{"ability", "binding", "inline"} {
		r := receipt.Request
		r.Operations = []core.Operation{aid("one", 4)}
		if mode == "ability" {
			r.Operations[0].Invoke.Ability = "another"
		}
		if mode == "binding" {
			*r.Operations[0].Invoke.Arguments[1].Value.Integer = 5
		}
		if mode == "inline" {
			r.Operations = []core.Operation{{Heal: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 4}}}
		}
		abilityReject(t, w, r, "E_REQUEST_REUSE")
	}
	stale := typedRequest(w, "stale", aid("fresh", 1))
	stale.ExpectedRevision = 0
	abilityReject(t, w, stale, "E_STALE")
	for _, tc := range []struct {
		name, code string
		mutate     func(*core.InvokeAbility)
	}{
		{"missing", "E_ARGUMENT", func(v *core.InvokeAbility) { v.Arguments = v.Arguments[:2] }},
		{"extra", "E_ARGUMENT", func(v *core.InvokeAbility) {
			v.Arguments = append(v.Arguments, core.AbilityArgument{Name: "extra", Value: core.AbilityValue{Integer: ptr(int64(1))}})
		}},
		{"duplicate", "E_DUPLICATE", func(v *core.InvokeAbility) { v.Arguments = append(v.Arguments, v.Arguments[0]) }},
		{"wrong kind", "E_TYPE", func(v *core.InvokeAbility) {
			v.Arguments[0].Value = core.AbilityValue{Item: ptr(core.ItemID("item:shield"))}
		}},
		{"wrong entity type", "E_TYPE", func(v *core.InvokeAbility) { *v.Arguments[0].Value.Character = "item:shield" }},
		{"unknown entity", "E_REFERENCE", func(v *core.InvokeAbility) { *v.Arguments[0].Value.Character = "missing" }},
		{"value sum", "E_IR", func(v *core.InvokeAbility) { v.Arguments[1].Value.Character = ptr(core.CharacterID("char:ada")) }},
		{"nonpositive", "E_AMOUNT", func(v *core.InvokeAbility) { *v.Arguments[1].Value.Integer = 0 }},
		{"bad identity", "E_ID", func(v *core.InvokeAbility) { *v.Arguments[2].Value.ConditionID = "" }},
		{"unknown ability", "E_ABILITY", func(v *core.InvokeAbility) { v.Ability = "missing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := aid("fresh", 4)
			tc.mutate(o.Invoke)
			abilityReject(t, w, typedRequest(w, "bad", o), tc.code)
		})
	}
	var tooLarge core.Operation
	if json.Unmarshal([]byte(`{"invoke":{"ability":"aid","arguments":[{"name":"amount","value":{"integer":9223372036854775808}}]}}`), &tooLarge) == nil {
		t.Fatal("typed boundary accepted overflow")
	}
	commitC(t, w, typedRequest(w, "bad", aid("fresh", 1)))
}

func TestAbilityConcurrentSharedAuthority(t *testing.T) {
	for _, same := range []bool{true, false} {
		w := world(t, abilityFixture(t))
		shared := *w
		var wg sync.WaitGroup
		var success atomic.Int32
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id := fmt.Sprint(i)
				if same {
					id = "same"
				}
				r := core.Request{ID: id, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{aid(id, 4)}}
				if _, e := shared.Execute(r); e == nil {
					success.Add(1)
				}
				w.Snapshot()
				w.History()
				w.CharacterStatus("char:ada")
			}(i)
		}
		wg.Wait()
		want := int32(1)
		if same {
			want = 16
		}
		if success.Load() != want || w.Revision() != 1 || len(w.Snapshot().Conditions) != 1 || len(w.History()) != 1 {
			t.Fatal("concurrent invocation", same, success.Load())
		}
		hp(t, w, 14)
	}
}

func TestAbilityDefinitionOnlyAuthoredChange(t *testing.T) {
	source, e := os.ReadFile("../examples/ability-world.nql")
	if e != nil {
		t.Fatal(e)
	}
	for _, changed := range []bool{false, true} {
		text := string(source)
		if changed {
			text = strings.Replace(text, `heal param "target" resource "hp" by param "amount";`, `heal param "target" resource "hp" by param "amount"; heal param "target" resource "hp" by 1;`, 1)
		}
		b, ds := compiler.CompileWorld("authored", text)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		w := world(t, b)
		commitC(t, w, typedRequest(w, "aid", aid("one", 4)))
		want := int64(14)
		if changed {
			want = 15
		}
		hp(t, w, want)
		stat(t, w, "char:ada", "defense", 15)
	}
}

func TestAbilityArgumentNamedActorDoesNotOverrideHost(t *testing.T) {
	b := abilityFixture(t)
	b.AbilityDefinitions[0].Parameters[0].ID = "actor"
	b.AbilityDefinitions[0].Steps[0].Heal.Target.Parameter = "actor"
	b.AbilityDefinitions[0].Steps[1].ApplyCondition.Target.Parameter = "actor"
	w := world(t, b)
	o := aid("one", 4)
	o.Invoke.Arguments[0].Name = "actor"
	r := typedRequest(w, "host-request", o)
	r.Actor.ID = "char:bea"
	got := commitC(t, w, r)
	c := conditionByID(t, w, "one")
	if c.Target != "char:ada" || c.Source.ID != "loc:shrine" || c.Cause.Actor.ID != "char:bea" || c.Cause.RequestID != "host-request" || c.AppliedRevision != 1 {
		t.Fatal("host metadata overwritten")
	}
	for _, e := range got.Events {
		if e.Actor.ID != "char:bea" || e.RequestID != "host-request" || e.Revision != 1 {
			t.Fatal("event attribution overwritten")
		}
	}
}

func TestAbilityRuntimeFaultOperandContext(t *testing.T) {
	w := world(t, abilityFixture(t))
	commitC(t, w, typedRequest(w, "first", aid("one", 4)))
	_, err := w.Execute(typedRequest(w, "duplicate", aid("one", 4)))
	var context *core.AbilityError
	var fault *core.Fault
	if !errors.As(err, &context) || !errors.As(err, &fault) || fault.Code != "E_DUPLICATE" || context.Step != 1 || context.Operand != "instance" || context.Parameter != "instance" {
		t.Error("creation collision blamed wrong operand", err)
	}
	_, err = w.Execute(typedRequest(w, "same-owner", handoff("item:parcel", "char:ada", "char:ada")))
	if !errors.As(err, &context) || context.Step != 0 || context.Operand != "to" || context.Parameter != "recipient" {
		t.Fatal("unchanged recipient blamed wrong operand", err)
	}
	// The creation ID also selects derived Effect identities. A preexisting
	// direct Effect may occupy that identity without invalidating genesis.
	b := abilityFixture(t)
	b.Effects = append(b.Effects, core.Effect{ID: "condition:3:one:4:ward", Source: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Stat: "defense", Amount: 0})
	w = world(t, b)
	before, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	_, err = w.Execute(typedRequest(w, "collision", aid("one", 4)))
	contractEqual(t, before, w.Snapshot())
	contractEqual(t, history, w.History())
	if !errors.As(err, &context) || !errors.As(err, &fault) || fault.Code != "E_DUPLICATE" || fault.Field != "effect" || context.Step != 1 || context.Operand != "instance" || context.Parameter != "instance" {
		t.Fatal("derived Effect collision blamed wrong operand", err)
	}
	commitC(t, w, typedRequest(w, "collision", aid("two", 4)))
	hp(t, w, 14)
	stat(t, w, "char:ada", "defense", 15)
}
