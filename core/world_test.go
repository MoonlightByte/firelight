package core

import (
	"math"
	"testing"
)

func TestRevisionExhaustion(t *testing.T) {
	b := Blueprint{Rules: Rules{Transfer: Unequips, Wear: WearAny}, Locations: []Location{{ID: "l"}}, Characters: []Character{{ID: "c", Location: "l", Resources: map[ResourceID]Resource{"hp": {Current: 1, Minimum: 0, Maximum: 2}}}}}
	w, e := NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	// Private test setup reaches a boundary impossible to iterate to in M1.
	w.authority.state.Revision = Revision(math.MaxUint64)
	_, e = w.Execute(Request{ID: "max", ExpectedRevision: Revision(math.MaxUint64), Actor: Ref{CharacterKind, "c"}, Operations: []Operation{{Damage: &ResourceOp{Character: "c", Resource: "hp", Amount: 1}}}})
	if e == nil || w.Revision() != Revision(math.MaxUint64) || len(w.History()) != 0 {
		t.Fatal("revision exhaustion wrapped or partially published")
	}
}
