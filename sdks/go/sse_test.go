package toggleslash

import (
	"reflect"
	"testing"
)

type sseEvent struct{ name, data string }

func parseChunks(chunks ...string) []sseEvent {
	var p sseParser
	var out []sseEvent
	for _, c := range chunks {
		p.feed([]byte(c), func(name, data string) { out = append(out, sseEvent{name, data}) })
	}
	return out
}

func TestSSEParser(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []sseEvent
	}{
		{"lf", []string{"event: changed\ndata: \"v2\"\n\n"}, []sseEvent{{"changed", `"v2"`}}},
		{"crlf", []string{"event: changed\r\ndata: x\r\n\r\n"}, []sseEvent{{"changed", "x"}}},
		{"cr", []string{"event: changed\rdata: x\r\r"}, []sseEvent{{"changed", "x"}}},
		{"bom", []string{"\xEF\xBB\xBFevent: connected\ndata: t\n\n"}, []sseEvent{{"connected", "t"}}},
		{"comment", []string{": keep alive\n\nevent: changed\ndata: t\n\n"}, []sseEvent{{"changed", "t"}}},
		{"no data, no event", []string{"event: changed\n\n"}, nil},
		{"multi data", []string{"event: changed\ndata: a\ndata: b\n\n"}, []sseEvent{{"changed", "a\nb"}}},
		{"split anywhere", []string{"eve", "nt: chan", "ged\nda", "ta: v\n", "\n"}, []sseEvent{{"changed", "v"}}},
		{"split crlf", []string{"event: changed\r", "\ndata: v\r", "\n\r", "\n"}, []sseEvent{{"changed", "v"}}},
		{"no space after colon", []string{"event:changed\ndata:v\n\n"}, []sseEvent{{"changed", "v"}}},
		{"default name", []string{"data: v\n\n"}, []sseEvent{{"message", "v"}}},
		{"name does not carry over", []string{"event: a\ndata: 1\n\ndata: 2\n\n"}, []sseEvent{{"a", "1"}, {"message", "2"}}},
		{"unknown field", []string{"id: 5\nretry: 10\nevent: changed\ndata: v\n\n"}, []sseEvent{{"changed", "v"}}},
		{"incomplete is dropped", []string{"event: changed\ndata: v\n"}, nil},
	}
	for _, tc := range cases {
		got := parseChunks(tc.chunks...)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
