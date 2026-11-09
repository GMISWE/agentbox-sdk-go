package agentbox

import (
	"context"
	"net/http"
	"testing"
)

func executionClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	transport := func(_ context.Context, _, _ string, _ http.Header, _ []byte) (*RawResponse, error) {
		return &RawResponse{StatusCode: status, Body: []byte(body)}, nil
	}
	c, err := NewClient(WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A reply that is not a JSON object is an error for Execute and CancelExecution. GetExecution already refuses
// the same bytes; these two used to return an Execution with no ID that looked like a finished command.
func TestExecutionRepliesThatAreNotObjectsAreErrors(t *testing.T) {
	for _, body := range []string{`{"execution_id":`, `<html>upstream unavailable</html>`, `null`, `[1]`, `"ok"`} {
		c := executionClient(t, http.StatusOK, body)
		if ex, err := c.Sandboxes.Execute(context.Background(), "task-1", "true", ExecuteParams{}); err == nil {
			t.Errorf("Execute(%q) = %+v, want an error", body, ex)
		}
		if ex, err := c.Sandboxes.CancelExecution(context.Background(), "task-1", "exec-1"); err == nil {
			t.Errorf("CancelExecution(%q) = %+v, want an error", body, ex)
		}
	}
}

func TestExecutionRepliesThatAreObjectsKeepTheirMeaning(t *testing.T) {
	c := executionClient(t, http.StatusAccepted, `{"execution_id":"exec-1","status":"running"}`)
	ex, err := c.Sandboxes.Execute(context.Background(), "task-1", "sleep 5", ExecuteParams{})
	if err != nil || ex.Data["execution_id"] != "exec-1" || !ex.Accepted {
		t.Fatalf("202 reply: %+v, %v", ex, err)
	}
	c = executionClient(t, http.StatusOK, `{"execution_id":"exec-2","status":"completed"}`)
	ex, err = c.Sandboxes.Execute(context.Background(), "task-1", "true", ExecuteParams{})
	if err != nil || ex.Data["execution_id"] != "exec-2" || ex.Accepted {
		t.Fatalf("200 reply: %+v, %v", ex, err)
	}
	c = executionClient(t, http.StatusAccepted, ``)
	ex, err = c.Sandboxes.CancelExecution(context.Background(), "task-1", "exec-1")
	if err != nil || len(ex.Data) != 0 || !ex.Accepted {
		t.Fatalf("empty 202 reply: %+v, %v", ex, err)
	}
}
