package agentbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestClient(t *testing.T, baseURL string, opts ...Option) *Client {
	t.Helper()
	opts = append([]Option{WithAPIKey("test-key"), WithBaseURL(baseURL)}, opts...)
	c, err := NewClient(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRedirectSameOriginKeepsAuthorization(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestRedirectDifferentPortDropsAuthorization(t *testing.T) {
	var leaked atomic.Value
	leaked.Store("unset")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization") + "|" + r.Header.Get("Cookie"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	}))
	defer origin.Close()

	c := newTestClient(t, origin.URL)
	headers := http.Header{"Cookie": []string{"session=1"}}
	if err := c.request(context.Background(), "GET", "/start", nil, nil, headers, nil); err != nil {
		t.Fatal(err)
	}
	if v := leaked.Load(); v != "|" {
		t.Fatalf("credentials forwarded to other port: %q", v)
	}
}

func TestRedirectDifferentHostDropsAuthorization(t *testing.T) {
	var leaked atomic.Value
	leaked.Store("unset")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	// 127.0.0.1 -> localhost is a different host name on the same loopback.
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusFound)
	}))
	defer origin.Close()

	c := newTestClient(t, origin.URL)
	if err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if v := leaked.Load(); v != "" {
		t.Fatalf("Authorization forwarded to other host: %q", v)
	}
}

func mustReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Request{URL: u, Header: http.Header{"Authorization": {"Bearer k"}, "Cookie": {"a=b"}}}
}

func TestCheckRedirectOrigins(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		keep     bool
	}{
		{"same", "https://a.test/x", "https://a.test/y", true},
		{"default port explicit", "https://a.test/x", "https://A.TEST:443/y", true},
		{"http default port", "http://a.test/x", "http://a.test:80/y", true},
		{"other port", "http://a.test:8080/x", "http://a.test:8081/y", false},
		{"https to http", "https://a.test/x", "http://a.test/y", false},
		{"other host", "https://a.test/x", "https://b.test/y", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := mustReq(t, tc.from)
			next := mustReq(t, tc.to)
			if err := checkRedirect(next, []*http.Request{orig}); err != nil {
				t.Fatal(err)
			}
			if kept := next.Header.Get("Authorization") != ""; kept != tc.keep {
				t.Fatalf("Authorization kept = %v, want %v", kept, tc.keep)
			}
			if kept := next.Header.Get("Cookie") != ""; kept != tc.keep {
				t.Fatalf("Cookie kept = %v, want %v", kept, tc.keep)
			}
		})
	}
}

func TestCheckRedirectRejectsUnsupportedScheme(t *testing.T) {
	err := checkRedirect(mustReq(t, "file:///etc/passwd"), []*http.Request{mustReq(t, "https://a.test/")})
	if err == nil || !strings.Contains(err.Error(), "unsupported redirect scheme") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPSToHTTPRedirectDropsAuthorization(t *testing.T) {
	var leaked atomic.Value
	leaked.Store("unset")
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer plain.Close()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer tlsSrv.Close()

	c := newTestClient(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))
	if err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if v := leaked.Load(); v != "" {
		t.Fatalf("Authorization forwarded over https->http: %q", v)
	}
}

func TestTooManyRedirects(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		http.Redirect(w, r, fmt.Sprintf("/hop%d", n), http.StatusFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil)
	var te *TransportError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Fatalf("err = %#v", err)
	}
	if hits != 6 {
		t.Fatalf("hits = %d, want 6 (original + 5 redirects)", hits)
	}
}

func TestRedirectWithoutLocationIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 302 || apiErr.Kind != ErrorKindGeneric {
		t.Fatalf("request err = %#v", err)
	}

	_, err = c.stream(context.Background(), "/tasks/t/stream")
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 302 {
		t.Fatalf("stream err = %#v", err)
	}
}

func TestRedirectToUnsupportedSchemeIsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "file:///etc/passwd")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.request(context.Background(), "GET", "/start", nil, nil, nil, nil)
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %#v", err)
	}
}

func TestStreamRedirectDropsAuthorizationAcrossPorts(t *testing.T) {
	var leaked atomic.Value
	leaked.Store("unset")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	c := newTestClient(t, origin.URL)
	s, err := c.stream(context.Background(), "/tasks/t/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v := leaked.Load(); v != "" {
		t.Fatalf("Authorization forwarded by stream: %q", v)
	}
}

func TestCallerHTTPClientWithCheckRedirectUntouched(t *testing.T) {
	called := false
	custom := func(*http.Request, []*http.Request) error { called = true; return http.ErrUseLastResponse }
	hc := &http.Client{CheckRedirect: custom}
	c := newTestClient(t, "https://example.test", WithHTTPClient(hc))
	if c.httpClient != hc {
		t.Fatal("caller client with CheckRedirect must be used as is")
	}
	_ = c.httpClient.CheckRedirect(nil, nil)
	if !called {
		t.Fatal("caller CheckRedirect replaced")
	}
}

func TestCallerHTTPClientNotMutated(t *testing.T) {
	hc := &http.Client{}
	c := newTestClient(t, "https://example.test", WithHTTPClient(hc))
	if hc.CheckRedirect != nil {
		t.Fatal("caller client was mutated")
	}
	if c.httpClient == hc || c.httpClient.CheckRedirect == nil {
		t.Fatal("SDK policy not applied to a copy")
	}
}

func TestStreamReadFailureIsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + strings.Repeat("x", 2<<20) + "\n\n"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	s, err := c.stream(context.Background(), "/tasks/t/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.Next(); ok {
		t.Fatal("expected no event")
	}
	var te *TransportError
	if !errors.As(s.Err(), &te) || !errors.Is(s.Err(), bufio.ErrTooLong) {
		t.Fatalf("Err = %#v", s.Err())
	}
}

func TestErrorKindsForExtraStatuses(t *testing.T) {
	cases := []struct {
		status int
		kind   ErrorKind
		is     func(error) bool
	}{
		{413, ErrorKindRequestTooLarge, IsRequestTooLargeError},
		{415, ErrorKindUnsupportedMediaType, IsUnsupportedMediaTypeError},
		{416, ErrorKindRangeNotSatisfiable, IsRangeNotSatisfiableError},
		{499, ErrorKindRequestCanceled, IsRequestCanceledError},
	}
	for _, tc := range cases {
		err := errorFromResponse(tc.status, map[string]any{"error": "x"})
		if !tc.is(err) || err.(*APIError).Kind != tc.kind {
			t.Fatalf("status %d: %#v", tc.status, err)
		}
	}
	err := errorFromResponse(507, "full")
	if !IsInsufficientStorageError(err) || !IsServerError(err) {
		t.Fatalf("507: %#v", err)
	}
	if IsInsufficientStorageError(errorFromResponse(500, "x")) {
		t.Fatal("500 must not be insufficient storage")
	}
}

func TestAPIKeyWithLineBreakRejected(t *testing.T) {
	for _, key := range []string{"a\nb", "a\rb"} {
		if _, err := NewClient(WithAPIKey(key)); err == nil || !strings.Contains(err.Error(), "line breaks") {
			t.Fatalf("key %q: err = %v", key, err)
		}
	}
	// Surrounding whitespace is trimmed before the check.
	if _, err := NewClient(WithAPIKey("abc\n")); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRefreshAndUpdateKeepGeneratedAPIKey(t *testing.T) {
	body := `{"id":"dep-1","slug":"demo","revision":2}`
	transport := func(_ context.Context, method, rawURL string, _ http.Header, _ []byte) (*RawResponse, error) {
		return &RawResponse{StatusCode: 200, Body: []byte(body)}, nil
	}
	c := newTestClient(t, "https://example.test", WithTransport(transport))
	agent := &Agent{client: c, Data: map[string]any{"slug": "demo", "generated_api_key": "secret"}}

	if err := agent.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if agent.GeneratedAPIKey() != "secret" || agent.Revision() != 2 {
		t.Fatalf("after Refresh: %#v", agent.Data)
	}
	if err := agent.Update(context.Background(), AgentUpdateFields{"description": "x"}); err != nil {
		t.Fatal(err)
	}
	if agent.GeneratedAPIKey() != "secret" {
		t.Fatalf("after Update: %#v", agent.Data)
	}

	body = `{"slug":"demo","generated_api_key":""}`
	if err := agent.Refresh(context.Background()); err != nil || agent.GeneratedAPIKey() != "secret" {
		t.Fatalf("empty new value must not win: %v %#v", err, agent.Data)
	}

	body = `{"slug":"demo","generated_api_key":"fresh"}`
	if err := agent.Update(context.Background(), AgentUpdateFields{}); err != nil || agent.GeneratedAPIKey() != "fresh" {
		t.Fatalf("non-empty new value must win: %v %#v", err, agent.Data)
	}
}
