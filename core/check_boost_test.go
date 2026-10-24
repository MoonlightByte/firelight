package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

func boostFixture() core.Blueprint {
	b := actorCheckFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "boost-grant", Instances: core.Unique})
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "trained", Type: "boost-grant", Target: "a"})
	b.AbilityDefinitions[0].Steps[1].Check.Boost = &core.CheckBoostPolicy{Condition: "boost-grant", Resource: "focus", Cost: 1, Sides: 10}
	return b
}
func boostInvocation(face int64, use bool, bonus *int64) core.Operation {
	op := battleInvoke("care", "a", "a")
	op.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{face}}}
	op.Invoke.CheckBoost = &core.CheckBoostInput{Step: 1, Use: use, Face: bonus}
	return op
}
func TestCheckBoostFixedOutcomesAndExactRetry(t *testing.T) {
	for _, tc := range []struct {
		name             string
		base             int64
		use              bool
		bonus            *int64
		total, focus, hp int64
	}{
		{"rescued", 6, true, gearPtr(int64(1)), 10, 0, 9},
		{"still failed", 1, true, gearPtr(int64(1)), 5, 1, 5},
		{"declined", 6, false, nil, 9, 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := boostFixture()
			w := gearWorld(t, b)
			op := boostInvocation(tc.base, tc.use, tc.bonus)
			ask := battleInvoke("care", "a", "a")
			ask.Invoke.HumanRolls = []core.AbilityRollInput{{Step: 1, Faces: []int64{tc.base}}}
			before := w.ExportCheckpoint()
			need, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, ask)
			if err != nil || need.Stage != "check_boost" || need.CheckBoost.Base.Total != tc.base+3 || need.CheckBoost.Policy.Cost != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("pending comparison", need, err)
			}
			r := battleExec(t, w, "boosted", "a", op)
			var checked *core.CheckResult
			for _, e := range r.Events {
				if e.Check != nil {
					checked = e.Check
				}
			}
			if checked == nil || checked.Total != tc.total || checked.Boost.BaseTotal != tc.base+3 || checked.Boost.Paid != 1-tc.focus || !reflect.DeepEqual(checked.Boost.Grants, []core.ConditionInstanceID{"trained"}) {
				t.Fatal("fixed Check oracle", r)
			}
			if s := w.Snapshot(); s.Characters[0].Resources["focus"].Current != tc.focus || s.Characters[0].Resources["hp"].Current != tc.hp {
				t.Fatal(s.Characters[0])
			}
			cp := w.ExportCheckpoint()
			snapshot := w.Snapshot()
			snapshot.AbilityDefinitions[0].Steps[1].Check.Boost.Cost = 99
			rebuilt, err := core.Replay(b, w.History())
			if err != nil {
				t.Fatal(err)
			}
			retry, err := rebuilt.Execute(r.Request)
			if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
				t.Fatal("retry/replay", err)
			}
			r.Request.Operations[0].Invoke.CheckBoost.Use = !tc.use
			checked.Boost.Grants[0] = "changed"
			if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
				t.Fatal("boost aliases authority")
			}
			h := w.History()
			for i := range h[0].Events {
				if h[0].Events[i].Check != nil {
					h[0].Events[i].Check.Boost.Paid = 99
				}
			}
			if partial, err := core.Replay(b, h); err == nil || partial != nil {
				t.Fatal("tampered boost accepted")
			}
		})
	}
}

func TestCheckBoostNPCContingencyAndUnavailableOffer(t *testing.T) {
	for _, available := range []bool{false, true} {
		b := boostFixture()
		if available {
			b.Characters[0].Resources["focus"] = core.Resource{Current: 2, Maximum: 3}
		} else {
			b.Conditions = nil
		}
		w := gearWorld(t, b)
		op := boostInvocation(6, false, nil)
		op.Invoke.CheckBoost = nil
		r := battleExec(t, w, "plain-failure", "a", op)
		if r.Events[1].Check.Success || r.Events[1].Check.Boost != nil {
			t.Fatal("unavailable boost altered failure", r)
		}
	}
	for _, threshold := range []int64{0, 100} {
		var unboostedCalls uint64
		for _, use := range []bool{false, true} {
			b := boostFixture()
			for i := range b.Conditions {
				if b.Conditions[i].ID == "trained" {
					b.Conditions[i].Target = "b"
				}
			}
			b.Characters[1].Resources["focus"] = core.Resource{Current: 3, Maximum: 3}
			b.AbilityDefinitions[0].Steps[1].Check.Threshold = core.CheckValue{Literal: gearPtr(threshold)}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", battleBegin())
			endCurrent(t, w, "pass")
			op := battleInvoke("care", "b", "b")
			if use {
				op.Invoke.CheckBoost = &core.CheckBoostInput{Step: 1, Use: true}
			}
			forged := core.CanonicalInvocation(*op.Invoke)
			forged.CheckBoost = &core.CheckBoostInput{Step: 1, Use: true, Face: gearPtr(int64(1))}
			battleReject(t, w, core.Request{ID: "forged", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{{Invoke: &forged}}})
			r := battleExec(t, w, "npc", "b", op)
			check := r.Events[1].Check
			if !use {
				unboostedCalls = check.CallsAfter
			} else if threshold == 0 {
				if check.Boost != nil || check.CallsAfter != unboostedCalls {
					t.Fatal("base success rolled bonus", check)
				}
			} else if check.Boost == nil || check.Boost.Draw < 1 || check.Boost.Draw > 10 || check.Boost.Paid != 0 || check.CallsAfter <= unboostedCalls {
				t.Fatal("NPC bonus not recorded", check)
			}
			if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
				t.Fatal("NPC boost replay", err)
			}
		}
	}
}
func TestCheckBoostAdmissionRollbackAndFrozenComparison(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*core.Blueprint)
		op   core.Operation
	}{
		{"success input", nil, boostInvocation(7, true, gearPtr(int64(1)))},
		{"decline face", nil, boostInvocation(6, false, gearPtr(int64(1)))},
		{"missing face", nil, boostInvocation(6, true, nil)},
		{"outside die", nil, boostInvocation(6, true, gearPtr(int64(11)))},
		{"missing grant", func(b *core.Blueprint) { b.Conditions = nil }, boostInvocation(6, true, gearPtr(int64(1)))},
		{"prefix exhausts", func(b *core.Blueprint) { b.Characters[0].Resources["focus"] = core.Resource{Current: 2, Maximum: 3} }, boostInvocation(6, true, gearPtr(int64(1)))},
		{"late failure", func(b *core.Blueprint) {
			b.AbilityDefinitions[0].Steps[1].Check.Success = append(b.AbilityDefinitions[0].Steps[1].Check.Success, core.AbilityPrimitive{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Parameter: "actor"}, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}})
		}, boostInvocation(6, true, gearPtr(int64(1)))},
		{"overflow", func(b *core.Blueprint) {
			c := b.AbilityDefinitions[0].Steps[1].Check
			c.Modifier = core.CheckValue{Literal: gearPtr(int64(math.MaxInt64 - 2))}
			c.Threshold = core.CheckValue{Literal: gearPtr(int64(math.MaxInt64))}
		}, boostInvocation(1, true, gearPtr(int64(10)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := boostFixture()
			if tc.edit != nil {
				tc.edit(&b)
			}
			w := gearWorld(t, b)
			battleReject(t, w, gearRequest(w, "bad", tc.op))
		})
	}
	b := boostFixture()
	b.Characters[0].Stats["boost-skill"] = 3
	for i := range b.ConditionDefinitions {
		if b.ConditionDefinitions[i].ID == "boost-grant" {
			b.ConditionDefinitions[i].EndsAtMinimum = gearPtr(core.ResourceID("focus"))
			b.ConditionDefinitions[i].Modifiers = []core.ConditionModifier{{ID: "skill", Stat: "boost-skill", Fixed: gearPtr(int64(3))}}
		}
	}
	c := b.AbilityDefinitions[0].Steps[1].Check
	c.Modifier = core.CheckValue{Stat: &core.StatOperand{Character: core.AbilityOperand{Parameter: "actor"}, Stat: "boost-skill"}}
	base := int64(6) // authored base3 + granting Condition3
	c.Threshold = core.CheckValue{Literal: gearPtr(base + 7)}
	w := gearWorld(t, b)
	r := battleExec(t, w, "freeze", "a", boostInvocation(6, true, gearPtr(int64(1))))
	var check *core.CheckResult
	for _, e := range r.Events {
		if e.Check != nil {
			check = e.Check
		}
	}
	if check == nil || check.Modifier != base || check.Total != base+7 || !check.Success {
		t.Fatal("payment recomputed accepted comparison", r)
	}
	for _, v := range w.Snapshot().Conditions {
		if v.ID == "trained" {
			t.Fatal("minimum consequence missing")
		}
	}
}
