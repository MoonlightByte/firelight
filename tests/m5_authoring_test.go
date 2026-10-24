package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// M5 is a content evaluator: all mutations and reconstruction use public APIs.
// Fixed oracles were reviewed and frozen before any author submission.
var m5IDs = []string{"ability:two-person-care", "ability:paired-conditions", "ability:ownership-exchange", "ability:aid-and-reward"}
var m5Names = []string{"iris", "rowan", "sol"}

func m5Read(t testing.TB, path string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("testdata", "m5", path))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func m5Write(t testing.TB, name string, v any) {
	t.Helper()
	dir := os.Getenv("NQL_M5_EVIDENCE")
	if dir == "" {
		return
	}
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, name+".json"), append(b, '\n'), 0644); e != nil {
		t.Fatal(e)
	}
}
func m5Load(t testing.TB, variant string, cases ...int) core.Blueprint {
	t.Helper()
	src := m5Read(t, "fixtures/"+variant+".nql")
	base, ds := compiler.CompileWorld("fixture", src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, c := range cases {
		src += "\n" + m5Read(t, fmt.Sprintf("submissions/c%02d-definition.nql", c))
	}
	b, ds := compiler.CompileWorld("submitted-world", src)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	w := world(t, b)
	clean := w.Snapshot().Blueprint
	clean.AbilityDefinitions = nil
	if !reflect.DeepEqual(clean, world(t, base).Snapshot().Blueprint) {
		t.Fatal("submission changed starter facts")
	}
	if len(b.AbilityDefinitions) != len(cases) {
		t.Fatal("extra/missing definition")
	}
	for _, c := range cases {
		m5Shape(t, b, c)
	}
	return b
}
func m5Shape(t testing.TB, b core.Blueprint, c int) {
	t.Helper()
	var d *core.AbilityDefinition
	for i := range b.AbilityDefinitions {
		if string(b.AbilityDefinitions[i].ID) == m5IDs[c-1] {
			d = &b.AbilityDefinitions[i]
		}
	}
	if d == nil {
		t.Fatal("required Ability missing")
	}
	signatures := [][]string{{"first:character", "first_amount:int", "second:character", "second_amount:int"}, {"guard_id:condition_id", "mark_id:condition_id", "target:character"}, {"left:character", "outgoing:item", "returning:item", "right:character"}, {"donor:character", "gift:item", "guard_id:condition_id", "heal_amount:int", "recipient:character"}}
	sig := []string{}
	for _, p := range d.Parameters {
		sig = append(sig, string(p.ID)+":"+string(p.Kind))
	}
	sort.Strings(sig)
	if !reflect.DeepEqual(sig, signatures[c-1]) {
		t.Fatal("signature", sig)
	}
	parameter := func(v core.AbilityOperand) string {
		if v.Literal != nil || v.Parameter == "" {
			t.Fatal("required operand must bind parameter")
		}
		return string(v.Parameter)
	}
	steps := []string{}
	for _, s := range d.Steps {
		if h := s.Heal; h != nil {
			steps = append(steps, "heal:"+parameter(h.Target)+":"+string(h.Resource)+":"+parameter(h.Amount))
		}
		if x := s.Transfer; x != nil {
			steps = append(steps, "transfer:"+parameter(x.Item)+":"+parameter(x.From)+":"+parameter(x.To))
		}
		if x := s.ApplyCondition; x != nil {
			if x.Magnitude != nil || x.Source == nil || *x.Source != (core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}) {
				t.Fatal("Condition literal source/magnitude")
			}
			steps = append(steps, "condition:"+parameter(x.Instance)+":"+string(x.Type)+":"+parameter(x.Target))
		}
	}
	expected := [][]string{{"heal:first:hp:first_amount", "heal:second:hp:second_amount"}, {"condition:guard_id:condition:guarded:target", "condition:mark_id:condition:marked:target"}, {"transfer:outgoing:left:right", "transfer:returning:right:left"}, {"heal:recipient:hp:heal_amount", "transfer:gift:donor:recipient", "condition:guard_id:condition:guarded:recipient"}}
	if !reflect.DeepEqual(steps, expected[c-1]) {
		t.Fatal("body contract", steps)
	}
}
func m5Request(t testing.TB, w *core.World, id, src string) core.Request {
	t.Helper()
	ops, ds := compiler.CompileActions("authored-action", src, w.Snapshot().Blueprint)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	return core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:rowan"}, Operations: ops}
}
func m5Run(t testing.TB, w *core.World, id, src string) core.Batch {
	t.Helper()
	return commitC(t, w, m5Request(t, w, id, src))
}
func m5Normal(t testing.TB, c int) string {
	return m5Read(t, fmt.Sprintf("submissions/c%02d-invocation.nql", c))
}
func m5Arg(t testing.TB, r core.Request, name string) core.AbilityValue {
	t.Helper()
	if len(r.Operations) != 1 || r.Operations[0].Invoke == nil {
		t.Fatal("one invocation required")
	}
	for _, a := range r.Operations[0].Invoke.Arguments {
		if string(a.Name) == name {
			return a.Value
		}
	}
	t.Fatal("missing binding", name)
	return core.AbilityValue{}
}
func m5ID(t testing.TB, r core.Request, name string) core.ConditionInstanceID {
	v := m5Arg(t, r, name)
	if v.ConditionID == nil {
		t.Fatal("missing creation binding")
	}
	return *v.ConditionID
}

// Generates only labeled evaluator-control invocation SOURCE, not replacement bodies.
func m5Control(t testing.TB, r core.Request, changes map[string]core.AbilityValue) string {
	t.Helper()
	v := core.CanonicalInvocation(*r.Operations[0].Invoke)
	var out strings.Builder
	fmt.Fprintf(&out, "invoke %q {\n", v.Ability)
	for _, a := range v.Arguments {
		value := a.Value
		if x, ok := changes[string(a.Name)]; ok {
			value = x
		}
		fmt.Fprintf(&out, "%q = ", a.Name)
		switch {
		case value.Character != nil:
			fmt.Fprintf(&out, "character %q", *value.Character)
		case value.Item != nil:
			fmt.Fprintf(&out, "item %q", *value.Item)
		case value.Integer != nil:
			fmt.Fprintf(&out, "int %d", *value.Integer)
		case value.ConditionID != nil:
			fmt.Fprintf(&out, "condition_id %q", *value.ConditionID)
		default:
			t.Fatal("invalid evaluator value")
		}
		out.WriteString(";\n")
	}
	out.WriteString("};\n")
	return out.String()
}
func m5Facts(t testing.TB, w *core.World, hp, def [3]int64, owners [3]string, shieldWearer string) {
	t.Helper()
	for i, n := range m5Names {
		p, e := w.CharacterStatus(core.CharacterID("char:" + n))
		if e != nil || p.Resources["hp"].Current != hp[i] || len(p.Stats) != 1 || p.Stats[0].Effective != def[i] {
			t.Fatal("fixed character oracle", n, p, e)
		}
	}
	for i, id := range []core.ItemID{"item:shield", "item:token", "item:reward"} {
		found := false
		for _, v := range w.Snapshot().Items {
			if v.ID == id {
				found = true
				wearer := ""
				if i == 0 {
					wearer = shieldWearer
				}
				if string(v.Owner) != owners[i] || string(v.Wearer) != wearer {
					t.Fatal("fixed item oracle", v)
				}
			}
		}
		if !found {
			t.Fatal("missing item")
		}
	}
}
func m5Unrelated(t testing.TB, initial core.Snapshot, w *core.World) {
	t.Helper()
	now := w.Snapshot()
	if !reflect.DeepEqual(initial.Rules, now.Rules) || !reflect.DeepEqual(initial.Locations, now.Locations) || !reflect.DeepEqual(initial.Effects, now.Effects) || !reflect.DeepEqual(initial.Relationships, now.Relationships) || !reflect.DeepEqual(initial.ConditionDefinitions, now.ConditionDefinitions) || !reflect.DeepEqual(initial.AbilityDefinitions, now.AbilityDefinitions) {
		t.Fatal("unrelated canonical records changed")
	}
	for i, before := range initial.Characters {
		after := now.Characters[i]
		for key, r := range after.Resources {
			r.Current = before.Resources[key].Current
			after.Resources[key] = r
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("unrelated character fact changed")
		}
	}
}
func m5Events(t testing.TB, b core.Batch, c int, kinds []core.EventKind, steps []int) {
	t.Helper()
	if len(b.Events) != len(kinds) || len(b.Request.Operations) != 1 {
		t.Fatal("event count", b)
	}
	for i, e := range b.Events {
		if e.Kind != kinds[i] || e.ID != fmt.Sprintf("event:%d:%d", b.Revision, i+1) || e.Revision != b.Revision || e.RequestID != b.Request.ID || e.Actor != b.Request.Actor || e.Operation != 0 {
			t.Fatal("event envelope", e)
		}
		if steps[i] < 0 {
			if e.AbilityOrigin != nil || !reflect.DeepEqual(e.Ability, b.Request.Operations[0].Invoke) {
				t.Fatal("terminal invocation")
			}
		} else if e.AbilityOrigin == nil || string(e.AbilityOrigin.Ability) != m5IDs[c-1] || e.AbilityOrigin.Step != steps[i] {
			t.Fatal("body origin", e)
		}
		if e.Condition != nil {
			x := e.Condition.After
			if x == nil || x.Source == nil || *x.Source != (core.Ref{Kind: core.LocationKind, ID: "loc:shrine"}) || x.Cause == nil || x.Cause.Actor != b.Request.Actor || x.Cause.RequestID != b.Request.ID || x.Cause.Operation != 0 || !reflect.DeepEqual(x.Cause.AbilityOrigin, e.AbilityOrigin) || x.AppliedRevision != b.Revision {
				t.Fatal("Condition provenance")
			}
			if x.Type == "condition:guarded" {
				if len(e.Condition.AfterEffects) != 1 {
					t.Fatal("guarded effect")
				}
				f := e.Condition.AfterEffects[0]
				if f.Amount != 3 || f.Source != (core.Ref{Kind: core.ConditionKind, ID: string(x.ID)}) || f.ConditionOrigin == nil || f.ConditionOrigin.Instance != x.ID || f.ConditionOrigin.Modifier != "ward" || f.Target.ID != string(x.Target) || f.Stat != "defense" {
					t.Fatal("derived provenance", f)
				}
			} else if x.Type != "condition:marked" || e.Condition.AfterEffects != nil {
				t.Fatal("marked effects")
			}
		}
	}
}
func m5RetryReplay(t testing.TB, b core.Blueprint, w *core.World, label string) {
	t.Helper()
	before, history := w.Snapshot(), w.History()
	r, e := core.Replay(b, history)
	if e != nil || !reflect.DeepEqual(before, r.Snapshot()) || !reflect.DeepEqual(history, r.History()) {
		t.Fatal("complete reconstruction", e)
	}
	for _, world := range []*core.World{w, r} {
		for _, receipt := range history {
			got, e := world.Execute(receipt.Request)
			if e != nil || !reflect.DeepEqual(got, receipt) {
				t.Fatal("historical receipt", e)
			}
		}
		if !reflect.DeepEqual(before, world.Snapshot()) || !reflect.DeepEqual(history, world.History()) {
			t.Fatal("retry republished")
		}
	}
	for _, n := range []int{0, len(history) / 2, len(history)} {
		prefix, e := core.Replay(b, history[:n])
		if e != nil || prefix.Revision() != core.Revision(n) {
			t.Fatal("valid prefix", e)
		}
	}
	m5Write(t, label, map[string]any{"genesis": b, "snapshot": before, "history": history, "reconstructed_snapshot": r.Snapshot(), "reconstructed_history": r.History(), "complete_equality": true, "all_historical_receipts_equal": true})
}
func m5Reject(t testing.TB, w *core.World, id, src, code string, runtime bool) {
	t.Helper()
	before, h := w.Snapshot(), w.History()
	ops, ds := compiler.CompileActions("evaluator-negative", src, before.Blueprint)
	stage := "compiler"
	var err error
	if len(ds) == 0 {
		stage = "runtime"
		_, err = w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "char:rowan"}, Operations: ops})
	}
	m5Write(t, id+"-rejection", map[string]any{"source": src, "stage": stage, "diagnostics": ds, "error": fmt.Sprint(err), "before": before, "after": w.Snapshot(), "before_history": h, "after_history": w.History()})
	if runtime {
		var f *core.Fault
		if len(ds) > 0 || !errors.As(err, &f) || f.Code != code {
			t.Fatal("expected runtime rejection", code, ds, err)
		}
	} else if len(ds) != 1 || ds[0].Code != code {
		t.Fatal("expected compiler rejection", code, ds, err)
	}
	if !reflect.DeepEqual(before, w.Snapshot()) || !reflect.DeepEqual(h, w.History()) {
		t.Fatal("failure partially published")
	}
}
func m5Removal(t testing.TB, w *core.World, id string, instance core.ConditionInstanceID, target string) {
	t.Helper()
	b := m5Run(t, w, id, fmt.Sprintf("remove condition %q from %q;", instance, target))
	if len(b.Events) != 1 || b.Events[0].Kind != core.ConditionRemoved || b.Events[0].AbilityOrigin != nil || b.Events[0].Condition.Before.ID != instance {
		t.Fatal("direct removal envelope")
	}
}
func TestM5AuthoringCases(t *testing.T) {
	for c := 1; c <= 4; c++ {
		t.Run(fmt.Sprintf("C%02d", c), func(t *testing.T) {
			b := m5Load(t, "normal", c)
			w := world(t, b)
			initial := w.Snapshot()
			old, _ := w.CharacterStatus("char:iris")
			oldBytes := contractBytes(t, old)
			src := m5Normal(t, c)
			r := m5Request(t, w, fmt.Sprintf("C%02d-normal", c), src)
			first := commitC(t, w, r)
			if w.Revision() != 1 {
				t.Fatal("one commit")
			}
			m5Write(t, fmt.Sprintf("C%02d-first-evaluation", c), map[string]any{"source": src, "before": initial, "after": w.Snapshot(), "receipt": first})
			kinds := [][]core.EventKind{{core.HealingApplied, core.HealingApplied, core.AbilityInvoked}, {core.ConditionApplied, core.ConditionApplied, core.AbilityInvoked}, {core.ItemUnequipped, core.ItemTransferred, core.ItemTransferred, core.AbilityInvoked}, {core.HealingApplied, core.ItemTransferred, core.ConditionApplied, core.AbilityInvoked}}
			steps := [][]int{{0, 1, -1}, {0, 1, -1}, {0, 0, 1, -1}, {0, 1, 2, -1}}
			m5Events(t, first, c, kinds[c-1], steps[c-1])
			hp := [3]int64{6, 9, 11}
			def := [3]int64{10, 8, 9}
			owners := [3]string{"char:iris", "char:rowan", "char:rowan"}
			wearer := "char:sol"
			switch c {
			case 1:
				hp = [3]int64{10, 12, 11}
				a, z := first.Events[0].Resource, first.Events[1].Resource
				if a.Character != "char:iris" || a.Requested != 4 || a.Before.Current != 6 || a.After.Current != 10 || z.Character != "char:rowan" || z.Requested != 5 || z.Before.Current != 9 || z.After.Current != 12 {
					t.Fatal("resource clamp facts")
				}
			case 2:
				def[0] = 13
				if len(w.Snapshot().Conditions) != 2 || len(w.Snapshot().ConditionEffects) != 1 {
					t.Fatal("paired membership")
				}
				if first.Events[0].Condition.After.ID != m5ID(t, r, "guard_id") || first.Events[1].Condition.After.ID != m5ID(t, r, "mark_id") {
					t.Fatal("exact IDs")
				}
			case 3:
				owners[0] = "char:rowan"
				owners[1] = "char:iris"
				wearer = ""
				def[2] = 7
			case 4:
				hp[0] = 10
				def[0] = 13
				owners[2] = "char:iris"
				if len(w.Snapshot().Conditions) != 1 || first.Events[2].Condition.After.ID != m5ID(t, r, "guard_id") {
					t.Fatal("reward Condition")
				}
			}
			m5Facts(t, w, hp, def, owners, wearer)
			m5Unrelated(t, initial, w)
			contractEqual(t, oldBytes, old)
			now, _ := w.CharacterStatus("char:iris")
			if old.Revision != 0 || now.Revision != 1 {
				t.Fatal("projection revision")
			}
			if c == 1 || c == 3 {
				if len(w.Snapshot().Conditions) != 0 || len(w.Snapshot().RetiredConditionIDs) != 0 {
					t.Fatal("unexpected Condition mutation")
				}
			}
			m5RetryReplay(t, b, w, fmt.Sprintf("C%02d-normal-history", c))
			changed := r
			changed.Actor = core.Ref{Kind: core.CharacterKind, ID: "char:sol"}
			abilityReject(t, w, changed, "E_REQUEST_REUSE")
			if c == 2 || c == 4 {
				m5Removal(t, w, fmt.Sprintf("C%02d-cleanup", c), m5ID(t, r, "guard_id"), "char:iris")
				def[0] = 10
				m5Facts(t, w, hp, def, owners, wearer)
				if len(w.Snapshot().RetiredConditionIDs) != 1 || w.Snapshot().RetiredConditionIDs[0] != m5ID(t, r, "guard_id") {
					t.Fatal("removed lifetime")
				}
				if c == 2 && (len(w.Snapshot().Conditions) != 1 || w.Snapshot().Conditions[0].ID != m5ID(t, r, "mark_id")) {
					t.Fatal("marked lost")
				}
			} else {
				m5Run(t, w, fmt.Sprintf("C%02d-later", c), `heal "char:sol" resource "hp" by 1;`)
			}
			m5RetryReplay(t, b, w, fmt.Sprintf("C%02d-late-history", c))
			stale := r
			stale.ID = "fresh-stale"
			abilityReject(t, w, stale, "E_STALE")
			// Change an actual binding under the same committed ID, rather than metadata alone.
			altered := m5Control(t, r, map[string]core.AbilityValue{})
			formatted := m5Request(t, w, "format", "// same semantic intent\r\n"+altered)
			formatted.ID = r.ID
			formatted.ExpectedRevision = r.ExpectedRevision
			if got, e := w.Execute(formatted); e != nil || !reflect.DeepEqual(got, first) {
				t.Fatal("source-free historical identity", e)
			}
			conflict := formatted
			v := core.CanonicalInvocation(*conflict.Operations[0].Invoke)
			for i := range v.Arguments {
				if v.Arguments[i].Value.Character != nil {
					v.Arguments[i].Value.Character = ptr(core.CharacterID("char:sol"))
					break
				}
			}
			conflict.Operations = []core.Operation{{Invoke: &v}}
			abilityReject(t, w, conflict, "E_REQUEST_REUSE")
			m5Controls(t, c, b, r, src)
		})
	}
}
func m5Controls(t *testing.T, c int, b core.Blueprint, r core.Request, src string) {
	w := world(t, b)
	switch c {
	case 1:
		m5Reject(t, w, "C01-kind", m5Control(t, r, map[string]core.AbilityValue{"first": {Item: ptr(core.ItemID("item:shield"))}}), "E_TYPE", false)
		m5Reject(t, w, "C01-amount", m5Control(t, r, map[string]core.AbilityValue{"second_amount": {Integer: ptr(int64(0))}}), "E_AMOUNT", false)
		m5Run(t, w, "C01-amount", src)
	case 2:
		same := core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("control:paired:same"))}
		m5Reject(t, w, "C02-duplicate", m5Control(t, r, map[string]core.AbilityValue{"guard_id": same, "mark_id": same}), "E_DUPLICATE", true)
		m5Run(t, w, "C02-duplicate", m5Control(t, r, map[string]core.AbilityValue{"guard_id": same, "mark_id": {ConditionID: ptr(core.ConditionInstanceID("control:paired:other"))}}))
	case 3:
		alt := m5Load(t, "keeps-worn", 3)
		aw := world(t, alt)
		receipt := m5Run(t, aw, "C03-alternate", src)
		m5Events(t, receipt, 3, []core.EventKind{core.ItemTransferred, core.ItemTransferred, core.AbilityInvoked}, []int{0, 1, -1})
		m5Facts(t, aw, [3]int64{6, 9, 11}, [3]int64{10, 8, 9}, [3]string{"char:rowan", "char:iris", "char:rowan"}, "char:sol")
		m5RetryReplay(t, alt, aw, "C03-alternate-history")
		b = m5Load(t, "wrong-owner", 3)
		w = world(t, b)
		m5Reject(t, w, "C03-owner", src, "E_OWNER", true)
		m5Facts(t, w, [3]int64{6, 9, 11}, [3]int64{10, 8, 9}, [3]string{"char:iris", "char:sol", "char:rowan"}, "char:sol")
		m5Run(t, w, "C03-explicit-setup", `transfer "item:token" from "char:sol" to "char:rowan";`)
		m5Run(t, w, "C03-owner", src)
	case 4:
		b = m5Load(t, "existing-guard", 4)
		w = world(t, b)
		m5Reject(t, w, "C04-unique", src, "E_RULE", true)
		m5Facts(t, w, [3]int64{6, 9, 11}, [3]int64{13, 8, 9}, [3]string{"char:iris", "char:rowan", "char:rowan"}, "char:sol")
		m5Removal(t, w, "C04-explicit-setup", "genesis:guard", "char:iris")
		m5Run(t, w, "C04-unique", src)
	}
	m5RetryReplay(t, b, w, fmt.Sprintf("C%02d-control-continuation", c))
}
func TestM5CombinedJourney(t *testing.T) {
	b := m5Load(t, "normal", 1, 2, 3, 4)
	w := world(t, b)
	initial := w.Snapshot()
	for c := 1; c <= 4; c++ {
		r := m5Request(t, w, "bindings", m5Normal(t, c))
		changes := map[string]core.AbilityValue{}
		if c == 2 {
			changes["guard_id"] = core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("journey:iris:guard"))}
			changes["mark_id"] = core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("journey:iris:mark"))}
		}
		if c == 4 {
			changes["recipient"] = core.AbilityValue{Character: ptr(core.CharacterID("char:sol"))}
			changes["guard_id"] = core.AbilityValue{ConditionID: ptr(core.ConditionInstanceID("journey:sol:guard"))}
		}
		source := m5Control(t, r, changes)
		m5Write(t, fmt.Sprintf("journey-C%02d-source", c), map[string]string{"role": "evaluator predefined journey bindings", "source": source})
		batch := m5Run(t, w, fmt.Sprintf("journey-C%02d", c), source)
		want := []int{3, 3, 4, 4}[c-1]
		if len(batch.Events) != want || w.Revision() != core.Revision(c) {
			t.Fatal("journey batch")
		}
	}
	m5Facts(t, w, [3]int64{10, 12, 15}, [3]int64{13, 8, 10}, [3]string{"char:rowan", "char:iris", "char:sol"}, "")
	if len(w.Snapshot().Conditions) != 3 {
		t.Fatal("journey membership")
	}
	m5Unrelated(t, initial, w)
	m5RetryReplay(t, b, w, "journey-four")
	m5Removal(t, w, "journey-remove-iris", "journey:iris:guard", "char:iris")
	m5Removal(t, w, "journey-remove-sol", "journey:sol:guard", "char:sol")
	m5Facts(t, w, [3]int64{10, 12, 15}, [3]int64{10, 8, 7}, [3]string{"char:rowan", "char:iris", "char:sol"}, "")
	s := w.Snapshot()
	if w.Revision() != 6 || len(s.Conditions) != 1 || s.Conditions[0].ID != "journey:iris:mark" || !reflect.DeepEqual(s.RetiredConditionIDs, []core.ConditionInstanceID{"journey:iris:guard", "journey:sol:guard"}) {
		t.Fatal("journey lifetime oracle")
	}
	count := 0
	for _, batch := range w.History() {
		count += len(batch.Events)
	}
	if count != 16 {
		t.Fatal("journey events", count)
	}
	m5RetryReplay(t, b, w, "journey-final")
}

// Evaluates every original launcher input as a separately labeled API rerun,
// including failed/inspection attempts. It never rewrites an author's bytes.
func TestM5RecordedAttempts(t *testing.T) {
	dirs, e := filepath.Glob("testdata/m5/c*/attempts/*")
	if e != nil || len(dirs) == 0 {
		t.Fatal("missing real author attempts", e)
	}
	for _, dir := range dirs {
		name := filepath.Base(filepath.Dir(filepath.Dir(dir))) + "-" + filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			src, e := os.ReadFile(filepath.Join(dir, "world.nql"))
			if e != nil {
				t.Fatal(e)
			}
			b, ds := compiler.CompileWorld("original-recorded-world", string(src))
			result := map[string]any{"role": "controller public-API rerun of exact attempt", "world_diagnostics": ds}
			if len(ds) > 0 {
				m5Write(t, "attempt-"+name, result)
				return
			}
			w := world(t, b)
			result["before"] = w.Snapshot()
			action, e := os.ReadFile(filepath.Join(dir, "invocation.nql"))
			if os.IsNotExist(e) {
				result["stage"] = "inspection"
			} else {
				if e != nil {
					t.Fatal(e)
				}
				ops, ds := compiler.CompileActions("original-recorded-action", string(action), b)
				result["action_diagnostics"] = ds
				if len(ds) == 0 {
					var metadata struct {
						RequestID        string        `json:"request_id"`
						ExpectedRevision core.Revision `json:"expected_revision"`
						Actor            core.Ref      `json:"actor"`
					}
					raw, e := os.ReadFile(filepath.Join(dir, "request.json"))
					if e != nil {
						t.Fatal(e)
					}
					if e = json.Unmarshal(raw, &metadata); e != nil {
						t.Fatal(e)
					}
					receipt, err := w.Execute(core.Request{ID: metadata.RequestID, ExpectedRevision: metadata.ExpectedRevision, Actor: metadata.Actor, Operations: ops})
					result["stage"] = "runtime"
					result["receipt"] = receipt
					result["error"] = fmt.Sprint(err)
					result["after"] = w.Snapshot()
					result["history"] = w.History()
					if err == nil {
						var cli struct {
							Result core.Batch    `json:"result"`
							World  core.Snapshot `json:"world"`
						}
						raw, e = os.ReadFile(filepath.Join(dir, "execution.stdout.json"))
						if e != nil {
							t.Fatal(e)
						}
						if e = json.Unmarshal(raw, &cli); e != nil {
							t.Fatal(e)
						}
						if !reflect.DeepEqual(cli.Result, receipt) || !reflect.DeepEqual(cli.World, w.Snapshot()) {
							t.Fatal("original CLI receipt/state differs from API execution")
						}
						m5RetryReplay(t, b, w, "primary-receipt-"+name)
						m5Run(t, w, "primary-later-"+name, `heal "char:sol" resource "hp" by 1;`)
						m5RetryReplay(t, b, w, "primary-late-receipt-"+name)
						result["original_cli_receipt_and_state_equal"] = true
					}
				} else {
					result["stage"] = "action compiler"
				}
			}
			if _, recorded := result["after"]; !recorded {
				result["after"] = w.Snapshot()
				result["history"] = w.History()
			}
			m5Write(t, "attempt-"+name, result)
		})
	}
}
