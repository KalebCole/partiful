package callable_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KalebCole/partiful/internal/transport"
	"github.com/KalebCole/partiful/internal/transport/callable"
	"github.com/KalebCole/partiful/internal/transport/firestore"
	"github.com/KalebCole/partiful/internal/transport/poster"
)

type gateCase struct {
	ID     string
	Verify func(*testing.T)
}

func TestApplicableEvidenceGateManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private endpoint body", http.StatusInternalServerError)
	}))
	defer server.Close()

	callableClient := callable.New(callable.Config{
		BaseURL: server.URL, AmplitudeDeviceID: "device", UserID: "user",
		EventDefaults: &callable.EventDefaults{Theme: "theme", Effect: "effect"},
		Posters:       []transport.Poster{{PosterID: "poster", Name: "Poster", URL: "https://example.invalid/poster.png", ContentType: "image/png", Width: 1, Height: 1, Tags: []string{}, Categories: []string{}}},
	})
	firestoreClient := firestore.New(firestore.Config{BaseURL: server.URL})
	posterClient := poster.New(poster.Config{BaseURL: server.URL})
	ctx := context.Background()
	credential := transport.Credential("secret")
	posterID := transport.PosterID("poster")
	stringValue := "value"

	endpoint := func(operation string, call func() error) func(*testing.T) {
		return func(t *testing.T) {
			err := call()
			var failure *transport.ProtocolFailure
			if !errors.As(err, &failure) {
				t.Fatalf("%s error = %T, want *transport.ProtocolFailure", operation, err)
			}
			if failure.Operation != operation || err.Error() != "remote protocol failure" {
				t.Fatalf("%s failure = %#v, public=%q", operation, failure, err)
			}
		}
	}
	noDispatch := func(calls ...func() error) func(*testing.T) {
		return func(t *testing.T) {
			for _, call := range calls {
				err := call()
				var failure *transport.ProtocolFailure
				if !errors.As(err, &failure) || failure.DispatchState != transport.DispatchNotStarted || failure.Class != "evidence.required" {
					t.Fatalf("failure = %#v", err)
				}
			}
		}
	}

	manifest := []gateCase{
		{"OP11-ENDPOINT-ERRORS:getMyUpcomingEventsForHomePage", endpoint("getMyUpcomingEventsForHomePage", func() error {
			_, err := callableClient.ListUpcomingEvents(ctx, transport.ListHomeEventsRequest{Credential: credential})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getMyPastEventsForHomePage", endpoint("getMyPastEventsForHomePage", func() error {
			_, err := callableClient.ListPastEvents(ctx, transport.ListHomeEventsRequest{Credential: credential})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getEventInfo", endpoint("getEventInfo", func() error {
			_, err := callableClient.GetEvent(ctx, transport.GetEventRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:createEvent", endpoint("createEvent", func() error {
			_, err := callableClient.CreateEvent(ctx, transport.CreateEventRequest{Credential: credential, Title: "Party", Start: time.Now(), Timezone: "UTC", PosterID: &posterID})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:cancelEvent", endpoint("cancelEvent", func() error {
			_, err := callableClient.CancelEvent(ctx, transport.CancelEventRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getContacts", endpoint("getContacts", func() error {
			_, err := callableClient.GetContacts(ctx, transport.GetContactsRequest{Credential: credential, MaxResults: 1000})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getGuests", endpoint("getGuests", func() error {
			_, err := callableClient.GetGuests(ctx, transport.GetGuestsRequest{Credential: credential, EventID: "event", IncludeInvitedGuests: true, MaxResults: 500})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:addInvitedGuestsAsHost", endpoint("addInvitedGuestsAsHost", func() error {
			_, err := callableClient.InviteGuest(ctx, transport.InviteGuestRequest{Credential: credential, EventID: "event", ContactID: "contact"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getCurrentGuest", endpoint("getCurrentGuest", func() error {
			_, err := callableClient.GetCurrentGuest(ctx, transport.GetCurrentGuestRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:addGuest", endpoint("addGuest", func() error {
			_, err := callableClient.SetGuest(ctx, transport.SetGuestRequest{Credential: credential, EventID: "event", Status: "GOING", DisplayName: "Example", PartySize: 1, Timezone: "UTC"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:markEventInterest", endpoint("markEventInterest", func() error {
			_, err := callableClient.MarkInterest(ctx, transport.MarkInterestRequest{Credential: credential, EventID: "event", Interested: true})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:createCohostRequest", endpoint("createCohostRequest", func() error {
			_, err := callableClient.InviteCohost(ctx, transport.CohostRequest{Credential: credential, EventID: "event", ContactID: "contact"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:deleteCohostRequest", endpoint("deleteCohostRequest", func() error {
			_, err := callableClient.RevokeCohostInvite(ctx, transport.CohostRequest{Credential: credential, EventID: "event", ContactID: "contact"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:removeCohost", endpoint("removeCohost", func() error {
			_, err := callableClient.RemoveCohost(ctx, transport.CohostRequest{Credential: credential, EventID: "event", ContactID: "contact"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:generateEventCohostLink", endpoint("generateEventCohostLink", func() error {
			_, err := callableClient.CreateCohostLink(ctx, transport.CohostLinkRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:revokeEventCohostLink", endpoint("revokeEventCohostLink", func() error {
			_, err := callableClient.RevokeCohostLink(ctx, transport.CohostLinkRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:createTextBlast", endpoint("createTextBlast", func() error {
			_, err := callableClient.CreateTextBlast(ctx, transport.CreateTextBlastRequest{Credential: credential, EventID: "event", Message: "hello", Groups: []transport.TextBlastGroup{{Name: "GOING"}}})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:firestoreGetEvent", endpoint("firestoreGetEvent", func() error {
			_, err := firestoreClient.GetEvent(ctx, transport.GetEventDocumentRequest{Credential: credential, EventID: "event"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:firestorePatchEvent", endpoint("firestorePatchEvent", func() error {
			_, err := firestoreClient.PatchEvent(ctx, transport.PatchEventDocumentRequest{Credential: credential, EventID: "event", Fields: map[string]transport.FieldValue{"title": {String: &stringValue}}, FieldMask: []string{"title"}})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:firestoreGetGuest", endpoint("firestoreGetGuest", func() error {
			_, err := firestoreClient.GetGuest(ctx, transport.GetGuestDocumentRequest{Credential: credential, EventID: "event", GuestID: "guest"})
			return err
		})},
		{"OP11-ENDPOINT-ERRORS:getPosterCatalog", endpoint("getPosterCatalog", func() error { _, err := posterClient.GetCatalog(ctx, transport.GetPosterCatalogRequest{}); return err })},
		{"OP11-EVENT-LIST-REQUEST", noDispatch(
			func() error {
				_, err := callableClient.ListUpcomingEvents(ctx, transport.ListHomeEventsRequest{})
				return err
			},
			func() error {
				_, err := callableClient.ListPastEvents(ctx, transport.ListHomeEventsRequest{})
				return err
			},
		)},
		{"OP11-GET-EVENT-REQUEST", noDispatch(
			func() error {
				_, err := callableClient.GetEvent(ctx, transport.GetEventRequest{EventID: "event"})
				return err
			},
			func() error {
				_, err := firestoreClient.GetEvent(ctx, transport.GetEventDocumentRequest{EventID: "event"})
				return err
			},
		)},
		{"OP11-BLAST-FIRESTORE-READS", noDispatch(
			func() error {
				_, err := firestoreClient.ListEventGuests(ctx, transport.ListEventDocumentsRequest{EventID: "event"})
				return err
			},
			func() error {
				_, err := firestoreClient.ListEventHostMessages(ctx, transport.ListEventDocumentsRequest{EventID: "event"})
				return err
			},
		)},
		{"OP11-UPLOAD-PHOTO", func(t *testing.T) {
			for _, typ := range []reflect.Type{reflect.TypeOf(callableClient), reflect.TypeOf(firestoreClient), reflect.TypeOf(posterClient)} {
				for index := 0; index < typ.NumMethod(); index++ {
					if strings.Contains(strings.ToLower(typ.Method(index).Name), "upload") {
						t.Fatalf("unexpected upload method %s", typ.Method(index).Name)
					}
				}
			}
		}},
	}

	ids := make([]string, len(manifest))
	for index, gate := range manifest {
		ids[index] = gate.ID
		t.Run(gate.ID, gate.Verify)
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for index := 1; index < len(sorted); index++ {
		if sorted[index] == sorted[index-1] {
			t.Fatalf("duplicate gate identity %s", sorted[index])
		}
	}
	if len(manifest) != 25 {
		t.Fatalf("gate entries = %d, want 25", len(manifest))
	}
}
