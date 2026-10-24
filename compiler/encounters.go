package compiler

import (
	"fmt"
	"github.com/MoonlightByte/firelight/core"
)

type encounterPositionNode struct{ Character, X, Y token }
type encounterDefinitionNode struct {
	ID, Name, Location    token
	Protected, Opposition []encounterPositionNode
}

func (p *parser) encounterDefinition() encounterDefinitionNode {
	p.expect("encounter")
	n := encounterDefinitionNode{ID: p.str()}
	p.expect("named")
	n.Name = p.str()
	p.expect("at")
	n.Location = p.str()
	p.expect("{")
	for p.peek().Text != "}" && p.err == nil {
		kind := p.choice("protected", "opposition")
		var position encounterPositionNode
		if kind.Text == "opposition" {
			position.Character = p.str()
		}
		p.expect("at")
		position.X = p.num()
		position.Y = p.num()
		p.expect(";")
		if kind.Text == "protected" {
			n.Protected = append(n.Protected, position)
		} else {
			n.Opposition = append(n.Opposition, position)
		}
	}
	p.expect("}")
	p.expect(";")
	return n
}
func (a *analyzer) encounterDefinitions(b core.Blueprint, nodes []encounterDefinitionNode) []core.BattleEncounterDefinition {
	var out []core.BattleEncounterDefinition
	seen := map[string]bool{}
	for i, n := range nodes {
		a.requireEquipmentID(n.ID)
		if seen[n.ID.Text] || i >= 16 {
			a.checkAt(&core.Fault{Code: "E_ENCOUNTER", Expected: "at most 16 unique encounter definitions", Actual: n.ID.Text, Message: "duplicate encounter identity or capacity exceeded"}, n.ID)
		}
		seen[n.ID.Text] = true
		a.checkRef(b, core.LocationKind, n.Location)
		d := core.BattleEncounterDefinition{ID: core.EncounterID(n.ID.Text), Name: n.Name.Text, Location: core.LocationID(n.Location.Text)}
		positions := map[string]token{"id": n.ID, "name": n.Name, "location": n.Location, "formation": n.ID}
		for j, p := range n.Protected {
			d.ProtectedPositions = append(d.ProtectedPositions, core.BattlePosition{X: a.battleNumber(p.X, -1024, 1024), Y: a.battleNumber(p.Y, -1024, 1024)})
			positions[fmt.Sprintf("protected[%d].x", j)] = p.X
			positions[fmt.Sprintf("protected[%d].y", j)] = p.Y
		}
		for j, p := range n.Opposition {
			a.checkRef(b, core.CharacterKind, p.Character)
			d.Opposition = append(d.Opposition, core.EncounterOpponent{Character: core.CharacterID(p.Character.Text), Position: core.BattlePosition{X: a.battleNumber(p.X, -1024, 1024), Y: a.battleNumber(p.Y, -1024, 1024)}})
			positions[fmt.Sprintf("opposition[%d].character", j)] = p.Character
			positions[fmt.Sprintf("opposition[%d].x", j)] = p.X
			positions[fmt.Sprintf("opposition[%d].y", j)] = p.Y
		}
		err := b.CheckBattleEncounterDefinition(d)
		at := n.ID
		if f, ok := err.(*core.Fault); ok {
			if token, exists := positions[f.Field]; exists {
				at = token
			}
		}
		a.checkAt(err, at)
		out = append(out, d)
	}
	return out
}
