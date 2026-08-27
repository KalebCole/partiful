package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

type eventCredentials struct {
	authorization auth.Authorization
	err           error
	calls         int
}

func (provider *eventCredentials) Acquire(context.Context) (auth.Authorization, error) {
	provider.calls++
	return provider.authorization, provider.err
}

type eventCallable struct {
	upcoming      transport.ListHomeEventsResult
	past          transport.ListHomeEventsResult
	event         transport.GetEventResult
	createRequest *transport.CreateEventRequest
	cancelRequest *transport.CancelEventRequest
	createCalls   int
	cancelCalls   int
	listCalls     int
	getCalls      int
	mutationErr   error
}

func (remote *eventCallable) ListUpcomingEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	remote.listCalls++
	return remote.upcoming, nil
}
func (remote *eventCallable) ListPastEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	remote.listCalls++
	return remote.past, nil
}
func (remote *eventCallable) GetEvent(context.Context, transport.GetEventRequest) (transport.GetEventResult, error) {
	remote.getCalls++
	return remote.event, nil
}
func (remote *eventCallable) CreateEvent(_ context.Context, request transport.CreateEventRequest) (transport.Completion, error) {
	remote.createCalls++
	copy := request
	remote.createRequest = &copy
	return transport.Completion{DispatchState: transport.DispatchStarted}, remote.mutationErr
}
func (remote *eventCallable) CancelEvent(_ context.Context, request transport.CancelEventRequest) (transport.Completion, error) {
	remote.cancelCalls++
	copy := request
	remote.cancelRequest = &copy
	return transport.Completion{DispatchState: transport.DispatchStarted}, remote.mutationErr
}
func (*eventCallable) GetContacts(context.Context, transport.GetContactsRequest) (transport.GetContactsResult, error) {
	return transport.GetContactsResult{}, errors.New("unused")
}
func (*eventCallable) GetGuests(context.Context, transport.GetGuestsRequest) (transport.GetGuestsResult, error) {
	return transport.GetGuestsResult{}, errors.New("unused")
}
func (*eventCallable) InviteGuest(context.Context, transport.InviteGuestRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) GetCurrentGuest(context.Context, transport.GetCurrentGuestRequest) (transport.GetCurrentGuestResult, error) {
	return transport.GetCurrentGuestResult{}, errors.New("unused")
}
func (*eventCallable) SetGuest(context.Context, transport.SetGuestRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) MarkInterest(context.Context, transport.MarkInterestRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) InviteCohost(context.Context, transport.CohostRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) RevokeCohostInvite(context.Context, transport.CohostRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) RemoveCohost(context.Context, transport.CohostRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) CreateCohostLink(context.Context, transport.CohostLinkRequest) (transport.CohostLinkResult, error) {
	return transport.CohostLinkResult{}, errors.New("unused")
}
func (*eventCallable) RevokeCohostLink(context.Context, transport.CohostLinkRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}
func (*eventCallable) CreateTextBlast(context.Context, transport.CreateTextBlastRequest) (transport.Completion, error) {
	return transport.Completion{}, errors.New("unused")
}

type eventFirestore struct {
	request *transport.PatchEventDocumentRequest
	calls   int
	err     error
}

func (*eventFirestore) GetEvent(context.Context, transport.GetEventDocumentRequest) (transport.EventDocument, error) {
	return transport.EventDocument{}, errors.New("unused")
}
func (remote *eventFirestore) PatchEvent(_ context.Context, request transport.PatchEventDocumentRequest) (transport.EventDocument, error) {
	remote.calls++
	copy := request
	remote.request = &copy
	return transport.EventDocument{EventID: request.EventID}, remote.err
}
func (*eventFirestore) GetGuest(context.Context, transport.GetGuestDocumentRequest) (transport.GuestDocument, error) {
	return transport.GuestDocument{}, errors.New("unused")
}
func (*eventFirestore) ListEventGuests(context.Context, transport.ListEventDocumentsRequest) (transport.GuestDocumentPage, error) {
	return transport.GuestDocumentPage{}, errors.New("unused")
}
func (*eventFirestore) ListEventHostMessages(context.Context, transport.ListEventDocumentsRequest) (transport.HostMessageDocumentPage, error) {
	return transport.HostMessageDocumentPage{}, errors.New("unused")
}

type eventPosters struct {
	posters []transport.Poster
	calls   int
}

func (remote *eventPosters) GetCatalog(context.Context, transport.GetPosterCatalogRequest) (transport.GetPosterCatalogResult, error) {
	remote.calls++
	return transport.GetPosterCatalogResult{Posters: remote.posters}, nil
}

func eventService(t *testing.T, gates GateManifest, credentials *eventCredentials, callable *eventCallable, firestore *eventFirestore, posters *eventPosters) *Service {
	t.Helper()
	service := testService(t, gates)
	codec, err := NewCursorCodec([]byte("event-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := BindEventOperations(service, credentials, callable, firestore, posters, codec); err != nil {
		t.Fatal(err)
	}
	return service
}

func closedEventGates(t *testing.T) GateManifest {
	t.Helper()
	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatal(err)
	}
	entries := manifest.Entries()
	for index := range entries {
		entries[index].State = GateClosed
	}
	closed, err := NewGateManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	return closed
}

func TestEventOperationGatesPreventAuthenticationAndDispatch(t *testing.T) {
	manifest, _ := DefaultGateManifest()
	credentials := &eventCredentials{}
	callable := &eventCallable{}
	firestore := &eventFirestore{}
	posters := &eventPosters{}
	service := eventService(t, manifest, credentials, callable, firestore, posters)

	for _, test := range []struct {
		operation domain.OperationID
		input     any
	}{
		{domain.OperationListEvents, domain.ListEventsInput{When: domain.EventWhenUpcoming}},
		{domain.OperationGetEvent, domain.GetEventInput{EventID: "event"}},
		{domain.OperationCreateEvent, domain.CreateEventInput{Title: "Party", Start: time.Now(), Timezone: "UTC"}},
	} {
		_, err := service.Invoke(context.Background(), test.operation, test.input)
		var public *domain.Error
		if !errors.As(err, &public) || public.Code != "EVIDENCE_GATE_OPEN" {
			t.Fatalf("%s error = %#v", test.operation, err)
		}
	}
	if credentials.calls != 0 || callable.listCalls != 0 || callable.getCalls != 0 || callable.createCalls != 0 || firestore.calls != 0 || posters.calls != 0 {
		t.Fatalf("open gates touched dependencies: credentials=%d callable=%+v firestore=%d posters=%d", credentials.calls, callable, firestore.calls, posters.calls)
	}
}

func TestListEventsProjectsClosedEnumsAndSlicesLocally(t *testing.T) {
	title := "Party"
	state, role, rsvp := "PUBLISHED", "host", "GOING"
	remote := &eventCallable{upcoming: transport.ListHomeEventsResult{Events: []transport.EventSummary{
		{EventID: "first", Title: &title, State: &state, UserRole: &role, RSVPStatus: &rsvp},
		{EventID: "second"},
	}}}
	credentials := &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}
	service := eventService(t, closedEventGates(t), credentials, remote, &eventFirestore{}, &eventPosters{})

	got, err := service.Invoke(context.Background(), domain.OperationListEvents, domain.ListEventsInput{When: domain.EventWhenUpcoming, CollectionInput: domain.CollectionInput{Limit: 1}})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(domain.EventsResult)
	if len(result.Events) != 1 || result.Events[0].EventID != "first" || result.Events[0].State == nil || *result.Events[0].State != "active" || result.Events[0].MyRSVP == nil || *result.Events[0].MyRSVP != domain.EventReadRSVPGoing || !result.HasMore || result.NextCursor == nil {
		t.Fatalf("result = %#v", result)
	}
	if credentials.calls != 1 || remote.listCalls != 1 {
		t.Fatalf("calls = credentials:%d list:%d", credentials.calls, remote.listCalls)
	}
}

func TestGetEventProjectsOnlyApplicationVisibleFields(t *testing.T) {
	title, timezone, state, role, rsvp := "Party", "America/Los_Angeles", "CANCELED", "guest", "MAYBE"
	description, location := "Description", "Seattle"
	remote := &eventCallable{event: transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event", Title: &title, Timezone: &timezone, State: &state, UserRole: &role, RSVPStatus: &rsvp}, Description: &description, Location: &location}}}
	service := eventService(t, closedEventGates(t), &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}, remote, &eventFirestore{}, &eventPosters{})

	got, err := service.Invoke(context.Background(), domain.OperationGetEvent, domain.GetEventInput{EventID: "event"})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(domain.EventResult)
	if result.Event.EventID != "event" || result.Event.Timezone == nil || *result.Event.Timezone != domain.Timezone(timezone) || result.Event.State == nil || *result.Event.State != "cancelled" || result.Event.MyRSVP == nil || *result.Event.MyRSVP != domain.EventReadRSVPMaybe || result.Event.Description == nil || result.Event.Location == nil || result.Event.Address != nil || result.Event.GuestLimit != nil || result.Event.Poster != nil || result.Event.Links != nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateEventDryRunIsLocalAndExactPosterMatchDispatchesOnce(t *testing.T) {
	poster := transport.Poster{PosterID: "poster", Name: "Poster", URL: "https://example.invalid/poster", ContentType: "image/png", Width: 1, Height: 1, Tags: []string{}, Categories: []string{}}
	credentials := &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}
	callable := &eventCallable{}
	posters := &eventPosters{posters: []transport.Poster{poster}}
	service := eventService(t, closedEventGates(t), credentials, callable, &eventFirestore{}, posters)
	start := time.Date(2026, 8, 30, 18, 0, 0, 0, time.UTC)

	got, err := service.Invoke(context.Background(), domain.OperationCreateEvent, domain.CreateEventInput{Title: "Party", Start: start, Timezone: "UTC", PosterID: pointer(domain.PosterID("poster")), DryRun: true})
	if err != nil || got.(domain.SubmittedResult).Submitted {
		t.Fatalf("dry run = %#v, %v", got, err)
	}
	if credentials.calls != 0 || posters.calls != 0 || callable.createCalls != 0 {
		t.Fatal("dry run touched dependencies")
	}

	got, err = service.Invoke(context.Background(), domain.OperationCreateEvent, domain.CreateEventInput{Title: "Party", Start: start, Timezone: "UTC", PosterID: pointer(domain.PosterID("poster"))})
	if err != nil || !got.(domain.SubmittedResult).Submitted {
		t.Fatalf("create = %#v, %v", got, err)
	}
	if credentials.calls != 1 || posters.calls != 1 || callable.createCalls != 1 || callable.createRequest == nil || callable.createRequest.PosterID == nil || *callable.createRequest.PosterID != "poster" {
		t.Fatalf("create dependencies = credentials:%d posters:%d callable:%d request:%#v", credentials.calls, posters.calls, callable.createCalls, callable.createRequest)
	}
}

func TestEventMutationDryRunsValidateIntentWithoutTransportSupport(t *testing.T) {
	credentials := &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}
	callable := &eventCallable{}
	firestore := &eventFirestore{}
	posters := &eventPosters{}
	service := eventService(t, closedEventGates(t), credentials, callable, firestore, posters)
	links := []domain.EventLink{{Label: "site", URL: "https://example.invalid"}}
	posterID := domain.PosterID("poster")

	created, err := service.Invoke(context.Background(), domain.OperationCreateEvent, domain.CreateEventInput{
		Title: "Party", Start: time.Now(), Timezone: "UTC", Links: links, DryRun: true,
	})
	if err != nil || created.(domain.SubmittedResult).Submitted {
		t.Fatalf("create dry run = %#v, %v", created, err)
	}
	updated, err := service.Invoke(context.Background(), domain.OperationUpdateEvent, domain.UpdateEventInput{
		EventID: "event", Description: domain.Change[string]{Set: true},
		Links:    domain.Change[[]domain.EventLink]{Set: true, Value: &links},
		PosterID: domain.Change[domain.PosterID]{Set: true, Value: &posterID}, DryRun: true,
	})
	if err != nil {
		t.Fatalf("update dry run = %#v, %v", updated, err)
	}
	result := updated.(domain.UpdateEventResult)
	if result.Submitted || !reflect.DeepEqual(result.Fields, []string{"description", "links", "poster_id"}) {
		t.Fatalf("update dry run result = %#v", result)
	}
	if credentials.calls != 0 || callable.createCalls != 0 || firestore.calls != 0 || posters.calls != 0 {
		t.Fatalf("dry runs touched dependencies: credentials=%d create=%d patch=%d posters=%d", credentials.calls, callable.createCalls, firestore.calls, posters.calls)
	}
}

func TestCreateEventRejectsDuplicatePosterIDsBeforeAuthentication(t *testing.T) {
	poster := transport.Poster{PosterID: "duplicate", Name: "Poster", URL: "https://example.invalid/poster", ContentType: "image/png", Tags: []string{}, Categories: []string{}}
	credentials := &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}
	callable := &eventCallable{}
	posters := &eventPosters{posters: []transport.Poster{poster, poster}}
	service := eventService(t, closedEventGates(t), credentials, callable, &eventFirestore{}, posters)

	_, err := service.Invoke(context.Background(), domain.OperationCreateEvent, domain.CreateEventInput{Title: "Party", Start: time.Now(), Timezone: "UTC", PosterID: pointer(domain.PosterID("duplicate"))})
	var public *domain.Error
	if !errors.As(err, &public) || public.Type != domain.ErrorMatchAmbiguous {
		t.Fatalf("error = %#v", err)
	}
	if credentials.calls != 0 || callable.createCalls != 0 {
		t.Fatalf("duplicate poster dispatched: credentials=%d create=%d", credentials.calls, callable.createCalls)
	}
}

func TestUpdateEventMapsSupportedFieldsWithSortedMaskAndOneAttempt(t *testing.T) {
	title, timezone, limit := "New title", domain.Timezone("UTC"), 20
	start := time.Date(2026, 9, 1, 20, 0, 0, 0, time.FixedZone("offset", -7*60*60))
	credentials := &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}
	firestore := &eventFirestore{}
	service := eventService(t, closedEventGates(t), credentials, &eventCallable{}, firestore, &eventPosters{})
	input := domain.UpdateEventInput{EventID: "event", Title: domain.Change[string]{Set: true, Value: &title}, Start: domain.Change[time.Time]{Set: true, Value: &start}, Timezone: domain.Change[domain.Timezone]{Set: true, Value: &timezone}, GuestLimit: domain.Change[int]{Set: true, Value: &limit}}

	got, err := service.Invoke(context.Background(), domain.OperationUpdateEvent, input)
	if err != nil {
		t.Fatal(err)
	}
	result := got.(domain.UpdateEventResult)
	if !result.Submitted || !reflect.DeepEqual(result.Fields, []string{"guest_limit", "start", "timezone", "title"}) {
		t.Fatalf("result = %#v", result)
	}
	if firestore.calls != 1 || firestore.request == nil || !firestore.request.MustExist || !reflect.DeepEqual(firestore.request.FieldMask, []string{"enableWaitlist", "maxCapacity", "startDate", "timezone", "title"}) {
		t.Fatalf("request = %#v", firestore.request)
	}
	if value := firestore.request.Fields["startDate"].String; value == nil || *value != start.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("startDate = %#v", value)
	}
}

func TestUpdateEventFailsClosedForUnrepresentableClearLinksAndPoster(t *testing.T) {
	service := eventService(t, closedEventGates(t), &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}, &eventCallable{}, &eventFirestore{}, &eventPosters{})
	for _, input := range []domain.UpdateEventInput{
		{EventID: "event", Description: domain.Change[string]{Set: true}},
		{EventID: "event", Links: domain.Change[[]domain.EventLink]{Set: true, Value: &[]domain.EventLink{{Label: "site", URL: "https://example.invalid"}}}},
		{EventID: "event", PosterID: domain.Change[domain.PosterID]{Set: true, Value: pointer(domain.PosterID("poster"))}},
	} {
		_, err := service.Invoke(context.Background(), domain.OperationUpdateEvent, input)
		var public *domain.Error
		if !errors.As(err, &public) || public.Code != "EVIDENCE_GATE_OPEN" {
			t.Fatalf("input %#v error = %#v", input, err)
		}
	}
}

func TestCancelEventInvertsNotificationFlagAndNeverRetries(t *testing.T) {
	message := "cancelled"
	callable := &eventCallable{mutationErr: &transport.ProtocolFailure{Operation: "cancelEvent", Class: "remote.unavailable", DispatchState: transport.DispatchStarted}}
	service := eventService(t, closedEventGates(t), &eventCredentials{authorization: auth.Authorization{AccessToken: auth.NewSecret("credential")}}, callable, &eventFirestore{}, &eventPosters{})

	_, err := service.Invoke(context.Background(), domain.OperationCancelEvent, domain.CancelEventInput{EventID: "event", Message: &message, NotifyGuests: true})
	if err == nil || callable.cancelCalls != 1 || callable.cancelRequest == nil || callable.cancelRequest.SkipNotifyGuests {
		t.Fatalf("cancel = calls:%d request:%#v error:%v", callable.cancelCalls, callable.cancelRequest, err)
	}
}

func pointer[T any](value T) *T { return &value }
