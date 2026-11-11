package sse

import (
	"slices"
	"testing"
)

func parseAll(chunks ...string) []Event {
	var p Parser
	var out []Event
	for _, c := range chunks {
		out = append(out, p.Write([]byte(c))...)
	}
	return out
}

func TestEvents(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []Event
	}{
		{"named", []string{"event: connected\ndata: abc\n\n"}, []Event{{"connected", "abc"}}},
		{"default name", []string{"data: x\n\n"}, []Event{{"message", "x"}}},
		{"one space dropped, no more", []string{"data:  two\n\n"}, []Event{{"message", " two"}}},
		{"no space", []string{"data:x\n\n"}, []Event{{"message", "x"}}},
		{"data lines joined with LF", []string{"data: a\ndata: b\n\n"}, []Event{{"message", "a\nb"}}},
		{"comment ignored", []string{": ping\n\ndata: x\n\n"}, []Event{{"message", "x"}}},
		{"no data, no event", []string{"event: changed\n\n"}, nil},
		{"CRLF", []string{"event: changed\r\ndata: t\r\n\r\n"}, []Event{{"changed", "t"}}},
		{"lone CR", []string{"event: changed\rdata: t\r\r", ": end\n"}, []Event{{"changed", "t"}}},
		{"split anywhere", []string{"ev", "ent: chan", "ged\nda", "ta: ", "t\n", "\n"}, []Event{{"changed", "t"}}},
		{"CR then LF in two chunks", []string{"data: a\r", "\ndata: b\r\n\r", "\n"}, []Event{{"message", "a\nb"}}},
		{"BOM at the start", []string{"\xEF\xBB\xBFdata: x\n\n"}, []Event{{"message", "x"}}},
		{"BOM split", []string{"\xEF", "\xBB", "\xBFdata: x\n\n"}, []Event{{"message", "x"}}},
		{"incomplete event never dispatched", []string{"event: changed\ndata: t\n"}, nil},
		{"name resets between events", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []Event{{"a", "1"}, {"message", "2"}}},
		{"field without colon", []string{"data\n\n"}, []Event{{"message", ""}}},
		{"unknown field", []string{"id: 5\nretry: 10\ndata: x\n\n"}, []Event{{"message", "x"}}},
		{"invalid UTF-8 replaced", []string{"data: a\xFFb\n\n"}, []Event{{"message", "a�b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAll(tc.chunks...)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEveryByteBoundary(t *testing.T) {
	const doc = "\xEF\xBB\xBF: hi\r\nevent: connected\r\ndata: \"v1\"\r\n\r\nevent: changed\rdata: \"v2\"\r\r: end\n"
	want := parseAll(doc)
	if len(want) != 2 {
		t.Fatalf("whole: %q", want)
	}
	for i := 0; i <= len(doc); i++ {
		got := parseAll(doc[:i], doc[i:])
		if !slices.Equal(got, want) {
			t.Fatalf("split at %d: got %q", i, got)
		}
	}
	var p Parser
	var got []Event
	for i := range len(doc) {
		got = append(got, p.Write([]byte{doc[i]})...)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("one byte at a time: %q", got)
	}
}
