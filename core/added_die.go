package core

import (
	"fmt"
	"math/big"
	"sort"
)

// An added die is rolled and added after a check's d20 is kept (SRD 5.2.1
// Bless: 1d4 on saving throws; Guidance: 1d4 on one skill's ability checks).
// A Condition modifier carries it on a stat; it never changes the stat's
// value, and it reaches every check of that stat or of a stat whose recipe
// reads it, as a roll mode does.
const (
	MaxAddedDieCount = 10
	MaxAddedDieSides = 100
)

// AddedDie is a Condition modifier's die: count dice of sides faces.
type AddedDie struct {
	Count int64 `json:"count"`
	Sides int64 `json:"sides"`
}

// AddedDieSource is one die in force on a checked stat: the instance and
// modifier that carry it, the stat the modifier names, and the die.
type AddedDieSource struct {
	Instance ConditionInstanceID `json:"instance"`
	Type     ConditionTypeID     `json:"type"`
	Modifier ConditionModifierID `json:"modifier"`
	Stat     StatID              `json:"stat"`
	Count    int64               `json:"count"`
	Sides    int64               `json:"sides"`
}

// AddedFaces are the faces supplied for the added die of one Condition type.
type AddedFaces struct {
	Type  ConditionTypeID `json:"type"`
	Faces []int64         `json:"faces"`
}

// AddedDieResult is one rolled added die: its source, its faces, whether the
// world's random source drew them, and their sum.
type AddedDieResult struct {
	AddedDieSource
	Faces []int64 `json:"faces"`
	Drawn bool    `json:"drawn,omitempty"`
	Sum   int64   `json:"sum"`
}

// addedDice lists the dice in force on a stat: the die modifiers of every
// instance on the Character that latest selection does not suppress, on the
// stat or on a stat an active term of its recipe reads. One Condition type
// adds at most one die (the same effect does not combine with itself); the
// first in (instance, type, modifier) order is the one that counts.
func (d *derivation) addedDice(id CharacterID, stat StatID, recipe *RecipeExplanation) []AddedDieSource {
	reads := map[StatID]bool{stat: true}
	if recipe != nil {
		for _, t := range recipe.Terms {
			if t.Active {
				reads[t.Term.Stat] = true
			}
		}
	}
	d.indexModifiers()
	var mods []modifierAt
	for s := range reads {
		mods = append(mods, d.dice[characterStat{id, s}]...)
	}
	var all []AddedDieSource
	for _, x := range mods {
		c := d.b.Conditions[x.owner]
		if d.instanceSuppressedBy(c) == nil {
			all = append(all, AddedDieSource{Instance: c.ID, Type: c.Type, Modifier: x.m.ID, Stat: x.m.Stat, Count: x.m.Die.Count, Sides: x.m.Die.Sides})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Instance != all[j].Instance {
			return all[i].Instance < all[j].Instance
		}
		if all[i].Type != all[j].Type {
			return all[i].Type < all[j].Type
		}
		return all[i].Modifier < all[j].Modifier
	})
	var out []AddedDieSource
	seen := map[ConditionTypeID]bool{}
	for _, s := range all {
		if !seen[s.Type] {
			seen[s.Type] = true
			out = append(out, s)
		}
	}
	return out
}

func checkAddedDie(d ConditionDefinition, m ConditionModifier) error {
	if m.Roll != "" || m.Bound != "" || m.Fixed != nil || m.Magnitude || m.ResourceMaximum != "" {
		return conditionFault("E_IR", string(d.ID), "die", "a stat die without amount, bound or roll", string(m.ID))
	}
	if m.Die.Count < 1 || m.Die.Count > MaxAddedDieCount || m.Die.Sides < 2 || m.Die.Sides > MaxAddedDieSides {
		return conditionFault("E_RULE", string(d.ID), "die", fmt.Sprintf("1..%d dice of 2..%d sides", MaxAddedDieCount, MaxAddedDieSides), string(m.ID))
	}
	return nil
}

// checkAddedFaces is the static check of supplied added faces: one group per
// valid type, each of 1 to MaxAddedDieCount faces in 1..MaxAddedDieSides.
func checkAddedFaces(subject string, groups []AddedFaces, fault func(field, expected, actual string) error) error {
	seen := map[ConditionTypeID]bool{}
	for _, g := range groups {
		if !validID(string(g.Type)) || seen[g.Type] {
			return fault("added", "valid Condition type, named once", string(g.Type))
		}
		seen[g.Type] = true
		if len(g.Faces) < 1 || len(g.Faces) > MaxAddedDieCount {
			return fault("added", fmt.Sprintf("1..%d faces", MaxAddedDieCount), fmt.Sprint(len(g.Faces)))
		}
		for _, f := range g.Faces {
			if f < 1 || f > MaxAddedDieSides {
				return fault("added", fmt.Sprintf("faces in 1..%d", MaxAddedDieSides), fmt.Sprint(f))
			}
		}
	}
	return nil
}

// rollAddedDice rolls the dice in force after the d20 is kept, in source
// order: a die takes its supplied faces, or draws them from the world's
// random source. Every supplied group must name a die in force and match it.
func rollAddedDice(b *Blueprint, character CharacterID, sources []AddedDieSource, supplied []AddedFaces, ctx applicationContext) ([]AddedDieResult, *big.Int, error) {
	inForce := map[ConditionTypeID]bool{}
	for _, s := range sources {
		inForce[s.Type] = true
	}
	groups := map[ConditionTypeID][]int64{}
	for _, g := range supplied {
		if !inForce[g.Type] {
			return nil, nil, rollFault(string(character), "added", "added faces for a die in force", string(g.Type), "no added die of this type applies")
		}
		groups[g.Type] = g.Faces
	}
	sum := new(big.Int)
	var out []AddedDieResult
	for _, s := range sources {
		r := AddedDieResult{AddedDieSource: s}
		if faces, ok := groups[s.Type]; ok {
			if int64(len(faces)) != s.Count {
				return nil, nil, rollFault(string(character), "added", fmt.Sprintf("%d %s faces", s.Count, s.Type), fmt.Sprint(len(faces)), "added faces do not match the die")
			}
			for _, f := range faces {
				if f < 1 || f > s.Sides {
					return nil, nil, rollFault(string(character), "added", fmt.Sprintf("1..%d", s.Sides), fmt.Sprint(f), "added face outside the die")
				}
			}
			r.Faces = append([]int64(nil), faces...)
		} else {
			state := b.checkRandom()
			if state == nil {
				return nil, nil, rollFault(string(character), "added", fmt.Sprintf("%d %s faces or a world random source", s.Count, s.Type), "absent", "added die has no faces and no random source")
			}
			for range s.Count {
				draw, err := drawStateDie(state, uint64(s.Sides), ctx.random)
				if err != nil {
					return nil, nil, err
				}
				r.Faces = append(r.Faces, int64(draw))
			}
			r.Drawn = true
		}
		for _, f := range r.Faces {
			r.Sum += f
		}
		sum.Add(sum, big.NewInt(r.Sum))
		out = append(out, r)
	}
	return out, sum, nil
}

// requireAddedFaces is the Save's rule: it never draws, so every die in force
// needs its supplied faces.
func requireAddedFaces(character CharacterID, sources []AddedDieSource, supplied []AddedFaces) error {
	have := map[ConditionTypeID]bool{}
	for _, g := range supplied {
		have[g.Type] = true
	}
	for _, s := range sources {
		if !have[s.Type] {
			return rollFault(string(character), "added", fmt.Sprintf("faces for every added die (%d %s)", s.Count, s.Type), "absent", "save added die has no faces")
		}
	}
	return nil
}

func cloneAddedFaces(v []AddedFaces) []AddedFaces {
	if v == nil {
		return nil
	}
	out := make([]AddedFaces, len(v))
	for i, g := range v {
		out[i] = AddedFaces{Type: g.Type, Faces: append([]int64(nil), g.Faces...)}
	}
	return out
}

func cloneAddedDieResults(v []AddedDieResult) []AddedDieResult {
	if v == nil {
		return nil
	}
	out := make([]AddedDieResult, len(v))
	for i, r := range v {
		out[i] = r
		out[i].Faces = append([]int64(nil), r.Faces...)
	}
	return out
}
