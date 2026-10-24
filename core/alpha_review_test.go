package core

import (
	"reflect"
	"testing"
)

func TestAlphaReviewDownedAndDistantEligibility(t *testing.T) {
	for _, name := range []string{"downed target", "downed actor", "distant target"} {
		t.Run(name, func(t *testing.T) {
			b := alphaFixture([2]uint64{1, 2})
			actor, target := CharacterID("a"), CharacterID("enemy")
			switch name {
			case "downed target":
				character(&b, "enemy").Resources["hp"] = Resource{Current: 0, Maximum: 6}
			case "downed actor":
				character(&b, "b").Resources["hp"] = Resource{Current: 0, Maximum: 10}
				actor = "b"
			case "distant target":
				b.Adventure.Definition.InitialEncounter = ""
				b.AbilityDefinitions[0].Action.Phase = EitherPhase
				other := alphaParam("target")
				b.AbilityDefinitions[0].Action.Requirements = []Requirement{{Kind: Colocated, Subject: alphaParam("actor"), Other: &other}}
				character(&b, "b").Location = "home"
				target = "b"
			}
			w := alphaWorld(t, b)
			alphaReject(t, w, alphaRequest(w, "same-logical-attempt", actor, "strike", target, ""), "E_ELIGIBILITY")
			// Rejected ID remains usable. This ordinary trusted API operation is
			// a continuation control, not an automatic gameplay repair.
			alphaCommit(t, w, Request{ID: "same-logical-attempt", Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{Heal: &ResourceOp{Character: "a", Resource: "hp", Amount: 1}}}})
		})
	}
}

func TestAlphaReviewRejectsPreappliedTimedGenesis(t *testing.T) {
	for _, r := range []uint64{1, 99} {
		b := alphaFixture([2]uint64{1, 2})
		b.Conditions = []ConditionInstance{{ID: "fabricated", Type: "marked", Target: "a", Lifetime: &ConditionLifetime{Encounter: "enc", AppliedRound: r, ExpiresAfterRound: r}}}
		if e := Validate(b); e == nil {
			t.Fatal("declaration accepted timed genesis")
		}
		if w, e := NewWorld(b); e == nil || w != nil {
			t.Fatal("constructor accepted pre-applied timed genesis")
		}
	}
}
func TestAlphaReviewTerminalAssessmentUsesWorkBudget(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	d := abilityDefinition(&b, "strike")
	d.Steps = nil
	for i := 0; i < 127; i++ {
		d.Steps = append(d.Steps, AbilityStep{Heal: &HealTemplate{Target: alphaParam("actor"), Resource: "hp", Amount: alphaInt(1)}})
	}
	d.Steps = append(d.Steps, AbilityStep{Damage: &DamageTemplate{Target: alphaParam("target"), Resource: "hp", Amount: alphaInt(6)}})
	w := alphaWorld(t, b)
	alphaReject(t, w, alphaRequest(w, "scan", "a", "strike", "enemy", ""), "E_LIMIT")
}
func TestAlphaReviewImmediateClaimCompletionTrace(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	b.Adventure.Definition.Objectives[0].Destination = "field"
	abilityDefinition(&b, "strike").Steps[1].Check.Success[0].Damage.Amount = alphaInt(6)
	w := alphaWorld(t, b)
	alphaCommit(t, w, alphaRequest(w, "win", "a", "strike", "enemy", ""))
	r := Request{ID: "claim", ExpectedRevision: w.Revision(), Actor: Ref{CharacterKind, "a"}, Operations: []Operation{{Claim: &ClaimObjective{Objective: "goal", Recipient: "a"}}}}
	receipt := alphaCommit(t, w, r)
	expected := ObjectiveProgress{ID: "goal"}
	kinds := []EventKind{ItemTransferred, ObjectiveClaimed, ObjectiveCompleted}
	if len(receipt.Events) != len(kinds) {
		t.Fatal(receipt)
	}
	for i, e := range receipt.Events {
		if e.Kind != kinds[i] {
			t.Fatal(receipt)
		}
		if e.Objective != nil {
			if e.Objective.Before != expected {
				t.Fatal("contradictory ordered before fact", e)
			}
			expected = e.Objective.After
		}
	}
	if !expected.Claimed || !expected.Completed || expected != w.Snapshot().Adventure.State.Objectives[0] || receipt.Events[2].Claim == nil {
		t.Fatal("missing final truth or terminal intent")
	}
	cp := w.ExportCheckpoint()
	replay, e := Replay(cp.Genesis, cp.History)
	if e != nil || !reflect.DeepEqual(cp, replay.ExportCheckpoint()) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(receipt, alphaCommit(t, replay, r)) || !reflect.DeepEqual(cp, replay.ExportCheckpoint()) {
		t.Fatal("retry changed completed objective")
	}
	r.ID = "again"
	r.ExpectedRevision = w.Revision()
	alphaReject(t, w, r, "E_ADVENTURE")
}
func TestAlphaReviewActualUniformRejectionSample(t *testing.T) {
	// Independent reviewer inverted the pinned PCG LCG to obtain a first zero
	// raw sample. Uniform d20 rejects it, then consumes a second raw sample.
	b := alphaFixture([2]uint64{15827655171185078204, 17459065950105678717})
	w := alphaWorld(t, b)
	r := alphaRequest(w, "sample", "a", "strike", "enemy", "")
	r.Operations = append(r.Operations, Operation{Spend: &ResourceOp{Character: "a", Resource: "focus", Amount: 2}})
	alphaReject(t, w, r, "E_INSUFFICIENT_RESOURCE")
	r.Operations = r.Operations[:1]
	receipt := alphaCommit(t, w, r)
	c := receipt.Events[1].Check
	if c.Draw != 5 || c.CallsBefore != 0 || c.CallsAfter != 2 || c.Success {
		t.Fatal(c)
	}
	before := w.ExportCheckpoint()
	if !reflect.DeepEqual(receipt, alphaCommit(t, w, r)) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("retry consumed raw samples")
	}
	replay, e := Replay(before.Genesis, before.History)
	if e != nil || !reflect.DeepEqual(before, replay.ExportCheckpoint()) {
		t.Fatal(e)
	}
}

func TestAlphaReviewActivationUsesWorkBudget(t *testing.T) {
	b := alphaFixture([2]uint64{1, 2})
	b.Adventure.Definition.InitialEncounter = ""
	for _, id := range []CharacterID{"a", "b", "c"} {
		character(&b, id).Location = "home"
	}
	// Three party moves + Travel +124 heals consume128; selecting the first
	// participant still costs work, so no movement/activation may publish.
	d := abilityDefinition(&b, "strike")
	d.Action.Phase = Exploration
	d.Action.Requirements = nil
	d.Steps = nil
	for i := 0; i < 124; i++ {
		d.Steps = append(d.Steps, AbilityStep{Heal: &HealTemplate{Target: alphaParam("actor"), Resource: "hp", Amount: alphaInt(1)}})
	}
	d.Steps = append(d.Steps, AbilityStep{Travel: &PartyTravel{To: "field"}})
	w := alphaWorld(t, b)
	alphaReject(t, w, alphaRequest(w, "enter", "a", "strike", "enemy", ""), "E_LIMIT")
}
