package agentbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

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

func hasSuffix(rawURL, suffix string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return len(parsed.Path) >= len(suffix) && parsed.Path[len(parsed.Path)-len(suffix):] == suffix
}
