package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func spanFixture(maximum int64) core.Blueprint {
	b := battleAbilityFixture()
	b.Battle.Rules.LocationSpans = []core.LocationSpan{{Location: "room", Maximum: 30}}
	actor, target := core.AbilityOperand{Parameter: "actor"}, core.AbilityOperand{Parameter: "target"}
	b.AbilityDefinitions[0].Action.Requirements = []core.Requirement{{Kind: core.WithinRange, Subject: actor, Other: &target, Distance: &core.DistanceLimit{Maximum: maximum, Unit: "feet"}}}
	b.Characters[1].Resources["hp"] = core.Resource{Current: 6, Maximum: 30}
	return b
}

func TestLocationSpanRangeProofAndExactPrecedence(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, maximum := range []int64{4, 5, 29, 30, 60} {
			b := spanFixture(maximum)
			w := gearWorld(t, b)
			if active {
				battleExec(t, w, "begin", "a", battleBegin())
			}
			pass := maximum >= 30 || active && maximum >= 5
			op := battleInvoke("care", "a", "b")
			if !pass {
				battleReject(t, w, gearRequest(w, "care", op))
				continue
			}
			r := battleExec(t, w, "care", "a", op)
			if w.Snapshot().Characters[1].Resources["hp"].Current != 10 {
				t.Fatal("fixed6+4 oracle")
			}
			rebuilt, err := core.Replay(b, w.History())
			if err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
				t.Fatal("span replay", err)
			}
			before := rebuilt.ExportCheckpoint()
			retry, err := rebuilt.Execute(r.Request)
			if err != nil || !reflect.DeepEqual(r, retry) || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
				t.Fatal("span historical retry", err)
			}
			b.Battle.Rules.LocationSpans[0].Maximum = 1
			snapshot := w.Snapshot()
			snapshot.Battle.Rules.LocationSpans[0].Maximum = 1
			if w.Snapshot().Battle.Rules.LocationSpans[0].Maximum != 30 {
				t.Fatal("span alias")
			}
		}
	}
	for _, separate := range []bool{false, true} {
		b := spanFixture(60)
		if separate {
			b.Locations = append(b.Locations, core.Location{ID: "elsewhere"})
			b.Characters[1].Location = "elsewhere"
		} else {
			b.Battle.Rules.LocationSpans = nil
		}
		w := gearWorld(t, b)
		battleReject(t, w, gearRequest(w, "unknown", battleInvoke("care", "a", "b")))
		battleExec(t, w, "self", "a", battleInvoke("care", "a", "a"))
	}
}

func TestLocationSpanFormationAdmissionAndMalformedRules(t *testing.T) {
	w := gearWorld(t, spanFixture(60))
	bad := battleBegin()
	bad.BeginBattle.Participants[1].Position.X = 7
	bad.BeginBattle.Participants[0].Initiative = nil
	before := w.ExportCheckpoint()
	if need, err := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "a"}, bad); err == nil {
		t.Fatal("contradictory35foot formation requested dice", need)
	}
	battleReject(t, w, gearRequest(w, "bad", bad))
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("formation consumed RNG or state")
	}
	for _, spans := range [][]core.LocationSpan{
		{{Location: "room", Maximum: 0}}, {{Location: "missing", Maximum: 30}},
		{{Location: "room", Maximum: 30}, {Location: "room", Maximum: 40}},
	} {
		b := spanFixture(60)
		b.Battle.Rules.LocationSpans = spans
		if _, err := core.NewWorld(b); err == nil {
			t.Fatal("invalid span admitted", spans)
		}
	}
}
