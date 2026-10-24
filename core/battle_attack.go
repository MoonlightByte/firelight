package core

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

type preparedWeapon struct {
	encounter   *BattleEncounter
	grant       AttackGrant
	roll        WeaponRollResult
	attackCount int
	advantage   bool
	damageCount int
	origin      ItemID // a ranged shot's ammunition origin
}

func (b *Blueprint) prepareWeapon(v WeaponAttack, actor Ref) (preparedWeapon, error) {
	out := preparedWeapon{}
	if err := b.checkBattleStatic(Operation{WeaponAttack: &v}); err != nil {
		return out, err
	}
	e, err := b.checkBattleWindow(v.BattleWindow, actor)
	if err != nil {
		return out, err
	}
	out.encounter = e
	if v.Allowance == "" && e.ActionUsed {
		return out, battleFault("action", "one unused action", "already spent")
	}
	a := battleParticipant(e, v.Actor)
	t := battleParticipant(e, v.Target)
	if t == nil || a.Admission.Side == t.Admission.Side || !b.battleAble(v.Target) || character(b, v.Target).Location != e.Location {
		return out, battleFault("target", "present able opposing participant", string(v.Target))
	}
	grant, found, err := selectedEquipmentGrant(b, v.Actor, v.Grant)
	if err != nil {
		return out, err
	}
	out.grant = grant
	if !found || (out.grant.Melee == nil && out.grant.Ranged == nil) {
		return out, battleFault("grant", "currently worn executable melee or ranged grant", string(v.Grant))
	}
	if err := b.checkWeaponCondition(v, grant); err != nil {
		return out, err
	}
	var stat StatID
	if out.grant.Ranged != nil {
		stat = out.grant.Ranged.Stat
	} else {
		stat = out.grant.Melee.Stat
	}
	if err := b.CheckStat(v.Actor, stat); err != nil {
		return out, err
	}
	distance := gridDistance(a.Admission.Position, t.Admission.Position, b.Battle.Rules.SquareSize)
	if out.grant.Ranged != nil {
		if distance > out.grant.Ranged.Maximum {
			return out, battleFault("range", "target within authored maximum range", fmt.Sprint(distance))
		}
		origin, err := b.checkWeaponAmmunition(v, out.grant.Ranged)
		if err != nil {
			return out, err
		}
		out.origin = origin
	} else {
		if v.Ammunition != "" {
			return out, battleFault("ammunition", "no ammunition on melee attack", string(v.Ammunition))
		}
		if distance > out.grant.Melee.Reach {
			return out, battleFault("reach", "target within authored melee reach", fmt.Sprint(distance))
		}
	}
	out.roll = WeaponRollResult{Source: a.Admission.Controller, Modifier: out.grant.ToHit, DamageBonus: out.grant.DamageBonus, DamageKind: out.grant.DamageKind, Actual: "0", CallsBefore: b.Battle.State.Random.Calls, CallsAfter: b.Battle.State.Random.Calls}
	if a.Admission.Controller == AgentController && v.Human != nil {
		return out, battleFault("dice source", "runtime-generated agent dice", "supplied human record")
	}
	defense, err := explain(b, v.Target, b.Battle.Rules.Defense)
	if err != nil {
		return out, err
	}
	out.roll.Defense = defense.Effective
	advantage, disadvantage := false, false
	if out.grant.Ranged != nil {
		out.roll.Causes = b.rangedCauses(v, out, distance)
		disadvantage = len(out.roll.Causes) != 0
	}
	for _, c := range b.Conditions {
		for _, p := range b.Battle.Rules.RollPolicies {
			target := v.Actor
			if p.On == "target" {
				target = v.Target
			}
			if c.Target == target && c.Type == p.Condition {
				out.roll.Causes = append(out.roll.Causes, BattleRollCause{Condition: c.ID, Direction: p.Direction})
				if p.Direction == "advantage" {
					advantage = true
				} else {
					disadvantage = true
				}
			}
		}
	}
	for _, i := range b.Items {
		if i.Wearer != v.Actor || i.Equipment == nil {
			continue
		}
		for _, p := range b.Battle.Rules.ArmorPolicies {
			if i.Equipment.Definition == p.Definition && i.Equipment.Mode == p.Mode && p.AttackStat == stat && !b.hasCondition(v.Actor, p.Training) {
				out.roll.Causes = append(out.roll.Causes, BattleRollCause{Equipment: &EquipmentOrigin{Item: i.ID, Definition: p.Definition, Mode: p.Mode}, Direction: "disadvantage"})
				disadvantage = true
			}
		}
	}
	sort.Slice(out.roll.Causes, func(i, j int) bool {
		a, z := out.roll.Causes[i], out.roll.Causes[j]
		if a.Rule != z.Rule {
			return a.Rule < z.Rule
		}
		if a.Character != z.Character {
			return a.Character < z.Character
		}
		if a.Condition != z.Condition {
			return a.Condition < z.Condition
		}
		if a.Equipment != nil && z.Equipment != nil {
			if a.Equipment.Item != z.Equipment.Item {
				return a.Equipment.Item < z.Equipment.Item
			}
		}
		return a.Direction < z.Direction
	})
	out.attackCount = 1
	if advantage != disadvantage {
		out.attackCount = 2
	}
	out.advantage = advantage && !disadvantage
	return out, nil
}
func validFaces(faces []int64, sides int64) error {
	for _, f := range faces {
		if f < 1 || f > sides {
			return battleFault("face", fmt.Sprintf("raw face1..%d", sides), fmt.Sprint(f))
		}
	}
	return nil
}
func (b *Blueprint) selectWeaponRoll(p *preparedWeapon, faces []int64) error {
	if len(faces) != p.attackCount {
		return battleFault("attack dice", fmt.Sprintf("exactly%d faces", p.attackCount), fmt.Sprint(len(faces)))
	}
	if err := validFaces(faces, b.Battle.Rules.DieSides); err != nil {
		return err
	}
	p.roll.Attack = cloneFaces(faces)
	p.roll.Selected = keptFace(faces, p.advantage)
	r := b.Battle.Rules
	score, err := scoreAttack(p.roll.Selected, p.roll.Modifier, p.roll.Defense, r.MissFace, r.CriticalFace)
	if err != nil {
		return err
	}
	p.roll.Total, p.roll.Hit, p.roll.Critical = score.Total, score.Hit, score.Critical
	if p.roll.Hit {
		p.damageCount = int(attackDiceCount(p.grant.DamageCount, r.CriticalMultiplier, p.roll.Critical))
	}
	return nil
}
func (b *Blueprint) finishWeaponDamage(p *preparedWeapon, target CharacterID, faces []int64) error {
	if len(faces) != p.damageCount {
		return battleFault("damage dice", fmt.Sprintf("exactly%d faces", p.damageCount), fmt.Sprint(len(faces)))
	}
	if err := validFaces(faces, p.grant.DamageSides); err != nil {
		return err
	}
	p.roll.Damage = cloneFaces(faces)
	sum := big.NewInt(0)
	if !p.roll.Hit {
		p.roll.RawDamage = "0"
		return nil
	}
	sum.SetInt64(p.grant.DamageBonus)
	for _, f := range faces {
		sum.Add(sum, big.NewInt(f))
	}
	p.roll.RawDamage = sum.String()
	resist, vulnerable, immune := false, false, false
	for _, c := range b.Conditions {
		if c.Target != target {
			continue
		}
		for _, policy := range b.Battle.Rules.DamagePolicies {
			if policy.Condition == c.Type && policy.Kind == p.grant.DamageKind {
				p.roll.DamageCauses = append(p.roll.DamageCauses, BattleDamageCause{c.ID, policy.Treatment})
				switch policy.Treatment {
				case "resistance":
					resist = true
				case "vulnerability":
					vulnerable = true
				case "immunity":
					immune = true
				}
			}
		}
	}
	sort.Slice(p.roll.DamageCauses, func(i, j int) bool {
		a, z := p.roll.DamageCauses[i], p.roll.DamageCauses[j]
		if a.Condition != z.Condition {
			return a.Condition < z.Condition
		}
		return a.Treatment < z.Treatment
	})
	sum = treatDamage(sum, resist, vulnerable, immune)
	if !sum.IsInt64() {
		return fail("E_OVERFLOW", string(target), "damage", "final requested int64", sum.String(), "final damage exceeds numeric domain")
	}
	p.roll.Requested = sum.Int64()
	return nil
}
func (b *Blueprint) humanWeaponInput(v WeaponAttack, actor Ref) (preparedWeapon, BattleInputRequirements, error) {
	p, err := b.prepareWeapon(v, actor)
	need := BattleInputRequirements{}
	if err != nil {
		return p, need, err
	}
	if p.roll.Source != HumanController {
		return p, need, battleFault("input query", "human input only", "agent")
	}
	rolls := v.Human
	if rolls == nil {
		rolls = &HumanAttackRolls{}
	}
	if len(rolls.Attack) > p.attackCount || len(rolls.Damage) > 200 {
		return p, need, battleFault("dice", "required prefix only", "extra")
	}
	if err = validFaces(rolls.Attack, b.Battle.Rules.DieSides); err != nil {
		return p, need, err
	}
	if len(rolls.Attack) < p.attackCount {
		if len(rolls.Damage) > 0 {
			return p, need, battleFault("dice stage", "complete attack faces before damage", "premature damage")
		}
		need.Stage = "attack"
		need.Count = p.attackCount - len(rolls.Attack)
		need.Sides = b.Battle.Rules.DieSides
		copy := cloneWeaponRoll(p.roll)
		need.Provisional = &copy
		return p, need, nil
	}
	if err = b.selectWeaponRoll(&p, rolls.Attack); err != nil {
		return p, need, err
	}
	if len(rolls.Damage) > p.damageCount {
		return p, need, battleFault("damage dice", "required prefix only", "extra")
	}
	if err = validFaces(rolls.Damage, p.grant.DamageSides); err != nil {
		return p, need, err
	}
	if len(rolls.Damage) < p.damageCount {
		need.Stage = "damage"
		need.Count = p.damageCount - len(rolls.Damage)
		need.Sides = p.grant.DamageSides
	} else {
		need.Stage = "complete"
		if err = b.finishWeaponDamage(&p, v.Target, rolls.Damage); err != nil {
			return p, need, err
		}
	}
	copy := cloneWeaponRoll(p.roll)
	need.Provisional = &copy
	return p, need, nil
}

// BattleInput reports only input requirements from a detached current basis.
// It never rolls, reserves or publishes, and cannot preview agent randomness.
func (w *World) BattleInput(revision Revision, actor Ref, operation Operation) (BattleInputRequirements, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	if revision != w.authority.state.Revision {
		return BattleInputRequirements{}, fail("E_STALE", "", "revision", fmt.Sprint(w.authority.state.Revision), fmt.Sprint(revision), "input basis is historical")
	}
	op := cloneOperation(operation)
	b := cloneBlueprint(w.authority.state.Blueprint)
	if err := op.checkShape(); err != nil {
		return BattleInputRequirements{}, err
	}
	out := BattleInputRequirements{Revision: revision, Stage: "complete"}
	actorCheck := false
	if op.Invoke != nil {
		if d := abilityDefinition(&b, op.Invoke.Ability); d != nil {
			for _, step := range d.Steps {
				actorCheck = actorCheck || step.Check != nil && step.Check.ActorRoll
			}
		}
	}
	if actorCheck || b.Battle != nil && len(b.Battle.Rules.ConditionChecks) > 0 || len(op.ConditionRolls) > 0 || op.WeaponAttack != nil && op.WeaponAttack.Allowance != "" || op.Invoke != nil && op.Invoke.Allowance != "" {
		if err := b.CheckRef(actor); err != nil {
			return out, err
		}
		_, _, err := evaluateRequest(b, revision, Request{ID: "input-query", ExpectedRevision: revision, Actor: actor, Operations: []Operation{op}})
		var need *InputRequired
		if errors.As(err, &need) {
			need.Need.Revision = revision
			return need.Need, nil
		}
		return out, err
	}
	if op.Invoke != nil {
		need, err := b.abilityInput(*op.Invoke, actor)
		need.Revision = revision
		return need, err
	}
	if op.BeginBattle != nil {
		if err := b.checkBegin(*op.BeginBattle, actor); err != nil {
			return out, err
		}
		for _, p := range op.BeginBattle.Participants {
			if p.Controller == HumanController && p.Initiative == nil {
				out.Stage = "initiative"
				out.Count = 1
				out.Sides = b.Battle.Rules.DieSides
			}
		}
		return out, nil
	}
	if op.WeaponAttack != nil {
		_, need, err := b.humanWeaponInput(*op.WeaponAttack, actor)
		need.Revision = revision
		return need, err
	}
	return out, battleFault("input query", "BeginBattle or human WeaponAttack", "unsupported operation")
}
