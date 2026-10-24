package core_test

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"reflect"
	"sync"
	"testing"
)

func battleFixture() core.Blueprint {
	b := gearFixture()
	b.Clock = &core.Clock{Unit: "seconds"}
	b.Characters[0].Stats["initiative"] = 100
	b.Characters[1].Stats["initiative"] = 0
	for i := range b.Characters {
		b.Characters[i].Resources["hp"] = core.Resource{Current: 30, Minimum: 0, Maximum: 30}
	}
	for i := range b.Equipment.Definitions[0].Modes {
		b.Equipment.Definitions[0].Modes[i].Attacks[0].Melee = &core.MeleeProfile{Reach: 5, Stat: "strength"}
	}
	b.Items[0].Wearer = "a"
	b.Items[0].Equipment.Mode = "one"
	b.Items[1].Owner = "b"
	b.Items[1].Wearer = "b"
	b.Items[1].Equipment.Mode = "one"
	b.Battle = &core.Battle{Rules: core.BattleRules{Version: core.BattleVersion, Health: "hp", Defense: "defense", Initiative: "initiative", PartyRoot: "a", Membership: "party-member", MembershipStrength: 1, DieSides: 20, MissFace: 1, CriticalFace: 20, CriticalMultiplier: 2, GridUnit: "feet", SquareSize: 5, ClockUnit: "seconds", RoundTicks: 6, Seed: [2]uint64{12, 34}, ProtectedOutcome: core.StableDown, OppositionOutcome: core.Dead}}
	return b
}
func battleBegin() core.Operation {
	return core.Operation{BeginBattle: &core.BeginBattle{ID: "gate", Location: "room", Participants: []core.BattleAdmission{
		{Character: "a", Side: core.ProtectedSide, Controller: core.HumanController, Position: core.BattlePosition{X: 0, Y: 0}, TieRank: 0, Initiative: gearPtr(int64(15))},
		{Character: "b", Side: core.OppositionSide, Controller: core.AgentController, Position: core.BattlePosition{X: 1, Y: 0}, TieRank: 1},
	}}}
}
func battleAttack(t *testing.T, w *core.World, actor, target core.CharacterID, faces, damage []int64, finish bool) core.Operation {
	t.Helper()
	s := w.Snapshot()
	e := s.Battle.State.Encounters[len(s.Battle.State.Encounters)-1]
	g, err := w.EquipmentView(actor)
	if err != nil || len(g.Attacks) == 0 {
		t.Fatalf("grants %v %v", g, err)
	}
	v := &core.WeaponAttack{BattleWindow: core.BattleWindow{Encounter: e.ID, Round: e.Round, Cursor: e.Cursor, Actor: actor}, Grant: g.Attacks[0].ID, Target: target, FinishTurn: finish}
	if actor == "a" {
		v.Human = &core.HumanAttackRolls{Attack: faces, Damage: damage}
	}
	return core.Operation{WeaponAttack: v}
}
func battleExec(t *testing.T, w *core.World, id string, actor core.CharacterID, ops ...core.Operation) core.Batch {
	t.Helper()
	r, err := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: string(actor)}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func battleReject(t *testing.T, w *core.World, r core.Request) error {
	t.Helper()
	before := w.ExportCheckpoint()
	_, existed := w.Receipt(r.ID)
	_, err := w.Execute(r)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("failure changed checkpoint")
	}
	if _, exists := w.Receipt(r.ID); exists != existed {
		t.Fatal("failure reserved receipt")
	}
	return err
}
func TestBattleFixedHitAndDamageOracles(t *testing.T) {
	cases := []struct {
		name                    string
		face, hitBonus, defense int64
		damage                  []int64
		hit, crit               bool
		requested               int64
	}{
		{"natural-one", 1, 100, 10, nil, false, false, 0}, {"natural-twenty", 20, 0, 100, []int64{4, 5}, true, true, 12}, {"exact", 10, 5, 15, []int64{4}, true, false, 7}, {"total-twenty", 15, 5, 15, []int64{4}, true, false, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := battleFixture()
			b.Characters[1].Stats["defense"] = tc.defense - 3
			b.Equipment.Definitions[0].Modes[0].Attacks[0].ToHit = core.ScalarRecipe{Base: tc.hitBonus}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", battleBegin())
			beforeCalls := w.Snapshot().Battle.State.Random.Calls
			op := battleAttack(t, w, "a", "b", []int64{tc.face}, tc.damage, false)
			result := battleExec(t, w, "swing", "a", op)
			roll := result.Events[len(result.Events)-1].WeaponAttack.Roll
			if roll.Hit != tc.hit || roll.Critical != tc.crit || roll.Requested != tc.requested || w.Snapshot().Battle.State.Random.Calls != beforeCalls || !w.Snapshot().Battle.State.Encounters[0].ActionUsed || w.Snapshot().Clock.Tick != 0 {
				t.Fatalf("oracle: %+v", roll)
			}
		})
	}
	t.Run("two-hand-critical", func(t *testing.T) {
		b := battleFixture()
		b.Items[0].Equipment.Mode = "two"
		w := gearWorld(t, b)
		battleExec(t, w, "begin", "a", battleBegin())
		r := battleExec(t, w, "swing", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{10, 1}, false))
		if r.Events[len(r.Events)-1].WeaponAttack.Roll.Requested != 14 {
			t.Fatal(r)
		}
	})
}
func TestBattleDamagePoliciesExactAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		policies []string
		want     int64
	}{
		{"resistance", []string{"resistance"}, 3}, {"vulnerability", []string{"vulnerability"}, 14}, {"both", []string{"resistance", "vulnerability"}, 6}, {"immune", []string{"immunity"}, 0}, {"duplicate", []string{"resistance", "resistance"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := battleFixture()
			for i, treatment := range tc.policies {
				id := core.ConditionTypeID(fmt.Sprint("defense", i))
				b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: id, Instances: core.Coexist})
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(id), Type: id, Target: "b"})
				b.Battle.Rules.DamagePolicies = append(b.Battle.Rules.DamagePolicies, core.BattleDamagePolicy{Condition: id, Kind: "slashing", Treatment: treatment})
			}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", battleBegin())
			r := battleExec(t, w, "attack", "a", battleAttack(t, w, "a", "b", []int64{15}, []int64{4}, false))
			roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
			if roll.Requested != tc.want || !roll.Hit || len(roll.DamageCauses) != len(tc.policies) {
				t.Fatal(roll)
			}
			if tc.want == 0 && len(r.Events) != 1 {
				t.Fatal("zero damage emitted resource mutation")
			}
		})
	}
	for _, tc := range []struct {
		name     string
		bonus    int64
		resist   bool
		want     int64
		overflow bool
	}{
		{"floor-negative", -3, false, 0, false}, {"exact-after-half", math.MaxInt64, true, 4611686018427387904, false}, {"final-overflow", math.MaxInt64, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := battleFixture()
			b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage.Bonus = core.ScalarRecipe{Base: tc.bonus}
			if tc.resist {
				b.Battle.Rules.DamagePolicies = []core.BattleDamagePolicy{{Condition: "martial", Kind: "slashing", Treatment: "resistance"}}
			}
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", battleBegin())
			op := battleAttack(t, w, "a", "b", []int64{15}, []int64{1}, false)
			r := gearRequest(w, "hit", op)
			if tc.overflow {
				err := battleReject(t, w, r)
				var f *core.Fault
				if !errors.As(err, &f) || f.Code != "E_OVERFLOW" {
					t.Fatal(err)
				}
				return
			}
			receipt, err := w.Execute(r)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Events[len(receipt.Events)-1].WeaponAttack.Roll.Requested != tc.want {
				t.Fatal(receipt)
			}
		})
	}
}
func TestBattleHumanInputQueryAndStrictFailures(t *testing.T) {
	w := gearWorld(t, battleFixture())
	op := battleBegin()
	op.BeginBattle.Participants[0].Initiative = nil
	before := w.ExportCheckpoint()
	need, err := w.BattleInput(0, core.Ref{Kind: core.CharacterKind, ID: "a"}, op)
	if err != nil || need.Stage != "initiative" || need.Count != 1 || need.Sides != 20 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatalf("%+v %v", need, err)
	}
	battleReject(t, w, gearRequest(w, "missing-init", op))
	battleExec(t, w, "begin", "a", battleBegin())
	attack := battleAttack(t, w, "a", "b", nil, nil, true)
	before = w.ExportCheckpoint()
	need, err = w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, attack)
	if err != nil || need.Stage != "attack" || need.Count != 1 {
		t.Fatal(need, err)
	}
	attack.WeaponAttack.Human.Attack = []int64{20}
	need, err = w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, attack)
	if err != nil || need.Stage != "damage" || need.Count != 2 || need.Sides != 8 || !need.Provisional.Critical {
		t.Fatal(need, err)
	}
	attack.WeaponAttack.Human.Damage = []int64{4}
	need, err = w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, attack)
	if err != nil || need.Count != 1 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal(need, err)
	}
	for _, mutate := range []func(*core.WeaponAttack){func(a *core.WeaponAttack) { a.Human.Attack = []int64{0} }, func(a *core.WeaponAttack) { a.Human.Attack = []int64{21} }, func(a *core.WeaponAttack) { a.Human.Attack = []int64{1}; a.Human.Damage = []int64{2} }, func(a *core.WeaponAttack) { a.Human.Attack = []int64{20}; a.Human.Damage = []int64{9, 1} }, func(a *core.WeaponAttack) { a.Target = "a" }, func(a *core.WeaponAttack) { a.Grant = "wrong" }, func(a *core.WeaponAttack) { a.Round++ }} {
		a := core.CanonicalRequest(gearRequest(w, "invalid", attack))
		mutate(a.Operations[0].WeaponAttack)
		battleReject(t, w, a)
	}
	battleExec(t, w, "valid-after-failure", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, true))
	// NPCs cannot supply faces or disclose a preview of their future RNG.
	npc := battleAttack(t, w, "b", "a", nil, nil, true)
	if _, err = w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "b"}, npc); err == nil {
		t.Fatal("NPC preview")
	}
	npc.WeaponAttack.Human = &core.HumanAttackRolls{}
	battleReject(t, w, core.Request{ID: "npc-forge", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{npc}})
}
func TestBattleCriticalVictoryReplayAndRetry(t *testing.T) {
	b := battleFixture()
	b.Characters[1].Resources["hp"] = core.Resource{Current: 10, Minimum: 0, Maximum: 10}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	r := battleExec(t, w, "kill", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true))
	s := w.Snapshot()
	if s.Clock.Tick != 6 || s.Battle.State.Encounters[0].Status != core.Victory || len(s.Battle.State.Life) != 1 || s.Battle.State.Life[0].Outcome != core.Dead || len(r.Events) != 3 || r.Events[0].Resource.Life.After.Outcome != core.Dead {
		t.Fatalf("%+v %+v", s.Battle, r)
	}
	battleExec(t, w, "doff", "a", unwear("sword"))
	before := w.ExportCheckpoint()
	retry, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("retry", err)
	}
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(rebuilt.Snapshot(), w.Snapshot()) {
		t.Fatal("replay", err)
	}
	retry, err = rebuilt.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(retry, r) {
		t.Fatal("historical retry", err)
	}
	changed := core.CanonicalRequest(r.Request)
	changed.Operations[0].WeaponAttack.Human.Damage[0] = 3
	battleReject(t, w, changed)
	history := w.History()
	history[1].Events[0].Resource.Life.After.Outcome = core.StableDown
	if bad, err := core.Replay(b, history); err == nil || bad != nil {
		t.Fatal("accepted forged consequence")
	}
	battleReject(t, w, gearRequest(w, "revive", core.Operation{Heal: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 1}}))
}
func TestBattleTurnRollbackClockAndStableRecovery(t *testing.T) {
	b := battleFixture()
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "miss", "a", battleAttack(t, w, "a", "b", []int64{1}, nil, false))
	e := w.Snapshot().Battle.State.Encounters[0]
	if e.Cursor != 0 || !e.ActionUsed {
		t.Fatal(e)
	}
	battleReject(t, w, gearRequest(w, "again", battleAttack(t, w, "a", "b", []int64{1}, nil, false)))
	battleExec(t, w, "end", "a", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 0, Actor: "a"}}})
	npc := battleAttack(t, w, "b", "a", nil, nil, true)
	bad := core.Request{ID: "late", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{npc, {Spend: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 100}}}}
	battleReject(t, w, bad)
	battleExec(t, w, "npc", "b", npc)
	s := w.Snapshot()
	if s.Clock.Tick != 6 || s.Battle.State.Encounters[0].Round != 2 || s.Battle.State.Random.Calls <= 1 {
		t.Fatal(s.Battle)
	}
	// Administrative health changes maintain life meaning; normal host admission
	// will not expose them as player Attack substitutes.
	b = battleFixture()
	w = gearWorld(t, b)
	battleExec(t, w, "down", "a", core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 30}})
	if w.Snapshot().Battle.State.Life[0].Outcome != core.StableDown {
		t.Fatal("not stable")
	}
	battleExec(t, w, "heal", "a", core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}})
	if len(w.Snapshot().Battle.State.Life) != 0 {
		t.Fatal("stale down outcome")
	}
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(rebuilt.Snapshot(), w.Snapshot()) {
		t.Fatal(err)
	}
	battleReject(t, w, gearRequest(w, "unknown-zero", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 30}}))
}
func TestBattleDetachedAndConcurrentExactReceipt(t *testing.T) {
	b := battleFixture()
	w := gearWorld(t, b)
	b.Battle.Rules.Seed[0] = 99
	b.Equipment.Definitions[0].Modes[0].Attacks[0].Melee.Reach = 999
	r := battleExec(t, w, "begin", "a", battleBegin())
	r.Events[0].BattleStarted.After.Roster[0].Admission.Initiative = gearPtr(int64(1))
	r.Request.Operations[0].BeginBattle.Participants[0].Position.X = 900
	request := gearRequest(w, "same", battleAttack(t, w, "a", "b", []int64{15}, []int64{4}, false))
	original := core.CanonicalRequest(request)
	results := make(chan core.Batch, 6)
	errs := make(chan error, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := w.Execute(original); results <- r; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for r := range results {
		r.Events[len(r.Events)-1].WeaponAttack.Roll.Attack[0] = 1
		r.Events[len(r.Events)-1].WeaponAttack.Grant.Melee.Reach = 999
	}
	if w.Revision() != 2 {
		t.Fatal(w.Revision())
	}
	receipt, _ := w.Receipt("same")
	if receipt.Events[len(receipt.Events)-1].WeaponAttack.Roll.Attack[0] != 15 || receipt.Events[len(receipt.Events)-1].WeaponAttack.Grant.Melee.Reach != 5 {
		t.Fatal("alias")
	}
}

func TestBattleCurrentPresenceCannotBeRepairedAfterAction(t *testing.T) {
	for _, attack := range []bool{true, false} {
		t.Run(fmt.Sprint(attack), func(t *testing.T) {
			b := battleFixture()
			b.Locations = append(b.Locations, core.Location{ID: "elsewhere", Name: "Elsewhere"})
			w := gearWorld(t, b)
			battleExec(t, w, "begin", "a", battleBegin())
			middle := battleAttack(t, w, "a", "b", []int64{15}, []int64{4}, true)
			if !attack {
				middle = core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 0, Actor: "a"}}}
			}
			battleReject(t, w, gearRequest(w, "absent-action", core.Operation{Move: &core.Move{Character: "a", To: "elsewhere"}}, middle, core.Operation{Move: &core.Move{Character: "a", To: "room"}}))
		})
	}
}

func TestBattleSelectedGrantIgnoresUnrelatedTransientOverflow(t *testing.T) {
	b := battleFixture()
	for i := range b.Characters {
		b.Characters[i].Stats["pressure"] = 0
	}
	other := core.EquipmentAttack{ID: "unselected", Name: "Unselected", ToHit: core.ScalarRecipe{Base: math.MaxInt64, Terms: []core.StatTerm{{Stat: "pressure", Divisor: 1}}}, Damage: core.DamageGrant{Count: 1, Sides: 8, Kind: "slashing", Bonus: core.ScalarRecipe{}}, Melee: &core.MeleeProfile{Reach: 5, Stat: "strength"}}
	b.Equipment.Definitions[0].Modes[0].Attacks = append(b.Equipment.Definitions[0].Modes[0].Attacks, other)
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	view, err := w.EquipmentView("a")
	if err != nil {
		t.Fatal(err)
	}
	var grant core.EquipmentGrantID
	for _, g := range view.Attacks {
		if g.Origin.Member == "slash" {
			grant = g.ID
		}
	}
	attack := battleAttack(t, w, "a", "b", []int64{15}, []int64{4}, false)
	attack.WeaponAttack.Grant = grant
	e := core.Effect{ID: "temporary", Source: core.Ref{Kind: core.CharacterKind, ID: "a"}, Target: core.Ref{Kind: core.CharacterKind, ID: "a"}, Mode: core.OnCharacter, Stat: "pressure", Amount: 1}
	_, err = w.Execute(gearRequest(w, "independent-attack", core.Operation{Apply: &e}, attack, core.Operation{Remove: &core.EffectOp{Effect: e.ID}}))
	if err != nil {
		t.Fatalf("selected finite attack rejected because unrelated grant transiently overflowed: %v", err)
	}
	if err = core.Validate(w.Snapshot().Blueprint); err != nil {
		t.Fatal(err)
	}
}

func TestBattleBiasAndUntrainedArmor(t *testing.T) {
	for _, tc := range []struct {
		name              string
		adv, dis          int
		armor             bool
		faces, damage     []int64
		selected, defense int64
	}{
		{"advantage", 2, 0, false, []int64{1, 20}, []int64{4, 5}, 20, 12},
		{"disadvantage", 0, 2, false, []int64{1, 20}, nil, 1, 12},
		{"cancel", 2, 1, false, []int64{15}, []int64{4}, 15, 12},
		{"untrained", 0, 0, true, []int64{1, 20}, nil, 1, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := battleFixture()
			for _, name := range []core.ConditionTypeID{"adv", "dis", "armor-training"} {
				b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: name, Instances: core.Coexist})
			}
			b.Battle.Rules.RollPolicies = []core.BattleRollPolicy{{Condition: "adv", On: "attacker", Direction: "advantage"}, {Condition: "dis", On: "attacker", Direction: "disadvantage"}}
			for i := 0; i < tc.adv; i++ {
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprint("adv", i)), Type: "adv", Target: "a"})
			}
			for i := 0; i < tc.dis; i++ {
				b.Conditions = append(b.Conditions, core.ConditionInstance{ID: core.ConditionInstanceID(fmt.Sprint("dis", i)), Type: "dis", Target: "a"})
			}
			if tc.armor {
				b.Items[2].Wearer = "a"
				b.Items[2].Equipment.Mode = "worn"
				b.Battle.Rules.ArmorPolicies = []core.BattleArmorPolicy{{Definition: "leather", Mode: "worn", Training: "armor-training", AttackStat: "strength"}}
			}
			w := gearWorld(t, b)
			gearDefense(t, w, "a", tc.defense)
			battleExec(t, w, "begin", "a", battleBegin())
			r := battleExec(t, w, "attack", "a", battleAttack(t, w, "a", "b", tc.faces, tc.damage, false))
			roll := r.Events[len(r.Events)-1].WeaponAttack.Roll
			if roll.Selected != tc.selected || len(roll.Causes) != tc.adv+tc.dis+map[bool]int{true: 1}[tc.armor] {
				t.Fatal(roll)
			}
			gearDefense(t, w, "a", tc.defense)
		})
	}
}
func TestBattleRosterBoundariesAndHistoricalLife(t *testing.T) {
	for _, change := range []func(*core.BeginBattle){
		func(v *core.BeginBattle) { v.Participants[1].Position = v.Participants[0].Position },
		func(v *core.BeginBattle) { v.Participants[1].TieRank = 0 },
		func(v *core.BeginBattle) { v.Participants[1].Controller = core.HumanController },
		func(v *core.BeginBattle) { v.Participants[1].Initiative = gearPtr(int64(15)) },
		func(v *core.BeginBattle) { v.Participants[0].Side = core.OppositionSide },
	} {
		w := gearWorld(t, battleFixture())
		op := battleBegin()
		change(op.BeginBattle)
		battleReject(t, w, gearRequest(w, "invalid", op))
	}
	b := battleFixture()
	c := b.Characters[1]
	c.ID = "c"
	b.Characters = append(b.Characters, c)
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "c"}, Kind: "party-member", To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Strength: 1})
	w := gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "omit-party", battleBegin()))
	b = battleFixture()
	b.Characters[1].Resources["hp"] = core.Resource{Current: 10, Maximum: 10}
	c = b.Characters[1]
	c.ID = "c"
	b.Characters = append(b.Characters, c)
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "kill", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true))
	battleReject(t, w, gearRequest(w, "repeat-scene", battleBegin()))
	op := battleBegin()
	op.BeginBattle.ID = "second"
	op.BeginBattle.Participants[1].Character = "c"
	battleExec(t, w, "second", "a", op)
	if len(w.Snapshot().Battle.State.Life) != 1 || w.Snapshot().Battle.State.Life[0].Character != "b" {
		t.Fatal("lost old death")
	}
	battleReject(t, w, gearRequest(w, "old-dead-heal", core.Operation{Heal: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 1}}))
}
func TestBattleTerminalRoundTimeAndExpiryRollback(t *testing.T) {
	b := battleFixture()
	b.Characters[1].Resources["hp"] = core.Resource{Current: 10, Maximum: 10}
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "expires", Type: "guarded", Target: "a", ClockLifetime: &core.ClockLifetime{AppliedTick: 0, ExpiresTick: 6}})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "pass", "a", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 0, Actor: "a"}}})
	battleExec(t, w, "npc-pass", "b", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 1, Actor: "b"}}})
	if w.Snapshot().Clock.Tick != 6 {
		t.Fatal("round not settled")
	}
	battleExec(t, w, "round2-kill", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true))
	s := w.Snapshot()
	if s.Clock.Tick != 12 {
		t.Fatal(s.Clock)
	}
	for _, c := range s.Conditions {
		if c.ID == "expires" {
			t.Fatal("deadline retained")
		}
	}
	// Expiration removes the cancellation only at terminal Clock settlement.
	b = battleFixture()
	b.Characters[0].Stats["pressure"] = math.MaxInt64
	b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "pressure-down", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "down", Stat: "pressure", Fixed: gearPtr(int64(-1))}}})
	b.Conditions = append(b.Conditions, core.ConditionInstance{ID: "expires", Type: "pressure-down", Target: "a", ClockLifetime: &core.ClockLifetime{ExpiresTick: 6}})
	b.Effects = append(b.Effects, core.Effect{ID: "pressure-up", Source: core.Ref{Kind: core.CharacterKind, ID: "a"}, Target: core.Ref{Kind: core.CharacterKind, ID: "a"}, Mode: core.OnCharacter, Stat: "pressure", Amount: 1})
	b.Characters[1].Resources["hp"] = core.Resource{Current: 10, Maximum: 10}
	w = gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	battleReject(t, w, gearRequest(w, "late-expiry", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true)))
}

func TestBattleAgentDownsPlayerThenWholePartyDefeat(t *testing.T) {
	b := battleFixture()
	b.Characters[0].Resources["hp"] = core.Resource{Current: 5, Maximum: 5}
	b.Characters[1].Stats["initiative"] = 1000
	b.Characters = append(b.Characters, core.Character{ID: "c", Name: "Companion", Location: "room", Stats: map[core.StatID]int64{"strength": 12, "dexterity": 10, "defense": 10, "initiative": -100}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 5, Maximum: 5}}})
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "c"}, Kind: "party-member", To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Strength: 1})
	b.Equipment.Definitions[0].Modes[0].Attacks[0].ToHit = core.ScalarRecipe{Base: 100}
	b.Equipment.Definitions[0].Modes[0].Attacks[0].Damage.Bonus = core.ScalarRecipe{Base: 30}
	begin := battleBegin()
	begin.BeginBattle.Participants = append(begin.BeginBattle.Participants, core.BattleAdmission{Character: "c", Side: core.ProtectedSide, Controller: core.AgentController, Position: core.BattlePosition{Y: 1}, TieRank: 2})
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", begin)
	first := battleExec(t, w, "down-player", "b", battleAttack(t, w, "b", "a", nil, nil, true))
	s := w.Snapshot()
	e := s.Battle.State.Encounters[0]
	if e.Status != core.Active || e.Roster[e.Cursor].Admission.Character != "c" || e.Round != 1 || s.Clock.Tick != 0 || len(s.Battle.State.Life) != 1 || s.Battle.State.Life[0].Character != "a" || s.Battle.State.Life[0].Outcome != core.StableDown || first.Events[len(first.Events)-1].WeaponAttack.Roll.Actual != "5" {
		t.Fatalf("player-down oracle: %+v %+v", s.Battle, first)
	}
	battleReject(t, w, core.Request{ID: "downed-actor", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: e.Cursor, Actor: "a"}}}}})
	battleExec(t, w, "companion-pass", "c", core.Operation{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: e.Cursor, Actor: "c"}}})
	battleReject(t, w, core.Request{ID: "downed-target", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: []core.Operation{battleAttack(t, w, "b", "a", nil, nil, true)}})
	last := battleExec(t, w, "down-companion", "b", battleAttack(t, w, "b", "c", nil, nil, true))
	s = w.Snapshot()
	if s.Battle.State.Encounters[0].Status != core.Defeat || s.Clock.Tick != 12 || len(s.Battle.State.Life) != 2 || s.Battle.State.Life[1].Character != "c" || s.Battle.State.Life[1].Outcome != core.StableDown {
		t.Fatalf("defeat oracle: %+v", s.Battle)
	}
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatalf("replay %v", err)
	}
	retry, err := rebuilt.Execute(last.Request)
	if err != nil || !reflect.DeepEqual(retry, last) || rebuilt.Snapshot().Clock.Tick != 12 {
		t.Fatalf("defeat receipt changed %v", err)
	}
}

func TestBattleOutcomeTraversalWorkLimit(t *testing.T) {
	for _, n := range []int{124, 126} {
		w := gearWorld(t, battleFixture())
		battleExec(t, w, "begin", "a", battleBegin())
		ops := []core.Operation{{EndBattleTurn: &core.EndBattleTurn{BattleWindow: core.BattleWindow{Encounter: "gate", Round: 1, Cursor: 0, Actor: "a"}}}}
		for range n {
			ops = append(ops, core.Operation{Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}})
		}
		if n == 124 {
			battleExec(t, w, "exact128", "a", ops...)
		} else {
			battleReject(t, w, gearRequest(w, "over128", ops...))
		}
	}
}
