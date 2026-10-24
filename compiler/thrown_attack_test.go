package compiler_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
)

// thrownSource is battleSource with the guide's thrown attack beside the
// one-handed longsword's slash, which Tess wears.
func thrownSource(t *testing.T) string {
	t.Helper()
	guide, err := os.ReadFile("../docs/GRAMMAR.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(guide), "## Thrown attacks\n\n```nql\n")
	if !ok {
		t.Fatal("the guide has no thrown attack example")
	}
	example, _, _ := strings.Cut(after, "```")
	const slash = "    melee reach 5 stat \"strength\";\n   }\n"
	s := battleSource(t)
	if !strings.Contains(s, slash) {
		t.Fatal("no slash")
	}
	return strings.Replace(s, slash, slash+example, 1)
}

// The guide's thrown attack lowers, is granted with its numbers, survives a
// save, and Battle refuses it.
func TestThrownAttackGuideExample(t *testing.T) {
	s := strings.Replace(thrownSource(t), `stat "initiative" = 2;`, `stat "initiative" = 30;`, 1)
	b, ds := compiler.CompileWorld("thrown.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	attacks := b.Equipment.Definitions[0].Modes[0].Attacks
	if len(attacks) != 2 || attacks[1].ID != "thrown" || attacks[1].Melee != nil || attacks[1].Ranged != nil || !reflect.DeepEqual(attacks[1].Thrown, &core.ThrownProfile{Stat: "strength", Normal: 20, Maximum: 60}) {
		t.Fatalf("%+v", attacks)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	view, err := w.EquipmentView("tess")
	if err != nil {
		t.Fatal(err)
	}
	var grant core.AttackGrant
	for _, g := range view.Attacks {
		if g.Origin.Member == "thrown" {
			grant = g
		}
	}
	// Strength 16 and proficiency 2: +5 to hit, 1d4+3 piercing.
	if grant.Thrown == nil || grant.ToHit != 5 || grant.DamageCount != 1 || grant.DamageSides != 4 || grant.DamageBonus != 3 {
		t.Fatalf("%+v", view.Attacks)
	}
	var buffer bytes.Buffer
	if _, err := nativesave.Write(&buffer, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "thrown", Next: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := nativesave.Read(&buffer)
	if err != nil || !reflect.DeepEqual(loaded.World.ExportCheckpoint(), w.ExportCheckpoint()) {
		t.Fatal("round trip", err)
	}
	tess := core.Ref{Kind: core.CharacterKind, ID: "tess"}
	run := func(id, source string) (core.Batch, error) {
		ops, ds := compiler.CompileActions(id, source, b)
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		return w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: tess, Operations: ops})
	}
	begin, err := os.ReadFile("../examples/native_melee/begin.nql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run("begin", string(begin)); err != nil {
		t.Fatal(err)
	}
	_, err = run("throw", fmt.Sprintf(`battle attack "gate-encounter" round 1 cursor 0 by "tess" using %q target "raider" finish true { attack 15; damage 2; };`, grant.ID))
	var f *core.Fault
	if !errors.As(err, &f) || f.Code != "E_BATTLE" || f.Field != "grant" {
		t.Fatal(err)
	}
}

func TestThrownAttackCompilerPositions(t *testing.T) {
	s := thrownSource(t)
	for _, tc := range []struct{ from, to, at, code string }{
		{`thrown normal 20`, `thrown normal 0`, `0`, "E_BATTLE"},
		{`maximum 60`, `maximum 19`, `19`, "E_EQUIPMENT"},
		{`maximum 60`, `maximum 1000001`, `1000001`, "E_BATTLE"},
		{`maximum 60 stat "strength"`, `maximum 60 stat "missing"`, `"missing"`, "E_STAT"},
		{`maximum 60 stat "strength";`, `maximum 60;`, `;`, "E_PARSE"},
		{`maximum 60 stat "strength"`, `maximum 60 stat ""`, `""`, "E_EQUIPMENT"},
	} {
		bad := strings.Replace(s, tc.from, tc.to, 1)
		if bad == s {
			t.Fatal("no change", tc.from)
		}
		at := strings.Index(bad, tc.to) + strings.LastIndex(tc.to, tc.at)
		_, ds := compiler.CompileWorld("bad-thrown", bad)
		if len(ds) != 1 || ds[0].Code != tc.code || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatalf("%+v: %+v", tc, ds)
		}
	}
}
