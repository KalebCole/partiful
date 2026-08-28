package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

type recordingApplication struct {
	calls     int
	operation domain.OperationID
	input     any
	result    any
	err       error
}

func (application *recordingApplication) Invoke(_ context.Context, operation domain.OperationID, input any) (any, error) {
	application.calls++
	application.operation = operation
	application.input = input
	return application.result, application.err
}

func newTestCLI(t *testing.T, application *recordingApplication, stdin string) (*CLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	adapter, err := New(Config{
		Catalog:     catalog,
		Application: application,
		Stdin:       strings.NewReader(stdin),
		Stdout:      stdout,
		Stderr:      stderr,
		IsTerminal:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter, stdout, stderr
}

func TestHelpIsDerivedForEveryPublicCommandPath(t *testing.T) {
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range catalog.Definitions() {
		descriptor := descriptor
		t.Run(descriptor.CLI.Path, func(t *testing.T) {
			application := &recordingApplication{}
			adapter, stdout, stderr := newTestCLI(t, application, "")
			args := append(strings.Fields(descriptor.CLI.Path), "--help")
			if exit := adapter.Run(context.Background(), args); exit != int(domain.ExitSuccess) {
				t.Fatalf("Run() exit = %d, want 0; stderr=%q", exit, stderr.String())
			}
			if application.calls != 0 {
				t.Fatalf("help invoked application %d times", application.calls)
			}
			output := stdout.String()
			if !strings.Contains(output, "Usage: partiful "+descriptor.CLI.Path) || !strings.Contains(output, descriptor.Help) {
				t.Fatalf("help output did not contain registry path and help: %q", output)
			}
			for _, global := range []string{"--json", "--plain", "--no-input", "--version"} {
				if !strings.Contains(output, global) {
					t.Errorf("help output missing global flag %s", global)
				}
			}
			if stderr.Len() != 0 {
				t.Fatalf("help wrote stderr: %q", stderr.String())
			}
		})
	}
}

func TestRootHelpListsEveryPublicCommand(t *testing.T) {
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	adapter, stdout, stderr := newTestCLI(t, &recordingApplication{}, "")
	if exit := adapter.Run(context.Background(), []string{"--help"}); exit != int(domain.ExitSuccess) {
		t.Fatalf("root help exit = %d; stderr=%q", exit, stderr.String())
	}
	for _, descriptor := range catalog.Definitions() {
		if !strings.Contains(stdout.String(), descriptor.CLI.Path+"\t"+descriptor.Help) {
			t.Errorf("root help missing %q", descriptor.CLI.Path)
		}
	}
}

func TestHelpForAllPublicPathsMatchesGolden(t *testing.T) {
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var actual strings.Builder
	for _, descriptor := range catalog.Definitions() {
		adapter, stdout, stderr := newTestCLI(t, &recordingApplication{}, "")
		if exit := adapter.Run(context.Background(), append(strings.Fields(descriptor.CLI.Path), "--help")); exit != int(domain.ExitSuccess) {
			t.Fatalf("%s help exit = %d; stderr=%q", descriptor.CLI.Path, exit, stderr.String())
		}
		fmt.Fprintf(&actual, "## %s\n%s", descriptor.CLI.Path, stdout.String())
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/help.golden", []byte(actual.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile("testdata/help.golden")
	if err != nil {
		t.Fatal(err)
	}
	if actual.String() != string(expected) {
		t.Fatalf("all-path help differs from testdata/help.golden")
	}
}

func TestJSONInvocationUsesRegistryOperationAndPublicFieldNames(t *testing.T) {
	application := &recordingApplication{result: domain.VersionResult{
		CLIVersion: "0.1.0", CommandContractRevision: "1", TransportContractRevision: "2026-08-12.7",
	}}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"--json", "version"}); exit != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if application.calls != 1 || application.operation != domain.OperationGetVersion {
		t.Fatalf("application call = (%d, %q), want (1, %q)", application.calls, application.operation, domain.OperationGetVersion)
	}
	if _, ok := application.input.(struct{}); !ok {
		t.Fatalf("application input type = %T, want struct{}", application.input)
	}
	want := "{\"cli_version\":\"0.1.0\",\"command_contract_revision\":\"1\",\"transport_contract_revision\":\"2026-08-12.7\"}\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRegistryFlagsPositionalsAndStdinContentDecodeToTypedInput(t *testing.T) {
	application := &recordingApplication{result: domain.SetRSVPResult{EventID: "evt_public", Intent: domain.RSVPIntentGoing, Submitted: true}}
	adapter, _, stderr := newTestCLI(t, application, "A short public reply")
	args := []string{
		"rsvp", "set", "evt_public", "--status", "going", "--display-name", "Sample Guest",
		"--party-size", "2", "--plus-one", "Guest One", "--timezone", "America/Los_Angeles",
		"--message-file", "-", "--dry-run", "--json",
	}

	if exit := adapter.Run(context.Background(), args); exit != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	input, ok := application.input.(domain.SetRSVPInput)
	if !ok {
		t.Fatalf("application input type = %T, want domain.SetRSVPInput", application.input)
	}
	if input.EventID != "evt_public" || input.Status != domain.RSVPIntentGoing || input.PartySize == nil || *input.PartySize != 2 || len(input.PlusOnes) != 1 || input.Message == nil || *input.Message != "A short public reply" || !input.DryRun {
		t.Fatalf("typed input was not decoded from CLI sources: %#v", input)
	}
}

func TestClassifiedApplicationErrorsUseStableExitAndJSONStderr(t *testing.T) {
	tests := []struct {
		name      string
		errorType domain.ErrorType
		exit      domain.ExitClass
	}{
		{"auth", domain.ErrorAuthRequired, domain.ExitAuth},
		{"permission", domain.ErrorPermissionDenied, domain.ExitPermission},
		{"not-found", domain.ErrorResourceNotFound, domain.ExitNotFound},
		{"conflict", domain.ErrorStateConflict, domain.ExitConflict},
		{"remote", domain.ErrorRemoteUnavailable, domain.ExitRemote},
		{"protocol", domain.ErrorContractProtocolChanged, domain.ExitProtocol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &recordingApplication{err: &domain.Error{
				Type: test.errorType, Code: "SAFE_CODE", Message: "safe public message", Retryable: true,
			}}
			adapter, stdout, stderr := newTestCLI(t, application, "")
			if exit := adapter.Run(context.Background(), []string{"version", "--json"}); exit != int(test.exit) {
				t.Fatalf("Run() exit = %d, want %d", exit, test.exit)
			}
			if stdout.Len() != 0 {
				t.Fatalf("failure stdout = %q, want empty", stdout.String())
			}
			want := "{\"error\":{\"type\":\"" + string(test.errorType) + "\",\"code\":\"SAFE_CODE\",\"message\":\"safe public message\",\"retryable\":true,\"details\":{}}}\n"
			if stderr.String() != want {
				t.Fatalf("stderr = %q, want %q", stderr.String(), want)
			}
		})
	}
}

func TestInternalFailureRedactsApplicationText(t *testing.T) {
	application := &recordingApplication{err: &domain.Error{
		Type: domain.ErrorInternalFailure, Code: "PRIVATE_TOKEN_VALUE", Message: "private message content",
	}}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"version", "--json"}); exit != int(domain.ExitInternal) {
		t.Fatalf("Run() exit = %d, want %d", exit, domain.ExitInternal)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure stdout = %q, want empty", stdout.String())
	}
	want := "{\"error\":{\"type\":\"internal.failure\",\"code\":\"INTERNAL_FAILURE\",\"message\":\"the operation failed\",\"retryable\":false,\"details\":{}}}\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want redacted %q", stderr.String(), want)
	}
}

func TestApplicationGateErrorPassesThroughWithoutFallback(t *testing.T) {
	gateError := &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "EVIDENCE_GATE_OPEN", Message: "operation is unavailable until its evidence gate is closed"}
	application := &recordingApplication{err: gateError}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"events", "get", "evt_public", "--json"}); exit != int(domain.ExitProtocol) {
		t.Fatalf("Run() exit = %d, want %d", exit, domain.ExitProtocol)
	}
	if application.calls != 1 {
		t.Fatalf("application calls = %d, want one with no fallback", application.calls)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "EVIDENCE_GATE_OPEN") {
		t.Fatalf("gate result streams stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRSVPJSONUsesTheCommandResultSchemaEnvelope(t *testing.T) {
	status := domain.EventReadRSVPGoing
	application := &recordingApplication{result: domain.RSVPResult{EventID: "evt_public", Status: &status}}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"rsvp", "get", "evt_public", "--json"}); exit != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit = %d; stderr=%q", exit, stderr.String())
	}
	want := "{\"rsvp\":{\"event_id\":\"evt_public\",\"status\":\"going\"}}\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestDefaultOutputIsHumanReadableAndRemainsOnStdout(t *testing.T) {
	application := &recordingApplication{result: domain.SubmittedResult{Submitted: true}}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"events", "create", "--title", "Dinner", "--start", "2026-08-30T18:00:00-07:00", "--timezone", "America/Los_Angeles", "--dry-run"}); exit != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit = %d; stderr=%q", exit, stderr.String())
	}
	if stdout.String() != "submitted: true\n" {
		t.Fatalf("human stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("success stderr = %q, want empty", stderr.String())
	}
}

func TestPlainCollectionOutputIsStableTSV(t *testing.T) {
	title := "Dinner"
	application := &recordingApplication{result: domain.EventsResult{Events: []domain.EventSummary{{EventID: "evt_public", Title: &title}}}}
	adapter, stdout, stderr := newTestCLI(t, application, "")

	if exit := adapter.Run(context.Background(), []string{"events", "list", "--when", "upcoming", "--plain"}); exit != int(domain.ExitSuccess) {
		t.Fatalf("Run() exit = %d; stderr=%q", exit, stderr.String())
	}
	want := "event_id\ttitle\tstart\tend\ttimezone\tstate\tuser_role\tmy_rsvp\nevt_public\tDinner\t\t\t\t\t\t\n"
	if stdout.String() != want {
		t.Fatalf("plain stdout = %q, want %q", stdout.String(), want)
	}
}

func TestAllPublicPathsDecodeTheirRegistryInputType(t *testing.T) {
	arguments := map[string][]string{
		"auth login":            {"auth", "login"},
		"auth status":           {"auth", "status"},
		"auth logout":           {"auth", "logout", "--dry-run"},
		"events list":           {"events", "list", "--when", "upcoming"},
		"events get":            {"events", "get", "evt_public"},
		"events create":         {"events", "create", "--title", "Dinner", "--start", "2026-08-30T18:00:00-07:00", "--timezone", "America/Los_Angeles", "--dry-run"},
		"events update":         {"events", "update", "evt_public", "--title", "Dinner", "--dry-run"},
		"events cancel":         {"events", "cancel", "evt_public", "--dry-run"},
		"guests list":           {"guests", "list", "evt_public"},
		"guests invite":         {"guests", "invite", "evt_public", "--contact-ref", "contact_public", "--dry-run"},
		"rsvp get":              {"rsvp", "get", "evt_public"},
		"rsvp set":              {"rsvp", "set", "evt_public", "--status", "interested", "--dry-run"},
		"contacts list":         {"contacts", "list"},
		"cohosts invite":        {"cohosts", "invite", "evt_public", "--contact-ref", "contact_public", "--dry-run"},
		"cohosts revoke-invite": {"cohosts", "revoke-invite", "evt_public", "--contact-ref", "contact_public", "--dry-run"},
		"cohosts remove":        {"cohosts", "remove", "evt_public", "--contact-ref", "contact_public", "--dry-run"},
		"cohosts link create":   {"cohosts", "link", "create", "evt_public", "--dry-run"},
		"cohosts link revoke":   {"cohosts", "link", "revoke", "evt_public", "--dry-run"},
		"blasts send":           {"blasts", "send", "evt_public", "--audience", "all-guests", "--message-file", "-", "--dry-run"},
		"posters list":          {"posters", "list"},
		"posters search":        {"posters", "search", "--query", "bright"},
		"schema":                {"schema"},
		"doctor":                {"doctor"},
		"version":               {"version"},
	}
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range catalog.Definitions() {
		descriptor := descriptor
		t.Run(descriptor.CLI.Path, func(t *testing.T) {
			application := &recordingApplication{result: map[string]any{}}
			adapter, _, stderr := newTestCLI(t, application, "short message")
			args, found := arguments[descriptor.CLI.Path]
			if !found {
				t.Fatal("missing test arguments")
			}
			args = append(append([]string(nil), args...), "--json")
			if exit := adapter.Run(context.Background(), args); exit != int(domain.ExitSuccess) {
				t.Fatalf("Run() exit = %d; stderr=%q", exit, stderr.String())
			}
			if got := reflect.TypeOf(application.input); got != descriptor.InputType {
				t.Fatalf("input type = %v, want %v", got, descriptor.InputType)
			}
		})
	}
}
