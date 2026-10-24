package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestActionLocationUsesOrderedCanonicalFact(t *testing.T) {
	b := battleAbilityFixture()
	b.Locations = append(b.Locations, core.Location{ID: "road", Name: "Road"})
	b.AbilityDefinitions[0].Action.Requirements = append(b.AbilityDefinitions[0].Action.Requirements, core.Requirement{Kind: core.AtLocation, Subject: core.AbilityOperand{Parameter: "actor"}, Location: "road"})
	w := gearWorld(t, b)
	battleReject(t, w, gearRequest(w, "elsewhere", battleInvoke("care", "a", "a")))
	r := gearExecute(t, w, "arrive-care", core.Operation{Move: &core.Move{Character: "a", To: "road"}}, battleInvoke("care", "a", "a"))
	if w.Snapshot().Characters[0].Resources["hp"].Current != 9 {
		t.Fatal("location action")
	}
	gearExecute(t, w, "leave", core.Operation{Move: &core.Move{Character: "a", To: "room"}})
	cp := w.ExportCheckpoint()
	historical, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(historical, r) || !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
		t.Fatal("historical location retry", err)
	}
	rebuilt, err := core.Replay(b, w.History())
	if err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
		t.Fatal("location replay", err)
	}
	for _, rule := range []core.Requirement{
		{Kind: core.AtLocation, Subject: core.AbilityOperand{Parameter: "actor"}, Location: "a"},
		{Kind: core.AtLocation, Subject: core.AbilityOperand{Parameter: "actor"}},
		{Kind: core.HasCondition, Subject: core.AbilityOperand{Parameter: "actor"}, Condition: "guarded", Location: "road"},
	} {
		b.AbilityDefinitions[0].Action.Requirements[2] = rule
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("illegal location predicate accepted")
		}
	}
}
