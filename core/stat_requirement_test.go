package core_test

import (
	"bytes"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func statRequirementFixture(adventure bool) core.Blueprint {
	b := battleAbilityFixture()
	b.Characters[0].Stats["experience"] = 299
	b.Characters[0].Stats["level"] = 1
	b.Characters[0].Stats["rating"] = 0
	b.Equipment.Derived = append(b.Equipment.Derived, core.DerivedStatRule{Stat: "rating", Terms: []core.StatTerm{{Stat: "experience", Divisor: 1}}})
	b.ConditionDefinitions = append(b.ConditionDefinitions,
		core.ConditionDefinition{ID: "earned", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "xp", Stat: "experience", Fixed: gearPtr(int64(1))}}},
		core.ConditionDefinition{ID: "gained", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "level", Stat: "level", Fixed: gearPtr(int64(1))}}})
	actor := core.AbilityOperand{Parameter: "actor"}
	b.AbilityDefinitions[0] = core.AbilityDefinition{ID: "care", Parameters: []core.AbilityParameter{{ID: "actor", Kind: core.CharacterArgument}, {ID: "id", Kind: core.ConditionIDArgument}},
		Action: &core.ActionPolicy{Actor: actor, Phase: core.Exploration, Requirements: []core.Requirement{
			{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{Stat: "experience", Minimum: 300, Maximum: math.MaxInt64}},
			{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{Stat: "level", Minimum: 1, Maximum: 1}},
			{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{Stat: "rating", Minimum: 300, Maximum: 300}},
		}}, Steps: []core.AbilityStep{
			{Expand: &core.ExpandTemplate{Target: actor, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(6))}}}},
			{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Parameter: "id"}, Type: "gained", Target: actor}},
		}}
	if adventure {
		b.Battle = nil
		b.Adventure = &core.Adventure{Definition: core.AdventureDefinition{Party: []core.CharacterID{"a"}, Health: "hp", Seed: [2]uint64{1, 2}, Grants: []core.AbilityGrant{{Character: "a", Abilities: []core.AbilityID{"care", "wait"}}}}}
	}
	return b
}
func statRequirementInvoke(id core.ConditionInstanceID) core.Operation {
	return core.Operation{Invoke: &core.InvokeAbility{Ability: "care", Arguments: []core.AbilityArgument{
		{Name: "actor", Value: core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}},
		{Name: "id", Value: core.AbilityValue{ConditionID: &id}},
	}}}
}
func statRequirementEarn() core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "xp-1", Type: "earned", Target: "a"}}
}

func TestStatRequirementOrderedCandidateAndHistoricalTruth(t *testing.T) {
	for _, adventure := range []bool{false, true} {
		b := statRequirementFixture(adventure)
		w := gearWorld(t, b)
		// Base 299 fails; an earlier +1 contributor makes BOTH effective XP and
		// the derived recipe 300. A second level gain in the same request fails.
		gearReject(t, w, "no-xp", statRequirementInvoke("gain-1"))
		gearReject(t, w, "double", statRequirementEarn(), statRequirementInvoke("gain-1"), statRequirementInvoke("gain-2"))
		gearReject(t, w, "late", statRequirementEarn(), statRequirementInvoke("gain-1"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 4}})
		r := gearExecute(t, w, "double", statRequirementEarn(), statRequirementInvoke("gain-1"))
		level, err := w.Explain("a", "level")
		if err != nil || level.Base != 1 || level.Effective != 2 || len(level.Contributions) != 1 || w.Snapshot().Characters[0].Resources["hp"] != (core.Resource{Current: 5, Maximum: 36}) || r.Revision != 1 {
			t.Fatal("fixed advancement composition oracle", adventure, level, r, err)
		}
		gearReject(t, w, "again", statRequirementInvoke("gain-2"))
		gearExecute(t, w, "lose-xp", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "xp-1", Target: "a"}})
		cp := w.ExportCheckpoint()
		retry, err := w.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
			t.Fatal("historical eligibility rechecked", err)
		}
		var archive bytes.Buffer
		if _, err := nativesave.Write(&archive, cp, nativesave.SessionState{Campaign: "stat-range", Next: 1}); err != nil {
			t.Fatal(err)
		}
		loaded, err := nativesave.Read(&archive)
		if err != nil {
			t.Fatal(err)
		}
		retry, err = loaded.World.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
			t.Fatal("archive receipt truth", err)
		}
		bad := w.History()
		bad[0].Events[1].Capacity.After.Maximum++
		if partial, err := core.Replay(cp.Genesis, bad); err == nil || partial != nil {
			t.Fatal("forged consequence accepted")
		}
		b.AbilityDefinitions[0].Action.Requirements[0].StatRange.Minimum = 0
		snapshot := w.Snapshot()
		snapshot.AbilityDefinitions[0].Action.Requirements[1].StatRange.Maximum = 99
		cp.Genesis.AbilityDefinitions[0].Action.Requirements[0].StatRange.Stat = "forged"
		fresh := w.ExportCheckpoint()
		if fresh.Genesis.AbilityDefinitions[0].Action.Requirements[0].StatRange.Minimum != 300 || fresh.State.AbilityDefinitions[0].Action.Requirements[1].StatRange.Maximum != 1 {
			t.Fatal("range payload alias")
		}
	}
}

func TestStatRequirementClosedShapeAndSelectorTypes(t *testing.T) {
	actor := core.AbilityOperand{Parameter: "actor"}
	for _, bad := range []core.Requirement{
		{Kind: core.StatBetween, Subject: actor},
		{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{Stat: "level", Minimum: 2, Maximum: 1}},
		{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{}},
		{Kind: core.StatBetween, Subject: actor, StatRange: &core.StatRange{Stat: "level"}, Resource: "hp"},
		{Kind: core.AboveMinimum, Subject: actor, Resource: "hp", StatRange: &core.StatRange{Stat: "level"}},
		{Kind: core.StatBetween, Subject: core.AbilityOperand{Parameter: "id"}, StatRange: &core.StatRange{Stat: "level"}},
		{Kind: core.StatBetween, Subject: core.AbilityOperand{Parameter: "undeclared"}, StatRange: &core.StatRange{Stat: "level"}},
		{Kind: core.StatBetween, Subject: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("room"))}}, StatRange: &core.StatRange{Stat: "level"}},
		{Kind: core.StatBetween, Subject: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("missing"))}}, StatRange: &core.StatRange{Stat: "level"}},
		{Kind: core.StatBetween, Subject: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}, StatRange: &core.StatRange{Stat: "missing"}},
	} {
		b := statRequirementFixture(false)
		b.AbilityDefinitions[0].Action.Requirements = []core.Requirement{bad}
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid predicate accepted", bad)
		}
	}
	b := statRequirementFixture(false)
	b.AbilityDefinitions[0].Action.Requirements[0].StatRange.Stat = "absent"
	w := gearWorld(t, b) // A parameter's selector resolves at its actual use.
	err := battleReject(t, w, gearRequest(w, "bound-missing", statRequirementInvoke("gain-1")))
	var ae *core.AbilityError
	var fault *core.Fault
	if !errors.As(err, &ae) || ae.Operand != "requirement.0.stat" || ae.Parameter != "actor" || !errors.As(err, &fault) || fault.Code != "E_STAT" {
		t.Fatal("lost bound selector context", err)
	}
}

func TestStatRequirementSignedEndpointsAndCandidateRemoval(t *testing.T) {
	for _, endpoint := range []int64{math.MinInt64, math.MaxInt64} {
		b := statRequirementFixture(false)
		b.Characters[0].Stats["level"] = endpoint
		b.AbilityDefinitions[0].Action.Requirements = []core.Requirement{{Kind: core.StatBetween, Subject: core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}, StatRange: &core.StatRange{Stat: "level", Minimum: endpoint, Maximum: endpoint}}}
		b.AbilityDefinitions[0].Steps = b.AbilityDefinitions[0].Steps[:1]
		w := gearWorld(t, b)
		gearExecute(t, w, "endpoint", statRequirementInvoke("unused"))
	}
	b := statRequirementFixture(false)
	w := gearWorld(t, b)
	gearExecute(t, w, "earn", statRequirementEarn())
	gearReject(t, w, "remove-first", core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "xp-1", Target: "a"}}, statRequirementInvoke("gain-1"))
	gearExecute(t, w, "remove-first", statRequirementInvoke("gain-1"))
}
