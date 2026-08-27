package firestore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/KalebCole/partiful/internal/transport"
)

func TestClientImplementsFirestoreTransport(t *testing.T) {
	var _ transport.FirestoreTransport = (*Client)(nil)
}

func TestOpenReadGatesDoNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})

	checks := []struct {
		operation string
		call      func() error
	}{
		{"firestoreGetEvent", func() error {
			_, err := client.GetEvent(context.Background(), transport.GetEventDocumentRequest{EventID: "e"})
			return err
		}},
		{"firestoreListEventGuests", func() error {
			_, err := client.ListEventGuests(context.Background(), transport.ListEventDocumentsRequest{EventID: "e"})
			return err
		}},
		{"firestoreListEventHostMessages", func() error {
			_, err := client.ListEventHostMessages(context.Background(), transport.ListEventDocumentsRequest{EventID: "e"})
			return err
		}},
	}
	for _, check := range checks {
		err := check.call()
		var failure *transport.ProtocolFailure
		if !errors.As(err, &failure) || failure.Operation != check.operation || failure.Class != "evidence.required" || failure.DispatchState != transport.DispatchNotStarted {
			t.Fatalf("%s error = %#v", check.operation, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}

func TestPatchEventBuildsMaskedRequestAndDecodesStrictValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/projects/getpartiful/databases/(default)/documents/events/event" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query()["updateMask.fieldPaths"]; len(got) != 2 || got[0] != "active" || got[1] != "title" {
			t.Fatalf("mask = %#v", got)
		}
		if r.URL.Query().Get("currentDocument.exists") != "true" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing bearer credential")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		fields := body["fields"].(map[string]any)
		if fields["title"].(map[string]any)["stringValue"] != "Party" || fields["active"].(map[string]any)["booleanValue"] != true {
			t.Fatalf("body = %#v", body)
		}
		_, _ = io.WriteString(w, `{"name":"projects/getpartiful/databases/(default)/documents/events/event","fields":{"title":{"stringValue":"Party"},"active":{"booleanValue":true}}}`)
	}))
	defer server.Close()

	title, active := "Party", true
	client := New(Config{BaseURL: server.URL})
	result, err := client.PatchEvent(context.Background(), transport.PatchEventDocumentRequest{
		Credential: "secret", EventID: "event", MustExist: true,
		FieldMask: []string{"active", "title"},
		Fields:    map[string]transport.FieldValue{"title": {String: &title}, "active": {Boolean: &active}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EventID != "event" || result.Fields["title"].String == nil || *result.Fields["title"].String != "Party" {
		t.Fatalf("result = %#v", result)
	}
}

func TestUnknownFirestoreValueFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"name":"projects/getpartiful/databases/(default)/documents/events/e/guests/g","fields":{"status":{"futureValue":"secret"}}}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	_, err := client.GetGuest(context.Background(), transport.GetGuestDocumentRequest{Credential: "secret", EventID: "e", GuestID: "g"})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "contract.protocol_changed" || failure.DispatchState != transport.DispatchStarted {
		t.Fatalf("error = %#v", err)
	}
}

func TestPatchWithoutCredentialDoesNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	title := "Party"
	client := New(Config{BaseURL: server.URL})
	_, err := client.PatchEvent(context.Background(), transport.PatchEventDocumentRequest{
		EventID: "event", Fields: map[string]transport.FieldValue{"title": {String: &title}}, FieldMask: []string{"title"},
	})
	var failure *transport.ProtocolFailure
	if !errors.As(err, &failure) || failure.Class != "auth.required" || failure.DispatchState != transport.DispatchNotStarted {
		t.Fatalf("error = %#v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("dispatches = %d, want 0", calls.Load())
	}
}
