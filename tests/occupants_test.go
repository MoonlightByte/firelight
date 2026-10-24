package tests

import (
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The map world of world_map_test.go with three occupants: wolves not yet
// counted at the ford, bandits at the mill and a friendly guide at camp.
const occupantRoster = `occupant "occ:wolves" named "Wolves" at "loc:ford" { creatures 2 to 4; attitude hostile; type "wolf"; }
occupant "occ:bandits" named "Bandits" at "loc:mill" { creatures 3; attitude hostile; }
occupant "occ:mira" named "Mira Tidewise" at "loc:camp" { person; attitude friendly; alias "Mira"; }
`

func occupantFixture(t testing.TB, mapExtra string) core.Blueprint {
	t.Helper()
	b, ds := compiler.CompileWorld("occupants.nql", mapPlaces+occupantRoster+"map {\n"+mapRoutes+mapExtra+"}\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return b
}

func count(n int64) *int64 { return &n }

func occupant(t testing.TB, w *core.World, id core.OccupantID) core.Occupant {
	t.Helper()
	for _, o := range w.Snapshot().Occupants {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("occupant %s missing", id)
	return core.Occupant{}
}

func wantFault(t testing.TB, err error, code, field, actual string) {
	t.Helper()
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != code || f.Field != field || actual != "" && f.Actual != actual {
		t.Fatalf("got %#v, want %s %s %q", err, code, field, actual)
	}
}

func TestOccupantDeclarations(t *testing.T) {
	b := occupantFixture(t, "")
	want := []core.Occupant{
		{ID: "occ:bandits", Name: "Bandits", Location: "loc:mill", Kind: core.OccupantCreatures, Count: count(3), Attitude: core.AttitudeHostile},
		{ID: "occ:mira", Name: "Mira Tidewise", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(1), Attitude: core.AttitudeFriendly, Aliases: []string{"Mira"}},
		{ID: "occ:wolves", Name: "Wolves", Location: "loc:ford", Kind: core.OccupantCreatures, Range: &core.CountRange{Min: 2, Max: 4}, Attitude: core.AttitudeHostile, Type: "wolf"},
	}
	if got := world(t, b).Snapshot().Occupants; !reflect.DeepEqual(got, want) {
		t.Fatalf("occupants\n%+v\nwant\n%+v", got, want)
	}

	// The attitude defaults to indifferent. A tally says how members left; a
	// person with one has left. Aliases and the tally are kept sorted, so the
	// same roster written two ways is the same state.
	src := func(roster string) []core.Occupant {
		b, ds := compiler.CompileWorld("roster.nql", mapPlaces+roster)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		return world(t, b).Snapshot().Occupants
	}
	one := src(`occupant "occ:b" named "B" at "loc:mill" { creatures 1; tally fled 2; tally defeated 1; alias "Zed"; alias "Ann"; }
occupant "occ:a" named "A" at "loc:camp" { tally died 1; person; }
`)
	two := src(`occupant "occ:a" named "A" at "loc:camp" { person; tally died 1; }
occupant "occ:b" named "B" at "loc:mill" { alias "Ann"; tally defeated 1; alias "Zed"; creatures 1; tally fled 2; }
`)
	want = []core.Occupant{
		{ID: "occ:a", Name: "A", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(0), Attitude: core.AttitudeIndifferent, Tally: []core.OccupantTally{{Outcome: core.OutcomeDied, Count: 1}}},
		{ID: "occ:b", Name: "B", Location: "loc:mill", Kind: core.OccupantCreatures, Count: count(1), Attitude: core.AttitudeIndifferent, Aliases: []string{"Ann", "Zed"}, Tally: []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 1}, {Outcome: core.OutcomeFled, Count: 2}}},
	}
	if !reflect.DeepEqual(one, want) || !reflect.DeepEqual(two, want) {
		t.Fatalf("rosters\n%+v\n%+v\nwant\n%+v", one, two, want)
	}

	// Tally entries sort by outcome, then reason; one outcome may have several.
	got := src(`occupant "occ:d" named "D" at "loc:mill" { creatures 1; tally fled 1; tally departed 1 because "the toll"; tally departed 2 because "a fire"; }` + "\n")
	if want := []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 2, Reason: "a fire"}, {Outcome: core.OutcomeDeparted, Count: 1, Reason: "the toll"}, {Outcome: core.OutcomeFled, Count: 1}}; !reflect.DeepEqual(got[0].Tally, want) {
		t.Fatalf("tally %+v", got[0].Tally)
	}

	// Occupants need no map.
	if got := src(`occupant "occ:c" named "C" at "loc:isle" { creatures 1000000000; }` + "\n"); len(got) != 1 || *got[0].Count != core.MaxOccupantCount {
		t.Fatalf("largest group %+v", got)
	}
	// A group of unknown size is counted at any size.
	b, ds := compiler.CompileWorld("roster.nql", mapPlaces+`occupant "occ:c" named "C" at "loc:isle" { creatures; }`+"\n")
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	if got := occupant(t, w, "occ:c"); got.Count != nil || got.Range != nil {
		t.Fatalf("unknown size %+v", got)
	}
	_, err := w.Execute(request(t, w, "none", `count occupant "occ:c" as 0;`))
	wantFault(t, err, "E_OCCUPANT", "count", "0")
	execute(t, w, "many", `count occupant "occ:c" as 1000000000;`)
	if got := occupant(t, w, "occ:c"); *got.Count != core.MaxOccupantCount {
		t.Fatalf("counted %+v", got)
	}
}

func TestOccupantLifecycle(t *testing.T) {
	b := occupantFixture(t, "")
	w := world(t, b)

	r := execute(t, w, "count", `count occupant "occ:wolves" as 3;`)
	wantKinds(t, r, core.OccupantCounted)
	c := r.Events[0].Occupant
	if c.Before.Range == nil || c.Before.Count != nil || c.After.Range != nil || *c.After.Count != 3 || c.Intent.Count.Count != 3 {
		t.Fatalf("count event %+v", c)
	}

	r = execute(t, w, "fight", `resolve occupant "occ:wolves" as defeated by 2; resolve occupant "occ:wolves" as fled;`)
	wantKinds(t, r, core.OccupantResolved, core.OccupantResolved)
	if got := r.Events[1].Occupant.Before; *got.Count != 1 || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 2}}) {
		t.Fatalf("first resolution %+v", got)
	}
	if got := occupant(t, w, "occ:wolves"); *got.Count != 0 || got.Location != "loc:ford" || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 2}, {Outcome: core.OutcomeFled, Count: 1}}) {
		t.Fatalf("wolves after the fight %+v", got)
	}
	r = execute(t, w, "more", `resolve occupant "occ:bandits" as surrendered by 1; resolve occupant "occ:bandits" as defeated by 1; resolve occupant "occ:bandits" as surrendered by 1;`)
	if got := r.Events[1].Occupant.After.Tally; !reflect.DeepEqual(got, []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 1}, {Outcome: core.OutcomeSurrendered, Count: 1}}) {
		t.Fatalf("event tally %v, want it sorted", got)
	}
	if got := occupant(t, w, "occ:bandits"); *got.Count != 0 || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 1}, {Outcome: core.OutcomeSurrendered, Count: 2}}) {
		t.Fatalf("one outcome adds up %+v", got)
	}

	r = execute(t, w, "talk", `set occupant "occ:mira" attitude indifferent; rename occupant "occ:mira" to "Mira the Elder";`)
	wantKinds(t, r, core.OccupantAttitudeChanged, core.OccupantRenamed)
	if got := r.Events[1].Occupant.After.Aliases; !reflect.DeepEqual(got, []string{"Mira", "Mira Tidewise"}) {
		t.Fatalf("event aliases %v, want them sorted", got)
	}
	if got := occupant(t, w, "occ:mira"); got.Name != "Mira the Elder" || got.Attitude != core.AttitudeIndifferent || !reflect.DeepEqual(got.Aliases, []string{"Mira", "Mira Tidewise"}) {
		t.Fatalf("renamed %+v", got)
	}
	// Taking an alias back as the name drops it from the aliases.
	execute(t, w, "again", `rename occupant "occ:mira" to "Mira";`)
	if got := occupant(t, w, "occ:mira"); got.Name != "Mira" || !reflect.DeepEqual(got.Aliases, []string{"Mira Tidewise", "Mira the Elder"}) {
		t.Fatalf("renamed back %+v", got)
	}

	r = execute(t, w, "walk", `move occupant "occ:mira" to "loc:mill";`)
	wantKinds(t, r, core.OccupantMoved)
	if got := r.Events[0].Occupant; got.Before.Location != "loc:camp" || got.After.Location != "loc:mill" {
		t.Fatalf("move event %+v", got)
	}

	// A created occupant is known to the rest of its request.
	r = execute(t, w, "scout", `create occupant "occ:scout" named "Scout" at "loc:gate" { person; alias "Zed"; alias "Ann"; }; move occupant "occ:scout" to "loc:keep";`)
	wantKinds(t, r, core.OccupantCreated, core.OccupantMoved)
	if got := r.Events[0].Occupant; got.Before != nil || got.After.Attitude != core.AttitudeIndifferent || *got.After.Count != 1 || !reflect.DeepEqual(got.After.Aliases, []string{"Ann", "Zed"}) {
		t.Fatalf("created %+v", got)
	}
	// The intent stays as requested; the record is sorted.
	if got := r.Events[0].Occupant.Intent.Create.Aliases; !reflect.DeepEqual(got, []string{"Zed", "Ann"}) {
		t.Fatalf("intent aliases %v", got)
	}

	execute(t, w, "join", `resolve occupant "occ:mira" as joined;`)
	if got := occupant(t, w, "occ:mira"); *got.Count != 0 || got.Location != "loc:mill" || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeJoined, Count: 1}}) {
		t.Fatalf("joined %+v", got)
	}
	costReplay(t, b, w)
}

func TestOccupantRefusalsLeaveNoTrace(t *testing.T) {
	aliases := ""
	for i := 0; i < core.MaxOccupantAliases; i++ {
		aliases += fmt.Sprintf(` alias "A%02d";`, i)
	}
	for _, tc := range []struct {
		name, actions, code, field, actual string
	}{
		{"move resolved", `resolve occupant "occ:bandits" as defeated; move occupant "occ:bandits" to "loc:camp";`, "E_OCCUPANT", "move", "resolved"},
		{"move nowhere", `move occupant "occ:bandits" to "loc:mill";`, "E_NO_CHANGE", "operation", ""},
		{"count a person", `count occupant "occ:mira" as 1;`, "E_OCCUPANT", "count", "person"},
		{"count twice", `count occupant "occ:bandits" as 3;`, "E_OCCUPANT", "count", "counted"},
		{"count above", `count occupant "occ:wolves" as 5;`, "E_OCCUPANT", "count", "5"},
		{"count below", `count occupant "occ:wolves" as 1;`, "E_OCCUPANT", "count", "1"},
		{"count again", `count occupant "occ:wolves" as 2; count occupant "occ:wolves" as 3;`, "E_OCCUPANT", "count", "counted"},
		{"resolve uncounted", `resolve occupant "occ:wolves" as fled;`, "E_OCCUPANT", "resolve", "uncounted"},
		{"resolve none left", `resolve occupant "occ:bandits" as fled; resolve occupant "occ:bandits" as fled;`, "E_OCCUPANT", "resolve", "none"},
		{"resolve too many", `resolve occupant "occ:bandits" as fled by 4;`, "E_OCCUPANT", "resolve", "4"},
		{"resolve a person twice over", `resolve occupant "occ:mira" as died by 2;`, "E_OCCUPANT", "resolve", "2"},
		{"same attitude", `set occupant "occ:bandits" attitude hostile;`, "E_NO_CHANGE", "operation", ""},
		{"same name", `rename occupant "occ:mira" to "Mira Tidewise";`, "E_NO_CHANGE", "operation", ""},
		{"too many aliases", `create occupant "occ:x" named "X" at "loc:camp" { person;` + aliases + ` }; rename occupant "occ:x" to "Y";`, "E_OCCUPANT", "aliases", "17"},
		{"later failure", `count occupant "occ:wolves" as 3; resolve occupant "occ:wolves" as defeated by 4;`, "E_OCCUPANT", "resolve", "4"},
		{"leave a party twice", `leave party "char:bea"; leave party "char:bea";`, "E_NO_CHANGE", "operation", ""},
		{"join twice", `join party "char:ada";`, "E_NO_CHANGE", "operation", ""},
		{"close a closed route", `close route "loc:camp" to "loc:ford"; close route "loc:camp" to "loc:ford";`, "E_NO_CHANGE", "operation", ""},
		{"open an open route", `open route "loc:camp" to "loc:ford";`, "E_NO_CHANGE", "operation", ""},
		{"mark twice", `mark hazard "loc:camp"; mark hazard "loc:camp";`, "E_NO_CHANGE", "operation", ""},
		{"mark visited twice", `mark visited "loc:gate"; mark visited "loc:gate";`, "E_NO_CHANGE", "operation", ""},
		{"clear nothing", `clear hazard "loc:camp";`, "E_NO_CHANGE", "operation", ""},
		{"clear hostile", `clear hazard "loc:mill";`, "E_MAP", "hazard", "hostile occupant occ:bandits"},
		{"clear uncounted hostile", `clear hazard "loc:ford";`, "E_MAP", "hazard", "hostile occupant occ:wolves"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := world(t, occupantFixture(t, ""))
			before, history := w.Snapshot(), w.History()
			_, err := w.Execute(request(t, w, "go", tc.actions))
			wantFault(t, err, tc.code, tc.field, tc.actual)
			unchanged(t, w, before, history)
		})
	}

	// The engine checks what it is handed, whatever the compiler saw.
	w := world(t, occupantFixture(t, ""))
	before, history := w.Snapshot(), w.History()
	for _, tc := range []struct {
		name        string
		op          core.Operation
		code, field string
	}{
		{"two changes", core.Operation{Occupant: &core.OccupantOp{Move: &core.MoveOccupant{Occupant: "occ:mira", To: "loc:mill"}, Rename: &core.RenameOccupant{Occupant: "occ:mira", Name: "M"}}}, "E_IR", "occupant"},
		{"no change", core.Operation{Occupant: &core.OccupantOp{}}, "E_IR", "occupant"},
		{"two map edits", core.Operation{MapEdit: &core.MapEdit{MarkHazard: locPtr("loc:camp"), ClearHazard: locPtr("loc:camp")}}, "E_IR", "map edit"},
		{"unknown occupant", core.Operation{Occupant: &core.OccupantOp{Attitude: &core.SetAttitude{Occupant: "occ:nobody", Attitude: core.AttitudeHostile}}}, "E_OCCUPANT", "occupant"},
		{"unknown attitude", core.Operation{Occupant: &core.OccupantOp{Attitude: &core.SetAttitude{Occupant: "occ:mira", Attitude: "angry"}}}, "E_OCCUPANT", "attitude"},
		{"unknown outcome", core.Operation{Occupant: &core.OccupantOp{Resolve: &core.ResolveOccupant{Occupant: "occ:mira", Outcome: "won"}}}, "E_OCCUPANT", "outcome"},
		{"zero resolved", core.Operation{Occupant: &core.OccupantOp{Resolve: &core.ResolveOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled, Amount: count(0)}}}, "E_AMOUNT", "amount"},
		{"departed without a reason", core.Operation{Occupant: &core.OccupantOp{Resolve: &core.ResolveOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeDeparted}}}, "E_OCCUPANT", "reason"},
		{"long reason", core.Operation{Occupant: &core.OccupantOp{Resolve: &core.ResolveOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled, Reason: strings.Repeat("r", core.MaxReasonBytes+1)}}}, "E_OCCUPANT", "reason"},
		{"return and resolve", core.Operation{Occupant: &core.OccupantOp{Resolve: &core.ResolveOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled}, Return: &core.ReturnOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled}}}, "E_IR", "occupant"},
		{"return nobody", core.Operation{Occupant: &core.OccupantOp{Return: &core.ReturnOccupant{Occupant: "occ:nobody", Outcome: core.OutcomeFled}}}, "E_OCCUPANT", "occupant"},
		{"return an unknown outcome", core.Operation{Occupant: &core.OccupantOp{Return: &core.ReturnOccupant{Occupant: "occ:bandits", Outcome: "won"}}}, "E_OCCUPANT", "outcome"},
		{"return a negative amount", core.Operation{Occupant: &core.OccupantOp{Return: &core.ReturnOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled, Amount: count(-1)}}}, "E_AMOUNT", "amount"},
		{"return a blank reason", core.Operation{Occupant: &core.OccupantOp{Return: &core.ReturnOccupant{Occupant: "occ:bandits", Outcome: core.OutcomeFled, Reason: "\t"}}}, "E_OCCUPANT", "reason"},
		{"blank name", core.Operation{Occupant: &core.OccupantOp{Rename: &core.RenameOccupant{Occupant: "occ:mira", Name: " "}}}, "E_OCCUPANT", "name"},
		{"move to a character", core.Operation{Occupant: &core.OccupantOp{Move: &core.MoveOccupant{Occupant: "occ:mira", To: "char:ada"}}}, "E_TYPE", "reference"},
		{"create taken", core.Operation{Occupant: &core.OccupantOp{Create: &core.Occupant{ID: "char:ada", Name: "A", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(1), Attitude: core.AttitudeFriendly}}}, "E_DUPLICATE", "occupant"},
		{"create resolved", core.Operation{Occupant: &core.OccupantOp{Create: &core.Occupant{ID: "occ:x", Name: "X", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(0), Attitude: core.AttitudeFriendly, Tally: []core.OccupantTally{{Outcome: core.OutcomeDied, Count: 1}}}}}, "E_OCCUPANT", "create"},
		{"create without kind", core.Operation{Occupant: &core.OccupantOp{Create: &core.Occupant{ID: "occ:x", Name: "X", Location: "loc:camp", Count: count(1), Attitude: core.AttitudeFriendly}}}, "E_OCCUPANT", "kind"},
		{"create nowhere", core.Operation{Occupant: &core.OccupantOp{Create: &core.Occupant{ID: "occ:x", Name: "X", Location: "loc:nowhere", Kind: core.OccupantPerson, Count: count(1), Attitude: core.AttitudeFriendly}}}, "E_REFERENCE", "reference"},
		{"close undeclared", core.Operation{MapEdit: &core.MapEdit{CloseRoute: &core.MapLink{From: "loc:camp", To: "loc:gate"}}}, "E_MAP", "route"},
		{"join a place", core.Operation{MapEdit: &core.MapEdit{JoinParty: charPtr("loc:camp")}}, "E_TYPE", "reference"},
		{"mark a character", core.Operation{MapEdit: &core.MapEdit{MarkHazard: locPtr("char:ada")}}, "E_TYPE", "reference"},
	} {
		// A second operation that would be refused for another reason: the
		// first is refused before anything runs.
		later := core.Operation{Occupant: &core.OccupantOp{Move: &core.MoveOccupant{Occupant: "occ:bandits", To: "loc:mill"}}}
		_, err := w.Execute(core.Request{ID: "raw", ExpectedRevision: 0, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: []core.Operation{tc.op, later}})
		t.Run(tc.name, func(t *testing.T) { wantFault(t, err, tc.code, tc.field, "") })
		unchanged(t, w, before, history)
	}
}

// Why members left is kept per reason, and members come back from the entry a
// request names.
func TestOccupantReasonsAndReturn(t *testing.T) {
	b := occupantFixture(t, ` visited "loc:ford"; visited "loc:mill";`+"\n")
	w := world(t, b)
	r := execute(t, w, "parley", `resolve occupant "occ:bandits" as departed by 1 because "bribed to leave"; resolve occupant "occ:bandits" as departed by 1 because "scared off by the fire"; resolve occupant "occ:bandits" as defeated;`)
	wantKinds(t, r, core.OccupantResolved, core.OccupantResolved, core.OccupantResolved)
	if got := r.Events[0].Occupant.Intent.Resolve; got.Reason != "bribed to leave" || *got.Amount != 1 {
		t.Fatalf("intent %+v", got)
	}
	departed := []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 1, Reason: "bribed to leave"}, {Outcome: core.OutcomeDeparted, Count: 1, Reason: "scared off by the fire"}}
	if got := occupant(t, w, "occ:bandits"); *got.Count != 0 || !reflect.DeepEqual(got.Tally, append([]core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 1}}, departed...)) {
		t.Fatalf("bandits %+v", got.Tally)
	}
	if p, _ := w.LocationView("loc:mill"); *p.Hazard || len(p.Resolved) != 1 || !reflect.DeepEqual(p.Resolved[0].Tally[1:], departed) {
		t.Fatalf("mill %+v", p)
	}

	refused := func(actions, code, field, actual string) {
		t.Helper()
		before, history := w.Snapshot(), w.History()
		_, err := w.Execute(request(t, w, "no", actions))
		wantFault(t, err, code, field, actual)
		unchanged(t, w, before, history)
	}
	// Two departed entries: a return names one.
	refused(`return occupant "occ:bandits" from departed;`, "E_OCCUPANT", "return", "ambiguous")
	refused(`return occupant "occ:bandits" from departed "lost";`, "E_OCCUPANT", "return", "none")
	refused(`return occupant "occ:bandits" from fled;`, "E_OCCUPANT", "return", "none")
	refused(`return occupant "occ:bandits" from departed "bribed to leave" by 2;`, "E_OCCUPANT", "return", "2")
	refused(`return occupant "occ:wolves" from fled;`, "E_OCCUPANT", "return", "none")

	r = execute(t, w, "back", `return occupant "occ:bandits" from departed "bribed to leave";`)
	wantKinds(t, r, core.OccupantReturned)
	c := r.Events[0].Occupant
	if *c.Before.Count != 0 || len(c.Before.Tally) != 3 || *c.After.Count != 1 || c.Intent.Return.Reason != "bribed to leave" || c.Intent.Return.Amount != nil {
		t.Fatalf("return event %+v", c)
	}
	// Back and hostile, the bandits stop trips at the mill again.
	if p, _ := w.LocationView("loc:mill"); !*p.Hazard || len(p.Present) != 1 || !reflect.DeepEqual(p.Present[0].Tally, []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 1}, departed[1]}) {
		t.Fatalf("mill %+v", p)
	}
	// Without a reason a return takes the outcome's only entry.
	execute(t, w, "rally", `return occupant "occ:bandits" from defeated; return occupant "occ:bandits" from departed;`)
	if got := occupant(t, w, "occ:bandits"); *got.Count != 3 || got.Tally != nil {
		t.Fatalf("all back %+v", got)
	}

	// Without a reason a return takes the entry with none, when there is one;
	// then the only entry left, in part.
	execute(t, w, "ford", `count occupant "occ:wolves" as 4; resolve occupant "occ:wolves" as fled by 3 because "fire"; resolve occupant "occ:wolves" as fled;`)
	execute(t, w, "one", `return occupant "occ:wolves" from fled;`)
	if got := occupant(t, w, "occ:wolves"); *got.Count != 1 || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeFled, Count: 3, Reason: "fire"}}) {
		t.Fatalf("wolves %+v", got)
	}
	execute(t, w, "two", `return occupant "occ:wolves" from fled by 2;`)
	if got := occupant(t, w, "occ:wolves"); *got.Count != 3 || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeFled, Count: 1, Reason: "fire"}}) {
		t.Fatalf("wolves %+v", got)
	}

	// A reason of 200 bytes is the longest, given or named.
	long := strings.Repeat("r", core.MaxReasonBytes)
	execute(t, w, "long", `resolve occupant "occ:wolves" as fled by 1 because "`+long+`"; return occupant "occ:wolves" from fled "`+long+`";`)
	if got := occupant(t, w, "occ:wolves"); *got.Count != 3 || len(got.Tally) != 1 {
		t.Fatalf("wolves %+v", got)
	}

	// A person comes back from the dead, as the same occupant.
	execute(t, w, "fall", `resolve occupant "occ:mira" as died because "the bridge gave way";`)
	r = execute(t, w, "rise", `return occupant "occ:mira" from died;`)
	if got := r.Events[0].Occupant.After; *got.Count != 1 || got.Tally != nil || got.Name != "Mira Tidewise" || got.Location != "loc:camp" {
		t.Fatalf("mira %+v", got)
	}
	costReplay(t, b, w)
}

func locPtr(id string) *core.LocationID   { v := core.LocationID(id); return &v }
func charPtr(id string) *core.CharacterID { v := core.CharacterID(id); return &v }

// A place where a hostile occupant is present stops a trip like a declared
// hazard, until the occupant is resolved or no longer hostile.
func TestOccupantHazards(t *testing.T) {
	b := occupantFixture(t, ` visited "loc:ford"; visited "loc:mill";`+"\n")
	w := world(t, b)
	view, err := w.MapView("char:ada")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range view.Exits {
		if !x.Hazard {
			t.Fatalf("exit to %s is not a hazard: %+v", x.To.ID, view.Exits)
		}
	}
	for _, d := range view.Destinations {
		if d.To.ID == "loc:gate" && (d.HaltsAt == nil || *d.HaltsAt != "loc:ford") {
			t.Fatalf("gate halts at %v", d.HaltsAt)
		}
	}

	// The wolves, not yet counted, stop the party at the ford.
	r := execute(t, w, "go", `travel party to "loc:gate";`)
	if got := trip(t, r); !got.Halted || !reflect.DeepEqual(got.Stops, locs("loc:ford")) {
		t.Fatalf("trip %+v", got)
	}
	// Resolved, they no longer do; the bandits at the mill do.
	execute(t, w, "fight", `count occupant "occ:wolves" as 2; resolve occupant "occ:wolves" as fled;`)
	r = execute(t, w, "on", `travel party to "loc:gate";`)
	if got := trip(t, r); !got.Halted || !reflect.DeepEqual(got.Stops, locs("loc:mill")) {
		t.Fatalf("trip %+v", got)
	}
	// Bandits who are no longer hostile let the party pass.
	execute(t, w, "parley", `set occupant "occ:bandits" attitude indifferent; move occupant "occ:mira" to "loc:gate";`)
	execute(t, w, "back", `travel party to "loc:ford";`)
	execute(t, w, "turn", `set occupant "occ:bandits" attitude hostile; set occupant "occ:mira" attitude hostile;`)
	// A hostile occupant at the destination is reached; on the way it stops.
	r = execute(t, w, "again", `travel party to "loc:gate";`)
	if got := trip(t, r); !got.Halted || !reflect.DeepEqual(got.Stops, locs("loc:mill")) {
		t.Fatalf("trip %+v", got)
	}
	execute(t, w, "calm", `set occupant "occ:bandits" attitude friendly;`)
	r = execute(t, w, "last", `travel party to "loc:gate";`)
	if got := trip(t, r); got.Halted || !reflect.DeepEqual(got.Stops, locs("loc:gate")) {
		t.Fatalf("trip %+v", got)
	}
	costReplay(t, b, w)
}

// A place marked visited may be passed through, as if the party had walked
// it. Marking moves no one and passes no time.
func TestMarkVisited(t *testing.T) {
	b := mapFixture(t, "")
	w := world(t, b)
	_, err := w.Execute(request(t, w, "go", `travel party to "loc:gate";`))
	wantMapFault(t, err, "travel path", "unvisited stop")
	before := w.Snapshot()
	r := execute(t, w, "known", `mark visited "loc:mill";`)
	wantKinds(t, r, core.VisitMarked)
	if e := r.Events[0].MapEdit; e == nil || e.MarkVisited == nil || *e.MarkVisited != "loc:mill" {
		t.Fatalf("event %+v", r.Events[0])
	}
	after := w.Snapshot()
	if !reflect.DeepEqual(after.Map.Visited, locs("loc:mill")) || !reflect.DeepEqual(after.Characters, before.Characters) || !reflect.DeepEqual(after.Clock, before.Clock) || !reflect.DeepEqual(after.Map.Hazards, before.Map.Hazards) {
		t.Fatalf("after mark %+v", after.Map)
	}
	r = execute(t, w, "go", `travel party to "loc:gate";`)
	if got := trip(t, r); !reflect.DeepEqual(got.Stops, locs("loc:mill", "loc:gate")) || got.Ticks != 50 || !reflect.DeepEqual(got.Visited, locs("loc:camp", "loc:gate")) {
		t.Fatalf("trip %+v", got)
	}
	costReplay(t, b, w)
}

func TestMapEditsInPlay(t *testing.T) {
	b := occupantFixture(t, ` visited "loc:ford"; visited "loc:mill";`+"\n")
	w := world(t, b)
	r := execute(t, w, "edits", `leave party "char:bea"; join party "char:cy"; close route "loc:camp" to "loc:ford"; mark hazard "loc:camp"; mark hazard "loc:mill";`)
	wantKinds(t, r, core.PartyLeft, core.PartyJoined, core.RouteClosed, core.HazardMarked, core.HazardMarked)
	if e := r.Events[2].MapEdit; e == nil || *e.CloseRoute != (core.MapLink{From: "loc:camp", To: "loc:ford"}) {
		t.Fatalf("close event %+v", r.Events[2])
	}
	m := w.Snapshot().Map
	if !reflect.DeepEqual(m.Party, []core.CharacterID{"char:ada", "char:cy"}) || !reflect.DeepEqual(m.Closed, []core.MapLink{{From: "loc:camp", To: "loc:ford"}}) || !reflect.DeepEqual(m.Hazards, locs("loc:camp", "loc:mill")) {
		t.Fatalf("map %+v", m)
	}
	// The party travels as it now is: Cy is at the keep.
	_, err := w.Execute(request(t, w, "go", `travel party to "loc:ford";`))
	wantMapFault(t, err, "travel party", "loc:keep")
	execute(t, w, "regroup", `leave party "char:cy"; open route "loc:camp" to "loc:ford"; clear hazard "loc:camp";`)
	// The declared hazard at the mill goes; the bandits there remain one.
	execute(t, w, "clear", `clear hazard "loc:mill";`)
	if m := w.Snapshot().Map; len(m.Hazards) != 0 {
		t.Fatalf("declared hazards %v", m.Hazards)
	}
	p, err := w.LocationView("loc:mill")
	if err != nil || !*p.Hazard || !*p.Visited {
		t.Fatalf("mill %+v %v", p, err)
	}
	r = execute(t, w, "go", `travel party to "loc:ford";`)
	if got := trip(t, r); !reflect.DeepEqual(got.Stops, locs("loc:ford")) {
		t.Fatalf("trip %+v", got)
	}
	// A member who joins travels in the same request, in ID order.
	r = execute(t, w, "abe", `create character "char:abe" named "Abe" at "loc:ford" { }; join party "char:abe"; travel party to "loc:camp";`)
	if r.Events[2].Kind != core.EntityMoved || r.Events[2].Movement.Character != "char:abe" {
		t.Fatalf("first move %+v", r.Events[2])
	}
	costReplay(t, b, w)
}

// tamper changes every part of an occupant record a copy could share.
func tamper(o *core.Occupant) {
	o.Name, o.Tally = "Why", append(o.Tally, core.OccupantTally{Outcome: core.OutcomeDied, Count: 9})
	if len(o.Aliases) > 0 {
		o.Aliases[0] = "Why"
	}
	if len(o.Tally) > 1 {
		o.Tally[0].Count = 9
	}
	if o.Count != nil {
		*o.Count = 9
	}
	if o.Range != nil {
		o.Range.Max = 9
	}
}

// Requests, receipts, snapshots and history are the caller's to change.
func TestOccupantDetached(t *testing.T) {
	w := world(t, occupantFixture(t, ""))
	r := request(t, w, "go", `create occupant "occ:x" named "X" at "loc:camp" { creatures 2 to 3; alias "Ex"; }; count occupant "occ:x" as 2; resolve occupant "occ:x" as fled by 1; close route "loc:camp" to "loc:ford"; mark hazard "loc:camp"; return occupant "occ:x" from fled by 1;`)
	batch, err := w.Execute(r)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, history := contractBytes(t, w.Snapshot()), contractBytes(t, w.History())
	create := r.Operations[0].Occupant.Create
	create.Name, create.Aliases[0], *create.Range = "Y", "Why", core.CountRange{Min: 7, Max: 9}
	*r.Operations[2].Occupant.Resolve.Amount = 2
	r.Operations[3].MapEdit.CloseRoute.To = "loc:mill"
	*r.Operations[4].MapEdit.MarkHazard = "loc:mill"
	*r.Operations[5].Occupant.Return.Amount = 2
	for _, e := range batch.Events {
		if c := e.Occupant; c != nil {
			tamper(&c.After)
			if c.Before != nil {
				tamper(c.Before)
			}
			if c.Intent.Resolve != nil {
				*c.Intent.Resolve.Amount = 2
			}
			if c.Intent.Return != nil {
				*c.Intent.Return.Amount = 2
			}
		}
		if m := e.MapEdit; m != nil && m.CloseRoute != nil {
			m.CloseRoute.To = "loc:mill"
		}
	}
	contractEqual(t, snapshot, w.Snapshot())
	contractEqual(t, history, w.History())
	// Snapshots and history are copies too.
	s := w.Snapshot()
	for i := range s.Occupants {
		tamper(&s.Occupants[i])
	}
	contractEqual(t, snapshot, w.Snapshot())
	h := w.History()
	for _, e := range h[0].Events {
		if c := e.Occupant; c != nil {
			tamper(&c.After)
			if c.Before != nil {
				tamper(c.Before)
			}
			if v := c.Intent.Create; v != nil {
				tamper(v)
			}
		}
		if m := e.MapEdit; m != nil && m.MarkHazard != nil {
			*m.MarkHazard = "loc:mill"
		}
		if m := e.MapEdit; m != nil && m.CloseRoute != nil {
			m.CloseRoute.To = "loc:mill"
		}
	}
	ops := h[0].Request.Operations
	*ops[2].Occupant.Resolve.Amount = 2
	ops[3].MapEdit.CloseRoute.To = "loc:mill"
	*ops[4].MapEdit.MarkHazard = "loc:mill"
	*ops[5].Occupant.Return.Amount = 2
	contractEqual(t, history, w.History())
}

func TestLocationView(t *testing.T) {
	b := occupantFixture(t, ` visited "loc:ford";`+"\n")
	w := world(t, b)
	execute(t, w, "fight", `count occupant "occ:wolves" as 4; resolve occupant "occ:wolves" as defeated by 3; create occupant "occ:den" named "Old Den Wolves" at "loc:ford" { creatures 2; attitude indifferent; }; resolve occupant "occ:den" as departed because "the den flooded";`)
	p, err := w.LocationView("loc:ford")
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	want := core.LocationProjection{
		Revision: 1, Clock: &core.Clock{Unit: "minute", Tick: 100},
		Location: core.Location{ID: "loc:ford", Name: "Ford"}, Visited: &yes, Hazard: &yes,
		Characters: []core.CharacterSummary{},
		Present:    []core.Occupant{{ID: "occ:wolves", Name: "Wolves", Location: "loc:ford", Kind: core.OccupantCreatures, Count: count(1), Attitude: core.AttitudeHostile, Type: "wolf", Tally: []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 3}}}},
		Resolved:   []core.Occupant{{ID: "occ:den", Name: "Old Den Wolves", Location: "loc:ford", Kind: core.OccupantCreatures, Count: count(0), Attitude: core.AttitudeIndifferent, Tally: []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 2, Reason: "the den flooded"}}}},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("view\n%+v\nwant\n%+v", p, want)
	}
	// The view is the caller's to change.
	*p.Present[0].Count, p.Resolved[0].Tally[0].Count, *p.Hazard = 9, 9, false
	if again, _ := w.LocationView("loc:ford"); !reflect.DeepEqual(again, want) {
		t.Fatalf("view aliases the world: %+v", again)
	}

	camp, err := w.LocationView("loc:camp")
	if err != nil || len(camp.Characters) != 2 || camp.Characters[0].ID != "char:ada" || camp.Characters[1].ID != "char:bea" || len(camp.Present) != 1 || *camp.Visited || *camp.Hazard {
		t.Fatalf("camp %+v %v", camp, err)
	}
	if _, err := w.LocationView("loc:nowhere"); err == nil {
		t.Fatal("view of an unknown place")
	}
	// Several places read in one pass, in the order asked, each its own copy;
	// at an unknown place the views before it come back with the fault.
	all, err := w.LocationsView(locs("loc:camp", "loc:ford", "loc:camp"))
	if err != nil || len(all) != 3 || !reflect.DeepEqual(all[0], camp) || !reflect.DeepEqual(all[1], want) || !reflect.DeepEqual(all[2], camp) {
		t.Fatalf("views %+v %v", all, err)
	}
	all[0].Characters[0].Name, *all[1].Present[0].Count = "Why", 9
	if again, _ := w.LocationsView(locs("loc:camp", "loc:ford", "loc:camp")); !reflect.DeepEqual(again[2], camp) || !reflect.DeepEqual(again[1], want) {
		t.Fatalf("views alias each other or the world: %+v", again)
	}
	all, err = w.LocationsView(locs("loc:ford", "loc:nowhere", "loc:camp"))
	wantFault(t, err, "E_REFERENCE", "reference", "")
	if len(all) != 1 || !reflect.DeepEqual(all[0], want) {
		t.Fatalf("views before the fault %+v", all)
	}
	if all, err := w.LocationsView(nil); err != nil || len(all) != 0 {
		t.Fatalf("no places %+v %v", all, err)
	}
	if _, err := w.LocationView("char:ada"); err == nil {
		t.Fatal("view of a character")
	}
	// Without a map the view has no visit or hazard.
	plain, ds := compiler.CompileWorld("plain.nql", mapPlaces+occupantRoster)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	p, err = world(t, plain).LocationView("loc:mill")
	if err != nil || p.Visited != nil || p.Hazard != nil || len(p.Present) != 1 {
		t.Fatalf("plain %+v %v", p, err)
	}
}

// Each view reads one revision while requests commit; run with -race too.
func TestLocationsViewConcurrent(t *testing.T) {
	w := world(t, occupantFixture(t, ""))
	var moves [2][]core.Operation
	for k, to := range []string{"loc:mill", "loc:camp"} {
		ops, ds := compiler.CompileActions("move.nql", `move occupant "occ:mira" to "`+to+`";`, w.Snapshot().Blueprint)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		moves[k] = ops
	}
	const n = 200
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_, errs[i] = w.Execute(core.Request{ID: fmt.Sprint(i), ExpectedRevision: core.Revision(i), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: moves[i%2]})
		}
	}()
	// Mira is at the mill after an odd number of moves, at camp otherwise.
	for i := 0; i < n; i++ {
		v, err := w.LocationsView(locs("loc:camp", "loc:mill"))
		if err != nil {
			t.Fatal(err)
		}
		mill := v[1].Revision%2 == 1
		if v[0].Revision != v[1].Revision || (len(v[0].Present) == 0) != mill || (len(v[1].Present) == 2) != mill {
			t.Fatalf("torn view %+v", v)
		}
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
}

func TestOccupantCompileRefusals(t *testing.T) {
	head := strings.Count(mapPlaces, "\n") + 1 // the roster's first line
	aliases := ""
	for i := 0; i <= core.MaxOccupantAliases; i++ {
		aliases += fmt.Sprintf(` alias "A%02d";`, i)
	}
	for _, tc := range []struct {
		name, body string
		line       int    // line within the body
		at         string // the last occurrence on that line marks the column
		code       string
		expected   string
	}{
		{"no kind", `occupant "occ:x" named "X" at "loc:camp" { attitude hostile; }`, 1, `"occ:x"`, "E_OCCUPANT", "person or creatures"},
		{"negative count", `occupant "occ:x" named "X" at "loc:camp" { creatures -1; }`, 1, `-1`, "E_OCCUPANT", ""},
		{"nobody", `occupant "occ:x" named "X" at "loc:camp" { creatures 0; }`, 1, `0`, "E_OCCUPANT", ""},
		{"too many", `occupant "occ:x" named "X" at "loc:camp" { creatures 1000000001; }`, 1, `1000000001`, "E_OCCUPANT", ""},
		{"too many with the tally", `occupant "occ:x" named "X" at "loc:camp" { creatures 1000000000; tally fled 1; }`, 1, `1000000000`, "E_OCCUPANT", ""},
		{"negative with a tally", `occupant "occ:x" named "X" at "loc:camp" { creatures -1; tally fled 3; }`, 1, `-1`, "E_OCCUPANT", ""},
		{"range too large", `occupant "occ:x" named "X" at "loc:camp" { creatures 1 to 1000000001; }`, 1, `1 to`, "E_OCCUPANT", "1 <= min < max <= 1000000000"},
		{"range backwards", `occupant "occ:x" named "X" at "loc:camp" { creatures 4 to 2; }`, 1, `4`, "E_OCCUPANT", "1 <= min < max <= 1000000000"},
		{"range from zero", `occupant "occ:x" named "X" at "loc:camp" { creatures 0 to 2; }`, 1, `0`, "E_OCCUPANT", "1 <= min < max <= 1000000000"},
		{"range of one", `occupant "occ:x" named "X" at "loc:camp" { creatures 2 to 2; }`, 1, `2 to`, "E_OCCUPANT", "1 <= min < max <= 1000000000"},
		{"range with a tally", `occupant "occ:x" named "X" at "loc:camp" { creatures 2 to 3; tally fled 1; }`, 1, `fled`, "E_OCCUPANT", "a counted group"},
		{"person and creatures", `occupant "occ:x" named "X" at "loc:camp" { person; creatures 2; }`, 1, `creatures`, "E_DUPLICATE", "one person or creatures"},
		{"two attitudes", `occupant "occ:x" named "X" at "loc:camp" { person; attitude hostile; attitude friendly; }`, 1, `attitude`, "E_DUPLICATE", "one attitude"},
		{"two types", `occupant "occ:x" named "X" at "loc:camp" { person; type "a"; type "b"; }`, 1, `type`, "E_DUPLICATE", "one type"},
		{"unknown attitude", `occupant "occ:x" named "X" at "loc:camp" { person; attitude angry; }`, 1, `angry`, "E_PARSE", ""},
		{"unknown outcome", `occupant "occ:x" named "X" at "loc:camp" { person; tally won 1; }`, 1, `won`, "E_PARSE", ""},
		{"outcome twice", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 1; tally fled 2; }`, 1, `fled`, "E_OCCUPANT", "one entry per outcome and reason"},
		{"reason twice", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 1 because "fire"; tally fled 1 because "gold"; tally fled 2 because "gold"; }`, 1, `fled`, "E_OCCUPANT", "one entry per outcome and reason"},
		{"departed without a reason", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 1; tally departed 2; }`, 1, `departed`, "E_OCCUPANT", "a reason for departed"},
		{"blank reason", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 1 because "fire"; tally died 1 because " "; }`, 1, `" "`, "E_OCCUPANT", "valid text of at most 200 bytes"},
		{"long reason", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally departed 1 because "` + strings.Repeat("r", core.MaxReasonBytes+1) + `"; }`, 1, `"r`, "E_OCCUPANT", "valid text of at most 200 bytes"},
		{"reason without text", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 1 because; }`, 1, `;`, "E_PARSE", ""},
		{"empty tally", `occupant "occ:x" named "X" at "loc:camp" { creatures 1; tally fled 0; }`, 1, `0`, "E_OCCUPANT", "1..1000000000"},
		{"person leaves twice", `occupant "occ:x" named "X" at "loc:camp" { person; tally fled 1; tally died 1; }`, 1, `fled`, "E_OCCUPANT", "a person leaves once"},
		{"alias is the name", `occupant "occ:x" named "X" at "loc:camp" { person; alias "X"; }`, 1, `"X"`, "E_OCCUPANT", "a name not already used by this occupant"},
		{"alias twice", `occupant "occ:x" named "X" at "loc:camp" { person; alias "Y"; alias "Y"; }`, 1, `"Y"`, "E_OCCUPANT", "a name not already used by this occupant"},
		{"blank alias", `occupant "occ:x" named "X" at "loc:camp" { person; alias "Y"; alias " "; }`, 1, `" "`, "E_OCCUPANT", "valid name"},
		{"too many aliases", `occupant "occ:x" named "X" at "loc:camp" { person;` + aliases + ` }`, 1, `"A16"`, "E_OCCUPANT", "at most 16 aliases"},
		{"blank name", `occupant "occ:x" named "" at "loc:camp" { person; }`, 1, `""`, "E_OCCUPANT", "valid name"},
		{"blank type", `occupant "occ:x" named "X" at "loc:camp" { person; type " "; }`, 1, `" "`, "E_OCCUPANT", "valid type"},
		{"unknown place", `occupant "occ:x" named "X" at "loc:nowhere" { person; }`, 1, `"loc:nowhere"`, "E_REFERENCE", ""},
		{"at a character", `occupant "occ:x" named "X" at "char:ada" { person; }`, 1, `"char:ada"`, "E_TYPE", ""},
		{"a place's ID", `occupant "loc:camp" named "X" at "loc:camp" { person; }`, 1, `"loc:camp" named`, "E_DUPLICATE", "unique canonical ID"},
		{"two of one ID", "occupant \"occ:x\" named \"X\" at \"loc:camp\" { person; }\noccupant \"occ:x\" named \"Y\" at \"loc:camp\" { person; }", 2, `"occ:x"`, "E_DUPLICATE", "unique canonical ID"},
		{"missing brace", `occupant "occ:x" named "X" at "loc:camp" { person;`, 1, `;`, "E_PARSE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileWorld("roster.nql", mapPlaces+tc.body+"\n")
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			text := strings.Split(tc.body, "\n")[tc.line-1]
			line, column := head+tc.line-1, strings.LastIndex(text, tc.at)+1
			if tc.name == "a place's ID" || tc.name == "range of one" {
				column = strings.Index(text, tc.at) + 1
			}
			if tc.name == "missing brace" {
				line, column = head+1, 1
			}
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != line || d.Column != column {
				t.Fatalf("got %#v, want %s %q at %d:%d", d, tc.code, tc.expected, line, column)
			}
		})
	}

	// Occupants need a clock.
	src := strings.Replace(mapPlaces, "clock \"minute\" at 100;\n", "", 1) + `occupant "occ:x" named "X" at "loc:camp" { person; }` + "\n"
	_, ds := compiler.CompileWorld("roster.nql", src)
	if len(ds) != 1 || ds[0].Code != "E_OCCUPANT" || ds[0].Expected != "declared clock" || ds[0].Line != head-1 || ds[0].Column != 1 {
		t.Fatalf("no clock: %#v", ds)
	}
}

func TestOccupantActionCompileRefusals(t *testing.T) {
	catalog := occupantFixture(t, "")
	plain, ds := compiler.CompileWorld("plain.nql", strings.Replace(mapPlaces, "clock \"minute\" at 100;\n", "", 1))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct {
		name, src, at, code, expected string
		noMap                         bool
	}{
		{"unknown occupant", `move occupant "occ:nobody" to "loc:camp";`, `"occ:nobody"`, "E_OCCUPANT", "declared occupant", false},
		{"unknown place", `move occupant "occ:mira" to "loc:nowhere";`, `"loc:nowhere"`, "E_REFERENCE", "", false},
		{"move to a character", `move occupant "occ:mira" to "char:ada";`, `"char:ada"`, "E_TYPE", "", false},
		{"by zero", `resolve occupant "occ:bandits" as fled by 0;`, `0`, "E_AMOUNT", "", false},
		{"blank name", `rename occupant "occ:mira" to " ";`, `" "`, "E_OCCUPANT", "valid name", false},
		{"unknown outcome", `resolve occupant "occ:mira" as won;`, `won`, "E_PARSE", "", false},
		{"unknown attitude", `set occupant "occ:mira" attitude angry;`, `angry`, "E_PARSE", "", false},
		{"no occupant keyword", `count "occ:wolves" as 3;`, `"occ:wolves"`, "E_PARSE", "occupant", false},
		{"create taken", `create occupant "occ:mira" named "M" at "loc:camp" { person; };`, `"occ:mira"`, "E_DUPLICATE", "unique occupant ID", false},
		{"create twice", `create occupant "occ:x" named "X" at "loc:camp" { person; }; create occupant "occ:x" named "X" at "loc:camp" { person; };`, `"occ:x"`, "E_DUPLICATE", "unique occupant ID", false},
		{"create a character's ID", `create occupant "char:ada" named "A" at "loc:camp" { person; };`, `"char:ada"`, "E_DUPLICATE", "unique occupant ID", false},
		{"create resolved", `create occupant "occ:x" named "X" at "loc:camp" { creatures 2; tally fled 1; };`, `fled`, "E_OCCUPANT", "an occupant present with no one left", false},
		{"create nobody", `create occupant "occ:x" named "X" at "loc:camp" { creatures 0; };`, `0`, "E_OCCUPANT", "", false},
		{"create nowhere", `create occupant "occ:x" named "X" at "loc:nowhere" { person; };`, `"loc:nowhere"`, "E_REFERENCE", "", false},
		{"create at a character", `create occupant "occ:x" named "X" at "char:ada" { person; };`, `"char:ada"`, "E_TYPE", "", false},
		{"create blank and nowhere", `create occupant "occ:x" named " " at "loc:nowhere" { person; };`, `" "`, "E_OCCUPANT", "valid name", false},
		{"create without a clock", `create occupant "occ:x" named "X" at "loc:camp" { person; };`, `occupant`, "E_OCCUPANT", "declared clock", true},
		{"join nobody", `join party "char:nobody";`, `"char:nobody"`, "E_REFERENCE", "", false},
		{"close undeclared", `close route "loc:camp" to "loc:gate";`, `"loc:camp"`, "E_MAP", "declared route", false},
		{"mark without a map", `mark hazard "loc:camp";`, `mark`, "E_MAP", "world map", true},
		{"mark visited without a map", `mark visited "loc:camp";`, `mark`, "E_MAP", "world map", true},
		{"mark visited nowhere", `mark visited "loc:nowhere";`, `"loc:nowhere"`, "E_REFERENCE", "", false},
		{"mark visited a character", `mark visited "char:ada";`, `"char:ada"`, "E_TYPE", "", false},
		{"mark an unknown fact", `mark seen "loc:camp";`, `seen`, "E_PARSE", "", false},
		{"clear a character", `clear hazard "char:ada";`, `"char:ada"`, "E_TYPE", "", false},
		{"open without to", `open route "loc:camp";`, `;`, "E_PARSE", "to", false},
		{"departed without a reason", `resolve occupant "occ:mira" as departed;`, `departed`, "E_OCCUPANT", "a reason for departed", false},
		{"blank reason", `resolve occupant "occ:mira" as died because " ";`, `" "`, "E_OCCUPANT", "valid text of at most 200 bytes", false},
		{"reason before by", `resolve occupant "occ:bandits" as fled because "fire" by 1;`, `by`, "E_PARSE", "", false},
		{"return by zero", `return occupant "occ:bandits" from fled by 0;`, `0`, "E_AMOUNT", "", false},
		{"return a blank reason", `return occupant "occ:bandits" from fled " " by 1;`, `" "`, "E_OCCUPANT", "valid text of at most 200 bytes", false},
		{"return an unknown outcome", `return occupant "occ:mira" from won;`, `won`, "E_PARSE", "", false},
		{"return nobody", `return occupant "occ:nobody" from fled;`, `"occ:nobody"`, "E_OCCUPANT", "declared occupant", false},
		{"return as", `return occupant "occ:mira" as died;`, `as`, "E_PARSE", "from", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalog
			if tc.noMap {
				c = plain
			}
			_, ds := compiler.CompileActions("action.nql", tc.src, c)
			if len(ds) != 1 {
				t.Fatalf("diagnostics %v", ds)
			}
			d := ds[0]
			column := strings.LastIndex(tc.src, tc.at) + 1
			if tc.name == "create taken" || tc.name == "create a character's ID" || tc.name == "create without a clock" {
				column = strings.Index(tc.src, tc.at) + 1
			}
			if d.Code != tc.code || tc.expected != "" && d.Expected != tc.expected || d.Line != 1 || d.Column != column {
				t.Fatalf("got %#v, want %s %q at 1:%d", d, tc.code, tc.expected, column)
			}
		})
	}
}

func TestOccupantWorldLimits(t *testing.T) {
	b := occupantFixture(t, "")
	b.Occupants = nil
	for i := 0; i <= core.MaxWorldOccupants; i++ {
		b.Occupants = append(b.Occupants, core.Occupant{ID: core.OccupantID(fmt.Sprintf("occ:%05d", i)), Name: "N", Location: "loc:camp", Kind: core.OccupantPerson, Count: count(1), Attitude: core.AttitudeFriendly})
	}
	_, err := core.NewWorld(b)
	wantFault(t, err, "E_OCCUPANT", "occupants", "exceeded")
	b.Occupants = b.Occupants[:core.MaxWorldOccupants]
	w := world(t, b)
	before, history := w.Snapshot(), w.History()
	_, err = w.Execute(request(t, w, "one more", `create occupant "occ:more" named "M" at "loc:camp" { person; };`))
	wantFault(t, err, "E_OCCUPANT", "occupants", "exceeded")
	unchanged(t, w, before, history)

	// A record from a checkpoint is checked like a declared one.
	for _, tc := range []struct {
		name  string
		edit  func(*core.Occupant)
		field string
	}{
		{"count and range", func(o *core.Occupant) { o.Kind, o.Range = core.OccupantCreatures, &core.CountRange{Min: 1, Max: 2} }, "count"},
		{"uncounted with a tally", func(o *core.Occupant) {
			o.Kind, o.Count, o.Tally = core.OccupantCreatures, nil, []core.OccupantTally{{Outcome: core.OutcomeFled, Count: 1}}
		}, "tally"},
		{"person range", func(o *core.Occupant) { o.Range = &core.CountRange{Min: 1, Max: 2} }, "range"},
		{"person of two", func(o *core.Occupant) { o.Count = count(2) }, "count"},
		{"person without count", func(o *core.Occupant) { o.Count = nil }, "count"},
		{"person gone without a tally", func(o *core.Occupant) { o.Count = count(0) }, "count"},
		{"person here after leaving", func(o *core.Occupant) { o.Tally = []core.OccupantTally{{Outcome: core.OutcomeDied, Count: 1}} }, "count"},
		{"negative group", func(o *core.Occupant) {
			o.Kind, o.Count, o.Tally = core.OccupantCreatures, count(-1), []core.OccupantTally{{Outcome: core.OutcomeFled, Count: 3}}
		}, "count"},
		{"unknown outcome", func(o *core.Occupant) { o.Count, o.Tally = count(0), []core.OccupantTally{{Outcome: "won", Count: 1}} }, "outcome"},
		{"departed without a reason", func(o *core.Occupant) {
			o.Count, o.Tally = count(0), []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 1}}
		}, "reason"},
		{"blank reason", func(o *core.Occupant) {
			o.Count, o.Tally = count(0), []core.OccupantTally{{Outcome: core.OutcomeDied, Count: 1, Reason: " "}}
		}, "reason"},
		{"one entry twice", func(o *core.Occupant) {
			o.Kind, o.Tally = core.OccupantCreatures, []core.OccupantTally{{Outcome: core.OutcomeFled, Count: 1, Reason: "fire"}, {Outcome: core.OutcomeFled, Count: 1, Reason: "fire"}}
		}, "tally"},
		{"unknown attitude", func(o *core.Occupant) { o.Attitude = "" }, "attitude"},
		{"unknown kind", func(o *core.Occupant) { o.Kind = "ghost" }, "kind"},
		{"blank ID", func(o *core.Occupant) { o.ID = " " }, "id"},
	} {
		b := occupantFixture(t, "")
		tc.edit(&b.Occupants[2])
		_, err := core.NewWorld(b)
		t.Run(tc.name, func(t *testing.T) { wantFault(t, err, "E_OCCUPANT", tc.field, "") })
	}
	b = occupantFixture(t, "")
	b.Occupants[2].Location = "loc:nowhere"
	_, err = core.NewWorld(b)
	wantFault(t, err, "E_REFERENCE", "reference", "")
	b = occupantFixture(t, "")
	b.Occupants[1].ID = b.Occupants[0].ID
	_, err = core.NewWorld(b)
	wantFault(t, err, "E_DUPLICATE", "occupant", "")
	b = occupantFixture(t, "")
	b.Occupants[0].ID = "char:ada"
	_, err = core.NewWorld(b)
	wantFault(t, err, "E_DUPLICATE", "occupant", "")
}

// occModel is the occupant rules written out again, independently, to check
// the engine against random requests.
type occModel struct {
	name, loc, kind, attitude string
	count                     int64
	counted, ranged           bool
	min, max                  int64
	aliases                   []string
	tally                     map[occEntry]int64
}

// occEntry is one tally entry's key: how they left, and why.
type occEntry struct{ outcome, reason string }

func (m occModel) copy() occModel {
	m.aliases = append([]string(nil), m.aliases...)
	tally := map[occEntry]int64{}
	for k, v := range m.tally {
		tally[k] = v
	}
	m.tally = tally
	return m
}

func (m occModel) present() bool { return !m.counted || m.count > 0 }

// entries lists the tally's keys in a fixed order.
func (m occModel) entries() []occEntry {
	var out []occEntry
	for k := range m.tally {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].outcome < out[j].outcome || out[i].outcome == out[j].outcome && out[i].reason < out[j].reason
	})
	return out
}

func (m occModel) record(id string) core.Occupant {
	o := core.Occupant{ID: core.OccupantID(id), Name: m.name, Location: core.LocationID(m.loc), Kind: core.OccupantKind(m.kind), Attitude: core.Attitude(m.attitude)}
	if m.counted {
		o.Count = count(m.count)
	} else if m.ranged {
		o.Range = &core.CountRange{Min: m.min, Max: m.max}
	}
	if len(m.aliases) > 0 {
		o.Aliases = append([]string(nil), m.aliases...)
		sort.Strings(o.Aliases)
	}
	for _, k := range []string{"defeated", "departed", "died", "fled", "joined", "surrendered"} {
		for _, r := range []string{"", "fire", "gold"} {
			if n := m.tally[occEntry{k, r}]; n > 0 {
				o.Tally = append(o.Tally, core.OccupantTally{Outcome: core.Outcome(k), Count: n, Reason: r})
			}
		}
	}
	return o
}

// badModelReason is a reason the engine refuses, given or missing.
func badModelReason(outcome, reason string) bool {
	return reason == "" && outcome == "departed" || reason == " " || len(reason) > core.MaxReasonBytes
}

func TestOccupantsMatchModel(t *testing.T) {
	places := []string{"loc:camp", "loc:ford", "loc:mill", "loc:gate", "loc:keep", "loc:isle"}
	attitudes := []string{"friendly", "indifferent", "hostile"}
	outcomes := []string{"defeated", "fled", "surrendered", "departed", "joined", "died", "won"}
	names := []string{"Ann", "Bo", "Cy", "Di", "Ed", "Fay", "Gus", "Hal", "Ivy", "Jo", "Kit", "Lu", "Mo", "Ned", "Oz", "Pip", "Quin", "Ray", "Sue", "Tom"}
	rng := rand.New(rand.NewSource(41))
	// Valid reasons sort as listed; a blank and an overlong one are refused.
	reasons := []string{"", "", "fire", "gold", " ", strings.Repeat("r", core.MaxReasonBytes+1)}
	reason := func() string {
		if rng.Intn(10) == 0 {
			return reasons[4+rng.Intn(2)]
		}
		return reasons[rng.Intn(4)]
	}
	accepted, refused, hazards, applied := 0, map[string]int{}, 0, map[string]int{}
	for round := 0; round < 200; round++ {
		b := occupantFixture(t, ` visited "loc:ford"; visited "loc:mill";`+"\n")
		b.Occupants = nil
		w := world(t, b)
		model := map[string]occModel{}
		for step := 0; step < 60; step++ {
			next := map[string]occModel{}
			for k, v := range model {
				next[k] = v.copy()
			}
			var ops []core.Operation
			code, field, actual := "", "", ""
			refuse := func(c, f string) {
				if code == "" {
					code, field = c, f
				}
			}
			// refuseReturn also says why a return is refused.
			refuseReturn := func(a string) {
				if code == "" {
					code, field, actual = "E_OCCUPANT", "return", a
				}
			}
			var verbs []string
			for k := 0; k < 1+rng.Intn(3); k++ {
				// Mostly an occupant that exists, or a new one to create,
				// sometimes one that does not exist.
				verb := rng.Intn(7)
				id := "occ:none"
				if ids := keysOf(next); verb == 0 && rng.Intn(4) > 0 || len(ids) == 0 {
					id = fmt.Sprintf("occ:%d", len(ids)+rng.Intn(2))
				} else if rng.Intn(20) > 0 {
					id = ids[rng.Intn(len(ids))]
				}
				if verb == 6 && rng.Intn(4) > 0 {
					// A return mostly names an occupant with a tally.
					var left []string
					for _, k := range keysOf(next) {
						if len(next[k].tally) > 0 {
							left = append(left, k)
						}
					}
					if len(left) > 0 {
						id = left[rng.Intn(len(left))]
					}
				}
				m, known := next[id]
				op := core.OccupantOp{}
				switch verb {
				case 0:
					o := occModel{name: names[rng.Intn(len(names))], loc: places[rng.Intn(len(places))], attitude: attitudes[rng.Intn(3)], tally: map[occEntry]int64{}}
					switch rng.Intn(4) {
					case 0:
						o.kind, o.counted, o.count = "person", true, 1
					case 1:
						o.kind, o.counted, o.count = "creatures", true, int64(1+rng.Intn(4))
					case 2:
						o.kind, o.min, o.max = "creatures", 1, core.MaxOccupantCount
					default:
						o.kind, o.ranged, o.min = "creatures", true, int64(1+rng.Intn(3))
						o.max = o.min + int64(1+rng.Intn(3))
					}
					r := o.record(id)
					op.Create = &r
					if known {
						refuse("E_DUPLICATE", "occupant")
					}
					next[id] = o
				case 1:
					to := places[rng.Intn(len(places))]
					op.Move = &core.MoveOccupant{Occupant: core.OccupantID(id), To: core.LocationID(to)}
					switch {
					case !known:
						refuse("E_OCCUPANT", "occupant")
					case !m.present():
						refuse("E_OCCUPANT", "move")
					case m.loc == to:
						refuse("E_NO_CHANGE", "operation")
					}
					m.loc = to
				case 2:
					n := int64(rng.Intn(7))
					op.Count = &core.CountOccupant{Occupant: core.OccupantID(id), Count: n}
					switch {
					case !known:
						refuse("E_OCCUPANT", "occupant")
					case m.kind != "creatures" || m.counted || n < m.min || n > m.max:
						refuse("E_OCCUPANT", "count")
					}
					m.counted, m.count = true, n
				case 3:
					outcome, why := outcomes[rng.Intn(len(outcomes))], reason()
					v := &core.ResolveOccupant{Occupant: core.OccupantID(id), Outcome: core.Outcome(outcome), Reason: why}
					n := m.count
					if rng.Intn(2) == 0 {
						n = int64(rng.Intn(5))
						v.Amount = count(n)
					}
					op.Resolve = v
					switch {
					case !known:
						refuse("E_OCCUPANT", "occupant")
					case outcome == "won":
						refuse("E_OCCUPANT", "outcome")
					case v.Amount != nil && n <= 0:
						refuse("E_AMOUNT", "amount")
					case badModelReason(outcome, why):
						refuse("E_OCCUPANT", "reason")
					case !m.counted || m.count == 0 || n > m.count:
						refuse("E_OCCUPANT", "resolve")
					}
					if known {
						m.count -= n
						m.tally[occEntry{outcome, why}] += n
					}
				case 4:
					a := attitudes[rng.Intn(3)]
					op.Attitude = &core.SetAttitude{Occupant: core.OccupantID(id), Attitude: core.Attitude(a)}
					switch {
					case !known:
						refuse("E_OCCUPANT", "occupant")
					case m.attitude == a:
						refuse("E_NO_CHANGE", "operation")
					}
					m.attitude = a
				case 6:
					outcome, why := outcomes[rng.Intn(len(outcomes))], reason()
					if entries := m.entries(); len(entries) > 0 && rng.Intn(4) > 0 {
						// Mostly an outcome the occupant has, by the entry's
						// reason or by none.
						e := entries[rng.Intn(len(entries))]
						outcome = e.outcome
						if rng.Intn(2) == 0 {
							why = e.reason
						}
					}
					v := &core.ReturnOccupant{Occupant: core.OccupantID(id), Outcome: core.Outcome(outcome), Reason: why}
					if rng.Intn(2) == 0 {
						v.Amount = count(int64(rng.Intn(4)))
					}
					op.Return = v
					// The entry: the one with this reason; without a reason the
					// outcome's entry with none, or its only one.
					entry, found, others := occEntry{outcome, why}, false, 0
					if m.tally[entry] > 0 {
						found = true
					} else if why == "" {
						for _, k := range m.entries() {
							if k.outcome == outcome {
								entry, others = k, others+1
							}
						}
						found = others == 1
					}
					n := m.tally[entry]
					if v.Amount != nil {
						n = *v.Amount
					}
					switch {
					case !known:
						refuse("E_OCCUPANT", "occupant")
					case outcome == "won":
						refuse("E_OCCUPANT", "outcome")
					case v.Amount != nil && n <= 0:
						refuse("E_AMOUNT", "amount")
					case why != "" && badModelReason(outcome, why):
						refuse("E_OCCUPANT", "reason")
					case !found && others > 1:
						refuseReturn("ambiguous")
					case !found:
						refuseReturn("none")
					case n > m.tally[entry]:
						refuseReturn(fmt.Sprint(n))
					}
					if known && found {
						m.count += n
						if m.tally[entry] -= n; m.tally[entry] <= 0 {
							delete(m.tally, entry)
						}
					}
				default:
					name := names[rng.Intn(len(names))]
					op.Rename = &core.RenameOccupant{Occupant: core.OccupantID(id), Name: name}
					if !known {
						refuse("E_OCCUPANT", "occupant")
						break
					}
					aliases := []string{m.name}
					for _, a := range m.aliases {
						if a != name {
							aliases = append(aliases, a)
						}
					}
					switch {
					case m.name == name:
						refuse("E_NO_CHANGE", "operation")
					case len(aliases) > core.MaxOccupantAliases:
						refuse("E_OCCUPANT", "aliases")
					}
					m.name, m.aliases = name, aliases
				}
				if known && op.Create == nil {
					next[id] = m
				}
				ops = append(ops, core.Operation{Occupant: &op})
				verbs = append(verbs, []string{"create", "move", "count", "resolve", "attitude", "rename", "return"}[verb])
			}
			s := w.Snapshot()
			before, history := s, w.History()
			_, err := w.Execute(core.Request{ID: fmt.Sprintf("r%d", step), ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:ada"}, Operations: ops})
			if code != "" {
				wantFault(t, err, code, field, actual)
				unchanged(t, w, before, history)
				if actual == "none" || actual == "ambiguous" {
					field += " " + actual
				}
				refused[code+" "+field]++
			} else {
				if err != nil {
					t.Fatalf("round %d step %d: %v for %+v", round, step, err, ops)
				}
				model = next
				accepted++
				for _, v := range verbs {
					applied[v]++
				}
			}
			want := []core.Occupant{}
			for _, id := range keysOf(model) {
				want = append(want, model[id].record(id))
			}
			if got := w.Snapshot().Occupants; !reflect.DeepEqual(append([]core.Occupant{}, got...), want) {
				t.Fatalf("round %d step %d: occupants\n%+v\nwant\n%+v", round, step, got, want)
			}
			// Hostile, present occupants are hazards in the map view and the
			// location view; nothing else is, here.
			hostile := map[string]bool{}
			for _, m := range model {
				if m.attitude == "hostile" && m.present() {
					hostile[m.loc] = true
				}
			}
			view, err := w.MapView("char:ada")
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range view.Exits {
				if x.Hazard != hostile[string(x.To.ID)] {
					t.Fatalf("round %d step %d: exit %s hazard %v", round, step, x.To.ID, x.Hazard)
				}
			}
			all, err := w.LocationsView(locs(places...))
			if err != nil || len(all) != len(places) {
				t.Fatalf("round %d step %d: views %v", round, step, err)
			}
			for k, at := range places {
				p, err := w.LocationView(core.LocationID(at))
				if err != nil || *p.Hazard != hostile[at] || !reflect.DeepEqual(all[k], p) {
					t.Fatalf("round %d step %d: %s %+v %v", round, step, at, p, err)
				}
				if hostile[at] {
					hazards++
				}
				here := 0
				for _, m := range model {
					if m.loc == at {
						here++
					}
				}
				for i, o := range append(append([]core.Occupant{}, p.Present...), p.Resolved...) {
					m := model[string(o.ID)]
					if m.loc != at || m.present() != (i < len(p.Present)) {
						t.Fatalf("round %d step %d: %s listed at %s", round, step, o.ID, at)
					}
				}
				if len(p.Present)+len(p.Resolved) != here {
					t.Fatalf("round %d step %d: %d listed at %s, want %d", round, step, len(p.Present)+len(p.Resolved), at, here)
				}
			}
		}
		costReplay(t, b, w)
	}
	t.Logf("%d accepted, applying %v; %d hazard checks; refused %v", accepted, applied, hazards, refused)
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestOccupantExample(t *testing.T) {
	src, err := os.ReadFile("../examples/occupants/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	fight, err := os.ReadFile("../examples/occupants/fight.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("world.nql", string(src))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	mill := core.LocationID("loc:valley/B03")
	p, err := w.LocationView(mill)
	if err != nil || len(p.Present) != 1 || p.Present[0].Range == nil || !*p.Hazard || !*p.Visited {
		t.Fatalf("mill %+v %v", p, err)
	}

	// The goblins stop the trip to the watchtower at the mill.
	r := execute(t, w, "go", `travel party to "loc:valley/B04";`)
	if got, want := trip(t, r), (core.Trip{From: "loc:valley/B01", Stops: locs("loc:valley/B02", "loc:valley/B03"), Ticks: 600, Halted: true}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	r = execute(t, w, "fight", string(fight))
	wantKinds(t, r, core.OccupantCounted, core.OccupantResolved, core.OccupantResolved)
	p, _ = w.LocationView(mill)
	tally := []core.OccupantTally{{Outcome: core.OutcomeDefeated, Count: 2}, {Outcome: core.OutcomeFled, Count: 1}}
	if len(p.Present) != 0 || len(p.Resolved) != 1 || !reflect.DeepEqual(p.Resolved[0].Tally, tally) || *p.Hazard || len(p.Characters) != 2 {
		t.Fatalf("mill after the fight %+v", p)
	}
	r = execute(t, w, "on", `travel party to "loc:valley/B04";`)
	if got, want := trip(t, r), (core.Trip{From: "loc:valley/B03", Stops: locs("loc:valley/B04"), Ticks: 600, Visited: locs("loc:valley/B04")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("trip %+v, want %+v", got, want)
	}
	execute(t, w, "mira", `move occupant "occ:valley/B02/mira" to "loc:valley/B04";`)
	if p, _ = w.LocationView("loc:valley/B04"); len(p.Present) != 1 || p.Present[0].Name != "Ranger Mira" || w.Snapshot().Clock.Tick != 30000 {
		t.Fatalf("watchtower %+v", p)
	}

	// Why she left, and coming back (docs/LOCATIONS.md).
	execute(t, w, "away", `resolve occupant "occ:valley/B02/mira" as departed because "rode north to fetch the healer";`)
	if got := occupant(t, w, "occ:valley/B02/mira"); *got.Count != 0 || !reflect.DeepEqual(got.Tally, []core.OccupantTally{{Outcome: core.OutcomeDeparted, Count: 1, Reason: "rode north to fetch the healer"}}) {
		t.Fatalf("mira away %+v", got)
	}
	r = execute(t, w, "back", `return occupant "occ:valley/B02/mira" from departed;`)
	wantKinds(t, r, core.OccupantReturned)
	if p, _ = w.LocationView("loc:valley/B04"); len(p.Present) != 1 || p.Present[0].Tally != nil || *p.Present[0].Count != 1 {
		t.Fatalf("watchtower %+v", p)
	}
	costReplay(t, b, w)
}
