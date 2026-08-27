package domain_test

import (
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
)

func TestErrorUsesOnlySanitizedPublicMessage(t *testing.T) {
	t.Parallel()

	var err error = &domain.Error{Message: "Partiful denied this operation."}
	if got := err.Error(); got != "Partiful denied this operation." {
		t.Fatalf("Error() = %q", got)
	}
}

func TestStableErrorTypesMapToExitClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errorType domain.ErrorType
		want      domain.ExitClass
	}{
		{domain.ErrorUsageInvalid, domain.ExitUsage},
		{domain.ErrorInputInvalid, domain.ExitUsage},
		{domain.ErrorMatchAmbiguous, domain.ExitUsage},
		{domain.ErrorAuthRequired, domain.ExitAuth},
		{domain.ErrorAuthExpired, domain.ExitAuth},
		{domain.ErrorAuthHumanRequired, domain.ExitAuth},
		{domain.ErrorAuthStoreUnavailable, domain.ExitAuth},
		{domain.ErrorAuthPersistenceFailed, domain.ExitAuth},
		{domain.ErrorAuthBusy, domain.ExitAuth},
		{domain.ErrorPermissionDenied, domain.ExitPermission},
		{domain.ErrorResourceNotFound, domain.ExitNotFound},
		{domain.ErrorStateConflict, domain.ExitConflict},
		{domain.ErrorRemoteUnavailable, domain.ExitRemote},
		{domain.ErrorRemoteRateLimited, domain.ExitRemote},
		{domain.ErrorContractProtocolChanged, domain.ExitProtocol},
		{domain.ErrorInternalFailure, domain.ExitInternal},
	}

	for _, test := range tests {
		test := test
		t.Run(string(test.errorType), func(t *testing.T) {
			t.Parallel()
			if got := test.errorType.ExitClass(); got != test.want {
				t.Fatalf("ExitClass() = %d, want %d", got, test.want)
			}
		})
	}
}
