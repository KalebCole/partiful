package app

import (
	"context"
	"fmt"

	"github.com/KalebCole/partiful/internal/domain"
)

// CredentialInspector performs a local-only credential inspection. It must not
// refresh credentials or make a remote request.
type CredentialInspector interface {
	InspectCredentials(context.Context) (domain.AuthState, error)
}

type UtilityOperationsConfig struct {
	Credentials CredentialInspector
	Version     domain.VersionResult
}

// BindUtilityOperations registers schema, local doctor, and version operations.
func BindUtilityOperations(service *Service, config UtilityOperationsConfig) error {
	if service == nil || config.Credentials == nil || config.Version.CLIVersion == "" || config.Version.CommandContractRevision == "" || config.Version.TransportContractRevision == "" {
		return fmt.Errorf("bind utility operations: missing dependency")
	}
	if err := BindOperation(service, OperationSpec[domain.CommandSchemaInput, domain.CommandSchemaResult]{
		Operation: domain.OperationGetCommandSchema,
		Execute: func(_ context.Context, _ *Invocation, input domain.CommandSchemaInput) (domain.CommandSchemaResult, error) {
			result, err := service.catalog.Project(input.Command)
			if err != nil {
				return domain.CommandSchemaResult{}, &domain.Error{Type: domain.ErrorInputInvalid, Code: "UNKNOWN_COMMAND", Message: "command path is not in the public command catalog"}
			}
			return result, nil
		},
	}); err != nil {
		return err
	}
	if err := BindOperation(service, OperationSpec[struct{}, domain.DoctorResult]{
		Operation: domain.OperationRunDoctor,
		Execute: func(ctx context.Context, _ *Invocation, _ struct{}) (domain.DoctorResult, error) {
			return inspectCredentials(ctx, config.Credentials), nil
		},
	}); err != nil {
		return err
	}
	return BindOperation(service, OperationSpec[struct{}, domain.VersionResult]{
		Operation: domain.OperationGetVersion,
		Execute: func(context.Context, *Invocation, struct{}) (domain.VersionResult, error) {
			return config.Version, nil
		},
	})
}

func inspectCredentials(ctx context.Context, credentials CredentialInspector) domain.DoctorResult {
	state, err := credentials.InspectCredentials(ctx)
	check := domain.DoctorCheck{Name: "credentials"}
	result := domain.DoctorResult{Healthy: true}
	switch {
	case err != nil:
		check.Status = domain.DoctorStatusFail
		check.Message = "Authentication credentials could not be inspected."
		check.Remediation = stringPointer("Check local credential storage permissions.")
		result.Healthy = false
	case !state.Authenticated || state.TokenState == domain.TokenStateMissing:
		check.Status = domain.DoctorStatusWarn
		check.Message = "Authentication credentials are not available."
		check.Remediation = stringPointer("Run partiful auth login.")
	case state.TokenState == domain.TokenStateExpired:
		check.Status = domain.DoctorStatusFail
		check.Message = "Authentication credentials are expired."
		check.Remediation = stringPointer("Run partiful auth login.")
		result.Healthy = false
	case state.TokenState == domain.TokenStateExpiring || state.TokenState == domain.TokenStateUnknown:
		check.Status = domain.DoctorStatusWarn
		check.Message = "Authentication credentials are available but their local token state needs attention."
		check.Remediation = stringPointer("Run partiful auth status.")
	case state.TokenState == domain.TokenStateHealthy:
		check.Status = domain.DoctorStatusPass
		check.Message = "Authentication credentials are available."
	default:
		check.Status = domain.DoctorStatusFail
		check.Message = "Authentication credentials could not be inspected."
		check.Remediation = stringPointer("Check local credential storage integrity.")
		result.Healthy = false
	}
	result.Checks = []domain.DoctorCheck{check}
	return result
}

func stringPointer(value string) *string { return &value }
