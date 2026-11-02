package agentbox

import "testing"

func TestErrorFromResponseCodeAndMessage(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		code    string
		message string
		str     string
	}{
		{"code with message", map[string]any{"code": "c1", "message": "m1", "error": "e1"}, "c1", "m1", "agentbox: c1: m1 (status=409)"},
		{"code with error", map[string]any{"code": "c1", "error": "e1"}, "c1", "e1", "agentbox: c1: e1 (status=409)"},
		{"code only", map[string]any{"code": "c1"}, "c1", "Request failed", "agentbox: c1: Request failed (status=409)"},
		{"error and message", map[string]any{"error": "snapshot_not_ready", "message": "only a ready snapshot", "snapshot_status": "deleting"}, "snapshot_not_ready", "only a ready snapshot", "agentbox: snapshot_not_ready: only a ready snapshot (status=409)"},
		{"only error", map[string]any{"error": "unauthorized"}, "unauthorized", "unauthorized", "agentbox: unauthorized (status=409)"},
		{"error equals message", map[string]any{"error": "same", "message": "same"}, "same", "same", "agentbox: same (status=409)"},
		{"only message", map[string]any{"message": "plain text"}, "", "plain text", "agentbox: plain text (status=409)"},
		{"non-string error", map[string]any{"error": 123.0}, "", "Request failed", "agentbox: Request failed (status=409)"},
		{"non-string error with message", map[string]any{"error": 123.0, "message": "m"}, "", "m", "agentbox: m (status=409)"},
		{"non-string code", map[string]any{"code": 5.0, "error": "e1", "message": "m"}, "e1", "m", "agentbox: e1: m (status=409)"},
		{"empty error with message", map[string]any{"error": "", "message": "x"}, "", "x", "agentbox: x (status=409)"},
		{"empty object", map[string]any{}, "", "Request failed", "agentbox: Request failed (status=409)"},
		{"non-object", "raw", "", "Request failed", "agentbox: Request failed (status=409)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr, ok := errorFromResponse(409, tc.payload).(*APIError)
			if !ok {
				t.Fatal("not an *APIError")
			}
			if apiErr.Code != tc.code || apiErr.Message != tc.message {
				t.Fatalf("code=%q message=%q, want %q %q", apiErr.Code, apiErr.Message, tc.code, tc.message)
			}
			if apiErr.Error() != tc.str {
				t.Fatalf("Error() = %q, want %q", apiErr.Error(), tc.str)
			}
		})
	}
}

func TestErrorFromResponseDetailsPassthrough(t *testing.T) {
	apiErr := errorFromResponse(409, map[string]any{"error": "e", "message": "m", "details": map[string]any{"k": "v"}}).(*APIError)
	if apiErr.Details["k"] != "v" {
		t.Fatalf("details = %v", apiErr.Details)
	}
	apiErr = errorFromResponse(409, map[string]any{"details": "nope"}).(*APIError)
	if apiErr.Details != nil {
		t.Fatalf("details = %v, want nil", apiErr.Details)
	}
}

func TestErrorFromBodyJSONArray(t *testing.T) {
	apiErr := errorFromBody(502, []byte(`["x"]`)).(*APIError)
	if apiErr.Code != "" || apiErr.Message != "Request failed" {
		t.Fatalf("got %+v", apiErr)
	}
}

func TestErrorFromBodyNonJSON(t *testing.T) {
	for name, body := range map[string]string{"plain text": "upstream exploded", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			apiErr, ok := errorFromBody(502, []byte(body)).(*APIError)
			if !ok {
				t.Fatal("not an *APIError")
			}
			if apiErr.Code != "" || apiErr.Message != "Request failed" || apiErr.StatusCode != 502 {
				t.Fatalf("got %+v", apiErr)
			}
		})
	}
}
