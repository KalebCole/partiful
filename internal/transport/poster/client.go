package poster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KalebCole/partiful/internal/transport"
)

const (
	defaultBaseURL          = "https://assets.getpartiful.com"
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

func (client *Client) GetCatalog(ctx context.Context, _ transport.GetPosterCatalogRequest) (transport.GetPosterCatalogResult, error) {
	if !client.configured {
		return transport.GetPosterCatalogResult{}, failure(transport.DispatchNotStarted, "contract.protocol_changed", false)
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	endpoint := client.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(client.baseURL.Path, "/") + "/posters.json"})
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return transport.GetPosterCatalogResult{}, failure(transport.DispatchNotStarted, "contract.protocol_changed", false)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return transport.GetPosterCatalogResult{}, failure(transport.DispatchStarted, "remote.unavailable", true)
	}
	defer response.Body.Close()
	body, err := readBounded(response.Body, client.maxResponseBytes)
	if err != nil || response.StatusCode != http.StatusOK {
		return transport.GetPosterCatalogResult{}, failure(transport.DispatchStarted, "contract.protocol_changed", false)
	}
	var records []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		URL         string   `json:"url"`
		BlurHash    *string  `json:"blurHash,omitempty"`
		ContentType string   `json:"contentType"`
		Width       *int     `json:"width"`
		Height      *int     `json:"height"`
		Tags        []string `json:"tags"`
		Categories  []string `json:"categories"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&records) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return transport.GetPosterCatalogResult{}, failure(transport.DispatchStarted, "contract.protocol_changed", false)
	}
	result := transport.GetPosterCatalogResult{Posters: make([]transport.Poster, len(records))}
	for index, record := range records {
		parsedURL, parseErr := url.Parse(record.URL)
		if record.ID == "" || record.Name == "" || parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || !validContentType(record.ContentType) || record.Width == nil || record.Height == nil || *record.Width < 0 || *record.Height < 0 || record.Tags == nil || record.Categories == nil {
			return transport.GetPosterCatalogResult{}, failure(transport.DispatchStarted, "contract.protocol_changed", false)
		}
		result.Posters[index] = transport.Poster{PosterID: transport.PosterID(record.ID), Name: record.Name, URL: record.URL, ContentType: record.ContentType, Width: *record.Width, Height: *record.Height, Tags: record.Tags, Categories: record.Categories}
	}
	return result, nil
}

func failure(state transport.DispatchState, class string, retryable bool) error {
	return &transport.ProtocolFailure{Operation: "getPosterCatalog", Class: class, Retryable: retryable, DispatchState: state}
}
func validContentType(value string) bool {
	switch value {
	case "image/avif", "image/gif", "image/jpeg", "image/png":
		return true
	}
	return false
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
