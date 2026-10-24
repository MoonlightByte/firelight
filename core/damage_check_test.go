package core_test

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

// concentrationFixture is checkFixture with a concentration rule: a save:dex
// check against DC max(10, damage / 2) up to 30, ended by incapacitated, which
// stunned and unconscious include. The ward on b is what a spell anchors.
func concentrationFixture() core.Blueprint {
	b := checkFixture()
	b.Characters[0].Resources["hp"] = core.Resource{Current: 200, Maximum: 200}
	b.Characters = append(b.Characters, core.Character{ID: "b", Name: "B", Location: "room", Stats: map[core.StatID]int64{"defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 10, Maximum: 10}}})
	for i := range b.ConditionDefinitions {
		if b.ConditionDefinitions[i].ID == "unconscious" {
			b.ConditionDefinitions[i].Includes = []core.ConditionTypeID{"incapacitated"}
		}
	}
	b.ConditionDefinitions = append(b.ConditionDefinitions,
		core.ConditionDefinition{ID: "incapacitated", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "flat", Stat: "flat", Roll: core.Disadvantage}}},
		core.ConditionDefinition{ID: "stunned", Instances: core.Coexist, Includes: []core.ConditionTypeID{"incapacitated"}},
		core.ConditionDefinition{ID: "concentration", Instances: core.Replace, EndsOnCondition: gearPtr(core.ConditionTypeID("incapacitated"))},
		core.ConditionDefinition{ID: "ward", Instances: core.Coexist, Modifiers: []core.ConditionModifier{{ID: "ac", Stat: "defense", Fixed: gearPtr(int64(2))}}},
	)
	b.ConditionChecks = []core.ConditionCheckPolicy{{Condition: "concentration", Resource: "hp", Stat: "save:dex", Sides: 20, Minimum: 10, Divisor: 2, Maximum: 30}}
	return b
}

func wardOp(anchor core.ConditionInstanceID) core.Operation {
	return core.Operation{ApplyCondition: &core.ApplyCondition{Instance: "b:ward", Type: "ward", Target: "b", Anchor: &anchor}}
}

// damageOp damages a, with the player's faces for one instance's check.
func damageOp(instance core.ConditionInstanceID, amount int64, faces ...int64) core.Operation {
	o := core.Operation{Damage: &core.ResourceOp{Character: "a", Resource: "hp", Amount: amount}}
	for _, f := range faces {
		o.ConditionRolls = append(o.ConditionRolls, core.ConditionRollInput{Condition: instance, Target: "a", Face: f})
	}
	return o
}

// damageChecks are the checks damage triggered in a batch.
func damageChecks(batch core.Batch) []core.StatCheckResult {
	var out []core.StatCheckResult
	for _, e := range batch.Events {
		if e.Kind == core.StatCheckResolved && e.StatCheck.Condition != nil {
			out = append(out, *e.StatCheck)
		}
	}
	return out
}

// removed finds the removal event of one instance in a batch.
func removed(t *testing.T, batch core.Batch, id core.ConditionInstanceID) core.ConditionChange {
	t.Helper()
	for _, e := range batch.Events {
		if e.Kind == core.ConditionRemoved && e.Condition.Before.ID == id {
			return *e.Condition
		}
	}
	t.Fatalf("%s not removed: %+v", id, batch.Events)
	return core.ConditionChange{}
}

func present(t *testing.T, w *core.World, id core.ConditionInstanceID, want bool) {
	t.Helper()
	found := false
	for _, c := range w.Snapshot().Conditions {
		found = found || c.ID == id
	}
	if found != want {
		t.Fatalf("%s present %v, want %v", id, found, want)
	}
}

func TestDamageChecksOutsideBattle(t *testing.T) {
	b := concentrationFixture()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, w, "focus", applyOp("a:focus", "concentration", "a", nil), wardOp("a:focus"))
	// 12 damage is the minimum DC 10: 8 + 2 holds.
	batch := execute(t, w, "hit", damageOp("a:focus", 12, 8))
	rs := damageChecks(batch)
	if len(rs) != 1 || *rs[0].Condition != "a:focus" || rs[0].Damage != 12 || *rs[0].Intent.DC != 10 || rs[0].Intent.Stat != "save:dex" || rs[0].Intent.Sides != 20 || *rs[0].Total != 10 || !*rs[0].Success || rs[0].Drawn {
		t.Fatalf("%+v", rs)
	}
	if last := batch.Events[len(batch.Events)-1]; last.Kind != core.DamageApplied {
		t.Fatalf("damage is not the operation's last event: %+v", last)
	}
	present(t, w, "b:ward", true)
	// 40 damage is DC 20: 17 + 2 fails, ending the concentration and the
	// ward anchored to it.
	batch = execute(t, w, "blast", damageOp("a:focus", 40, 17))
	if rs = damageChecks(batch); len(rs) != 1 || *rs[0].Intent.DC != 20 || *rs[0].Success || *rs[0].Margin != -1 {
		t.Fatalf("%+v", rs)
	}
	if c := removed(t, batch, "b:ward"); c.AnchorRemoved == nil || *c.AnchorRemoved != "a:focus" || c.CheckFailed {
		t.Fatalf("%+v", c)
	}
	if c := removed(t, batch, "a:focus"); !c.CheckFailed || c.MinimumResource != nil {
		t.Fatalf("%+v", c)
	}
	if x, _ := w.Explain("b", "defense"); x.Effective != 10 {
		t.Fatalf("ward outlived concentration: %+v", x)
	}
	// Damage with no checked instance checks nothing.
	if rs = damageChecks(execute(t, w, "graze", damageOp("", 1))); len(rs) != 0 {
		t.Fatalf("%+v", rs)
	}
	// 70 damage caps at DC 30. Without faces the world's dice roll, and no
	// d20 reaches 30 + 0 - 2.
	execute(t, w, "focus:2", applyOp("a:focus:2", "concentration", "a", nil))
	// Faces go to the instance they name: these name another, and the check
	// that runs leaves them.
	refused(t, w, "E_ROLL", "faces only for a triggered check", damageOp("a:other", 70, 5))
	calls := w.Snapshot().Dice.State.Calls
	batch = execute(t, w, "crush", damageOp("", 70))
	if rs = damageChecks(batch); len(rs) != 1 || *rs[0].Intent.DC != 30 || !rs[0].Drawn || len(rs[0].Faces) != 1 || rs[0].Intent.Faces != nil || *rs[0].Success || w.Snapshot().Dice.State.Calls != calls+1 {
		t.Fatalf("%+v", rs)
	}
	present(t, w, "a:focus:2", false)
	// The save's roll modes apply: restrained needs two faces, and the lower
	// counts. The fault names the instance and the DC.
	execute(t, w, "focus:3", applyOp("a:focus:3", "concentration", "a", nil), applyOp("a:rope", "restrained", "a", nil))
	refused(t, w, "E_ROLL", "2 faces (disadvantage)", damageOp("a:focus:3", 10, 15))
	_, err = w.Execute(core.Request{ID: "one", ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: []core.Operation{damageOp("a:focus:3", 10, 15)}})
	var f *core.Fault
	if !errors.As(err, &f) || f.Subject != "a:focus:3" || !strings.Contains(f.Message, "damage check DC 10") {
		t.Fatal(err)
	}
	batch = execute(t, w, "sting", damageOp("a:focus:3", 10, 15, 12))
	if rs = damageChecks(batch); len(rs) != 1 || rs[0].Roll != core.Disadvantage || rs[0].Kept != 12 || !*rs[0].Success {
		t.Fatalf("%+v", rs)
	}
	// Faces no check takes are refused, whoever they name.
	refused(t, w, "E_ROLL", "faces only for a triggered check", damageOp("a:focus:3", 10, 15, 12, 9))
	refused(t, w, "E_ROLL", "faces only for a triggered check", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 1}, ConditionRolls: []core.ConditionRollInput{{Condition: "a:focus:3", Target: "a", Face: 3}}})
	// At 0 hp unconscious is held and includes incapacitated: concentration
	// ends with the Resource, before any check.
	batch = execute(t, w, "fall", damageOp("", 500))
	if rs = damageChecks(batch); len(rs) != 0 {
		t.Fatalf("%+v", rs)
	}
	if c := removed(t, batch, "a:focus:3"); c.MinimumResource == nil || *c.MinimumResource != "hp" || c.CheckFailed {
		t.Fatalf("%+v", c)
	}
	// Nor can concentration begin while unconscious is held.
	refused(t, w, "E_CONDITION", "blocking Condition absent on target", applyOp("a:focus:4", "concentration", "a", nil))
	replayAndSave(t, b, w)
}

func TestDamageCheckWithoutRandomSource(t *testing.T) {
	b := concentrationFixture()
	b.Dice = nil
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, w, "focus", applyOp("a:focus", "concentration", "a", nil))
	refused(t, w, "E_ROLL", "1 face (normal) or a world random source", damageOp("", 4))
	r := damageChecks(execute(t, w, "hit", damageOp("a:focus", 4, 20)))
	if len(r) != 1 || !*r[0].Success {
		t.Fatalf("%+v", r)
	}
	// Supplied faces must fit the policy's die.
	refused(t, w, "E_ROLL", "", damageOp("a:focus", 4, 21))
	replayAndSave(t, b, w)
}

func TestConditionIncludes(t *testing.T) {
	b := concentrationFixture()
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	// Applying a type that includes the blocker ends the maintained instance.
	execute(t, w, "focus", applyOp("a:focus", "concentration", "a", nil))
	batch := execute(t, w, "stun", applyOp("a:stun", "stunned", "a", nil))
	if c := removed(t, batch, "a:focus"); c.EndingCondition == nil || *c.EndingCondition != "a:stun" {
		t.Fatalf("%+v", c)
	}
	// While it is present the maintained type cannot begin.
	refused(t, w, "E_CONDITION", "blocking Condition absent on target", applyOp("a:focus:2", "concentration", "a", nil))
	// Inclusion is presence only: incapacitated's roll modifier stays its own.
	rolls(t, w, "flat", "")
	execute(t, w, "daze", applyOp("a:daze", "incapacitated", "a", nil))
	rolls(t, w, "flat", core.Disadvantage, "incapacitated")
	execute(t, w, "wake", removeOp("a:stun", "a"), removeOp("a:daze", "a"))
	execute(t, w, "focus:2", applyOp("a:focus:2", "concentration", "a", nil))
	replayAndSave(t, b, w)
}

func TestConditionIncludeRefusals(t *testing.T) {
	var f *core.Fault
	many := make([]core.ConditionTypeID, core.MaxConditionIncludes+1)
	for i := range many {
		many[i] = core.ConditionTypeID(fmt.Sprint("t", i))
	}
	for _, tc := range []struct {
		code     string
		includes []core.ConditionTypeID
	}{
		{"E_LIMIT", many},
		{"E_CONDITION_TYPE", []core.ConditionTypeID{"self"}},
		{"E_CONDITION_TYPE", []core.ConditionTypeID{"x", "x"}},
		{"E_CONDITION_TYPE", []core.ConditionTypeID{" "}},
	} {
		d := core.ConditionDefinition{ID: "self", Instances: core.Coexist, Includes: tc.includes}
		if err := core.CheckConditionDefinition(d); !errors.As(err, &f) || f.Code != tc.code || f.Field != "includes" {
			t.Errorf("%v: want %s, got %v", tc.includes, tc.code, err)
		}
	}
	// References must be declared and one level deep.
	for _, tc := range []struct {
		code, expected string
		includes       core.ConditionTypeID
	}{
		{"E_CONDITION_TYPE", "declared Condition type", "missing"},
		{"E_RULE", "type that includes no other", "stunned"},
	} {
		b := concentrationFixture()
		b.ConditionDefinitions = append(b.ConditionDefinitions, core.ConditionDefinition{ID: "worse", Instances: core.Coexist, Includes: []core.ConditionTypeID{tc.includes}})
		if err := core.Validate(b); !errors.As(err, &f) || f.Code != tc.code || f.Expected != tc.expected || f.Field != "includes" {
			t.Errorf("%s: want %s, got %v", tc.includes, tc.code, err)
		}
	}
}

func TestWorldConditionCheckRefusals(t *testing.T) {
	var f *core.Fault
	// A Battle world keeps its checks in its rules.
	bb := battleFixture()
	bb.ConditionDefinitions = append(bb.ConditionDefinitions, core.ConditionDefinition{ID: "concentration", Instances: core.Replace})
	bb.ConditionChecks = concentrationFixture().ConditionChecks
	if err := core.Validate(bb); !errors.As(err, &f) || f.Code != "E_RULE" || f.Field != "condition_checks" {
		t.Fatal(err)
	}
	policy := concentrationFixture().ConditionChecks[0]
	for name, mutate := range map[string]func(*core.ConditionCheckPolicy){
		"type":      func(p *core.ConditionCheckPolicy) { p.Condition = "missing" },
		"resource":  func(p *core.ConditionCheckPolicy) { p.Resource = "" },
		"stat":      func(p *core.ConditionCheckPolicy) { p.Stat = "" },
		"one side":  func(p *core.ConditionCheckPolicy) { p.Sides = 1 },
		"big die":   func(p *core.ConditionCheckPolicy) { p.Sides = 1001 },
		"minimum":   func(p *core.ConditionCheckPolicy) { p.Minimum = 0 },
		"divisor":   func(p *core.ConditionCheckPolicy) { p.Divisor = 0 },
		"maximum":   func(p *core.ConditionCheckPolicy) { p.Maximum = 9 },
		"duplicate": nil,
	} {
		b := concentrationFixture()
		if mutate == nil {
			b.ConditionChecks = append(b.ConditionChecks, policy)
		} else {
			mutate(&b.ConditionChecks[0])
		}
		if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_RULE" || f.Field != "condition_check" {
			t.Errorf("%s: %v", name, err)
		}
	}
	b := concentrationFixture()
	for i := range 16 {
		p := policy
		p.Resource = core.ResourceID(fmt.Sprint("r", i))
		b.ConditionChecks = append(b.ConditionChecks, p)
	}
	if err := core.Validate(b); !errors.As(err, &f) || f.Code != "E_LIMIT" || f.Field != "condition_checks" {
		t.Fatal(err)
	}
	// A checked type needs its target to hold the Resource and the stat.
	w, err := core.NewWorld(concentrationFixture())
	if err != nil {
		t.Fatal(err)
	}
	refused(t, w, "E_STAT", "", applyOp("b:focus", "concentration", "b", nil))
}
