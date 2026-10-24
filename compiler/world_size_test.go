package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"strings"
	"testing"
)

// A world source may be up to 16 MiB, so a world can be declared up to its
// entity caps; an action source keeps the 1 MiB bound (docs/WORLD_SIZE.md).
func TestWorldAndActionSourceLimits(t *testing.T) {
	if compiler.MaxSourceBytes != 1<<20 || compiler.MaxWorldSourceBytes != 16<<20 {
		t.Fatal("source bounds changed", compiler.MaxSourceBytes, compiler.MaxWorldSourceBytes)
	}
	head := "rules { transfer unequips; wear any; }\nlocation \"l\" named \"L\";\ncharacter \"c\" named \"C\" at \"l\" { }\n"
	padded := func(text string, size int) string { return text + "//" + strings.Repeat("x", size-len(text)-3) + "\n" }
	if _, ds := compiler.CompileWorld("world.nql", padded(head, compiler.MaxWorldSourceBytes)); len(ds) != 0 {
		t.Fatal("world at the source bound refused", ds)
	}
	_, ds := compiler.CompileWorld("world.nql", padded(head, compiler.MaxWorldSourceBytes+1))
	if len(ds) != 1 || ds[0].Code != "E_LIMIT" || ds[0].Expected != "source <= 16 MiB" {
		t.Fatal("world over the source bound accepted or misreported", ds)
	}
	b, ds := compiler.CompileWorld("world.nql", head)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	action := `move "c" to "l";` + "\n"
	if _, ds := compiler.CompileActions("actions.nql", padded(action, compiler.MaxSourceBytes), b); len(ds) != 0 {
		t.Fatal("actions at the source bound refused", ds)
	}
	_, ds = compiler.CompileActions("actions.nql", padded(action, compiler.MaxSourceBytes+1), b)
	if len(ds) != 1 || ds[0].Code != "E_LIMIT" || ds[0].Expected != "source <= 1 MiB" {
		t.Fatal("actions over the source bound accepted or misreported", ds)
	}
}
