package tests

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"reflect"
	"testing"
)

func alphaLoadout(t testing.TB, policy core.TransferPolicy) core.Blueprint {
	t.Helper()
	src, err := os.ReadFile("../examples/ability-world.nql")
	if err != nil {
		t.Fatal(err)
	}
	b, ds := compiler.CompileWorld("alpha-loadout.nql", string(src)+`
ability "loadout" named "HandOff and Equip" {
 parameter "item" item;
 parameter "giver" character;
 parameter "recipient" character;
 transfer param "item" from param "giver" to param "recipient";
 equip param "item" on param "recipient";
}
ability "change" named "Change Wearer" {
 parameter "item" item;
 parameter "wearer" character;
 unequip param "item";
 equip param "item" on param "wearer";
}
`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	b.Rules.Transfer = policy
	return b
}
func loadout() core.Operation {
	o := handoff("item:shield", "char:bea", "char:cy")
	o.Invoke.Ability = "loadout"
	return o
}
func TestAlphaLoadoutAtomicPoliciesAndHistory(t *testing.T) {
	for _, policy := range []core.TransferPolicy{core.Unequips, core.KeepsWorn} {
		t.Run(string(policy), func(t *testing.T) {
			b := alphaLoadout(t, policy)
			w := world(t, b)
			r := typedRequest(w, "load", loadout())
			if policy == core.KeepsWorn {
				costReject(t, w, r, "E_WORN")
				if item(t, w, "item:shield").Owner != "char:bea" {
					t.Fatal("private transfer escaped")
				}
				stat(t, w, "char:ada", "defense", 12)
				stat(t, w, "char:cy", "defense", 9)
				execute(t, w, "explicit-off", `unequip "item:shield";`)
				r.ExpectedRevision = w.Revision()
			}
			first := commitC(t, w, r)
			stat(t, w, "char:ada", "defense", 10)
			stat(t, w, "char:cy", "defense", 11)
			shield := item(t, w, "item:shield")
			if shield.Owner != "char:cy" || shield.Wearer != "char:cy" {
				t.Fatal(shield)
			}
			want := []core.EventKind{core.ItemUnequipped, core.ItemTransferred, core.ItemEquipped, core.AbilityInvoked}
			if policy == core.KeepsWorn {
				want = want[1:]
			}
			for i, k := range want {
				if first.Events[i].Kind != k {
					t.Fatal(first)
				}
			}
			execute(t, w, "off", `unequip "item:shield";`)
			before := w.Snapshot()
			if !reflect.DeepEqual(first, commitC(t, w, r)) || !reflect.DeepEqual(before, w.Snapshot()) {
				t.Fatal("historical retry equipped again")
			}
			costReplay(t, b, w)
		})
	}
}
func TestAlphaLoadoutLateFailureAndExplicitWearer(t *testing.T) {
	b := alphaLoadout(t, core.Unequips)
	w := world(t, b)
	r := typedRequest(w, "late", loadout(), core.Operation{Transfer: &core.Transfer{Item: "item:parcel", From: "char:bea", To: "char:cy"}})
	costReject(t, w, r, "E_OWNER")
	stat(t, w, "char:ada", "defense", 12)
	stat(t, w, "char:cy", "defense", 9)
	r.Operations[1].Transfer.From = "char:ada"
	commitC(t, w, r)
	execute(t, w, "change", `invoke "change" { "item" = item "item:shield"; "wearer" = character "char:ada"; };`)
	stat(t, w, "char:ada", "defense", 12)
	stat(t, w, "char:cy", "defense", 9)
	if item(t, w, "item:shield").Owner != "char:cy" {
		t.Fatal("wearing changed ownership")
	}
	costReplay(t, b, w)
}
