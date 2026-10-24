package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"strings"
	"testing"
)

func routeSource(t *testing.T, route string) string {
	s := battleSource(t)
	return strings.Replace(s, "zero protected stable_down opposition dead;", "zero protected stable_down opposition dead;\n"+route, 1) + "\nlocation \"road\" named \"Road\";\n"
}

func TestNativeTravelSourceAndInvocation(t *testing.T) {
	s := routeSource(t, `route "gate" to "road" ticks 600 victory optional;`)
	s += `ability "walk" named "Take the road" { travel party to "road"; }`
	b, ds := compiler.CompileWorld("route.nql", s)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if len(b.Battle.Rules.Routes) != 1 || b.Battle.Rules.Routes[0].Ticks != 600 {
		t.Fatal(b.Battle.Rules.Routes)
	}
	direct, ds := compiler.CompileActions("direct", `travel party to "road";`, b)
	if len(ds) != 0 || len(direct) != 1 || direct[0].Travel.To != "road" {
		t.Fatal(direct, ds)
	}
	_, ds = compiler.CompileActions("wrong-target", `travel party to "tess";`, b)
	if len(ds) != 1 || ds[0].Position.Column != 17 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	ops, ds := compiler.CompileActions("walk.nql", `invoke "walk" {};`, b)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	r, err := w.Execute(core.Request{ID: "walk", Actor: core.Ref{Kind: core.CharacterKind, ID: "tess"}, Operations: ops})
	if err != nil || w.Snapshot().Clock.Tick != 600 || r.Events[len(r.Events)-1].Kind != core.AbilityInvoked || r.Events[len(r.Events)-2].Kind != core.PartyTraveled {
		t.Fatal(r, err)
	}
}

func TestNativeTravelPositionedDefinitions(t *testing.T) {
	for _, tc := range []struct{ route, bad string }{
		{`route "tess" to "road" ticks 600 victory optional;`, `"tess"`},
		{`route "gate" to "missing" ticks 600 victory optional;`, `"missing"`},
		{`route "gate" to "road" ticks 0 victory optional;`, `0`},
		{`route "gate" to "gate" ticks 600 victory optional;`, `"gate" ticks`},
	} {
		s := routeSource(t, tc.route)
		_, ds := compiler.CompileWorld("bad-route.nql", s)
		line := strings.Count(s[:strings.Index(s, tc.route)], "\n") + 1
		column := strings.Index(tc.route, tc.bad) + 1
		if len(ds) != 1 || ds[0].Position.Source != "bad-route.nql" || ds[0].Position.Line != line || ds[0].Position.Column != column {
			t.Fatalf("%s: %+v", tc.route, ds)
		}
	}
	route := `route "gate" to "road" ticks 600 victory optional;`
	s := routeSource(t, route+"\n"+route)
	_, ds := compiler.CompileWorld("duplicate", s)
	if len(ds) != 1 || ds[0].Position.Line != strings.Count(s[:strings.LastIndex(s, route)], "\n")+1 || ds[0].Position.Column != 7 {
		t.Fatal(ds)
	}
}
