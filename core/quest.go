package core

import (
	"sort"
	"strconv"
	"strings"
)

// MaxWorldQuests bounds one world's quests.
const MaxWorldQuests = 4096

// MaxQuestRequirements bounds the quests one quest requires.
const MaxQuestRequirements = 16

// MaxQuestLog bounds one quest's log. Notes are dropped first.
const MaxQuestLog = 16

// MaxQuestTextBytes bounds an agent's text: an impact or a reason.
const MaxQuestTextBytes = 512

type QuestID string
type QuestStatus string
type QuestEntryKind string

const (
	StatusUnstarted QuestStatus = "unstarted"
	StatusStarted   QuestStatus = "started"
	StatusCompleted QuestStatus = "completed"
	StatusFailed    QuestStatus = "failed"

	EntryStarted   QuestEntryKind = "started"
	EntryNote      QuestEntryKind = "note"
	EntryCompleted QuestEntryKind = "completed"
	EntryFailed    QuestEntryKind = "failed"
	EntryReopened  QuestEntryKind = "reopened"
)

const (
	QuestStarted   EventKind = "QuestStarted"
	QuestNoted     EventKind = "QuestNoted"
	QuestCompleted EventKind = "QuestCompleted"
	QuestFailed    EventKind = "QuestFailed"
	QuestReopened  EventKind = "QuestReopened"
)

// Quest is a story goal the agent pursues. Requires names the quests that
// must be completed before this one completes; SideOf groups a side quest
// under another and is never read for rules. Status follows the log: the
// engine writes both, and a declared quest states them back.
type Quest struct {
	ID       QuestID      `json:"id"`
	Name     string       `json:"name"`
	SideOf   QuestID      `json:"side_of,omitempty"`
	Requires []QuestID    `json:"requires,omitempty"`
	Status   QuestStatus  `json:"status"`
	Log      []QuestEntry `json:"log,omitempty"`
}

// QuestEntry is one line of a quest's log, never rewritten. Text is the
// agent's impact; Reason says why a quest reopened or completed ahead of
// the requirements in Ahead. The engine keeps both and never reads them.
type QuestEntry struct {
	Kind   QuestEntryKind `json:"kind"`
	Text   *string        `json:"text,omitempty"`
	Ahead  []QuestID      `json:"ahead,omitempty"`
	Reason *string        `json:"reason,omitempty"`
	Tick   *uint64        `json:"tick,omitempty"`
}

// QuestOp is one change to one quest. Exactly one variant is present.
type QuestOp struct {
	Start    *QuestStep     `json:"start,omitempty"`
	Note     *QuestStep     `json:"note,omitempty"`
	Complete *CompleteQuest `json:"complete,omitempty"`
	Fail     *QuestStep     `json:"fail,omitempty"`
	Reopen   *ReopenQuest   `json:"reopen,omitempty"`
}
type QuestStep struct {
	Quest QuestID `json:"quest"`
	Text  *string `json:"text,omitempty"`
}

// CompleteQuest completes a quest. Ahead, when present, is why it completes
// before its requirements.
type CompleteQuest struct {
	Quest QuestID `json:"quest"`
	Ahead *string `json:"ahead,omitempty"`
	Text  *string `json:"text,omitempty"`
}
type ReopenQuest struct {
	Quest  QuestID `json:"quest"`
	Reason *string `json:"reason,omitempty"`
}

// QuestChange is a quest event: the request's intent, the record before and
// after, and the note dropped to make room in a full log.
type QuestChange struct {
	Intent  QuestOp     `json:"intent"`
	Before  Quest       `json:"before"`
	After   Quest       `json:"after"`
	Dropped *QuestEntry `json:"dropped,omitempty"`
}

func questFault(subject, field, expected, actual string) error {
	return fail("E_QUEST", subject, field, expected, actual, "invalid quest "+field)
}

func questMissing(id QuestID) error {
	return fail("E_REFERENCE", string(id), "reference", "quest", "missing", "entity does not exist")
}

func cloneText(s *string) *string { return copyPtr(s) }

func cloneQuestEntry(e QuestEntry) QuestEntry {
	e.Text, e.Reason, e.Tick = cloneText(e.Text), cloneText(e.Reason), copyPtr(e.Tick)
	e.Ahead = append([]QuestID(nil), e.Ahead...)
	return e
}

func cloneQuest(q Quest) Quest {
	q.Requires = append([]QuestID(nil), q.Requires...)
	if q.Log != nil {
		log := make([]QuestEntry, len(q.Log))
		for i, e := range q.Log {
			log[i] = cloneQuestEntry(e)
		}
		q.Log = log
	}
	return q
}

func cloneQuests(list []Quest) []Quest {
	if list == nil {
		return nil
	}
	out := make([]Quest, len(list))
	for i := range list {
		out[i] = cloneQuest(list[i])
	}
	return out
}

func cloneQuestStep(v *QuestStep) *QuestStep {
	if v == nil {
		return nil
	}
	return &QuestStep{Quest: v.Quest, Text: cloneText(v.Text)}
}

func cloneQuestOp(o *QuestOp) *QuestOp {
	if o == nil {
		return nil
	}
	out := &QuestOp{Start: cloneQuestStep(o.Start), Note: cloneQuestStep(o.Note), Fail: cloneQuestStep(o.Fail)}
	if v := o.Complete; v != nil {
		out.Complete = &CompleteQuest{Quest: v.Quest, Ahead: cloneText(v.Ahead), Text: cloneText(v.Text)}
	}
	if v := o.Reopen; v != nil {
		out.Reopen = &ReopenQuest{Quest: v.Quest, Reason: cloneText(v.Reason)}
	}
	return out
}

func cloneQuestChange(c *QuestChange) *QuestChange {
	if c == nil {
		return nil
	}
	out := &QuestChange{Intent: *cloneQuestOp(&c.Intent), Before: cloneQuest(c.Before), After: cloneQuest(c.After)}
	if c.Dropped != nil {
		d := cloneQuestEntry(*c.Dropped)
		out.Dropped = &d
	}
	return out
}

func sortQuestIDs(ids []QuestID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

// normalizeQuests sorts quests by ID and each quest's requirements. A log
// stays in the order it was written.
func normalizeQuests(b *Blueprint) {
	sort.Slice(b.Quests, func(i, j int) bool { return b.Quests[i].ID < b.Quests[j].ID })
	for i := range b.Quests {
		sortQuestIDs(b.Quests[i].Requires)
		for j := range b.Quests[i].Log {
			sortQuestIDs(b.Quests[i].Log[j].Ahead)
		}
	}
}

func (o QuestOp) variants() int {
	n := 0
	for _, yes := range []bool{o.Start != nil, o.Note != nil, o.Complete != nil, o.Fail != nil, o.Reopen != nil} {
		if yes {
			n++
		}
	}
	return n
}

// subject is the quest the operation changes.
func (o QuestOp) subject() QuestID {
	switch {
	case o.Start != nil:
		return o.Start.Quest
	case o.Note != nil:
		return o.Note.Quest
	case o.Complete != nil:
		return o.Complete.Quest
	case o.Fail != nil:
		return o.Fail.Quest
	}
	return o.Reopen.Quest
}

// QuestStatusOf is the status a log leaves a quest in: the last status
// change, or unstarted without one.
func QuestStatusOf(log []QuestEntry) QuestStatus {
	s := StatusUnstarted
	for _, e := range log {
		switch e.Kind {
		case EntryStarted, EntryReopened:
			s = StatusStarted
		case EntryCompleted:
			s = StatusCompleted
		case EntryFailed:
			s = StatusFailed
		}
	}
	return s
}

const textLimit = "valid text of at most 512 bytes"

// checkQuestText checks an agent's text, when given.
func checkQuestText(id QuestID, field string, s *string) error {
	if s == nil {
		return nil
	}
	if !validID(*s) {
		return questFault(string(id), field, textLimit, "invalid")
	}
	if len(*s) > MaxQuestTextBytes {
		return questFault(string(id), field, textLimit, strconv.Itoa(len(*s))+" bytes")
	}
	return nil
}

// next is the status an entry moves a quest to from s, if the actions allow
// that entry there.
func next(s QuestStatus, k QuestEntryKind) (QuestStatus, bool) {
	switch k {
	case EntryStarted:
		return StatusStarted, s == StatusUnstarted
	case EntryNote:
		return s, s == StatusStarted
	case EntryCompleted:
		return StatusCompleted, s == StatusUnstarted || s == StatusStarted
	case EntryFailed:
		return StatusFailed, s == StatusUnstarted || s == StatusStarted
	}
	return StatusStarted, s == StatusCompleted || s == StatusFailed
}

// checkQuest checks one record on its own: its fields, its links to declared
// quests, and a log the actions could have written.
func checkQuest(q Quest, declared func(QuestID) bool) error {
	id := string(q.ID)
	if !validID(id) {
		return questFault(id, "id", "valid quest ID", "invalid")
	}
	if !validID(q.Name) {
		return questFault(id, "name", "valid name", "invalid")
	}
	if q.SideOf != "" {
		if q.SideOf == q.ID {
			return questFault(id, "side_of", "another quest", string(q.SideOf))
		}
		if !declared(q.SideOf) {
			return questFault(id, "side_of", "a declared quest", string(q.SideOf))
		}
	}
	if len(q.Requires) > MaxQuestRequirements {
		return questFault(id, "requires", "at most 16 requirements", strconv.Itoa(len(q.Requires)))
	}
	if err := checkQuestIDs(q.ID, "requires", q.Requires, declared); err != nil {
		return err
	}
	switch q.Status {
	case StatusUnstarted, StatusStarted, StatusCompleted, StatusFailed:
	default:
		return questFault(id, "status", "unstarted, started, completed or failed", string(q.Status))
	}
	if len(q.Log) > MaxQuestLog {
		return questFault(id, "log", "at most 16 entries", strconv.Itoa(len(q.Log)))
	}
	s := StatusUnstarted
	for _, e := range q.Log {
		switch e.Kind {
		case EntryStarted, EntryNote, EntryCompleted, EntryFailed, EntryReopened:
		default:
			return questFault(id, "log", "started, note, completed, failed or reopened", string(e.Kind))
		}
		after, ok := next(s, e.Kind)
		if !ok {
			return questFault(id, "log", "a log the actions allow", string(e.Kind)+" after "+string(s))
		}
		if err := checkEntry(q.ID, e, declared); err != nil {
			return err
		}
		s = after
	}
	if s != q.Status {
		return questFault(id, "status", string(s)+", as the log ends", string(q.Status))
	}
	return nil
}

// checkEntry checks one log entry's texts and requirements.
func checkEntry(id QuestID, e QuestEntry, declared func(QuestID) bool) error {
	if err := checkQuestText(id, "text", e.Text); err != nil {
		return err
	}
	if err := checkQuestText(id, "reason", e.Reason); err != nil {
		return err
	}
	switch {
	case e.Kind == EntryNote && e.Text == nil:
		return questFault(string(id), "text", "text for a note", "absent")
	case e.Kind == EntryReopened && e.Text != nil:
		return questFault(string(id), "text", "no text for reopened", "present")
	case e.Kind == EntryReopened && e.Reason == nil:
		return questFault(string(id), "reason", "a reason for reopened", "absent")
	case e.Kind == EntryCompleted && len(e.Ahead) > 0 && e.Reason == nil:
		return questFault(string(id), "reason", "a reason for ahead", "absent")
	case e.Kind == EntryCompleted && len(e.Ahead) == 0 && e.Reason != nil:
		return questFault(string(id), "reason", "a reason only with ahead", "present")
	case e.Kind != EntryReopened && e.Kind != EntryCompleted && e.Reason != nil:
		return questFault(string(id), "reason", "a reason only for reopened or ahead", string(e.Kind))
	case e.Kind != EntryCompleted && len(e.Ahead) > 0:
		return questFault(string(id), "ahead", "ahead only for completed", string(e.Kind))
	}
	if len(e.Ahead) > MaxQuestRequirements {
		return questFault(string(id), "ahead", "at most 16 requirements", strconv.Itoa(len(e.Ahead)))
	}
	return checkQuestIDs(id, "ahead", e.Ahead, declared)
}

// checkQuestIDs checks a list of other declared quests, each once.
func checkQuestIDs(id QuestID, field string, ids []QuestID, declared func(QuestID) bool) error {
	seen := make(map[QuestID]bool, len(ids))
	for _, r := range ids {
		switch {
		case r == id:
			return questFault(string(id), field, "another quest", string(r))
		case !declared(r):
			return questFault(string(id), field, "a declared quest", string(r))
		case seen[r]:
			return questFault(string(id), field, "each quest once", string(r))
		}
		seen[r] = true
	}
	return nil
}

// validateQuests checks every quest against its world. Quest IDs are their
// own and differ from every character, item and location ID; occupants,
// checked first, differ from quests. Requirements never form a cycle.
func validateQuests(b *Blueprint, refs Refs) error {
	if len(b.Quests) == 0 {
		return nil
	}
	if len(b.Quests) > MaxWorldQuests {
		return questFault("", "quests", "at most 4096 quests", "exceeded")
	}
	index := make(map[QuestID]int, len(b.Quests))
	for i, q := range b.Quests {
		_, taken := refs.KindOf(string(q.ID))
		if _, dup := index[q.ID]; dup || taken {
			return fail("E_DUPLICATE", string(q.ID), "quest", "unique quest ID", string(q.ID), "duplicate quest identity")
		}
		index[q.ID] = i
	}
	declared := func(id QuestID) bool { _, ok := index[id]; return ok }
	for _, q := range b.Quests {
		if err := checkQuest(q, declared); err != nil {
			return err
		}
	}
	// Depth-first, in ID order: a requirement still on the path closes a
	// cycle, named by the quest that requires it.
	const (
		fresh = iota
		onPath
		done
	)
	state := make([]int, len(b.Quests))
	var visit func(i int) error
	visit = func(i int) error {
		state[i] = onPath
		for _, r := range b.Quests[i].Requires {
			j := index[r]
			switch state[j] {
			case onPath:
				return questFault(string(b.Quests[i].ID), "requires", "no cycle", string(r))
			case fresh:
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		state[i] = done
		return nil
	}
	for i := range b.Quests {
		if state[i] == fresh {
			if err := visit(i); err != nil {
				return err
			}
		}
	}
	return nil
}

func questIndex(b *Blueprint, id QuestID) int {
	for i := range b.Quests {
		if b.Quests[i].ID == id {
			return i
		}
	}
	return -1
}

// checkQuestOp checks what a quest operation names and the texts it gives.
// Whether the change applies to the quest's current state is checked when it
// runs.
func (b Blueprint) checkQuestOp(o QuestOp) error {
	if o.variants() != 1 {
		return fail("E_IR", "", "quest", "exactly one quest change", "invalid sum", "quest operation requires exactly one variant")
	}
	id := o.subject()
	if questIndex(&b, id) < 0 {
		return questMissing(id)
	}
	switch {
	case o.Start != nil:
		return checkQuestText(id, "text", o.Start.Text)
	case o.Note != nil:
		if o.Note.Text == nil {
			return questFault(string(id), "text", "text for a note", "absent")
		}
		return checkQuestText(id, "text", o.Note.Text)
	case o.Complete != nil:
		if err := checkQuestText(id, "reason", o.Complete.Ahead); err != nil {
			return err
		}
		return checkQuestText(id, "text", o.Complete.Text)
	case o.Fail != nil:
		return checkQuestText(id, "text", o.Fail.Text)
	}
	if o.Reopen.Reason == nil {
		return questFault(string(id), "reason", "a reason for reopen", "absent")
	}
	return checkQuestText(id, "reason", o.Reopen.Reason)
}

// openRequirements lists the quest's requirements not yet completed.
func openRequirements(b *Blueprint, q Quest) []QuestID {
	var open []QuestID
	for _, r := range q.Requires {
		if i := questIndex(b, r); i < 0 || b.Quests[i].Status != StatusCompleted {
			open = append(open, r)
		}
	}
	return open
}

func joinQuestIDs(ids []QuestID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = string(id)
	}
	return strings.Join(s, ", ")
}

// withEntry is the log with e added. A full log drops its oldest note to make
// room; a full log of status changes takes no more.
func withEntry(id QuestID, log []QuestEntry, e QuestEntry) ([]QuestEntry, *QuestEntry, error) {
	out := append([]QuestEntry(nil), log...)
	var dropped *QuestEntry
	if len(out) >= MaxQuestLog {
		at := -1
		for i := range out {
			if out[i].Kind == EntryNote {
				at = i
				break
			}
		}
		if at < 0 {
			return nil, nil, questFault(string(id), "log", "at most 16 status changes", "full")
		}
		d := out[at]
		dropped = &d
		out = append(out[:at], out[at+1:]...)
	}
	return append(out, e), dropped, nil
}

// applyQuest makes one quest change and records it.
func applyQuest(b *Blueprint, op QuestOp) ([]Event, error) {
	id := op.subject()
	i := questIndex(b, id)
	if i < 0 {
		return nil, questMissing(id)
	}
	q := &b.Quests[i]
	e := QuestEntry{}
	if b.Clock != nil {
		t := b.Clock.Tick
		e.Tick = &t
	}
	status, kind := q.Status, EventKind("")
	open := func(want string) error { return questFault(string(id), "status", want, string(q.Status)) }
	switch {
	case op.Start != nil:
		switch q.Status {
		case StatusStarted:
			return nil, noChange(string(id))
		case StatusCompleted, StatusFailed:
			return nil, open("unstarted")
		}
		e.Kind, e.Text, status, kind = EntryStarted, cloneText(op.Start.Text), StatusStarted, QuestStarted
	case op.Note != nil:
		if q.Status != StatusStarted {
			return nil, open("started")
		}
		e.Kind, e.Text, kind = EntryNote, cloneText(op.Note.Text), QuestNoted
	case op.Complete != nil:
		switch q.Status {
		case StatusCompleted:
			return nil, noChange(string(id))
		case StatusFailed:
			return nil, open("unstarted or started")
		}
		reqs := openRequirements(b, *q)
		switch {
		case op.Complete.Ahead == nil && len(reqs) > 0:
			return nil, questFault(string(id), "requires", "completed requirements", joinQuestIDs(reqs))
		case op.Complete.Ahead != nil && len(reqs) == 0:
			return nil, questFault(string(id), "ahead", "an open requirement", "none")
		}
		e.Kind, e.Text, status, kind = EntryCompleted, cloneText(op.Complete.Text), StatusCompleted, QuestCompleted
		if op.Complete.Ahead != nil {
			e.Ahead, e.Reason = reqs, cloneText(op.Complete.Ahead)
		}
	case op.Fail != nil:
		switch q.Status {
		case StatusFailed:
			return nil, noChange(string(id))
		case StatusCompleted:
			return nil, open("unstarted or started")
		}
		e.Kind, e.Text, status, kind = EntryFailed, cloneText(op.Fail.Text), StatusFailed, QuestFailed
	case op.Reopen != nil:
		if q.Status != StatusCompleted && q.Status != StatusFailed {
			return nil, open("completed or failed")
		}
		e.Kind, e.Reason, status, kind = EntryReopened, cloneText(op.Reopen.Reason), StatusStarted, QuestReopened
	}
	log, dropped, err := withEntry(id, q.Log, e)
	if err != nil {
		return nil, err
	}
	before := cloneQuest(*q)
	q.Status, q.Log = status, log
	return []Event{{Kind: kind, Quest: &QuestChange{Intent: *cloneQuestOp(&op), Before: before, After: cloneQuest(*q), Dropped: dropped}}}, nil
}

func questEvent(k EventKind) bool {
	switch k {
	case QuestStarted, QuestNoted, QuestCompleted, QuestFailed, QuestReopened:
		return true
	}
	return false
}

// QuestProjection is one quest as the agent reads it: the record, the
// requirements still open, and whether it is ready to finish.
type QuestProjection struct {
	Revision Revision `json:"revision"`
	Quest
	Open  []QuestID `json:"open"`
	Ready bool      `json:"ready"`
}

// QuestsView reads quests in the order given, all at one revision. At an
// unknown quest it returns the views before it and the fault.
func (w *World) QuestsView(ids []QuestID) ([]QuestProjection, error) {
	w.authority.mu.RLock()
	defer w.authority.mu.RUnlock()
	s := &w.authority.state
	b := &s.Blueprint
	index := make(map[QuestID]int, len(b.Quests))
	for i, q := range b.Quests {
		index[q.ID] = i
	}
	out := make([]QuestProjection, 0, len(ids))
	for _, id := range ids {
		i, ok := index[id]
		if !ok {
			return out, questMissing(id)
		}
		q := cloneQuest(b.Quests[i])
		open := []QuestID{}
		for _, r := range q.Requires {
			if j, ok := index[r]; !ok || b.Quests[j].Status != StatusCompleted {
				open = append(open, r)
			}
		}
		ready := len(open) == 0 && (q.Status == StatusUnstarted || q.Status == StatusStarted)
		out = append(out, QuestProjection{Revision: s.Revision, Quest: q, Open: open, Ready: ready})
	}
	return out, nil
}
