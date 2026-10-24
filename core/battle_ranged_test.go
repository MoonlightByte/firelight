package core_test

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"testing"
)

func rangedFixture() core.Blueprint {
	b := battleFixture()
	b.Battle.Rules.Ranged = &core.BattleRangedPolicy{CloseDistance: 5, IgnoreThreat: []core.ConditionTypeID{"blind", "incapable"}, Concealed: []core.ConditionTypeID{"hidden"}, RecoveryNumerator: 1, RecoveryDenominator: 2, RecoveryTicks: 60}
	for _, id := range []core.ConditionTypeID{"blind", "incapable", "hidden"} {
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: id, Instances: core.Unique})
	}
	bow := core.EquipmentDefinition{ID: "bow", Name: "Bow", DefaultMode: "two", Modes: []core.EquipmentMode{{ID: "two", Slots: []core.SlotUse{{Slot: "hand", Amount: 2}}, Attacks: []core.EquipmentAttack{{ID: "shot", Name: "Shot", Ranged: &core.RangedProfile{Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionOrigin: "arrows"}, ToHit: core.ScalarRecipe{Base: 4}, Damage: core.DamageGrant{Count: 1, Sides: 6, Kind: "piercing", Bonus: core.ScalarRecipe{Base: 2}}}}}}}
	b.Equipment.Definitions = append(b.Equipment.Definitions, bow)
	b.Items[0].Equipment = &core.EquipmentInstance{Definition: "bow", Mode: "two"}
	b.Items = append(b.Items, core.Item{ID: "arrows", Name: "Arrows", Description: "Identified test stock", Owner: "a", Quantity: gearPtr(int64(5)), Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}})
	return b
}
func rangedBegin(distance int64) core.Operation {
	op := battleBegin()
	op.BeginBattle.Participants[1].Position.X = distance / 5
	return op
}
func shot(t *testing.T, w *core.World, faces, damage []int64, finish bool, stock core.ItemID) core.Operation {
	op := battleAttack(t, w, "a", "b", faces, damage, finish)
	op.WeaponAttack.Ammunition = stock
	return op
}
func stockCount(t *testing.T, w *core.World, id core.ItemID) int64 {
	t.Helper()
	for _, i := range w.Snapshot().Items {
		if i.ID == id {
			return *i.Quantity
		}
	}
	t.Fatalf("missing stock %s", id)
	return -1
}
func TestRangedRangeAndAmmunitionOracles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		distance  int64
		condition core.ConditionTypeID
		target    core.CharacterID
		faces     []int64
		cause     string
	}{
		{"normal", 40, "", "", []int64{1}, ""},
		{"long", 85, "", "", []int64{15, 7}, "long_range"},
		{"close", 5, "", "", []int64{1, 20}, "close_opponent"},
		{"blind-opponent", 5, "blind", "b", []int64{1}, ""},
		{"incapable-opponent", 5, "incapable", "b", []int64{1}, ""},
		{"hidden-shooter", 5, "hidden", "a", []int64{1}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := rangedFixture()
			if tc.condition != "" {
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "exception", Type: tc.condition, Target: tc.target})
			}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", rangedBegin(tc.distance))
			before := w.ExportCheckpoint()
			op := shot(t, w, nil, nil, false, "arrows")
			need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
			if err != nil || need.Count != len(tc.faces) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatalf("pending %v %v", need, err)
			}
			r := battleExec(t, w, "shot", "a", shot(t, w, tc.faces, nil, false, "arrows"))
			roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
			if roll.Hit || stockCount(t, w, "arrows") != 4 || w.Snapshot().Battle.State.Random.Calls != before.State.Battle.State.Random.Calls {
				t.Fatal("miss/ammunition/RNG oracle", r)
			}
			if tc.cause != "" && (len(roll.Causes) != 1 || roll.Causes[0].Rule != tc.cause) {
				t.Fatal("cause", roll)
			}
			if tc.cause == "long_range" && roll.Selected != 7 {
				t.Fatal("long-range selection", roll)
			}
			if r.Events[0].Kind != core.ItemConsumed || r.Events[0].Consumption.Item != "arrows" || r.Events[0].Consumption.Requested != 1 {
				t.Fatal("missing exact physical debit", r)
			}
			cp := w.ExportCheckpoint()
			rebuilt, err := core.Replay(cp.Genesis, cp.History)
			if err != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
				t.Fatal("replay", err)
			}
		})
	}
}
func TestRangedRejectsBeforeInputAndRollsBack(t *testing.T) {
	for _, kind := range []string{"too-far", "empty", "unknown-custody", "wrong-owner", "same-name", "late-failure", "melee-ammo"} {
		t.Run(kind, func(t *testing.T) {
			b := rangedFixture()
			distance := int64(40)
			if kind == "too-far" {
				distance = 325
			}
			if kind == "empty" {
				*b.Items[len(b.Items)-1].Quantity = 0
			}
			if kind == "unknown-custody" {
				b.Items[len(b.Items)-1].Custody = nil
			}
			if kind == "wrong-owner" {
				b.Items[len(b.Items)-1].Owner = "b"
			}
			if kind == "same-name" {
				extra := b.Items[len(b.Items)-1]
				extra.ID = "unrelated"
				b.Items = append(b.Items, extra)
			}
			if kind == "melee-ammo" {
				b.Items[0].Equipment = &core.EquipmentInstance{Definition: "longsword", Mode: "one"}
				distance = 5
			}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", rangedBegin(distance))
			ammo := core.ItemID("arrows")
			if kind == "same-name" {
				ammo = "unrelated"
			}
			op := shot(t, w, []int64{1}, nil, false, ammo)
			ops := []core.Operation{op}
			if kind == "late-failure" {
				ops = append(ops, core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "missing", Amount: 1}})
			} else {
				if _, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, op); err == nil {
					t.Fatal("invalid shot reached input")
				}
			}
			battleReject(t, w, gearRequest(w, "rejected", ops...))
		})
	}
}
func completedRanged(t *testing.T, b core.Blueprint, shots int) *core.World {
	b.Battle.Rules.OppositionOutcome = core.StableDown
	b.Characters[1].Resources["hp"] = core.Resource{Current: 3, Maximum: 30}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", rangedBegin(40))
	for i := 0; i < shots; i++ {
		faces := []int64{1}
		var damage []int64
		if i == shots-1 {
			faces = []int64{15}
			damage = []int64{1}
		}
		battleExec(t, w, fmt.Sprint("shot", i), "a", shot(t, w, faces, damage, true, "arrows"))
		if i < shots-1 {
			e := w.Snapshot().Battle.State.Encounters[0]
			battleExec(t, w, fmt.Sprint("pass", i), "b", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: "b"}}})
		}
	}
	return w
}
func TestAmmunitionRecoveryOnceReplayAndReuse(t *testing.T) {
	w := completedRanged(t, rangedFixture(), 5)
	op := core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}}
	before := w.Snapshot().Clock.Tick
	r := battleExec(t, w, "recover", "a", op)
	if stockCount(t, w, "arrows") != 0 || stockCount(t, w, "recovered") != 2 || w.Snapshot().Clock.Tick != before+60 {
		t.Fatal("five shots recover two once", r)
	}
	if len(r.Events) != 3 || r.Events[0].Kind != core.ItemCreated || r.Events[1].Kind != core.TimeAdvanced || r.Events[2].Kind != core.AmmunitionRecovered || r.Events[0].Item.SplitFrom != "" {
		t.Fatal("recovery trace/provenance", r)
	}
	battleReject(t, w, gearRequest(w, "recover-again", op))
	for _, split := range []bool{false, true} {
		cp := w.ExportCheckpoint()
		loaded, err := core.Replay(cp.Genesis, cp.History)
		if err != nil {
			t.Fatal(err)
		}
		stock := core.ItemID("recovered")
		if split {
			battleExec(t, loaded, "split", "a", core.Operation{Split: &core.SplitItem{Item: stock, Created: "portion", Amount: 1, Handler: "a"}})
			stock = "portion"
		}
		battleExec(t, loaded, "revive", "a", core.Operation{Restore: &core.RestoreResource{Character: "b", Resource: "hp"}})
		begin := rangedBegin(40)
		begin.BeginBattle.ID = "second"
		battleExec(t, loaded, "second-begin", "a", begin)
		battleExec(t, loaded, "use-recovered", "a", shot(t, loaded, []int64{1}, nil, false, stock))
		before := loaded.ExportCheckpoint()
		retry, err := loaded.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, loaded.ExportCheckpoint()) {
			t.Fatal("historical recovery retry", err)
		}
		shotReceipt, _ := loaded.Receipt("shot4")
		retry, err = loaded.Execute(shotReceipt.Request)
		if err != nil || !reflect.DeepEqual(retry, shotReceipt) || !reflect.DeepEqual(before, loaded.ExportCheckpoint()) {
			t.Fatal("historical shot retry", err)
		}
	}
	// Every returned nested record is detached, including terminal result intent.
	r.Events[2].AmmunitionRecovery.Intent.Create[0].Created = "mutated"
	r.Events[2].AmmunitionRecovery.After.Stocks[0].Spent = 999
	view := w.Snapshot()
	view.Battle.State.Encounters[0].Ammunition[0].Stocks[0].Recovered = "mutated"
	again, _ := w.Receipt("recover")
	if again.Events[2].AmmunitionRecovery.After.Stocks[0].Spent != 5 || w.Snapshot().Battle.State.Encounters[0].Ammunition[0].Stocks[0].Recovered != "recovered" {
		t.Fatal("alias")
	}
}
func TestAmmunitionZeroAndRecoveryRollback(t *testing.T) {
	w := completedRanged(t, rangedFixture(), 1)
	bad := core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "invented"}}}}
	battleReject(t, w, gearRequest(w, "zero-id", bad))
	before := w.Snapshot()
	r := battleExec(t, w, "zero", "a", core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a"}})
	if len(r.Events) != 2 || len(w.Snapshot().Items) != len(before.Items) || w.Snapshot().Clock.Tick != before.Clock.Tick+60 {
		t.Fatal("zero recovery", r)
	}
	for _, capacity := range []bool{false, true} {
		b := rangedFixture()
		if capacity {
			// A world at the item cap publishes; recovering into a new item is refused.
			for len(b.Items) < core.MaxWorldItems {
				b.Items = append(b.Items, core.Item{ID: core.ItemID(fmt.Sprint(len(b.Items)))})
			}
		} else {
			b.Clock.Tick = math.MaxUint64 - 30
		}
		w := completedRanged(t, b, 3)
		err := battleReject(t, w, gearRequest(w, "failed-recovery", core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}}))
		var f *core.Fault
		if capacity && (!errors.As(err, &f) || f.Field != "capacity") {
			t.Fatal("expected item capacity", err)
		}
	}
}

func TestRangedProjectionLineageRecoveryAndDetachment(t *testing.T) {
	w := completedRanged(t, rangedFixture(), 5)
	view := func() core.EquipmentProjection {
		v, err := w.EquipmentView("a")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := view()
	if len(before.AmmunitionRecovery) != 1 || before.AmmunitionRecovery[0].Stocks[0] != (core.RecoverableStock{Origin: "arrows", Spent: 5, Recoverable: 2}) || before.AmmunitionRecovery[0].Claimed || before.AmmunitionRecovery[0].Ticks != 60 {
		t.Fatal("five arrows yield an unclaimed plan for two", before.AmmunitionRecovery)
	}
	battleExec(t, w, "recover", "a", core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}})
	battleExec(t, w, "split", "a", core.Operation{Split: &core.SplitItem{Item: "recovered", Created: "part", Amount: 1, Handler: "a"}})
	after := view()
	if after.Revision <= before.Revision || !after.AmmunitionRecovery[0].Claimed || after.AmmunitionRecovery[0].Stocks[0].Recoverable != 0 || before.AmmunitionRecovery[0].Stocks[0].Recoverable != 2 {
		t.Fatal("projection freshness/claim", before, after)
	}
	seen := map[core.ItemID]core.ItemID{}
	for _, i := range after.Items {
		if i.AmmunitionOrigin != "" {
			seen[i.Item.ID] = i.AmmunitionOrigin
		}
	}
	if !reflect.DeepEqual(seen, map[core.ItemID]core.ItemID{"arrows": "arrows", "recovered": "arrows", "part": "arrows"}) {
		t.Fatal("recovered and split origins", seen)
	}
	other := view()
	after.Attacks[0].Ranged.Stat = "changed"
	after.AmmunitionRecovery[0].Stocks[0].Spent = 99
	current := view()
	if !reflect.DeepEqual(other, current) {
		t.Fatal("nested projection changed authority or another view")
	}
}

func TestRangedCloseThirdOpponentAndAdvantageCancellation(t *testing.T) {
	for _, advantage := range []bool{false, true} {
		b := rangedFixture()
		third := b.Characters[1]
		third.ID, third.Name = "c", "Third opponent"
		b.Characters = append(b.Characters, third)
		if advantage {
			b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "adv", Instances: core.Unique})
			b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "adv-instance", Type: "adv", Target: "a"})
			b.Battle.Rules.RollPolicies = []core.BattleRollPolicy{{Condition: "adv", On: "attacker", Direction: "advantage"}}
		}
		w := gearWorld(t, b)
		begin := rangedBegin(40)
		begin.BeginBattle.Participants = append(begin.BeginBattle.Participants, core.BattleAdmission{Character: "c", Side: core.OppositionSide, Controller: core.AgentController, Position: core.BattlePosition{X: 0, Y: 1}, TieRank: 2})
		battleExec(t, w, "begin", "a", begin)
		faces := []int64{1, 20}
		if advantage {
			faces = []int64{1}
		}
		r := battleExec(t, w, "shot", "a", shot(t, w, faces, nil, false, "arrows"))
		roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
		found := false
		for _, cause := range roll.Causes {
			if cause.Rule == "close_opponent" && cause.Character == "c" {
				found = true
			}
		}
		if !found || roll.Selected != 1 || roll.Hit || stockCount(t, w, "arrows") != 4 {
			t.Fatal("third opponent/cancellation", roll)
		}
	}
}

func TestAmmunitionRecoveryEligibilityRollback(t *testing.T) {
	for _, kind := range []string{"wrong-actor", "wrong-location", "active-encounter"} {
		t.Run(kind, func(t *testing.T) {
			b := rangedFixture()
			b.Locations = append(b.Locations, core.Location{ID: "elsewhere", Name: "Elsewhere"})
			w := completedRanged(t, b, 3)
			op := core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}}
			switch kind {
			case "wrong-actor":
				op.RecoverAmmunition.Actor = "b"
			case "wrong-location":
				battleExec(t, w, "move", "a", core.Operation{Move: &core.Move{Character: "a", To: "elsewhere"}})
			case "active-encounter":
				battleExec(t, w, "revive", "a", core.Operation{Restore: &core.RestoreResource{Character: "b", Resource: "hp"}})
				begin := rangedBegin(40)
				begin.BeginBattle.ID = "second"
				battleExec(t, w, "begin-second", "a", begin)
			}
			battleReject(t, w, gearRequest(w, "bad-recovery", op))
		})
	}
}

func TestRangedAgentLateFailurePreservesRandomAndStock(t *testing.T) {
	b := rangedFixture()
	b.Characters[0].Stats["initiative"] = 0
	b.Characters[1].Stats["initiative"] = 1000
	b.Items[0].Owner, b.Items[0].Wearer = "b", "b"
	b.Items[1].Wearer = ""
	b.Items[1].Equipment.Mode = ""
	b.Items[len(b.Items)-1].Owner = "b"
	b.Items[len(b.Items)-1].Custody = &core.Ref{Kind: core.CharacterKind, ID: "b"}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", rangedBegin(40))
	op := battleAttack(t, w, "b", "a", nil, nil, true)
	op.WeaponAttack.Ammunition = "arrows"
	r := core.Request{ID: "agent", Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, ExpectedRevision: w.Revision(), Operations: []core.Operation{op, {Spend: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 100}}}}
	before := w.ExportCheckpoint()
	battleReject(t, w, r)
	r.Operations = r.Operations[:1]
	result, err := w.Execute(r)
	if err != nil || stockCount(t, w, "arrows") != 4 || w.Snapshot().Battle.State.Random.Calls <= before.State.Battle.State.Random.Calls {
		t.Fatal("agent continuation after rejected late spend", result, err)
	}
	cp := w.ExportCheckpoint()
	loaded, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(cp, loaded.ExportCheckpoint()) {
		t.Fatal("ranged agent replay", err)
	}
	retry, err := loaded.Execute(r)
	if err != nil || !reflect.DeepEqual(retry, result) || !reflect.DeepEqual(cp, loaded.ExportCheckpoint()) {
		t.Fatal("agent historical retry rolled or consumed again", err)
	}
}

func TestAmmunitionRecoveryMultipleOriginsOneClaim(t *testing.T) {
	b := rangedFixture()
	b.Characters[1].Resources["hp"] = core.Resource{Current: 3, Maximum: 30}
	base := b.Equipment.Definitions[len(b.Equipment.Definitions)-1]
	attack := base.Modes[0].Attacks[0]
	profile := *attack.Ranged
	profile.AmmunitionOrigin = "other-arrows"
	attack.Ranged = &profile
	b.Equipment.Definitions = append(b.Equipment.Definitions, core.EquipmentDefinition{ID: "other-bow", Name: "Other bow", DefaultMode: "two", Modes: []core.EquipmentMode{{ID: "two", Slots: []core.SlotUse{{Slot: "hand", Amount: 2}}, Attacks: []core.EquipmentAttack{attack}}}})
	b.Items = append(b.Items, core.Item{ID: "other-arrows", Name: "Other arrows", Quantity: gearPtr(int64(3)), Owner: "a", Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}}, core.Item{ID: "other-bow", Name: "Other bow", Owner: "a", Equipment: &core.EquipmentInstance{Definition: "other-bow"}})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", rangedBegin(40))
	for i := 0; i < 4; i++ {
		stock := core.ItemID("arrows")
		if i >= 2 {
			stock = "other-arrows"
		}
		if i == 2 {
			battleExec(t, w, "change-bow", "a", core.Operation{Unequip: &core.Unequip{Item: "sword"}}, wear("other-bow", "a", "two"))
		}
		faces := []int64{1}
		var damage []int64
		if i == 3 {
			faces, damage = []int64{15}, []int64{1}
		}
		battleExec(t, w, fmt.Sprint("shot", i), "a", shot(t, w, faces, damage, true, stock))
		if i < 3 {
			e := w.Snapshot().Battle.State.Encounters[0]
			battleExec(t, w, fmt.Sprint("pass", i), "b", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: "b"}}})
		}
	}
	before := w.Snapshot().Clock.Tick
	r := battleExec(t, w, "recover", "a", core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "other-arrows", Created: "return-second"}, {Origin: "arrows", Created: "return-first"}}}})
	if stockCount(t, w, "return-first") != 1 || stockCount(t, w, "return-second") != 1 || w.Snapshot().Clock.Tick != before+60 || len(r.Events) != 4 {
		t.Fatal("two origins each recover one for a single minute", r)
	}
	cp := w.ExportCheckpoint()
	loaded, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(cp, loaded.ExportCheckpoint()) {
		t.Fatal("multi-origin replay", err)
	}
}
