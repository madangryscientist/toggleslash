package toggleslash

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendsIfNoneMatchOnlyWithRulesetAndETag(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	if got := s.requests("/sdk/v1/ruleset")[0].Header.Get("If-None-Match"); got != "" {
		t.Fatalf("first request sent If-None-Match %q", got)
	}
	c.refresh(false)
	reqs := s.requests("/sdk/v1/ruleset")
	if got := reqs[len(reqs)-1].Header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("second request sent If-None-Match %q", got)
	}
	if c.Status().LastError != LastErrorNone {
		t.Fatal("a 304 left an error")
	}
}

func TestNoETagMeansNoIfNoneMatch(t *testing.T) {
	s := newFakeServer(t)
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", "")
		_, _ = w.Write([]byte(`{"flags":[],"segments":[]}`))
	})
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	c.refresh(false)
	for _, r := range s.requests("/sdk/v1/ruleset") {
		if _, ok := r.Header["If-None-Match"]; ok {
			t.Fatal("If-None-Match sent with no ETag held")
		}
	}
}

func TestBadRulesetKeepsTheLastGoodOne(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{"flags":`,
		"flags missing":  `{"segments":[]}`,
		"segments wrong": `{"flags":[],"segments":{}}`,
		"an array":       `[]`,
		"null":           `null`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newFakeServer(t, boolFlag("a", true))
			var log errorLog
			c := newTestClient(t, s, WithStream(false), WithOnError(log.add))
			c.Ready()
			s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("ETag", `"v2"`)
				_, _ = w.Write([]byte(body))
			})
			c.refresh(false)
			st := c.Status()
			if st.LastError != LastErrorBadRuleset || st.ETag != `"v1"` {
				t.Fatalf("status = %+v", st)
			}
			if !c.BoolVariation("a", user("u"), false) {
				t.Fatal("the last good ruleset was lost")
			}
			if log.count() != 1 {
				t.Fatalf("errors = %v", log.messages())
			}
		})
	}
}

func TestStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   LastError
	}{{401, "http_401"}, {404, "http_404"}, {500, "http_500"}, {304, "http_304"}, {302, "http_302"}} {
		s := newFakeServer(t)
		s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		var log errorLog
		c := newTestClient(t, s, WithStream(false), WithOnError(log.add))
		if c.Ready() {
			t.Fatalf("%d: ready", tc.status)
		}
		if got := c.Status().LastError; got != tc.want {
			t.Fatalf("%d: lastError = %q", tc.status, got)
		}
		waitFor(t, "onError", func() bool { return log.count() == 1 })
		if strings.Contains(log.messages()[0], s.URL) {
			t.Fatalf("error text carries the URL: %q", log.messages()[0])
		}
		c.Close()
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var hits atomic.Int32
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	})
	s.on("/elsewhere", func(http.ResponseWriter, *http.Request) { hits.Add(1) })
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	if hits.Load() != 0 || c.Status().LastError != "http_302" {
		t.Fatalf("redirect followed (%d) or lastError = %q", hits.Load(), c.Status().LastError)
	}
}

func TestTrailingSlashOnURL(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := New(s.URL+"///", testKey, WithStream(false), WithRequestTimeout(2*time.Second))
	t.Cleanup(c.Close)
	if !c.Ready() {
		t.Fatal("not ready")
	}
}

func TestNetworkErrorTextHasNoURL(t *testing.T) {
	var log errorLog
	c := New("http://127.0.0.1:1/secret-path", testKey, WithStream(false), WithOnError(log.add))
	t.Cleanup(c.Close)
	c.Ready()
	waitFor(t, "onError", func() bool { return log.count() == 1 })
	if m := log.messages()[0]; strings.Contains(m, "127.0.0.1") || strings.Contains(m, "secret-path") || strings.Contains(m, testKey) {
		t.Fatalf("message leaks: %q", m)
	}
}

func TestStreamFlipArrivesAndTagEqualEventIsNoFetch(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", false))
	c := newTestClient(t, s)
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := len(s.requests("/sdk/v1/ruleset"))
	synced := c.Status().LastSyncAt
	s.write("event: changed\ndata: \"v1\"\n\n")
	waitFor(t, "sync on equal tag", func() bool { return c.Status().LastSyncAt.After(synced) })
	if got := len(s.requests("/sdk/v1/ruleset")); got != before {
		t.Fatalf("a tag-equal event fetched (%d to %d)", before, got)
	}
	s.setFlags(boolFlag("a", true))
	waitFor(t, "flip", func() bool { return c.BoolVariation("a", user("u"), false) })
	if st := c.Status(); st.ETag != `"v2"` || st.Stream != StreamConnected {
		t.Fatalf("status = %+v", st)
	}
	if got := s.requests("/sdk/v1/stream")[0].Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q", got)
	}
}

func TestKeepAliveDoesNotMoveLastSyncAt(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s)
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := c.Status().LastSyncAt
	s.write(": keep-alive\n\nevent: ping\ndata: x\n\n")
	time.Sleep(100 * time.Millisecond)
	if !c.Status().LastSyncAt.Equal(before) {
		t.Fatal("a keep-alive moved lastSyncAt")
	}
}

func TestStreamNon200AndReconnect(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var mode atomic.Int32
	s.on("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 0 {
			w.WriteHeader(503)
			return
		}
		s.serveStream(w, r)
	})
	clk := newFakeClock()
	var log errorLog
	c := newTestClient(t, s, withClock(clk), withRandom(func() float64 { return 1 }), WithOnError(log.add),
		WithPollInterval(30*time.Second))
	c.Ready()
	waitFor(t, "http_503", func() bool { return c.Status().LastError == "http_503" && c.Status().Stream == StreamDisconnected })
	mode.Store(1)
	waitFor(t, "backoff armed", func() bool { return clk.Armed() >= 2 })
	clk.Advance(2 * time.Second)
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	if st := c.Status(); st.LastError != LastErrorNone {
		t.Fatalf("connected left lastError %q", st.LastError)
	}
}

func TestCleanStreamEndLeavesLastErrorAlone(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s)
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	s.dropStreams()
	waitFor(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	if got := c.Status().LastError; got != LastErrorNone {
		t.Fatalf("clean end set lastError %q", got)
	}
}

func TestStreamWatchdogDropsAQuietStreamAndKeepAliveResetsIt(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s, withWatchdog(400*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	for i := 0; i < 4; i++ {
		time.Sleep(200 * time.Millisecond)
		s.write(": ka\n\n")
	}
	if c.Status().Stream != StreamConnected {
		t.Fatal("keep-alives did not hold the stream open")
	}
	waitFor(t, "watchdog drop", func() bool {
		st := c.Status()
		return st.Stream == StreamDisconnected && st.LastError == LastErrorTimeout
	})
}

func TestStreamOpenTimeout(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c := New(s.URL, testKey, WithRequestTimeout(300*time.Millisecond), WithPollInterval(time.Hour))
	t.Cleanup(c.Close)
	c.Ready()
	waitFor(t, "open timeout", func() bool { return c.Status().LastError == LastErrorTimeout })
}

func TestDelay(t *testing.T) {
	c := &Client{cfg: config{stream: true, pollInterval: 30 * time.Second}, random: func() float64 { return 1 }}
	for failures, want := range map[int]time.Duration{0: time.Second, 1: 2 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second, 40: 30 * time.Second} {
		if got := c.delay(failures); got != want {
			t.Errorf("delay(%d) = %v, want %v", failures, got, want)
		}
	}
	c.random = func() float64 { return 0 }
	if got := c.delay(2); got != 2*time.Second {
		t.Errorf("jitter floor: %v", got)
	}
	c.cfg.stream = false
	if got := c.delay(3); got != 30*time.Second {
		t.Errorf("stream off: %v", got)
	}
}

func TestPollingWhenStreamOff(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", false))
	clk := newFakeClock()
	c := newTestClient(t, s, WithStream(false), withClock(clk), WithPollInterval(30*time.Second))
	c.Ready()
	waitFor(t, "armed", func() bool { return clk.Armed() >= 2 })
	s.setFlags(boolFlag("a", true))
	clk.Advance(30 * time.Second)
	waitFor(t, "poll", func() bool { return c.BoolVariation("a", user("u"), false) })
	if len(s.requests("/sdk/v1/stream")) != 0 {
		t.Fatal("stream off opened a stream")
	}
}

func TestRetryAfterHoldsRulesetAndStream(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var limited atomic.Bool
	s.on("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		if limited.Load() {
			w.Header().Set("Retry-After", "99999")
			w.WriteHeader(429)
			return
		}
		s.serveStream(w, r)
	})
	clk := newFakeClock()
	c := newTestClient(t, s, withClock(clk), withRandom(func() float64 { return 0 }), WithPollInterval(time.Second))
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	limited.Store(true)
	s.dropStreams()
	waitFor(t, "backoff armed", func() bool { return clk.Armed() >= 2 })
	clk.Advance(time.Second)
	waitFor(t, "429", func() bool { return c.Status().LastError == "http_429" })
	// The header asked for 99999 s; the cap is 300 s.
	if rem := c.holdRemaining(); rem <= 299*time.Second || rem > 300*time.Second {
		t.Fatalf("hold = %v", rem)
	}
	n := len(s.requests("/sdk/v1/stream"))
	clk.Advance(100 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := len(s.requests("/sdk/v1/stream")); got != n {
		t.Fatalf("stream retried during the hold (%d to %d)", n, got)
	}
}

func TestFirstLoadIsNotHeldAndOnlyA429IsRead(t *testing.T) {
	s := newFakeServer(t)
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "100")
		w.WriteHeader(503)
	})
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	if rem := c.holdRemaining(); rem > 0 {
		t.Fatalf("a 503 Retry-After raised a hold of %v", rem)
	}
}

func TestParseRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"0", 0}, {"5", 5 * time.Second}, {"\t7 ", 7 * time.Second}, {"300", 300 * time.Second}, {"301", 300 * time.Second},
		{"99999999999999999999", 300 * time.Second}, {"007", 7 * time.Second},
	} {
		in, want := tc.in, tc.want
		got, ok := parseRetryAfter(in)
		if !ok || got != want {
			t.Errorf("%q = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-1", "1.5", "1e3", "abc", "Wed, 21 Oct 2015 07:28:00 GMT", "+5", "5s"} {
		if _, ok := parseRetryAfter(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
