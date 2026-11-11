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
	ev := func(name, data string) sseEvent { return sseEvent{name: name, data: data} }
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"lf", []string{"event: changed\ndata: \"a\"\n\n"}, []sseEvent{ev("changed", `"a"`)}},
		{"crlf", []string{"event: changed\r\ndata: x\r\n\r\n"}, []sseEvent{ev("changed", "x")}},
		{"lone cr", []string{"event: changed\rdata: x\r\r"}, []sseEvent{ev("changed", "x")}},
		{"cr then lf split across reads", []string{"event: changed\r", "\ndata: x\r", "\n\r", "\n"}, []sseEvent{ev("changed", "x")}},
		{"comment ignored", []string{": keep-alive\n\n", "event: connected\ndata: t\n\n"}, []sseEvent{ev("connected", "t")}},
		{"comment inside an event", []string{"event: changed\n: note\ndata: x\n\n"}, []sseEvent{ev("changed", "x")}},
		{"several data lines join with lf", []string{"data: a\ndata: b\ndata\n\n"}, []sseEvent{ev("message", "a\nb\n")}},
		{"no data line drops the event", []string{"event: changed\n\n", "data: ok\n\n"}, []sseEvent{ev("message", "ok")}},
		{"empty data line dispatches", []string{"event: changed\ndata:\n\n"}, []sseEvent{ev("changed", "")}},
		{"name defaults to message", []string{"data: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"one space is stripped, a second is kept", []string{"data:  x\n\n"}, []sseEvent{ev("message", " x")}},
		{"no space after the colon", []string{"data:x\n\n"}, []sseEvent{ev("message", "x")}},
		{"unfinished event is discarded", []string{"event: changed\ndata: x\n"}, nil},
		{"id and retry are ignored", []string{"id: 1\nretry: 5\ndata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"event name resets after dispatch", []string{"event: changed\ndata: a\n\ndata: b\n\n"}, []sseEvent{ev("changed", "a"), ev("message", "b")}},
		{"bom at the start", []string{"\xef\xbb\xbfdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"bom split across reads", []string{"\xef\xbb", "\xbfdata: x\n\n"}, []sseEvent{ev("message", "x")}},
		{"utf-8 split across reads", []string{"data: \xf0\x9f", "\x98\x80\n\n"}, []sseEvent{ev("message", "\U0001F600")}},
		{"invalid utf-8 is repaired", []string{"data: \xff\xe2\x82\n\n"}, []sseEvent{ev("message", "��")}},
		{"byte at a time", []string{"d", "a", "t", "a", ":", " ", "x", "\n", "\n"}, []sseEvent{ev("message", "x")}},
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
