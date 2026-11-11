package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

var midday = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestDelay(t *testing.T) {
	jitter := 0.0
	c := &Client{cfg: config{stream: true, pollInterval: 30 * time.Second, jitter: func() float64 { return jitter }}}
	cases := []struct {
		failures int
		jitter   float64
		want     time.Duration
	}{
		{0, 0, 500 * time.Millisecond},
		{0, 0.999999, time.Second - 1},
		{1, 0, time.Second},
		{2, 0, 2 * time.Second},
		{3, 0, 4 * time.Second},
		{4, 0, 8 * time.Second},
		{5, 0, 15 * time.Second},
		{6, 0, 15 * time.Second},
		{1000, 0, 15 * time.Second},
	}
	for _, tc := range cases {
		jitter = tc.jitter
		got := c.delay(tc.failures)
		if d := got - tc.want; d < -time.Microsecond || d > time.Microsecond {
			t.Errorf("delay(%d) with jitter %v = %v, want %v", tc.failures, tc.jitter, got, tc.want)
		}
	}
	c.cfg.stream = false
	if got := c.delay(7); got != 30*time.Second {
		t.Errorf("delay with the stream off = %v, want the poll interval", got)
	}
	c.cfg = config{stream: true, pollInterval: 500 * time.Millisecond, jitter: func() float64 { return 0 }}
	if got := c.delay(4); got != 250*time.Millisecond {
		t.Errorf("delay is capped by the poll interval: %v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"0", 0, true}, {"5", 5 * time.Second, true}, {" 12 ", 12 * time.Second, true},
		{"300", 300 * time.Second, true}, {"301", 300 * time.Second, true},
		{"99999999999999999999999999", 300 * time.Second, true},
		{"", 0, false}, {"1.5", 0, false}, {"-3", 0, false}, {"+3", 0, false}, {"3s", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false}, {"1e3", 0, false}, {"٣", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPollsWithTheStreamOffAndSendsIfNoneMatch(t *testing.T) {
	f := newFakeServer(t)
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Second),
		WithFlushInterval(bigInterval), withClock(clock))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if got := c.Status(); got.Stream != StreamOff || got.ETag != `"1"` || !got.LastSyncAt.Equal(midday) {
		t.Fatalf("status after the first load = %+v", got)
	}
	first := f.seen("/sdk/v1/ruleset")
	if len(first) != 1 || first[0].Header.Get("If-None-Match") != "" {
		t.Fatalf("the first request carried If-None-Match: %+v", first)
	}
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/ruleset", 2)
	if got := f.seen("/sdk/v1/ruleset")[1].Header.Get("If-None-Match"); got != `"1"` {
		t.Errorf("If-None-Match = %q, want the etag as received, quotes included", got)
	}
	eventually(t, "the 304 to count as a sync", func() bool {
		return c.Status().LastSyncAt.Equal(midday.Add(30 * time.Second))
	})
	if f.count("/sdk/v1/stream") != 0 {
		t.Error("a stream was opened with the stream off")
	}
}

func TestA304WithNothingHeldIsAnError(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotModified) })
	var errs errorLog
	c := newTestClient(t, f, quiet(WithOnError(errs.add))...)
	if c.Ready() || c.Status().LastError != "http_304" || errs.count() != 1 {
		t.Errorf("ready %v, lastError %q, errors %v", c.Ready(), c.Status().LastError, errs.messages())
	}
}

func TestBadRulesetNeverReplacesTheOneHeld(t *testing.T) {
	f := newFakeServer(t)
	var calls atomic.Int32
	bodies := []string{`{}`, `[]`, `{"flags":{},"segments":[]}`, `{"flags":[],"segments":3}`, `not json`, `{"flags":[`}
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		if n == 1 {
			w.Header().Set("ETag", `"1"`)
			_, _ = io.WriteString(w, defaultRuleset(1))
			return
		}
		_, _ = io.WriteString(w, bodies[(n-2)%len(bodies)])
	})
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Second), WithFlushInterval(bigInterval), withClock(clock))
	c.Ready()
	for i := range bodies {
		clock.awaitTimer(t, time.Second)
		clock.advance(time.Second)
		f.await("/sdk/v1/ruleset", i+2)
		eventually(t, "bad_ruleset", func() bool { return c.Status().LastError == SyncBadRuleset })
		if c.Status().ETag != `"1"` || !c.BoolVariation("f", user("u"), false) {
			t.Fatalf("body %q replaced the ruleset", bodies[i])
		}
		// Let the failed attempt finish its wait setup before the next round.
		eventually(t, "the next wait", func() bool { return len(clock.armed()) > 0 })
	}
}

func TestEveryRequestCarriesTheKey(t *testing.T) {
	f := newFakeServer(t)
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithPollInterval(30*time.Second), WithFlushInterval(time.Minute), withClock(clock))
	c.Ready()
	f.await("/sdk/v1/stream", 1)
	c.BoolVariation("f", user("u"), false)
	clock.awaitTimer(t, time.Minute)
	clock.advance(time.Minute)
	f.await("/sdk/v1/events", 1)
	for _, path := range []string{"/sdk/v1/ruleset", "/sdk/v1/stream", "/sdk/v1/events"} {
		for _, r := range f.seen(path) {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("%s Authorization = %q", path, r.Header.Get("Authorization"))
			}
		}
	}
	if got := f.seen("/sdk/v1/stream")[0].Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("stream Accept = %q", got)
	}
	if got := f.seen("/sdk/v1/events")[0].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("events Content-Type = %q", got)
	}
}

func TestTrailingSlashesAreRemovedFromTheURL(t *testing.T) {
	f := newFakeServer(t)
	c := NewClient(f.url()+"///", "k", quiet()...)
	defer c.Close()
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if f.seen("/sdk/v1/ruleset")[0].Path != "/sdk/v1/ruleset" {
		t.Error("path was not joined cleanly")
	}
}

func TestAnUnusableURLIsANetworkErrorNotAPanic(t *testing.T) {
	for _, url := range []string{"", "not a url", "http://[::1", "ftp://x", "http://127.0.0.1:1"} {
		c := NewClient(url, "k", quiet()...)
		if c.Ready() || c.Status().LastError != SyncNetwork {
			t.Errorf("url %q: ready %v, lastError %q", url, c.Ready(), c.Status().LastError)
		}
		c.Close()
	}
}

func TestStreamConnectedFollowsChangesAndPollsNever(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithPollInterval(time.Hour), WithFlushInterval(bigInterval), WithRequestTimeout(5*time.Second))
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	if c.Status().LastError != "" {
		t.Errorf("lastError = %q", c.Status().LastError)
	}
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Errorf("the tag held was refetched: %d ruleset requests", f.count("/sdk/v1/ruleset"))
	}
	f.handle("/sdk/v1/ruleset", f.rulesetVersion(2))
	f.feed <- ": keep-alive\n\nevent: changed\ndata: \"2\"\n\n"
	eventually(t, "the changed ruleset", func() bool { return c.Status().ETag == `"2"` })
	if f.count("/sdk/v1/ruleset") != 2 {
		t.Errorf("ruleset requests = %d, want 2", f.count("/sdk/v1/ruleset"))
	}
	before := c.Status().LastSyncAt
	// The same tag again is a sync and fetches nothing.
	f.feed <- "event: changed\ndata: \"2\"\n\n"
	eventually(t, "a sync from a tag held", func() bool { return c.Status().LastSyncAt.After(before) })
	if f.count("/sdk/v1/ruleset") != 2 {
		t.Errorf("a tag already held fetched the ruleset again")
	}
	// Other events and data-less events are ignored.
	f.feed <- "event: other\ndata: \"3\"\n\nevent: changed\n\n"
	time.Sleep(50 * time.Millisecond)
	if c.Status().ETag != `"2"` || f.count("/sdk/v1/ruleset") != 2 {
		t.Error("an event that is not connected or changed was acted on")
	}
}

func TestAFailedFetchEndsTheStreamAndCountsAsNotConnected(t *testing.T) {
	f := newFakeServer(t)
	var calls atomic.Int32
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("ETag", `"2"`)
			_, _ = io.WriteString(w, defaultRuleset(2))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	// The stream announces a tag the client does not hold, and the fetch it asks for fails.
	f.handle("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: connected\ndata: \"9\"\n\n")
		w.(http.Flusher).Flush()
		f.hang(w, r)
	})
	clock := newFakeClock(midday)
	var errs errorLog
	c := newTestClient(t, f, WithPollInterval(30*time.Second), WithFlushInterval(bigInterval),
		withClock(clock), withJitter(func() float64 { return 0 }), WithOnError(errs.add))
	c.Ready()
	f.await("/sdk/v1/ruleset", 2)
	eventually(t, "lastError http_500", func() bool { return c.Status().LastError == "http_500" })
	// Not connected, failures 1: ceiling 2 s, jitter 0 gives 1 s.
	clock.awaitTimer(t, time.Second)
	if got := c.Status().Stream; got != StreamDisconnected {
		t.Errorf("stream = %q, want disconnected", got)
	}
	clock.advance(time.Second)
	f.await("/sdk/v1/ruleset", 3) // the poll
	f.await("/sdk/v1/stream", 2)  // then the stream again
	if errs.count() < 2 {
		t.Errorf("errors reported: %v", errs.messages())
	}
}

func TestAStreamThatWasUpAndDroppedReconnectsWithoutAPoll(t *testing.T) {
	f := newFakeServer(t)
	var streams atomic.Int32
	f.handle("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		if streams.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event: connected\ndata: \"1\"\n\n")
			return // the server ends the stream cleanly
		}
		f.defaultStream(w, r)
	})
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithPollInterval(30*time.Second), WithFlushInterval(bigInterval),
		withClock(clock), withJitter(func() float64 { return 0 }))
	c.Ready()
	// failures 0: ceiling 1 s, jitter 0 gives 0.5 s.
	clock.awaitTimer(t, 500*time.Millisecond)
	if c.Status().LastError != "" {
		t.Errorf("a stream that ended cleanly set lastError %q", c.Status().LastError)
	}
	clock.advance(500 * time.Millisecond)
	f.await("/sdk/v1/stream", 2)
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Errorf("polled after a stream that was up: %d ruleset requests", f.count("/sdk/v1/ruleset"))
	}
}

func TestAStreamRefusedWithAStatusIsAnHTTPError(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/stream", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	var errs errorLog
	c := newTestClient(t, f, WithPollInterval(time.Hour), WithFlushInterval(bigInterval), WithOnError(errs.add))
	c.Ready()
	eventually(t, "http_403", func() bool { return c.Status().LastError == "http_403" })
	if got := c.Status().Stream; got != StreamDisconnected && got != StreamConnecting {
		t.Errorf("stream = %q", got)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	f := newFakeServer(t)
	other := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.url()+"/sdk/v1/ruleset", http.StatusFound)
	})
	c := newTestClient(t, f, quiet()...)
	if c.Ready() || c.Status().LastError != "http_302" {
		t.Errorf("ready %v, lastError %q", c.Ready(), c.Status().LastError)
	}
	if other.count("/sdk/v1/ruleset") != 0 {
		t.Error("the redirect was followed, and the key went with it")
	}
}

// The stream is dropped as a timeout when a real socket stays quiet for longer than the watchdog, and a
// keep-alive restarts the time.
func TestWatchdogDropsAQuietSocketAndAKeepAliveRestartsIt(t *testing.T) {
	f := newFakeServer(t)
	watchdog := 400 * time.Millisecond
	var startedAt atomic.Int64
	f.handle("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		startedAt.CompareAndSwap(0, time.Now().UnixNano())
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: connected\ndata: \"1\"\n\n")
		w.(http.Flusher).Flush()
		for range 6 { // 600 ms of keep-alives, longer than the watchdog
			time.Sleep(100 * time.Millisecond)
			_, _ = io.WriteString(w, ": keep-alive\n\n")
			w.(http.Flusher).Flush()
		}
		f.hang(w, r) // then quiet, with the socket open
	})
	var errs errorLog
	c := newTestClient(t, f, WithPollInterval(time.Hour), WithFlushInterval(bigInterval),
		withWatchdog(watchdog), WithOnError(errs.add))
	c.Ready()
	eventually(t, "the watchdog to drop the stream", func() bool { return c.Status().LastError == SyncTimeout })
	elapsed := time.Since(time.Unix(0, startedAt.Load()))
	if elapsed < 600*time.Millisecond {
		t.Errorf("dropped after %v: the keep-alives did not restart the watchdog (%v)", elapsed, watchdog)
	}
	if got := errs.messages(); len(got) == 0 || got[0] != "GET /sdk/v1/stream timed out" {
		t.Errorf("errors = %v", got)
	}
}

func TestAStreamThatDoesNotOpenInTimeIsATimeout(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/stream", f.hang)
	c := newTestClient(t, f, WithPollInterval(time.Hour), WithFlushInterval(bigInterval), WithRequestTimeout(200*time.Millisecond))
	c.Ready()
	eventually(t, "timeout", func() bool { return c.Status().LastError == SyncTimeout })
}

func TestARulesetBodyThatNeverEndsIsATimeout(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"flags":[`)
		w.(http.Flusher).Flush()
		f.hang(w, r)
	})
	c := newTestClient(t, f, quiet(WithRequestTimeout(250*time.Millisecond))...)
	start := time.Now()
	if c.Ready() || c.Status().LastError != SyncTimeout || time.Since(start) > 3*time.Second {
		t.Errorf("ready %v, lastError %q after %v", c.Ready(), c.Status().LastError, time.Since(start))
	}
}

// A Retry-After above 300 holds for exactly 300 s, on each of the three routes.
func TestRetryAfterIsCappedAndHeldPerRoute(t *testing.T) {
	tooMany := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "99999")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	t.Run("ruleset", func(t *testing.T) {
		f := newFakeServer(t)
		var calls atomic.Int32
		f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				f.rulesetVersion(1)(w, r)
				return
			}
			tooMany(w, r)
		})
		clock := newFakeClock(midday)
		c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Second), WithFlushInterval(bigInterval), withClock(clock))
		c.Ready()
		clock.awaitTimer(t, 30*time.Second)
		clock.advance(30 * time.Second)
		f.await("/sdk/v1/ruleset", 2)
		eventually(t, "http_429", func() bool { return c.Status().LastError == "http_429" })
		clock.awaitTimer(t, 300*time.Second)
		clock.advance(299 * time.Second)
		time.Sleep(30 * time.Millisecond)
		if f.count("/sdk/v1/ruleset") != 2 {
			t.Error("the ruleset was requested during the hold")
		}
		clock.advance(time.Second)
		f.await("/sdk/v1/ruleset", 3)
	})
	t.Run("stream, and the ruleset it blocks too", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/stream", tooMany)
		clock := newFakeClock(midday)
		c := newTestClient(t, f, WithPollInterval(30*time.Second), WithFlushInterval(bigInterval),
			withClock(clock), withJitter(func() float64 { return 0 }))
		c.Ready()
		f.await("/sdk/v1/stream", 1)
		eventually(t, "http_429", func() bool { return c.Status().LastError == "http_429" })
		clock.awaitTimer(t, 300*time.Second)
		clock.advance(299 * time.Second)
		time.Sleep(30 * time.Millisecond)
		if f.count("/sdk/v1/stream") != 1 || f.count("/sdk/v1/ruleset") != 1 {
			t.Error("a request went out during the hold")
		}
		clock.advance(time.Second)
		f.await("/sdk/v1/ruleset", 2) // the poll after the wait
	})
	t.Run("events, which holds only the usage reports", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/events", tooMany)
		clock := newFakeClock(midday)
		c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second), withClock(clock))
		c.Ready()
		c.BoolVariation("f", user("u"), false)
		clock.awaitTimer(t, 30*time.Second)
		clock.advance(30 * time.Second)
		f.await("/sdk/v1/events", 1)
		eventually(t, "the next flush wait", func() bool { return len(clock.armed()) >= 2 })
		clock.advance(30 * time.Second)
		clock.advance(30 * time.Second)
		time.Sleep(30 * time.Millisecond)
		if f.count("/sdk/v1/events") != 1 {
			t.Error("events were sent during the hold")
		}
		clock.advance(300 * time.Second)
		f.await("/sdk/v1/events", 2)
		if c.Status().LastError != "" {
			t.Errorf("a usage 429 reached status: %q", c.Status().LastError)
		}
	})
}

func TestRetryAfterOnlyCountsOnA429AndWhenItIsWholeSeconds(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newTestClient(t, f, quiet()...)
	c.Ready()
	if left := c.holdLeft(&c.syncHold); left != 0 {
		t.Errorf("a 503 with Retry-After held for %v", left)
	}
}

func TestCloseAbandonsTheLoopsWithoutReportingTheirAbortedRequests(t *testing.T) {
	f := newFakeServer(t)
	var errs errorLog
	c := NewClient(f.url(), "k", WithPollInterval(time.Hour), WithFlushInterval(bigInterval), WithOnError(errs.add))
	c.Ready()
	f.await("/sdk/v1/stream", 1)
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	c.Close()
	if got := c.Status(); got.Stream != StreamClosed {
		t.Errorf("stream = %q after Close", got.Stream)
	}
	time.Sleep(50 * time.Millisecond)
	if errs.count() != 0 || c.Status().LastError != "" {
		t.Errorf("Close caused errors: %v, lastError %q", errs.messages(), c.Status().LastError)
	}
	n := f.count("/sdk/v1/ruleset") + f.count("/sdk/v1/stream")
	c.Close()
	time.Sleep(20 * time.Millisecond)
	if f.count("/sdk/v1/ruleset")+f.count("/sdk/v1/stream") != n {
		t.Error("a request went out after Close")
	}
	_ = fmt.Sprint()
}
