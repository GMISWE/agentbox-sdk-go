package agentbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
)

// Beta: ShellMessage is one message received from the shell service. The
// shell API and its message protocol may change in a later release.
//
// Known Type values:
//
//   - "status": Message is "connected" (first message) or "closed" (the shell
//     ended and the service is about to close the socket).
//   - "stdout": Data is raw terminal output, including the echo of your input,
//     "\r\n" line endings and control sequences. stderr is merged into stdout.
//   - "error": Message describes the problem, for example a second concurrent
//     attach to the same sandbox.
//
// Other JSON types pass through unchanged. A text frame that is not JSON
// yields Type "" with Data set to the raw text.
type ShellMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

// Beta: ShellConn is an interactive Sandbox shell connection. The shell API
// and its message protocol may change in a later release.
//
// The service speaks TEXT frames carrying a JSON envelope; raw text, binary
// frames and unknown message types are silently ignored. Use SendShellInput,
// ResizeShell and RecvShellMessage to talk to the shell instead of calling
// Send and Recv with hand-built frames. They work with any ShellConn.
//
// A ShellConn is not safe for concurrent writers. At most one goroutine may
// read at a time. One writer and one reader may run together, and Close may
// be called from any goroutine to unblock a pending read.
type ShellConn interface {
	// Send writes a text message to the shell.
	Send(message string) error
	// Recv reads the next text message from the shell.
	Recv() (string, error)
	// Close closes the connection.
	Close() error
}

func sendShellJSON(conn ShellConn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Send(string(data))
}

// Beta: SendShellInput writes {"type":"stdin","data":data} with conn.Send.
// data is typed into the shell as is, so end a command with "\n". The shell
// API may change in a later release.
func SendShellInput(conn ShellConn, data string) error {
	return sendShellJSON(conn, struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}{"stdin", data})
}

// Beta: ResizeShell writes {"type":"resize","cols":cols,"rows":rows} with
// conn.Send. It returns an error without sending when cols or rows is not
// positive. The shell API may change in a later release.
func ResizeShell(conn ShellConn, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("agentbox: shell size must be positive, got cols=%d rows=%d", cols, rows)
	}
	return sendShellJSON(conn, struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}{"resize", cols, rows})
}

// Beta: RecvShellMessage reads the next text frame with conn.Recv and decodes
// it with DecodeShellMessage. The shell API may change in a later release.
func RecvShellMessage(conn ShellConn) (ShellMessage, error) {
	text, err := conn.Recv()
	if err != nil {
		return ShellMessage{}, err
	}
	return DecodeShellMessage(text), nil
}

// Beta: DecodeShellMessage decodes one text frame. A JSON object with a
// non-empty "type" is decoded; anything else (plain text, "null", arrays,
// objects without a type) yields Type "" with Data set to the raw text. The
// shell API may change in a later release.
func DecodeShellMessage(text string) ShellMessage {
	var m ShellMessage
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.Type == "" {
		return ShellMessage{Data: text}
	}
	return m
}

// Beta: ShellDialer opens an authenticated shell WebSocket connection. The
// shell API may change in a later release.
type ShellDialer func(ctx context.Context, wsURL string, headers http.Header) (ShellConn, error)

type wsShellConn struct {
	conn *websocket.Conn
}

func (w *wsShellConn) Send(message string) error {
	return w.conn.WriteMessage(websocket.TextMessage, []byte(message))
}

func (w *wsShellConn) Recv() (string, error) {
	_, data, err := w.conn.ReadMessage()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *wsShellConn) Close() error {
	return w.conn.Close()
}

func defaultShellDialer(ctx context.Context, wsURL string, headers http.Header) (ShellConn, error) {
	dialer := *websocket.DefaultDialer
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
			// Only a real HTTP error answer maps to APIError. A 101 with a bad
			// accept key or a 2xx from a proxy stays a transport failure.
			if resp.StatusCode >= 300 {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
				return nil, errorFromBody(resp.StatusCode, body)
			}
		}
		return nil, newTransportError(err)
	}
	return &wsShellConn{conn: conn}, nil
}
