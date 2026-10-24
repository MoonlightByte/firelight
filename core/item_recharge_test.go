package core

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

// rechargeFixture adds recharge, dice and last-charge rules to the charges
// fixture. The clock is optional, as in host worlds.
func rechargeFixture(t *testing.T, clock *uint64) Blueprint {
	t.Helper()
	b := chargesFixture(t)
	b.ItemDefinitions = append(b.ItemDefinitions,
		ItemDefinition{ID: "lamp", Name: "Lamp", Kind: "wondrous", Charges: &ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 100, At: 30, All: true}}},
		ItemDefinition{ID: "ring", Name: "Ring", Kind: "ring", Charges: &ChargeRule{Max: 10, Recharge: &ChargeRecharge{Every: 100, By: 2}}},
		ItemDefinition{ID: "rod", Name: "Rod", Kind: "rod", Charges: &ChargeRule{Max: 10, Recharge: &ChargeRecharge{Every: 100, At: 30, Dice: &ChargeDice{Count: 1, Sides: 6, Plus: 1}}}},
		ItemDefinition{ID: "blade", Name: "Blade", Kind: "weapon", Charges: &ChargeRule{Max: 3, InitialDice: &ChargeDice{Count: 1, Sides: 3}}},
		ItemDefinition{ID: "scarab", Name: "Scarab", Kind: "wondrous", Charges: &ChargeRule{Max: 12, Exhausted: &ChargeExhausted{Destroy: true}}},
		ItemDefinition{ID: "magi", Name: "Magi", Kind: "staff", Charges: &ChargeRule{Max: 50,
			Recharge:  &ChargeRecharge{Every: 100, At: 30, Dice: &ChargeDice{Count: 4, Sides: 6, Plus: 2}},
			Exhausted: &ChargeExhausted{Roll: 20, DestroyAtMost: 1, RegainAtLeast: 20, Regain: &ChargeDice{Count: 1, Sides: 12, Plus: 1}}}},
		ItemDefinition{ID: "cask", Name: "Cask", Kind: "wondrous", Charges: &ChargeRule{Max: 5, InitialDice: &ChargeDice{Count: 1, Sides: 20}}},
		ItemDefinition{ID: "talisman", Name: "Talisman", Kind: "wondrous", Charges: &ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20, DestroyAtMost: 1}}},
		ItemDefinition{ID: "orb", Name: "Orb", Kind: "wondrous", Charges: &ChargeRule{Max: 5, Exhausted: &ChargeExhausted{Roll: 20, RegainAtLeast: 2, Regain: &ChargeDice{Count: 1, Sides: 2, Plus: 10}}}})
	if clock != nil {
		b.Clock = &Clock{Unit: "second", Tick: *clock}
	}
	return b
}

func tickPtr(v uint64) *uint64 { return &v }

// oracleRecharge steps one tick at a time and applies each boundary it meets.
func oracleRecharge(r *ChargeRule, seed *[2]uint64, current int64, from, to uint64) int64 {
	rc := r.Recharge
	for t := from + 1; t <= to; t++ {
		if t < rc.At || (t-rc.At)%rc.Every != 0 {
			continue
		}
		switch {
		case rc.All:
			current = r.Max
		case rc.By > 0:
			current = min(r.Max, current+rc.By)
		default:
			if current < r.Max {
				current = min(r.Max, current+rollChargeDice(chargeStream(*seed, chargeRollRecharge, t), *rc.Dice))
			}
		}
	}
	return current
}

func TestRechargeOracleAndBatching(t *testing.T) {
	b := rechargeFixture(t, nil)
	seed := &[2]uint64{11, 12}
	rng := rand.New(rand.NewPCG(1, 2))
	for _, id := range []ItemDefinitionID{"lamp", "ring", "rod", "magi"} {
		r := b.itemDefinition(id).Charges
		for range 300 {
			current := rng.Int64N(r.Max + 1)
			from := rng.Uint64N(800)
			to := from + rng.Uint64N(900)
			want := oracleRecharge(r, seed, current, from, to)
			if got := recharge(r, seed, current, from, to); got != want {
				t.Fatalf("%s from %d to %d at %d: got %d, oracle %d", id, from, to, current, got, want)
			}
			// The lemma: any split of (from, to] gives the same count.
			split, at := current, from
			for at < to {
				next := min(to, at+rng.Uint64N(250))
				split = recharge(r, seed, split, at, next)
				at = next
			}
			if split != want {
				t.Fatalf("%s split result %d, whole %d", id, split, want)
			}
		}
	}
	// Exact boundaries: (from, to] includes to and excludes from.
	ring := b.itemDefinition("ring").Charges
	for _, tc := range []struct {
		from, to uint64
		want     int64
	}{{99, 100, 2}, {100, 199, 0}, {100, 200, 2}, {0, 99, 0}, {0, 300, 6}, {0, 100000, 10}} {
		if got := recharge(ring, nil, 0, tc.from, tc.to); got != tc.want {
			t.Fatal("ring boundary", tc, got)
		}
	}
	lamp := b.itemDefinition("lamp").Charges
	if recharge(lamp, nil, 0, 0, 29) != 0 || recharge(lamp, nil, 0, 29, 30) != 3 || recharge(lamp, nil, 1, 30, 129) != 1 {
		t.Fatal("offset boundary")
	}
	// Overflow: boundaries near the top of uint64 and a huge count.
	if got := recharge(ring, nil, 0, math.MaxUint64-250, math.MaxUint64); got != 6 { // MaxUint64-15, -115, -215
		t.Fatal("top of the clock", got)
	}
	if got := recharge(ring, nil, 9, 0, math.MaxUint64); got != 10 {
		t.Fatal("huge count", got)
	}
	if n := nextRecharge(*ring.Recharge, math.MaxUint64-5); n != nil {
		t.Fatal("next boundary past uint64 must be absent", *n)
	}
	if n := nextRecharge(*lamp.Recharge, math.MaxUint64-5); n != nil {
		t.Fatal("next boundary with an offset past uint64 must be absent", *n)
	}
	if n := nextRecharge(*lamp.Recharge, 0); n == nil || *n != 30 {
		t.Fatal("next boundary before the offset", n)
	}
	if n := nextRecharge(*lamp.Recharge, 30); n == nil || *n != 130 {
		t.Fatal("next boundary after a boundary", n)
	}
}

func TestChargeRollVectors(t *testing.T) {
	// Pinned vectors for ChargeRollVersion. A change here changes every
	// campaign's future rolls and needs a new profile.
	if ChargeRollVersion != "go1.27-chacha8-uint64n-v1" {
		t.Fatal(ChargeRollVersion)
	}
	got := []int64{}
	for _, k := range []struct{ purpose, tick uint64 }{{chargeRollInitial, 0}, {chargeRollRecharge, 130}, {chargeRollRecharge, 230}, {chargeRollExhausted, 130}, {chargeRollRegain, 130}} {
		got = append(got, rollChargeDice(chargeStream([2]uint64{1, 2}, k.purpose, k.tick), ChargeDice{Count: 3, Sides: 20}))
	}
	want := []int64{34, 30, 23, 51, 34}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("charge roll vectors", got)
	}
	// Seeds, purposes and ticks each separate the streams.
	same := func(a, b *rand.Rand) bool {
		for range 8 {
			if a.Uint64() != b.Uint64() {
				return false
			}
		}
		return true
	}
	base := chargeStream([2]uint64{1, 2}, chargeRollRecharge, 130)
	for _, other := range []*rand.Rand{
		chargeStream([2]uint64{1, 3}, chargeRollRecharge, 130),
		chargeStream([2]uint64{2, 2}, chargeRollRecharge, 130),
		chargeStream([2]uint64{1, 2}, chargeRollExhausted, 130),
		chargeStream([2]uint64{1, 2}, chargeRollRecharge, 131),
	} {
		if same(chargeStream([2]uint64{1, 2}, chargeRollRecharge, 130), other) {
			t.Fatal("streams not separated")
		}
	}
	if !same(base, chargeStream([2]uint64{1, 2}, chargeRollRecharge, 130)) {
		t.Fatal("a stream must repeat for the same key")
	}
}

func TestRechargeTimeTable(t *testing.T) {
	b := rechargeFixture(t, tickPtr(1000))
	b.Items = append(b.Items,
		Item{ID: "on-time", From: "ring", Owner: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(750)}},
		Item{ID: "ahead", From: "ring", Owner: "a", Charges: &ItemCharges{Current: 2, AsOf: tickPtr(1200)}},
		Item{ID: "loose", From: "ring", Owner: "a", Charges: &ItemCharges{Current: 2}},
		Item{ID: "full", From: "ring", Owner: "a", Charges: &ItemCharges{Current: 9, AsOf: tickPtr(950)}})
	w := catalogWorld(t, b)
	if v, err := w.ItemChargesView([]ItemID{"full"}); err != nil || v[0].Current != 10 || v[0].NextRecharge != nil {
		t.Fatal("a full item shows no next recharge", v, err)
	}
	view, err := w.ItemChargesView([]ItemID{"on-time", "ahead", "loose", "held"})
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range []ItemChargesProjection{
		{Item: "on-time", Current: 7, Available: 7, AsOf: tickPtr(1000), Clock: ChargeClockCurrent, NextRecharge: tickPtr(1100)},
		{Item: "ahead", Current: 2, Available: 2, AsOf: tickPtr(1200), Clock: ChargeClockBehind, NextRecharge: tickPtr(1300)},
		{Item: "loose", Current: 2, Available: 2, AsOf: tickPtr(1000), Clock: ChargeClockUnanchored, NextRecharge: tickPtr(1100)},
		{Item: "held", Current: 3, Available: 3},
	} {
		v := view[n]
		v.Revision, v.Owner, v.Max = 0, "", 0
		if !reflect.DeepEqual(v, want) {
			t.Fatalf("view row %d: %+v", n, v)
		}
	}
	r, err := w.Execute(inventoryRequest("spend", 0, expend("on-time", "a", 1), expend("ahead", "a", 1), expend("loose", "a", 1)))
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range []ChargeExpenditure{
		{Item: "on-time", Owner: "a", Requested: 1, Before: 7, After: 6, Clock: ChargeClockCurrent, AsOf: tickPtr(1000)},
		{Item: "ahead", Owner: "a", Requested: 1, Before: 2, After: 1, Clock: ChargeClockBehind, AsOf: tickPtr(1200)},
		{Item: "loose", Owner: "a", Requested: 1, Before: 2, After: 1, Clock: ChargeClockUnanchored, AsOf: tickPtr(1000)},
	} {
		if !reflect.DeepEqual(*r.Events[n].Charges, want) {
			t.Fatalf("event %d: %+v", n, *r.Events[n].Charges)
		}
	}
	for id, want := range map[ItemID]ItemCharges{"on-time": {Current: 6, Max: 10, AsOf: tickPtr(1000)}, "ahead": {Current: 1, Max: 10, AsOf: tickPtr(1200)}, "loose": {Current: 1, Max: 10, AsOf: tickPtr(1000)}} {
		if got := chargesOf(t, w, id); !reflect.DeepEqual(got, want) {
			t.Fatal("stored pair", id, got)
		}
	}
	// Time advanced earlier in the same request is credited; advancing time
	// itself does no charge work and emits no charge event.
	r, err = w.Execute(inventoryRequest("later", 1, Operation{AdvanceTime: &AdvanceTime{Ticks: 200}}, expend("on-time", "a", 10)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Events) != 2 || r.Events[0].Kind != TimeAdvanced || r.Events[1].Charges.Before != 10 || *r.Events[1].Charges.AsOf != 1200 {
		t.Fatal("advance then spend", r.Events)
	}
	replayed, err := Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	h := w.History()
	h[1].Events[1].Charges.Before = 9
	if _, err := Replay(b, h); err == nil {
		t.Fatal("tampered recharge replayed")
	}

	// No clock: recharging items are inert in views and refuse to spend;
	// other charged items still spend.
	free := rechargeFixture(t, nil)
	free.Items = append(free.Items, Item{ID: "on-time", From: "ring", Owner: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(750)}})
	fw := catalogWorld(t, free)
	if v, err := fw.ItemChargesView([]ItemID{"on-time"}); err != nil || v[0].Current != 1 || v[0].Clock != ChargeClockAbsent || *v[0].AsOf != 750 || *v[0].NextRecharge != 800 {
		t.Fatal("clockless view", v, err)
	}
	_, err = fw.Execute(inventoryRequest("spend", 0, expend("on-time", "a", 1)))
	chargesFaultIs(t, err, Fault{Code: "E_CHARGES", Subject: "on-time", Field: "clock", Expected: "world clock", Actual: "absent"})
	if _, err := fw.Execute(inventoryRequest("spend", 0, expend("held", "a", 1))); err != nil {
		t.Fatal("an item without recharge spends without a clock", err)
	}
}

func TestChargeSeedsAndCreation(t *testing.T) {
	b := rechargeFixture(t, tickPtr(500))
	b.Dice = &Dice{Seed: [2]uint64{7, 9}}
	w := catalogWorld(t, b)
	create := func(id ItemID, from ItemDefinitionID) Operation {
		return Operation{CreateItem: &Item{ID: id, From: from, Owner: "a"}}
	}
	request := inventoryRequest("loot", 0, create("blade-1", "blade"), create("rod-1", "rod"), create("lamp-1", "lamp"))
	r, err := w.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	blade, rod, lamp := chargesOf(t, w, "blade-1"), chargesOf(t, w, "rod-1"), chargesOf(t, w, "lamp-1")
	if blade.Seed == nil || rod.Seed == nil || *blade.Seed == *rod.Seed || lamp.Seed != nil {
		t.Fatal("seeds", blade, rod, lamp)
	}
	if blade.AsOf != nil || rod.AsOf == nil || *rod.AsOf != 500 || *lamp.AsOf != 500 || rod.Current != 10 || lamp.Current != 3 {
		t.Fatal("anchors and counts", blade, rod, lamp)
	}
	if blade.Current != min(3, rollChargeDice(chargeStream(*blade.Seed, chargeRollInitial, 0), ChargeDice{Count: 1, Sides: 3})) {
		t.Fatal("initial roll", blade)
	}
	if r.Events[0].ItemIntent.Charges != nil {
		t.Fatal("creation intent expanded", r.Events[0].ItemIntent)
	}
	// The same genesis and request draw the same seeds; retry returns the receipt.
	again := catalogWorld(t, b)
	if r2, err := again.Execute(request); err != nil || !reflect.DeepEqual(r2, r) {
		t.Fatal("seed draw not deterministic", err)
	}
	if r3, err := w.Execute(request); err != nil || !reflect.DeepEqual(r3, r) {
		t.Fatal("retry", err)
	}
	replayed, err := Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	// An initial roll above the maximum is capped; a last-charge roll alone
	// makes an item rolling, so creation draws its seed.
	if _, err := w.Execute(inventoryRequest("more", 1, create("cask-1", "cask"), create("talisman-1", "talisman"))); err != nil {
		t.Fatal(err)
	}
	cask, talisman := chargesOf(t, w, "cask-1"), chargesOf(t, w, "talisman-1")
	if roll := rollChargeDice(chargeStream(*cask.Seed, chargeRollInitial, 0), ChargeDice{Count: 1, Sides: 20}); cask.Current != min(5, roll) || talisman.Seed == nil || talisman.Current != 3 {
		t.Fatal("cask and talisman", cask, talisman)
	}
	caskRule := b.itemDefinition("cask").Charges
	for s := uint64(1); ; s++ {
		seed := [2]uint64{s, 0}
		if rollChargeDice(chargeStream(seed, chargeRollInitial, 0), ChargeDice{Count: 1, Sides: 20}) > 5 {
			if got := initialCharges(caskRule, &seed); got != 5 {
				t.Fatal("initial roll above the maximum must cap", got)
			}
			break
		}
	}
	// A stated seed and count are kept and draw nothing.
	diceBefore := w.Snapshot().Dice.State
	stated := Operation{CreateItem: &Item{ID: "rod-2", From: "rod", Owner: "a", Charges: &ItemCharges{Current: 4, Seed: &[2]uint64{5, 6}, AsOf: tickPtr(400)}}}
	if _, err := w.Execute(inventoryRequest("given", 2, stated)); err != nil {
		t.Fatal(err)
	}
	if c := chargesOf(t, w, "rod-2"); !reflect.DeepEqual(c, ItemCharges{Current: 4, Max: 10, AsOf: tickPtr(400), Seed: &[2]uint64{5, 6}}) || !reflect.DeepEqual(w.Snapshot().Dice.State, diceBefore) {
		t.Fatal("stated seed", c)
	}
	// Without a random source a rolling item cannot be created.
	plain := catalogWorld(t, rechargeFixture(t, tickPtr(500)))
	_, err = plain.Execute(inventoryRequest("loot", 0, create("rod-1", "rod")))
	chargesFaultIs(t, err, Fault{Code: "E_CHARGES", Subject: "rod-1", Field: "charges.seed", Expected: "world random source", Actual: "absent"})
	if _, err := plain.Execute(inventoryRequest("loot", 0, create("lamp-1", "lamp"))); err != nil {
		t.Fatal("an item without dice needs no random source", err)
	}
	// Without a clock a recharging item is created unanchored.
	clockless := rechargeFixture(t, nil)
	clockless.Dice = &Dice{Seed: [2]uint64{7, 9}}
	cw := catalogWorld(t, clockless)
	if _, err := cw.Execute(inventoryRequest("loot", 0, create("rod-1", "rod"))); err != nil || chargesOf(t, cw, "rod-1").AsOf != nil {
		t.Fatal("clockless creation", err)
	}

	for _, tc := range []struct {
		name string
		item Item
		want Fault
	}{
		{"declared without seed", Item{ID: "x", From: "rod", Owner: "a", Charges: &ItemCharges{Current: 1}},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges.seed", Expected: "item seed", Actual: "absent"}},
		{"derived without seed", Item{ID: "x", From: "blade", Owner: "a"},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges.seed", Expected: "declared count and item seed", Actual: "absent"}},
		{"last-charge roll without seed", Item{ID: "x", From: "talisman", Owner: "a", Charges: &ItemCharges{Current: 1}},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges.seed", Expected: "item seed", Actual: "absent"}},
		{"seed without dice", Item{ID: "x", From: "wand", Owner: "a", Charges: &ItemCharges{Current: 1, Seed: &[2]uint64{1, 1}}},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges.seed", Expected: "no seed without dice", Actual: "present"}},
		{"anchor without recharge", Item{ID: "x", From: "wand", Owner: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(3)}},
			Fault{Code: "E_CHARGES", Subject: "x", Field: "charges.as_of", Expected: "no anchor without a recharge rule", Actual: "3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := rechargeFixture(t, tickPtr(500))
			bad.Items = append(bad.Items, tc.item)
			_, err := NewWorld(bad)
			chargesFaultIs(t, err, tc.want)
		})
	}
}

func TestChargeRuleRefusals(t *testing.T) {
	dice := func(c, s, p int64) *ChargeDice { return &ChargeDice{Count: c, Sides: s, Plus: p} }
	for _, tc := range []struct {
		name  string
		rule  ChargeRule
		field string
	}{
		{"initial both", ChargeRule{Max: 3, Initial: inventoryInt(1), InitialDice: dice(1, 3, 0)}, "charges.initial"},
		{"initial dice floor", ChargeRule{Max: 3, InitialDice: dice(2, 3, 2)}, "charges.initial"},
		{"dice count", ChargeRule{Max: 3, InitialDice: dice(0, 3, 0)}, "charges.initial.count"},
		{"dice count bound", ChargeRule{Max: 30, InitialDice: dice(21, 3, 0)}, "charges.initial.count"},
		{"dice sides", ChargeRule{Max: 3, InitialDice: dice(1, 1, 0)}, "charges.initial.sides"},
		{"dice sides bound", ChargeRule{Max: 3, InitialDice: dice(1, 1001, 0)}, "charges.initial.sides"},
		{"dice plus", ChargeRule{Max: 3, InitialDice: dice(1, 2, -1)}, "charges.initial.plus"},
		{"every", ChargeRule{Max: 3, Recharge: &ChargeRecharge{All: true}}, "charges.recharge.every"},
		{"at", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10, At: 10, All: true}}, "charges.recharge.at"},
		{"no amount", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10}}, "charges.recharge.amount"},
		{"two amounts", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10, All: true, By: 1}}, "charges.recharge.amount"},
		{"by negative", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10, By: -1}}, "charges.recharge.by"},
		{"by bound", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10, By: 1001}}, "charges.recharge.by"},
		{"recharge dice", ChargeRule{Max: 3, Recharge: &ChargeRecharge{Every: 10, Dice: dice(1, 6, 10001)}}, "charges.recharge.dice.plus"},
		{"exhausted empty", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{}}, "charges.exhausted"},
		{"exhausted both", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Destroy: true, Roll: 20}}, "charges.exhausted"},
		{"destroy with threshold", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Destroy: true, DestroyAtMost: 1}}, "charges.exhausted"},
		{"roll", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 1, DestroyAtMost: 1}}, "charges.exhausted.roll"},
		{"no threshold", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20}}, "charges.exhausted"},
		{"destroy above die", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20, DestroyAtMost: 21}}, "charges.exhausted.destroy"},
		{"regain overlaps", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20, DestroyAtMost: 5, RegainAtLeast: 5, Regain: dice(1, 4, 0)}}, "charges.exhausted.regain"},
		{"regain without dice", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20, RegainAtLeast: 20}}, "charges.exhausted.regain"},
		{"regain dice", ChargeRule{Max: 3, Exhausted: &ChargeExhausted{Roll: 20, RegainAtLeast: 20, Regain: dice(1, 1, 0)}}, "charges.exhausted.regain.sides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := chargesFixture(t)
			rule := tc.rule
			b.ItemDefinitions[2].Charges = &rule
			b.Items = b.Items[:len(b.Items)-2]
			_, err := NewWorld(b)
			var got *Fault
			if !errors.As(err, &got) || got.Code != "E_CATALOG" || got.Subject != "wand" || got.Field != tc.field {
				t.Fatalf("want E_CATALOG %s, got %v", tc.field, err)
			}
		})
	}
}

// exhaustSeed finds a seed whose last-charge face at tick is face.
func exhaustSeed(t *testing.T, tick uint64, face int64) [2]uint64 {
	t.Helper()
	for s := uint64(1); s < 100000; s++ {
		seed := [2]uint64{s, 0}
		if rollChargeDice(chargeStream(seed, chargeRollExhausted, tick), ChargeDice{Count: 1, Sides: 20}) == face {
			return seed
		}
	}
	t.Fatal("no seed for face", face)
	return [2]uint64{}
}

func TestChargeExhaustion(t *testing.T) {
	const now = 1000
	destroy, regain, none := exhaustSeed(t, now, 1), exhaustSeed(t, now, 20), exhaustSeed(t, now, 10)
	b := rechargeFixture(t, tickPtr(now))
	b.Items = append(b.Items,
		Item{ID: "scarab-1", From: "scarab", Owner: "a", Wearer: "a", Charges: &ItemCharges{Current: 2}},
		Item{ID: "doomed", From: "magi", Owner: "a", Wearer: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(now), Seed: &destroy}},
		Item{ID: "lucky", From: "magi", Owner: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(now), Seed: &regain}},
		Item{ID: "quiet", From: "magi", Owner: "a", Charges: &ItemCharges{Current: 1, AsOf: tickPtr(now), Seed: &none}},
		Item{ID: "orb-1", From: "orb", Owner: "a", Charges: &ItemCharges{Current: 1, Seed: &regain}})
	w := catalogWorld(t, b)
	r, err := w.Execute(inventoryRequest("first", 0, expend("scarab-1", "a", 1)))
	if err != nil || len(r.Events) != 1 || r.Events[0].Charges.Exhausted != nil {
		t.Fatal("a spend that leaves charges rolls nothing", r, err)
	}
	r, err = w.Execute(inventoryRequest("last", 1, expend("scarab-1", "a", 1), expend("doomed", "a", 1), expend("lucky", "a", 1), expend("quiet", "a", 1)))
	if err != nil {
		t.Fatal(err)
	}
	e := r.Events
	if len(e) != 6 || e[0].Kind != ItemUnequipped || e[0].Wearing.Reason != "charges exhausted" || !reflect.DeepEqual(e[1].Charges.Exhausted, &ChargeExhaustion{Outcome: ChargesDestroyed}) {
		t.Fatal("unconditional destruction while worn", e)
	}
	if e[2].Kind != ItemUnequipped || !reflect.DeepEqual(e[3].Charges.Exhausted, &ChargeExhaustion{Face: 1, Outcome: ChargesDestroyed}) {
		t.Fatal("rolled destruction while worn", e[2], e[3])
	}
	regained := rollChargeDice(chargeStream(regain, chargeRollRegain, now), ChargeDice{Count: 1, Sides: 12, Plus: 1})
	x := e[4].Charges.Exhausted
	newSeed := *x.Seed
	if x.Face != 20 || x.Outcome != ChargesRegained || x.Regained != regained || e[4].Charges.After != regained || x.Seed == nil || *x.Seed == regain {
		t.Fatal("regain", x, e[4].Charges)
	}
	if !reflect.DeepEqual(e[5].Charges.Exhausted, &ChargeExhaustion{Face: 10, Outcome: ChargesNone}) || e[5].Charges.After != 0 {
		t.Fatal("no outcome", e[5].Charges)
	}
	s := w.Snapshot()
	for _, id := range []ItemID{"scarab-1", "doomed"} {
		i := item(&s.Blueprint, id)
		if itemQuantity(*i) != 0 || i.Wearer != "" || i.Charges.Current != 0 {
			t.Fatal("destroyed unit", *i)
		}
	}
	if c := chargesOf(t, w, "lucky"); c.Current != regained || *c.Seed != *x.Seed {
		t.Fatal("regained state", c)
	}
	// Emptying again at the same tick rolls from the new seed, not the 20.
	r, err = w.Execute(inventoryRequest("again", 2, expend("lucky", "a", regained)))
	if err != nil {
		t.Fatal(err)
	}
	face := rollChargeDice(chargeStream(*x.Seed, chargeRollExhausted, now), ChargeDice{Count: 1, Sides: 20})
	if got := r.Events[len(r.Events)-1].Charges.Exhausted; got == nil || got.Face != face {
		t.Fatal("second emptying must use the replaced seed", got, face)
	}
	_, err = w.Execute(inventoryRequest("ghost", 3, expend("doomed", "a", 1)))
	chargesFaultIs(t, err, Fault{Code: "E_QUANTITY", Subject: "doomed", Field: "quantity", Expected: "one physical unit", Actual: "0"})
	replayed, err := Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	for _, tamper := range []func(h []Batch){
		func(h []Batch) { h[1].Events[5].Charges.Exhausted.Face = 11 },
		func(h []Batch) { h[1].Events[4].Charges.Exhausted.Seed[0]++ },
		func(h []Batch) { h[1].Events[1].Charges.Exhausted.Outcome = ChargesNone },
	} {
		h := w.History()
		tamper(h)
		if _, err := Replay(b, h); err == nil {
			t.Fatal("tampered exhaustion replayed")
		}
	}
	if *w.History()[1].Events[4].Charges.Exhausted.Seed != newSeed || *x.Seed != newSeed {
		t.Fatal("history aliases the exhaustion seed")
	}
	// A regain above the maximum is capped.
	r, err = w.Execute(inventoryRequest("orb", 3, expend("orb-1", "a", 1)))
	if err != nil || r.Events[0].Charges.Exhausted.Outcome != ChargesRegained || r.Events[0].Charges.Exhausted.Regained < 11 || r.Events[0].Charges.After != 5 || chargesOf(t, w, "orb-1").Current != 5 {
		t.Fatal("regain cap", r, err)
	}
}

// BenchmarkRechargeWorstCase measures the most groups one read can need: an
// empty item of maximum 1,000 regaining 1d2 at each of many boundaries.
func BenchmarkRechargeWorstCase(b *testing.B) {
	r := &ChargeRule{Max: MaxItemCharges, Recharge: &ChargeRecharge{Every: 1, Dice: &ChargeDice{Count: 1, Sides: 2}}}
	for range b.N {
		if recharge(r, &[2]uint64{1, 2}, 0, 0, math.MaxUint64) != MaxItemCharges {
			b.Fatal("not full")
		}
	}
}

// BenchmarkRechargeWorstDice measures many dice per group: 20d2 groups.
func BenchmarkRechargeWorstDice(b *testing.B) {
	r := &ChargeRule{Max: MaxItemCharges, Recharge: &ChargeRecharge{Every: 1, Dice: &ChargeDice{Count: 20, Sides: 2}}}
	for range b.N {
		recharge(r, &[2]uint64{1, 2}, 0, 0, math.MaxUint64)
	}
}
