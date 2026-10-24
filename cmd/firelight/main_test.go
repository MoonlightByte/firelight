package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExecute(t *testing.T) {
	var out, errs bytes.Buffer
	code := run([]string{"-world", "../../examples/micro-world.nql", "-execute", "../../examples/journey.nql", "-actor", "char:ada", "-status", "char:ada"}, strings.NewReader(""), &out, &errs)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	var v output
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Revision != 1 || v.Result == nil || len(v.Result.Events) != 11 || v.Status.Resources["hp"].Current != 16 || v.Status.Location.ID != "loc:garden" {
		t.Fatalf("unexpected CLI result: %s", &out)
	}
}
func TestCLIContinuation(t *testing.T) {
	var out, errs bytes.Buffer
	input := "damage \"char:ada\" resource \"hp\" by ;\ndamage \"char:ada\" resource \"hp\" by 2;\n:inspect\n:quit\n"
	if code := run([]string{"-world", "../../examples/micro-world.nql", "-repl", "-actor", "char:ada"}, strings.NewReader(input), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, &errs)
	}
	if !strings.Contains(errs.String(), `"code": "E_PARSE"`) {
		t.Fatalf("no structured compiler error: %s", &errs)
	}
	decoder := json.NewDecoder(&out)
	var receipt, inspection output
	if e := decoder.Decode(&receipt); e != nil {
		t.Fatal(e)
	}
	if e := decoder.Decode(&inspection); e != nil {
		t.Fatal(e)
	}
	if receipt.Revision != 1 || inspection.World.Revision != 1 || inspection.World.Characters[0].Resources["hp"].Current != 18 {
		t.Fatal("valid command did not continue after malformed input")
	}
}
func TestCLIReadOnlyAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"-status", "char:bea"}, 0},
		{[]string{"-transfer", "item:shield:001", "-to", "char:cy"}, 0},
		{[]string{"-execute", "../../examples/journey.nql"}, 1},
		{[]string{"-status", "loc:hall"}, 1},
		{[]string{"trailing"}, 2},
	} {
		var out, errs bytes.Buffer
		args := append([]string{"-world", "../../examples/micro-world.nql"}, tc.args...)
		if got := run(args, strings.NewReader(""), &out, &errs); got != tc.want {
			t.Fatalf("%v: exit %d, want %d: %s", tc.args, got, tc.want, &errs)
		}
	}
}

// The CLI reads a world file up to the 16 MiB world source bound and an
// action file up to the 1 MiB action bound (docs/WORLD_SIZE.md).
func TestCLISourceFileLimits(t *testing.T) {
	world, err := os.ReadFile("../../examples/micro-world.nql")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	big := filepath.Join(dir, "world.nql")
	// The padding comes first, so a read cut at 1 MiB would lose the declarations.
	if err := os.WriteFile(big, append([]byte("//"+strings.Repeat("x", 2<<20)+"\n"), world...), 0600); err != nil {
		t.Fatal(err)
	}
	long := filepath.Join(dir, "actions.nql")
	if err := os.WriteFile(long, []byte("//"+strings.Repeat("x", 1<<20)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run([]string{"-world", big, "-status", "char:bea"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("world over 1 MiB: exit %d: %s", code, &errs)
	}
	errs.Reset()
	if code := run([]string{"-world", big, "-execute", long, "-actor", "char:bea"}, strings.NewReader(""), &out, &errs); code != 1 || !strings.Contains(errs.String(), `"code": "E_LIMIT"`) {
		t.Fatalf("actions over 1 MiB: exit %d: %s", code, &errs)
	}
}
