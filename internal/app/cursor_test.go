package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/KalebCole/partiful/internal/domain"
)

type cursorItem struct {
	Name string
	Rank int
}

func TestCursorRoundTripBindsScopeAndProjectedSequence(t *testing.T) {
	t.Parallel()

	codec, err := NewCursorCodec([]byte("installation-secret-one"))
	if err != nil {
		t.Fatal(err)
	}
	account := "private-account"
	scope := CursorScope{Operation: domain.OperationListContacts, CanonicalFilter: "private query", AccountIdentity: &account}
	items := []cursorItem{{Name: "First", Rank: 1}, {Name: "Second", Rank: 2}}

	cursor, err := codec.Encode(scope, 1, items)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(cursor)
	for _, secret := range []string{"private-account", "private query", "First", "Second"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("cursor contains private value %q", secret)
		}
	}
	offset, err := codec.Decode(cursor, scope, items)
	if err != nil || offset != 1 {
		t.Fatalf("Decode() = %d, %v; want 1, nil", offset, err)
	}
}

func TestCursorRejectsTamperingAndBindingMismatchWithSafeErrors(t *testing.T) {
	t.Parallel()

	codec, _ := NewCursorCodec([]byte("installation-secret-one"))
	otherCodec, _ := NewCursorCodec([]byte("installation-secret-two"))
	account := "account-one"
	scope := CursorScope{Operation: domain.OperationListContacts, CanonicalFilter: "query", AccountIdentity: &account}
	items := []cursorItem{{Name: "First", Rank: 1}, {Name: "Second", Rank: 2}}
	cursor, _ := codec.Encode(scope, 1, items)
	replacement := "A"
	if strings.HasSuffix(string(cursor), replacement) {
		replacement = "B"
	}
	modified := domain.Cursor(string(cursor)[:len(cursor)-1] + replacement)
	wrongAccount := "account-two"

	cases := []struct {
		name  string
		codec *CursorCodec
		value domain.Cursor
		scope CursorScope
		items []cursorItem
		kind  domain.ErrorType
		code  string
	}{
		{name: "tampered", codec: codec, value: modified, scope: scope, items: items, kind: domain.ErrorInputInvalid, code: "INVALID_CURSOR"},
		{name: "wrong operation", codec: codec, value: cursor, scope: CursorScope{Operation: domain.OperationListPosters}, items: items, kind: domain.ErrorInputInvalid, code: "INVALID_CURSOR"},
		{name: "wrong filter", codec: codec, value: cursor, scope: CursorScope{Operation: scope.Operation, CanonicalFilter: "other", AccountIdentity: &account}, items: items, kind: domain.ErrorInputInvalid, code: "INVALID_CURSOR"},
		{name: "wrong account", codec: codec, value: cursor, scope: CursorScope{Operation: scope.Operation, CanonicalFilter: scope.CanonicalFilter, AccountIdentity: &wrongAccount}, items: items, kind: domain.ErrorInputInvalid, code: "INVALID_CURSOR"},
		{name: "wrong installation", codec: otherCodec, value: cursor, scope: scope, items: items, kind: domain.ErrorInputInvalid, code: "INVALID_CURSOR"},
		{name: "stale sequence", codec: codec, value: cursor, scope: scope, items: []cursorItem{{Name: "Changed", Rank: 1}}, kind: domain.ErrorStateConflict, code: "CURSOR_STALE"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.codec.Decode(test.value, test.scope, test.items)
			var applicationError *domain.Error
			if !errors.As(err, &applicationError) || applicationError.Type != test.kind || applicationError.Code != test.code {
				t.Fatalf("Decode() error = %#v, want %s/%s", err, test.kind, test.code)
			}
			if !reflect.DeepEqual(applicationError.Details, domain.ErrorDetails{}) || strings.Contains(applicationError.Error(), "account") || strings.Contains(applicationError.Error(), "query") {
				t.Fatalf("Decode() leaked mismatch details: %#v", applicationError)
			}
		})
	}
}

func TestCursorRejectsImpossibleOffsetAndInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := NewCursorCodec(nil); err == nil {
		t.Fatal("NewCursorCodec(nil) succeeded")
	}
	codec, _ := NewCursorCodec([]byte("installation-secret"))
	scope := CursorScope{Operation: domain.OperationListPosters}
	items := []cursorItem{{Name: "Only", Rank: 1}}
	cursor, _ := codec.Encode(scope, 2, items)
	_, err := codec.Decode(cursor, scope, items)
	var applicationError *domain.Error
	if !errors.As(err, &applicationError) || applicationError.Code != "INVALID_CURSOR" {
		t.Fatalf("Decode(impossible offset) error = %#v", err)
	}
}

func TestCursorRejectsAuthenticatedUnsupportedVersion(t *testing.T) {
	t.Parallel()
	codec, _ := NewCursorCodec([]byte("installation-secret"))
	scope := CursorScope{Operation: domain.OperationListPosters}
	items := []cursorItem{{Name: "Only", Rank: 1}}
	sequence, _ := canonicalSequence(items)
	payload, _ := json.Marshal(cursorPayload{
		Version: 2, Operation: scope.Operation,
		Filter: keyedDigest(codec.filterKey, nil), Sequence: keyedDigest(codec.sequenceKey, sequence),
	})
	cursor := domain.Cursor(base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(keyedMAC(codec.signingKey, payload)))
	_, err := codec.Decode(cursor, scope, items)
	var applicationError *domain.Error
	if !errors.As(err, &applicationError) || applicationError.Code != "INVALID_CURSOR" {
		t.Fatalf("Decode(unsupported version) error = %#v", err)
	}
}
