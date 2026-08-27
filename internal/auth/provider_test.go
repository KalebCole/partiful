package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
)

type memoryStore struct {
	backend      BackendKind
	mu           sync.Mutex
	slots        map[Slot][]byte
	ignoreDelete bool
	corruptLoad  Slot
}

func (store *memoryStore) Backend() BackendKind { return store.backend }
func (store *memoryStore) Load(_ context.Context, slot Slot) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := append([]byte(nil), store.slots[slot]...)
	if slot == store.corruptLoad && len(value) > 0 {
		value[0] ^= 0xff
	}
	return value, nil
}
func (store *memoryStore) Store(_ context.Context, slot Slot, value []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.slots[slot] = append([]byte(nil), value...)
	return nil
}
func (store *memoryStore) Delete(_ context.Context, slot Slot) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ignoreDelete {
		return nil
	}
	delete(store.slots, slot)
	return nil
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type testPrompter struct{}

func (testPrompter) PhoneNumber(context.Context) (Secret, error) {
	return NewSecret("private-phone"), nil
}
func (testPrompter) VerificationCode(context.Context) (Secret, error) {
	return NewSecret("private-code"), nil
}

type fakeTransport struct {
	mu         sync.Mutex
	calls      []string
	refresh    Session
	refreshErr error
}

func (transport *fakeTransport) called(name string) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.calls = append(transport.calls, name)
}
func (transport *fakeTransport) SendCode(context.Context, SendCodeRequest) (SendCodeResult, error) {
	transport.called("send")
	return SendCodeResult{Challenge: NewSecret("challenge")}, nil
}
func (transport *fakeTransport) ExchangeLoginCode(context.Context, LoginCodeRequest) (LoginCodeResult, error) {
	transport.called("exchange")
	return LoginCodeResult{CustomToken: NewSecret("custom")}, nil
}
func (transport *fakeTransport) SignIn(context.Context, SignInRequest) (Session, error) {
	transport.called("signin")
	return transport.refresh, nil
}
func (transport *fakeTransport) Refresh(context.Context, RefreshRequest) (Session, error) {
	transport.called("refresh")
	return transport.refresh, transport.refreshErr
}
func (transport *fakeTransport) LookupAccount(context.Context, LookupRequest) (Account, error) {
	transport.called("lookup")
	return Account{Identity: NewSecret("account")}, nil
}

type mutexCoordinator struct{ mu sync.Mutex }

func (coordinator *mutexCoordinator) Do(ctx context.Context, operation func(context.Context) error) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return operation(ctx)
}

type warningSink struct{ warnings []Diagnostic }

func (sink *warningSink) Warn(_ context.Context, warning Diagnostic) {
	sink.warnings = append(sink.warnings, warning)
}

type allowGates map[string]bool

func (gates allowGates) Allows(operation string) bool { return gates[operation] }

func closedAuthGates(operations ...string) allowGates {
	gates := allowGates{}
	for _, operation := range operations {
		gates["OP11-AUTH-REQUESTS:"+operation] = true
		gates["OP11-ENDPOINT-ERRORS:"+operation] = true
	}
	return gates
}

func testCredential(generation uint64, access string) Credential {
	return Credential{SchemaVersion: 1, Generation: generation, AccountIdentity: NewSecret("account"), AccessToken: NewSecret(access), RefreshToken: NewSecret("refresh"), InstallationSecret: NewSecret("installation")}
}

func TestLoadActiveRejectsConflictingEqualGenerations(t *testing.T) {
	first, _ := EncodeCredential(testCredential(7, "first"))
	second, _ := EncodeCredential(testCredential(7, "second"))
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{SlotA: first, SlotB: second}}
	_, _, err := LoadActive(context.Background(), store)
	if !IsErrorType(err, domain.ErrorAuthStoreUnavailable) {
		t.Fatalf("LoadActive error = %v", err)
	}
}

func TestLoadActiveReportsCorruptionWhenAllPresentSlotsAreInvalid(t *testing.T) {
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{SlotA: []byte("invalid-a"), SlotB: []byte("invalid-b")}}
	_, _, err := LoadActive(context.Background(), store)
	if !IsErrorType(err, domain.ErrorAuthStoreUnavailable) {
		t.Fatalf("LoadActive error = %v", err)
	}
}

func TestCommitNextRejectsMismatchedReadbackAndPreservesActiveSlot(t *testing.T) {
	active, _ := EncodeCredential(testCredential(3, "active"))
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{SlotA: active}, corruptLoad: SlotB}
	_, err := CommitNext(context.Background(), store, SlotA, testCredential(4, "replacement"))
	if !IsErrorType(err, domain.ErrorAuthPersistenceFailed) {
		t.Fatalf("CommitNext error = %v", err)
	}
	store.corruptLoad = ""
	credential, slot, err := LoadActive(context.Background(), store)
	if err != nil || slot != SlotA || credential.AccessToken.Reveal() != "active" {
		t.Fatalf("active = %#v slot=%q error=%v", credential, slot, err)
	}
}

func TestLoginOpenGateMakesNoTransportCall(t *testing.T) {
	transport := &fakeTransport{}
	provider := NewProvider(ProviderConfig{Store: &memoryStore{backend: "memory", slots: map[Slot][]byte{}}, Transport: transport, Clock: fixedClock{}, Coordinator: &mutexCoordinator{}})
	_, err := provider.Login(context.Background(), testPrompter{})
	if !IsErrorType(err, domain.ErrorContractProtocolChanged) {
		t.Fatalf("Login error = %v", err)
	}
	if len(transport.calls) != 0 {
		t.Fatalf("transport calls = %v", transport.calls)
	}
}

func TestLoginCommitsVerifiedNextGeneration(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	transport := &fakeTransport{refresh: Session{AccessToken: NewSecret("access"), RefreshToken: NewSecret("refresh"), ExpiresAt: &expires}}
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{}}
	provider := NewProvider(ProviderConfig{Store: store, Transport: transport, Clock: fixedClock{now}, Coordinator: &mutexCoordinator{}, Gates: closedAuthGates("sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "lookupFirebaseUser"), NewInstallationSecret: func() (Secret, error) { return NewSecret("installation"), nil }})
	state, err := provider.Login(context.Background(), testPrompter{})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Authenticated || state.TokenState != domain.TokenStateHealthy {
		t.Fatalf("state = %+v", state)
	}
	credential, _, err := LoadActive(context.Background(), store)
	if err != nil || credential.Generation != 1 || credential.AccessToken.Reveal() != "access" {
		t.Fatalf("credential = %#v, error = %v", credential, err)
	}
}

func TestConcurrentAcquireRefreshesOnce(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	expiring := now.Add(time.Minute)
	healthy := now.Add(time.Hour)
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{}}
	encoded, _ := EncodeCredential(Credential{SchemaVersion: 1, Generation: 1, AccountIdentity: NewSecret("account"), AccessToken: NewSecret("old"), RefreshToken: NewSecret("refresh"), AccessTokenExpires: &expiring, InstallationSecret: NewSecret("installation")})
	store.slots[SlotA] = encoded
	transport := &fakeTransport{refresh: Session{AccessToken: NewSecret("new"), RefreshToken: NewSecret("new-refresh"), ExpiresAt: &healthy}}
	provider := NewProvider(ProviderConfig{Store: store, Transport: transport, Clock: fixedClock{now}, Coordinator: &mutexCoordinator{}, Gates: closedAuthGates("refreshToken")})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			authorization, err := provider.Acquire(context.Background())
			if err != nil || authorization.AccessToken.Reveal() != "new" {
				t.Errorf("Acquire = %q, %v", authorization.AccessToken.Reveal(), err)
			}
		}()
	}
	wg.Wait()
	count := 0
	for _, call := range transport.calls {
		if call == "refresh" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("refresh calls = %d", count)
	}
}

func TestAcquireReusesStillValidTokenWhenEarlyRefreshFails(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	expiring := now.Add(time.Minute)
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{}}
	encoded, _ := EncodeCredential(Credential{SchemaVersion: 1, Generation: 1, AccountIdentity: NewSecret("account"), AccessToken: NewSecret("still-valid"), RefreshToken: NewSecret("refresh"), AccessTokenExpires: &expiring, InstallationSecret: NewSecret("installation")})
	store.slots[SlotA] = encoded
	sink := &warningSink{}
	provider := NewProvider(ProviderConfig{Store: store, Transport: &fakeTransport{refreshErr: errors.New("private failure")}, Clock: fixedClock{now}, Coordinator: &mutexCoordinator{}, Diagnostics: sink, Gates: closedAuthGates("refreshToken")})
	authorization, err := provider.Acquire(context.Background())
	if err != nil || authorization.AccessToken.Reveal() != "still-valid" {
		t.Fatalf("Acquire = %q, %v", authorization.AccessToken.Reveal(), err)
	}
	if len(sink.warnings) != 1 || sink.warnings[0].State != "using_valid_token" {
		t.Fatalf("warnings = %+v", sink.warnings)
	}
}

func TestAcquireFailsHardWhenExpiredRefreshFails(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{}}
	encoded, _ := EncodeCredential(Credential{SchemaVersion: 1, Generation: 1, AccountIdentity: NewSecret("account"), AccessToken: NewSecret("expired"), RefreshToken: NewSecret("refresh"), AccessTokenExpires: &expired, InstallationSecret: NewSecret("installation")})
	store.slots[SlotA] = encoded
	provider := NewProvider(ProviderConfig{Store: store, Transport: &fakeTransport{refreshErr: errors.New("private failure")}, Clock: fixedClock{now}, Coordinator: &mutexCoordinator{}, Gates: closedAuthGates("refreshToken")})
	_, err := provider.Acquire(context.Background())
	if !IsErrorType(err, domain.ErrorAuthExpired) {
		t.Fatalf("Acquire error = %v", err)
	}
}

func TestLogoutIsLocalAndIdempotent(t *testing.T) {
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{SlotA: []byte("old"), SlotB: []byte("old")}}
	transport := &fakeTransport{}
	provider := NewProvider(ProviderConfig{Store: store, CleanupStores: []CredentialStore{store}, Transport: transport, Clock: fixedClock{}, Coordinator: &mutexCoordinator{}})
	for range 2 {
		state, err := provider.Logout(context.Background())
		if err != nil || state.Authenticated || state.TokenState != domain.TokenStateMissing {
			t.Fatalf("Logout = %+v, %v", state, err)
		}
	}
	if len(transport.calls) != 0 {
		t.Fatalf("transport calls = %v", transport.calls)
	}
}

func TestSecretFormattingAndErrorsDoNotExposeValues(t *testing.T) {
	secret := NewSecret("generated-secret")
	if secret.String() == secret.Reveal() {
		t.Fatal("String exposed secret")
	}
	private := errors.New("raw backend generated-secret")
	if got := persistenceError("file").Error(); got == private.Error() || got == secret.Reveal() {
		t.Fatalf("public error exposed private value: %q", got)
	}
}

func TestLogoutVerifiesDeletionAndClearsBackendMarker(t *testing.T) {
	store := &memoryStore{backend: "memory", slots: map[Slot][]byte{SlotA: []byte("old")}, ignoreDelete: true}
	clears := 0
	provider := NewProvider(ProviderConfig{Store: store, Transport: &fakeTransport{}, Clock: fixedClock{}, Coordinator: &mutexCoordinator{}, ClearBackendMarker: func() error { clears++; return nil }})
	_, err := provider.Logout(context.Background())
	if !IsErrorType(err, domain.ErrorAuthPersistenceFailed) {
		t.Fatalf("Logout error = %v", err)
	}
	if clears != 0 {
		t.Fatalf("marker cleared before verified deletion")
	}
	store.ignoreDelete = false
	if _, err := provider.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if clears != 1 {
		t.Fatalf("marker clears = %d, want 1", clears)
	}
}
