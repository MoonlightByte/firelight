package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
)

const relationshipWorld = `rules { transfer unequips; wear any; }
location "room" named "Room";
character "a" named "A" at "room" {}
character "b" named "B" at "room" {}
relation_kind "trust" min -10000 max 10000;
ability "support" named "Support" {
 parameter "speaker" character;
 parameter "amount" int;
 adjust relation character param "speaker" "trust" character "b" by param "amount";
}
`

func TestRelationshipCompilerSignedParametersAndPositions(t *testing.T) {
	b, ds := compiler.CompileWorld("relations.nql", relationshipWorld)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, source := range []string{`adjust relation character "a" "trust" character "b" by -9223372036854775808;`,
		`invoke "support" { "speaker" = character "a"; "amount" = int -800; };`} {
		ops, ds := compiler.CompileActions("adjust.nql", source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		w, err := core.NewWorld(b)
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.Execute(core.Request{ID: "adjust", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: ops})
		if err != nil {
			t.Fatal(err)
		}
		want := int64(-10000)
		if strings.HasPrefix(source, "invoke") {
			want = -800
		}
		if got := w.Snapshot().Relationships[0].Strength; got != want {
			t.Fatal(got, want)
		}
	}
	for _, tc := range []struct{ source, code, at string }{
		{`adjust relation character "a" "missing" character "b" by 1;`, "E_RELATION_KIND", `"missing"`},
		{`adjust relation item "a" "trust" character "b" by 1;`, "E_TYPE", `"a"`},
		{`adjust relation character "a" "trust" character "absent" by 1;`, "E_REFERENCE", `"absent"`},
		{`adjust relation character "a" "trust" character "b" by 0;`, "E_AMOUNT", "0;"},
		{`relate character "a" "trust" character "b" = 10001;`, "E_RELATION_BOUNDS", "10001"},
		{`invoke "support" { "speaker" = character "a"; "amount" = int 0; };`, "E_AMOUNT", "0;"},
	} {
		_, ds := compiler.CompileActions("bad.nql", tc.source, b)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Source != "bad.nql" || ds[0].Position.Line != 1 || ds[0].Position.Column != strings.Index(tc.source, tc.at)+1 {
			t.Fatalf("%s: %+v", tc.source, ds)
		}
	}
}

func TestRelationshipDefinitionDiagnosticsAndFlatBranches(t *testing.T) {
	for _, tc := range []struct {
		text, code string
		line       int
		at         string
	}{
		{relationshipWorld + `relation_kind "trust" min -1 max 1;`, "E_DUPLICATE", 11, `"trust"`},
		{strings.Replace(relationshipWorld, "min -10000", "min 1", 1), "E_RELATION_BOUNDS", 5, "1 max"},
		{strings.Replace(relationshipWorld, `by param "amount"`, `by 0`, 1), "E_AMOUNT", 9, "0;"},
		{strings.Replace(relationshipWorld, `"trust" character "b"`, `"unknown" character "b"`, 1), "E_RELATION_KIND", 9, `"unknown"`},
		{relationshipWorld + `relation character "a" "trust" character "b" = 10001;`, "E_RELATION_BOUNDS", 11, "10001"},
	} {
		_, ds := compiler.CompileWorld("bad-world.nql", tc.text)
		line := strings.Split(tc.text, "\n")[tc.line-1]
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != tc.line || ds[0].Position.Column != strings.Index(line, tc.at)+1 {
			t.Fatalf("%s: %+v", tc.text, ds)
		}
	}
	// Branch conversion and nested clones must retain the same closed primitive.
	text := relationshipWorld + `ability "branch" named "Branch" {
 check "choice" die 6 plus 0 against 3 {
 success { adjust relation character "a" "trust" character "b" by 5; }
 failure { adjust relation character "b" "trust" character "a" by -5; }
 }
}`
	b, ds := compiler.CompileWorld("branch.nql", text)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	s := w.Snapshot()
	for _, d := range s.AbilityDefinitions {
		if d.ID == "branch" {
			*d.Steps[0].Check.Success[0].AdjustRelationship.Amount.Literal.Integer = 20
		}
	}
	if reflect.DeepEqual(s, w.Snapshot()) {
		t.Fatal("test did not mutate detached branch")
	}
	for _, d := range w.Snapshot().AbilityDefinitions {
		if d.ID == "branch" && *d.Steps[0].Check.Success[0].AdjustRelationship.Amount.Literal.Integer != 5 {
			t.Fatal("branch alias")
		}
	}
}
