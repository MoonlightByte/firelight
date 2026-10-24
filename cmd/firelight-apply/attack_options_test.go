package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// A Fighter holding a Longsword with a Javelin carried.
const optionsWorld = `rules { transfer unequips; wear any; }
location "loc:hall" named "Hall";
character "char:a" named "Ada" at "loc:hall" { stat "strength" = 16; stat "defense" = 10; }
equipment "nql-equipment-v1" {
 slot "hand" capacity 2;
 definition "gear:longsword" named "Longsword" { description "Versatile."; default "held";
  mode "held" { occupy "hand" by 1;
   attack "melee" named "Longsword" { hit { constant 2; term stat "strength" offset -10 divide 2; }
    damage "slashing" dice 1 sides 8 { constant 0; term stat "strength" offset -10 divide 2; } melee reach 5 stat "strength"; } }
  mode "two-handed" { occupy "hand" by 2;
   attack "melee" named "Longsword, two-handed" { hit { constant 2; term stat "strength" offset -10 divide 2; }
    damage "slashing" dice 1 sides 10 { constant 0; term stat "strength" offset -10 divide 2; } melee reach 5 stat "strength"; } } }
 definition "gear:javelin" named "Javelin" { description "Thrown."; default "held";
  mode "held" { occupy "hand" by 1;
   attack "thrown" named "Javelin, thrown" { hit { constant 2; term stat "strength" offset -10 divide 2; }
    damage "piercing" dice 1 sides 6 { constant 0; term stat "strength" offset -10 divide 2; } thrown normal 30 maximum 120 stat "strength"; } } }
}
item "item:sword" named "Longsword" { owner "char:a"; wearer "char:a"; definition "gear:longsword"; mode "held"; }
item "item:javelin" named "Javelin" { owner "char:a"; custody character "char:a"; definition "gear:javelin"; }
`

func rawInvoke(t *testing.T, doc map[string]any) (int, string) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := run(bytes.NewReader(raw), &out)
	return code, out.String()
}

func TestAttackOptionsRequest(t *testing.T) {
	_, r := invoke(t, map[string]any{"world": optionsWorld, "equipment": []string{"char:a"}, "attack_options": []string{"char:a"}})
	if !r.OK || len(r.AttackOptions) != 1 || len(r.Equipment) != 1 {
		t.Fatalf("ok %v error %q options %d", r.OK, r.Error, len(r.AttackOptions))
	}
	v := r.AttackOptions[0]
	if v.Character.ID != "char:a" || len(v.Options) != 3 {
		t.Fatalf("%+v", v)
	}
	for k, want := range []struct {
		item, mode string
		worn       bool
		hit, sides int64
	}{{"item:javelin", "held", false, 5, 6}, {"item:sword", "held", true, 5, 8}, {"item:sword", "two-handed", false, 5, 10}} {
		o := v.Options[k]
		if string(o.Item) != want.item || string(o.Mode) != want.mode || o.Worn != want.worn || len(o.Grants) != 1 || o.Grants[0].ToHit != want.hit || o.Grants[0].DamageSides != want.sides {
			t.Fatalf("option %d: %+v", k, o)
		}
	}
	// The view follows equipment in the response.
	_, body := rawInvoke(t, map[string]any{"world": optionsWorld, "equipment": []string{"char:a"}, "attack_options": []string{"char:a"}})
	if e, a := strings.Index(body, `"equipment":`), strings.Index(body, `"attack_options":`); e < 0 || a < e {
		t.Fatal("order", e, a)
	}
}

// Without the field the response carries no attack_options key.
func TestAttackOptionsAbsent(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	for _, doc := range []map[string]any{
		{"world": optionsWorld},
		{"world": optionsWorld, "equipment": []string{"char:a"}, "status": []string{"char:a"}},
		{"world": testWorld, "actions": `unequip "item:scale:1";`, "actor": actor, "request": "host:1", "equipment": []string{"char:a"}},
		{"world": testWorld, "equipment": []string{"char:nope"}},
	} {
		if _, body := rawInvoke(t, doc); strings.Contains(body, "attack_options") {
			t.Fatal(body)
		}
	}
	if code, body := rawInvoke(t, map[string]any{"world": optionsWorld, "attack_option": []string{"char:a"}}); code != 2 || !strings.Contains(body, `"phase":"input"`) {
		t.Fatal(code, body)
	}
}

// After a commit a failing view keeps the receipt and names the view.
func TestAttackOptionsViewError(t *testing.T) {
	actor := map[string]any{"kind": "character", "id": "char:a"}
	_, g := invoke(t, map[string]any{"world": optionsWorld})
	_, r := invoke(t, map[string]any{"checkpoint": checkpointDoc(t, g.Checkpoint), "actions": `unequip "item:sword";`,
		"actor": actor, "request": "host:1", "equipment": []string{"char:a"}, "attack_options": []string{"char:a", "char:nope"}})
	if !r.OK || r.Receipt == nil || r.ViewError == nil || r.ViewError.View != "attack_options" || r.ViewError.Fault == nil {
		t.Fatalf("ok %v receipt %v view error %+v", r.OK, r.Receipt != nil, r.ViewError)
	}
	if len(r.Equipment) != 1 || len(r.AttackOptions) != 1 || r.AttackOptions[0].Revision != 1 {
		t.Fatalf("views before the failure: equipment %d options %d", len(r.Equipment), len(r.AttackOptions))
	}
	// The unworn sword is now carried: both modes listed, neither worn.
	for _, o := range r.AttackOptions[0].Options {
		if o.Worn {
			t.Fatalf("%+v", o)
		}
	}
	// Without a transaction the failing view fails the response.
	_, read := invoke(t, map[string]any{"world": optionsWorld, "attack_options": []string{"char:nope"}})
	if read.OK || read.Phase != "attack_options" || read.Fault == nil {
		t.Fatalf("read-only: ok %v phase %s", read.OK, read.Phase)
	}
}
