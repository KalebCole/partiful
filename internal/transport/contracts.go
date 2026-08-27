package transport

import (
	"context"
	"time"
)

// Private scalar identities are deliberately distinct from public domain values.
type (
	EventID       string
	GuestID       string
	ContactID     string
	PosterID      string
	RemoteCursor  string
	Credential    string
	CorrelationID string
)

type DispatchState string

const (
	DispatchNotStarted DispatchState = "not-started"
	DispatchStarted    DispatchState = "started"
)

// ProtocolFailure contains only classified, sanitized transport facts.
type ProtocolFailure struct {
	Operation     string
	Class         string
	Retryable     bool
	DispatchState DispatchState
	CorrelationID CorrelationID
}

func (failure *ProtocolFailure) Error() string {
	if failure == nil {
		return ""
	}
	return "remote protocol failure"
}

type Completion struct{ DispatchState DispatchState }

type EventSummary struct {
	EventID    EventID
	Title      *string
	Start      *time.Time
	End        *time.Time
	Timezone   *string
	State      *string
	UserRole   *string
	RSVPStatus *string
}

type EventDetail struct {
	EventSummary
	Description *string
	Location    *string
}

type Contact struct {
	ContactID        ContactID
	DisplayName      string
	SharedEventCount int
}

type Guest struct {
	GuestID     GuestID
	ContactID   *ContactID
	DisplayName string
	RSVPStatus  *string
	PartySize   *int
}

type CurrentGuest struct {
	GuestID GuestID
	Status  string
}

type Poster struct {
	PosterID    PosterID
	Name        string
	URL         string
	ContentType string
	Width       int
	Height      int
	Tags        []string
	Categories  []string
}

type ListHomeEventsRequest struct{ Credential Credential }
type ListHomeEventsResult struct{ Events []EventSummary }
type GetEventRequest struct {
	Credential Credential
	EventID    EventID
}
type GetEventResult struct{ Event EventDetail }
type CreateEventRequest struct {
	Credential  Credential
	Title       string
	Start       time.Time
	End         *time.Time
	Timezone    string
	Description *string
	Location    *string
	Visibility  *string
	GuestLimit  *int
	PosterID    *PosterID
}
type CancelEventRequest struct {
	Credential       Credential
	EventID          EventID
	Message          *string
	SkipNotifyGuests bool
}
type GetContactsRequest struct {
	Credential Credential
	MaxResults int
	Cursor     *RemoteCursor
}
type GetContactsResult struct {
	Contacts []Contact
	Cursor   *RemoteCursor
}
type GetGuestsRequest struct {
	Credential           Credential
	EventID              EventID
	IncludeInvitedGuests bool
	MaxResults           int
	Cursor               *RemoteCursor
}
type GetGuestsResult struct {
	Guests []Guest
	Cursor *RemoteCursor
}
type InviteGuestRequest struct {
	Credential Credential
	EventID    EventID
	ContactID  ContactID
	Message    string
}
type CohostRequest struct {
	Credential Credential
	EventID    EventID
	ContactID  ContactID
}
type CohostLinkRequest struct {
	Credential Credential
	EventID    EventID
}
type CohostLinkResult struct{ URL string }
type GetCurrentGuestRequest struct {
	Credential Credential
	EventID    EventID
}
type GetCurrentGuestResult struct{ Guest *CurrentGuest }
type SetGuestRequest struct {
	Credential           Credential
	EventID              EventID
	GuestID              *GuestID
	Status               string
	DisplayName          string
	PartySize            int
	PlusOnes             []string
	Timezone             string
	Message              *string
	QuestionnaireVersion *int
	QuestionnaireAnswers map[string]string
}
type MarkInterestRequest struct {
	Credential Credential
	EventID    EventID
	Interested bool
}
type TextBlastGroup struct {
	Name     string
	GuestIDs []GuestID
}
type CreateTextBlastRequest struct {
	Credential      Credential
	EventID         EventID
	Message         string
	ShowOnEventPage bool
	Groups          []TextBlastGroup
}

// CallableTransport is the private typed port for evidenced callable operations.
type CallableTransport interface {
	ListUpcomingEvents(context.Context, ListHomeEventsRequest) (ListHomeEventsResult, error)
	ListPastEvents(context.Context, ListHomeEventsRequest) (ListHomeEventsResult, error)
	GetEvent(context.Context, GetEventRequest) (GetEventResult, error)
	CreateEvent(context.Context, CreateEventRequest) (Completion, error)
	CancelEvent(context.Context, CancelEventRequest) (Completion, error)
	GetContacts(context.Context, GetContactsRequest) (GetContactsResult, error)
	GetGuests(context.Context, GetGuestsRequest) (GetGuestsResult, error)
	InviteGuest(context.Context, InviteGuestRequest) (Completion, error)
	GetCurrentGuest(context.Context, GetCurrentGuestRequest) (GetCurrentGuestResult, error)
	SetGuest(context.Context, SetGuestRequest) (Completion, error)
	MarkInterest(context.Context, MarkInterestRequest) (Completion, error)
	InviteCohost(context.Context, CohostRequest) (Completion, error)
	RevokeCohostInvite(context.Context, CohostRequest) (Completion, error)
	RemoveCohost(context.Context, CohostRequest) (Completion, error)
	CreateCohostLink(context.Context, CohostLinkRequest) (CohostLinkResult, error)
	RevokeCohostLink(context.Context, CohostLinkRequest) (Completion, error)
	CreateTextBlast(context.Context, CreateTextBlastRequest) (Completion, error)
}

type FieldValue struct {
	String  *string
	Integer *int64
	Boolean *bool
	Time    *time.Time
	Strings []string
}

type EventDocument struct {
	EventID EventID
	Fields  map[string]FieldValue
}

type GuestDocument struct {
	GuestID GuestID
	Status  *string
	CheckIn *string
}

type HostMessageDocument struct{ Kind *string }

type GetEventDocumentRequest struct {
	Credential Credential
	EventID    EventID
}
type PatchEventDocumentRequest struct {
	Credential Credential
	EventID    EventID
	Fields     map[string]FieldValue
	FieldMask  []string
	MustExist  bool
}
type GetGuestDocumentRequest struct {
	Credential Credential
	EventID    EventID
	GuestID    GuestID
}
type ListEventDocumentsRequest struct {
	Credential Credential
	EventID    EventID
	Cursor     *RemoteCursor
}
type GuestDocumentPage struct {
	Documents []GuestDocument
	Cursor    *RemoteCursor
}
type HostMessageDocumentPage struct {
	Documents []HostMessageDocument
	Cursor    *RemoteCursor
}

// FirestoreTransport is the private typed port for evidenced document operations.
type FirestoreTransport interface {
	GetEvent(context.Context, GetEventDocumentRequest) (EventDocument, error)
	PatchEvent(context.Context, PatchEventDocumentRequest) (EventDocument, error)
	GetGuest(context.Context, GetGuestDocumentRequest) (GuestDocument, error)
	ListEventGuests(context.Context, ListEventDocumentsRequest) (GuestDocumentPage, error)
	ListEventHostMessages(context.Context, ListEventDocumentsRequest) (HostMessageDocumentPage, error)
}

type GetPosterCatalogRequest struct{}
type GetPosterCatalogResult struct{ Posters []Poster }

// PosterTransport is the private typed port for the unauthenticated poster catalog.
type PosterTransport interface {
	GetCatalog(context.Context, GetPosterCatalogRequest) (GetPosterCatalogResult, error)
}
