package agentbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDefaultAPIRoot(t *testing.T) {
	t.Setenv("GMI_AGENTBOX_BASE_URL", "")

	var got string
	transport := func(_ context.Context, _ string, rawURL string, _ http.Header, _ []byte) (*RawResponse, error) {
		got = rawURL
		return &RawResponse{StatusCode: 200, Body: []byte(`{"items":[],"total":0,"page":1,"page_size":20}`)}, nil
	}
	client, err := NewClient(WithAPIKey("test-key"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL != "https://api.gmi-serving.com/v1/agents" {
		t.Fatalf("base url = %s", client.BaseURL)
	}
	if _, err := client.Agents.List(context.Background(), 1, 20); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "api.gmi-serving.com" || parsed.Path != "/v1/agents/deployments" {
		t.Fatalf("request url = %s", got)
	}
	if strings.Contains(got, "/api/v1/ie/container") {
		t.Fatalf("legacy prefix still present: %s", got)
	}

	tot, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://ce-tot.gmicloud-dev.com/api/v1/ie/container"),
		WithTransport(transport),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tot.Agents.List(context.Background(), 1, 20); err != nil {
		t.Fatal(err)
	}
	parsed, err = url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/v1/ie/container/deployments" {
		t.Fatalf("tot url = %s", got)
	}
}

func TestErrorHelpersUnwrapWrappedAPIError(t *testing.T) {
	apiErr := errorFromResponse(507, map[string]any{"error": "full"})
	wrapped := fmt.Errorf("x: %w", apiErr)
	if !IsServerError(wrapped) || !IsInsufficientStorageError(wrapped) {
		t.Fatal("wrapped 507 APIError not detected")
	}
	if IsNotFoundError(wrapped) {
		t.Fatal("unexpected match")
	}
}

func TestPathIDsAreEscapedOnTheWire(t *testing.T) {
	var got atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.RequestURI)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	if _, err := c.Sandboxes.GetExecution(context.Background(), "task 1", "x?y#z%"); err != nil {
		t.Fatal(err)
	}
	if v := got.Load(); v != "/tasks/task%201/executions/x%3Fy%23z%25" {
		t.Fatalf("request URI = %v", v)
	}
}

func TestInvalidPathIDsRejectedBeforeRequest(t *testing.T) {
	c := newTestClient(t, "https://example.test",
		WithTransport(func(_ context.Context, method, rawURL string, _ http.Header, _ []byte) (*RawResponse, error) {
			t.Fatalf("unexpected request %s %s", method, rawURL)
			return nil, nil
		}),
		WithStreamTransport(func(_ context.Context, method, rawURL string, _ http.Header, _ []byte) (*RawStream, error) {
			t.Fatalf("unexpected stream %s %s", method, rawURL)
			return nil, nil
		}),
		WithShellDialer(func(_ context.Context, wsURL string, _ http.Header) (ShellConn, error) {
			t.Fatalf("unexpected shell dial %s", wsURL)
			return nil, nil
		}),
	)
	ctx := context.Background()
	for _, id := range []string{"", ".", "..", "a/b", "../deployments/demo-agent"} {
		calls := map[string]func() error{
			"Sandboxes.Get":          func() error { _, err := c.Sandboxes.Get(ctx, id); return err },
			"Sandboxes.Delete":       func() error { return c.Sandboxes.Delete(ctx, id) },
			"Sandboxes.Launch":       func() error { _, err := c.Sandboxes.Launch(ctx, id, LaunchParams{IdcName: "idc-1"}); return err },
			"Sandboxes.GetExecution": func() error { _, err := c.Sandboxes.GetExecution(ctx, "task-1", id); return err },
			"Sandboxes.Stream":       func() error { _, err := c.Sandboxes.Stream(ctx, id); return err },
			"Sandboxes.Shell":        func() error { _, err := c.Sandboxes.Shell(ctx, id); return err },
			"Agents.Delete":          func() error { return c.Agents.Delete(ctx, id) },
			"Agents.BuildLogs":       func() error { _, err := c.Agents.BuildLogs(ctx, "demo-agent", id, BuildLogParams{}); return err },
			"Snapshots.Delete":       func() error { _, err := c.Snapshots.Delete(ctx, id); return err },
		}
		for name, call := range calls {
			if err := call(); err == nil || !strings.Contains(err.Error(), "invalid ID") {
				t.Fatalf("%s(%q): err = %v", name, id, err)
			}
		}
	}
	if err := (&Sandbox{client: c}).Delete(ctx); err == nil {
		t.Fatal("deleted a Sandbox that has no ID")
	}
}
