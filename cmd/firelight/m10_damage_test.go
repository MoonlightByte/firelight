package main

import (
	"bytes"
	"encoding/json"
	"github.com/MoonlightByte/firelight/core"
	"os"
	"strings"
	"testing"
)

func TestM10DamageCLIExecuteAndDeliveryBoundary(t *testing.T) {
	for _, mode := range []string{"legacy", "structured"} {
		args := []string{"-world", "../../examples/m10/world.nql", "-execute", "../../examples/m10/focused-strike.nql", "-actor", "char:tess", "-request", "m10:R1", "--runtime-errors=" + mode}
		var errOut bytes.Buffer
		writer := &m8FailedWriter{}
		if code := run(args, strings.NewReader(""), writer, &errOut); code != 1 || writer.calls != 1 || errOut.String() != "{\n  \"error\": \"sink unavailable\"\n}\n" {
			t.Fatal("postcommit delivery mislabeled", code, errOut.String())
		}
		var actual output
		if err := json.Unmarshal(writer.Bytes(), &actual); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile("testdata/m10-expected-first-batch.json")
		if err != nil {
			t.Fatal(err)
		}
		var want core.Batch
		if err = json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if actual.Result == nil || actual.Revision != 1 {
			t.Fatal(actual)
		}
		m8Equal(t, m8Bytes(t, want), *actual.Result)
	}
}
func TestM10DamageFaultFormatterAndHistoricalSuccess(t *testing.T) {
	src, err := os.ReadFile("../../examples/m10/world.nql")
	if err != nil {
		t.Fatal(err)
	}
	// Genuine typed binding failure may be formatted by a host; normal CLI source
	// compilation rejects this before Execute, which the real CLI captures check.
	_, w := m8World(t, string(src))
	r := core.Request{ID: "typed", Actor: core.Ref{Kind: core.CharacterKind, ID: "char:tess"}, Operations: []core.Operation{{Invoke: &core.InvokeAbility{Ability: "ability:focused-strike"}}}}
	failure := m8Reject(t, w, r)
	result := runtimeErrorResponse(failure, true)
	want := &runtimeErrorRecord{Error: failure.Error(), RuntimeDiagnostic: &runtimeDiagnostic{Version: 1, Phase: "execute", Execution: executionDiagnostic{DetailScope: "evaluation_not_snapshot"}, Fault: &faultDiagnostic{Code: "E_ARGUMENT", Subject: "", Field: "argument", Expected: "required binding", Actual: "missing", Message: "invalid ability argument"}, Ability: &abilityDiagnostic{Definition: "ability:focused-strike", Step: -1, Operand: "argument", Parameter: "payer"}}}
	m8Equal(t, m8Bytes(t, want), result)
	action, err := os.ReadFile("../../examples/m10/focused-strike.nql")
	if err != nil {
		t.Fatal(err)
	}
	original := m8Request(t, w, "original", string(action))
	receipt, err := w.Execute(original)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Execute(m8Request(t, w, "empty", `spend "char:orin" resource "focus" by 1; damage "char:lyra" resource "hp" by 2;`))
	if err != nil {
		t.Fatal(err)
	}
	lateState, lateHistory := m8Bytes(t, w.Snapshot()), m8Bytes(t, w.History())
	retry, err := w.Execute(original)
	if err != nil || runtimeErrorResponse(err, true) != nil {
		t.Fatal("successful retry formatted as rejection", err)
	}
	m8Equal(t, m8Bytes(t, receipt), retry)
	m8Equal(t, lateState, w.Snapshot())
	m8Equal(t, lateHistory, w.History())
}
