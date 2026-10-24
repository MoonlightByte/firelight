package core

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
)

// MaxItemCharges bounds a definition's charge maximum.
const MaxItemCharges = 1000

// ChargeRollVersion pins the generator behind item-seeded charge rolls: Go's
// ChaCha8 keyed by seed, purpose and tick, drawn with Uint64N(sides)+1.
const ChargeRollVersion = "go1.27-chacha8-uint64n-v1"

// Charge dice bounds keep one group within 30,000 and cheap to derive.
const (
	MaxChargeDiceCount = 20
	MaxChargeDiceSides = 1000
	MaxChargeDicePlus  = 10000
)

// ChargeRule is a definition's charge allowance (docs/rebuild/ITEM_CHARGES_PLAN.md).
// Initial or InitialDice sets a new item's count; it defaults to Max. Charges
// grant no power; they are only spent exactly.
type ChargeRule struct {
	Max         int64            `json:"max"`
	Initial     *int64           `json:"initial,omitempty"`
	InitialDice *ChargeDice      `json:"initial_dice,omitempty"`
	Recharge    *ChargeRecharge  `json:"recharge,omitempty"`
	Exhausted   *ChargeExhausted `json:"exhausted,omitempty"`
}

// ChargeDice is Count dice of Sides faces plus a flat amount.
type ChargeDice struct {
	Count int64 `json:"count"`
	Sides int64 `json:"sides"`
	Plus  int64 `json:"plus,omitempty"`
}

// ChargeRecharge regains charges at every absolute boundary k*Every+At. The
// amount is exactly one of All, By or Dice.
type ChargeRecharge struct {
	Every uint64      `json:"every"`
	At    uint64      `json:"at,omitempty"`
	All   bool        `json:"all,omitempty"`
	By    int64       `json:"by,omitempty"`
	Dice  *ChargeDice `json:"dice,omitempty"`
}

// ChargeExhausted applies when a spend takes the last charge: Destroy always
// destroys; Roll rolls one die, destroying at or below DestroyAtMost and
// regaining Regain at or above RegainAtLeast.
type ChargeExhausted struct {
	Destroy       bool        `json:"destroy,omitempty"`
	Roll          int64       `json:"roll,omitempty"`
	DestroyAtMost int64       `json:"destroy_at_most,omitempty"`
	RegainAtLeast int64       `json:"regain_at_least,omitempty"`
	Regain        *ChargeDice `json:"regain,omitempty"`
}

// ItemCharges is one instance's charge state. An empty Max on submitted intent
// derives the definition's maximum; resolved records always carry it. AsOf is
// the tick through which recharge is counted; absent, the item is unanchored.
// Seed keys the item's rolls and is present exactly when its rule uses dice.
type ItemCharges struct {
	Current int64      `json:"current"`
	Max     int64      `json:"max,omitempty"`
	AsOf    *uint64    `json:"as_of,omitempty"`
	Seed    *[2]uint64 `json:"seed,omitempty"`
}

// ChargeClock says how recharge was read for an item with a recharge rule.
type ChargeClock string

const (
	ChargeClockAbsent     ChargeClock = "absent"
	ChargeClockBehind     ChargeClock = "behind"
	ChargeClockUnanchored ChargeClock = "unanchored"
	ChargeClockCurrent    ChargeClock = "current"
)

// Exhaustion outcomes.
const (
	ChargesDestroyed = "destroyed"
	ChargesRegained  = "regained"
	ChargesNone      = "none"
)

// ExpendCharges spends charges exactly. Like ConsumeItem, ownership is a
// precondition and known custody must be the stated owner; a worn item may be
// used. Zero charges do not destroy the item unless its rule says so.
type ExpendCharges struct {
	Item   ItemID      `json:"item"`
	Owner  CharacterID `json:"owner"`
	Amount int64       `json:"amount"`
}

// ChargeExpenditure records one exact spend: the available count before, the
// stored count and anchor after, how the clock was read, and any last-charge
// roll.
type ChargeExpenditure struct {
	Item      ItemID            `json:"item"`
	Owner     CharacterID       `json:"owner"`
	Requested int64             `json:"requested"`
	Before    int64             `json:"before"`
	After     int64             `json:"after"`
	Clock     ChargeClock       `json:"clock,omitempty"`
	AsOf      *uint64           `json:"as_of,omitempty"`
	Exhausted *ChargeExhaustion `json:"exhausted,omitempty"`
}

// ChargeExhaustion is the last-charge result. Face is absent for an
// unconditional rule. A regain replaces the item seed with Seed, so the next
// emptying at the same tick rolls afresh.
type ChargeExhaustion struct {
	Face     int64      `json:"face,omitempty"`
	Outcome  string     `json:"outcome"`
	Regained int64      `json:"regained,omitempty"`
	Seed     *[2]uint64 `json:"seed,omitempty"`
}

func chargesFault(id, field, expected, actual, message string) error {
	return fail("E_CHARGES", id, field, expected, actual, message)
}

func cloneChargeRule(r *ChargeRule) *ChargeRule {
	if r == nil {
		return nil
	}
	out := *r
	out.Initial = copyPtr(r.Initial)
	out.InitialDice = copyPtr(r.InitialDice)
	if r.Recharge != nil {
		rc := *r.Recharge
		rc.Dice = copyPtr(r.Recharge.Dice)
		out.Recharge = &rc
	}
	if r.Exhausted != nil {
		x := *r.Exhausted
		x.Regain = copyPtr(r.Exhausted.Regain)
		out.Exhausted = &x
	}
	return &out
}

func cloneItemCharges(c *ItemCharges) *ItemCharges {
	if c == nil {
		return nil
	}
	out := *c
	out.AsOf = copyPtr(c.AsOf)
	out.Seed = copyPtr(c.Seed)
	return &out
}

func cloneChargeExpenditure(v *ChargeExpenditure) *ChargeExpenditure {
	if v == nil {
		return nil
	}
	out := *v
	out.AsOf = copyPtr(v.AsOf)
	if v.Exhausted != nil {
		x := *v.Exhausted
		x.Seed = copyPtr(v.Exhausted.Seed)
		out.Exhausted = &x
	}
	return &out
}

// usesDice reports whether the rule rolls, so its items carry a seed.
func (r *ChargeRule) usesDice() bool {
	return r.InitialDice != nil || (r.Recharge != nil && r.Recharge.Dice != nil) || (r.Exhausted != nil && r.Exhausted.Roll > 0)
}

func checkChargeDice(id, field string, d ChargeDice) error {
	if d.Count < 1 || d.Count > MaxChargeDiceCount {
		return catalogFault(id, field+".count", "1 to 20", strconv.FormatInt(d.Count, 10))
	}
	if d.Sides < 2 || d.Sides > MaxChargeDiceSides {
		return catalogFault(id, field+".sides", "2 to 1000", strconv.FormatInt(d.Sides, 10))
	}
	if d.Plus < 0 || d.Plus > MaxChargeDicePlus {
		return catalogFault(id, field+".plus", "0 to 10000", strconv.FormatInt(d.Plus, 10))
	}
	return nil
}

// checkChargeRule reports definition faults in the catalog's namespace.
func checkChargeRule(id string, r *ChargeRule) error {
	if r == nil {
		return nil
	}
	if r.Max < 1 || r.Max > MaxItemCharges {
		return catalogFault(id, "charges.max", "1 to 1000", strconv.FormatInt(r.Max, 10))
	}
	if r.Initial != nil && r.InitialDice != nil {
		return catalogFault(id, "charges.initial", "a count or dice", "both")
	}
	if r.Initial != nil && (*r.Initial < 0 || *r.Initial > r.Max) {
		return catalogFault(id, "charges.initial", "0 to the maximum", strconv.FormatInt(*r.Initial, 10))
	}
	if d := r.InitialDice; d != nil {
		if err := checkChargeDice(id, "charges.initial", *d); err != nil {
			return err
		}
		if d.Count+d.Plus > r.Max {
			return catalogFault(id, "charges.initial", "lowest roll within the maximum", strconv.FormatInt(d.Count+d.Plus, 10))
		}
	}
	if rc := r.Recharge; rc != nil {
		if rc.Every == 0 {
			return catalogFault(id, "charges.recharge.every", "positive tick period", "0")
		}
		if rc.At >= rc.Every {
			return catalogFault(id, "charges.recharge.at", "offset below the period", strconv.FormatUint(rc.At, 10))
		}
		amounts := 0
		for _, set := range []bool{rc.All, rc.By != 0, rc.Dice != nil} {
			if set {
				amounts++
			}
		}
		if amounts != 1 {
			return catalogFault(id, "charges.recharge.amount", "exactly one of all, by or dice", strconv.Itoa(amounts))
		}
		if rc.By < 0 || rc.By > MaxItemCharges {
			return catalogFault(id, "charges.recharge.by", "1 to 1000", strconv.FormatInt(rc.By, 10))
		}
		if rc.Dice != nil {
			if err := checkChargeDice(id, "charges.recharge.dice", *rc.Dice); err != nil {
				return err
			}
		}
	}
	if x := r.Exhausted; x != nil {
		if x.Destroy == (x.Roll != 0) {
			return catalogFault(id, "charges.exhausted", "destroy or roll", "invalid")
		}
		if x.Destroy {
			if x.DestroyAtMost != 0 || x.RegainAtLeast != 0 || x.Regain != nil {
				return catalogFault(id, "charges.exhausted", "no roll thresholds without a roll", "present")
			}
			return nil
		}
		if x.Roll < 2 || x.Roll > MaxChargeDiceSides {
			return catalogFault(id, "charges.exhausted.roll", "2 to 1000", strconv.FormatInt(x.Roll, 10))
		}
		if x.DestroyAtMost == 0 && x.RegainAtLeast == 0 {
			return catalogFault(id, "charges.exhausted", "a destroy or regain threshold", "none")
		}
		if x.DestroyAtMost < 0 || x.DestroyAtMost > x.Roll {
			return catalogFault(id, "charges.exhausted.destroy", "1 to the die", strconv.FormatInt(x.DestroyAtMost, 10))
		}
		if x.RegainAtLeast < 0 || x.RegainAtLeast > x.Roll || (x.RegainAtLeast != 0 && x.RegainAtLeast <= x.DestroyAtMost) {
			return catalogFault(id, "charges.exhausted.regain", "above the destroy threshold, up to the die", strconv.FormatInt(x.RegainAtLeast, 10))
		}
		if (x.RegainAtLeast != 0) != (x.Regain != nil) {
			return catalogFault(id, "charges.exhausted.regain", "dice exactly with a regain threshold", "invalid")
		}
		if x.Regain != nil {
			if err := checkChargeDice(id, "charges.exhausted.regain", *x.Regain); err != nil {
				return err
			}
		}
	}
	return nil
}

// Roll purposes keep an item's streams apart.
const (
	chargeRollInitial uint64 = iota + 1
	chargeRollRecharge
	chargeRollExhausted
	chargeRollRegain
)

// chargeStream is the item's generator for one purpose at one tick. Its key is
// the seed, the purpose and the tick, each as a little-endian uint64.
func chargeStream(seed [2]uint64, purpose, tick uint64) *rand.Rand {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[0:], seed[0])
	binary.LittleEndian.PutUint64(key[8:], seed[1])
	binary.LittleEndian.PutUint64(key[16:], purpose)
	binary.LittleEndian.PutUint64(key[24:], tick)
	return rand.New(rand.NewChaCha8(key))
}

func rollChargeDice(r *rand.Rand, d ChargeDice) int64 {
	total := d.Plus
	for range d.Count {
		total += int64(r.Uint64N(uint64(d.Sides))) + 1
	}
	return total
}

func initialCharges(r *ChargeRule, seed *[2]uint64) int64 {
	switch {
	case r.Initial != nil:
		return *r.Initial
	case r.InitialDice != nil && seed != nil:
		return min(r.Max, rollChargeDice(chargeStream(*seed, chargeRollInitial, 0), *r.InitialDice))
	case r.InitialDice != nil:
		return r.InitialDice.Count + r.InitialDice.Plus // static checks only; creation rolls
	}
	return r.Max
}

// resolveItemCharges derives or checks an instance's charge state against its
// definition: empty derives the initial count, Max empty derives and equal is
// accepted, anything else refuses. Fresh creation intent may omit the seed,
// which creation draws. It never aliases the caller's state.
func resolveItemCharges(i Item, d *ItemDefinition, fresh bool) (*ItemCharges, error) {
	id := string(i.ID)
	if d.Charges == nil {
		if i.Charges != nil {
			return nil, chargesFault(id, "charges", "no charges for this definition", "present", "the item's definition has no charges")
		}
		return nil, nil
	}
	r := d.Charges
	if i.Charges == nil {
		if r.usesDice() && !fresh {
			return nil, chargesFault(id, "charges.seed", "declared count and item seed", "absent", "a rolling item needs its seed; declare it or create the item")
		}
		return &ItemCharges{Current: initialCharges(r, nil), Max: r.Max}, nil
	}
	c := *cloneItemCharges(i.Charges)
	if c.Max != 0 && c.Max != r.Max {
		return nil, chargesFault(id, "charges.max", strconv.FormatInt(r.Max, 10), strconv.FormatInt(c.Max, 10), "the maximum comes from the item definition")
	}
	if c.Current < 0 || c.Current > r.Max {
		return nil, chargesFault(id, "charges.current", fmt.Sprintf("0 to %d", r.Max), strconv.FormatInt(c.Current, 10), "current charges out of range")
	}
	if c.AsOf != nil && r.Recharge == nil {
		return nil, chargesFault(id, "charges.as_of", "no anchor without a recharge rule", strconv.FormatUint(*c.AsOf, 10), "only recharging items carry an anchor")
	}
	if c.Seed != nil && !r.usesDice() {
		return nil, chargesFault(id, "charges.seed", "no seed without dice", "present", "only rolling items carry a seed")
	}
	if c.Seed == nil && r.usesDice() && !fresh {
		return nil, chargesFault(id, "charges.seed", "item seed", "absent", "a rolling item needs its seed")
	}
	c.Max = r.Max
	return &c, nil
}

// prepareChargedCreation fills what creation derives on a detached intent: the
// seed from the world's random source, the anchor from the clock, and the
// initial count, rolled when the rule says dice. Stated values are kept.
func (b *Blueprint) prepareChargedCreation(i *Item, ctx applicationContext) error {
	if i.From == "" {
		return nil
	}
	d := b.itemDefinition(i.From)
	if d == nil || d.Charges == nil {
		return nil
	}
	r := d.Charges
	stated := i.Charges != nil
	c := &ItemCharges{}
	if stated {
		c = cloneItemCharges(i.Charges)
	}
	if r.usesDice() && c.Seed == nil {
		state := b.checkRandom()
		if state == nil {
			return chargesFault(string(i.ID), "charges.seed", "world random source", "absent", "creating a rolling item draws its seed from the world's dice")
		}
		words, err := drawStateWords(state, 2, ctx.random)
		if err != nil {
			return err
		}
		c.Seed = &[2]uint64{words[0], words[1]}
	}
	if r.Recharge != nil && c.AsOf == nil && b.Clock != nil {
		c.AsOf = copyPtr(&b.Clock.Tick)
	}
	if !stated {
		c.Current = initialCharges(r, c.Seed)
	}
	i.Charges = c
	return nil
}

// checkChargedItem keeps a charged item one physical unit: no stack, no
// container, no recoverable count. A consumed unit keeps its identity at 0.
func checkChargedItem(i Item) error {
	if i.Charges == nil {
		return nil
	}
	id := string(i.ID)
	if i.From == "" {
		return chargesFault(id, "charges", "item from a charged definition", "uncatalogued item", "only catalog items carry charges")
	}
	if i.Container {
		return chargesFault(id, "container", "an item, not a container", "container", "a container cannot hold charges")
	}
	if itemQuantity(i) > 1 {
		return chargesFault(id, "quantity", "at most one unit", strconv.FormatInt(itemQuantity(i), 10), "a charged item is one physical unit")
	}
	if i.Recoverable != 0 {
		return chargesFault(id, "recoverable", "no recoverable count", strconv.FormatInt(i.Recoverable, 10), "a charged item is not recoverable stock")
	}
	return nil
}

// stockFault refuses stock operations that would duplicate or resurrect a
// charged item.
func chargedStockFault(i Item, operation string) error {
	return chargesFault(string(i.ID), "charges", "uncharged stock", "charged item", "a charged item cannot be "+operation)
}

// rechargeBoundaries counts boundaries k*every+at in (from, to] and returns the
// first. The first is meaningful only when the count is positive.
func rechargeBoundaries(rc ChargeRecharge, from, to uint64) (count, first uint64) {
	through := func(x uint64) uint64 {
		if x < rc.At {
			return 0
		}
		return (x-rc.At)/rc.Every + 1
	}
	count = through(to) - through(from)
	if count > 0 {
		first = rc.At + through(from)*rc.Every
	}
	return count, first
}

// recharge applies the boundaries in (from, to] in order, capping at the
// maximum after each and stopping once full. Dice need at most max-current
// groups, because every group regains at least one charge.
func recharge(r *ChargeRule, seed *[2]uint64, current int64, from, to uint64) int64 {
	rc := r.Recharge
	count, first := rechargeBoundaries(*rc, from, to)
	if count == 0 || current >= r.Max {
		return current
	}
	switch {
	case rc.All:
		return r.Max
	case rc.By > 0:
		if count >= uint64((r.Max-current+rc.By-1)/rc.By) {
			return r.Max
		}
		return current + int64(count)*rc.By
	}
	for k := uint64(0); k < count && current < r.Max; k++ {
		current = min(r.Max, current+rollChargeDice(chargeStream(*seed, chargeRollRecharge, first+k*rc.Every), *rc.Dice))
	}
	return current
}

// nextRecharge is the first boundary after the anchor, if one fits in uint64.
func nextRecharge(rc ChargeRecharge, after uint64) *uint64 {
	if after < rc.At {
		return copyPtr(&rc.At)
	}
	k := (after-rc.At)/rc.Every + 1
	if k > (math.MaxUint64-rc.At)/rc.Every {
		return nil
	}
	next := rc.At + k*rc.Every
	return &next
}

// chargeReading is an item's normalized charge state at the current tick: the
// pair (Current, AsOf) a host may store, and how the clock was read.
type chargeReading struct {
	Current int64
	AsOf    *uint64
	Clock   ChargeClock
	Next    *uint64
}

// readCharges derives the count at the world's tick (D1). Time never refuses:
// without a clock, behind the anchor or unanchored, nothing is credited, and
// the anchor never moves backwards.
func (b *Blueprint) readCharges(i Item) chargeReading {
	c := i.Charges
	out := chargeReading{Current: c.Current, AsOf: copyPtr(c.AsOf)}
	r := b.itemDefinition(i.From).Charges
	rc := r.Recharge
	if rc == nil {
		return out
	}
	switch {
	case b.Clock == nil:
		out.Clock = ChargeClockAbsent
	case c.AsOf == nil:
		out.Clock, out.AsOf = ChargeClockUnanchored, copyPtr(&b.Clock.Tick)
	case b.Clock.Tick < *c.AsOf:
		out.Clock = ChargeClockBehind
	default:
		out.Clock, out.AsOf = ChargeClockCurrent, copyPtr(&b.Clock.Tick)
		out.Current = recharge(r, c.Seed, c.Current, *c.AsOf, b.Clock.Tick)
	}
	if out.AsOf != nil && out.Current < r.Max {
		out.Next = nextRecharge(*rc, *out.AsOf)
	}
	return out
}

func (b Blueprint) checkExpendCharges(v ExpendCharges) error {
	if v.Amount <= 0 {
		return fail("E_AMOUNT", string(v.Item), "charges", "positive int64", "nonpositive", "charge amount must be positive")
	}
	for _, r := range []Ref{{ItemKind, string(v.Item)}, {CharacterKind, string(v.Owner)}} {
		if err := b.CheckRef(r); err != nil {
			return err
		}
	}
	return nil
}

func expendCharges(b *Blueprint, v ExpendCharges) ([]Event, error) {
	i := item(b, v.Item)
	if i.Charges == nil {
		return nil, chargesFault(string(i.ID), "charges", "charged item", "none", "the item has no charges")
	}
	r := b.itemDefinition(i.From).Charges
	if r.Recharge != nil && b.Clock == nil {
		return nil, chargesFault(string(i.ID), "clock", "world clock", "absent", "a recharging item is spent against the world clock")
	}
	if i.Container || (i.Custody != nil && !carriedBy(*i, v.Owner)) {
		return nil, custodyFault(i.ID, "item carried by stated owner", fmt.Sprint(i.Custody), "known custody does not permit expending charges")
	}
	if i.Owner != v.Owner {
		return nil, fail("E_OWNER", string(i.ID), "owner", string(i.Owner), string(v.Owner), "stated owner is not the current owner")
	}
	if itemQuantity(*i) == 0 {
		return nil, quantityFault(*i, "one physical unit", "a consumed item has no usable charges")
	}
	read := b.readCharges(*i)
	if read.Current < v.Amount {
		return nil, chargesFault(string(i.ID), "charges", fmt.Sprintf("at least %d charges", v.Amount), strconv.FormatInt(read.Current, 10), "insufficient charges for exact expenditure")
	}
	c := cloneItemCharges(i.Charges)
	c.Current, c.AsOf = read.Current-v.Amount, read.AsOf
	change := &ChargeExpenditure{Item: i.ID, Owner: i.Owner, Requested: v.Amount, Before: read.Current, Clock: read.Clock, AsOf: copyPtr(read.AsOf)}
	var events []Event
	if c.Current == 0 && r.Exhausted != nil {
		var tick uint64
		if b.Clock != nil {
			tick = b.Clock.Tick
		}
		change.Exhausted = exhaust(r, c, tick)
		if change.Exhausted.Outcome == ChargesDestroyed {
			if i.Wearer != "" {
				w := &WearingChange{Item: i.ID, Before: i.Wearer, Reason: "charges exhausted"}
				if i.Equipment != nil {
					w.ModeBefore = i.Equipment.Mode
					i.Equipment.Mode = ""
				}
				i.Wearer = ""
				events = append(events, Event{Kind: ItemUnequipped, Wearing: w})
			}
			var zero int64
			i.Quantity = &zero
		}
	}
	change.After = c.Current
	i.Charges = c
	return append(events, Event{Kind: ChargesExpended, Charges: change}), nil
}

// exhaust resolves the last-charge rule at the spend tick. A regain replaces
// the seed from the regain stream: an item may empty again at the same tick,
// and its next roll must not repeat this one.
func exhaust(r *ChargeRule, c *ItemCharges, tick uint64) *ChargeExhaustion {
	x := r.Exhausted
	if x.Destroy {
		return &ChargeExhaustion{Outcome: ChargesDestroyed}
	}
	face := rollChargeDice(chargeStream(*c.Seed, chargeRollExhausted, tick), ChargeDice{Count: 1, Sides: x.Roll})
	out := &ChargeExhaustion{Face: face, Outcome: ChargesNone}
	switch {
	case x.DestroyAtMost > 0 && face <= x.DestroyAtMost:
		out.Outcome = ChargesDestroyed
	case x.RegainAtLeast > 0 && face >= x.RegainAtLeast:
		stream := chargeStream(*c.Seed, chargeRollRegain, tick)
		out.Outcome, out.Regained = ChargesRegained, rollChargeDice(stream, *x.Regain)
		c.Current = min(r.Max, out.Regained)
		c.Seed = &[2]uint64{stream.Uint64(), stream.Uint64()}
		out.Seed = copyPtr(c.Seed)
	}
	return out
}

// ItemChargesProjection is one charged item's normalized state at a revision.
// Current and AsOf are the pair a host may store; Available is what can be
// spent now, 0 for a consumed unit.
type ItemChargesProjection struct {
	Revision     Revision    `json:"revision"`
	Item         ItemID      `json:"item"`
	Owner        CharacterID `json:"owner"`
	Max          int64       `json:"max"`
	Current      int64       `json:"current"`
	Available    int64       `json:"available"`
	AsOf         *uint64     `json:"as_of,omitempty"`
	Seed         *[2]uint64  `json:"seed,omitempty"`
	Clock        ChargeClock `json:"clock,omitempty"`
	NextRecharge *uint64     `json:"next_recharge,omitempty"`
}

// ItemChargesView reads exact item IDs in request order at one revision. An
// unknown or uncharged item returns the prior results and a fault.
func (w *World) ItemChargesView(ids []ItemID) ([]ItemChargesProjection, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	s := &w.authority.state
	out := make([]ItemChargesProjection, 0, len(ids))
	for _, id := range ids {
		i := item(&s.Blueprint, id)
		if i == nil || i.Charges == nil {
			return out, chargesFault(string(id), "id", "charged item", string(id), "no charged item with this ID")
		}
		read := s.Blueprint.readCharges(*i)
		available := read.Current
		if itemQuantity(*i) == 0 {
			available = 0
		}
		out = append(out, ItemChargesProjection{Revision: s.Revision, Item: i.ID, Owner: i.Owner, Max: i.Charges.Max, Current: read.Current, Available: available, AsOf: read.AsOf, Seed: copyPtr(i.Charges.Seed), Clock: read.Clock, NextRecharge: read.Next})
	}
	return out, nil
}
