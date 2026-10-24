package core_test

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestBufferAbsorbedDamageStillRunsConditionChecks(t *testing.T) {
	b := conditionCheckFixture()
	b.Characters[0].Resources["temp"] = core.Resource{Current: 30, Maximum: 30}
	b.Characters[0].Buffers = []core.ResourceBuffer{{Pool: "temp", Target: "hp"}}
	w := gearWorld(t, b)
	before := w.ExportCheckpoint()
	need, e := w.BattleInput(w.Revision(), core.Ref{Kind: core.CharacterKind, ID: "b"}, conditionDamage("a", 22))
	if e != nil || need.Stage != "condition" || need.Condition.Damage != 22 || need.Condition.Threshold != 11 || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal(need, e)
	}
	r := battleExec(t, w, "hit", "b", conditionDamage("a", 22, 9))
	last := r.Events[len(r.Events)-1]
	if r.Events[0].Kind != core.ConditionCheckResolved || !r.Events[0].ConditionCheck.Roll.Success || last.Resource.Before.Current != 200 || last.Resource.After.Current != 200 || last.Resource.Buffer.Absorbed != 22 || last.Resource.Buffer.After.Current != 8 {
		t.Fatal(r)
	}
	rebuilt, e := core.Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
		t.Fatal("check replay", e)
	}
}
func TestBufferWeaponDamageUsesSharedEvaluator(t *testing.T) {
	b := hitConditionFixture()
	b.Characters[1].Resources["temp"] = core.Resource{Current: 3, Maximum: 10}
	b.Characters[1].Buffers = []core.ResourceBuffer{{Pool: "temp", Target: "hp"}}
	w := gearWorld(t, b)
	battleExec(t, w, "begin", "a", battleBegin())
	r := battleExec(t, w, "hit", "a", hitConditionAttack(t, w, "sap", true))
	v := r.Events[0].Resource
	if v.Requested != 4 || v.Buffer.Absorbed != 3 || v.After.Current != 29 || v.Buffer.After.Current != 0 || r.Events[len(r.Events)-1].WeaponAttack.Roll.Actual != "1" {
		t.Fatal(r)
	}
	rebuilt, e := core.Replay(b, w.History())
	if e != nil || !reflect.DeepEqual(w.ExportCheckpoint(), rebuilt.ExportCheckpoint()) {
		t.Fatal("weapon replay", e)
	}
}
func TestBufferCannotRepurposeBattleHealthOrTurnPools(t *testing.T) {
	for _, pool := range []core.ResourceID{"hp", "focus"} {
		b := battleAbilityFixture()
		b.Characters[0].Resources["target"] = core.Resource{Current: 10, Maximum: 10}
		b.Characters[0].Buffers = []core.ResourceBuffer{{Pool: pool, Target: "target"}}
		if pool == "focus" {
			b.Battle.Rules.TurnResources = []core.ResourceID{"focus"}
		}
		if _, e := core.NewWorld(b); e == nil {
			t.Fatal("reserved pool accepted", pool)
		}
	}
}
func TestBufferAllowanceTargetRejectedAtGenesisAndCreation(t *testing.T) {
	for _, mode := range []string{"genesis", "creation"} {
		t.Run(mode, func(t *testing.T) {
			b := extraActionFixture()
			b.Characters[0].Resources["temp"] = core.Resource{Current: 5, Maximum: 10}
			if mode == "genesis" {
				b.Characters[0].Buffers = []core.ResourceBuffer{{Pool: "temp", Target: "extra"}}
				if _, e := core.NewWorld(b); e == nil {
					t.Fatal("allowance accepted as buffer target")
				}
				return
			}
			w := gearWorld(t, b)
			c := b.Characters[0]
			c.ID = "new"
			c.Buffers = []core.ResourceBuffer{{Pool: "temp", Target: "extra"}}
			before := w.ExportCheckpoint()
			_, e := w.Execute(gearRequest(w, "birth", core.Operation{CreateCharacter: &c}))
			if e == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("buffered allowance birth accepted", e)
			}
		})
	}
}
