package toggleslash

import "testing"

// eventsNamed runs the parser over chunks and names what it produced, so a test can read the result at a glance.
func eventsNamed(parser *sseParser, chunks ...string) []string {
	var out []string
	for _, chunk := range chunks {
		for _, event := range parser.feed([]byte(chunk)) {
			out = append(out, event.name+"="+event.data)
		}
	}
	return out
}

func TestSSEReadsEventsAndSkipsComments(t *testing.T) {
	parser := &sseParser{}
	got := eventsNamed(parser, ": keep-alive\n\nevent: connected\ndata: \"v1\"\n\n")
	if len(got) != 1 || got[0] != `connected="v1"` {
		t.Errorf("read %v, expected the connected event only", got)
	}
}

func TestSSEHoldsAnEventUntilItsBlankLine(t *testing.T) {
	parser := &sseParser{}
	if got := eventsNamed(parser, "event: changed\ndata: \"v"); len(got) != 0 {
		t.Fatalf("an incomplete event was delivered: %v", got)
	}
	got := eventsNamed(parser, "2\"\n\n")
	if len(got) != 1 || got[0] != `changed="v2"` {
		t.Errorf("read %v, expected the finished event", got)
	}
}

func TestSSEAcceptsLFCRLFAndLoneCR(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		parser := &sseParser{}
		got := eventsNamed(parser, "data: one"+ending+"data: two"+ending+ending)
		if len(got) != 1 || got[0] != "message=one\ntwo" {
			t.Errorf("with %q read %v, expected the joined message", ending, got)
		}
	}
}

func TestSSEJoinsADividedCRLF(t *testing.T) {
	parser := &sseParser{}
	events := parser.feed([]byte("data: a\r"))
	events = append(events, parser.feed([]byte("\ndata: b\r\n\r\n"))...)
	if len(events) != 1 || events[0].data != "a\nb" {
		t.Errorf("read %v, expected the two data lines", events)
	}
}

func TestSSEDropsAnEventWithNoData(t *testing.T) {
	parser := &sseParser{}
	if got := eventsNamed(parser, "event: ping\n\n"); len(got) != 0 {
		t.Errorf("read %v, expected nothing", got)
	}
}

func TestSSEKeepsAUTF8CharacterSplitAcrossReads(t *testing.T) {
	parser := &sseParser{}
	// The check mark U+2713 is E2 9C 93, cut between the second and third byte.
	events := parser.feed([]byte("data: \xe2\x9c"))
	events = append(events, parser.feed([]byte("\x93\n\n"))...)
	if len(events) != 1 || events[0].data != "\u2713" {
		t.Errorf("read %v, expected the check mark", events)
	}
}

func TestSSEDropsAnIncompleteEventAtTheEnd(t *testing.T) {
	parser := &sseParser{}
	if got := eventsNamed(parser, "data: never ended\n"); len(got) != 0 {
		t.Errorf("read %v, expected an incomplete event to be dropped", got)
	}
}
