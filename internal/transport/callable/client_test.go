package callable

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/transport"
)

func TestClientImplementsCallableTransport(t *testing.T) {
	var _ transport.CallableTransport = (*Client)(nil)
}

func TestOpenEventListGateDoesNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	_, err := client.ListUpcomingEvents(context.Background(), transport.ListHomeEventsRequest{Credential: "secret"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %T, want *transport.ProtocolFailure", err)
	}
	if failure.Operation != "getMyUpcomingEventsForHomePage" || failure.Class != "evidence.required" || failure.DispatchState != transport.DispatchNotStarted {
		t.Fatalf("failure = %#v", failure)
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}

func TestGetContactsBuildsExactRequestAndReturnsTypedPrivatePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getContacts" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var got any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		wantJSON := `{"data":{"params":{},"amplitudeDeviceId":"device","userId":"user","paging":{"maxResults":1000,"cursor":"cursor"}}}`
		var want any
		_ = json.Unmarshal([]byte(wantJSON), &want)
		if !deepEqualJSON(got, want) {
			t.Fatalf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "safe-correlation")
		_, _ = io.WriteString(w, `{"result":{"data":[{"id":"private-contact","name":"Example","sharedEventCount":2}],"paging":{"nextCursor":"next"}}}`)
	}))
	defer server.Close()

	cursor := transport.RemoteCursor("cursor")
	client := New(Config{BaseURL: server.URL, AmplitudeDeviceID: "device", UserID: "user"})
	result, err := client.GetContacts(context.Background(), transport.GetContactsRequest{Credential: "secret", MaxResults: 1000, Cursor: &cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contacts) != 1 || result.Contacts[0].ContactID != "private-contact" || result.Contacts[0].DisplayName != "Example" || result.Cursor == nil || *result.Cursor != "next" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateEventUsesInjectedAcceptedDefaultsAndCompletePoster(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Data struct {
				Params struct {
					Event     map[string]any `json:"event"`
					CohostIDs []string       `json:"cohostIds"`
				} `json:"params"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		event := body.Data.Params.Event
		if event["status"] != "UNSAVED" || event["title"] != "Party" || event["rsvpButtonGlyphType"] != "emojis" {
			t.Fatalf("event = %#v", event)
		}
		display := event["displaySettings"].(map[string]any)
		if display["theme"] != "theme" || display["effect"] != "effect" || display["titleFont"] != nil {
			t.Fatalf("displaySettings = %#v", display)
		}
		image := event["image"].(map[string]any)
		poster := image["poster"].(map[string]any)
		if image["source"] != "partiful_posters" || poster["id"] != "poster" || poster["url"] != "https://example.invalid/poster.png" {
			t.Fatalf("image = %#v", image)
		}
		_, _ = io.WriteString(w, `{"result":"private-event-id"}`)
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:       server.URL,
		EventDefaults: &EventDefaults{Theme: "theme", Effect: "effect"},
		Posters:       []transport.Poster{{PosterID: "poster", Name: "Poster", URL: "https://example.invalid/poster.png", ContentType: "image/png", Width: 100, Height: 200, Tags: []string{}, Categories: []string{}}},
	})
	posterID := transport.PosterID("poster")
	result, err := client.CreateEvent(context.Background(), transport.CreateEventRequest{
		Credential: "secret", Title: "Party", Start: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), Timezone: "UTC", PosterID: &posterID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DispatchState != transport.DispatchStarted {
		t.Fatalf("result = %#v", result)
	}
}

func TestUnknownClosedEnumFailsAfterOneDispatchWithoutLeakingBody(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"result":{"data":{"currentGuest":{"id":"private","status":"FUTURE_STATUS","secret":"do-not-leak"}}}}`)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	_, err := client.GetCurrentGuest(context.Background(), transport.GetCurrentGuestRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "contract.protocol_changed" || failure.DispatchState != transport.DispatchStarted {
		t.Fatalf("error = %#v", err)
	}
	if err.Error() != "remote protocol failure" {
		t.Fatalf("public error = %q", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("dispatches = %d, want 1", calls.Load())
	}
}

func TestCurrentGuestAllowsAcceptedBusinessFieldsWhileKeepingEnvelopeStrict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"data":{"currentGuest":{"id":"guest","status":"GOING","name":"Name","count":1,"plusOnes":[],"userId":"user","anchorGuestId":"anchor"}}}}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	result, err := client.GetCurrentGuest(context.Background(), transport.GetCurrentGuestRequest{Credential: "secret", EventID: "event"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Guest == nil || result.Guest.GuestID != "guest" || result.Guest.Status != "GOING" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMutationDoesNotRetryAmbiguousFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "private upstream body", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	_, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Operation != "cancelEvent" || failure.DispatchState != transport.DispatchStarted {
		t.Fatalf("error = %#v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("dispatches = %d, want 1", calls.Load())
	}
}

func TestMutationDoesNotFollowRedirect(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/cancelEvent" {
			http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
			return
		}
		_, _ = io.WriteString(w, `{"result":{}}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	_, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.DispatchState != transport.DispatchStarted {
		t.Fatalf("error = %#v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("dispatches = %d, want 1", calls.Load())
	}
}

func TestCancelAcceptsGenericCallableCompletionEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{}}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	result, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{Credential: "secret", EventID: "event"})
	if err != nil {
		t.Fatal(err)
	}
	if result.DispatchState != transport.DispatchStarted {
		t.Fatalf("result = %#v", result)
	}
}

func TestAuthenticatedOperationWithoutCredentialDoesNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	_, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "auth.required" || failure.DispatchState != transport.DispatchNotStarted {
		t.Fatalf("error = %#v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}

func TestGetGuestsRejectsUnknownEnvelopeMember(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"data":[{"id":"guest","name":"Example","status":"GOING","count":1}],"paging":{"nextCursor":null}},"future":"unsafe"}`)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, AmplitudeDeviceID: "device"})
	_, err := client.GetGuests(context.Background(), transport.GetGuestsRequest{Credential: "secret", EventID: "event", IncludeInvitedGuests: true, MaxResults: 500})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "contract.protocol_changed" {
		t.Fatalf("error = %#v", err)
	}
}

func TestResponseLimitFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"data":{}}}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, MaxResponseBytes: 8})
	_, err := client.GetCurrentGuest(context.Background(), transport.GetCurrentGuestRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "contract.protocol_changed" {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientAddsBoundedDeadline(t *testing.T) {
	deadlineSeen := make(chan bool, 1)
	client := New(Config{HTTPClient: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		_, ok := r.Context().Deadline()
		deadlineSeen <- ok
		return nil, context.DeadlineExceeded
	}), BaseURL: "https://example.invalid", Timeout: time.Second})
	_, _ = client.GetCurrentGuest(context.Background(), transport.GetCurrentGuestRequest{Credential: "secret", EventID: "event"})
	if !<-deadlineSeen {
		t.Fatal("request context had no deadline")
	}
}

func TestCohostLinkRejectsUnknownEnvelopeMember(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"data":{"path":"/e/event/cohost/link"}},"unexpected":true}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	_, err := client.CreateCohostLink(context.Background(), transport.CohostLinkRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "contract.protocol_changed" {
		t.Fatalf("error = %#v", err)
	}
}

func TestMalformedRequestsDoNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	posterID := transport.PosterID("poster")
	client := New(Config{
		BaseURL: server.URL, AmplitudeDeviceID: "device",
		EventDefaults: &EventDefaults{Theme: "theme", Effect: "effect"},
		Posters:       []transport.Poster{{PosterID: posterID, Name: "Poster", URL: "https://example.invalid/poster.png", ContentType: "image/png", Width: 1, Height: 1, Tags: []string{}, Categories: []string{}}},
	})
	credential := transport.Credential("secret")
	cases := []func() error{
		func() error {
			_, err := client.CreateEvent(context.Background(), transport.CreateEventRequest{Credential: credential, Start: time.Now(), Timezone: "UTC", PosterID: &posterID})
			return err
		},
		func() error {
			_, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{Credential: credential})
			return err
		},
		func() error {
			_, err := client.GetGuests(context.Background(), transport.GetGuestsRequest{Credential: credential, IncludeInvitedGuests: true, MaxResults: 500})
			return err
		},
		func() error {
			_, err := client.InviteGuest(context.Background(), transport.InviteGuestRequest{Credential: credential, EventID: "event"})
			return err
		},
		func() error {
			_, err := client.GetCurrentGuest(context.Background(), transport.GetCurrentGuestRequest{Credential: credential})
			return err
		},
		func() error {
			_, err := client.SetGuest(context.Background(), transport.SetGuestRequest{Credential: credential, EventID: "event", Status: "GOING", DisplayName: "Name", PartySize: 2, Timezone: "UTC"})
			return err
		},
		func() error {
			_, err := client.MarkInterest(context.Background(), transport.MarkInterestRequest{Credential: credential})
			return err
		},
		func() error {
			_, err := client.InviteCohost(context.Background(), transport.CohostRequest{Credential: credential, EventID: "event"})
			return err
		},
		func() error {
			_, err := client.CreateCohostLink(context.Background(), transport.CohostLinkRequest{Credential: credential})
			return err
		},
		func() error {
			_, err := client.CreateTextBlast(context.Background(), transport.CreateTextBlastRequest{Credential: credential, EventID: "event", Message: "hello"})
			return err
		},
	}
	for index, call := range cases {
		var failure *transport.ProtocolFailure
		if err := call(); !errors.As(err, &failure) || failure.DispatchState != transport.DispatchNotStarted {
			t.Fatalf("case %d error = %#v", index, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestInvalidBaseURLFailsClosedWithoutDispatch(t *testing.T) {
	var calls atomic.Int32
	client := New(Config{BaseURL: "://bad", HTTPClient: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected dispatch")
	})})
	_, err := client.CancelEvent(context.Background(), transport.CancelEventRequest{Credential: "secret", EventID: "event"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.DispatchState != transport.DispatchNotStarted {
		t.Fatalf("error = %#v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}

func deepEqualJSON(a, b any) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
