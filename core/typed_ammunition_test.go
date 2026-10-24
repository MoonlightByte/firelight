package core_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// typedFixture is rangedFixture with the bow's ammunition named by item type
// "arrow". Ada carries two stocks from that type, a plain stock, a stock of
// another type and a split whose root is not plain stock.
func typedFixture() core.Blueprint {
	b := rangedFixture()
	b.ItemDefinitions = []core.ItemDefinition{
		{ID: "arrow", Name: "Arrows", Description: "Identified test stock", Kind: "ammunition"},
		{ID: "bolt", Name: "Bolts", Kind: "ammunition"},
	}
	bow := &b.Equipment.Definitions[len(b.Equipment.Definitions)-1].Modes[0].Attacks[0]
	bow.Ranged = &core.RangedProfile{Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionType: "arrow"}
	b.Items[len(b.Items)-1].From = "arrow"
	carried := func(id core.ItemID, quantity int64) core.Item {
		i := core.Item{ID: id, Owner: "a", Custody: &core.Ref{Kind: core.CharacterKind, ID: "a"}}
		if quantity > 0 {
			i.Quantity = gearPtr(quantity)
		}
		return i
	}
	quiver := carried("quiver", 3)
	quiver.From = "arrow"
	stones := carried("stones", 5)
	stones.Name = "Stones"
	bolts := carried("bolts", 5)
	bolts.From = "bolt"
	// A single relic from the type, and a stock that claims to split from it.
	relic := carried("relic", 0)
	relic.From = "arrow"
	shard := carried("shard", 1)
	shard.From, shard.SplitFrom = "arrow", "relic"
	b.Items = append(b.Items, quiver, stones, bolts, relic, shard)
	return b
}

func ammunitionOrigins(t *testing.T, w *core.World) map[core.ItemID]core.ItemID {
	t.Helper()
	v, err := w.EquipmentView("a")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[core.ItemID]core.ItemID{}
	for _, i := range v.Items {
		if i.AmmunitionOrigin != "" {
			seen[i.Item.ID] = i.AmmunitionOrigin
		}
	}
	return seen
}

// A typed bow shoots any stock whose original root is declared from its type,
// and records each shot under that root.
func TestTypedAmmunitionShots(t *testing.T) {
	w := gearWorld(t, typedFixture())
	battleExec(t, w, "split", "a", core.Operation{Split: &core.SplitItem{Item: "arrows", Created: "part", Amount: 2, Handler: "a"}})
	if got := ammunitionOrigins(t, w); !reflect.DeepEqual(got, map[core.ItemID]core.ItemID{"arrows": "arrows", "part": "arrows", "quiver": "quiver"}) {
		t.Fatal("typed stocks", got)
	}
	battleExec(t, w, "begin", "a", rangedBegin(40))
	for _, stock := range []core.ItemID{"stones", "bolts", "shard", "relic"} {
		err := battleReject(t, w, gearRequest(w, "refused-"+string(stock), shot(t, w, []int64{1}, nil, true, stock)))
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != "E_BATTLE" || f.Field != "ammunition" || f.Actual != string(stock) {
			t.Fatal(stock, err)
		}
	}
	pass := func(id string) {
		e := w.Snapshot().Battle.State.Encounters[0]
		battleExec(t, w, id, "b", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: "b"}}})
	}
	for n, stock := range []core.ItemID{"quiver", "part", "arrows"} {
		r := battleExec(t, w, "shot-"+string(stock), "a", shot(t, w, []int64{1}, nil, true, stock))
		if r.Events[0].Kind != core.ItemConsumed || r.Events[0].Consumption.Item != stock {
			t.Fatal("debit", r)
		}
		if n < 2 {
			pass("pass-" + string(stock))
		}
	}
	if stockCount(t, w, "quiver") != 2 || stockCount(t, w, "part") != 1 || stockCount(t, w, "arrows") != 2 {
		t.Fatal("stocks")
	}
	got := w.Snapshot().Battle.State.Encounters[0].Ammunition
	want := []core.ActorAmmunition{{Actor: "a", Stocks: []core.AmmunitionStock{{Origin: "quiver", Spent: 1}, {Origin: "arrows", Spent: 2}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	cp := w.ExportCheckpoint()
	rebuilt, err := core.Replay(cp.Genesis, cp.History)
	if err != nil || !reflect.DeepEqual(w.Snapshot(), rebuilt.Snapshot()) {
		t.Fatal("replay", err)
	}
}

// Recovered arrows keep their origin, so a typed bow shoots them and their
// splits in a later fight.
func TestTypedAmmunitionRecovery(t *testing.T) {
	w := completedRanged(t, typedFixture(), 5)
	battleExec(t, w, "recover", "a", core.Operation{RecoverAmmunition: &core.RecoverAmmunition{Encounter: "gate", Actor: "a", Create: []core.AmmunitionCreation{{Origin: "arrows", Created: "recovered"}}}})
	battleExec(t, w, "split", "a", core.Operation{Split: &core.SplitItem{Item: "recovered", Created: "portion", Amount: 1, Handler: "a"}})
	if got := ammunitionOrigins(t, w); got["recovered"] != "arrows" || got["portion"] != "arrows" {
		t.Fatal("recovered origins", got)
	}
	battleExec(t, w, "revive", "a", core.Operation{Restore: &core.RestoreResource{Character: "b", Resource: "hp"}})
	begin := rangedBegin(40)
	begin.BeginBattle.ID = "second"
	battleExec(t, w, "second-begin", "a", begin)
	battleExec(t, w, "use-recovered", "a", shot(t, w, []int64{1}, nil, false, "portion"))
	e := w.Snapshot().Battle.State.Encounters[1]
	if e.ID != "second" || !reflect.DeepEqual(e.Ammunition, []core.ActorAmmunition{{Actor: "a", Stocks: []core.AmmunitionStock{{Origin: "arrows", Spent: 1}}}}) {
		t.Fatalf("%+v", e.Ammunition)
	}
}

// A Condition type's typed ranged attack classifies the type's stocks too.
func TestTypedAmmunitionConditionProjection(t *testing.T) {
	b := typedFixture()
	bow := &b.Equipment.Definitions[len(b.Equipment.Definitions)-1].Modes[0].Attacks[0]
	profile := bow.Ranged
	bow.Ranged = nil
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "spitter", Instances: core.Unique, Attacks: []core.EquipmentAttack{{ID: "spit", Name: "Spit", Ranged: profile, ToHit: core.ScalarRecipe{Base: 4}, Damage: core.DamageGrant{Count: 1, Sides: 6, Kind: "acid"}}}})
	w := gearWorld(t, b)
	if got := ammunitionOrigins(t, w); !reflect.DeepEqual(got, map[core.ItemID]core.ItemID{"arrows": "arrows", "quiver": "quiver"}) {
		t.Fatal(got)
	}
}

func TestTypedAmmunitionValidation(t *testing.T) {
	if err := core.Validate(typedFixture()); err != nil {
		t.Fatal(err)
	}
	// In IR a profile sets exactly one of the two.
	for _, p := range []core.RangedProfile{{Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionType: "arrow"}, {Stat: "dexterity", Normal: 80, Maximum: 320, AmmunitionOrigin: "arrows"}} {
		data, err := json.Marshal(p)
		want := `{"stat":"dexterity","normal":80,"maximum":320,"ammunition_type":"arrow"}`
		if p.AmmunitionOrigin != "" {
			want = `{"stat":"dexterity","normal":80,"maximum":320,"ammunition_origin":"arrows"}`
		}
		if err != nil || string(data) != want {
			t.Fatal(string(data), err)
		}
	}
	for _, tc := range []struct {
		name, actual string
		edit         func(*core.Blueprint, *core.RangedProfile)
	}{
		{"an item and a type", "arrow", func(_ *core.Blueprint, p *core.RangedProfile) { p.AmmunitionOrigin = "arrows" }},
		{"an undeclared type", "missing", func(_ *core.Blueprint, p *core.RangedProfile) { p.AmmunitionType = "missing" }},
		{"a type with equipment", "longbow", func(b *core.Blueprint, p *core.RangedProfile) {
			b.ItemDefinitions = append(b.ItemDefinitions, core.ItemDefinition{ID: "longbow", Name: "Longbow", Kind: "weapon", Equipment: "bow"})
			p.AmmunitionType = "longbow"
		}},
		{"neither", "invalid", func(_ *core.Blueprint, p *core.RangedProfile) { p.AmmunitionType = "" }},
		{"a bad range", "invalid", func(_ *core.Blueprint, p *core.RangedProfile) { p.Maximum = 79 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := typedFixture()
			tc.edit(&b, b.Equipment.Definitions[len(b.Equipment.Definitions)-1].Modes[0].Attacks[0].Ranged)
			var f *core.Fault
			if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Field != "ranged" || f.Subject != "shot" || f.Actual != tc.actual {
				t.Fatal(err)
			}
		})
	}
}
