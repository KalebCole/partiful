package app

import (
	"context"
	"errors"
	"testing"

	"github.com/KalebCole/partiful/internal/command"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

func TestListContactsTraversesPrivatelyProjectsAndSlices(t *testing.T) {
	service, remote, provider := newPeopleTestService(t)
	cursor := transport.RemoteCursor("remote-page")
	remote.contactPages = []transport.GetContactsResult{
		{Contacts: []transport.Contact{{ContactID: "contact-a", DisplayName: "Alpha", SharedEventCount: 2}, {ContactID: "contact-b", DisplayName: "Beta"}, {ContactID: "contact-a", DisplayName: "Duplicate"}}, Cursor: &cursor},
		{},
	}

	resultAny, err := service.Invoke(context.Background(), domain.OperationListContacts, domain.ListContactsInput{Query: stringPtr("alp"), CollectionInput: domain.CollectionInput{Limit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(domain.ContactsResult)
	wantRef, _ := DeriveContactReference(provider.authorization.InstallationSecret, provider.authorization.AccountIdentity, "contact-a")
	if len(result.Contacts) != 1 || result.Contacts[0].ContactRef != wantRef || result.Contacts[0].DisplayName != "Alpha" || result.Contacts[0].SharedEventCount != 2 || result.HasMore || result.NextCursor != nil {
		t.Fatal("contacts result did not preserve the projected filtered contact")
	}
	if remote.contactCalls != 2 || remote.contactRequests[0].MaxResults != 1000 || remote.contactRequests[0].Cursor != nil || remote.contactRequests[1].Cursor == nil || *remote.contactRequests[1].Cursor != cursor {
		t.Fatal("contact traversal did not copy the server cursor through the private transport")
	}
}

func TestListContactsRejectsInvalidCollectionBeforeTransport(t *testing.T) {
	service, remote, provider := newPeopleTestService(t)
	provider.acquireCalls = 0
	_, err := service.Invoke(context.Background(), domain.OperationListContacts, domain.ListContactsInput{CollectionInput: domain.CollectionInput{Limit: 101}})
	assertPeopleErrorType(t, err, domain.ErrorUsageInvalid)
	if provider.acquireCalls != 0 || remote.contactCalls != 0 {
		t.Fatal("invalid collection input acquired credentials or reached transport")
	}
}

func TestContactReferencesAreInstallationAndAccountScoped(t *testing.T) {
	first, err := DeriveContactReference([]byte("installation-one"), "account-one", "contact")
	if err != nil {
		t.Fatal(err)
	}
	same, _ := DeriveContactReference([]byte("installation-one"), "account-one", "contact")
	otherInstallation, _ := DeriveContactReference([]byte("installation-two"), "account-one", "contact")
	otherAccount, _ := DeriveContactReference([]byte("installation-one"), "account-two", "contact")
	if first == "" || first != same || first == otherInstallation || first == otherAccount || first == domain.ContactRef("contact") {
		t.Fatal("contact reference is not stable and scoped")
	}
}

func TestInviteGuestResolvesExactlyOneSelectorAndDispatchesOnce(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.contactPages = []transport.GetContactsResult{{Contacts: []transport.Contact{{ContactID: "contact-a", DisplayName: "Alpha"}, {ContactID: "contact-b", DisplayName: "Alpha"}}}}

	_, err := service.Invoke(context.Background(), domain.OperationInviteGuest, domain.InviteGuestInput{EventID: "event", ContactSelector: domain.ContactSelector{Contact: stringPtr("Alpha")}})
	assertPeopleErrorType(t, err, domain.ErrorMatchAmbiguous)
	var ambiguous *domain.Error
	if !errors.As(err, &ambiguous) || len(ambiguous.Details.Candidates) != 2 || ambiguous.Details.Candidates[0].ContactRef == "" || ambiguous.Details.Candidates[0].ContactRef == domain.ContactRef("contact-a") {
		t.Fatal("ambiguous selector did not return safe opaque candidates")
	}
	if remote.inviteGuestCalls != 0 {
		t.Fatal("ambiguous selector dispatched a mutation")
	}

	remote.contactPages = []transport.GetContactsResult{{Contacts: []transport.Contact{{ContactID: "contact-a", DisplayName: "Alpha"}}}}
	resultAny, err := service.Invoke(context.Background(), domain.OperationInviteGuest, domain.InviteGuestInput{EventID: "event", ContactSelector: domain.ContactSelector{Contact: stringPtr("Alpha")}, Message: stringPtr("sanitized-content")})
	if err != nil || !resultAny.(domain.SubmittedResult).Submitted || remote.inviteGuestCalls != 1 || remote.lastInviteGuest.ContactID != "contact-a" || remote.lastInviteGuest.Message != "sanitized-content" {
		t.Fatal("guest invitation did not resolve and dispatch exactly once")
	}

	_, err = service.Invoke(context.Background(), domain.OperationInviteGuest, domain.InviteGuestInput{EventID: "event", ContactSelector: domain.ContactSelector{Contact: stringPtr("Alpha"), ContactRef: contactRefPtr("ref")}})
	assertPeopleErrorType(t, err, domain.ErrorUsageInvalid)
	if remote.inviteGuestCalls != 1 {
		t.Fatal("invalid selector dispatched a mutation")
	}
}

func TestContactReferenceResolutionRejectsModifiedReference(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.contactPages = []transport.GetContactsResult{{Contacts: []transport.Contact{{ContactID: "contact-a", DisplayName: "Alpha"}}}}
	_, err := service.Invoke(context.Background(), domain.OperationInviteGuest, domain.InviteGuestInput{EventID: "event", ContactSelector: domain.ContactSelector{ContactRef: contactRefPtr("modified")}})
	assertPeopleErrorCode(t, err, "INVALID_CONTACT_REF")
	if remote.inviteGuestCalls != 0 {
		t.Fatal("modified reference dispatched a mutation")
	}
}

func TestListGuestsOpenProjectionGateMakesZeroTransportCalls(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	_, err := service.Invoke(context.Background(), domain.OperationListGuests, domain.ListGuestsInput{EventID: "event"})
	assertPeopleErrorCode(t, err, "EVIDENCE_GATE_OPEN")
	if remote.guestCalls != 0 {
		t.Fatal("open guest projection gate dispatched a read")
	}
}

func TestGetRSVPProjectsNullAndClosedStatus(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	resultAny, err := service.Invoke(context.Background(), domain.OperationGetRSVP, domain.GetEventInput{EventID: "event"})
	if err != nil || resultAny.(domain.RSVPResult).Status != nil {
		t.Fatal("null current guest was not preserved")
	}
	remote.currentGuest = &transport.CurrentGuest{GuestID: "guest", Status: "GOING"}
	resultAny, err = service.Invoke(context.Background(), domain.OperationGetRSVP, domain.GetEventInput{EventID: "event"})
	if err != nil || resultAny.(domain.RSVPResult).Status == nil || *resultAny.(domain.RSVPResult).Status != domain.EventReadRSVPGoing {
		t.Fatal("current guest status was not projected")
	}
}

func TestSetRSVPUsesExactBranchesAndOneMutationAttempt(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.currentGuest = &transport.CurrentGuest{GuestID: "guest", Status: "INTERESTED"}
	input := domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentGoing, DisplayName: stringPtr("Guest"), PartySize: intPtr(2), PlusOnes: []string{"Plus one"}, Timezone: timezonePtr("America/Los_Angeles")}
	resultAny, err := service.Invoke(context.Background(), domain.OperationSetRSVP, input)
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(domain.SetRSVPResult)
	if !result.Submitted || result.Intent != domain.RSVPIntentGoing || remote.currentGuestCalls != 1 || remote.setGuestCalls != 1 || remote.lastSetGuest.GuestID == nil || remote.lastSetGuest.Status != "GOING" {
		t.Fatal("going RSVP did not read the current guest then submit one exact mutation")
	}

	resultAny, err = service.Invoke(context.Background(), domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentNotInterested})
	if err != nil || !resultAny.(domain.SetRSVPResult).Submitted || remote.interestCalls != 1 || remote.lastInterest.Interested {
		t.Fatal("not-interested RSVP did not use the interest toggle")
	}
}

func TestSetRSVPRejectsInvalidAndGatedSpecialPathsBeforeTransport(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	_, err := service.Invoke(context.Background(), domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentGoing, DisplayName: stringPtr("Guest"), PartySize: intPtr(2), Timezone: timezonePtr("America/Los_Angeles")})
	assertPeopleErrorType(t, err, domain.ErrorInputInvalid)
	_, err = service.Invoke(context.Background(), domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentInterested, DisplayName: stringPtr("Guest")})
	assertPeopleErrorType(t, err, domain.ErrorInputInvalid)
	_, err = service.Invoke(context.Background(), domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentGoing, DisplayName: stringPtr("Guest"), PartySize: intPtr(1), Timezone: timezonePtr("America/Los_Angeles"), QuestionnaireResponse: map[string]string{"question": "answer"}})
	assertPeopleErrorCode(t, err, "EVIDENCE_GATE_OPEN")
	if remote.currentGuestCalls != 0 || remote.setGuestCalls != 0 || remote.interestCalls != 0 {
		t.Fatal("invalid or gated RSVP input reached transport")
	}
}

func TestSetRSVPUnknownRemoteErrorFailsClosedAsEvidenceClaim(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.currentGuestError = errors.New("sanitized unknown failure")
	_, err := service.Invoke(context.Background(), domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentGoing, DisplayName: stringPtr("Guest"), PartySize: intPtr(1), Timezone: timezonePtr("America/Los_Angeles")})
	assertPeopleErrorCode(t, err, "EVIDENCE_CLAIM_OPEN")
	if remote.currentGuestCalls != 1 || remote.setGuestCalls != 0 {
		t.Fatal("unknown pre-read outcome dispatched or retried the mutation")
	}
}

func TestCohostCommandsReturnIntentWithoutStateRead(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.contactPages = []transport.GetContactsResult{{Contacts: []transport.Contact{{ContactID: "contact-a", DisplayName: "Alpha"}}}}
	operations := []struct {
		operation domain.OperationID
		status    domain.CohostStatus
	}{
		{domain.OperationInviteCohost, domain.CohostStatusInvited},
		{domain.OperationRevokeCohostInvite, domain.CohostStatusRevoked},
		{domain.OperationRemoveCohost, domain.CohostStatusRemoved},
	}
	for _, test := range operations {
		resultAny, err := service.Invoke(context.Background(), test.operation, domain.CohostInput{EventID: "event", ContactSelector: domain.ContactSelector{Contact: stringPtr("Alpha")}})
		if err != nil {
			t.Fatal(err)
		}
		result := resultAny.(domain.CohostResult)
		if result.Cohost.DisplayName != "Alpha" || result.Cohost.Status != test.status {
			t.Fatal("cohost command did not return its narrow intent")
		}
	}
	if remote.inviteCohostCalls != 1 || remote.revokeCohostCalls != 1 || remote.removeCohostCalls != 1 {
		t.Fatal("cohost mutations were not dispatched exactly once")
	}
}

func TestCohostLinkCommandsReturnOnlyReviewedIntent(t *testing.T) {
	service, remote, _ := newPeopleTestService(t)
	remote.cohostURL = "https://partiful.com/e/sanitized?accept-cohost=sanitized"
	resultAny, err := service.Invoke(context.Background(), domain.OperationCreateCohostLink, domain.CohostLinkInput{EventID: "event"})
	if err != nil {
		t.Fatal(err)
	}
	created := resultAny.(domain.CohostLinkResult)
	if created.Link.URL == nil || *created.Link.URL != remote.cohostURL || created.Link.State != domain.CohostLinkActive {
		t.Fatal("cohost link creation did not return the reviewed URL")
	}
	resultAny, err = service.Invoke(context.Background(), domain.OperationRevokeCohostLink, domain.CohostLinkInput{EventID: "event"})
	if err != nil {
		t.Fatal(err)
	}
	revoked := resultAny.(domain.CohostLinkResult)
	if revoked.Link.URL != nil || revoked.Link.State != domain.CohostLinkRevoked || remote.createLinkCalls != 1 || remote.revokeLinkCalls != 1 {
		t.Fatal("cohost link revocation did not return revoked intent")
	}
}

func TestPeopleMutationDryRunsPerformNoAuthenticationResolutionOrDispatch(t *testing.T) {
	service, remote, provider := newPeopleTestService(t)
	provider.acquireCalls = 0
	cases := []struct {
		operation domain.OperationID
		input     any
	}{
		{domain.OperationInviteGuest, domain.InviteGuestInput{EventID: "event", ContactSelector: domain.ContactSelector{ContactRef: contactRefPtr("opaque")}, DryRun: true}},
		{domain.OperationSetRSVP, domain.SetRSVPInput{EventID: "event", Status: domain.RSVPIntentInterested, DryRun: true}},
		{domain.OperationInviteCohost, domain.CohostInput{EventID: "event", ContactSelector: domain.ContactSelector{ContactRef: contactRefPtr("opaque")}, DryRun: true}},
		{domain.OperationCreateCohostLink, domain.CohostLinkInput{EventID: "event", DryRun: true}},
	}
	for _, test := range cases {
		if _, err := service.Invoke(context.Background(), test.operation, test.input); err != nil {
			t.Fatalf("dry-run %s failed: %v", test.operation, err)
		}
	}
	if provider.acquireCalls != 0 || remote.totalCalls() != 0 {
		t.Fatal("dry-run acquired credentials or reached transport")
	}
}

type fakePeopleProvider struct {
	authorization PeopleAuthorization
	acquireCalls  int
}

func (provider *fakePeopleProvider) Acquire(context.Context) (PeopleAuthorization, error) {
	provider.acquireCalls++
	return provider.authorization, nil
}

type fakePeopleTransport struct {
	contactPages      []transport.GetContactsResult
	contactCalls      int
	contactRequests   []transport.GetContactsRequest
	guestCalls        int
	inviteGuestCalls  int
	lastInviteGuest   transport.InviteGuestRequest
	currentGuest      *transport.CurrentGuest
	currentGuestError error
	currentGuestCalls int
	setGuestCalls     int
	lastSetGuest      transport.SetGuestRequest
	interestCalls     int
	lastInterest      transport.MarkInterestRequest
	inviteCohostCalls int
	revokeCohostCalls int
	removeCohostCalls int
	cohostURL         string
	createLinkCalls   int
	revokeLinkCalls   int
}

func (fake *fakePeopleTransport) GetContacts(_ context.Context, request transport.GetContactsRequest) (transport.GetContactsResult, error) {
	fake.contactRequests = append(fake.contactRequests, request)
	index := fake.contactCalls
	fake.contactCalls++
	if len(fake.contactPages) == 1 {
		if request.Cursor != nil {
			return transport.GetContactsResult{}, nil
		}
		result := fake.contactPages[0]
		if result.Cursor == nil {
			cursor := transport.RemoteCursor("terminal-sentinel")
			result.Cursor = &cursor
		}
		return result, nil
	}
	if index >= len(fake.contactPages) {
		return transport.GetContactsResult{}, nil
	}
	return fake.contactPages[index], nil
}
func (fake *fakePeopleTransport) GetGuests(context.Context, transport.GetGuestsRequest) (transport.GetGuestsResult, error) {
	fake.guestCalls++
	return transport.GetGuestsResult{}, nil
}
func (fake *fakePeopleTransport) InviteGuest(_ context.Context, request transport.InviteGuestRequest) (transport.Completion, error) {
	fake.inviteGuestCalls++
	fake.lastInviteGuest = request
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) GetCurrentGuest(context.Context, transport.GetCurrentGuestRequest) (transport.GetCurrentGuestResult, error) {
	fake.currentGuestCalls++
	return transport.GetCurrentGuestResult{Guest: fake.currentGuest}, fake.currentGuestError
}
func (fake *fakePeopleTransport) SetGuest(_ context.Context, request transport.SetGuestRequest) (transport.Completion, error) {
	fake.setGuestCalls++
	fake.lastSetGuest = request
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) MarkInterest(_ context.Context, request transport.MarkInterestRequest) (transport.Completion, error) {
	fake.interestCalls++
	fake.lastInterest = request
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) InviteCohost(context.Context, transport.CohostRequest) (transport.Completion, error) {
	fake.inviteCohostCalls++
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) RevokeCohostInvite(context.Context, transport.CohostRequest) (transport.Completion, error) {
	fake.revokeCohostCalls++
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) RemoveCohost(context.Context, transport.CohostRequest) (transport.Completion, error) {
	fake.removeCohostCalls++
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (fake *fakePeopleTransport) CreateCohostLink(context.Context, transport.CohostLinkRequest) (transport.CohostLinkResult, error) {
	fake.createLinkCalls++
	return transport.CohostLinkResult{URL: fake.cohostURL}, nil
}
func (fake *fakePeopleTransport) RevokeCohostLink(context.Context, transport.CohostLinkRequest) (transport.Completion, error) {
	fake.revokeLinkCalls++
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}
func (*fakePeopleTransport) ListUpcomingEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	return transport.ListHomeEventsResult{}, errors.New("unused")
}
func (*fakePeopleTransport) ListPastEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	return transport.ListHomeEventsResult{}, errors.New("unused")
}
func (*fakePeopleTransport) GetEvent(context.Context, transport.GetEventRequest) (transport.GetEventResult, error) {
	return transport.GetEventResult{}, errors.New("unused")
}
func (*fakePeopleTransport) CreateEvent(context.Context, transport.CreateEventRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*fakePeopleTransport) CancelEvent(context.Context, transport.CancelEventRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*fakePeopleTransport) CreateTextBlast(context.Context, transport.CreateTextBlastRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}

func (fake *fakePeopleTransport) totalCalls() int {
	return fake.contactCalls + fake.guestCalls + fake.inviteGuestCalls + fake.currentGuestCalls + fake.setGuestCalls + fake.interestCalls + fake.inviteCohostCalls + fake.revokeCohostCalls + fake.removeCohostCalls + fake.createLinkCalls + fake.revokeLinkCalls
}

func newPeopleTestService(t *testing.T) (*Service, *fakePeopleTransport, *fakePeopleProvider) {
	t.Helper()
	catalog, err := command.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(catalog, manifest)
	provider := &fakePeopleProvider{authorization: PeopleAuthorization{Credential: "credential", AccountIdentity: "account", InstallationSecret: []byte("installation-secret")}}
	remote := &fakePeopleTransport{}
	if err := BindPeopleOperations(service, provider, remote); err != nil {
		t.Fatal(err)
	}
	return service, remote, provider
}

func assertPeopleErrorType(t *testing.T, err error, want domain.ErrorType) {
	t.Helper()
	var publicError *domain.Error
	if !errors.As(err, &publicError) || publicError.Type != want {
		t.Fatalf("error type = %T, want %s", err, want)
	}
}

func assertPeopleErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var publicError *domain.Error
	if !errors.As(err, &publicError) || publicError.Code != want {
		t.Fatalf("error code did not match %s", want)
	}
}

func stringPtr(value string) *string                           { return &value }
func intPtr(value int) *int                                    { return &value }
func timezonePtr(value domain.Timezone) *domain.Timezone       { return &value }
func contactRefPtr(value domain.ContactRef) *domain.ContactRef { return &value }

var _ transport.CallableTransport = (*fakePeopleTransport)(nil)
var _ PeopleCredentialProvider = (*fakePeopleProvider)(nil)
