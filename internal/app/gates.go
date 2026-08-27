package app

import (
	"fmt"
	"sort"
	"strings"
)

// GateState is the settled evidence classification for one exact gate identity.
type GateState string

const (
	GateClosed        GateState = "CLOSED"
	GateOpenOperation GateState = "OPEN_OPERATION"
	GateOpenPath      GateState = "OPEN_PATH"
	GateOpenClaim     GateState = "OPEN_CLAIM"
	GateDormant       GateState = "DORMANT"
)

var openAPIOperations = []string{
	"addGuest", "addInvitedGuestsAsHost", "cancelEvent", "createCohostRequest", "createEvent", "createTextBlast",
	"deleteCohostRequest", "firestoreGetEvent", "firestoreGetGuest", "firestoreListDocuments", "firestorePatchEvent",
	"generateEventCohostLink", "getContacts", "getCurrentGuest", "getEventInfo", "getGuests", "getLoginToken",
	"getMyPastEventsForHomePage", "getMyUpcomingEventsForHomePage", "getPosterCatalog", "lookupFirebaseUser",
	"markEventInterest", "refreshToken", "removeCohost", "revokeEventCohostLink", "sendAuthCodeTrusted",
	"signInWithCustomToken", "uploadEventPhoto",
}

// Gate is one explicit evidence boundary. Source is a non-secret evidence pointer.
type Gate struct {
	Identity string
	State    GateState
	Source   string
}

// GateManifest is immutable after validation.
type GateManifest struct {
	byIdentity map[string]Gate
}

func NewGateManifest(entries []Gate) (GateManifest, error) {
	manifest := GateManifest{byIdentity: make(map[string]Gate, len(entries))}
	for _, entry := range entries {
		identity := strings.TrimSpace(entry.Identity)
		if identity == "" || strings.HasSuffix(identity, ":") || identity == "OP11-ENDPOINT-ERRORS" || identity == "OP11-MUTATION-OUTCOME" || identity == "OP11-AUTH-REQUESTS" || identity == "OP11-PROJECTION" {
			return GateManifest{}, fmt.Errorf("gate manifest: missing full identity")
		}
		if identity != entry.Identity {
			return GateManifest{}, fmt.Errorf("gate manifest: non-canonical identity %q", entry.Identity)
		}
		if operation, parameterized := parameterizedOperation(identity); parameterized && !isOpenAPIOperation(operation) {
			return GateManifest{}, fmt.Errorf("gate manifest: %s names unknown operation %q", identity, operation)
		}
		if _, exists := manifest.byIdentity[identity]; exists {
			return GateManifest{}, fmt.Errorf("gate manifest: duplicate full identity %q", identity)
		}
		if entry.State != GateClosed && entry.State != GateOpenOperation && entry.State != GateOpenPath && entry.State != GateOpenClaim && entry.State != GateDormant {
			return GateManifest{}, fmt.Errorf("gate manifest: %s has invalid state %q", entry.Identity, entry.State)
		}
		if strings.TrimSpace(entry.Source) == "" {
			return GateManifest{}, fmt.Errorf("gate manifest: %s has empty source", entry.Identity)
		}
		manifest.byIdentity[identity] = entry
	}
	return manifest, nil
}

func (manifest GateManifest) Lookup(identity string) (Gate, bool) {
	gate, ok := manifest.byIdentity[identity]
	return gate, ok
}

func (manifest GateManifest) Allows(identity string) bool {
	gate, ok := manifest.Lookup(identity)
	return ok && gate.State == GateClosed
}

func (manifest GateManifest) Entries() []Gate {
	entries := make([]Gate, 0, len(manifest.byIdentity))
	for _, gate := range manifest.byIdentity {
		entries = append(entries, gate)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Identity < entries[right].Identity })
	return entries
}

func DefaultGateManifest() (GateManifest, error) {
	entries := []Gate{
		gate("OP11-EVENT-LIST-REQUEST", GateOpenOperation),
		gate("OP11-GET-EVENT-REQUEST", GateOpenOperation),
		gate("OP11-CREATE-EVENT-ID", GateOpenOperation),
		gate("OP11-GUEST-COHOST-FIELD", GateOpenOperation),
		gate("OP11-CURRENT-GUEST-VARIANT", GateOpenClaim),
		gate("OP11-RSVP-SPECIAL-PATHS", GateOpenOperation),
		gate("OP11-COHOST-STATE-READ", GateOpenClaim),
		gate("OP11-BLAST-FIRESTORE-READS", GateOpenOperation),
		gate("OP11-BLAST-GROUPS", GateOpenOperation),
		gate("OP11-POSTER-DUPLICATE-ID", GateOpenPath),
		gate("OP11-UPLOAD-PHOTO", GateDormant),
		gate("OP15-EVENT-DETAIL-PROJECTION:address", GateOpenOperation),
		gate("OP15-EVENT-DETAIL-PROJECTION:guest_limit", GateOpenOperation),
		gate("OP15-EVENT-DETAIL-PROJECTION:poster", GateOpenOperation),
		gate("OP15-EVENT-DETAIL-PROJECTION:links", GateOpenOperation),
		gate("COLLECTION-GUEST-PAGE-21", GateOpenPath),
	}
	for _, operation := range []string{"sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "refreshToken", "lookupFirebaseUser"} {
		entries = append(entries, gate("OP11-AUTH-REQUESTS:"+operation, GateOpenPath))
	}
	for _, operation := range openAPIOperations {
		entries = append(entries, gate("OP11-ENDPOINT-ERRORS:"+operation, GateOpenClaim))
	}
	for _, operation := range []string{"createEvent", "cancelEvent", "firestorePatchEvent", "addInvitedGuestsAsHost", "addGuest", "markEventInterest", "createCohostRequest", "deleteCohostRequest", "removeCohost", "generateEventCohostLink", "revokeEventCohostLink", "createTextBlast"} {
		entries = append(entries, gate("OP11-MUTATION-OUTCOME:"+operation, GateOpenClaim))
	}
	return NewGateManifest(entries)
}

func gate(identity string, state GateState) Gate {
	return Gate{Identity: identity, State: state, Source: "https://github.com/KalebCole/partiful/issues/15#issuecomment-5435624713"}
}

func parameterizedOperation(identity string) (string, bool) {
	for _, prefix := range []string{"OP11-AUTH-REQUESTS:", "OP11-ENDPOINT-ERRORS:", "OP11-MUTATION-OUTCOME:"} {
		if strings.HasPrefix(identity, prefix) {
			return strings.TrimPrefix(identity, prefix), true
		}
	}
	return "", false
}

func isOpenAPIOperation(operation string) bool {
	for _, candidate := range openAPIOperations {
		if operation == candidate {
			return true
		}
	}
	return false
}
