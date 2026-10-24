package tests

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"os"
	"reflect"
	"testing"
)

func nativeFixture(t testing.TB, path string) core.Blueprint {
	t.Helper()
	s, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	b, d := compiler.CompileWorld(path, string(s))
	if len(d) != 0 {
		t.Fatal(d)
	}
	return b
}
func nativeRequest(w *core.World, n int, actor, ability, target, itemID, giver string) core.Request {
	char := core.CharacterID(actor)
	v := core.InvokeAbility{Ability: core.AbilityID(ability), Arguments: []core.AbilityArgument{{Name: "actor", Value: core.AbilityValue{Character: &char}}}}
	for _, a := range []struct{ name, value string }{{"target", target}, {"giver", giver}} {
		if a.value != "" {
			id := core.CharacterID(a.value)
			v.Arguments = append(v.Arguments, core.AbilityArgument{Name: core.ParameterID(a.name), Value: core.AbilityValue{Character: &id}})
		}
	}
	if itemID != "" {
		id := core.ItemID(itemID)
		v.Arguments = append(v.Arguments, core.AbilityArgument{Name: "item", Value: core.AbilityValue{Item: &id}})
	}
	for _, d := range w.Snapshot().AbilityDefinitions {
		if d.ID == v.Ability {
			for _, p := range d.Parameters {
				if p.Kind == core.ConditionIDArgument {
					id := core.ConditionInstanceID(fmt.Sprintf("condition:native:%d:%s", n, p.ID))
					v.Arguments = append(v.Arguments, core.AbilityArgument{Name: p.ID, Value: core.AbilityValue{ConditionID: &id}})
				}
			}
		}
	}
	return core.Request{ID: fmt.Sprintf("native:request:%d", n), ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: actor}, Operations: []core.Operation{{Invoke: &v}}}
}
func TestAlphaAuthoredVariations(t *testing.T) {
	for _, path := range []string{"../examples/alpha/adventure.nql", "../examples/alpha/variations/strong-scout.nql"} {
		t.Run(path, func(t *testing.T) {
			w := world(t, nativeFixture(t, path))
			commitC(t, w, nativeRequest(w, 1, "char:orin", "ability:to-path", "", "", ""))
			r := commitC(t, w, nativeRequest(w, 2, "char:orin", "ability:focused-strike", "char:scout", "", ""))
			want := int64(4)
			if path != "../examples/alpha/adventure.nql" {
				want = 8
			}
			p, _ := w.CharacterStatus("char:scout")
			a, _ := w.CharacterStatus("char:orin")
			if p.Resources["hp"].Current != want || a.Resources["focus"].Current != 4 || r.Events[1].Check.Draw != 16 {
				t.Fatal(p, a, r)
			}
			found := false
			for _, p := range w.Snapshot().Adventure.State.Encounters {
				if p.ID == "encounter:path" {
					found = true
					if p.Status != core.Active || p.Cursor != 1 || p.Round != 1 {
						t.Fatal(p)
					}
				}
			}
			if !found {
				t.Fatal("missing path encounter")
			}
		})
	}
	for _, path := range []string{"../examples/alpha/adventure.nql", "../examples/alpha/variations/keeps-worn.nql"} {
		t.Run(path, func(t *testing.T) {
			w := world(t, nativeFixture(t, path))
			r := nativeRequest(w, 1, "char:orin", "ability:handoff", "char:tess", "item:shield", "char:orin")
			if path != "../examples/alpha/adventure.nql" {
				costReject(t, w, r, "E_WORN")
				stat(t, w, "char:lyra", "defense", 13)
				stat(t, w, "char:tess", "defense", 9)
				return
			}
			out := commitC(t, w, r)
			want := []core.EventKind{core.ItemUnequipped, core.ItemTransferred, core.ItemEquipped, core.AbilityInvoked}
			if len(out.Events) != len(want) {
				t.Fatal(out)
			}
			for i, k := range want {
				if out.Events[i].Kind != k {
					t.Fatal(out)
				}
			}
			stat(t, w, "char:lyra", "defense", 11)
			stat(t, w, "char:tess", "defense", 11)
		})
	}
}
func nativeCurrent(w *core.World) core.CharacterID {
	s := w.Snapshot()
	for _, p := range s.Adventure.State.Encounters {
		if p.Status == core.Active {
			for _, d := range s.Adventure.Definition.Encounters {
				if d.ID == p.ID {
					return d.Order[p.Cursor].Character
				}
			}
		}
	}
	return ""
}
func TestAlphaTimedPaidReceiptAfterNativeReconstruction(t *testing.T) {
	w := world(t, nativeFixture(t, "../examples/alpha/adventure.nql"))
	n := 1
	commit := func(actor, ability, target string) core.Batch {
		r := nativeRequest(w, n, actor, ability, target, "", "")
		n++
		return commitC(t, w, r)
	}
	commit("char:orin", "ability:to-path", "")
	commit("char:orin", "ability:pass", "")
	first := commit("char:lyra", "ability:steady-aid", "char:orin")
	// Opponent Pass is an ordinary granted Ability here. This API continuation
	// isolates retention/expiry, not the play host's automatic opponent strategy.
	for count := 0; count < 40; count++ {
		p, _ := w.CharacterStatus("char:lyra")
		if p.Resources["focus"].Current == 0 && len(w.Snapshot().Conditions) == 0 {
			break
		}
		actor := string(nativeCurrent(w))
		if actor == "char:lyra" && p.Resources["focus"].Current > 0 {
			commit(actor, "ability:guard", "")
		} else {
			commit(actor, "ability:pass", "")
		}
	}
	p, _ := w.CharacterStatus("char:lyra")
	if p.Resources["focus"].Current != 0 || len(w.Snapshot().Conditions) != 0 || len(w.Snapshot().RetiredConditionIDs) != 8 {
		t.Fatal(p, w.Snapshot().Conditions, w.Snapshot().RetiredConditionIDs)
	}
	before := w.ExportCheckpoint()
	var data bytes.Buffer
	session := nativesave.SessionState{Campaign: "native", Next: uint64(n), LastAttempt: &nativesave.AttemptRef{RequestID: fmt.Sprintf("native:request:%d", n-1), Committed: true}}
	if _, e := nativesave.Write(&data, before, session); e != nil {
		t.Fatal(e)
	}
	loaded, e := nativesave.Read(bytes.NewReader(data.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) || !reflect.DeepEqual(session, loaded.Session) {
		t.Fatal("native continuation mismatch")
	}
	retry := commitC(t, loaded.World, first.Request)
	if !reflect.DeepEqual(first, retry) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("historical aid recharged/reapplied/advanced")
	}
}
func TestAlphaNative4096CompleteReceipts(t *testing.T) {
	w := world(t, nativeFixture(t, "../examples/alpha/adventure.nql"))
	var first core.Batch
	for n := 1; n <= 4096; n++ {
		b := commitC(t, w, nativeRequest(w, n, "char:orin", "ability:rest", "char:orin", "", ""))
		if n == 1 {
			first = b
		}
	}
	before := w.ExportCheckpoint()
	if before.Head != 4096 || before.State.Adventure.State.Random.Calls != 0 {
		t.Fatal("unexpected campaign head/random")
	}
	var data bytes.Buffer
	info, e := nativesave.Write(&data, before, nativesave.SessionState{Campaign: "native", Next: 4097, LastAttempt: &nativesave.AttemptRef{RequestID: "native:request:4096", Committed: true}})
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := nativesave.Read(bytes.NewReader(data.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) || !reflect.DeepEqual(first, commitC(t, loaded.World, first.Request)) {
		t.Fatal("complete Alpha history or earliest receipt lost")
	}
	t.Logf("actual native Alpha archive: %d requests, %d bytes, head%d", len(before.History), info.Bytes, info.Head)
	dir := t.TempDir()
	if root := os.Getenv("NQL_NATIVE_TEST_ROOT"); root != "" {
		dir, e = os.MkdirTemp(root, "alpha-capacity-")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if e := os.RemoveAll(dir); e != nil {
				t.Error(e)
			}
		})
	}
	store, e := nativesave.Acquire(dir, "native", true)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := store.Save(before, loaded.Session)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	store, e = nativesave.Acquire(dir, "native", false)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	fromDisk, e := store.Load()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, fromDisk.World.ExportCheckpoint()) || !reflect.DeepEqual(loaded.Session, fromDisk.Session) || !reflect.DeepEqual(first, commitC(t, fromDisk.World, first.Request)) || !reflect.DeepEqual(before, fromDisk.World.ExportCheckpoint()) {
		t.Fatal("filesystem save/reopen lost full history, host allocation or early receipt")
	}
	t.Logf("actual Store save/close/reacquire/load: root=%s generation=%s bytes=%d; full checkpoint/early retry PASS", dir, saved.Generation, saved.Bytes)
}

func TestAlphaFieldCareComposesWithExistingGuard(t *testing.T) {
	b := nativeFixture(t, "../examples/alpha/adventure.nql")
	for i := range b.Characters {
		if b.Characters[i].ID == "char:orin" {
			b.Characters[i].Resources["hp"] = core.Resource{Current: 12, Minimum: 0, Maximum: 16}
		}
	}
	w := world(t, b)
	commitC(t, w, nativeRequest(w, 1, "char:orin", "ability:to-path", "", "", ""))
	commitC(t, w, nativeRequest(w, 2, "char:orin", "ability:guard", "", "", ""))
	before := w.ExportCheckpoint()
	costReject(t, w, nativeRequest(w, 3, "char:lyra", "ability:steady-aid", "char:orin", "", ""), "E_ELIGIBILITY")
	r := commitC(t, w, nativeRequest(w, 3, "char:lyra", "ability:field-care", "char:orin", "", ""))
	after := w.ExportCheckpoint()
	if !reflect.DeepEqual(before.State.Conditions, after.State.Conditions) || !reflect.DeepEqual(before.State.ConditionEffects, after.State.ConditionEffects) || !reflect.DeepEqual(before.State.Adventure.State.Random, after.State.Adventure.State.Random) {
		t.Fatal("heal-only support changed guard provenance or RNG")
	}
	if len(r.Events) != 4 || r.Events[0].Kind != core.ResourceSpent || r.Events[1].Kind != core.HealingApplied || r.Events[1].Resource.Before.Current != 12 || r.Events[1].Resource.After.Current != 16 || r.Events[2].Kind != core.TurnAdvanced || r.Events[3].Kind != core.AbilityInvoked {
		t.Fatal("authored FieldCare consequence oracle", r)
	}
	p, _ := w.CharacterStatus("char:lyra")
	if p.Resources["focus"].Current != 7 {
		t.Fatal("FieldCare did not pay exactly one Focus")
	}
	stat(t, w, "char:orin", "defense", 13)
}
