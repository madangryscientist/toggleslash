package toggleslash

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

// feed is a stream route a test writes to by hand.
type feed struct{ ch chan string }

func newFeed() *feed { return &feed{ch: make(chan string, 64)} }

func (f *feed) send(raw string) { f.ch <- raw }

func (f *feed) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	for {
		select {
		case s := <-f.ch:
			_, _ = io.WriteString(w, s)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func TestStreamConnectedThenChanged(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	f := newFeed()
	srv.handle(routeStream, f.handler)
	c := start(t, srv)
	c.Ready()
	f.send("event: connected\ndata: \"v1\"\n\n")
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	if n := srv.count(routeRuleset); n != 1 {
		t.Fatalf("the tag held needs no fetch, got %d ruleset requests", n)
	}
	srv.publish(rulesetOf(flag("f", 1, 2)), `"v2"`)
	f.send(": keep-alive\n\nevent: other\ndata: x\n\nevent: changed\ndata: \"v2\"\n\n")
	eventually(t, "the new tag", func() bool { return c.Status().ETag == `"v2"` })
	if !c.BoolVariation("f", shopper, false) {
		t.Fatal("the new ruleset was not served")
	}
	if got := srv.got(routeRuleset)[1].header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("If-None-Match %q", got)
	}
}

func TestStreamFetchFailureEndsTheStreamAndBacksOff(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	f := newFeed()
	srv.handle(routeStream, f.handler)
	clk := newFakeClock(epoch)
	c := start(t, srv, withClock(clk), withJitter(func() float64 { return 0 }))
	c.Ready()
	f.send("event: connected\ndata: \"v1\"\n\n")
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	f.send("event: changed\ndata: \"v2\"\n\n")
	eventually(t, "http_500", func() bool { return c.Status().LastError == "http_500" })
	eventually(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	// The fetch the stream asked for failed, so it counts as not connected: failures is 1 and the wait is
	// 1 s * 2^1 * 0.5.
	eventually(t, "the backoff timer", func() bool { return slices.Contains(clk.Waiting(), time.Second) })
}

func TestBackoffGrowsWhileTheStreamWillNotOpen(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	srv.handle(routeStream, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	clk := newFakeClock(epoch)
	c := start(t, srv, withClock(clk), withJitter(func() float64 { return 0 }), WithPollInterval(10*time.Second))
	c.Ready()
	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
		eventually(t, "the backoff timer "+want.String(), func() bool { return slices.Contains(clk.Waiting(), want) })
		polls := srv.count(routeRuleset)
		clk.Advance(want)
		eventually(t, "a poll", func() bool { return srv.count(routeRuleset) > polls })
	}
	if c.Status().LastError != "http_503" && c.Status().LastError != "" {
		t.Fatalf("lastError %q", c.Status().LastError)
	}
}

func TestPollingOnlyWaitsThePollInterval(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	clk := newFakeClock(epoch)
	c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(7*time.Second))
	c.Ready()
	eventually(t, "the poll timer", func() bool { return slices.Contains(clk.Waiting(), 7*time.Second) })
	clk.Advance(7 * time.Second)
	eventually(t, "a 304 poll", func() bool { return srv.count(routeRuleset) == 2 })
	if srv.count(routeStream) != 0 {
		t.Fatal("WithStream(false) opened a stream")
	}
	eventually(t, "lastSyncAt moved", func() bool { return c.Status().LastSyncAt.Equal(epoch.Add(7 * time.Second)) })
}

func TestIfNoneMatchOnlyWithARulesetAndATag(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	// A server that sends no ETag: nothing to send back.
	srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, rulesetOf(flag("f", 0, 1)))
	})
	clk := newFakeClock(epoch)
	c := start(t, srv, WithStream(false), withClock(clk))
	c.Ready()
	eventually(t, "the poll timer", func() bool { return clk.Armed() >= 2 })
	clk.Advance(time.Hour)
	eventually(t, "second request", func() bool { return srv.count(routeRuleset) == 2 })
	for i, r := range srv.got(routeRuleset) {
		if _, has := r.header["If-None-Match"]; has {
			t.Fatalf("request %d sent If-None-Match", i)
		}
	}
}

func TestNotModifiedWithNothingHeldIsAFailure(t *testing.T) {
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotModified) })
	c := start(t, srv, WithStream(false))
	if c.Ready() {
		t.Fatal("ready from a 304")
	}
	if got := c.Status().LastError; got != "http_304" {
		t.Fatalf("lastError %q", got)
	}
}

func TestRequestTimeoutBoundsTheBodyToo(t *testing.T) {
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"flags":[`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	c := start(t, srv, WithStream(false), WithRequestTimeout(100*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready from half a body")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Ready took %v", time.Since(start))
	}
	if got := c.Status().LastError; got != LastErrorTimeout {
		t.Fatalf("lastError %q", got)
	}
}

func TestWatchdogEndsAQuietStream(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	// connected, then silence: not even a keep-alive.
	srv.handle(routeStream, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	c := start(t, srv, withWatchdog(80*time.Millisecond), withJitter(func() float64 { return 0 }))
	c.Ready()
	eventually(t, "a second stream request", func() bool { return srv.count(routeStream) >= 2 })
}

func TestRetryAfterParsing(t *testing.T) {
	cases := []struct {
		header string
		status int
		want   time.Duration
		ok     bool
	}{
		{"5", 429, 5 * time.Second, true},
		{"0", 429, 0, true},
		{" 12 ", 429, 12 * time.Second, true},
		{"300", 429, 300 * time.Second, true},
		{"301", 429, 300 * time.Second, true},
		{"99999999999999999999999", 429, 300 * time.Second, true},
		{"1.5", 429, 0, false},
		{"-1", 429, 0, false},
		{"+5", 429, 0, false},
		{"Wed, 21 Oct 2037 07:28:00 GMT", 429, 0, false},
		{"", 429, 0, false},
		{"5", 503, 0, false},
		{"5", 200, 0, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.header != "" {
			h.Set("Retry-After", tc.header)
		}
		got, ok := retryAfter(h, tc.status)
		if got != tc.want || ok != tc.ok {
			t.Errorf("retryAfter(%q, %d) = %v, %v; want %v, %v", tc.header, tc.status, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRetryAfterIsCappedOnRulesetAndStream(t *testing.T) {
	t.Run("ruleset", func(t *testing.T) {
		srv := newFakeServer(t, "")
		srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "86400")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		clk := newFakeClock(epoch)
		c := start(t, srv, WithStream(false), withClock(clk))
		c.Ready()
		eventually(t, "a wait of 300 s", func() bool { return slices.Contains(clk.Waiting(), 300*time.Second) })
		if slices.ContainsFunc(clk.Waiting(), func(d time.Duration) bool { return d > 300*time.Second }) {
			t.Fatalf("a wait beyond the cap: %v", clk.Waiting())
		}
	})
	t.Run("stream holds the ruleset too", func(t *testing.T) {
		srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
		srv.handle(routeStream, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "86400")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		clk := newFakeClock(epoch)
		c := start(t, srv, withClock(clk), withJitter(func() float64 { return 0 }))
		c.Ready()
		eventually(t, "a wait of 300 s", func() bool { return slices.Contains(clk.Waiting(), 300*time.Second) })
		polls := srv.count(routeRuleset)
		clk.Advance(299 * time.Second)
		time.Sleep(20 * time.Millisecond)
		if srv.count(routeRuleset) != polls {
			t.Fatal("polled inside the hold")
		}
	})
}

func TestRetryAfterIsIgnoredOffA429(t *testing.T) {
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "250")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	clk := newFakeClock(epoch)
	c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(9*time.Second))
	c.Ready()
	eventually(t, "the plain poll wait", func() bool { return slices.Contains(clk.Waiting(), 9*time.Second) })
}

// The headers this SDK sends are the ones below and nothing else. net/http adds User-Agent and Content-Length
// by itself; compression is off, so no Accept-Encoding; Connection is not sent on a keep-alive HTTP/1.1
// request; and Host is on the wire but not in the request's header map, so it is read from r.Host.
func TestHeaderAudit(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	clk := newFakeClock(epoch)
	// A client with the stream off makes the plain and the conditional ruleset request and an events request.
	poller := start(t, srv, WithStream(false), withClock(clk), WithFlushInterval(time.Minute), WithPollInterval(time.Minute))
	poller.Ready()
	eventually(t, "the timers", func() bool { return clk.Armed() == 2 })
	poller.BoolVariation("f", NewContext("secret-user", map[string]any{"email": "a@b.c"}), false)
	clk.Advance(time.Minute)
	eventually(t, "the conditional request and the flush", func() bool {
		return srv.count(routeRuleset) == 2 && srv.count(routeEvents) == 1
	})
	// A client with the stream on makes the stream request.
	streamer := start(t, srv)
	streamer.Ready()
	eventually(t, "connected", func() bool { return streamer.Status().Stream == StreamConnected })

	type route struct {
		name    string
		seen    seen
		allowed []string
	}
	rs := srv.got(routeRuleset)
	routes := []route{
		{"ruleset", rs[0], []string{"Authorization", "User-Agent"}},
		{"ruleset (conditional)", rs[1], []string{"Authorization", "User-Agent", "If-None-Match"}},
		{"stream", srv.got(routeStream)[0], []string{"Authorization", "User-Agent", "Accept"}},
		{"events", srv.got(routeEvents)[0], []string{"Authorization", "User-Agent", "Content-Type", "Content-Length"}},
	}
	for _, r := range routes {
		var names []string
		for name, values := range r.seen.header {
			names = append(names, name)
			if !slices.Contains(r.allowed, name) {
				t.Errorf("%s sent an unexpected header %s: %v", r.name, name, values)
			}
			for _, v := range values {
				if strings.Contains(v, "secret-user") || strings.Contains(v, "a@b.c") {
					t.Errorf("%s: header %s carries the context", r.name, name)
				}
			}
		}
		slices.Sort(names)
		t.Logf("%-22s headers %v; Host %s; User-Agent %q", r.name, names, "127.0.0.1:port", r.seen.header.Get("User-Agent"))
		if got := r.seen.header.Get("Authorization"); got != "Bearer sdk-secret" {
			t.Errorf("%s: Authorization %q", r.name, got)
		}
		if ua := r.seen.header.Get("User-Agent"); !strings.HasPrefix(ua, "Go-http-client/") {
			t.Errorf("%s: User-Agent %q names more than the library", r.name, ua)
		}
		if !strings.HasPrefix(r.seen.host, "127.0.0.1:") {
			t.Errorf("%s: Host %q", r.name, r.seen.host)
		}
		if strings.Contains(r.seen.path, "secret") || strings.Contains(r.seen.body, "secret-user") || strings.Contains(r.seen.body, "a@b.c") {
			t.Errorf("%s: the path or body carries the context: %q %q", r.name, r.seen.path, r.seen.body)
		}
	}
	if got := routes[1].seen.header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("If-None-Match %q", got)
	}
	if got := routes[2].seen.header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("stream Accept %q", got)
	}
	if got := routes[3].seen.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("events Content-Type %q", got)
	}
}

func TestTLSServer(t *testing.T) {
	tls := newTLSServer(t, rulesetOf(flag("f", 1, 1)))
	c := New(tls.URL, "k", WithStream(false), WithTransport(tls.Client().Transport))
	t.Cleanup(c.Close)
	if !c.Ready() || !c.BoolVariation("f", shopper, false) {
		t.Fatalf("over TLS: ready %v status %+v", c.Ready(), c.Status())
	}
	// Without trusting the test certificate the load fails as a network error and nothing is served.
	bad := New(tls.URL, "k", WithStream(false))
	t.Cleanup(bad.Close)
	if bad.Ready() || bad.Status().LastError != LastErrorNetwork {
		t.Fatalf("an untrusted certificate: %+v", bad.Status())
	}
}
