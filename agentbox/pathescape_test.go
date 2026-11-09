package agentbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// An ID or slug is one path segment. If it holds "/", "?" or "#", or is "." or "..", the request must
// still go to the same route with the value escaped inside that one segment.
func TestPathSegmentsAreEscaped(t *testing.T) {
	var mu sync.Mutex
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uris = append(uris, r.RequestURI)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx := context.Background()
	const bad = "a/b?x=1#f"
	const esc = "a%2Fb%3Fx=1%23f"

	cases := []struct {
		name string
		call func()
		want string
	}{
		{"Agents.Get", func() { _, _ = c.Agents.Get(ctx, bad) }, "/deployments/" + esc},
		{"Agents.Update", func() { _, _ = c.Agents.Update(ctx, bad, AgentUpdateFields{"title": "x"}) }, "/deployments/" + esc},
		{"Agents.Delete", func() { _ = c.Agents.Delete(ctx, bad) }, "/deployments/" + esc},
		{"Agents.Delete dot dot", func() { _ = c.Agents.Delete(ctx, "..") }, "/deployments/%2E%2E"},
		{"Agents.ListBuilds", func() { _, _ = c.Agents.ListBuilds(ctx, bad) }, "/deployments/" + esc + "/builds"},
		{"Agents.GetBuild", func() { _, _ = c.Agents.GetBuild(ctx, bad, bad) }, "/deployments/" + esc + "/builds/" + esc},
		{"Agents.BuildLogs", func() { _, _ = c.Agents.BuildLogs(ctx, bad, bad, BuildLogParams{}) }, "/deployments/" + esc + "/builds/" + esc + "/logs"},
		{"Agents.ListSnapshots", func() { _, _ = c.Agents.ListSnapshots(ctx, bad) }, "/deployments/" + esc + "/snapshots"},
		{"Sandboxes.Get", func() { _, _ = c.Sandboxes.Get(ctx, bad) }, "/tasks/" + esc},
		{"Sandboxes.Delete", func() { _ = c.Sandboxes.Delete(ctx, "..") }, "/tasks/%2E%2E"},
		{"Sandboxes.Pause", func() { _, _ = c.Sandboxes.Pause(ctx, bad) }, "/tasks/" + esc + "/pause"},
		{"Sandboxes.Resume", func() { _, _ = c.Sandboxes.Resume(ctx, bad, ResumeParams{}) }, "/tasks/" + esc + "/resume"},
		{"Sandboxes.SetTimeout", func() { _, _ = c.Sandboxes.SetTimeout(ctx, bad, 120) }, "/tasks/" + esc + "/timeout"},
		{"Sandboxes.GetExecution", func() { _, _ = c.Sandboxes.GetExecution(ctx, bad, bad) }, "/tasks/" + esc + "/executions/" + esc},
		{"Sandboxes.CancelExecution", func() { _, _ = c.Sandboxes.CancelExecution(ctx, bad, bad) }, "/tasks/" + esc + "/executions/" + esc + "/cancel"},
		{"Sandboxes.CreateSnapshot", func() { _, _ = c.Sandboxes.CreateSnapshot(ctx, bad, CreateSnapshotParams{}) }, "/tasks/" + esc + "/snapshots"},
		{"Snapshots.Rename", func() { _, _ = c.Snapshots.Rename(ctx, bad, "n") }, "/snapshots/" + esc},
		{"Snapshots.Delete", func() { _, _ = c.Snapshots.Delete(ctx, bad) }, "/snapshots/" + esc},
	}
	for _, tc := range cases {
		mu.Lock()
		uris = nil
		mu.Unlock()
		tc.call()
		mu.Lock()
		got := append([]string(nil), uris...)
		mu.Unlock()
		if len(got) != 1 {
			t.Errorf("%s: got %d requests %v, want 1", tc.name, len(got), got)
			continue
		}
		if !strings.HasPrefix(got[0], tc.want) || (len(got[0]) > len(tc.want) && got[0][len(tc.want)] != '?') {
			t.Errorf("%s: request URI %q, want path %q", tc.name, got[0], tc.want)
		}
		if strings.Contains(got[0], "#") {
			t.Errorf("%s: request URI %q still holds a fragment", tc.name, got[0])
		}
	}
}
