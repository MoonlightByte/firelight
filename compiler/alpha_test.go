package compiler

import (
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const alphaCompilerEntities = `rules { transfer unequips; wear owner_only; }
location "l" named "Arena";
location "home" named "Home";
character "a" named "Ally" at "l" {
 stat "attack" = 4; stat "defense" = 12;
 resource "hp" = 6 min 0 max 6;
 resource "focus" = 3 min 0 max 3;
}
character "h" named "Hostile" at "l" {
 stat "attack" = 4; stat "defense" = 12;
 resource "hp" = 6 min 0 max 6;
 resource "focus" = 3 min 0 max 3;
}
item "relic" named "Relic" { owner "h"; }
condition type "guard" { instances unique; modifier "ward" stat "defense" add 3; }
`
const alphaCompilerAdventure = `adventure {
 party "a";
 health "hp";
 link "l" to "home";
 link "home" to "l";
 grant "a" { ability "pass"; }
 grant "h" { ability "pass"; }
 encounter "enc" at "l" {
  participant "a" team party;
  participant "h" team hostile;
 }
 objective "goal" item "relic" custodian "h" victory "enc" destination "home";
 initial encounter "enc";
 seed 1 2;
}
`
const alphaCompilerPass = `ability "pass" named "Pass" {
 parameter "actor" character;
 action actor param "actor" phase encounter;
}
`

func TestAlphaAdventureForwardReferencesAndGenesis(t *testing.T) {
	b, ds := CompileWorld("alpha.nql", alphaCompilerAdventure+alphaCompilerPass+alphaCompilerEntities)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := core.AdventureDefinition{
		Party: []core.CharacterID{"a"}, Health: "hp",
		Links:            []core.LocationLink{{From: "l", To: "home"}, {From: "home", To: "l"}},
		Grants:           []core.AbilityGrant{{Character: "a", Abilities: []core.AbilityID{"pass"}}, {Character: "h", Abilities: []core.AbilityID{"pass"}}},
		Encounters:       []core.EncounterDefinition{{ID: "enc", Location: "l", Order: []core.Participant{{Character: "a", Team: core.PartyTeam}, {Character: "h", Team: core.HostileTeam}}}},
		Objectives:       []core.ObjectiveDefinition{{ID: "goal", Item: "relic", Custodian: "h", Victory: "enc", Destination: "home"}},
		InitialEncounter: "enc", Seed: [2]uint64{1, 2},
	}
	if b.Adventure == nil || !reflect.DeepEqual(b.Adventure.Definition, want) || !reflect.DeepEqual(b.Adventure.State, core.AdventureState{}) {
		t.Fatalf("wrong definition or compiler-created state: %+v", b.Adventure)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Snapshot().Adventure.State.Random.Bytes) != 20 || !reflect.DeepEqual(b.Adventure.State, core.AdventureState{}) {
		t.Fatal("constructor did not initialize a detached continuation state")
	}
}

func TestAlphaTypedRequirementsAndNonrecursiveBranches(t *testing.T) {
	source := abilityCatalog + `ability "checked" named "Checked" {
 parameter "actor" character;
 parameter "target" character;
 parameter "item" item;
 parameter "instance" condition_id;
 action actor param "actor" phase encounter;
 require alive param "target" resource "hp";
 require colocated param "actor" with param "target";
 require allied param "actor" with param "target";
 require hostile param "actor" with param "target";
 require distinct param "actor" with param "target";
 require has param "target" condition "t";
 require lacks param "target" condition "t";
 require party_owned param "item";
 spend param "actor" resource "hp" by 2;
 check "strike" die 20 plus stat param "actor" "defense" against stat "c" "defense" {
  success {
   damage param "target" resource "hp" by 4;
   apply condition param "instance" of "t" to param "target" from item "i" duration 2;
  }
  failure { }
 }
}`
	b, ds := CompileWorld("checked.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	d := b.AbilityDefinitions[0]
	wantKinds := []core.RequirementKind{core.AboveMinimum, core.Colocated, core.Allied, core.Hostile, core.Distinct, core.HasCondition, core.LacksCondition, core.PartyOwned}
	if d.Action == nil || d.Action.Actor.Parameter != "actor" || d.Action.Phase != core.EncounterPhase || len(d.Action.Requirements) != len(wantKinds) {
		t.Fatalf("policy did not lower: %+v", d.Action)
	}
	for i, k := range wantKinds {
		if d.Action.Requirements[i].Kind != k {
			t.Fatalf("requirement %d: %+v", i, d.Action.Requirements[i])
		}
	}
	check := d.Steps[1].Check
	if check == nil || check.ID != "strike" || check.Sides != 20 || check.Modifier.Stat.Character.Parameter != "actor" || check.Threshold.Stat.Stat != "defense" || *check.Threshold.Stat.Character.Literal.Character != "c" || len(check.Success) != 2 || check.Failure == nil || len(check.Failure) != 0 {
		t.Fatalf("check did not lower: %+v", check)
	}
	condition := check.Success[1].ApplyCondition
	if condition == nil || condition.Duration == nil || *condition.Duration != 2 || condition.Instance.Parameter != "instance" || condition.Source.ID != "i" {
		t.Fatalf("branch duration/provenance template: %+v", condition)
	}
	// Numeric check values have actual presence, including zero and MinInt64.
	for _, values := range []string{"0 against -1", "-9223372036854775808 against 9223372036854775807"} {
		b, ds := CompileWorld("signed.nql", abilityCatalog+`ability "signed" named "Signed" { check "c" die 2 plus `+values+` { success {} failure {} } }`)
		if len(ds) != 0 || b.AbilityDefinitions[0].Steps[0].Check.Modifier.Literal == nil || b.AbilityDefinitions[0].Steps[0].Check.Threshold.Literal == nil {
			t.Fatal("signed literal rejected or absence conflated with zero", ds)
		}
	}
}

func TestAlphaTravelClaimAndLegacyOmission(t *testing.T) {
	b, ds := CompileWorld("travel.nql", alphaCompilerEntities+alphaCompilerAdventure+alphaCompilerPass+`ability "travel" named "Travel" {
 action actor "a" phase exploration;
 travel party to "home";
}
ability "claim" named "Claim" {
 parameter "target" character;
 action actor "a" phase exploration;
 claim objective "goal" to param "target";
}`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if b.AbilityDefinitions[1].Steps[0].Travel.To != "home" || b.AbilityDefinitions[2].Steps[0].Claim.Objective != "goal" || b.AbilityDefinitions[2].Steps[0].Claim.Recipient.Parameter != "target" {
		t.Fatal("literal travel/typed claim failed")
	}
	legacy, ds := CompileWorld("legacy.nql", abilityCatalog+abilityDefinitionSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"adventure":`, `"action":`, `"duration":`, `"check":`, `"path":`} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("Alpha absence changed legacy encoding: %s", field)
		}
	}
}

func TestAlphaCompilerDiagnosticsAtAuthoredOperands(t *testing.T) {
	for _, tc := range []struct{ name, source, code, context string }{
		{"unknown actor", `ability "a" named "A" { action actor param «"missing" phase encounter; }`, "E_PARAMETER", `step -1 operand "actor"`},
		{"wrong actor type", `ability "a" named "A" { parameter "actor" int; action actor param «"actor" phase encounter; }`, "E_TYPE", `step -1 operand "actor"`},
		{"wrong requirement other", `ability "a" named "A" { parameter "target" item; action actor "c" phase encounter; require colocated "c" with param «"target"; }`, "E_TYPE", `step -1 operand "requirement.0.other"`},
		{"wrong owned operand", `ability "a" named "A" { action actor "c" phase encounter; require party_owned «"c"; }`, "E_TYPE", `operand "requirement.0.subject"`},
		{"unknown condition", `ability "a" named "A" { action actor "c" phase encounter; require lacks "c" condition «"absent"; }`, "E_CONDITION_TYPE", `operand "requirement.0.condition"`},
		{"missing literal resource", `ability "a" named "A" { action actor "c" phase encounter; require alive "c" resource «"missing"; }`, "E_RESOURCE", `operand "requirement.0.resource"`},
		{"invalid resource on parameter", `ability "a" named "A" { parameter "actor" character; action actor param "actor" phase encounter; require alive param "actor" resource «"\n"; }`, "E_ID", `operand "requirement.0.resource"`},
		{"empty resource on parameter", `ability "a" named "A" { parameter "actor" character; action actor param "actor" phase encounter; require alive param "actor" resource «""; }`, "E_ID", `operand "requirement.0.resource"`},
		{"invalid literal resource selector", `ability "a" named "A" { action actor "c" phase encounter; require alive "c" resource «"\n"; }`, "E_ID", `operand "requirement.0.resource"`},
		{"requirement without policy", `ability "a" named "A" { «require alive "c" resource "hp"; heal "c" resource "hp" by 1; }`, "E_ACTION", ""},
		{"empty check identity", `ability "a" named "A" { check «"" die 20 plus 0 against 0 { success {} failure {} } }`, "E_ID", `step 0 operand "check"`},
		{"small die", `ability "a" named "A" { check "c" die «1 plus 0 against 0 { success {} failure {} } }`, "E_BOUNDS", `operand "sides"`},
		{"large die", `ability "a" named "A" { check "c" die «1001 plus 0 against 0 { success {} failure {} } }`, "E_BOUNDS", `operand "sides"`},
		{"second check", `ability "a" named "A" { check "first" die 20 plus 0 against 0 { success {} failure {} } check «"second" die 20 plus 0 against 0 { success {} failure {} } }`, "E_LIMIT", `step 1 operand "check"`},
		{"unknown modifier", `ability "a" named "A" { check "c" die 20 plus stat param «"unknown" "defense" against 0 { success {} failure {} } }`, "E_PARAMETER", `operand "modifier"`},
		{"missing threshold stat", `ability "a" named "A" { check "c" die 20 plus 0 against stat «"n" "defense" { success {} failure {} } }`, "E_STAT", `operand "threshold"`},
		{"success wrong target", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { heal "c" resource "hp" by 1; damage «"i" resource "hp" by 2; } failure {} } }`, "E_TYPE", `path [0 1]`},
		{"failure wrong target", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success {} failure { damage «"i" resource "hp" by 2; } } }`, "E_TYPE", `path [1 0]`},
		{"branch missing parameter", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { apply condition "new" of "t" to param «"missing"; } failure {} } }`, "E_PARAMETER", `path [0 0]`},
		{"branch literal amount", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success {} failure { heal "c" resource "hp" by «0; } } }`, "E_AMOUNT", `path [1 0]`},
		{"duration zero", `ability "a" named "A" { apply condition "new" of "t" to "c" duration «0; }`, "E_AMOUNT", ""},
		{"duration negative", `ability "a" named "A" { apply condition "new" of "t" to "c" duration «-1; }`, "E_AMOUNT", ""},
		{"duration overflow", `ability "a" named "A" { apply condition "new" of "t" to "c" duration «9223372036854775808; }`, "E_INTEGER", ""},
		{"nested check", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { «check "nested" die 2 plus 0 against 0 { success {} failure {} } } failure {} } }`, "E_PARSE", ""},
		{"branch invocation", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { «invoke "a" {}; } failure {} } }`, "E_PARSE", ""},
		{"branch metadata", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { «parameter "x" int; } failure {} } }`, "E_PARSE", ""},
		{"numeric parameter unsupported", `ability "a" named "A" { parameter "x" int; check "c" die 20 plus «param "x" against 0 { success {} failure {} } }`, "E_PARSE", ""},
		{"location parameter unsupported", `ability "a" named "A" { parameter "x" «location; travel party to "l"; }`, "E_PARSE", ""},
		{"dynamic travel unsupported", `ability "a" named "A" { travel party to «param "x"; }`, "E_PARSE", ""},
		{"wrong travel destination", `ability "a" named "A" { travel party to «"c"; }`, "E_TYPE", `operand "destination"`},
		{"missing claim objective", `ability "a" named "A" { claim objective «"missing" to "c"; }`, "E_OBJECTIVE", `operand "objective"`},
		{"dynamic duration unsupported", `ability "a" named "A" { apply condition "new" of "t" to "c" duration «param "x"; }`, "E_PARSE", ""},
		{"duplicate action", `ability "a" named "A" { action actor "c" phase either; «action actor "c" phase either; }`, "E_DUPLICATE", ""},
		{"duplicate duration", `ability "a" named "A" { apply condition "new" of "t" to "c" duration 2 «duration 3; }`, "E_DUPLICATE", ""},
		{"missing failure branch", `ability "a" named "A" { check "c" die 20 plus 0 against 0 { success {} «} }`, "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, line, column := spendMarkedPosition(t, abilityCatalog+tc.source)
			b, ds := CompileWorld("alpha-error.nql", s)
			if !reflect.DeepEqual(b, core.Blueprint{}) || len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "alpha-error.nql" || ds[0].Line != line || ds[0].Column != column || !strings.Contains(ds[0].Message, tc.context) {
				t.Fatalf("want %s %d:%d %s; got %+v", tc.code, line, column, tc.context, ds)
			}
		})
	}
}

func TestAlphaCompilerAuthoredWorkBounds(t *testing.T) {
	// Both branches count toward admission, including the branch not selected
	// by a future check. The check itself is also one authored work item.
	prefix := abilityCatalog + `ability "a" named "A" { check `
	branch := strings.Repeat(`heal "c" resource "hp" by 1;`, 127)
	valid := prefix + `"check" die 20 plus 0 against 0 { success {` + branch + `} failure {} } }`
	if _, ds := CompileWorld("bounded.nql", valid); len(ds) != 0 {
		t.Fatal("128 total authored steps rejected", ds)
	}
	s, line, column := spendMarkedPosition(t, prefix+`«"check" die 20 plus 0 against 0 { success {`+branch+`} failure { heal "c" resource "hp" by 1; } } }`)
	if _, ds := CompileWorld("bounded.nql", s); len(ds) != 1 || ds[0].Code != "E_LIMIT" || ds[0].Line != line || ds[0].Column != column {
		t.Fatal("unchosen branch omitted from complete authored limit", ds)
	}
	policy := abilityCatalog + `ability "a" named "A" { action actor "c" phase encounter; ` + strings.Repeat(`require alive "c" resource "hp";`, 16)
	if _, ds := CompileWorld("bounded.nql", policy+`}`); len(ds) != 0 {
		t.Fatal("16 requirements rejected", ds)
	}
	s, line, column = spendMarkedPosition(t, policy+`«require alive "c" resource "hp"; }`)
	if _, ds := CompileWorld("bounded.nql", s); len(ds) != 1 || ds[0].Code != "E_LIMIT" || ds[0].Line != line || ds[0].Column != column {
		t.Fatal("requirement limit did not locate first excess clause", ds)
	}
}

func TestAlphaAdventureDeclarationDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, fragment, code string }{
		{"missing health", `adventure { seed 1 2; «}`, "E_PARSE"},
		{"missing seed", `adventure { health "hp"; «}`, "E_PARSE"},
		{"negative seed", `adventure { health "hp"; seed 1 «-2; }`, "E_AMOUNT"},
		{"oversize seed", `adventure { health "hp"; seed 1 «9223372036854775808; }`, "E_INTEGER"},
		{"duplicate health", `adventure { health "hp"; «health "hp"; seed 1 2; }`, "E_DUPLICATE"},
		{"duplicate seed", `adventure { health "hp"; seed 1 2; «seed 3 4; }`, "E_DUPLICATE"},
		{"duplicate party", `adventure { party "a"; party «"a"; health "hp"; seed 1 2; }`, "E_DUPLICATE"},
		{"wrong party kind", `adventure { party «"relic"; health "hp"; seed 1 2; }`, "E_TYPE"},
		{"wrong link kind", `adventure { party "a"; health "hp"; link "l" to «"a"; seed 1 2; }`, "E_TYPE"},
		{"unknown grant", `adventure { party "a"; health "hp"; grant "a" { ability «"missing"; } seed 1 2; }`, "E_ABILITY"},
		{"duplicate grant", `adventure { party "a"; health "hp"; grant "a" { ability "pass"; ability «"pass"; } seed 1 2; }`, "E_DUPLICATE"},
		{"unknown initial", `adventure { party "a"; health "hp"; initial encounter «"missing"; seed 1 2; }`, "E_ENCOUNTER"},
		{"duplicate participant", `adventure { party "a"; health "hp"; encounter "e" at "l" { participant "a" team party; participant «"a" team party; } seed 1 2; }`, "E_DUPLICATE"},
		{"invalid team", `adventure { party "a"; health "hp"; encounter "e" at "l" { participant "a" team «neutral; } seed 1 2; }`, "E_PARSE"},
		{"duplicate block", `adventure { health "hp"; seed 1 2; } «adventure { health "hp"; seed 3 4; }`, "E_DUPLICATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, line, column := spendMarkedPosition(t, alphaCompilerEntities+alphaCompilerPass+tc.fragment)
			b, ds := CompileWorld("adventure-error.nql", s)
			if !reflect.DeepEqual(b, core.Blueprint{}) || len(ds) != 1 || ds[0].Code != tc.code || ds[0].Source != "adventure-error.nql" || ds[0].Line != line || ds[0].Column != column {
				t.Fatalf("want %s %d:%d; got %+v", tc.code, line, column, ds)
			}
		})
	}
}

func TestAlphaInvocationBranchDiagnosticsUseActualBinding(t *testing.T) {
	b, ds := CompileWorld("definition.nql", abilityCatalog+`ability "a" named "A" {
 parameter "target" character;
 check "c" die 20 plus 0 against 0 { success {} failure { heal param "target" resource "hp" by 1; } }
}`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	s, line, column := spendMarkedPosition(t, `invoke "a" { "target" = character «"n"; };`)
	_, ds = CompileActions("invocation.nql", s, b)
	if len(ds) != 1 || ds[0].Code != "E_RESOURCE" || ds[0].Source != "invocation.nql" || ds[0].Line != line || ds[0].Column != column || !strings.Contains(ds[0].Message, "path [1 0]") {
		t.Fatal("unchosen branch must still bind and diagnose at actual argument", ds)
	}
	b.AbilityDefinitions[0].Steps[0].Check.Failure[0].Heal.Amount = core.AbilityOperand{Literal: &core.AbilityValue{Integer: ptrCompiler(int64(0))}}
	_, ds = CompileActions("invocation.nql", `invoke "a" { "target" = character "c"; };`, b)
	if len(ds) != 1 || ds[0].Source != "invocation.nql" || ds[0].Line != 1 || ds[0].Column != 8 || !strings.Contains(ds[0].Message, "path [1 0]") {
		t.Fatal("typed catalog invented a missing definition location", ds)
	}
}

func FuzzAlphaCompiler(f *testing.F) {
	f.Add(alphaCompilerEntities + alphaCompilerAdventure + alphaCompilerPass)
	f.Add(abilityCatalog + `ability "a" named "A" { check "c" die 20 plus stat "c" "defense" against -1 { success {} failure {} } }`)
	f.Add(`ability "a" named "A" { check "c" die 20 plus 0 against 0 { success { check "nested" } failure {} } }`)
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 8192 {
			return
		}
		_, ds := CompileWorld("fuzz-alpha.nql", source)
		for _, d := range ds {
			if d.Source != "fuzz-alpha.nql" || d.Line < 1 || d.Column < 1 || d.Code == "" {
				t.Fatal("invalid diagnostic", d)
			}
		}
	})
}
