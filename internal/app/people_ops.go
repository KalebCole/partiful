package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

const (
	contactTraversalMaximum = 1000
	guestTraversalMaximum   = 500
)

// PeopleAuthorization is the private authorization material needed by people operations.
// AccountIdentity and InstallationSecret scope opaque contact references; neither is projected.
type PeopleAuthorization struct {
	Credential         transport.Credential
	AccountIdentity    string
	InstallationSecret []byte
}

// PeopleCredentialProvider acquires private authorization material at invocation time.
type PeopleCredentialProvider interface {
	Acquire(context.Context) (PeopleAuthorization, error)
}

// DeriveContactReference creates a stable, non-reversible installation- and account-scoped reference.
func DeriveContactReference(installationSecret []byte, accountIdentity string, contactID transport.ContactID) (domain.ContactRef, error) {
	if len(installationSecret) == 0 || strings.TrimSpace(accountIdentity) == "" || contactID == "" {
		return "", fmt.Errorf("contact reference: incomplete scope")
	}
	mac := hmac.New(sha256.New, installationSecret)
	_, _ = mac.Write([]byte("partiful-contact-reference-v1\x00"))
	_, _ = mac.Write([]byte(accountIdentity))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(contactID))
	return domain.ContactRef("contact_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), nil
}

// BindPeopleOperations installs the people-operation application workflows.
func BindPeopleOperations(service *Service, credentials PeopleCredentialProvider, remote transport.CallableTransport) error {
	if service == nil || credentials == nil || remote == nil {
		return fmt.Errorf("bind people operations: missing dependency")
	}
	workflows := peopleWorkflows{service: service, credentials: credentials, remote: remote}
	bindings := []func() error{
		func() error {
			return BindOperation(service, OperationSpec[domain.ListGuestsInput, domain.GuestsResult]{Operation: domain.OperationListGuests, RequiredGates: []string{"OP11-GUEST-COHOST-FIELD"}, Execute: func(ctx context.Context, _ *Invocation, input domain.ListGuestsInput) (domain.GuestsResult, error) {
				return workflows.listGuests(ctx, input)
			}})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.InviteGuestInput, domain.SubmittedResult]{Operation: domain.OperationInviteGuest, ErrorGate: "OP11-ENDPOINT-ERRORS:addInvitedGuestsAsHost", OutcomeGate: "OP11-MUTATION-OUTCOME:addInvitedGuestsAsHost", Execute: workflows.inviteGuest})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.GetEventInput, domain.RSVPResult]{Operation: domain.OperationGetRSVP, ErrorGate: "OP11-ENDPOINT-ERRORS:getCurrentGuest", Execute: func(ctx context.Context, _ *Invocation, input domain.GetEventInput) (domain.RSVPResult, error) {
				return workflows.getRSVP(ctx, input)
			}})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.SetRSVPInput, domain.SetRSVPResult]{Operation: domain.OperationSetRSVP, Execute: workflows.setRSVP})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.ListContactsInput, domain.ContactsResult]{Operation: domain.OperationListContacts, ErrorGate: "OP11-ENDPOINT-ERRORS:getContacts", Execute: func(ctx context.Context, _ *Invocation, input domain.ListContactsInput) (domain.ContactsResult, error) {
				return workflows.listContacts(ctx, input)
			}})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CohostInput, domain.CohostResult]{Operation: domain.OperationInviteCohost, ErrorGate: "OP11-ENDPOINT-ERRORS:createCohostRequest", OutcomeGate: "OP11-MUTATION-OUTCOME:createCohostRequest", Execute: workflows.inviteCohost})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CohostInput, domain.CohostResult]{Operation: domain.OperationRevokeCohostInvite, ErrorGate: "OP11-ENDPOINT-ERRORS:deleteCohostRequest", OutcomeGate: "OP11-MUTATION-OUTCOME:deleteCohostRequest", Execute: workflows.revokeCohostInvite})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CohostInput, domain.CohostResult]{Operation: domain.OperationRemoveCohost, ErrorGate: "OP11-ENDPOINT-ERRORS:removeCohost", OutcomeGate: "OP11-MUTATION-OUTCOME:removeCohost", Execute: workflows.removeCohost})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CohostLinkInput, domain.CohostLinkResult]{Operation: domain.OperationCreateCohostLink, ErrorGate: "OP11-ENDPOINT-ERRORS:generateEventCohostLink", OutcomeGate: "OP11-MUTATION-OUTCOME:generateEventCohostLink", Execute: workflows.createCohostLink})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CohostLinkInput, domain.CohostLinkResult]{Operation: domain.OperationRevokeCohostLink, ErrorGate: "OP11-ENDPOINT-ERRORS:revokeEventCohostLink", OutcomeGate: "OP11-MUTATION-OUTCOME:revokeEventCohostLink", Execute: workflows.revokeCohostLink})
		},
	}
	for _, bind := range bindings {
		if err := bind(); err != nil {
			return err
		}
	}
	return nil
}

type peopleWorkflows struct {
	service     *Service
	credentials PeopleCredentialProvider
	remote      transport.CallableTransport
}

func (workflows peopleWorkflows) authorize(ctx context.Context) (PeopleAuthorization, error) {
	authorization, err := workflows.credentials.Acquire(ctx)
	if err != nil {
		return PeopleAuthorization{}, err
	}
	if authorization.Credential == "" || authorization.AccountIdentity == "" || len(authorization.InstallationSecret) == 0 {
		return PeopleAuthorization{}, &domain.Error{Type: domain.ErrorAuthRequired, Code: "AUTH_REQUIRED", Message: "authentication is required"}
	}
	return authorization, nil
}

func (workflows peopleWorkflows) listGuests(context.Context, domain.ListGuestsInput) (domain.GuestsResult, error) {
	// The callable response does not expose the public cohost field. The operation gate
	// prevents a partial or guessed projection and must be promoted with the transport.
	return domain.GuestsResult{}, evidenceGateOpen()
}

func (workflows peopleWorkflows) listContacts(ctx context.Context, input domain.ListContactsInput) (domain.ContactsResult, error) {
	normalizedCollection, err := NormalizeCollectionInput(input.CollectionInput)
	if err != nil {
		return domain.ContactsResult{}, err
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return domain.ContactsResult{}, err
	}
	contacts, err := workflows.materializeContacts(ctx, authorization)
	if err != nil {
		return domain.ContactsResult{}, err
	}
	query := ""
	if input.Query != nil {
		query = strings.ToLower(strings.TrimSpace(*input.Query))
	}
	projected := make([]domain.Contact, 0, len(contacts))
	for _, contact := range contacts {
		if query != "" && !strings.Contains(strings.ToLower(contact.DisplayName), query) {
			continue
		}
		reference, deriveErr := DeriveContactReference(authorization.InstallationSecret, authorization.AccountIdentity, contact.ContactID)
		if deriveErr != nil {
			return domain.ContactsResult{}, deriveErr
		}
		projected = append(projected, domain.Contact{ContactRef: reference, DisplayName: contact.DisplayName, SharedEventCount: contact.SharedEventCount})
	}
	codec, err := NewCursorCodec(authorization.InstallationSecret)
	if err != nil {
		return domain.ContactsResult{}, err
	}
	page, err := SliceCollection(codec, CursorScope{Operation: domain.OperationListContacts, CanonicalFilter: query}, normalizedCollection, projected)
	if err != nil {
		return domain.ContactsResult{}, err
	}
	return domain.ContactsResult{Contacts: page.Items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

func (workflows peopleWorkflows) materializeContacts(ctx context.Context, authorization PeopleAuthorization) ([]transport.Contact, error) {
	contacts, err := MaterializeContactPages(ctx, func(ctx context.Context, cursor *transport.RemoteCursor) (RemotePage[transport.Contact], error) {
		result, fetchErr := workflows.remote.GetContacts(ctx, transport.GetContactsRequest{Credential: authorization.Credential, MaxResults: contactTraversalMaximum, Cursor: cursor})
		return RemotePage[transport.Contact]{Items: result.Contacts, NextCursor: result.Cursor}, fetchErr
	})
	return DedupeContacts(contacts), err
}

func (workflows peopleWorkflows) inviteGuest(ctx context.Context, invocation *Invocation, input domain.InviteGuestInput) (domain.SubmittedResult, error) {
	if err := validateEventAndSelector(input.EventID, input.ContactSelector); err != nil {
		return domain.SubmittedResult{}, err
	}
	if input.DryRun {
		return domain.SubmittedResult{Submitted: false}, nil
	}
	authorization, contact, err := workflows.resolveContact(ctx, input.ContactSelector)
	if err != nil {
		return domain.SubmittedResult{}, err
	}
	message := ""
	if input.Message != nil {
		message = *input.Message
	}
	_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return workflows.remote.InviteGuest(ctx, transport.InviteGuestRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID), ContactID: contact.ContactID, Message: message})
	})
	if err != nil {
		return domain.SubmittedResult{}, err
	}
	return domain.SubmittedResult{Submitted: true}, nil
}

func (workflows peopleWorkflows) getRSVP(ctx context.Context, input domain.GetEventInput) (domain.RSVPResult, error) {
	if strings.TrimSpace(string(input.EventID)) == "" {
		return domain.RSVPResult{}, invalidPeopleInput()
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return domain.RSVPResult{}, err
	}
	response, err := workflows.remote.GetCurrentGuest(ctx, transport.GetCurrentGuestRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID)})
	if err != nil {
		return domain.RSVPResult{}, err
	}
	result := domain.RSVPResult{EventID: input.EventID}
	if response.Guest == nil {
		return result, nil
	}
	status := domain.EventReadRSVP(strings.ReplaceAll(strings.ToLower(response.Guest.Status), "_", "-"))
	if !status.Valid() {
		return domain.RSVPResult{}, &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "PROTOCOL_CHANGED", Message: "the remote RSVP status is not supported"}
	}
	result.Status = &status
	return result, nil
}

func (workflows peopleWorkflows) setRSVP(ctx context.Context, invocation *Invocation, input domain.SetRSVPInput) (domain.SetRSVPResult, error) {
	if err := validateRSVPInput(input); err != nil {
		return domain.SetRSVPResult{}, err
	}
	result := domain.SetRSVPResult{EventID: input.EventID, Intent: input.Status, Submitted: false}
	if input.DryRun {
		return result, nil
	}
	if len(input.QuestionnaireResponse) != 0 {
		return domain.SetRSVPResult{}, evidenceGateOpen()
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return domain.SetRSVPResult{}, err
	}
	switch input.Status {
	case domain.RSVPIntentInterested, domain.RSVPIntentNotInterested:
		_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
			return workflows.remote.MarkInterest(ctx, transport.MarkInterestRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID), Interested: input.Status == domain.RSVPIntentInterested})
		})
		err = workflows.guardRemoteClaim("OP11-ENDPOINT-ERRORS:markEventInterest", err)
	case domain.RSVPIntentGoing, domain.RSVPIntentNotGoing:
		current, readErr := workflows.remote.GetCurrentGuest(ctx, transport.GetCurrentGuestRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID)})
		if readErr != nil {
			return domain.SetRSVPResult{}, workflows.guardRemoteClaim("OP11-ENDPOINT-ERRORS:getCurrentGuest", readErr)
		}
		var guestID *transport.GuestID
		if current.Guest != nil {
			value := current.Guest.GuestID
			guestID = &value
		}
		status := "GOING"
		if input.Status == domain.RSVPIntentNotGoing {
			status = "DECLINED"
		}
		_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
			return workflows.remote.SetGuest(ctx, transport.SetGuestRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID), GuestID: guestID, Status: status, DisplayName: *input.DisplayName, PartySize: *input.PartySize, PlusOnes: append([]string(nil), input.PlusOnes...), Timezone: string(*input.Timezone), Message: input.Message})
		})
		err = workflows.guardRemoteClaim("OP11-ENDPOINT-ERRORS:addGuest", err)
	}
	if err != nil {
		return domain.SetRSVPResult{}, err
	}
	result.Submitted = true
	return result, nil
}

func (workflows peopleWorkflows) guardRemoteClaim(identity string, err error) error {
	if err == nil || workflows.service.gates.Allows(identity) || hasEvidencedClassification(err) {
		return err
	}
	return evidenceClaimOpen()
}

func (workflows peopleWorkflows) inviteCohost(ctx context.Context, invocation *Invocation, input domain.CohostInput) (domain.CohostResult, error) {
	return workflows.mutateCohost(ctx, invocation, input, domain.CohostStatusInvited, workflows.remote.InviteCohost)
}

func (workflows peopleWorkflows) revokeCohostInvite(ctx context.Context, invocation *Invocation, input domain.CohostInput) (domain.CohostResult, error) {
	return workflows.mutateCohost(ctx, invocation, input, domain.CohostStatusRevoked, workflows.remote.RevokeCohostInvite)
}

func (workflows peopleWorkflows) removeCohost(ctx context.Context, invocation *Invocation, input domain.CohostInput) (domain.CohostResult, error) {
	return workflows.mutateCohost(ctx, invocation, input, domain.CohostStatusRemoved, workflows.remote.RemoveCohost)
}

func (workflows peopleWorkflows) mutateCohost(ctx context.Context, invocation *Invocation, input domain.CohostInput, status domain.CohostStatus, mutate func(context.Context, transport.CohostRequest) (transport.Completion, error)) (domain.CohostResult, error) {
	if err := validateEventAndSelector(input.EventID, input.ContactSelector); err != nil {
		return domain.CohostResult{}, err
	}
	result := domain.CohostResult{EventID: input.EventID}
	result.Cohost.Status = status
	if input.Contact != nil {
		result.Cohost.DisplayName = strings.TrimSpace(*input.Contact)
	}
	if input.DryRun {
		return result, nil
	}
	authorization, contact, err := workflows.resolveContact(ctx, input.ContactSelector)
	if err != nil {
		return domain.CohostResult{}, err
	}
	if _, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return mutate(ctx, transport.CohostRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID), ContactID: contact.ContactID})
	}); err != nil {
		return domain.CohostResult{}, err
	}
	result.Cohost.DisplayName = contact.DisplayName
	return result, nil
}

func (workflows peopleWorkflows) createCohostLink(ctx context.Context, invocation *Invocation, input domain.CohostLinkInput) (domain.CohostLinkResult, error) {
	result := domain.CohostLinkResult{EventID: input.EventID}
	result.Link.State = domain.CohostLinkActive
	if err := validateEvent(input.EventID); err != nil || input.DryRun {
		return result, err
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return domain.CohostLinkResult{}, err
	}
	response, err := DispatchMutation(invocation, func() (transport.CohostLinkResult, error) {
		return workflows.remote.CreateCohostLink(ctx, transport.CohostLinkRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID)})
	})
	if err != nil {
		return domain.CohostLinkResult{}, err
	}
	if strings.TrimSpace(response.URL) == "" {
		return domain.CohostLinkResult{}, &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "PROTOCOL_CHANGED", Message: "the remote cohost link is invalid"}
	}
	result.Link.URL = &response.URL
	return result, nil
}

func (workflows peopleWorkflows) revokeCohostLink(ctx context.Context, invocation *Invocation, input domain.CohostLinkInput) (domain.CohostLinkResult, error) {
	result := domain.CohostLinkResult{EventID: input.EventID}
	result.Link.State = domain.CohostLinkRevoked
	if err := validateEvent(input.EventID); err != nil || input.DryRun {
		return result, err
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return domain.CohostLinkResult{}, err
	}
	if _, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return workflows.remote.RevokeCohostLink(ctx, transport.CohostLinkRequest{Credential: authorization.Credential, EventID: transport.EventID(input.EventID)})
	}); err != nil {
		return domain.CohostLinkResult{}, err
	}
	return result, nil
}

func (workflows peopleWorkflows) resolveContact(ctx context.Context, selector domain.ContactSelector) (PeopleAuthorization, transport.Contact, error) {
	if err := validateSelector(selector); err != nil {
		return PeopleAuthorization{}, transport.Contact{}, err
	}
	authorization, err := workflows.authorize(ctx)
	if err != nil {
		return PeopleAuthorization{}, transport.Contact{}, err
	}
	contacts, err := workflows.materializeContacts(ctx, authorization)
	if err != nil {
		return PeopleAuthorization{}, transport.Contact{}, err
	}
	matches := make([]transport.Contact, 0, 1)
	for _, contact := range contacts {
		matched := false
		if selector.Contact != nil {
			matched = strings.EqualFold(strings.TrimSpace(*selector.Contact), strings.TrimSpace(contact.DisplayName))
		} else {
			reference, deriveErr := DeriveContactReference(authorization.InstallationSecret, authorization.AccountIdentity, contact.ContactID)
			if deriveErr != nil {
				return PeopleAuthorization{}, transport.Contact{}, deriveErr
			}
			matched = subtle.ConstantTimeCompare([]byte(reference), []byte(*selector.ContactRef)) == 1
		}
		if matched {
			matches = append(matches, contact)
		}
	}
	if len(matches) == 0 {
		if selector.ContactRef != nil {
			return PeopleAuthorization{}, transport.Contact{}, &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_CONTACT_REF", Message: "the contact reference is invalid or expired"}
		}
		return PeopleAuthorization{}, transport.Contact{}, &domain.Error{Type: domain.ErrorResourceNotFound, Code: "CONTACT_NOT_FOUND", Message: "no contact matched the selector"}
	}
	if len(matches) > 1 {
		candidates := make([]domain.Contact, len(matches))
		for index, match := range matches {
			reference, deriveErr := DeriveContactReference(authorization.InstallationSecret, authorization.AccountIdentity, match.ContactID)
			if deriveErr != nil {
				return PeopleAuthorization{}, transport.Contact{}, deriveErr
			}
			candidates[index] = domain.Contact{ContactRef: reference, DisplayName: match.DisplayName, SharedEventCount: match.SharedEventCount}
		}
		return PeopleAuthorization{}, transport.Contact{}, &domain.Error{Type: domain.ErrorMatchAmbiguous, Code: "CONTACT_AMBIGUOUS", Message: "more than one contact matched the selector", Details: domain.ErrorDetails{Candidates: candidates}}
	}
	return authorization, matches[0], nil
}

func validateEventAndSelector(eventID domain.EventID, selector domain.ContactSelector) error {
	if err := validateEvent(eventID); err != nil {
		return err
	}
	return validateSelector(selector)
}

func validateEvent(eventID domain.EventID) error {
	if strings.TrimSpace(string(eventID)) == "" {
		return invalidPeopleInput()
	}
	return nil
}

func validateSelector(selector domain.ContactSelector) error {
	count := 0
	if selector.ContactRef != nil {
		count++
		if strings.TrimSpace(string(*selector.ContactRef)) == "" {
			return invalidSelector()
		}
	}
	if selector.Contact != nil {
		count++
		if strings.TrimSpace(*selector.Contact) == "" {
			return invalidSelector()
		}
	}
	if count != 1 {
		return invalidSelector()
	}
	return nil
}

func validateRSVPInput(input domain.SetRSVPInput) error {
	if err := validateEvent(input.EventID); err != nil {
		return err
	}
	switch input.Status {
	case domain.RSVPIntentInterested, domain.RSVPIntentNotInterested:
		if input.DisplayName != nil || input.PartySize != nil || len(input.PlusOnes) != 0 || input.Timezone != nil || input.Message != nil || len(input.QuestionnaireResponse) != 0 {
			return invalidPeopleInput()
		}
	case domain.RSVPIntentGoing, domain.RSVPIntentNotGoing:
		if input.DisplayName == nil || strings.TrimSpace(*input.DisplayName) == "" || input.PartySize == nil || *input.PartySize < 1 || len(input.PlusOnes) != *input.PartySize-1 || input.Timezone == nil {
			return invalidPeopleInput()
		}
		if _, err := time.LoadLocation(string(*input.Timezone)); err != nil {
			return invalidPeopleInput()
		}
		for _, name := range input.PlusOnes {
			if strings.TrimSpace(name) == "" {
				return invalidPeopleInput()
			}
		}
	default:
		return invalidPeopleInput()
	}
	return nil
}

func invalidSelector() error {
	return &domain.Error{Type: domain.ErrorUsageInvalid, Code: "INVALID_CONTACT_SELECTOR", Message: "provide exactly one contact selector"}
}

func invalidPeopleInput() error {
	return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_PEOPLE_INPUT", Message: "people operation input is invalid"}
}
