package main

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

const testWorld = `rules { transfer unequips; wear any; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "dexterity" = 13; stat "defense" = 10; }
condition type "training:shield" { instances unique; }
condition "training:a:shield" of "training:shield" to "char:a";
equipment "nql-equipment-v1" {
 slot "hand" capacity 2; slot "body" capacity 1; slot "shield" capacity 1;
 derive stat "defense" { term stat "dexterity" offset -10 divide 2; }
 definition "gear:scale" named "Scale mail" { description "Medium armor."; default "worn";
  mode "worn" { occupy "body" by 1; base stat "defense" { constant 14; term stat "dexterity" offset -10 divide 2 ceiling 2; } } }
 definition "gear:shield" named "Shield" { description "Shield."; default "held";
  mode "held" { occupy "hand" by 1; occupy "shield" by 1; modifier "defense" stat "defense" add 2 requires "training:shield"; } }
}
item "item:scale:1" named "Scale mail" { owner "char:a"; wearer "char:a"; definition "gear:scale"; mode "worn"; }
item "item:shield:1" named "Shield" { owner "char:a"; wearer "char:a"; definition "gear:shield"; mode "held"; }
`

func invoke(t *testing.T, doc map[string]any) (int, output) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := run(bytes.NewReader(raw), &out)
	var resp output
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, out.String())
	}
	return code, resp
}

func checkpointDoc(t *testing.T, cp *core.Checkpoint) map[string]any {
	t.Helper()
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	return generic
}

func explainEffective(t *testing.T, resp output) int64 {
	t.Helper()
	if len(resp.Explain) != 1 {
		t.Fatalf("expected one explanation, got %d (phase %s, error %q)", len(resp.Explain), resp.Phase, resp.Error)
	}
	return resp.Explain[0].Effective
}

func TestGenesisExecuteRetryAndRejects(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	explain := []map[string]any{{"character": "char:a", "stat": "defense"}}

	code, g := invoke(t, map[string]any{"world": testWorld, "explain": explain})
	if code != 0 || !g.OK || g.Revision != 0 || g.Checkpoint == nil {
		t.Fatalf("genesis: code %d ok %v rev %d error %q diagnostics %+v", code, g.OK, g.Revision, g.Error, g.Diagnostics)
	}
	if v := explainEffective(t, g); v != 17 {
		t.Fatalf("genesis defense = %d, want 17 (14 + 1 dex + 2 shield)", v)
	}

	code, r := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint),
		"actions": `unequip "item:scale:1";`, "actor": actor, "request": "host:1", "explain": explain})
	if code != 0 || !r.OK || r.Phase != "executed" || r.Revision != 1 || r.Historical || r.Receipt == nil {
		t.Fatalf("unequip: code %d ok %v phase %s rev %d historical %v error %q", code, r.OK, r.Phase, r.Revision, r.Historical, r.Error)
	}
	if v := explainEffective(t, r); v != 13 {
		t.Fatalf("after unequip defense = %d, want 13", v)
	}
	if len(r.Checkpoint.History) != 1 || r.Checkpoint.Head != 1 {
		t.Fatalf("checkpoint history %d head %d, want 1/1", len(r.Checkpoint.History), r.Checkpoint.Head)
	}
	cp1 := checkpointDoc(t, r.Checkpoint)

	// Exact retry of the committed request returns the original receipt without a new revision.
	_, retry := invoke(t, map[string]any{"checkpoint": cp1, "actions": `unequip "item:scale:1";`,
		"actor": actor, "request": "host:1", "expected_revision": 0})
	if !retry.OK || !retry.Historical || retry.Revision != 1 || retry.Receipt.Revision != 1 {
		t.Fatalf("retry: ok %v historical %v rev %d error %q", retry.OK, retry.Historical, retry.Revision, retry.Error)
	}

	// So does a retry that omits expected_revision (the host never sends one).
	_, bare := invoke(t, map[string]any{"checkpoint": cp1, "actions": `unequip "item:scale:1";`,
		"actor": actor, "request": "host:1"})
	if !bare.OK || !bare.Historical || bare.Revision != 1 || !reflect.DeepEqual(bare.Receipt, r.Receipt) {
		t.Fatalf("defaulted retry: ok %v historical %v rev %d error %q", bare.OK, bare.Historical, bare.Revision, bare.Error)
	}

	// Same ID with different content is refused, with or without expected_revision.
	_, bareReuse := invoke(t, map[string]any{"checkpoint": cp1, "actions": `equip "item:scale:1" on "char:a";`,
		"actor": actor, "request": "host:1", "expected_revision": 1})
	if bareReuse.OK || bareReuse.Fault == nil || bareReuse.Fault.Code != "E_REQUEST_REUSE" {
		t.Fatalf("explicit reuse: ok %v fault %+v", bareReuse.OK, bareReuse.Fault)
	}
	_, reuse := invoke(t, map[string]any{"checkpoint": cp1, "actions": `equip "item:scale:1" on "char:a";`,
		"actor": actor, "request": "host:1"})
	if reuse.OK || reuse.Fault == nil || reuse.Fault.Code != "E_REQUEST_REUSE" {
		t.Fatalf("reuse: ok %v fault %+v", reuse.OK, reuse.Fault)
	}

	// Stale expected revision is refused.
	_, stale := invoke(t, map[string]any{"checkpoint": cp1, "actions": `equip "item:scale:1" on "char:a";`,
		"actor": actor, "request": "host:2", "expected_revision": 0})
	if stale.OK || stale.Fault == nil || stale.Fault.Code != "E_STALE" {
		t.Fatalf("stale: ok %v fault %+v", stale.OK, stale.Fault)
	}

	// A rejected transaction leaves no checkpoint and reports the untouched revision.
	_, hands := invoke(t, map[string]any{"checkpoint": cp1,
		"actions": `equip "item:scale:1" on "char:a"; equip "item:shield:1" on "char:a";`,
		"actor":   actor, "request": "host:2"})
	if hands.OK || hands.Checkpoint != nil || hands.Revision != 1 {
		t.Fatalf("double equip: ok %v checkpoint %v rev %d error %q", hands.OK, hands.Checkpoint != nil, hands.Revision, hands.Error)
	}

	// Re-equip restores the derived value and the equipment view shows the slots.
	_, again := invoke(t, map[string]any{"checkpoint": cp1, "actions": `equip "item:scale:1" on "char:a";`,
		"actor": actor, "request": "host:2", "equipment": []string{"char:a"}, "explain": explain})
	if !again.OK || again.Revision != 2 || explainEffective(t, again) != 17 || len(again.Equipment) != 1 {
		t.Fatalf("re-equip: ok %v rev %d error %q", again.OK, again.Revision, again.Error)
	}
}

func TestReplayRejectsTamperedCheckpoint(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": testWorld})
	_, r := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": `unequip "item:scale:1";`,
		"actor": map[string]any{"id": "char:a"}, "request": "host:1"})
	cp := checkpointDoc(t, r.Checkpoint)
	cp["Head"] = 5
	_, bad := invoke(t, map[string]any{"checkpoint": cp})
	if bad.OK || bad.Phase != "replay" || bad.Fault == nil || bad.Fault.Code != "E_REPLAY" {
		t.Fatalf("tampered head: ok %v phase %s fault %+v", bad.OK, bad.Phase, bad.Fault)
	}
	// A forged event that disagrees with the recorded request is also refused.
	cp = checkpointDoc(t, r.Checkpoint)
	history := cp["History"].([]any)
	batch := history[0].(map[string]any)
	batch["events"] = []any{}
	_, forged := invoke(t, map[string]any{"checkpoint": cp})
	if forged.OK || forged.Phase != "replay" {
		t.Fatalf("forged events: ok %v phase %s error %q", forged.OK, forged.Phase, forged.Error)
	}
}

func TestInputContract(t *testing.T) {
	var out bytes.Buffer
	if code := run(strings.NewReader(`{"bogus":1}`), &out); code != 2 {
		t.Fatalf("unknown field: code %d body %s", code, out.String())
	}
	_, both := invoke(t, map[string]any{"world": testWorld, "checkpoint": map[string]any{}})
	if both.OK || both.Phase != "input" {
		t.Fatalf("world and checkpoint together: ok %v phase %s", both.OK, both.Phase)
	}
	_, noActor := invoke(t, map[string]any{"world": testWorld, "actions": `unequip "item:scale:1";`, "request": "x"})
	if noActor.OK || noActor.Phase != "input" {
		t.Fatalf("missing actor: ok %v phase %s", noActor.OK, noActor.Phase)
	}
	_, diag := invoke(t, map[string]any{"world": testWorld, "actions": `unequip "item:nope";`,
		"actor": map[string]any{"id": "char:a"}, "request": "x"})
	if diag.OK || diag.Phase != "compile" || len(diag.Diagnostics) != 1 || diag.Diagnostics[0].Code != "E_REFERENCE" {
		t.Fatalf("compile diagnostic: ok %v phase %s diagnostics %+v", diag.OK, diag.Phase, diag.Diagnostics)
	}
	_, none := invoke(t, map[string]any{})
	if none.OK || none.Phase != "input" {
		t.Fatalf("empty: ok %v phase %s", none.OK, none.Phase)
	}
}

const timedWorld = `rules { transfer unequips; wear any; }
clock "second" at 1000;
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "defense" = 10; resource "hp" = 20 min 0 max 20; }
condition type "spell:shield-of-faith" { instances unique; modifier "sof" stat "defense" add 2; }
condition type "potion:bark" { instances replace; modifier "bark" stat "defense" add magnitude; }
condition type "spell:bless" { instances unique; }
condition "effect:sof:a" of "spell:shield-of-faith" to "char:a" ticks 10;
`

func TestEndedConditions(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": timedWorld})
	if !g.OK || g.EndedConditions != nil {
		t.Fatalf("genesis: ok %v error %q diagnostics %+v ended %+v", g.OK, g.Error, g.Diagnostics, g.EndedConditions)
	}
	cp0 := checkpointDoc(t, g.Checkpoint)

	first := `apply condition "effect:bark:1" of "potion:bark" to "char:a" magnitude 3 ticks 60;
apply condition "effect:bless:a" of "spell:bless" to "char:a"; advance time by 15;`
	_, r1 := invoke(t, map[string]any{"checkpoint": cp0, "actions": first, "actor": actor, "request": "host:1"})
	if !r1.OK || len(r1.EndedConditions) != 1 {
		t.Fatalf("advance: ok %v error %q ended %+v", r1.OK, r1.Error, r1.EndedConditions)
	}
	sof := r1.EndedConditions[0]
	if sof.Instance != "effect:sof:a" || sof.Type != "spell:shield-of-faith" || sof.Target != "char:a" || sof.Reason != "clock" ||
		sof.Deadline == nil || *sof.Deadline != 1010 || sof.By != nil || len(sof.Modifiers) != 1 ||
		sof.Modifiers[0] != (endedModifier{ID: "sof", Stat: "defense", Amount: 2}) {
		t.Fatalf("clock expiry: %+v", sof)
	}

	// A historical retry reports the same endings from the original receipt.
	cp1 := checkpointDoc(t, r1.Checkpoint)
	_, retry := invoke(t, map[string]any{"checkpoint": cp1, "actions": first, "actor": actor, "request": "host:1"})
	if !retry.OK || !retry.Historical || len(retry.EndedConditions) != 1 || retry.EndedConditions[0].Reason != "clock" {
		t.Fatalf("retry: ok %v historical %v ended %+v", retry.OK, retry.Historical, retry.EndedConditions)
	}

	// Replacement and explicit removal are listed with the amounts they had in force.
	second := `apply condition "effect:bark:2" of "potion:bark" to "char:a" magnitude 1 ticks 60;
remove condition "effect:bless:a" from "char:a";`
	_, r2 := invoke(t, map[string]any{"checkpoint": cp1, "actions": second, "actor": actor, "request": "host:2"})
	if !r2.OK || len(r2.EndedConditions) != 2 {
		t.Fatalf("replace/remove: ok %v error %q ended %+v", r2.OK, r2.Error, r2.EndedConditions)
	}
	bark, bless := r2.EndedConditions[0], r2.EndedConditions[1]
	if bark.Instance != "effect:bark:1" || bark.Reason != "replaced" || bark.By == nil || *bark.By != "effect:bark:2" ||
		bark.Deadline != nil || len(bark.Modifiers) != 1 || bark.Modifiers[0] != (endedModifier{ID: "bark", Stat: "defense", Amount: 3}) {
		t.Fatalf("replacement: %+v", bark)
	}
	if bless.Instance != "effect:bless:a" || bless.Reason != "removed" || bless.By != nil || bless.Modifiers == nil || len(bless.Modifiers) != 0 {
		t.Fatalf("explicit removal: %+v", bless)
	}

	// A request that ends nothing carries no ended_conditions key at all.
	raw, err := json.Marshal(map[string]any{"checkpoint": checkpointDoc(t, r2.Checkpoint), "actions": `advance time by 1;`, "actor": actor, "request": "host:3"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run(bytes.NewReader(raw), &out); code != 0 || strings.Contains(out.String(), "ended_conditions") || !strings.Contains(out.String(), `"phase": "executed"`) {
		t.Fatalf("quiet request: code %d body %s", code, out.String())
	}
}

// The host flow for Aid: apply and heal in one request, read the raised
// maximum from status, then redeclare it at the next genesis and let it expire.
func TestResourceMaximumHostFlow(t *testing.T) {
	actor := map[string]any{"id": "char:a"}
	world := func(hp, max string, aid string) string {
		return `rules { transfer unequips; wear any; }
clock "second" at 1000;
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "defense" = 10; resource "hp" = ` + hp + ` min 0 max ` + max + `; }
condition type "spell:aid" { instances replace; modifier "aid" resource "hp" maximum add magnitude; }
` + aid
	}
	_, r := invoke(t, map[string]any{"world": world("20", "20", ""), "status": []string{"char:a"}, "actor": actor, "request": "host:aid",
		"actions": `apply condition "effect:aid:a" of "spell:aid" to "char:a" magnitude 5 ticks 28800; heal "char:a" resource "hp" by 5;`})
	if !r.OK || len(r.Status) != 1 {
		t.Fatalf("aid: ok %v error %q diagnostics %+v", r.OK, r.Error, r.Diagnostics)
	}
	hp := r.Status[0].Resources["hp"]
	if hp.Current != 25 || hp.Maximum != 25 || r.EndedConditions != nil {
		t.Fatalf("aid: hp %+v ended %+v", hp, r.EndedConditions)
	}
	expires := r.Status[0].Conditions[0].ClockLifetime.ExpiresTick

	// Checkpoint continuation replays the stored maximum exactly.
	_, c := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "status": []string{"char:a"}})
	if !c.OK || c.Status[0].Resources["hp"] != hp {
		t.Fatalf("replay: ok %v error %q", c.OK, c.Error)
	}

	// Stateless genesis: declare what status reported, with the remaining ticks.
	aid := `condition "effect:aid:a" of "spell:aid" to "char:a" magnitude 5 ticks ` + strconv.FormatUint(expires-1000, 10) + `;`
	_, e := invoke(t, map[string]any{"world": world("25", "25", aid), "status": []string{"char:a"}, "actor": actor, "request": "host:rest",
		"actions": `advance time by 28800;`})
	if !e.OK || len(e.EndedConditions) != 1 {
		t.Fatalf("expiry: ok %v error %q ended %+v", e.OK, e.Error, e.EndedConditions)
	}
	ended := e.EndedConditions[0]
	if ended.Reason != "clock" || len(ended.Modifiers) != 1 || ended.Modifiers[0] != (endedModifier{ID: "aid", ResourceMaximum: "hp", Amount: 5}) {
		t.Fatalf("ended %+v", ended)
	}
	if hp := e.Status[0].Resources["hp"]; hp.Current != 20 || hp.Maximum != 20 {
		t.Fatalf("after expiry hp %+v", hp)
	}
}

// A max HP drain held until a long rest: damage, then the drain, then a
// stateless genesis at the drained values and a rest that removes it.
func TestResourceMaximumDrainHostFlow(t *testing.T) {
	actor := map[string]any{"id": "char:a"}
	world := func(hp, max string, drain string) string {
		return `rules { transfer unequips; wear any; }
clock "second" at 1000;
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "defense" = 10; resource "hp" = ` + hp + ` min 0 max ` + max + `; }
condition type "curse:drain" { instances coexist; modifier "drain" resource "hp" maximum add magnitude; }
` + drain
	}
	_, r := invoke(t, map[string]any{"world": world("20", "20", ""), "status": []string{"char:a"}, "actor": actor, "request": "host:drain",
		"actions": `damage "char:a" resource "hp" by 7; apply condition "effect:drain:a" of "curse:drain" to "char:a" magnitude -7;`})
	if !r.OK || len(r.Status) != 1 {
		t.Fatalf("drain: ok %v error %q diagnostics %+v", r.OK, r.Error, r.Diagnostics)
	}
	hp := r.Status[0].Resources["hp"]
	if hp.Current != 13 || hp.Maximum != 13 || r.EndedConditions != nil {
		t.Fatalf("drain: hp %+v ended %+v", hp, r.EndedConditions)
	}

	// Checkpoint continuation replays the drained maximum exactly.
	_, c := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "status": []string{"char:a"}})
	if !c.OK || c.Status[0].Resources["hp"] != hp {
		t.Fatalf("replay: ok %v error %q", c.OK, c.Error)
	}

	// Stateless genesis declares what status reported; the rest gives the 7 back.
	drain := `condition "effect:drain:a" of "curse:drain" to "char:a" magnitude -7;`
	_, e := invoke(t, map[string]any{"world": world("13", "13", drain), "status": []string{"char:a"}, "actor": actor, "request": "host:rest",
		"actions": `remove condition "effect:drain:a" from "char:a"; heal "char:a" resource "hp" by 7;`})
	if !e.OK || len(e.EndedConditions) != 1 {
		t.Fatalf("rest: ok %v error %q ended %+v", e.OK, e.Error, e.EndedConditions)
	}
	ended := e.EndedConditions[0]
	if ended.Reason != "removed" || len(ended.Modifiers) != 1 || ended.Modifiers[0] != (endedModifier{ID: "drain", ResourceMaximum: "hp", Amount: -7}) {
		t.Fatalf("ended %+v", ended)
	}
	if hp := e.Status[0].Resources["hp"]; hp.Current != 20 || hp.Maximum != 20 {
		t.Fatalf("after rest hp %+v", hp)
	}
}

// A view that fails after a commit must not hide the commit: the host treats
// ok false as "nothing happened" and would lose the checkpoint.
func TestViewFailureAfterCommitKeepsTheCommit(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": testWorld})
	cp0 := checkpointDoc(t, g.Checkpoint)
	doc := map[string]any{"checkpoint": cp0, "actions": `unequip "item:scale:1";`, "actor": actor, "request": "host:1",
		"status": []string{"char:a", "char:nope"}, "explain": []map[string]any{{"character": "char:a", "stat": "defense"}}}
	_, r := invoke(t, doc)
	if !r.OK || r.Phase != "executed" || r.Revision != 1 || r.Receipt == nil || r.Checkpoint == nil || len(r.Checkpoint.History) != 1 {
		t.Fatalf("commit hidden: ok %v phase %s rev %d receipt %v checkpoint %v error %q", r.OK, r.Phase, r.Revision, r.Receipt != nil, r.Checkpoint != nil, r.Error)
	}
	if r.ViewError == nil || r.ViewError.View != "status" || r.ViewError.Fault == nil || r.Error != "" || r.Fault != nil {
		t.Fatalf("view error %+v, error %q fault %+v", r.ViewError, r.Error, r.Fault)
	}
	if len(r.Status) != 1 || r.Status[0].Character.ID != "char:a" || r.Explain != nil {
		t.Fatalf("views before the failure are kept and later ones omitted: status %d explain %d", len(r.Status), len(r.Explain))
	}

	// The returned checkpoint carries the commit, and a historical retry with
	// the same failing view still reports its receipt.
	doc["checkpoint"] = checkpointDoc(t, r.Checkpoint)
	_, retry := invoke(t, doc)
	if !retry.OK || !retry.Historical || retry.Revision != 1 || retry.ViewError == nil {
		t.Fatalf("retry: ok %v historical %v rev %d view error %+v", retry.OK, retry.Historical, retry.Revision, retry.ViewError)
	}

	// Without a transaction nothing happened, so a failing view fails the response.
	_, read := invoke(t, map[string]any{"checkpoint": cp0, "status": []string{"char:nope"}})
	if read.OK || read.Phase != "status" || read.Checkpoint != nil || read.ViewError != nil || read.Fault == nil {
		t.Fatalf("read-only: ok %v phase %s checkpoint %v view error %+v", read.OK, read.Phase, read.Checkpoint != nil, read.ViewError)
	}
}

// A panic inside the engine still produces a JSON body, with its own exit code
// and nothing for the host to persist.
func TestPanicBecomesInternalResponse(t *testing.T) {
	var logged bytes.Buffer
	defer func(f func(input) output, w io.Writer) { applyFunc, stderr = f, w }(applyFunc, stderr)
	applyFunc = func(input) output { panic("boom") }
	stderr = &logged
	var out bytes.Buffer
	code := run(strings.NewReader(`{"world": "x"}`), &out)
	var resp output
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("no JSON body: %v %q", err, out.String())
	}
	if code != exitInternal || resp.OK || resp.Phase != "internal" || !strings.Contains(resp.Error, "boom") || resp.Checkpoint != nil || resp.Receipt != nil {
		t.Fatalf("code %d response %+v", code, resp)
	}
	if !strings.Contains(logged.String(), "panic: boom") || !strings.Contains(logged.String(), "goroutine") {
		t.Fatalf("stack not logged: %q", logged.String())
	}
}

// FuzzApplyInput feeds mutated request documents, including checkpoints, to
// the whole tool. Any engine panic is a defect even though run recovers it.
func FuzzApplyInput(f *testing.F) {
	actor := map[string]any{"id": "char:a"}
	seed := func(doc map[string]any) []byte {
		raw, err := json.Marshal(doc)
		if err != nil {
			f.Fatal(err)
		}
		return raw
	}
	f.Add(seed(map[string]any{"world": testWorld, "status": []string{"char:a"}}))
	f.Add(seed(map[string]any{"world": timedWorld, "actions": `advance time by 15;`, "actor": actor, "request": "r"}))
	f.Add(seed(map[string]any{"world": timedWorld + `condition type "curse:drain" { instances coexist; modifier "drain" resource "hp" maximum add magnitude; }`,
		"actions": `apply condition "d:1" of "curse:drain" to "char:a" magnitude -7 ticks 10; advance time by 15;`, "actor": actor, "request": "r", "status": []string{"char:a"}}))
	for _, w := range []string{testWorld, timedWorld} {
		var out bytes.Buffer
		actions := `unequip "item:scale:1";`
		if w == timedWorld {
			actions = `apply condition "effect:bark:1" of "potion:bark" to "char:a" magnitude 3 ticks 60; advance time by 15;`
		}
		if run(bytes.NewReader(seed(map[string]any{"world": w, "actions": actions, "actor": actor, "request": "r1"})), &out) != exitResponse {
			f.Fatal(out.String())
		}
		var resp output
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.Checkpoint == nil {
			f.Fatalf("seed checkpoint: %v %s", err, out.String())
		}
		f.Add(seed(map[string]any{"checkpoint": resp.Checkpoint, "status": []string{"char:a"}, "explain": []map[string]any{{"character": "char:a", "stat": "defense"}}}))
		f.Add(seed(map[string]any{"checkpoint": resp.Checkpoint, "actions": `advance time by 1;`, "actor": actor, "request": "r2"}))
	}
	defer func(w io.Writer) { stderr = w }(stderr)
	stderr = io.Discard
	f.Fuzz(func(t *testing.T, data []byte) {
		var out bytes.Buffer
		code := run(bytes.NewReader(data), &out)
		if code == exitInternal {
			t.Fatalf("engine panicked: %s", out.String())
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("exit %d without a JSON body: %q", code, out.String())
		}
	})
}

// The host flow for a saving throw: the engine rolls with the world's dice or
// takes the player's faces, and the response lists each check's outcome.
func TestChecks(t *testing.T) {
	world := `rules { transfer unequips; wear any; }
dice seed 3 5;
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "save:dexterity" = 4; stat "bonus:checks" = 0; }
condition type "state:restrained" { instances coexist; modifier "save:dexterity" stat "save:dexterity" roll disadvantage; }
`
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": world, "explain": []map[string]any{{"character": "char:a", "stat": "save:dexterity"}}})
	if !g.OK || g.Checks != nil || g.Explain[0].Roll != "" {
		t.Fatalf("genesis: %+v", g)
	}
	actions := `apply condition "char:a:net" of "state:restrained" to "char:a";
check "char:a" stat "save:dexterity" die 20 against 15 faces 18 9;
check "char:a" stat "save:dexterity" die 20 against 15;`
	_, r := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": actions, "actor": actor, "request": "host:1", "explain": []map[string]any{{"character": "char:a", "stat": "save:dexterity"}}})
	if !r.OK || len(r.Checks) != 2 || r.Explain[0].Roll != core.Disadvantage {
		t.Fatalf("checks: ok %v error %q %+v", r.OK, r.Error, r)
	}
	player, engine := r.Checks[0], r.Checks[1]
	if player.Roll != core.Disadvantage || player.Kept != 9 || *player.Total != 13 || *player.Success || *player.Margin != -2 || player.Drawn {
		t.Fatalf("player faces: %+v", player)
	}
	if !engine.Drawn || len(engine.Faces) != 2 || *engine.Total != engine.Kept+4 {
		t.Fatalf("engine roll: %+v", engine)
	}
	// A historical retry reports the same checks from the original receipt.
	_, retry := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": actions, "actor": actor, "request": "host:1"})
	if !retry.OK || !retry.Historical || !reflect.DeepEqual(retry.Checks, r.Checks) {
		t.Fatalf("retry: %+v", retry)
	}
}

func TestDamageChecks(t *testing.T) {
	world := `rules { transfer unequips; wear any; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "save:constitution" = 2; resource "hp" = 30 min 0 max 30; }
character "char:b" named "Bo" at "loc:hall" { stat "defense" = 10; }
condition type "state:concentration" { instances replace; }
condition type "spell:shield-of-faith" { instances coexist; modifier "ac" stat "defense" add 2; }
condition_check "state:concentration" after_damage resource "hp" stat "save:constitution" sides 20 minimum 10 divide 2 maximum 30;
`
	actions := `apply condition "char:a:focus" of "state:concentration" to "char:a";
apply condition "char:b:sof" of "spell:shield-of-faith" to "char:b" anchor "char:a:focus";
damage "char:a" resource "hp" by 6 faces "char:a:focus" 12;
damage "char:a" resource "hp" by 6 faces "char:a:focus" 3;`
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, r := invoke(t, map[string]any{"world": world, "actions": actions, "actor": actor, "request": "host:1"})
	if !r.OK || len(r.Checks) != 2 || len(r.EndedConditions) != 2 {
		t.Fatalf("ok %v error %q %+v", r.OK, r.Error, r)
	}
	held, failed := r.Checks[0], r.Checks[1]
	if *held.Condition != "char:a:focus" || held.Damage != 6 || *held.Intent.DC != 10 || *held.Total != 14 || !*held.Success || *failed.Total != 5 || *failed.Success {
		t.Fatalf("checks: %+v", r.Checks)
	}
	sof, focus := r.EndedConditions[0], r.EndedConditions[1]
	if sof.Instance != "char:b:sof" || sof.Reason != "anchor" || *sof.By != "char:a:focus" || focus.Instance != "char:a:focus" || focus.Reason != "check" || focus.By != nil {
		t.Fatalf("ended: %+v", r.EndedConditions)
	}
	// Without faces or dice the refusal names the instance and what it needs.
	_, r = invoke(t, map[string]any{"world": world, "actions": `apply condition "char:a:focus" of "state:concentration" to "char:a";
damage "char:a" resource "hp" by 6;`, "actor": actor, "request": "host:1"})
	if r.OK || r.Fault == nil || r.Fault.Code != "E_ROLL" || r.Fault.Expected != "1 face (normal) or a world random source" {
		t.Fatalf("no source: %+v", r)
	}
}

// An XP award: the receipt names the new base, checkpoint continuation replays
// it, and a host that rebuilds per call declares that base at the next genesis.
func TestStatAdjustHostFlow(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	world := func(xp string) string {
		return `rules { transfer unequips; wear any; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "level" = 1; stat "xp" = ` + xp + `; stat "xp:pending" = 0; }
equipment "nql-equipment-v1" {
 derive stat "xp:pending" { term stat "xp" offset 0 divide 1 step 0 = 1 step 300 = 2; term stat "level" offset 0 divide 1 step 1 = -1 step 2 = -2; }
}
`
	}
	stats := func(r output) map[core.StatID][2]int64 {
		out := map[core.StatID][2]int64{}
		for _, s := range r.Status[0].Stats {
			out[s.Stat] = [2]int64{s.Base, s.Effective}
		}
		return out
	}
	award := map[string]any{"world": world("250"), "actions": `adjust "char:a" stat "xp" by 50;`, "actor": actor, "request": "host:xp", "status": []string{"char:a"}}
	_, r := invoke(t, award)
	if !r.OK || len(r.Receipt.Events) != 1 || r.Receipt.Events[0].Kind != core.StatAdjusted || *r.Receipt.Events[0].StatAdjustment != (core.StatAdjustment{Character: "char:a", Stat: "xp", Amount: 50, Before: 250, After: 300}) {
		t.Fatalf("award: ok %v error %q %+v", r.OK, r.Error, r.Receipt)
	}
	if s := stats(r); s["xp"] != [2]int64{300, 300} || s["xp:pending"] != [2]int64{0, 1} {
		t.Fatalf("award: %+v", s)
	}
	// Continuation replays the award, and a retry is historical.
	_, c := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `adjust "char:a" stat "xp" by -50;`, "actor": actor, "request": "host:fix", "status": []string{"char:a"}})
	if s := stats(c); !c.OK || c.Revision != 2 || s["xp"] != [2]int64{250, 250} || s["xp:pending"][1] != 0 {
		t.Fatalf("continue: ok %v error %q %+v", c.OK, c.Error, c.Status)
	}
	delete(award, "world")
	award["checkpoint"] = checkpointDoc(t, c.Checkpoint)
	_, retry := invoke(t, award)
	if !retry.OK || !retry.Historical || !reflect.DeepEqual(retry.Receipt, r.Receipt) {
		t.Fatalf("retry: ok %v historical %v error %q", retry.OK, retry.Historical, retry.Error)
	}
	// A stateless genesis declares the stored base.
	_, g := invoke(t, map[string]any{"world": world("300"), "status": []string{"char:a"}})
	if s := stats(g); !g.OK || s["xp:pending"] != [2]int64{0, 1} {
		t.Fatalf("genesis: ok %v error %q %+v", g.OK, g.Error, s)
	}
}

func TestAmmunitionHostFlow(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	world := func(arrows string) string {
		return `rules { transfer unequips; wear any; recovery 1 of 2; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { }
item "item:arrows" named "Arrows" { owner "char:a"; custody character "char:a"; ` + arrows + ` }
`
	}
	at := []map[string]any{{"kind": "character", "id": "char:a"}}
	arrows := func(r output) core.Item {
		if len(r.ItemsAt) != 1 || len(r.ItemsAt[0].Items) != 1 {
			t.Fatalf("items_at: %+v", r.ItemsAt)
		}
		return r.ItemsAt[0].Items[0]
	}
	shoot := map[string]any{"world": world("quantity 20;"), "actions": `consume "item:arrows" from "char:a" by 5 recoverable;`, "actor": actor, "request": "host:shoot", "items_at": at}
	_, r := invoke(t, shoot)
	if !r.OK || len(r.Receipt.Events) != 1 || !r.Receipt.Events[0].Consumption.Recoverable {
		t.Fatalf("shoot: ok %v error %q %+v", r.OK, r.Error, r.Receipt)
	}
	if i := arrows(r); *i.Quantity != 15 || i.Recoverable != 5 {
		t.Fatalf("shoot: %+v", i)
	}
	// Continuation recovers half of 5, and a retry is historical.
	_, c := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `recover "item:arrows" from "char:a";`, "actor": actor, "request": "host:gather", "items_at": at})
	if i := arrows(c); !c.OK || c.Revision != 2 || c.Receipt.Events[0].Kind != core.ItemRecovered || c.Receipt.Events[0].ItemRecovery.Allowed != 2 || *i.Quantity != 17 || i.Recoverable != 0 {
		t.Fatalf("gather: ok %v error %q %+v", c.OK, c.Error, c.Receipt)
	}
	delete(shoot, "world")
	shoot["checkpoint"] = checkpointDoc(t, c.Checkpoint)
	_, retry := invoke(t, shoot)
	if !retry.OK || !retry.Historical || !reflect.DeepEqual(retry.Receipt, r.Receipt) {
		t.Fatalf("retry: ok %v historical %v error %q", retry.OK, retry.Historical, retry.Error)
	}
	// A stateless genesis declares the stored count and recovers the same.
	_, g := invoke(t, map[string]any{"world": world("quantity 15; recoverable 5;"), "actions": `recover "item:arrows" from "char:a";`, "actor": actor, "request": "host:gather", "items_at": at})
	if i := arrows(g); !g.OK || !reflect.DeepEqual(g.Receipt.Events[0].ItemRecovery, c.Receipt.Events[0].ItemRecovery) || *i.Quantity != 17 || i.Recoverable != 0 {
		t.Fatalf("genesis: ok %v error %q %+v", g.OK, g.Error, g.Receipt)
	}
	// Nothing spent: the recovery is refused and nothing changes.
	_, n := invoke(t, map[string]any{"world": world("quantity 15;"), "actions": `recover "item:arrows" from "char:a";`, "actor": actor, "request": "host:none"})
	if n.OK || n.Fault == nil || n.Fault.Code != "E_QUANTITY" || n.Fault.Field != "recoverable" {
		t.Fatalf("none: ok %v %+v", n.OK, n.Fault)
	}
}

// A world past the old 1 MiB genesis record: 10,000 stored items in a chest,
// as a host declares storage. A world call with a retrieval and a checkpoint
// replay with a consumption both work; a world source over 16 MiB and an
// action source over 1 MiB are refused at compile (docs/WORLD_SIZE.md).
func TestWorldSizeHostFlow(t *testing.T) {
	var world strings.Builder
	world.WriteString(testWorld)
	world.WriteString(`item "container:chest" named "Chest" { container true; custody location "loc:hall"; }` + "\n")
	for n := 0; n < 10000; n++ {
		world.WriteString(`item "item:gem:` + strconv.Itoa(n) + `" named "Gemstone" { owner "char:a"; custody item "container:chest"; quantity 2; }` + "\n")
	}
	actor := map[string]any{"id": "char:a"}
	_, first := invoke(t, map[string]any{"world": world.String(), "actions": `place "item:gem:7" from item "container:chest" to character "char:a" by "char:a";`, "actor": actor, "request": "r1"})
	if !first.OK || first.Phase != "executed" {
		t.Fatalf("genesis: ok %v phase %s error %q fault %+v", first.OK, first.Phase, first.Error, first.Fault)
	}
	genesis, err := json.Marshal(first.Checkpoint.Genesis)
	if err != nil || len(genesis) <= core.MaxArchiveRecordBytes {
		t.Fatal("fixture genesis is not above the batch record bound", len(genesis), err)
	}
	_, second := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, first.Checkpoint), "actions": `consume "item:gem:7" from "char:a" by 1;`, "actor": actor, "request": "r2"})
	if !second.OK || second.Revision != 2 || second.Receipt.Events[0].Consumption.After != 1 {
		t.Fatalf("replay: ok %v phase %s error %q", second.OK, second.Phase, second.Error)
	}
	sized := func(n int) string { return testWorld + "//" + strings.Repeat("x", n-len(testWorld)-2) }
	if _, edge := invoke(t, map[string]any{"world": sized(16 << 20)}); !edge.OK {
		t.Fatalf("world at the bound: phase %s error %q", edge.Phase, edge.Error)
	}
	_, big := invoke(t, map[string]any{"world": sized(16<<20 + 1)})
	if big.OK || big.Phase != "compile" || big.Error != "world source exceeds 16777216 bytes" {
		t.Fatalf("oversized world: ok %v phase %s error %q", big.OK, big.Phase, big.Error)
	}
	_, long := invoke(t, map[string]any{"world": testWorld, "actions": "//" + strings.Repeat("x", 1<<20), "actor": actor, "request": "r3"})
	if long.OK || long.Phase != "compile" || long.Error != "action source exceeds 1048576 bytes" {
		t.Fatalf("oversized actions: ok %v phase %s error %q", long.OK, long.Phase, long.Error)
	}
}
