package tests

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"os"
	"reflect"
	"testing"
)

func costFixture(t testing.TB) core.Blueprint {
	t.Helper()
	s, err := os.ReadFile("../examples/m7/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("m7/world.nql", string(s))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return b
}
func costRequest(w *core.World, id string, ops ...core.Operation) core.Request {
	return core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, Operations: ops}
}
func spend(amount int64) core.Operation {
	return core.Operation{Spend: &core.ResourceOp{Character: "char:orin", Resource: "focus", Amount: amount}}
}
func focusHeal(amount int64) core.Operation {
	return core.Operation{Heal: &core.ResourceOp{Character: "char:orin", Resource: "focus", Amount: amount}}
}
func steady(id string) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "ability:steady-aid", Arguments: []core.AbilityArgument{
		{Name: "payer", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}},
		{Name: "target", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:lyra"))}},
		{Name: "guard_id", Value: core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID(id))}},
	}}}
}
func pay(amount int64) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "ability:pay", Arguments: []core.AbilityArgument{
		{Name: "payer", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}},
		{Name: "amount", Value: core.AbilityValue{Integer: ptr(amount)}},
	}}}
}
func costCharacter(b *core.Blueprint, id core.CharacterID) *core.Character {
	for i := range b.Characters {
		if b.Characters[i].ID == id {
			return &b.Characters[i]
		}
	}
	panic("missing test character")
}
func costBalance(b *core.Blueprint, current, min, max int64) {
	costCharacter(b, "char:orin").Resources["focus"] = core.Resource{Current: current, Minimum: min, Maximum: max}
}
func costFacts(t testing.TB, w *core.World, focus, hp, defense int64, revision core.Revision) {
	t.Helper()
	s := w.Snapshot()
	if s.Revision != revision || costCharacter(&s.Blueprint, "char:orin").Resources["focus"].Current != focus || costCharacter(&s.Blueprint, "char:lyra").Resources["hp"].Current != hp {
		t.Fatalf("want focus/hp/revision %d/%d/%d, got %+v", focus, hp, revision, s)
	}
	stat(t, w, "char:lyra", "defense", defense)
}
func costReject(t testing.TB, w *core.World, r core.Request, code string) error {
	t.Helper()
	s, h := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	got, err := w.Execute(r)
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != code || !reflect.DeepEqual(got, core.Batch{}) {
		t.Fatalf("want empty rejection %s: %+v %v", code, got, err)
	}
	contractEqual(t, s, w.Snapshot())
	contractEqual(t, h, w.History())
	return err
}
func costReplay(t testing.TB, b core.Blueprint, w *core.World) *core.World {
	t.Helper()
	h := w.History()
	before := contractBytes(t, h)
	rebuilt, err := core.Replay(b, h)
	if err != nil {
		t.Fatal(err)
	}
	contractEqual(t, before, h)
	contractEqual(t, contractBytes(t, w.Snapshot()), rebuilt.Snapshot())
	contractEqual(t, before, rebuilt.History())
	for _, receipt := range h {
		contractEqual(t, contractBytes(t, receipt), commitC(t, rebuilt, receipt.Request))
	}
	contractEqual(t, before, rebuilt.History())
	return rebuilt
}

func TestResourceSpendSteadyAidFixedReceipt(t *testing.T) {
	b := costFixture(t)
	w := world(t, b)
	opsSource, err := os.ReadFile("../examples/m7/steady-aid.nql")
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("steady-aid.nql", string(opsSource), b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	r := costRequest(w, "R1", ops...)
	before := w.Snapshot()
	oldStatus, _ := w.CharacterStatus("char:orin")
	got := commitC(t, w, r)
	costFacts(t, w, 1, 10, 13, 1)
	actor := core.Ref{Kind: core.CharacterKind, ID: "char:tess"}
	condition := core.ConditionInstance{ID: "guard:m7:one", Type: "condition:guarded", Target: "char:lyra", Source: &core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}, AppliedRevision: 1,
		Cause: &core.ConditionCause{RequestID: "R1", Actor: actor, Operation: 0, AbilityOrigin: &core.AbilityOrigin{Ability: "ability:steady-aid", Step: 2}}}
	effect := core.Effect{ID: "condition:12:guard:m7:one:4:ward", ConditionOrigin: &core.ConditionEffectOrigin{Instance: "guard:m7:one", Modifier: "ward"}, Source: core.Ref{Kind: core.ConditionKind, ID: "guard:m7:one"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "char:lyra"}, Stat: "defense", Amount: 3}
	invocation := core.InvokeAbility{Ability: "ability:steady-aid", Arguments: []core.AbilityArgument{
		{Name: "guard_id", Value: core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("guard:m7:one"))}},
		{Name: "payer", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:orin"))}},
		{Name: "target", Value: core.AbilityValue{Character: ptr(core.CharacterID("char:lyra"))}},
	}}
	want := core.Batch{Revision: 1, Request: core.Request{ID: "R1", Actor: actor, Operations: []core.Operation{{Invoke: &invocation}}}, Events: []core.Event{
		{Kind: core.ResourceSpent, Resource: &core.ResourceChange{Character: "char:orin", Resource: "focus", Requested: 2, Before: core.Resource{Current: 3, Minimum: 0, Maximum: 5}, After: core.Resource{Current: 1, Minimum: 0, Maximum: 5}}},
		{Kind: core.HealingApplied, Resource: &core.ResourceChange{Character: "char:lyra", Resource: "hp", Requested: 4, Before: core.Resource{Current: 6, Minimum: 0, Maximum: 20}, After: core.Resource{Current: 10, Minimum: 0, Maximum: 20}}},
		{Kind: core.ConditionApplied, Condition: &core.ConditionChange{After: &condition, AfterEffects: []core.Effect{effect}}},
		{Kind: core.AbilityInvoked, Ability: &invocation},
	}}
	for i := range want.Events {
		e := &want.Events[i]
		e.ID = fmt.Sprintf("event:1:%d", i+1)
		e.Actor = actor
		e.RequestID = "R1"
		e.Revision = 1
		if i < 3 {
			e.AbilityOrigin = &core.AbilityOrigin{Ability: "ability:steady-aid", Step: i}
		}
	}
	contractEqual(t, contractBytes(t, want), got)
	contractEqual(t, contractBytes(t, want), commitC(t, w, r))
	// Full independent delta from the initial snapshot, including unrelated facts.
	before.Revision = 1
	costCharacter(&before.Blueprint, "char:orin").Resources["focus"] = core.Resource{Current: 1, Minimum: 0, Maximum: 5}
	costCharacter(&before.Blueprint, "char:lyra").Resources["hp"] = core.Resource{Current: 10, Minimum: 0, Maximum: 20}
	before.Conditions = []core.ConditionInstance{condition}
	before.ConditionEffects = []core.Effect{effect}
	contractEqual(t, contractBytes(t, before), w.Snapshot())
	newStatus, _ := w.CharacterStatus("char:orin")
	if oldStatus.Revision != 0 || oldStatus.Resources["focus"].Current != 3 || newStatus.Revision != 1 || newStatus.Resources["focus"].Current != 1 {
		t.Fatal("historical status")
	}
	costReplay(t, b, w)
}

func TestResourceSpendExactNumericsAndAuthoredVariants(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		current, min, max, amount, want int64
		fails                           bool
	}{
		{"exact", 2, 0, 5, 2, 0, false}, {"insufficient", 1, 0, 5, 2, 1, true},
		{"positive floor", 4, 2, 7, 2, 2, false}, {"above floor", 3, 2, 7, 2, 3, true},
		{"negative floor", -3, -5, 5, 2, -5, false},
		{"wide distance", math.MaxInt64, math.MinInt64, math.MaxInt64, math.MaxInt64, 0, false},
		{"negative exact edge", -1, math.MinInt64, math.MaxInt64, math.MaxInt64, math.MinInt64, false},
		{"min plus one", math.MinInt64 + 1, math.MinInt64, math.MaxInt64, 1, math.MinInt64, false},
		{"underflow", math.MinInt64, math.MinInt64, math.MaxInt64, 1, math.MinInt64, true},
	} {
		for _, invoked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invoked=%t", tc.name, invoked), func(t *testing.T) {
				b := costFixture(t)
				costBalance(&b, tc.current, tc.min, tc.max)
				w := world(t, b)
				o := spend(tc.amount)
				if invoked {
					o = pay(tc.amount)
				}
				r := costRequest(w, "numeric", o)
				if tc.fails {
					err := costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
					var f *core.Fault
					errors.As(err, &f)
					if f.Subject != "char:orin" || f.Field != "focus" || f.Actual != fmt.Sprintf("current=%d minimum=%d requested=%d", tc.current, tc.min, tc.amount) {
						t.Fatal(f)
					}
					if invoked {
						var ae *core.AbilityError
						if !errors.As(err, &ae) || ae.Step != 0 || ae.Operand != "payer" || ae.Parameter != "payer" {
							t.Fatal(err)
						}
					}
					return
				}
				receipt := commitC(t, w, r)
				want := core.ResourceChange{Character: "char:orin", Resource: "focus", Requested: tc.amount, Before: core.Resource{Current: tc.current, Minimum: tc.min, Maximum: tc.max}, After: core.Resource{Current: tc.want, Minimum: tc.min, Maximum: tc.max}}
				if !reflect.DeepEqual(receipt.Events[0].Resource, &want) || receipt.Events[0].Kind != core.ResourceSpent {
					t.Fatal(receipt)
				}
				costFacts(t, w, tc.want, 6, 10, 1)
				costReplay(t, b, w)
			})
		}
	}
	for _, tc := range []struct {
		name                       string
		focus, hp, cost            int64
		self                       bool
		wantFocus, wantHP, wantDef int64
	}{
		{"exact aid", 2, 6, 2, false, 0, 10, 13}, {"clamped benefit", 3, 19, 2, false, 1, 20, 13},
		{"cost three", 3, 6, 3, false, 0, 10, 13}, {"self", 3, 6, 2, true, 1, 12, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := costFixture(t)
			costBalance(&b, tc.focus, 0, 5)
			costCharacter(&b, "char:lyra").Resources["hp"] = core.Resource{Current: tc.hp, Maximum: 20}
			*b.AbilityDefinitions[0].Steps[0].Spend.Amount.Literal.Integer = tc.cost
			w := world(t, b)
			op := steady("guard:m7:one")
			target := core.CharacterID("char:lyra")
			if tc.self {
				target = "char:orin"
				*op.Invoke.Arguments[1].Value.Character = target
			}
			commitC(t, w, costRequest(w, "variant", op))
			s, _ := w.CharacterStatus(target)
			if s.Resources["hp"].Current != tc.wantHP {
				t.Fatal(s)
			}
			stat(t, w, target, "defense", tc.wantDef)
			p, _ := w.CharacterStatus("char:orin")
			if p.Resources["focus"].Current != tc.wantFocus {
				t.Fatal(p)
			}
			costReplay(t, b, w)
		})
	}
}

func TestResourceSpendOrderedCandidateAndLateRollback(t *testing.T) {
	for _, invoked := range []bool{false, true} {
		for _, balance := range []int64{3, 4} {
			t.Run(fmt.Sprintf("pair/%t/%d", invoked, balance), func(t *testing.T) {
				b := costFixture(t)
				costBalance(&b, balance, 0, 5)
				w := world(t, b)
				o := spend(2)
				if invoked {
					o = pay(2)
				}
				r := costRequest(w, "pair", o, o)
				if balance == 3 {
					costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
					r.Operations = []core.Operation{spend(2)}
					commitC(t, w, r)
					costFacts(t, w, 1, 6, 10, 1)
					return
				}
				got := commitC(t, w, r)
				n := 2
				if invoked {
					n = 4
				}
				if len(got.Events) != n {
					t.Fatal(got)
				}
				costFacts(t, w, 0, 6, 10, 1)
				costReplay(t, b, w)
			})
		}
	}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("funding/%t", reverse), func(t *testing.T) {
			b := costFixture(t)
			costBalance(&b, 1, 0, 5)
			w := world(t, b)
			ops := []core.Operation{focusHeal(2), spend(2)}
			if reverse {
				ops[0], ops[1] = ops[1], ops[0]
			}
			r := costRequest(w, "funding", ops...)
			if reverse {
				costReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
				return
			}
			got := commitC(t, w, r)
			if len(got.Events) != 2 || got.Events[0].Kind != core.HealingApplied || got.Events[1].Resource.Before.Current != 3 {
				t.Fatal(got)
			}
			costFacts(t, w, 1, 6, 10, 1)
			costReplay(t, b, w)
		})
	}
	t.Run("late unique", func(t *testing.T) {
		b := costFixture(t)
		b.Conditions = []core.ConditionInstance{{ID: "blocker", Type: "condition:guarded", Target: "char:lyra"}}
		w := world(t, b)
		r := costRequest(w, "failed", steady("guard:m7:one"))
		err := costReject(t, w, r, "E_RULE")
		var ae *core.AbilityError
		if !errors.As(err, &ae) || ae.Step != 2 {
			t.Fatal("did not reach late step", err)
		}
		costFacts(t, w, 3, 6, 13, 0)
		commitC(t, w, costRequest(w, "unblock", removeC("blocker", "char:lyra")))
		r.ExpectedRevision = w.Revision()
		commitC(t, w, r)
		costFacts(t, w, 1, 10, 13, 2)
		if !reflect.DeepEqual(w.Snapshot().RetiredConditionIDs, []core.ConditionInstanceID{"blocker"}) {
			t.Fatal("lifetime reservations")
		}
		costReplay(t, b, w)
	})
	t.Run("late outer", func(t *testing.T) {
		b := costFixture(t)
		w := world(t, b)
		r := costRequest(w, "failed", steady("guard:m7:one"), core.Operation{Transfer: &core.Transfer{Item: "item:token", From: "char:tess", To: "char:lyra"}})
		for _, o := range r.Operations {
			if err := b.CheckOperationStatic(o); err != nil {
				t.Fatal("must pass static validation", err)
			}
		}
		costReject(t, w, r, "E_OWNER")
		r.Operations[1].Transfer.From = "char:orin"
		commitC(t, w, r)
		costFacts(t, w, 1, 10, 13, 1)
		costReplay(t, b, w)
	})
}

func costJourney(t testing.TB) (core.Blueprint, *core.World) {
	t.Helper()
	b := costFixture(t)
	w := world(t, b)
	rows := []struct {
		id                 string
		op                 core.Operation
		focus, hp, defense int64
		events             int
		active             []core.ConditionInstanceID
		retired            []core.ConditionInstanceID
	}{
		{"R1", steady("guard:m7:one"), 1, 10, 13, 4, []core.ConditionInstanceID{"guard:m7:one"}, nil},
		{"R2", removeC("guard:m7:one", "char:lyra"), 1, 10, 10, 5, nil, []core.ConditionInstanceID{"guard:m7:one"}},
		{"R3", focusHeal(3), 4, 10, 10, 6, nil, []core.ConditionInstanceID{"guard:m7:one"}},
		{"R4", steady("guard:m7:two"), 2, 14, 13, 10, []core.ConditionInstanceID{"guard:m7:two"}, []core.ConditionInstanceID{"guard:m7:one"}},
		{"R5", spend(2), 0, 14, 13, 11, []core.ConditionInstanceID{"guard:m7:two"}, []core.ConditionInstanceID{"guard:m7:one"}},
		{"R6", removeC("guard:m7:two", "char:lyra"), 0, 14, 10, 12, nil, []core.ConditionInstanceID{"guard:m7:one", "guard:m7:two"}},
	}
	count := 0
	for i, row := range rows {
		got := commitC(t, w, costRequest(w, row.id, row.op))
		count += len(got.Events)
		costFacts(t, w, row.focus, row.hp, row.defense, core.Revision(i+1))
		if count != row.events {
			t.Fatal("journey event oracle", count, row.events)
		}
		s := w.Snapshot()
		var active []core.ConditionInstanceID
		for _, c := range s.Conditions {
			active = append(active, c.ID)
		}
		if !reflect.DeepEqual(active, row.active) || !reflect.DeepEqual(s.RetiredConditionIDs, row.retired) {
			t.Fatal("journey lifetimes", s.Conditions, s.RetiredConditionIDs)
		}
	}
	return b, w
}

func TestResourceSpendJourneyReceiptsAndIntent(t *testing.T) {
	b, w := costJourney(t)
	rebuilt := costReplay(t, b, w)
	for _, live := range []*core.World{w, rebuilt} {
		before := contractBytes(t, live.Snapshot())
		h := live.History()
		for _, receipt := range h {
			contractEqual(t, contractBytes(t, receipt), commitC(t, live, receipt.Request))
		}
		contractEqual(t, before, live.Snapshot())
		contractEqual(t, contractBytes(t, h), live.History())
		for _, variant := range []string{"payer", "ability", "inline"} {
			r := live.History()[0].Request
			switch variant {
			case "payer":
				*r.Operations[0].Invoke.Arguments[1].Value.Character = "char:tess"
			case "ability":
				r.Operations[0].Invoke.Ability = "ability:pay"
			case "inline":
				r.Operations = []core.Operation{spend(2), {Heal: &core.ResourceOp{Character: "char:lyra", Resource: "hp", Amount: 4}}, {ApplyCondition: &core.ApplyCondition{Instance: "guard:m7:one", Type: "condition:guarded", Target: "char:lyra", Source: &core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}}}}
			}
			costReject(t, live, r, "E_REQUEST_REUSE")
		}
		r := costRequest(live, "fresh", steady("fresh"))
		r.ExpectedRevision = 0
		costReject(t, live, r, "E_STALE")
		r.ExpectedRevision = live.Revision()
		costReject(t, live, r, "E_INSUFFICIENT_RESOURCE")
	}
	for _, invoked := range []bool{false, true} {
		live := world(t, b)
		op := spend(2)
		if invoked {
			op = pay(2)
		}
		r := costRequest(live, "identity", op)
		got := commitC(t, live, r)
		contractEqual(t, contractBytes(t, got), commitC(t, live, r))
		if invoked {
			*r.Operations[0].Invoke.Arguments[1].Value.Integer = 1
		} else {
			r.Operations[0] = core.Operation{Damage: &core.ResourceOp{Character: "char:orin", Resource: "focus", Amount: 2}}
		}
		costReject(t, live, r, "E_REQUEST_REUSE")
	}
}
