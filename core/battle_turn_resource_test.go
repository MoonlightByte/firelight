package core_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

func turnResourceFixture() core.Blueprint {
	b := battleAbilityFixture()
	b.Battle.Rules.TurnResources = []core.ResourceID{"turn:bonus"}
	b.Characters[0].Resources["turn:bonus"] = core.Resource{Maximum: 1}
	b.Characters[1].Resources["turn:bonus"] = core.Resource{Maximum: 1}
	b.AbilityDefinitions[0].Action.TurnCost = &core.TurnCost{Resource: "turn:bonus", Amount: 1}
	*b.AbilityDefinitions[0].Steps[1].Heal.Amount.Literal.Integer = 2
	return b
}

func TestBattleTurnResourceOrderingAndHistory(t *testing.T) {
	b := turnResourceFixture()
	w := gearWorld(t, b)
	begin := battleExec(t, w, "begin", "a", battleBegin())
	if len(begin.Events) != 2 || begin.Events[0].Kind != core.ResourceRestored || begin.Events[0].Restoration.Character != "a" || begin.Events[1].Kind != core.BattleStarted || w.Snapshot().Characters[1].Resources["turn:bonus"].Current != 0 {
		t.Fatal("first actor only restores before terminal event", begin)
	}
	r := battleExec(t, w, "bonus", "a", battleInvoke("care", "a", "a"))
	s := w.Snapshot()
	e := s.Battle.State.Encounters[0]
	if s.Characters[0].Resources["hp"].Current != 7 || s.Characters[0].Resources["focus"].Current != 1 || s.Characters[0].Resources["turn:bonus"].Current != 0 || e.Cursor != 0 || e.Round != 1 || e.ActionUsed || s.Clock.Tick != 0 {
		t.Fatal("bonus must leave standard action available", s)
	}
	if len(r.Events) != 4 || r.Events[0].Resource.Resource != "turn:bonus" || r.Events[0].AbilityOrigin != nil || r.Events[1].AbilityOrigin.Step != 0 || r.Events[2].AbilityOrigin.Step != 1 || r.Events[3].Kind != core.AbilityInvoked || r.Events[3].BattleAction == nil {
		t.Fatal("prefix must not shift body provenance or terminal intent", r)
	}
	err := battleReject(t, w, gearRequest(w, "second-bonus", battleInvoke("care", "a", "a")))
	var ae *core.AbilityError
	if !errors.As(err, &ae) || ae.Step != -1 || ae.Operand != "turn_cost.resource" {
		t.Fatal("cost error lost Ability context", err)
	}
	attack := battleExec(t, w, "standard", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, true))
	if attack.Events[len(attack.Events)-2].Restoration.Character != "b" || w.Snapshot().Characters[0].Resources["turn:bonus"].Current != 0 || w.Snapshot().Characters[1].Resources["turn:bonus"].Current != 1 {
		t.Fatal("next actor only restoration", attack)
	}
	battleExec(t, w, "wait", "b", battleInvoke("wait", "b", ""))
	if w.Snapshot().Clock.Tick != 6 || w.Snapshot().Battle.State.Encounters[0].Round != 2 || w.Snapshot().Characters[0].Resources["turn:bonus"].Current != 1 {
		t.Fatal("round wrap restores original actor")
	}
	cp := w.ExportCheckpoint()
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, cp, nativesave.SessionState{Campaign: "allowance", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), cp) {
		t.Fatal("full reconstruction", err)
	}
	for _, world := range []*core.World{w, loaded.World} {
		retry, err := world.Execute(r.Request)
		if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(world.ExportCheckpoint(), cp) {
			t.Fatal("historical receipt must not pay again", err)
		}
	}
	b.Battle.Rules.TurnResources[0] = "forged"
	b.AbilityDefinitions[0].Action.TurnCost.Amount = 99
	s.Battle.Rules.TurnResources[0] = "forged"
	s.AbilityDefinitions[0].Action.TurnCost.Resource = "forged"
	if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("nested ownership")
	}
	history := w.History()
	history[0].Events[0].Restoration.After.Current = 0
	if bad, err := core.Replay(turnResourceFixture(), history); bad != nil || err == nil {
		t.Fatal("forged restoration accepted")
	}
}

func TestBattleTurnResourceUsedStandardAndRollback(t *testing.T) {
	w := gearWorld(t, turnResourceFixture())
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "late", battleInvoke("care", "a", "a"), core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "focus", Amount: 2}}))
	battleExec(t, w, "attack-open", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, false))
	battleExec(t, w, "bonus-after-attack", "a", battleInvoke("care", "a", "a"))
	if !w.Snapshot().Battle.State.Encounters[0].ActionUsed || w.Snapshot().Battle.State.Encounters[0].Cursor != 0 {
		t.Fatal("bonus action reset the used standard action")
	}
	battleReject(t, w, gearRequest(w, "second-standard", battleInvoke("wait", "a", "")))
	b := turnResourceFixture()
	// Outside Battle neither missing allowance nor its zero balance blocks body.
	delete(b.Characters[0].Resources, "turn:bonus")
	w = gearWorld(t, b)
	r := battleExec(t, w, "exploration", "a", battleInvoke("care", "a", "a"))
	if len(r.Events) != 3 || r.Events[0].Resource.Resource != "focus" || w.Snapshot().Characters[0].Resources["hp"].Current != 7 {
		t.Fatal("outside Battle paid a turn allowance", r)
	}
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "missing-allowance", battleInvoke("care", "a", "a")))
	battleExec(t, w, "ordinary-nonuser", "a", battleInvoke("wait", "a", ""))
}

func TestBattleTurnResourceHumanInputPrefix(t *testing.T) {
	b := rolledCareFixture()
	b.Battle.Rules.TurnResources = []core.ResourceID{"turn:bonus"}
	b.Characters[0].Resources["turn:bonus"] = core.Resource{Maximum: 1}
	b.AbilityDefinitions[0].Action.TurnCost = &core.TurnCost{Resource: "turn:bonus", Amount: 1}
	// The actual cost also removes a maintained root before the indexed body.
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "allowance-present", Instances: core.Unique, EndsAtMinimum: gearPtr(core.ResourceID("turn:bonus"))})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "condition", "a", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "old-allowance", Type: "allowance-present", Target: "a"}})
	cp := w.ExportCheckpoint()
	input, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
	if err != nil || input.Stage != "ability" || *input.AbilityStep != 1 || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("preview changed state or roll index", input, err)
	}
	r := battleExec(t, w, "rolled-bonus", "a", rolledCare("a", "a", 4, 5))
	if r.Events[0].Kind != core.ConditionRemoved || r.Events[1].Resource.Resource != "turn:bonus" || r.Events[2].AbilityOrigin.Step != 0 || r.Events[4].ResourceRoll.Requested != 12 || w.Snapshot().Characters[0].Resources["hp"].Current != 17 {
		t.Fatal("shared cost consequences / fixed human roll", r)
	}
	cp = w.ExportCheckpoint()
	if _, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a")); err == nil || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("unaffordable roll question admitted", err)
	}
}

func TestBattleTurnResourceDownedActorAndFinish(t *testing.T) {
	for _, down := range []bool{false, true} {
		b := turnResourceFixture()
		b.Characters = append(b.Characters, core.Character{ID: "c", Location: "room", Stats: map[core.StatID]int64{"initiative": 0, "defense": 10, "dexterity": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 10, Maximum: 10}}})
		b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "c"}, Kind: "party-member", To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Strength: 1})
		d := &b.AbilityDefinitions[0]
		d.Action.TurnCost.Finish = !down
		if down {
			d.Steps = []core.AbilityStep{{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Parameter: "actor"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(5))}}}}}
		}
		w := gearWorld(t, b)
		begin := battleBegin()
		begin.BeginBattle.Participants = append(begin.BeginBattle.Participants, core.BattleAdmission{Character: "c", Side: core.ProtectedSide, Controller: core.AgentController, Position: core.BattlePosition{X: 0, Y: 1}, TieRank: 2})
		battleExec(t, w, "begin", "a", begin)
		r := battleExec(t, w, "bonus", "a", battleInvoke("care", "a", "a"))
		e := r.Events[len(r.Events)-1].BattleAction.After
		if e.Cursor != 1 || e.Status != core.Active || e.ActionUsed {
			t.Fatal("explicit finish or downed actor must advance", down, e)
		}
		if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("turn settlement replay", err)
		}
	}
}

func TestBattleTurnResourceInvalidDefinitions(t *testing.T) {
	for _, mutate := range []func(*core.Blueprint){
		func(b *core.Blueprint) { b.Battle.Rules.TurnResources = []core.ResourceID{"hp"} },
		func(b *core.Blueprint) { b.Battle.Rules.TurnResources = []core.ResourceID{"turn:bonus", "turn:bonus"} },
		func(b *core.Blueprint) { b.Battle.Rules.TurnResources = []core.ResourceID{"missing"} },
		func(b *core.Blueprint) { b.AbilityDefinitions[0].Action.TurnCost.Amount = 0 },
		func(b *core.Blueprint) { b.AbilityDefinitions[0].Action.TurnCost.Resource = "focus" },
		func(b *core.Blueprint) { b.Battle = nil },
	} {
		b := turnResourceFixture()
		mutate(&b)
		if w, err := core.NewWorld(b); w != nil || err == nil {
			t.Fatal("invalid turn policy published")
		}
	}
}

func TestBattleTurnResourceInputWorkBoundary(t *testing.T) {
	for _, floorRemoval := range []bool{false, true} {
		b := rolledCareFixture()
		b.Battle.Rules.TurnResources = []core.ResourceID{"turn:bonus"}
		b.Characters[0].Resources["turn:bonus"] = core.Resource{Maximum: 1}
		b.AbilityDefinitions[0].Action.TurnCost = &core.TurnCost{Resource: "turn:bonus", Amount: 1}
		roll := b.AbilityDefinitions[0].Steps[1]
		b.AbilityDefinitions[0].Steps = nil
		for range 127 {
			b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, core.AbilityStep{Heal: &core.HealTemplate{Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(1))}}}})
		}
		b.AbilityDefinitions[0].Steps = append(b.AbilityDefinitions[0].Steps, roll)
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "floor", Instances: core.Unique, EndsAtMinimum: gearPtr(core.ResourceID("turn:bonus"))})
		w := gearWorld(t, b)
		battleExec(t, w, "begin", "a", battleBegin())
		if floorRemoval {
			battleExec(t, w, "floor", "a", core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "floor", Type: "floor", Target: "a"}})
		}
		before := w.ExportCheckpoint()
		_, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, rolledCare("a", "a"))
		var fault *core.Fault
		if !errors.As(err, &fault) || fault.Code != "E_LIMIT" || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
			t.Fatalf("preview admitted over-budget roll, floor removal=%v: %v", floorRemoval, err)
		}
		op := rolledCare("a", "a", 1, 1)
		op.Invoke.HumanRolls[0].Step = 127
		err = battleReject(t, w, gearRequest(w, "over-budget", op))
		if !errors.As(err, &fault) || fault.Code != "E_LIMIT" {
			t.Fatal("execution did not agree on limit", err)
		}
	}
}
