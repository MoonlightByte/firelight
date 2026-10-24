package core_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func gearPtr[T any](v T) *T { return &v }
func gearFixture() core.Blueprint {
	str := core.StatTerm{Stat: "strength", Offset: -10, Divisor: 2}
	dex := core.StatTerm{Stat: "dexterity", Offset: -10, Divisor: 2}
	attack := func(sides int64) core.EquipmentAttack {
		return core.EquipmentAttack{ID: "slash", Name: "Slash", ToHit: core.ScalarRecipe{Terms: []core.StatTerm{str, {Stat: "proficiency", Divisor: 1, Requires: "martial"}}}, Damage: core.DamageGrant{Count: 1, Sides: sides, Kind: "slashing", Bonus: core.ScalarRecipe{Terms: []core.StatTerm{str}}}}
	}
	armor := func(id core.EquipmentDefinitionID, base int64, cap *int64) core.EquipmentDefinition {
		term := dex
		term.Ceiling = cap
		return core.EquipmentDefinition{ID: id, Name: string(id), Description: "Reviewed test armor", DefaultMode: "worn", Modes: []core.EquipmentMode{{ID: "worn", Slots: []core.SlotUse{{Slot: "body", Amount: 1}}, Overrides: []core.StatBaseOverride{{Stat: "defense", Recipe: core.ScalarRecipe{Base: base, Terms: []core.StatTerm{term}}}}}}}
	}
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}},
		Characters: []core.Character{
			{ID: "a", Name: "Alex", Location: "room", Stats: map[core.StatID]int64{"strength": 16, "dexterity": 14, "proficiency": 2, "defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 8, Minimum: 0, Maximum: 10}}},
			{ID: "b", Name: "Alex", Location: "room", Stats: map[core.StatID]int64{"strength": 12, "dexterity": 16, "proficiency": 2, "defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 6, Minimum: 0, Maximum: 10}}},
		},
		ConditionDefinitions: []core.ConditionDefinition{{ID: "martial", Instances: core.Unique}, {ID: "shield-trained", Instances: core.Unique}, {ID: "strong", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "strength", Stat: "strength", Fixed: gearPtr(int64(2))}}}, {ID: "guarded", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "defense", Stat: "defense", Fixed: gearPtr(int64(3))}}}},
		Conditions:           []core.ConditionInstance{{ID: "a-martial", Type: "martial", Target: "a"}, {ID: "b-martial", Type: "martial", Target: "b"}, {ID: "a-shield", Type: "shield-trained", Target: "a"}, {ID: "b-shield", Type: "shield-trained", Target: "b"}},
		Relationships:        []core.Relationship{{From: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "trust", To: core.Ref{Kind: core.CharacterKind, ID: "b"}, Strength: 4}},
		Equipment: &core.EquipmentRules{Version: core.EquipmentVersion, Slots: []core.SlotDefinition{{ID: "hand", Capacity: 2}, {ID: "body", Capacity: 1}, {ID: "shield", Capacity: 1}}, Derived: []core.DerivedStatRule{{Stat: "defense", Terms: []core.StatTerm{dex}}}, Definitions: []core.EquipmentDefinition{
			{ID: "longsword", Name: "Longsword", Description: "Versatile: one hand 1d8; two hands 1d10. Mastery Sap is not executed by this core fixture.", DefaultMode: "one", Modes: []core.EquipmentMode{{ID: "one", Slots: []core.SlotUse{{Slot: "hand", Amount: 1}}, Attacks: []core.EquipmentAttack{attack(8)}}, {ID: "two", Slots: []core.SlotUse{{Slot: "hand", Amount: 2}}, Attacks: []core.EquipmentAttack{attack(10)}}}},
			armor("leather", 11, nil), armor("chain-shirt", 13, gearPtr(int64(2))),
			{ID: "shield", Name: "Shield", Description: "A trained wearer gains +2 defense; occupies one hand and the shield slot.", DefaultMode: "held", Modes: []core.EquipmentMode{{ID: "held", Slots: []core.SlotUse{{Slot: "hand", Amount: 1}, {Slot: "shield", Amount: 1}}, Modifiers: []core.EquipmentModifier{{ID: "defense", Stat: "defense", Amount: 2, Requires: "shield-trained"}}}}},
		}},
	}
	for _, v := range []struct {
		id  core.ItemID
		def core.EquipmentDefinitionID
	}{{"sword", "longsword"}, {"sword2", "longsword"}, {"leather", "leather"}, {"chain", "chain-shirt"}, {"shield", "shield"}, {"shield2", "shield"}} {
		b.Items = append(b.Items, core.Item{ID: v.id, Name: string(v.def), Owner: "a", Equipment: &core.EquipmentInstance{Definition: v.def}})
	}
	return b
}
func gearWorld(t *testing.T, b core.Blueprint) *core.World {
	t.Helper()
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func gearRequest(w *core.World, id string, ops ...core.Operation) core.Request {
	return core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops}
}
func gearExecute(t *testing.T, w *core.World, id string, ops ...core.Operation) core.Batch {
	t.Helper()
	r, e := w.Execute(gearRequest(w, id, ops...))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func wear(item core.ItemID, who core.CharacterID, mode core.EquipmentModeID) core.Operation {
	return core.Operation{Equip: &core.Equip{Item: item, Wearer: who, Mode: mode}}
}
func unwear(item core.ItemID) core.Operation {
	return core.Operation{Unequip: &core.Unequip{Item: item}}
}
func gearDefense(t *testing.T, w *core.World, id core.CharacterID, want int64) {
	t.Helper()
	x, e := w.Explain(id, "defense")
	if e != nil || x.Effective != want {
		t.Fatalf("defense %s want%d got%+v err%v", id, want, x, e)
	}
}
func gearAttack(t *testing.T, w *core.World, id core.CharacterID, hit, sides, bonus int64) {
	t.Helper()
	v, e := w.EquipmentView(id)
	if e != nil || len(v.Attacks) != 1 {
		t.Fatalf("grants %+v %v", v, e)
	}
	a := v.Attacks[0]
	if a.ToHit != hit || a.DamageCount != 1 || a.DamageSides != sides || a.DamageBonus != bonus {
		t.Fatalf("wrong grant %+v", a)
	}
}
func gearReject(t *testing.T, w *core.World, id string, ops ...core.Operation) {
	t.Helper()
	before := w.ExportCheckpoint()
	_, e := w.Execute(gearRequest(w, id, ops...))
	if e == nil {
		t.Fatal("accepted invalid request")
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("rejection changed state/history/receipt")
	}
}

func TestEquipmentCanonicalCompositionAndFallback(t *testing.T) {
	w := gearWorld(t, gearFixture())
	initial := w.Snapshot()
	gearExecute(t, w, "load", wear("leather", "a", ""), wear("shield", "a", ""), wear("sword", "a", ""))
	gearDefense(t, w, "a", 15)
	gearAttack(t, w, "a", 5, 8, 3)
	x, _ := w.Explain("a", "defense")
	if x.Base != 11 || x.Recipe == nil || x.Recipe.Origin.Item != "leather" || len(x.Contributions) != 1 || x.Contributions[0].EquipmentOrigin.Item != "shield" {
		t.Fatalf("lost provenance %+v", x)
	}
	gearExecute(t, w, "guard", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "guard", Type: "guarded", Target: "a"}})
	gearDefense(t, w, "a", 18)
	gearExecute(t, w, "remove-guard", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "guard", Target: "a"}})
	gearDefense(t, w, "a", 15)
	gearExecute(t, w, "strong", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "strong-a", Type: "strong", Target: "a"}})
	gearAttack(t, w, "a", 6, 8, 4)
	gearExecute(t, w, "remove-strong", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "strong-a", Target: "a"}})
	gearAttack(t, w, "a", 5, 8, 3)
	gearExecute(t, w, "remove-training", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "a-martial", Target: "a"}})
	gearAttack(t, w, "a", 3, 8, 3)
	gearExecute(t, w, "remove-leather", unwear("leather"))
	gearDefense(t, w, "a", 14)
	gearExecute(t, w, "remove-shield-training", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "a-shield", Target: "a"}})
	gearDefense(t, w, "a", 12)
	v, _ := w.EquipmentView("a")
	if v.Slots[0].Used != 2 || v.Slots[2].Used != 1 {
		t.Fatal("training removal incorrectly freed shield")
	}
	gearExecute(t, w, "remove-shield", unwear("shield"))
	gearDefense(t, w, "a", 12)
	now := w.Snapshot()
	if !reflect.DeepEqual(initial.Relationships, now.Relationships) || !reflect.DeepEqual(initial.Characters, now.Characters) {
		t.Fatal("changed base stats/resources/location or relationships")
	}
}
func TestEquipmentModesSlotsTransferAndRollback(t *testing.T) {
	w := gearWorld(t, gearFixture())
	gearExecute(t, w, "load", wear("leather", "a", ""), wear("shield", "a", ""), wear("sword", "a", "one"))
	gearReject(t, w, "two-body", wear("chain", "a", ""), unwear("leather"))
	gearReject(t, w, "two-shields", unwear("sword"), wear("shield2", "a", ""))
	gearReject(t, w, "two-hands", unwear("sword"), wear("sword", "a", "two"))
	gearReject(t, w, "already-worn", wear("sword", "b", "one"))
	gearReject(t, w, "bad-mode", wear("sword2", "b", "invented"))
	gearExecute(t, w, "b-load", wear("chain", "b", ""), wear("shield2", "b", ""))
	gearDefense(t, w, "b", 17)
	gearReject(t, w, "late", core.Operation{Transfer: &core.Transfer{Item: "sword", From: "a", To: "b"}}, wear("sword", "b", "two"))
	batch := gearExecute(t, w, "handoff", unwear("sword"), core.Operation{Transfer: &core.Transfer{Item: "sword", From: "a", To: "b"}}, wear("sword", "b", "one"))
	if len(batch.Events) != 3 || batch.Events[0].Wearing.ModeBefore != "one" || batch.Events[2].Wearing.ModeAfter != "one" || batch.Events[2].Wearing.RequestedMode != "one" {
		t.Fatalf("event trace %+v", batch)
	}
	gearAttack(t, w, "b", 3, 8, 1)
	v, _ := w.EquipmentView("a")
	if len(v.Attacks) != 0 {
		t.Fatal("old wearer retains attack")
	}
	gearExecute(t, w, "two", unwear("shield2"), unwear("sword"), wear("sword", "b", "two"))
	gearAttack(t, w, "b", 3, 10, 1)
	gearExecute(t, w, "implicit", core.Operation{Transfer: &core.Transfer{Item: "sword", From: "b", To: "a"}})
	h := w.History()
	last := h[len(h)-1]
	if last.Events[0].Wearing.ModeBefore != "two" || last.Events[0].Wearing.Reason != "transfer rule" {
		t.Fatal("implicit mode provenance lost")
	}
}
func TestEquipmentExactReplayDefaultIntentAndNativeProfile(t *testing.T) {
	w := gearWorld(t, gearFixture())
	initial := w.ExportCheckpoint().Genesis
	r := gearRequest(w, "first", wear("sword", "a", ""))
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	if receipt.Events[0].Wearing.RequestedMode != "" || receipt.Events[0].Wearing.ModeAfter != "one" {
		t.Fatal("default intent not retained")
	}
	gearExecute(t, w, "off", unwear("sword"))
	for _, replay := range []bool{false, true} {
		subject := w
		if replay {
			subject, e = core.Replay(initial, w.History())
			if e != nil {
				t.Fatal(e)
			}
		}
		before := subject.ExportCheckpoint()
		again, e := subject.Execute(r)
		if e != nil || !reflect.DeepEqual(receipt, again) || !reflect.DeepEqual(before, subject.ExportCheckpoint()) {
			t.Fatal("historical retry changed outcome")
		}
		changed := r
		changed.Operations = []core.Operation{wear("sword", "a", "one")}
		_, e = subject.Execute(changed)
		var f *core.Fault
		if !errors.As(e, &f) || f.Code != "E_REQUEST_REUSE" {
			t.Fatalf("identity conflict %v", e)
		}
	}
	var data bytes.Buffer
	info, e := nativesave.Write(&data, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "gear", Next: 3})
	if e != nil {
		t.Fatal(e)
	}
	if info.Semantics != nativesave.EquipmentSemanticsVersion {
		t.Fatal("wrong semantics profile")
	}
	loaded, e := nativesave.Read(bytes.NewReader(data.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("archive changed truth")
	}
	again, e := loaded.World.Execute(r)
	if e != nil || !reflect.DeepEqual(again, receipt) {
		t.Fatal("archive lost receipt")
	}
	bad := strings.Replace(data.String(), nativesave.EquipmentSemanticsVersion, nativesave.SemanticsVersion, 1)
	if got, e := nativesave.Read(strings.NewReader(bad)); e == nil || got != nil {
		t.Fatal("profile/genesis mismatch accepted")
	}
	history := w.History()
	history[0].Events[0].Wearing.ModeAfter = "two"
	if got, e := core.Replay(initial, history); e == nil || got != nil {
		t.Fatal("tampered mode replayed")
	}
	if _, e := w.TransferView("sword", "b"); e == nil {
		t.Fatal("legacy projection claims sufficiency")
	}
}
func TestEquipmentFloorsCapsAndExactIntermediate(t *testing.T) {
	for _, tc := range []struct{ dex, want int64 }{{16, 15}, {9, 12}, {8, 12}, {7, 11}} {
		b := gearFixture()
		b.Characters[0].Stats["dexterity"] = tc.dex
		w := gearWorld(t, b)
		gearExecute(t, w, "shirt", wear("chain", "a", ""))
		gearDefense(t, w, "a", tc.want)
	}
	b := gearFixture()
	b.Characters[0].Stats["strength"] = math.MaxInt64
	b.Characters[0].Stats["defense"] = math.MinInt64
	// The derived rule applies to B too; keep its independent final value valid.
	b.Characters[1].Stats["defense"] = math.MinInt64
	b.Equipment.Derived[0].Terms = []core.StatTerm{{Stat: "strength", Offset: math.MaxInt64, Divisor: 1}}
	b.Effects = []core.Effect{{ID: "one", Source: core.Ref{Kind: core.CharacterKind, ID: "a"}, Mode: core.OnCharacter, Target: core.Ref{Kind: core.CharacterKind, ID: "a"}, Stat: "defense", Amount: 1}}
	w := gearWorld(t, b)
	gearDefense(t, w, "a", math.MaxInt64)
	gearReject(t, w, "overflow", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "guard", Type: "guarded", Target: "a"}})
	// Final fallback may overflow even when the currently worn override is valid.
	b = gearFixture()
	b.Characters[0].Stats["defense"] = math.MaxInt64
	for j := range b.Items {
		if b.Items[j].ID == "leather" {
			b.Items[j].Wearer = "a"
			b.Items[j].Equipment.Mode = "worn"
		}
	}
	w = gearWorld(t, b)
	gearDefense(t, w, "a", 13)
	gearReject(t, w, "bad-fallback", unwear("leather"))
}
func TestEquipmentDetachedInputsAndViews(t *testing.T) {
	b := gearFixture()
	w := gearWorld(t, b)
	b.Equipment.Definitions[2].Modes[0].Overrides[0].Recipe.Terms[0].Ceiling = gearPtr(int64(-100))
	b.Items[0].Equipment.Definition = "missing"
	gearExecute(t, w, "load", wear("chain", "a", ""), wear("sword", "a", ""))
	gearDefense(t, w, "a", 15)
	v, _ := w.EquipmentView("a")
	other, _ := w.EquipmentView("a")
	saved := w.ExportCheckpoint()
	for j := range v.Items {
		if v.Items[j].Definition != nil {
			v.Items[j].Definition.Modes[0].ID = "bad"
			v.Items[j].Item.Equipment.Mode = "bad"
		}
	}
	v.Attacks[0].Origin.Item = "bad"
	v.Attacks[0].HitRecipe.Terms[0].Term.Stat = "bad"
	s := w.Snapshot()
	s.Equipment.Definitions[0].Modes[0].Attacks[0].ToHit.Terms[0].Stat = "bad"
	if !reflect.DeepEqual(saved, w.ExportCheckpoint()) {
		t.Fatal("aliased authority")
	}
	now, _ := w.EquipmentView("a")
	if !reflect.DeepEqual(other, now) {
		t.Fatal("aliased other result")
	}
	ex, _ := w.Explain("a", "defense")
	*ex.Recipe.Terms[0].Term.Ceiling = -99
	gearDefense(t, w, "a", 15)
}
func TestEquipmentInvalidRecordsAndProfileCapacity(t *testing.T) {
	cases := map[string]func(*core.Blueprint){
		"cycle":        func(b *core.Blueprint) { b.Equipment.Derived[0].Terms[0].Stat = "defense" },
		"zero divisor": func(b *core.Blueprint) { b.Equipment.Derived[0].Terms[0].Divisor = 0 },
		"duplicate definition": func(b *core.Blueprint) {
			b.Equipment.Definitions = append(b.Equipment.Definitions, b.Equipment.Definitions[0])
		},
		"duplicate mode":      func(b *core.Blueprint) { d := &b.Equipment.Definitions[0]; d.Modes = append(d.Modes, d.Modes[0]) },
		"undeclared override": func(b *core.Blueprint) { b.Equipment.Definitions[1].Modes[0].Overrides[0].Stat = "strength" },
		"missing type": func(b *core.Blueprint) {
			b.Equipment.Definitions[0].Modes[0].Attacks[0].ToHit.Terms[1].Requires = "missing"
		},
		"bad version": func(b *core.Blueprint) { b.Equipment.Version = "unknown" },
		"unworn mode": func(b *core.Blueprint) { b.Items[0].Equipment.Mode = "one" },
		"oversized items": func(b *core.Blueprint) {
			for len(b.Items) < core.MaxWorldItems+1 {
				b.Items = append(b.Items, core.Item{ID: core.ItemID(fmt.Sprint(len(b.Items)))})
			}
		},
		"forged origin": func(b *core.Blueprint) {
			b.Effects = []core.Effect{{ID: "fake", EquipmentOrigin: &core.EquipmentOrigin{Item: "sword"}}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b := gearFixture()
			change(&b)
			if w, e := core.NewWorld(b); e == nil || w != nil {
				t.Fatal("accepted invalid equipment")
			}
		})
	}
	b := gearFixture()
	// A world publishes one instance short of the cap, takes the last, and
	// refuses one more. The fill uses a type without modifiers, which costs
	// no stat evaluation (docs/WORLD_SIZE.md).
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "marked", Instances: core.Coexist})
	for len(b.Conditions) < core.MaxWorldConditions-1 {
		b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprint(len(b.Conditions))), Type: "marked", Target: "a"})
	}
	w := gearWorld(t, b)
	gearExecute(t, w, "at-cap", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "at-cap", Type: "guarded", Target: "a"}})
	err := battleReject(t, w, gearRequest(w, "too-many", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "over-cap", Type: "guarded", Target: "a"}}))
	if f, ok := err.(*core.Fault); !ok || f.Field != "capacity" {
		t.Fatal("expected Condition capacity", err)
	}
	gearExecute(t, w, "too-many", wear("sword", "a", "")) // failed ID was not reserved
}
func TestEquipmentConcurrentExactRetry(t *testing.T) {
	w := gearWorld(t, gearFixture())
	r := gearRequest(w, "shared", wear("sword", "a", "two"))
	var wg sync.WaitGroup
	out := make(chan core.Batch, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); b, e := w.Execute(r); out <- b; errs <- e }()
	}
	wg.Wait()
	close(out)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first *core.Batch
	for b := range out {
		if first == nil {
			first = &b
		} else if !reflect.DeepEqual(*first, b) {
			t.Fatal("retry receipts differ")
		}
	}
	if w.Revision() != 1 || len(w.History()) != 1 {
		t.Fatal("concurrent replay duplicated")
	}
}

func TestEquipmentProfileHasNoUntypedWearFallback(t *testing.T) {
	b := gearFixture()
	b.Items = append(b.Items, core.Item{ID: "untyped", Owner: "a"})
	w := gearWorld(t, b)
	gearReject(t, w, "untyped", wear("untyped", "a", ""))
	b.Items[len(b.Items)-1].Wearer = "a"
	if got, e := core.NewWorld(b); e == nil || got != nil {
		t.Fatal("equipment profile admits untyped worn item")
	}
	// The older profile remains valid; this is not a reinterpretation of M1.
	b = core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room"}}, Characters: []core.Character{{ID: "a", Location: "room"}}, Items: []core.Item{{ID: "untyped", Owner: "a"}}}
	w = gearWorld(t, b)
	gearExecute(t, w, "legacy", wear("untyped", "a", ""))
}
