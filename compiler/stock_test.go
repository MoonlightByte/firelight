package compiler

import (
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestStockCompilerSequentialTypesAndCurrentCandidate(t *testing.T) {
	b, ds := CompileWorld("stock-world", `rules { transfer unequips; wear any; } location "room" named "Room";
character "a" named "A" at "room" {} character "b" named "B" at "room" {}
item "chest" named "Chest" { container true; custody location "room"; }`)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	b.Items = append(make([]core.Item, 0, 8), b.Items...)
	backing := b.Items[:cap(b.Items)]
	saved := append([]core.Item(nil), backing...)
	source := `create item "stock" named "Leaves" { owner "a"; quantity 6; custody character "a"; };
place "stock" from character "a" to character "b" by "a";
transfer "stock" from "a" to "b";
split "stock" quantity 2 into "new" by "b";
place "new" from character "b" to item "chest" by "b";`
	ops, ds := CompileActions("stock-action", source, b)
	if len(ds) != 0 || len(ops) != 5 || !reflect.DeepEqual(backing, saved) {
		t.Fatal(ops, ds, "catalog alias")
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Execute(core.Request{ID: "first", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
	if err != nil || r.Events[3].StockSplit.Created.Owner != "b" || r.Events[3].StockSplit.Created.Custody.ID != "b" {
		t.Fatal(r, err)
	}
	snap := w.Snapshot()
	compiled, ds := CompileActions("before-move", `split "stock" quantity 1 into "next" by "b";`, snap.Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	moved, ds := CompileActions("move", `place "stock" from character "b" to item "chest" by "b";`, snap.Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, err = w.Execute(core.Request{ID: "move", ExpectedRevision: 1, Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: moved}); err != nil {
		t.Fatal(err)
	}
	before := w.ExportCheckpoint()
	req := core.Request{ID: "late", ExpectedRevision: 1, Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: compiled}
	if _, err = w.Execute(req); err == nil || !reflect.DeepEqual(before, w.ExportCheckpoint()) {
		t.Fatal("stale split accepted")
	}
	req.ExpectedRevision = 2
	out, err := w.Execute(req)
	if err != nil || out.Events[0].StockSplit.Created.Custody.Kind != core.ItemKind {
		t.Fatal("compiler froze old custody", out, err)
	}
	for _, tc := range []struct{ text, code, site string }{
		{`split "stock" quantity 0 into "x" by "b";`, "E_AMOUNT", "0"},
		{`split "stock" quantity 9223372036854775808 into "x" by "b";`, "E_INTEGER", "9223372036854775808"},
		{`split "stock" quantity 1 into "x" by "room";`, "E_TYPE", `"room"`},
		{`split "room" quantity 1 into "x" by "a";`, "E_TYPE", `"room"`},
		{`split "stock" quantity 1 into "a" by "b";`, "E_DUPLICATE", `"a"`},
	} {
		_, ds := CompileActions("bad-stock", tc.text, w.Snapshot().Blueprint)
		if len(ds) != 1 || ds[0].Source != "bad-stock" || ds[0].Line != 1 || ds[0].Column != strings.Index(tc.text, tc.site)+1 || ds[0].Code != tc.code {
			t.Fatal(tc, ds)
		}
	}
}
