package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"strings"
	"testing"
)

func m10Source(t testing.TB) string {
	t.Helper()
	s, e := os.ReadFile("../examples/m10/world.nql")
	if e != nil {
		t.Fatal(e)
	}
	return string(s)
}
func m10Compile(t testing.TB, s string) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("m10-world.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return b
}
func m10Fixture(t testing.TB) core.Blueprint { return m10Compile(t, m10Source(t)) }
func m10Actions(t testing.TB, b core.Blueprint, s string) []core.Operation {
	t.Helper()
	ops, ds := compiler.CompileActions("m10-actions.nql", s, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return ops
}
func m10Invoke(id core.AbilityID, args ...core.AbilityArgument) core.Operation {
	v := core.CanonicalInvocation(core.InvokeAbility{Ability: id, Arguments: args})
	return core.Operation{Invoke: &v}
}
func m10Character(name core.ParameterID, id core.CharacterID) core.AbilityArgument {
	return core.AbilityArgument{Name: name, Value: core.AbilityValue{Character: ptr(id)}}
}
func m10Int(name core.ParameterID, n int64) core.AbilityArgument {
	return core.AbilityArgument{Name: name, Value: core.AbilityValue{Integer: ptr(n)}}
}
func m10Strike() core.Operation {
	return m10Invoke("ability:focused-strike", m10Character("payer", "char:orin"), m10Character("target", "char:lyra"))
}
func m10Damage(n int64) core.Operation {
	return core.Operation{Damage: &core.ResourceOp{Character: "char:lyra", Resource: "hp", Amount: n}}
}
func m10Heal(n int64) core.Operation {
	return core.Operation{Heal: &core.ResourceOp{Character: "char:lyra", Resource: "hp", Amount: n}}
}

const m10ParamDefinition = `ability "parameter-damage" named "Parameterized" {
 damage param "target" resource "hp" by param "amount";
 parameter "amount" int; parameter "target" character;
}`

func m10Param(n int64) core.Operation {
	return m10Invoke("parameter-damage", m10Int("amount", n), m10Character("target", "char:lyra"))
}
func m10Stamp(r core.Request, events []core.Event, ability core.AbilityID) core.Batch {
	for i := range events {
		e := &events[i]
		e.ID = fmt.Sprintf("event:%d:%d", r.ExpectedRevision+1, i+1)
		e.Revision = r.ExpectedRevision + 1
		e.RequestID = r.ID
		e.Actor = r.Actor
		if ability != "" && e.Kind != core.AbilityInvoked {
			e.AbilityOrigin = &core.AbilityOrigin{Ability: ability, Step: i}
		}
	}
	return core.Batch{Revision: r.ExpectedRevision + 1, Request: r, Events: events}
}
func m10Change(kind core.EventKind, char core.CharacterID, res core.ResourceID, amount int64, before, after core.Resource) core.Event {
	return core.Event{Kind: kind, Resource: &core.ResourceChange{Character: char, Resource: res, Requested: amount, Before: before, After: after}}
}
func m10Resource(n, max int64) core.Resource {
	return core.Resource{Current: n, Minimum: 0, Maximum: max}
}

func TestM10FocusedStrikeCompleteReceipt(t *testing.T) {
	b := m10Fixture(t)
	w := world(t, b)
	src, e := os.ReadFile("../examples/m10/focused-strike.nql")
	if e != nil {
		t.Fatal(e)
	}
	r := costRequest(w, "m10:R1", m10Actions(t, b, string(src))...)
	old := w.Snapshot()
	payer, _ := w.CharacterStatus("char:orin")
	target, _ := w.CharacterStatus("char:lyra")
	raw, e := os.ReadFile("testdata/m10-expected-first-batch.json")
	if e != nil {
		t.Fatal(e)
	}
	var want core.Batch
	if e = json.Unmarshal(raw, &want); e != nil {
		t.Fatal(e)
	}
	got := commitC(t, w, r)
	contractEqual(t, contractBytes(t, want), got)
	contractEqual(t, contractBytes(t, want), commitC(t, w, r))
	old.Revision = 1
	costCharacter(&old.Blueprint, "char:orin").Resources["focus"] = m10Resource(1, 5)
	costCharacter(&old.Blueprint, "char:lyra").Resources["hp"] = m10Resource(2, 20)
	contractEqual(t, contractBytes(t, old), w.Snapshot())
	p2, _ := w.CharacterStatus("char:orin")
	t2, _ := w.CharacterStatus("char:lyra")
	if payer.Revision != 0 || target.Revision != 0 || payer.Resources["focus"].Current != 3 || target.Resources["hp"].Current != 6 || p2.Revision != 1 || t2.Revision != 1 || p2.Resources["focus"].Current != 1 || t2.Resources["hp"].Current != 2 {
		t.Fatal("historical projection")
	}
	costReplay(t, b, w)
	t.Logf("native complete receipt: %s", contractBytes(t, got))
}

func TestM10DamageAbilityNumericMatrix(t *testing.T) {
	for _, tc := range m10DamageRows {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, m10Source(t)+m10ParamDefinition)
			before := core.Resource{Current: tc.current, Minimum: tc.minimum, Maximum: tc.maximum}
			after := core.Resource{Current: tc.after, Minimum: tc.minimum, Maximum: tc.maximum}
			costCharacter(&b, "char:lyra").Resources["hp"] = before
			w := world(t, b)
			r := costRequest(w, "m10:"+tc.name, m10Param(tc.amount))
			got := commitC(t, w, r)
			want := m10Stamp(r, []core.Event{m10Change(core.DamageApplied, "char:lyra", "hp", tc.amount, before, after), {Kind: core.AbilityInvoked, Ability: r.Operations[0].Invoke}}, "parameter-damage")
			contractEqual(t, contractBytes(t, want), got)
			s := w.Snapshot()
			if costCharacter(&s.Blueprint, "char:lyra").Resources["hp"] != after {
				t.Fatal("endpoint")
			}
			costReplay(t, b, w)
		})
	}
}

func TestM10PaidControlsAndAuthoredVariants(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		focus, min, max, hp, afterFocus, afterHP int64
		reject, self                             bool
	}{
		{"exact", 2, 0, 5, 6, 0, 2, false, false}, {"insufficient", 1, 0, 5, 6, 1, 6, true, false},
		{"clamp", 3, 0, 5, 2, 1, 0, false, false}, {"minimum", 3, 0, 5, 0, 1, 0, false, false},
		{"positive floor", 4, 2, 7, 6, 2, 2, false, false}, {"floor rejects", 3, 2, 7, 6, 3, 6, true, false},
		{"self", 3, 0, 5, 6, 1, 6, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Fixture(t)
			costBalance(&b, tc.focus, tc.min, tc.max)
			costCharacter(&b, "char:lyra").Resources["hp"] = m10Resource(tc.hp, 20)
			w := world(t, b)
			before := w.Snapshot()
			op := m10Strike()
			if tc.self {
				*op.Invoke.Arguments[1].Value.Character = "char:orin"
			}
			r := costRequest(w, "paid", op)
			if tc.reject {
				costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
				return
			}
			got := commitC(t, w, r)
			costFacts(t, w, tc.afterFocus, tc.afterHP, 10, 1)
			before.Revision = 1
			costCharacter(&before.Blueprint, "char:orin").Resources["focus"] = core.Resource{Current: tc.afterFocus, Minimum: tc.min, Maximum: tc.max}
			target := core.CharacterID("char:lyra")
			hpBefore, hpAfter := tc.hp, tc.afterHP
			if tc.self {
				target = "char:orin"
				hpBefore, hpAfter = 8, 4
			}
			costCharacter(&before.Blueprint, target).Resources["hp"] = m10Resource(hpAfter, 20)
			contractEqual(t, contractBytes(t, before), w.Snapshot())
			want := m10Stamp(r, []core.Event{m10Change(core.ResourceSpent, "char:orin", "focus", 2, core.Resource{Current: tc.focus, Minimum: tc.min, Maximum: tc.max}, core.Resource{Current: tc.afterFocus, Minimum: tc.min, Maximum: tc.max}), m10Change(core.DamageApplied, target, "hp", 4, m10Resource(hpBefore, 20), m10Resource(hpAfter, 20)), {Kind: core.AbilityInvoked, Ability: op.Invoke}}, op.Invoke.Ability)
			contractEqual(t, contractBytes(t, want), got)
			costReplay(t, b, w)
		})
	}
	for _, tc := range []struct {
		name, source string
		hp, damage   int64
	}{
		{"five", strings.Replace(m10Source(t), `resource "hp" by 4;`, `resource "hp" by 5;`, 1), 1, 5},
		{"renamed", strings.ReplaceAll(m10Source(t), "ability:focused-strike", "ability:unrelated-name"), 2, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, tc.source)
			w := world(t, b)
			op := m10Strike()
			op.Invoke.Ability = b.AbilityDefinitions[0].ID
			got := commitC(t, w, costRequest(w, "variant", op))
			costFacts(t, w, 1, tc.hp, 10, 1)
			if len(got.Events) != 3 || got.Events[0].Resource.Requested != 2 || got.Events[1].Resource.Requested != tc.damage {
				t.Fatal(got)
			}
			costReplay(t, b, w)
		})
	}
}

func TestM10OrderedCompositionAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		hp, focus  int64
		reject     bool
	}{
		{"damage then heal", `damage "char:lyra" resource "hp" by 4; heal "char:lyra" resource "hp" by 4;`, 4, 3, false},
		{"heal then damage", `heal "char:lyra" resource "hp" by 4; damage "char:lyra" resource "hp" by 4;`, 2, 3, false},
		{"damage then spend", `damage "char:orin" resource "focus" by 4; spend "char:orin" resource "focus" by 2;`, 2, 3, true},
		{"spend then damage", `spend "char:orin" resource "focus" by 2; damage "char:orin" resource "focus" by 4;`, 2, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := m10Compile(t, m10Source(t)+`ability "ordered" named "Ordered" {`+tc.body+`}`)
			costCharacter(&b, "char:lyra").Resources["hp"] = m10Resource(2, 20)
			w := world(t, b)
			r := costRequest(w, "order", m10Invoke("ordered"))
			if tc.reject {
				err := costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
				var ae *core.AbilityError
				if !errors.As(err, &ae) || ae.Step != 1 {
					t.Fatal(err)
				}
				return
			}
			got := commitC(t, w, r)
			costFacts(t, w, tc.focus, tc.hp, 10, 1)
			if len(got.Events) != 3 || got.Events[0].AbilityOrigin.Step != 0 || got.Events[1].AbilityOrigin.Step != 1 {
				t.Fatal(got)
			}
			// Fixed intermediate oracles catch net-delta optimization.
			first, second := got.Events[0].Resource, got.Events[1].Resource
			switch tc.name {
			case "damage then heal":
				if first.After.Current != 0 || second.Before.Current != 0 {
					t.Fatal(got)
				}
			case "heal then damage":
				if first.After.Current != 6 || second.Before.Current != 6 {
					t.Fatal(got)
				}
			case "spend then damage":
				if first.After.Current != 1 || second.Before.Current != 1 {
					t.Fatal(got)
				}
			}
			costReplay(t, b, w)
		})
	}
	t.Run("later outer", func(t *testing.T) {
		b := m10Fixture(t)
		w := world(t, b)
		r := costRequest(w, "failed", m10Strike(), core.Operation{Transfer: &core.Transfer{Item: "item:token", From: "char:tess", To: "char:lyra"}})
		for _, op := range r.Operations {
			if err := b.CheckOperationStatic(op); err != nil {
				t.Fatal(err)
			}
		}
		costReject(t, w, r, "E_OWNER")
		r.Operations[1].Transfer.From = "char:orin"
		got := commitC(t, w, r)
		costFacts(t, w, 1, 2, 10, 1)
		if len(got.Events) != 4 || got.Events[3].Operation != 1 {
			t.Fatal(got)
		}
		contractEqual(t, contractBytes(t, got), commitC(t, w, r))
		costReplay(t, b, w)
	})
	t.Run("later unique", func(t *testing.T) {
		b := m10Fixture(t)
		d := &b.AbilityDefinitions[0]
		d.Parameters = append(d.Parameters, core.AbilityParameter{ID: "guard_id", Kind: core.ConditionIDArgument})
		d.Steps = append(d.Steps, core.AbilityStep{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Parameter: "guard_id"}, Type: "condition:guarded", Target: core.AbilityOperand{Parameter: "target"}, Source: &core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}}})
		b.Conditions = []core.ConditionInstance{{ID: "old", Type: "condition:guarded", Target: "char:lyra"}}
		w := world(t, b)
		op := m10Strike()
		op.Invoke.Arguments = append(op.Invoke.Arguments, core.AbilityArgument{Name: "guard_id", Value: core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("fresh"))}})
		r := costRequest(w, "failed", op)
		err := costReject(t, w, r, "E_RULE")
		var ae *core.AbilityError
		if !errors.As(err, &ae) || ae.Step != 2 || ae.Operand != "target" {
			t.Fatal(err)
		}
		costFacts(t, w, 3, 6, 13, 0)
		// Another fresh ID alone still conflicts. Neither failed identity is reserved.
		*op.Invoke.Arguments[2].Value.ConditionID = "also-fresh"
		costReject(t, w, costRequest(w, "still-failed", op), "E_RULE")
		*op.Invoke.Arguments[2].Value.ConditionID = "fresh"
		commitC(t, w, costRequest(w, "unblock", removeC("old", "char:lyra")))
		r.ExpectedRevision = w.Revision()
		got := commitC(t, w, r)
		costFacts(t, w, 1, 2, 13, 2)
		s := w.Snapshot()
		if !reflect.DeepEqual(s.RetiredConditionIDs, []core.ConditionInstanceID{"old"}) || len(s.Conditions) != 1 || s.Conditions[0].ID != "fresh" {
			t.Fatal(s)
		}
		c := s.Conditions[0]
		if c.Source == nil || c.Source.ID != "loc:shrine" || c.AppliedRevision != 2 || c.Cause.RequestID != "failed" || c.Cause.Operation != 0 || c.Cause.Actor != r.Actor || c.Cause.AbilityOrigin.Step != 2 {
			t.Fatal(c)
		}
		if len(got.Events) != 4 || got.Events[2].Condition.After.ID != "fresh" {
			t.Fatal(got)
		}
		costReplay(t, b, w)
	})
}

func m10Journey(t testing.TB) (core.Blueprint, *core.World) {
	t.Helper()
	b := m10Fixture(t)
	w := world(t, b)
	rows := []struct {
		op        core.Operation
		focus, hp int64
		events    int
	}{
		{m10Strike(), 1, 2, 3}, {focusHeal(3), 4, 2, 4}, {m10Strike(), 2, 0, 7}, {m10Heal(5), 2, 5, 8}, {spend(2), 0, 5, 9}, {m10Damage(1), 0, 4, 10},
	}
	count := 0
	for i, row := range rows {
		r := costRequest(w, fmt.Sprintf("m10:R%d", i+1), row.op)
		got := commitC(t, w, r)
		count += len(got.Events)
		costFacts(t, w, row.focus, row.hp, 10, core.Revision(i+1))
		if count != row.events {
			t.Fatal(count, row.events)
		}
		if i == 2 && (got.Events[1].Resource.Requested != 4 || got.Events[1].Resource.Before.Current != 2 || got.Events[1].Resource.After.Current != 0) {
			t.Fatal(got)
		}
	}
	return b, w
}
func TestM10JourneyHistoricalReceiptsAndIdentity(t *testing.T) {
	b, w := m10Journey(t)
	s, h := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	for _, i := range []int{0, 2} {
		original := w.History()[i]
		contractEqual(t, contractBytes(t, original), commitC(t, w, original.Request))
	}
	contractEqual(t, s, w.Snapshot())
	contractEqual(t, h, w.History())
	rebuilt := costReplay(t, b, w)
	for _, v := range []*core.World{w, rebuilt} {
		r := costRequest(v, "new", m10Strike())
		r.ExpectedRevision = 0
		costReject(t, v, r, "E_STALE")
		r.ExpectedRevision = v.Revision()
		costReject(t, v, r, "E_INSUFFICIENT_RESOURCE")
		original := v.History()[0].Request
		for _, variant := range []string{"target", "ability", "inline"} {
			r := v.History()[0].Request
			switch variant {
			case "target":
				*r.Operations[0].Invoke.Arguments[1].Value.Character = "char:orin"
			case "ability":
				r.Operations[0].Invoke.Ability = "other"
			case "inline":
				r.Operations = []core.Operation{spend(2), m10Damage(4)}
			}
			costReject(t, v, r, "E_REQUEST_REUSE")
		}
		contractEqual(t, contractBytes(t, v.History()[0]), commitC(t, v, original))
		// Rejected ID is reusable after explicit funding, not implicitly repaired.
		commitC(t, v, costRequest(v, "fund", focusHeal(2)))
		r.ExpectedRevision = v.Revision()
		commitC(t, v, r)
		costFacts(t, v, 0, 0, 10, 8)
	}
	t.Logf("native six-request history: %s", h)
}
func TestM10InlineEquivalenceKeepsDistinctIntent(t *testing.T) {
	b := m10Fixture(t)
	a, z := world(t, b), world(t, b)
	invoked := commitC(t, a, costRequest(a, "same-id", m10Strike()))
	inline := commitC(t, z, costRequest(z, "same-id", spend(2), m10Damage(4)))
	// Complete snapshots are the mechanical manifest: all definitions, identities,
	// resources/bounds, locations, ownership/wearing, stats, relations/lifetimes.
	contractEqual(t, contractBytes(t, a.Snapshot()), z.Snapshot())
	for _, id := range []core.CharacterID{"char:orin", "char:lyra", "char:tess"} {
		x, _ := a.CharacterStatus(id)
		y, _ := z.CharacterStatus(id)
		contractEqual(t, contractBytes(t, x), y)
	}
	if reflect.DeepEqual(invoked, inline) || len(invoked.Events) != 3 || len(inline.Events) != 2 || inline.Events[1].Operation != 1 || inline.Events[0].AbilityOrigin != nil || invoked.Request.Operations[0].Invoke == nil {
		t.Fatal("intent erased")
	}
	costReplay(t, b, a)
	costReplay(t, b, z)
}
