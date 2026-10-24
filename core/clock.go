package core

import (
	"fmt"
	"math"
	"sort"
)

const MaxClockConditions = 64
const TimeAdvanced EventKind = "TimeAdvanced"

// Clock is logical world time. The authored unit has no built-in conversion to
// real time, rounds, a calendar or another subsystem's clock.
type Clock struct {
	Unit string `json:"unit"`
	Tick uint64 `json:"tick"`
}
type AdvanceTime struct {
	Ticks int64 `json:"ticks"`
}
type AdvanceTimeTemplate struct {
	Ticks AbilityOperand `json:"ticks"`
}
type ClockLifetime struct {
	AppliedTick uint64 `json:"applied_tick"`
	ExpiresTick uint64 `json:"expires_tick"`
}
type ClockExpiration struct {
	Deadline uint64 `json:"deadline"`
}
type TimeChange struct {
	Before    uint64 `json:"before"`
	After     uint64 `json:"after"`
	Requested int64  `json:"requested"`
}

func clockFault(id, field, expected, actual string) error {
	return fail("E_CLOCK", id, field, expected, actual, "invalid clock "+field)
}
func validateClock(b *Blueprint) error {
	if b.Clock == nil {
		return nil
	}
	if !validID(b.Clock.Unit) {
		return clockFault("", "unit", "valid authored unit", b.Clock.Unit)
	}
	if len(b.Characters) > MaxWorldCharacters || len(b.Items) > MaxWorldItems || len(b.Locations) > MaxWorldLocations || len(b.ConditionDefinitions) > MaxWorldConditionDefinitions || len(b.Conditions) > MaxWorldConditions {
		return clockFault("", "capacity", "native entity/Condition bounds", "exceeded")
	}
	return nil
}
func checkClockLifetimes(b *Blueprint) error {
	count := 0
	for _, c := range b.Conditions {
		l := c.ClockLifetime
		if l == nil {
			continue
		}
		count++
		if c.Lifetime != nil || b.Clock == nil || l.ExpiresTick <= l.AppliedTick || l.AppliedTick > b.Clock.Tick || l.ExpiresTick <= b.Clock.Tick {
			return clockFault(string(c.ID), "lifetime", "one coherent active lifetime: applied <= tick < expires", "invalid")
		}
	}
	if count > MaxClockConditions {
		return clockFault("", "timed capacity", "at most 64 active clock lifetimes", fmt.Sprint(count))
	}
	return nil
}
func (b *Blueprint) clockLifetime(duration uint64) (*ClockLifetime, error) {
	if b.Clock == nil || duration == 0 {
		return nil, clockFault("", "duration", "positive duration with Clock", "invalid")
	}
	if duration > math.MaxUint64-b.Clock.Tick {
		return nil, clockFault("", "duration", "nonoverflowing deadline", "overflow")
	}
	return &ClockLifetime{AppliedTick: b.Clock.Tick, ExpiresTick: b.Clock.Tick + duration}, nil
}
func (b *Blueprint) checkAdvanceTime(v AdvanceTime) error {
	if b.Clock == nil {
		return clockFault("", "advance", "Clock present", "absent")
	}
	if v.Ticks <= 0 {
		return clockFault("", "ticks", "positive int64", fmt.Sprint(v.Ticks))
	}
	return nil
}

// advanceClock stages expiration through the existing Condition removal path.
// The caller accounts for the explicit primitive or one implicit time helper.
func advanceClock(b *Blueprint, v AdvanceTime, ctx applicationContext) ([]Event, error) {
	if err := b.checkAdvanceTime(v); err != nil {
		return nil, err
	}
	before := b.Clock.Tick
	if uint64(v.Ticks) > math.MaxUint64-before {
		return nil, clockFault("", "advance", "nonoverflowing tick", "overflow")
	}
	after := before + uint64(v.Ticks)
	type expiry struct {
		ID       ConditionInstanceID
		Target   CharacterID
		Deadline uint64
	}
	var due []expiry
	for _, c := range b.Conditions {
		if l := c.ClockLifetime; l != nil && l.ExpiresTick <= after {
			due = append(due, expiry{c.ID, c.Target, l.ExpiresTick})
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].Deadline != due[j].Deadline {
			return due[i].Deadline < due[j].Deadline
		}
		return due[i].ID < due[j].ID
	})
	events := []Event{}
	for _, c := range due {
		if conditionIndex(b, c.ID) < 0 {
			continue
		}
		*ctx.work++
		if *ctx.work > MaxOperations {
			return nil, fail("E_LIMIT", string(c.ID), "work", "at most 128", "clock expiry exceeded", "clock expiry exceeds request work budget")
		}
		es, err := applyConditionOperation(b, Operation{RemoveCondition: &RemoveCondition{Instance: c.ID, Target: c.Target}}, ctx)
		if err != nil {
			return nil, err
		}
		es[len(es)-1].Condition.ClockExpiration = &ClockExpiration{Deadline: c.Deadline}
		events = append(events, es...)
	}
	decayEvents, err := decayRelationships(b, before, after, ctx)
	if err != nil {
		return nil, err
	}
	events = append(events, decayEvents...)
	b.Clock.Tick = after
	return append(events, Event{Kind: TimeAdvanced, Time: &TimeChange{Before: before, After: after, Requested: v.Ticks}}), nil
}
func equipmentTime(b *Blueprint, ticks int64, ctx applicationContext) ([]Event, error) {
	if ticks == 0 {
		return nil, nil
	}
	*ctx.work++
	if *ctx.work > MaxOperations {
		return nil, fail("E_LIMIT", "", "work", "at most 128", "equipment time exceeded", "equipment time exceeds request work budget")
	}
	return advanceClock(b, AdvanceTime{Ticks: ticks}, ctx)
}
func unequipItem(b *Blueprint, i *Item, reason string, ctx applicationContext) ([]Event, error) {
	change := &WearingChange{Item: i.ID, Before: i.Wearer, Reason: reason}
	var ticks int64
	if m := b.wornMode(*i); m != nil {
		ticks = m.UnequipTicks
	}
	events, err := equipmentTime(b, ticks, ctx)
	if err != nil {
		return nil, err
	}
	if i.Equipment != nil {
		change.ModeBefore = i.Equipment.Mode
		i.Equipment.Mode = ""
	}
	i.Wearer = ""
	return append(events, Event{Kind: ItemUnequipped, Wearing: change}), nil
}
