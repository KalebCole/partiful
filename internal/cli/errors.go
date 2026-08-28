package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/KalebCole/partiful/internal/domain"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type      domain.ErrorType `json:"type"`
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	Retryable bool             `json:"retryable"`
	Details   map[string]any   `json:"details"`
}

func (adapter *CLI) writeError(err error, jsonOutput bool) int {
	var classified *domain.Error
	if !errors.As(err, &classified) || classified == nil {
		classified = &domain.Error{
			Type: domain.ErrorInternalFailure, Code: "INTERNAL_FAILURE", Message: "the operation failed",
		}
	}
	if classified.Type == domain.ErrorInternalFailure {
		classified = &domain.Error{
			Type: domain.ErrorInternalFailure, Code: "INTERNAL_FAILURE", Message: "the operation failed",
		}
	}
	if jsonOutput {
		details := make(map[string]any)
		if classified.Details.Remediation != "" {
			details["remediation"] = classified.Details.Remediation
		}
		if classified.Details.BackendKind != "" {
			details["backend_kind"] = classified.Details.BackendKind
		}
		if classified.Details.State != "" {
			details["state"] = classified.Details.State
		}
		if len(classified.Details.Candidates) > 0 {
			details["candidates"] = publicValueOf(classified.Details.Candidates)
		}
		encoded, marshalErr := json.Marshal(errorEnvelope{Error: errorBody{
			Type: classified.Type, Code: classified.Code, Message: classified.Message,
			Retryable: classified.Retryable, Details: details,
		}})
		if marshalErr == nil {
			fmt.Fprintln(adapter.stderr, string(encoded))
		} else {
			fmt.Fprintln(adapter.stderr, "internal.failure: the operation failed")
		}
	} else {
		fmt.Fprintf(adapter.stderr, "%s: %s\n", classified.Type, classified.Message)
	}
	return int(classified.Type.ExitClass())
}

func publicValueOf(value any) any {
	return publicValue(reflect.ValueOf(value))
}
