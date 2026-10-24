package tests

import (
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestInitializationIllegalStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*core.Blueprint)
	}{
		{"duplicate character", func(b *core.Blueprint) { b.Characters = append(b.Characters, b.Characters[0]) }},
		{"cross-kind collision", func(b *core.Blueprint) { b.Items[0].ID = core.ItemID(b.Locations[0].ID) }},
		{"owner location", func(b *core.Blueprint) { b.Items[0].Owner = "loc:hall" }},
		{"wearer item", func(b *core.Blueprint) { b.Items[0].Wearer = "item:cloak:002" }},
		{"character at item", func(b *core.Blueprint) { b.Characters[0].Location = "item:shield:001" }},
		{"missing location", func(b *core.Blueprint) { b.Characters[0].Location = "missing" }},
		{"resource bounds", func(b *core.Blueprint) {
			b.Characters[0].Resources["hp"] = core.Resource{Current: 40, Minimum: 0, Maximum: 30}
		}},
		{"duplicate effects", func(b *core.Blueprint) { b.Effects = append(b.Effects, b.Effects[0]) }},
		{"duplicate relation", func(b *core.Blueprint) { b.Relationships = append(b.Relationships, b.Relationships[0]) }},
		{"wrong effect target", func(b *core.Blueprint) { b.Effects[0].Target = core.Ref{Kind: core.CharacterKind, ID: "char:ada"} }},
		{"missing effect source", func(b *core.Blueprint) { b.Effects[0].Source.ID = "missing" }},
		{"unknown rule", func(b *core.Blueprint) { b.Rules.Transfer = "surprise" }},
		{"owner-only initial", func(b *core.Blueprint) { b.Rules.Wear = core.OwnerOnly }},
		{"invalid identity", func(b *core.Blueprint) { b.Items[0].ID = "bad\nID" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fixture(t)
			tc.change(&b)
			if w, e := core.NewWorld(b); e == nil || w != nil {
				t.Fatal("illegal blueprint published")
			}
		})
	}
}

func TestForgedIRCannotBypassAuthority(t *testing.T) {
	w := world(t, fixture(t))
	for _, o := range []core.Operation{
		{},
		{Move: &core.Move{Character: "char:ada", To: "loc:garden"}, Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 1}},
		{Equip: &core.Equip{Item: "loc:hall", Wearer: "char:ada"}},
		{Equip: &core.Equip{Item: "item:missing", Wearer: "char:ada"}},
		{Equip: &core.Equip{Item: "item:shield:001", Wearer: "char:cy"}},
		{Damage: &core.ResourceOp{Character: "char:ada", Resource: "unknown", Amount: 1}},
		{Move: &core.Move{Character: "loc:hall", To: "loc:garden"}},
		{Move: &core.Move{Character: "char:ada", To: "item:shield:001"}},
		{Remove: &core.EffectOp{Effect: "missing"}},
		{Relate: &core.Relationship{From: core.Ref{Kind: core.ItemKind, ID: "char:ada"}, Kind: "trust", To: core.Ref{Kind: core.CharacterKind, ID: "char:bea"}, Strength: 3}},
	} {
		s, h := w.Snapshot(), w.History()
		r := core.Request{ID: "forged", ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 2}}, o}}
		if _, e := w.Execute(r); e == nil {
			t.Fatalf("forged IR accepted: %+v", o)
		}
		unchanged(t, w, s, h)
	}
	r := request(t, w, "actor", `damage "char:ada" resource "hp" by 1;`)
	r.Actor.Kind = core.LocationKind
	if _, e := w.Execute(r); e == nil {
		t.Fatal("forged actor type accepted")
	}
}

func TestAllDetachedBoundariesAndWriterSurface(t *testing.T) {
	b := fixture(t)
	b.ItemDefinitions = []core.ItemDefinition{{ID: "cloth", Name: "Cloth", Kind: "gear", Price: &core.ItemPrice{Unit: "cp", Amount: 2, UnitsPerLot: 1}},
		{ID: "wand", Name: "Wand", Kind: "wand", Charges: &core.ChargeRule{Max: 7, Recharge: &core.ChargeRecharge{Every: 10, Dice: &core.ChargeDice{Count: 1, Sides: 6}}}}}
	asOf := uint64(40)
	b.Items = append(b.Items, core.Item{ID: "item:wand:001", From: "wand", Owner: "char:ada", Charges: &core.ItemCharges{Current: 3, AsOf: &asOf, Seed: &[2]uint64{5, 6}}})
	wand := len(b.Items) - 1
	w := world(t, b)
	b.Items[wand].Charges.Current = 900
	*b.Items[wand].Charges.AsOf = 900
	b.Items[wand].Charges.Seed[0] = 900
	b.ItemDefinitions[1].Charges.Max = 900
	b.ItemDefinitions[1].Charges.Recharge.Dice.Sides = 900
	b.ItemDefinitions[0].Price.Amount = 900
	catalog, err := w.ItemDefinitionsView([]core.ItemDefinitionID{"cloth"})
	if err != nil {
		t.Fatal(err)
	}
	catalog[0].Price.Amount = 900
	if catalog, err = w.ItemDefinitionsView([]core.ItemDefinitionID{"cloth"}); err != nil || catalog[0].Price.Amount != 2 {
		t.Fatal("definition view aliases caller", err)
	}
	b.Characters[0].Stats["defense"] = 900
	b.Items[0].Owner = "char:cy"
	b.Effects[0].Amount = 900
	stat(t, w, "char:ada", "defense", 10)
	stat(t, w, "char:bea", "defense", 15)
	r := request(t, w, "first", `damage "char:ada" resource "hp" by 1; apply effect "effect:x" from character "char:cy" on character "char:ada" stat "defense" add 2; relate character "char:bea" "trust" character "char:cy" = 9;`)
	result, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	r.Operations[0].Damage.Amount = 900
	r.Operations[1].Apply.Amount = 900
	r.Operations = nil
	s := w.Snapshot()
	s.Characters[0].Stats["defense"] = 900
	s.Characters[0].Resources["hp"] = core.Resource{}
	s.Items[0].Owner = "fake"
	s.Effects[0].Amount = 900
	s.Relationships[0].Strength = 900
	result.Events[0].Resource.After.Current = 900
	result.Events[1].Effect.After.Amount = 900
	result.Events[2].Relationship.After.Strength = 900
	result.Request.Operations[0].Damage.Amount = 900
	s.Items[wand].Charges.Current = 900
	*s.Items[wand].Charges.AsOf = 900
	s.Items[wand].Charges.Seed[1] = 900
	*s.ItemDefinitions[1].Charges.Recharge.Dice = core.ChargeDice{}
	charges, err := w.ItemChargesView([]core.ItemID{"item:wand:001"})
	if err != nil {
		t.Fatal(err)
	}
	*charges[0].AsOf, charges[0].Seed[0] = 900, 900
	charges, err = w.ItemChargesView([]core.ItemID{"item:wand:001"})
	again := w.Snapshot().Items[wand].Charges
	if err != nil || charges[0].Current != 3 || charges[0].Max != 7 || *charges[0].AsOf != 40 || *charges[0].Seed != [2]uint64{5, 6} ||
		again.Current != 3 || *again.AsOf != 40 || *again.Seed != [2]uint64{5, 6} || w.Snapshot().ItemDefinitions[1].Charges.Recharge.Dice.Sides != 6 {
		t.Fatal("charge state aliases caller, snapshot or view", err, charges)
	}
	h := w.History()
	h[0].Request.Operations[1].Apply.Amount = 900
	h[0].Events[1].Effect.After.Amount = 900
	h[0].Events[2].Relationship.After.Strength = 900
	h[0].Events = nil
	ex, _ := w.Explain("char:ada", "defense")
	ex.Contributions[0].Amount = 900
	stat(t, w, "char:ada", "defense", 12)
	p, _ := w.CharacterStatus("char:ada")
	if p.Resources["hp"].Current != 19 {
		t.Fatal("input or result alias changed resource")
	}
	if w.History()[0].Events[1].Effect.After.Amount != 2 || w.History()[0].Request.Operations[0].Damage.Amount != 1 {
		t.Fatal("history aliases caller")
	}
	// Structural alternate-writer check, in addition to behavioral alias tests.
	typ := reflect.TypeOf(core.World{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Fatal("exported canonical field")
		}
	}
	allowed := map[string]bool{"ExportSince": true, "ReceiptMetadata": true, "ReceiptRevision": true, "ItemsAt": true, "BattleInput": true, "Receipt": true, "EquipmentView": true, "ExportCheckpoint": true, "Preview": true, "Execute": true, "Snapshot": true, "Revision": true, "History": true, "Explain": true, "TransferView": true, "CharacterStatus": true, "MapView": true, "LocationView": true, "LocationsView": true, "QuestsView": true}
	allowed["ItemDefinitionsView"] = true // detached read, exercised above
	allowed["ItemChargesView"] = true     // value read, exercised above
	allowed["JournalView"] = true         // detached read, exercised in TestJournalView
	allowed["AttackOptionsView"] = true   // detached read, exercised in TestAttackOptionsReadOnly
	handle := reflect.TypeOf(w)
	for i := 0; i < handle.NumMethod(); i++ {
		if !allowed[handle.Method(i).Name] {
			t.Fatalf("unreviewed world method: %s", handle.Method(i).Name)
		}
	}
}

func TestReplayCorruptionRejectedWithoutPartialWorld(t *testing.T) {
	b := fixture(t)
	w := world(t, b)
	execute(t, w, "a", `transfer "item:shield:001" from "char:ada" to "char:cy"; damage "char:ada" resource "hp" by 1;`)
	execute(t, w, "b", `heal "char:ada" resource "hp" by 2;`)
	for _, tc := range []struct {
		name   string
		mutate func([]core.Batch) []core.Batch
	}{
		{"before fact", func(h []core.Batch) []core.Batch { h[0].Events[2].Resource.Before.Current = 21; return h }},
		{"damage amount", func(h []core.Batch) []core.Batch { h[0].Events[2].Resource.After.Current = 18; return h }},
		{"damage direction", func(h []core.Batch) []core.Batch { h[0].Events[2].Resource.After.Current = 21; return h }},
		{"resource maximum", func(h []core.Batch) []core.Batch { h[0].Events[2].Resource.After.Maximum = 999; return h }},
		{"missing rule event", func(h []core.Batch) []core.Batch { h[0].Events = h[0].Events[1:]; return h }},
		{"rule reason", func(h []core.Batch) []core.Batch { h[0].Events[0].Wearing.Reason = "explicit"; return h }},
		{"missing payload", func(h []core.Batch) []core.Batch { h[0].Events[2].Resource = nil; return h }},
		{"extra payload", func(h []core.Batch) []core.Batch { h[0].Events[2].Movement = &core.MovementChange{}; return h }},
		{"event kind", func(h []core.Batch) []core.Batch { h[0].Events[2].Kind = "Unknown"; return h }},
		{"operation index", func(h []core.Batch) []core.Batch { h[0].Events[2].Operation = 100; return h }},
		{"event ID", func(h []core.Batch) []core.Batch { h[0].Events[2].ID = "event:1:99"; return h }},
		{"actor", func(h []core.Batch) []core.Batch { h[0].Events[2].Actor.ID = "char:cy"; return h }},
		{"correlation", func(h []core.Batch) []core.Batch { h[0].Events[2].RequestID = "another"; return h }},
		{"revision gap", func(h []core.Batch) []core.Batch { h[1].Revision = 4; return h }},
		{"late failure", func(h []core.Batch) []core.Batch { h[1].Events[0].Resource.After.Current = 3; return h }},
		{"duplicate request", func(h []core.Batch) []core.Batch { h[1].Request.ID = h[0].Request.ID; return h }},
		{"retained command mismatch", func(h []core.Batch) []core.Batch { h[0].Request.Operations[1].Damage.Amount = 2; return h }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := w.Snapshot()
			history := w.History()
			bad := tc.mutate(w.History())
			if got, e := core.Replay(b, bad); e == nil || got != nil {
				t.Fatal("corrupt replay returned a usable world")
			}
			unchanged(t, w, snapshot, history)
		})
	}
}

func TestBoundsAreExecutedContracts(t *testing.T) {
	w := world(t, fixture(t))
	ops := make([]core.Operation, core.MaxOperations)
	for i := range ops {
		ops[i] = core.Operation{Heal: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 1}}
	}
	r := core.Request{ID: "limit", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops}
	if _, e := w.Execute(r); e != nil {
		t.Fatal("128 operations rejected", e)
	}
	r.ID = "too-many"
	r.ExpectedRevision = 1
	r.Operations = append(r.Operations, r.Operations[0])
	s, h := w.Snapshot(), w.History()
	if _, e := w.Execute(r); e == nil {
		t.Fatal("129 operations accepted")
	}
	unchanged(t, w, s, h)
	b := fixture(t)
	w = world(t, b)
	for i := 0; i < core.MaxReplayBatches; i++ {
		r := core.Request{ID: fmt.Sprintf("bounded:%d", i), ExpectedRevision: core.Revision(i), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{{Heal: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 1}}}}
		if _, e := w.Execute(r); e != nil {
			t.Fatal(e)
		}
	}
	if replay, e := core.Replay(b, w.History()); e != nil || replay.Revision() != 1024 {
		t.Fatal("1024 batches not replayable", e)
	}
	history := w.History()
	history = append(history, history[0])
	if got, e := core.Replay(b, history); e == nil || got != nil {
		t.Fatal("1025 batches accepted")
	}
	history = w.History()[:1]
	for len(history[0].Events) <= 2*core.MaxOperations {
		history[0].Events = append(history[0].Events, history[0].Events[0])
	}
	if got, e := core.Replay(b, history); e == nil || got != nil {
		t.Fatal("event limit ignored")
	}
	_, ds := compiler.CompileActions("many", strings.Repeat(`heal "char:ada" resource "hp" by 1;`, 129), b)
	if len(ds) == 0 || ds[0].Code != "E_LIMIT" {
		t.Fatal("compiler operation bound missing")
	}
}

func TestCompilerFeedbackAndContinuation(t *testing.T) {
	w := world(t, fixture(t))
	before := w.Snapshot()
	for _, tc := range []struct{ src, code string }{
		{"\n damage \"char:ada\" resource \"hp\" by ;", "E_PARSE"},
		{`equip "loc:hall" on "char:ada";`, "E_TYPE"},
		{`damage "char:ada" resource "missing" by 1;`, "E_RESOURCE"},
		{`damage "char:ada" resource "hp" by 9223372036854775808;`, "E_INTEGER"},
		{`damage "char:ada" resource "hp" by -9223372036854775809;`, "E_INTEGER"},
		{`damage "char:ada" resource "hp" by -1;`, "E_AMOUNT"},
	} {
		ops, ds := compiler.CompileActions("bad.nql", tc.src, w.Snapshot().Blueprint)
		if ops != nil || len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "bad.nql" || ds[0].Line < 1 || ds[0].Column < 1 || ds[0].Expected == "" || ds[0].Actual == "" || ds[0].Message == "" {
			t.Fatalf("diagnostic missing: %+v", ds)
		}
	}
	if !reflect.DeepEqual(before, w.Snapshot()) || len(w.History()) != 0 {
		t.Fatal("compiler mutated world")
	}
	execute(t, w, "continued", `damage "char:ada" resource "hp" by 1;`)
	if w.Revision() != 1 {
		t.Fatal("valid continuation failed")
	}
	// Lowered IR is data independent of syntax; JSON transport is round-trippable.
	ops, ds := compiler.CompileActions("x", `heal "char:ada" resource "hp" by 1;`, w.Snapshot().Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	data, _ := json.Marshal(ops)
	var recovered []core.Operation
	if e := json.Unmarshal(data, &recovered); e != nil || !reflect.DeepEqual(ops, recovered) {
		t.Fatal("typed IR roundtrip", e)
	}
}

func TestReplayExactEventBoundary(t *testing.T) {
	b := fixture(t)
	ops := make([]core.Operation, core.MaxOperations)
	for i := range ops {
		id := core.ItemID(fmt.Sprintf("boundary:item:%d", i))
		b.Items = append(b.Items, core.Item{ID: id, Name: "Boundary token", Owner: "char:ada", Wearer: "char:bea"})
		ops[i] = core.Operation{Transfer: &core.Transfer{Item: id, From: "char:ada", To: "char:cy"}}
	}
	w := world(t, b)
	batch, e := w.Execute(core.Request{ID: "exact-events", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops})
	if e != nil {
		t.Fatal(e)
	}
	if len(batch.Events) != 256 {
		t.Fatalf("got %d events, want 256", len(batch.Events))
	}
	for i := 0; i < 256; i += 2 {
		if batch.Events[i].Kind != core.ItemUnequipped || batch.Events[i+1].Kind != core.ItemTransferred {
			t.Fatal("boundary event trace")
		}
	}
	replayed, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) {
		t.Fatal("256-event boundary replay differs")
	}
}

func FuzzTransactionRollback(f *testing.F) {
	f.Add(int64(7), true)
	f.Add(int64(-1), false)
	f.Add(int64(9223372036854775807), true)
	f.Fuzz(func(t *testing.T, amount int64, invalidSecond bool) {
		w := world(t, fixture(t))
		before, history := w.Snapshot(), w.History()
		ops := []core.Operation{{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: amount}}}
		if invalidSecond {
			ops = append(ops, core.Operation{Equip: &core.Equip{Item: "missing", Wearer: "char:ada"}})
		}
		_, e := w.Execute(core.Request{ID: "property", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops})
		if amount <= 0 || invalidSecond {
			if e == nil {
				t.Fatal("invalid operation succeeded")
			}
			unchanged(t, w, before, history)
		} else {
			if e != nil {
				t.Fatal(e)
			}
			p, _ := w.CharacterStatus("char:ada")
			want := int64(0)
			if amount < 20 {
				want = 20 - amount
			}
			if p.Resources["hp"].Current != want || w.Revision() != 1 {
				t.Fatal("resource property violated")
			}
		}
	})
}
