package compiler

type rollResourceNode struct {
	Kind, Resource, Count, Sides token
	Minimum                      *token
	Target, Caster               abilityOperandNode
	Bonus                        recipeNode
}
type abilityRollNode struct {
	Step   token
	Faces  []token
	Engine *token // `roll <step> engine;`: NQL draws the step
}

func (p *parser) rollResource() rollResourceNode {
	r := rollResourceNode{Kind: p.choice("heal", "damage", "expand"), Target: p.abilityOperand(false)}
	p.expect("resource")
	r.Resource = p.str()
	p.expect("dice")
	r.Count = p.num()
	p.expect("sides")
	r.Sides = p.num()
	// expand requires a floor; heal may declare one (damage's is refused by the checker)
	if r.Kind.Text == "expand" || p.isWord("min") {
		p.expect("min")
		minimum := p.num()
		r.Minimum = &minimum
	}
	p.expect("bonus")
	p.expect("from")
	r.Caster = p.abilityOperand(false)
	r.Bonus = p.recipe()
	return r
}
