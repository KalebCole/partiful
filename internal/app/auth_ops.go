package app

import (
	"context"
	"fmt"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
)

var authLoginGateIdentities = []string{
	"OP11-AUTH-REQUESTS:sendAuthCodeTrusted",
	"OP11-AUTH-REQUESTS:getLoginToken",
	"OP11-AUTH-REQUESTS:signInWithCustomToken",
	"OP11-AUTH-REQUESTS:lookupFirebaseUser",
}

var authApplicableGateIdentities = []string{
	"OP11-AUTH-REQUESTS:sendAuthCodeTrusted",
	"OP11-AUTH-REQUESTS:getLoginToken",
	"OP11-AUTH-REQUESTS:signInWithCustomToken",
	"OP11-AUTH-REQUESTS:refreshToken",
	"OP11-AUTH-REQUESTS:lookupFirebaseUser",
	"OP11-ENDPOINT-ERRORS:sendAuthCodeTrusted",
	"OP11-ENDPOINT-ERRORS:getLoginToken",
	"OP11-ENDPOINT-ERRORS:signInWithCustomToken",
	"OP11-ENDPOINT-ERRORS:refreshToken",
	"OP11-ENDPOINT-ERRORS:lookupFirebaseUser",
}

// BindAuthOperations registers the shared authentication operations. Login is
// held behind every request-shape gate before prompting. The provider owns
// branch-specific refresh and endpoint-classification gates.
func BindAuthOperations(service *Service, provider auth.Provider, prompter auth.LoginPrompter) error {
	if service == nil || provider == nil || prompter == nil {
		return fmt.Errorf("bind auth operations: missing dependency")
	}
	for _, identity := range authApplicableGateIdentities {
		if _, found := service.gates.Lookup(identity); !found {
			return fmt.Errorf("bind auth operations: missing gate %q", identity)
		}
	}
	if err := BindOperation(service, OperationSpec[struct{}, domain.AuthState]{
		Operation:     domain.OperationAuthLoginInteractive,
		RequiredGates: authLoginGateIdentities,
		Execute: func(ctx context.Context, _ *Invocation, _ struct{}) (domain.AuthState, error) {
			return provider.Login(ctx, prompter)
		},
	}); err != nil {
		return err
	}
	if err := BindOperation(service, OperationSpec[struct{}, domain.AuthState]{
		Operation: domain.OperationGetAuthStatus,
		Execute: func(ctx context.Context, _ *Invocation, _ struct{}) (domain.AuthState, error) {
			return provider.Status(ctx)
		},
	}); err != nil {
		return err
	}
	return BindOperation(service, OperationSpec[domain.LogoutInput, domain.AuthState]{
		Operation: domain.OperationLogout,
		Execute: func(ctx context.Context, _ *Invocation, input domain.LogoutInput) (domain.AuthState, error) {
			if input.DryRun {
				return domain.AuthState{Authenticated: false, TokenState: domain.TokenStateMissing}, nil
			}
			return provider.Logout(ctx)
		},
	})
}
