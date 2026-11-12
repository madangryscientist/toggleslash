package toggleslash

import (
	"reflect"
	"strings"
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
	ev := func(name, data string) sseEvent { return sseEvent{name, data} }
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"lf", []string{"event: a\ndata: x\n\n"}, []sseEvent{ev("a", "x")}},
		{"crlf", []string{"event: a\r\ndata: x\r\n\r\n"}, []sseEvent{ev("a", "x")}},
		{"lone cr", []string{"event: a\rdata: x\r\r"}, []sseEvent{ev("a", "x")}},
		{"cr then lf split across reads", []string{"event: a\r", "\ndata: x\r", "\n\r", "\n"}, []sseEvent{ev("a", "x")}},
		{"byte by byte", strings.Split("event: a\ndata: x\n\n", ""), []sseEvent{ev("a", "x")}},
		{"default name", []string{"data: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"no data line is dropped", []string{"event: a\n\n"}, nil},
		{"empty data line counts", []string{"event: a\ndata:\n\n"}, []sseEvent{ev("a", "")}},
		{"multi data joined with LF", []string{"data: a\ndata: b\ndata\n\n"}, []sseEvent{ev("message", "a\nb\n")}},
		{"comment ignored", []string{": hi\ndata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"one leading space only", []string{"data:  x\n\n"}, []sseEvent{ev("message", " x")}},
		{"no space", []string{"data:x\n\n"}, []sseEvent{ev("message", "x")}},
		{"other fields ignored", []string{"id: 1\nretry: 5\ndata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"bom", []string{"\xef\xbb\xbfdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"bom split", []string{"\xef", "\xbb", "\xbfdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"not a bom", []string{"\xef\xbbdata: x\n\n"}, nil},
		{"incomplete event discarded", []string{"data: x\n"}, nil},
		{"utf8 split across reads", []string{"data: \xe2\x82", "\xac\n\n"}, []sseEvent{ev("message", "€")}},
		{"invalid utf8 becomes one U+FFFD per run", []string{"data: \xff\xfe\n\n"}, []sseEvent{ev("message", "��")}},
		{"two events", []string{"data: 1\n\ndata: 2\n\n"}, []sseEvent{ev("message", "1"), ev("message", "2")}},
		{"event name resets", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []sseEvent{ev("a", "1"), ev("message", "2")}},
		{"quotes kept", []string{"event: connected\ndata: \"v1\"\n\n"}, []sseEvent{ev("connected", `"v1"`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := feedAll(tc.chunks...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
