package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
)

const credentialSchemaVersion = 1

type credentialWire struct {
	SchemaVersion      int        `json:"schema_version"`
	Generation         uint64     `json:"generation"`
	AccountIdentity    string     `json:"account_identity"`
	AccessToken        string     `json:"access_token"`
	RefreshToken       string     `json:"refresh_token"`
	AccessTokenExpires *time.Time `json:"access_token_expires,omitempty"`
	InstallationSecret string     `json:"installation_secret"`
}

func EncodeCredential(credential Credential) ([]byte, error) {
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	return json.Marshal(credentialWire{
		SchemaVersion: credential.SchemaVersion, Generation: credential.Generation,
		AccountIdentity: credential.AccountIdentity.Reveal(), AccessToken: credential.AccessToken.Reveal(),
		RefreshToken: credential.RefreshToken.Reveal(), AccessTokenExpires: credential.AccessTokenExpires,
		InstallationSecret: credential.InstallationSecret.Reveal(),
	})
}

func DecodeCredential(data []byte) (Credential, error) {
	var wire credentialWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Credential{}, storeError("corrupt", false)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Credential{}, storeError("corrupt", false)
	}
	credential := Credential{
		SchemaVersion: wire.SchemaVersion, Generation: wire.Generation,
		AccountIdentity: NewSecret(wire.AccountIdentity), AccessToken: NewSecret(wire.AccessToken),
		RefreshToken: NewSecret(wire.RefreshToken), AccessTokenExpires: wire.AccessTokenExpires,
		InstallationSecret: NewSecret(wire.InstallationSecret),
	}
	if err := validateCredential(credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func validateCredential(credential Credential) error {
	if credential.SchemaVersion != credentialSchemaVersion || credential.Generation == 0 ||
		credential.AccountIdentity.Reveal() == "" || credential.AccessToken.Reveal() == "" ||
		credential.RefreshToken.Reveal() == "" || credential.InstallationSecret.Reveal() == "" {
		return storeError("corrupt", false)
	}
	return nil
}

func storeError(state string, retryable bool) error {
	return &domain.Error{Type: domain.ErrorAuthStoreUnavailable, Code: "AUTH_STORE_UNAVAILABLE", Message: "Credential storage is unavailable.", Retryable: retryable, Details: domain.ErrorDetails{State: state}}
}

func persistenceError(backend BackendKind) error {
	return &domain.Error{Type: domain.ErrorAuthPersistenceFailed, Code: "AUTH_PERSISTENCE_FAILED", Message: "Credentials could not be persisted safely.", Details: domain.ErrorDetails{BackendKind: string(backend)}}
}

func IsErrorType(err error, errorType domain.ErrorType) bool {
	var classified *domain.Error
	return errors.As(err, &classified) && classified.Type == errorType
}

func protocolError() error {
	return ProtocolChangedError()
}

// ProtocolChangedError is safe for adapters that reject an unverified shape.
func ProtocolChangedError() error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "AUTH_PROTOCOL_CHANGED", Message: "The authentication response did not match the verified contract."}
}

func requiredError() error {
	return &domain.Error{Type: domain.ErrorAuthRequired, Code: "AUTH_REQUIRED", Message: "Partiful authentication is required.", Details: domain.ErrorDetails{Remediation: "Run `partiful auth login` in a private terminal, then retry."}}
}

func expiredError(cause error) error {
	return &domain.Error{Type: domain.ErrorAuthExpired, Code: "AUTH_EXPIRED", Message: "Partiful authentication has expired.", Details: domain.ErrorDetails{Remediation: "Run `partiful auth login` in a private terminal, then retry."}, Retryable: false}
}
