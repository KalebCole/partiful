package firestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/transport"
)

const (
	defaultBaseURL          = "https://firestore.googleapis.com"
	defaultTimeout          = 15 * time.Second
	defaultMaxResponseBytes = int64(2 << 20)
)

type Config struct {
	HTTPClient       http.RoundTripper
	BaseURL          string
	Timeout          time.Duration
	MaxResponseBytes int64
}

type Client struct {
	httpClient       *http.Client
	baseURL          *url.URL
	timeout          time.Duration
	maxResponseBytes int64
	configured       bool
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
	httpClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if config.HTTPClient != nil {
		httpClient.Transport = config.HTTPClient
	}
	return &Client{httpClient: httpClient, baseURL: parsed, timeout: timeout, maxResponseBytes: limit, configured: configured}
}

func blocked(operation string) error {
	return &transport.ProtocolFailure{Operation: operation, Class: "evidence.required", DispatchState: transport.DispatchNotStarted}
}
func protocolChanged(operation string, state transport.DispatchState) error {
	return &transport.ProtocolFailure{Operation: operation, Class: "contract.protocol_changed", DispatchState: state}
}

func (client *Client) GetEvent(context.Context, transport.GetEventDocumentRequest) (transport.EventDocument, error) {
	return transport.EventDocument{}, blocked("firestoreGetEvent")
}
func (client *Client) ListEventGuests(context.Context, transport.ListEventDocumentsRequest) (transport.GuestDocumentPage, error) {
	return transport.GuestDocumentPage{}, blocked("firestoreListEventGuests")
}
func (client *Client) ListEventHostMessages(context.Context, transport.ListEventDocumentsRequest) (transport.HostMessageDocumentPage, error) {
	return transport.HostMessageDocumentPage{}, blocked("firestoreListEventHostMessages")
}

func (client *Client) PatchEvent(ctx context.Context, request transport.PatchEventDocumentRequest) (transport.EventDocument, error) {
	if request.EventID == "" || len(request.FieldMask) == 0 || len(request.Fields) == 0 {
		return transport.EventDocument{}, blocked("firestorePatchEvent")
	}
	mask := append([]string(nil), request.FieldMask...)
	sort.Strings(mask)
	if !sameFieldSet(mask, request.Fields) {
		return transport.EventDocument{}, blocked("firestorePatchEvent")
	}
	wireFields := make(map[string]any, len(request.Fields))
	for name, value := range request.Fields {
		encoded, ok := encodeValue(value)
		if !ok || !validFieldPath(name) {
			return transport.EventDocument{}, blocked("firestorePatchEvent")
		}
		wireFields[name] = encoded
	}
	payload, _ := json.Marshal(map[string]any{"fields": wireFields})
	query := url.Values{}
	for _, field := range mask {
		query.Add("updateMask.fieldPaths", field)
	}
	if request.MustExist {
		query.Set("currentDocument.exists", "true")
	}
	body, err := client.request(ctx, "firestorePatchEvent", http.MethodPatch, eventPath(request.EventID), query, request.Credential, payload)
	if err != nil {
		return transport.EventDocument{}, err
	}
	return decodeEventDocument(body, request.EventID, "firestorePatchEvent")
}

func (client *Client) GetGuest(ctx context.Context, request transport.GetGuestDocumentRequest) (transport.GuestDocument, error) {
	if request.EventID == "" || request.GuestID == "" {
		return transport.GuestDocument{}, blocked("firestoreGetGuest")
	}
	wirePath := eventPath(request.EventID) + "/guests/" + url.PathEscape(string(request.GuestID))
	body, err := client.request(ctx, "firestoreGetGuest", http.MethodGet, wirePath, nil, request.Credential, nil)
	if err != nil {
		return transport.GuestDocument{}, err
	}
	var document rawDocument
	if decodeStrict(body, &document) != nil || document.Name == "" || !strings.HasSuffix(document.Name, "/events/"+string(request.EventID)+"/guests/"+string(request.GuestID)) {
		return transport.GuestDocument{}, protocolChanged("firestoreGetGuest", transport.DispatchStarted)
	}
	fields, ok := decodeFields(document.Fields)
	if !ok {
		return transport.GuestDocument{}, protocolChanged("firestoreGetGuest", transport.DispatchStarted)
	}
	result := transport.GuestDocument{GuestID: request.GuestID}
	if value, exists := fields["status"]; exists {
		if value.String == nil || !validGuestStatus(*value.String) {
			return transport.GuestDocument{}, protocolChanged("firestoreGetGuest", transport.DispatchStarted)
		}
		result.Status = value.String
	}
	if value, exists := fields["checkIn"]; exists {
		if value.String == nil {
			return transport.GuestDocument{}, protocolChanged("firestoreGetGuest", transport.DispatchStarted)
		}
		result.CheckIn = value.String
	}
	return result, nil
}

func eventPath(eventID transport.EventID) string {
	return "/v1/projects/getpartiful/databases/(default)/documents/events/" + url.PathEscape(string(eventID))
}

func (client *Client) request(ctx context.Context, operation, method, requestPath string, query url.Values, credential transport.Credential, payload []byte) ([]byte, error) {
	if !client.configured {
		return nil, protocolChanged(operation, transport.DispatchNotStarted)
	}
	if credential == "" {
		return nil, &transport.ProtocolFailure{Operation: operation, Class: "auth.required", DispatchState: transport.DispatchNotStarted}
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	endpoint := *client.baseURL
	endpoint.Path = strings.TrimRight(client.baseURL.Path, "/") + requestPath
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(requestCtx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, protocolChanged(operation, transport.DispatchNotStarted)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+string(credential))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(req)
	if err != nil {
		return nil, &transport.ProtocolFailure{Operation: operation, Class: "remote.unavailable", Retryable: true, DispatchState: transport.DispatchStarted}
	}
	defer response.Body.Close()
	body, readErr := readBounded(response.Body, client.maxResponseBytes)
	if readErr != nil {
		return nil, protocolChanged(operation, transport.DispatchStarted)
	}
	if response.StatusCode != http.StatusOK {
		return nil, protocolChanged(operation, transport.DispatchStarted)
	}
	return body, nil
}

type rawDocument struct {
	Name       string                     `json:"name"`
	Fields     map[string]json.RawMessage `json:"fields"`
	CreateTime *string                    `json:"createTime,omitempty"`
	UpdateTime *string                    `json:"updateTime,omitempty"`
}

func decodeEventDocument(body []byte, expected transport.EventID, operation string) (transport.EventDocument, error) {
	var document rawDocument
	if decodeStrict(body, &document) != nil || !strings.HasSuffix(document.Name, "/events/"+string(expected)) {
		return transport.EventDocument{}, protocolChanged(operation, transport.DispatchStarted)
	}
	fields, ok := decodeFields(document.Fields)
	if !ok {
		return transport.EventDocument{}, protocolChanged(operation, transport.DispatchStarted)
	}
	return transport.EventDocument{EventID: expected, Fields: fields}, nil
}

func encodeValue(value transport.FieldValue) (map[string]any, bool) {
	count := 0
	var result map[string]any
	if value.String != nil {
		count++
		result = map[string]any{"stringValue": *value.String}
	}
	if value.Integer != nil {
		count++
		result = map[string]any{"integerValue": strconv.FormatInt(*value.Integer, 10)}
	}
	if value.Boolean != nil {
		count++
		result = map[string]any{"booleanValue": *value.Boolean}
	}
	if value.Time != nil {
		count++
		result = map[string]any{"timestampValue": value.Time.UTC().Format(time.RFC3339Nano)}
	}
	if value.Strings != nil {
		count++
		values := make([]map[string]any, len(value.Strings))
		for index, item := range value.Strings {
			values[index] = map[string]any{"stringValue": item}
		}
		result = map[string]any{"arrayValue": map[string]any{"values": values}}
	}
	return result, count == 1
}

func decodeFields(fields map[string]json.RawMessage) (map[string]transport.FieldValue, bool) {
	result := make(map[string]transport.FieldValue, len(fields))
	for name, raw := range fields {
		value, ok := decodeValue(raw)
		if !ok {
			return nil, false
		}
		result[name] = value
	}
	return result, true
}

func decodeValue(raw json.RawMessage) (transport.FieldValue, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != 1 {
		return transport.FieldValue{}, false
	}
	for kind, encoded := range object {
		switch kind {
		case "stringValue":
			var value string
			if json.Unmarshal(encoded, &value) != nil {
				return transport.FieldValue{}, false
			}
			return transport.FieldValue{String: &value}, true
		case "integerValue":
			var text string
			if json.Unmarshal(encoded, &text) != nil {
				return transport.FieldValue{}, false
			}
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return transport.FieldValue{}, false
			}
			return transport.FieldValue{Integer: &value}, true
		case "booleanValue":
			var value bool
			if json.Unmarshal(encoded, &value) != nil {
				return transport.FieldValue{}, false
			}
			return transport.FieldValue{Boolean: &value}, true
		case "timestampValue":
			var text string
			if json.Unmarshal(encoded, &text) != nil {
				return transport.FieldValue{}, false
			}
			value, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return transport.FieldValue{}, false
			}
			return transport.FieldValue{Time: &value}, true
		case "arrayValue":
			var array struct {
				Values []json.RawMessage `json:"values"`
			}
			if decodeStrict(encoded, &array) != nil {
				return transport.FieldValue{}, false
			}
			values := make([]string, len(array.Values))
			for index, item := range array.Values {
				decoded, ok := decodeValue(item)
				if !ok || decoded.String == nil {
					return transport.FieldValue{}, false
				}
				values[index] = *decoded.String
			}
			return transport.FieldValue{Strings: values}, true
		default:
			return transport.FieldValue{}, false
		}
	}
	return transport.FieldValue{}, false
}

func sameFieldSet(mask []string, fields map[string]transport.FieldValue) bool {
	if len(mask) != len(fields) {
		return false
	}
	for index, name := range mask {
		if index > 0 && mask[index-1] == name {
			return false
		}
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}
func validFieldPath(value string) bool {
	return value != "" && path.Clean(value) == value && !strings.ContainsAny(value, "/[]*~")
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
		return errors.New("trailing JSON")
	}
	return nil
}
func validGuestStatus(status string) bool {
	switch status {
	case "READY_TO_SEND", "SENDING", "SEND_ERROR", "DELIVERY_ERROR", "SENT", "INTERESTED", "WAITLIST", "MAYBE", "DECLINED", "GOING", "PENDING_APPROVAL", "APPROVED", "WITHDRAWN", "WAITLISTED_FOR_APPROVAL", "REJECTED", "RESPONDED_TO_FIND_A_TIME":
		return true
	}
	return false
}
