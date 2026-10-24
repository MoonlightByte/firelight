package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"strings"
	"testing"
)

const removalSource = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" { stat "defense" = 10; }
condition type "trained" { instances coexist; }
ability "remove" named "Remove" {
 parameter "old" condition_ref;
 remove condition param "old" of "trained" from "a";
}`

func TestConditionRefSourceAndPosition(t *testing.T) {
	b, ds := compiler.CompileWorld("remove", removalSource)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	ops, ds := compiler.CompileActions("invoke", `invoke "remove" { "old" = condition_ref "not-active-yet"; };`, b)
	if len(ds) != 0 || *ops[0].Invoke.Arguments[0].Value.ConditionRef != "not-active-yet" {
		t.Fatal(ds)
	}
	for _, source := range []string{`remove condition "old" from "a";`, `remove condition "old" of "trained" from "a";`} {
		if _, ds = compiler.CompileActions("direct", source, b); len(ds) != 0 {
			t.Fatal(ds)
		}
	}
	bad := strings.Replace(removalSource, `of "trained"`, `of "missing"`, 1)
	_, ds = compiler.CompileWorld("bad", bad)
	if len(ds) != 1 || ds[0].Position.Line != 7 || ds[0].Position.Column != 34 {
		t.Fatal("type site", ds)
	}
	source := `invoke "remove" { "old" = condition_id "new"; };`
	_, ds = compiler.CompileActions("bad-value", source, b)
	if len(ds) != 1 || ds[0].Code != "E_TYPE" || ds[0].Position.Column != 40 {
		t.Fatal("value site", ds)
	}
	branch := strings.Replace(removalSource, `remove condition param "old" of "trained" from "a";`, `check "gate" die 6 plus 0 against 3 { success { remove condition param "old" of "trained" from "a"; } failure {} }`, 1)
	// Definition lowering preserves the same closed primitive inside a flat branch.
	out, ds := compiler.CompileWorld("branch", branch)
	if len(ds) != 0 || out.AbilityDefinitions[0].Steps[0].Check.Success[0].RemoveCondition == nil {
		t.Fatal(ds)
	}
}
