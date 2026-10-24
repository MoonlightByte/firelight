package tests

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math"
	"os"
	"strings"
	"testing"
)

// SRD 5.2.1 character stats from examples/srd-stats (docs/CHARACTER_STATS.md).
// Every expected number is worked by hand from the SRD rules, not recomputed.

var srdAbilities = []string{"strength", "dexterity", "constitution", "intelligence", "wisdom", "charisma"}

var srdSkills = []string{"acrobatics", "animal-handling", "arcana", "athletics", "deception", "history",
	"insight", "intimidation", "investigation", "medicine", "nature", "perception",
	"performance", "persuasion", "religion", "sleight-of-hand", "stealth", "survival"}

func srdStatsFile(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("../examples/srd-stats/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// srdStatsBlock returns the lines between a BEGIN/END marker pair.
func srdStatsBlock(t testing.TB, name string) string {
	t.Helper()
	s := srdStatsFile(t, "world.nql")
	begin, end := "// BEGIN srd-stats:"+name+"\n", "// END srd-stats:"+name
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < i {
		t.Fatalf("marker %s", name)
	}
	return s[i+len(begin) : strings.LastIndex(s[:j], "\n")+1]
}

func srdStatsFixture(t testing.TB, edit func(string) string) core.Blueprint {
	t.Helper()
	s := srdStatsFile(t, "world.nql")
	if edit != nil {
		s = edit(s)
	}
	b, ds := compiler.CompileWorld("srd-stats.nql", s)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func statsRequest(t testing.TB, w *core.World, id, src string) core.Request {
	t.Helper()
	return statsRequestAs(t, w, id, src, "char:fighter")
}

func statsRequestAs(t testing.TB, w *core.World, id, src, actor string) core.Request {
	t.Helper()
	s := w.Snapshot()
	ops, ds := compiler.CompileActions("update.nql", src, s.Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: actor}, Operations: ops}
}

func statsExecute(t testing.TB, w *core.World, id, src string) core.Batch {
	t.Helper()
	return commitC(t, w, statsRequest(t, w, id, src))
}

// srdSheet lists abilities, checks and saves in srdAbilities order, and skills
// in srdSkills order.
func srdSheet(scores, checks, saves, skills []int64, extra map[string]int64) map[core.StatID]int64 {
	out := map[core.StatID]int64{}
	for i, a := range srdAbilities {
		out[core.StatID(a)] = scores[i]
		out[core.StatID("mod:"+a)] = (scores[i] - 10) >> 1
		out[core.StatID("check:"+a)] = checks[i]
		out[core.StatID("save:"+a)] = saves[i]
	}
	for i, s := range srdSkills {
		out[core.StatID("skill:"+s)] = skills[i]
	}
	for k, v := range extra {
		out[core.StatID(k)] = v
	}
	return out
}

func wantSheet(t testing.TB, w *core.World, id core.CharacterID, want map[core.StatID]int64) {
	t.Helper()
	p, err := w.CharacterStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[core.StatID]int64{}
	for _, s := range p.Stats {
		got[s.Stat] = s.Effective
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s %s = %d (declared %v), want %d", id, k, g, ok, v)
		}
	}
}

var fighterSheet = srdSheet(
	[]int64{16, 12, 15, 8, 13, 10},
	[]int64{3, 1, 2, -1, 1, 0},
	[]int64{5, 1, 4, -1, 1, 0},
	[]int64{1, 1, -1, 5, 0, -1, 1, 0, -1, 1, -1, 3, 0, 0, -1, 1, 1, 1},
	map[string]int64{"level": 1, "speed": 30, "proficiency": 2, "initiative": 3, "passive:perception": 13, "defense": 11,
		"bonus:saves": 0, "bonus:checks": 0, "bonus:d20": 0, "xp": 0, "xp:next": 300, "xp:pending": 0})

var bardSheet = srdSheet(
	[]int64{8, 14, 12, 10, 13, 17},
	[]int64{-1, 2, 1, 0, 1, 3},
	[]int64{-1, 5, 1, 0, 1, 6},
	[]int64{3, 2, 1, 0, 6, 1, 4, 4, 1, 2, 1, 2, 9, 9, 1, 3, 3, 2},
	map[string]int64{"level": 5, "proficiency": 3, "initiative": 2, "passive:perception": 12, "defense": 12,
		"spell-dc:charisma": 14, "spell-attack:charisma": 6, "xp": 6500, "xp:next": 14000, "xp:pending": 0})

var rogueSheet = srdSheet(
	[]int64{10, 20, 14, 14, 12, 8},
	[]int64{0, 5, 2, 2, 1, -1},
	[]int64{0, 11, 2, 8, 1, -1},
	[]int64{11, 1, 2, 0, -1, 2, 1, -1, 8, 1, 2, 13, -1, -1, 2, 5, 17, 1},
	map[string]int64{"level": 17, "proficiency": 6, "initiative": 5, "passive:perception": 23, "defense": 15,
		"xp": 230000, "xp:next": 265000, "xp:pending": 0})

func edited(base map[core.StatID]int64, changes map[string]int64) map[core.StatID]int64 {
	out := map[core.StatID]int64{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range changes {
		out[core.StatID(k)] = v
	}
	return out
}

func TestSRDStatsSheets(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	wantSheet(t, w, "char:fighter", fighterSheet)
	wantSheet(t, w, "char:bard", bardSheet)
	wantSheet(t, w, "char:rogue", rogueSheet)

	// Casters alone declare spell stats; a derived rule never adds a stat.
	if _, err := w.Explain("char:fighter", "spell-dc:charisma"); err == nil {
		t.Fatal("fighter gained an undeclared spell stat")
	}
	p, err := w.CharacterStatus("char:bard")
	if err != nil || len(p.Stats) != 56 {
		t.Fatal("bard status stats", len(p.Stats), err)
	}
}

func TestSRDStatsExplainGatedTerms(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	active := func(want ...bool) {
		t.Helper()
		e, err := w.Explain("char:bard", "skill:athletics")
		if err != nil || e.Recipe == nil || len(e.Recipe.Terms) != len(want) {
			t.Fatal(e, err)
		}
		for i, x := range e.Recipe.Terms {
			if x.Active != want[i] {
				t.Fatalf("term %d active %v, want %v: %+v", i, x.Active, want[i], e.Recipe.Terms)
			}
		}
	}
	// Strength, proficiency, expertise, Jack of All Trades, checks, d20.
	active(true, false, false, true, true, true)
	stat(t, w, "char:bard", "skill:athletics", 0)
	statsExecute(t, w, "train", `apply condition "char:bard:prof:skill:athletics" of "prof:skill:athletics" to "char:bard";`)
	active(true, true, false, false, true, true)
	stat(t, w, "char:bard", "skill:athletics", 2)
	statsExecute(t, w, "forget", `remove condition "char:bard:prof:skill:athletics" from "char:bard";`)
	active(true, false, false, true, true, true)
	stat(t, w, "char:bard", "skill:athletics", 0)

	// Losing Jack of All Trades removes the half bonus from every skill
	// the bard lacks, and from nothing else.
	statsExecute(t, w, "lose", `remove condition "char:bard:feature:jack-of-all-trades" from "char:bard";`)
	wantSheet(t, w, "char:bard", edited(bardSheet, map[string]int64{
		"skill:acrobatics": 2, "skill:animal-handling": 1, "skill:arcana": 0, "skill:athletics": -1, "skill:history": 0,
		"skill:intimidation": 3, "skill:investigation": 0, "skill:medicine": 1, "skill:nature": 0, "skill:perception": 1,
		"skill:religion": 0, "skill:sleight-of-hand": 2, "skill:stealth": 2, "skill:survival": 1, "passive:perception": 11}))
	costReplay(t, b, w)
}

// The example update files are what an agent sends: facts in, no totals.
func TestSRDStatsAgentUpdates(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	statsExecute(t, w, "level-up", srdStatsFile(t, "level-up.nql"))
	// Levels taken without XP, as in a milestone game, leave the XP 4 levels
	// behind.
	levelled := edited(fighterSheet, map[string]int64{
		"level": 5, "proficiency": 3, "strength": 18, "mod:strength": 4, "check:strength": 4,
		"save:strength": 7, "save:constitution": 5, "skill:athletics": 7, "skill:perception": 4,
		"initiative": 4, "passive:perception": 14, "xp:next": 14000, "xp:pending": -4})
	wantSheet(t, w, "char:fighter", levelled)
	wantResource(t, w, "char:fighter", "hp", 44, 44)

	statsExecute(t, w, "proficiency", srdStatsFile(t, "proficiency.nql"))
	levelled = edited(levelled, map[string]int64{"skill:stealth": 4, "skill:insight": 4})
	wantSheet(t, w, "char:fighter", levelled)
	wantSheet(t, w, "char:bard", edited(bardSheet, map[string]int64{"skill:athletics": 2}))

	// Expertise doubles the proficiency bonus on top of proficiency.
	statsExecute(t, w, "expertise", `apply condition "char:fighter:expertise:skill:stealth" of "expertise:skill:stealth" to "char:fighter";`)
	stat(t, w, "char:fighter", "skill:stealth", 7)

	// A proficiency is had once; the engine refuses a second and changes nothing.
	before, history := w.Snapshot(), w.History()
	r := statsRequest(t, w, "twice", `apply condition "char:fighter:prof:skill:stealth:2" of "prof:skill:stealth" to "char:fighter";`)
	if _, err := w.Execute(r); err == nil {
		t.Fatal("second Stealth proficiency accepted")
	}
	unchanged(t, w, before, history)

	// Level drain is the removal of the level record; the bonuses follow.
	statsExecute(t, w, "drain", `remove effect "char:fighter:level:5";`)
	stat(t, w, "char:fighter", "proficiency", 2)
	stat(t, w, "char:fighter", "skill:stealth", 5)
	stat(t, w, "char:fighter", "save:strength", 6)
	costReplay(t, b, w)
}

func TestSRDStatsConditionEffects(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)

	// A Cloak of Protection adds 1 to AC and to every save.
	statsExecute(t, w, "cloak", `apply condition "char:fighter:cloak" of "item:cloak-of-protection" to "char:fighter";`)
	cloaked := edited(fighterSheet, map[string]int64{"defense": 12, "bonus:saves": 1,
		"save:strength": 6, "save:dexterity": 2, "save:constitution": 5, "save:intelligence": 0, "save:wisdom": 2, "save:charisma": 1})
	wantSheet(t, w, "char:fighter", cloaked)

	// Two Exhaustion levels: -4 on every d20 Test and 10 feet of speed. A
	// passive score is not a roll, so it keeps its value.
	statsExecute(t, w, "tired", `apply condition "char:fighter:exhaustion:1" of "state:exhaustion" to "char:fighter"; apply condition "char:fighter:exhaustion:2" of "state:exhaustion" to "char:fighter";`)
	tired := map[string]int64{"speed": 20, "bonus:d20": -4, "initiative": -1, "passive:perception": 13}
	for _, a := range srdAbilities {
		tired["check:"+a] = fighterSheet[core.StatID("check:"+a)] - 4
		tired["save:"+a] = cloaked[core.StatID("save:"+a)] - 4
	}
	for _, s := range srdSkills {
		tired["skill:"+s] = fighterSheet[core.StatID("skill:"+s)] - 4
	}
	wantSheet(t, w, "char:fighter", edited(cloaked, tired))

	// A Stone of Good Luck adds 1 to checks and saves, and so to passive
	// Perception and Initiative, but not to a spell save DC.
	statsExecute(t, w, "luck", `apply condition "char:bard:stone" of "item:stone-of-good-luck" to "char:bard";`)
	lucky := map[string]int64{"bonus:checks": 1, "bonus:saves": 1, "initiative": 3, "passive:perception": 13}
	for _, a := range srdAbilities {
		lucky["check:"+a] = bardSheet[core.StatID("check:"+a)] + 1
		lucky["save:"+a] = bardSheet[core.StatID("save:"+a)] + 1
	}
	for _, s := range srdSkills {
		lucky["skill:"+s] = bardSheet[core.StatID("skill:"+s)] + 1
	}
	wantSheet(t, w, "char:bard", edited(bardSheet, lucky))
	stat(t, w, "char:bard", "spell-dc:charisma", 14)

	// A derived total takes direct contributions too.
	statsExecute(t, w, "boots", `apply effect "char:rogue:boots" from character "char:rogue" on character "char:rogue" stat "skill:stealth" add 2;`)
	stat(t, w, "char:rogue", "skill:stealth", 19)
	costReplay(t, b, w)
}

// The SRD conditions change totals only through their numeric parts: Speed 0,
// Exhaustion levels, and Unconscious while hit points are at 0.
func TestSRDStatsConditions(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	statsExecute(t, w, "conditions", srdStatsFile(t, "conditions.nql"))

	// Fighter: grappled, two Exhaustion levels, Gauntlets of Ogre Power.
	// Strength 19 lifts the Strength modifier from 3 to 4; Exhaustion takes 4
	// from every d20 Test but not from passive Perception.
	tired := map[string]int64{"strength": 19, "mod:strength": 4, "speed": 0, "bonus:d20": -4, "initiative": -1, "passive:perception": 13}
	for _, a := range srdAbilities {
		tired["check:"+a] = fighterSheet[core.StatID("check:"+a)] - 4
		tired["save:"+a] = fighterSheet[core.StatID("save:"+a)] - 4
	}
	for _, s := range srdSkills {
		tired["skill:"+s] = fighterSheet[core.StatID("skill:"+s)] - 4
	}
	for _, k := range []string{"check:strength", "save:strength", "skill:athletics"} {
		tired[k]++
	}
	wantSheet(t, w, "char:fighter", edited(fighterSheet, tired))

	// Bard: a hill giant Belt (Strength 21, modifier 5) and 0 hit points.
	// Athletics is 5 + 1 (Jack of All Trades).
	belted := map[string]int64{"strength": 21, "mod:strength": 5, "check:strength": 5, "save:strength": 5, "skill:athletics": 6, "speed": 0}
	wantSheet(t, w, "char:bard", edited(bardSheet, belted))
	heldTypes := func(id core.CharacterID) []core.HeldCondition {
		p, err := w.CharacterStatus(id)
		if err != nil {
			t.Fatal(err)
		}
		return p.Held
	}
	if h := heldTypes("char:bard"); len(h) != 1 || h[0] != (core.HeldCondition{Type: "state:unconscious", Resource: "hp"}) {
		t.Fatal("bard at 0 HP", h)
	}

	// A second grappler keeps Speed 0 until both let go; a Long Rest takes
	// one Exhaustion level.
	statsExecute(t, w, "wolf", `apply condition "char:fighter:grappled:wolf" of "state:grappled" to "char:fighter" from character "char:bard";`)
	statsExecute(t, w, "ogre-lets-go", `remove condition "char:fighter:grappled:ogre" from "char:fighter";`)
	stat(t, w, "char:fighter", "speed", 0)
	statsExecute(t, w, "free", `remove condition "char:fighter:grappled:wolf" from "char:fighter";`)
	stat(t, w, "char:fighter", "speed", 20)
	statsExecute(t, w, "long-rest", `remove condition "char:fighter:exhaustion:2" from "char:fighter";`)
	stat(t, w, "char:fighter", "speed", 25)
	stat(t, w, "char:fighter", "skill:athletics", 4)
	statsExecute(t, w, "gauntlets-off", `remove condition "char:fighter:gauntlets" from "char:fighter";`)
	stat(t, w, "char:fighter", "skill:athletics", 3)

	// Any healing wakes the bard; knocked out at 1 HP, it stays Unconscious
	// until the agent removes the instance.
	statsExecute(t, w, "cure", `heal "char:bard" resource "hp" by 1;`)
	stat(t, w, "char:bard", "speed", 30)
	if h := heldTypes("char:bard"); len(h) != 0 {
		t.Fatal("healed bard", h)
	}
	statsExecute(t, w, "knockout", `apply condition "char:bard:knocked-out" of "state:unconscious" to "char:bard";`)
	stat(t, w, "char:bard", "speed", 0)
	statsExecute(t, w, "first-aid", `remove condition "char:bard:knocked-out" from "char:bard";`)
	stat(t, w, "char:bard", "speed", 30)
	costReplay(t, b, w)
}

// Checks and saves add the kept face to the sheet's total. Faces come from
// the player or from the world's dice; roll modes come from the request and
// the SRD conditions.
func TestSRDStatsChecks(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	batch := statsExecute(t, w, "checks", srdStatsFile(t, "checks.nql"))
	var got []core.StatCheckResult
	for _, e := range batch.Events {
		if e.StatCheck != nil {
			got = append(got, *e.StatCheck)
		}
	}
	if len(got) != 7 {
		t.Fatalf("%d checks", len(got))
	}
	type outcome struct {
		roll               core.RollMode
		kept, bonus, total int64
		success            bool
		margin             int64
		sources            []core.ConditionTypeID
	}
	want := func(i int, o outcome) {
		t.Helper()
		r := got[i]
		var types []core.ConditionTypeID
		for _, s := range r.Sources {
			types = append(types, s.Type)
		}
		if r.Roll != o.roll || r.Kept != o.kept || r.Bonus != o.bonus || r.Total == nil || *r.Total != o.total || r.Success == nil || *r.Success != o.success || *r.Margin != o.margin || fmt.Sprint(types) != fmt.Sprint(o.sources) || r.Drawn {
			t.Errorf("check %d: %+v", i, r)
		}
	}
	// Poisoned Athletics 5 rolls with disadvantage: 6 + 5 = 11 against 15.
	want(0, outcome{core.Disadvantage, 6, 5, 11, false, -4, []core.ConditionTypeID{"state:poisoned"}})
	// The bard's Dexterity save 5 with advantage: 15 + 5 = 20 against 13.
	want(1, outcome{core.Advantage, 15, 5, 20, true, 7, nil})
	// Restrained disadvantage cancels the requested advantage: one face.
	want(3, outcome{"", 12, 11, 23, true, 8, []core.ConditionTypeID{"state:restrained"}})
	// Unconscious at 0 HP fails Dexterity saves but not Wisdom saves.
	want(5, outcome{"", 11, 1, 12, false, -1, nil})
	if r := got[4]; r.Roll != core.AutoFail || r.Success == nil || *r.Success || r.Total != nil || r.Faces != nil ||
		len(r.Sources) != 1 || r.Sources[0] != (core.RollSource{HeldAtMinimum: "hp", Type: "state:unconscious", Modifier: "save:dexterity", Stat: "save:dexterity", Roll: core.AutoFail}) {
		t.Errorf("unconscious save: %+v", r)
	}
	// The dice roll Stealth 17 once, and the poisoned fighter's Initiative 3
	// twice: Initiative is a Dexterity check, so it reads bonus:checks.
	if r := got[2]; !r.Drawn || len(r.Faces) != 1 || *r.Total != r.Kept+17 || *r.Success != (*r.Total >= 25) || *r.Margin != *r.Total-25 {
		t.Errorf("stealth: %+v", r)
	}
	if r := got[6]; !r.Drawn || r.Roll != core.Disadvantage || len(r.Faces) != 2 || r.Kept != min(r.Faces[0], r.Faces[1]) || *r.Total != r.Kept+3 || r.Success != nil || r.Margin != nil {
		t.Errorf("initiative: %+v", r)
	}
	// Roll modes never change a total.
	x, err := w.Explain("char:fighter", "skill:athletics")
	if err != nil || x.Effective != 5 || x.Roll != core.Disadvantage {
		t.Fatal("athletics", x, err)
	}
	stat(t, w, "char:fighter", "passive:perception", 13)
	costReplay(t, b, w)
}

// Concentration holds a spell's effects. A Constitution save on damage keeps
// it; a failed save, Incapacitated or a new concentration ends it and the
// effects anchored to it.
func TestSRDStatsConcentration(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	type ended = map[core.ConditionInstanceID]core.ConditionChange
	run := func(id, src string) ([]core.StatCheckResult, ended) {
		t.Helper()
		var checks []core.StatCheckResult
		removed := ended{}
		for _, e := range statsExecute(t, w, id, src).Events {
			if e.Kind == core.StatCheckResolved {
				checks = append(checks, *e.StatCheck)
			}
			if e.Kind == core.ConditionRemoved {
				removed[e.Condition.Before.ID] = *e.Condition
			}
		}
		return checks, removed
	}
	// The bard's save:constitution is +1: Constitution 12, no proficiency.
	checks, removed := run("concentration", srdStatsFile(t, "concentration.nql"))
	if len(checks) != 2 || *checks[0].Intent.DC != 10 || *checks[0].Total != 10 || !*checks[0].Success || checks[0].Damage != 12 ||
		*checks[1].Intent.DC != 10 || *checks[1].Total != 5 || *checks[1].Success || checks[1].Damage != 20 {
		t.Fatalf("%+v", checks)
	}
	if c := removed["char:bard:concentration"]; !c.CheckFailed {
		t.Fatalf("%+v", c)
	}
	if c := removed["char:fighter:shield-of-faith"]; c.AnchorRemoved == nil || *c.AnchorRemoved != "char:bard:concentration" {
		t.Fatalf("%+v", c)
	}
	if c := removed["char:bard:concentration:2"]; c.EndingCondition == nil || *c.EndingCondition != "char:bard:stunned" {
		t.Fatalf("%+v", c)
	}
	stat(t, w, "char:fighter", "defense", 11)
	// While Stunned the bard cannot concentrate.
	s := w.Snapshot()
	ops, ds := compiler.CompileActions("focus.nql", `apply condition "char:bard:concentration:3" of "state:concentration" to "char:bard";`, s.Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	var f *core.Fault
	if _, err := w.Execute(core.Request{ID: "stunned-focus", ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:bard"}, Operations: ops}); !errors.As(err, &f) || f.Code != "E_CONDITION" {
		t.Fatal(err)
	}
	// At 0 hit points Unconscious, which includes Incapacitated, ends the
	// concentration before any save.
	run("recover", `remove condition "char:bard:stunned" from "char:bard";
apply condition "char:bard:concentration:3" of "state:concentration" to "char:bard";`)
	checks, removed = run("fall", `damage "char:bard" resource "hp" by 5;`)
	if c := removed["char:bard:concentration:3"]; len(checks) != 0 || c.MinimumResource == nil || *c.MinimumResource != "hp" {
		t.Fatalf("%+v %+v", checks, c)
	}
	// Nor can the bard concentrate while held Unconscious.
	s = w.Snapshot()
	ops, ds = compiler.CompileActions("focus.nql", `apply condition "char:bard:concentration:4" of "state:concentration" to "char:bard";`, s.Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, err := w.Execute(core.Request{ID: "unconscious-focus", ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:bard"}, Operations: ops}); !errors.As(err, &f) || f.Code != "E_CONDITION" {
		t.Fatal(err)
	}
	// A new concentration replaces the old one and its shield.
	run("rest", `heal "char:bard" resource "hp" by 33;
apply condition "char:bard:concentration:4" of "state:concentration" to "char:bard";
apply condition "char:fighter:shield-of-faith:2" of "spell:shield-of-faith" to "char:fighter" anchor "char:bard:concentration:4";`)
	stat(t, w, "char:fighter", "defense", 13)
	_, removed = run("recast", `apply condition "char:bard:concentration:5" of "state:concentration" to "char:bard";`)
	if c := removed["char:bard:concentration:4"]; c.ReplacedBy == nil || *c.ReplacedBy != "char:bard:concentration:5" {
		t.Fatalf("%+v", c)
	}
	if _, ok := removed["char:fighter:shield-of-faith:2"]; !ok {
		t.Fatal("shield outlived its concentration")
	}
	// Without the player's faces the world's dice roll the save; half of 30
	// damage is DC 15.
	checks, _ = run("hit", `damage "char:bard" resource "hp" by 30;`)
	if len(checks) != 1 || !checks[0].Drawn || *checks[0].Intent.DC != 15 || *checks[0].Total != checks[0].Kept+1 {
		t.Fatalf("%+v", checks)
	}
	costReplay(t, b, w)
}

// The concentration block needs only the SRD conditions and the save.
func TestSRDStatsConcentrationBlockStandsAlone(t *testing.T) {
	s := "rules { transfer unequips; wear any; }\nlocation \"loc:hall\" named \"Hall\";\n" +
		`character "char:a" named "A" at "loc:hall" { stat "save:constitution" = 3; resource "hp" = 20 min 0 max 20; }
condition "char:a:concentration" of "state:concentration" to "char:a";
` + srdStatsBlock(t, "srd-conditions") + srdStatsBlock(t, "concentration")
	b, ds := compiler.CompileWorld("alone.nql", s)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	batch := commitC(t, w, statsRequestAs(t, w, "hit", `damage "char:a" resource "hp" by 4 faces "char:a:concentration" 6;`, "char:a"))
	for _, e := range batch.Events {
		if e.Kind == core.StatCheckResolved && (*e.StatCheck.Total != 9 || *e.StatCheck.Success) {
			t.Fatalf("%+v", e.StatCheck)
		}
	}
	if len(w.Snapshot().Conditions) != 0 {
		t.Fatal("a failed save kept concentration")
	}
}

func TestSRDStatsExperience(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	sheet := func(id core.CharacterID, level, xp, next, pending int64) {
		t.Helper()
		stat(t, w, id, "level", level)
		stat(t, w, id, "xp", xp)
		stat(t, w, id, "xp:next", next)
		stat(t, w, id, "xp:pending", pending)
	}
	// 300 XP each: the fighter reaches level 2 at 300; the bard, 6,500 at
	// level 5, and the rogue, 230,000 at level 17, stay short of 14,000 and
	// 265,000.
	batch := statsExecute(t, w, "award", srdStatsFile(t, "xp.nql"))
	if a := batch.Events[1].StatAdjustment; len(batch.Events) != 3 || batch.Events[1].Kind != core.StatAdjusted || a.Character != "char:bard" || a.Stat != "xp" || a.Amount != 300 || a.Before != 6500 || a.After != 6800 {
		t.Fatalf("%+v", batch.Events)
	}
	sheet("char:fighter", 1, 300, 300, 1)
	sheet("char:bard", 5, 6800, 14000, 0)
	sheet("char:rogue", 17, 230300, 265000, 0)
	// The agent takes the level; the XP stays.
	statsExecute(t, w, "level", `apply effect "char:fighter:level:2" from character "char:fighter" on character "char:fighter" stat "level" add 1;`)
	sheet("char:fighter", 2, 300, 900, 0)
	// 13,999 is one short of level 6, 14,000 reaches it, and 23,000 reaches
	// level 7 as well.
	statsExecute(t, w, "short", `adjust "char:bard" stat "xp" by 7199;`)
	sheet("char:bard", 5, 13999, 14000, 0)
	statsExecute(t, w, "reach", `adjust "char:bard" stat "xp" by 1;`)
	sheet("char:bard", 5, 14000, 14000, 1)
	statsExecute(t, w, "jump", `adjust "char:bard" stat "xp" by 9000;`)
	sheet("char:bard", 5, 23000, 14000, 2)
	x, err := w.Explain("char:bard", "xp:pending")
	if err != nil || len(x.Recipe.Terms) != 2 || len(x.Recipe.Terms[0].Term.Steps) != 20 || x.Recipe.Terms[0].Input != 23000 || x.Recipe.Terms[0].Exact != "7" || x.Recipe.Terms[1].Input != 5 || x.Recipe.Terms[1].Exact != "-5" {
		t.Fatalf("%+v %v", x, err)
	}
	// 355,000 is level 20, three above the rogue's 17. Level 20 has no next
	// level, and more XP earns none.
	statsExecute(t, w, "epic", `adjust "char:rogue" stat "xp" by 124700;`)
	sheet("char:rogue", 17, 355000, 265000, 3)
	statsExecute(t, w, "twenty", `apply effect "char:rogue:level:18" from character "char:rogue" on character "char:rogue" stat "level" add 1;
apply effect "char:rogue:level:19" from character "char:rogue" on character "char:rogue" stat "level" add 1;
apply effect "char:rogue:level:20" from character "char:rogue" on character "char:rogue" stat "level" add 1;
adjust "char:rogue" stat "xp" by 1000000;`)
	sheet("char:rogue", 20, 1355000, 0, 0)
	// A correction takes XP away. A level ahead of the XP shows as negative.
	statsExecute(t, w, "undo", `adjust "char:fighter" stat "xp" by -300;`)
	sheet("char:fighter", 2, 0, 900, -1)

	// A total is derived, never adjusted, and the stat must be declared.
	s := w.Snapshot()
	for _, tc := range []struct{ src, code string }{
		{`adjust "char:bard" stat "xp:next" by 1;`, "E_RULE"},
		{`adjust "char:fighter" stat "spell-dc:charisma" by 1;`, "E_STAT"},
		{`adjust "char:bard" stat "xp" by 0;`, "E_AMOUNT"},
	} {
		if _, ds := compiler.CompileActions("bad.nql", tc.src, s.Blueprint); len(ds) != 1 || ds[0].Code != tc.code {
			t.Fatal(tc.src, ds)
		}
	}
	var f *core.Fault
	if _, err := w.Execute(core.Request{ID: "overflow", ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:bard"}, Operations: []core.Operation{{AdjustStat: &core.AdjustStat{Character: "char:bard", Stat: "xp", Amount: math.MaxInt64}}}}); !errors.As(err, &f) || f.Code != "E_OVERFLOW" {
		t.Fatal(err)
	}
	costReplay(t, b, w)
}

// The XP block stands alone with the two stats it reads, and follows the SRD
// Character Advancement table at every level.
func TestSRDStatsExperienceTable(t *testing.T) {
	// SRD 5.2.1 Character Advancement: the XP levels 1 to 20 need.
	need := []int64{0, 300, 900, 2700, 6500, 14000, 23000, 34000, 48000, 64000,
		85000, 100000, 120000, 140000, 165000, 195000, 225000, 265000, 305000, 355000}
	var s strings.Builder
	s.WriteString("rules { transfer unequips; wear any; }\nlocation \"loc:hall\" named \"Hall\";\n")
	for level := 1; level <= 20; level++ {
		fmt.Fprintf(&s, `character "char:%d" named "L%d" at "loc:hall" { stat "level" = %d; stat "xp" = %d; stat "xp:next" = 0; stat "xp:pending" = 0; }
`, level, level, level, need[level-1])
	}
	s.WriteString("equipment \"nql-equipment-v1\" {\n")
	s.WriteString(srdStatsBlock(t, "xp"))
	s.WriteString("}\n")
	b, ds := compiler.CompileWorld("xp.nql", s.String())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	for level := 1; level <= 20; level++ {
		id := core.CharacterID(fmt.Sprintf("char:%d", level))
		award := func(amount, pending int64) {
			t.Helper()
			commitC(t, w, statsRequestAs(t, w, fmt.Sprintf("%s:%d", id, amount), fmt.Sprintf(`adjust "%s" stat "xp" by %d;`, id, amount), string(id)))
			stat(t, w, id, "xp:pending", pending)
		}
		stat(t, w, id, "xp:pending", 0)
		if level == 20 {
			stat(t, w, id, "xp:next", 0)
			award(1000, 0)
			continue
		}
		stat(t, w, id, "xp:next", need[level])
		// One short of the next level, then on it.
		award(need[level]-need[level-1]-1, 0)
		award(1, 1)
	}
}

// The marked blocks stand alone: a world with only the stats a rule needs
// takes them unchanged. Proficiency and Jack of All Trades follow the SRD
// table at every level.
func TestSRDStatsProficiencyTable(t *testing.T) {
	var s strings.Builder
	s.WriteString("rules { transfer unequips; wear any; }\nlocation \"loc:hall\" named \"Hall\";\n")
	for level := 1; level <= 20; level++ {
		fmt.Fprintf(&s, `character "char:%d" named "L%d" at "loc:hall" { stat "level" = %d; stat "strength" = 10; stat "dexterity" = 10; stat "proficiency" = 0; stat "bonus:checks" = 0; stat "bonus:d20" = 0; stat "skill:athletics" = 0; stat "skill:acrobatics" = 0; }
condition "char:%d:prof:skill:athletics" of "prof:skill:athletics" to "char:%d";
condition "char:%d:feature:jack-of-all-trades" of "feature:jack-of-all-trades" to "char:%d";
`, level, level, level, level, level, level, level)
	}
	s.WriteString(srdStatsBlock(t, "conditions"))
	s.WriteString("equipment \"nql-equipment-v1\" {\n slot \"body\" capacity 1;\n")
	s.WriteString(srdStatsBlock(t, "derive"))
	s.WriteString("}\n")
	b, ds := compiler.CompileWorld("table.nql", s.String())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	for level := 1; level <= 20; level++ {
		id := core.CharacterID(fmt.Sprintf("char:%d", level))
		pb := int64(2 + (level-1)/4)
		stat(t, w, id, "proficiency", pb)
		stat(t, w, id, "skill:athletics", pb)
		stat(t, w, id, "skill:acrobatics", pb/2)
	}
}

func TestSRDStatsUnlessValidation(t *testing.T) {
	jack := `requires "feature:jack-of-all-trades" unless "prof:skill:arcana"`
	for _, tc := range []struct{ to, token string }{
		{`requires "feature:jack-of-all-trades" unless "prof:skill:missing"`, `"prof:skill:missing"`},
		{`requires "prof:skill:arcana" unless "prof:skill:arcana"`, `unless "prof:skill:arcana"`},
	} {
		s := strings.Replace(srdStatsFile(t, "world.nql"), jack, tc.to, 1)
		_, ds := compiler.CompileWorld("bad.nql", s)
		at := strings.Index(s, tc.to) + strings.Index(tc.to, tc.token)
		if strings.HasPrefix(tc.token, "unless ") {
			at += len("unless ")
		}
		if len(ds) != 1 || ds[0].Code != "E_EQUIPMENT" || ds[0].Position.Line != strings.Count(s[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc.to, ds)
		}
	}

	// A Blueprint that skips the compiler meets the same rule.
	b := srdStatsFixture(t, nil)
	for _, unless := range []core.ConditionTypeID{"prof:skill:missing", "feature:jack-of-all-trades"} {
		bad := srdStatsFixture(t, nil)
		for i, r := range bad.Equipment.Derived {
			if r.Stat == "skill:arcana" {
				bad.Equipment.Derived[i].Terms[3].Unless = unless
			}
		}
		_, err := core.NewWorld(bad)
		var f *core.Fault
		if !errors.As(err, &f) || f.Code != "E_EQUIPMENT" || f.Field != "unless" || f.Actual != string(unless) {
			t.Fatal(unless, err)
		}
	}
	if _, err := core.NewWorld(b); err != nil {
		t.Fatal(err)
	}
}

// Every nql line the guide shows comes from the example files.
func TestSRDStatsGuideMatchesExamples(t *testing.T) {
	guide, err := os.ReadFile("../docs/CHARACTER_STATS.md")
	if err != nil {
		t.Fatal(err)
	}
	examples := srdStatsFile(t, "world.nql") + srdStatsFile(t, "level-up.nql") + srdStatsFile(t, "proficiency.nql") + srdStatsFile(t, "conditions.nql") + srdStatsFile(t, "checks.nql") + srdStatsFile(t, "concentration.nql") + srdStatsFile(t, "xp.nql")
	lines := map[string]bool{}
	for _, l := range strings.Split(examples, "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	blocks := strings.Split(string(guide), "```")
	shown := 0
	for i := 1; i < len(blocks); i += 2 {
		if !strings.HasPrefix(blocks[i], "nql\n") {
			continue
		}
		for _, l := range strings.Split(strings.TrimPrefix(blocks[i], "nql\n"), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || l == "..." {
				continue
			}
			if !lines[l] {
				t.Errorf("guide line not in examples/srd-stats: %s", l)
			}
			shown++
		}
	}
	if shown == 0 {
		t.Fatal("guide shows no nql")
	}
}
