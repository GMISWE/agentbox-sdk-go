package agentbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestReadinessWait(t *testing.T) {
	for _, kind := range []string{"sandbox", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			response := func(status string) *RawResponse {
				body := fmt.Sprintf(`{"id":"resource-1","task_status":%q}`, status)
				if kind == "snapshot" {
					body = fmt.Sprintf(`{"snapshots":[{"id":"resource-1","status":%q}]}`, status)
				}
				return &RawResponse{StatusCode: http.StatusOK, Body: []byte(body)}
			}
			ready := "running"
			if kind == "snapshot" {
				ready = "ready"
			}
			wait := func(ctx context.Context, transport Transport, timeout, interval time.Duration, opts ...Option) error {
				client, err := NewClient(append([]Option{WithAPIKey("test-key"), WithTransport(transport)}, opts...)...)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "snapshot" {
					s := &Snapshot{client: client, agentSlug: "demo-agent", Data: map[string]any{"id": "resource-1"}}
					return s.WaitUntilReady(ctx, WaitUntilReadyParams{Timeout: timeout, PollInterval: interval})
				}
				s := &Sandbox{client: client, Data: map[string]any{"id": "resource-1"}}
				return s.WaitUntilRunning(ctx, WaitUntilRunningParams{Timeout: timeout, PollInterval: interval})
			}
			checkTimeout := func(t *testing.T, err error) {
				t.Helper()
				if kind == "snapshot" {
					var timeout *SnapshotWaitTimeout
					if !errors.As(err, &timeout) {
						t.Fatalf("expected SnapshotWaitTimeout, got %v", err)
					}
				} else {
					var timeout *SandboxWaitTimeout
					if !errors.As(err, &timeout) {
						t.Fatalf("expected SandboxWaitTimeout, got %v", err)
					}
				}
			}

			t.Run("poll_interval_exceeds_timeout", func(t *testing.T) {
				calls := 0
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					calls++
					if calls > 1 {
						return response(ready), nil
					}
					return response("pending"), nil
				}
				start := time.Now()
				err := wait(context.Background(), transport, 20*time.Millisecond, 300*time.Millisecond)
				elapsed := time.Since(start)
				checkTimeout(t, err)
				if calls != 1 {
					t.Fatalf("made %d requests; should not refresh after the deadline", calls)
				}
				if elapsed > 200*time.Millisecond {
					t.Fatalf("timeout took %s", elapsed)
				}
			})

			t.Run("refresh_uses_wait_deadline", func(t *testing.T) {
				transport := func(ctx context.Context, _ string, _ string, _ http.Header, _ []byte) (*RawResponse, error) {
					timer := time.NewTimer(300 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						return nil, newTransportError(ctx.Err())
					case <-timer.C:
						return response(ready), nil
					}
				}
				start := time.Now()
				err := wait(context.Background(), transport, 20*time.Millisecond, time.Millisecond)
				elapsed := time.Since(start)
				checkTimeout(t, err)
				if elapsed > 200*time.Millisecond {
					t.Fatalf("refresh exceeded wait timeout: %s", elapsed)
				}
			})

			t.Run("late_ready_response", func(t *testing.T) {
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					time.Sleep(40 * time.Millisecond)
					return response(ready), nil
				}
				checkTimeout(t, wait(context.Background(), transport, 20*time.Millisecond, time.Millisecond))
			})

			t.Run("cancel_during_poll", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var timer *time.Timer
				calls := 0
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					calls++
					if calls == 1 {
						timer = time.AfterFunc(20*time.Millisecond, cancel)
					}
					return response("pending"), nil
				}
				start := time.Now()
				err := wait(ctx, transport, time.Second, 300*time.Millisecond)
				if timer != nil {
					timer.Stop()
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation, got %v", err)
				}
				if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
					t.Fatalf("cancellation took %s", elapsed)
				}
				if calls != 1 {
					t.Fatalf("made %d requests after cancellation", calls)
				}
			})

			t.Run("already_canceled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					t.Error("made a request with an already canceled context")
					return response(ready), nil
				}
				if err := wait(ctx, transport, time.Second, time.Millisecond); !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation, got %v", err)
				}
			})

			t.Run("ready_before_timeout", func(t *testing.T) {
				calls := 0
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					calls++
					if calls > 1 {
						return response(ready), nil
					}
					return response("pending"), nil
				}
				if err := wait(context.Background(), transport, time.Second, time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if calls != 2 {
					t.Fatalf("requests = %d", calls)
				}
			})

			t.Run("transport_error", func(t *testing.T) {
				want := errors.New("offline")
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					return nil, want
				}
				if err := wait(context.Background(), transport, time.Second, time.Millisecond); !errors.Is(err, want) {
					t.Fatalf("expected original transport error, got %v", err)
				}
			})

			t.Run("custom_sleep_is_capped", func(t *testing.T) {
				var slept time.Duration
				transport := func(context.Context, string, string, http.Header, []byte) (*RawResponse, error) {
					return response("pending"), nil
				}
				err := wait(context.Background(), transport, 20*time.Millisecond, time.Second,
					WithSleep(func(d time.Duration) {
						slept = d
						time.Sleep(d)
					}))
				checkTimeout(t, err)
				if slept <= 0 || slept > 20*time.Millisecond {
					t.Fatalf("custom sleep duration = %s", slept)
				}
			})
		})
	}
}
