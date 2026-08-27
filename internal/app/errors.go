package app

import (
	"errors"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

func evidenceGateOpen() error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "EVIDENCE_GATE_OPEN", Message: "operation is unavailable until its evidence gate is closed"}
}

func evidenceClaimOpen() error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "EVIDENCE_CLAIM_OPEN", Message: "the remote outcome cannot be classified by accepted evidence"}
}

func invalidOperationInput() error {
	return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_OPERATION_INPUT", Message: "operation input has the wrong type"}
}

func operationUnavailable() error {
	return &domain.Error{Type: domain.ErrorInternalFailure, Code: "OPERATION_UNAVAILABLE", Message: "operation is not registered"}
}

func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *domain.Error
	if errors.As(err, &applicationError) {
		return applicationError
	}
	var failure *transport.ProtocolFailure
	if errors.As(err, &failure) {
		errorType := domain.ErrorContractProtocolChanged
		switch failure.Class {
		case string(domain.ErrorPermissionDenied):
			errorType = domain.ErrorPermissionDenied
		case string(domain.ErrorResourceNotFound):
			errorType = domain.ErrorResourceNotFound
		case string(domain.ErrorStateConflict):
			errorType = domain.ErrorStateConflict
		case string(domain.ErrorRemoteUnavailable):
			errorType = domain.ErrorRemoteUnavailable
		case string(domain.ErrorRemoteRateLimited):
			errorType = domain.ErrorRemoteRateLimited
		case string(domain.ErrorContractProtocolChanged):
			errorType = domain.ErrorContractProtocolChanged
		}
		return &domain.Error{Type: errorType, Code: stableErrorCode(errorType), Message: "the remote operation failed", Retryable: failure.Retryable}
	}
	return &domain.Error{Type: domain.ErrorInternalFailure, Code: "INTERNAL_FAILURE", Message: "the operation failed"}
}

func hasEvidencedClassification(err error) bool {
	var applicationError *domain.Error
	if errors.As(err, &applicationError) {
		return true
	}
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) {
		return false
	}
	switch domain.ErrorType(failure.Class) {
	case domain.ErrorPermissionDenied, domain.ErrorResourceNotFound, domain.ErrorStateConflict,
		domain.ErrorRemoteUnavailable, domain.ErrorRemoteRateLimited, domain.ErrorContractProtocolChanged:
		return true
	default:
		return false
	}
}

func stableErrorCode(errorType domain.ErrorType) string {
	switch errorType {
	case domain.ErrorPermissionDenied:
		return "PERMISSION_DENIED"
	case domain.ErrorResourceNotFound:
		return "RESOURCE_NOT_FOUND"
	case domain.ErrorStateConflict:
		return "STATE_CONFLICT"
	case domain.ErrorRemoteUnavailable:
		return "REMOTE_UNAVAILABLE"
	case domain.ErrorRemoteRateLimited:
		return "REMOTE_RATE_LIMITED"
	default:
		return "PROTOCOL_CHANGED"
	}
}
