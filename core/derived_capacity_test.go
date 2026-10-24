package core_test

import (
	"bytes"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/nativesave"
	"reflect"
	"strings"
	"testing"
)

func derivedCapacityFixture(characters, recipes int) core.Blueprint {
	b := core.Blueprint{Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "room", Name: "Room"}}, Equipment: &core.EquipmentRules{Version: core.EquipmentVersion}}
	for d := 0; d < recipes; d++ {
		b.Equipment.Derived = append(b.Equipment.Derived, core.DerivedStatRule{Stat: core.StatID(fmt.Sprint("derived:", d)), Terms: []core.StatTerm{{Stat: "input", Offset: -10, Divisor: 2}}})
	}
	for c := 0; c < characters; c++ {
		v := core.Character{ID: core.CharacterID(fmt.Sprint("character:", c)), Name: "Same display name", Location: "room", Stats: map[core.StatID]int64{"input": 12}}
		for _, d := range b.Equipment.Derived {
			v.Stats[d.Stat] = 0
		}
		b.Characters = append(b.Characters, v)
	}
	return b
}

func derivedCapacitySource(recipes int) string {
	var source strings.Builder
	source.WriteString("rules { transfer unequips; wear any; }\nlocation \"room\" named \"Room\";\ncharacter \"c\" named \"C\" at \"room\" { stat \"input\" = 12;\n")
	for d := 0; d < recipes; d++ {
		fmt.Fprintf(&source, "stat \"derived:%d\" = 0;\n", d)
	}
	source.WriteString("}\nequipment \"nql-equipment-v1\" {\n")
	for d := 0; d < recipes; d++ {
		fmt.Fprintf(&source, "derive stat \"derived:%d\" { term stat \"input\" offset -10 divide 2; }\n", d)
	}
	source.WriteString("}\n")
	return source.String()
}

func TestDerivedCapacityCoreAndCompilerBoundary(t *testing.T) {
	for _, count := range []int{17, 32, 64, 65} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			w, e := core.NewWorld(derivedCapacityFixture(1, count))
			_, diagnostics := compiler.CompileWorld("capacity.nql", derivedCapacitySource(count))
			if count <= 64 {
				if e != nil || len(diagnostics) != 0 {
					t.Fatalf("valid recipe count: %v %+v", e, diagnostics)
				}
				status, e := w.CharacterStatus("character:0")
				if e != nil || len(status.Stats) != count+1 {
					t.Fatal("accepted recipes missing from status")
				}
			} else if e == nil || w != nil || len(diagnostics) == 0 || diagnostics[0].Position.Line == 0 {
				t.Fatal("65-recipe world accepted or unpositioned compiler diagnostic")
			}
		})
	}
}

func TestDerivedCapacityPreservesOtherBoundsAndAcyclicInputs(t *testing.T) {
	for _, name := range []string{"slots", "overrides", "terms", "self", "forward"} {
		t.Run(name, func(t *testing.T) {
			b := derivedCapacityFixture(1, 64)
			switch name {
			case "slots":
				for i := 0; i < 17; i++ {
					b.Equipment.Slots = append(b.Equipment.Slots, core.SlotDefinition{ID: core.SlotID(fmt.Sprint("slot:", i)), Capacity: 1})
				}
			case "overrides":
				m := core.EquipmentMode{ID: "mode"}
				for i := 0; i < 17; i++ {
					m.Overrides = append(m.Overrides, core.StatBaseOverride{Stat: core.StatID(fmt.Sprint("derived:", i)), Recipe: core.ScalarRecipe{Base: 10}})
				}
				b.Equipment.Definitions = []core.EquipmentDefinition{{ID: "d", Name: "D", DefaultMode: "mode", Modes: []core.EquipmentMode{m}}}
			case "terms":
				for len(b.Equipment.Derived[63].Terms) < 9 {
					b.Equipment.Derived[63].Terms = append(b.Equipment.Derived[63].Terms, core.StatTerm{Stat: "input", Divisor: 1})
				}
			case "self":
				b.Equipment.Derived[63].Terms[0].Stat = "derived:63"
			case "forward":
				b.Equipment.Derived[0].Terms[0].Stat = "derived:63"
			}
			if w, e := core.NewWorld(b); e == nil || w != nil {
				t.Fatal("recipe capacity weakened another invariant")
			}
		})
	}
}

func TestDerivedCapacity4096ExplanationsEffectsArchiveAndReceipts(t *testing.T) {
	w, e := core.NewWorld(derivedCapacityFixture(64, 64))
	if e != nil {
		t.Fatal(e)
	}
	r := core.Request{ID: "sheet:request:1", ExpectedRevision: 0, Actor: core.Ref{Kind: core.CharacterKind, ID: "character:0"}, Operations: []core.Operation{{Apply: &core.Effect{ID: "effect:input", Source: core.Ref{Kind: core.CharacterKind, ID: "character:0"}, Target: core.Ref{Kind: core.CharacterKind, ID: "character:0"}, Mode: core.OnCharacter, Stat: "input", Amount: 2}}}}
	receipt, e := w.Execute(r)
	if e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	if _, e = nativesave.Write(&archive, w.ExportCheckpoint(), nativesave.SessionState{Campaign: "sheet", Next: 2}); e != nil {
		t.Fatal(e)
	}
	loaded, e := nativesave.Read(bytes.NewReader(archive.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("archive changed canonical facts or receipt history")
	}
	count := 0
	for c := 0; c < 64; c++ {
		status, e := loaded.World.CharacterStatus(core.CharacterID(fmt.Sprint("character:", c)))
		if e != nil {
			t.Fatal(e)
		}
		for _, stat := range status.Stats {
			if !strings.HasPrefix(string(stat.Stat), "derived:") {
				continue
			}
			want := int64(1)
			if c == 0 {
				want = 2 // floor(((12 + 2) - 10) / 2), through the shared effect evaluator.
			}
			if stat.Effective != want || stat.Recipe == nil {
				t.Fatalf("derived explanation: %+v", stat)
			}
			count++
		}
	}
	if count != 4096 {
		t.Fatal("missing derived explanations")
	}
	again, e := loaded.World.Execute(r)
	if e != nil || !reflect.DeepEqual(again, receipt) || loaded.World.Revision() != 1 {
		t.Fatal("reconstructed historical retry changed outcome")
	}
}
