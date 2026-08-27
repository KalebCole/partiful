package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/auth"
	"github.com/KalebCole/partiful/internal/domain"
	"github.com/KalebCole/partiful/internal/transport"
)

var eventDetailProjectionGates = []string{
	"OP15-EVENT-DETAIL-PROJECTION:address",
	"OP15-EVENT-DETAIL-PROJECTION:guest_limit",
	"OP15-EVENT-DETAIL-PROJECTION:poster",
	"OP15-EVENT-DETAIL-PROJECTION:links",
}

// EventCredentialProvider acquires the private credential required by event transports.
type EventCredentialProvider interface {
	Acquire(context.Context) (auth.Authorization, error)
}

// BindEventOperations installs event reads and one-attempt mutation workflows.
func BindEventOperations(service *Service, credentials EventCredentialProvider, callable transport.CallableTransport, firestore transport.FirestoreTransport, posters transport.PosterTransport, cursors *CursorCodec) error {
	if service == nil || credentials == nil || callable == nil || firestore == nil || posters == nil || cursors == nil {
		return fmt.Errorf("bind event operations: missing dependency")
	}
	workflows := eventWorkflows{service: service, credentials: credentials, callable: callable, firestore: firestore, posters: posters, cursors: cursors}
	bindings := []func() error{
		func() error {
			return BindOperation(service, OperationSpec[domain.ListEventsInput, domain.EventsResult]{
				Operation: domain.OperationListEvents, RequiredGates: []string{"OP11-EVENT-LIST-REQUEST"}, Execute: workflows.listEvents,
			})
		},
		func() error {
			gates := append([]string{"OP11-GET-EVENT-REQUEST"}, eventDetailProjectionGates...)
			return BindOperation(service, OperationSpec[domain.GetEventInput, domain.EventResult]{
				Operation: domain.OperationGetEvent, RequiredGates: gates, ErrorGate: "OP11-ENDPOINT-ERRORS:getEventInfo", Execute: workflows.getEvent,
			})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CreateEventInput, domain.SubmittedResult]{
				Operation: domain.OperationCreateEvent, RequiredGates: []string{"OP11-CREATE-EVENT-ID"}, ErrorGate: "OP11-ENDPOINT-ERRORS:createEvent", OutcomeGate: "OP11-MUTATION-OUTCOME:createEvent", Execute: workflows.createEvent,
			})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.UpdateEventInput, domain.UpdateEventResult]{
				Operation: domain.OperationUpdateEvent, ErrorGate: "OP11-ENDPOINT-ERRORS:firestorePatchEvent", OutcomeGate: "OP11-MUTATION-OUTCOME:firestorePatchEvent", Execute: workflows.updateEvent,
			})
		},
		func() error {
			return BindOperation(service, OperationSpec[domain.CancelEventInput, domain.CancelEventResult]{
				Operation: domain.OperationCancelEvent, ErrorGate: "OP11-ENDPOINT-ERRORS:cancelEvent", OutcomeGate: "OP11-MUTATION-OUTCOME:cancelEvent", Execute: workflows.cancelEvent,
			})
		},
	}
	for _, bind := range bindings {
		if err := bind(); err != nil {
			return err
		}
	}
	return nil
}

type eventWorkflows struct {
	service     *Service
	credentials EventCredentialProvider
	callable    transport.CallableTransport
	firestore   transport.FirestoreTransport
	posters     transport.PosterTransport
	cursors     *CursorCodec
}

func (workflows eventWorkflows) authorize(ctx context.Context) (transport.Credential, error) {
	authorization, err := workflows.credentials.Acquire(ctx)
	if err != nil {
		return "", err
	}
	credential := transport.Credential(authorization.AccessToken.Reveal())
	if credential == "" {
		return "", &domain.Error{Type: domain.ErrorAuthRequired, Code: "AUTH_REQUIRED", Message: "authentication is required"}
	}
	return credential, nil
}

func (workflows eventWorkflows) listEvents(ctx context.Context, _ *Invocation, input domain.ListEventsInput) (domain.EventsResult, error) {
	normalized, err := NormalizeCollectionInput(input.CollectionInput)
	if err != nil {
		return domain.EventsResult{}, err
	}
	if input.When != domain.EventWhenUpcoming && input.When != domain.EventWhenPast {
		return domain.EventsResult{}, invalidEventInput()
	}
	credential, err := workflows.authorize(ctx)
	if err != nil {
		return domain.EventsResult{}, err
	}
	var response transport.ListHomeEventsResult
	if input.When == domain.EventWhenUpcoming {
		response, err = workflows.callable.ListUpcomingEvents(ctx, transport.ListHomeEventsRequest{Credential: credential})
	} else {
		response, err = workflows.callable.ListPastEvents(ctx, transport.ListHomeEventsRequest{Credential: credential})
	}
	if err != nil {
		identity := "OP11-ENDPOINT-ERRORS:getMyUpcomingEventsForHomePage"
		if input.When == domain.EventWhenPast {
			identity = "OP11-ENDPOINT-ERRORS:getMyPastEventsForHomePage"
		}
		return domain.EventsResult{}, workflows.guardRemoteClaim(identity, err)
	}
	projected, err := ProjectSequence(response.Events, projectEventSummary)
	if err != nil {
		return domain.EventsResult{}, err
	}
	page, err := SliceCollection(workflows.cursors, CursorScope{Operation: domain.OperationListEvents, CanonicalFilter: string(input.When)}, normalized, projected)
	if err != nil {
		return domain.EventsResult{}, err
	}
	return domain.EventsResult{Events: page.Items, NextCursor: page.NextCursor, HasMore: page.HasMore}, nil
}

func (workflows eventWorkflows) getEvent(ctx context.Context, _ *Invocation, input domain.GetEventInput) (domain.EventResult, error) {
	if strings.TrimSpace(string(input.EventID)) == "" {
		return domain.EventResult{}, invalidEventInput()
	}
	credential, err := workflows.authorize(ctx)
	if err != nil {
		return domain.EventResult{}, err
	}
	response, err := workflows.callable.GetEvent(ctx, transport.GetEventRequest{Credential: credential, EventID: transport.EventID(input.EventID)})
	if err != nil {
		return domain.EventResult{}, err
	}
	event, err := projectEventDetail(response.Event)
	if err != nil {
		return domain.EventResult{}, err
	}
	return domain.EventResult{Event: event}, nil
}

func (workflows eventWorkflows) createEvent(ctx context.Context, invocation *Invocation, input domain.CreateEventInput) (domain.SubmittedResult, error) {
	if err := validateCreateEvent(input); err != nil {
		return domain.SubmittedResult{}, err
	}
	if input.DryRun {
		return domain.SubmittedResult{Submitted: false}, nil
	}
	if len(input.Links) != 0 {
		return domain.SubmittedResult{}, evidenceGateOpen()
	}
	var posterID *transport.PosterID
	if input.PosterID != nil {
		if !workflows.service.gates.Allows("OP11-POSTER-DUPLICATE-ID") {
			return domain.SubmittedResult{}, evidenceGateOpen()
		}
		catalog, err := workflows.posters.GetCatalog(ctx, transport.GetPosterCatalogRequest{})
		if err != nil {
			return domain.SubmittedResult{}, workflows.guardRemoteClaim("OP11-ENDPOINT-ERRORS:getPosterCatalog", err)
		}
		matches := 0
		for _, poster := range catalog.Posters {
			if poster.PosterID == transport.PosterID(*input.PosterID) {
				matches++
			}
		}
		if matches == 0 {
			return domain.SubmittedResult{}, &domain.Error{Type: domain.ErrorResourceNotFound, Code: "POSTER_NOT_FOUND", Message: "the poster was not found"}
		}
		if matches != 1 {
			return domain.SubmittedResult{}, &domain.Error{Type: domain.ErrorMatchAmbiguous, Code: "POSTER_AMBIGUOUS", Message: "the poster identifier is not unique"}
		}
		value := transport.PosterID(*input.PosterID)
		posterID = &value
	}
	credential, err := workflows.authorize(ctx)
	if err != nil {
		return domain.SubmittedResult{}, err
	}
	visibility := optionalVisibility(input.Visibility)
	_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return workflows.callable.CreateEvent(ctx, transport.CreateEventRequest{
			Credential: credential, Title: strings.TrimSpace(input.Title), Start: input.Start, End: input.End, Timezone: string(input.Timezone),
			Description: input.Description, Location: input.Location, Visibility: visibility, GuestLimit: input.GuestLimit, PosterID: posterID,
		})
	})
	if err != nil {
		return domain.SubmittedResult{}, err
	}
	return domain.SubmittedResult{Submitted: true}, nil
}

func (workflows eventWorkflows) updateEvent(ctx context.Context, invocation *Invocation, input domain.UpdateEventInput) (domain.UpdateEventResult, error) {
	if input.DryRun {
		publicFields, err := validateEventUpdateIntent(input)
		return domain.UpdateEventResult{EventID: input.EventID, Fields: publicFields, Submitted: false}, err
	}
	fields, mask, publicFields, err := mapEventUpdate(input)
	result := domain.UpdateEventResult{EventID: input.EventID, Fields: publicFields, Submitted: false}
	if err != nil {
		return result, err
	}
	credential, err := workflows.authorize(ctx)
	if err != nil {
		return domain.UpdateEventResult{}, err
	}
	_, err = DispatchMutation(invocation, func() (transport.EventDocument, error) {
		return workflows.firestore.PatchEvent(ctx, transport.PatchEventDocumentRequest{Credential: credential, EventID: transport.EventID(input.EventID), Fields: fields, FieldMask: mask, MustExist: true})
	})
	if err != nil {
		return domain.UpdateEventResult{}, err
	}
	result.Submitted = true
	return result, nil
}

func (workflows eventWorkflows) cancelEvent(ctx context.Context, invocation *Invocation, input domain.CancelEventInput) (domain.CancelEventResult, error) {
	result := domain.CancelEventResult{EventID: input.EventID, NotifyGuests: input.NotifyGuests, Submitted: false}
	if strings.TrimSpace(string(input.EventID)) == "" {
		return result, invalidEventInput()
	}
	if input.DryRun {
		return result, nil
	}
	credential, err := workflows.authorize(ctx)
	if err != nil {
		return domain.CancelEventResult{}, err
	}
	_, err = DispatchMutation(invocation, func() (transport.Completion, error) {
		return workflows.callable.CancelEvent(ctx, transport.CancelEventRequest{Credential: credential, EventID: transport.EventID(input.EventID), Message: input.Message, SkipNotifyGuests: !input.NotifyGuests})
	})
	if err != nil {
		return domain.CancelEventResult{}, err
	}
	result.Submitted = true
	return result, nil
}

func mapEventUpdate(input domain.UpdateEventInput) (map[string]transport.FieldValue, []string, []string, error) {
	if strings.TrimSpace(string(input.EventID)) == "" {
		return nil, nil, nil, invalidEventInput()
	}
	fields := make(map[string]transport.FieldValue)
	public := make([]string, 0, 8)
	addString := func(publicName, privateName string, change domain.Change[string]) error {
		if !change.Set {
			return nil
		}
		public = append(public, publicName)
		if change.Value == nil {
			return evidenceGateOpen()
		}
		value := *change.Value
		if publicName == "title" {
			value = strings.TrimSpace(value)
			if value == "" {
				return invalidEventInput()
			}
		}
		fields[privateName] = transport.FieldValue{String: &value}
		return nil
	}
	if err := addString("title", "title", input.Title); err != nil {
		return nil, nil, public, err
	}
	if err := addString("description", "description", input.Description); err != nil {
		return nil, nil, public, err
	}
	if input.Start.Set {
		public = append(public, "start")
		if input.Start.Value == nil || input.Start.Value.IsZero() {
			return nil, nil, public, invalidEventInput()
		}
		value := input.Start.Value.UTC().Format(time.RFC3339Nano)
		fields["startDate"] = transport.FieldValue{String: &value}
	}
	if input.End.Set {
		public = append(public, "end")
		if input.End.Value == nil {
			return nil, nil, public, evidenceGateOpen()
		}
		value := input.End.Value.UTC().Format(time.RFC3339Nano)
		fields["endDate"] = transport.FieldValue{String: &value}
	}
	if input.Timezone.Set {
		public = append(public, "timezone")
		if input.Timezone.Value == nil || !validTimezone(*input.Timezone.Value) {
			return nil, nil, public, invalidEventInput()
		}
		value := string(*input.Timezone.Value)
		fields["timezone"] = transport.FieldValue{String: &value}
	}
	if input.GuestLimit.Set {
		public = append(public, "guest_limit")
		if input.GuestLimit.Value == nil {
			return nil, nil, public, evidenceGateOpen()
		}
		if *input.GuestLimit.Value < 1 {
			return nil, nil, public, invalidEventInput()
		}
		limit := int64(*input.GuestLimit.Value)
		waitlist := false
		fields["maxCapacity"] = transport.FieldValue{Integer: &limit}
		fields["enableWaitlist"] = transport.FieldValue{Boolean: &waitlist}
	}
	if input.Links.Set {
		public = append(public, "links")
		return nil, nil, public, evidenceGateOpen()
	}
	if input.PosterID.Set {
		public = append(public, "poster_id")
		return nil, nil, public, evidenceGateOpen()
	}
	if len(public) == 0 {
		return nil, nil, nil, invalidEventInput()
	}
	sort.Strings(public)
	mask := make([]string, 0, len(fields))
	for name := range fields {
		mask = append(mask, name)
	}
	sort.Strings(mask)
	return fields, mask, public, nil
}

func projectEventDetail(value transport.EventDetail) (domain.EventDetail, error) {
	summary, err := projectEventSummary(value.EventSummary)
	if err != nil {
		return domain.EventDetail{}, err
	}
	return domain.EventDetail{EventSummary: summary, Description: cloneString(value.Description), Location: cloneString(value.Location)}, nil
}

func projectEventSummary(value transport.EventSummary) (domain.EventSummary, error) {
	if value.EventID == "" {
		return domain.EventSummary{}, protocolChangedEvent()
	}
	result := domain.EventSummary{EventID: domain.EventID(value.EventID), Title: cloneString(value.Title), Start: cloneTime(value.Start), End: cloneTime(value.End), UserRole: cloneString(value.UserRole)}
	if value.Timezone != nil {
		if !validTimezone(domain.Timezone(*value.Timezone)) {
			return domain.EventSummary{}, protocolChangedEvent()
		}
		timezone := domain.Timezone(*value.Timezone)
		result.Timezone = &timezone
	}
	if value.State != nil {
		state := ""
		switch *value.State {
		case "PUBLISHED":
			state = "active"
		case "CANCELED":
			state = "cancelled"
		default:
			return domain.EventSummary{}, protocolChangedEvent()
		}
		result.State = &state
	}
	if value.RSVPStatus != nil {
		status := domain.EventReadRSVP(strings.ReplaceAll(strings.ToLower(*value.RSVPStatus), "_", "-"))
		if !status.Valid() {
			return domain.EventSummary{}, protocolChangedEvent()
		}
		result.MyRSVP = &status
	}
	return result, nil
}

func validateCreateEvent(input domain.CreateEventInput) error {
	if strings.TrimSpace(input.Title) == "" || input.Start.IsZero() || !validTimezone(input.Timezone) {
		return invalidEventInput()
	}
	if input.End != nil && input.End.Before(input.Start) {
		return invalidEventInput()
	}
	if input.Visibility != nil && *input.Visibility != domain.EventVisibilityPrivate && *input.Visibility != domain.EventVisibilityPublic {
		return invalidEventInput()
	}
	if input.GuestLimit != nil && *input.GuestLimit < 1 {
		return invalidEventInput()
	}
	return validateEventLinks(input.Links)
}

func validateEventUpdateIntent(input domain.UpdateEventInput) ([]string, error) {
	if strings.TrimSpace(string(input.EventID)) == "" {
		return nil, invalidEventInput()
	}
	fields := make([]string, 0, 8)
	if input.Title.Set {
		fields = append(fields, "title")
		if input.Title.Value == nil || strings.TrimSpace(*input.Title.Value) == "" {
			return fields, invalidEventInput()
		}
	}
	if input.Description.Set {
		fields = append(fields, "description")
	}
	if input.Start.Set {
		fields = append(fields, "start")
		if input.Start.Value == nil || input.Start.Value.IsZero() {
			return fields, invalidEventInput()
		}
	}
	if input.End.Set {
		fields = append(fields, "end")
	}
	if input.Timezone.Set {
		fields = append(fields, "timezone")
		if input.Timezone.Value == nil || !validTimezone(*input.Timezone.Value) {
			return fields, invalidEventInput()
		}
	}
	if input.GuestLimit.Set {
		fields = append(fields, "guest_limit")
		if input.GuestLimit.Value != nil && *input.GuestLimit.Value < 1 {
			return fields, invalidEventInput()
		}
	}
	if input.Links.Set {
		fields = append(fields, "links")
		if input.Links.Value != nil {
			if err := validateEventLinks(*input.Links.Value); err != nil {
				return fields, err
			}
		}
	}
	if input.PosterID.Set {
		fields = append(fields, "poster_id")
		if input.PosterID.Value != nil && strings.TrimSpace(string(*input.PosterID.Value)) == "" {
			return fields, invalidEventInput()
		}
	}
	if len(fields) == 0 {
		return nil, invalidEventInput()
	}
	sort.Strings(fields)
	return fields, nil
}

func validateEventLinks(links []domain.EventLink) error {
	for _, link := range links {
		parsed, err := url.ParseRequestURI(strings.TrimSpace(link.URL))
		if strings.TrimSpace(link.Label) == "" || err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return invalidEventInput()
		}
	}
	return nil
}

func validTimezone(value domain.Timezone) bool {
	_, err := time.LoadLocation(string(value))
	return strings.TrimSpace(string(value)) != "" && err == nil
}
func optionalVisibility(value *domain.EventVisibility) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func invalidEventInput() error {
	return &domain.Error{Type: domain.ErrorInputInvalid, Code: "INVALID_EVENT_INPUT", Message: "event operation input is invalid"}
}
func protocolChangedEvent() error {
	return &domain.Error{Type: domain.ErrorContractProtocolChanged, Code: "PROTOCOL_CHANGED", Message: "the remote event cannot be projected safely"}
}
func (workflows eventWorkflows) guardRemoteClaim(identity string, err error) error {
	if err == nil || workflows.service.gates.Allows(identity) || hasEvidencedClassification(err) {
		return err
	}
	return evidenceClaimOpen()
}
