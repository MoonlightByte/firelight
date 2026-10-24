package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

const custodySource = `
rules { transfer keeps_worn; wear any; }
clock "seconds" at 0;
location "hall" named "Hall";
character "a" named "A" at "hall" { stat "guard" = 10; resource "hp" = 8 min 0 max 10; }
character "b" named "B" at "hall" { stat "guard" = 10; resource "hp" = 8 min 0 max 10; }
item "scroll" named "Prayer" { owner "a"; custody character "b"; quantity 3; }
item "chest" named "Chest" { container true; custody location "hall"; }
ability "store" named "Store" {
 parameter "item" item;
 parameter "holder" character;
 parameter "box" item;
 place param "item" from character param "holder" to item param "box" by param "holder";
}
`

func TestCustodyGrammarInvocationAndDetachedDefinitions(t *testing.T) {
	b, ds := compiler.CompileWorld("custody", custodySource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	direct := `place "scroll" from character "b" to item "chest" by "b";`
	invoke := `invoke "store" { "item" = item "scroll"; "holder" = character "b"; "box" = item "chest"; };`
	var outcomes []core.Snapshot
	for _, source := range []string{direct, invoke} {
		ops, ds := compiler.CompileActions("action", source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		x, ds := compiler.CompileActions("different", "\n"+source, b)
		if len(ds) != 0 || !reflect.DeepEqual(ops, x) {
			t.Fatal("source changed intent", ds)
		}
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		r, err := w.Execute(core.Request{ID: "place", Actor: core.Ref{Kind: core.CharacterKind, ID: "b"}, Operations: ops})
		if err != nil || r.Events[0].Kind != core.ItemPlaced {
			t.Fatal(r, err)
		}
		if source == invoke && (r.Events[0].AbilityOrigin == nil || r.Events[len(r.Events)-1].Kind != core.AbilityInvoked || !reflect.DeepEqual(r.Request.Operations, ops)) {
			t.Fatal("lost invocation", r)
		}
		outcomes = append(outcomes, w.Snapshot())
		if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(rebuilt.ExportCheckpoint(), w.ExportCheckpoint()) {
			t.Fatal("place replay", err)
		}
		if source == invoke {
			before := w.ExportCheckpoint()
			snap := w.Snapshot()
			snap.AbilityDefinitions[0].Steps[0].Place.From.Subject.Parameter = "forged"
			b.AbilityDefinitions[0].Steps[0].Place.To.Subject.Parameter = "forged"
			if !reflect.DeepEqual(before, w.ExportCheckpoint()) {
				t.Fatal("nested placement template alias")
			}
		}
	}
	if !reflect.DeepEqual(outcomes[0], outcomes[1]) {
		t.Fatal("inline and invocation facts differ")
	}
}

func TestCustodyPositionedNegativeInputsAndContinuation(t *testing.T) {
	b, ds := compiler.CompileWorld("custody", custodySource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ source, token, code string }{
		{`place "scroll" from character "chest" to location "hall" by "b";`, `"chest"`, "E_TYPE"},
		{`place "scroll" from character "b" to item "scroll" by "b";`, `"scroll" by`, "E_CUSTODY"},
		{`place "scroll" from character "b" to item "chest" by "hall";`, `"hall"`, "E_TYPE"},
		{`create item "new" named "New" { custody character "hall"; };`, `"hall"`, "E_TYPE"},
	} {
		_, ds := compiler.CompileActions("bad", "\n"+tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != 2 || ds[0].Column != strings.Index(tc.source, tc.token)+1 {
			t.Fatal(tc, ds)
		}
	}
	for _, tc := range []struct{ from, to, token string }{
		{`from character param "holder"`, `from character param "box"`, `param "box" to`},
		{`to item param "box"`, `to location "absent"`, `"absent"`},
		{`to item param "box"`, `to item "scroll"`, `"scroll" by`},
		{`custody character "b"`, `custody character "absent"`, `"absent"`},
		{`custody character "b"`, `custody item "scroll"`, `"scroll"; quantity`},
		{`custody location "hall"; }`, "custody location \"hall\"; }\nitem \"gem\" named \"Gem\" { custody item \"scroll\"; }", `"scroll"; }`},
	} {
		s := strings.Replace(custodySource, tc.from, tc.to, 1)
		_, ds := compiler.CompileWorld("bad", s)
		at := strings.Index(s, tc.token)
		if strings.HasPrefix(tc.token, "param ") {
			at += len("param ")
		}
		if len(ds) != 1 || ds[0].Line != strings.Count(s[:at], "\n")+1 || ds[0].Column != at-strings.LastIndex(s[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
	for _, s := range []string{
		strings.Replace(custodySource, `container true;`, `container true; container true;`, 1),
		strings.Replace(custodySource, `custody character "b";`, `custody character "b"; custody character "b";`, 1),
		strings.Replace(custodySource, `custody location "hall";`, `custody item "chest";`, 1),
	} {
		if _, ds := compiler.CompileWorld("bad", s); len(ds) == 0 {
			t.Fatal("invalid declaration admitted")
		}
	}
	if _, ds := compiler.CompileActions("corrected", `place "scroll" from character "b" to item "chest" by "b";`, b); len(ds) != 0 {
		t.Fatal(ds)
	}
}

func TestCustodyCarriedRequirementAndCheckBranch(t *testing.T) {
	s := strings.Replace(battleSource(t), `zero protected stable_down opposition dead;`, "zero protected stable_down opposition dead;\n action \"store\";", 1) + `
item "prayer" named "Prayer" { owner "lyra"; custody character "tess"; }
item "chest" named "Chest" { container true; custody location "gate"; }
ability "store" named "Store" {
 parameter "actor" character;
 action actor param "actor" phase exploration;
 require carried_item "prayer" by param "actor";
 check "test" die 2 plus 0 against 0 {
  success { place "prayer" from character param "actor" to item "chest" by param "actor"; }
  failure {}
 }
}
`
	b, ds := compiler.CompileWorld("carried", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	ops, ds := compiler.CompileActions("invoke", `invoke "store" { "actor" = character "tess"; };`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "store", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range r.Events {
		if e.Custody != nil {
			found = e.AbilityOrigin != nil && reflect.DeepEqual(e.AbilityOrigin.Path, []int{0, 0})
		}
	}
	if !found {
		t.Fatal("branch placement origin", r)
	}
	before := w.ExportCheckpoint()
	if _, err := w.Execute(core.Request{ID: "again", ExpectedRevision: w.Revision(), Actor: r.Request.Actor, Operations: ops}); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("stored item counted as carried", err)
	}
	if rebuilt, err := core.Replay(b, w.History()); err != nil || !reflect.DeepEqual(before, rebuilt.ExportCheckpoint()) {
		t.Fatal("branch reconstruction", err)
	}
}
