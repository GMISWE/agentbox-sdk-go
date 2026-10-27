package agentbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

func TestShellHandshake101BadAcceptIsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", "bogus")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer srv.Close()

	_, err := defaultShellDialer(context.Background(), wsURL(srv), http.Header{})
	var te *TransportError
	if !errors.As(err, &te) || !errors.Is(err, websocket.ErrBadHandshake) {
		t.Fatalf("err = %#v", err)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("unexpected APIError: %#v", apiErr)
	}
}

func TestShellHandshake200IsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>proxy</html>"))
	}))
	defer srv.Close()

	_, err := defaultShellDialer(context.Background(), wsURL(srv), http.Header{})
	var te *TransportError
	if !errors.As(err, &te) || !errors.Is(err, websocket.ErrBadHandshake) {
		t.Fatalf("err = %#v", err)
	}
}

// minimalConn implements only Send/Recv/Close.
type minimalConn struct {
	sent []string
	recv []string
}

func (m *minimalConn) Send(s string) error { m.sent = append(m.sent, s); return nil }
func (m *minimalConn) Recv() (string, error) {
	if len(m.recv) == 0 {
		return "", errors.New("eof")
	}
	s := m.recv[0]
	m.recv = m.recv[1:]
	return s, nil
}
func (m *minimalConn) Close() error { return nil }

func TestShellHelpersWorkWithAnyShellConn(t *testing.T) {
	var conn ShellConn = &minimalConn{recv: []string{`{"type":"stdout","data":"x"}`, "plain"}}
	if err := SendShellInput(conn, "ls\n"); err != nil {
		t.Fatal(err)
	}
	if err := ResizeShell(conn, 100, 30); err != nil {
		t.Fatal(err)
	}
	if err := ResizeShell(conn, 0, 30); err == nil {
		t.Fatal("zero cols accepted")
	}
	mc := conn.(*minimalConn)
	if len(mc.sent) != 2 || mc.sent[0] != `{"type":"stdin","data":"ls\n"}` || mc.sent[1] != `{"type":"resize","cols":100,"rows":30}` {
		t.Fatalf("sent = %q", mc.sent)
	}
	if m, err := RecvShellMessage(conn); err != nil || m.Type != "stdout" || m.Data != "x" {
		t.Fatalf("m = %#v, err = %v", m, err)
	}
	if m, err := RecvShellMessage(conn); err != nil || m.Type != "" || m.Data != "plain" {
		t.Fatalf("m = %#v, err = %v", m, err)
	}
	if _, err := RecvShellMessage(conn); err == nil {
		t.Fatal("expected recv error")
	}
}

func TestDecodeShellMessage(t *testing.T) {
	if m := DecodeShellMessage(`{"type":"status","message":"connected"}`); m.Type != "status" || m.Message != "connected" {
		t.Fatalf("m = %#v", m)
	}
	for _, raw := range []string{"null", "[1]", `{"data":"x"}`, "text"} {
		if m := DecodeShellMessage(raw); m.Type != "" || m.Data != raw {
			t.Fatalf("raw %q: m = %#v", raw, m)
		}
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

// shellServer is a fake shell service speaking the JSON envelope.
type shellServer struct {
	*httptest.Server
	mu     sync.Mutex
	resize []map[string]any
	raw    []string
}

func newShellServer(t *testing.T, greeting ...string) *shellServer {
	t.Helper()
	ss := &shellServer{}
	up := websocket.Upgrader{}
	ss.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"status","message":"connected"}`))
		for _, g := range greeting {
			_ = c.WriteMessage(websocket.TextMessage, []byte(g))
		}
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				ss.mu.Lock()
				ss.raw = append(ss.raw, string(data))
				ss.mu.Unlock()
				continue
			}
			switch m["type"] {
			case "stdin":
				out, _ := json.Marshal(map[string]any{"type": "stdout", "data": m["data"].(string) + "OUT\r\n"})
				_ = c.WriteMessage(websocket.TextMessage, out)
			case "resize":
				ss.mu.Lock()
				ss.resize = append(ss.resize, m)
				ss.mu.Unlock()
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"status","message":"resized"}`))
			}
		}
	}))
	t.Cleanup(ss.Close)
	return ss
}

func dialShell(t *testing.T, ss *shellServer) ShellConn {
	t.Helper()
	conn, err := defaultShellDialer(context.Background(), wsURL(ss.Server), http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if w, ok := conn.(*wsShellConn); ok {
		_ = w.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
	return conn
}

func TestShellRecvMessageConnectedStatus(t *testing.T) {
	conn := dialShell(t, newShellServer(t))
	m, err := RecvShellMessage(conn)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "status" || m.Message != "connected" {
		t.Fatalf("m = %#v", m)
	}
}

func TestShellSendInputRoundTrip(t *testing.T) {
	conn := dialShell(t, newShellServer(t))
	if _, err := RecvShellMessage(conn); err != nil {
		t.Fatal(err)
	}
	if err := SendShellInput(conn, "echo \"hi\"\n"); err != nil {
		t.Fatal(err)
	}
	m, err := RecvShellMessage(conn)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "stdout" || m.Data != "echo \"hi\"\nOUT\r\n" {
		t.Fatalf("m = %#v", m)
	}
}

func TestShellResizeSendsExactEnvelope(t *testing.T) {
	ss := newShellServer(t)
	conn := dialShell(t, ss)
	if _, err := RecvShellMessage(conn); err != nil {
		t.Fatal(err)
	}
	if err := ResizeShell(conn, 120, 32); err != nil {
		t.Fatal(err)
	}
	// The server acknowledges after recording, so the record is visible.
	if m, err := RecvShellMessage(conn); err != nil || m.Message != "resized" {
		t.Fatalf("m = %#v, err = %v", m, err)
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if len(ss.resize) != 1 {
		t.Fatalf("resize = %#v", ss.resize)
	}
	got := ss.resize[0]
	if got["type"] != "resize" || got["cols"] != float64(120) || got["rows"] != float64(32) || len(got) != 3 {
		t.Fatalf("got = %#v", got)
	}
}

func TestShellResizeRejectsNonPositive(t *testing.T) {
	ss := newShellServer(t)
	conn := dialShell(t, ss)
	for _, sz := range [][2]int{{0, 24}, {80, 0}, {-1, 24}, {80, -5}} {
		if err := ResizeShell(conn, sz[0], sz[1]); err == nil {
			t.Fatalf("Resize(%d, %d) succeeded", sz[0], sz[1])
		}
	}
	// Nothing was sent: a valid message still works and the server saw no resize.
	if err := SendShellInput(conn, "x"); err != nil {
		t.Fatal(err)
	}
	for {
		m, err := RecvShellMessage(conn)
		if err != nil {
			t.Fatal(err)
		}
		if m.Type == "stdout" {
			break
		}
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if len(ss.resize) != 0 {
		t.Fatalf("resize = %#v", ss.resize)
	}
}

func TestShellRecvMessageNonJSONFrame(t *testing.T) {
	conn := dialShell(t, newShellServer(t, "plain text\r\n"))
	if _, err := RecvShellMessage(conn); err != nil {
		t.Fatal(err)
	}
	m, err := RecvShellMessage(conn)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "" || m.Data != "plain text\r\n" || m.Message != "" {
		t.Fatalf("m = %#v", m)
	}
}

func TestShellRecvMessageErrorAndUnknownType(t *testing.T) {
	conn := dialShell(t, newShellServer(t, `{"type":"error","message":"already attached"}`, `{"type":"future","data":"d"}`))
	_, _ = RecvShellMessage(conn)
	if m, _ := RecvShellMessage(conn); m.Type != "error" || m.Message != "already attached" {
		t.Fatalf("m = %#v", m)
	}
	if m, _ := RecvShellMessage(conn); m.Type != "future" || m.Data != "d" {
		t.Fatalf("m = %#v", m)
	}
}

func TestShellSendWritesRawText(t *testing.T) {
	ss := newShellServer(t)
	conn := dialShell(t, ss)
	if err := conn.Send("ls\n"); err != nil {
		t.Fatal(err)
	}
	// Flush ordering: a later resize is acknowledged after the raw frame was read.
	if err := ResizeShell(conn, 80, 24); err != nil {
		t.Fatal(err)
	}
	for {
		m, err := RecvShellMessage(conn)
		if err != nil {
			t.Fatal(err)
		}
		if m.Message == "resized" {
			break
		}
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if len(ss.raw) != 1 || ss.raw[0] != "ls\n" {
		t.Fatalf("raw = %q", ss.raw)
	}
}

func TestShellRecvMessageNonObjectFramesKeepRawText(t *testing.T) {
	conn := dialShell(t, newShellServer(t, "null", "[1,2]", `{"data":"no type"}`))
	_, _ = RecvShellMessage(conn)
	for _, want := range []string{"null", "[1,2]", `{"data":"no type"}`} {
		m, err := RecvShellMessage(conn)
		if err != nil {
			t.Fatal(err)
		}
		if m.Type != "" || m.Data != want {
			t.Fatalf("want raw %q, got %#v", want, m)
		}
	}
}
