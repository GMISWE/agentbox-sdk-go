package agentbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
)

// ShellMessage is one message received from the shell service.
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

// ShellConn is an interactive Sandbox shell connection.
//
// The service speaks TEXT frames carrying a JSON envelope. Raw text, binary
// frames and unknown message types are silently ignored, so use SendInput and
// Resize to talk to the shell rather than Send.
//
// A ShellConn is not safe for concurrent writers: do not call Send, SendInput
// and Resize from several goroutines at once. At most one goroutine may read
// (Recv, RecvMessage) at a time. One writer and one reader may run together,
// and Close may be called from any goroutine to unblock a pending read.
type ShellConn interface {
	// Send writes a raw text frame to the shell without any envelope. The
	// service ignores frames that are not the JSON envelope; prefer SendInput
	// and Resize.
	Send(message string) error
	// Recv reads the next text frame as received, without decoding it.
	Recv() (string, error)
	// SendInput writes {"type":"stdin","data":data}; data is typed into the
	// shell as is, so end a command with "\n".
	SendInput(data string) error
	// Resize writes {"type":"resize","cols":cols,"rows":rows}. It returns an
	// error without sending when cols or rows is not positive.
	Resize(cols, rows int) error
	// RecvMessage reads the next text frame and decodes it as a ShellMessage.
	RecvMessage() (ShellMessage, error)
	// Close closes the connection.
	Close() error
}

// ShellDialer opens an authenticated shell WebSocket connection.
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

func (w *wsShellConn) sendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.Send(string(data))
}

func (w *wsShellConn) SendInput(data string) error {
	return w.sendJSON(struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}{"stdin", data})
}

func (w *wsShellConn) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("agentbox: shell size must be positive, got cols=%d rows=%d", cols, rows)
	}
	return w.sendJSON(struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}{"resize", cols, rows})
}

func (w *wsShellConn) RecvMessage() (ShellMessage, error) {
	text, err := w.Recv()
	if err != nil {
		return ShellMessage{}, err
	}
	var m ShellMessage
	// Not a JSON object with a type (plain text, "null", arrays, ...): keep
	// the raw text rather than returning an empty message.
	if err := json.Unmarshal([]byte(text), &m); err != nil || m.Type == "" {
		return ShellMessage{Data: text}, nil
	}
	return m, nil
}

func (w *wsShellConn) Close() error {
	return w.conn.Close()
}

func defaultShellDialer(ctx context.Context, wsURL string, headers http.Header) (ShellConn, error) {
	dialer := *websocket.DefaultDialer
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			return nil, errorFromBody(resp.StatusCode, body)
		}
		return nil, newTransportError(err)
	}
	return &wsShellConn{conn: conn}, nil
}
