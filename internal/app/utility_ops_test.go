package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/version"
)

type fakeCredentialInspector struct {
	state domain.AuthState
	err   error
	calls int
}

func (inspector *fakeCredentialInspector) InspectCredentials(context.Context) (domain.AuthState, error) {
	inspector.calls++
	return inspector.state, inspector.err
}

func utilityConfig(inspector CredentialInspector) UtilityOperationsConfig {
	current := version.Current()
	return UtilityOperationsConfig{
		Credentials: inspector,
		Version: domain.VersionResult{
			CLIVersion:                current.CLIVersion,
			CommandContractRevision:   current.CommandContractRevision,
			TransportContractRevision: current.TransportContractRevision,
		},
	}
}

func TestUtilityOperationsProjectCatalogAndVersion(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	inspector := &fakeCredentialInspector{state: domain.AuthState{Authenticated: true, TokenState: domain.TokenStateHealthy}}
	if err := BindUtilityOperations(service, utilityConfig(inspector)); err != nil {
		t.Fatal(err)
	}

	allValue, err := service.Invoke(context.Background(), domain.OperationGetCommandSchema, domain.CommandSchemaInput{})
	if err != nil {
		t.Fatal(err)
	}
	all := allValue.(domain.CommandSchemaResult)
	if len(all.Commands) != 24 || len(all.MCPTools) != 23 || !reflect.DeepEqual(all.CLIOnlyCommands, []string{"auth login"}) {
		t.Fatalf("schema inventory = commands:%d tools:%d cli-only:%v", len(all.Commands), len(all.MCPTools), all.CLIOnlyCommands)
	}

	path := "posters search"
	oneValue, err := service.Invoke(context.Background(), domain.OperationGetCommandSchema, domain.CommandSchemaInput{Command: &path})
	if err != nil {
		t.Fatal(err)
	}
	one := oneValue.(domain.CommandSchemaResult)
	if len(one.Commands) != 1 || one.Commands[0].CLIPath != path || one.Commands[0].MCP == nil || one.Commands[0].MCP.Name != "posters_search" {
		t.Fatalf("single schema = %#v", one)
	}

	versionValue, err := service.Invoke(context.Background(), domain.OperationGetVersion, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	gotVersion := versionValue.(domain.VersionResult)
	current := version.Current()
	wantVersion := domain.VersionResult{CLIVersion: current.CLIVersion, CommandContractRevision: current.CommandContractRevision, TransportContractRevision: current.TransportContractRevision}
	if gotVersion != wantVersion {
		t.Fatalf("version = %#v, want %#v", gotVersion, wantVersion)
	}
}

func TestSchemaRejectsUnknownCommandAsInputInvalid(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	if err := BindUtilityOperations(service, utilityConfig(&fakeCredentialInspector{})); err != nil {
		t.Fatal(err)
	}
	path := "not a command"
	_, err := service.Invoke(context.Background(), domain.OperationGetCommandSchema, domain.CommandSchemaInput{Command: &path})
	var public *domain.Error
	if !errors.As(err, &public) || public.Type != domain.ErrorInputInvalid || public.Code != "UNKNOWN_COMMAND" {
		t.Fatalf("schema error = %#v", err)
	}
}

func TestDoctorUsesOnlyRedactedLocalCredentialInspection(t *testing.T) {
	cases := []struct {
		name       string
		inspector  *fakeCredentialInspector
		wantHealth bool
		wantStatus domain.DoctorStatus
		wantText   string
	}{
		{name: "available", inspector: &fakeCredentialInspector{state: domain.AuthState{Authenticated: true, TokenState: domain.TokenStateHealthy}}, wantHealth: true, wantStatus: domain.DoctorStatusPass, wantText: "available"},
		{name: "missing", inspector: &fakeCredentialInspector{state: domain.AuthState{TokenState: domain.TokenStateMissing}}, wantHealth: true, wantStatus: domain.DoctorStatusWarn, wantText: "not available"},
		{name: "invalid local state", inspector: &fakeCredentialInspector{state: domain.AuthState{Authenticated: true, TokenState: domain.TokenState("future-private-state")}}, wantHealth: false, wantStatus: domain.DoctorStatusFail, wantText: "could not be inspected"},
		{name: "unreadable", inspector: &fakeCredentialInspector{err: errors.New("private path token phone raw body")}, wantHealth: false, wantStatus: domain.DoctorStatusFail, wantText: "could not be inspected"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest, _ := DefaultGateManifest()
			service := testService(t, manifest)
			if err := BindUtilityOperations(service, utilityConfig(test.inspector)); err != nil {
				t.Fatal(err)
			}
			value, err := service.Invoke(context.Background(), domain.OperationRunDoctor, struct{}{})
			if err != nil {
				t.Fatal(err)
			}
			got := value.(domain.DoctorResult)
			if got.Healthy != test.wantHealth || len(got.Checks) != 1 || got.Checks[0].Name != "credentials" || got.Checks[0].Status != test.wantStatus || !strings.Contains(strings.ToLower(got.Checks[0].Message), test.wantText) {
				t.Fatalf("doctor = %#v", got)
			}
			text := strings.ToLower(got.Checks[0].Message)
			for _, private := range []string{"private path", "token", "phone", "raw body"} {
				if strings.Contains(text, private) {
					t.Fatalf("doctor leaked %q in %q", private, text)
				}
			}
			if test.inspector.calls != 1 {
				t.Fatalf("inspection calls = %d, want 1", test.inspector.calls)
			}
		})
	}
}

func TestBindUtilityOperationsRequiresLocalInspector(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	if err := BindUtilityOperations(testService(t, manifest), UtilityOperationsConfig{}); err == nil {
		t.Fatal("BindUtilityOperations accepted nil inspector")
	}
	if err := BindUtilityOperations(testService(t, manifest), UtilityOperationsConfig{Credentials: &fakeCredentialInspector{}}); err == nil {
		t.Fatal("BindUtilityOperations accepted empty version contract")
	}
}
