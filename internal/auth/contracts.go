package auth

import (
	"context"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
)

// Secret is a secret-bearing value whose diagnostic formatting is always redacted.
type Secret struct{ value string }

func NewSecret(value string) Secret  { return Secret{value: value} }
func (secret Secret) Reveal() string { return secret.value }
func (Secret) String() string        { return "[REDACTED]" }
func (Secret) GoString() string      { return "[REDACTED]" }

type BackendKind string
type Slot string

const (
	SlotA Slot = "a"
	SlotB Slot = "b"
)

// Credential is the private versioned record shared by the provider and store.
type Credential struct {
	SchemaVersion      int
	Generation         uint64
	AccountIdentity    Secret
	AccessToken        Secret
	RefreshToken       Secret
	AccessTokenExpires *time.Time
	InstallationSecret Secret
}

// LoginPrompter is implemented only by a private interactive terminal adapter.
type LoginPrompter interface {
	PhoneNumber(context.Context) (Secret, error)
	VerificationCode(context.Context) (Secret, error)
}

// Provider is the application-facing credential lifecycle seam.
type Provider interface {
	Login(context.Context, LoginPrompter) (domain.AuthState, error)
	Status(context.Context) (domain.AuthState, error)
	Acquire(context.Context) (Authorization, error)
	Logout(context.Context) (domain.AuthState, error)
}

type Authorization struct {
	AccessToken     Secret
	AccountIdentity Secret
}

// CredentialStore stores opaque credential slots and owns no lifecycle policy.
type CredentialStore interface {
	Backend() BackendKind
	Load(context.Context, Slot) ([]byte, error)
	Store(context.Context, Slot, []byte) error
	Delete(context.Context, Slot) error
}

// AuthTransport exposes only the evidenced typed authentication exchanges.
type AuthTransport interface {
	SendCode(context.Context, SendCodeRequest) (SendCodeResult, error)
	ExchangeLoginCode(context.Context, LoginCodeRequest) (LoginCodeResult, error)
	SignIn(context.Context, SignInRequest) (Session, error)
	Refresh(context.Context, RefreshRequest) (Session, error)
	LookupAccount(context.Context, LookupRequest) (Account, error)
}

type SendCodeRequest struct{ PhoneNumber Secret }
type SendCodeResult struct{ Challenge Secret }
type LoginCodeRequest struct {
	Challenge Secret
	Code      Secret
}
type LoginCodeResult struct{ CustomToken Secret }
type SignInRequest struct{ CustomToken Secret }
type RefreshRequest struct{ RefreshToken Secret }
type LookupRequest struct{ AccessToken Secret }
type Account struct{ Identity Secret }
type Session struct {
	AccessToken  Secret
	RefreshToken Secret
	ExpiresAt    *time.Time
}

// RefreshCoordinator serializes refresh work across processes and bounds waiting.
type RefreshCoordinator interface {
	Do(context.Context, func(context.Context) error) error
}

type Clock interface{ Now() time.Time }

type Diagnostic struct {
	Kind        string
	Backend     BackendKind
	State       string
	Remediation string
}

type DiagnosticSink interface {
	Warn(context.Context, Diagnostic)
}
