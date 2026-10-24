package core

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"reflect"
	"sort"
)

// RollMode is how a die test is rolled. A Condition modifier imposes one on
// a stat; a StatCheck request may ask for advantage or disadvantage.
type RollMode string

const (
	Advantage    RollMode = "advantage"
	Disadvantage RollMode = "disadvantage"
	AutoFail     RollMode = "fail"
)

const StatCheckResolved EventKind = "StatCheckResolved"

// MaxStatCheckSides bounds a StatCheck die, as a Battle die is bounded.
const MaxStatCheckSides = 1000

// StatCheck resolves one die test on a Character's effective stat outside
// any Ability: an ability check, saving throw or initiative the caller names.
// Faces are the player's roll; without them the world's random source rolls.
// Added are the faces of added dice by Condition type; a die in force without
// them is drawn. Without a DC the check reports its total only.
type StatCheck struct {
	Character CharacterID  `json:"character"`
	Stat      StatID       `json:"stat"`
	Sides     uint64       `json:"sides"`
	DC        *int64       `json:"dc,omitempty"`
	Mode      RollMode     `json:"mode,omitempty"`
	Faces     []int64      `json:"faces,omitempty"`
	Added     []AddedFaces `json:"added,omitempty"`
}

// RollSource is one Condition modifier imposing a mode on a stat: on the stat
// itself or on a stat its recipe reads, so a mode on a pool such as
// bonus:checks reaches every stat that adds the pool. Instance names the
// Condition instance; a held type has none and names its Resource instead.
type RollSource struct {
	Instance      ConditionInstanceID `json:"instance,omitempty"`
	HeldAtMinimum ResourceID          `json:"held_at_minimum,omitempty"`
	Type          ConditionTypeID     `json:"type"`
	Modifier      ConditionModifierID `json:"modifier"`
	Stat          StatID              `json:"stat"`
	Roll          RollMode            `json:"roll"`
}

// StatCheckResult is the resolved check. Roll is the net mode of the request
// and its sources: fail is an automatic failure with no faces and no total.
// Kept is the face that counts: the higher with advantage, the lower with
// disadvantage. Added lists each added die rolled after the kept face; the
// total is the kept face, the bonus and the dice. Success and Margin need a
// DC; Margin is total minus DC. A check that damage triggered names its
// Condition instance and the damage.
type StatCheckResult struct {
	Intent    StatCheck            `json:"intent"`
	Condition *ConditionInstanceID `json:"condition,omitempty"`
	Damage    int64                `json:"damage,omitempty"`
	Roll      RollMode             `json:"roll,omitempty"`
	Sources   []RollSource         `json:"roll_sources,omitempty"`
	Drawn     bool                 `json:"drawn,omitempty"`
	Faces     []int64              `json:"faces,omitempty"`
	Kept      int64                `json:"kept,omitempty"`
	Added     []AddedDieResult     `json:"added,omitempty"`
	Bonus     int64                `json:"bonus"`
	Total     *int64               `json:"total,omitempty"`
	Success   *bool                `json:"success,omitempty"`
	Margin    *int64               `json:"margin,omitempty"`
}

// Dice is a world's random source for StatChecks. A world with an Adventure
// or a Battle rolls from that state instead and cannot declare Dice.
type Dice struct {
	Seed  [2]uint64   `json:"seed"`
	State RandomState `json:"state"`
}

func rollMode(m RollMode) bool {
	return m == Advantage || m == Disadvantage || m == AutoFail
}

// rollSources lists the modes in force on a stat: those of every instance on
// the Character that latest selection does not suppress, then those of every
// held type, whose modifier names the stat or a stat an active term of its
// recipe reads.
func (d *derivation) rollSources(id CharacterID, stat StatID, recipe *RecipeExplanation) []RollSource {
	reads := map[StatID]bool{stat: true}
	if recipe != nil {
		for _, t := range recipe.Terms {
			if t.Active {
				reads[t.Term.Stat] = true
			}
		}
	}
	// A modifier names one stat, so the buckets of the stats read are
	// disjoint; byPlace restores the order a scan of the modifiers takes.
	d.indexModifiers()
	var mods, heldMods []modifierAt
	for s := range reads {
		mods = append(mods, d.rolls[characterStat{id, s}]...)
		heldMods = append(heldMods, d.heldRolls[s]...)
	}
	byPlace(mods)
	byPlace(heldMods)
	var out []RollSource
	for _, x := range mods {
		c := d.b.Conditions[x.owner]
		if d.instanceSuppressedBy(c) == nil {
			out = append(out, RollSource{Instance: c.ID, Type: c.Type, Modifier: x.m.ID, Stat: x.m.Stat, Roll: x.m.Roll})
		}
	}
	holder, held := d.character(id), d.heldTypes()
	for _, x := range heldMods {
		if def := held[x.owner]; heldBy(holder, def) {
			out = append(out, RollSource{HeldAtMinimum: *def.HeldAtMinimum, Type: def.ID, Modifier: x.m.ID, Stat: x.m.Stat, Roll: x.m.Roll})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Instance != out[j].Instance {
			return out[i].Instance < out[j].Instance
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Modifier < out[j].Modifier
	})
	return out
}

// netRoll combines a requested mode with its sources. Any fail is an
// automatic failure. Advantage and disadvantage cancel; neither stacks.
func netRoll(sources []RollSource, requested RollMode) RollMode {
	advantage, disadvantage := requested == Advantage, requested == Disadvantage
	for _, s := range sources {
		switch s.Roll {
		case AutoFail:
			return AutoFail
		case Advantage:
			advantage = true
		case Disadvantage:
			disadvantage = true
		}
	}
	if advantage == disadvantage {
		return ""
	}
	if advantage {
		return Advantage
	}
	return Disadvantage
}

func rollFault(subject, field, expected, actual, message string) error {
	return fail("E_ROLL", subject, field, expected, actual, message)
}

func (b Blueprint) checkStatCheck(v StatCheck) error {
	if err := b.CheckStat(v.Character, v.Stat); err != nil {
		return err
	}
	if v.Sides < 2 || v.Sides > MaxStatCheckSides {
		return rollFault(string(v.Character), "sides", "2..1000", fmt.Sprint(v.Sides), "invalid check die")
	}
	if v.Mode != "" && v.Mode != Advantage && v.Mode != Disadvantage {
		return rollFault(string(v.Character), "mode", "omitted, advantage or disadvantage", string(v.Mode), "invalid requested roll mode")
	}
	if len(v.Faces) > 2 {
		return rollFault(string(v.Character), "faces", "at most 2 faces", fmt.Sprint(len(v.Faces)), "too many check faces")
	}
	for _, f := range v.Faces {
		if f < 1 || uint64(f) > v.Sides {
			return rollFault(string(v.Character), "faces", fmt.Sprintf("faces in 1..%d", v.Sides), fmt.Sprint(f), "check face outside the die")
		}
	}
	return checkAddedFaces(string(v.Character), v.Added, func(field, expected, actual string) error {
		return rollFault(string(v.Character), field, expected, actual, "invalid added faces")
	})
}

// checkRandom is the random state a StatCheck draws from.
func (b *Blueprint) checkRandom() *RandomState {
	switch {
	case b.Adventure != nil:
		return &b.Adventure.State.Random
	case b.Battle != nil:
		return &b.Battle.State.Random
	case b.Dice != nil:
		return &b.Dice.State
	}
	return nil
}

func resolveStatCheck(b *Blueprint, v StatCheck, ctx applicationContext) ([]Event, error) {
	out, err := statCheck(b, v, ctx)
	if err != nil {
		return nil, err
	}
	return []Event{{Kind: StatCheckResolved, StatCheck: &out}}, nil
}

// statCheck adds the kept face, then any added dice, to the stat's effective
// value. Supplied faces must match the net mode exactly: one, two with
// advantage or disadvantage, none for an automatic failure, which also rolls
// no added die.
func statCheck(b *Blueprint, v StatCheck, ctx applicationContext) (StatCheckResult, error) {
	x, err := explain(b, v.Character, v.Stat)
	if err != nil {
		return StatCheckResult{}, err
	}
	out := StatCheckResult{Intent: *cloneStatCheck(&v), Sources: x.RollSources, Bonus: x.Effective}
	out.Roll = netRoll(x.RollSources, v.Mode)
	need := 1
	switch out.Roll {
	case AutoFail:
		need = 0
	case Advantage, Disadvantage:
		need = 2
	}
	expected := map[RollMode]string{"": "1 face (normal)", Advantage: "2 faces (advantage)", Disadvantage: "2 faces (disadvantage)", AutoFail: "0 faces (fail)"}[out.Roll]
	if len(v.Faces) > 0 && len(v.Faces) != need {
		return out, rollFault(string(v.Character), "faces", expected, fmt.Sprint(len(v.Faces)), "supplied faces do not match the roll mode")
	}
	if out.Roll == AutoFail {
		if len(v.Added) > 0 {
			return out, rollFault(string(v.Character), "added", "no added faces (fail)", fmt.Sprint(len(v.Added)), "an automatic failure rolls no added die")
		}
		if v.DC != nil {
			out.Success = new(bool)
		}
		return out, nil
	}
	out.Faces = append([]int64(nil), v.Faces...)
	if len(out.Faces) == 0 {
		state := b.checkRandom()
		if state == nil {
			return out, rollFault(string(v.Character), "faces", expected+" or a world random source", "absent", "check has no faces and no random source")
		}
		for range need {
			draw, err := drawStateDie(state, v.Sides, ctx.random)
			if err != nil {
				return out, err
			}
			out.Faces = append(out.Faces, int64(draw))
		}
		out.Drawn = true
	}
	out.Kept = out.Faces[0]
	if need == 2 && (out.Roll == Advantage) == (out.Faces[1] > out.Kept) {
		out.Kept = out.Faces[1]
	}
	added, dice, err := rollAddedDice(b, v.Character, x.AddedDice, v.Added, ctx)
	if err != nil {
		return out, err
	}
	out.Added = added
	total := new(big.Int).Add(big.NewInt(out.Kept), big.NewInt(out.Bonus))
	total.Add(total, dice)
	if !total.IsInt64() {
		return out, fail("E_OVERFLOW", string(v.Character), string(v.Stat), "int64 check total", total.String(), "check total exceeds numeric domain")
	}
	sum := total.Int64()
	out.Total = &sum
	if v.DC != nil {
		margin := new(big.Int).Sub(total, big.NewInt(*v.DC))
		if !margin.IsInt64() {
			return out, fail("E_OVERFLOW", string(v.Character), string(v.Stat), "int64 check margin", margin.String(), "check margin exceeds numeric domain")
		}
		success, m := margin.Sign() >= 0, margin.Int64()
		out.Success, out.Margin = &success, &m
	}
	return out, nil
}

func cloneStatCheck(v *StatCheck) *StatCheck {
	if v == nil {
		return nil
	}
	out := *v
	out.DC = copyPtr(v.DC)
	out.Faces = append([]int64(nil), v.Faces...)
	out.Added = cloneAddedFaces(v.Added)
	return &out
}

func cloneStatCheckResult(v *StatCheckResult) *StatCheckResult {
	if v == nil {
		return nil
	}
	out := *v
	out.Intent = *cloneStatCheck(&v.Intent)
	out.Condition = copyPtr(v.Condition)
	out.Sources = append([]RollSource(nil), v.Sources...)
	out.Faces = append([]int64(nil), v.Faces...)
	out.Added = cloneAddedDieResults(v.Added)
	out.Total, out.Success, out.Margin = copyPtr(v.Total), copyPtr(v.Success), copyPtr(v.Margin)
	return &out
}

func cloneDice(v *Dice) *Dice {
	if v == nil {
		return nil
	}
	out := *v
	out.State.Bytes = append([]byte(nil), v.State.Bytes...)
	return &out
}

func diceFault(field, expected, actual string) error {
	return fail("E_DICE", "", field, expected, actual, "invalid world dice")
}

// validateDice accepts a declaration with zero state, which NewWorld
// initializes, or a valid versioned PCG continuation.
func validateDice(b *Blueprint) error {
	if b.Dice == nil {
		return nil
	}
	if b.Adventure != nil || b.Battle != nil {
		return diceFault("dice", "no Adventure or Battle random state", "present")
	}
	if reflect.DeepEqual(b.Dice.State, RandomState{}) {
		return nil
	}
	p := rand.NewPCG(0, 0)
	if b.Dice.State.Version != RandomVersion || len(b.Dice.State.Bytes) != 20 || p.UnmarshalBinary(b.Dice.State.Bytes) != nil {
		return diceFault("state", "versioned 20-byte PCG continuation", "invalid")
	}
	return nil
}

func (b *Blueprint) initializeDice() error {
	if b.Dice == nil {
		return nil
	}
	if err := validateDice(b); err != nil {
		return err
	}
	p := rand.NewPCG(b.Dice.Seed[0], b.Dice.Seed[1])
	raw, err := p.MarshalBinary()
	if err != nil {
		return err
	}
	expected := RandomState{Version: RandomVersion, Bytes: raw}
	if !reflect.DeepEqual(b.Dice.State, RandomState{}) && !reflect.DeepEqual(b.Dice.State, expected) {
		return diceFault("genesis", "zero or exact initial continuation", "historical state")
	}
	b.Dice.State = expected
	return nil
}
