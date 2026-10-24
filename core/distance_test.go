package core_test

import (
	"reflect"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func rangedCareFixture() core.Blueprint {
	b := battleAbilityFixture()
	target := core.AbilityOperand{Parameter: "target"}
	b.AbilityDefinitions[0].Action.Requirements = []core.Requirement{{Kind: core.WithinRange, Subject: core.AbilityOperand{Parameter: "actor"}, Other: &target, Distance: &core.DistanceLimit{Maximum: 5, Unit: "feet"}}}
	return b
}

func TestActionRangeUsesKnownGridNotRoom(t *testing.T) {
	for _, position := range []core.BattlePosition{{X: 1}, {X: 1, Y: -1}, {X: 2}} {
		b := rangedCareFixture()
		w := gearWorld(t, b)
		// Colocation alone cannot establish distance; failure reserves nothing.
		battleReject(t, w, gearRequest(w, "unknown", battleInvoke("care", "a", "b")))
		begin := battleBegin()
		begin.BeginBattle.Participants[1].Position = position
		battleExec(t, w, "begin", "a", begin)
		if position.X == 2 {
			battleReject(t, w, gearRequest(w, "far", battleInvoke("care", "a", "b")))
			continue
		}
		r := battleExec(t, w, "near", "a", battleInvoke("care", "a", "b"))
		if len(r.Events) != 3 || w.Snapshot().Characters[0].Resources["focus"].Current != 1 {
			t.Fatal("adjacent/diagonal action failed", r)
		}
		cp := w.ExportCheckpoint()
		if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(cp, rebuilt.ExportCheckpoint()) {
			t.Fatal("range replay", err)
		}
		b.AbilityDefinitions[0].Action.Requirements[0].Distance.Maximum = 999
		s := w.Snapshot()
		s.AbilityDefinitions[0].Action.Requirements[0].Distance.Unit = "forged"
		if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
			t.Fatal("range aliases authority")
		}
	}
	b := rangedCareFixture()
	b.AbilityDefinitions[0].Action.Requirements[0].Distance.Maximum = 0
	w := gearWorld(t, b)
	battleExec(t, w, "self", "a", battleInvoke("care", "a", "a"))
}

func TestActionRangeClosedTypedShape(t *testing.T) {
	for _, mutate := range []func(*core.Requirement){
		func(r *core.Requirement) { r.Distance.Maximum = -1 },
		func(r *core.Requirement) { r.Distance.Unit = "meters" },
		func(r *core.Requirement) { r.Distance = nil },
		func(r *core.Requirement) { r.Other = nil },
		func(r *core.Requirement) { r.Kind = core.Colocated },
		func(r *core.Requirement) { r.Resource = "hp" },
	} {
		b := rangedCareFixture()
		mutate(&b.AbilityDefinitions[0].Action.Requirements[0])
		if w, err := core.NewWorld(b); w != nil || err == nil {
			t.Fatal("invalid range definition accepted")
		}
	}
}
