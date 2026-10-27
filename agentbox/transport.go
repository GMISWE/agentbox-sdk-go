package agentbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects is the maximum number of redirects the SDK follows.
const maxRedirects = 5

// checkRedirect is the SDK redirect policy: at most maxRedirects hops, only
// http/https targets, and Authorization/Cookie are dropped when the origin
// (scheme, host, port) changes from the original request.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("agentbox: stopped after %d redirects", maxRedirects)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("agentbox: unsupported redirect scheme %q", req.URL.Scheme)
	}
	if originOf(req.URL) != originOf(via[0].URL) {
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
	}
	return nil
}

// originOf returns scheme://host:port with the default port made explicit.
func originOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// withRedirectPolicy returns a shallow copy of hc with the SDK redirect
// policy applied; the caller's client is never mutated. If hc already sets
// CheckRedirect, the SDK policy runs first (so credentials are stripped and
// limits enforced) and the caller's function is called after it.
func withRedirectPolicy(hc *http.Client) *http.Client {
	cp := *hc
	if user := hc.CheckRedirect; user != nil {
		cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if err := checkRedirect(req, via); err != nil {
				return err
			}
			return user(req, via)
		}
	} else {
		cp.CheckRedirect = checkRedirect
	}
	return &cp
}

// RawResponse is a transport-level HTTP response.
type RawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// RawStream is a transport-level streaming HTTP response, read line by line
// (used for Server-Sent Events).
type RawStream struct {
	StatusCode int
	Header     http.Header
	Lines      *bufio.Scanner
	closer     io.Closer
}

// Close releases the underlying connection.
func (s *RawStream) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

// Transport performs a single request/response HTTP round trip.
type Transport func(ctx context.Context, method, url string, headers http.Header, body []byte) (*RawResponse, error)

// StreamTransport performs a streaming HTTP request, returning a line reader.
type StreamTransport func(ctx context.Context, method, url string, headers http.Header, body []byte) (*RawStream, error)

func defaultTransport(httpClient *http.Client) Transport {
	return func(ctx context.Context, method, url string, headers http.Header, body []byte) (*RawResponse, error) {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, newTransportError(err)
		}
		req.Header = headers.Clone()

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, newTransportError(err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, newTransportError(err)
		}

		return &RawResponse{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       respBody,
		}, nil
	}
}

func defaultStreamTransport(httpClient *http.Client) StreamTransport {
	return func(ctx context.Context, method, url string, headers http.Header, body []byte) (*RawStream, error) {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, newTransportError(err)
		}
		req.Header = headers.Clone()

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, newTransportError(err)
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		return &RawStream{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Lines:      scanner,
			closer:     resp.Body,
		}, nil
	}
}
