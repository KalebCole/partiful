package domain

import "time"

// Public scalar identities never contain private transport identifiers.
type (
	EventID    string
	ContactRef string
	PosterID   string
	Cursor     string
	Timezone   string
)

// CollectionInput is the shared bounded collection contract.
type CollectionInput struct {
	Limit    int
	Cursor   *Cursor
	All      bool
	MaxItems *int
}

// ContactSelector selects one contact by opaque reference or unique display name.
type ContactSelector struct {
	ContactRef *ContactRef
	Contact    *string
}

// EventReadRSVP is the closed application-visible RSVP read vocabulary.
type EventReadRSVP string

const (
	EventReadRSVPReadyToSend           EventReadRSVP = "ready-to-send"
	EventReadRSVPSending               EventReadRSVP = "sending"
	EventReadRSVPSendError             EventReadRSVP = "send-error"
	EventReadRSVPDeliveryError         EventReadRSVP = "delivery-error"
	EventReadRSVPSent                  EventReadRSVP = "sent"
	EventReadRSVPInterested            EventReadRSVP = "interested"
	EventReadRSVPWaitlist              EventReadRSVP = "waitlist"
	EventReadRSVPMaybe                 EventReadRSVP = "maybe"
	EventReadRSVPDeclined              EventReadRSVP = "declined"
	EventReadRSVPGoing                 EventReadRSVP = "going"
	EventReadRSVPPendingApproval       EventReadRSVP = "pending-approval"
	EventReadRSVPApproved              EventReadRSVP = "approved"
	EventReadRSVPWithdrawn             EventReadRSVP = "withdrawn"
	EventReadRSVPWaitlistedForApproval EventReadRSVP = "waitlisted-for-approval"
	EventReadRSVPRejected              EventReadRSVP = "rejected"
	EventReadRSVPRespondedToFindATime  EventReadRSVP = "responded-to-find-a-time"
)

var eventReadRSVPValues = [...]EventReadRSVP{
	EventReadRSVPReadyToSend,
	EventReadRSVPSending,
	EventReadRSVPSendError,
	EventReadRSVPDeliveryError,
	EventReadRSVPSent,
	EventReadRSVPInterested,
	EventReadRSVPWaitlist,
	EventReadRSVPMaybe,
	EventReadRSVPDeclined,
	EventReadRSVPGoing,
	EventReadRSVPPendingApproval,
	EventReadRSVPApproved,
	EventReadRSVPWithdrawn,
	EventReadRSVPWaitlistedForApproval,
	EventReadRSVPRejected,
	EventReadRSVPRespondedToFindATime,
}

func EventReadRSVPValues() []EventReadRSVP {
	result := make([]EventReadRSVP, len(eventReadRSVPValues))
	copy(result, eventReadRSVPValues[:])
	return result
}

func (value EventReadRSVP) Valid() bool {
	for _, candidate := range eventReadRSVPValues {
		if value == candidate {
			return true
		}
	}
	return false
}

type EventWhen string

const (
	EventWhenUpcoming EventWhen = "upcoming"
	EventWhenPast     EventWhen = "past"
)

type EventVisibility string

const (
	EventVisibilityPrivate EventVisibility = "private"
	EventVisibilityPublic  EventVisibility = "public"
)

type EventLink struct {
	Label string
	URL   string
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

type EventSummary struct {
	EventID  EventID
	Title    *string
	Start    *time.Time
	End      *time.Time
	Timezone *Timezone
	State    *string
	UserRole *string
	MyRSVP   *EventReadRSVP
}

type EventDetail struct {
	EventSummary
	Description *string
	Location    *string
	Address     *string
	Visibility  *EventVisibility
	GuestLimit  *int
	Poster      *Poster
	Links       []EventLink
}

type ListEventsInput struct {
	When EventWhen
	CollectionInput
}

type EventsResult struct {
	Events     []EventSummary
	NextCursor *Cursor
	HasMore    bool
}

type GetEventInput struct{ EventID EventID }
type EventResult struct{ Event EventDetail }

type CreateEventInput struct {
	Title       string
	Start       time.Time
	Timezone    Timezone
	End         *time.Time
	Description *string
	Location    *string
	Visibility  *EventVisibility
	GuestLimit  *int
	Links       []EventLink
	PosterID    *PosterID
	DryRun      bool
}

type SubmittedResult struct{ Submitted bool }

type Change[T any] struct {
	Set   bool
	Value *T
}

type UpdateEventInput struct {
	EventID     EventID
	Title       Change[string]
	Description Change[string]
	Start       Change[time.Time]
	End         Change[time.Time]
	Timezone    Change[Timezone]
	GuestLimit  Change[int]
	Links       Change[[]EventLink]
	PosterID    Change[PosterID]
	DryRun      bool
}

type UpdateEventResult struct {
	EventID   EventID
	Fields    []string
	Submitted bool
}

type CancelEventInput struct {
	EventID      EventID
	Message      *string
	NotifyGuests bool
	DryRun       bool
}

type CancelEventResult struct {
	EventID      EventID
	NotifyGuests bool
	Submitted    bool
}

type Guest struct {
	DisplayName string
	RSVPStatus  *EventReadRSVP
	PartySize   *int
	Cohost      bool
}

type ListGuestsInput struct {
	EventID EventID
	CollectionInput
}

type GuestsResult struct {
	Guests     []Guest
	NextCursor *Cursor
	HasMore    bool
}

type InviteGuestInput struct {
	EventID EventID
	ContactSelector
	Message *string
	DryRun  bool
}

type RSVPResult struct {
	EventID EventID
	Status  *EventReadRSVP
}

type RSVPIntent string

const (
	RSVPIntentGoing         RSVPIntent = "going"
	RSVPIntentNotGoing      RSVPIntent = "not-going"
	RSVPIntentInterested    RSVPIntent = "interested"
	RSVPIntentNotInterested RSVPIntent = "not-interested"
)

type SetRSVPInput struct {
	EventID               EventID
	Status                RSVPIntent
	DisplayName           *string
	PartySize             *int
	PlusOnes              []string
	Timezone              *Timezone
	Message               *string
	QuestionnaireResponse map[string]string
	DryRun                bool
}

type SetRSVPResult struct {
	EventID   EventID
	Intent    RSVPIntent
	Submitted bool
}

type Contact struct {
	ContactRef       ContactRef
	DisplayName      string
	SharedEventCount int
}

type ListContactsInput struct {
	Query *string
	CollectionInput
}

type ContactsResult struct {
	Contacts   []Contact
	NextCursor *Cursor
	HasMore    bool
}

type CohostInput struct {
	EventID EventID
	ContactSelector
	DryRun bool
}

type CohostStatus string

const (
	CohostStatusInvited CohostStatus = "invited"
	CohostStatusRevoked CohostStatus = "revoked"
	CohostStatusRemoved CohostStatus = "removed"
)

type CohostResult struct {
	EventID EventID
	Cohost  struct {
		DisplayName string
		Status      CohostStatus
	}
}

type CohostLinkInput struct {
	EventID EventID
	DryRun  bool
}

type CohostLinkState string

const (
	CohostLinkActive  CohostLinkState = "active"
	CohostLinkRevoked CohostLinkState = "revoked"
)

type CohostLinkResult struct {
	EventID EventID
	Link    struct {
		URL   *string
		State CohostLinkState
	}
}

type SendBlastInput struct {
	EventID         EventID
	Audience        string
	Message         string
	ShowOnEventPage bool
	DryRun          bool
}

type BlastResult struct {
	EventID         EventID
	Submitted       bool
	Audience        string
	ShowOnEventPage bool
	RecipientStatus string
}

type ListPostersInput struct{ CollectionInput }
type SearchPostersInput struct {
	Query string
	CollectionInput
}

type PostersResult struct {
	Posters    []Poster
	NextCursor *Cursor
	HasMore    bool
}

type TokenState string

const (
	TokenStateHealthy  TokenState = "healthy"
	TokenStateExpiring TokenState = "expiring"
	TokenStateExpired  TokenState = "expired"
	TokenStateMissing  TokenState = "missing"
	TokenStateUnknown  TokenState = "unknown"
)

type AuthState struct {
	Authenticated bool
	TokenState    TokenState
	ExpiresAt     *time.Time
}

type DoctorStatus string

const (
	DoctorStatusPass DoctorStatus = "pass"
	DoctorStatusWarn DoctorStatus = "warn"
	DoctorStatusFail DoctorStatus = "fail"
)

type DoctorCheck struct {
	Name        string
	Status      DoctorStatus
	Message     string
	Remediation *string
}

type DoctorResult struct {
	Healthy bool
	Checks  []DoctorCheck
}
