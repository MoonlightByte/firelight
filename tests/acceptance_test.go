package tests

import (
	"encoding/json"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixture(t testing.TB) core.Blueprint {
	t.Helper()
	src, e := os.ReadFile("../examples/micro-world.nql")
	if e != nil {
		t.Fatal(e)
	}
	b, ds := compiler.CompileWorld("micro-world.nql", string(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}
func world(t testing.TB, b core.Blueprint) *core.World {
	t.Helper()
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func request(t testing.TB, w *core.World, id, src string) core.Request {
	t.Helper()
	s := w.Snapshot()
	ops, ds := compiler.CompileActions("action.nql", src, s.Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops}
}
func execute(t testing.TB, w *core.World, id, src string) core.Batch {
	t.Helper()
	r := request(t, w, id, src)
	b, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func stat(t testing.TB, w *core.World, id core.CharacterID, s core.StatID, want int64) {
	t.Helper()
	e, err := w.Explain(id, s)
	if err != nil {
		t.Fatal(err)
	}
	if e.Effective != want {
		t.Fatalf("%s.%s = %d, want %d (%+v)", id, s, e.Effective, want, e)
	}
}
func unchanged(t testing.TB, w *core.World, before core.Snapshot, history []core.Batch) {
	t.Helper()
	if !reflect.DeepEqual(w.Snapshot(), before) || !reflect.DeepEqual(w.History(), history) {
		t.Fatal("failed request changed canonical state/history/revision")
	}
}
func item(t testing.TB, w *core.World, id core.ItemID) core.Item {
	t.Helper()
	for _, i := range w.Snapshot().Items {
		if i.ID == id {
			return i
		}
	}
	t.Fatal("item missing")
	return core.Item{}
}

func TestMicroWorldIdentityAndFixedOracles(t *testing.T) {
	w := world(t, fixture(t))
	s := w.Snapshot()
	if len(s.Characters) != 3 || len(s.Items) != 10 || len(s.Locations) != 2 || len(s.Relationships) != 2 {
		t.Fatal("fixture cardinality")
	}
	if s.Characters[0].Name != s.Characters[1].Name || s.Characters[0].ID == s.Characters[1].ID {
		t.Fatal("duplicate display name identity test ineffective")
	}
	stat(t, w, "char:bea", "defense", 15)
	stat(t, w, "char:ada", "resolve", 9)
	ex, e := w.Explain("char:bea", "defense")
	if e != nil {
		t.Fatal(e)
	}
	if ex.Base != 10 || len(ex.Contributions) != 2 || ex.Contributions[0].Effect != "effect:blessing" || ex.Contributions[1].Effect != "effect:shield" {
		t.Fatalf("lost provenance: %+v", ex)
	}
	execute(t, w, "remove-shield", `remove effect "effect:shield";`)
	stat(t, w, "char:bea", "defense", 13)
	ex, _ = w.Explain("char:bea", "defense")
	if len(ex.Contributions) != 1 || ex.Contributions[0].Effect != "effect:blessing" {
		t.Fatal("removal destroyed independent contributor")
	}
}

func TestIntegratedJourneyAndReplay(t *testing.T) {
	b := fixture(t)
	w := world(t, b)
	src, e := os.ReadFile("../examples/journey.nql")
	if e != nil {
		t.Fatal(e)
	}
	result := execute(t, w, "journey", string(src))
	wantKinds := []core.EventKind{core.ItemUnequipped, core.ItemTransferred, core.ItemEquipped, core.DamageApplied, core.HealingApplied, core.EffectApplied, core.EffectRemoved, core.EffectExpired, core.EntityMoved, core.RelationshipChanged, core.ItemUnequipped}
	if len(result.Events) != len(wantKinds) || result.Revision != 1 {
		t.Fatal("incorrect atomic event count/revision")
	}
	for n, k := range wantKinds {
		ev := result.Events[n]
		if ev.Kind != k || ev.ID != fmt.Sprintf("event:1:%d", n+1) || ev.Revision != 1 || ev.RequestID != "journey" || ev.Actor.ID != "char:ada" {
			t.Fatalf("event %d: %+v", n, ev)
		}
	}
	if *result.Events[0].Wearing != (core.WearingChange{Item: "item:shield:001", Before: "char:bea", After: "", Reason: "transfer rule"}) {
		t.Fatal("wrong rule-driven unequip facts")
	}
	if *result.Events[1].Ownership != (core.OwnershipChange{Item: "item:shield:001", Before: "char:ada", After: "char:cy"}) {
		t.Fatal("wrong ownership facts")
	}
	if result.Events[3].Resource.Before.Current != 20 || result.Events[3].Resource.After.Current != 13 || result.Events[4].Resource.After.Current != 16 {
		t.Fatal("resource event oracle")
	}
	shield := item(t, w, "item:shield:001")
	if shield.Owner != "char:cy" || shield.Wearer != "" {
		t.Fatal("final ownership/wearing")
	}
	p, e := w.CharacterStatus("char:ada")
	if e != nil {
		t.Fatal(e)
	}
	if p.Location.ID != "loc:garden" || p.Resources["hp"].Current != 16 {
		t.Fatal("final location/resource")
	}
	stat(t, w, "char:ada", "defense", 10)
	stat(t, w, "char:bea", "defense", 10)
	for _, r := range w.Snapshot().Relationships {
		if r.Kind == "trust" && r.Strength != 5 {
			t.Fatal("relationship update")
		}
		if r.Kind == "secret_debt" && r.Strength != 417 {
			t.Fatal("unrelated relationship changed")
		}
	}
	// JSON is only transport: every typed event/request payload survives roundtrip.
	encoded, e := json.Marshal(w.History())
	if e != nil {
		t.Fatal(e)
	}
	var log []core.Batch
	if e = json.Unmarshal(encoded, &log); e != nil {
		t.Fatal(e)
	}
	replayed, e := core.Replay(b, log)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(replayed.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(replayed.History(), w.History()) {
		t.Fatal("full M1 event replay differs")
	}
	retry, e := replayed.Execute(result.Request)
	if e != nil || !reflect.DeepEqual(retry, result) || replayed.Revision() != 1 {
		t.Fatal("replay lost idempotency receipt", e)
	}
	execute(t, replayed, "continued", `heal "char:ada" resource "hp" by 2;`)
	if replayed.Revision() != 2 {
		t.Fatal("cannot continue after replay")
	}
	again, e := core.Replay(b, replayed.History())
	if e != nil || !reflect.DeepEqual(again.Snapshot(), replayed.Snapshot()) {
		t.Fatal("continued history no longer replayable", e)
	}
	w2 := world(t, b)
	execute(t, w2, "journey", string(src))
	if !reflect.DeepEqual(w2.History(), w.History()) {
		t.Fatal("sequence not deterministic")
	}
}

func TestAuthoredRulesAndIndependentWearing(t *testing.T) {
	for _, policy := range []core.TransferPolicy{core.Unequips, core.KeepsWorn} {
		t.Run(string(policy), func(t *testing.T) {
			b := fixture(t)
			b.Rules.Transfer = policy
			w := world(t, b)
			execute(t, w, "give", `transfer "item:shield:001" from "char:ada" to "char:cy";`)
			i := item(t, w, "item:shield:001")
			if i.Owner != "char:cy" {
				t.Fatal("owner not transferred")
			}
			if policy == core.KeepsWorn {
				if i.Wearer != "char:bea" {
					t.Fatal("wearer was incorrectly coupled to owner")
				}
				stat(t, w, "char:bea", "defense", 15)
			} else {
				if i.Wearer != "" {
					t.Fatal("unequip rule ignored")
				}
				stat(t, w, "char:bea", "defense", 13)
				execute(t, w, "wear", `equip "item:shield:001" on "char:ada";`)
				stat(t, w, "char:ada", "defense", 12)
			}
		})
	}
	b := fixture(t)
	b.Rules.Wear = core.OwnerOnly
	b.Rules.Transfer = core.KeepsWorn
	b.Items[0].Wearer = b.Items[0].Owner
	w := world(t, b)
	for _, src := range []string{`transfer "item:shield:001" from "char:ada" to "char:cy"; unequip "item:shield:001";`, `unequip "item:shield:001"; equip "item:shield:001" on "char:bea";`} {
		s, h := w.Snapshot(), w.History()
		if _, e := w.Execute(request(t, w, "bad", src)); e == nil {
			t.Fatal("owner_only accepted illegal transition")
		}
		unchanged(t, w, s, h)
	}
	execute(t, w, "legal", `unequip "item:shield:001"; transfer "item:shield:001" from "char:ada" to "char:cy"; equip "item:shield:001" on "char:cy";`)
	if i := item(t, w, "item:shield:001"); i.Owner != "char:cy" || i.Wearer != "char:cy" {
		t.Fatal("owner_only legal path failed")
	}
}

func TestAtomicFailureAndLateRetry(t *testing.T) {
	w := world(t, fixture(t))
	s, h := w.Snapshot(), w.History()
	bad := request(t, w, "retryable", `damage "char:ada" resource "hp" by 7; transfer "item:shield:001" from "char:bea" to "char:cy";`)
	if _, e := w.Execute(bad); e == nil {
		t.Fatal("incorrect owner accepted")
	}
	unchanged(t, w, s, h)
	a := execute(t, w, "retryable", `damage "char:ada" resource "hp" by 2;`)
	execute(t, w, "b", `heal "char:ada" resource "hp" by 1;`)
	s, h = w.Snapshot(), w.History()
	again, e := w.Execute(a.Request)
	if e != nil || !reflect.DeepEqual(again, a) {
		t.Fatal("late identical retry failed", e)
	}
	unchanged(t, w, s, h)
	a.Request.Operations[0].Damage.Amount = 3
	if _, e = w.Execute(a.Request); e == nil {
		t.Fatal("changed request ID reuse accepted")
	}
	unchanged(t, w, s, h)
	stale := request(t, w, "stale", `heal "char:ada" resource "hp" by 1;`)
	stale.ExpectedRevision = 0
	if _, e = w.Execute(stale); e == nil {
		t.Fatal("stale publication accepted")
	}
	unchanged(t, w, s, h)
}

func TestNumericResourceBounds(t *testing.T) {
	w := world(t, fixture(t))
	execute(t, w, "damage", `damage "char:ada" resource "hp" by 9223372036854775807;`)
	p, _ := w.CharacterStatus("char:ada")
	if p.Resources["hp"].Current != 0 {
		t.Fatal("damage did not clamp")
	}
	b := execute(t, w, "heal", `heal "char:ada" resource "hp" by 9223372036854775807;`)
	if b.Events[0].Resource.After.Current != 30 {
		t.Fatal("healing did not clamp")
	}
	b = execute(t, w, "at-bound", `heal "char:ada" resource "hp" by 1;`)
	if b.Events[0].Resource.Before != b.Events[0].Resource.After || b.Events[0].Resource.Requested != 1 {
		t.Fatal("zero actual change not explicit")
	}
	for _, amount := range []int64{0, -1, math.MinInt64} {
		s, h := w.Snapshot(), w.History()
		r := core.Request{ID: "invalid", ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: amount}}}}
		if _, e := w.Execute(r); e == nil {
			t.Fatal("nonpositive amount accepted")
		}
		unchanged(t, w, s, h)
	}
	edges := fixture(t)
	edges.Characters[0].Resources["hp"] = core.Resource{Current: math.MaxInt64, Minimum: math.MinInt64, Maximum: math.MaxInt64}
	w = world(t, edges)
	execute(t, w, "edge1", `damage "char:ada" resource "hp" by 9223372036854775807;`)
	p, _ = w.CharacterStatus("char:ada")
	if p.Resources["hp"].Current != 0 {
		t.Fatal("wide-range damage")
	}
	execute(t, w, "edge2", `damage "char:ada" resource "hp" by 9223372036854775807; damage "char:ada" resource "hp" by 2;`)
	p, _ = w.CharacterStatus("char:ada")
	if p.Resources["hp"].Current != math.MinInt64 {
		t.Fatal("minimum overflow clamp")
	}
}

func overflowFixture(t testing.TB) core.Blueprint {
	b := fixture(t)
	b.Characters[1].Stats["defense"] = math.MaxInt64
	for i := range b.Effects {
		if b.Effects[i].ID == "effect:shield" {
			b.Effects[i].Amount = -1
		}
		if b.Effects[i].ID == "effect:blessing" {
			b.Effects[i].Amount = 1
		}
	}
	return b
}
func TestExactDerivedArithmeticAndRemovalOverflow(t *testing.T) {
	b := overflowFixture(t)
	w := world(t, b)
	stat(t, w, "char:bea", "defense", math.MaxInt64)
	for i, j := 0, len(b.Effects)-1; i < j; i, j = i+1, j-1 {
		b.Effects[i], b.Effects[j] = b.Effects[j], b.Effects[i]
	}
	stat(t, world(t, b), "char:bea", "defense", math.MaxInt64)
	for _, src := range []string{`unequip "item:shield:001";`, `remove effect "effect:shield";`, `expire effect "effect:shield";`, `transfer "item:shield:001" from "char:ada" to "char:cy";`} {
		s, h := w.Snapshot(), w.History()
		if _, e := w.Execute(request(t, w, "overflow", src)); e == nil {
			t.Fatal("derived overflow published")
		}
		unchanged(t, w, s, h)
	}
	// Numeric bounds are checked on the final candidate, not effect list order.
	execute(t, w, "cancel", `remove effect "effect:shield"; remove effect "effect:blessing";`)
	stat(t, w, "char:bea", "defense", math.MaxInt64)
}

func TestProjectionNegativeControlsAndFreshness(t *testing.T) {
	w := world(t, fixture(t))
	p, e := w.TransferView("item:shield:001", "char:cy")
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Validate(); e != nil {
		t.Fatal(e)
	}
	a, e := p.Assess()
	if e != nil || !a.Allowed {
		t.Fatal("valid standalone projection", e)
	}
	for _, remove := range []func(*core.TransferProjection){func(p *core.TransferProjection) { p.Owner = nil }, func(p *core.TransferProjection) { p.Rules = nil }, func(p *core.TransferProjection) { p.Effects = nil }, func(p *core.TransferProjection) { delete(*p.WearerTotals, "defense") }, func(p *core.TransferProjection) { *p.Effects = (*p.Effects)[:0] }} {
		q, _ := w.TransferView("item:shield:001", "char:cy")
		remove(&q)
		if q.Validate() == nil {
			t.Fatal("under-included projection accepted")
		}
	}
	status, _ := w.CharacterStatus("char:bea")
	full, _ := json.Marshal(w.Snapshot())
	narrow, _ := json.Marshal(p)
	character, _ := json.Marshal(status)
	if !strings.Contains(string(full), "secret_debt") || strings.Contains(string(narrow), "secret_debt") || strings.Contains(string(character), "secret_debt") {
		t.Fatal("hidden relationship negative control ineffective")
	}
	if strings.Contains(string(narrow), "resources") || strings.Contains(string(narrow), "char:ada\",\"name") {
		t.Fatal("unrelated task facts over-included")
	}
	r := request(t, w, "old-plan", `transfer "item:shield:001" from "char:ada" to "char:cy";`)
	execute(t, w, "change", `damage "char:bea" resource "hp" by 1;`)
	newP, _ := w.TransferView("item:shield:001", "char:cy")
	if *p.Revision != 0 || *newP.Revision != 1 || status.Revision != 0 {
		t.Fatal("projections not historical values")
	}
	if _, e := w.Execute(r); e == nil {
		t.Fatal("stale projection plan accepted")
	}
	*newP.Owner = core.CharacterFact{Value: "char:cy"}
	(*newP.Effects)[0].Amount = 999
	(*newP.WearerTotals)["defense"] = 0
	status.Resources["hp"] = core.Resource{}
	stat(t, w, "char:bea", "defense", 15)
	if item(t, w, "item:shield:001").Owner != "char:ada" {
		t.Fatal("projection became writable authority")
	}
	ow := world(t, overflowFixture(t))
	op, _ := ow.TransferView("item:shield:001", "char:cy")
	decision, e := op.Assess()
	if e != nil || decision.Allowed {
		t.Fatal("overflow projection failed to detect illegal transfer", e)
	}
}

func TestEffectAttachmentAndDynamicWearer(t *testing.T) {
	b := fixture(t)
	b.Effects = append(b.Effects, core.Effect{ID: "effect:gift", Source: core.Ref{Kind: core.CharacterKind, ID: "char:cy"}, Mode: core.WhileWorn, Target: core.Ref{Kind: core.ItemKind, ID: "item:shield:001"}, Stat: "defense", Amount: 1})
	w := world(t, b)
	stat(t, w, "char:bea", "defense", 16)
	p, _ := w.TransferView("item:shield:001", "char:cy")
	if len(*p.Effects) != 2 {
		t.Fatal("character-sourced worn effect missing from item projection")
	}
	execute(t, w, "change-wearer", `unequip "item:shield:001"; equip "item:shield:001" on "char:ada";`)
	stat(t, w, "char:ada", "defense", 13)
	stat(t, w, "char:bea", "defense", 13)
	// Applying an inactive effect defers compatibility until equip.
	execute(t, w, "inactive", `apply effect "effect:rare" from character "char:cy" on worn "item:blade:003" stat "rare" add 1;`)
	s, h := w.Snapshot(), w.History()
	if _, e := w.Execute(request(t, w, "wrong-stat", `equip "item:blade:003" on "char:ada";`)); e == nil {
		t.Fatal("incompatible worn stat accepted")
	}
	unchanged(t, w, s, h)
	b = fixture(t)
	b.Characters[0].Stats["rare"] = 7
	w = world(t, b)
	execute(t, w, "dynamic", `unequip "item:shield:001"; equip "item:shield:001" on "char:ada"; apply effect "effect:rare" from character "char:cy" on worn "item:shield:001" stat "rare" add 1;`)
	stat(t, w, "char:ada", "rare", 8)
}

func TestConcurrentPublicationAndCopiedHandle(t *testing.T) {
	w := world(t, fixture(t))
	copyHandle := *w
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			handle := w
			if i%2 == 0 {
				handle = &copyHandle
			}
			r := core.Request{ID: fmt.Sprintf("race:%d", i), ExpectedRevision: 0, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{{Damage: &core.ResourceOp{Character: "char:ada", Resource: "hp", Amount: 1}}}}
			if _, e := handle.Execute(r); e == nil {
				successes.Add(1)
			}
			_ = handle.Snapshot()
			_, _ = handle.CharacterStatus("char:ada")
			_ = handle.History()
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 || w.Revision() != 1 || copyHandle.Revision() != 1 || len(w.History()) != 1 {
		t.Fatal("competing revisions or copied handle bypassed one authority")
	}
	p, _ := w.CharacterStatus("char:ada")
	if p.Resources["hp"].Current != 19 {
		t.Fatal("lost/corrupted concurrent resource mutation")
	}
}
