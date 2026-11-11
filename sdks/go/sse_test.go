package toggleslash

import (
	"fmt"
	"reflect"
	"testing"
)

func feedAll(chunks ...string) []sseEvent {
	var p sseParser
	var out []sseEvent
	for _, c := range chunks {
		out = append(out, p.Feed([]byte(c))...)
	}
	return out
}

func TestSSEParser(t *testing.T) {
	ev := func(name, data string) sseEvent { return sseEvent{Name: name, Data: data} }
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"one event", []string{"event: changed\ndata: \"2\"\n\n"}, []sseEvent{ev("changed", `"2"`)}},
		{"default name is message", []string{"data: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"empty event name is message", []string{"event:\ndata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"no space after the colon", []string{"data:x\n\n"}, []sseEvent{ev("message", "x")}},
		{"only one leading space is removed", []string{"data:  x\n\n"}, []sseEvent{ev("message", " x")}},
		{"data lines join with LF", []string{"data: a\ndata: b\ndata:\n\n"}, []sseEvent{ev("message", "a\nb\n")}},
		{"an empty data line is still data", []string{"data\n\n"}, []sseEvent{ev("message", "")}},
		{"no data, no event", []string{"event: changed\n\n"}, nil},
		{"comments are ignored", []string{": hello\ndata: x\n: more\n\n"}, []sseEvent{ev("message", "x")}},
		{"a comment alone dispatches nothing", []string{": keep-alive\n\n"}, nil},
		{"unknown fields are ignored", []string{"id: 7\nretry: 5\nfoo: bar\ndata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"CRLF", []string{"event: a\r\ndata: 1\r\n\r\n"}, []sseEvent{ev("a", "1")}},
		{"lone CR", []string{"event: a\rdata: 1\r\r"}, []sseEvent{ev("a", "1")}},
		{"CR then LF split across reads", []string{"data: 1\r", "\n\r", "\ndata: 2\n\n"}, []sseEvent{ev("message", "1"), ev("message", "2")}},
		{"byte order mark", []string{"\xEF\xBB\xBFdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"byte order mark split", []string{"\xEF", "\xBB", "\xBFdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"a start that is not a BOM is not data", []string{"\xEF", "data: x\n\n"}, nil},
		{"an incomplete event is discarded", []string{"event: a\ndata: 1\n"}, nil},
		{"an incomplete last line is discarded", []string{"data: 1\n\ndata: 2"}, []sseEvent{ev("message", "1")}},
		{"two events in one read", []string{"data: 1\n\ndata: 2\n\n"}, []sseEvent{ev("message", "1"), ev("message", "2")}},
		{"split anywhere", []string{"da", "ta: ", "1\n", "\n"}, []sseEvent{ev("message", "1")}},
		{"UTF-8 split across reads", []string{"data: \xC3", "\xA9\xF0\x9F", "\x98\x80\n\n"}, []sseEvent{ev("message", "é😀")}},
		{"invalid UTF-8 becomes U+FFFD", []string{"data: \xFF\n\n"}, []sseEvent{ev("message", "�")}},
		{"the name resets after an event", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []sseEvent{ev("a", "1"), ev("message", "2")}},
		{"a field name is case sensitive", []string{"DATA: 1\n\n"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedAll(tc.chunks...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", fmt.Sprint(got), fmt.Sprint(tc.want))
			}
		})
	}
}

// Every split of a stream into two reads gives the same events.
func TestSSEParserIsIndependentOfChunking(t *testing.T) {
	stream := "\xEF\xBB\xBF: hi\r\nevent: connected\r\ndata: \"é\"\r\r\ndata: a\ndata: b\n\nevent: x\n\n"
	want := feedAll(stream)
	if len(want) != 2 {
		t.Fatalf("whole = %v", want)
	}
	for i := 0; i <= len(stream); i++ {
		if got := feedAll(stream[:i], stream[i:]); !reflect.DeepEqual(got, want) {
			t.Fatalf("split at %d gave %v, want %v", i, got, want)
		}
	}
	var bytewise []sseEvent
	var p sseParser
	for i := range len(stream) {
		bytewise = append(bytewise, p.Feed([]byte{stream[i]})...)
	}
	if !reflect.DeepEqual(bytewise, want) {
		t.Errorf("byte by byte gave %v", bytewise)
	}
}
