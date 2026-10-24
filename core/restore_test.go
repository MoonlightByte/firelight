package core_test

import (
	"bytes"
	"errors"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"math"
	"reflect"
	"testing"
)

func restoreOp(c core.CharacterID, resource core.ResourceID) core.Operation {
	return core.Operation{Restore: &core.RestoreResource{Character: c, Resource: resource}}
}

func TestRestoreCanonicalMaximumHistoryAndDetachment(t *testing.T) {
	b := battleAbilityFixture()
	b.Characters[0].Resources["wide"] = core.Resource{Minimum: math.MinInt64, Current: math.MinInt64, Maximum: math.MaxInt64}
	w := gearWorld(t, b)
	op := restoreOp("a", "wide")
	r := gearExecute(t, w, "restore", op, restoreOp("a", "hp"))
	if len(r.Events) != 2 || r.Events[0].Kind != core.ResourceRestored || r.Events[0].Restoration.Before.Current != math.MinInt64 || r.Events[0].Restoration.After.Current != math.MaxInt64 || r.Events[1].Restoration.After.Current != 30 {
		t.Fatal("exact restoration oracle", r)
	}
	gearExecute(t, w, "spend-after", core.Operation{Spend: &core.ResourceOp{Character: "a", Resource: "wide", Amount: 1}})
	before := w.ExportCheckpoint()
	historical, err := w.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(historical, r) || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("retry restored again", err)
	}
	var archive bytes.Buffer
	if _, err := nativesave.Write(&archive, before, nativesave.SessionState{Campaign: "restore", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&archive)
	if err != nil || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("restore archive", err)
	}
	historical, err = loaded.World.Execute(r.Request)
	if err != nil || !reflect.DeepEqual(historical, r) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("reconstructed historical retry", err)
	}
	op.Restore.Character = "b"
	r.Events[0].Restoration.After.Current = 0
	r.Request.Operations[0].Restore.Resource = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) || historical.Events[0].Restoration.After.Current != math.MaxInt64 {
		t.Fatal("restoration alias")
	}
	bad := w.History()
	bad[0].Events[0].Restoration.After.Current = 0
	if partial, err := core.Replay(b, bad); err == nil || partial != nil {
		t.Fatal("forged restoration accepted")
	}
	full := gearExecute(t, w, "already-full", restoreOp("a", "hp"))
	if full.Events[0].Restoration.Before != full.Events[0].Restoration.After {
		t.Fatal("full resource must still emit truthful zero-change result")
	}
}

func TestRestoreAbilityRollbackAndLife(t *testing.T) {
	b := battleAbilityFixture()
	actor := core.AbilityOperand{Parameter: "actor"}
	b.AbilityDefinitions[0].Steps = []core.AbilityStep{{Restore: &core.RestoreTemplate{Target: core.AbilityOperand{Parameter: "target"}, Resource: "hp"}}, {Spend: &core.SpendTemplate{Payer: actor, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: gearPtr(int64(4))}}}}}
	w := gearWorld(t, b)
	_, err := w.Execute(gearRequest(w, "late", battleInvoke("care", "a", "a")))
	var ae *core.AbilityError
	if !errors.As(err, &ae) || ae.Step != 1 || w.Revision() != 0 || !reflect.DeepEqual(w.ExportCheckpoint(), gearWorld(t, b).ExportCheckpoint()) {
		t.Fatal("late failure published restoration", err)
	}
	b.AbilityDefinitions[0].Steps = b.AbilityDefinitions[0].Steps[:1]
	b.Relationships = append(b.Relationships, core.Relationship{From: core.Ref{Kind: core.CharacterKind, ID: "b"}, To: core.Ref{Kind: core.CharacterKind, ID: "a"}, Kind: "party-member", Strength: 1})
	w = gearWorld(t, b)
	gearExecute(t, w, "down", core.Operation{Damage: &core.ResourceOp{Character: "b", Resource: "hp", Amount: 30}})
	r := gearExecute(t, w, "recover", battleInvoke("care", "a", "b"))
	if len(r.Events) != 2 || r.Events[0].Restoration.Life.Before.Outcome != core.StableDown || r.Events[0].Restoration.Life.After != nil || r.Events[0].AbilityOrigin.Step != 0 || r.Events[1].Ability.Ability != "care" {
		t.Fatal("recovery provenance", r)
	}
	// Mutating nested returned life or a definition operand cannot change either authority.
	before := w.ExportCheckpoint()
	r.Events[0].Restoration.Life.Before.Cause.Actor.ID = "forged"
	b.AbilityDefinitions[0].Steps[0].Restore.Target.Parameter = "forged"
	if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("nested restore alias")
	}
	if _, err := core.Replay(before.Genesis, w.History()); err != nil {
		t.Fatal(err)
	}
	// No resurrection: use the existing admitted opposition damage outcome.
	deadFixture := battleFixture()
	deadFixture.Characters[1].Resources["hp"] = core.Resource{Current: 10, Maximum: 10}
	w = gearWorld(t, deadFixture)
	battleExec(t, w, "begin", "a", battleBegin())
	battleExec(t, w, "dead", "a", battleAttack(t, w, "a", "b", []int64{20}, []int64{4, 5}, true))
	battleReject(t, w, gearRequest(w, "no-resurrection", restoreOp("b", "hp")))
	for _, bad := range []core.Operation{
		restoreOp("missing", "hp"), restoreOp("room", "hp"), restoreOp("a", "missing"),
		{Restore: &core.RestoreResource{Character: "a", Resource: "hp"}, Heal: &core.ResourceOp{Character: "a", Resource: "hp", Amount: 1}},
	} {
		battleReject(t, w, gearRequest(w, "invalid", bad))
	}
}
