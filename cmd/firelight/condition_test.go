package main

import (
	"bytes"
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

func TestCLIConditionJourneyAndContinuation(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"-world", "../../examples/condition-world.nql", "-execute", "../../examples/condition-journey.nql", "-actor", "char:ada", "-status", "char:ada"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	var v output
	if e := json.Unmarshal(out.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v.Revision != 1 || len(v.Result.Events) != 4 || len(v.Status.Conditions) != 2 || v.Status.Stats[0].Effective != 15 || v.Result.Events[3].Kind != core.ConditionRemoved {
		t.Fatalf("condition journey: %s", &out)
	}
	out.Reset()
	errs.Reset()
	input := `apply condition "bad" of "blessing" to "char:ada" magnitude ;
apply condition "bad-stat" of "incompatible" to "char:ada";
apply condition "ok" of "marked" to "char:ada";
:inspect
:quit
`
	if code := run([]string{"-world", "../../examples/condition-world.nql", "-repl", "-actor", "char:ada"}, strings.NewReader(input), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	if !strings.Contains(errs.String(), `"code": "E_PARSE"`) || !strings.Contains(errs.String(), `"code": "E_STAT"`) {
		t.Fatal("missing diagnostics", &errs)
	}
	d := json.NewDecoder(&out)
	var receipt, inspect output
	if e := d.Decode(&receipt); e != nil {
		t.Fatal(e)
	}
	if e := d.Decode(&inspect); e != nil {
		t.Fatal(e)
	}
	if receipt.Revision != 1 || inspect.World.Revision != 1 || len(inspect.World.Conditions) != 1 || inspect.World.Conditions[0].ID != "ok" {
		t.Fatal("condition continuation failed")
	}
}
