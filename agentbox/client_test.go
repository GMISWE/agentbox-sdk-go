package agentbox

import (
	"context"
	"net/http"
	"net/url"
	"strings"
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
