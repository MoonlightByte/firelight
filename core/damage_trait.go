package core

import (
	"fmt"
	"math/big"
	"sort"
)

// DamageTrait is a character's own treatment of one damage kind, from its
// stat block: resistance, vulnerability or immunity.
type DamageTrait struct {
	Treatment string `json:"treatment"`
	Kind      string `json:"kind"`
}

// DamageTraitPolicy gives every instance of a Condition type a treatment of
// one damage kind on its target, as Battle's damage policies do.
type DamageTraitPolicy struct {
	Condition ConditionTypeID `json:"condition"`
	Treatment string          `json:"treatment"`
	Kind      string          `json:"kind"`
}

// AttackTreatment is one source of a treatment an attack's damage met: the
// target's own trait, or a Condition instance on it.
type AttackTreatment struct {
	Character CharacterID         `json:"character,omitempty"`
	Condition ConditionInstanceID `json:"condition,omitempty"`
	Treatment string              `json:"treatment"`
}

const (
	MaxDamageTraitsPerTreatment = 32
	MaxDamageTraitPolicies      = 16
)

var treatments = []string{"resistance", "vulnerability", "immunity"}

func validTreatment(t string) bool {
	return t == "resistance" || t == "vulnerability" || t == "immunity"
}

func traitFault(subject, field, expected, actual string) error {
	return fail("E_ATTACK", subject, field, expected, actual, "invalid "+field)
}

// checkDamageTraits checks a character's own traits: known treatments,
// valid kinds, each kind once per treatment, at most 32 of each.
func checkDamageTraits(c Character) error {
	seen, count := map[DamageTrait]bool{}, map[string]int{}
	for _, t := range c.DamageTraits {
		switch {
		case !validTreatment(t.Treatment):
			return traitFault(string(c.ID), "damage trait", "resistance, vulnerability or immunity", t.Treatment)
		case !validID(t.Kind):
			return traitFault(string(c.ID), "damage trait", "valid damage kind", t.Kind)
		case seen[t]:
			return traitFault(string(c.ID), "damage trait", "each kind once per treatment", t.Kind)
		}
		seen[t] = true
		count[t.Treatment]++
		if count[t.Treatment] > MaxDamageTraitsPerTreatment {
			return traitFault(string(c.ID), "damage trait", fmt.Sprintf("at most %d of each treatment", MaxDamageTraitsPerTreatment), t.Treatment)
		}
	}
	return nil
}

// checkTraitsHaveLaw refuses a character's own traits in a world without the
// attack law, which alone applies them: Battle has its own policies.
func checkTraitsHaveLaw(b *Blueprint, c Character) error {
	if len(c.DamageTraits) > 0 && b.Attacks == nil {
		return traitFault(string(c.ID), "damage trait", "world attacks law", "absent")
	}
	return nil
}

// CheckDamageTraits and CheckDamageTraitPolicies are the checks a compiler
// positions at its tokens.
func CheckDamageTraits(c Character) error        { return checkDamageTraits(c) }
func CheckDamageTraitPolicies(b Blueprint) error { return validateDamageTraitPolicies(&b) }

// validateDamageTraitPolicies checks the world's policies: an attack law,
// declared Condition types, known treatments, valid kinds, unique, at most
// 16.
func validateDamageTraitPolicies(b *Blueprint) error {
	if len(b.DamageTraits) == 0 {
		return nil
	}
	if b.Attacks == nil {
		return traitFault("", "damage trait", "world attacks law", "absent")
	}
	if len(b.DamageTraits) > MaxDamageTraitPolicies {
		return traitFault("", "damage trait", fmt.Sprintf("at most %d policies", MaxDamageTraitPolicies), fmt.Sprint(len(b.DamageTraits)))
	}
	seen := map[DamageTraitPolicy]bool{}
	for _, p := range b.DamageTraits {
		switch {
		case conditionDefinition(b, p.Condition) == nil:
			return traitFault(string(p.Condition), "damage trait", "declared Condition type", string(p.Condition))
		case !validTreatment(p.Treatment):
			return traitFault(string(p.Condition), "damage trait", "resistance, vulnerability or immunity", p.Treatment)
		case !validID(p.Kind):
			return traitFault(string(p.Condition), "damage trait", "valid damage kind", p.Kind)
		case seen[p]:
			return traitFault(string(p.Condition), "damage trait", "unique policy", p.Kind)
		}
		seen[p] = true
	}
	return nil
}

// attackTreatments lists every source of a treatment of the kind on the
// target: its own traits first, by treatment, then its Condition instances
// under a policy, by instance, each in the same treatment order.
func attackTreatments(b *Blueprint, target CharacterID, kind string) (out []AttackTreatment, resist, vulnerable, immune bool) {
	c := character(b, target)
	for _, t := range treatments {
		for _, v := range c.DamageTraits {
			if v.Treatment == t && v.Kind == kind {
				out = append(out, AttackTreatment{Character: target, Treatment: t})
			}
		}
	}
	var held []ConditionInstance
	for _, i := range b.Conditions {
		if i.Target == target {
			held = append(held, i)
		}
	}
	sort.Slice(held, func(i, j int) bool { return held[i].ID < held[j].ID })
	for _, i := range held {
		for _, t := range treatments {
			for _, p := range b.DamageTraits {
				if p.Condition == i.Type && p.Treatment == t && p.Kind == kind {
					out = append(out, AttackTreatment{Condition: i.ID, Treatment: t})
				}
			}
		}
	}
	for _, t := range out {
		switch t.Treatment {
		case "resistance":
			resist = true
		case "vulnerability":
			vulnerable = true
		case "immunity":
			immune = true
		}
	}
	return out, resist, vulnerable, immune
}

// DamageTreated records a Damage that carried a kind: the kind and the amount
// as asked, and every source of a treatment it met. The event's requested is
// the treated amount. UnusedFaces are the check faces no check took: the
// host cannot know beforehand whether the treated damage triggers one.
type DamageTreated struct {
	Kind        string            `json:"kind"`
	Amount      int64             `json:"amount"`
	Treatments  []AttackTreatment `json:"treatments"`
	UnusedFaces []AttackCheckFace `json:"unused_faces,omitempty"`
}

func cloneDamageTreated(v *DamageTreated) *DamageTreated {
	if v == nil {
		return nil
	}
	out := *v
	out.Treatments = append(make([]AttackTreatment, 0, len(v.Treatments)), v.Treatments...)
	out.UnusedFaces = append([]AttackCheckFace(nil), v.UnusedFaces...)
	return &out
}

// checkDamageKind checks a kind: only Damage carries one, it is valid text,
// and a world with a Battle refuses it, since Battle keeps its own damage
// policies.
func (b Blueprint) checkDamageKind(damage bool, v ResourceOp) error {
	switch {
	case !damage:
		return fail("E_TYPE", string(v.Character), "kind", "damage", "another resource operation", "only damage carries a kind")
	case !validID(v.Kind):
		return traitFault(string(v.Character), "kind", "valid damage kind", v.Kind)
	case b.Battle != nil:
		return traitFault(string(v.Character), "kind", "world without Battle", "Battle")
	}
	return nil
}

// treatKindedDamage applies the damage law, shared with attacks and Battle,
// to a Damage's amount with the target's traits of its kind.
func treatKindedDamage(b *Blueprint, v ResourceOp) (*DamageTreated, int64, error) {
	treatments, resist, vulnerable, immune := attackTreatments(b, v.Character, v.Kind)
	amount := treatDamage(big.NewInt(v.Amount), resist, vulnerable, immune)
	if !amount.IsInt64() {
		return nil, 0, fail("E_OVERFLOW", string(v.Character), "damage", "final requested int64", amount.String(), "final damage exceeds numeric domain")
	}
	treated := &DamageTreated{Kind: v.Kind, Amount: v.Amount, Treatments: append(make([]AttackTreatment, 0, len(treatments)), treatments...)}
	return treated, amount.Int64(), nil
}

// takeUnusedFaces records the operation's check faces no check took and
// marks them taken, so a treated Damage never refuses them.
func takeUnusedFaces(treated *DamageTreated, cursor *conditionInputCursor) {
	if cursor == nil {
		return
	}
	for _, f := range cursor.Values[cursor.Next:] {
		treated.UnusedFaces = append(treated.UnusedFaces, AttackCheckFace{Condition: f.Condition, Face: f.Face})
	}
	cursor.Next = len(cursor.Values)
}
