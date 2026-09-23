package agentbox

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
)

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
			return nil, &TransportError{Message: err.Error()}
		}
		req.Header = headers.Clone()

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, &TransportError{Message: err.Error()}
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, &TransportError{Message: err.Error()}
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
			return nil, &TransportError{Message: err.Error()}
		}
		req.Header = headers.Clone()

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, &TransportError{Message: err.Error()}
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
