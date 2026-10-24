package core

import (
	"sort"
	"strconv"
)

// MaxWorldOccupants bounds one world's occupant records, resolved ones
// included.
const MaxWorldOccupants = 32768

// MaxOccupantCount bounds the members one creature group ever has.
const MaxOccupantCount = 1_000_000_000

// MaxOccupantAliases bounds the other names one occupant is known by.
const MaxOccupantAliases = 16

// MaxReasonBytes bounds the text an agent gives for how members left.
const MaxReasonBytes = 200

type OccupantID string
type OccupantKind string
type Attitude string
type Outcome string

const (
	OccupantPerson    OccupantKind = "person"
	OccupantCreatures OccupantKind = "creatures"

	AttitudeFriendly    Attitude = "friendly"
	AttitudeIndifferent Attitude = "indifferent"
	AttitudeHostile     Attitude = "hostile"

	OutcomeDefeated    Outcome = "defeated"
	OutcomeFled        Outcome = "fled"
	OutcomeSurrendered Outcome = "surrendered"
	OutcomeDeparted    Outcome = "departed"
	OutcomeJoined      Outcome = "joined"
	OutcomeDied        Outcome = "died"
)

const (
	OccupantCreated         EventKind = "OccupantCreated"
	OccupantMoved           EventKind = "OccupantMoved"
	OccupantCounted         EventKind = "OccupantCounted"
	OccupantResolved        EventKind = "OccupantResolved"
	OccupantAttitudeChanged EventKind = "OccupantAttitudeChanged"
	OccupantRenamed         EventKind = "OccupantRenamed"
	OccupantReturned        EventKind = "OccupantReturned"
)

// Occupant is someone or something at a location that is not a Character: a
// person without a sheet, or a group of creatures. Count is the members
// present; a group not yet counted has none, and may have an authored Range.
// Tally records how members left, so a resolved occupant stays where it was
// last, with its history.
type Occupant struct {
	ID       OccupantID      `json:"id"`
	Name     string          `json:"name"`
	Location LocationID      `json:"location"`
	Kind     OccupantKind    `json:"kind"`
	Count    *int64          `json:"count,omitempty"`
	Range    *CountRange     `json:"range,omitempty"`
	Attitude Attitude        `json:"attitude"`
	Type     string          `json:"type,omitempty"`
	Aliases  []string        `json:"aliases,omitempty"`
	Tally    []OccupantTally `json:"tally,omitempty"`
}
type CountRange struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// OccupantTally is how many left one way, for one reason. The reason is the
// agent's text; the engine keeps it and never reads it. A departure always
// has one.
type OccupantTally struct {
	Outcome Outcome `json:"outcome"`
	Count   int64   `json:"count"`
	Reason  string  `json:"reason,omitempty"`
}

// OccupantOp is one change to the occupants. Exactly one variant is present.
type OccupantOp struct {
	Create   *Occupant        `json:"create,omitempty"`
	Move     *MoveOccupant    `json:"move,omitempty"`
	Count    *CountOccupant   `json:"count,omitempty"`
	Resolve  *ResolveOccupant `json:"resolve,omitempty"`
	Attitude *SetAttitude     `json:"attitude,omitempty"`
	Rename   *RenameOccupant  `json:"rename,omitempty"`
	Return   *ReturnOccupant  `json:"return,omitempty"`
}
type MoveOccupant struct {
	Occupant OccupantID `json:"occupant"`
	To       LocationID `json:"to"`
}
type CountOccupant struct {
	Occupant OccupantID `json:"occupant"`
	Count    int64      `json:"count"`
}

// ResolveOccupant says how present members left, and why. Without an amount
// every present member leaves.
type ResolveOccupant struct {
	Occupant OccupantID `json:"occupant"`
	Outcome  Outcome    `json:"outcome"`
	Amount   *int64     `json:"amount,omitempty"`
	Reason   string     `json:"reason,omitempty"`
}

// ReturnOccupant brings members back from one tally entry to the occupant's
// place. Reason names the entry; without it the request takes the outcome's
// entry with no reason, or the outcome's only entry. Without an amount the
// whole entry returns.
type ReturnOccupant struct {
	Occupant OccupantID `json:"occupant"`
	Outcome  Outcome    `json:"outcome"`
	Reason   string     `json:"reason,omitempty"`
	Amount   *int64     `json:"amount,omitempty"`
}
type SetAttitude struct {
	Occupant OccupantID `json:"occupant"`
	Attitude Attitude   `json:"attitude"`
}
type RenameOccupant struct {
	Occupant OccupantID `json:"occupant"`
	Name     string     `json:"name"`
}

// OccupantChange is an occupant event: the request's intent and the record
// before and after. A created occupant has no before.
type OccupantChange struct {
	Intent OccupantOp `json:"intent"`
	Before *Occupant  `json:"before,omitempty"`
	After  Occupant   `json:"after"`
}

func occupantFault(subject, field, expected, actual string) error {
	return fail("E_OCCUPANT", subject, field, expected, actual, "invalid occupant "+field)
}

func cloneOccupant(o *Occupant) *Occupant {
	if o == nil {
		return nil
	}
	v := *o
	v.Count = copyPtr(o.Count)
	v.Range = copyPtr(o.Range)
	v.Aliases = append([]string(nil), o.Aliases...)
	v.Tally = append([]OccupantTally(nil), o.Tally...)
	return &v
}

func cloneOccupants(list []Occupant) []Occupant {
	if list == nil {
		return nil
	}
	out := make([]Occupant, len(list))
	for i := range list {
		out[i] = *cloneOccupant(&list[i])
	}
	return out
}

func cloneOccupantOp(o *OccupantOp) *OccupantOp {
	if o == nil {
		return nil
	}
	return &OccupantOp{Create: cloneOccupant(o.Create), Move: copyPtr(o.Move), Count: copyPtr(o.Count), Resolve: cloneResolve(o.Resolve), Attitude: copyPtr(o.Attitude), Rename: copyPtr(o.Rename), Return: cloneReturn(o.Return)}
}

func cloneResolve(v *ResolveOccupant) *ResolveOccupant {
	if v == nil {
		return nil
	}
	out := *v
	out.Amount = copyPtr(v.Amount)
	return &out
}

func cloneReturn(v *ReturnOccupant) *ReturnOccupant {
	if v == nil {
		return nil
	}
	out := *v
	out.Amount = copyPtr(v.Amount)
	return &out
}

func cloneOccupantChange(c *OccupantChange) *OccupantChange {
	if c == nil {
		return nil
	}
	return &OccupantChange{Intent: *cloneOccupantOp(&c.Intent), Before: cloneOccupant(c.Before), After: *cloneOccupant(&c.After)}
}

func normalizeOccupant(o *Occupant) {
	sort.Strings(o.Aliases)
	sort.Slice(o.Tally, func(i, j int) bool {
		a, z := o.Tally[i], o.Tally[j]
		return a.Outcome < z.Outcome || a.Outcome == z.Outcome && a.Reason < z.Reason
	})
}

func normalizeOccupants(b *Blueprint) {
	sort.Slice(b.Occupants, func(i, j int) bool { return b.Occupants[i].ID < b.Occupants[j].ID })
	for i := range b.Occupants {
		normalizeOccupant(&b.Occupants[i])
	}
}

func (o OccupantOp) variants() int {
	n := 0
	for _, yes := range []bool{o.Create != nil, o.Move != nil, o.Count != nil, o.Resolve != nil, o.Attitude != nil, o.Rename != nil, o.Return != nil} {
		if yes {
			n++
		}
	}
	return n
}

// subject is the occupant the operation changes.
func (o OccupantOp) subject() OccupantID {
	switch {
	case o.Create != nil:
		return o.Create.ID
	case o.Move != nil:
		return o.Move.Occupant
	case o.Count != nil:
		return o.Count.Occupant
	case o.Resolve != nil:
		return o.Resolve.Occupant
	case o.Attitude != nil:
		return o.Attitude.Occupant
	case o.Return != nil:
		return o.Return.Occupant
	}
	return o.Rename.Occupant
}

func validAttitude(a Attitude) bool {
	return a == AttitudeFriendly || a == AttitudeIndifferent || a == AttitudeHostile
}

func validOutcome(o Outcome) bool {
	switch o {
	case OutcomeDefeated, OutcomeFled, OutcomeSurrendered, OutcomeDeparted, OutcomeJoined, OutcomeDied:
		return true
	}
	return false
}

const outcomeChoices = "defeated, fled, surrendered, departed, joined or died"

// checkReasonText checks a reason given: valid text of at most 200 bytes.
func checkReasonText(id, reason string) error {
	if !validID(reason) {
		return occupantFault(id, "reason", "valid text of at most 200 bytes", "invalid")
	}
	if len(reason) > MaxReasonBytes {
		return occupantFault(id, "reason", "valid text of at most 200 bytes", strconv.Itoa(len(reason))+" bytes")
	}
	return nil
}

// checkReason checks why members left: optional, except that a departure
// always says why.
func checkReason(id string, o Outcome, reason string) error {
	if reason != "" {
		return checkReasonText(id, reason)
	}
	if o == OutcomeDeparted {
		return occupantFault(id, "reason", "a reason for departed", "absent")
	}
	return nil
}

// present reports whether any member is still there: an uncounted group is.
func present(o Occupant) bool { return o.Count == nil || *o.Count > 0 }

// checkOccupant checks one record on its own and its location.
func checkOccupant(o Occupant, location func(LocationID) error) error {
	id := string(o.ID)
	if !validID(id) {
		return occupantFault(id, "id", "valid occupant ID", "invalid")
	}
	if !validID(o.Name) {
		return occupantFault(id, "name", "valid name", "invalid")
	}
	if err := location(o.Location); err != nil {
		return err
	}
	if !validAttitude(o.Attitude) {
		return occupantFault(id, "attitude", "friendly, indifferent or hostile", string(o.Attitude))
	}
	if o.Type != "" && !validID(o.Type) {
		return occupantFault(id, "type", "valid type", "invalid")
	}
	if len(o.Aliases) > MaxOccupantAliases {
		return occupantFault(id, "aliases", "at most 16 aliases", strconv.Itoa(len(o.Aliases)))
	}
	names := map[string]bool{o.Name: true}
	for _, a := range o.Aliases {
		if !validID(a) {
			return occupantFault(id, "alias", "valid name", "invalid")
		}
		if names[a] {
			return occupantFault(id, "alias", "a name not already used by this occupant", a)
		}
		names[a] = true
	}
	var left int64
	entries := map[OccupantTally]bool{}
	for _, t := range o.Tally {
		if !validOutcome(t.Outcome) {
			return occupantFault(id, "outcome", outcomeChoices, string(t.Outcome))
		}
		if err := checkReason(id, t.Outcome, t.Reason); err != nil {
			return err
		}
		key := OccupantTally{Outcome: t.Outcome, Reason: t.Reason}
		if entries[key] {
			return occupantFault(id, "tally", "one entry per outcome and reason", string(t.Outcome))
		}
		entries[key] = true
		if t.Count < 1 || t.Count > MaxOccupantCount {
			return occupantFault(id, "tally", "1..1000000000", strconv.FormatInt(t.Count, 10))
		}
		left += t.Count
	}
	switch o.Kind {
	case OccupantPerson:
		if o.Range != nil {
			return occupantFault(id, "range", "a range only for creatures", "person")
		}
		if left > 1 {
			return occupantFault(id, "tally", "a person leaves once", strconv.FormatInt(left, 10))
		}
		if o.Count == nil || *o.Count != 1-left {
			return occupantFault(id, "count", "1 while present, 0 once left", countText(o.Count))
		}
	case OccupantCreatures:
		if o.Range != nil && o.Count != nil {
			return occupantFault(id, "count", "a count or a range", "both")
		}
		if o.Count == nil {
			if len(o.Tally) > 0 {
				return occupantFault(id, "tally", "a counted group", "uncounted")
			}
			if r := o.Range; r != nil && (r.Min < 1 || r.Min >= r.Max || r.Max > MaxOccupantCount) {
				return occupantFault(id, "range", "1 <= min < max <= 1000000000", strconv.FormatInt(r.Min, 10)+" to "+strconv.FormatInt(r.Max, 10))
			}
			break
		}
		// A count past the bound makes the sum pass it, or wrap below 1.
		if c := *o.Count; c < 0 || c+left < 1 || c+left > MaxOccupantCount {
			return occupantFault(id, "count", "0..1000000000 present, 1..1000000000 with those who left", strconv.FormatInt(c, 10))
		}
	default:
		return occupantFault(id, "kind", "person or creatures", string(o.Kind))
	}
	return nil
}

func countText(c *int64) string {
	if c == nil {
		return "absent"
	}
	return strconv.FormatInt(*c, 10)
}

// validateOccupants checks every occupant record against its world. Occupant
// IDs are their own and differ from every character, item and location ID.
func validateOccupants(b *Blueprint, refs Refs) error {
	if len(b.Occupants) == 0 {
		return nil
	}
	if b.Clock == nil {
		return occupantFault("", "clock", "declared clock", "absent")
	}
	if len(b.Occupants) > MaxWorldOccupants {
		return occupantFault("", "occupants", "at most 32768 occupants", "exceeded")
	}
	location := func(id LocationID) error { return refs.CheckRef(Ref{LocationKind, string(id)}) }
	seen := make(map[OccupantID]bool, len(b.Occupants))
	for _, o := range b.Occupants {
		if err := checkOccupant(o, location); err != nil {
			return err
		}
		if _, taken := refs.KindOf(string(o.ID)); taken || seen[o.ID] || questIndex(b, QuestID(o.ID)) >= 0 {
			return fail("E_DUPLICATE", string(o.ID), "occupant", "unique occupant ID", string(o.ID), "duplicate occupant identity")
		}
		seen[o.ID] = true
	}
	return nil
}

func occupantIndex(b *Blueprint, id OccupantID) int {
	for i := range b.Occupants {
		if b.Occupants[i].ID == id {
			return i
		}
	}
	return -1
}

// checkOccupantOp checks what an occupant operation names. Whether the
// change applies to the occupant's current state is checked when it runs.
func (b Blueprint) checkOccupantOp(o OccupantOp) error {
	if o.variants() != 1 {
		return fail("E_IR", "", "occupant", "exactly one occupant change", "invalid sum", "occupant operation requires exactly one variant")
	}
	if v := o.Create; v != nil {
		if b.Clock == nil {
			return occupantFault(string(v.ID), "clock", "declared clock", "absent")
		}
		if err := checkOccupant(*v, func(id LocationID) error { return b.CheckRef(Ref{LocationKind, string(id)}) }); err != nil {
			return err
		}
		// A valid record with no tally has someone present.
		if len(v.Tally) > 0 {
			return occupantFault(string(v.ID), "create", "an occupant present with no one left", "resolved")
		}
		if _, taken := b.KindOf(string(v.ID)); taken || occupantIndex(&b, v.ID) >= 0 || questIndex(&b, QuestID(v.ID)) >= 0 {
			return fail("E_DUPLICATE", string(v.ID), "occupant", "unique occupant ID", string(v.ID), "duplicate occupant identity")
		}
		return nil
	}
	id := o.subject()
	if occupantIndex(&b, id) < 0 {
		return occupantFault(string(id), "occupant", "declared occupant", "missing")
	}
	switch {
	case o.Move != nil:
		return b.CheckRef(Ref{LocationKind, string(o.Move.To)})
	case o.Resolve != nil:
		if !validOutcome(o.Resolve.Outcome) {
			return occupantFault(string(id), "outcome", outcomeChoices, string(o.Resolve.Outcome))
		}
		if a := o.Resolve.Amount; a != nil && *a <= 0 {
			return fail("E_AMOUNT", string(id), "amount", "positive int64", "nonpositive", "resolved amount must be positive")
		}
		return checkReason(string(id), o.Resolve.Outcome, o.Resolve.Reason)
	case o.Return != nil:
		if !validOutcome(o.Return.Outcome) {
			return occupantFault(string(id), "outcome", outcomeChoices, string(o.Return.Outcome))
		}
		if a := o.Return.Amount; a != nil && *a <= 0 {
			return fail("E_AMOUNT", string(id), "amount", "positive int64", "nonpositive", "returned amount must be positive")
		}
		if r := o.Return.Reason; r != "" {
			return checkReasonText(string(id), r)
		}
	case o.Attitude != nil:
		if !validAttitude(o.Attitude.Attitude) {
			return occupantFault(string(id), "attitude", "friendly, indifferent or hostile", string(o.Attitude.Attitude))
		}
	case o.Rename != nil:
		if !validID(o.Rename.Name) {
			return occupantFault(string(id), "name", "valid name", "invalid")
		}
	}
	return nil
}

// applyOccupant makes one occupant change and records it.
func applyOccupant(b *Blueprint, op OccupantOp) ([]Event, error) {
	if v := op.Create; v != nil {
		o := cloneOccupant(v)
		normalizeOccupant(o)
		b.Occupants = append(b.Occupants, *o)
		return []Event{{Kind: OccupantCreated, Occupant: &OccupantChange{Intent: *cloneOccupantOp(&op), After: *cloneOccupant(o)}}}, nil
	}
	id := op.subject()
	i := occupantIndex(b, id)
	if i < 0 {
		return nil, occupantFault(string(id), "occupant", "declared occupant", "missing")
	}
	o := &b.Occupants[i]
	before := cloneOccupant(o)
	var kind EventKind
	switch {
	case op.Move != nil:
		if !present(*o) {
			return nil, occupantFault(string(id), "move", "present occupant", "resolved")
		}
		if o.Location == op.Move.To {
			return nil, noChange(string(id))
		}
		o.Location, kind = op.Move.To, OccupantMoved
	case op.Count != nil:
		if o.Kind != OccupantCreatures {
			return nil, occupantFault(string(id), "count", "creature group", string(o.Kind))
		}
		if o.Count != nil {
			return nil, occupantFault(string(id), "count", "uncounted group", "counted")
		}
		n, low, high := op.Count.Count, int64(1), int64(MaxOccupantCount)
		if o.Range != nil {
			low, high = o.Range.Min, o.Range.Max
		}
		if n < low || n > high {
			return nil, occupantFault(string(id), "count", strconv.FormatInt(low, 10)+".."+strconv.FormatInt(high, 10), strconv.FormatInt(n, 10))
		}
		o.Count, o.Range, kind = &n, nil, OccupantCounted
	case op.Resolve != nil:
		v := op.Resolve
		if o.Count == nil {
			return nil, occupantFault(string(id), "resolve", "counted group", "uncounted")
		}
		n := *o.Count
		if n == 0 {
			return nil, occupantFault(string(id), "resolve", "present members", "none")
		}
		if v.Amount != nil {
			if *v.Amount > n {
				return nil, occupantFault(string(id), "resolve", "1.."+strconv.FormatInt(n, 10), strconv.FormatInt(*v.Amount, 10))
			}
			n = *v.Amount
		}
		left := *o.Count - n
		o.Count = &left
		added := false
		for j := range o.Tally {
			if o.Tally[j].Outcome == v.Outcome && o.Tally[j].Reason == v.Reason {
				o.Tally[j].Count += n
				added = true
			}
		}
		if !added {
			o.Tally = append(o.Tally, OccupantTally{Outcome: v.Outcome, Count: n, Reason: v.Reason})
		}
		kind = OccupantResolved
	case op.Return != nil:
		v := op.Return
		j, err := tallyEntry(o, v)
		if err != nil {
			return nil, err
		}
		n := o.Tally[j].Count
		if v.Amount != nil {
			if *v.Amount > n {
				return nil, occupantFault(string(id), "return", "1.."+strconv.FormatInt(n, 10), strconv.FormatInt(*v.Amount, 10))
			}
			n = *v.Amount
		}
		if o.Tally[j].Count -= n; o.Tally[j].Count == 0 {
			o.Tally = append(o.Tally[:j], o.Tally[j+1:]...)
		}
		back := *o.Count + n
		o.Count, kind = &back, OccupantReturned
	case op.Attitude != nil:
		if o.Attitude == op.Attitude.Attitude {
			return nil, noChange(string(id))
		}
		o.Attitude, kind = op.Attitude.Attitude, OccupantAttitudeChanged
	case op.Rename != nil:
		name := op.Rename.Name
		if o.Name == name {
			return nil, noChange(string(id))
		}
		// The old name becomes an alias; a name that was an alias stops
		// being one. The world's check refuses a 17th alias.
		aliases := []string{o.Name}
		for _, a := range o.Aliases {
			if a != name {
				aliases = append(aliases, a)
			}
		}
		o.Name, o.Aliases, kind = name, aliases, OccupantRenamed
	}
	normalizeOccupant(o)
	return []Event{{Kind: kind, Occupant: &OccupantChange{Intent: *cloneOccupantOp(&op), Before: before, After: *cloneOccupant(o)}}}, nil
}

// tallyEntry finds the entry a return takes members from: the one with the
// reason given, or without one, the outcome's entry with no reason, or the
// outcome's only entry.
func tallyEntry(o *Occupant, v *ReturnOccupant) (int, error) {
	at, others := -1, 0
	for j, t := range o.Tally {
		if t.Outcome != v.Outcome {
			continue
		}
		if t.Reason == v.Reason {
			return j, nil
		}
		at, others = j, others+1
	}
	switch {
	case v.Reason != "" || others == 0:
		return -1, occupantFault(string(o.ID), "return", "a tally entry to return from", "none")
	case others > 1:
		return -1, occupantFault(string(o.ID), "return", "a reason naming one entry", "ambiguous")
	}
	return at, nil
}

// hazards is where a trip stops on its way: the declared hazards and every
// place where a hostile occupant is present.
func (b *Blueprint) hazards() map[LocationID]bool {
	out := map[LocationID]bool{}
	for _, at := range b.Map.Hazards {
		out[at] = true
	}
	for _, o := range b.Occupants {
		if o.Attitude == AttitudeHostile && present(o) {
			out[o.Location] = true
		}
	}
	return out
}

// LocationProjection is who is at a place: its characters, the occupants
// present and the occupants resolved there. With a map it also says whether
// the party has visited the place and whether a trip stops there.
type LocationProjection struct {
	Revision   Revision           `json:"revision"`
	Clock      *Clock             `json:"clock,omitempty"`
	Location   Location           `json:"location"`
	Visited    *bool              `json:"visited,omitempty"`
	Hazard     *bool              `json:"hazard,omitempty"`
	Characters []CharacterSummary `json:"characters"`
	Present    []Occupant         `json:"present"`
	Resolved   []Occupant         `json:"resolved"`
}

func (w *World) LocationView(id LocationID) (LocationProjection, error) {
	out, err := w.LocationsView([]LocationID{id})
	if err != nil {
		return LocationProjection{}, err
	}
	return out[0], nil
}

// LocationsView reads places in the order given, all at one revision, for the
// cost of one pass over the world. At a place that is not a location it
// returns the views before it and the fault.
func (w *World) LocationsView(ids []LocationID) ([]LocationProjection, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	s := &w.authority.state
	b := &s.Blueprint
	refs := NewRefs(b)
	names := make(map[LocationID]Location, len(b.Locations))
	for _, l := range b.Locations {
		if _, ok := names[l.ID]; !ok {
			names[l.ID] = l
		}
	}
	characters := map[LocationID][]CharacterSummary{}
	for _, c := range b.Characters {
		characters[c.Location] = append(characters[c.Location], CharacterSummary{c.ID, c.Name})
	}
	occupants := map[LocationID][]int{}
	for i, o := range b.Occupants {
		occupants[o.Location] = append(occupants[o.Location], i)
	}
	var hazards, visited map[LocationID]bool
	if b.Map != nil {
		hazards, visited = b.hazards(), map[LocationID]bool{}
		for _, at := range b.Map.Visited {
			visited[at] = true
		}
	}
	out := make([]LocationProjection, 0, len(ids))
	for _, id := range ids {
		if err := refs.CheckRef(Ref{LocationKind, string(id)}); err != nil {
			return out, err
		}
		p := LocationProjection{Revision: s.Revision, Clock: copyPtr(b.Clock), Location: names[id], Characters: append([]CharacterSummary{}, characters[id]...), Present: []Occupant{}, Resolved: []Occupant{}}
		for _, i := range occupants[id] {
			o := *cloneOccupant(&b.Occupants[i])
			if present(o) {
				p.Present = append(p.Present, o)
			} else {
				p.Resolved = append(p.Resolved, o)
			}
		}
		if b.Map != nil {
			v, h := visited[id], hazards[id]
			p.Visited, p.Hazard = &v, &h
		}
		out = append(out, p)
	}
	return out, nil
}

func occupantEvent(k EventKind) bool {
	switch k {
	case OccupantCreated, OccupantMoved, OccupantCounted, OccupantResolved, OccupantAttitudeChanged, OccupantRenamed, OccupantReturned:
		return true
	}
	return false
}
