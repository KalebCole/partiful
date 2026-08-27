package domain

// ExitClass is a stable process exit classification shared by public adapters.
type ExitClass int

const (
	ExitSuccess    ExitClass = 0
	ExitUsage      ExitClass = 2
	ExitAuth       ExitClass = 3
	ExitPermission ExitClass = 4
	ExitNotFound   ExitClass = 5
	ExitConflict   ExitClass = 6
	ExitRemote     ExitClass = 8
	ExitProtocol   ExitClass = 9
	ExitInternal   ExitClass = 10
)

// ErrorType identifies a stable application failure class.
type ErrorType string

const (
	ErrorUsageInvalid            ErrorType = "usage.invalid"
	ErrorInputInvalid            ErrorType = "input.invalid"
	ErrorMatchAmbiguous          ErrorType = "match.ambiguous"
	ErrorAuthRequired            ErrorType = "auth.required"
	ErrorAuthExpired             ErrorType = "auth.expired"
	ErrorAuthHumanRequired       ErrorType = "auth.human_required"
	ErrorAuthStoreUnavailable    ErrorType = "auth.store_unavailable"
	ErrorAuthPersistenceFailed   ErrorType = "auth.persistence_failed"
	ErrorAuthBusy                ErrorType = "auth.busy"
	ErrorPermissionDenied        ErrorType = "permission.denied"
	ErrorResourceNotFound        ErrorType = "resource.not_found"
	ErrorStateConflict           ErrorType = "state.conflict"
	ErrorRemoteUnavailable       ErrorType = "remote.unavailable"
	ErrorRemoteRateLimited       ErrorType = "remote.rate_limited"
	ErrorContractProtocolChanged ErrorType = "contract.protocol_changed"
	ErrorInternalFailure         ErrorType = "internal.failure"
)

// ExitClass returns the stable exit classification for an error type.
func (errorType ErrorType) ExitClass() ExitClass {
	switch errorType {
	case ErrorUsageInvalid, ErrorInputInvalid, ErrorMatchAmbiguous:
		return ExitUsage
	case ErrorAuthRequired, ErrorAuthExpired, ErrorAuthHumanRequired,
		ErrorAuthStoreUnavailable, ErrorAuthPersistenceFailed, ErrorAuthBusy:
		return ExitAuth
	case ErrorPermissionDenied:
		return ExitPermission
	case ErrorResourceNotFound:
		return ExitNotFound
	case ErrorStateConflict:
		return ExitConflict
	case ErrorRemoteUnavailable, ErrorRemoteRateLimited:
		return ExitRemote
	case ErrorContractProtocolChanged:
		return ExitProtocol
	default:
		return ExitInternal
	}
}

// Error is the sanitized application error exposed by CLI and MCP adapters.
type Error struct {
	Type      ErrorType
	Code      string
	Message   string
	Retryable bool
	Details   ErrorDetails
}

type ErrorDetails struct {
	Remediation string
	BackendKind string
	State       string
	Candidates  []Contact
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}
