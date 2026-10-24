package compiler_test

import (
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/compiler"
)

func TestRelationshipDecaySourceAndOperands(t *testing.T) {
	const rule = `relation_kind "mood" min -10000 max 10000 decay every 86400 retain 97 of 100 baseline 0;`
	base := relationshipWorld + "clock \"seconds\" at 0;\n"
	b, ds := compiler.CompileWorld("decay", base+rule)
	if len(ds) != 0 || len(b.RelationDefinitions) != 2 || b.RelationDefinitions[1].Decay.Period != 86400 || b.RelationDefinitions[1].Decay.Numerator != 97 {
		t.Fatal(b.RelationDefinitions, ds)
	}
	for _, row := range []struct{ before, after, at, code string }{
		{"every 86400", "every -1", "-1 retain", "E_RELATION_DECAY"},
		{"retain 97", "retain 100", "100 of", "E_RELATION_DECAY"},
		{"of 100", "of 1001", "1001", "E_RELATION_DECAY"},
		{"baseline 0", "baseline 10001", "10001", "E_RELATION_BOUNDS"},
	} {
		source := strings.Replace(rule, row.before, row.after, 1)
		_, ds = compiler.CompileWorld("bad-decay", base+source)
		if len(ds) != 1 || ds[0].Code != row.code || ds[0].Position.Line != 12 || ds[0].Position.Column != strings.Index(source, row.at)+1 {
			t.Fatal(row, ds)
		}
	}
	_, ds = compiler.CompileWorld("no-clock", relationshipWorld+rule)
	if len(ds) != 1 || ds[0].Code != "E_CLOCK" || ds[0].Position.Line != 11 || ds[0].Position.Column != strings.Index(rule, "decay")+1 {
		t.Fatal(ds)
	}
}
