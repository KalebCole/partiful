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
		if _, exists := manifest.byIdentity[entry.Identity]; exists {
			return GateManifest{}, fmt.Errorf("gate manifest: duplicate full identity %q", entry.Identity)
		}
		if entry.State != GateClosed && entry.State != GateOpenOperation && entry.State != GateOpenPath && entry.State != GateOpenClaim && entry.State != GateDormant {
			return GateManifest{}, fmt.Errorf("gate manifest: %s has invalid state %q", entry.Identity, entry.State)
		}
		if strings.TrimSpace(entry.Source) == "" {
			return GateManifest{}, fmt.Errorf("gate manifest: %s has empty source", entry.Identity)
		}
		manifest.byIdentity[entry.Identity] = entry
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
		gate("OP11-CURRENT-GUEST-NOT-FOUND", GateOpenClaim),
		gate("OP11-RSVP-SPECIAL-STATUS", GateOpenOperation),
		gate("OP11-COHOST-STATE", GateOpenClaim),
		gate("OP11-BLAST-FIRESTORE-TARGETS", GateOpenOperation),
		gate("OP11-BLAST-GROUPS", GateOpenOperation),
		gate("OP11-POSTER-DUPLICATE", GateOpenPath),
		gate("OP11-UPLOAD-PHOTO", GateDormant),
		gate("OP11-PROJECTION:EVENT-LIST-SUMMARY", GateOpenOperation),
		gate("OP11-PROJECTION:EVENT-DETAIL", GateOpenOperation),
		gate("OP11-PROJECTION:GUEST-LIST-REQUIRED", GateOpenOperation),
		gate("OP11-PROJECTION:POSTER-PUBLIC-FIELDS", GateOpenOperation),
		gate("COLLECTION-GUEST-PAGE-21", GateOpenPath),
	}
	for _, operation := range []string{"sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "refreshToken", "lookupFirebaseUser"} {
		entries = append(entries, gate("OP11-AUTH-REQUESTS:"+operation, GateOpenPath))
	}
	for _, operation := range []string{
		"sendAuthCodeTrusted", "getLoginToken", "signInWithCustomToken", "refreshToken", "lookupFirebaseUser",
		"getMyUpcomingEventsForHomePage", "getMyPastEventsForHomePage", "getEventInfo", "createEvent", "updateEvent", "cancelEvent",
		"getGuests", "addGuest", "markGuestInterested", "inviteGuest", "setCohostStatus", "getCurrentGuest", "sendBlast",
		"getContacts", "getPosterCatalog", "queryEvent", "queryGuest", "queryGuestConfig", "createMessage", "createFeedMessage",
		"updateMessage", "uploadEventPhoto", "putPosterImage",
	} {
		entries = append(entries, gate("OP11-ENDPOINT-ERRORS:"+operation, GateOpenClaim))
	}
	for _, operation := range []string{"createEvent", "updateEvent", "cancelEvent", "addGuest", "markGuestInterested", "inviteGuest", "setCohostStatus", "sendBlast", "createMessage", "createFeedMessage", "updateMessage", "putPosterImage"} {
		entries = append(entries, gate("OP11-MUTATION-OUTCOME:"+operation, GateOpenClaim))
	}
	return NewGateManifest(entries)
}

func gate(identity string, state GateState) Gate {
	return Gate{Identity: identity, State: state, Source: "https://github.com/KalebCole/partiful/issues/15#issuecomment-5435499705"}
}
