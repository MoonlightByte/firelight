package main

import (
	"bytes"
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

func TestCLIAbilityJourneyAndContinuation(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"-world", "../../examples/ability-world.nql", "-execute", "../../examples/ability-journey.nql", "-actor", "char:bea", "-status", "char:ada", "-inspect"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	var v output
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v.Revision != 1 || len(v.Result.Events) != 6 || v.Result.Events[2].Kind != core.AbilityInvoked || v.Result.Events[5].Kind != core.AbilityInvoked || v.Status.Resources["hp"].Current != 14 || v.Status.Stats[0].Effective != 13 || len(v.World.AbilityDefinitions) != 2 {
		t.Fatal("authored CLI journey", out.String())
	}
	out.Reset()
	errs.Reset()
	input := `invoke "aid" { "amount" = int ; };
invoke "aid" { "target" = item "item:shield"; "amount" = int 4; "instance" = condition_id "bad"; };
invoke "aid" { "target" = character "char:ada"; "amount" = int 4; "instance" = condition_id "one"; };
:inspect
:quit
`
	if code := run([]string{"-world", "../../examples/ability-world.nql", "-repl", "-actor", "char:bea"}, strings.NewReader(input), &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if !strings.Contains(errs.String(), `"code": "E_PARSE"`) || !strings.Contains(errs.String(), `"code": "E_TYPE"`) {
		t.Fatal("missing diagnostics", errs.String())
	}
	d := json.NewDecoder(&out)
	var receipt, inspect output
	if e := d.Decode(&receipt); e != nil {
		t.Fatal(e)
	}
	if e := d.Decode(&inspect); e != nil {
		t.Fatal(e)
	}
	if receipt.Revision != 1 || len(receipt.Result.Events) != 3 || inspect.World.Revision != 1 || len(inspect.World.Conditions) != 1 || inspect.World.Conditions[0].ID != "one" || inspect.World.Conditions[0].Cause.Actor.ID != "char:bea" {
		t.Fatal("CLI continuation")
	}
}
