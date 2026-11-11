package sse

import (
	"reflect"
	"testing"
)

func parseAll(chunks ...string) []Event {
	var p Parser
	var out []Event
	for _, c := range chunks {
		p.Feed([]byte(c), func(e Event) bool { out = append(out, e); return true })
	}
	return out
}

func TestEventsAndDefaults(t *testing.T) {
	got := parseAll("event: connected\ndata: \"a\"\n\n: keep-alive\n\ndata: x\n\nevent: changed\ndata:\"b\"\n\n")
	want := []Event{{"connected", `"a"`}, {"message", "x"}, {"changed", `"b"`}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestLineEndingsAndMultilineData(t *testing.T) {
	got := parseAll("event: e\r\ndata: 1\rdata: 2\ndata\r\n\r\n")
	want := []Event{{"e", "1\n2\n"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestNoDataIsDroppedAndIncompleteIsDiscarded(t *testing.T) {
	if got := parseAll("event: changed\n\nid: 7\nretry: 9\n\n"); len(got) != 0 {
		t.Errorf("got %#v", got)
	}
	if got := parseAll("event: changed\ndata: x\n"); len(got) != 0 {
		t.Errorf("incomplete event emitted: %#v", got)
	}
}

func TestBOMIsSkippedOnlyAtTheStart(t *testing.T) {
	got := parseAll("\xEF\xBB\xBFdata: a\n\n\xEF\xBB\xBFdata: b\n\n")
	if len(got) != 1 || got[0].Data != "a" {
		t.Errorf("got %#v", got)
	}
	got = parseAll("\xEF\xBBdata: a\n\n")
	if len(got) != 0 {
		t.Errorf("a partial BOM is not a BOM: %#v", got)
	}
}

func TestEverySplitGivesTheSameEvents(t *testing.T) {
	stream := "\xEF\xBB\xBF: hi\r\nevent: connected\r\ndata: \"é\U0001F600\"\r\n\r\nevent: changed\rdata: a\rdata: b\r\r"
	want := parseAll(stream)
	if len(want) != 2 || want[0].Data != "\"é\U0001F600\"" || want[1].Data != "a\nb" {
		t.Fatalf("whole: %#v", want)
	}
	for i := 0; i <= len(stream); i++ {
		for j := i; j <= len(stream); j++ {
			if got := parseAll(stream[:i], stream[i:j], stream[j:]); !reflect.DeepEqual(got, want) {
				t.Fatalf("split at %d,%d: %#v", i, j, got)
			}
		}
	}
}

func TestInvalidUTF8IsReplaced(t *testing.T) {
	got := parseAll("data: a\xFFb\n\n")
	if len(got) != 1 || got[0].Data != "a�b" {
		t.Errorf("got %#v", got)
	}
}

func TestEmitCanStop(t *testing.T) {
	var p Parser
	n := 0
	ok := p.Feed([]byte("data: a\n\ndata: b\n\n"), func(Event) bool { n++; return false })
	if ok || n != 1 {
		t.Errorf("ok=%v n=%d", ok, n)
	}
}
