package compiler

import "github.com/MoonlightByte/firelight/core"

type clockNode struct{ Unit, Tick token }

func (p *parser) clock() clockNode {
	n := clockNode{Unit: p.str()}
	p.expect("at")
	n.Tick = p.num()
	p.expect(";")
	return n
}
func (a *analyzer) clock(n clockNode) *core.Clock {
	a.requireEquipmentID(n.Unit)
	return &core.Clock{Unit: n.Unit.Text, Tick: a.unsignedLiteral(n.Tick, false)}
}
func (a *analyzer) checkModeTicks(t token, n int64) {
	if n < 0 {
		a.checkAt(coreEquipmentError("", "time", "nonnegative int64", t.Text), t)
	}
}
