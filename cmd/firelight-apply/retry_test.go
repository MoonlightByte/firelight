package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

var retryWorld = occupantWorld + `item "item:rope" named "Rope" { owner "char:a"; custody character "char:a"; quantity 5; }
item type "itemdef:torch" named "Torch" { kind "gear"; }
`

// An exact retry of a committed request that creates something is answered
// with its original receipt, right after the commit and after a later one.
// The same ID with other content is refused as reuse; the same actions under
// a new ID are refused at compile, since the IDs they create are taken.
func TestCreatingRequestRetries(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	for _, tc := range []struct{ name, actions, changed, created string }{
		{"item", `create item "item:torch" named "Torch" { owner "char:a"; quantity 3; };`,
			`create item "item:torch" named "Torch" { owner "char:a"; quantity 4; };`, `"item:torch"`},
		{"catalog item", `create item "item:torch" from "itemdef:torch" { owner "char:a"; quantity 3; };`,
			`create item "item:torch" from "itemdef:torch" { owner "char:a"; quantity 4; };`, `"item:torch"`},
		{"split", `split "item:rope" quantity 2 into "item:rope:2" by "char:a";`,
			`split "item:rope" quantity 3 into "item:rope:2" by "char:a";`, `"item:rope:2"`},
		{"character", `create character "char:b" named "Bo" at "loc:camp" { };`,
			`create character "char:b" named "Bea" at "loc:camp" { };`, `"char:b"`},
		{"occupant", `create occupant "occ:peddler" named "Peddler" at "loc:camp" { person; };`,
			`create occupant "occ:peddler" named "Peddler" at "loc:ford" { person; };`, `"occ:peddler"`},
		{"all, and used", `create character "char:b" named "Bo" at "loc:camp" { }; create item "item:torch" named "Torch" { owner "char:a"; quantity 3; }; transfer "item:torch" from "char:a" to "char:b"; split "item:rope" quantity 2 into "item:rope:2" by "char:a"; create occupant "occ:peddler" named "Peddler" at "loc:camp" { person; }; set occupant "occ:peddler" attitude friendly;`,
			`create character "char:b" named "Bo" at "loc:camp" { }; create item "item:torch" named "Torch" { owner "char:a"; quantity 3; };`, `"char:b"`},
	} {
		_, g := invoke(t, map[string]any{"world": retryWorld})
		make := map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": tc.actions, "actor": actor, "request": "host:make"}
		_, r := invoke(t, make)
		if !r.OK || r.Phase != "executed" || r.Revision != 1 || r.Historical || r.Receipt == nil {
			t.Fatalf("%s: commit: ok %v phase %s error %q fault %+v diagnostics %+v", tc.name, r.OK, r.Phase, r.Error, r.Fault, r.Diagnostics)
		}
		retry := func(step string, cp map[string]any, revision core.Revision) {
			t.Helper()
			make["checkpoint"] = cp
			_, again := invoke(t, make)
			if !again.OK || again.Phase != "executed" || !again.Historical || again.Revision != revision || !reflect.DeepEqual(again.Receipt, r.Receipt) {
				t.Fatalf("%s: %s: ok %v phase %s historical %v rev %d error %q fault %+v diagnostics %+v", tc.name, step, again.OK, again.Phase, again.Historical, again.Revision, again.Error, again.Fault, again.Diagnostics)
			}
		}
		retry("retry", checkpointDoc(t, r.Checkpoint), 1)
		_, w := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, r.Checkpoint), "actions": `advance time by 5;`, "actor": actor, "request": "host:wait"})
		if !w.OK || w.Revision != 2 {
			t.Fatalf("%s: wait: ok %v error %q", tc.name, w.OK, w.Error)
		}
		later := checkpointDoc(t, w.Checkpoint)
		retry("retry after a later request", later, 2)

		_, reuse := invoke(t, map[string]any{"checkpoint": later, "actions": tc.changed, "actor": actor, "request": "host:make"})
		if reuse.OK || reuse.Phase != "execute" || reuse.Fault == nil || reuse.Fault.Code != "E_REQUEST_REUSE" || reuse.Revision != 2 {
			t.Fatalf("%s: reuse: ok %v phase %s fault %+v diagnostics %+v", tc.name, reuse.OK, reuse.Phase, reuse.Fault, reuse.Diagnostics)
		}
		_, fresh := invoke(t, map[string]any{"checkpoint": later, "actions": tc.actions, "actor": actor, "request": "host:make-again"})
		if fresh.OK || fresh.Phase != "compile" || len(fresh.Diagnostics) != 1 || fresh.Diagnostics[0].Code != "E_DUPLICATE" || fresh.Diagnostics[0].Column != strings.Index(tc.actions, tc.created)+1 {
			t.Fatalf("%s: new ID: ok %v phase %s diagnostics %+v", tc.name, fresh.OK, fresh.Phase, fresh.Diagnostics)
		}
	}
}

// On the live_state path the host writes what was created into its source
// and keeps the returned document. A retry is answered as already applied.
func TestCreatingRequestRetriesWithLiveState(t *testing.T) {
	_, g := invoke(t, map[string]any{"world": retryWorld, "new_live_state": true})
	actions := `create occupant "occ:peddler" named "Peddler" at "loc:camp" { person; }; create item "item:torch" named "Torch" { owner "char:a"; quantity 3; };`
	make := map[string]any{"world": retryWorld, "live_state": liveDoc(t, g.LiveState), "actions": actions, "actor": liveActor, "request": "host:make", "locations": []string{"loc:camp"}}
	_, r := invoke(t, make)
	if !r.OK || r.Phase != "executed" || r.Receipt == nil || r.LiveState == nil || len(r.LiveState.Requests) != 1 {
		t.Fatalf("commit: ok %v phase %s error %q fault %+v diagnostics %+v", r.OK, r.Phase, r.Error, r.Fault, r.Diagnostics)
	}
	d := r.LiveState
	written := retryWorld + `item "item:torch" named "Torch" { owner "char:a"; quantity 3; }
`
	make["world"], make["live_state"] = written, liveDoc(t, d)
	_, again := invoke(t, make)
	if !again.OK || again.Phase != "executed" || !again.Historical || again.Receipt != nil || !reflect.DeepEqual(again.LiveState, d) {
		t.Fatalf("retry: ok %v phase %s historical %v error %q fault %+v diagnostics %+v", again.OK, again.Phase, again.Historical, again.Error, again.Fault, again.Diagnostics)
	}
	if present := again.Locations[0].Present; len(present) != 2 || present[1].ID != "occ:peddler" {
		t.Fatalf("retry view %+v", again.Locations)
	}

	make["actions"] = strings.Replace(actions, "quantity 3", "quantity 4", 1)
	_, reuse := invoke(t, make)
	if reuse.OK || reuse.Phase != "execute" || reuse.Fault == nil || reuse.Fault.Code != "E_REQUEST_REUSE" || reuse.LiveState != nil {
		t.Fatalf("reuse: ok %v phase %s fault %+v diagnostics %+v", reuse.OK, reuse.Phase, reuse.Fault, reuse.Diagnostics)
	}
	make["actions"], make["request"] = actions, "host:make-again"
	_, fresh := invoke(t, make)
	if fresh.OK || fresh.Phase != "compile" || len(fresh.Diagnostics) != 1 || fresh.Diagnostics[0].Code != "E_DUPLICATE" || fresh.Diagnostics[0].Column != strings.Index(actions, `"occ:peddler"`)+1 {
		t.Fatalf("new ID: ok %v phase %s diagnostics %+v", fresh.OK, fresh.Phase, fresh.Diagnostics)
	}

	// A request that only creates an item changes nothing the document
	// holds, so it is not kept: retried after the host wrote the item, it is
	// refused at compile.
	lamp := `create item "item:lamp" named "Lamp" { owner "char:a"; };`
	_, r = invoke(t, map[string]any{"world": written, "live_state": liveDoc(t, d), "actions": lamp, "actor": liveActor, "request": "host:lamp"})
	if !r.OK || r.Receipt == nil || !reflect.DeepEqual(r.LiveState, d) {
		t.Fatalf("lamp: ok %v error %q live %+v", r.OK, r.Error, r.LiveState)
	}
	_, r = invoke(t, map[string]any{"world": written + `item "item:lamp" named "Lamp" { owner "char:a"; }
`, "live_state": liveDoc(t, d), "actions": lamp, "actor": liveActor, "request": "host:lamp"})
	if r.OK || r.Phase != "compile" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "E_DUPLICATE" || r.Diagnostics[0].Column != 13 || r.LiveState != nil {
		t.Fatalf("lamp retry: ok %v phase %s diagnostics %+v", r.OK, r.Phase, r.Diagnostics)
	}
}
