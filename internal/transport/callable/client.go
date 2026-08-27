package callable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/transport"
)

const (
	defaultBaseURL          = "https://api.partiful.com"
	defaultTimeout          = 15 * time.Second
	defaultMaxResponseBytes = int64(2 << 20)
)

type Config struct {
	HTTPClient        http.RoundTripper
	BaseURL           string
	AmplitudeDeviceID string
	UserID            string
	Timeout           time.Duration
	MaxResponseBytes  int64
	EventDefaults     *EventDefaults
	Posters           []transport.Poster
}

// EventDefaults are accepted deployment values that the public operation does
// not own. The client does not invent a theme, effect, or title font.
type EventDefaults struct {
	Theme     string
	Effect    string
	TitleFont *string
}

type Client struct {
	httpClient        *http.Client
	baseURL           *url.URL
	amplitudeDeviceID string
	userID            string
	timeout           time.Duration
	maxResponseBytes  int64
	eventDefaults     *EventDefaults
	posters           map[transport.PosterID]transport.Poster
	configured        bool
}

func New(config Config) *Client {
	base := config.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	parsed, err := url.Parse(base)
	configured := err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	if !configured {
		parsed, _ = url.Parse(defaultBaseURL)
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	limit := config.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if config.HTTPClient != nil {
		client.Transport = config.HTTPClient
	}
	posters := make(map[transport.PosterID]transport.Poster, len(config.Posters))
	for _, catalogPoster := range config.Posters {
		posters[catalogPoster.PosterID] = catalogPoster
	}
	return &Client{httpClient: client, baseURL: parsed, amplitudeDeviceID: config.AmplitudeDeviceID, userID: config.UserID, timeout: timeout, maxResponseBytes: limit, eventDefaults: config.EventDefaults, posters: posters, configured: configured}
}

func unsupported(operation string) error {
	return &transport.ProtocolFailure{Operation: operation, Class: "evidence.required", DispatchState: transport.DispatchNotStarted}
}
func invalid(operation string) error {
	return &transport.ProtocolFailure{Operation: operation, Class: "input.invalid", DispatchState: transport.DispatchNotStarted}
}

func (client *Client) ListUpcomingEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	return transport.ListHomeEventsResult{}, unsupported("getMyUpcomingEventsForHomePage")
}
func (client *Client) ListPastEvents(context.Context, transport.ListHomeEventsRequest) (transport.ListHomeEventsResult, error) {
	return transport.ListHomeEventsResult{}, unsupported("getMyPastEventsForHomePage")
}
func (client *Client) GetEvent(context.Context, transport.GetEventRequest) (transport.GetEventResult, error) {
	return transport.GetEventResult{}, unsupported("getEventInfo")
}
func (client *Client) CreateEvent(ctx context.Context, request transport.CreateEventRequest) (transport.Completion, error) {
	if request.Title == "" || request.Start.IsZero() || request.Timezone == "" || (request.Visibility != nil && *request.Visibility != "public" && *request.Visibility != "private") || (request.GuestLimit != nil && *request.GuestLimit < 1) {
		return transport.Completion{}, invalid("createEvent")
	}
	if client.eventDefaults == nil || client.eventDefaults.Theme == "" || client.eventDefaults.Effect == "" || request.PosterID == nil {
		return transport.Completion{}, unsupported("createEvent")
	}
	catalogPoster, ok := client.posters[*request.PosterID]
	if !ok || catalogPoster.PosterID == "" || catalogPoster.Name == "" || catalogPoster.URL == "" || catalogPoster.ContentType == "" || catalogPoster.Width < 0 || catalogPoster.Height < 0 || catalogPoster.Tags == nil || catalogPoster.Categories == nil {
		return transport.Completion{}, unsupported("createEvent")
	}
	poster := map[string]any{
		"id": catalogPoster.PosterID, "name": catalogPoster.Name, "url": catalogPoster.URL,
		"contentType": catalogPoster.ContentType, "width": catalogPoster.Width, "height": catalogPoster.Height,
		"tags": catalogPoster.Tags, "categories": catalogPoster.Categories,
	}
	image := map[string]any{
		"source": "partiful_posters", "poster": poster, "url": catalogPoster.URL,
		"blurHash": nil, "contentType": catalogPoster.ContentType,
		"name": catalogPoster.Name, "height": catalogPoster.Height, "width": catalogPoster.Width,
	}
	statuses := []string{"READY_TO_SEND", "SENDING", "SENT", "SEND_ERROR", "DELIVERY_ERROR", "INTERESTED", "MAYBE", "GOING", "DECLINED", "WAITLIST", "PENDING_APPROVAL", "APPROVED", "WITHDRAWN", "RESPONDED_TO_FIND_A_TIME", "WAITLISTED_FOR_APPROVAL", "REJECTED"}
	counts := make(map[string]int, len(statuses))
	for _, status := range statuses {
		counts[status] = 0
	}
	event := map[string]any{
		"title": request.Title, "startDate": request.Start.UTC().Format(time.RFC3339Nano), "timezone": request.Timezone,
		"guestStatusCounts": counts, "displaySettings": map[string]any{"theme": client.eventDefaults.Theme, "effect": client.eventDefaults.Effect, "titleFont": client.eventDefaults.TitleFont},
		"status": "UNSAVED", "showHostList": true, "showGuestCount": true, "showGuestList": true,
		"showActivityTimestamps": true, "displayInviteButton": true, "allowGuestPhotoUpload": true,
		"enableGuestReminders": true, "rsvpsEnabled": true, "allowGuestsToInviteMutuals": true,
		"visibility": "public", "rsvpButtonGlyphType": "emojis", "image": image,
	}
	if request.End != nil {
		event["endDate"] = request.End.UTC().Format(time.RFC3339Nano)
	}
	if request.Description != nil {
		event["description"] = *request.Description
	}
	if request.Location != nil {
		event["locationInfo"] = map[string]any{"type": "freeform", "value": *request.Location}
	}
	if request.GuestLimit != nil {
		event["maxCapacity"] = *request.GuestLimit
		event["enableWaitlist"] = false
	}
	if request.Visibility != nil && *request.Visibility == "public" {
		event["isPublic"] = true
	}
	return client.complete(ctx, "createEvent", request.Credential, map[string]any{"event": event, "cohostIds": []string{}}, false)
}

func (client *Client) CancelEvent(ctx context.Context, request transport.CancelEventRequest) (transport.Completion, error) {
	if request.EventID == "" {
		return transport.Completion{}, invalid("cancelEvent")
	}
	message := ""
	if request.Message != nil {
		message = *request.Message
	}
	params := map[string]any{"eventId": string(request.EventID), "cancellationMessage": message, "shouldSkipNotifyGuests": request.SkipNotifyGuests}
	return client.complete(ctx, "cancelEvent", request.Credential, params, false)
}

func (client *Client) GetContacts(ctx context.Context, request transport.GetContactsRequest) (transport.GetContactsResult, error) {
	if client.amplitudeDeviceID == "" || request.MaxResults != 1000 {
		return transport.GetContactsResult{}, unsupported("getContacts")
	}
	paging := map[string]any{"maxResults": 1000, "cursor": nil}
	if request.Cursor != nil {
		paging["cursor"] = string(*request.Cursor)
	}
	body, failure := client.call(ctx, "getContacts", request.Credential, map[string]any{}, paging)
	if failure != nil {
		return transport.GetContactsResult{}, failure
	}
	var response struct {
		Result struct {
			Data []struct {
				ID               string `json:"id"`
				Name             string `json:"name"`
				SharedEventCount int    `json:"sharedEventCount"`
			} `json:"data"`
			Paging struct {
				NextCursor *string `json:"nextCursor"`
			} `json:"paging"`
		} `json:"result"`
	}
	if err := decodeStrict(body, &response); err != nil {
		return transport.GetContactsResult{}, protocolChanged("getContacts", "", transport.DispatchStarted)
	}
	result := transport.GetContactsResult{Contacts: make([]transport.Contact, len(response.Result.Data))}
	for index, value := range response.Result.Data {
		if value.ID == "" || value.Name == "" || value.SharedEventCount < 0 {
			return transport.GetContactsResult{}, protocolChanged("getContacts", "", transport.DispatchStarted)
		}
		result.Contacts[index] = transport.Contact{ContactID: transport.ContactID(value.ID), DisplayName: value.Name, SharedEventCount: value.SharedEventCount}
	}
	if response.Result.Paging.NextCursor != nil {
		cursor := transport.RemoteCursor(*response.Result.Paging.NextCursor)
		result.Cursor = &cursor
	}
	return result, nil
}

func (client *Client) GetGuests(ctx context.Context, request transport.GetGuestsRequest) (transport.GetGuestsResult, error) {
	if request.EventID == "" {
		return transport.GetGuestsResult{}, invalid("getGuests")
	}
	if client.amplitudeDeviceID == "" || !request.IncludeInvitedGuests || request.MaxResults != 500 {
		return transport.GetGuestsResult{}, unsupported("getGuests")
	}
	params := map[string]any{"eventId": string(request.EventID), "includeInvitedGuests": true}
	paging := map[string]any{"maxResults": 500, "cursor": nil}
	if request.Cursor != nil {
		paging["cursor"] = string(*request.Cursor)
	}
	body, failure := client.call(ctx, "getGuests", request.Credential, params, paging)
	if failure != nil {
		return transport.GetGuestsResult{}, failure
	}
	var response struct {
		Result struct {
			Data   []json.RawMessage `json:"data"`
			Paging struct {
				NextCursor *string `json:"nextCursor"`
			} `json:"paging"`
		} `json:"result"`
	}
	if err := decodeStrict(body, &response); err != nil {
		return transport.GetGuestsResult{}, protocolChanged("getGuests", "", transport.DispatchStarted)
	}
	result := transport.GetGuestsResult{Guests: make([]transport.Guest, len(response.Result.Data))}
	for index, raw := range response.Result.Data {
		var value struct {
			ID            string  `json:"id"`
			Name          string  `json:"name"`
			Status        string  `json:"status"`
			Count         int     `json:"count"`
			UserID        *string `json:"userId"`
			AnchorGuestID *string `json:"anchorGuestId"`
		}
		if json.Unmarshal(raw, &value) != nil {
			return transport.GetGuestsResult{}, protocolChanged("getGuests", "", transport.DispatchStarted)
		}
		if value.ID == "" || value.Name == "" || value.Count < 1 || !validGuestStatus(value.Status) {
			return transport.GetGuestsResult{}, protocolChanged("getGuests", "", transport.DispatchStarted)
		}
		status, size := value.Status, value.Count
		result.Guests[index] = transport.Guest{GuestID: transport.GuestID(value.ID), DisplayName: value.Name, RSVPStatus: &status, PartySize: &size}
	}
	if response.Result.Paging.NextCursor != nil {
		cursor := transport.RemoteCursor(*response.Result.Paging.NextCursor)
		result.Cursor = &cursor
	}
	return result, nil
}

func (client *Client) InviteGuest(ctx context.Context, request transport.InviteGuestRequest) (transport.Completion, error) {
	if request.EventID == "" || request.ContactID == "" {
		return transport.Completion{}, invalid("addInvitedGuestsAsHost")
	}
	params := map[string]any{"eventId": string(request.EventID), "userIdsToInvite": []string{string(request.ContactID)}, "invitationMessage": request.Message, "otherMutualsCount": 0, "phoneContactsToInvite": []any{}, "emailsToInvite": []any{}}
	return client.complete(ctx, "addInvitedGuestsAsHost", request.Credential, params, false)
}

func (client *Client) GetCurrentGuest(ctx context.Context, request transport.GetCurrentGuestRequest) (transport.GetCurrentGuestResult, error) {
	if request.EventID == "" {
		return transport.GetCurrentGuestResult{}, invalid("getCurrentGuest")
	}
	body, failure := client.call(ctx, "getCurrentGuest", request.Credential, map[string]any{"eventId": string(request.EventID)}, nil)
	if failure != nil {
		return transport.GetCurrentGuestResult{}, failure
	}
	var response struct {
		Result struct {
			Data struct {
				CurrentGuest json.RawMessage `json:"currentGuest"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := decodeStrict(body, &response); err != nil {
		return transport.GetCurrentGuestResult{}, protocolChanged("getCurrentGuest", "", transport.DispatchStarted)
	}
	if len(response.Result.Data.CurrentGuest) == 0 || string(response.Result.Data.CurrentGuest) == "null" {
		return transport.GetCurrentGuestResult{}, nil
	}
	var guest struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(response.Result.Data.CurrentGuest, &guest) != nil {
		return transport.GetCurrentGuestResult{}, protocolChanged("getCurrentGuest", "", transport.DispatchStarted)
	}
	if guest.ID == "" || !validGuestStatus(guest.Status) {
		return transport.GetCurrentGuestResult{}, protocolChanged("getCurrentGuest", "", transport.DispatchStarted)
	}
	return transport.GetCurrentGuestResult{Guest: &transport.CurrentGuest{GuestID: transport.GuestID(guest.ID), Status: guest.Status}}, nil
}

func (client *Client) SetGuest(ctx context.Context, request transport.SetGuestRequest) (transport.Completion, error) {
	if request.EventID == "" || request.DisplayName == "" || request.Timezone == "" || request.PartySize < 1 || len(request.PlusOnes) != request.PartySize-1 {
		return transport.Completion{}, invalid("addGuest")
	}
	if request.Status != "GOING" && request.Status != "DECLINED" {
		return transport.Completion{}, unsupported("addGuest")
	}
	rsvp := map[string]any{"name": request.DisplayName, "count": request.PartySize, "plusOnes": namedPlusOnes(request.PlusOnes), "status": request.Status, "timezone": request.Timezone, "shouldFollowOrgs": false}
	if request.GuestID != nil {
		rsvp["guestId"] = string(*request.GuestID)
	}
	if request.Message != nil {
		rsvp["message"] = *request.Message
	}
	if request.QuestionnaireVersion != nil {
		rsvp["questionnaireResponse"] = map[string]any{"questionnaireVersion": *request.QuestionnaireVersion, "answers": request.QuestionnaireAnswers}
	}
	return client.complete(ctx, "addGuest", request.Credential, map[string]any{"eventId": string(request.EventID), "rsvp": rsvp}, true)
}

func namedPlusOnes(names []string) []map[string]string {
	result := make([]map[string]string, len(names))
	for index, name := range names {
		result[index] = map[string]string{"name": name}
	}
	return result
}

func (client *Client) MarkInterest(ctx context.Context, request transport.MarkInterestRequest) (transport.Completion, error) {
	if request.EventID == "" {
		return transport.Completion{}, invalid("markEventInterest")
	}
	body, failure := client.call(ctx, "markEventInterest", request.Credential, map[string]any{"eventId": string(request.EventID), "interested": request.Interested}, nil)
	if failure != nil {
		return transport.Completion{}, failure
	}
	var response struct {
		Result struct {
			Data struct {
				Success    bool `json:"success"`
				Interested bool `json:"interested"`
			} `json:"data"`
		} `json:"result"`
	}
	if decodeStrict(body, &response) != nil || !response.Result.Data.Success || response.Result.Data.Interested != request.Interested {
		return transport.Completion{}, protocolChanged("markEventInterest", "", transport.DispatchStarted)
	}
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}

func (client *Client) InviteCohost(ctx context.Context, request transport.CohostRequest) (transport.Completion, error) {
	return client.cohost(ctx, "createCohostRequest", request)
}
func (client *Client) RevokeCohostInvite(ctx context.Context, request transport.CohostRequest) (transport.Completion, error) {
	return client.cohost(ctx, "deleteCohostRequest", request)
}
func (client *Client) RemoveCohost(ctx context.Context, request transport.CohostRequest) (transport.Completion, error) {
	return client.cohost(ctx, "removeCohost", request)
}
func (client *Client) cohost(ctx context.Context, operation string, request transport.CohostRequest) (transport.Completion, error) {
	if request.EventID == "" || request.ContactID == "" {
		return transport.Completion{}, invalid(operation)
	}
	return client.complete(ctx, operation, request.Credential, map[string]any{"eventId": string(request.EventID), "targetUserId": string(request.ContactID)}, false)
}
func (client *Client) CreateCohostLink(ctx context.Context, request transport.CohostLinkRequest) (transport.CohostLinkResult, error) {
	if request.EventID == "" {
		return transport.CohostLinkResult{}, invalid("generateEventCohostLink")
	}
	body, failure := client.call(ctx, "generateEventCohostLink", request.Credential, map[string]any{"eventId": string(request.EventID)}, nil)
	if failure != nil {
		return transport.CohostLinkResult{}, failure
	}
	var response struct {
		Result struct {
			Data struct {
				Path string `json:"path"`
			} `json:"data"`
		} `json:"result"`
	}
	if decodeStrict(body, &response) != nil || response.Result.Data.Path == "" {
		return transport.CohostLinkResult{}, protocolChanged("generateEventCohostLink", "", transport.DispatchStarted)
	}
	return transport.CohostLinkResult{URL: response.Result.Data.Path}, nil
}
func (client *Client) RevokeCohostLink(ctx context.Context, request transport.CohostLinkRequest) (transport.Completion, error) {
	if request.EventID == "" {
		return transport.Completion{}, invalid("revokeEventCohostLink")
	}
	return client.complete(ctx, "revokeEventCohostLink", request.Credential, map[string]any{"eventId": string(request.EventID)}, false)
}
func (client *Client) CreateTextBlast(ctx context.Context, request transport.CreateTextBlastRequest) (transport.Completion, error) {
	if request.EventID == "" || request.Message == "" || len([]rune(request.Message)) > 480 || len(request.Groups) == 0 {
		return transport.Completion{}, invalid("createTextBlast")
	}
	groups := make([]string, len(request.Groups))
	for index, group := range request.Groups {
		if group.Name == "" {
			return transport.Completion{}, invalid("createTextBlast")
		}
		groups[index] = group.Name
	}
	params := map[string]any{"eventId": string(request.EventID), "message": map[string]any{"text": request.Message, "to": groups, "showOnEventPage": request.ShowOnEventPage}}
	return client.complete(ctx, "createTextBlast", request.Credential, params, true)
}

func (client *Client) complete(ctx context.Context, operation string, credential transport.Credential, params map[string]any, nestedData bool) (transport.Completion, error) {
	body, failure := client.call(ctx, operation, credential, params, nil)
	if failure != nil {
		return transport.Completion{}, failure
	}
	if !validCompletion(body, nestedData) {
		return transport.Completion{}, protocolChanged(operation, "", transport.DispatchStarted)
	}
	return transport.Completion{DispatchState: transport.DispatchStarted}, nil
}

func validCompletion(body []byte, nestedData bool) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil || len(top) != 1 {
		return false
	}
	raw, ok := top["result"]
	if !ok {
		raw, ok = top["data"]
	}
	if !ok {
		return false
	}
	if !nestedData {
		return string(raw) != "null"
	}
	var result map[string]json.RawMessage
	return json.Unmarshal(raw, &result) == nil && len(result) >= 1 && result["data"] != nil && string(result["data"]) != "null"
}

func (client *Client) call(ctx context.Context, operation string, credential transport.Credential, params map[string]any, paging map[string]any) ([]byte, *transport.ProtocolFailure) {
	if !client.configured {
		return nil, protocolChanged(operation, "", transport.DispatchNotStarted)
	}
	if credential == "" {
		return nil, &transport.ProtocolFailure{Operation: operation, Class: "auth.required", DispatchState: transport.DispatchNotStarted}
	}
	data := map[string]any{"params": params}
	if client.amplitudeDeviceID != "" {
		data["amplitudeDeviceId"] = client.amplitudeDeviceID
	}
	if client.userID != "" {
		data["userId"] = client.userID
	}
	if paging != nil {
		data["paging"] = paging
	}
	payload, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return nil, protocolChanged(operation, "", transport.DispatchNotStarted)
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	endpoint := client.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(client.baseURL.Path, "/") + "/" + operation})
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, protocolChanged(operation, "", transport.DispatchNotStarted)
	}
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+string(credential))
	}
	response, err := client.httpClient.Do(req)
	if err != nil {
		return nil, &transport.ProtocolFailure{Operation: operation, Class: "remote.unavailable", Retryable: true, DispatchState: transport.DispatchStarted}
	}
	defer response.Body.Close()
	correlation := transport.CorrelationID(response.Header.Get("X-Request-ID"))
	body, err := readBounded(response.Body, client.maxResponseBytes)
	if err != nil {
		return nil, protocolChanged(operation, correlation, transport.DispatchStarted)
	}
	if response.StatusCode != http.StatusOK {
		return nil, &transport.ProtocolFailure{Operation: operation, Class: "contract.protocol_changed", DispatchState: transport.DispatchStarted, CorrelationID: correlation}
	}
	return body, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("response exceeds limit")
	}
	return body, nil
}
func decodeStrict(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func protocolChanged(operation string, correlation transport.CorrelationID, state transport.DispatchState) *transport.ProtocolFailure {
	return &transport.ProtocolFailure{Operation: operation, Class: "contract.protocol_changed", DispatchState: state, CorrelationID: correlation}
}
func validGuestStatus(status string) bool {
	switch status {
	case "READY_TO_SEND", "SENDING", "SEND_ERROR", "DELIVERY_ERROR", "SENT", "INTERESTED", "WAITLIST", "MAYBE", "DECLINED", "GOING", "PENDING_APPROVAL", "APPROVED", "WITHDRAWN", "WAITLISTED_FOR_APPROVAL", "REJECTED", "RESPONDED_TO_FIND_A_TIME":
		return true
	}
	return false
}
