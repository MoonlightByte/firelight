package core

import (
	"fmt"
	"math/big"
)

const SaveScored EventKind = "SaveScored"

// The effect of a save on its damage: the full damage on a failed save, and
// on a successful one half of it or none.
const (
	SaveFull = "full"
	SaveHalf = "half"
	SaveNone = "none"
)

// SaveProfile is a save's typed numbers, from a spell or a stat block: the
// stat the target rolls, the DC, and optionally the damage and what a
// successful save does to it.
type SaveProfile struct {
	Stat   StatID      `json:"stat"`
	DC     int64       `json:"dc"`
	Damage *SaveDamage `json:"damage,omitempty"`
}

// SaveDamage is the damage a failed save takes in full. Success is half or
// none: the effect of a successful save.
type SaveDamage struct {
	Count   int64  `json:"count"`
	Sides   int64  `json:"sides"`
	Bonus   int64  `json:"bonus"`
	Kind    string `json:"kind"`
	Success string `json:"success"`
}

// Save scores one saving throw from typed faces and applies its effect to
// the law's health Resource. The engine never draws: it takes the faces the
// net roll mode needs, in order, and the rest are unused. Damage is the sum
// of the damage dice as rolled, the bonus excluded; without it the save is
// scored and nothing is applied. CheckFaces are the target's faces for the
// checks the damage triggers, as on an attack. AddedFaces are the faces of
// the added dice in force on the save, by Condition type: the Save never
// draws, so each die in force needs its faces.
type Save struct {
	Source     CharacterID       `json:"source"`
	Profile    SaveProfile       `json:"profile"`
	Target     CharacterID       `json:"target"`
	Mode       RollMode          `json:"mode,omitempty"`
	Faces      []int64           `json:"faces,omitempty"`
	Damage     *int64            `json:"damage,omitempty"`
	CheckFaces []AttackCheckFace `json:"check_faces,omitempty"`
	AddedFaces []AddedFaces      `json:"added_faces,omitempty"`
}

// SaveResult is the SaveScored event. Check is the save as a stat check on
// the target. Effect is full, half or none when the profile has damage.
// When damage applies, RawDamage is the total plus the bonus, Treatments the
// sources of a resistance, vulnerability or immunity of its kind, and Damage
// the amount requested of the Damage, after the effect and the treatments.
// Unused names a damage total the effect did not need, UnusedSaveFaces the
// faces the roll mode did not need, and UnusedFaces the check faces no check
// took.
type SaveResult struct {
	Intent          Save              `json:"intent"`
	Check           StatCheckResult   `json:"check"`
	Effect          string            `json:"effect,omitempty"`
	DamageRolled    *int64            `json:"damage_rolled,omitempty"`
	RawDamage       string            `json:"raw_damage,omitempty"`
	Treatments      []AttackTreatment `json:"treatments,omitempty"`
	Damage          *int64            `json:"damage,omitempty"`
	Unused          []string          `json:"unused,omitempty"`
	UnusedSaveFaces []int64           `json:"unused_save_faces,omitempty"`
	UnusedFaces     []AttackCheckFace `json:"unused_faces,omitempty"`
}

func saveFault(subject, field, expected, actual string) error {
	return fail("E_SAVE", subject, field, expected, actual, "invalid save "+field)
}

// checkSave checks what does not depend on the outcome: the law, the
// characters, the stat, the profile, the mode and the faces.
func (b Blueprint) checkSave(v Save) error {
	l := b.Attacks
	if l == nil {
		return saveFault(string(v.Source), "law", "world attacks law", "absent")
	}
	for _, c := range []CharacterID{v.Source, v.Target} {
		if err := b.CheckRef(Ref{CharacterKind, string(c)}); err != nil {
			return err
		}
	}
	if d := v.Profile.Damage; d != nil {
		switch {
		case d.Count < 1 || d.Count > MaxAttackDiceCount:
			return saveFault(string(v.Source), "damage dice", fmt.Sprintf("1..%d", MaxAttackDiceCount), fmt.Sprint(d.Count))
		case d.Sides < 1 || d.Sides > MaxAttackDamageSides:
			return saveFault(string(v.Source), "damage sides", fmt.Sprintf("1..%d", MaxAttackDamageSides), fmt.Sprint(d.Sides))
		case !validID(d.Kind):
			return saveFault(string(v.Source), "damage kind", "valid damage kind", d.Kind)
		case d.Success != SaveHalf && d.Success != SaveNone:
			return saveFault(string(v.Source), "success", "half or none", d.Success)
		}
	} else if v.Damage != nil {
		return saveFault(string(v.Source), "damage", "a damage profile", "absent")
	}
	if v.Mode != "" && v.Mode != Advantage && v.Mode != Disadvantage {
		return saveFault(string(v.Source), "mode", "omitted, advantage or disadvantage", string(v.Mode))
	}
	if len(v.Faces) > 2 {
		return saveFault(string(v.Source), "faces", "at most 2 faces", fmt.Sprint(len(v.Faces)))
	}
	for _, f := range v.Faces {
		if f < 1 || f > l.DieSides {
			return saveFault(string(v.Source), "faces", fmt.Sprintf("faces in 1..%d", l.DieSides), fmt.Sprint(f))
		}
	}
	if err := checkAddedFaces(string(v.Source), v.AddedFaces, func(field, expected, actual string) error {
		return saveFault(string(v.Source), "added faces", expected, actual)
	}); err != nil {
		return err
	}
	if len(v.CheckFaces) > MaxOperations {
		return saveFault(string(v.Source), "check faces", "at most 128", fmt.Sprint(len(v.CheckFaces)))
	}
	for _, f := range v.CheckFaces {
		if !validID(string(f.Condition)) || f.Face < 1 || f.Face > 1000 {
			return saveFault(string(v.Source), "check faces", "valid instance and face 1..1000", fmt.Sprintf("%q %d", f.Condition, f.Face))
		}
	}
	if err := b.CheckStat(v.Target, v.Profile.Stat); err != nil {
		return err
	}
	if v.Profile.Damage != nil {
		return b.CheckResource(v.Target, l.Health)
	}
	return nil
}

// applySave scores the save as a stat check on the law's die, applies its
// effect to the damage, then the target's treatments, through the Damage
// operation, and ends with the SaveScored event, whose intent replays it.
func applySave(b *Blueprint, v Save, ctx applicationContext) ([]Event, error) {
	l := b.Attacks
	x, err := explain(b, v.Target, v.Profile.Stat)
	if err != nil {
		return nil, err
	}
	need, expected := 1, "1 face (normal)"
	switch netRoll(x.RollSources, v.Mode) {
	case AutoFail:
		need, expected = 0, "0 faces (fail)"
	case Advantage:
		need, expected = 2, "2 faces (advantage)"
	case Disadvantage:
		need, expected = 2, "2 faces (disadvantage)"
	}
	if len(v.Faces) < need {
		return nil, rollFault(string(v.Target), "faces", expected, fmt.Sprint(len(v.Faces)), "save faces do not cover the roll mode")
	}
	if need > 0 {
		if err := requireAddedFaces(v.Target, x.AddedDice, v.AddedFaces); err != nil {
			return nil, err
		}
	}
	dc := v.Profile.DC
	check, err := statCheck(b, StatCheck{Character: v.Target, Stat: v.Profile.Stat, Sides: uint64(l.DieSides), DC: &dc, Mode: v.Mode, Faces: append([]int64(nil), v.Faces[:need]...), Added: cloneAddedFaces(v.AddedFaces)}, ctx)
	if err != nil {
		return nil, err
	}
	result := SaveResult{Intent: *cloneSave(&v), Check: check, UnusedSaveFaces: append([]int64(nil), v.Faces[need:]...)}
	if len(result.UnusedSaveFaces) == 0 {
		result.UnusedSaveFaces = nil
	}
	result.UnusedFaces = append([]AttackCheckFace(nil), v.CheckFaces...)
	d := v.Profile.Damage
	if d == nil || v.Damage == nil {
		if d != nil {
			result.Effect = saveEffect(*check.Success, d.Success)
		}
		return []Event{{Kind: SaveScored, Save: &result}}, nil
	}
	result.Effect = saveEffect(*check.Success, d.Success)
	if result.Effect == SaveNone {
		result.Unused = []string{"damage"}
		return []Event{{Kind: SaveScored, Save: &result}}, nil
	}
	if *v.Damage < d.Count || *v.Damage > d.Count*d.Sides {
		return nil, saveFault(string(v.Source), "damage", fmt.Sprintf("%d..%d", d.Count, d.Count*d.Sides), fmt.Sprint(*v.Damage))
	}
	result.DamageRolled = copyPtr(v.Damage)
	raw := new(big.Int).Add(big.NewInt(*v.Damage), big.NewInt(d.Bonus))
	result.RawDamage = raw.String()
	// The save's effect comes first, then the treatments, as in SRD.
	amount := new(big.Int).Set(raw)
	if amount.Sign() < 0 {
		amount.SetInt64(0)
	}
	if result.Effect == SaveHalf {
		amount.Quo(amount, big.NewInt(2))
	}
	var resist, vulnerable, immune bool
	result.Treatments, resist, vulnerable, immune = attackTreatments(b, v.Target, d.Kind)
	amount = treatDamage(amount, resist, vulnerable, immune)
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
	return append(events, Event{Kind: SaveScored, Save: &result}), nil
}

// saveEffect is full on a failed save and the profile's effect on a
// successful one.
func saveEffect(success bool, onSuccess string) string {
	if success {
		return onSuccess
	}
	return SaveFull
}

func cloneSave(v *Save) *Save {
	if v == nil {
		return nil
	}
	out := *v
	out.Profile.Damage = copyPtr(v.Profile.Damage)
	out.Faces = append([]int64(nil), v.Faces...)
	out.Damage = copyPtr(v.Damage)
	out.CheckFaces = append([]AttackCheckFace(nil), v.CheckFaces...)
	out.AddedFaces = cloneAddedFaces(v.AddedFaces)
	return &out
}

func cloneSaveResult(v *SaveResult) *SaveResult {
	if v == nil {
		return nil
	}
	out := *v
	out.Intent = *cloneSave(&v.Intent)
	out.Check = *cloneStatCheckResult(&v.Check)
	out.DamageRolled = copyPtr(v.DamageRolled)
	out.Damage = copyPtr(v.Damage)
	out.Treatments = append([]AttackTreatment(nil), v.Treatments...)
	out.Unused = append([]string(nil), v.Unused...)
	out.UnusedSaveFaces = append([]int64(nil), v.UnusedSaveFaces...)
	out.UnusedFaces = append([]AttackCheckFace(nil), v.UnusedFaces...)
	return &out
}
