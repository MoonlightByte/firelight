package core_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func hitConditionFixture() core.Blueprint {
	b := battleFixture()
	b.ConditionDefinitions = append(b.ConditionDefinitions,
		core.ConditionDefinition{ID: "mastery", Instances: core.Unique},
		core.ConditionDefinition{ID: "sapped", Instances: core.Coexist, UntilSourceTurn: true, EndsAfterAttack: true})
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "a-mastery", Type: "mastery", Target: "a"})
	b.Battle.Rules.HitConditions = []core.WeaponHitCondition{{ID: "sap", Definition: "longsword", Mode: "one", Attack: "slash", Requires: "mastery", Condition: "sapped"}}
	b.Battle.Rules.RollPolicies = []core.BattleRollPolicy{{Condition: "sapped", On: "attacker", Direction: "disadvantage"}}
	return b
}
func hitConditionAttack(t *testing.T, w *core.World, id core.ConditionInstanceID, hit bool) core.Operation {
	op := battleAttack(t, w, "a", "b", []int64{1}, nil, true)
	if hit {
		op.WeaponAttack.Human = &core.HumanAttackRolls{Attack: []int64{15}, Damage: []int64{1}}
	}
	op.WeaponAttack.HitCondition = &core.WeaponConditionChoice{Rule: "sap", Instance: id}
	return op
}
func endCurrent(t *testing.T, w *core.World, id string) core.Batch {
	e := w.Snapshot().Battle.State.Encounters[0]
	actor := e.Roster[e.Cursor].Admission.Character
	return battleExec(t, w, id, actor, core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: actor}}})
}

func TestWeaponHitConditionAtomicRollConsumptionAndReplay(t *testing.T) {
	b := hitConditionFixture()
	w := gearWorld(t, b)
	begin := battleBegin()
	battleExec(t, w, "begin", "a", begin)
	op := hitConditionAttack(t, w, "sap-one", true)
	battleReject(t, w, gearRequest(w, "late", op, core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 100}}))
	r := battleExec(t, w, "hit", "a", op)
	if len(r.Events) != 3 || r.Events[0].Kind != core.DamageApplied || r.Events[0].Resource.Requested != 4 || r.Events[1].Kind != core.ConditionApplied || r.Events[1].Condition.After.Source.ID != "a" || r.Events[1].Condition.After.Cause.RequestID != "hit" || r.Events[2].WeaponAttack.Intent.HitCondition.Instance != "sap-one" {
		t.Fatal("ordered damage/Condition/selected original intent", r)
	}
	attack := battleAttack(t, w, "b", "a", nil, nil, true)
	response := battleExec(t, w, "sapped-miss", "b", attack)
	roll := response.Events[len(response.Events)-1].WeaponAttack.Roll
	if len(roll.Attack) != 2 || roll.Selected != min(roll.Attack[0], roll.Attack[1]) || len(roll.Causes) != 1 || roll.Causes[0].Condition != "sap-one" || response.Events[0].Condition.BattleEnding.Trigger != "attack" || len(w.Snapshot().Conditions) != len(b.Conditions) {
		t.Fatal("next attack disadvantage and removal", response)
	}
	// Used lifetime identities reject before input, including retired IDs.
	used := hitConditionAttack(t, w, "sap-one", true)
	used.WeaponAttack.Human = nil
	before := w.ExportCheckpoint()
	if _, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, used); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("retired ID reached input")
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "sap", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("reconstruction", err)
	}
	retry, err := loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("historical receipt", err)
	}
	response.Events[0].Condition.BattleEnding.Window.Actor = "forged"
	op.WeaponAttack.HitCondition.Instance = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested alias")
	}
	history := w.History()
	history[len(history)-1].Events[0].Condition.BattleEnding.Trigger = "terminal"
	if bad, err := core.Replay(b, history); bad != nil || err == nil {
		t.Fatal("forged lifecycle reason")
	}
}

func TestWeaponHitConditionMissZeroDamageAndSourceTurn(t *testing.T) {
	for _, hit := range []bool{false, true} {
		b := hitConditionFixture()
		b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage.Bonus = core.ScalarRecipe{Base: -10}
		w := gearWorld(t, b)
		battleExec(t, w, "begin", "a", battleBegin())
		r := battleExec(t, w, "attack", "a", hitConditionAttack(t, w, "zero", hit))
		if w.Snapshot().Characters[1].Resources["hp"].Current != 30 {
			t.Fatal("clamped zero damage changed health")
		}
		if hit {
			if len(r.Events) != 2 || r.Events[0].Kind != core.ConditionApplied {
				t.Fatal("zero-damage hit lost consequence", r)
			}
			end := endCurrent(t, w, "skip")
			found := false
			for _, event := range end.Events {
				if event.Condition != nil && event.Condition.BattleEnding != nil {
					found = event.Condition.BattleEnding.Trigger == "source_turn" && event.Condition.BattleEnding.Window.Actor == "a"
				}
			}
			if !found || len(w.Snapshot().Conditions) != len(b.Conditions) {
				t.Fatal("next source turn did not expire effect", end)
			}
		} else if len(r.Events) != 1 || len(w.Snapshot().Conditions) != len(b.Conditions) || len(w.Snapshot().RetiredConditionIDs) != 0 {
			t.Fatal("miss reserved/applied condition")
		}
	}
}

func TestWeaponHitConditionSkippedSourceAndTerminal(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		b := hitConditionFixture()
		actor := core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("b"))}}
		target := core.AbilityOperand{Literal: &core.AbilityValue{Character: gearPtr(core.CharacterID("a"))}}
		amount := core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(30))}}
		b.AbilityDefinitions = []core.AbilityDefinition{{
			ID:     "down-source",
			Action: &core.ActionPolicy{Actor: actor, Phase: core.EncounterPhase, Finish: gearPtr(false)},
			Steps:  []core.AbilityStep{{Damage: &core.DamageTemplate{Target: target, Resource: "hp", Amount: amount}}},
		}}
		b.Battle.Rules.Actions = []core.AbilityID{"down-source"}
		begin := battleBegin()
		if !terminal {
			b.Characters = append(b.Characters, core.Character{ID: "c", Location: "room", Stats: map[core.StatID]int64{"initiative": -100, "defense": 10, "dexterity": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 30, Maximum: 30}}})
			b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "c"}, To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", Strength: 1})
			begin.BeginBattle.Participants = append(begin.BeginBattle.Participants, core.BattleAdmission{Character: "c", Side: core.ProtectedSide, Controller: core.AgentController, Position: core.BattlePosition{Y: 1}, TieRank: 2})
		}
		w := gearWorld(t, b)
		battleExec(t, w, "begin", "a", begin)
		battleExec(t, w, "hit", "a", hitConditionAttack(t, w, "sap", true))
		battleExec(t, w, "down-source", "b", core.Operation{Invoke: &core.InvokeAbility{Ability: "down-source"}})
		if !terminal {
			endCurrent(t, w, "b-end")
			endCurrent(t, w, "c-end")
		}
		if len(w.Snapshot().Conditions) != len(b.Conditions) {
			t.Fatal("downed source kept Sap")
		}
		if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("lifecycle reconstruction", err)
		}
	}
}

func TestWeaponHitConditionHumanMissAndCancelledDisadvantage(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		b := hitConditionFixture()
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "advantage", Instances: core.Unique})
		b.Battle.Rules.RollPolicies = append(b.Battle.Rules.RollPolicies, core.BattleRollPolicy{Condition: "advantage", On: "attacker", Direction: "advantage"})
		w := gearWorld(t, b)
		battleExec(t, w, "begin", "a", battleBegin())
		endCurrent(t, w, "a-end")
		battleExec(t, w, "sapped", "b", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "sap-on-a", Type: "sapped", Target: "a", Source: &core.Ref{Kind: core.CharacterKind, ID: "b"}}})
		if cancel {
			battleExec(t, w, "advantage", "b", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "adv", Type: "advantage", Target: "a"}})
		}
		endCurrent(t, w, "b-end")
		faces := []int64{20, 1}
		if cancel {
			faces = []int64{1}
		}
		r := battleExec(t, w, "human-miss", "a", battleAttack(t, w, "a", "b", faces, nil, false))
		roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
		if roll.Selected != 1 || roll.Hit || r.Events[0].Condition.Before.ID != "sap-on-a" || r.Events[0].Condition.BattleEnding.Trigger != "attack" {
			t.Fatal("one-shot condition survived/cancelled incorrectly", r)
		}
	}
}
