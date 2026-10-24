// Package livestate keeps what only the engine writes between calls, for a
// host that writes a fresh world source for each call.
//
// The world source declares the static world and the seeds: every place, the
// characters and the authored occupants. The document holds the occupants as
// play left them. Each place is taken once: the first call that declares it
// takes its seeds, and from then on its occupants come from the document and
// the source's seeds for it are ignored. A place is taken whole, never one
// occupant at a time, so a renamed seed cannot duplicate a group in play.
//
// In a world with a map, the document also holds where each character
// stands and the map state play changes: the party, the visited places, the
// declared hazards and the closed routes. Characters are taken once, like
// places, and so are the hazards on a place and the closures of the routes
// that touch it. The routes, the clock and the sheets stay with the world
// source.
// A world that stops declaring a map keeps the held characters and map
// state, unchanged, and ignores them until it declares a map again.
//
// The document holds each quest's status and log, taken once like a
// character. A held quest the world stops declaring is kept, unchanged, and
// ignored until the world declares it again.
//
// The document holds the journal, every entry as recorded. It replaces the
// source's journal; a document that holds none takes the source's once.
// The places and participants an entry names are history: they are not
// checked against the world.
//
// The host stores the document whole and passes it back unchanged. Only the
// engine writes it: a wrong format or a checksum that does not match refuses
// it. The checksum detects damage; it is never the identity of anything.
// The recent requests that changed what it holds are kept with a digest, so
// a retry after a crash is answered as already applied instead of applying
// twice.
package livestate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MoonlightByte/firelight/core"
)

const (
	// Format names the document.
	Format = "nql-live-state"
	// Version is the document version this engine writes. It reads every
	// version from 1.
	Version = 5
	// MaxRequests bounds the recent requests kept for retries.
	MaxRequests = 64
)

// Document is the live state.
//
// Places are the places whose occupants the document holds, sorted.
// Occupants are sorted by ID, each at one of those places. With a map, the
// document holds the map state and where each character stands, sorted by
// ID; without one it holds those an earlier world with a map left, or
// neither. Quests are sorted by ID. Clock is the world's clock as play left
// it, held from the first world that declares one. Journal is the world's
// journal, absent until an entry is recorded or declared. Revision counts
// the requests that changed what the document holds; Requests are the last of
// them, oldest first, at consecutive revisions ending at Revision.
type Document struct {
	Format     string            `json:"format"`
	Version    int               `json:"version"`
	Revision   uint64            `json:"revision"`
	Places     []core.LocationID `json:"places"`
	Occupants  []core.Occupant   `json:"occupants"`
	Characters []Character       `json:"characters,omitempty"`
	Map        *Map              `json:"map,omitempty"`
	Quests     []Quest           `json:"quests,omitempty"`
	Clock      *core.Clock       `json:"clock,omitempty"`
	Journal    *core.Journal     `json:"journal,omitempty"`
	Requests   []Request         `json:"requests"`
	Checksum   string            `json:"checksum,omitempty"`
}

// Character is where a held character stands.
type Character struct {
	ID       core.CharacterID `json:"id"`
	Location core.LocationID  `json:"location"`
}

// Quest is a held quest's play state. Its name and requirements stay with
// the world source.
type Quest struct {
	ID     core.QuestID      `json:"id"`
	Status core.QuestStatus  `json:"status"`
	Log    []core.QuestEntry `json:"log,omitempty"`
}

// Map is the map state play changes, each list sorted: the party, the
// visited places, the declared hazards and the closed routes. The routes
// themselves stay with the world source.
type Map struct {
	Party   []core.CharacterID `json:"party"`
	Visited []core.LocationID  `json:"visited"`
	Hazards []core.LocationID  `json:"hazards"`
	Closed  []core.MapLink     `json:"closed"`
}

// Request is a recent request that changed what the document holds: its ID,
// the revision it made and the digest of its actor and operations.
type Request struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

func fault(subject, field, expected, actual string) error {
	return &core.Fault{Code: "E_LIVE_STATE", Subject: subject, Field: field, Expected: expected, Actual: actual, Message: "live state refused: " + field}
}

// validText is the engine's rule for IDs: valid UTF-8, not blank, no control
// characters.
func validText(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// checksum is the SHA-256 of the document's encoding without the checksum.
func checksum(d Document) string {
	d.Checksum = ""
	if d.Places == nil {
		d.Places = []core.LocationID{}
	}
	if d.Occupants == nil {
		d.Occupants = []core.Occupant{}
	}
	if d.Requests == nil {
		d.Requests = []Request{}
	}
	if d.Map != nil {
		d.Map = fullMap(*d.Map)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		// Every field encodes; a failure is an engine fault.
		panic("livestate: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fullMap is m with every list present, as the engine writes it.
func fullMap(m Map) *Map {
	if m.Party == nil {
		m.Party = []core.CharacterID{}
	}
	if m.Visited == nil {
		m.Visited = []core.LocationID{}
	}
	if m.Hazards == nil {
		m.Hazards = []core.LocationID{}
	}
	if m.Closed == nil {
		m.Closed = []core.MapLink{}
	}
	return &m
}

func linkLess(a, b core.MapLink) bool {
	if a.From != b.From {
		return a.From < b.From
	}
	return a.To < b.To
}

// Check refuses a document the engine did not write as it is. It checks the
// document on its own; Merge checks it against the world.
func Check(d *Document) error {
	if d.Format != Format {
		return fault("", "format", Format, d.Format)
	}
	if d.Version < 1 || d.Version > Version {
		return fault("", "version", "1 to "+strconv.Itoa(Version), strconv.Itoa(d.Version))
	}
	if d.Checksum == "" {
		return fault("", "checksum", "the checksum the engine wrote", "absent")
	}
	if d.Checksum != checksum(*d) {
		return fault("", "checksum", "the checksum the engine wrote", "mismatch")
	}
	places := make(map[core.LocationID]bool, len(d.Places))
	for i, p := range d.Places {
		if !validText(string(p)) || i > 0 && p <= d.Places[i-1] {
			return fault(string(p), "places", "valid place IDs, sorted, each once", string(p))
		}
		places[p] = true
	}
	for i, o := range d.Occupants {
		if i > 0 && o.ID <= d.Occupants[i-1].ID {
			return fault(string(o.ID), "occupants", "occupants sorted by ID, each once", string(o.ID))
		}
		if !places[o.Location] {
			return fault(string(o.ID), "location", "a place the document holds", string(o.Location))
		}
	}
	if err := checkMap(d, places); err != nil {
		return err
	}
	if d.Version < 3 && len(d.Quests) > 0 {
		return fault("", "version", "3 for quests", strconv.Itoa(d.Version))
	}
	if d.Clock != nil {
		if d.Version < 4 {
			return fault("", "version", "4 for a clock", strconv.Itoa(d.Version))
		}
		if !validText(d.Clock.Unit) {
			return fault("", "clock", "a valid clock unit", d.Clock.Unit)
		}
	}
	if d.Journal != nil {
		if d.Version < 5 {
			return fault("", "version", "5 for a journal", strconv.Itoa(d.Version))
		}
		if err := core.CheckJournal(*d.Journal); err != nil {
			f, _ := err.(*core.Fault)
			return fault(f.Subject, "journal", f.Expected, f.Actual)
		}
	}
	for i, q := range d.Quests {
		if !validText(string(q.ID)) || i > 0 && q.ID <= d.Quests[i-1].ID {
			return fault(string(q.ID), "quests", "valid quest IDs, sorted, each once", string(q.ID))
		}
	}
	if d.Revision == math.MaxUint64 {
		return fault("", "revision", "a revision below the maximum", "maximum")
	}
	want := min(d.Revision, MaxRequests)
	if uint64(len(d.Requests)) != want {
		return fault("", "requests", strconv.FormatUint(want, 10)+" recent requests", strconv.Itoa(len(d.Requests)))
	}
	seen := make(map[string]bool, len(d.Requests))
	for i, r := range d.Requests {
		if !validText(r.ID) || seen[r.ID] {
			return fault(r.ID, "request", "valid request IDs, each once", r.ID)
		}
		seen[r.ID] = true
		if rev := d.Revision - want + 1 + uint64(i); r.Revision != rev {
			return fault(r.ID, "revision", strconv.FormatUint(rev, 10), strconv.FormatUint(r.Revision, 10))
		}
		if !validDigest(r.Digest) {
			return fault(r.ID, "digest", "64 lowercase hex digits", r.Digest)
		}
	}
	return nil
}

// checkMap checks the characters and the map state: each list sorted, each
// entry once, at places the document holds, and the party among the
// characters.
func checkMap(d *Document, places map[core.LocationID]bool) error {
	if d.Version == 1 && (len(d.Characters) > 0 || d.Map != nil) {
		return fault("", "version", "2 for characters or a map", "1")
	}
	if len(d.Characters) > 0 && d.Map == nil {
		return fault("", "characters", "held only with a map", "no map")
	}
	held := make(map[core.CharacterID]bool, len(d.Characters))
	for i, c := range d.Characters {
		if !validText(string(c.ID)) || i > 0 && c.ID <= d.Characters[i-1].ID {
			return fault(string(c.ID), "characters", "valid character IDs, sorted, each once", string(c.ID))
		}
		if !places[c.Location] {
			return fault(string(c.ID), "location", "a place the document holds", string(c.Location))
		}
		held[c.ID] = true
	}
	if d.Map == nil {
		return nil
	}
	const order = "sorted, each once, held"
	for i, p := range d.Map.Party {
		if i > 0 && p <= d.Map.Party[i-1] || !held[p] {
			return fault(string(p), "party", order, string(p))
		}
	}
	for _, list := range []struct {
		field string
		ids   []core.LocationID
	}{{"visited", d.Map.Visited}, {"hazards", d.Map.Hazards}} {
		for i, p := range list.ids {
			if i > 0 && p <= list.ids[i-1] || !places[p] {
				return fault(string(p), list.field, order, string(p))
			}
		}
	}
	for i, l := range d.Map.Closed {
		if i > 0 && !linkLess(d.Map.Closed[i-1], l) || !places[l.From] || !places[l.To] {
			return fault(string(l.From), "closed", "routes between held places, sorted, each once", string(l.From)+" -> "+string(l.To))
		}
	}
	return nil
}

// Merge sets the world's occupants: the document's at the places it holds,
// and the world source's seeds at every other place. Every place the document
// holds must still be declared. It sets each held quest's status and log and,
// with a map, where each held character stands and the map state, with the
// source's hazards and closures at places it does not hold yet. A world
// without a map leaves the source's characters and ignores the held map.
// The world then validates the records.
func Merge(b *core.Blueprint, d *Document) error {
	declared := make(map[core.LocationID]bool, len(b.Locations))
	for _, l := range b.Locations {
		declared[l.ID] = true
	}
	held := make(map[core.LocationID]bool, len(d.Places))
	for _, p := range d.Places {
		if !declared[p] {
			return fault(string(p), "place", "a place the world declares", "not declared")
		}
		held[p] = true
	}
	occupants := append([]core.Occupant(nil), d.Occupants...)
	for _, o := range b.Occupants {
		if !held[o.Location] {
			occupants = append(occupants, o)
		}
	}
	b.Occupants = occupants
	mergeQuests(b, d)
	if d.Journal != nil {
		j := cloneJournal(*d.Journal)
		b.Journal = &j
	}
	if err := mergeClock(b, d); err != nil {
		return err
	}
	if d.Map == nil {
		// A world that declares a map takes its state from the source once.
		return nil
	}
	if b.Map == nil {
		// A world without a map holds the document's characters and map
		// unchanged, and ignores them until it declares a map again.
		return nil
	}
	// The source's characters and map are copied before they change: the
	// source alone may still be needed to place a fault.
	at := make(map[core.CharacterID]int, len(b.Characters))
	b.Characters = append([]core.Character(nil), b.Characters...)
	for i, c := range b.Characters {
		at[c.ID] = i
	}
	for _, p := range d.Map.Party {
		if _, ok := at[p]; !ok {
			return fault(string(p), "party", "a character the world declares", "not declared")
		}
	}
	// A held character the world no longer declares is dropped; a new one
	// stands where the source says and is held from now on.
	for _, c := range d.Characters {
		if i, ok := at[c.ID]; ok {
			b.Characters[i].Location = c.Location
		}
	}
	routes := make(map[core.MapLink]bool, len(b.Map.Routes))
	for _, r := range b.Map.Routes {
		routes[core.MapLink{From: r.From, To: r.To}] = true
	}
	m := *b.Map
	m.Party = append([]core.CharacterID(nil), d.Map.Party...)
	m.Visited = append([]core.LocationID(nil), d.Map.Visited...)
	m.Hazards = append([]core.LocationID(nil), d.Map.Hazards...)
	m.Closed = nil
	// A closure of a route the world does not declare is held out of the
	// world, unchanged; Next keeps it, and it applies again when the world
	// declares the route again.
	for _, l := range d.Map.Closed {
		if routes[l] {
			m.Closed = append(m.Closed, l)
		}
	}
	// A place the document does not hold yet takes the source's hazard on
	// it and the closures of the routes that touch it, as it takes its
	// seeds: play cannot have changed them. Its visited mark is not taken.
	hazard := make(map[core.LocationID]bool, len(m.Hazards))
	for _, p := range m.Hazards {
		hazard[p] = true
	}
	for _, p := range b.Map.Hazards {
		if !held[p] && !hazard[p] {
			hazard[p] = true
			m.Hazards = append(m.Hazards, p)
		}
	}
	closed := make(map[core.MapLink]bool, len(m.Closed))
	for _, l := range m.Closed {
		closed[l] = true
	}
	for _, l := range b.Map.Closed {
		if (!held[l.From] || !held[l.To]) && !closed[l] {
			closed[l] = true
			m.Closed = append(m.Closed, l)
		}
	}
	b.Map = &m
	return nil
}

// mergeClock runs the world at the document's tick. A clock the document
// does not hold yet is the source's, taken once; one the world does not
// declare is held. A clock in another unit is refused: its ticks would mean
// something else. A condition the source declares for `ticks N` runs N ticks
// from the document's tick, as the source counted them from its own.
func mergeClock(b *core.Blueprint, d *Document) error {
	if d.Clock == nil || b.Clock == nil {
		return nil
	}
	if b.Clock.Unit != d.Clock.Unit {
		return fault("", "clock", "unit "+d.Clock.Unit, "unit "+b.Clock.Unit)
	}
	now := d.Clock.Tick
	conditions := make([]core.ConditionInstance, len(b.Conditions))
	for i, c := range b.Conditions {
		if l := c.ClockLifetime; l != nil {
			left := l.ExpiresTick - l.AppliedTick
			if l.ExpiresTick < l.AppliedTick || left > math.MaxUint64-now {
				return fault(string(c.ID), "clock", "a lifetime that ends after the held tick", "overflow")
			}
			c.ClockLifetime = &core.ClockLifetime{AppliedTick: now, ExpiresTick: now + left}
		}
		conditions[i] = c
	}
	if b.Conditions != nil {
		b.Conditions = conditions
	}
	c := *b.Clock
	c.Tick = now
	b.Clock = &c
	return nil
}

// mergeQuests gives each declared quest the document holds its status and
// log. A quest the document does not hold yet keeps the source's, and one
// the world no longer declares stays in the document. The source's quests
// are copied before they change: the source alone may still be needed to
// place a fault.
func mergeQuests(b *core.Blueprint, d *Document) {
	if len(d.Quests) == 0 {
		return
	}
	held := make(map[core.QuestID]Quest, len(d.Quests))
	for _, q := range d.Quests {
		held[q.ID] = q
	}
	b.Quests = append([]core.Quest(nil), b.Quests...)
	for i := range b.Quests {
		if q, ok := held[b.Quests[i].ID]; ok {
			b.Quests[i].Status, b.Quests[i].Log = q.Status, append([]core.QuestEntry(nil), q.Log...)
		}
	}
}

// Changes reports whether a receipt changed what the document holds: an
// occupant, a quest, the journal, in a world with a map where a character
// stands or the map state and, in a world with a clock, the time. It reads
// the events, not the outcome, so a request that changes a record and changes it back is still kept, as a kept world keeps
// it.
func Changes(b core.Batch, withMap, withClock bool) bool {
	for _, e := range b.Events {
		if e.Occupant != nil || e.Quest != nil || e.Journal != nil || withMap && (e.Movement != nil || e.Trip != nil || e.MapEdit != nil) || withClock && e.Time != nil {
			return true
		}
	}
	return false
}

// mapState is what the document holds of a world with a map.
func mapState(s core.Snapshot) Document {
	var d Document
	for _, c := range s.Characters {
		d.Characters = append(d.Characters, Character{ID: c.ID, Location: c.Location})
	}
	sort.Slice(d.Characters, func(i, j int) bool { return d.Characters[i].ID < d.Characters[j].ID })
	m := s.Map
	d.Map = fullMap(Map{
		Party:   append([]core.CharacterID(nil), m.Party...),
		Visited: append([]core.LocationID(nil), m.Visited...),
		Hazards: append([]core.LocationID(nil), m.Hazards...),
		Closed:  append([]core.MapLink(nil), m.Closed...),
	})
	sort.Slice(d.Map.Party, func(i, j int) bool { return d.Map.Party[i] < d.Map.Party[j] })
	sort.Slice(d.Map.Visited, func(i, j int) bool { return d.Map.Visited[i] < d.Map.Visited[j] })
	sort.Slice(d.Map.Hazards, func(i, j int) bool { return d.Map.Hazards[i] < d.Map.Hazards[j] })
	sort.Slice(d.Map.Closed, func(i, j int) bool { return linkLess(d.Map.Closed[i], d.Map.Closed[j]) })
	return d
}

// heldClosures is the world's closures and each held one of a route the
// world does not declare, sorted.
func heldClosures(closed, held []core.MapLink, routes []core.MapRoute) []core.MapLink {
	declared := make(map[core.MapLink]bool, len(routes))
	for _, r := range routes {
		declared[core.MapLink{From: r.From, To: r.To}] = true
	}
	for _, l := range held {
		if !declared[l] {
			closed = append(closed, l)
		}
	}
	sort.Slice(closed, func(i, j int) bool { return linkLess(closed[i], closed[j]) })
	return closed
}

// heldMap is a copy of the characters and the map state d holds, for a world
// that declares no map.
func heldMap(d Document) ([]Character, *Map) {
	m := *d.Map
	m.Party = append([]core.CharacterID(nil), m.Party...)
	m.Visited = append([]core.LocationID(nil), m.Visited...)
	m.Hazards = append([]core.LocationID(nil), m.Hazards...)
	m.Closed = append([]core.MapLink(nil), m.Closed...)
	return append([]Character(nil), d.Characters...), fullMap(m)
}

// Find returns the recent request with this ID, if the document keeps one.
func (d *Document) Find(id string) (Request, bool) {
	for _, r := range d.Requests {
		if r.ID == id {
			return r, true
		}
	}
	return Request{}, false
}

// Digest identifies a request's content: its actor and operations.
func Digest(actor core.Ref, ops []core.Operation) string {
	raw, err := json.Marshal(struct {
		Actor      core.Ref         `json:"actor"`
		Operations []core.Operation `json:"operations"`
	}{actor, ops})
	if err != nil {
		panic("livestate: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Next is the document after a call on the world s: it holds every place the
// world declares, the occupants and the quests as they are now and, with a
// map, every character and the map state, with each closure held in prev of a
// route the world does not declare. Without a map, the characters and
// map state held in prev are kept as they were, as is a quest held in prev
// that the world no longer declares. A request that changed what it
// holds is given the next revision and kept, with the oldest dropped past
// MaxRequests; pass an empty id for a call that changed nothing it holds.
func Next(prev Document, s core.Snapshot, id, digest string) Document {
	d := Document{Format: Format, Version: Version, Revision: prev.Revision, Places: []core.LocationID{}, Occupants: []core.Occupant{}, Requests: []Request{}}
	for _, l := range s.Locations {
		d.Places = append(d.Places, l.ID)
	}
	d.Occupants = append(d.Occupants, s.Occupants...)
	if s.Map != nil {
		m := mapState(s)
		d.Characters, d.Map = m.Characters, m.Map
		if prev.Map != nil {
			d.Map.Closed = heldClosures(d.Map.Closed, prev.Map.Closed, s.Map.Routes)
		}
	} else if prev.Map != nil {
		d.Characters, d.Map = heldMap(prev)
	}
	d.Quests = nextQuests(prev.Quests, s.Quests)
	if s.Journal != nil {
		j := cloneJournal(*s.Journal)
		d.Journal = &j
	} else if prev.Journal != nil {
		j := cloneJournal(*prev.Journal)
		d.Journal = &j
	}
	if s.Clock != nil {
		c := *s.Clock
		d.Clock = &c
	} else if prev.Clock != nil {
		c := *prev.Clock
		d.Clock = &c
	}
	d.Requests = append(d.Requests, prev.Requests...)
	if id != "" {
		d.Revision++
		d.Requests = append(d.Requests, Request{ID: id, Revision: d.Revision, Digest: digest})
		if n := len(d.Requests); n > MaxRequests {
			d.Requests = d.Requests[n-MaxRequests:]
		}
	}
	d.Checksum = checksum(d)
	return d
}

// cloneJournal copies a journal so the document and the world never share
// its entries.
func cloneJournal(j core.Journal) core.Journal {
	out := core.Journal{Next: j.Next, Entries: make([]core.JournalEntry, len(j.Entries))}
	for i, e := range j.Entries {
		if e.Tick != nil {
			t := *e.Tick
			e.Tick = &t
		}
		if e.Text != nil {
			t := *e.Text
			e.Text = &t
		}
		e.Participants = append([]string(nil), e.Participants...)
		out.Entries[i] = e
	}
	return out
}

// nextQuests is every quest the world declares, as it is now, and every held
// one it does not, as it was, sorted by ID.
func nextQuests(held []Quest, declared []core.Quest) []Quest {
	var out []Quest
	now := make(map[core.QuestID]bool, len(declared))
	for _, q := range declared {
		now[q.ID] = true
		var log []core.QuestEntry
		if len(q.Log) > 0 {
			log = append(log, q.Log...)
		}
		out = append(out, Quest{ID: q.ID, Status: q.Status, Log: log})
	}
	for _, q := range held {
		if !now[q.ID] {
			out = append(out, q)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
