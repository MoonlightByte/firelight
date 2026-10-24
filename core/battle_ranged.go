package core

import (
	"fmt"
	"math"
	"math/big"
)

// RangedProfile names a finite physical ammunition lineage, not a display name
// or a second Resource balance. Descriptive attacks may have neither profile.
// The ammunition is one original stock, or any stock whose original root is
// declared from an item type, such as a catalog bow's arrows.
type RangedProfile struct {
	Stat             StatID           `json:"stat"`
	Normal           int64            `json:"normal"`
	Maximum          int64            `json:"maximum"`
	AmmunitionOrigin ItemID           `json:"ammunition_origin,omitempty"`
	AmmunitionType   ItemDefinitionID `json:"ammunition_type,omitempty"`
}

// ThrownProfile is a weapon thrown as its own projectile: no ammunition.
// Grants list it; Battle does not execute it (its grant is refused).
type ThrownProfile struct {
	Stat    StatID `json:"stat"`
	Normal  int64  `json:"normal"`
	Maximum int64  `json:"maximum"`
}

// Conditions and distances are authored for the admitted clear encounter scene.
// This policy is not a general visibility, cover, or special-senses evaluator.
type BattleRangedPolicy struct {
	CloseDistance       int64             `json:"close_distance"`
	IgnoreThreat        []ConditionTypeID `json:"ignore_threat,omitempty"`
	Concealed           []ConditionTypeID `json:"concealed,omitempty"`
	RecoveryNumerator   int64             `json:"recovery_numerator"`
	RecoveryDenominator int64             `json:"recovery_denominator"`
	RecoveryTicks       int64             `json:"recovery_ticks"`
}
type AmmunitionStock struct {
	Origin    ItemID `json:"origin"`
	Spent     int64  `json:"spent"`
	Recovered ItemID `json:"recovered,omitempty"`
}

// One claim recovers all of this actor's ammunition origins from one encounter.
// Spent is recoverable historical use, never a copy of current stock quantity.
type ActorAmmunition struct {
	Actor   CharacterID       `json:"actor"`
	Claimed bool              `json:"claimed"`
	Stocks  []AmmunitionStock `json:"stocks"`
}

func cloneRangedPolicy(p *BattleRangedPolicy) *BattleRangedPolicy {
	if p == nil {
		return nil
	}
	v := *p
	v.IgnoreThreat = append([]ConditionTypeID(nil), p.IgnoreThreat...)
	v.Concealed = append([]ConditionTypeID(nil), p.Concealed...)
	return &v
}
func cloneActorAmmunition(a []ActorAmmunition) []ActorAmmunition {
	if a == nil {
		return nil
	}
	out := append([]ActorAmmunition{}, a...)
	for i := range out {
		out[i].Stocks = append([]AmmunitionStock(nil), a[i].Stocks...)
	}
	return out
}
func (b *Blueprint) ammunitionOrigin(id ItemID) bool {
	if b.CheckRef(Ref{ItemKind, string(id)}) != nil {
		return false
	}
	i := item(b, id)
	return i.Quantity != nil && i.Equipment == nil && !i.Container && i.SplitFrom == ""
}
func (b *Blueprint) checkRangedProfile(a EquipmentAttack) error {
	p := a.Ranged
	if p == nil {
		return nil
	}
	typed := p.AmmunitionType != ""
	if a.Melee != nil || !validID(string(p.Stat)) || p.Normal < 1 || p.Maximum < p.Normal || p.Maximum > 1000000 || !typed && !b.ammunitionOrigin(p.AmmunitionOrigin) {
		return equipmentFault(a.ID, "ranged", "one executable profile, valid stat/ranges and original plain stock", "invalid")
	}
	if typed && (p.AmmunitionOrigin != "" || !b.ammunitionType(p.AmmunitionType)) {
		return equipmentFault(a.ID, "ranged", "one ammunition: an original plain stock or a declared item type without equipment", string(p.AmmunitionType))
	}
	return nil
}

// ammunitionType is an item type whose stocks can be shot: declared, and
// without equipment. Each stock is checked as plain when it is shot.
func (b *Blueprint) ammunitionType(id ItemDefinitionID) bool {
	d := b.itemDefinition(id)
	return d != nil && d.Equipment == ""
}

// shotOrigin is the origin a shot from stock i records, or "" if p cannot
// shoot it: the stock's original root, when that root is p's origin or, for
// an item type, an original plain stock declared from that type.
func (b *Blueprint) shotOrigin(i *Item, p *RangedProfile) ItemID {
	origin := b.stockAmmunitionOrigin(i)
	if p.AmmunitionType == "" {
		if origin == p.AmmunitionOrigin {
			return origin
		}
		return ""
	}
	if b.ammunitionOrigin(origin) && item(b, origin).From == p.AmmunitionType {
		return origin
	}
	return ""
}
func (b *Blueprint) checkRangedPolicy() error {
	if b.Battle == nil {
		return nil
	}
	p := b.Battle.Rules.Ranged
	if p == nil {
		if b.Equipment != nil {
			for _, d := range b.Equipment.Definitions {
				for _, m := range d.Modes {
					for _, a := range m.Attacks {
						if a.Ranged != nil {
							return battleFault("ranged policy", "authored ranged rules", "absent")
						}
					}
				}
			}
		}
		for _, t := range b.ConditionDefinitions {
			for _, a := range t.Attacks {
				if a.Ranged != nil {
					return battleFault("ranged policy", "authored ranged rules", "absent")
				}
			}
		}
		return nil
	}
	if p.CloseDistance < 0 || p.CloseDistance > 1000000 || p.RecoveryDenominator < 1 || p.RecoveryDenominator > 1000 || p.RecoveryNumerator < 0 || p.RecoveryNumerator > p.RecoveryDenominator || p.RecoveryTicks < 1 {
		return battleFault("ranged policy", "bounded distance, recovery fraction0..1 and positive ticks", "invalid")
	}
	for _, list := range [][]ConditionTypeID{p.IgnoreThreat, p.Concealed} {
		if len(list) > 16 {
			return battleFault("ranged policy", "at most 16 Conditions per list", "exceeded")
		}
		seen := map[ConditionTypeID]bool{}
		for _, id := range list {
			if conditionDefinition(b, id) == nil || seen[id] {
				return battleFault("ranged policy", "unique declared Condition types", string(id))
			}
			seen[id] = true
		}
	}
	return nil
}
func recoveredAmount(p *BattleRangedPolicy, spent int64) int64 {
	n := new(big.Int).Mul(big.NewInt(spent), big.NewInt(p.RecoveryNumerator))
	return n.Quo(n, big.NewInt(p.RecoveryDenominator)).Int64()
}
func (b *Blueprint) checkAmmunitionHistory() error {
	recovered := map[ItemID]bool{}
	for _, e := range b.Battle.State.Encounters {
		if len(e.Ammunition) > 16 {
			return battleFault("ammunition", "at most 16 shooters", "exceeded")
		}
		actors := map[CharacterID]bool{}
		for _, a := range e.Ammunition {
			p := b.Battle.Rules.Ranged
			if p == nil || actors[a.Actor] || battleParticipant(&e, a.Actor) == nil || len(a.Stocks) == 0 || len(a.Stocks) > 64 || a.Claimed && e.Status == Active {
				return battleFault("ammunition", "bounded unique admitted shooter with valid claim", string(a.Actor))
			}
			actors[a.Actor] = true
			origins := map[ItemID]bool{}
			for _, s := range a.Stocks {
				if !b.ammunitionOrigin(s.Origin) || origins[s.Origin] || s.Spent <= 0 || !a.Claimed && s.Recovered != "" {
					return battleFault("ammunition", "unique original stock with positive use", string(s.Origin))
				}
				origins[s.Origin] = true
				if a.Claimed && (recoveredAmount(p, s.Spent) > 0) != (s.Recovered != "") {
					return battleFault("ammunition recovery", "identity only for positive recovery", string(s.Recovered))
				}
				if s.Recovered != "" {
					if recovered[s.Recovered] || !b.ammunitionOrigin(s.Recovered) || s.Recovered == s.Origin {
						return battleFault("ammunition recovery", "unique recovered plain root stock", string(s.Recovered))
					}
					recovered[s.Recovered] = true
				}
			}
		}
	}
	// Origins are original catalog roots, never recovery-to-recovery chains.
	for _, e := range b.Battle.State.Encounters {
		for _, a := range e.Ammunition {
			for _, s := range a.Stocks {
				if recovered[s.Origin] {
					return battleFault("ammunition origin", "original stock", string(s.Origin))
				}
			}
		}
	}
	return nil
}
func (b *Blueprint) stockAmmunitionOrigin(i *Item) ItemID {
	for i.SplitFrom != "" {
		i = item(b, i.SplitFrom)
	}
	root := i.ID
	if b.Battle == nil {
		return root
	}
	for _, e := range b.Battle.State.Encounters {
		for _, a := range e.Ammunition {
			if a.Claimed {
				for _, s := range a.Stocks {
					if s.Recovered == root {
						return s.Origin
					}
				}
			}
		}
	}
	return root
}

// Only origins that an authored executable attack can shoot are ammunition:
// an equipment mode's or a Condition type's. This is a read-only
// classification of a validated snapshot, not new Item data.
func (b *Blueprint) projectedAmmunitionOrigin(i Item) ItemID {
	if b.Equipment == nil || i.Quantity == nil || i.Equipment != nil || i.Container {
		return ""
	}
	var attacks []EquipmentAttack
	for _, d := range b.Equipment.Definitions {
		for _, m := range d.Modes {
			attacks = append(attacks, m.Attacks...)
		}
	}
	for _, t := range b.ConditionDefinitions {
		attacks = append(attacks, t.Attacks...)
	}
	for _, a := range attacks {
		if a.Ranged != nil {
			if origin := b.shotOrigin(&i, a.Ranged); origin != "" {
				return origin
			}
		}
	}
	return ""
}

type RecoverableStock struct {
	Origin      ItemID `json:"origin"`
	Spent       int64  `json:"spent"`
	Recoverable int64  `json:"recoverable"`
}

// This historical, detached view supplies quantities for planning identities.
// It does not establish current legality: Preview/Execute also check the actor,
// encounter, location, fresh identities, world capacity and clock arithmetic.
type AmmunitionRecoveryProjection struct {
	Encounter EncounterID        `json:"encounter"`
	Location  LocationID         `json:"location"`
	Claimed   bool               `json:"claimed"`
	Ticks     int64              `json:"ticks"`
	Stocks    []RecoverableStock `json:"stocks"`
}

func (b *Blueprint) ammunitionRecoveryViews(actor CharacterID) []AmmunitionRecoveryProjection {
	if b.Battle == nil || b.Battle.Rules.Ranged == nil {
		return nil
	}
	var out []AmmunitionRecoveryProjection
	for _, e := range b.Battle.State.Encounters {
		for _, a := range e.Ammunition {
			if a.Actor != actor {
				continue
			}
			v := AmmunitionRecoveryProjection{Encounter: e.ID, Location: e.Location, Claimed: a.Claimed, Ticks: b.Battle.Rules.Ranged.RecoveryTicks}
			for _, s := range a.Stocks {
				amount := int64(0)
				if !a.Claimed {
					amount = recoveredAmount(b.Battle.Rules.Ranged, s.Spent)
				}
				v.Stocks = append(v.Stocks, RecoverableStock{Origin: s.Origin, Spent: s.Spent, Recoverable: amount})
			}
			out = append(out, v)
		}
	}
	return out
}

// checkWeaponAmmunition returns the origin the shot records.
func (b *Blueprint) checkWeaponAmmunition(v WeaponAttack, p *RangedProfile) (ItemID, error) {
	if err := b.CheckRef(Ref{ItemKind, string(v.Ammunition)}); err != nil {
		return "", err
	}
	i := item(b, v.Ammunition)
	origin := b.shotOrigin(i, p)
	if i.Quantity == nil || *i.Quantity < 1 || i.Equipment != nil || i.Container || i.Wearer != "" || i.Owner != v.Actor || !carriedBy(*i, v.Actor) || origin == "" {
		return "", battleFault("ammunition", "positive unworn plain stock of declared origin, owned and explicitly carried by actor", string(v.Ammunition))
	}
	return origin, nil
}
func (b *Blueprint) anyCondition(target CharacterID, list []ConditionTypeID) bool {
	for _, id := range list {
		if b.hasCondition(target, id) {
			return true
		}
	}
	return false
}
func (b *Blueprint) rangedCauses(v WeaponAttack, p preparedWeapon, distance int64) []BattleRollCause {
	var causes []BattleRollCause
	if distance > p.grant.Ranged.Normal {
		causes = append(causes, BattleRollCause{Rule: "long_range", Direction: "disadvantage"})
	}
	r := b.Battle.Rules.Ranged
	if b.anyCondition(v.Actor, r.Concealed) {
		return causes
	}
	actor := battleParticipant(p.encounter, v.Actor)
	for _, other := range p.encounter.Roster {
		a := other.Admission
		if a.Side == actor.Admission.Side || !b.battleAble(a.Character) || b.anyCondition(a.Character, r.IgnoreThreat) {
			continue
		}
		if gridDistance(actor.Admission.Position, a.Position, b.Battle.Rules.SquareSize) <= r.CloseDistance {
			causes = append(causes, BattleRollCause{Rule: "close_opponent", Character: a.Character, Direction: "disadvantage"})
		}
	}
	return causes
}
func recordAmmunition(e *BattleEncounter, actor CharacterID, origin ItemID) error {
	for i := range e.Ammunition {
		a := &e.Ammunition[i]
		if a.Actor != actor {
			continue
		}
		for j := range a.Stocks {
			if a.Stocks[j].Origin == origin {
				if a.Stocks[j].Spent == math.MaxInt64 {
					return battleFault("ammunition use", "int64 count", "overflow")
				}
				a.Stocks[j].Spent++
				return nil
			}
		}
		if len(a.Stocks) >= 64 {
			return battleFault("ammunition use", "at most 64 origins per shooter", fmt.Sprint(len(a.Stocks)))
		}
		a.Stocks = append(a.Stocks, AmmunitionStock{Origin: origin, Spent: 1})
		return nil
	}
	if len(e.Ammunition) >= 16 {
		return battleFault("ammunition use", "at most 16 shooters", "exceeded")
	}
	e.Ammunition = append(e.Ammunition, ActorAmmunition{Actor: actor, Stocks: []AmmunitionStock{{Origin: origin, Spent: 1}}})
	return nil
}
