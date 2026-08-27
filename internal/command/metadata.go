package command

import "github.com/KalebCole/partiful/internal/domain"

func applyMetadata(definitions []Descriptor) {
	help := []string{
		"Authenticate an account in an interactive terminal.",
		"Show the current authentication state.",
		"Remove locally stored credentials.",
		"List events for the authenticated account.",
		"Get one event by its public event ID.",
		"Create an event.",
		"Update selected event fields.",
		"Cancel an event.",
		"List guests for an event.",
		"Invite one contact to an event.",
		"Get the authenticated account RSVP for an event.",
		"Set the authenticated account RSVP intent.",
		"List account contacts using opaque contact references.",
		"Invite a contact as a cohost.",
		"Revoke a pending cohost invitation.",
		"Remove a current cohost.",
		"Create a cohost access link.",
		"Revoke a cohost access link.",
		"Send one blast to all event guests.",
		"List the public poster catalog.",
		"Search the public poster catalog.",
		"Show command input, result, and adapter schemas.",
		"Run local installation and credential diagnostics.",
		"Show CLI and contract revisions.",
	}
	for index := range definitions {
		definitions[index].Help = help[index]
	}

	input := []domain.ErrorType{domain.ErrorInputInvalid, domain.ErrorInternalFailure}
	localAuth := []domain.ErrorType{
		domain.ErrorInputInvalid, domain.ErrorAuthHumanRequired, domain.ErrorAuthStoreUnavailable,
		domain.ErrorAuthPersistenceFailed, domain.ErrorAuthBusy, domain.ErrorRemoteUnavailable,
		domain.ErrorRemoteRateLimited, domain.ErrorContractProtocolChanged, domain.ErrorInternalFailure,
	}
	protected := []domain.ErrorType{
		domain.ErrorInputInvalid, domain.ErrorAuthRequired, domain.ErrorAuthExpired,
		domain.ErrorAuthStoreUnavailable, domain.ErrorAuthPersistenceFailed, domain.ErrorAuthBusy,
		domain.ErrorPermissionDenied, domain.ErrorResourceNotFound, domain.ErrorStateConflict,
		domain.ErrorRemoteUnavailable, domain.ErrorRemoteRateLimited,
		domain.ErrorContractProtocolChanged, domain.ErrorInternalFailure,
	}
	contact := append(append([]domain.ErrorType(nil), protected...), domain.ErrorMatchAmbiguous)

	definitions[0].FailureTypes = cloneErrorTypes(localAuth)
	definitions[1].FailureTypes = cloneErrorTypes(localAuth)
	definitions[2].FailureTypes = []domain.ErrorType{
		domain.ErrorInputInvalid, domain.ErrorAuthStoreUnavailable, domain.ErrorAuthPersistenceFailed,
		domain.ErrorAuthBusy, domain.ErrorInternalFailure,
	}
	for index := 3; index <= 12; index++ {
		definitions[index].FailureTypes = cloneErrorTypes(protected)
	}
	for index := 9; index <= 9; index++ {
		definitions[index].FailureTypes = cloneErrorTypes(contact)
	}
	for index := 13; index <= 15; index++ {
		definitions[index].FailureTypes = cloneErrorTypes(contact)
	}
	for index := 16; index <= 18; index++ {
		definitions[index].FailureTypes = cloneErrorTypes(protected)
	}
	definitions[19].FailureTypes = []domain.ErrorType{
		domain.ErrorInputInvalid, domain.ErrorRemoteUnavailable, domain.ErrorRemoteRateLimited,
		domain.ErrorContractProtocolChanged, domain.ErrorInternalFailure,
	}
	definitions[20].FailureTypes = cloneErrorTypes(definitions[19].FailureTypes)
	definitions[21].FailureTypes = cloneErrorTypes(input)
	definitions[22].FailureTypes = cloneErrorTypes(input)
	definitions[23].FailureTypes = []domain.ErrorType{domain.ErrorInternalFailure}
}

func cloneErrorTypes(source []domain.ErrorType) []domain.ErrorType {
	return append([]domain.ErrorType(nil), source...)
}
