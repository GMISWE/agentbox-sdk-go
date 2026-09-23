// Package agentbox is a thin Go client for the GMI AgentBox API.
//
// Public types are Agent (a registered template) and Sandbox (a launched
// container). Registering an Agent does not start anything.
package agentbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production AgentBox service origin.
const DefaultBaseURL = "https://console.gmicloud.ai"

// DefaultWaitTimeout is the default timeout for Sandbox.WaitUntilRunning.
const DefaultWaitTimeout = 300 * time.Second

// Version is the SDK version.
const Version = "0.1.0"

const servicePath = "/api/v1/ie/container"

const (
	runningStatus = "running"
)

var waitFailureStatuses = map[string]bool{
	"error":    true,
	"deleted":  true,
	"stopped":  true,
	"stopping": true,
}

// Client is the AgentBox API client.
type Client struct {
	APIKey  string
	BaseURL string

	serviceURL string
	timeout    time.Duration

	httpClient      *http.Client
	transport       Transport
	streamTransport StreamTransport
	shellDialer     ShellDialer
	sleep           func(time.Duration)

	Agents    *AgentCollection
	Sandboxes *SandboxCollection
	Idcs      *IdcCollection
	Products  *ProductCollection
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key explicitly, overriding GMI_AGENTBOX_API_KEY.
func WithAPIKey(apiKey string) Option {
	return func(c *Client) { c.APIKey = apiKey }
}

// WithBaseURL sets the service origin explicitly, overriding
// GMI_AGENTBOX_BASE_URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.BaseURL = baseURL }
}

// WithTimeout sets the per-request HTTP timeout. Default is 30s.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.timeout = timeout }
}

// WithHTTPClient overrides the *http.Client used by the default transport.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// WithTransport overrides the request/response transport (for testing).
func WithTransport(transport Transport) Option {
	return func(c *Client) { c.transport = transport }
}

// WithStreamTransport overrides the streaming transport (for testing).
func WithStreamTransport(transport StreamTransport) Option {
	return func(c *Client) { c.streamTransport = transport }
}

// WithShellDialer overrides the WebSocket shell dialer (for testing).
func WithShellDialer(dialer ShellDialer) Option {
	return func(c *Client) { c.shellDialer = dialer }
}

// WithSleep overrides the sleep function used by WaitUntilRunning polling
// (for testing).
func WithSleep(sleep func(time.Duration)) Option {
	return func(c *Client) { c.sleep = sleep }
}

func defaultAPIKey() string { return os.Getenv("GMI_AGENTBOX_API_KEY") }

func defaultBaseURL() string {
	if v := os.Getenv("GMI_AGENTBOX_BASE_URL"); v != "" {
		return v
	}
	return DefaultBaseURL
}

// NewClient constructs an AgentBox client. The API key defaults to
// GMI_AGENTBOX_API_KEY and is required (via option or env var). The base URL
// defaults to GMI_AGENTBOX_BASE_URL or DefaultBaseURL.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		APIKey:  defaultAPIKey(),
		BaseURL: defaultBaseURL(),
		timeout: 30 * time.Second,
		sleep:   time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}

	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		return nil, fmt.Errorf("agentbox: GMI_AGENTBOX_API_KEY is required (or use WithAPIKey)")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.serviceURL = c.BaseURL + servicePath

	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout}
	}
	if c.transport == nil {
		c.transport = defaultTransport(c.httpClient)
	}
	if c.streamTransport == nil {
		c.streamTransport = defaultStreamTransport(&http.Client{})
	}
	if c.shellDialer == nil {
		c.shellDialer = defaultShellDialer
	}

	c.Agents = &AgentCollection{client: c}
	c.Sandboxes = &SandboxCollection{client: c}
	c.Idcs = &IdcCollection{client: c}
	c.Products = &ProductCollection{client: c}

	return c, nil
}

func joinURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func queryURL(rawURL string, params map[string]any) string {
	values := url.Values{}
	for key, value := range params {
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			if v == "" {
				continue
			}
			values.Add(key, v)
		case []string:
			for _, item := range v {
				values.Add(key, item)
			}
		case int:
			values.Add(key, strconv.Itoa(v))
		case bool:
			values.Add(key, strconv.FormatBool(v))
		default:
			values.Add(key, fmt.Sprintf("%v", v))
		}
	}
	if len(values) == 0 {
		return rawURL
	}
	return rawURL + "?" + values.Encode()
}

func jsonLoads(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	return parsed
}

// request performs an authenticated JSON round trip and decodes the response
// body into result (if non-nil).
func (c *Client) request(ctx context.Context, method, path string, params map[string]any, body any, headers http.Header, result any) error {
	resp, err := c.requestRaw(ctx, method, path, params, body, headers)
	if err != nil {
		return err
	}
	if result == nil || len(resp.Body) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Body, result)
}

func (c *Client) requestRaw(ctx context.Context, method, path string, params map[string]any, body any, extraHeaders http.Header) (*RawResponse, error) {
	fullURL := joinURL(c.serviceURL, path)

	requestHeaders := c.authHeaders(nil)
	requestHeaders.Set("Accept", "application/json")

	if len(params) > 0 {
		fullURL = queryURL(fullURL, params)
	}

	var encodedBody []byte
	switch v := body.(type) {
	case nil:
		encodedBody = nil
	case []byte:
		encodedBody = v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("agentbox: encode request body: %w", err)
		}
		encodedBody = encoded
		if requestHeaders.Get("Content-Type") == "" {
			requestHeaders.Set("Content-Type", "application/json")
		}
	}
	for key, values := range extraHeaders {
		for _, value := range values {
			requestHeaders.Set(key, value)
		}
	}

	resp, err := c.transport(ctx, method, fullURL, requestHeaders, encodedBody)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		payload := jsonLoads(resp.Body)
		if payload != nil {
			return nil, errorFromResponse(resp.StatusCode, payload)
		}
		return nil, errorFromResponse(resp.StatusCode, string(resp.Body))
	}
	return resp, nil
}

func (c *Client) authHeaders(extra http.Header) http.Header {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	if c.APIKey != "" {
		headers.Set("Authorization", "Bearer "+c.APIKey)
	}
	for key, values := range extra {
		for _, value := range values {
			headers.Set(key, value)
		}
	}
	return headers
}

func (c *Client) stream(ctx context.Context, path string) (*Stream, error) {
	fullURL := joinURL(c.serviceURL, path)
	headers := c.authHeaders(nil)
	headers.Set("Accept", "text/event-stream")

	raw, err := c.streamTransport(ctx, "GET", fullURL, headers, nil)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		defer raw.Close()
		var body []byte
		for raw.Lines.Scan() {
			body = append(body, raw.Lines.Bytes()...)
		}
		payload := jsonLoads(body)
		if payload != nil {
			return nil, errorFromResponse(raw.StatusCode, payload)
		}
		return nil, errorFromResponse(raw.StatusCode, string(body))
	}
	return &Stream{raw: raw}, nil
}

// Health returns local client configuration.
func (c *Client) Health() map[string]any {
	return map[string]any{
		"status":        "ok",
		"baseUrl":       c.BaseURL,
		"authenticated": c.APIKey != "",
	}
}

// Eligibility describes the organization's available runtimes and data
// centers, from GET /eligibility.
type Eligibility struct {
	Eligible    bool
	DataCenters []string
	Data        map[string]any
}

// Eligibility returns the organization's container-launch entitlement.
func (c *Client) Eligibility(ctx context.Context) (*Eligibility, error) {
	var payload map[string]any
	if err := c.request(ctx, http.MethodGet, "/eligibility", nil, nil, nil, &payload); err != nil {
		return nil, err
	}

	eligible, _ := payload["eligible"].(bool)
	var dataCenters []string
	if v, ok := payload["dataCenters"].([]any); ok {
		dataCenters = toStringSlice(v)
	} else if v, ok := payload["data_centers"].([]any); ok {
		dataCenters = toStringSlice(v)
	}

	return &Eligibility{
		Eligible:    eligible,
		DataCenters: dataCenters,
		Data:        payload,
	}, nil
}

func toStringSlice(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func compact(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		if value == nil {
			continue
		}
		out[key] = value
	}
	return out
}
