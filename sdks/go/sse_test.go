package toggleslash

import (
	"reflect"
	"testing"
)

func feedAll(chunks ...string) []sseEvent {
	var p sseParser
	var out []sseEvent
	for _, c := range chunks {
		out = append(out, p.feed([]byte(c))...)
	}
	return out
}

func TestSSEParser(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"one event", []string{"event: changed\ndata: \"a\"\n\n"}, []sseEvent{{"changed", `"a"`}}},
		{"CRLF", []string{"event: changed\r\ndata: x\r\n\r\n"}, []sseEvent{{"changed", "x"}}},
		{"bare CR", []string{"event: changed\rdata: x\r\r"}, []sseEvent{{"changed", "x"}}},
		{"CRLF split across reads", []string{"event: changed\r", "\ndata: x\r", "\n\r", "\n"}, []sseEvent{{"changed", "x"}}},
		{"split in a field", []string{"eve", "nt: chan", "ged\nda", "ta: x\n", "\n"}, []sseEvent{{"changed", "x"}}},
		{"default name", []string{"data: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"comment is ignored", []string{": keep-alive\n\nevent: changed\ndata: x\n\n"}, []sseEvent{{"changed", "x"}}},
		{"two data lines join with LF", []string{"data: a\ndata: b\n\n"}, []sseEvent{{"message", "a\nb"}}},
		{"no data, no event", []string{"event: changed\n\n"}, nil},
		{"one space after the colon is removed, no more", []string{"data:  x\n\n"}, []sseEvent{{"message", " x"}}},
		{"no space after the colon", []string{"data:x\n\n"}, []sseEvent{{"message", "x"}}},
		{"unknown field is ignored", []string{"id: 4\nretry: 10\ndata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"BOM at the start is dropped", []string{"\xef\xbb\xbfdata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"BOM split across reads", []string{"\xef", "\xbb", "\xbfdata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"name does not leak into the next event", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"an event not ended by a blank line is not dispatched", []string{"event: changed\ndata: x\n"}, nil},
		{"two events in one read", []string{"data: 1\n\ndata: 2\n\n"}, []sseEvent{{"message", "1"}, {"message", "2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedAll(tc.chunks...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
