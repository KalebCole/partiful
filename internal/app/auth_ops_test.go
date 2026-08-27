package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
)

type fakeAuthProvider struct {
	loginState  domain.AuthState
	statusState domain.AuthState
	logoutState domain.AuthState
	loginErr    error
	statusErr   error
	logoutErr   error
	loginCalls  int
	statusCalls int
	logoutCalls int
}

func (provider *fakeAuthProvider) Login(context.Context, auth.LoginPrompter) (domain.AuthState, error) {
	provider.loginCalls++
	return provider.loginState, provider.loginErr
}
func (provider *fakeAuthProvider) Status(context.Context) (domain.AuthState, error) {
	provider.statusCalls++
	return provider.statusState, provider.statusErr
}
func (provider *fakeAuthProvider) Acquire(context.Context) (auth.Authorization, error) {
	return auth.Authorization{}, errors.New("not used")
}
func (provider *fakeAuthProvider) Logout(context.Context) (domain.AuthState, error) {
	provider.logoutCalls++
	return provider.logoutState, provider.logoutErr
}

type fakeLoginPrompter struct{}

var authLoginRequestGateIdentities = []string{
	"OP11-AUTH-REQUESTS:sendAuthCodeTrusted",
	"OP11-AUTH-REQUESTS:getLoginToken",
	"OP11-AUTH-REQUESTS:signInWithCustomToken",
	"OP11-AUTH-REQUESTS:lookupFirebaseUser",
}

func (fakeLoginPrompter) PhoneNumber(context.Context) (auth.Secret, error) {
	return auth.NewSecret("private"), nil
}
func (fakeLoginPrompter) VerificationCode(context.Context) (auth.Secret, error) {
	return auth.NewSecret("private"), nil
}

func testService(t *testing.T, gates GateManifest) *Service {
	t.Helper()
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return NewService(catalog, gates)
}

func manifestWithClosedGates(t *testing.T, identities ...string) GateManifest {
	t.Helper()
	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatal(err)
	}
	closed := make(map[string]bool, len(identities))
	for _, identity := range identities {
		closed[identity] = true
	}
	entries := manifest.Entries()
	for index := range entries {
		if closed[entries[index].Identity] {
			entries[index].State = GateClosed
		}
	}
	result, err := NewGateManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBindAuthOperationsKeepsLoginBehindAllRequestGates(t *testing.T) {
	provider := &fakeAuthProvider{}
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	if err := BindAuthOperations(service, provider, fakeLoginPrompter{}); err != nil {
		t.Fatal(err)
	}

	_, err := service.Invoke(context.Background(), domain.OperationAuthLoginInteractive, struct{}{})
	var public *domain.Error
	if !errors.As(err, &public) || public.Code != "EVIDENCE_GATE_OPEN" {
		t.Fatalf("login error = %#v", err)
	}
	if provider.loginCalls != 0 {
		t.Fatalf("provider login calls = %d, want 0", provider.loginCalls)
	}
}

func TestAuthLoginEndpointErrorGatesDoNotBlockAnEvidencedRequest(t *testing.T) {
	provider := &fakeAuthProvider{loginState: domain.AuthState{Authenticated: true, TokenState: domain.TokenStateHealthy}}
	service := testService(t, manifestWithClosedGates(t, authLoginRequestGateIdentities...))
	if err := BindAuthOperations(service, provider, fakeLoginPrompter{}); err != nil {
		t.Fatal(err)
	}

	_, err := service.Invoke(context.Background(), domain.OperationAuthLoginInteractive, struct{}{})
	if err != nil {
		t.Fatalf("login with closed request gates = %v", err)
	}
	if provider.loginCalls != 1 {
		t.Fatalf("provider login calls = %d, want 1", provider.loginCalls)
	}
}

func TestAuthOperationsDelegateLoginStatusAndLogout(t *testing.T) {
	login := domain.AuthState{Authenticated: true, TokenState: domain.TokenStateHealthy}
	status := domain.AuthState{TokenState: domain.TokenStateMissing}
	logout := domain.AuthState{TokenState: domain.TokenStateMissing}
	provider := &fakeAuthProvider{loginState: login, statusState: status, logoutState: logout}
	service := testService(t, manifestWithClosedGates(t, authLoginRequestGateIdentities...))
	if err := BindAuthOperations(service, provider, fakeLoginPrompter{}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		operation domain.OperationID
		input     any
		want      domain.AuthState
	}{
		{domain.OperationAuthLoginInteractive, struct{}{}, login},
		{domain.OperationGetAuthStatus, struct{}{}, status},
		{domain.OperationLogout, domain.LogoutInput{}, logout},
	}
	for _, test := range cases {
		got, err := service.Invoke(context.Background(), test.operation, test.input)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s = %#v, %v; want %#v", test.operation, got, err, test.want)
		}
	}
	if provider.loginCalls != 1 || provider.statusCalls != 1 || provider.logoutCalls != 1 {
		t.Fatalf("calls = login:%d status:%d logout:%d", provider.loginCalls, provider.statusCalls, provider.logoutCalls)
	}
}

func TestLogoutDryRunDoesNotReadOrDeleteCredentials(t *testing.T) {
	provider := &fakeAuthProvider{}
	manifest, _ := DefaultGateManifest()
	service := testService(t, manifest)
	if err := BindAuthOperations(service, provider, fakeLoginPrompter{}); err != nil {
		t.Fatal(err)
	}

	got, err := service.Invoke(context.Background(), domain.OperationLogout, domain.LogoutInput{DryRun: true})
	want := domain.AuthState{Authenticated: false, TokenState: domain.TokenStateMissing}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("dry-run logout = %#v, %v; want %#v", got, err, want)
	}
	if provider.statusCalls != 0 || provider.logoutCalls != 0 {
		t.Fatalf("dry run touched provider: status=%d logout=%d", provider.statusCalls, provider.logoutCalls)
	}
}

func TestBindAuthOperationsRejectsMissingDependencies(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	if err := BindAuthOperations(testService(t, manifest), nil, fakeLoginPrompter{}); err == nil {
		t.Fatal("BindAuthOperations accepted nil provider")
	}
	if err := BindAuthOperations(testService(t, manifest), &fakeAuthProvider{}, nil); err == nil {
		t.Fatal("BindAuthOperations accepted nil login prompter")
	}
}
