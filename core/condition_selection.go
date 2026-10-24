package core

// Selection never removes an instance/effect or changes presence predicates.
// The winner derives from existing provenance; it is never stored. A
// derivation finds each group's winner once per unchanging state.
func (d *derivation) conditionSuppressedBy(e Effect) *ConditionInstanceID {
	if e.ConditionOrigin == nil {
		return nil
	}
	i := d.instance(e.ConditionOrigin.Instance)
	if i < 0 {
		return nil
	}
	return d.instanceSuppressedBy(d.b.Conditions[i])
}

// instanceSuppressedBy names the winner that suppresses all of c's numeric
// modifiers, additive and bound alike: the latest instance of c's type on
// c's target. laterCondition is a strict weak order whose equal instances
// share an ID, so the latest instance's ID does not depend on scan order.
func (d *derivation) instanceSuppressedBy(c ConditionInstance) *ConditionInstanceID {
	def := d.definition(c.Type)
	if def == nil || def.ModifierSelection != LatestModifiers {
		return nil
	}
	if d.latest == nil {
		d.latest = map[conditionGroup]int{}
	}
	group := conditionGroup{c.Type, c.Target}
	w, ok := d.latest[group]
	if !ok {
		w = -1
		for _, i := range d.conditionsOn(c.Target) {
			if d.b.Conditions[i].Type == c.Type && (w < 0 || laterCondition(d.b.Conditions[i], d.b.Conditions[w])) {
				w = i
			}
		}
		d.latest[group] = w
	}
	if w < 0 || !laterCondition(d.b.Conditions[w], c) || d.b.Conditions[w].ID == c.ID {
		return nil
	}
	id := d.b.Conditions[w].ID
	return &id
}

func laterCondition(a, z ConditionInstance) bool {
	if a.AppliedRevision != z.AppliedRevision {
		return a.AppliedRevision > z.AppliedRevision
	}
	aop, zop, astep, zstep := -1, -1, -1, -1
	var apath, zpath []int
	if a.Cause != nil {
		aop = a.Cause.Operation
		if o := a.Cause.AbilityOrigin; o != nil {
			astep, apath = o.Step, o.Path
		}
	}
	if z.Cause != nil {
		zop = z.Cause.Operation
		if o := z.Cause.AbilityOrigin; o != nil {
			zstep, zpath = o.Step, o.Path
		}
	}
	if aop != zop {
		return aop > zop
	}
	if astep != zstep {
		return astep > zstep
	}
	for i := 0; i < len(apath) && i < len(zpath); i++ {
		if apath[i] != zpath[i] {
			return apath[i] > zpath[i]
		}
	}
	if len(apath) != len(zpath) {
		return len(apath) > len(zpath)
	}
	// Genesis and same-creation provenance have no finer causal timestamp.
	// This explicit deterministic tie policy never derives identity from names.
	return a.ID > z.ID
}
