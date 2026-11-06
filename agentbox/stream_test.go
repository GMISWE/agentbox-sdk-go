package agentbox

import (
	"bufio"
	"strings"
	"testing"
)

// newTestStream wraps raw SSE text into a Stream for parser tests.
func newTestStream(raw string) *Stream {
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Stream{raw: &RawStream{StatusCode: 200, Lines: sc}}
}

// TestSSEFieldValueLeadingSpace follows the WHATWG SSE parsing model
// (https://html.spec.whatwg.org/multipage/server-sent-events.html): a field
// value has exactly ONE leading U+0020 removed, and trailing whitespace is
// preserved.
func TestSSEFieldValueLeadingSpace(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"single space is stripped", "data: hello", "hello"},
		{"only one space is stripped", "data:  lead-two", " lead-two"},
		{"trailing space is preserved", "data: trail ", "trail "},
		{"no space passes through", "data:exactly", "exactly"},
		{"tab after colon is preserved", "data:\tx", "\tx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStream(tc.line + "\n\n")
			ev, ok := s.Next()
			if !ok {
				t.Fatalf("no event parsed")
			}
			got, ok := ev.Data.(string)
			if !ok {
				t.Fatalf("data is not a string: %#v", ev.Data)
			}
			if got != tc.want {
				t.Fatalf("data field = %q, spec wants %q", got, tc.want)
			}
		})
	}
}

// TestSSEEventFieldSpace: the same one-space rule applies to the event field.
func TestSSEEventFieldSpace(t *testing.T) {
	s := newTestStream("event:  done\ndata: {}\n\n")
	ev, ok := s.Next()
	if !ok {
		t.Fatal("no event parsed")
	}
	if ev.Event != " done" {
		t.Fatalf("event field = %q, spec wants %q", ev.Event, " done")
	}
}

// TestSSEMultiLineData: multiple data lines join with U+000A per the spec.
func TestSSEMultiLineData(t *testing.T) {
	s := newTestStream("data: a\ndata: b\n\n")
	ev, ok := s.Next()
	if !ok {
		t.Fatal("no event parsed")
	}
	if ev.Data != "a\nb" {
		t.Fatalf("data = %q, want %q", ev.Data, "a\nb")
	}
}
