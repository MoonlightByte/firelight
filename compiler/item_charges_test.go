package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const chargesSource = catalogSource + `item type "wand" named "Wand" { kind "wand"; magical true; charges 7; }
item type "cube" named "Cube" { kind "wondrous"; charges 36; initial 10; }
`

func TestChargesSourceAndActions(t *testing.T) {
	b, ds := CompileWorld("world", chargesSource+`item "w" from "wand" { owner "a"; custody character "a"; charges 3 of 7; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if d := b.ItemDefinitions; len(d) != 3 || !reflect.DeepEqual(d[1].Charges, &core.ChargeRule{Max: 7}) || d[2].Charges.Max != 36 || *d[2].Charges.Initial != 10 {
		t.Fatal(d)
	}
	if c := b.Items[0].Charges; c == nil || *c != (core.ItemCharges{Current: 3, Max: 7}) {
		t.Fatal(b.Items[0])
	}
	source := `create item "n" from "cube" { owner "a"; charges 4; }; expend 2 charges of "w" by "a"; expend 1 charges of "n" by "a";`
	ops, ds := CompileActions("actions", source, b)
	if len(ds) != 0 || len(ops) != 3 {
		t.Fatal(ops, ds)
	}
	if c := ops[0].CreateItem.Charges; c == nil || *c != (core.ItemCharges{Current: 4}) {
		t.Fatal("creation intent", ops[0].CreateItem)
	}
	if !reflect.DeepEqual(ops[1].ExpendCharges, &core.ExpendCharges{Item: "w", Owner: "a", Amount: 2}) {
		t.Fatal(ops[1])
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Execute(core.Request{ID: "r", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}); err != nil {
		t.Fatal(err)
	}
	v, err := w.ItemChargesView([]core.ItemID{"w", "n"})
	if err != nil || v[0].Current != 1 || v[1].Current != 3 || v[1].Max != 36 {
		t.Fatal(v, err)
	}
	again, ds := CompileRetry("retry", source, w.Snapshot().Blueprint)
	if len(ds) != 0 || !reflect.DeepEqual(ops, again) {
		t.Fatal("post-state retry", ds)
	}
}

func TestChargesDiagnostics(t *testing.T) {
	for _, tc := range []struct{ source, code, at string }{
		{`item type "d" named "D" { kind "x"; initial 3; }`, "E_CATALOG", "3"},
		{`item type "d" named "D" { kind "x"; charges 0; }`, "E_CATALOG", "0"},
		{`item type "d" named "D" { kind "x"; charges 1001; }`, "E_CATALOG", "1001"},
		{`item type "d" named "D" { kind "x"; charges 5; initial 9; }`, "E_CATALOG", "9"},
		{`item type "d" named "D" { kind "x"; charges 5; charges 6; }`, "E_DUPLICATE", "charges 6"},
		{`item "i" from "wand" { owner "a"; charges 8; }`, "E_CHARGES", "8"},
		{`item "i" from "wand" { owner "a"; charges 2 of 6; }`, "E_CHARGES", "6"},
		{`item "i" from "torch" { owner "a"; charges 1; }`, "E_CHARGES", "1"},
		{`item "i" named "I" { owner "a"; charges 1; }`, "E_CHARGES", "1"},
		{`item "i" from "wand" { owner "a"; charges 1; charges 2; }`, "E_DUPLICATE", "charges 2"},
		{`item "i" from "wand" { owner "a"; quantity 2; }`, "E_CHARGES", "2"},
	} {
		_, ds := CompileWorld("bad", chargesSource+tc.source)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatal(tc, ds)
		}
	}
	b, ds := CompileWorld("good", chargesSource+`item "w" from "wand" { owner "a"; charges 3; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, code, at string }{
		{`create item "n" from "wand" { owner "a"; charges 9; };`, "E_CHARGES", "9"},
		{`create item "n" from "wand" { owner "a"; charges 2 of 5; };`, "E_CHARGES", "5"},
		{`create item "n" from "wand" { owner "a"; quantity 2; };`, "E_CHARGES", "2"},
		{`expend 0 charges of "w" by "a";`, "E_AMOUNT", "0"},
		{`expend 1 of "w" by "a";`, "E_PARSE", "of"},
		{`expend 1 charges of "w" by "missing";`, "E_REFERENCE", `"missing"`},
	} {
		_, ds := CompileActions("bad", tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatal(tc, ds)
		}
	}
}

const rechargeSource = chargesSource + `item type "rod" named "Rod" { kind "rod"; charges 10; recharge every 86400 at 21600 dice 1 sides 6 plus 1; exhausted roll 20 destroy 1 regain 20 dice 1 sides 12 plus 1; }
item type "lamp" named "Lamp" { kind "lamp"; charges 3; recharge every 100 all; }
item type "ring" named "Ring" { kind "ring"; charges 10; recharge every 100 at 5 by 2; }
item type "blade" named "Blade" { kind "weapon"; charges 3; initial dice 1 sides 3; exhausted destroy; }
`

func TestRechargeSource(t *testing.T) {
	b, ds := CompileWorld("world", rechargeSource+`item "r" from "rod" { owner "a"; charges 3 of 10 since 43320647400 seed 18446744073709551615 89012345; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	rules := map[core.ItemDefinitionID]*core.ChargeRule{}
	for _, d := range b.ItemDefinitions {
		rules[d.ID] = d.Charges
	}
	for id, want := range map[core.ItemDefinitionID]core.ChargeRule{
		"rod": {Max: 10, Recharge: &core.ChargeRecharge{Every: 86400, At: 21600, Dice: &core.ChargeDice{Count: 1, Sides: 6, Plus: 1}},
			Exhausted: &core.ChargeExhausted{Roll: 20, DestroyAtMost: 1, RegainAtLeast: 20, Regain: &core.ChargeDice{Count: 1, Sides: 12, Plus: 1}}},
		"lamp":  {Max: 3, Recharge: &core.ChargeRecharge{Every: 100, All: true}},
		"ring":  {Max: 10, Recharge: &core.ChargeRecharge{Every: 100, At: 5, By: 2}},
		"blade": {Max: 3, InitialDice: &core.ChargeDice{Count: 1, Sides: 3}, Exhausted: &core.ChargeExhausted{Destroy: true}},
	} {
		if !reflect.DeepEqual(rules[id], &want) {
			t.Fatalf("%s: %+v", id, rules[id])
		}
	}
	since := uint64(43320647400)
	if c := b.Items[0].Charges; !reflect.DeepEqual(c, &core.ItemCharges{Current: 3, Max: 10, AsOf: &since, Seed: &[2]uint64{18446744073709551615, 89012345}}) {
		t.Fatal(c)
	}
	ops, ds := CompileActions("actions", `create item "n" from "blade" { owner "a"; }; expend 1 charges of "r" by "a";`, b)
	if len(ds) != 0 || len(ops) != 2 || ops[0].CreateItem.Charges != nil {
		t.Fatal("a rolling item's creation leaves the seed to the engine", ops, ds)
	}
}

func TestRechargeDiagnostics(t *testing.T) {
	for _, tc := range []struct{ source, code, at string }{
		{`item type "d" named "D" { kind "x"; recharge every 10 all; }`, "E_CATALOG", "recharge"},
		{`item type "d" named "D" { kind "x"; exhausted destroy; }`, "E_CATALOG", "exhausted"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 0 all; }`, "E_CATALOG", "0"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10 at 12 all; }`, "E_CATALOG", "12"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10 by 0; }`, "E_CATALOG", "by"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10 by 1001; }`, "E_CATALOG", "1001"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10 dice 1 sides 1; }`, "E_CATALOG", "1;"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10; }`, "E_PARSE", "; }"},
		{`item type "d" named "D" { kind "x"; charges 5; recharge every 10 all; recharge every 9 all; }`, "E_DUPLICATE", "recharge every 9"},
		{`item type "d" named "D" { kind "x"; charges 5; initial dice 6 sides 4; }`, "E_CATALOG", "6 sides"},
		{`item type "d" named "D" { kind "x"; charges 5; initial dice 1 sides 4 plus 10001; }`, "E_CATALOG", "10001"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted roll 20; }`, "E_CATALOG", "roll"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted roll 1001 destroy 1; }`, "E_CATALOG", "1001"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted roll 20 destroy 21; }`, "E_CATALOG", "21"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted roll 20 destroy 4 regain 3 dice 1 sides 4; }`, "E_CATALOG", "3 dice"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted roll 20 regain 20 dice 0 sides 4; }`, "E_CATALOG", "0 sides"},
		{`item type "d" named "D" { kind "x"; charges 5; exhausted maybe; }`, "E_PARSE", "maybe"},
		{`item "i" from "rod" { owner "a"; charges 2; }`, "E_CHARGES", "2"},
		{`item "i" from "blade" { owner "a"; }`, "E_CHARGES", `"blade"`},
		{`item "i" from "wand" { owner "a"; charges 2 seed 1 2; }`, "E_CHARGES", "1 2"},
		{`item "i" from "wand" { owner "a"; charges 2 since 7; }`, "E_CHARGES", "7"},
		{`item "i" from "rod" { owner "a"; charges 2 seed 18446744073709551616 1; }`, "E_INTEGER", "18446744073709551616"},
		{`item "i" from "rod" { owner "a"; charges 2 since -1 seed 1 1; }`, "E_INTEGER", "-1"},
	} {
		_, ds := CompileWorld("bad", rechargeSource+tc.source)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatal(tc, ds)
		}
	}
}
