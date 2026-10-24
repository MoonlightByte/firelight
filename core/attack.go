package core

import (
	"fmt"
	"math/big"
)

// AttackLaw is a world's attack scoring outside Battle: the target's defense
// stat, the Resource that damage lowers, and the die law. Battle keeps its
// own rules, so a world declares one or the other.
type AttackLaw struct {
	Defense            StatID     `json:"defense"`
	Health             ResourceID `json:"health"`
	DieSides           int64      `json:"die_sides"`
	MissFace           int64      `json:"miss_face"`
	CriticalFace       int64      `json:"critical_face"`
	CriticalMultiplier int64      `json:"critical_multiplier"`
}

const AttackScored EventKind = "AttackScored"

// Profile bounds: a critical multiplies the dice count by at most 8, so the
// largest total stays small.
const (
	MaxAttackDiceCount   = 64
	MaxAttackDamageSides = 1000
)

// AttackProfile is the attack's typed numbers, from a stat block or a sheet.
type AttackProfile struct {
	ToHit       int64  `json:"to_hit"`
	DamageCount int64  `json:"damage_count"`
	DamageSides int64  `json:"damage_sides"`
	DamageBonus int64  `json:"damage_bonus"`
	DamageKind  string `json:"damage_kind"`
}

// Attack scores one attack from typed faces and, on a hit, applies its damage
// to the law's health Resource. The engine never draws: every face and total
// is the host's. Damage and CriticalDamage are sums of the damage dice as
// rolled, the bonus excluded; only the one the outcome needs is used. With
// neither, the attack is scored and nothing is applied. CheckFaces are the
// target's faces for the checks the damage triggers; those no check takes
// are unused, never refused.
type Attack struct {
	Attacker       CharacterID       `json:"attacker"`
	Profile        AttackProfile     `json:"profile"`
	Target         CharacterID       `json:"target"`
	Mode           RollMode          `json:"mode,omitempty"`
	Faces          []int64           `json:"faces"`
	Damage         *int64            `json:"damage,omitempty"`
	CriticalDamage *int64            `json:"critical_damage,omitempty"`
	CheckFaces     []AttackCheckFace `json:"check_faces,omitempty"`
}

// AttackCheckFace is one face for a check on one of the target's Condition
// instances.
type AttackCheckFace struct {
	Condition ConditionInstanceID `json:"condition"`
	Face      int64               `json:"face"`
}

// AttackResult is the AttackScored event. Kept is the face that counts and
// Defense the target's effective defense. On a hit with a total, DamageCount
// is the dice behind it (critical applied), RawDamage the total plus the
// bonus, Treatments the sources of a resistance, vulnerability or immunity
// of its kind, and Damage the amount requested of the Damage. Unused names the
// damage clauses the outcome did not need, and UnusedFaces the check faces
// no check took.
type AttackResult struct {
	Intent       Attack            `json:"intent"`
	Kept         int64             `json:"kept"`
	Total        int64             `json:"total"`
	Defense      int64             `json:"defense"`
	Hit          bool              `json:"hit"`
	Critical     bool              `json:"critical"`
	NaturalMiss  bool              `json:"natural_miss"`
	DamageCount  int64             `json:"damage_count,omitempty"`
	DamageRolled *int64            `json:"damage_rolled,omitempty"`
	RawDamage    string            `json:"raw_damage,omitempty"`
	Treatments   []AttackTreatment `json:"treatments,omitempty"`
	Damage       *int64            `json:"damage,omitempty"`
	Unused       []string          `json:"unused,omitempty"`
	UnusedFaces  []AttackCheckFace `json:"unused_faces,omitempty"`
}

// attackScore is the attack law's verdict on a kept face.
type attackScore struct {
	Total         int64
	Hit, Critical bool
}

// keptFace is the face that counts: the higher of two with advantage, the
// lower without, and the only face otherwise.
func keptFace(faces []int64, advantage bool) int64 {
	kept := faces[0]
	if len(faces) == 2 && ((advantage && faces[1] > faces[0]) || (!advantage && faces[1] < faces[0])) {
		kept = faces[1]
	}
	return kept
}

// scoreAttack is the attack law, shared with Battle: the total is the kept
// face plus the modifier; the critical face always hits and the miss face
// never does; any other face hits when the total reaches the defense.
func scoreAttack(kept, modifier, defense, missFace, criticalFace int64) (attackScore, error) {
	total := new(big.Int).Add(big.NewInt(kept), big.NewInt(modifier))
	if !total.IsInt64() {
		return attackScore{}, fail("E_OVERFLOW", "", "attack total", "int64", total.String(), "attack total exceeds numeric domain")
	}
	s := attackScore{Total: total.Int64(), Critical: kept == criticalFace}
	s.Hit = kept != missFace && (s.Critical || s.Total >= defense)
	return s, nil
}

// flatDamage reports whether a damage profile has no dice: count and sides
// both 0, so the damage is its bonus alone. A flat hit's damage total is 0.
func flatDamage(count, sides int64) bool {
	return count == 0 && sides == 0
}

// attackDiceCount is the damage dice of a hit: the count, multiplied on a
// critical.
func attackDiceCount(count, multiplier int64, critical bool) int64 {
	if critical {
		return count * multiplier
	}
	return count
}

// treatDamage is the damage law, shared with Battle: the raw damage floored
// at 0, halved rounding down for a resistance, then doubled for a
// vulnerability; an immunity makes it 0.
func treatDamage(raw *big.Int, resist, vulnerable, immune bool) *big.Int {
	v := new(big.Int).Set(raw)
	if v.Sign() < 0 {
		v.SetInt64(0)
	}
	if resist {
		v.Quo(v, big.NewInt(2))
	}
	if vulnerable {
		v.Mul(v, big.NewInt(2))
	}
	if immune {
		v.SetInt64(0)
	}
	return v
}

func attackFault(subject, field, expected, actual string) error {
	return fail("E_ATTACK", subject, field, expected, actual, "invalid attack "+field)
}

func validateAttackLaw(b *Blueprint) error {
	l := b.Attacks
	if l == nil {
		return nil
	}
	if b.Battle != nil {
		return attackFault("", "attacks", "no Battle, whose rules declare their own", "Battle")
	}
	switch {
	case !validID(string(l.Defense)):
		return attackFault("", "defense", "valid stat ID", string(l.Defense))
	case !validID(string(l.Health)):
		return attackFault("", "health", "valid resource ID", string(l.Health))
	case l.DieSides < 2 || l.DieSides > 1000:
		return attackFault("", "die", "2..1000", fmt.Sprint(l.DieSides))
	case l.MissFace < 1 || l.MissFace > l.DieSides:
		return attackFault("", "miss", fmt.Sprintf("1..%d", l.DieSides), fmt.Sprint(l.MissFace))
	case l.CriticalFace < 1 || l.CriticalFace > l.DieSides:
		return attackFault("", "critical", fmt.Sprintf("1..%d", l.DieSides), fmt.Sprint(l.CriticalFace))
	case l.MissFace == l.CriticalFace:
		return attackFault("", "critical", "a face other than the miss face", fmt.Sprint(l.CriticalFace))
	case l.CriticalMultiplier < 1 || l.CriticalMultiplier > 8:
		return attackFault("", "critical dice", "1..8", fmt.Sprint(l.CriticalMultiplier))
	}
	return nil
}

// checkAttack checks what does not depend on the outcome: the law, the
// characters, the profile, the mode and the faces.
func (b Blueprint) checkAttack(v Attack) error {
	l := b.Attacks
	if l == nil {
		return attackFault(string(v.Attacker), "law", "world attacks law", "absent")
	}
	for _, c := range []CharacterID{v.Attacker, v.Target} {
		if err := b.CheckRef(Ref{CharacterKind, string(c)}); err != nil {
			return err
		}
	}
	p := v.Profile
	flat := flatDamage(p.DamageCount, p.DamageSides)
	switch {
	case !flat && (p.DamageCount < 1 || p.DamageCount > MaxAttackDiceCount):
		return attackFault(string(v.Attacker), "damage dice", fmt.Sprintf("1..%d", MaxAttackDiceCount), fmt.Sprint(p.DamageCount))
	case !flat && (p.DamageSides < 1 || p.DamageSides > MaxAttackDamageSides):
		return attackFault(string(v.Attacker), "damage sides", fmt.Sprintf("1..%d", MaxAttackDamageSides), fmt.Sprint(p.DamageSides))
	case !validID(p.DamageKind):
		return attackFault(string(v.Attacker), "damage kind", "valid damage kind", p.DamageKind)
	case v.Mode != "" && v.Mode != Advantage && v.Mode != Disadvantage:
		return attackFault(string(v.Attacker), "mode", "omitted, advantage or disadvantage", string(v.Mode))
	}
	want, expected := 1, "exactly 1 face"
	if v.Mode != "" {
		want, expected = 2, "exactly 2 faces"
	}
	if len(v.Faces) != want {
		return attackFault(string(v.Attacker), "faces", expected, fmt.Sprint(len(v.Faces)))
	}
	for _, f := range v.Faces {
		if f < 1 || f > l.DieSides {
			return attackFault(string(v.Attacker), "faces", fmt.Sprintf("faces in 1..%d", l.DieSides), fmt.Sprint(f))
		}
	}
	if len(v.CheckFaces) > MaxOperations {
		return attackFault(string(v.Attacker), "check faces", "at most 128", fmt.Sprint(len(v.CheckFaces)))
	}
	for _, f := range v.CheckFaces {
		if !validID(string(f.Condition)) || f.Face < 1 || f.Face > 1000 {
			return attackFault(string(v.Attacker), "check faces", "valid instance and face 1..1000", fmt.Sprintf("%q %d", f.Condition, f.Face))
		}
	}
	if err := b.CheckStat(v.Target, l.Defense); err != nil {
		return err
	}
	return b.CheckResource(v.Target, l.Health)
}

// attackWork charges the Damage an attack applies, as Battle does.
func attackWork(ctx applicationContext) error {
	*ctx.work++
	if *ctx.work > MaxOperations {
		return fail("E_LIMIT", "", "work", "at most 128", "attack exceeded", "attack work budget exceeded")
	}
	return nil
}

// applyAttack scores the attack, applies its damage through the Damage
// operation, and ends with the AttackScored event, whose intent replays it.
func applyAttack(b *Blueprint, v Attack, ctx applicationContext) ([]Event, error) {
	l := b.Attacks
	defense, err := explain(b, v.Target, l.Defense)
	if err != nil {
		return nil, err
	}
	result := AttackResult{Intent: *cloneAttack(&v), Kept: keptFace(v.Faces, v.Mode == Advantage), Defense: defense.Effective}
	score, err := scoreAttack(result.Kept, v.Profile.ToHit, result.Defense, l.MissFace, l.CriticalFace)
	if err != nil {
		return nil, err
	}
	result.Total, result.Hit, result.Critical = score.Total, score.Hit, score.Critical
	result.NaturalMiss = result.Kept == l.MissFace
	used, usedName := v.Damage, "damage"
	other, otherName := v.CriticalDamage, "critical_damage"
	if result.Critical {
		used, usedName, other, otherName = other, otherName, used, usedName
	}
	result.UnusedFaces = append([]AttackCheckFace(nil), v.CheckFaces...)
	if !result.Hit {
		for _, c := range []struct {
			total *int64
			name  string
		}{{v.Damage, "damage"}, {v.CriticalDamage, "critical_damage"}} {
			if c.total != nil {
				result.Unused = append(result.Unused, c.name)
			}
		}
		return []Event{{Kind: AttackScored, Attack: &result}}, nil
	}
	if other != nil {
		result.Unused = append(result.Unused, otherName)
	}
	if used == nil {
		if other != nil {
			return nil, attackFault(string(v.Attacker), "damage", usedName, "absent")
		}
		return []Event{{Kind: AttackScored, Attack: &result}}, nil
	}
	count := attackDiceCount(v.Profile.DamageCount, l.CriticalMultiplier, result.Critical)
	if *used < count || *used > count*v.Profile.DamageSides {
		return nil, attackFault(string(v.Attacker), usedName, fmt.Sprintf("%d..%d", count, count*v.Profile.DamageSides), fmt.Sprint(*used))
	}
	result.DamageCount, result.DamageRolled = count, copyPtr(used)
	raw := new(big.Int).Add(big.NewInt(*used), big.NewInt(v.Profile.DamageBonus))
	result.RawDamage = raw.String()
	var resist, vulnerable, immune bool
	result.Treatments, resist, vulnerable, immune = attackTreatments(b, v.Target, v.Profile.DamageKind)
	amount := treatDamage(raw, resist, vulnerable, immune)
	if !amount.IsInt64() {
		return nil, fail("E_OVERFLOW", string(v.Target), "damage", "final requested int64", amount.String(), "final damage exceeds numeric domain")
	}
	requested := amount.Int64()
	result.Damage = &requested
	var events []Event
	if requested > 0 {
		if err := attackWork(ctx); err != nil {
			return nil, err
		}
		op := Operation{Damage: &ResourceOp{Character: v.Target, Resource: l.Health, Amount: requested}}
		if err := b.CheckOperationStatic(op); err != nil {
			return nil, err
		}
		// The check faces are this attack's own: a check takes them in
		// order, and the rest are unused.
		cursor := &conditionInputCursor{}
		for _, f := range v.CheckFaces {
			cursor.Values = append(cursor.Values, ConditionRollInput{Condition: f.Condition, Target: v.Target, Face: f.Face})
		}
		child := ctx
		child.conditionInput = cursor
		events, err = applyOperation(b, op, child)
		if err != nil {
			return nil, err
		}
		result.UnusedFaces = result.UnusedFaces[cursor.Next:]
	}
	if len(result.UnusedFaces) == 0 {
		result.UnusedFaces = nil
	}
	return append(events, Event{Kind: AttackScored, Attack: &result}), nil
}

func cloneAttack(v *Attack) *Attack {
	if v == nil {
		return nil
	}
	out := *v
	out.Faces = append([]int64(nil), v.Faces...)
	out.Damage = copyPtr(v.Damage)
	out.CriticalDamage = copyPtr(v.CriticalDamage)
	out.CheckFaces = append([]AttackCheckFace(nil), v.CheckFaces...)
	return &out
}

func cloneAttackResult(v *AttackResult) *AttackResult {
	if v == nil {
		return nil
	}
	out := *v
	out.Intent = *cloneAttack(&v.Intent)
	out.DamageRolled = copyPtr(v.DamageRolled)
	out.Damage = copyPtr(v.Damage)
	out.Unused = append([]string(nil), v.Unused...)
	out.UnusedFaces = append([]AttackCheckFace(nil), v.UnusedFaces...)
	out.Treatments = append([]AttackTreatment(nil), v.Treatments...)
	return &out
}
