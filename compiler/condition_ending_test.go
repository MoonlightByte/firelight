package compiler_test

import (
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"strings"
	"testing"
)

func TestConditionEndingSourceAndSites(t *testing.T) {
	source := strings.Replace(maintenanceSource, `ends_at_minimum "hp";`, `ends_at_minimum "hp"; ends_on_condition "blocked";`, 1) + "\n" + `condition type "blocked" { instances coexist; }`
	b, ds := compiler.CompileWorld("ending", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	commands := `invoke "ward" { "actor" = character "a"; "root" = condition_id "r"; "child" = condition_id "c"; }; apply condition "x" of "blocked" to "a";`
	one, ds := compiler.CompileActions("first", commands, b)
	other, otherDS := compiler.CompileActions("other", "\n"+commands, b)
	if len(ds) != 0 || len(otherDS) != 0 || !reflect.DeepEqual(one, other) {
		t.Fatal("source identity", ds, otherDS)
	}
	r, err := w.Execute(core.Request{ID: "R", Actor: core.Ref{Kind: core.CharacterKind, ID: "a"}, Operations: one})
	if err != nil || len(r.Events) != 7 || *r.Events[5].Condition.EndingCondition != "x" {
		t.Fatal("authored body/ending", r, err)
	}
	for _, kind := range []string{"missing", "root", ""} {
		bad := strings.Replace(source, `ends_on_condition "blocked"`, `ends_on_condition "`+kind+`"`, 1)
		_, errors := compiler.CompileWorld("bad-ending", bad)
		index := strings.Index(bad, `ends_on_condition `) + len(`ends_on_condition `)
		line, col := strings.Count(bad[:index], "\n")+1, index-strings.LastIndex(bad[:index], "\n")
		if len(errors) != 1 || errors[0].Code != "E_CONDITION_TYPE" || errors[0].Position.Source != "bad-ending" || errors[0].Position.Line != line || errors[0].Position.Column != col {
			t.Fatal("ending declaration site", line, col, errors)
		}
	}
	bad := strings.Replace(source, `ends_on_condition "blocked";`, `ends_on_condition "blocked"; ends_on_condition "blocked";`, 1)
	_, ds = compiler.CompileWorld("duplicate", bad)
	if len(ds) != 1 || ds[0].Code != "E_DUPLICATE" {
		t.Fatal(ds)
	}
	conflict := source + "\n" + `condition "root-on" of "root" to "a";` + "\n" + `condition "block-on" of "blocked" to "a";`
	_, ds = compiler.CompileWorld("genesis-conflict", conflict)
	index := strings.Index(conflict, `condition "root-on" of "root" to `) + len(`condition "root-on" of "root" to `)
	line, column := strings.Count(conflict[:index], "\n")+1, index-strings.LastIndex(conflict[:index], "\n")
	if len(ds) != 1 || ds[0].Code != "E_CONDITION" || ds[0].Position.Source != "genesis-conflict" || ds[0].Position.Line != line || ds[0].Position.Column != column {
		t.Fatal("genesis target site", line, column, ds)
	}
}
