package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const m8Spend = `spend "char:orin" resource "focus" by 2;`
const m8Aid = `invoke "ability:steady-aid" { "payer" = character "char:orin"; "target" = character "char:lyra"; "guard_id" = condition_id "guard:m7:one"; };`
const m8Pay = `invoke "ability:pay" { "payer" = character "char:orin"; "amount" = int 2; };`

func m8Source(t *testing.T, focus int64, extra string) string {
	t.Helper()
	b, err := os.ReadFile("../../examples/m7/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(string(b), `resource "focus" = 3 min 0 max 5;`, fmt.Sprintf(`resource "focus" = %d min 0 max 5;`, focus), 1) + "\n" + extra
}
func m8World(t *testing.T, source string) (core.Blueprint, *core.World) {
	t.Helper()
	b, ds := compiler.CompileWorld("fixture.nql", source)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	w, err := core.NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	return b, w
}
func m8Request(t *testing.T, w *core.World, id, source string) core.Request {
	t.Helper()
	s := w.Snapshot()
	ops, ds := compiler.CompileActions("action.nql", source, s.Blueprint)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	return core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, Operations: ops}
}
func m8Bytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func m8Equal(t *testing.T, want []byte, got any) {
	t.Helper()
	if b := m8Bytes(t, got); !bytes.Equal(want, b) {
		t.Fatalf("expected %s\ngot %s", want, b)
	}
}
func m8Reject(t *testing.T, w *core.World, r core.Request) error {
	t.Helper()
	s, h := m8Bytes(t, w.Snapshot()), m8Bytes(t, w.History())
	got, err := w.Execute(r)
	if err == nil || !reflect.DeepEqual(got, core.Batch{}) {
		t.Fatal("expected rejected attempt", got, err)
	}
	m8Equal(t, s, w.Snapshot())
	m8Equal(t, h, w.History())
	return err
}
func m8Insufficient(actual string) core.Fault {
	return core.Fault{Code: "E_INSUFFICIENT_RESOURCE", Subject: "char:orin", Field: "focus", Expected: "current - requested >= minimum 0", Actual: actual, Message: "the full resource amount cannot be paid"}
}
func m8File(t *testing.T, source string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fixture.nql")
	if err := os.WriteFile(p, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

type m8FailedWriter struct {
	bytes.Buffer
	calls int
}

func (w *m8FailedWriter) Write(b []byte) (int, error) {
	w.calls++
	_, _ = w.Buffer.Write(b)
	return 0, errors.New("sink unavailable")
}

func TestM8CharacterizeRuntimeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		focus                       int64
		source                      string
		step                        int
		ability, operand, parameter string
	}{
		{"direct", 1, m8Spend, 0, "", "", ""},
		{"aid", 1, m8Aid, 0, "ability:steady-aid", "payer", "payer"},
		{"candidate", 3, m8Spend + m8Spend, 0, "", "", ""},
		{"second invocation", 3, m8Pay + m8Pay, 0, "ability:pay", "payer", "payer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := m8World(t, m8Source(t, tc.focus, ""))
			err := m8Reject(t, w, m8Request(t, w, "failed", tc.source))
			var f *core.Fault
			var a *core.AbilityError
			want := m8Insufficient("current=1 minimum=0 requested=2")
			if !errors.As(err, &f) || !reflect.DeepEqual(*f, want) {
				t.Fatal("actual Fault differs", err, f)
			}
			if found := errors.As(err, &a); found != (tc.ability != "") {
				t.Fatal("wrapper presence", err)
			}
			if a != nil && (string(a.Definition) != tc.ability || a.Step != tc.step || a.Operand != tc.operand || string(a.Parameter) != tc.parameter) {
				t.Fatal(a)
			}
			p, e := w.CharacterStatus("char:orin")
			if e != nil || p.Resources["focus"].Current != tc.focus || p.Revision != 0 {
				t.Fatal("candidate is not snapshot", p, e)
			}
		})
	}
	_, w := m8World(t, m8Source(t, 3, ""))
	r := core.Request{ID: "binding", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, Operations: []core.Operation{{Invoke: &core.InvokeAbility{Ability: "ability:pay"}}}}
	err := m8Reject(t, w, r)
	var a *core.AbilityError
	var f *core.Fault
	if !errors.As(err, &a) || !errors.As(err, &f) || a.Step != -1 || a.Operand != "argument" || a.Parameter != "payer" || f.Code != "E_ARGUMENT" || f.Subject != "" || f.Expected != "required binding" || f.Actual != "missing" {
		t.Fatal(err)
	}
}

func TestM8CharacterizeLegacyRouting(t *testing.T) {
	var out, errs bytes.Buffer
	args := []string{"-world", m8File(t, m8Source(t, 1, "")), "-execute", m8File(t, m8Spend), "-actor", "char:tess"}
	if exit := run(args, strings.NewReader(""), &out, &errs); exit != 1 || out.Len() != 0 {
		t.Fatal(exit, out.String(), errs.String())
	}
	want := "{\n  \"error\": \"E_INSUFFICIENT_RESOURCE: the full resource amount cannot be paid\"\n}\n"
	if errs.String() != want {
		t.Fatal("legacy bytes changed", errs.String())
	}
	out.Reset()
	errs.Reset()
	args[3] = m8File(t, `invoke "ability:pay" {};`)
	if exit := run(args, strings.NewReader(""), &out, &errs); exit != 1 || !strings.Contains(errs.String(), `"diagnostics"`) || strings.Contains(errs.String(), `runtime_diagnostic`) {
		t.Fatal(exit, errs.String())
	}
	args[1] = "../../examples/m7/world.nql"
	args[3] = "../../examples/m7/steady-aid.nql"
	errs.Reset()
	writer := &m8FailedWriter{}
	if exit := run(args, strings.NewReader(""), writer, &errs); exit != 1 || writer.calls != 1 || errs.String() != "{\n  \"error\": \"sink unavailable\"\n}\n" {
		t.Fatal(exit, writer.calls, errs.String())
	}
	var attempted output
	if err := json.Unmarshal(writer.Bytes(), &attempted); err != nil || attempted.Revision != 1 || attempted.Result == nil || len(attempted.Result.Events) != 4 {
		t.Fatal("must reach writer after successful Execute", attempted, err)
	}
}

func TestM8RuntimeResponseMappingAndDetachment(t *testing.T) {
	f := &core.Fault{Code: "E_FUTURE", Subject: "quoted \"\\ é{}", Field: "", Expected: "line1\nline2", Actual: "-9223372036854775808", Message: "a sentence"}
	a := &core.AbilityError{Definition: "ability:é{}", Step: 0, Operand: "payer", Parameter: "", Err: f}
	err := fmt.Errorf("outer: %w", a)
	left, right := runtimeErrorResponse(err, true), runtimeErrorResponse(err, true)
	var got map[string]any
	if e := json.Unmarshal(m8Bytes(t, left), &got); e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"error": err.Error(), "runtime_diagnostic": map[string]any{
		"version": float64(1), "phase": "execute", "execution": map[string]any{"attempt_committed": false, "detail_scope": "evaluation_not_snapshot"},
		"fault":   map[string]any{"code": "E_FUTURE", "subject": "quoted \"\\ é{}", "field": "", "expected": "line1\nline2", "actual": "-9223372036854775808", "message": "a sentence"},
		"ability": map[string]any{"definition": "ability:é{}", "step": float64(0), "operand": "payer", "parameter": ""},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("full field inventory/mapping", got)
	}
	original, sibling := m8Bytes(t, a), m8Bytes(t, right)
	left.RuntimeDiagnostic.Fault.Actual = "changed"
	left.RuntimeDiagnostic.Ability.Step = 99
	left.RuntimeDiagnostic.Execution.AttemptCommitted = true
	m8Equal(t, original, a)
	m8Equal(t, sibling, right)
	f.Actual = "input changed"
	a.Step = -1
	a.Parameter = "input changed"
	m8Equal(t, sibling, right)
	for _, e := range []error{errors.New("E_INSUFFICIENT_RESOURCE is only prose"), fmt.Errorf("outer: %w", errors.New("ordinary"))} {
		r := runtimeErrorResponse(e, true)
		if r.Error != e.Error() || r.RuntimeDiagnostic.Fault != nil || r.RuntimeDiagnostic.Ability != nil {
			t.Fatal("invented typed error", r)
		}
		if !strings.Contains(string(m8Bytes(t, r)), `"fault":null,"ability":null`) {
			t.Fatal("absent detail must be null")
		}
	}
	contextOnly := runtimeErrorResponse(&core.AbilityError{Definition: "context", Step: -1, Err: errors.New("untyped")}, true)
	if contextOnly.RuntimeDiagnostic.Fault != nil || contextOnly.RuntimeDiagnostic.Ability.Step != -1 || contextOnly.RuntimeDiagnostic.Ability.Parameter != "" {
		t.Fatal(contextOnly)
	}
	empty := runtimeErrorResponse(&core.Fault{}, true)
	if string(m8Bytes(t, empty.RuntimeDiagnostic.Fault)) != `{"code":"","subject":"","field":"","expected":"","actual":"","message":""}` {
		t.Fatal("empty keys omitted")
	}
	for _, structured := range []bool{false, true} {
		if runtimeErrorResponse(nil, structured) != nil {
			t.Fatal("nil produced failure")
		}
	}
	legacy := runtimeErrorResponse(errors.New("exact outer text"), false)
	if string(m8Bytes(t, legacy)) != `{"error":"exact outer text"}` {
		t.Fatal("legacy additions", legacy)
	}
}

func TestM8RuntimeCandidateAndAbilityContext(t *testing.T) {
	double, err := os.ReadFile("../../examples/m8/two-spends.nql")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                        string
		focus                       int64
		extra, action               string
		step                        int
		ability, operand, parameter string
		fault                       core.Fault
	}{
		{"direct", 1, "", m8Spend, 0, "", "", "", m8Insufficient("current=1 minimum=0 requested=2")},
		{"aid", 1, "", m8Aid, 0, "ability:steady-aid", "payer", "payer", m8Insufficient("current=1 minimum=0 requested=2")},
		{"candidate", 3, "", m8Spend + m8Spend, 0, "", "", "", m8Insufficient("current=1 minimum=0 requested=2")},
		{"double body", 3, string(double), `invoke "ability:double-pay" { "payer" = character "char:orin"; };`, 1, "ability:double-pay", "payer", "payer", m8Insufficient("current=1 minimum=0 requested=2")},
		{"second invocation", 3, "", m8Pay + m8Pay, 0, "ability:pay", "payer", "payer", m8Insufficient("current=1 minimum=0 requested=2")},
		{"literal", 1, `ability "literal" named "Literal" { spend "char:orin" resource "focus" by 2; }`, `invoke "literal" {};`, 0, "literal", "payer", "", m8Insufficient("current=1 minimum=0 requested=2")},
		{"late condition", 3, `condition "guard:m8:existing" of "condition:guarded" to "char:lyra";`, m8Aid, 2, "ability:steady-aid", "target", "target", core.Fault{Code: "E_RULE", Subject: "guard:m7:one", Field: "instances", Expected: "one instance per type/target", Actual: "multiple", Message: "invalid condition instances"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, w := m8World(t, m8Source(t, tc.focus, tc.extra))
			r := m8Request(t, w, "failed", tc.action)
			e := m8Reject(t, w, r)
			s, h := m8Bytes(t, w.Snapshot()), m8Bytes(t, w.History())
			actual := runtimeErrorResponse(e, true)
			d := actual.RuntimeDiagnostic
			f := d.Fault
			if actual.Error != e.Error() || d.Version != 1 || d.Phase != "execute" || d.Execution.AttemptCommitted || d.Execution.DetailScope != "evaluation_not_snapshot" {
				t.Fatal(actual)
			}
			if f == nil || !reflect.DeepEqual(core.Fault{Code: f.Code, Subject: f.Subject, Field: f.Field, Expected: f.Expected, Actual: f.Actual, Message: f.Message}, tc.fault) {
				t.Fatal("fixed Fault", f)
			}
			if (d.Ability != nil) != (tc.ability != "") {
				t.Fatal("context presence")
			}
			if d.Ability != nil && !reflect.DeepEqual(*d.Ability, abilityDiagnostic{tc.ability, tc.step, tc.operand, tc.parameter}) {
				t.Fatal(d.Ability)
			}
			p, _ := w.CharacterStatus("char:orin")
			if p.Resources["focus"].Current != tc.focus || w.Revision() != 0 {
				t.Fatal(p)
			}
			d.Fault.Actual = "mutated response"
			m8Equal(t, s, w.Snapshot())
			m8Equal(t, h, w.History())
		})
	}
	_, w := m8World(t, m8Source(t, 3, ""))
	missing := core.Request{ID: "missing", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, Operations: []core.Operation{{Invoke: &core.InvokeAbility{Ability: "ability:pay"}}}}
	for _, wrongKind := range []bool{false, true} {
		r := missing
		if wrongKind {
			id := core.ItemID("item:token")
			r.Operations = []core.Operation{{Invoke: &core.InvokeAbility{Ability: "ability:pay", Arguments: []core.AbilityArgument{{Name: "payer", Value: core.AbilityValue{Item: &id}}}}}}
		}
		d := runtimeErrorResponse(m8Reject(t, w, r), true).RuntimeDiagnostic
		if d.Ability.Step != -1 || d.Ability.Operand != "argument" || d.Ability.Parameter != "payer" || d.Fault.Subject != "" {
			t.Fatal(d)
		}
		if wrongKind {
			if d.Fault.Code != "E_TYPE" || d.Fault.Expected != "character" || d.Fault.Actual != "item" {
				t.Fatal(d.Fault)
			}
		} else if d.Fault.Code != "E_ARGUMENT" || d.Fault.Expected != "required binding" || d.Fault.Actual != "missing" {
			t.Fatal(d.Fault)
		}
	}
}

func TestM8RuntimeHistoricalSuccessIsolation(t *testing.T) {
	b, w := m8World(t, m8Source(t, 3, ""))
	actions := []string{m8Aid, `remove condition "guard:m7:one" from "char:lyra";`, `heal "char:orin" resource "focus" by 3;`, strings.Replace(m8Aid, "guard:m7:one", "guard:m7:two", 1), m8Spend, `remove condition "guard:m7:two" from "char:lyra";`}
	focus, hp, defense, counts := []int64{1, 1, 4, 2, 0, 0}, []int64{10, 10, 10, 14, 14, 14}, []int64{13, 10, 10, 13, 13, 10}, []int{4, 1, 1, 4, 1, 1}
	for i, source := range actions {
		got, e := w.Execute(m8Request(t, w, fmt.Sprintf("R%d", i+1), source))
		if e != nil {
			t.Fatal(e)
		}
		p, _ := w.CharacterStatus("char:orin")
		target, _ := w.CharacterStatus("char:lyra")
		if p.Resources["focus"].Current != focus[i] || target.Resources["hp"].Current != hp[i] || target.Stats[0].Effective != defense[i] || got.Revision != core.Revision(i+1) || len(got.Events) != counts[i] {
			t.Fatal("journey oracle", i, got, p, target)
		}
	}
	rebuilt, e := core.Replay(b, w.History())
	if e != nil {
		t.Fatal(e)
	}
	m8Equal(t, m8Bytes(t, w.Snapshot()), rebuilt.Snapshot())
	m8Equal(t, m8Bytes(t, w.History()), rebuilt.History())
	for _, live := range []*core.World{w, rebuilt} {
		s, h := m8Bytes(t, live.Snapshot()), m8Bytes(t, live.History())
		if !reflect.DeepEqual(live.Snapshot().RetiredConditionIDs, []core.ConditionInstanceID{"guard:m7:one", "guard:m7:two"}) {
			t.Fatal("retired IDs")
		}
		for _, receipt := range live.History() {
			for _, mode := range []bool{false, true} {
				got, err := live.Execute(receipt.Request)
				if err != nil || runtimeErrorResponse(err, mode) != nil {
					t.Fatal("historical success reclassified", err)
				}
				m8Equal(t, m8Bytes(t, receipt), got)
				var outputBytes bytes.Buffer
				if err := emit(&outputBytes, output{Revision: got.Revision, Result: &got}); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(outputBytes.String(), "runtime_diagnostic") {
					t.Fatal("success contamination")
				}
			}
		}
		fresh := m8Request(t, live, "fresh", m8Spend)
		fresh.ExpectedRevision = 0
		d := runtimeErrorResponse(m8Reject(t, live, fresh), true).RuntimeDiagnostic
		if d.Fault.Code != "E_STALE" || d.Fault.Expected != "6" || d.Fault.Actual != "0" || d.Ability != nil {
			t.Fatal(d)
		}
		fresh.ExpectedRevision = 6
		d = runtimeErrorResponse(m8Reject(t, live, fresh), true).RuntimeDiagnostic
		if d.Fault.Code != "E_INSUFFICIENT_RESOURCE" || d.Fault.Actual != "current=0 minimum=0 requested=2" {
			t.Fatal(d)
		}
		changed := live.History()[0].Request
		changed.Operations = m8Request(t, live, "unused", m8Spend).Operations
		d = runtimeErrorResponse(m8Reject(t, live, changed), true).RuntimeDiagnostic
		if d.Fault.Code != "E_REQUEST_REUSE" || d.Fault.Subject != "R1" || d.Fault.Expected != "identical retry" || d.Fault.Actual != "different content" || d.Execution.AttemptCommitted {
			t.Fatal(d)
		}
		m8Equal(t, s, live.Snapshot())
		m8Equal(t, h, live.History())
	}
}

func TestM8CLIModesAndContinuation(t *testing.T) {
	input, err := os.ReadFile("../../examples/m8/continuation.repl")
	if err != nil {
		t.Fatal(err)
	}
	var baselineOut, baselineErr []byte
	for _, mode := range []string{"", "legacy", "structured"} {
		args := []string{"-world", "../../examples/m7/world.nql", "-repl", "-actor", "char:tess"}
		if mode != "" {
			args = append(args, "--runtime-errors="+mode)
		}
		var out, errs bytes.Buffer
		if code := run(args, bytes.NewReader(input), &out, &errs); code != 0 {
			t.Fatal(code, errs.String())
		}
		if mode == "" {
			baselineOut = append([]byte{}, out.Bytes()...)
			baselineErr = append([]byte{}, errs.Bytes()...)
		} else if !bytes.Equal(out.Bytes(), baselineOut) || (mode == "legacy" && !bytes.Equal(errs.Bytes(), baselineErr)) {
			t.Fatal("mode altered old progression")
		}
		decoder := json.NewDecoder(&errs)
		var runtime map[string]json.RawMessage
		var diagnostic map[string]json.RawMessage
		if e := decoder.Decode(&runtime); e != nil {
			t.Fatal(e)
		}
		if e := decoder.Decode(&diagnostic); e != nil {
			t.Fatal(e)
		}
		if _, ok := diagnostic["diagnostics"]; !ok || len(diagnostic) != 1 {
			t.Fatal("compiler routing", diagnostic)
		}
		if mode == "structured" {
			var d runtimeDiagnostic
			if e := json.Unmarshal(runtime["runtime_diagnostic"], &d); e != nil || d.Fault.Actual != "current=3 minimum=0 requested=99" || d.Ability != nil {
				t.Fatal(d, e)
			}
		} else if len(runtime) != 1 {
			t.Fatal("legacy extra fields")
		}
		d := json.NewDecoder(&out)
		var receipt, inspect output
		if e := d.Decode(&receipt); e != nil {
			t.Fatal(e)
		}
		if e := d.Decode(&inspect); e != nil {
			t.Fatal(e)
		}
		if receipt.Revision != 1 || receipt.Result.Request.ID != "cli:1:3" || inspect.World.Revision != 1 || len(inspect.World.Conditions) != 1 {
			t.Fatal("only valid action commits", receipt, inspect)
		}
	}
}

func TestM8CLIPhaseAndDelivery(t *testing.T) {
	for _, mode := range []string{"legacy", "structured"} {
		for _, args := range [][]string{
			{"-world", "../../examples/m7/world.nql", "-status", "missing"},
			{"-world", "../../examples/m7/world.nql", "-execute", "../../examples/m7/steady-aid.nql", "-actor", "char:tess", "-status", "missing"},
			{"-world", ".missing-world"},
		} {
			var out, errs bytes.Buffer
			code := run(append(args, "--runtime-errors="+mode), strings.NewReader(""), &out, &errs)
			if code != 1 || strings.Contains(errs.String(), "runtime_diagnostic") {
				t.Fatal("non-execute path relabeled", code, errs.String())
			}
		}
		for _, repl := range []bool{false, true} {
			args := []string{"-world", "../../examples/m7/world.nql", "-actor", "char:tess", "--runtime-errors=" + mode}
			input := ""
			if repl {
				args = append(args, "-repl")
				input = m8Aid + "\n:quit\n"
			} else {
				args = append(args, "-execute", "../../examples/m7/steady-aid.nql")
			}
			w := &m8FailedWriter{}
			var errs bytes.Buffer
			if code := run(args, strings.NewReader(input), w, &errs); code != 1 || w.calls != 1 || errs.String() != "{\n  \"error\": \"sink unavailable\"\n}\n" {
				t.Fatal("delivery mislabeled/retried", code, w.calls, errs.String())
			}
			var attempted output
			if e := json.Unmarshal(w.Bytes(), &attempted); e != nil || attempted.Revision != 1 || attempted.Result == nil || len(attempted.Result.Events) != 4 {
				t.Fatal(attempted, e)
			}
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{"--runtime-errors=bogus", "-world", ".must-not-open"}, strings.NewReader(""), &out, &errs); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "Usage of nql:") || strings.Contains(errs.String(), ".must-not-open") || strings.Contains(errs.String(), "runtime_diagnostic") {
		t.Fatal("invalid option not usage/pre-load", code, errs.String())
	}
}
