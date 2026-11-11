package toggleslash

import (
	"reflect"
	"testing"
)

func parseAll(chunks ...string) []sseEvent {
	var p sseParser
	var out []sseEvent
	for _, c := range chunks {
		p.feed([]byte(c), func(e sseEvent) { out = append(out, e) })
	}
	return out
}

func TestSSEParser(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []sseEvent
	}{
		{"lf", "event: a\ndata: 1\n\n", []sseEvent{{"a", "1"}}},
		{"crlf", "event: a\r\ndata: 1\r\n\r\n", []sseEvent{{"a", "1"}}},
		{"lone cr", "event: a\rdata: 1\r\r", []sseEvent{{"a", "1"}}},
		{"mixed line ends", "event: a\r\ndata: 1\n\rdata: 2\r\r", []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"default name", "data: x\n\n", []sseEvent{{"message", "x"}}},
		{"comment", ":keep alive\nevent: a\n:another\ndata: 1\n\n", []sseEvent{{"a", "1"}}},
		{"only a comment", ":hi\n\n", nil},
		{"data lines join with LF", "data: a\ndata: b\ndata:\ndata: c\n\n", []sseEvent{{"message", "a\nb\n\nc"}}},
		{"no data line drops the event", "event: a\n\nevent: b\ndata: 1\n\n", []sseEvent{{"b", "1"}}},
		{"empty data line is data", "data\n\n", []sseEvent{{"message", ""}}},
		{"one leading space only", "data:  two\n\n", []sseEvent{{"message", " two"}}},
		{"no space", "data:x\n\n", []sseEvent{{"message", "x"}}},
		{"unknown field", "id: 4\nretry: 5\nfoo: bar\ndata: 1\n\n", []sseEvent{{"message", "1"}}},
		{"incomplete event is discarded", "data: 1\n\ndata: 2\n", []sseEvent{{"message", "1"}}},
		{"incomplete line is discarded", "data: 1\n\ndata: 2", []sseEvent{{"message", "1"}}},
		{"byte order mark", "\xef\xbb\xbfdata: 1\n\n", []sseEvent{{"message", "1"}}},
		{"event name resets", "event: a\ndata: 1\n\ndata: 2\n\n", []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"multibyte", "data: héllo \U0001F600\n\n", []sseEvent{{"message", "héllo \U0001F600"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			whole := parseAll(tc.input)
			if !reflect.DeepEqual(whole, tc.want) {
				t.Fatalf("whole: got %q want %q", whole, tc.want)
			}
			// The same bytes one at a time must read the same, so a read boundary anywhere changes nothing.
			var each []string
			for i := range len(tc.input) {
				each = append(each, tc.input[i:i+1])
			}
			split := parseAll(each...)
			if !reflect.DeepEqual(split, tc.want) {
				t.Fatalf("byte by byte: got %q want %q", split, tc.want)
			}
		})
	}
}
