package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

type blastCredentials struct {
	calls int
}

func (provider *blastCredentials) Acquire(context.Context) (auth.Authorization, error) {
	provider.calls++
	return auth.Authorization{AccessToken: auth.NewSecret("credential")}, nil
}

type blastCallable struct {
	transport.CallableTransport
	event       transport.GetEventResult
	request     *transport.CreateTextBlastRequest
	getCalls    int
	createCalls int
	createErr   error
}

func (remote *blastCallable) GetEvent(context.Context, transport.GetEventRequest) (transport.GetEventResult, error) {
	remote.getCalls++
	return remote.event, nil
}

func (remote *blastCallable) CreateTextBlast(_ context.Context, request transport.CreateTextBlastRequest) (transport.Completion, error) {
	remote.createCalls++
	copy := request
	copy.Groups = append([]transport.TextBlastGroup(nil), request.Groups...)
	remote.request = &copy
	return transport.Completion{DispatchState: transport.DispatchStarted}, remote.createErr
}

type blastFirestore struct {
	transport.FirestoreTransport
	event          transport.EventDocument
	guestPages     []transport.GuestDocumentPage
	hostPages      []transport.HostMessageDocumentPage
	getEventCalls  int
	guestListCalls int
	hostListCalls  int
}

func (remote *blastFirestore) GetEvent(context.Context, transport.GetEventDocumentRequest) (transport.EventDocument, error) {
	remote.getEventCalls++
	return remote.event, nil
}

func (remote *blastFirestore) ListEventGuests(_ context.Context, request transport.ListEventDocumentsRequest) (transport.GuestDocumentPage, error) {
	index := remote.guestListCalls
	remote.guestListCalls++
	if index >= len(remote.guestPages) {
		return transport.GuestDocumentPage{}, errors.New("unexpected guest page")
	}
	if index > 0 && request.Cursor == nil {
		return transport.GuestDocumentPage{}, errors.New("missing guest cursor")
	}
	return remote.guestPages[index], nil
}

func (remote *blastFirestore) ListEventHostMessages(_ context.Context, request transport.ListEventDocumentsRequest) (transport.HostMessageDocumentPage, error) {
	index := remote.hostListCalls
	remote.hostListCalls++
	if index >= len(remote.hostPages) {
		return transport.HostMessageDocumentPage{}, errors.New("unexpected host-message page")
	}
	if index > 0 && request.Cursor == nil {
		return transport.HostMessageDocumentPage{}, errors.New("missing host-message cursor")
	}
	return remote.hostPages[index], nil
}

func blastService(t *testing.T, manifest GateManifest, credentials *blastCredentials, callable *blastCallable, firestore *blastFirestore) *Service {
	t.Helper()
	service := testService(t, manifest)
	if err := BindBlastOperation(service, credentials, callable, firestore); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSendBlastOpenOperationGatesPreventAuthenticationAndTransport(t *testing.T) {
	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatal(err)
	}
	credentials := &blastCredentials{}
	callable := &blastCallable{}
	firestore := &blastFirestore{}
	service := blastService(t, manifest, credentials, callable, firestore)

	_, err = service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content"})
	var public *domain.Error
	if !errors.As(err, &public) || public.Code != "EVIDENCE_GATE_OPEN" {
		t.Fatalf("error = %#v", err)
	}
	if credentials.calls != 0 || callable.getCalls != 0 || callable.createCalls != 0 || firestore.getEventCalls != 0 || firestore.guestListCalls != 0 || firestore.hostListCalls != 0 {
		t.Fatalf("open gates touched dependencies: credentials=%d callable=%+v firestore=%+v", credentials.calls, callable, firestore)
	}
}

func TestSendBlastDryRunIsLocal(t *testing.T) {
	credentials := &blastCredentials{}
	callable := &blastCallable{}
	firestore := &blastFirestore{}
	service := blastService(t, closedEventGates(t), credentials, callable, firestore)

	got, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content", ShowOnEventPage: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(domain.BlastResult)
	if result.EventID != "event" || result.Submitted || result.Audience != "all-guests" || !result.ShowOnEventPage || result.RecipientStatus != "not-reported" {
		t.Fatalf("result = %#v", result)
	}
	if credentials.calls != 0 || callable.getCalls != 0 || callable.createCalls != 0 || firestore.getEventCalls != 0 || firestore.guestListCalls != 0 || firestore.hostListCalls != 0 {
		t.Fatal("dry run touched dependencies")
	}
}

func TestSendBlastOpenClaimGatesPermitNarrowSuccessfulIntent(t *testing.T) {
	manifest, err := DefaultGateManifest()
	if err != nil {
		t.Fatal(err)
	}
	entries := manifest.Entries()
	for index := range entries {
		switch entries[index].Identity {
		case "OP11-GET-EVENT-REQUEST", "OP11-BLAST-FIRESTORE-READS", "OP11-BLAST-GROUPS":
			entries[index].State = GateClosed
		}
	}
	manifest, err = NewGateManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	status := "GOING"
	callable := &blastCallable{event: transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event"}}}}
	firestore := &blastFirestore{
		event:      transport.EventDocument{EventID: "event", Fields: map[string]transport.FieldValue{}},
		guestPages: []transport.GuestDocumentPage{{Documents: []transport.GuestDocument{{GuestID: "guest", Status: &status}}}},
		hostPages:  []transport.HostMessageDocumentPage{{}},
	}
	service := blastService(t, manifest, &blastCredentials{}, callable, firestore)

	got, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content"})
	if err != nil || !got.(domain.BlastResult).Submitted || callable.createCalls != 1 {
		t.Fatalf("result = %#v, error = %v, create calls = %d", got, err, callable.createCalls)
	}
}

func TestSendBlastDerivesOrdinaryAudienceAfterBoundedReadsAndDispatchesOnce(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	invited, going, maybe, declined, waitlist := "SENT", "GOING", "MAYBE", "DECLINED", "WAITLIST"
	checkIn := "present"
	guestCursor := transport.RemoteCursor("guest-next")
	hostCursor := transport.RemoteCursor("host-next")
	textBlast := "TEXT_BLAST"
	other := "OTHER"
	credentials := &blastCredentials{}
	callable := &blastCallable{event: transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event", Start: &future}}}}
	firestore := &blastFirestore{
		event: transport.EventDocument{EventID: "event", Fields: map[string]transport.FieldValue{}},
		guestPages: []transport.GuestDocumentPage{
			{Documents: []transport.GuestDocument{{GuestID: "invited", Status: &invited}, {GuestID: "checked-in", Status: &going, CheckIn: &checkIn}, {GuestID: "going", Status: &going}}, Cursor: &guestCursor},
			{Documents: []transport.GuestDocument{{GuestID: "maybe", Status: &maybe}, {GuestID: "declined", Status: &declined}, {GuestID: "waitlist", Status: &waitlist}}},
		},
		hostPages: []transport.HostMessageDocumentPage{
			{Documents: []transport.HostMessageDocument{{}, {Kind: &textBlast}, {Kind: &other}}, Cursor: &hostCursor},
			{Documents: []transport.HostMessageDocument{}},
		},
	}
	service := blastService(t, closedEventGates(t), credentials, callable, firestore)

	got, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content", ShowOnEventPage: true})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(domain.BlastResult)
	if !result.Submitted || result.RecipientStatus != "not-reported" {
		t.Fatalf("result = %#v", result)
	}
	wantGroups := []transport.TextBlastGroup{
		{Name: "invited", GuestIDs: []transport.GuestID{"invited"}},
		{Name: "checkedIn", GuestIDs: []transport.GuestID{"checked-in"}},
		{Name: "GOING", GuestIDs: []transport.GuestID{"checked-in", "going"}},
		{Name: "MAYBE", GuestIDs: []transport.GuestID{"maybe"}},
		{Name: "DECLINED", GuestIDs: []transport.GuestID{"declined"}},
		{Name: "WAITLIST", GuestIDs: []transport.GuestID{"waitlist"}},
	}
	if callable.request == nil || callable.request.EventID != "event" || callable.request.Message != "content" || !callable.request.ShowOnEventPage || !reflect.DeepEqual(callable.request.Groups, wantGroups) {
		t.Fatalf("request = %#v", callable.request)
	}
	if credentials.calls != 1 || callable.getCalls != 1 || callable.createCalls != 1 || firestore.getEventCalls != 1 || firestore.guestListCalls != 2 || firestore.hostListCalls != 2 {
		t.Fatalf("calls = credentials:%d get:%d create:%d event-doc:%d guests:%d hosts:%d", credentials.calls, callable.getCalls, callable.createCalls, firestore.getEventCalls, firestore.guestListCalls, firestore.hostListCalls)
	}
}

func TestSendBlastIncludesAllInvitedRecipientsWithoutInventedCap(t *testing.T) {
	future := time.Now().Add(time.Hour)
	status := "SENT"
	guests := make([]transport.GuestDocument, 101)
	for index := range guests {
		guests[index] = transport.GuestDocument{GuestID: transport.GuestID(fmt.Sprintf("guest-%d", index)), Status: &status}
	}
	callable := &blastCallable{event: transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event", Start: &future}}}}
	firestore := &blastFirestore{
		event:      transport.EventDocument{EventID: "event", Fields: map[string]transport.FieldValue{}},
		guestPages: []transport.GuestDocumentPage{{Documents: guests}},
		hostPages:  []transport.HostMessageDocumentPage{{}},
	}
	service := blastService(t, closedEventGates(t), &blastCredentials{}, callable, firestore)

	_, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content"})
	if err != nil {
		t.Fatal(err)
	}
	if callable.request == nil || len(callable.request.Groups) != 1 || callable.request.Groups[0].Name != "invited" || !reflect.DeepEqual(callable.request.Groups[0].GuestIDs, guestIDs(guests)) {
		t.Fatalf("request = %#v", callable.request)
	}
}

func guestIDs(guests []transport.GuestDocument) []transport.GuestID {
	ids := make([]transport.GuestID, len(guests))
	for index := range guests {
		ids[index] = guests[index].GuestID
	}
	return ids
}

func TestSendBlastRejectsInvalidInputBeforeAuthentication(t *testing.T) {
	credentials := &blastCredentials{}
	service := blastService(t, closedEventGates(t), credentials, &blastCallable{}, &blastFirestore{})
	for _, input := range []domain.SendBlastInput{
		{Audience: "all-guests", Message: "content"},
		{EventID: "event", Audience: "selected-guests", Message: "content"},
		{EventID: "event", Audience: "all-guests"},
		{EventID: "event", Audience: "all-guests", Message: string(make([]rune, 481))},
	} {
		_, err := service.Invoke(context.Background(), domain.OperationSendBlast, input)
		var public *domain.Error
		if !errors.As(err, &public) || public.Type != domain.ErrorInputInvalid {
			t.Fatalf("input %#v error = %#v", input, err)
		}
	}
	if credentials.calls != 0 {
		t.Fatalf("invalid inputs authenticated %d times", credentials.calls)
	}
}

func TestSendBlastRejectsExpiredEventWithoutMutation(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	status := "GOING"
	callable := &blastCallable{event: transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event", Start: &past}}}}
	firestore := &blastFirestore{
		event:      transport.EventDocument{EventID: "event", Fields: map[string]transport.FieldValue{}},
		guestPages: []transport.GuestDocumentPage{{Documents: []transport.GuestDocument{{GuestID: "guest", Status: &status}}}},
		hostPages:  []transport.HostMessageDocumentPage{{}},
	}
	service := blastService(t, closedEventGates(t), &blastCredentials{}, callable, firestore)

	_, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content"})
	var public *domain.Error
	if !errors.As(err, &public) || public.Type != domain.ErrorStateConflict || public.Code != "EVENT_EXPIRED" {
		t.Fatalf("error = %#v", err)
	}
	if callable.createCalls != 0 {
		t.Fatalf("expired event dispatched %d mutations", callable.createCalls)
	}
}

func TestSendBlastDoesNotRetryAmbiguousMutation(t *testing.T) {
	future := time.Now().Add(time.Hour)
	status := "GOING"
	callable := &blastCallable{
		event:     transport.GetEventResult{Event: transport.EventDetail{EventSummary: transport.EventSummary{EventID: "event", Start: &future}}},
		createErr: &transport.ProtocolFailure{Operation: "createTextBlast", Class: "remote.unavailable", DispatchState: transport.DispatchStarted},
	}
	firestore := &blastFirestore{
		event:      transport.EventDocument{EventID: "event", Fields: map[string]transport.FieldValue{}},
		guestPages: []transport.GuestDocumentPage{{Documents: []transport.GuestDocument{{GuestID: "guest", Status: &status}}}},
		hostPages:  []transport.HostMessageDocumentPage{{}},
	}
	service := blastService(t, closedEventGates(t), &blastCredentials{}, callable, firestore)

	_, err := service.Invoke(context.Background(), domain.OperationSendBlast, domain.SendBlastInput{EventID: "event", Audience: "all-guests", Message: "content"})
	if err == nil || callable.createCalls != 1 {
		t.Fatalf("error = %v, create calls = %d", err, callable.createCalls)
	}
}
