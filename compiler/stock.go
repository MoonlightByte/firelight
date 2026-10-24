package compiler

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
)

type stockNode struct{ Item, Created, Amount, Handler token }

func (a *analyzer) stock(b core.Blueprint, n stockNode) (core.Operation, core.Item) {
	v := &core.SplitItem{Item: core.ItemID(n.Item.Text), Created: core.ItemID(n.Created.Text),
		Amount: a.number(n.Amount), Handler: core.CharacterID(n.Handler.Text)}
	o := core.Operation{Split: v}
	err := b.CheckOperationStatic(o)
	at := n.Item
	var f *core.Fault
	if errors.As(err, &f) {
		switch f.Field {
		case "created":
			at = n.Created
		case "quantity":
			at = n.Amount
		case "handler":
			at = n.Handler
		}
	}
	a.checkAt(err, at)
	// Introduce immutable type facts only. Earlier operations may have moved,
	// transferred or consumed the source; only Execute knows candidate values.
	stub := core.Item{ID: v.Created}
	for _, i := range b.Items {
		if i.ID == v.Item && i.Equipment != nil {
			stub.Equipment = &core.EquipmentInstance{Definition: i.Equipment.Definition}
		}
	}
	return o, stub
}
