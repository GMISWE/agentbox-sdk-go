package agentbox

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// replies serves the given statuses in order (the last one repeats) and counts the calls.
func replies(t *testing.T, retryAfter string, statuses ...int) (*atomic.Int32, func(RetryPolicy) *Client) {
	t.Helper()
	var calls atomic.Int32
	transport := func(_ context.Context, _, _ string, _ http.Header, _ []byte) (*RawResponse, error) {
		i := int(calls.Add(1)) - 1
		status := statuses[min(i, len(statuses)-1)]
		h := http.Header{}
		if retryAfter != "" {
			h.Set("Retry-After", retryAfter)
		}
		body := `{"id":"ok"}`
		if status >= 300 {
			body = `{"error":{"code":"busy","message":"try later"}}`
		}
		return &RawResponse{StatusCode: status, Header: h, Body: []byte(body)}, nil
	}
	build := func(p RetryPolicy) *Client {
		opts := []Option{WithAPIKey("test-key"), WithBaseURL("https://example.test"), WithTransport(transport)}
		if p.MaxAttempts != 0 {
			opts = append(opts, WithRetry(p))
		}
		c, err := NewClient(opts...)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	return &calls, build
}

var fast = RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 20 * time.Millisecond}

func TestRetryIsOffUnlessSet(t *testing.T) {
	calls, build := replies(t, "", 503, 200)
	if _, err := build(RetryPolicy{}).Agents.Get(context.Background(), "a"); err == nil {
		t.Fatal("503 should be an error without WithRetry")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestAGetIsRepeatedAfterA503(t *testing.T) {
	calls, build := replies(t, "", 503, 502, 200)
	if _, err := build(fast).Agents.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestAPostIsNotRepeatedAfterA5xx(t *testing.T) {
	calls, build := replies(t, "", 503, 200)
	if _, err := build(fast).Sandboxes.Launch(context.Background(), "a", LaunchParams{IdcName: "i", InstanceType: "t"}); err == nil {
		t.Fatal("a 503 on a launch must come back as an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1: the launch may already have started", calls.Load())
	}
}

func TestA429IsRepeatedForAnyMethodAndHonoursRetryAfter(t *testing.T) {
	calls, build := replies(t, "0", 429, 202)
	start := time.Now()
	if _, err := build(fast).Sandboxes.Pause(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || time.Since(start) > time.Second {
		t.Fatalf("calls = %d after %v, want 2 right away", calls.Load(), time.Since(start))
	}
}

func TestMaxAttemptsEndsTheRetries(t *testing.T) {
	calls, build := replies(t, "", 503)
	_, err := build(RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond}).Agents.Get(context.Background(), "a")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || calls.Load() != 3 {
		t.Fatalf("err = %v after %d calls, want the 503 APIError after 3", err, calls.Load())
	}
}

func TestARetryAfterBeyondMaxDelayIsNotWaitedFor(t *testing.T) {
	calls, build := replies(t, "3600", 429)
	start := time.Now()
	_, err := build(fast).Agents.Get(context.Background(), "a")
	if !IsRateLimitError(err) || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("err = %v, calls = %d, after %v: want the 429 at once", err, calls.Load(), time.Since(start))
	}
}

func TestACancelledContextEndsTheWait(t *testing.T) {
	calls, build := replies(t, "5", 429)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := build(RetryPolicy{MaxAttempts: 3, MaxDelay: 10 * time.Second}).Agents.Get(ctx, "a")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d, after %v: want the context error well before the 5 s wait", err, calls.Load(), time.Since(start))
	}
}

func TestRetryAfterReadsSecondsAndHTTPDates(t *testing.T) {
	if d, ok := retryAfter("7"); !ok || d != 7*time.Second {
		t.Errorf("seconds: %v %v", d, ok)
	}
	when := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := retryAfter(when); !ok || d < 25*time.Second || d > 31*time.Second {
		t.Errorf("date %q: %v %v", when, d, ok)
	}
	for _, bad := range []string{"", "soon", "-3"} {
		if _, ok := retryAfter(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}
