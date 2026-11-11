package toggleslash

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// streamClient makes a client on a fake clock, following feed.
func streamClient(t *testing.T, f *fake, feed *sseFeed, opts ...Option) (*Client, *fakeClock) {
	t.Helper()
	fc := newFakeClock(epoch)
	f.set(&f.stream, feed.handler)
	opts = append([]Option{withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return 0.5 })}, opts...)
	return f.client(t, opts...), fc
}

func TestConnectedWithTheHeldTagMakesNoFetch(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	c, _ := streamClient(t, f, feed)
	feed.waitOpened(t)
	if c.Status().Stream != StreamConnecting {
		t.Fatalf("%+v", c.Status())
	}
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	feed.push(": keep-alive\n\nevent: changed\ndata: \"v1\"\n\n")
	feed.push("event: other\ndata: \"zzz\"\n\n")
	// Events are handled in order. A tag that is not the held one asks for a fetch, so when that fetch arrives
	// the two events before it have been handled, and neither made a request.
	feed.push("event: changed\ndata: \"v2\"\n\n")
	waitFor(t, "the fetch for v2", func() bool { return f.count("/sdk/v1/ruleset") == 2 })
}

func TestChangedFetchesAConditionalRuleset(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	var n atomic.Int32
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(rulesetJSON(flagJSON("f", `[false,true]`, 1, 1))))
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(rulesetJSON(flagJSON("f", `[false,true]`, 0, 2))))
	})
	c, _ := streamClient(t, f, feed)
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	if !c.BoolVariation("f", user, false) {
		t.Fatal("v1 serves true")
	}
	feed.push("event: changed\ndata: \"v2\"\n\n")
	waitFor(t, "v2", func() bool { return c.Status().ETag == `"v2"` })
	if c.BoolVariation("f", user, true) {
		t.Fatal("v2 serves false")
	}
	if got := f.requests("/sdk/v1/ruleset")[1].header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("If-None-Match %q", got)
	}
	if c.Status().Stream != StreamConnected {
		t.Fatalf("%+v", c.Status())
	}
}

func TestConnectedWithANewTagFetchesAndClearsTheError(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	c, _ := streamClient(t, f, feed)
	feed.waitOpened(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	feed.push("event: changed\ndata: \"other\"\n\n")
	waitFor(t, "the failed fetch", func() bool { return c.Status().LastError == "http_500" })
	if st := c.Status(); st.Stream == StreamConnected {
		t.Fatalf("a stream whose fetch failed is not connected: %+v", st)
	}
}

func TestFailedFetchEndsTheStream(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	var log errorLog
	c, fc := streamClient(t, f, feed, WithOnError(log.add))
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
	feed.push("event: changed\ndata: \"v2\"\n\n")
	waitFor(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	if c.Status().LastError != "http_502" {
		t.Fatalf("%+v", c.Status())
	}
	waitFor(t, "onError", func() bool { return log.count() == 1 })
	// Not connected: the wait is a backoff (failures = 1: ceiling 2 s, times 0.75), then a poll.
	fc.fireNext(t, "the backoff", exactly(1500*time.Millisecond))
	waitFor(t, "the poll", func() bool { return f.count("/sdk/v1/ruleset") == 3 })
}

func TestStreamStatusOnNon200(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	c := f.client(t, withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return 0 }))
	waitFor(t, "http_503", func() bool { return c.Status().LastError == "http_503" })
	if c.Status().Stream != StreamDisconnected {
		t.Fatalf("%+v", c.Status())
	}
	if !c.Ready() {
		t.Fatal("the ruleset loaded, so ready")
	}
}

func TestBackoffGrowsAndIsJittered(t *testing.T) {
	f := newFake(t) // the stream answers 503 every time
	fc := newFakeClock(epoch)
	c := f.client(t, withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return 0.5 }))
	_ = c
	want := []time.Duration{1500 * time.Millisecond, 3 * time.Second, 6 * time.Second, 12 * time.Second, 22500 * time.Millisecond, 22500 * time.Millisecond}
	for i, d := range want {
		fc.fireNext(t, "backoff "+strconv.Itoa(i), exactly(d))
		waitFor(t, "the next stream attempt", func() bool { return f.count("/sdk/v1/stream") == i+2 })
	}
}

func TestBackoffJitterBounds(t *testing.T) {
	for _, tc := range []struct {
		random float64
		want   time.Duration
	}{{0, time.Second}, {0.999999, 1999998 * time.Microsecond}} {
		f := newFake(t)
		fc := newFakeClock(epoch)
		// The flush timer is an hour, so the backoff is the only timer that is neither that nor the request's.
		f.client(t, WithFlushInterval(time.Hour), withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return tc.random }))
		near := func(d time.Duration) bool { return d >= tc.want-time.Millisecond && d <= tc.want+time.Millisecond }
		waitFor(t, "the first backoff", func() bool { return fc.everArmed(near) })
	}
}

func TestStreamOffWaitsThePollIntervalEveryTime(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	f.client(t, WithStream(false), WithPollInterval(45*time.Second), withClock(fc), WithRequestTimeout(testTimeout))
	for i := range 4 {
		fc.fireNext(t, "poll", exactly(45*time.Second))
		waitFor(t, "poll request", func() bool { return f.count("/sdk/v1/ruleset") == i+2 })
	}
}

func TestSuccessfulStreamResetsTheBackoff(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	c, fc := streamClient(t, f, feed)
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	feed.end <- struct{}{} // a clean end after connecting
	// failures resets to 0, then the first loop adds nothing: delay(0) is a 1 s ceiling, 0.75 of it.
	fc.fireNext(t, "reconnect", exactly(750*time.Millisecond))
	feed.waitOpened(t)
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Fatal("a stream that was up needs no poll")
	}
}

func TestIdleWatchdogOnTheFakeClock(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	c, fc := streamClient(t, f, feed)
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	waitFor(t, "the idle timer", func() bool { return fc.everArmed(exactly(60 * time.Second)) })
	armedBefore := countArmed(fc, 60*time.Second)
	feed.push(": ping\n\n") // a comment is a byte: it restarts the watchdog
	waitFor(t, "the watchdog re-armed", func() bool { return countArmed(fc, 60*time.Second) > armedBefore })
	if live := countLive(fc, 60*time.Second); live != 1 {
		t.Fatalf("%d idle timers live", live)
	}
	fc.fireNext(t, "the idle timer", exactly(60*time.Second))
	waitFor(t, "timeout", func() bool { return c.Status().LastError == LastErrorTimeout })
	waitFor(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
}

func countArmed(c *fakeClock, d time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, a := range c.armed {
		if a == d {
			n++
		}
	}
	return n
}

func countLive(c *fakeClock, d time.Duration) int {
	n := 0
	for _, l := range c.live() {
		if l == d {
			n++
		}
	}
	return n
}

func TestIdleWatchdogOnARealSocket(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	f.set(&f.stream, feed.handler)
	c := f.client(t, withIdle(300*time.Millisecond), WithPollInterval(time.Hour))
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	// Keep-alives restart the watchdog: three of them 200 ms apart outlast one 300 ms window.
	for i := range 3 {
		time.Sleep(200 * time.Millisecond) // pacing the keep-alives is the point of this loop
		feed.push(": ping\n\n")
		if c.Status().Stream != StreamConnected {
			t.Fatalf("dropped after %d keep-alives: %+v", i, c.Status())
		}
	}
	waitFor(t, "the quiet stream dropped", func() bool { return c.Status().Stream == StreamDisconnected })
	if c.Status().LastError != LastErrorTimeout {
		t.Fatalf("%+v", c.Status())
	}
}

func TestStreamOpenIsBoundedByTheRequestTimeout(t *testing.T) {
	f := newFake(t)
	f.set(&f.stream, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c := f.client(t, WithRequestTimeout(300*time.Millisecond), WithPollInterval(time.Hour))
	waitFor(t, "timeout", func() bool { return c.Status().LastError == LastErrorTimeout })
}

func TestStreamCleanEndIsNotAnError(t *testing.T) {
	f := newFake(t)
	var log errorLog
	feed := newFeed()
	c, _ := streamClient(t, f, feed, WithOnError(log.add))
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	feed.end <- struct{}{}
	waitFor(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	if c.Status().LastError != "" || log.count() != 0 {
		t.Fatalf("%+v %q", c.Status(), log.text())
	}
}

func TestIncompleteEventAtTheEndIsDiscarded(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	c, _ := streamClient(t, f, feed)
	feed.waitOpened(t)
	feed.push("event: changed\ndata: \"never-dispatched\"\n")
	feed.end <- struct{}{}
	waitFor(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Fatal("an incomplete event was acted on")
	}
}

func TestRetryAfterIsCappedAt300Seconds(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	f.set(&f.stream, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "100000")
		w.WriteHeader(429)
	})
	f.client(t, withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return 0.5 }))
	// The stream is held, the delay is 1.5 s, and the longer of the two is the wait.
	fc.fireNext(t, "the held wait", exactly(300*time.Second))
}

func TestRetryAfterOnTheRulesetIsCappedAt300Seconds(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "100000")
		w.WriteHeader(429)
	})
	// With the stream off the wait is the poll interval; the hold is longer, so the hold is the wait.
	f.client(t, WithStream(false), WithFlushInterval(time.Hour), withClock(fc), WithRequestTimeout(testTimeout))
	fc.fireNext(t, "the held wait", exactly(300*time.Second))
}

func TestRetryAfterShapes(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"0", 0, true}, {"1", time.Second, true}, {" 7 ", 7 * time.Second, true}, {"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true}, {"99999999999999999999999", 300 * time.Second, true},
		{"", 0, false}, {"1.5", 0, false}, {"-1", 0, false}, {"+1", 0, false}, {"1e3", 0, false}, {"abc", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false}, {"1 2", 0, false}, {"\t5", 0, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		h.Set("Retry-After", tc.value)
		got, ok := retryAfter(h)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got %v %v, want %v %v", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestOnly429ReadsRetryAfter(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	f.set(&f.stream, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "200")
		w.WriteHeader(503)
	})
	f.client(t, withClock(fc), WithRequestTimeout(testTimeout), withRandom(func() float64 { return 0.5 }))
	fc.fireNext(t, "the backoff", exactly(1500*time.Millisecond))
}

func TestAHoldNeverShrinks(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout))
	c.Ready()
	c.holdFor(100 * time.Second)
	c.holdFor(10 * time.Second)
	if got := c.holdWait(); got != 100*time.Second {
		t.Fatalf("%v", got)
	}
	c.holdFor(250 * time.Second)
	if got := c.holdWait(); got != 250*time.Second {
		t.Fatalf("%v", got)
	}
}

func TestFirstLoadIsNotHeld(t *testing.T) {
	// A hold begins with the first 429; nothing precedes the first load, so it is sent at once.
	f := newFake(t)
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if fc.everArmed(exactly(0)) {
		t.Fatal("slept before the first load")
	}
}
