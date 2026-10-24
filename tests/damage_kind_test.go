package tests

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

// damaged is the operation's DamageApplied record.
func damaged(t testing.TB, b core.Batch) *core.ResourceChange {
	t.Helper()
	e := b.Events[len(b.Events)-1]
	if e.Kind != core.DamageApplied || e.Resource == nil {
		t.Fatalf("%v", eventKinds(b))
	}
	return e.Resource
}

func TestDamageKindLaw(t *testing.T) {
	type treat = core.AttackTreatment
	own := func(t string) treat { return treat{Character: dummy, Treatment: t} }
	held := func(id core.ConditionInstanceID, t string) treat { return treat{Condition: id, Treatment: t} }
	for _, tc := range []struct {
		name, traits string
		conditions   []string
		amount       int64
		kind         string
		want         int64
		treatments   []treat
	}{
		{"none", "", nil, 7, "slashing", 7, []treat{}},
		{"resistance rounds down", `resist "slashing";`, nil, 7, "slashing", 3, []treat{own("resistance")}},
		{"resistance on 1", `resist "slashing";`, nil, 1, "slashing", 0, []treat{own("resistance")}},
		{"vulnerability", `vulnerable "slashing";`, nil, 7, "slashing", 14, []treat{own("vulnerability")}},
		{"immunity", `immune "slashing";`, nil, 7, "slashing", 0, []treat{own("immunity")}},
		{"resistance then vulnerability", `vulnerable "slashing"; resist "slashing";`, nil, 7, "slashing", 6, []treat{own("resistance"), own("vulnerability")}},
		{"immunity over both", `immune "slashing"; vulnerable "slashing"; resist "slashing";`, nil, 7, "slashing", 0, []treat{own("resistance"), own("vulnerability"), own("immunity")}},
		{"two resistances halve once", `resist "slashing";`, []string{"d:ward=spell:ward"}, 7, "slashing", 3, []treat{own("resistance"), held("d:ward", "resistance")}},
		{"another kind", `resist "piercing"; vulnerable "fire";`, nil, 7, "slashing", 7, []treat{}},
		{"exact bytes", `resist "slashing";`, nil, 7, "Slashing", 7, []treat{}},
		{"a Condition's resistance", "", []string{"d:ward=spell:ward"}, 7, "slashing", 3, []treat{held("d:ward", "resistance")}},
		{"own trait and Condition", `vulnerable "slashing";`, []string{"d:ward=spell:ward"}, 7, "slashing", 6, []treat{own("vulnerability"), held("d:ward", "resistance")}},
		{"Conditions by instance", "", []string{"d:z=spell:ward", "d:a=hex:frail", "d:m=spell:proof"}, 7, "slashing", 0, []treat{held("d:a", "vulnerability"), held("d:m", "immunity"), held("d:z", "resistance")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := traitWorld(t, tc.traits, tc.conditions...)
			b, err := attackRun(t, w, "r1", fmt.Sprintf("damage %q resource \"hp\" by %d kind %q;", dummy, tc.amount, tc.kind))
			if err != nil {
				t.Fatal(err)
			}
			r := damaged(t, b)
			want := &core.DamageTreated{Kind: tc.kind, Amount: tc.amount, Treatments: tc.treatments}
			if r.Requested != tc.want || !reflect.DeepEqual(r.Treated, want) || currentOf(w, dummy, "hp") != 100-tc.want || len(b.Events) != 1 {
				t.Fatalf("%+v %+v", r, r.Treated)
			}
		})
	}
}

// The mage resists fire and is immune to poison, holds 5 temporary hit
// points and concentration (DC the larger of 10 and half the damage).
const mage = "character \"char:mage\" named \"Mage\" at \"loc:valley/B03\" { stat \"ac\" = 12; stat \"save:constitution\" = 0;\n resource \"hp\" = 40 min 0 max 40; resource \"temp-hp\" = 5 min 0 max 5; buffer \"temp-hp\" before \"hp\";\n resist \"fire\"; immune \"poison\"; }\ncondition \"char:mage:concentration\" of \"state:concentration\" to \"char:mage\";\n"

func concentrating(w *core.World) bool {
	for _, c := range w.Snapshot().Blueprint.Conditions {
		if c.ID == "char:mage:concentration" {
			return true
		}
	}
	return false
}

func TestDamageKindOrder(t *testing.T) {
	// 30 fire resisted to 15: the buffer absorbs 5, hp loses 10, and the
	// check's DC is 10 (from 15, not 15 from 30), so 12 holds.
	_, w := attackWorld(t, mage)
	b, err := attackRun(t, w, "r1", `damage "char:mage" resource "hp" by 30 kind "fire" faces "char:mage:concentration" 12;`)
	if err != nil {
		t.Fatal(err)
	}
	r := damaged(t, b)
	if r.Requested != 15 || r.Buffer == nil || r.Buffer.Absorbed != 5 || r.After.Current != 30 || r.Treated.UnusedFaces != nil || !concentrating(w) {
		t.Fatalf("%+v %+v %v", r, r.Treated, eventKinds(b))
	}
	var dc int64
	for _, e := range b.Events {
		if e.StatCheck != nil && e.StatCheck.Intent.DC != nil {
			dc = *e.StatCheck.Intent.DC
		}
	}
	if dc != 10 {
		t.Fatalf("DC %d", dc)
	}
	// Treated to 0, by resistance on 1 or by immunity: one record, no buffer,
	// no check, the face unused and never refused.
	for i, damage := range []string{`by 1 kind "fire"`, `by 9 kind "poison"`} {
		b, err := attackRun(t, w, fmt.Sprint("z", i), `damage "char:mage" resource "hp" `+damage+` faces "char:mage:concentration" 3;`)
		if err != nil {
			t.Fatal(err)
		}
		r := damaged(t, b)
		if len(b.Events) != 1 || r.Requested != 0 || r.Buffer != nil || r.Before != r.After || currentOf(w, "char:mage", "temp-hp") != 0 || !concentrating(w) || !reflect.DeepEqual(r.Treated.UnusedFaces, []core.AttackCheckFace{{Condition: "char:mage:concentration", Face: 3}}) {
			t.Fatalf("%s: %+v %+v", damage, r, r.Treated)
		}
	}
	// A face for a Condition the target does not hold: a kindless damage
	// still refuses it, a kinded one records it.
	_, w = attackWorld(t, mage)
	if _, err := attackRun(t, w, "r1", `damage "char:mage" resource "hp" by 1 faces "char:ada:concentration" 3;`); err == nil || faultOf(t, err).Code != "E_ROLL" {
		t.Fatal(err)
	}
	b, err = attackRun(t, w, "r1", `damage "char:mage" resource "hp" by 1 kind "cold" faces "char:mage:concentration" 12 faces "char:ada:concentration" 3;`)
	if err != nil {
		t.Fatal(err)
	}
	if r := damaged(t, b); r.Requested != 1 || !concentrating(w) || !reflect.DeepEqual(r.Treated.UnusedFaces, []core.AttackCheckFace{{Condition: "char:ada:concentration", Face: 3}}) || len(b.Events) != 2 {
		t.Fatalf("%+v %v", r.Treated, eventKinds(b))
	}
	// With the world's dice and no faces (a host's check shape), a 0 draws nothing
	// and is not refused; a positive treated amount draws the check.
	_, w = attackWorld(t, "dice seed 20 25;\n"+mage)
	b, err = attackRun(t, w, "r1", `damage "char:mage" resource "hp" by 9 kind "poison";`)
	if err != nil || len(b.Events) != 1 || damaged(t, b).Requested != 0 || !concentrating(w) {
		t.Fatal(err, eventKinds(b))
	}
	b, err = attackRun(t, w, "r2", `damage "char:mage" resource "hp" by 30 kind "fire";`)
	if err != nil || damaged(t, b).Requested != 15 || len(b.Events) < 2 {
		t.Fatal(err, eventKinds(b))
	}
}

func TestDamageKindRefusals(t *testing.T) {
	b0, w := attackWorld(t, mage)
	rev := w.Revision()
	for _, tc := range []struct{ name, action, code, at string }{
		{"a kind on heal", `heal "char:mage" resource "hp" by 1 kind "fire";`, "E_PARSE", "kind"},
		{"a blank kind", `damage "char:mage" resource "hp" by 1 kind "";`, "E_ATTACK", ""},
		{"a kind with a control character", "damage \"char:mage\" resource \"hp\" by 1 kind \"fi\\tre\";", "E_ATTACK", "fi\tre"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := compiler.CompileActions("a.nql", tc.action, b0)
			if len(ds) != 1 || ds[0].Code != tc.code || !strings.HasPrefix(ds[0].Actual, shownAt(tc.code, tc.at)) {
				t.Fatalf("%+v", ds)
			}
		})
	}
	// In IR: a kind on any resource operation but damage, an invalid kind,
	// and a treated amount past int64.
	for _, tc := range []struct {
		name  string
		op    core.Operation
		code  string
		field string
	}{
		{"heal", core.Operation{Heal: &core.ResourceOp{Character: "char:mage", Resource: "hp", Amount: 1, Kind: "fire"}}, "E_TYPE", "kind"},
		{"spend", core.Operation{Spend: &core.ResourceOp{Character: "char:mage", Resource: "hp", Amount: 1, Kind: "fire"}}, "E_TYPE", "kind"},
		{"grant", core.Operation{Grant: &core.ResourceOp{Character: "char:mage", Resource: "temp-hp", Amount: 1, Kind: "fire"}}, "E_TYPE", "kind"},
		{"blank", core.Operation{Damage: &core.ResourceOp{Character: "char:mage", Resource: "hp", Amount: 1, Kind: " "}}, "E_ATTACK", "kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.Execute(core.Request{ID: "x", ExpectedRevision: rev, Actor: ada, Operations: []core.Operation{tc.op}})
			if f := faultOf(t, err); f.Code != tc.code || f.Field != tc.field {
				t.Fatal(f)
			}
		})
	}
	_, wv := attackWorld(t, "character \"char:glass\" named \"Glass\" at \"loc:valley/B03\" { stat \"ac\" = 1; resource \"hp\" = 9223372036854775807 min 0 max 9223372036854775807; vulnerable \"fire\"; }\n")
	if _, err := wv.Execute(core.Request{ID: "x", ExpectedRevision: wv.Revision(), Actor: ada, Operations: []core.Operation{{Damage: &core.ResourceOp{Character: "char:glass", Resource: "hp", Amount: math.MaxInt64, Kind: "fire"}}}}); faultOf(t, err).Code != "E_OVERFLOW" {
		t.Fatal(err)
	}
	if w.Revision() != rev || len(w.History()) != 0 {
		t.Fatal("a refusal left a trace")
	}
	// Next to a Battle, at the kind.
	battle, err := os.ReadFile("../examples/native_melee/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	bb, ds := compiler.CompileWorld("world.nql", string(battle))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, ds := compiler.CompileActions("a.nql", `damage "tess" resource "hp" by 1 kind "fire";`, bb); len(ds) != 1 || ds[0].Code != "E_ATTACK" || ds[0].Expected != "world without Battle" || !strings.HasPrefix(ds[0].Actual, `"fire"`) {
		t.Fatalf("%+v", ds)
	}
}

func TestDamageKindReplayAndDetachment(t *testing.T) {
	b0, w := attackWorld(t, "dice seed 20 25;\n"+mage)
	for i, a := range []string{
		`damage "char:mage" resource "hp" by 30 kind "fire" faces "char:mage:concentration" 12;`,
		`damage "char:mage" resource "hp" by 1 kind "fire" faces "char:mage:concentration" 3;`,
		`damage "char:mage" resource "hp" by 4 kind "poison";`,
		`damage "char:mage" resource "hp" by 3;`,
		`damage "char:mage" resource "hp" by 9 kind "fire";` + "\n" + `damage "char:mage" resource "hp" by 2 kind "cold" faces "char:mage:concentration" 15 faces "char:mage:concentration" 4;`,
	} {
		if _, err := attackRun(t, w, fmt.Sprint("r", i), a); err != nil {
			t.Fatal(i, err)
		}
	}
	h := w.History()
	r, err := core.Replay(b0, h)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Snapshot(), w.Snapshot()) || !reflect.DeepEqual(r.History(), h) {
		t.Fatal("replay differs")
	}
	// The receipt is a copy.
	b, err := attackRun(t, w, "r9", `damage "char:mage" resource "hp" by 1 kind "fire" faces "char:mage:concentration" 3;`)
	if err != nil {
		t.Fatal(err)
	}
	got := damaged(t, b).Treated
	got.Treatments[0].Treatment = "immunity"
	got.UnusedFaces[0].Face = 20
	got.Kind = "x"
	h = w.History()
	kept := h[len(h)-1].Events[0].Resource.Treated
	if kept.Kind != "fire" || kept.Treatments[0].Treatment != "resistance" || kept.UnusedFaces[0].Face != 3 {
		t.Fatalf("%+v", kept)
	}
}

// The engine against a model of the law on random kinded damage, with own
// and held traits.
func TestDamageKindRandomAgainstModel(t *testing.T) {
	_, w := attackWorld(t, "character \"char:t1\" named \"T1\" at \"loc:valley/B03\" { stat \"ac\" = 1; resource \"hp\" = 1000000000 min 0 max 1000000000; resist \"fire\"; }\ncharacter \"char:t2\" named \"T2\" at \"loc:valley/B03\" { stat \"ac\" = 1; resource \"hp\" = 1000000000 min 0 max 1000000000; vulnerable \"fire\"; resist \"cold\"; }\ncharacter \"char:t3\" named \"T3\" at \"loc:valley/B03\" { stat \"ac\" = 1; resource \"hp\" = 1000000000 min 0 max 1000000000; immune \"poison\"; }\ncondition type \"spell:ward\" { instances coexist; }\ndamage trait \"spell:ward\" resistance \"fire\";\ncondition \"t2:ward\" of \"spell:ward\" to \"char:t2\";\n")
	traits := map[core.CharacterID]map[string][3]bool{"char:t1": {"fire": {true, false, false}}, "char:t2": {"fire": {true, true, false}, "cold": {true, false, false}}, "char:t3": {"poison": {false, false, true}}}
	health := map[core.CharacterID]int64{"char:t1": 1e9, "char:t2": 1e9, "char:t3": 1e9}
	ids := []core.CharacterID{"char:t1", "char:t2", "char:t3"}
	kinds := []string{"slashing", "fire", "cold", "poison", ""}
	rng := rand.New(rand.NewPCG(3, 5))
	tally := map[string]int{}
	for i := range 2000 {
		target, kind, amount := ids[rng.IntN(3)], kinds[rng.IntN(5)], rng.Int64N(40)+1
		want := amount
		if tr := traits[target][kind]; tr[0] {
			want /= 2
		}
		if tr := traits[target][kind]; tr[1] {
			want *= 2
		}
		if tr := traits[target][kind]; tr[2] {
			want = 0
		}
		b, err := w.Execute(core.Request{ID: fmt.Sprint("r", i), ExpectedRevision: w.Revision(), Actor: ada, Operations: []core.Operation{{Damage: &core.ResourceOp{Character: target, Resource: "hp", Amount: amount, Kind: kind}}}})
		if err != nil {
			t.Fatal(i, err)
		}
		r := damaged(t, b)
		if r.Requested != want || (r.Treated != nil) != (kind != "") {
			t.Fatalf("%d: %s %d %q: %+v, model %d", i, target, amount, kind, r, want)
		}
		health[target] -= want
		if currentOf(w, target, "hp") != health[target] {
			t.Fatalf("%d: hp", i)
		}
		switch {
		case kind == "":
			tally["kindless"]++
		case want == 0:
			tally["zero"]++
		case want != amount:
			tally["treated"]++
		default:
			tally["untreated"]++
		}
	}
	for _, k := range []string{"kindless", "zero", "treated", "untreated"} {
		if tally[k] < 40 {
			t.Fatalf("too few %s: %v", k, tally)
		}
	}
	t.Logf("%v", tally)
}
