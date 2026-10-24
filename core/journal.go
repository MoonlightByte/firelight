package core

import (
	"math"
	"sort"
	"strconv"
	"unicode"
)

// MaxJournalEntries bounds a world's journal. A new entry in a full journal
// drops the oldest.
const MaxJournalEntries = 1024

// MaxJournalParticipants bounds the characters and occupants one entry names.
const MaxJournalParticipants = 16

// MaxJournalTextBytes bounds an entry's text.
const MaxJournalTextBytes = 512

const JournalRecorded EventKind = "JournalRecorded"

type JournalEntryID string
type JournalKind string

const (
	JournalDeparture JournalKind = "departure"
	JournalArrival   JournalKind = "arrival"
	JournalEvent     JournalKind = "event"
	JournalDiscovery JournalKind = "discovery"
	JournalNote      JournalKind = "note"
)

// Journal is the world's record of what happened where. Next is the seq the
// next entry takes: it only grows, so a seq never repeats, even after the
// oldest entries are dropped. Entries are in seq order.
type Journal struct {
	Next    uint64         `json:"next"`
	Entries []JournalEntry `json:"entries"`
}

// JournalEntry is one record, never rewritten. The engine writes the seq,
// the tick and, when the action names none, the place; the text is the
// agent's own words, kept and never read.
type JournalEntry struct {
	Seq          uint64         `json:"seq"`
	ID           JournalEntryID `json:"id"`
	Kind         JournalKind    `json:"kind"`
	Tick         *uint64        `json:"tick,omitempty"`
	Place        LocationID     `json:"place"`
	Participants []string       `json:"participants,omitempty"`
	Text         *string        `json:"text,omitempty"`
}

// RecordJournal adds an entry. Without a place, the entry is at the
// actor's place. Party names the map's party as it is then; With names
// characters or occupants. A name the world does not declare, or one past
// the 16th participant, is left out and reported in the event.
type RecordJournal struct {
	ID    JournalEntryID `json:"id"`
	Kind  JournalKind    `json:"kind"`
	Place *LocationID    `json:"place,omitempty"`
	Party bool           `json:"party,omitempty"`
	With  []string       `json:"with,omitempty"`
	Text  *string        `json:"text,omitempty"`
}

// JournalChange is the JournalRecorded event: the request's intent, the
// entry, the oldest entry dropped to make room, and the names the world did
// not declare.
type JournalChange struct {
	Intent              RecordJournal `json:"intent"`
	Entry               JournalEntry  `json:"entry"`
	Dropped             *JournalEntry `json:"dropped,omitempty"`
	DroppedParticipants []string      `json:"dropped_participants,omitempty"`
}

func journalFault(subject, field, expected, actual string) error {
	return fail("E_JOURNAL", subject, field, expected, actual, "invalid journal "+field)
}

func validJournalKind(k JournalKind) bool {
	switch k {
	case JournalDeparture, JournalArrival, JournalEvent, JournalDiscovery, JournalNote:
		return true
	}
	return false
}

const journalKinds = "departure, arrival, event, discovery or note"

func checkJournalText(id JournalEntryID, s *string) error {
	if s == nil {
		return nil
	}
	if !validID(*s) {
		// Text is one line: a newline or a tab is named, so a host that
		// forgot to flatten its prose sees why.
		actual := "invalid"
		for _, r := range *s {
			if unicode.IsControl(r) {
				actual = "control character"
				break
			}
		}
		return journalFault(string(id), "text", "valid text of at most 512 bytes", actual)
	}
	if len(*s) > MaxJournalTextBytes {
		return journalFault(string(id), "text", "valid text of at most 512 bytes", strconv.Itoa(len(*s))+" bytes")
	}
	return nil
}

func cloneJournalEntry(e JournalEntry) JournalEntry {
	e.Tick, e.Text = copyPtr(e.Tick), cloneText(e.Text)
	e.Participants = append([]string(nil), e.Participants...)
	return e
}

func cloneJournal(j *Journal) *Journal {
	if j == nil {
		return nil
	}
	out := &Journal{Next: j.Next, Entries: make([]JournalEntry, len(j.Entries))}
	for i, e := range j.Entries {
		out.Entries[i] = cloneJournalEntry(e)
	}
	return out
}

func cloneRecordJournal(v *RecordJournal) *RecordJournal {
	if v == nil {
		return nil
	}
	out := *v
	out.Place, out.Text = copyPtr(v.Place), cloneText(v.Text)
	out.With = append([]string(nil), v.With...)
	return &out
}

func cloneJournalChange(c *JournalChange) *JournalChange {
	if c == nil {
		return nil
	}
	out := &JournalChange{Intent: *cloneRecordJournal(&c.Intent), Entry: cloneJournalEntry(c.Entry), DroppedParticipants: append([]string(nil), c.DroppedParticipants...)}
	if c.Dropped != nil {
		d := cloneJournalEntry(*c.Dropped)
		out.Dropped = &d
	}
	return out
}

// normalizeJournal sorts each entry's participants. Entries stay in seq
// order, as they were written.
func normalizeJournal(b *Blueprint) {
	if b.Journal == nil {
		return
	}
	for i := range b.Journal.Entries {
		sort.Strings(b.Journal.Entries[i].Participants)
	}
}

// CheckJournal checks a journal on its own: its bounds, its order, and each
// entry's fields. The places and participants an entry names are history:
// they are not checked against the world, which may no longer declare them.
func CheckJournal(j Journal) error {
	if j.Next == 0 {
		return journalFault("", "next", "at least 1", "0")
	}
	if len(j.Entries) > MaxJournalEntries {
		return journalFault("", "entries", "at most 1024 entries", strconv.Itoa(len(j.Entries)))
	}
	seen := make(map[JournalEntryID]bool, len(j.Entries))
	for i, e := range j.Entries {
		id := string(e.ID)
		if !validID(id) {
			return journalFault(id, "id", "valid entry ID", "invalid")
		}
		if seen[e.ID] {
			return journalDuplicate(id)
		}
		seen[e.ID] = true
		if e.Seq == 0 || e.Seq >= j.Next || i > 0 && e.Seq <= j.Entries[i-1].Seq {
			return journalFault(id, "seq", "increasing, from 1 and below next", strconv.FormatUint(e.Seq, 10))
		}
		if !validJournalKind(e.Kind) {
			return journalFault(id, "kind", journalKinds, string(e.Kind))
		}
		if !validID(string(e.Place)) {
			return journalFault(id, "place", "valid place ID", "invalid")
		}
		if len(e.Participants) > MaxJournalParticipants {
			return journalFault(id, "participants", "at most 16 participants", strconv.Itoa(len(e.Participants)))
		}
		names := make(map[string]bool, len(e.Participants))
		for _, p := range e.Participants {
			if !validID(p) || names[p] {
				return journalFault(id, "participants", "valid IDs, each once", p)
			}
			names[p] = true
		}
		if err := checkJournalText(e.ID, e.Text); err != nil {
			return err
		}
	}
	return nil
}

func journalDuplicate(id string) error {
	return fail("E_DUPLICATE", id, "journal", "unique entry ID", id, "duplicate journal entry identity")
}

// validateJournal checks the journal and that no entry shares an ID with a
// character, item, place, occupant or quest: they share one namespace.
func validateJournal(b *Blueprint, refs Refs) error {
	if b.Journal == nil {
		return nil
	}
	if err := CheckJournal(*b.Journal); err != nil {
		return err
	}
	taken := make(map[string]bool, len(b.Occupants)+len(b.Quests))
	for _, o := range b.Occupants {
		taken[string(o.ID)] = true
	}
	for _, q := range b.Quests {
		taken[string(q.ID)] = true
	}
	for _, e := range b.Journal.Entries {
		if _, ok := refs.KindOf(string(e.ID)); ok || taken[string(e.ID)] {
			return journalDuplicate(string(e.ID))
		}
	}
	return nil
}

// checkJournalOp checks what can be known before the request runs.
func (b Blueprint) checkJournalOp(v RecordJournal) error {
	id := string(v.ID)
	if !validID(id) {
		return journalFault(id, "id", "valid entry ID", "invalid")
	}
	if !validJournalKind(v.Kind) {
		return journalFault(id, "kind", journalKinds, string(v.Kind))
	}
	if v.Place != nil {
		if err := b.CheckRef(Ref{LocationKind, string(*v.Place)}); err != nil {
			return err
		}
	}
	if v.Party && b.Map == nil {
		return journalFault(id, "party", "a world map", "absent")
	}
	if len(v.With) > MaxJournalParticipants {
		return journalFault(id, "participants", "at most 16 participants", strconv.Itoa(len(v.With)))
	}
	for _, p := range v.With {
		if !validID(p) {
			return journalFault(id, "participants", "valid IDs", p)
		}
	}
	return checkJournalText(v.ID, v.Text)
}

func journalIndex(b *Blueprint, id JournalEntryID) int {
	if b.Journal == nil {
		return -1
	}
	for i := range b.Journal.Entries {
		if b.Journal.Entries[i].ID == id {
			return i
		}
	}
	return -1
}

// recordJournal adds one entry at the request's actor's place unless the
// action names one.
func recordJournal(b *Blueprint, v RecordJournal, actor Ref) ([]Event, error) {
	if err := b.checkJournalOp(v); err != nil {
		return nil, err
	}
	id := string(v.ID)
	if _, ok := b.KindOf(id); ok || journalIndex(b, v.ID) >= 0 || occupantIndex(b, OccupantID(id)) >= 0 || questIndex(b, QuestID(id)) >= 0 {
		return nil, journalDuplicate(id)
	}
	e := JournalEntry{ID: v.ID, Kind: v.Kind, Text: cloneText(v.Text)}
	switch {
	case v.Place != nil:
		e.Place = *v.Place
	case actor.Kind == LocationKind:
		e.Place = LocationID(actor.ID)
	case actor.Kind == CharacterKind && character(b, CharacterID(actor.ID)) != nil:
		e.Place = character(b, CharacterID(actor.ID)).Location
	default:
		return nil, journalFault(id, "place", "a place, or an actor standing at one", string(actor.Kind))
	}
	if b.Clock != nil {
		t := b.Clock.Tick
		e.Tick = &t
	}
	// The party comes first, sorted, then the listed names in the order
	// given. A name that is no declared character or occupant, or one past
	// the 16th participant, is left out and reported, never refused: a
	// record in a trip's request never stops the trip.
	var candidates []string
	if v.Party {
		for _, c := range b.Map.Party {
			candidates = append(candidates, string(c))
		}
		sort.Strings(candidates)
	}
	candidates = append(candidates, v.With...)
	names := map[string]bool{}
	gone := map[string]bool{}
	var dropped []string
	for _, p := range candidates {
		if names[p] || gone[p] {
			continue
		}
		k, ok := b.KindOf(p)
		declared := ok && k == CharacterKind || !ok && occupantIndex(b, OccupantID(p)) >= 0
		if declared && len(names) < MaxJournalParticipants {
			names[p] = true
			e.Participants = append(e.Participants, p)
			continue
		}
		gone[p] = true
		dropped = append(dropped, p)
	}
	sort.Strings(e.Participants)
	sort.Strings(dropped)
	if b.Journal == nil {
		b.Journal = &Journal{Next: 1}
	}
	j := b.Journal
	if j.Next == math.MaxUint64 {
		return nil, fail("E_OVERFLOW", id, "journal", "an unused seq", "maximum", "journal seq exhausted")
	}
	e.Seq = j.Next
	j.Next++
	var old *JournalEntry
	if len(j.Entries) >= MaxJournalEntries {
		d := j.Entries[0]
		old = &d
		j.Entries = append([]JournalEntry(nil), j.Entries[1:]...)
	}
	j.Entries = append(j.Entries, e)
	return []Event{{Kind: JournalRecorded, Journal: &JournalChange{Intent: *cloneRecordJournal(&v), Entry: cloneJournalEntry(e), Dropped: old, DroppedParticipants: dropped}}}, nil
}

// JournalFilter selects entries. Every field given must match; Limit 0
// means no limit.
type JournalFilter struct {
	IDs         []JournalEntryID `json:"id,omitempty"`
	After       uint64           `json:"after,omitempty"`
	Since       *uint64          `json:"since,omitempty"`
	Place       LocationID       `json:"place,omitempty"`
	Participant string           `json:"participant,omitempty"`
	Kinds       []JournalKind    `json:"kind,omitempty"`
	Limit       int              `json:"limit,omitempty"`
}

// JournalProjection is the entries a filter selects, in seq order, and the
// seq the next entry will take.
type JournalProjection struct {
	Revision Revision       `json:"revision"`
	Next     uint64         `json:"next"`
	Entries  []JournalEntry `json:"entries"`
}

// JournalView reads the journal at one revision.
func (w *World) JournalView(f JournalFilter) (JournalProjection, error) {
	for _, k := range f.Kinds {
		if !validJournalKind(k) {
			return JournalProjection{}, journalFault("", "kind", journalKinds, string(k))
		}
	}
	if f.Limit < 0 || f.Limit > MaxJournalEntries {
		return JournalProjection{}, journalFault("", "limit", "0 to 1024", strconv.Itoa(f.Limit))
	}
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	s := &w.authority.state
	out := JournalProjection{Revision: s.Revision, Next: 1, Entries: []JournalEntry{}}
	j := s.Blueprint.Journal
	if j == nil {
		return out, nil
	}
	out.Next = j.Next
	ids := make(map[JournalEntryID]bool, len(f.IDs))
	for _, id := range f.IDs {
		ids[id] = true
	}
	kinds := make(map[JournalKind]bool, len(f.Kinds))
	for _, k := range f.Kinds {
		kinds[k] = true
	}
	for _, e := range j.Entries {
		switch {
		case e.Seq <= f.After,
			len(ids) > 0 && !ids[e.ID],
			f.Since != nil && (e.Tick == nil || *e.Tick < *f.Since),
			f.Place != "" && e.Place != f.Place,
			len(kinds) > 0 && !kinds[e.Kind]:
			continue
		}
		if f.Participant != "" {
			i := sort.SearchStrings(e.Participants, f.Participant)
			if i == len(e.Participants) || e.Participants[i] != f.Participant {
				continue
			}
		}
		out.Entries = append(out.Entries, cloneJournalEntry(e))
		if f.Limit > 0 && len(out.Entries) == f.Limit {
			break
		}
	}
	return out, nil
}
