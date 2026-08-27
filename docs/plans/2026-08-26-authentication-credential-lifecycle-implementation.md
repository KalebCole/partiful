# Authentication and Credential Lifecycle Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Build and verify the single shared authentication foundation used by the Partiful CLI and future MCP tools, including terminal SMS login, recoverable credential persistence, refresh coordination, status, logout, safe retries, and redaction.

**Architecture:** A package-local `CredentialProvider` owns all credential lifecycle behavior. CLI commands and the MCP authentication gate depend on that provider through narrow interfaces; they never read or refresh credentials directly. Two credential slots, a generation number, a process-local singleflight group, and an OS-owned cross-process lock make credential rotation deterministic and crash-tolerant.

**Tech Stack:** Go 1.26; standard `net/http`, `encoding/json`, and `testing`; Cobra v1.10.2; `zalando/go-keyring` v0.2.8; `gofrs/flock` v0.13.0; `golang.org/x/sync` v0.22.0; `golang.org/x/term` v0.45.0; OpenAPI 3.1 source contract.

**Normative inputs:**

- `docs/plans/2026-08-26-authentication-credential-lifecycle-design.md`
- `docs/command-model.md`
- `docs/architecture.md`
- `evidence/research/authentication.md`
- `spec/partiful.openapi.json`

**Scope boundary:** This plan builds authentication and its adapter seams. It does not register the other 21 domain MCP tools or implement domain operations. `auth login` remains CLI-only. Tests use scripted transports and disposable stores; they send no SMS and perform no live Partiful mutation.

---

## Task 1: Bootstrap the Go module and executable seam

**Objective:** Create a compilable module with dependency injection from `main` into the CLI root.

**Files:**

- Create: `go.mod`
- Create: `cmd/partiful/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`

**Step 1: Initialize the module and pin dependencies**

Run:

```bash
go mod init github.com/KalebCole/partiful
go mod edit -go=1.26.0
go get github.com/spf13/cobra@v1.10.2
go get github.com/zalando/go-keyring@v0.2.8
go get github.com/gofrs/flock@v0.13.0
go get golang.org/x/sync@v0.22.0
go get golang.org/x/term@v0.45.0
```

Expected: `go.mod` and `go.sum` exist with the named direct dependencies.

**Step 2: Write the failing root test**

Test a `NewRoot(Dependencies)` constructor that:

- is silent during construction;
- writes command results to injected stdout;
- writes diagnostics to injected stderr;
- exposes `--json`, `--plain`, and `--no-input` as persistent flags;
- rejects `--json --plain` with exit classification `usage.invalid`.

Use this dependency seam:

```go
type Dependencies struct {
    In     io.Reader
    Out    io.Writer
    ErrOut io.Writer
    IsTTY  func() bool
}

func NewRoot(deps Dependencies) *cobra.Command
```

**Step 3: Verify the red test**

Run:

```bash
go test ./internal/cli -run TestRoot -count=1
```

Expected: FAIL because `NewRoot` does not exist.

**Step 4: Implement the minimal root and main**

`main` calls `cli.NewRoot(cli.SystemDependencies()).ExecuteContext(context.Background())`. Keep process exit-code mapping in `main`; do not call `os.Exit` from packages or tests.

**Step 5: Verify green and build**

Run:

```bash
go test ./internal/cli -run TestRoot -count=1
go build ./cmd/partiful
```

Expected: PASS and a successful build.

**Step 6: Commit**

```bash
git add go.mod go.sum cmd/partiful/main.go internal/cli/root.go internal/cli/root_test.go
git commit -m "build: bootstrap partiful command"
```

---

## Task 2: Define stable authentication errors and deterministic time

**Objective:** Establish one error vocabulary and a fakeable clock before lifecycle code uses either.

**Files:**

- Create: `internal/auth/errors.go`
- Create: `internal/auth/errors_test.go`
- Create: `internal/auth/clock.go`
- Create: `internal/auth/clock_test.go`

**Step 1: Write failing table tests**

Cover exactly these stable error types:

```go
const (
    ErrRequired          ErrorType = "auth.required"
    ErrExpired           ErrorType = "auth.expired"
    ErrHumanRequired     ErrorType = "auth.human_required"
    ErrStoreUnavailable  ErrorType = "auth.store_unavailable"
    ErrPersistenceFailed ErrorType = "auth.persistence_failed"
    ErrBusy              ErrorType = "auth.busy"
)
```

Require safe fields only:

```go
type Error struct {
    Type        ErrorType
    Message     string
    Retryable   bool
    BackendKind string
    Remediation string
    Cause       error // never serialized
}
```

Test that `errors.Is` works by type and that public JSON excludes `Cause`, paths, key names, and account identity.

Define:

```go
type Clock interface { Now() time.Time }
type FakeClock struct { /* mutex-protected current time */ }
```

**Step 2: Verify red**

```bash
go test ./internal/auth -run 'Test(Error|FakeClock)' -count=1
```

Expected: FAIL because the package is absent.

**Step 3: Implement the smallest types and safe serializer**

Do not make raw backend errors directly JSON-marshallable. Add `PublicError(err error) PublicErrorEnvelope` as the only adapter serializer.

**Step 4: Verify green**

```bash
go test ./internal/auth -run 'Test(Error|FakeClock)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/errors.go internal/auth/errors_test.go internal/auth/clock.go internal/auth/clock_test.go
git commit -m "feat: define authentication error contract"
```

---

## Task 3: Add the private credential record and codecs

**Objective:** Separate persistence serialization from public and diagnostic output.

**Files:**

- Create: `internal/auth/credential.go`
- Create: `internal/auth/codec.go`
- Create: `internal/auth/codec_test.go`

**Step 1: Write failing codec tests**

Use a private record with no exported JSON tags:

```go
type Credential struct {
    SchemaVersion      int
    Generation         uint64
    AccountID          string
    AccountDisplay     string
    AccessToken        string
    RefreshToken       string
    ExpiresAt          *time.Time
    InstallationSecret [32]byte
}
```

Require:

- schema version `1` only;
- generation greater than zero;
- non-empty account ID, access token, refresh token, and installation secret;
- round-trip equality through `Codec.Encode` and `Codec.Decode`;
- unknown expiry remains `nil`;
- unknown schema versions fail closed;
- `fmt.Sprint(credential)` and public JSON do not reveal a generated secret corpus.

Define a dedicated private interface:

```go
type Codec interface {
    Encode(Credential) ([]byte, error)
    Decode([]byte) (Credential, error)
}
```

**Step 2: Verify red**

```bash
go test ./internal/auth -run 'TestCredential|TestCodec' -count=1
```

Expected: FAIL.

**Step 3: Implement a versioned private JSON wire type**

Keep the wire struct unexported inside `codec.go`. Implement `Credential.String()` as `[credential redacted]`. Never give `Credential` public JSON tags.

**Step 4: Verify green**

```bash
go test ./internal/auth -run 'TestCredential|TestCodec' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/credential.go internal/auth/codec.go internal/auth/codec_test.go
git commit -m "feat: add private credential codec"
```

---

## Task 4: Implement the backend-neutral two-slot protocol

**Objective:** Prove failed persistence cannot destroy the active generation.

**Files:**

- Create: `internal/auth/store.go`
- Create: `internal/auth/store_test.go`
- Create: `internal/auth/store_fake_test.go`

**Step 1: Write failing contract tests around a raw slot backend**

Use two fixed slots and one narrow raw interface:

```go
type Slot string
const ( SlotA Slot = "a"; SlotB Slot = "b" )

type SlotBackend interface {
    Kind() string
    ReadSlot(context.Context, Slot) ([]byte, error)
    WriteSlot(context.Context, Slot, []byte) error
    DeleteSlot(context.Context, Slot) error
}

type CredentialStore struct {
    backend SlotBackend
    codec   Codec
}
```

Contract cases:

- no slots returns a typed missing result, not corruption;
- one valid slot loads;
- two valid slots select the higher generation;
- equal generations with unequal records fail closed;
- invalid lower slot does not hide a valid higher slot;
- invalid only slot is `auth.store_unavailable`;
- commit writes the inactive or lower-generation slot;
- empty, truncated, mismatched, or invalid read-back fails;
- failed commit leaves the old generation loadable;
- successful commit becomes the highest valid generation;
- generation overflow fails before write.

**Step 2: Verify red**

```bash
go test ./internal/auth -run TestCredentialStore -count=1
```

Expected: FAIL.

**Step 3: Implement `Load`, `Commit`, and `DeleteBoth`**

`Commit` must:

```go
func (s *CredentialStore) Commit(ctx context.Context, next Credential) error {
    active, slot, err := s.loadWithSlot(ctx)
    // Handle missing as generation zero.
    // Require next.Generation == active.Generation+1.
    // Write only the other/lower slot.
    // Read it back and compare exact encoded bytes.
    // Decode again before returning nil.
}
```

Do not silently repair or delete an invalid slot during a read.

**Step 4: Verify green and race safety**

```bash
go test ./internal/auth -run TestCredentialStore -count=1
go test -race ./internal/auth -run TestCredentialStore -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/store.go internal/auth/store_test.go internal/auth/store_fake_test.go
git commit -m "feat: add recoverable credential store"
```

---

## Task 5: Add bounded OS credential-store operations

**Objective:** Use the platform store without allowing a hidden prompt to hang CLI or MCP processes.

**Files:**

- Create: `internal/auth/store_keyring.go`
- Create: `internal/auth/store_keyring_test.go`

**Step 1: Write failing timeout and classification tests**

Wrap `go-keyring` behind an internal driver so tests never touch the real keychain:

```go
type KeyringDriver interface {
    Get(service, user string) (string, error)
    Set(service, user, value string) error
    Delete(service, user string) error
}
```

Test every operation for:

- normal success;
- key-not-found;
- explicit denial;
- unsupported/unavailable backend;
- driver blocks forever but wrapper returns by the configured deadline;
- after a timeout, 25 more wrapper calls produce no second underlying driver
  call, proving the process-local circuit breaker prevents O(N) leaked calls;
- errors contain backend kind `os` but no service name or slot key.

Use 30 seconds on macOS and 10 seconds elsewhere in production. Inject a 25 ms timeout in tests and assert return within 250 ms.

**Step 2: Verify red**

```bash
go test ./internal/auth -run TestKeyring -count=1
```

Expected: FAIL.

**Step 3: Implement the adapter**

Use service `partiful` and opaque fixed users for slots A and B. Keep those names private. One goroutine may remain blocked after a driver timeout; trip the adapter breaker so an MCP process cannot accumulate blocked goroutines.

Platform error classification must be explicit and table-tested. Unknown errors are unavailable, not not-found. Denial never triggers fallback. Use an atomic counter in the blocking fake driver and require an exact final count of one; `go test -race` alone does not detect goroutine leaks.

**Step 4: Verify green and race safety**

```bash
go test ./internal/auth -run TestKeyring -count=1
go test -race ./internal/auth -run TestKeyring -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/store_keyring.go internal/auth/store_keyring_test.go
git commit -m "feat: bound OS credential store operations"
```

---

## Task 6: Add the protected file fallback and backend selection

**Objective:** Provide a deterministic fallback without mislabeling it as encrypted.

**Files:**

- Create: `internal/auth/store_file.go`
- Create: `internal/auth/store_file_test.go`
- Create: `internal/auth/store_select.go`
- Create: `internal/auth/store_select_test.go`

**Step 1: Write failing filesystem tests in `t.TempDir()`**

Require:

- private directory mode `0700` and file mode `0600` on POSIX;
- both fixed slots live under the platform user-data directory;
- no credential goes under a dotfile-sync configuration directory;
- `lstat` rejects symlinks, hard links, directories, devices, and FIFOs;
- writes use a same-directory temporary file, file sync, atomic rename, and directory sync where supported;
- a failed temporary write or rename preserves the prior slot bytes;
- diagnostics say `permission-protected file`, never `encrypted`;
- tests do not weaken permissions to make an assertion pass.

**Step 2: Write failing selector tests**

Define outcomes `available`, `missing`, `unsupported`, `unavailable`, and `denied`. Test:

- OS available selects OS with no warning;
- OS unsupported/unavailable during first selection selects file and emits one warning;
- denial fails without fallback;
- an existing OS marker plus later OS failure does not switch to file;
- marker absent probes OS before file;
- marker contains only backend kind;
- marker is atomically written and symlink-safe.

**Step 3: Verify red**

```bash
go test ./internal/auth -run 'Test(File|StoreSelection)' -count=1
```

Expected: FAIL.

**Step 4: Implement the file backend and selector**

Use one tested `dataDir()` helper:

- macOS: `os.UserConfigDir()` resolves to `~/Library/Application Support`;
- Windows: `os.UserConfigDir()` resolves to the user's AppData directory;
- Linux: `$XDG_DATA_HOME` when set, otherwise `~/.local/share`.

Put credential slots under `<dataDir>/partiful/credentials/`. Do not use
`os.UserCacheDir()` because credentials are not disposable cache data. Keep the
non-secret backend marker under the normal platform configuration directory.

Create one `WarningSink` interface for the selector. Do not let storage code print directly.

**Step 5: Verify green**

```bash
go test ./internal/auth -run 'Test(File|StoreSelection)' -count=1
go test -race ./internal/auth -run 'Test(File|StoreSelection)' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/auth/store_file.go internal/auth/store_file_test.go internal/auth/store_select.go internal/auth/store_select_test.go
git commit -m "feat: add protected credential fallback"
```

---

## Task 7: Coordinate refresh across goroutines and processes

**Objective:** Ensure one refresh wins and all waiters re-read its committed generation.

**Files:**

- Create: `internal/auth/lock.go`
- Create: `internal/auth/lock_test.go`
- Create: `internal/auth/coordinator.go`
- Create: `internal/auth/coordinator_test.go`

**Step 1: Write failing lock tests**

Define:

```go
type Unlock func() error
type Locker interface {
    Lock(context.Context) (Unlock, error)
}
```

Test:

- `gofrs/flock` lock acquisition is context-bounded;
- timeout maps to `auth.busy` and is retryable;
- cancel releases no unacquired lock;
- every acquired lock is released after success, error, and panic recovery;
- a child helper process that exits while holding the lock allows a second process to acquire it;
- no PID marker is used as ownership authority.

**Step 2: Write failing singleflight tests**

`RefreshCoordinator.Do(ctx, fn)` must combine `singleflight.Group` and `Locker`. Use barriers to start 50 callers. Assert one `fn` call and the same result for all callers.

**Step 3: Verify red**

```bash
go test ./internal/auth -run 'Test(FileLock|RefreshCoordinator)' -count=1
```

Expected: FAIL.

**Step 4: Implement with unconditional unlock**

Use a named return plus `defer` that always invokes `Unlock`. Do not use persistent lockfiles as stale-lock records; the file may exist, but ownership comes only from the open OS lock handle.

**Step 5: Verify green and race safety**

```bash
go test ./internal/auth -run 'Test(FileLock|RefreshCoordinator)' -count=1
go test -race ./internal/auth -run RefreshCoordinator -count=1
```

Expected: PASS and one refresh invocation.

**Step 6: Commit**

```bash
git add internal/auth/lock.go internal/auth/lock_test.go internal/auth/coordinator.go internal/auth/coordinator_test.go
git commit -m "feat: coordinate credential refresh"
```

---

## Task 8: Implement the evidenced authentication transport

**Objective:** Encode the five observed authentication operations without exposing raw secret-bearing responses.

**Files:**

- Create: `internal/auth/transport.go`
- Create: `internal/auth/transport_http.go`
- Create: `internal/auth/transport_http_test.go`
- Create: `internal/auth/testdata/*.json`

**Step 1: Define typed transport values and write failing tests**

```go
type Session struct {
    AccountID, AccountDisplay string
    AccessToken, RefreshToken string
    ExpiresAt *time.Time
}

type AuthTransport interface {
    SendCode(context.Context, string) error
    ExchangeCode(context.Context, string, string) (string, error) // phone, SMS code
    ExchangeCustomToken(context.Context, string) (Session, error)
    Refresh(context.Context, string) (Session, error)
}
```

Drive `httptest.Server` with captured, sanitized fixtures. Cover:

- `sendAuthCodeTrusted` request and HTTP 200 acceptance without inspecting or
  claiming a response body field;
- `getLoginToken` request and response;
- `accounts:signInWithCustomToken` request and response;
- `accounts:lookup` account binding;
- `v1/token` refresh response;
- `Referer: https://partiful.com/` on every Firebase request;
- missing referer fixture maps `API_KEY_HTTP_REFERRER_BLOCKED` to a transport contract error, not `auth.expired`;
- missing token fields fail closed;
- unknown expiry remains unknown;
- raw response body, phone, SMS code, custom token, and refresh token never appear in returned error text.

**Step 2: Verify red**

```bash
go test ./internal/auth -run TestHTTPAuthTransport -count=1
```

Expected: FAIL.

**Step 3: Implement minimal HTTP calls from the OpenAPI contract**

Use operation IDs and schemas from `spec/partiful.openapi.json`. Do not invent browser flow, rate limits, revocation, token lifetime, or refresh-token reuse behavior. Bound each request with its context.

`sendAuthCodeTrusted` supplies no evidenced challenge value. `ExchangeCode`
must send the same caller-provided phone number with the SMS code to
`getLoginToken`; do not add a server-issued verification ID to the model.

**Step 4: Verify green**

```bash
go test ./internal/auth -run TestHTTPAuthTransport -count=1
```

Expected: PASS with all requests served locally.

**Step 5: Commit**

```bash
git add internal/auth/transport.go internal/auth/transport_http.go internal/auth/transport_http_test.go internal/auth/testdata
git commit -m "feat: implement authentication transport"
```

---

## Task 9: Implement verified interactive login in the provider

**Objective:** Replace an account only after a complete session is persisted and read back.

**Files:**

- Create: `internal/auth/provider.go`
- Create: `internal/auth/provider_login_test.go`

**Step 1: Write failing login tests**

Construct `Provider` from interfaces only:

```go
type Provider struct {
    store       *CredentialStore
    transport   AuthTransport
    coordinator *RefreshCoordinator
    clock       Clock
    random      io.Reader
    diagnostics DiagnosticSink
}

type DiagnosticSink interface {
    Emit(DiagnosticEvent)
}

// Keep the initial event minimal. Task 13 defines the final allowlisted fields
// and production sink without changing Provider's dependency direction.
type DiagnosticEvent struct {
    Kind      string
    Operation string
}

type LoginInput struct { Phone, SMSCode string }
func (p *Provider) Login(context.Context, LoginInput) (Status, error)
```

Test:

- transport failure leaves existing generation unchanged;
- empty access token, refresh token, or account ID fails before commit;
- installation secret uses injected cryptographic randomness;
- lock acquisition is followed by a store re-read;
- next generation is active generation plus one;
- failed inactive-slot read-back preserves old account;
- success replaces old account;
- success status contains no account ID or tokens;
- a crash-simulation after verified slot write resolves the new highest generation on restart.

**Step 2: Verify red**

```bash
go test ./internal/auth -run TestProviderLogin -count=1
```

Expected: FAIL.

**Step 3: Implement the login transaction**

Run network exchanges before taking the cross-process lock. After taking it, re-read generation, build the final record, and commit. Do not publish success or cache the session before verified persistence.

**Step 4: Verify green and race safety**

```bash
go test ./internal/auth -run TestProviderLogin -count=1
go test -race ./internal/auth -run TestProviderLogin -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/provider.go internal/auth/provider_login_test.go
git commit -m "feat: commit login credentials atomically"
```

---

## Task 10: Implement token acquisition and refresh rotation

**Objective:** Return healthy tokens cheaply and rotate expiring tokens once across concurrent processes.

**Files:**

- Modify: `internal/auth/provider.go`
- Create: `internal/auth/provider_refresh_test.go`

**Step 1: Write failing refresh-state tests**

Use a fixed fake clock. Cover:

- healthy known-expiry token: no refresh;
- token expiring within five minutes: refresh;
- unknown expiry: no invented expiry and no automatic network health check;
- early refresh failure while token remains valid: return old token and one safe warning;
- expired token plus refresh failure: hard `auth.expired` or classified remote error;
- complete rotation is committed before token return;
- persistence failure never publishes the new access token;
- remote/local crash gap maps a rejected old refresh token to `auth.required` with login remediation;
- 50 concurrent callers invoke one refresh;
- waiter re-read detects a newer healthy generation and skips its own refresh.

**Step 2: Verify red**

```bash
go test ./internal/auth -run 'TestProvider(Acquire|Refresh)' -count=1
```

Expected: FAIL.

**Step 3: Implement `Acquire`**

```go
type Access struct {
    Token      string
    AccountID  string
    Generation uint64
}

func (p *Provider) Acquire(ctx context.Context) (Access, error)
```

Keep `Access` internal and redacted. The adapter transport receives it directly; it is never serialized. Inside the coordinator, re-read after lock acquisition before deciding to call refresh.

**Step 4: Verify green and stress concurrency**

```bash
go test ./internal/auth -run 'TestProvider(Acquire|Refresh)' -count=1
go test -race ./internal/auth -run 'TestProvider(Acquire|Refresh)' -count=20
```

Expected: PASS; refresh count remains one per generation.

**Step 5: Commit**

```bash
git add internal/auth/provider.go internal/auth/provider_refresh_test.go
git commit -m "feat: refresh shared credentials safely"
```

---

## Task 11: Add evidence-gated request retry

**Objective:** Retry only the exact proven read rejection and never retry an ambiguous mutation.

**Files:**

- Create: `internal/auth/retry.go`
- Create: `internal/auth/retry_test.go`

**Step 1: Write failing classifier tests**

Define explicit operation metadata:

```go
type OperationAuthPolicy struct {
    Name              string
    ReadOnly          bool
    ProvenRejections  []RejectionClassifier
    NonExecutionProof bool
    IdempotencyKey    bool
}
```

Initial registry: only `getContacts` has the proven HTTP `401` plus callable status `UNAUTHENTICATED` classifier. Test:

- exact `getContacts` rejection refreshes and retries once;
- generic `401` does not qualify;
- Firebase error, timeout, connection reset, `429`, and `5xx` do not qualify;
- every mutation policy refuses post-dispatch retry;
- even a qualifying read rejection retries at most once;
- retry does not occur if refresh persistence fails.

**Step 2: Verify red**

```bash
go test ./internal/auth -run TestAuthenticatedRetry -count=1
```

Expected: FAIL.

**Step 3: Implement policy-driven retry**

Do not infer read/write from HTTP method. Accept operation metadata from the executable operation registry. Adding another classifier must require an explicit registry entry and test fixture.

**Step 4: Verify green**

```bash
go test ./internal/auth -run TestAuthenticatedRetry -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/retry.go internal/auth/retry_test.go
git commit -m "feat: gate authenticated request retries"
```

---

## Task 12: Implement status and complete local logout

**Objective:** Expose usable-session state and delete every known local credential copy without claiming revocation.

**Files:**

- Modify: `internal/auth/provider.go`
- Create: `internal/auth/provider_status_test.go`
- Create: `internal/auth/provider_logout_test.go`

**Step 1: Write failing status tests**

Use this public type:

```go
type TokenState string
const (
    TokenHealthy  TokenState = "healthy"
    TokenExpiring TokenState = "expiring"
    TokenExpired  TokenState = "expired"
    TokenMissing  TokenState = "missing"
    TokenUnknown  TokenState = "unknown"
)

type Status struct {
    Authenticated bool       `json:"authenticated"`
    TokenState    TokenState `json:"token_state"`
    ExpiresAt     *time.Time `json:"expires_at"`
}
```

Test healthy, expiring with refresh, expired, missing, unknown expiry, inaccessible store, and corrupt store. Missing is normal status; inaccessible or corrupt is not missing.

**Step 2: Write failing logout tests**

Test:

- both slots are removed from selected backend;
- safe cleanup is attempted in the other backend and declared legacy locations;
- backend marker and in-process snapshots are cleared;
- repeated logout succeeds;
- partial deletion is `auth.persistence_failed`;
- safe details name backend kinds only;
- no network request occurs;
- output never says `revoked`.

**Step 3: Verify red**

```bash
go test ./internal/auth -run 'TestProvider(Status|Logout)' -count=1
```

Expected: FAIL.

**Step 4: Implement status and logout under the shared lock**

`Status` uses normal refresh rules. `Logout` deletes local state only and reports success only after all existing known copies are gone.

**Step 5: Verify green**

```bash
go test ./internal/auth -run 'TestProvider(Status|Logout)' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/auth/provider.go internal/auth/provider_status_test.go internal/auth/provider_logout_test.go
git commit -m "feat: add auth status and local logout"
```

---

## Task 13: Add secret-safe diagnostics and corpus tests

**Objective:** Make accidental credential disclosure mechanically difficult across every output channel.

**Files:**

- Create: `internal/auth/diagnostics.go`
- Create: `internal/auth/redaction_test.go`
- Create: `internal/testutil/secretcorpus.go`

**Step 1: Write the failing generated-corpus test**

Generate unique values for access token, refresh token, custom token, SMS code, phone, cookie, authorization header, installation secret, and raw response body. Feed them through:

- credential formatting;
- public error JSON;
- diagnostic sink;
- warning sink;
- HTTP failure formatting;
- panic recovery formatting;
- test helper failure messages.

Assert none of the raw or URL/base64-encoded forms appears.

**Step 2: Write failing allowlist tests**

Diagnostics may contain only method, classified host, operation ID, status, duration, and safe correlation metadata. Headers and bodies are excluded by default.

```go
type HTTPDiagnostic struct {
    Operation string
    HostClass string
    Method    string
    Status    int
    Duration  time.Duration
    Kind      string
}
```

The concrete sink converts `HTTPDiagnostic` to the already-declared minimal
`DiagnosticEvent`. Neither type may accept `Credential`, `Access`, raw headers,
or raw bodies at compile time.

**Step 3: Verify red**

```bash
go test ./internal/auth -run 'Test(Redaction|Diagnostic)' -count=1
```

Expected: FAIL.

**Step 4: Implement allowlist-only diagnostics**

Extend the provider-era event declaration only through safe scalar fields, or
keep transport diagnostics as a separate `HTTPDiagnostic` converted by the
sink. Do not redeclare `DiagnosticSink`, `DiagnosticEvent`, or another symbol
that would conflict with `provider.go`. Redact before formatting. Do not build
a blacklist of token field names. Preserve ordinary application-visible event
data outside this auth-only diagnostic path.

**Step 5: Verify green and scan source output**

```bash
go test ./internal/auth -run 'Test(Redaction|Diagnostic)' -count=1
go test ./internal/auth -run TestRedaction -count=50
```

Expected: PASS across generated corpora.

**Step 6: Commit**

```bash
git add internal/auth/diagnostics.go internal/auth/redaction_test.go internal/testutil/secretcorpus.go
git commit -m "test: enforce authentication redaction"
```

---

## Task 14: Wire the three CLI auth commands and private prompts

**Objective:** Deliver login, status, and logout through the shared provider with gogcli-style stdout/stderr discipline.

**Files:**

- Create: `internal/cli/auth.go`
- Create: `internal/cli/auth_test.go`
- Create: `internal/cli/terminal.go`
- Create: `internal/cli/terminal_test.go`
- Modify: `internal/cli/root.go`
- Modify: `cmd/partiful/main.go`

**Step 1: Write failing command tests**

Inject a fake provider. Cover:

- exactly `auth login`, `auth status`, and `auth logout` are registered;
- login rejects `--no-input`, non-TTY stdin, and MCP context before any transport call;
- prompts go to stderr;
- phone and SMS code never go to stdout, stderr, arguments, environment, or errors;
- SMS input uses `term.ReadPassword` through an injected terminal interface;
- `--json` success exactly matches the command model;
- human output contains no secrets or private IDs;
- classified failure leaves stdout empty and writes safe JSON to stderr under `--json`;
- `--json --plain` fails before provider invocation;
- logout says local credentials removed, never remote session revoked.

Use one adapter interface:

```go
type AuthOperations interface {
    Login(context.Context, auth.LoginInput) (auth.Status, error)
    Status(context.Context) (auth.Status, error)
    Logout(context.Context) (auth.Status, error)
}
```

**Step 2: Verify red**

```bash
go test ./internal/cli -run TestAuthCommands -count=1
```

Expected: FAIL.

**Step 3: Implement prompt and command adapters**

The CLI owns prompting only. It validates terminal state and gathers private values, then calls `Provider.Login`. No prompt logic enters `internal/auth`.

**Step 4: Verify green and build**

```bash
go test ./internal/cli -run 'Test(AuthCommands|Terminal)' -count=1
go build ./cmd/partiful
```

Expected: PASS and successful build.

**Step 5: Commit**

```bash
git add internal/cli/auth.go internal/cli/auth_test.go internal/cli/terminal.go internal/cli/terminal_test.go internal/cli/root.go cmd/partiful/main.go
git commit -m "feat: add CLI authentication commands"
```

---

## Task 15: Add the reusable MCP authentication gate

**Objective:** Let the future MCP server start unauthenticated and give protected tools exact remediation without adding an interactive login tool.

**Files:**

- Create: `internal/mcpauth/gate.go`
- Create: `internal/mcpauth/gate_test.go`
- Create: `internal/mcpauth/registry_guard_test.go`

**Step 1: Write failing gate tests**

Define no MCP SDK dependency yet:

```go
type CredentialSource interface {
    Acquire(context.Context) (auth.Access, error)
    Status(context.Context) (auth.Status, error)
    Logout(context.Context) (auth.Status, error)
}

type Gate struct { credentials CredentialSource }
func (g *Gate) Authorize(ctx context.Context, protected bool) (auth.Access, error)
```

Test:

- process construction succeeds with missing, expired, or inaccessible credentials;
- public tools bypass acquisition;
- protected tools with no usable credentials return exact `auth.required` remediation: `Run partiful auth login in a private terminal, then retry.`;
- protected tools use the same provider generation as CLI status/logout;
- a newer stored generation invalidates any prior in-memory snapshot;
- logout causes the next protected call to fail;
- no symbol or registry row can register `auth_login` as an MCP tool.

**Step 2: Verify red**

```bash
go test ./internal/mcpauth -count=1
```

Expected: FAIL.

**Step 3: Implement the gate and static registry guard**

Keep the gate SDK-neutral. The later MCP server task maps its typed errors to protocol-native structured errors. Do not create a generic argv execution tool.

**Step 4: Verify green**

```bash
go test ./internal/mcpauth -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/mcpauth/gate.go internal/mcpauth/gate_test.go internal/mcpauth/registry_guard_test.go
git commit -m "feat: add MCP authentication gate"
```

---

## Task 16: Prove cross-adapter lifecycle and failure behavior

**Objective:** Verify the composed provider, CLI adapter, and MCP gate observe one credential history.

**Files:**

- Create: `internal/integration/auth_lifecycle_test.go`
- Create: `internal/integration/auth_concurrency_test.go`
- Create: `internal/integration/auth_subprocess_test.go`

**Step 1: Write the composed lifecycle test**

Use a scripted transport and disposable file backend:

1. MCP gate starts with no credential.
2. Protected call returns `auth.required`.
3. CLI login commits generation 1.
4. CLI status and MCP status return the same public state.
5. Advance fake clock into the refresh window.
6. Concurrent CLI and MCP acquisition results in one refresh and generation 2.
7. Logout from CLI deletes both slots.
8. MCP protected call returns `auth.required` again.

Assert no secret corpus appears in captured stdout, stderr, diagnostics, or test errors.

**Step 2: Write cross-process lock subprocess tests**

Use the current test binary as a helper process. Two processes share a temporary credential directory and lock. Assert one process commits the rotation, the waiter re-reads it, and both finish without a stale lock. Add a helper that exits while holding the lock, then prove recovery.

**Step 3: Verify red**

```bash
go test ./internal/integration -count=1
```

Expected: FAIL until all production constructors are wired.

**Step 4: Add only the missing composition wiring**

Create production constructors in existing files. Do not duplicate provider or store logic in the integration package.

**Step 5: Verify green and stress**

```bash
go test ./internal/integration -count=1
go test -race ./internal/integration -count=20
```

Expected: PASS; exactly one refresh per generation and no secret output.

**Step 6: Commit**

```bash
git add internal/integration internal/auth internal/cli internal/mcpauth cmd/partiful/main.go
git commit -m "test: prove shared authentication lifecycle"
```

---

## Task 17: Add mechanical contract verification and operator documentation

**Objective:** Make authentication invariants auditable in CI and document honest storage behavior.

**Files:**

- Create: `scripts/verify_auth_contract.py`
- Create: `docs/authentication.md`
- Modify: `README.md`
- Modify: `scripts/verify_command_model.py`

**Step 1: Write the failing verifier**

The verifier must inspect source and executable help to assert:

- one production `CredentialProvider` constructor;
- CLI has three auth command paths;
- no MCP `auth_login` registration;
- stable public auth error set is exact;
- both credential slots exist for each backend;
- OS store adapter wraps read, write, and delete with timeouts;
- retry registry has no mutation marked eligible;
- all local Markdown links resolve.

Do not use text counts as proof of runtime behavior when a Go test can prove it. The script runs the named Go contract tests and fails if any select zero tests.

**Step 2: Verify red**

```bash
python3 scripts/verify_auth_contract.py
```

Expected: FAIL until the verifier and docs are complete.

**Step 3: Write honest user documentation**

Document:

- private terminal login;
- one active account;
- platform credential store default;
- permission-protected, not encrypted, fallback warning;
- `auth status` refresh behavior;
- local-only logout and no remote revocation claim;
- MCP remediation flow;
- safe troubleshooting by stable error type.

Never document token import, browser login, multiple accounts, generic environment credentials, mutation retry, or remote revocation.

**Step 4: Verify green**

```bash
python3 scripts/verify_auth_contract.py
python3 scripts/verify_command_model.py
```

Expected: both print `PASS` and non-zero test counts.

**Step 5: Commit**

```bash
git add scripts/verify_auth_contract.py scripts/verify_command_model.py docs/authentication.md README.md
git commit -m "docs: verify authentication contract"
```

---

## Task 18: Run the full release gate on a clean tree

**Objective:** Prove the authentication foundation builds, tests, races, vets, and matches its documents before closing issue #10.

**Files:** None unless a failing gate reveals a scoped defect.

**Step 1: Run format and generated-file checks**

```bash
test -z "$(gofmt -l cmd internal)"
go mod tidy
git diff --exit-code -- go.mod go.sum
```

Expected: no unformatted paths and no uncommitted module drift. If `go mod tidy` changes module files, inspect and commit only those files before continuing.

**Step 2: Run static and unit checks**

```bash
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
```

Expected: all exit 0 with non-zero test counts.

**Step 3: Run mechanical contract checks**

```bash
python3 scripts/verify_auth_contract.py
python3 scripts/verify_command_model.py
git diff --check
```

Expected: both verifiers print `PASS`; whitespace check is empty.

**Step 4: Exercise the real binary without live authentication**

```bash
go build -o ./build/partiful ./cmd/partiful
./build/partiful auth status --json
./build/partiful auth login --no-input --json
```

Expected:

- status returns valid missing-session JSON without a network request;
- login fails as `auth.human_required`, leaves stdout empty, and gives terminal remediation on stderr;
- neither command prints a phone number, token, key name, or credential path.

Do not perform a live SMS login as part of this issue.

**Step 5: Run a final gogcli-focused rubber-duck review**

Freeze the final committed implementation, tests, design, and these concrete gogcli reference files:

- `docs/spec.md`
- `docs/paths.md`
- `docs/commands/gog-auth-doctor.md`
- `internal/secrets/store.go`
- `internal/secrets/timeout_keyring.go`
- `internal/secrets/keyring_lock.go`

Ask the read-only Copilot critic only for blockers in timeout bounds, verified persistence, headless behavior, stdout/stderr discipline, and auth diagnostics. Adjudicate each finding. Convert accepted findings into regression tests before fixes, then rerun the affected gate and this full gate.

**Step 6: Inspect and commit any final scoped correction**

```bash
git status --short
git diff --check
git add <only-authentication-paths-changed>
git diff --cached --check
git diff --cached --name-only
git diff --cached
git commit -m "fix: harden authentication lifecycle"
```

Skip this commit when there is no correction.

**Step 7: Verify final repository state**

```bash
git status --short
git log -1 --oneline
```

Expected: clean working tree and a reviewed authentication implementation at `HEAD`.

**Step 8: Resolve issue #10 with read-after-write**

Re-read the issue immediately before posting. Comment with:

- design and implementation document links;
- exact verifier outputs;
- explicit statement that tests used no live SMS and no live mutation;
- any remaining live-login criterion marked unchecked rather than inferred.

Close only if the issue is still open and all mechanical acceptance checks pass. Re-read issue state after closing.

---

## Final acceptance checklist

- [ ] One production credential provider is shared by CLI and MCP authentication gate.
- [ ] `auth login` is CLI-only and refuses non-interactive execution before network access.
- [ ] The OS store is default; every operation is timeout-bounded.
- [ ] First-use unavailability falls back once; explicit denial never does.
- [ ] The file fallback is alias-safe and described as permission-protected, not encrypted.
- [ ] Login and refresh use verified two-slot commits.
- [ ] Concurrent callers perform one refresh and waiters re-read storage.
- [ ] Early refresh failure may use an unexpired token; expired failure is hard.
- [ ] Only the evidenced read rejection may retry, once; ambiguous mutations never retry.
- [ ] Status reports usability without inventing unknown expiry.
- [ ] Logout deletes every known local copy and makes no revocation claim.
- [ ] Generated secret corpora are absent from all captured channels.
- [ ] Integration tests compose CLI, provider, store, transport, and MCP gate.
- [ ] No test sends SMS or performs a live Partiful mutation.
- [ ] Both mechanical verifier scripts pass.
- [ ] Final gogcli-focused Copilot rubber-duck review has no unresolved blocker.
