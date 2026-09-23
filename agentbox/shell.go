package agentbox

import (
	"context"
	"net/http"

	"github.com/gorilla/websocket"
)

// ShellConn is an interactive Sandbox shell connection.
type ShellConn interface {
	// Send writes a text message to the shell.
	Send(message string) error
	// Recv reads the next text message from the shell.
	Recv() (string, error)
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

func (w *wsShellConn) Close() error {
	return w.conn.Close()
}

func defaultShellDialer(ctx context.Context, wsURL string, headers http.Header) (ShellConn, error) {
	dialer := *websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, &TransportError{Message: err.Error()}
	}
	return &wsShellConn{conn: conn}, nil
}
