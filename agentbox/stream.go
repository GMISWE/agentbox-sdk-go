package agentbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// StreamEvent is one Server-Sent Event from GET /tasks/{id}/stream.
type StreamEvent struct {
	Event string
	Data  any
	Raw   string
}

// Stream is a pull-based reader over a Server-Sent Events response.
type Stream struct {
	raw *RawStream
	err error
}

// Next advances to the next event, returning false when the stream ends
// (either cleanly or due to an error retrievable via Err).
func (s *Stream) Next() (StreamEvent, bool) {
	eventName := ""
	var dataLines []string

	for s.raw.Lines.Scan() {
		line := s.raw.Lines.Text()
		line = strings.TrimSuffix(line, "\r")

		switch {
		case strings.HasPrefix(line, ":"):
			return StreamEvent{
				Event: "heartbeat",
				Data:  strings.TrimSpace(strings.TrimPrefix(line, ":")),
				Raw:   line,
			}, true
		case line == "":
			if eventName == "" && len(dataLines) == 0 {
				continue
			}
			rawData := strings.Join(dataLines, "\n")
			event := StreamEvent{
				Event: eventName,
				Raw:   rawData,
			}
			if event.Event == "" {
				event.Event = "message"
			}
			if rawData != "" {
				var parsed any
				if err := json.Unmarshal([]byte(rawData), &parsed); err == nil {
					event.Data = parsed
				} else {
					event.Data = rawData
				}
			}
			return event, true
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := s.raw.Lines.Err(); err != nil {
		// Caller cancellation and an oversized line are not transport
		// failures; return them as is so errors.Is and == keep working.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, bufio.ErrTooLong) {
			s.err = err
		} else {
			s.err = newTransportError(err)
		}
	}
	return StreamEvent{}, false
}

// Err returns any error encountered while reading the stream. Read failures
// are a *TransportError; context cancellation or deadline errors and
// bufio.ErrTooLong (a line over 1 MiB) are returned unwrapped.
func (s *Stream) Err() error { return s.err }

// Close releases the underlying connection.
func (s *Stream) Close() error {
	if s.raw == nil {
		return nil
	}
	return s.raw.Close()
}
