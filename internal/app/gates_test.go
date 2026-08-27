package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

func TestNewGateManifestRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	valid := []Gate{
		{Identity: "OP11-ENDPOINT-ERRORS:getGuests", State: GateOpenClaim, Source: "/operations/getGuests/errors"},
		{Identity: "OP11-MUTATION-OUTCOME:createEvent", State: GateOpenClaim, Source: "/operations/createEvent/mutationOutcome"},
	}
	if _, err := NewGateManifest(valid); err != nil {
		t.Fatalf("NewGateManifest(valid) error = %v", err)
	}

	cases := []struct {
		name    string
		entries []Gate
		want    string
	}{
		{name: "missing", entries: []Gate{{State: GateOpenClaim, Source: "/source"}}, want: "missing full identity"},
		{name: "partial endpoint identity", entries: []Gate{{Identity: "OP11-ENDPOINT-ERRORS", State: GateOpenClaim, Source: "/source"}}, want: "missing full identity"},
		{name: "duplicate", entries: append(valid, valid[0]), want: "duplicate full identity"},
		{name: "whitespace identity", entries: []Gate{{Identity: " OP11-ENDPOINT-ERRORS:getGuests", State: GateOpenClaim, Source: "/source"}}, want: "non-canonical identity"},
		{name: "unknown operation", entries: []Gate{{Identity: "OP11-ENDPOINT-ERRORS:notInTheContract", State: GateOpenClaim, Source: "/source"}}, want: "unknown operation"},
		{name: "unknown state", entries: []Gate{{Identity: "OP11-ENDPOINT-ERRORS:getGuests", State: GateState("MAYBE"), Source: "/source"}}, want: "invalid state"},
		{name: "empty source", entries: []Gate{{Identity: "OP11-ENDPOINT-ERRORS:getGuests", State: GateOpenClaim, Source: "  "}}, want: "empty source"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewGateManifest(test.entries)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewGateManifest() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDefaultGateManifestIsCompleteAndExplicit(t *testing.T) {
	t.Parallel()

	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatalf("DefaultGateManifest() error = %v", err)
	}
	entries := manifest.Entries()
	if len(entries) != 61 {
		t.Fatalf("gate count = %d, want 61", len(entries))
	}

	wantStates := map[string]GateState{
		"OP11-EVENT-LIST-REQUEST":                  GateOpenOperation,
		"OP11-AUTH-REQUESTS:refreshToken":          GateOpenPath,
		"OP11-ENDPOINT-ERRORS:getGuests":           GateOpenClaim,
		"OP11-MUTATION-OUTCOME:createEvent":        GateOpenClaim,
		"OP11-CURRENT-GUEST-VARIANT":               GateOpenClaim,
		"OP11-RSVP-SPECIAL-PATHS":                  GateOpenOperation,
		"OP11-COHOST-STATE-READ":                   GateOpenClaim,
		"OP11-BLAST-FIRESTORE-READS":               GateOpenOperation,
		"OP11-POSTER-DUPLICATE-ID":                 GateOpenPath,
		"OP11-UPLOAD-PHOTO":                        GateDormant,
		"OP15-EVENT-DETAIL-PROJECTION:address":     GateOpenOperation,
		"OP15-EVENT-DETAIL-PROJECTION:guest_limit": GateOpenOperation,
		"COLLECTION-GUEST-PAGE-21":                 GateOpenPath,
	}
	for identity, want := range wantStates {
		gate, ok := manifest.Lookup(identity)
		if !ok || gate.State != want || gate.Source == "" {
			t.Fatalf("gate %q = %#v, found %t; want state %q and source", identity, gate, ok, want)
		}
	}

	endpointOperations := openAPIOperationIDs(t)
	mutationOperations := []string{
		"createEvent", "cancelEvent", "firestorePatchEvent", "addInvitedGuestsAsHost", "addGuest", "markEventInterest",
		"createCohostRequest", "deleteCohostRequest", "removeCohost", "generateEventCohostLink", "revokeEventCohostLink", "createTextBlast",
	}

	gotEndpoints := identitiesWithPrefix(entries, "OP11-ENDPOINT-ERRORS:")
	gotMutations := identitiesWithPrefix(entries, "OP11-MUTATION-OUTCOME:")
	for index := range endpointOperations {
		endpointOperations[index] = "OP11-ENDPOINT-ERRORS:" + endpointOperations[index]
	}
	for index := range mutationOperations {
		mutationOperations[index] = "OP11-MUTATION-OUTCOME:" + mutationOperations[index]
	}
	sort.Strings(endpointOperations)
	sort.Strings(mutationOperations)
	if !reflect.DeepEqual(gotEndpoints, endpointOperations) {
		t.Fatalf("endpoint gates = %v, want %v", gotEndpoints, endpointOperations)
	}
	if !reflect.DeepEqual(gotMutations, mutationOperations) {
		t.Fatalf("mutation gates = %v, want %v", gotMutations, mutationOperations)
	}
}

func openAPIOperationIDs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../spec/partiful.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	operations := make([]string, 0)
	for _, path := range document.Paths {
		for _, operation := range path {
			if operation.OperationID != "" {
				operations = append(operations, operation.OperationID)
			}
		}
	}
	sort.Strings(operations)
	return operations
}

func identitiesWithPrefix(entries []Gate, prefix string) []string {
	identities := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Identity, prefix) {
			identities = append(identities, entry.Identity)
		}
	}
	sort.Strings(identities)
	return identities
}

func TestServiceChecksEvidenceGatesBeforeTypedDispatch(t *testing.T) {
	t.Parallel()
	catalog, _ := command.DefaultCatalog()
	manifest, _ := NewGateManifest([]Gate{{Identity: "TEST:operation", State: GateOpenOperation, Source: "test"}})
	service := NewService(catalog, manifest)
	calls := 0
	err := BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
		Operation: domain.OperationGetVersion, RequiredGates: []string{"TEST:operation"},
		Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
			calls++
			return domain.VersionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
	var applicationError *domain.Error
	if calls != 0 || !errors.As(err, &applicationError) || applicationError.Type != domain.ErrorContractProtocolChanged || applicationError.Code != "EVIDENCE_GATE_OPEN" {
		t.Fatalf("Invoke(open gate) calls = %d, error = %#v", calls, err)
	}
}

func TestServiceOpenPathIsNotUnlockedByClosedSibling(t *testing.T) {
	t.Parallel()
	catalog, _ := command.DefaultCatalog()
	manifest, _ := NewGateManifest([]Gate{
		{Identity: "TEST:sibling", State: GateClosed, Source: "test"},
		{Identity: "TEST:path", State: GateOpenPath, Source: "test"},
	})
	service := NewService(catalog, manifest)
	calls := 0
	_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
		Operation: domain.OperationGetVersion, RequiredGates: []string{"TEST:sibling", "TEST:path"},
		Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
			calls++
			return domain.VersionResult{}, nil
		},
	})
	_, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
	var applicationError *domain.Error
	if calls != 0 || !errors.As(err, &applicationError) || applicationError.Code != "EVIDENCE_GATE_OPEN" {
		t.Fatalf("Invoke(open path with closed sibling) calls = %d, error = %#v", calls, err)
	}
}

func TestServiceUsesOneTypedHandlerAndSanitizesOpenClaims(t *testing.T) {
	t.Parallel()
	catalog, _ := command.DefaultCatalog()
	manifest, _ := NewGateManifest([]Gate{{Identity: "TEST:error", State: GateOpenClaim, Source: "test"}})
	wrongTypes := NewService(catalog, manifest)
	if err := BindOperation(wrongTypes, OperationSpec[string, domain.VersionResult]{
		Operation: domain.OperationGetVersion,
		Execute: func(context.Context, *Invocation, string) (domain.VersionResult, error) {
			return domain.VersionResult{}, nil
		},
	}); err == nil {
		t.Fatal("BindOperation accepted an input type that disagrees with the command catalog")
	}
	service := NewService(catalog, manifest)
	calls := 0
	_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
		Operation: domain.OperationGetVersion, ErrorGate: "TEST:error",
		Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
			calls++
			return domain.VersionResult{}, errors.New("private upstream body")
		},
	})
	_, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
	var applicationError *domain.Error
	if calls != 1 || !errors.As(err, &applicationError) || applicationError.Code != "EVIDENCE_CLAIM_OPEN" || strings.Contains(err.Error(), "private") {
		t.Fatalf("Invoke(open claim) calls = %d, error = %#v", calls, err)
	}

	closed, _ := NewGateManifest([]Gate{{Identity: "TEST:error", State: GateClosed, Source: "test"}})
	service = NewService(catalog, closed)
	_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
		Operation: domain.OperationGetVersion,
		Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
			return domain.VersionResult{CLIVersion: "test"}, nil
		},
	})
	result, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
	if err != nil || result.(domain.VersionResult).CLIVersion != "test" {
		t.Fatalf("Invoke(closed) = %#v, %v", result, err)
	}
	if _, err := service.Invoke(context.Background(), domain.OperationGetVersion, domain.CommandSchemaInput{}); err == nil {
		t.Fatal("Invoke(wrong typed input) succeeded")
	}
}

func TestServiceOpenClaimPermitsEvidencedErrorsAndNarrowMutationResult(t *testing.T) {
	t.Parallel()
	catalog, _ := command.DefaultCatalog()
	manifest, _ := NewGateManifest([]Gate{
		{Identity: "TEST:error", State: GateOpenClaim, Source: "test"},
		{Identity: "TEST:outcome", State: GateOpenClaim, Source: "test"},
	})

	t.Run("evidenced error", func(t *testing.T) {
		service := NewService(catalog, manifest)
		_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
			Operation: domain.OperationGetVersion, ErrorGate: "TEST:error",
			Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
				return domain.VersionResult{}, &transport.ProtocolFailure{Class: string(domain.ErrorRemoteRateLimited), DispatchState: transport.DispatchStarted}
			},
		})
		_, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
		var applicationError *domain.Error
		if !errors.As(err, &applicationError) || applicationError.Type != domain.ErrorRemoteRateLimited {
			t.Fatalf("Invoke(evidenced error) = %#v", err)
		}
	})

	t.Run("narrow intent result", func(t *testing.T) {
		service := NewService(catalog, manifest)
		_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
			Operation: domain.OperationGetVersion, OutcomeGate: "TEST:outcome",
			Execute: func(_ context.Context, invocation *Invocation, _ struct{}) (domain.VersionResult, error) {
				return DispatchMutation(invocation, func() (domain.VersionResult, error) {
					return domain.VersionResult{CLIVersion: "submitted"}, nil
				})
			},
		})
		result, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
		if err != nil || result.(domain.VersionResult).CLIVersion != "submitted" {
			t.Fatalf("Invoke(narrow result) = %#v, %v", result, err)
		}
	})
}

func TestMutationDispatchAllowsAtMostOneAttempt(t *testing.T) {
	t.Parallel()
	invocation := &Invocation{}
	calls := 0
	first, err := DispatchMutation(invocation, func() (string, error) {
		calls++
		return "sent", nil
	})
	if err != nil || first != "sent" || calls != 1 || invocation.MutationAttempts() != 1 {
		t.Fatalf("first dispatch = %q, %v; calls %d attempts %d", first, err, calls, invocation.MutationAttempts())
	}
	if _, err := DispatchMutation(invocation, func() (string, error) {
		calls++
		return "retried", nil
	}); err == nil || calls != 1 || invocation.MutationAttempts() != 1 {
		t.Fatalf("second dispatch error = %v; calls %d attempts %d", err, calls, invocation.MutationAttempts())
	}
}

func TestServiceMutationFaultsMakeAtMostOneWriteAndNoReadback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		before     error
		writeFault error
		wantWrites int
	}{
		{name: "before write", before: &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID", Message: "invalid"}},
		{name: "during write", writeFault: &transport.ProtocolFailure{Class: string(domain.ErrorRemoteUnavailable), DispatchState: transport.DispatchStarted}, wantWrites: 1},
		{name: "after response loss", writeFault: &transport.ProtocolFailure{Class: "unknown.response", DispatchState: transport.DispatchStarted}, wantWrites: 1},
		{name: "accepted intent without optional readback", wantWrites: 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			catalog, _ := command.DefaultCatalog()
			manifest, _ := NewGateManifest([]Gate{
				{Identity: "TEST:error", State: GateOpenClaim, Source: "test"},
				{Identity: "TEST:outcome", State: GateOpenClaim, Source: "test"},
			})
			service := NewService(catalog, manifest)
			writes, readbacks := 0, 0
			_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
				Operation: domain.OperationGetVersion, ErrorGate: "TEST:error", OutcomeGate: "TEST:outcome",
				Execute: func(_ context.Context, invocation *Invocation, _ struct{}) (domain.VersionResult, error) {
					if test.before != nil {
						return domain.VersionResult{}, test.before
					}
					return DispatchMutation(invocation, func() (domain.VersionResult, error) {
						writes++
						return domain.VersionResult{CLIVersion: "submitted"}, test.writeFault
					})
				},
			})
			result, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
			if writes != test.wantWrites || readbacks != 0 {
				t.Fatalf("writes = %d, readbacks = %d; want %d, 0", writes, readbacks, test.wantWrites)
			}
			if test.before == nil && test.writeFault == nil && (err != nil || result.(domain.VersionResult).CLIVersion != "submitted") {
				t.Fatalf("accepted intent = %#v, %v", result, err)
			}
			if (test.before != nil || test.writeFault != nil) && err == nil {
				t.Fatal("faulted mutation succeeded")
			}
		})
	}
}

func TestServiceNormalizesErrorsWithoutLeakingPrivateCause(t *testing.T) {
	t.Parallel()
	catalog, _ := command.DefaultCatalog()
	manifest, _ := NewGateManifest(nil)
	cases := []struct {
		name    string
		failure error
		want    domain.ErrorType
	}{
		{name: "classified transport", failure: &transport.ProtocolFailure{Operation: "getPosterCatalog", Class: "remote.rate_limited", Retryable: true, DispatchState: transport.DispatchStarted}, want: domain.ErrorRemoteRateLimited},
		{name: "unknown transport class", failure: &transport.ProtocolFailure{Operation: "getPosterCatalog", Class: "future.private.class", DispatchState: transport.DispatchStarted}, want: domain.ErrorContractProtocolChanged},
		{name: "unclassified cause", failure: errors.New("private upstream body"), want: domain.ErrorInternalFailure},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(catalog, manifest)
			_ = BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
				Operation: domain.OperationGetVersion,
				Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
					return domain.VersionResult{}, test.failure
				},
			})
			_, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
			var applicationError *domain.Error
			if !errors.As(err, &applicationError) || applicationError.Type != test.want || strings.Contains(err.Error(), "private") || applicationError.Details.Candidates != nil {
				t.Fatalf("Invoke() error = %#v, want sanitized %s", err, test.want)
			}
		})
	}
}
