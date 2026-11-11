package toggleslash

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const quietFlush = 24 * time.Hour

func fakeClockClient(t *testing.T, f *fakeServer, clk *fakeClock, opts ...Option) *Client {
	t.Helper()
	base := []Option{withClock(clk), withJitter(func() float64 { return 0 }), WithFlushInterval(quietFlush)}
	return newTestClient(t, f, append(base, opts...)...)
}

// step waits until the sync loop has asked for its nth wait (counting from 1), then moves the clock past it.
func step(t *testing.T, clk *fakeClock, n int) []time.Duration {
	t.Helper()
	eventually(t, 5*time.Second, "the sync loop to wait", func() bool { return len(clk.waits()) >= n })
	waits := clk.waits()
	clk.Advance(waits[n-1])
	return waits[:n]
}

func TestSSEParserSplitsAnywhere(t *testing.T) {
	stream := "\xef\xbb\xbf: hello\r\nevent: changed\r\ndata: \"v2\"\r\n\r\nevent: connected\ndata: a\ndata: b\n\ndata: bare\r\rid: 4\nretry: 9\nevent: changed\n\nevent: changed\ndata:  two spaces\n\nevent:x\ndata:y\n\n"
	want := []sseEvent{
		{"changed", `"v2"`},
		{"connected", "a\nb"},
		{"message", "bare"},
		{"changed", " two spaces"},
		{"x", "y"},
	}
	var whole sseParser
	if got := whole.feed([]byte(stream)); !reflect.DeepEqual(got, want) {
		t.Fatalf("whole: %q", got)
	}
	for size := 1; size < len(stream); size++ {
		var p sseParser
		var got []sseEvent
		for i := 0; i < len(stream); i += size {
			end := min(i+size, len(stream))
			got = append(got, p.feed([]byte(stream[i:end]))...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chunks of %d: %q", size, got)
		}
	}
}

func TestSSEParserBOMOnlyAtTheStart(t *testing.T) {
	var p sseParser
	// A BOM after the first byte is part of the field name, so the second line is an unknown field and makes no event.
	got := p.feed([]byte("data: a\n\n\xef\xbb\xbfdata: b\n\n"))
	if len(got) != 1 || got[0].data != "a" {
		t.Fatalf("%q", got)
	}
}

func TestSSEParserLossyUTF8AndLoneCR(t *testing.T) {
	var p sseParser
	got := p.feed([]byte("data: a\xffb\r\r"))
	if len(got) != 1 || got[0].data != "a�b" {
		t.Fatalf("%q", got)
	}
	if more := p.feed([]byte("\n")); len(more) != 0 {
		t.Fatalf("a LF after a CR CR made an event: %q", more)
	}
}

func TestStreamConnectsAndFollowsAChange(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	st := f.seen("stream")
	if len(st) != 1 || st[0].header.Get("Accept") != "text/event-stream" || st[0].header.Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("stream request = %+v", st)
	}
	if len(f.seen("ruleset")) != 1 {
		t.Errorf("the connected tag equal to the held one fetched again: %d", len(f.seen("ruleset")))
	}
	f.setRuleset(strings.Replace(twoFlags, `"fallthrough":{"variation":1}`, `"fallthrough":{"variation":0}`, 1), `"v2"`)
	f.publishTag()
	eventually(t, 3*time.Second, "the new ruleset", func() bool { return c.Status().ETag == `"v2"` })
	if c.BoolVariation("checkout", user, true) {
		t.Error("still serving the old ruleset")
	}
	if len(f.seen("stream")) != 1 {
		t.Error("the stream was reopened")
	}
}

func TestAnAnnouncedTagThatMatchesFetchesNothingButSyncs(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := c.Status().LastSyncAt
	clk.Advance(5 * time.Second)
	f.publishTag()
	eventually(t, 3*time.Second, "lastSyncAt to move", func() bool { return c.Status().LastSyncAt >= before+5000 })
	if got := c.Status().LastSyncAt; got != before+5000 {
		t.Errorf("lastSyncAt = %d, want %d", got, before+5000)
	}
	if len(f.seen("ruleset")) != 1 {
		t.Errorf("ruleset requests = %d", len(f.seen("ruleset")))
	}
}

func TestEventsOtherThanChangedAndConnectedAreIgnored(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	f.setRuleset(twoFlags, `"v9"`)
	f.publish("event: ping\ndata: \"v9\"\n\ndata: \"v9\"\n\n: \"v9\"\n\n")
	time.Sleep(100 * time.Millisecond)
	if len(f.seen("ruleset")) != 1 || c.Status().ETag != `"v1"` {
		t.Errorf("an unknown event fetched: %d", len(f.seen("ruleset")))
	}
}

func TestAFetchThatFailsAfterAnAnnouncementEndsTheStream(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithPollInterval(30*time.Second))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	f.script("ruleset", answer{status: 500})
	f.setRuleset(twoFlags, `"v2"`)
	f.publishTag()
	eventually(t, 3*time.Second, "the stream to end", func() bool { return c.Status().Stream == StreamDisconnected })
	if got := c.Status().LastError; got != "http_500" {
		t.Errorf("lastError = %q", got)
	}
	// Not connected, so failures counts it: 2s ceiling, jitter 0 gives 1s.
	eventually(t, 3*time.Second, "a wait", func() bool { return len(clk.waits()) >= 1 })
	if got := clk.waits()[0]; got != time.Second {
		t.Errorf("first wait = %s", got)
	}
	clk.Advance(time.Second)
	eventually(t, 3*time.Second, "the poll to load v2", func() bool { return c.Status().ETag == `"v2"` })
}

func TestBackoffSchedule(t *testing.T) {
	cases := []struct {
		name   string
		jitter float64
		want   []time.Duration
	}{
		{"low jitter", 0, []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second}},
		{"high jitter", 1, []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer(t)
			f.script("stream", answer{status: 503}, answer{status: 503}, answer{status: 503}, answer{status: 503}, answer{status: 503}, answer{status: 503})
			clk := newFakeClock()
			jit := tc.jitter
			c := fakeClockClient(t, f, clk, WithPollInterval(30*time.Second), withJitter(func() float64 { return jit }))
			c.Ready()
			var waits []time.Duration
			for i := 1; i <= len(tc.want); i++ {
				waits = step(t, clk, i)
			}
			if !reflect.DeepEqual(waits, tc.want) {
				t.Errorf("waits = %v, want %v", waits, tc.want)
			}
		})
	}
}

func TestAConnectedStreamResetsTheBackoff(t *testing.T) {
	f := newFakeServer(t)
	f.script("stream", answer{status: 503}, answer{status: 503}, answer{sse: []string{"event: connected\ndata: \"v1\"\n\n"}, endStream: true}, answer{status: 503})
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithPollInterval(30*time.Second))
	c.Ready()
	for i := 1; i <= 3; i++ {
		step(t, clk, i)
	}
	eventually(t, 3*time.Second, "the fourth wait", func() bool { return len(clk.waits()) >= 4 })
	waits := clk.waits()[:4]
	// 1s, 2s (two failures), then a stream that connected and ended: failures resets to 0, ceiling 1s, jitter 0 gives 0.5s,
	// then one more failure: 1s.
	want := []time.Duration{time.Second, 2 * time.Second, 500 * time.Millisecond, time.Second}
	if !reflect.DeepEqual(waits, want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	if len(f.seen("ruleset")) != 3 {
		t.Errorf("a stream that was up was followed by a poll: ruleset requests = %d", len(f.seen("ruleset")))
	}
}

func TestPollingOnlyWaitsThePollIntervalEveryTime(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithStream(false), WithPollInterval(7*time.Second))
	c.Ready()
	var waits []time.Duration
	for i := 1; i <= 3; i++ {
		waits = step(t, clk, i)
	}
	if !reflect.DeepEqual(waits, []time.Duration{7 * time.Second, 7 * time.Second, 7 * time.Second}) {
		t.Errorf("waits = %v", waits)
	}
}

func TestRetryAfterOnTheRulesetHoldsTheNextRequest(t *testing.T) {
	cases := []struct {
		name   string
		header string
		status int
		want   time.Duration // zero: no hold
	}{
		{"seconds", "120", 429, 120 * time.Second},
		{"padded", " 30 ", 429, 30 * time.Second},
		{"capped", "99999", 429, 300 * time.Second},
		{"huge", "99999999999999999999999", 429, 300 * time.Second},
		{"zero", "0", 429, 0},
		{"a date", "Wed, 21 Oct 2026 07:28:00 GMT", 429, 0},
		{"negative", "-5", 429, 0},
		{"fraction", "1.5", 429, 0},
		{"on a 503", "120", 503, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer(t)
			f.script("ruleset", answer{status: tc.status, header: map[string]string{"Retry-After": tc.header}})
			clk := newFakeClock()
			c := fakeClockClient(t, f, clk, WithStream(false), WithPollInterval(10*time.Second))
			c.Ready()
			eventually(t, 3*time.Second, "a wait", func() bool { return len(clk.waits()) >= 1 })
			got := clk.waits()[0]
			want := tc.want
			if want == 0 {
				want = 10 * time.Second
			}
			if got != want {
				t.Errorf("first wait = %s, want %s", got, want)
			}
		})
	}
}

func TestRetryAfterOnTheRulesetStopsTheStreamBeingAskedFirst(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{status: 429, header: map[string]string{"Retry-After": "45"}})
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithPollInterval(30*time.Second))
	c.Ready()
	eventually(t, 3*time.Second, "a wait", func() bool { return len(clk.waits()) >= 1 })
	if got := clk.waits()[0]; got != 45*time.Second {
		t.Errorf("first wait = %s", got)
	}
	if n := len(f.seen("stream")); n != 0 {
		t.Errorf("the stream was opened inside the hold: %d", n)
	}
	clk.Advance(45 * time.Second)
	eventually(t, 3*time.Second, "the stream", func() bool { return len(f.seen("stream")) == 1 })
}

func TestRetryAfterOnTheStreamIsHonoured(t *testing.T) {
	f := newFakeServer(t)
	f.script("stream", answer{status: 429, header: map[string]string{"Retry-After": "99999"}})
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithPollInterval(30*time.Second))
	c.Ready()
	waits := step(t, clk, 1)
	if waits[0] != 300*time.Second {
		t.Errorf("first wait = %s", waits[0])
	}
}

func TestAShorterRetryAfterNeverShortensALongerHold(t *testing.T) {
	var h hold
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.extend(now.Add(100 * time.Second))
	h.extend(now.Add(10 * time.Second))
	if got := h.remaining(now); got != 100*time.Second {
		t.Errorf("remaining = %s", got)
	}
	if got := h.remaining(now.Add(200 * time.Second)); got != 0 {
		t.Errorf("remaining after = %s", got)
	}
}

func TestAQuietStreamIsDroppedByTheWatchdog(t *testing.T) {
	f := newFakeServer(t)
	f.script("stream", answer{sse: []string{"event: connected\ndata: \"v1\"\n\n"}, holdStream: true})
	c := newTestClient(t, f, WithPollInterval(time.Hour), withWatchdog(200*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	eventually(t, 3*time.Second, "the watchdog", func() bool { return c.Status().LastError == SyncTimeout })
	if got := c.Status().Stream; got != StreamDisconnected {
		t.Errorf("stream = %q", got)
	}
}

func TestKeepAlivesKeepTheStreamUp(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithPollInterval(time.Hour), withWatchdog(300*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		f.publish(": keep-alive\n\n")
		time.Sleep(100 * time.Millisecond)
	}
	if st := c.Status(); st.Stream != StreamConnected || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
	if len(f.seen("stream")) != 1 {
		t.Errorf("stream requests = %d", len(f.seen("stream")))
	}
}

func TestAStreamThatNeverOpensTimesOut(t *testing.T) {
	f := newFakeServer(t)
	f.script("stream", answer{hang: true})
	c := newTestClient(t, f, WithRequestTimeout(200*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "the open to time out", func() bool { return c.Status().LastError == SyncTimeout })
}

func TestACleanEndIsNotAnError(t *testing.T) {
	f := newFakeServer(t)
	f.script("stream", answer{sse: []string{"event: connected\ndata: \"v1\"\n\n"}, endStream: true})
	errs := &errorLog{}
	clk := newFakeClock()
	c := fakeClockClient(t, f, clk, WithOnError(errs.add), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "the stream to end", func() bool {
		return c.Status().Stream == StreamDisconnected && len(clk.waits()) >= 1
	})
	if errs.count() != 0 || c.Status().LastError != "" {
		t.Errorf("errors = %v, lastError = %q", errs.messages(), c.Status().LastError)
	}
}
