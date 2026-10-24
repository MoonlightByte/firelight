// Command nql-apply is a stateless checkpoint transaction tool for external hosts.
//
// The host owns persistence. Each invocation reads one JSON document on stdin
// that carries either a world source (genesis) or a complete checkpoint
// (genesis plus history), optionally executes one transaction, optionally
// answers read-only views, and writes one JSON document on stdout that carries
// the resulting checkpoint. The process holds no state between invocations,
// opens no files and uses no platform-specific storage, so it runs wherever Go
// builds. Durability, locking and generation selection are the host's job.
//
// The checkpoint round-trips through encoding/json exactly as nativesave stores
// it; History is replayed and must agree with Head before any request is
// accepted. A repeated identical request returns its original receipt
// (historical); a changed request with a reused ID is rejected by the core.
//
// A host that writes a fresh world source for each call passes live_state
// instead: the document the last response returned, holding what only the
// engine writes (package livestate). new_live_state starts one from the
// source's seeds. docs/LIVE_STATE.md is the host guide.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"

	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"github.com/MoonlightByte/firelight/livestate"
)

// MaxInputBytes bounds one stdin document: the largest native archive plus room
// for a request and views.
const MaxInputBytes = 256 << 20

type explainQuery struct {
	Character core.CharacterID `json:"character"`
	Stat      core.StatID      `json:"stat"`
}

type input struct {
	World            string                  `json:"world,omitempty"`
	WorldName        string                  `json:"world_name,omitempty"`
	Checkpoint       *core.Checkpoint        `json:"checkpoint,omitempty"`
	Actions          string                  `json:"actions,omitempty"`
	ActionsName      string                  `json:"actions_name,omitempty"`
	Actor            *core.Ref               `json:"actor,omitempty"`
	Request          string                  `json:"request,omitempty"`
	ExpectedRevision *core.Revision          `json:"expected_revision,omitempty"`
	Status           []core.CharacterID      `json:"status,omitempty"`
	Equipment        []core.CharacterID      `json:"equipment,omitempty"`
	AttackOptions    []core.CharacterID      `json:"attack_options,omitempty"`
	Explain          []explainQuery          `json:"explain,omitempty"`
	ItemsAt          []core.Ref              `json:"items_at,omitempty"`
	Map              []core.CharacterID      `json:"map,omitempty"`
	Locations        []core.LocationID       `json:"locations,omitempty"`
	Quests           []core.QuestID          `json:"quests,omitempty"`
	Journal          *core.JournalFilter     `json:"journal,omitempty"`
	ItemDefinitions  []core.ItemDefinitionID `json:"item_definitions,omitempty"`
	ItemCharges      []core.ItemID           `json:"item_charges,omitempty"`
	IncludeState     bool                    `json:"include_state,omitempty"`
	LiveState        *livestate.Document     `json:"live_state,omitempty"`
	NewLiveState     bool                    `json:"new_live_state,omitempty"`
}

type faultRecord struct {
	Code     string `json:"code"`
	Subject  string `json:"subject,omitempty"`
	Field    string `json:"field,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Message  string `json:"message"`
}

// endedModifier is one modifier an ended Condition instance had in force, with
// a magnitude amount already resolved. It names either a stat or a Resource
// whose maximum the instance had raised. Bounds and roll modes are not
// additive and are not listed; a stat explanation shows those in force.
type endedModifier struct {
	ID              core.ConditionModifierID `json:"id"`
	Stat            core.StatID              `json:"stat,omitempty"`
	ResourceMaximum core.ResourceID          `json:"resource_maximum,omitempty"`
	Amount          int64                    `json:"amount"`
}

// endedCondition summarises one ConditionRemoved event of the receipt, so a
// host can report what ended without walking the events. Reason is anchor,
// clock, expired (Adventure rounds), battle, minimum, replaced, condition (an
// ends_on_condition type arrived), check (a damage-triggered check failed
// outside Battle) or removed (an explicit or Ability removal).
type endedCondition struct {
	Instance        core.ConditionInstanceID  `json:"instance"`
	Type            core.ConditionTypeID      `json:"type"`
	Target          core.CharacterID          `json:"target"`
	Reason          string                    `json:"reason"`
	Deadline        *uint64                   `json:"deadline,omitempty"`
	By              *core.ConditionInstanceID `json:"by,omitempty"`
	MinimumResource *core.ResourceID          `json:"minimum_resource,omitempty"`
	Modifiers       []endedModifier           `json:"modifiers"`
}

// viewError reports a view that failed after a transaction took effect.
type viewError struct {
	View  string       `json:"view"`
	Error string       `json:"error"`
	Fault *faultRecord `json:"fault,omitempty"`
}

type output struct {
	OK              bool                             `json:"ok"`
	Phase           string                           `json:"phase"`
	Revision        core.Revision                    `json:"revision"`
	Checkpoint      *core.Checkpoint                 `json:"checkpoint,omitempty"`
	LiveState       *livestate.Document              `json:"live_state,omitempty"`
	Receipt         *core.Batch                      `json:"receipt,omitempty"`
	Historical      bool                             `json:"historical,omitempty"`
	EndedConditions []endedCondition                 `json:"ended_conditions,omitempty"`
	Checks          []core.StatCheckResult           `json:"checks,omitempty"`
	Attacks         []core.AttackResult              `json:"attacks,omitempty"`
	Saves           []core.SaveResult                `json:"saves,omitempty"`
	Status          []core.CharacterStatusProjection `json:"status,omitempty"`
	Equipment       []core.EquipmentProjection       `json:"equipment,omitempty"`
	AttackOptions   []core.AttackOptionsProjection   `json:"attack_options,omitempty"`
	Explain         []core.StatExplanation           `json:"explain,omitempty"`
	ItemsAt         []core.CustodyProjection         `json:"items_at,omitempty"`
	Map             []core.MapProjection             `json:"map,omitempty"`
	Locations       []core.LocationProjection        `json:"locations,omitempty"`
	Quests          []core.QuestProjection           `json:"quests,omitempty"`
	Journal         *core.JournalProjection          `json:"journal,omitempty"`
	ItemDefinitions []core.ItemDefinitionProjection  `json:"item_definitions,omitempty"`
	ItemCharges     []core.ItemChargesProjection     `json:"item_charges,omitempty"`
	Diagnostics     []compiler.Diagnostic            `json:"diagnostics,omitempty"`
	Error           string                           `json:"error,omitempty"`
	Fault           *faultRecord                     `json:"fault,omitempty"`
	ViewError       *viewError                       `json:"view_error,omitempty"`
}

type sliceReader struct {
	batches []core.Batch
	next    int
}

func (r *sliceReader) Next() (core.Batch, error) {
	if r.next >= len(r.batches) {
		return core.Batch{}, io.EOF
	}
	b := r.batches[r.next]
	r.next++
	return b, nil
}

// Exit codes. A JSON response is written in every case except an encode
// failure.
const (
	exitResponse = 0 // complete response; ok carries the outcome
	exitInput    = 2 // input unreadable or undecodable, or the response could not be encoded
	exitInternal = 3 // the engine panicked; phase internal, nothing to persist
)

// applyFunc is apply, replaceable by tests that need a panic.
var applyFunc = apply

// newWorld is core.NewWorld, replaceable by tests that need a source the
// compiler accepts and genesis refuses.
var newWorld = core.NewWorld

// stderr receives the stack of a recovered panic.
var stderr io.Writer = os.Stderr

func main() {
	os.Exit(run(os.Stdin, os.Stdout))
}

// run returns 0 when a complete response was written, whether or not the
// transaction succeeded; the response's ok field carries the outcome. It returns
// 2 when the input could not be read or decoded and no world was touched, and
// 3 when the engine panicked (the response then has phase internal and no
// checkpoint, so the host persists nothing).
func run(in io.Reader, out io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(in, MaxInputBytes+1))
	if err != nil {
		return emitFatal(out, "input", err)
	}
	if len(raw) > MaxInputBytes {
		return emitFatal(out, "input", fmt.Errorf("input exceeds %d bytes", MaxInputBytes))
	}
	var req input
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return emitFatal(out, "input", err)
	}
	resp, code := safeApply(req)
	e := json.NewEncoder(out)
	e.SetIndent("", " ")
	if err := e.Encode(resp); err != nil {
		return exitInput
	}
	return code
}

// safeApply turns an engine panic into an internal-phase response at the
// process boundary, so a host always receives a JSON body.
func safeApply(req input) (resp output, code int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "%s: panic: %v\n%s", filepath.Base(os.Args[0]), p, debug.Stack())
			resp, code = output{OK: false, Phase: "internal", Error: fmt.Sprintf("internal error: %v", p)}, exitInternal
		}
	}()
	return applyFunc(req), exitResponse
}

func emitFatal(out io.Writer, phase string, err error) int {
	_ = json.NewEncoder(out).Encode(output{OK: false, Phase: phase, Error: err.Error()})
	return exitInput
}

func failure(phase string, err error) output {
	o := output{OK: false, Phase: phase, Error: err.Error()}
	var fault *core.Fault
	if errors.As(err, &fault) && fault != nil {
		o.Fault = &faultRecord{fault.Code, fault.Subject, fault.Field, fault.Expected, fault.Actual, fault.Message}
	}
	return o
}

func apply(req input) output {
	// 1. Construct the world: genesis from source, or reconstruction from checkpoint.
	// With live state, the document's occupants replace the source's seeds at
	// the places it holds, its quests' status and logs replace the source's,
	// and with a map its characters and map state replace the source's.
	var w *core.World
	var live *livestate.Document
	switch {
	case req.World != "" && req.Checkpoint != nil:
		return failure("input", fmt.Errorf("supply either world (genesis) or checkpoint, not both"))
	case req.LiveState != nil && req.NewLiveState:
		return failure("input", fmt.Errorf("supply either live_state or new_live_state, not both"))
	case (req.LiveState != nil || req.NewLiveState) && req.World == "":
		return failure("input", fmt.Errorf("live state goes with a world source, not a checkpoint"))
	case req.World != "":
		if len(req.World) > compiler.MaxWorldSourceBytes {
			return failure("compile", fmt.Errorf("world source exceeds %d bytes", compiler.MaxWorldSourceBytes))
		}
		name := req.WorldName
		if name == "" {
			name = "world.nql"
		}
		b, ds := compiler.CompileWorld(name, req.World)
		if len(ds) > 0 {
			return output{OK: false, Phase: "compile", Diagnostics: ds}
		}
		source := b
		if req.LiveState != nil || req.NewLiveState {
			live = &livestate.Document{}
			if req.LiveState != nil {
				if err := livestate.Check(req.LiveState); err != nil {
					return failure("live_state", err)
				}
				live = req.LiveState
			}
			if err := livestate.Merge(&b, live); err != nil {
				return failure("live_state", err)
			}
		}
		world, err := newWorld(b)
		if err != nil {
			// The source alone decides whether the fault is its own or the
			// document's.
			if live != nil {
				if _, e := newWorld(source); e == nil {
					return failure("live_state", err)
				}
			}
			return failure("genesis", err)
		}
		w = world
	case req.Checkpoint != nil:
		cp := req.Checkpoint
		world, err := core.ReplayArchive(cp.Genesis, cp.Head, len(cp.History), &sliceReader{batches: cp.History})
		if err != nil {
			return failure("replay", err)
		}
		w = world
	default:
		return failure("input", fmt.Errorf("supply world (genesis) or checkpoint"))
	}

	resp := output{OK: true, Phase: "ready"}

	// 2. Optional transaction.
	if req.Actions != "" {
		if len(req.Actions) > compiler.MaxSourceBytes {
			return failure("compile", fmt.Errorf("action source exceeds %d bytes", compiler.MaxSourceBytes))
		}
		if req.Actor == nil || req.Actor.ID == "" {
			return failure("input", fmt.Errorf("actor is required for execution (attribution, not authorization)"))
		}
		if req.Request == "" {
			return failure("input", fmt.Errorf("request id is required for execution"))
		}
		actor := *req.Actor
		if actor.Kind == "" {
			actor.Kind = core.EntityKind("character")
		}
		name := req.ActionsName
		if name == "" {
			name = "actions.nql"
		}
		s := w.Snapshot()
		// A request already committed is compiled as a retry: what its
		// actions created exists now and is not refused as a duplicate.
		compile := compiler.CompileActions
		_, kept := w.Receipt(req.Request)
		if live != nil {
			if _, ok := live.Find(req.Request); ok {
				kept = true
			}
		}
		if kept {
			compile = compiler.CompileRetry
		}
		ops, ds := compile(name, req.Actions, s.Blueprint)
		if len(ds) > 0 {
			resp.Phase = "compile"
			resp.OK = false
			resp.Diagnostics = ds
			resp.Revision = s.Revision
			return resp
		}
		// A request the document keeps has already applied: it is answered
		// as such, at the current state, with no change. The same ID with
		// other content is refused, as the core refuses it.
		var digest string
		if live != nil {
			digest = livestate.Digest(actor, ops)
			if prior, ok := live.Find(req.Request); ok {
				if prior.Digest != digest {
					return failure("execute", &core.Fault{Code: "E_REQUEST_REUSE", Subject: req.Request, Field: "request", Expected: "identical retry", Actual: "different content", Message: "request ID already committed with different content"})
				}
				resp.Phase, resp.Historical = "executed", true
				return finish(resp, req, w, live, "", "")
			}
		}
		expected := s.Revision
		if req.ExpectedRevision != nil {
			expected = *req.ExpectedRevision
		} else if prior, ok := w.Receipt(req.Request); ok {
			// A defaulted revision is not part of a retry's identity: an exact
			// retry against a later checkpoint returns its original receipt,
			// while changed content with the same ID is still refused.
			expected = prior.Request.ExpectedRevision
		}
		before := w.Revision()
		batch, err := w.Execute(core.Request{ID: req.Request, ExpectedRevision: expected, Actor: actor, Operations: ops})
		if err != nil {
			f := failure("execute", err)
			f.Revision = before
			return f
		}
		resp.Phase = "executed"
		resp.Receipt = &batch
		resp.Historical = batch.Revision <= before
		resp.EndedConditions = endedConditions(batch)
		resp.Checks = checks(batch)
		resp.Attacks = attacks(batch)
		resp.Saves = saves(batch)
		if live != nil && livestate.Changes(batch, s.Map != nil, s.Clock != nil) {
			return finish(resp, req, w, live, req.Request, digest)
		}
	}
	return finish(resp, req, w, live, "", "")
}

// finish adds the views, the checkpoint and the next live state. A request
// that changed what the live state holds is kept in it under its ID.
func finish(resp output, req input, w *core.World, live *livestate.Document, id, digest string) output {
	// 3. Read-only views on the resulting world. Without a transaction a
	// failing view fails the response. After a commit or a historical retry it
	// must not hide the transaction: the response stays ok with its receipt
	// and checkpoint, view_error names the failure, and later views are omitted.
	// A retry the live state answers has no receipt and is kept the same way.
	if f := views(&resp, req, w); f != nil {
		if resp.Receipt == nil && !resp.Historical {
			return withRevision(*f, w)
		}
		resp.ViewError = &viewError{View: f.Phase, Error: f.Error, Fault: f.Fault}
	}

	// 4. The checkpoint the host must persist. State is an audit observation and
	// is omitted unless asked for, to keep the document small.
	cp := w.ExportCheckpoint()
	if live != nil {
		next := livestate.Next(*live, cp.State, id, digest)
		resp.LiveState = &next
	}
	if !req.IncludeState {
		cp.State = core.Snapshot{}
	}
	resp.Checkpoint = &cp
	resp.Revision = w.Revision()
	return resp
}

// endedConditions lists the receipt's Condition removals in event order. Each
// removal event carries at most one ending marker; anchored dependents carry
// only the anchor, and their root's event carries the root's own reason.
func endedConditions(b core.Batch) []endedCondition {
	var out []endedCondition
	for _, e := range b.Events {
		c := e.Condition
		if e.Kind != core.ConditionRemoved || c == nil || c.Before == nil {
			continue
		}
		v := endedCondition{Instance: c.Before.ID, Type: c.Before.Type, Target: c.Before.Target, Reason: "removed", Modifiers: []endedModifier{}}
		switch {
		case c.AnchorRemoved != nil:
			v.Reason, v.By = "anchor", c.AnchorRemoved
		case c.ClockExpiration != nil:
			v.Reason, v.Deadline = "clock", &c.ClockExpiration.Deadline
		case c.Expiration != nil:
			v.Reason = "expired"
		case c.BattleEnding != nil:
			v.Reason = "battle"
		case c.MinimumResource != nil:
			v.Reason, v.MinimumResource = "minimum", c.MinimumResource
		case c.ReplacedBy != nil:
			v.Reason, v.By = "replaced", c.ReplacedBy
		case c.EndingCondition != nil:
			v.Reason, v.By = "condition", c.EndingCondition
		case c.CheckFailed:
			v.Reason = "check"
		}
		for _, f := range c.BeforeEffects {
			m := endedModifier{Stat: f.Stat, Amount: f.Amount}
			if f.ConditionOrigin != nil {
				m.ID = f.ConditionOrigin.Modifier
			}
			v.Modifiers = append(v.Modifiers, m)
		}
		for _, r := range c.Resources {
			v.Modifiers = append(v.Modifiers, endedModifier{ID: r.Modifier, ResourceMaximum: r.Resource, Amount: r.Amount})
		}
		sort.SliceStable(v.Modifiers, func(i, j int) bool { return v.Modifiers[i].ID < v.Modifiers[j].ID })
		out = append(out, v)
	}
	return out
}

// checks lists the receipt's resolved StatChecks in operation order, so a host
// can report totals and outcomes without walking the events.
func checks(b core.Batch) []core.StatCheckResult {
	var out []core.StatCheckResult
	for _, e := range b.Events {
		if e.Kind == core.StatCheckResolved && e.StatCheck != nil {
			out = append(out, *e.StatCheck)
		}
	}
	return out
}

// attacks lists the receipt's scored attacks in operation order, so a host can
// report hits and damage without walking the events.
func attacks(b core.Batch) []core.AttackResult {
	var out []core.AttackResult
	for _, e := range b.Events {
		if e.Kind == core.AttackScored && e.Attack != nil {
			out = append(out, *e.Attack)
		}
	}
	return out
}

// saves lists the receipt's scored saves in operation order, so a host can
// report verdicts and damage without walking the events.
func saves(b core.Batch) []core.SaveResult {
	var out []core.SaveResult
	for _, e := range b.Events {
		if e.Kind == core.SaveScored && e.Save != nil {
			out = append(out, *e.Save)
		}
	}
	return out
}

// views fills the requested read-only views in order and returns the first
// failure, if any.
func views(resp *output, req input, w *core.World) *output {
	for _, id := range req.Status {
		p, err := w.CharacterStatus(id)
		if err != nil {
			f := failure("status", err)
			return &f
		}
		resp.Status = append(resp.Status, p)
	}
	for _, id := range req.Equipment {
		p, err := w.EquipmentView(id)
		if err != nil {
			f := failure("equipment", err)
			return &f
		}
		resp.Equipment = append(resp.Equipment, p)
	}
	for _, id := range req.AttackOptions {
		p, err := w.AttackOptionsView(id)
		if err != nil {
			f := failure("attack_options", err)
			return &f
		}
		resp.AttackOptions = append(resp.AttackOptions, p)
	}
	for _, q := range req.Explain {
		p, err := w.Explain(q.Character, q.Stat)
		if err != nil {
			f := failure("explain", err)
			return &f
		}
		resp.Explain = append(resp.Explain, p)
	}
	for _, at := range req.ItemsAt {
		p, err := w.ItemsAt(at)
		if err != nil {
			f := failure("items_at", err)
			return &f
		}
		resp.ItemsAt = append(resp.ItemsAt, p)
	}
	for _, id := range req.Map {
		p, err := w.MapView(id)
		if err != nil {
			f := failure("map", err)
			return &f
		}
		resp.Map = append(resp.Map, p)
	}
	if len(req.Locations) > 0 {
		ps, err := w.LocationsView(req.Locations)
		resp.Locations = append(resp.Locations, ps...)
		if err != nil {
			f := failure("locations", err)
			return &f
		}
	}
	if len(req.Quests) > 0 {
		ps, err := w.QuestsView(req.Quests)
		resp.Quests = append(resp.Quests, ps...)
		if err != nil {
			f := failure("quests", err)
			return &f
		}
	}
	if req.Journal != nil {
		p, err := w.JournalView(*req.Journal)
		if err != nil {
			f := failure("journal", err)
			return &f
		}
		resp.Journal = &p
	}
	if len(req.ItemDefinitions) > 0 {
		ps, err := w.ItemDefinitionsView(req.ItemDefinitions)
		resp.ItemDefinitions = append(resp.ItemDefinitions, ps...)
		if err != nil {
			f := failure("item_definitions", err)
			return &f
		}
	}
	if len(req.ItemCharges) > 0 {
		ps, err := w.ItemChargesView(req.ItemCharges)
		resp.ItemCharges = append(resp.ItemCharges, ps...)
		if err != nil {
			f := failure("item_charges", err)
			return &f
		}
	}
	return nil
}

func withRevision(o output, w *core.World) output {
	o.Revision = w.Revision()
	return o
}
