package core

import (
	"fmt"
	"testing"
)

func TestEquipmentWorldSizeLimits(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		resize func(*Blueprint, int)
	}{
		{"characters", MaxWorldCharacters, func(b *Blueprint, n int) { b.Characters = make([]Character, n) }},
		{"items", MaxWorldItems, func(b *Blueprint, n int) { b.Items = make([]Item, n) }},
		{"locations", MaxWorldLocations, func(b *Blueprint, n int) { b.Locations = make([]Location, n) }},
		{"condition definitions", MaxWorldConditionDefinitions, func(b *Blueprint, n int) { b.ConditionDefinitions = make([]ConditionDefinition, n) }},
		{"conditions", MaxWorldConditions, func(b *Blueprint, n int) { b.Conditions = make([]ConditionInstance, n) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Blueprint{Equipment: &EquipmentRules{Version: EquipmentVersion}}
			tt.resize(&b, tt.limit)
			b.Clock = &Clock{Unit: "tick"}
			if err := validateClock(&b); err != nil {
				t.Fatal(err)
			}
			if err := validateEquipment(newDerivation(&b)); err != nil {
				t.Fatal("boundary rejected", err)
			}
			tt.resize(&b, tt.limit+1)
			if err := validateClock(&b); err == nil {
				t.Fatal("clock accepted over boundary")
			}
			if err := validateEquipment(newDerivation(&b)); err == nil {
				t.Fatal("over boundary accepted")
			}
		})
	}
}

func TestEquipmentDefinitionWorldLimit(t *testing.T) {
	b := Blueprint{Equipment: &EquipmentRules{Version: EquipmentVersion}}
	for i := 0; i < MaxWorldEquipmentDefinitions; i++ {
		b.Equipment.Definitions = append(b.Equipment.Definitions, EquipmentDefinition{ID: EquipmentDefinitionID(fmt.Sprint(i)), DefaultMode: "m", Modes: []EquipmentMode{{ID: "m"}}})
	}
	if err := validateEquipment(newDerivation(&b)); err != nil {
		t.Fatal(err)
	}
	b.Equipment.Definitions = append(b.Equipment.Definitions, EquipmentDefinition{ID: "overflow"})
	if err := validateEquipment(newDerivation(&b)); err == nil {
		t.Fatal("over definition cap accepted")
	}
}
