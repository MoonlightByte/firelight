package tests

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// Added dice (SRD 5.2.1 Bless and Guidance): a Condition modifier adds a die
// to every check of a stat, rolled after the d20 is kept. Every expected
// number is worked by hand from the fixture's sheets.

const addedDieTypes = `
condition type "spell:bless" { instances coexist; modifier "bless" stat "bonus:saves" die 1 4; }
condition type "spell:guidance" { instances coexist; modifier "guidance" stat "skill:athletics" die 1 4; }
condition type "spell:resolve" { instances coexist; modifier "resolve" stat "save:wisdom" die 1 6; }
condition type "spell:omen" { instances coexist; modifier_selection latest; modifier "omen" stat "save:wisdom" die 2 4; }
`

func addedDieWorld(t testing.TB) (core.Blueprint, *core.World) {
	t.Helper()
	b := srdStatsFixture(t, func(s string) string { return s + addedDieTypes })
	return b, world(t, b)
}

// addedCheck runs actions as the fighter and returns the resolved checks.
func addedChecks(t testing.TB, w *core.World, id, src string) []core.StatCheckResult {
	t.Helper()
	var out []core.StatCheckResult
	for _, e := range statsExecute(t, w, id, src).Events {
		if e.StatCheck != nil {
			out = append(out, *e.StatCheck)
		}
	}
	return out
}

func addedFault(t testing.TB, w *core.World, id, src string) *core.Fault {
	t.Helper()
	rev := w.Revision()
	_, err := w.Execute(statsRequest(t, w, id, src))
	f := faultOf(t, err)
	if w.Revision() != rev {
		t.Fatalf("%s: a refused check committed", id)
	}
	return f
}

func diceCalls(w *core.World) uint64 { return w.Snapshot().Blueprint.Dice.State.Calls }

func wantTotal(t testing.TB, r core.StatCheckResult, total int64) {
	t.Helper()
	sum := r.Kept + r.Bonus
	for _, d := range r.Added {
		sum += d.Sum
	}
	if r.Total == nil || *r.Total != total || sum != total {
		t.Fatalf("total %d, want %d: %+v", derefOr(r.Total), total, r)
	}
	if r.Intent.DC != nil && (*r.Margin != total-*r.Intent.DC || *r.Success != (total >= *r.Intent.DC)) {
		t.Fatalf("margin or success: %+v", r)
	}
}

func derefOr(v *int64) int64 {
	if v == nil {
		return -1
	}
	return *v
}

// A1: Guidance on Athletics adds one d4 to Athletics checks and to nothing
// else; A4: the record and the explanation list the die.
func TestAddedDieSkillScoped(t *testing.T) {
	b, w := addedDieWorld(t)
	got := addedChecks(t, w, "guidance", `apply condition "char:fighter:guidance" of "spell:guidance" to "char:fighter";
check "char:fighter" stat "skill:athletics" die 20 against 15 faces 10 added "spell:guidance" 3;
check "char:fighter" stat "skill:stealth" die 20 faces 10;
check "char:fighter" stat "check:strength" die 20 faces 10;
check "char:fighter" stat "save:strength" die 20 faces 10;`)
	if len(got) != 4 {
		t.Fatalf("%d checks", len(got))
	}
	source := core.AddedDieSource{Instance: "char:fighter:guidance", Type: "spell:guidance", Modifier: "guidance", Stat: "skill:athletics", Count: 1, Sides: 4}
	// Athletics 5: 10 + 5 + 3 = 18 against 15.
	if want := []core.AddedDieResult{{AddedDieSource: source, Faces: []int64{3}, Sum: 3}}; !reflect.DeepEqual(got[0].Added, want) || got[0].Bonus != 5 {
		t.Fatalf("%+v", got[0])
	}
	wantTotal(t, got[0], 18)
	for _, r := range got[1:] {
		if r.Added != nil || *r.Total != 10+r.Bonus {
			t.Fatalf("%s: %+v", r.Intent.Stat, r)
		}
	}
	x, err := w.Explain("char:fighter", "skill:athletics")
	if err != nil || !reflect.DeepEqual(x.AddedDice, []core.AddedDieSource{source}) || x.Effective != 5 {
		t.Fatal("the die changed the stat or is not explained", x, err)
	}
	for _, c := range x.Contributions {
		if c.ConditionOrigin != nil && c.ConditionOrigin.Modifier == "guidance" {
			t.Fatal("the die is a contribution", c)
		}
	}
	if x, _ := w.Explain("char:fighter", "skill:stealth"); x.AddedDice != nil {
		t.Fatal("stealth", x.AddedDice)
	}
	f := addedFault(t, w, "stealth-guided", `check "char:fighter" stat "skill:stealth" die 20 faces 10 added "spell:guidance" 3;`)
	if f.Code != "E_ROLL" || f.Field != "added" || f.Actual != "spell:guidance" {
		t.Fatalf("%+v", f)
	}
	costReplay(t, b, w)
}

// A2: Bless on bonus:saves adds one d4 to every save and to no check.
func TestAddedDieSavesScoped(t *testing.T) {
	b, w := addedDieWorld(t)
	got := addedChecks(t, w, "bless", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";
check "char:bard" stat "save:wisdom" die 20 against 13 faces 11 added "spell:bless" 2;
check "char:bard" stat "save:dexterity" die 20 against 13 faces 4 added "spell:bless" 4;
check "char:bard" stat "skill:athletics" die 20 faces 10;
check "char:bard" stat "initiative" die 20 faces 10;`)
	if len(got) != 4 {
		t.Fatalf("%d checks", len(got))
	}
	// Wisdom save 1: 11 + 1 + 2 = 14. Dexterity save 5: 4 + 5 + 4 = 13.
	wantTotal(t, got[0], 14)
	wantTotal(t, got[1], 13)
	for i, stat := range []core.StatID{"save:wisdom", "save:dexterity"} {
		if d := got[i].Added; len(d) != 1 || d[0].Type != "spell:bless" || d[0].Stat != "bonus:saves" || d[0].Instance != "char:bard:bless" || got[i].Intent.Stat != stat {
			t.Fatalf("%+v", got[i])
		}
	}
	for _, r := range got[2:] {
		if r.Added != nil {
			t.Fatalf("%s: %+v", r.Intent.Stat, r)
		}
	}
	costReplay(t, b, w)
}

// A3: faces are range-checked against the die, counted, and refused where
// no die applies or the check fails automatically.
func TestAddedDieFaceRefusals(t *testing.T) {
	_, w := addedDieWorld(t)
	statsExecute(t, w, "bless", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";`)
	for _, tc := range []struct{ name, src, expected, actual string }{
		{"a 5 on a d4", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 5;`, "1..4", "5"},
		{"two faces for 1d4", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 2 3;`, "1 spell:bless faces", "2"},
		{"a type not in force", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:resolve" 3;`, "added faces for a die in force", "spell:resolve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := addedFault(t, w, tc.name, tc.src)
			if f.Code != "E_ROLL" || f.Field != "added" || f.Expected != tc.expected || f.Actual != tc.actual {
				t.Fatalf("%+v", f)
			}
		})
	}
	// Unconscious at 0 HP fails Dexterity saves: no die rolls, none is taken.
	statsExecute(t, w, "fall", `damage "char:bard" resource "hp" by 40;`)
	calls := diceCalls(w)
	got := addedChecks(t, w, "fail", `check "char:bard" stat "save:dexterity" die 20 against 13;`)
	if r := got[0]; r.Roll != core.AutoFail || r.Added != nil || r.Total != nil || diceCalls(w) != calls {
		t.Fatalf("%+v", r)
	}
	f := addedFault(t, w, "fail-added", `check "char:bard" stat "save:dexterity" die 20 against 13 added "spell:bless" 2;`)
	if f.Code != "E_ROLL" || f.Field != "added" || f.Expected != "no added faces (fail)" {
		t.Fatalf("%+v", f)
	}
	// The Wisdom save still takes the die.
	if got := addedChecks(t, w, "wis", `check "char:bard" stat "save:wisdom" die 20 against 13 faces 11 added "spell:bless" 1;`); *got[0].Total != 13 {
		t.Fatalf("%+v", got[0])
	}
}

// A3 static: the compiler refuses impossible faces at the clause.
func TestAddedDieCompileRefusals(t *testing.T) {
	_, w := addedDieWorld(t)
	for _, tc := range []struct{ name, src, at string }{
		{"face 0", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 0;`, "0;"},
		{"face 101", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 101;`, "101"},
		{"eleven faces", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 1 1 1 1 1 1 1 1 1 1 1;`, "added"},
		{"a type twice", `check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 1 added "spell:bless" 2;`, `"spell:bless"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("a.nql", tc.src, w.Snapshot().Blueprint)
			if len(ds) != 1 || ds[0].Code != "E_ROLL" || ds[0].Column != strings.Index(tc.src, tc.at)+1 {
				t.Fatalf("%+v", ds)
			}
		})
	}
	ops, ds := compiler.CompileActions("a.nql", `check "char:bard" stat "save:wisdom" die 20 against 9 faces 11 added "spell:bless" 2 added "spell:resolve" 6;`, w.Snapshot().Blueprint)
	want := core.StatCheck{Character: "char:bard", Stat: "save:wisdom", Sides: 20, DC: i64(9), Faces: []int64{11}, Added: []core.AddedFaces{{Type: "spell:bless", Faces: []int64{2}}, {Type: "spell:resolve", Faces: []int64{6}}}}
	if len(ds) > 0 || len(ops) != 1 || !reflect.DeepEqual(*ops[0].StatCheck, want) {
		t.Fatalf("%+v %+v", ds, ops)
	}
}

// A5: one die per Condition type; different types each add; a suppressed
// latest-selection instance does not count.
func TestAddedDieStacking(t *testing.T) {
	b, w := addedDieWorld(t)
	got := addedChecks(t, w, "stack", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";
apply condition "char:bard:bless:2" of "spell:bless" to "char:bard";
check "char:bard" stat "save:dexterity" die 20 faces 4 added "spell:bless" 4;
apply condition "char:bard:resolve" of "spell:resolve" to "char:bard";
check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 2 added "spell:resolve" 6;`)
	if d := got[0].Added; len(d) != 1 || d[0].Instance != "char:bard:bless" {
		t.Fatalf("two Blesses stacked: %+v", got[0])
	}
	wantTotal(t, got[0], 13)
	// 11 + 1 + 2 + 6 = 20; the dice are in instance order.
	if d := got[1].Added; len(d) != 2 || d[0].Type != "spell:bless" || d[1].Type != "spell:resolve" || d[1].Sides != 6 {
		t.Fatalf("%+v", got[1])
	}
	wantTotal(t, got[1], 20)
	// Latest selection: the later Omen's die counts, the earlier one's not.
	got = addedChecks(t, w, "omen", `apply condition "char:bard:omen:a" of "spell:omen" to "char:bard";
apply condition "char:bard:omen:b" of "spell:omen" to "char:bard";
check "char:bard" stat "save:wisdom" die 20 faces 11 added "spell:bless" 1 added "spell:resolve" 1 added "spell:omen" 4 4;`)
	var omen []core.AddedDieResult
	for _, d := range got[0].Added {
		if d.Type == "spell:omen" {
			omen = append(omen, d)
		}
	}
	if len(omen) != 1 || omen[0].Instance != "char:bard:omen:b" || omen[0].Count != 2 || omen[0].Sum != 8 {
		t.Fatalf("%+v", got[0].Added)
	}
	// 11 + 1 + 1 + 1 + 8 = 22.
	wantTotal(t, got[0], 22)
	costReplay(t, b, w)
}

// A6: without faces the die draws after the d20 from the world's dice, and
// replays exactly; a supplied d20 can go with a drawn die.
func TestAddedDieDrawn(t *testing.T) {
	b, w := addedDieWorld(t)
	_, plain := addedDieWorld(t)
	statsExecute(t, w, "bless", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";`)
	statsExecute(t, plain, "nothing", `apply condition "char:bard:resolve" of "spell:resolve" to "char:bard";`)
	calls, plainCalls := diceCalls(w), diceCalls(plain)
	got := addedChecks(t, w, "drawn", `check "char:bard" stat "save:dexterity" die 20 against 13 advantage;`)
	ref := addedChecks(t, plain, "drawn", `check "char:bard" stat "save:dexterity" die 20 against 13 advantage;`)
	r := got[0]
	// The d20s draw first: the same faces as a world without the die, which
	// then draws less.
	if !r.Drawn || !reflect.DeepEqual(r.Faces, ref[0].Faces) || len(r.Added) != 1 || !r.Added[0].Drawn || len(r.Added[0].Faces) != 1 ||
		r.Added[0].Faces[0] < 1 || r.Added[0].Faces[0] > 4 || diceCalls(w)-calls <= diceCalls(plain)-plainCalls {
		t.Fatalf("%+v vs %+v, calls %d", r, ref[0], diceCalls(w)-calls)
	}
	wantTotal(t, r, r.Kept+5+r.Added[0].Faces[0])
	calls = diceCalls(w)
	got = addedChecks(t, w, "mixed", `check "char:bard" stat "save:wisdom" die 20 against 13 faces 11;`)
	if r := got[0]; r.Drawn || !r.Added[0].Drawn || diceCalls(w) == calls {
		t.Fatalf("%+v", r)
	}
	wantTotal(t, got[0], 11+1+got[0].Added[0].Sum)
	costReplay(t, b, w)
}

// A8: a world without die modifiers says nothing new.
func TestAddedDieAbsentIsSilent(t *testing.T) {
	b := srdStatsFixture(t, nil)
	w := world(t, b)
	batch := statsExecute(t, w, "checks", srdStatsFile(t, "checks.nql"))
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := w.Explain("char:fighter", "skill:athletics")
	rawX, _ := json.Marshal(x)
	if strings.Contains(string(raw), `"added`) || strings.Contains(string(rawX), `"added`) {
		t.Fatal("an added-die field appeared without a die")
	}
}

// A9: the typed Save never draws, so it needs every die's faces.
func TestAddedDieTypedSave(t *testing.T) {
	_, w := saveWorld(t, "condition type \"spell:bless\" { instances coexist; modifier \"bless\" stat \"save:dexterity\" die 1 4; }\ncondition \""+spider+":bless\" of \"spell:bless\" to \""+spider+"\";\n")
	const dex = `stat "save:dexterity" dc 15`
	_, err := attackRun(t, w, "no-die", save(spider, dex, "faces 10"))
	if f := faultOf(t, err); f.Code != "E_ROLL" || f.Field != "added" || f.Expected != "faces for every added die (1 spell:bless)" {
		t.Fatalf("%+v", f)
	}
	// 10 + 3 + 2 = 15 against 15.
	b, err := attackRun(t, w, "die", save(spider, dex, `faces 10 added "spell:bless" 2`))
	if err != nil {
		t.Fatal(err)
	}
	s := saved(b)
	if len(s) != 1 || len(s[0].Check.Added) != 1 || s[0].Check.Added[0].Drawn || !reflect.DeepEqual(s[0].Intent.AddedFaces, []core.AddedFaces{{Type: "spell:bless", Faces: []int64{2}}}) {
		t.Fatalf("%+v", s)
	}
	wantTotal(t, s[0].Check, 15)
	_, err = attackRun(t, w, "other", save(spider, dex, `faces 10 added "spell:bless" 2 added "spell:other" 1`))
	if f := faultOf(t, err); f.Code != "E_ROLL" || f.Field != "added" || f.Actual != "spell:other" {
		t.Fatalf("%+v", f)
	}
	src := save(spider, dex, `faces 10 added "spell:bless" 0`)
	if _, ds := compiler.CompileActions("a.nql", src, w.Snapshot().Blueprint); len(ds) != 1 || ds[0].Code != "E_SAVE" || ds[0].Column != strings.LastIndex(src, "0")+1 {
		t.Fatalf("%+v", ds)
	}
}

// A10: a damage-triggered concentration save under Bless draws the die.
func TestAddedDieConcentration(t *testing.T) {
	b, w := addedDieWorld(t)
	// The bard's Constitution save is +1; 12 damage is DC 10; the player
	// rolled 9, and the drawn d4 makes it at least 11.
	got := addedChecks(t, w, "focus", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";
apply condition "char:bard:concentration" of "state:concentration" to "char:bard";
damage "char:bard" resource "hp" by 12 faces "char:bard:concentration" 9;`)
	if len(got) != 1 || len(got[0].Added) != 1 || !got[0].Added[0].Drawn || got[0].Added[0].Type != "spell:bless" || !*got[0].Success {
		t.Fatalf("%+v", got)
	}
	wantTotal(t, got[0], 10+got[0].Added[0].Sum)
	costReplay(t, b, w)
}

// A11: a die modifier's own limits, and what it cannot be combined with.
func TestAddedDieDefinitionRefusals(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"no dice", `instances coexist; modifier "m" stat "bonus:saves" die 0 4;`, "E_RULE"},
		{"eleven dice", `instances coexist; modifier "m" stat "bonus:saves" die 11 4;`, "E_RULE"},
		{"one side", `instances coexist; modifier "m" stat "bonus:saves" die 1 1;`, "E_RULE"},
		{"101 sides", `instances coexist; modifier "m" stat "bonus:saves" die 1 101;`, "E_RULE"},
		{"held at a minimum", `instances coexist; held_at_minimum "hp"; modifier "m" stat "bonus:saves" die 1 4;`, "E_RULE"},
		{"an amount too", `instances coexist; modifier "m" stat "bonus:saves" die 1 4 add 1;`, "E_PARSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := srdStatsFile(t, "world.nql") + "condition type \"spell:x\" { " + tc.body + " }\n"
			_, ds := compiler.CompileWorld("w.nql", src)
			if len(ds) == 0 || ds[0].Code != tc.code {
				t.Fatalf("%+v", ds)
			}
			line := "condition type \"spell:x\" { " + tc.body + " }"
			if tc.code == "E_RULE" && tc.name != "held at a minimum" && (ds[0].Expected != "1..10 dice of 2..100 sides" || ds[0].Column != strings.Index(line, " die ")+2) {
				t.Fatalf("%+v", ds[0])
			}
		})
	}
	fixed := int64(1)
	for _, m := range []core.ConditionModifier{
		{ID: "m", Stat: "s", Die: &core.AddedDie{Count: 1, Sides: 4}, Fixed: &fixed},
		{ID: "m", Stat: "s", Die: &core.AddedDie{Count: 1, Sides: 4}, Roll: core.Advantage},
		{ID: "m", Stat: "s", Die: &core.AddedDie{Count: 1, Sides: 4}, Bound: core.AtLeast, Fixed: &fixed},
		{ID: "m", ResourceMaximum: "hp", Die: &core.AddedDie{Count: 1, Sides: 4}},
	} {
		if err := core.CheckConditionDefinition(core.ConditionDefinition{ID: "t", Instances: core.Coexist, Modifiers: []core.ConditionModifier{m}}); err == nil {
			t.Fatalf("accepted %+v", m)
		}
	}
}

// A12: the die survives a JSON round trip of the world, and a snapshot's die
// is detached from the world.
func TestAddedDieRoundTrip(t *testing.T) {
	_, w := addedDieWorld(t)
	s := w.Snapshot()
	raw, err := json.Marshal(s.Blueprint)
	if err != nil {
		t.Fatal(err)
	}
	var back core.Blueprint
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(back)
	if string(again) != string(raw) || !strings.Contains(string(raw), `"die":{"count":1,"sides":4}`) {
		t.Fatal("round trip")
	}
	for i := range s.Blueprint.ConditionDefinitions {
		for j := range s.Blueprint.ConditionDefinitions[i].Modifiers {
			if d := s.Blueprint.ConditionDefinitions[i].Modifiers[j].Die; d != nil {
				d.Sides = 99
			}
		}
	}
	statsExecute(t, w, "bless", `apply condition "char:bard:bless" of "spell:bless" to "char:bard";`)
	if x, _ := w.Explain("char:bard", "save:wisdom"); len(x.AddedDice) != 1 || x.AddedDice[0].Sides != 4 {
		t.Fatalf("snapshot not detached: %+v", x.AddedDice)
	}
}

// The guides' examples run as shown: GRAMMAR.md's types and check, and
// CHARACTER_STATS.md's record of it.
func TestAddedDieGuideExamples(t *testing.T) {
	grammar, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(grammar), "## Added-die Condition modifiers\n\n```nql\n")
	if !ok {
		t.Fatal("no added-die example")
	}
	types, rest, _ := strings.Cut(after, "```")
	_, rest, _ = strings.Cut(rest, "```nql\n")
	check, _, _ := strings.Cut(rest, "```")
	b := srdStatsFixture(t, func(s string) string { return s + types })
	w := world(t, b)
	got := addedChecks(t, w, "guide", `apply condition "char:fighter:guidance" of "spell:guidance" to "char:fighter";
`+check)
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	stats, err := os.ReadFile("../docs/CHARACTER_STATS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stats), "```json\n"+string(raw)+"\n```") {
		t.Fatalf("CHARACTER_STATS.md does not show %s", raw)
	}
}
