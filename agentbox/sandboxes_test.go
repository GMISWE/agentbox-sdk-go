package agentbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUploadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/task-1/files" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "/tmp/input.txt" {
			t.Errorf("path = %q", got)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if header.Filename != "input.txt" || string(content) != "hello\n" {
			t.Errorf("file = %q %q", header.Filename, content)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"path":"/tmp/input.txt"}]`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	files, err := client.Sandboxes.UploadFile(context.Background(), "task-1", "/tmp/input.txt", "input.txt", strings.NewReader("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || stringField(files[0], "path") != "/tmp/input.txt" {
		t.Fatalf("files = %#v", files)
	}
}

func TestUploadFileUsesCustomTransport(t *testing.T) {
	called := false
	transport := func(_ context.Context, method, rawURL string, headers http.Header, body []byte) (*RawResponse, error) {
		called = true
		if method != http.MethodPost || !hasSuffix(rawURL, "/tasks/task-1/files") {
			t.Fatalf("request = %s %s", method, rawURL)
		}
		if headers.Get("Content-Type") == "" || !bytes.Contains(body, []byte("hello\n")) {
			t.Fatalf("content type = %q, body = %q", headers.Get("Content-Type"), body)
		}
		return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`[]`)}, nil
	}
	client := newTestClient(t, "https://example.test", WithTransport(transport))

	if _, err := client.Sandboxes.UploadFile(context.Background(), "task-1", "/tmp/input.txt", "input.txt", strings.NewReader("hello\n")); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("custom transport was not called")
	}
}

func TestUploadFileRedirectIsNotReplayed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			t.Error("upload was replayed after redirect")
			return
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.Sandboxes.UploadFile(context.Background(), "task-1", "/tmp/input.txt", "input.txt", strings.NewReader("hello\n"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadFileCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Sandboxes.UploadFile(ctx, "task-1", "/tmp/input.txt", "input.txt", reader)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		_ = writer.Close()
		t.Fatal("upload request did not start while the input was still open")
	}
	cancel()
	select {
	case err := <-done:
		_ = writer.Close()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		_ = writer.Close()
		<-done
		t.Fatal("UploadFile did not return after context cancellation")
	}
}

func TestLaunchMetadataAndTimeout(t *testing.T) {
	var calls []struct {
		method string
		url    string
		body   []byte
	}
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, struct {
			method string
			url    string
			body   []byte
		}{method, rawURL, append([]byte(nil), body...)})
		switch {
		case method == http.MethodPost && hasSuffix(rawURL, "/deployments/demo-agent/tasks"):
			return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"task_id":"task-1","container_id":"ctr-1","status":"pending"}`)}, nil
		case method == http.MethodPost && hasSuffix(rawURL, "/tasks/task-1/timeout"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"task_id":"task-1","expires_at":"2026-09-30T02:00:00Z","expiry_pinned":true}`)}, nil
		case method == http.MethodGet && hasSuffix(rawURL, "/tasks"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"items":[],"total":0,"page":1,"page_size":20}`)}, nil
		case method == http.MethodGet && hasSuffix(rawURL, "/deployments/demo-agent/tasks"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"items":[],"total":0,"page":1,"page_size":20}`)}, nil
		case method == http.MethodGet && hasSuffix(rawURL, "/deployments/demo-agent"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"dep-1","slug":"demo-agent","idc":"idc-us-1"}`)}, nil
		default:
			t.Fatalf("unexpected %s %s", method, rawURL)
			return nil, nil
		}
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}

	timeoutSeconds := 3600
	sandbox, err := client.Sandboxes.Launch(context.Background(), "demo-agent", LaunchParams{
		InstanceType:   "gmi.sandbox.x-small",
		IdcName:        "idc-us-1",
		Metadata:       map[string]string{"session": "abc", "owner": "qa"},
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
	var launchBody map[string]any
	if err := json.Unmarshal(calls[0].body, &launchBody); err != nil {
		t.Fatal(err)
	}
	metadata, _ := launchBody["metadata"].(map[string]any)
	if metadata["session"] != "abc" || metadata["owner"] != "qa" {
		t.Fatalf("metadata = %#v", launchBody["metadata"])
	}
	if launchBody["timeout_seconds"] != float64(3600) {
		t.Fatalf("timeout_seconds = %#v", launchBody["timeout_seconds"])
	}
	if _, ok := launchBody["expires_at"]; ok {
		t.Fatal("launch body included expires_at")
	}
	if sandbox.Metadata()["session"] != "abc" || !sandbox.ExpiryPinned() || sandbox.ExpiresAt() != "" {
		t.Fatalf("sandbox metadata=%v pinned=%v expires=%q", sandbox.Metadata(), sandbox.ExpiryPinned(), sandbox.ExpiresAt())
	}

	if err := sandbox.SetTimeout(context.Background(), 7200); err != nil {
		t.Fatal(err)
	}
	var timeoutBody map[string]any
	if err := json.Unmarshal(calls[1].body, &timeoutBody); err != nil {
		t.Fatal(err)
	}
	if timeoutBody["timeout_seconds"] != float64(7200) {
		t.Fatalf("set timeout body = %#v", timeoutBody)
	}
	if calls[1].method != http.MethodPost || !hasSuffix(calls[1].url, "/tasks/task-1/timeout") {
		t.Fatalf("set timeout request = %s %s", calls[1].method, calls[1].url)
	}
	if sandbox.ExpiresAt() != "2026-09-30T02:00:00Z" || !sandbox.ExpiryPinned() {
		t.Fatalf("updated expiry = %q pinned=%v", sandbox.ExpiresAt(), sandbox.ExpiryPinned())
	}

	if _, err := client.Sandboxes.List(context.Background(), SandboxListParams{
		Metadata: map[string]string{"session": "abc", "owner": "qa"},
	}); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(calls[2].url)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("metadata[session]") != "abc" || parsed.Query().Get("metadata[owner]") != "qa" {
		t.Fatalf("list query = %s", parsed.RawQuery)
	}

	agent, err := client.Agents.Get(context.Background(), "demo-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ListSandboxes(context.Background(), AgentSandboxListParams{
		Metadata: map[string]string{"session": "abc"},
	}); err != nil {
		t.Fatal(err)
	}
	parsed, err = url.Parse(calls[len(calls)-1].url)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/deployments/demo-agent/tasks" || parsed.Query().Get("metadata[session]") != "abc" {
		t.Fatalf("agent list = %s", calls[len(calls)-1].url)
	}
}

func TestSandboxResume(t *testing.T) {
	var calls []struct {
		method string
		url    string
		body   []byte
	}
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, struct {
			method string
			url    string
			body   []byte
		}{method, rawURL, append([]byte(nil), body...)})
		switch {
		case method == http.MethodGet && hasSuffix(rawURL, "/tasks/task-1"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"task-1","task_status":"paused","expiry_pinned":false}`)}, nil
		case method == http.MethodPost && hasSuffix(rawURL, "/tasks/task-1/resume"):
			if len(body) == 0 {
				return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"task_id":"task-1","state":"resuming","expiry_pinned":false}`)}, nil
			}
			return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"task_id":"task-1","state":"resuming","expiry_pinned":true}`)}, nil
		default:
			t.Fatalf("unexpected %s %s", method, rawURL)
			return nil, nil
		}
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}

	sandbox, err := client.Sandboxes.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sandbox.Resume(context.Background(), ResumeParams{}); err != nil {
		t.Fatal(err)
	}
	if calls[1].method != http.MethodPost || !hasSuffix(calls[1].url, "/tasks/task-1/resume") || len(calls[1].body) != 0 {
		t.Fatalf("resume without timeout = %s %s body=%q", calls[1].method, calls[1].url, calls[1].body)
	}
	if sandbox.Status() != "resuming" || sandbox.ExpiryPinned() {
		t.Fatalf("status=%q pinned=%v", sandbox.Status(), sandbox.ExpiryPinned())
	}

	timeoutSeconds := 3600
	resumed, err := client.Sandboxes.Resume(context.Background(), "task-1", ResumeParams{TimeoutSeconds: &timeoutSeconds})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(calls[2].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["timeout_seconds"] != float64(3600) {
		t.Fatalf("resume body = %#v", body)
	}
	if resumed.TaskID != "task-1" || resumed.State != "resuming" || !resumed.ExpiryPinned {
		t.Fatalf("resume result = %+v", resumed)
	}
}

func TestSandboxPause(t *testing.T) {
	var calls []struct {
		method string
		url    string
		body   []byte
	}
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, struct {
			method string
			url    string
			body   []byte
		}{method, rawURL, append([]byte(nil), body...)})
		switch {
		case method == http.MethodGet && hasSuffix(rawURL, "/tasks/task-1"):
			if len(calls) == 1 {
				return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"task-1","task_status":"running","sandbox":{"state":"running"}}`)}, nil
			}
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"task-1","task_status":"paused","sandbox":{"state":"paused","paused_at":"2026-10-14T02:00:00Z","paused_expire_at":"2026-10-21T02:00:00Z"}}`)}, nil
		case method == http.MethodPost && hasSuffix(rawURL, "/tasks/task-1/pause"):
			return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"task_id":"task-1","state":"pausing"}`)}, nil
		default:
			t.Fatalf("unexpected %s %s", method, rawURL)
			return nil, nil
		}
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}

	sandbox, err := client.Sandboxes.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.PausedAt() != "" || sandbox.PausedExpireAt() != "" {
		t.Fatalf("running sandbox paused_at=%q paused_expire_at=%q", sandbox.PausedAt(), sandbox.PausedExpireAt())
	}
	if err := sandbox.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls[1].method != http.MethodPost || !hasSuffix(calls[1].url, "/tasks/task-1/pause") || len(calls[1].body) != 0 {
		t.Fatalf("pause = %s %s body=%q", calls[1].method, calls[1].url, calls[1].body)
	}
	if sandbox.Status() != "pausing" {
		t.Fatalf("status = %q", sandbox.Status())
	}

	if err := sandbox.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sandbox.Status() != "paused" || sandbox.PausedAt() != "2026-10-14T02:00:00Z" || sandbox.PausedExpireAt() != "2026-10-21T02:00:00Z" {
		t.Fatalf("status=%q paused_at=%q paused_expire_at=%q", sandbox.Status(), sandbox.PausedAt(), sandbox.PausedExpireAt())
	}

	paused, err := client.Sandboxes.Pause(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if paused.TaskID != "task-1" || paused.State != "pausing" {
		t.Fatalf("pause result = %+v", paused)
	}
}

func hasSuffix(rawURL, suffix string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return len(parsed.Path) >= len(suffix) && parsed.Path[len(parsed.Path)-len(suffix):] == suffix
}
