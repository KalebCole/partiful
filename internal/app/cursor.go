package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/KalebCole/partiful/internal/domain"
)

const cursorVersion = 1

type CursorScope struct {
	Operation       domain.OperationID
	CanonicalFilter string
	AccountIdentity *string
}

type CursorCodec struct {
	signingKey  []byte
	filterKey   []byte
	accountKey  []byte
	sequenceKey []byte
}

type cursorPayload struct {
	Version    int                `json:"v"`
	Operation  domain.OperationID `json:"o"`
	Filter     string             `json:"f"`
	Account    string             `json:"a,omitempty"`
	NextOffset int                `json:"n"`
	Sequence   string             `json:"s"`
}

func NewCursorCodec(installationSecret []byte) (*CursorCodec, error) {
	if len(installationSecret) == 0 {
		return nil, fmt.Errorf("cursor codec: empty installation secret")
	}
	return &CursorCodec{
		signingKey:  deriveCursorKey(installationSecret, "cursor-signing-v1"),
		filterKey:   deriveCursorKey(installationSecret, "cursor-filter-v1"),
		accountKey:  deriveCursorKey(installationSecret, "cursor-account-v1"),
		sequenceKey: deriveCursorKey(installationSecret, "cursor-sequence-v1"),
	}, nil
}

func (codec *CursorCodec) Encode(scope CursorScope, nextOffset int, projectedSequence any) (domain.Cursor, error) {
	if codec == nil || scope.Operation == "" || nextOffset < 0 {
		return "", invalidCursor()
	}
	sequence, err := canonicalSequence(projectedSequence)
	if err != nil {
		return "", &domain.Error{Type: domain.ErrorInternalFailure, Code: "CURSOR_SEQUENCE_INVALID", Message: "could not encode collection cursor"}
	}
	payload := cursorPayload{
		Version:    cursorVersion,
		Operation:  scope.Operation,
		Filter:     keyedDigest(codec.filterKey, []byte(scope.CanonicalFilter)),
		NextOffset: nextOffset,
		Sequence:   keyedDigest(codec.sequenceKey, sequence),
	}
	if scope.AccountIdentity != nil {
		payload.Account = keyedDigest(codec.accountKey, []byte(*scope.AccountIdentity))
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return "", &domain.Error{Type: domain.ErrorInternalFailure, Code: "CURSOR_ENCODE_FAILED", Message: "could not encode collection cursor"}
	}
	mac := keyedMAC(codec.signingKey, encodedPayload)
	return domain.Cursor(base64.RawURLEncoding.EncodeToString(encodedPayload) + "." + base64.RawURLEncoding.EncodeToString(mac)), nil
}

func (codec *CursorCodec) Decode(cursor domain.Cursor, scope CursorScope, projectedSequence any) (int, error) {
	if codec == nil || scope.Operation == "" {
		return 0, invalidCursor()
	}
	parts := strings.Split(string(cursor), ".")
	if len(parts) != 2 {
		return 0, invalidCursor()
	}
	encodedPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, invalidCursor()
	}
	providedMAC, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(providedMAC, keyedMAC(codec.signingKey, encodedPayload)) {
		return 0, invalidCursor()
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(encodedPayload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, invalidCursor()
	}
	wantAccount := ""
	if scope.AccountIdentity != nil {
		wantAccount = keyedDigest(codec.accountKey, []byte(*scope.AccountIdentity))
	}
	if payload.Version != cursorVersion || payload.Operation != scope.Operation || payload.Filter != keyedDigest(codec.filterKey, []byte(scope.CanonicalFilter)) || payload.Account != wantAccount || payload.NextOffset < 0 {
		return 0, invalidCursor()
	}
	sequence, err := canonicalSequence(projectedSequence)
	if err != nil {
		return 0, &domain.Error{Type: domain.ErrorInternalFailure, Code: "CURSOR_SEQUENCE_INVALID", Message: "could not validate collection cursor"}
	}
	if payload.Sequence != keyedDigest(codec.sequenceKey, sequence) {
		return 0, staleCursor()
	}
	var lengthProbe []json.RawMessage
	if json.Unmarshal(sequence, &lengthProbe) != nil || payload.NextOffset > len(lengthProbe) {
		return 0, invalidCursor()
	}
	return payload.NextOffset, nil
}

func canonicalSequence(sequence any) ([]byte, error) {
	encoded, err := json.Marshal(sequence)
	if err != nil {
		return nil, err
	}
	var probe []json.RawMessage
	if json.Unmarshal(encoded, &probe) != nil {
		return nil, fmt.Errorf("projected sequence is not an array")
	}
	return encoded, nil
}

func deriveCursorKey(secret []byte, purpose string) []byte {
	return keyedMAC(secret, []byte("partiful-app/"+purpose))
}

func keyedDigest(key, value []byte) string {
	return base64.RawURLEncoding.EncodeToString(keyedMAC(key, value))
}

func keyedMAC(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func invalidCursor() error {
	return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_CURSOR", Message: "collection cursor is invalid"}
}

func staleCursor() error {
	return &domain.Error{Type: domain.ErrorStateConflict, Code: "CURSOR_STALE", Message: "collection changed; restart without a cursor"}
}
