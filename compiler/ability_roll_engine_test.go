package compiler_test

// 7b-engine commit B (kit agents/kw/plan-7b-engine-r0.md): the
// `roll <step> engine;` invocation field.

import (
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

func TestEngineRollInvocationSource(t *testing.T) {
	b, ds := compiler.CompileWorld("rolled", authoredRollSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "rolled-care" { "actor" = character "tess"; "target" = character "tess"; roll 1 engine; };`
	ops, ds := compiler.CompileActions("engine", source, b)
	if len(ds) != 0 || len(ops) != 1 || len(ops[0].Invoke.HumanRolls) != 1 || !ops[0].Invoke.HumanRolls[0].Engine || ops[0].Invoke.HumanRolls[0].Step != 1 || ops[0].Invoke.HumanRolls[0].Faces != nil {
		t.Fatal("engine opt-out", ops, ds)
	}
	direct := core.CanonicalInvocation(core.InvokeAbility{Ability: "rolled-care", HumanRolls: []core.AbilityRollInput{{Step: 1, Engine: true}},
		Arguments: ops[0].Invoke.Arguments})
	if direct.HumanRolls[0].Engine != ops[0].Invoke.HumanRolls[0].Engine {
		t.Fatal("text and typed frontends differ")
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "engine", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range r.Events {
		found = found || (e.ResourceRoll != nil && e.ResourceRoll.EngineDrawn && e.ResourceRoll.Source == core.HumanController)
	}
	if !found {
		t.Fatal("no engine-drawn human roll", r.Events)
	}
	for _, bad := range []string{
		strings.Replace(source, "roll 1 engine;", "roll 1 engine { face 2; };", 1),
		strings.Replace(source, "roll 1 engine;", "roll 1 engines;", 1),
	} {
		if _, ds := compiler.CompileActions("bad", bad, b); len(ds) != 1 {
			t.Fatal("malformed engine opt-out accepted", bad, ds)
		}
	}
}
