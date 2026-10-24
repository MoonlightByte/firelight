package core

// An explicitly authored actor_roll uses the existing controller, raw-face,
// random-state and Check comparison contracts. Legacy checks remain automatic.
func (b *Blueprint) resolveActorCheck(v InvokeAbility, step int, c CheckTemplate, ctx applicationContext) (CheckResult, error) {
	out := CheckResult{Check: c.ID, Sides: c.Sides, CallsBefore: b.Battle.State.Random.Calls}
	var err error
	out.Source, err = b.abilityRollController(ctx.cause.Actor)
	if err != nil {
		return out, err
	}
	out.Modifier, err = checkValue(b, c.Modifier)
	if err != nil {
		return out, err
	}
	out.Threshold, err = checkValue(b, c.Threshold)
	if err != nil {
		return out, err
	}
	// A human may leave the check to NQL: it is drawn as for an agent.
	engine := len(v.HumanRolls) == 1 && v.HumanRolls[0].Step == step && v.HumanRolls[0].Engine
	if out.Source == HumanController && !engine {
		if len(v.HumanRolls) == 0 {
			return out, &InputRequired{Need: BattleInputRequirements{Stage: "ability", Count: 1, Sides: int64(c.Sides), AbilityStep: copyPtr(&step)}, Err: abilityFault("E_ROLL", "faces", "one supplied human check face", "absent")}
		}
		if len(v.HumanRolls) != 1 || v.HumanRolls[0].Step != step || len(v.HumanRolls[0].Faces) != 1 {
			return out, abilityFault("E_ROLL", "faces", "one complete check face", "invalid group")
		}
		out.Draw = uint64(v.HumanRolls[0].Faces[0])
	} else {
		if out.Source != HumanController && len(v.HumanRolls) != 0 {
			return out, abilityFault("E_ROLL", "faces", "no supplied NPC check faces", "present")
		}
		out.Draw, err = drawStateDie(&b.Battle.State.Random, c.Sides, ctx.random)
		if err != nil {
			return out, err
		}
		out.EngineDrawn = out.Source == HumanController
	}
	out.CallsAfter = b.Battle.State.Random.Calls
	out, err = completeCheck(out)
	if err != nil {
		return out, err
	}
	return b.resolveCheckBoost(v, step, c, out, ctx)
}
