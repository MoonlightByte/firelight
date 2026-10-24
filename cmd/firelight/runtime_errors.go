package main

import (
	"errors"
	"github.com/MoonlightByte/firelight/core"
)

// These records belong only to CLI presentation, never semantic identity.
type runtimeErrorRecord struct {
	Error             string             `json:"error"`
	RuntimeDiagnostic *runtimeDiagnostic `json:"runtime_diagnostic,omitempty"`
}
type runtimeDiagnostic struct {
	AbilityPath []int               `json:"ability_path,omitempty"`
	Version     int                 `json:"version"`
	Phase       string              `json:"phase"`
	Execution   executionDiagnostic `json:"execution"`
	Fault       *faultDiagnostic    `json:"fault"`
	Ability     *abilityDiagnostic  `json:"ability"`
}
type executionDiagnostic struct {
	AttemptCommitted bool   `json:"attempt_committed"`
	DetailScope      string `json:"detail_scope"`
}
type faultDiagnostic struct {
	Code     string `json:"code"`
	Subject  string `json:"subject"`
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}
type abilityDiagnostic struct {
	Definition string `json:"definition"`
	Step       int    `json:"step"`
	Operand    string `json:"operand"`
	Parameter  string `json:"parameter"`
}

// The caller must supply an Execute error, not a compiler or delivery error.
// No World is needed: even a current-looking Actual describes this evaluation.
func runtimeErrorResponse(err error, structured bool) *runtimeErrorRecord {
	if err == nil {
		return nil
	}
	out := &runtimeErrorRecord{Error: err.Error()}
	if !structured {
		return out
	}
	d := &runtimeDiagnostic{Version: 1, Phase: "execute", Execution: executionDiagnostic{DetailScope: "evaluation_not_snapshot"}}
	var fault *core.Fault
	if errors.As(err, &fault) && fault != nil {
		d.Fault = &faultDiagnostic{fault.Code, fault.Subject, fault.Field, fault.Expected, fault.Actual, fault.Message}
	}
	var ability *core.AbilityError
	if errors.As(err, &ability) && ability != nil {
		d.AbilityPath = append([]int(nil), ability.Path...)
		d.Ability = &abilityDiagnostic{Definition: string(ability.Definition), Step: ability.Step, Operand: ability.Operand, Parameter: string(ability.Parameter)}
	}
	out.RuntimeDiagnostic = d
	return out
}
