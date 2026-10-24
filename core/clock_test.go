package core_test

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func clockFixture() core.Blueprint {
	b := gearFixture()
	b.Clock = &core.Clock{Unit: "seconds"}
	b.Equipment.Definitions[1].Modes[0].EquipTicks = 60
	b.Equipment.Definitions[1].Modes[0].UnequipTicks = 60
	b.Equipment.Definitions[2].Modes[0].EquipTicks = 300
	b.Equipment.Definitions[2].Modes[0].UnequipTicks = 60
	for i := range b.Items {
		if b.Items[i].ID == "leather" {
			b.Items[i].Wearer = "a"
			b.Items[i].Equipment.Mode = "worn"
		}
		if b.Items[i].ID == "shield" {
			b.Items[i].Wearer = "a"
			b.Items[i].Equipment.Mode = "held"
		}
	}
	return b
}
func advance(ticks int64) core.Operation {
	return core.Operation{AdvanceTime: &core.AdvanceTime{Ticks: ticks}}
}
func timed(id core.ConditionInstanceID, duration uint64) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: id, Type: "guarded", Target: "a", ClockDuration: &duration}}
}
func TestClockEquipmentCompletionAndExpiry(t *testing.T) {
	w := gearWorld(t, clockFixture())
	gearExecute(t, w, "guard", timed("guard", 120))
	gearDefense(t, w, "a", 18)
	old, _ := w.CharacterStatus("a")
	var historicalLifetime *core.ClockLifetime
	for _, c := range old.Conditions {
		if c.Instance == "guard" {
			historicalLifetime = c.ClockLifetime
		}
	}
	if historicalLifetime == nil || historicalLifetime.AppliedTick != 0 || historicalLifetime.ExpiresTick != 120 {
		t.Fatal("status lost relevant lifetime")
	}
	r := gearExecute(t, w, "change", unwear("leather"), wear("chain", "a", ""))
	gearDefense(t, w, "a", 17)
	want := []core.EventKind{core.TimeAdvanced, core.ItemUnequipped, core.ConditionRemoved, core.TimeAdvanced, core.ItemEquipped}
	if len(r.Events) != len(want) {
		t.Fatalf("trace %+v", r.Events)
	}
	for i, k := range want {
		if r.Events[i].Kind != k {
			t.Fatalf("event%d got%s want%s", i, r.Events[i].Kind, k)
		}
	}
	if r.Events[0].Time.Before != 0 || r.Events[0].Time.After != 60 || r.Events[3].Time.Before != 60 || r.Events[3].Time.After != 360 || r.Events[2].Condition.ClockExpiration.Deadline != 120 {
		t.Fatal("incorrect timing trace")
	}
	if r.Events[2].Condition.BeforeEffects[0].Amount != 3 || r.Events[2].Condition.Before.ClockLifetime.ExpiresTick != 120 {
		t.Fatal("lost expiry provenance")
	}
	now, _ := w.CharacterStatus("a")
	if now.Clock.Tick != 360 || old.Clock.Tick != 0 {
		t.Fatal("freshness")
	}
	historicalLifetime.ExpiresTick = 999
	if r.Events[2].Condition.Before.ClockLifetime.ExpiresTick != 120 {
		t.Fatal("status aliases receipt")
	}
	gearExecute(t, w, "off-shield", unwear("shield"))
	gearDefense(t, w, "a", 15)
	gearReject(t, w, "reused-expired", timed("guard", 10))
	if !reflect.DeepEqual(w.Snapshot().Characters, clockFixture().Characters) || !reflect.DeepEqual(w.Snapshot().Relationships, clockFixture().Relationships) {
		t.Fatal("unrelated canonical facts changed")
	}
}
func TestClockBoundarySortedExpirationAndTransferPolicy(t *testing.T) {
	for _, policy := range []core.TransferPolicy{core.Unequips, core.KeepsWorn} {
		t.Run(string(policy), func(t *testing.T) {
			b := clockFixture()
			b.Rules.Transfer = policy
			w := gearWorld(t, b)
			gearExecute(t, w, "conditions", timed("z", 60), timed("a-timed", 60))
			gearExecute(t, w, "59", advance(59))
			gearDefense(t, w, "a", 21)
			r := gearExecute(t, w, "60", advance(1))
			if len(r.Events) != 3 || r.Events[0].Condition.Before.ID != "a-timed" || r.Events[1].Condition.Before.ID != "z" || r.Events[2].Kind != core.TimeAdvanced {
				t.Fatalf("expiry order %+v", r.Events)
			}
			gearDefense(t, w, "a", 15)
			r = gearExecute(t, w, "transfer", core.Operation{Transfer: &core.Transfer{Item: "leather", From: "a", To: "b"}})
			want := uint64(120)
			events := 3
			if policy == core.KeepsWorn {
				want = 60
				events = 1
			}
			if w.Snapshot().Clock.Tick != want || len(r.Events) != events || r.Events[len(r.Events)-1].Kind != core.ItemTransferred {
				t.Fatal("transfer timing")
			}
			if policy == core.KeepsWorn {
				gearDefense(t, w, "a", 15)
			} else {
				gearDefense(t, w, "a", 14)
			}
		})
	}
}
func TestClockLateFailureAndWorkRollback(t *testing.T) {
	w := gearWorld(t, clockFixture())
	gearExecute(t, w, "guard", timed("guard", 20))
	gearReject(t, w, "late", unwear("leather"), wear("chain", "a", ""), wear("sword", "a", "two"))
	// Same ID can continue, and expiry is neither visible nor reserved by failure.
	gearExecute(t, w, "late", advance(1))
	gearDefense(t, w, "a", 18)
	for _, n := range []int64{0, -1, math.MinInt64} {
		gearReject(t, w, "nonpositive", advance(n))
	}
	b := clockFixture()
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "z", Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{ExpiresTick: 5}})
	w = gearWorld(t, b)
	ops := make([]core.Operation, 128)
	for i := range ops {
		ops[i] = advance(1)
	}
	gearReject(t, w, "too-much", ops...)
}
func TestClockReplayReceiptsDetachmentAndCodec(t *testing.T) {
	b := clockFixture()
	w := gearWorld(t, b)
	b.Clock.Tick = 500
	r := gearRequest(w, "first", timed("guard", 100), unwear("leather"), wear("chain", "a", ""))
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	original := w.History()[0]
	*r.Operations[0].ApplyCondition.ClockDuration = 1
	receipt.Events[0].Condition.After.ClockLifetime.ExpiresTick = 1
	receipt.Events[3].Condition.ClockExpiration.Deadline = 1
	gearExecute(t, w, "later", unwear("chain"), advance(1))
	cp := w.ExportCheckpoint()
	replayed, e := core.Replay(cp.Genesis, cp.History)
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	info, e := nativesave.Write(&buf, cp, nativesave.SessionState{Campaign: "clock", Next: 3})
	if e != nil || info.Semantics != nativesave.ClockSemanticsVersion {
		t.Fatalf("archive %v %+v", e, info)
	}
	loaded, e := nativesave.Read(bytes.NewReader(buf.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []*core.World{w, replayed, loaded.World} {
		before := v.ExportCheckpoint()
		again, e := v.Execute(original.Request)
		if e != nil || !reflect.DeepEqual(again, original) || !reflect.DeepEqual(before, v.ExportCheckpoint()) {
			t.Fatal("retry changed historical outcome")
		}
		if !reflect.DeepEqual(cp, v.ExportCheckpoint()) {
			t.Fatal("lost canonical clock/expiry/history")
		}
	}
	for _, change := range []func([]core.Batch){
		func(h []core.Batch) { h[0].Events[3].Condition.ClockExpiration.Deadline++ },
		func(h []core.Batch) { h[0].Events[4].Time.After++ },
		func(h []core.Batch) { h[0].Events[0].Condition.After.ClockLifetime.ExpiresTick = 0 },
	} {
		h := w.History()
		change(h)
		if got, e := core.Replay(cp.Genesis, h); e == nil || got != nil {
			t.Fatal("tampered trace accepted")
		}
	}
	view, _ := w.EquipmentView("a")
	view.Clock.Tick = 999
	status, _ := w.CharacterStatus("a")
	status.Clock.Tick = 999
	s := w.Snapshot()
	s.Clock.Tick = 999
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("clock aliases authority")
	}
}
func TestClockInvalidLifetimesAndIntegerBounds(t *testing.T) {
	b := clockFixture()
	b.Clock.Tick = math.MaxUint64
	w := gearWorld(t, b)
	gearReject(t, w, "over", advance(1))
	gearReject(t, w, "duration", timed("overflow", 1))
	for name, change := range map[string]func(*core.Blueprint){
		"missing clock":   func(b *core.Blueprint) { b.Clock = nil },
		"negative timing": func(b *core.Blueprint) { b.Equipment.Definitions[1].Modes[0].EquipTicks = -1 },
		"future application": func(b *core.Blueprint) {
			b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "bad", Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{AppliedTick: 1, ExpiresTick: 2}})
		},
		"expired": func(b *core.Blueprint) {
			b.Clock.Tick = 5
			b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "bad", Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{ExpiresTick: 5}})
		},
		"too many": func(b *core.Blueprint) {
			for i := 0; i < 65; i++ {
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprint(i)), Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{ExpiresTick: 5}})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := clockFixture()
			change(&b)
			if w, e := core.NewWorld(b); e == nil || w != nil {
				t.Fatal("invalid clock accepted")
			}
		})
	}
	b = clockFixture()
	w = gearWorld(t, b)
	op := timed("mixed", 1)
	op.ApplyCondition.Duration = gearPtr(uint64(1))
	gearReject(t, w, "mixed", op)
	// 64 expirations fit the normal budget and all retain their identity reservations.
	for i := 0; i < 64; i++ {
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprint(i)), Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{ExpiresTick: 5}})
	}
	w = gearWorld(t, b)
	r := gearExecute(t, w, "drain", advance(5))
	if len(r.Events) != 65 || len(w.Snapshot().Conditions) != 4 {
		t.Fatal("bounded drain")
	}
}
