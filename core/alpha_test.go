package core

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func alphaPtr[T any](v T) *T { return &v }
func alphaChar(id CharacterID) AbilityOperand {
	return AbilityOperand{Literal: &AbilityValue{Character: &id}}
}
func alphaInt(v int64) AbilityOperand          { return AbilityOperand{Literal: &AbilityValue{Integer: &v}} }
func alphaParam(id ParameterID) AbilityOperand { return AbilityOperand{Parameter: id} }
func alphaFixture(seed [2]uint64) Blueprint {
	actor, target := alphaParam("actor"), alphaParam("target")
	policy := func(phase ActionPhase) *ActionPolicy {
		return &ActionPolicy{Actor: actor, Phase: phase, Requirements: []Requirement{}}
	}
	params := []AbilityParameter{{ID: "actor", Kind: CharacterArgument}, {ID: "target", Kind: CharacterArgument}}
	strike := AbilityDefinition{ID: "strike", Name: "Strike", Parameters: params, Action: policy(EncounterPhase), Steps: []AbilityStep{
		{Spend: &SpendTemplate{Payer: actor, Resource: "focus", Amount: alphaInt(2)}},
		{Check: &CheckTemplate{ID: "attack", Sides: 20, Modifier: CheckValue{Stat: &StatOperand{Character: actor, Stat: "attack"}}, Threshold: CheckValue{Stat: &StatOperand{Character: target, Stat: "defense"}}, Success: []AbilityPrimitive{{Damage: &DamageTemplate{Target: target, Resource: "hp", Amount: alphaInt(4)}}}, Failure: []AbilityPrimitive{}}},
	}}
	strike.Action.Requirements = []Requirement{{Kind: AboveMinimum, Subject: target, Resource: "hp"}, {Kind: Hostile, Subject: actor, Other: &target}, {Kind: Colocated, Subject: actor, Other: &target}}
	pass := AbilityDefinition{ID: "pass", Name: "Pass", Parameters: params[:1], Action: policy(EncounterPhase), Steps: []AbilityStep{}}
	guard := AbilityDefinition{ID: "guard", Name: "Guard", Parameters: []AbilityParameter{{ID: "actor", Kind: CharacterArgument}, {ID: "id", Kind: ConditionIDArgument}}, Action: policy(EncounterPhase), Steps: []AbilityStep{{Spend: &SpendTemplate{Payer: actor, Resource: "focus", Amount: alphaInt(1)}}, {ApplyCondition: &ConditionTemplate{Instance: alphaParam("id"), Type: "guarded", Target: actor, Duration: alphaPtr(uint64(1))}}}}
	b := Blueprint{Rules: Rules{Transfer: Unequips, Wear: WearAny}, Locations: []Location{{ID: "field"}, {ID: "home"}}, Items: []Item{{ID: "relic", Owner: "enemy"}, {ID: "shield", Owner: "a", Wearer: "a"}}, Effects: []Effect{{ID: "shield-bonus", Source: Ref{ItemKind, "shield"}, Mode: WhileWorn, Target: Ref{ItemKind, "shield"}, Stat: "defense", Amount: 2}}, ConditionDefinitions: []ConditionDefinition{{ID: "guarded", Instances: Unique, Modifiers: []ConditionModifier{{ID: "defense", Stat: "defense", Fixed: alphaPtr(int64(3))}}}, {ID: "marked", Instances: Coexist, Modifiers: []ConditionModifier{}}}, AbilityDefinitions: []AbilityDefinition{strike, pass, guard}}
	for _, id := range []CharacterID{"a", "b", "c", "enemy"} {
		hp := int64(10)
		if id == "enemy" {
			hp = 6
		}
		b.Characters = append(b.Characters, Character{ID: id, Name: string(id), Location: "field", Stats: map[StatID]int64{"attack": 4, "defense": 12}, Resources: map[ResourceID]Resource{"hp": {Current: hp, Minimum: 0, Maximum: hp}, "focus": {Current: 3, Minimum: 0, Maximum: 6}}})
	}
	b.Adventure = &Adventure{Definition: AdventureDefinition{Party: []CharacterID{"a", "b", "c"}, Health: "hp", Links: []LocationLink{{From: "field", To: "home"}, {From: "home", To: "field"}}, Encounters: []EncounterDefinition{{ID: "enc", Location: "field", Order: []Participant{{Character: "a", Team: PartyTeam}, {Character: "b", Team: PartyTeam}, {Character: "c", Team: PartyTeam}, {Character: "enemy", Team: HostileTeam}}}}, Objectives: []ObjectiveDefinition{{ID: "goal", Item: "relic", Custodian: "enemy", Victory: "enc", Destination: "home"}}, InitialEncounter: "enc", Seed: seed}}
	for _, id := range []CharacterID{"a", "b", "c", "enemy"} {
		b.Adventure.Definition.Grants = append(b.Adventure.Definition.Grants, AbilityGrant{Character: id, Abilities: []AbilityID{"strike", "pass", "guard"}})
	}
	return b
}
func alphaWorld(t testing.TB, b Blueprint) *World {
	t.Helper()
	w, e := NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func alphaRequest(w *World, id string, actor CharacterID, ability AbilityID, target CharacterID, condition ConditionInstanceID) Request {
	v := InvokeAbility{Ability: ability, Arguments: []AbilityArgument{{Name: "actor", Value: AbilityValue{Character: &actor}}}}
	if target != "" {
		v.Arguments = append(v.Arguments, AbilityArgument{Name: "target", Value: AbilityValue{Character: &target}})
	}
	if condition != "" {
		v.Arguments = append(v.Arguments, AbilityArgument{Name: "id", Value: AbilityValue{ConditionID: &condition}})
	}
	return Request{ID: id, ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, string(actor)}, Operations: []Operation{{Invoke: &v}}}
}
func alphaCommit(t testing.TB, w *World, r Request) Batch {
	t.Helper()
	x, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func alphaReject(t testing.TB, w *World, r Request, code string) {
	t.Helper()
	before := w.ExportCheckpoint()
	_, e := w.Execute(r)
	var f *Fault
	if !errors.As(e, &f) || f.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("rejected candidate changed authority/history")
	}
}
func TestAlphaPaidHitMissAndRetry(t *testing.T) {
	// These vectors were frozen from a separate standard-library-only executable.
	for _, tc := range []struct {
		seed [2]uint64
		draw uint64
		hp   int64
	}{{[2]uint64{1, 2}, 16, 2}, {[2]uint64{5, 6}, 6, 6}} {
		t.Run(fmt.Sprint(tc.seed), func(t *testing.T) {
			w := alphaWorld(t, alphaFixture(tc.seed))
			r := alphaRequest(w, "R1", "a", "strike", "enemy", "")
			receipt := alphaCommit(t, w, r)
			s := w.Snapshot()
			if character(&s.Blueprint, "a").Resources["focus"].Current != 1 || character(&s.Blueprint, "enemy").Resources["hp"].Current != tc.hp || s.Adventure.State.Random.Calls != 1 || s.Adventure.State.Encounters[0].Cursor != 1 {
				t.Fatal(s)
			}
			if receipt.Events[0].Kind != ResourceSpent || receipt.Events[1].Check.Draw != tc.draw || receipt.Events[len(receipt.Events)-2].Kind != TurnAdvanced || receipt.Events[len(receipt.Events)-1].Kind != AbilityInvoked {
				t.Fatal(receipt)
			}
			before := w.ExportCheckpoint()
			if !reflect.DeepEqual(receipt, alphaCommit(t, w, r)) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("retry charged/rolled/advanced")
			}
			replay, e := Replay(before.Genesis, before.History)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, replay.ExportCheckpoint()) || !reflect.DeepEqual(receipt, alphaCommit(t, replay, r)) {
				t.Fatal("replay continuation/receipt mismatch")
			}
		})
	}
}
func TestAlphaEligibilityAndLateRollback(t *testing.T) {
	w := alphaWorld(t, alphaFixture([2]uint64{1, 2}))
	alphaReject(t, w, alphaRequest(w, "wrong", "b", "strike", "enemy", ""), "E_ELIGIBILITY")
	wrong := alphaRequest(w, "actor", "a", "strike", "enemy", "")
	wrong.Actor.ID = "b"
	alphaReject(t, w, wrong, "E_ELIGIBILITY")
	alphaReject(t, w, alphaRequest(w, "ally", "a", "strike", "b", ""), "E_ELIGIBILITY")
	r := alphaRequest(w, "late", "a", "strike", "enemy", "")
	r.Operations = append(r.Operations, Operation{Spend: &ResourceOp{Character: "a", Resource: "focus", Amount: 2}})
	alphaReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
	r.Operations = r.Operations[:1]
	alphaCommit(t, w, r)
	alphaCommit(t, w, alphaRequest(w, "B", "b", "pass", "", ""))
	alphaCommit(t, w, alphaRequest(w, "C", "c", "pass", "", ""))
	alphaCommit(t, w, alphaRequest(w, "E", "enemy", "pass", "", ""))
	alphaReject(t, w, alphaRequest(w, "low", "a", "strike", "enemy", ""), "E_INSUFFICIENT_RESOURCE")
	alphaCommit(t, w, alphaRequest(w, "continue", "a", "pass", "", ""))
}
func TestAlphaLifetimeBoundariesAndTerminal(t *testing.T) {
	w := alphaWorld(t, alphaFixture([2]uint64{1, 2}))
	alphaCommit(t, w, alphaRequest(w, "guard", "a", "guard", "", "guard:1"))
	x, e := w.Explain("a", "defense")
	if e != nil || x.Effective != 17 || len(x.Contributions) != 2 {
		t.Fatalf("%+v %v", x, e)
	}
	for _, id := range []CharacterID{"b", "c"} {
		alphaCommit(t, w, alphaRequest(w, string(id), id, "pass", "", ""))
	}
	receipt := alphaCommit(t, w, alphaRequest(w, "end", "enemy", "pass", "", ""))
	if receipt.Events[0].Kind != ConditionRemoved || receipt.Events[0].Condition.Expiration.Reason != "round end" || receipt.Events[0].Condition.Before.Cause.RequestID != "guard" || receipt.Events[1].Kind != TurnAdvanced {
		t.Fatal(receipt)
	}
	s := w.Snapshot()
	if len(s.Conditions) != 0 || !reflect.DeepEqual(s.RetiredConditionIDs, []ConditionInstanceID{"guard:1"}) || s.Adventure.State.Encounters[0].Round != 2 {
		t.Fatal(s)
	}
	x, _ = w.Explain("a", "defense")
	if x.Effective != 14 {
		t.Fatal(x)
	}
	// A terminal last-slot action cleans scoped conditions without a round wrap.
	b := alphaFixture([2]uint64{1, 2})
	character(&b, "enemy").Resources["hp"] = Resource{Current: 4, Maximum: 6}
	w = alphaWorld(t, b)
	alphaCommit(t, w, alphaRequest(w, "guard2", "a", "guard", "", "guard:2"))
	receipt = alphaCommit(t, w, alphaRequest(w, "win", "b", "strike", "enemy", ""))
	if p := w.Snapshot().Adventure.State.Encounters[0]; p.Status != Victory || p.Round != 1 || p.Cursor != 1 {
		t.Fatal(p)
	}
	es := receipt.Events
	n := len(es)
	if es[n-3].Kind != ConditionRemoved || es[n-3].Condition.Expiration.Reason != "encounter end" || es[n-2].Kind != EncounterEnded || es[n-1].Kind != AbilityInvoked {
		t.Fatal(es)
	}
}
func TestAlphaManualRemovalBranchIdentityAndDetachment(t *testing.T) {
	b := alphaFixture([2]uint64{5, 6})
	d := abilityDefinition(&b, "strike")
	d.Parameters = append(d.Parameters, AbilityParameter{ID: "id", Kind: ConditionIDArgument})
	d.Steps[1].Check.Success = append(d.Steps[1].Check.Success, AbilityPrimitive{ApplyCondition: &ConditionTemplate{Instance: alphaParam("id"), Type: "marked", Target: alphaParam("target"), Duration: alphaPtr(uint64(2))}})
	w := alphaWorld(t, b)
	r := alphaRequest(w, "miss", "a", "strike", "enemy", "unused")
	alphaCommit(t, w, r)
	if len(w.Snapshot().Conditions) != 0 || len(w.Snapshot().RetiredConditionIDs) != 0 {
		t.Fatal("unchosen ID reserved")
	}
	r = alphaRequest(w, "guard", "b", "guard", "", "manual")
	receipt := alphaCommit(t, w, r)
	before := w.ExportCheckpoint()
	receipt.Events[1].Condition.After.Lifetime.ExpiresAfterRound = 999
	receipt.Events[1].Condition.After.Cause.AbilityOrigin.Path = []int{1, 9}
	b.AbilityDefinitions[0].Action.Actor.Parameter = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested mutable input/output escaped")
	}
	alphaCommit(t, w, Request{ID: "remove", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{RemoveCondition: &RemoveCondition{Instance: "manual", Target: "b"}}}})
	alphaCommit(t, w, alphaRequest(w, "C", "c", "pass", "", ""))
	es := alphaCommit(t, w, alphaRequest(w, "E", "enemy", "pass", "", ""))
	if len(es.Events) != 2 {
		t.Fatal("manually removed condition expired again", es)
	}
	replay, e := Replay(w.ExportCheckpoint().Genesis, w.History())
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), replay.ExportCheckpoint()) {
		t.Fatal(e)
	}
}
func TestAlphaNumericAndRandomExhaustionRollback(t *testing.T) {
	for _, which := range []string{"check overflow", "random counter", "round counter"} {
		t.Run(which, func(t *testing.T) {
			b := alphaFixture([2]uint64{1, 2})
			if which == "check overflow" {
				abilityDefinition(&b, "strike").Steps[1].Check.Modifier = CheckValue{Literal: alphaPtr(int64(math.MaxInt64))}
			}
			w := alphaWorld(t, b)
			r := alphaRequest(w, "bad", "a", "strike", "enemy", "")
			code := "E_OVERFLOW"
			if which == "random counter" {
				w.authority.state.Adventure.State.Random.Calls = math.MaxUint64
				code = "E_LIMIT"
			}
			if which == "round counter" {
				p := &w.authority.state.Adventure.State.Encounters[0]
				p.Round = math.MaxUint64
				p.Cursor = 3
				r = alphaRequest(w, "bad", "enemy", "pass", "", "")
			}
			alphaReject(t, w, r, code)
		})
	}
}
func TestAlphaConcurrentOriginalRetry(t *testing.T) {
	w := alphaWorld(t, alphaFixture([2]uint64{1, 2}))
	r := alphaRequest(w, "one", "a", "strike", "enemy", "")
	var wg sync.WaitGroup
	out := make(chan Batch, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := w.Execute(r)
			if e != nil {
				t.Error(e)
			}
			out <- b
		}()
	}
	wg.Wait()
	close(out)
	var first *Batch
	for b := range out {
		if first == nil {
			first = &b
		} else if !reflect.DeepEqual(*first, b) {
			t.Fatal("concurrent receipts differ")
		}
	}
	if w.Revision() != 1 || w.Snapshot().Adventure.State.Random.Calls != 1 {
		t.Fatal("duplicate commit/draw")
	}
}

func TestAlphaNestedConsequencesAndLateBranchFailure(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	d := abilityDefinition(&b, "strike")
	d.Parameters = append(d.Parameters, AbilityParameter{ID: "id", Kind: ConditionIDArgument})
	d.Steps[1].Check.Success = append(d.Steps[1].Check.Success, AbilityPrimitive{ApplyCondition: &ConditionTemplate{Instance: alphaParam("id"), Type: "guarded", Target: alphaParam("actor"), Duration: alphaPtr(uint64(2))}})
	w := alphaWorld(t, b)
	original := alphaRequest(w, "hit", "a", "strike", "enemy", "branch:guard")
	receipt := alphaCommit(t, w, original)
	c := w.Snapshot().Conditions[0]
	if !reflect.DeepEqual(c.Cause.AbilityOrigin.Path, []int{0, 1}) || c.Cause.AbilityOrigin.Step != 1 || c.Cause.RequestID != "hit" || c.Lifetime.ExpiresAfterRound != 2 {
		t.Fatal(c)
	}
	before := w.ExportCheckpoint()
	receipt.Events[3].Condition.After.Cause.AbilityOrigin.Path[0] = 1
	receipt.Events[3].AbilityOrigin.Path[1] = 77
	b.AbilityDefinitions[0].Steps[1].Check.Success[1].ApplyCondition.Duration = alphaPtr(uint64(999))
	s := w.Snapshot()
	s.AbilityDefinitions[2].Action.Requirements = nil
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested branch records share ownership")
	}
	if e := w.Preview(original); e != nil {
		t.Fatal("historical Preview rejected", e)
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("Preview advanced")
	}
	replay, e := Replay(before.Genesis, before.History)
	if e != nil || !reflect.DeepEqual(before, replay.ExportCheckpoint()) {
		t.Fatal(e)
	}
	// A valid check and private damage/cost are discarded when its chosen branch
	// hits a unique type/target conflict. A fresh ID is not a uniqueness repair.
	b = alphaFixture([2]uint64{1, 2})
	d = abilityDefinition(&b, "strike")
	d.Parameters = append(d.Parameters, AbilityParameter{ID: "id", Kind: ConditionIDArgument})
	d.Steps[1].Check.Success = append(d.Steps[1].Check.Success, AbilityPrimitive{ApplyCondition: &ConditionTemplate{Instance: alphaParam("id"), Type: "guarded", Target: alphaParam("actor"), Duration: alphaPtr(uint64(2))}})
	b.Conditions = []ConditionInstance{{ID: "existing", Type: "guarded", Target: "a"}}
	w = alphaWorld(t, b)
	r := alphaRequest(w, "failed", "a", "strike", "enemy", "fresh")
	before = w.ExportCheckpoint()
	_, e = w.Execute(r)
	var ae *AbilityError
	if !errors.As(e, &ae) || !reflect.DeepEqual(ae.Path, []int{0, 1}) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("late branch partially published")
	}
	alphaCommit(t, w, Request{ID: "remove", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{RemoveCondition: &RemoveCondition{Instance: "existing", Target: "a"}}}})
	r.ExpectedRevision = w.Revision()
	alphaCommit(t, w, r)
}
func TestAlphaDurationTwoAndTerminalLastSlot(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	abilityDefinition(&b, "guard").Steps[1].ApplyCondition.Duration = alphaPtr(uint64(2))
	w := alphaWorld(t, b)
	alphaCommit(t, w, alphaRequest(w, "guard", "a", "guard", "", "two"))
	for _, id := range []CharacterID{"b", "c", "enemy"} {
		alphaCommit(t, w, alphaRequest(w, "one:"+string(id), id, "pass", "", ""))
	}
	if len(w.Snapshot().Conditions) != 1 || w.Snapshot().Adventure.State.Encounters[0].Round != 2 {
		t.Fatal("duration2 expired at first boundary")
	}
	for _, id := range []CharacterID{"a", "b", "c", "enemy"} {
		alphaCommit(t, w, alphaRequest(w, "two:"+string(id), id, "pass", "", ""))
	}
	if len(w.Snapshot().Conditions) != 0 || w.Snapshot().Adventure.State.Encounters[0].Round != 3 {
		t.Fatal("duration2 survived second boundary")
	}
	b = alphaFixture([2]uint64{1, 2})
	b.Adventure.Definition.Encounters[0].Order = []Participant{{Character: "a", Team: PartyTeam}, {Character: "c", Team: PartyTeam}, {Character: "enemy", Team: HostileTeam}, {Character: "b", Team: PartyTeam}}
	character(&b, "enemy").Resources["hp"] = Resource{Current: 4, Maximum: 6}
	w = alphaWorld(t, b)
	alphaCommit(t, w, alphaRequest(w, "guard", "a", "guard", "", "end"))
	alphaCommit(t, w, alphaRequest(w, "C", "c", "pass", "", ""))
	alphaCommit(t, w, alphaRequest(w, "E", "enemy", "pass", "", ""))
	receipt := alphaCommit(t, w, alphaRequest(w, "last", "b", "strike", "enemy", ""))
	p := w.Snapshot().Adventure.State.Encounters[0]
	if p.Status != Victory || p.Round != 1 || p.Cursor != 3 {
		t.Fatal(p)
	}
	for _, e := range receipt.Events {
		if e.Kind == TurnAdvanced || e.Condition != nil && e.Condition.Expiration != nil && e.Condition.Expiration.Reason != "encounter end" {
			t.Fatal("terminal incorrectly wrapped", receipt)
		}
	}
}
func TestAlphaArchiveAdmissionKeepsHistoricalRetry(t *testing.T) {
	w := alphaWorld(t, alphaFixture([2]uint64{1, 2}))
	r := alphaRequest(w, "old", "a", "strike", "enemy", "")
	receipt := alphaCommit(t, w, r)
	// Private setup reaches an exact byte boundary without allocating128MiB in a
	// unit test. The real >=4096-request archive test independently covers capacity.
	w.authority.archiveBytes = MaxArchiveBytes
	alphaReject(t, w, alphaRequest(w, "new", "b", "pass", "", ""), "E_LIMIT")
	before := w.ExportCheckpoint()
	if !reflect.DeepEqual(receipt, alphaCommit(t, w, r)) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("full history denied/doubled retry")
	}
	w.authority.archiveBytes = MaxArchiveBytes - 1
	alphaReject(t, w, alphaRequest(w, "new2", "b", "pass", "", ""), "E_LIMIT")
}
func TestAlphaPreviewIsDetachedAndUsesCurrentAuthority(t *testing.T) {
	w := alphaWorld(t, alphaFixture([2]uint64{1, 2}))
	r := alphaRequest(w, "preview", "a", "strike", "enemy", "")
	before := w.ExportCheckpoint()
	for i := 0; i < 3; i++ {
		if e := w.Preview(r); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("preview consumed private facts")
	}
	alphaCommit(t, w, r)
	receipt := w.History()[0]
	r.Operations[0].Invoke.Arguments[0].Value.Character = alphaPtr(CharacterID("enemy"))
	if !reflect.DeepEqual(receipt, w.History()[0]) {
		t.Fatal("retained request shares caller values")
	}
}

func TestAlphaExactCheckBoundaryAndCandidateEquipment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seed  [2]uint64
		equip bool
		hit   bool
		draw  uint64
	}{{"equal hits", [2]uint64{12, 13}, false, true, 8}, {"one below misses", [2]uint64{22, 23}, false, false, 7}, {"candidate shield changes threshold", [2]uint64{12, 13}, true, false, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			b := alphaFixture(tc.seed)
			d := abilityDefinition(&b, "strike")
			if tc.equip {
				d.Steps = append([]AbilityStep{{Unequip: &UnequipTemplate{Item: AbilityOperand{Literal: &AbilityValue{Item: alphaPtr(ItemID("shield"))}}}}, {Equip: &EquipTemplate{Item: AbilityOperand{Literal: &AbilityValue{Item: alphaPtr(ItemID("shield"))}}, Wearer: alphaParam("target")}}}, d.Steps...)
			}
			w := alphaWorld(t, b)
			r := alphaCommit(t, w, alphaRequest(w, "check", "a", "strike", "enemy", ""))
			var check *CheckResult
			for _, e := range r.Events {
				if e.Check != nil {
					check = e.Check
				}
			}
			if check == nil || check.Draw != tc.draw || check.Success != tc.hit {
				t.Fatal(r)
			}
			if tc.equip && check.Threshold != 14 {
				t.Fatal("check ignored candidate equipment", check)
			}
			if character(&w.authority.state.Blueprint, "a").Resources["focus"].Current != 1 {
				t.Fatal("miss refunded")
			}
		})
	}
	b := alphaFixture([2]uint64{12, 13})
	b.Rules.Wear = OwnerOnly
	d := abilityDefinition(&b, "strike")
	d.Steps = append([]AbilityStep{{Unequip: &UnequipTemplate{Item: AbilityOperand{Literal: &AbilityValue{Item: alphaPtr(ItemID("shield"))}}}}, {Equip: &EquipTemplate{Item: AbilityOperand{Literal: &AbilityValue{Item: alphaPtr(ItemID("shield"))}}, Wearer: alphaParam("target")}}}, d.Steps...)
	w := alphaWorld(t, b)
	alphaReject(t, w, alphaRequest(w, "owner-only", "a", "strike", "enemy", ""), "E_RULE")
	alphaCommit(t, w, alphaRequest(w, "continuation", "a", "pass", "", ""))
}
