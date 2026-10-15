package agentbox

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type recordedCall struct {
	method string
	url    string
	body   []byte
}

func TestSandboxCreateSnapshotAndWait(t *testing.T) {
	var calls []recordedCall
	lists := 0
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, recordedCall{method, rawURL, append([]byte(nil), body...)})
		switch {
		case method == http.MethodGet && hasSuffix(rawURL, "/tasks/task-1"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":"task-1","deployment_slug":"demo-agent","task_status":"running"}`)}, nil
		case method == http.MethodPost && hasSuffix(rawURL, "/tasks/task-1/snapshots"):
			return &RawResponse{StatusCode: http.StatusCreated, Body: []byte(`{"snapshot":{"id":"snap-1","name":"task-1-20261014-020000","status":"creating","source_task_id":"task-1","deployment_id":"dep-1"}}`)}, nil
		case method == http.MethodGet && hasSuffix(rawURL, "/deployments/demo-agent/snapshots"):
			lists++
			if lists == 1 {
				return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"snapshots":[{"id":"snap-0","status":"ready"},{"id":"snap-1","status":"creating"}]}`)}, nil
			}
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"snapshots":[{"id":"snap-1","status":"ready","idc_name":"us-central-iowa16","product_name":"SMALL","vcpu":1,"ram_mb":2048,"disk_size_mb":20480,"ready_at":"2026-10-14T02:01:00Z"}]}`)}, nil
		default:
			t.Fatalf("unexpected %s %s", method, rawURL)
			return nil, nil
		}
	}
	var slept []time.Duration
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport),
		WithSleep(func(d time.Duration) { slept = append(slept, d) }))
	if err != nil {
		t.Fatal(err)
	}

	sandbox, err := client.Sandboxes.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := sandbox.CreateSnapshot(context.Background(), CreateSnapshotParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls[1].body) != 0 {
		t.Fatalf("create snapshot without name sent body %q", calls[1].body)
	}
	if snapshot.ID() != "snap-1" || snapshot.Status() != "creating" || snapshot.SourceSandboxID() != "task-1" || snapshot.AgentSlug() != "demo-agent" {
		t.Fatalf("snapshot = %+v slug=%q", snapshot.Data, snapshot.AgentSlug())
	}

	if err := snapshot.WaitUntilReady(context.Background(), WaitUntilReadyParams{PollInterval: time.Second}); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("slept = %v", slept)
	}
	if snapshot.Status() != "ready" || snapshot.IdcName() != "us-central-iowa16" || snapshot.ProductName() != "SMALL" ||
		snapshot.VCPU() != 1 || snapshot.RAMMB() != 2048 || snapshot.DiskSizeMB() != 20480 || snapshot.ReadyAt() != "2026-10-14T02:01:00Z" {
		t.Fatalf("ready snapshot = %+v", snapshot.Data)
	}

	named, err := client.Sandboxes.CreateSnapshot(context.Background(), "task-1", CreateSnapshotParams{Name: "base"})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(calls[len(calls)-1].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "base" || len(body) != 1 {
		t.Fatalf("create snapshot body = %#v", body)
	}
	if named.AgentSlug() != "" {
		t.Fatalf("collection create slug = %q", named.AgentSlug())
	}
	if err := named.Refresh(context.Background()); err == nil {
		t.Fatal("refresh without agent slug succeeded")
	}
}

func TestSnapshotWaitFailureAndGone(t *testing.T) {
	body := `{"snapshots":[{"id":"snap-1","status":"error","status_reason":"disk full"}]}`
	transport := func(_ context.Context, method, rawURL string, _ http.Header, _ []byte) (*RawResponse, error) {
		if method != http.MethodGet || !hasSuffix(rawURL, "/deployments/demo-agent/snapshots") {
			t.Fatalf("unexpected %s %s", method, rawURL)
		}
		return &RawResponse{StatusCode: http.StatusOK, Body: []byte(body)}, nil
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := client.Agents.ListSnapshots(context.Background(), "demo-agent")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshots[0]

	err = snapshot.WaitUntilReady(context.Background(), WaitUntilReadyParams{})
	failed, ok := err.(*SnapshotFailed)
	if !ok || failed.Status != "error" || failed.Message != "disk full" {
		t.Fatalf("wait err = %#v", err)
	}

	body = `{"snapshots":[]}`
	if err := snapshot.Refresh(context.Background()); !IsNotFoundError(err) {
		t.Fatalf("refresh of deleted snapshot err = %v", err)
	}
}

func TestSnapshotRenameAndDelete(t *testing.T) {
	var calls []recordedCall
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, recordedCall{method, rawURL, append([]byte(nil), body...)})
		switch {
		case method == http.MethodGet && hasSuffix(rawURL, "/deployments/demo-agent/snapshots"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"snapshots":[{"id":"snap-1","name":"old","status":"ready"}]}`)}, nil
		case method == http.MethodPatch && hasSuffix(rawURL, "/snapshots/snap-1"):
			return &RawResponse{StatusCode: http.StatusOK, Body: []byte(`{"snapshot":{"id":"snap-1","name":"new","status":"ready"}}`)}, nil
		case method == http.MethodDelete && hasSuffix(rawURL, "/snapshots/snap-1"):
			return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"snapshot_id":"snap-1","status":"deleting"}`)}, nil
		default:
			t.Fatalf("unexpected %s %s", method, rawURL)
			return nil, nil
		}
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{client: client, Data: map[string]any{"slug": "demo-agent"}}
	snapshots, err := agent.ListSnapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshots[0]

	if err := snapshot.Rename(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(calls[1].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "new" || snapshot.Name() != "new" || snapshot.AgentSlug() != "demo-agent" {
		t.Fatalf("rename body=%#v name=%q slug=%q", body, snapshot.Name(), snapshot.AgentSlug())
	}

	if err := snapshot.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status() != "deleting" {
		t.Fatalf("status after delete = %q", snapshot.Status())
	}
	deletion, err := client.Snapshots.Delete(context.Background(), "snap-1")
	if err != nil {
		t.Fatal(err)
	}
	if deletion.SnapshotID != "snap-1" || deletion.Status != "deleting" {
		t.Fatalf("deletion = %+v", deletion)
	}
}

func TestLaunchFromSnapshot(t *testing.T) {
	var calls []recordedCall
	transport := func(_ context.Context, method, rawURL string, _ http.Header, body []byte) (*RawResponse, error) {
		calls = append(calls, recordedCall{method, rawURL, append([]byte(nil), body...)})
		if method != http.MethodPost || !hasSuffix(rawURL, "/deployments/demo-agent/tasks") {
			t.Fatalf("unexpected %s %s", method, rawURL)
		}
		return &RawResponse{StatusCode: http.StatusAccepted, Body: []byte(`{"task_id":"task-2","status":"pending"}`)}, nil
	}
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}

	sandbox, err := client.Sandboxes.Launch(context.Background(), "demo-agent", LaunchParams{SnapshotID: "snap-1"})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(calls[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["snapshot_id"] != "snap-1" {
		t.Fatalf("launch body = %#v", body)
	}
	if _, ok := body["idc_name"]; ok {
		t.Fatalf("launch from snapshot sent idc_name: %#v", body)
	}
	if sandbox.SourceSnapshotID() != "snap-1" {
		t.Fatalf("source snapshot = %q", sandbox.SourceSnapshotID())
	}

	if _, err := client.Sandboxes.Launch(context.Background(), "demo-agent", LaunchParams{}); err == nil {
		t.Fatal("launch without idc or snapshot succeeded")
	}
}
