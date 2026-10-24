package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

// A committed request is compiled again on a retry, against the world it
// made. Each creating action lowers to the operations it lowered to before
// it ran; compiled fresh, the same text is refused at the created ID.
func TestCompileRetryLeavesOutWhatTheActionsCreate(t *testing.T) {
	s := rangedSource(t) + `item type "torch-def" named "Torch" { kind "gear"; }
`
	before, ds := compiler.CompileWorld("before", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	made := s + `
item "torch" named "Torch" { owner "tess"; quantity 3; }
item "catalog-torch" from "torch-def" { owner "tess"; quantity 3; }
item "arrows-2" named "Arrows" { quantity 2; owner "tess"; custody character "tess"; }
item "arrows-back" named "Arrows" { quantity 1; owner "tess"; custody character "tess"; }
character "nell" named "Nell" at "gate" { }
character "zed" named "Zed" at "gate" { }
occupant "peddler" named "Peddler" at "gate" { person; }
occupant "tinker" named "Tinker" at "gate" { person; }
`
	after, ds := compiler.CompileWorld("after", made)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	held, _ := compiler.CompileWorld("after", made)
	for _, tc := range []struct {
		name, source, created string
	}{
		{"item", `create item "torch" named "Torch" { owner "tess"; quantity 3; };`, `"torch"`},
		{"catalog item", `create item "catalog-torch" from "torch-def" { owner "tess"; quantity 3; };`, `"catalog-torch"`},
		{"split", `split "arrows" quantity 2 into "arrows-2" by "tess";`, `"arrows-2"`},
		{"character", `create character "nell" named "Nell" at "gate" { };`, `"nell"`},
		{"occupant", `create occupant "peddler" named "Peddler" at "gate" { person; };`, `"peddler"`},
		{"recovery", `battle recover "fight" by "tess" { ammunition "arrows" as "arrows-back"; };`, `"arrows-back"`},
		// Later actions find what earlier ones created.
		{"item used", `create item "torch" named "Torch" { owner "tess"; quantity 3; }; transfer "torch" from "tess" to "lyra";`, `"torch"`},
		{"split used", `split "arrows" quantity 2 into "arrows-2" by "tess"; transfer "arrows-2" from "tess" to "lyra";`, `"arrows-2"`},
		{"character used", `create character "nell" named "Nell" at "gate" { }; transfer "arrows" from "tess" to "nell";`, `"nell"`},
		{"occupant used", `create occupant "peddler" named "Peddler" at "gate" { person; }; set occupant "peddler" attitude friendly;`, `"peddler"`},
	} {
		want, ds := compiler.CompileActions("actions", tc.source, before)
		if len(ds) != 0 {
			t.Fatalf("%s: first compile: %+v", tc.name, ds)
		}
		_, ds = compiler.CompileActions("actions", tc.source, after)
		if len(ds) != 1 || ds[0].Code != "E_DUPLICATE" || ds[0].Column != strings.Index(tc.source, tc.created)+1 {
			t.Fatalf("%s: fresh compile after: %+v", tc.name, ds)
		}
		got, ds := compiler.CompileRetry("actions", tc.source, after)
		if len(ds) != 0 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: retry %+v %+v, want %+v", tc.name, got, ds, want)
		}
		got, ds = compiler.CompileRetry("actions", tc.source, before)
		if len(ds) != 0 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: retry before it ran %+v %+v", tc.name, got, ds)
		}
		if !reflect.DeepEqual(after, held) {
			t.Fatalf("%s: the retry changed the caller's catalog", tc.name)
		}
	}
}

// Only what the actions create is left out, and only from its own kind: an ID
// another kind holds is refused on a retry as it is the first time.
func TestCompileRetryKeepsEverythingElse(t *testing.T) {
	b, ds := compiler.CompileWorld("world", rangedSource(t))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tc := range []struct{ name, source, at string }{
		{"item over a character", `create item "lyra" named "Lyra" { owner "tess"; };`, `"lyra"`},
		{"character over an item", `create character "arrows" named "Arrows" at "gate" { };`, `"arrows"`},
		{"occupant over a character", `create occupant "lyra" named "Lyra" at "gate" { person; };`, `"lyra"`},
		{"split over a character", `split "arrows" quantity 2 into "raider" by "tess";`, `"raider"`},
		{"recovery over a character", `battle recover "fight" by "tess" { ammunition "arrows" as "raider"; };`, `"raider"`},
	} {
		_, want := compiler.CompileActions("actions", tc.source, b)
		_, got := compiler.CompileRetry("actions", tc.source, b)
		if len(want) != 1 || want[0].Code != "E_DUPLICATE" || want[0].Column != strings.Index(tc.source, tc.at)+1 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: retry %+v, first %+v", tc.name, got, want)
		}
	}
	// A reference to an entity no action creates still has to resolve.
	missing := `create item "torch" named "Torch" { owner "nell"; };`
	_, ds = compiler.CompileRetry("actions", missing, b)
	if len(ds) != 1 || ds[0].Code != "E_REFERENCE" || ds[0].Column != strings.Index(missing, `"nell"`)+1 {
		t.Fatalf("missing owner: %+v", ds)
	}
	// A request that could never have committed is still refused.
	_, ds = compiler.CompileRetry("actions", `split "arrows" quantity 2 into "arrows" by "tess";`, b)
	if len(ds) != 1 || ds[0].Code != "E_REFERENCE" || ds[0].Column != 7 {
		t.Fatalf("split onto its source: %+v", ds)
	}
}
