package toggleslash

import (
	"fmt"
	"testing"
)

type sseEvent struct{ name, data string }

func parseSSE(chunks ...string) []sseEvent {
	var out []sseEvent
	p := &sseParser{emit: func(name string, data []byte) { out = append(out, sseEvent{name, string(data)}) }}
	for _, c := range chunks {
		p.feed([]byte(c))
	}
	return out
}

func same(a, b []sseEvent) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func TestSSEParser(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"plain", []string{"event: changed\ndata: \"v2\"\n\n"}, []sseEvent{{"changed", `"v2"`}}},
		{"no space after the colon", []string{"event:changed\ndata:x\n\n"}, []sseEvent{{"changed", "x"}}},
		{"only one space is removed", []string{"data:  x\n\n"}, []sseEvent{{"message", " x"}}},
		{"default name is message", []string{"data: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"CRLF", []string{"event: a\r\ndata: x\r\n\r\n"}, []sseEvent{{"a", "x"}}},
		{"lone CR", []string{"event: a\rdata: x\r\r"}, []sseEvent{{"a", "x"}}},
		{"CR then LF split across chunks", []string{"event: a\r", "\ndata: x\r", "\n\r", "\n"}, []sseEvent{{"a", "x"}}},
		{"BOM on the first line", []string{"\xef\xbb\xbfevent: a\ndata: x\n\n"}, []sseEvent{{"a", "x"}}},
		{"BOM split across chunks", []string{"\xef", "\xbb", "\xbfdata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"comment lines", []string{": hello\nevent: a\n: again\ndata: x\n\n"}, []sseEvent{{"a", "x"}}},
		{"keep-alive alone emits nothing", []string{": keep-alive\n\n"}, nil},
		{"multi-line data joins with LF", []string{"data: a\ndata: b\ndata\n\n"}, []sseEvent{{"message", "a\nb\n"}}},
		{"an event with no data line is dropped", []string{"event: a\n\ndata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"an empty data line is still data", []string{"data:\n\n"}, []sseEvent{{"message", ""}}},
		{"unfinished event is discarded", []string{"event: a\ndata: x\n"}, nil},
		{"unknown fields are ignored", []string{"id: 5\nretry: 10\nfoo: bar\ndata: x\n\n"}, []sseEvent{{"message", "x"}}},
		{"name resets after an event", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"a split in the middle of a field name", []string{"eve", "nt: a\nda", "ta: x\n", "\n"}, []sseEvent{{"a", "x"}}},
		{"UTF-8 split in the data", []string{"data: \xc3", "\xa9\n\n"}, []sseEvent{{"message", "é"}}},
		{"a line with no colon is a field with an empty value", []string{"data\n\n"}, []sseEvent{{"message", ""}}},
		{"field names are case sensitive", []string{"DATA: x\n\n"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSSE(tc.chunks...); !same(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSSEEveryChunking feeds a stream one byte at a time and in every two-way split, and gets the same events.
func TestSSEEveryChunking(t *testing.T) {
	stream := "\xef\xbb\xbf: hi\r\nevent: connected\r\ndata: \"v1\"\r\n\r\nevent: changed\ndata: \"v2\"\n\ndata: a\rdata: b\r\r"
	want := parseSSE(stream)
	if len(want) != 3 {
		t.Fatalf("whole: %v", want)
	}
	var bytes []string
	for i := range len(stream) {
		bytes = append(bytes, stream[i:i+1])
	}
	if got := parseSSE(bytes...); !same(got, want) {
		t.Fatalf("byte by byte: %v", got)
	}
	for i := 0; i <= len(stream); i++ {
		if got := parseSSE(stream[:i], stream[i:]); !same(got, want) {
			t.Fatalf("split at %d: %v", i, got)
		}
	}
}
