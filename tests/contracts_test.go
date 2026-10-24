package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

// Serialized oracles are captured BEFORE mutation; another aliased snapshot
// cannot accidentally become the expected answer. Fixed semantic facts below
// separately guard against two equally wrong reconstruction paths.
func contractBytes(t testing.TB, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func contractEqual(t testing.TB, before []byte, after any) {
	t.Helper()
	if !bytes.Equal(before, contractBytes(t, after)) {
		t.Fatal("detached/borrowed record changed")
	}
}
func contractFixture(t testing.TB) core.Blueprint {
	b := conditionFixture(t)
	b.Conditions = []core.ConditionInstance{{ID: "genesis", Type: "blessing", Target: "char:cy", Source: ptr(core.Ref{Kind: core.ItemKind, ID: "item:venom"}), Magnitude: ptr(int64(1))}}
	b.RetiredConditionIDs = []core.ConditionInstanceID{"before-genesis"}
	return b
}
func contractJourney(t testing.TB) (core.Blueprint, *core.World) {
	b := contractFixture(t)
	w := world(t, b)
	execute(t, w, "mixed", `apply condition "a" of "blessing" to "char:ada" magnitude 2 from location "loc:shrine";
apply condition "b" of "blessing" to "char:ada" magnitude 2 from item "item:venom";
apply condition "mark" of "marked" to "char:ada";
damage "char:ada" resource "hp" by 3;
transfer "item:shield" from "char:bea" to "char:cy";
equip "item:shield" on "char:bea";
move "char:ada" to "loc:shrine";
relate character "char:ada" "trust" character "char:bea" = 5;
apply effect "decoy" from item "item:venom" on character "char:ada" stat "defense" add 2;`)
	stat(t, w, "char:ada", "defense", 16)
	execute(t, w, "remove-a", `remove condition "a" from "char:ada"; expire effect "decoy"; heal "char:ada" resource "hp" by 1;`)
	stat(t, w, "char:ada", "defense", 12)
	execute(t, w, "remove-b", `remove condition "b" from "char:ada"; unequip "item:shield";
apply effect "brief" from character "char:ada" on character "char:ada" stat "defense" add 1; remove effect "brief";`)
	execute(t, w, "ephemeral", `apply condition "ephemeral" of "marked" to "char:ada"; remove condition "ephemeral" from "char:ada";`)
	return b, w
}
func contractFinal(t testing.TB, w *core.World) {
	t.Helper()
	s := w.Snapshot()
	stat(t, w, "char:ada", "defense", 10)
	stat(t, w, "char:bea", "defense", 10)
	stat(t, w, "char:cy", "defense", 10)
	if s.Revision != 4 || len(s.Conditions) != 2 || len(s.ConditionEffects) != 1 || len(s.Effects) != 1 || s.Effects[0].ID != "shield" || !reflect.DeepEqual(s.RetiredConditionIDs, []core.ConditionInstanceID{"a", "b", "before-genesis", "ephemeral"}) {
		t.Fatal("fixed lifecycle oracle", s)
	}
	if s.Conditions[0].ID != "genesis" || s.Conditions[1].ID != "mark" || s.Conditions[1].Cause.RequestID != "mixed" || s.Conditions[1].AppliedRevision != 1 {
		t.Fatal("fixed provenance oracle")
	}
	if s.Characters[0].Resources["hp"].Current != 18 || s.Characters[0].Location != "loc:shrine" || s.Items[0].Owner != "char:cy" || s.Items[0].Wearer != "" {
		t.Fatal("fixed resource/location/item oracle")
	}
	if s.Relationships[0].Strength != 5 || s.Relationships[1].Strength != 417 {
		t.Fatal("fixed relationship oracle")
	}
	h := w.History()
	want := [][]core.EventKind{
		{core.ConditionApplied, core.ConditionApplied, core.ConditionApplied, core.DamageApplied, core.ItemUnequipped, core.ItemTransferred, core.ItemEquipped, core.EntityMoved, core.RelationshipChanged, core.EffectApplied},
		{core.ConditionRemoved, core.EffectExpired, core.HealingApplied},
		{core.ConditionRemoved, core.ItemUnequipped, core.EffectApplied, core.EffectRemoved},
		{core.ConditionApplied, core.ConditionRemoved},
	}
	for i, kinds := range want {
		if len(h[i].Events) != len(kinds) {
			t.Fatal("event count", i)
		}
		for j, kind := range kinds {
			if h[i].Events[j].Kind != kind {
				t.Fatal("event order", i, j)
			}
		}
	}
	if len(h[0].Request.Operations) != 9 || h[0].Events[4].Operation != 4 || h[0].Events[5].Operation != 4 || h[0].Events[4].Wearing.Before != "char:ada" || h[0].Events[8].Relationship.Before.Strength != 4 {
		t.Fatal("transaction/rule trace")
	}
}

// Deliberately explicit mutation inventory, mirroring mutable record FAMILIES,
// not clone implementations. Production clone helpers are never test oracles.
func tamperCondition(c *core.ConditionInstance) {
	if c == nil {
		return
	}
	c.Type = "tampered"
	if c.Source != nil {
		c.Source.ID = "tampered"
	}
	if c.Magnitude != nil {
		*c.Magnitude = 99
	}
	if c.Cause != nil {
		c.Cause.Actor.ID = "tampered"
		c.Cause.RequestID = "tampered"
	}
}
func tamperEffect(e *core.Effect) {
	if e == nil {
		return
	}
	e.Amount = 99
	if e.ConditionOrigin != nil {
		e.ConditionOrigin.Instance = "tampered"
		e.ConditionOrigin.Modifier = "tampered"
	}
}
func tamperBlueprint(b *core.Blueprint) {
	for i := range b.Characters {
		b.Characters[i].Name = "tampered"
		b.Characters[i].Stats["tampered"] = 99
		b.Characters[i].Resources["tampered"] = core.Resource{}
	}
	for i := range b.Items {
		b.Items[i].Owner = "tampered"
	}
	for i := range b.Locations {
		b.Locations[i].Name = "tampered"
	}
	for i := range b.Effects {
		tamperEffect(&b.Effects[i])
	}
	for i := range b.Relationships {
		b.Relationships[i].Strength = 99
	}
	for i := range b.ConditionDefinitions {
		d := &b.ConditionDefinitions[i]
		d.Instances = core.Unique
		for j := range d.Modifiers {
			m := &d.Modifiers[j]
			m.Stat = "tampered"
			if m.Fixed != nil {
				*m.Fixed = 99
			}
		}
	}
	for i := range b.Conditions {
		tamperCondition(&b.Conditions[i])
	}
	for i := range b.RetiredConditionIDs {
		b.RetiredConditionIDs[i] = "tampered"
	}
}
func tamperRequest(r *core.Request) {
	r.Actor.ID = "tampered"
	for i := range r.Operations {
		o := &r.Operations[i]
		if v := o.ApplyCondition; v != nil {
			v.Type = "tampered"
			if v.Source != nil {
				v.Source.ID = "tampered"
			}
			if v.Magnitude != nil {
				*v.Magnitude = 99
			}
		}
		if v := o.RemoveCondition; v != nil {
			v.Target = "tampered"
		}
		if v := o.Transfer; v != nil {
			v.To = "tampered"
		}
		if v := o.Equip; v != nil {
			v.Wearer = "tampered"
		}
		if v := o.Unequip; v != nil {
			v.Item = "tampered"
		}
		if v := o.Damage; v != nil {
			v.Amount = 99
		}
		if v := o.Heal; v != nil {
			v.Amount = 99
		}
		tamperEffect(o.Apply)
		if v := o.Remove; v != nil {
			v.Effect = "tampered"
		}
		if v := o.Expire; v != nil {
			v.Effect = "tampered"
		}
		if v := o.Move; v != nil {
			v.To = "tampered"
		}
		if v := o.Relate; v != nil {
			v.Strength = 99
		}
	}
}
func tamperBatch(b *core.Batch) {
	tamperRequest(&b.Request)
	for i := range b.Events {
		e := &b.Events[i]
		e.Actor.ID = "tampered"
		if v := e.Condition; v != nil {
			tamperCondition(v.Before)
			tamperCondition(v.After)
			for j := range v.BeforeEffects {
				tamperEffect(&v.BeforeEffects[j])
			}
			for j := range v.AfterEffects {
				tamperEffect(&v.AfterEffects[j])
			}
		}
		if v := e.Ownership; v != nil {
			v.Before = "tampered"
		}
		if v := e.Wearing; v != nil {
			v.Before = "tampered"
		}
		if v := e.Resource; v != nil {
			v.Before.Current = 99
		}
		if v := e.Effect; v != nil {
			tamperEffect(v.Before)
			tamperEffect(v.After)
		}
		if v := e.Movement; v != nil {
			v.Before = "tampered"
		}
		if v := e.Relationship; v != nil {
			v.After.Strength = 99
			if v.Before != nil {
				v.Before.Strength = 99
			}
		}
	}
}

func TestContractNestedOwnershipAcrossReconstruction(t *testing.T) {
	genesis, live := contractJourney(t)
	contractFinal(t, live)
	inputHistory := live.History()
	genesisBytes, historyBytes := contractBytes(t, genesis), contractBytes(t, inputHistory)
	rebuilt, e := core.Replay(genesis, inputHistory)
	if e != nil {
		t.Fatal(e)
	}
	contractEqual(t, genesisBytes, genesis)
	contractEqual(t, historyBytes, inputHistory)
	stateBytes := contractBytes(t, live.Snapshot())
	tamperBlueprint(&genesis)
	for i := range inputHistory {
		tamperBatch(&inputHistory[i])
	}
	for _, w := range []*core.World{live, rebuilt} {
		contractEqual(t, stateBytes, w.Snapshot())
		contractEqual(t, historyBytes, w.History())
		first, second := w.Snapshot(), w.Snapshot()
		siblingBytes := contractBytes(t, second)
		tamperBlueprint(&first.Blueprint)
		for i := range first.ConditionEffects {
			tamperEffect(&first.ConditionEffects[i])
		}
		contractEqual(t, siblingBytes, second)
		contractEqual(t, stateBytes, w.Snapshot())
		h1, h2 := w.History(), w.History()
		for i := range h1 {
			tamperBatch(&h1[i])
		}
		contractEqual(t, historyBytes, h2)
		contractEqual(t, historyBytes, w.History())
		for _, receipt := range h2 {
			want := contractBytes(t, receipt)
			a := commitC(t, w, receipt.Request)
			sibling := commitC(t, w, receipt.Request)
			tamperBatch(&a)
			contractEqual(t, want, sibling)
			contractEqual(t, want, commitC(t, w, receipt.Request))
		}
		contractFinal(t, w)
	}
	// NewWorld and the first Execute retain none of the caller's nested input.
	b := contractFixture(t)
	fresh := world(t, b)
	before := contractBytes(t, fresh.Snapshot())
	tamperBlueprint(&b)
	contractEqual(t, before, fresh.Snapshot())
	for _, batch := range live.History() {
		r := batch.Request
		input := contractBytes(t, r)
		got := commitC(t, fresh, r)
		contractEqual(t, input, r)
		want := contractBytes(t, got)
		tamperRequest(&r)
		tamperBatch(&got)
		original := live.History()[int(fresh.Revision())-1]
		contractEqual(t, want, commitC(t, fresh, original.Request))
	}
	contractFinal(t, fresh)
}

func TestContractProjectionSiblingOwnershipAndBorrowing(t *testing.T) {
	b := contractFixture(t)
	w := world(t, b)
	execute(t, w, "bless", `apply condition "a" of "steady" to "char:ada";`)
	state, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	p, _ := w.CharacterStatus("char:ada")
	sibling, _ := w.CharacterStatus("char:ada")
	want := contractBytes(t, sibling)
	p.Resources["hp"] = core.Resource{}
	p.Conditions[0].Instance = "tampered"
	for i := range p.ActiveEffects {
		tamperEffect(&p.ActiveEffects[i])
	}
	for i := range p.Stats {
		for j := range p.Stats[i].Contributions {
			c := &p.Stats[i].Contributions[j]
			c.Amount = 99
			if c.ConditionOrigin != nil {
				c.ConditionOrigin.Instance = "tampered"
			}
		}
	}
	contractEqual(t, want, sibling)
	ex, _ := w.Explain("char:ada", "defense")
	ex2, _ := w.Explain("char:ada", "defense")
	want = contractBytes(t, ex2)
	for i := range ex.Contributions {
		ex.Contributions[i].Amount = 99
		if ex.Contributions[i].ConditionOrigin != nil {
			ex.Contributions[i].ConditionOrigin.Instance = "tampered"
		}
	}
	contractEqual(t, want, ex2)
	tr, _ := w.TransferView("item:shield", "char:cy")
	tr2, _ := w.TransferView("item:shield", "char:cy")
	want = contractBytes(t, tr2)
	if e := tr.Validate(); e != nil {
		t.Fatal(e)
	}
	if _, e := tr.Assess(); e != nil {
		t.Fatal(e)
	}
	contractEqual(t, want, tr)
	*tr.Revision = 99
	tr.Item.Name = "tampered"
	tr.Owner.Value = "tampered"
	tr.Wearer.Value = "tampered"
	tr.Recipient.Name = "tampered"
	tr.Rules.Transfer = core.KeepsWorn
	tr.RequiredEffects[0] = "tampered"
	(*tr.Effects)[0].Amount = 99
	(*tr.WearerTotals)["defense"] = 99
	contractEqual(t, want, tr2)
	contractEqual(t, state, w.Snapshot())
	contractEqual(t, history, w.History())
	// Validation APIs borrow inputs, including on rejection; they do not normalize
	// the caller's data or reserve instance/request IDs.
	before := contractBytes(t, b)
	checks := []error{core.Validate(b), b.CheckRef(core.Ref{Kind: core.CharacterKind, ID: "char:ada"}), b.CheckStat("char:ada", "defense"), b.CheckResource("char:ada", "hp"), b.CheckEffectStatic(b.Effects[0]), core.CheckConditionDefinition(b.ConditionDefinitions[3]), b.CheckConditionStatic(core.ApplyCondition{Instance: "valid", Type: "marked", Target: "char:ada"})}
	for _, e := range checks {
		if e != nil {
			t.Fatal(e)
		}
	}
	contractEqual(t, before, b)
	o := applyC("bad", "blessing", "char:ada", nil)
	ob := contractBytes(t, o)
	if b.CheckOperationStatic(o) == nil {
		t.Fatal("missing magnitude accepted")
	}
	contractEqual(t, ob, o)
	contractEqual(t, before, b)
}

func TestContractMixedReplayTruthAndPositiveControls(t *testing.T) {
	genesis, w := contractJourney(t)
	contractFinal(t, w)
	h := w.History()
	expected := contractBytes(t, w.Snapshot())
	history := contractBytes(t, h)
	encoded, e := json.MarshalIndent(h, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	// Reorder object keys as well as whitespace at this transport-only boundary.
	// Fixture integers are small and exact even through JSON's generic numbers.
	var object any
	if e = json.Unmarshal(encoded, &object); e != nil {
		t.Fatal(e)
	}
	encoded, e = json.MarshalIndent(object, "", "\t")
	if e != nil {
		t.Fatal(e)
	}
	var transported []core.Batch
	if e = json.Unmarshal(encoded, &transported); e != nil {
		t.Fatal(e)
	}
	rebuilt, e := core.Replay(genesis, transported)
	if e != nil {
		t.Fatal(e)
	}
	contractEqual(t, expected, rebuilt.Snapshot())
	contractEqual(t, history, rebuilt.History())
	contractFinal(t, rebuilt)
	for _, v := range h {
		contractEqual(t, contractBytes(t, v), commitC(t, rebuilt, v.Request))
	}
	contractEqual(t, expected, rebuilt.Snapshot())
	contractEqual(t, history, rebuilt.History())
	conflict := rebuilt.History()[0].Request
	conflict.Operations[0].ApplyCondition.Target = "char:bea"
	for _, tc := range []struct {
		r    core.Request
		code string
	}{{conflict, "E_REQUEST_REUSE"}, {core.Request{ID: "stale", ExpectedRevision: 0, Actor: conflict.Actor, Operations: []core.Operation{applyC("new", "marked", "char:ada", nil)}}, "E_STALE"}, {typedRequest(rebuilt, "reserved", applyC("ephemeral", "marked", "char:ada", nil)), "E_DUPLICATE"}} {
		_, e := rebuilt.Execute(tc.r)
		var f *core.Fault
		if !errors.As(e, &f) || f.Code != tc.code {
			t.Fatal("request truth", tc.code, e)
		}
		contractEqual(t, expected, rebuilt.Snapshot())
		contractEqual(t, history, rebuilt.History())
	}
	// Rejected request ID is free; continuation remains reconstructible.
	commitC(t, rebuilt, typedRequest(rebuilt, "reserved", applyC("new", "marked", "char:ada", nil)))
	again, e := core.Replay(genesis, rebuilt.History())
	if e != nil {
		t.Fatal(e)
	}
	contractEqual(t, contractBytes(t, rebuilt.Snapshot()), again.Snapshot())
	for n := 0; n <= len(h); n++ {
		prefix, e := core.Replay(genesis, h[:n])
		if e != nil || prefix.Revision() != core.Revision(n) {
			t.Fatal("valid prefix", n, e)
		}
		if n == 3 { // identical visible active facts after ephemeral apply/remove,
			// but its reservation, revision and receipt are not yet historical truth.
			stat(t, prefix, "char:ada", "defense", 10)
			if len(prefix.Snapshot().RetiredConditionIDs) != 3 {
				t.Fatal("prefix lifetime")
			}
			beforeCycle, afterCycle := prefix.Snapshot(), w.Snapshot()
			afterCycle.Revision = beforeCycle.Revision
			afterCycle.RetiredConditionIDs = beforeCycle.RetiredConditionIDs
			if !reflect.DeepEqual(beforeCycle, afterCycle) {
				t.Fatal("ephemeral transaction changed unrelated active facts")
			}
			commitC(t, prefix, h[3].Request)
			contractEqual(t, expected, prefix.Snapshot())
		}
	}
	// A different coherent record is valid: consistency does not authenticate the
	// author's choices, nor prove that a prefix contains every committed batch.
	alternative := world(t, genesis)
	r := w.History()[0].Request
	*r.Operations[0].ApplyCondition.Magnitude = 3
	commitC(t, alternative, r)
	stat(t, alternative, "char:ada", "defense", 17)
	a, e := core.Replay(genesis, alternative.History())
	if e != nil {
		t.Fatal("coherent alternative", e)
	}
	stat(t, a, "char:ada", "defense", 17)
}

func TestContractReplayInconsistencyIsAllOrNothing(t *testing.T) {
	genesis, live := contractJourney(t)
	state, original := contractBytes(t, live.Snapshot()), contractBytes(t, live.History())
	cases := []struct {
		name   string
		change func([]core.Batch)
	}{
		{"retained intent", func(h []core.Batch) { h[0].Request.Operations[3].Damage.Amount++ }},
		{"event kind", func(h []core.Batch) { h[0].Events[3].Kind = core.HealingApplied }},
		{"extra payload", func(h []core.Batch) { h[0].Events[3].Movement = &core.MovementChange{} }},
		{"resource before", func(h []core.Batch) { h[0].Events[3].Resource.Before.Current++ }},
		{"rule unequip omitted", func(h []core.Batch) { h[0].Events = append(h[0].Events[:4], h[0].Events[5:]...) }},
		{"operation grouping", func(h []core.Batch) { h[0].Events[5].Operation++ }},
		{"late revision", func(h []core.Batch) { h[3].Revision++ }},
		{"late actor", func(h []core.Batch) { h[3].Events[1].Actor.ID = "char:bea" }},
		{"late cause", func(h []core.Batch) { h[3].Events[1].Condition.Before.Cause.Operation++ }},
		{"late request duplicate", func(h []core.Batch) { h[3].Request.ID = h[0].Request.ID }},
		{"consequence identity", func(h []core.Batch) { h[0].Events[0].Condition.AfterEffects[0].ID = "wrong" }},
		{"consequence amount", func(h []core.Batch) { h[0].Events[0].Condition.AfterEffects[0].Amount++ }},
		{"consequence null shape", func(h []core.Batch) { h[3].Events[1].Condition.BeforeEffects = []core.Effect{} }},
		{"relationship before", func(h []core.Batch) { h[0].Events[8].Relationship.Before.Strength++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := live.History()
			tc.change(h)
			hb, gb := contractBytes(t, h), contractBytes(t, genesis)
			w, e := core.Replay(genesis, h)
			var fault *core.Fault
			if w != nil || !errors.As(e, &fault) || fault.Code != "E_REPLAY" {
				t.Fatal("inconsistency escaped", w, e)
			}
			contractEqual(t, hb, h)
			contractEqual(t, gb, genesis)
			contractEqual(t, state, live.Snapshot())
			contractEqual(t, original, live.History())
		})
	}
	// Two contributors make order corruption observable; a single-effect fixture
	// cannot falsify this part of exact typed event comparison.
	w := world(t, genesis)
	execute(t, w, "ordered", `apply condition "steady" of "steady" to "char:ada";`)
	h := w.History()
	effects := h[0].Events[0].Condition.AfterEffects
	if len(effects) != 2 || effects[0].ConditionOrigin.Modifier != "ward" || effects[1].ConditionOrigin.Modifier != "resolve" {
		t.Fatal("authored consequence order")
	}
	effects[0], effects[1] = effects[1], effects[0]
	if partial, e := core.Replay(genesis, h); e == nil || partial != nil {
		t.Fatal("reordered consequences accepted")
	}
}

func TestContractEmptyRepresentationsAndSharedInputLeaves(t *testing.T) {
	for _, empty := range []bool{false, true} {
		b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}}
		if empty {
			b.ConditionDefinitions = []core.ConditionDefinition{}
			b.Conditions = []core.ConditionInstance{}
			b.RetiredConditionIDs = []core.ConditionInstanceID{}
		}
		w := world(t, b)
		s := w.Snapshot()
		if s.Characters == nil || s.Items == nil || s.Locations == nil || s.Effects == nil || s.Relationships == nil || s.ConditionEffects != nil {
			t.Fatal("M1 empty normalization")
		}
		if (s.ConditionDefinitions != nil) != empty || (s.Conditions != nil) != empty || (s.RetiredConditionIDs != nil) != empty {
			t.Fatal("Condition nil/empty preservation")
		}
	}
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "l"}}, Characters: []core.Character{{ID: "c", Location: "l"}}, Items: []core.Item{{ID: "i"}}}
	w := world(t, b)
	s := w.Snapshot()
	status, e := w.CharacterStatus("c")
	if e != nil || s.Characters[0].Stats == nil || s.Characters[0].Resources == nil || status.Resources == nil || status.Stats == nil || status.ActiveEffects == nil || status.Conditions != nil {
		t.Fatal("empty character/projection shape", e)
	}
	tr, e := w.TransferView("i", "c")
	if e != nil || tr.RequiredEffects == nil || tr.Effects == nil || *tr.Effects == nil || tr.WearerTotals == nil || *tr.WearerTotals == nil {
		t.Fatal("empty transfer shape", e)
	}
	b.Characters[0].Stats = map[core.StatID]int64{"s": 1}
	b.ConditionDefinitions = []core.ConditionDefinition{{ID: "nil", Instances: core.Coexist}, {ID: "empty", Instances: core.Coexist, Modifiers: []core.ConditionModifier{}}}
	w = world(t, b)
	ex, e := w.Explain("c", "s")
	if e != nil || ex.Contributions == nil || len(ex.Contributions) != 0 || w.Snapshot().ConditionDefinitions[0].Modifiers == nil || w.Snapshot().ConditionDefinitions[1].Modifiers != nil {
		t.Fatal("empty modifier/contributor shape", e)
	} // definitions sort as empty, nil
	// The caller may share one fixed-amount pointer between templates. Retained
	// templates and separately returned definitions still own independent leaves.
	b = contractFixture(t)
	fixed := int64(2)
	b.ConditionDefinitions[3].Modifiers[0].Fixed = &fixed
	b.ConditionDefinitions[3].Modifiers[1].Fixed = &fixed
	w = world(t, b)
	before := contractBytes(t, w.Snapshot())
	fixed = 99
	contractEqual(t, before, w.Snapshot())
	s = w.Snapshot()
	for i := range s.ConditionDefinitions {
		if s.ConditionDefinitions[i].ID == "steady" {
			mods := s.ConditionDefinitions[i].Modifiers
			*mods[0].Fixed = 88
			if *mods[1].Fixed != 2 {
				t.Fatal("shared input leaf survived into detached definitions")
			}
		}
	}
	contractEqual(t, before, w.Snapshot())
}
