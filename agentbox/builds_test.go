package agentbox

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTemplateBuildLogs(t *testing.T) {
	var lastURL string
	transport := func(_ context.Context, method, rawURL string, _ http.Header, _ []byte) (*RawResponse, error) {
		lastURL = rawURL
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent/builds/build-1/logs"):
			if u.Query().Get("offset") == "2" {
				return &RawResponse{StatusCode: 200, Body: []byte(`{"entries":[],"next_offset":2,"has_more":false}`)}, nil
			}
			return &RawResponse{StatusCode: 200, Body: []byte(`{
				"entries": [
					{"offset": 0, "level": "info", "message": "Pulling base image ubuntu:22.04", "timestamp": "2026-09-27T08:00:06Z"},
					{"offset": 1, "level": "info", "message": "Running command 1/3: apt-get update", "timestamp": null}
				],
				"next_offset": 2,
				"has_more": false
			}`)}, nil
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent/builds/build-err"):
			return &RawResponse{StatusCode: 200, Body: []byte(`{
				"id": "build-err",
				"template_id": "tpl-1",
				"status": "error",
				"trigger": "initial_build",
				"source_type": "image",
				"artifact_state": "absent",
				"created_at": "2026-09-27T08:00:00Z",
				"started_at": "2026-09-27T08:00:05Z",
				"finished_at": "2026-09-27T08:10:00Z",
				"failure": {"code": "TemplateBuild.OutOfMemory", "message": "out of memory"}
			}`)}, nil
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent/builds/build-1"):
			return &RawResponse{StatusCode: 200, Body: []byte(`{
				"id": "build-1",
				"template_id": "tpl-1",
				"status": "building",
				"trigger": "initial_build",
				"source_type": "image",
				"artifact_state": "absent",
				"created_at": "2026-09-27T08:00:00Z",
				"started_at": "2026-09-27T08:00:05Z",
				"finished_at": null
			}`)}, nil
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent/builds"):
			return &RawResponse{StatusCode: 200, Body: []byte(`{
				"builds": [{
					"id": "build-1",
					"template_id": "tpl-1",
					"status": "building",
					"trigger": "initial_build",
					"source_type": "image",
					"artifact_state": "absent",
					"created_at": "2026-09-27T08:00:00Z",
					"started_at": "2026-09-27T08:00:05Z",
					"finished_at": null
				}]
			}`)}, nil
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent"):
			return &RawResponse{StatusCode: 200, Body: []byte(`{"id":"dep-1","slug":"demo-agent"}`)}, nil
		case method == http.MethodGet && strings.HasSuffix(u.Path, "/deployments/demo-agent/builds/missing/logs"):
			return &RawResponse{StatusCode: 410, Body: []byte(`{"error":"build_logs_expired"}`)}, nil
		default:
			t.Fatalf("unexpected request %s %s", method, rawURL)
			return nil, nil
		}
	}

	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL("https://example.test"),
		WithTransport(transport),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	agent, err := client.Agents.Get(ctx, "demo-agent")
	if err != nil {
		t.Fatal(err)
	}

	builds, err := agent.ListBuilds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds[0].ID != "build-1" || builds[0].Status != "building" {
		t.Fatalf("builds = %#v", builds)
	}
	if builds[0].FinishedAt != nil || builds[0].Failure != nil {
		t.Fatalf("expected no finish or failure, got %#v", builds[0])
	}
	if !strings.HasSuffix(lastURL, "/deployments/demo-agent/builds") {
		t.Fatalf("list url = %s", lastURL)
	}

	build, err := agent.GetBuild(ctx, "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if build.TemplateID != "tpl-1" || build.Trigger != "initial_build" || build.SourceType != "image" || build.ArtifactState != "absent" {
		t.Fatalf("build = %#v", build)
	}
	started, err := time.Parse(time.RFC3339, "2026-09-27T08:00:05Z")
	if err != nil {
		t.Fatal(err)
	}
	if build.StartedAt == nil || !build.StartedAt.Equal(started) || build.FinishedAt != nil {
		t.Fatalf("timestamps = started %#v finished %#v", build.StartedAt, build.FinishedAt)
	}

	failed, err := client.Agents.GetBuild(ctx, "demo-agent", "build-err")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "error" || failed.Failure == nil || failed.Failure.Code != "TemplateBuild.OutOfMemory" || failed.Failure.Message != "out of memory" {
		t.Fatalf("failed build = %#v", failed)
	}

	page, err := agent.BuildLogs(ctx, "build-1", BuildLogParams{})
	if err != nil {
		t.Fatal(err)
	}
	logsURL, err := url.Parse(lastURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(logsURL.Path, "/deployments/demo-agent/builds/build-1/logs") {
		t.Fatalf("logs path = %s", logsURL.Path)
	}
	if logsURL.Query().Get("offset") != "0" || logsURL.Query().Get("limit") != "100" || logsURL.Query().Get("level") != "" {
		t.Fatalf("logs query = %s", logsURL.RawQuery)
	}
	if page.HasMore || page.NextOffset != 2 || len(page.Entries) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Entries[0].Message != "Pulling base image ubuntu:22.04" || page.Entries[0].Timestamp == nil {
		t.Fatalf("first entry = %#v", page.Entries[0])
	}
	if page.Entries[1].Timestamp != nil {
		t.Fatalf("expected nil timestamp, got %#v", page.Entries[1].Timestamp)
	}

	filtered, err := client.Agents.BuildLogs(ctx, "demo-agent", "build-1", BuildLogParams{Offset: 2, Limit: 50, Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	filteredURL, err := url.Parse(lastURL)
	if err != nil {
		t.Fatal(err)
	}
	if filteredURL.Query().Get("offset") != "2" || filteredURL.Query().Get("limit") != "50" || filteredURL.Query().Get("level") != "error" {
		t.Fatalf("filtered query = %s", filteredURL.RawQuery)
	}
	if filtered.NextOffset != 2 || len(filtered.Entries) != 0 {
		t.Fatalf("filtered page = %#v", filtered)
	}

	_, err = client.Agents.BuildLogs(ctx, "demo-agent", "missing", BuildLogParams{})
	if !IsGoneError(err) {
		t.Fatalf("expected gone error, got %v", err)
	}
}

func TestBuildLogStatusErrors(t *testing.T) {
	gone := errorFromResponse(410, map[string]any{"error": "build_logs_expired"})
	if !IsGoneError(gone) || IsServerError(gone) {
		t.Fatalf("410 mapped to %#v", gone)
	}
	goneErr := gone.(*APIError)
	if goneErr.StatusCode != 410 || goneErr.Message != "build_logs_expired" {
		t.Fatalf("gone = %#v", goneErr)
	}

	unsupported := errorFromResponse(501, map[string]any{"error": "not_supported_by_runtime"})
	if !IsNotSupportedError(unsupported) || IsServerError(unsupported) {
		t.Fatalf("501 mapped to %#v", unsupported)
	}

	server := errorFromResponse(503, map[string]any{"error": "unavailable"})
	if !IsServerError(server) || IsNotSupportedError(server) {
		t.Fatalf("503 mapped to %#v", server)
	}
}
