package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"reflect"
	"strings"
	"testing"
)

func TestLocationSpanAuthorshipAndSourceSites(t *testing.T) {
	base := battleSource(t)
	clause := `location_span "gate" maximum 30;`
	s := strings.Replace(base, `zero protected stable_down opposition dead;`, `zero protected stable_down opposition dead;`+"\n "+clause, 1)
	b, ds := compiler.CompileWorld("span", s)
	if len(ds) != 0 || len(b.Battle.Rules.LocationSpans) != 1 || b.Battle.Rules.LocationSpans[0].Maximum != 30 {
		t.Fatal(ds)
	}
	other, ds := compiler.CompileWorld("elsewhere", "\n"+s)
	if len(ds) != 0 || !reflect.DeepEqual(b, other) {
		t.Fatal("source changed span semantics", ds)
	}
	for _, tc := range []struct{ clause, token string }{
		{`location_span "missing" maximum 30;`, `"missing"`},
		{`location_span "gate" maximum 0;`, `0`},
		{`location_span "gate" maximum 9223372036854775808;`, `9223372036854775808`},
		{clause + "\n " + clause, `"gate"`},
	} {
		bad := strings.Replace(s, clause, tc.clause, 1)
		at := strings.Index(bad, tc.clause) + strings.LastIndex(tc.clause, tc.token)
		_, ds := compiler.CompileWorld("bad-span", bad)
		if len(ds) != 1 || ds[0].Position.Line != strings.Count(bad[:at], "\n")+1 || ds[0].Position.Column != at-strings.LastIndex(bad[:at], "\n") {
			t.Fatal(tc, ds)
		}
	}
}
