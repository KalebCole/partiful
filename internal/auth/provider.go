package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
)

const earlyRefreshWindow = 5 * time.Minute

type GateSet interface{ Allows(string) bool }

type ProviderConfig struct {
	Store                 CredentialStore
	CleanupStores         []CredentialStore
	Transport             AuthTransport
	Coordinator           RefreshCoordinator
	Clock                 Clock
	Diagnostics           DiagnosticSink
	Gates                 GateSet
	NewInstallationSecret func() (Secret, error)
	ClearBackendMarker    func() error
}

type credentialProvider struct{ config ProviderConfig }

func NewProvider(config ProviderConfig) Provider {
	if config.NewInstallationSecret == nil {
		config.NewInstallationSecret = randomInstallationSecret
	}
	return &credentialProvider{config: config}
}

func randomInstallationSecret() (Secret, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return Secret{}, err
	}
	return NewSecret(base64.RawURLEncoding.EncodeToString(value)), nil
}

func (provider *credentialProvider) ready() error {
	if provider.config.Store == nil || provider.config.Transport == nil || provider.config.Coordinator == nil || provider.config.Clock == nil {
		return &domain.Error{Type: domain.ErrorInternalFailure, Code: "AUTH_NOT_CONFIGURED", Message: "Authentication is not configured."}
	}
	return nil
}

func (provider *credentialProvider) coordinate(ctx context.Context, operation func(context.Context) error) error {
	err := provider.config.Coordinator.Do(ctx, operation)
	if err == nil {
		return nil
	}
	var public *domain.Error
	if errors.As(err, &public) {
		return public
	}
	return &domain.Error{Type: domain.ErrorAuthBusy, Code: "AUTH_BUSY", Message: "Authentication storage is busy.", Retryable: true}
}

func (provider *credentialProvider) allowsAuthOperations(operations ...string) bool {
	if provider.config.Gates == nil {
		return false
	}
	for _, operation := range operations {
		if !provider.config.Gates.Allows("OP11-AUTH-REQUESTS:"+operation) || !provider.config.Gates.Allows("OP11-ENDPOINT-ERRORS:"+operation) {
			return false
		}
	}
	return true
}

func (provider *credentialProvider) Login(ctx context.Context, prompter LoginPrompter) (domain.AuthState, error) {
	if err := provider.ready(); err != nil {
		return domain.AuthState{}, err
	}
	if prompter == nil {
		return domain.AuthState{}, &domain.Error{Type: domain.ErrorAuthHumanRequired, Code: "AUTH_HUMAN_REQUIRED", Message: "Authentication requires a private interactive terminal."}
	}
	if !provider.allowsAuthOperations("sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "lookupFirebaseUser") {
		return domain.AuthState{}, protocolError()
	}
	phone, err := prompter.PhoneNumber(ctx)
	if err != nil {
		return domain.AuthState{}, err
	}
	challenge, err := provider.config.Transport.SendCode(ctx, SendCodeRequest{PhoneNumber: phone})
	if err != nil {
		return domain.AuthState{}, err
	}
	code, err := prompter.VerificationCode(ctx)
	if err != nil {
		return domain.AuthState{}, err
	}
	login, err := provider.config.Transport.ExchangeLoginCode(ctx, LoginCodeRequest{Challenge: challenge.Challenge, Code: code})
	if err != nil {
		return domain.AuthState{}, err
	}
	session, err := provider.config.Transport.SignIn(ctx, SignInRequest{CustomToken: login.CustomToken})
	if err != nil {
		return domain.AuthState{}, err
	}
	account, err := provider.config.Transport.LookupAccount(ctx, LookupRequest{AccessToken: session.AccessToken})
	if err != nil {
		return domain.AuthState{}, err
	}
	if session.AccessToken.Reveal() == "" || session.RefreshToken.Reveal() == "" || account.Identity.Reveal() == "" {
		return domain.AuthState{}, protocolError()
	}
	var committed Credential
	err = provider.coordinate(ctx, func(ctx context.Context) error {
		prior, priorSlot, loadErr := LoadActive(ctx, provider.config.Store)
		generation := uint64(1)
		var installation Secret
		if loadErr == nil {
			generation = prior.Generation + 1
			installation = prior.InstallationSecret
		} else if !IsErrorType(loadErr, domain.ErrorAuthRequired) {
			return loadErr
		} else {
			var secretErr error
			installation, secretErr = provider.config.NewInstallationSecret()
			if secretErr != nil {
				return persistenceError(provider.config.Store.Backend())
			}
		}
		committed = Credential{SchemaVersion: credentialSchemaVersion, Generation: generation, AccountIdentity: account.Identity, AccessToken: session.AccessToken, RefreshToken: session.RefreshToken, AccessTokenExpires: session.ExpiresAt, InstallationSecret: installation}
		_, commitErr := CommitNext(ctx, provider.config.Store, priorSlot, committed)
		return commitErr
	})
	if err != nil {
		return domain.AuthState{}, err
	}
	return stateFor(committed, provider.config.Clock.Now()), nil
}

func (provider *credentialProvider) Status(ctx context.Context) (domain.AuthState, error) {
	if err := provider.ready(); err != nil {
		return domain.AuthState{}, err
	}
	credential, _, err := LoadActive(ctx, provider.config.Store)
	if IsErrorType(err, domain.ErrorAuthRequired) {
		return domain.AuthState{TokenState: domain.TokenStateMissing}, nil
	}
	if err != nil {
		return domain.AuthState{}, err
	}
	state := stateFor(credential, provider.config.Clock.Now())
	if state.TokenState == domain.TokenStateExpiring || state.TokenState == domain.TokenStateExpired {
		if _, err := provider.Acquire(ctx); err != nil {
			return domain.AuthState{}, err
		}
		credential, _, err = LoadActive(ctx, provider.config.Store)
		if err != nil {
			return domain.AuthState{}, err
		}
		state = stateFor(credential, provider.config.Clock.Now())
	}
	return state, nil
}

func stateFor(credential Credential, now time.Time) domain.AuthState {
	if credential.AccessTokenExpires == nil {
		return domain.AuthState{Authenticated: true, TokenState: domain.TokenStateUnknown}
	}
	state := domain.TokenStateHealthy
	if !credential.AccessTokenExpires.After(now) {
		state = domain.TokenStateExpired
	} else if !credential.AccessTokenExpires.After(now.Add(earlyRefreshWindow)) {
		state = domain.TokenStateExpiring
	}
	return domain.AuthState{Authenticated: true, TokenState: state, ExpiresAt: credential.AccessTokenExpires}
}

func (provider *credentialProvider) Acquire(ctx context.Context) (Authorization, error) {
	if err := provider.ready(); err != nil {
		return Authorization{}, err
	}
	credential, _, err := LoadActive(ctx, provider.config.Store)
	if err != nil {
		return Authorization{}, err
	}
	state := stateFor(credential, provider.config.Clock.Now())
	if state.TokenState == domain.TokenStateHealthy || state.TokenState == domain.TokenStateUnknown {
		return Authorization{AccessToken: credential.AccessToken, AccountIdentity: credential.AccountIdentity}, nil
	}
	if !provider.allowsAuthOperations("refreshToken") {
		return Authorization{}, protocolError()
	}
	var refreshed Credential
	err = provider.coordinate(ctx, func(ctx context.Context) error {
		current, slot, err := LoadActive(ctx, provider.config.Store)
		if err != nil {
			return err
		}
		currentState := stateFor(current, provider.config.Clock.Now())
		if current.Generation > credential.Generation && (currentState.TokenState == domain.TokenStateHealthy || currentState.TokenState == domain.TokenStateUnknown) {
			refreshed = current
			return nil
		}
		session, refreshErr := provider.config.Transport.Refresh(ctx, RefreshRequest{RefreshToken: current.RefreshToken})
		if refreshErr != nil {
			if current.AccessTokenExpires != nil && current.AccessTokenExpires.After(provider.config.Clock.Now()) {
				refreshed = current
				if provider.config.Diagnostics != nil {
					provider.config.Diagnostics.Warn(ctx, Diagnostic{Kind: "refresh", Backend: provider.config.Store.Backend(), State: "using_valid_token"})
				}
				return nil
			}
			return expiredError(refreshErr)
		}
		if session.AccessToken.Reveal() == "" || session.RefreshToken.Reveal() == "" {
			return protocolError()
		}
		refreshed = current
		refreshed.Generation++
		refreshed.AccessToken = session.AccessToken
		refreshed.RefreshToken = session.RefreshToken
		refreshed.AccessTokenExpires = session.ExpiresAt
		_, err = CommitNext(ctx, provider.config.Store, slot, refreshed)
		return err
	})
	if err != nil {
		return Authorization{}, err
	}
	return Authorization{AccessToken: refreshed.AccessToken, AccountIdentity: refreshed.AccountIdentity}, nil
}

func (provider *credentialProvider) Logout(ctx context.Context) (domain.AuthState, error) {
	if err := provider.ready(); err != nil {
		return domain.AuthState{}, err
	}
	err := provider.coordinate(ctx, func(ctx context.Context) error {
		stores := append([]CredentialStore{provider.config.Store}, provider.config.CleanupStores...)
		seen := map[CredentialStore]bool{}
		var failed BackendKind
		for _, store := range stores {
			if store == nil || seen[store] {
				continue
			}
			seen[store] = true
			for _, slot := range []Slot{SlotA, SlotB} {
				if err := store.Delete(ctx, slot); err != nil {
					failed = store.Backend()
				}
			}
			for _, slot := range []Slot{SlotA, SlotB} {
				value, err := store.Load(ctx, slot)
				if err != nil || len(value) != 0 {
					failed = store.Backend()
				}
			}
		}
		if failed != "" {
			return persistenceError(failed)
		}
		if provider.config.ClearBackendMarker != nil {
			if err := provider.config.ClearBackendMarker(); err != nil {
				return persistenceError(provider.config.Store.Backend())
			}
		}
		return nil
	})
	if err != nil {
		return domain.AuthState{}, err
	}
	return domain.AuthState{Authenticated: false, TokenState: domain.TokenStateMissing}, nil
}

var _ Provider = (*credentialProvider)(nil)
