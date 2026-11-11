package toggleslash

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// feedAll feeds the input in chunks of the given size and returns every event.
func feedAll(t *testing.T, input string, chunk int) []sseEvent {
	t.Helper()
	var p sseParser
	var out []sseEvent
	b := []byte(input)
	for len(b) > 0 {
		n := min(chunk, len(b))
		evs, err := p.feed(b[:n])
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
		out = append(out, evs...)
		b = b[n:]
	}
	return out
}

func TestSSEParser(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []sseEvent
	}{
		{"named event", "event: connected\ndata: \"v1\"\n\n", []sseEvent{{"connected", `"v1"`}}},
		{"default name", "data: x\n\n", []sseEvent{{"message", "x"}}},
		{"CR", "event: a\rdata: b\r\r", []sseEvent{{"a", "b"}}},
		{"CRLF", "event: a\r\ndata: b\r\n\r\n", []sseEvent{{"a", "b"}}},
		{"mixed endings", "event: a\r\ndata: b\n\r", []sseEvent{{"a", "b"}}},
		{"comment", ": hello\nevent: a\ndata: b\n\n", []sseEvent{{"a", "b"}}},
		{"a comment alone is nothing", ": ping\n\n", nil},
		{"two data lines", "data: a\ndata: b\n\n", []sseEvent{{"message", "a\nb"}}},
		{"no data, no event", "event: a\n\n", nil},
		{"only one leading space goes", "data:  two\n\n", []sseEvent{{"message", " two"}}},
		{"no space", "data:x\n\n", []sseEvent{{"message", "x"}}},
		{"no colon is a field with an empty value", "data\n\n", []sseEvent{{"message", ""}}},
		{"unknown fields", "id: 4\nretry: 10\nfoo: bar\ndata: x\n\n", []sseEvent{{"message", "x"}}},
		{"name resets", "event: a\ndata: 1\n\ndata: 2\n\n", []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"BOM on the first line", "\xEF\xBB\xBFdata: x\n\n", []sseEvent{{"message", "x"}}},
		{"BOM later is not skipped", "data: a\n\n\xEF\xBB\xBFdata: b\n\n", []sseEvent{{"message", "a"}}},
		{"incomplete event is discarded", "data: a\n\ndata: b", []sseEvent{{"message", "a"}}},
		{"invalid bytes", "data: a\xFFb\n\n", []sseEvent{{"message", "a�b"}}},
		{"a character", "data: \xC3\xA9\n\n", []sseEvent{{"message", "é"}}},
	}
	for _, tc := range cases {
		for _, chunk := range []int{1, 2, 3, 7, 1 << 20} {
			got := feedAll(t, tc.input, chunk)
			if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("%s (chunks of %d): got %v, want %v", tc.name, chunk, got, tc.want)
			}
		}
	}
}

func TestSSECRLFSplitAcrossReads(t *testing.T) {
	var p sseParser
	var all []sseEvent
	for _, chunk := range []string{"data: a\r", "\n\r", "\ndata: b\n\n"} {
		evs, err := p.feed([]byte(chunk))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, evs...)
	}
	want := []sseEvent{{"message", "a"}, {"message", "b"}}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("got %v, want %v", all, want)
	}
}

func TestSSEEventTooLarge(t *testing.T) {
	var p sseParser
	chunk := []byte(strings.Repeat("x", 64<<10))
	var err error
	for i := 0; i < 40 && err == nil; i++ {
		_, err = p.feed(chunk)
	}
	if !errors.Is(err, errEventTooLarge) {
		t.Fatalf("a line that never ends: err = %v", err)
	}
}
