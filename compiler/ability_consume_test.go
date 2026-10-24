package compiler

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func TestAbilityConsumeTypedParametersAndPositions(t *testing.T) {
	definition := `
ability "eat" named "Eat" {
 parameter "stock" item; parameter "owner" character; parameter "units" int;
 consume param "stock" from param "owner" by param "units";
}`
	b, ds := CompileWorld("consume.nql", inventorySource+definition)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	source := `invoke "eat" { "stock" = item "stock"; "owner" = character "a"; "units" = int 1; };`
	ops, ds := CompileActions("use.nql", source, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	other, ds := CompileActions("other.nql", "\n"+source, b)
	if len(ds) != 0 || !reflect.DeepEqual(ops, other) {
		t.Fatal("source identity", ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "one", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil || r.Events[0].Kind != core.ItemConsumed || r.Events[0].Consumption.After != 2 {
		t.Fatal(r, err)
	}
	for _, tc := range []struct{ statement, code, token string }{
		{`consume "stock" from "a" by 0;`, "E_AMOUNT", "0"},
		{`consume "stock" from "stock" by 1;`, "E_TYPE", `"stock" by`},
		{`consume "a" from "a" by 1;`, "E_TYPE", `"a" from`},
		{`consume "missing" from "a" by 1;`, "E_REFERENCE", `"missing"`},
		{`consume "stock" from "a" by 9223372036854775808;`, "E_INTEGER", "9223372036854775808"},
	} {
		text := inventorySource + "\nability \"bad\" named \"Bad\" {\n " + tc.statement + "\n}"
		_, ds := CompileWorld("bad.nql", text)
		at := strings.LastIndex(text, tc.token)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Line != strings.Count(text[:at], "\n")+1 || ds[0].Column != at-strings.LastIndex(text[:at], "\n") {
			t.Fatalf("%s: %+v", tc.statement, ds)
		}
	}
	for _, amount := range []string{"0", "-1"} {
		_, ds := CompileActions("bad-use.nql", strings.Replace(source, "int 1", "int "+amount, 1), b)
		if len(ds) != 1 || ds[0].Code != "E_AMOUNT" {
			t.Fatal(amount, ds)
		}
	}
	// Positive int64 boundary is typed; affordability remains a candidate fact.
	maxSource := strings.Replace(inventorySource, "quantity 3", "quantity 9223372036854775807", 1) + ` ability "all" named "All" { consume "stock" from "a" by 9223372036854775807; }`
	b, ds = CompileWorld("max.nql", maxSource)
	if len(ds) != 0 || *b.AbilityDefinitions[0].Steps[0].Consume.Amount.Literal.Integer != math.MaxInt64 {
		t.Fatal(ds)
	}
	branch := inventorySource + ` ability "branch" named "Branch" { check "pick" die 2 plus 0 against 1 { success { consume "stock" from "a" by 1; } failure {} } }`
	b, ds = CompileWorld("branch.nql", branch)
	if len(ds) != 0 || b.AbilityDefinitions[0].Steps[0].Check.Success[0].Consume == nil {
		t.Fatal("flat branch", ds)
	}
	w, err = core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	*b.AbilityDefinitions[0].Steps[0].Check.Success[0].Consume.Amount.Literal.Integer = 999
	if *w.Snapshot().AbilityDefinitions[0].Steps[0].Check.Success[0].Consume.Amount.Literal.Integer != 1 {
		t.Fatal("branch alias")
	}
}
