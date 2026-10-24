package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/livestate"
)

// clockWorld is timedWorld without its declared condition: a clock at 1,000
// seconds, a hall and Ada.
var clockWorld = strings.Replace(timedWorld, `condition "effect:sof:a" of "spell:shield-of-faith" to "char:a" ticks 10;
`, "", 1)

// reseal gives d the checksum the engine would write, as a test of an older
// version needs.
func reseal(t *testing.T, d livestate.Document) livestate.Document {
	t.Helper()
	d.Checksum = ""
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	d.Checksum = "sha256:" + hex.EncodeToString(sum[:])
	return d
}

// The document holds the clock: the engine is its only writer, and the
// source's tick is ignored once the document holds one.
func TestLiveStateClock(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": clockWorld, "new_live_state": true})
	d := g.LiveState
	if !g.OK || d == nil || d.Version != 5 || d.Clock == nil || d.Clock.Unit != "second" || d.Clock.Tick != 1000 {
		t.Fatalf("start: ok %v error %q live %+v", g.OK, g.Error, d)
	}

	// A wait is kept, and its retry does not move the clock again.
	wait := map[string]any{"world": clockWorld, "live_state": liveDoc(t, d), "actions": `advance time by 500;`, "actor": liveActor, "request": "host:wait"}
	_, r := invoke(t, wait)
	if !r.OK || r.LiveState.Clock.Tick != 1500 || r.LiveState.Revision != 1 || len(r.LiveState.Requests) != 1 || r.LiveState.Requests[0].ID != "host:wait" {
		t.Fatalf("wait: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	d = r.LiveState
	wait["live_state"] = liveDoc(t, d)
	_, again := invoke(t, wait)
	if !again.OK || !again.Historical || again.Receipt != nil || !reflect.DeepEqual(again.LiveState, d) {
		t.Fatalf("retry: ok %v historical %v live %+v", again.OK, again.Historical, again.LiveState)
	}

	// A host that declares the clock at 0 every call, or at any other tick,
	// runs at the document's.
	for _, at := range []string{"at 0;", "at 99999;"} {
		world := strings.Replace(clockWorld, "at 1000;", at, 1)
		_, r = invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, d), "actions": `advance time by 1;`, "actor": liveActor, "request": "host:tick " + at})
		if !r.OK || r.Receipt == nil || r.LiveState.Clock.Tick != 1501 {
			t.Fatalf("%s: ok %v error %q live %+v", at, r.OK, r.Error, r.LiveState)
		}
		for _, e := range r.Receipt.Events {
			if e.Time != nil && (e.Time.Before != 1500 || e.Time.After != 1501) {
				t.Fatalf("%s: time %+v", at, e.Time)
			}
		}
	}
	// A view alone changes nothing.
	_, r = invoke(t, map[string]any{"world": strings.Replace(clockWorld, "at 1000;", "at 0;", 1), "live_state": liveDoc(t, d)})
	if !r.OK || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("view: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}

	// A condition the source declares for ticks 10 runs 10 ticks from the
	// document's tick, whatever tick the source counted from.
	timed := strings.Replace(timedWorld, "at 1000;", "at 0;", 1)
	_, r = invoke(t, map[string]any{"world": timed, "live_state": liveDoc(t, d), "actions": `advance time by 9;`, "actor": liveActor, "request": "host:nine"})
	if !r.OK || r.EndedConditions != nil || r.LiveState.Clock.Tick != 1509 {
		t.Fatalf("nine: ok %v error %q ended %+v", r.OK, r.Error, r.EndedConditions)
	}
	_, r = invoke(t, map[string]any{"world": timed, "live_state": liveDoc(t, d), "actions": `advance time by 10;`, "actor": liveActor, "request": "host:ten"})
	if !r.OK || len(r.EndedConditions) != 1 || r.EndedConditions[0].Instance != "effect:sof:a" || *r.EndedConditions[0].Deadline != 1510 {
		t.Fatalf("ten: ok %v error %q ended %+v", r.OK, r.Error, r.EndedConditions)
	}

	// Ticks in another unit are refused, never converted.
	_, r = invoke(t, map[string]any{"world": strings.Replace(clockWorld, `clock "second"`, `clock "minute"`, 1), "live_state": liveDoc(t, d)})
	if r.OK || r.Phase != "live_state" || r.Fault == nil || r.Fault.Code != "E_LIVE_STATE" || r.Fault.Field != "clock" || r.Fault.Expected != "unit second" || r.Fault.Actual != "unit minute" || r.LiveState != nil {
		t.Fatalf("unit: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}

	// A world without a clock holds the document's, unchanged.
	clockless := strings.Replace(clockWorld, "clock \"second\" at 1000;\n", "", 1)
	_, r = invoke(t, map[string]any{"world": clockless, "live_state": liveDoc(t, d)})
	if !r.OK || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("clockless: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	// Without a clock anywhere, there is none, and time cannot pass.
	_, n := invoke(t, map[string]any{"world": clockless, "new_live_state": true})
	if !n.OK || n.LiveState.Clock != nil {
		t.Fatalf("no clock: ok %v live %+v", n.OK, n.LiveState)
	}

	// A version 3 document takes the source's clock once, then holds it.
	old := *n.LiveState
	old.Version = 3
	old = reseal(t, old)
	_, r = invoke(t, map[string]any{"world": strings.Replace(clockWorld, "at 1000;", "at 28800;", 1), "live_state": liveDoc(t, &old), "actions": `advance time by 60;`, "actor": liveActor, "request": "host:first"})
	if !r.OK || r.LiveState.Version != 5 || r.LiveState.Clock.Tick != 28860 {
		t.Fatalf("upgrade: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	_, r = invoke(t, map[string]any{"world": clockWorld, "live_state": liveDoc(t, r.LiveState), "actions": `advance time by 60;`, "actor": liveActor, "request": "host:second"})
	if !r.OK || r.LiveState.Clock.Tick != 28920 {
		t.Fatalf("held after upgrade: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	// An engine before version 4 never wrote a clock: a version 3 document
	// with one is not a document this engine wrote.
	bad := *d
	bad.Version = 3
	bad = reseal(t, bad)
	_, r = invoke(t, map[string]any{"world": clockWorld, "live_state": liveDoc(t, &bad)})
	if r.OK || r.Phase != "live_state" || r.Fault == nil || r.Fault.Field != "version" || r.Fault.Actual != "3" {
		t.Fatalf("version 3 with a clock: ok %v phase %s fault %+v", r.OK, r.Phase, r.Fault)
	}
}

// A host that redeclares each timed effect with the ticks it has left, from
// the document's tick, ends it at the same tick and in the same request as a
// kept world, whatever tick its source declares.
func TestLiveStateTimedEffectMatchesKeptWorld(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": timedWorld})
	cp := g.Checkpoint
	_, n := invoke(t, map[string]any{"world": timedWorld, "new_live_state": true})
	d := n.LiveState
	const expires = 1010
	for i, step := range []int{3, 4, 5, 2} {
		id := "host:wait " + strings.Repeat("i", i+1)
		actions := "advance time by " + strconv.Itoa(step) + ";"
		_, kept := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, cp), "actions": actions, "actor": liveActor, "request": id})
		world := strings.Replace(clockWorld, "at 1000;", "at "+strconv.Itoa(i*7919)+";", 1)
		if left := expires - int(d.Clock.Tick); left > 0 {
			world += `condition "effect:sof:a" of "spell:shield-of-faith" to "char:a" ticks ` + strconv.Itoa(left) + ";\n"
		}
		_, live := invoke(t, map[string]any{"world": world, "live_state": liveDoc(t, d), "actions": actions, "actor": liveActor, "request": id, "status": []string{"char:a"}})
		if !kept.OK || !live.OK || !reflect.DeepEqual(live.EndedConditions, kept.EndedConditions) {
			t.Fatalf("step %d: kept ok %v %q ended %+v; live ok %v %q ended %+v", i, kept.OK, kept.Error, kept.EndedConditions, live.OK, live.Error, live.EndedConditions)
		}
		// The effect ends in the third request, at its own tick.
		if ended := len(live.EndedConditions) == 1; ended != (i == 2) || ended && *live.EndedConditions[0].Deadline != expires {
			t.Fatalf("step %d: ended %+v", i, live.EndedConditions)
		}
		// Status shows the effect applied at the held tick and ending at its
		// own: the remaining ticks are what a host reads.
		if i < 2 {
			c := live.Status[0].Conditions
			if len(c) != 1 || c[0].ClockLifetime.ExpiresTick != expires || c[0].ClockLifetime.AppliedTick != d.Clock.Tick {
				t.Fatalf("step %d: status %+v", i, c)
			}
		}
		cp, d = kept.Checkpoint, live.LiveState
	}
	if d.Clock.Tick != 1014 {
		t.Fatalf("clock %+v", d.Clock)
	}
}
