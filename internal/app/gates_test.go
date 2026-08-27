package app

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

func TestNewGateManifestRejectsMissingAndDuplicateFullIdentity(t *testing.T) {
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
		"OP11-EVENT-LIST-REQUEST":            GateOpenOperation,
		"OP11-AUTH-REQUESTS:refreshToken":    GateOpenPath,
		"OP11-ENDPOINT-ERRORS:getGuests":     GateOpenClaim,
		"OP11-MUTATION-OUTCOME:createEvent":  GateOpenClaim,
		"OP11-UPLOAD-PHOTO":                  GateDormant,
		"OP11-PROJECTION:EVENT-LIST-SUMMARY": GateOpenOperation,
		"COLLECTION-GUEST-PAGE-21":           GateOpenPath,
	}
	for identity, want := range wantStates {
		gate, ok := manifest.Lookup(identity)
		if !ok || gate.State != want || gate.Source == "" {
			t.Fatalf("gate %q = %#v, found %t; want state %q and source", identity, gate, ok, want)
		}
	}

	endpointOperations := []string{
		"sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "refreshToken", "lookupFirebaseUser",
		"getMyUpcomingEventsForHomePage", "getMyPastEventsForHomePage", "getEventInfo", "createEvent", "updateEvent", "cancelEvent",
		"getGuests", "addGuest", "markGuestInterested", "inviteGuest", "setCohostStatus", "getCurrentGuest", "sendBlast",
		"getContacts", "getPosterCatalog", "queryEvent", "queryGuest", "queryGuestConfig", "createMessage", "createFeedMessage",
		"updateMessage", "uploadEventPhoto", "putPosterImage",
	}
	mutationOperations := []string{"createEvent", "updateEvent", "cancelEvent", "addGuest", "markGuestInterested", "inviteGuest", "setCohostStatus", "sendBlast", "createMessage", "createFeedMessage", "updateMessage", "putPosterImage"}

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
