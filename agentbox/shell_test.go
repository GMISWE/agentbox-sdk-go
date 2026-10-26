package agentbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

func TestShellHandshake401IsAuthenticationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized","message":"bad key"}`))
	}))
	defer srv.Close()

	_, err := defaultShellDialer(context.Background(), wsURL(srv), http.Header{})
	if !IsAuthenticationError(err) {
		t.Fatalf("err = %#v", err)
	}
	if apiErr := err.(*APIError); apiErr.Message != "bad key" || apiErr.Code != "unauthorized" {
		t.Fatalf("err = %#v", apiErr)
	}
}

func TestShellHandshakeRedirectIsGenericAPIError(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()

	_, err := defaultShellDialer(context.Background(), wsURL(srv), http.Header{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrorKindGeneric || apiErr.StatusCode != 302 {
		t.Fatalf("err = %#v", err)
	}
	if hits != 1 {
		t.Fatalf("redirect followed: hits = %d", hits)
	}
}

func TestShellHandshake503PlainBodyIsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := defaultShellDialer(context.Background(), wsURL(srv), http.Header{})
	if !IsServerError(err) {
		t.Fatalf("err = %#v", err)
	}
}

func TestShellConnectionRefusedIsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := wsURL(srv)
	srv.Close()

	_, err := defaultShellDialer(context.Background(), addr, http.Header{})
	var te *TransportError
	if !errors.As(err, &te) || te.Err == nil {
		t.Fatalf("err = %#v", err)
	}
}
