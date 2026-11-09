package agentbox

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy says when the client repeats a request. The zero value retries
// nothing. Turn it on with WithRetry.
//
// A 429 means the gateway did not process the request, so any method is
// repeated after it. A 502, 503 or 504 may have reached the server before it
// failed, so only GET and HEAD are repeated after those; a POST that may have
// started a sandbox or a command is never repeated. A network error is
// returned as it is.
//
// The wait before attempt n+1 is the reply's Retry-After (seconds or an HTTP
// date) when it has one, else BaseDelay doubled for each attempt so far, with
// jitter and capped at MaxDelay. A Retry-After longer than MaxDelay is not
// waited for: the reply is returned as the error it is. The wait ends early
// with the context's error when the context is done.
type RetryPolicy struct {
	// MaxAttempts is the total number of tries, the first one included.
	// 0 or 1 means no retry.
	MaxAttempts int
	// BaseDelay is the first backoff. Default 500ms.
	BaseDelay time.Duration
	// MaxDelay caps one wait. Default 10s.
	MaxDelay time.Duration
}

// next reports whether to try again after resp (the attempt-th try) and how
// long to wait first.
func (p RetryPolicy) next(attempt int, method string, resp *RawResponse) (time.Duration, bool) {
	if attempt >= p.MaxAttempts {
		return 0, false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		if method != http.MethodGet && method != http.MethodHead {
			return 0, false
		}
	default:
		return 0, false
	}
	base, ceiling := p.BaseDelay, p.MaxDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if ceiling <= 0 {
		ceiling = 10 * time.Second
	}
	if wait, ok := retryAfter(resp.Header.Get("Retry-After")); ok {
		if wait > ceiling {
			return 0, false
		}
		return wait, true
	}
	wait := base << (attempt - 1)
	if wait <= 0 || wait > ceiling {
		wait = ceiling
	}
	return wait/2 + rand.N(wait/2+1), true
}

// retryAfter reads a Retry-After value: whole seconds or an HTTP date.
func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

// waitFor sleeps for d, or returns the context's error when it is done first.
func waitFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
