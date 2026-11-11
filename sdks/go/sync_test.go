package toggleslash

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// allowedHeaders are the only request headers a request may carry: the ones net/http adds on its own and the
// ones the protocol names. Anything else is an unexplained header.
var allowedHeaders = map[string]bool{
	"Authorization": true, "If-None-Match": true, "Content-Type": true, "Accept": true,
	"User-Agent": true, "Content-Length": true, "Accept-Encoding": true,
}

func TestRequestHeadersAndRoutes(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "the stream", func() bool { return c.Status().Stream == StreamConnected })
	c.BoolVariation("a", user, false)
	c.Close()

	rs := f.requests("/sdk/v1/ruleset")
	if len(rs) != 1 || rs[0].Header.Get("If-None-Match") != "" {
		t.Fatalf("first ruleset request: %+v", rs)
	}
	if len(f.requests("/sdk/v1/stream")) != 1 {
		t.Fatal("no stream request")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.reqs {
		switch r.Path {
		case "/sdk/v1/ruleset", "/sdk/v1/stream", "/sdk/v1/events":
		default:
			t.Errorf("a request to %s", r.Path)
		}
		if r.RawQuery != "" {
			t.Errorf("a query on %s: %s", r.Path, r.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		for name := range r.Header {
			if !allowedHeaders[name] {
				t.Errorf("%s carries the header %s", r.Path, name)
			}
		}
		if r.Path == "/sdk/v1/stream" && r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("stream Accept = %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("Accept-Encoding") != "" {
			t.Errorf("%s sent Accept-Encoding %q", r.Path, r.Header.Get("Accept-Encoding"))
		}
	}
}

func TestIfNoneMatchOnlyWithARulesetAndATag(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithPollInterval(20*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "a second request", func() bool { return len(f.requests("/sdk/v1/ruleset")) >= 3 })
	rs := f.requests("/sdk/v1/ruleset")
	if rs[0].Header.Get("If-None-Match") != "" {
		t.Fatal("the first request sent If-None-Match")
	}
	if rs[1].Header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("second request If-None-Match = %q", rs[1].Header.Get("If-None-Match"))
	}
	// 304 is a sync: lastSyncAt moves and the tag stays.
	before := c.Status().LastSyncAt
	eventually(t, 3*time.Second, "lastSyncAt to move on a 304", func() bool { return c.Status().LastSyncAt.After(before) })
	if c.Status().ETag != `"v1"` {
		t.Fatalf("etag = %q", c.Status().ETag)
	}

	// A 200 with no ETag header: the ruleset is held with no tag, so no If-None-Match is sent after it.
	g := newFakeServer(t, boolFlag("a", true, 1))
	g.rulesetFn = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, g.body()) }
	d := newTestClient(t, g, WithPollInterval(20*time.Millisecond))
	d.Ready()
	eventually(t, 3*time.Second, "a second request", func() bool { return len(g.requests("/sdk/v1/ruleset")) >= 3 })
	for _, r := range g.requests("/sdk/v1/ruleset") {
		if r.Header.Get("If-None-Match") != "" {
			t.Fatal("If-None-Match sent although the server gave no ETag")
		}
	}
	if d.Status().ETag != "" {
		t.Fatalf("etag = %q", d.Status().ETag)
	}
}

func TestA304WithNoRulesetHeldIsAnError(t *testing.T) {
	f := newFakeServer(t)
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }
	errs := &errorLog{}
	c := newTestClient(t, f, WithOnError(errs.add))
	if c.Ready() {
		t.Fatal("ready")
	}
	eventually(t, time.Second, "onError", func() bool { return errs.count() == 1 })
	if c.Status().LastError != "http_304" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
}

func TestBadRulesets(t *testing.T) {
	for name, body := range map[string]string{
		"not json":           "<html>",
		"an array":           "[]",
		"flags missing":      `{"segments":[]}`,
		"segments missing":   `{"flags":[]}`,
		"flags not an array": `{"flags":{},"segments":[]}`,
		"segments not array": `{"flags":[],"segments":null}`,
		"too deep":           strings.Repeat("[", 5000) + strings.Repeat("]", 5000),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeServer(t, boolFlag("a", true, 1))
			f.rulesetFn = func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"x"`)
				_, _ = io.WriteString(w, body)
			}
			errs := &errorLog{}
			c := newTestClient(t, f, WithOnError(errs.add))
			if c.Ready() {
				t.Fatal("ready with a bad ruleset")
			}
			eventually(t, time.Second, "onError", func() bool { return errs.count() >= 1 })
			st := c.Status()
			if st.LastError != LastErrorBadRuleset || st.ETag != "" || !st.LastSyncAt.IsZero() {
				t.Fatalf("status = %+v", st)
			}
		})
	}
}

func TestABadRulesetKeepsTheLastGoodOne(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 4))
	c := newTestClient(t, f, WithPollInterval(20*time.Millisecond))
	c.Ready()
	f.mu.Lock()
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"bad"`)
		_, _ = io.WriteString(w, `{"flags":3}`)
	}
	f.mu.Unlock()
	eventually(t, 3*time.Second, "bad_ruleset", func() bool { return c.Status().LastError == LastErrorBadRuleset })
	if !c.BoolVariation("a", user, false) || c.Status().ETag != `"v1"` {
		t.Fatalf("the last good ruleset was not kept: %+v", c.Status())
	}
}

func TestStatusCodesAreErrorsAndTheOldRulesetStays(t *testing.T) {
	for _, code := range []int{401, 403, 404, 429, 500, 503} {
		f := newFakeServer(t, boolFlag("a", true, 1))
		f.rulesetFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
		c := newTestClient(t, f)
		c.Ready()
		want := "http_" + strconv.Itoa(code)
		if got := c.Status().LastError; got != want {
			t.Errorf("%d: lastError = %q, want %q", code, got, want)
		}
	}
}

func TestARedirectIsAnErrorAndTheKeyIsNeverResent(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"flags":[],"segments":[]}`)
	}))
	defer other.Close()
	f := newFakeServer(t)
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", http.StatusFound) }
	c := newTestClient(t, f)
	if c.Ready() {
		t.Fatal("ready after a redirect")
	}
	if got := c.Status().LastError; got != "http_302" {
		t.Fatalf("lastError = %q", got)
	}
	if hits.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestErrorsCarryFixedTextOnly(t *testing.T) {
	// A refused connection: net/http's error text would carry the URL.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	errs := &errorLog{}
	c := New(url, testKey, WithStream(false), WithOnError(errs.add), WithRequestTimeout(time.Second))
	defer c.Close()
	c.Ready()
	eventually(t, time.Second, "onError", func() bool { return errs.count() >= 1 })
	for _, text := range errs.texts() {
		if strings.Contains(text, url) || strings.Contains(text, "127.0.0.1") || strings.Contains(text, testKey) {
			t.Errorf("error text leaks: %q", text)
		}
	}
	if c.Status().LastError != LastErrorNetwork {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
}

func TestABodyThatNeverEndsIsATimeout(t *testing.T) {
	f := newFakeServer(t)
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"flags":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(150*time.Millisecond))
	defer c.Close()
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if c.Status().LastError != LastErrorTimeout {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
}

func TestStreamSwapsOnChanged(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", false, 1))
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	if c.BoolVariation("a", user, true) {
		t.Fatal("served true")
	}
	f.publish(boolFlag("a", true, 2))
	eventually(t, 3*time.Second, "the swap", func() bool { return c.Status().ETag == `"v2"` })
	if !c.BoolVariation("a", user, false) {
		t.Fatal("did not serve the new ruleset")
	}
	if got := len(f.requests("/sdk/v1/ruleset")); got != 2 {
		t.Fatalf("ruleset requests = %d, want 2 (the first load and the change)", got)
	}
}

func TestAStreamTagEqualToTheHeldOneIsASyncWithNoFetch(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := c.Status().LastSyncAt
	time.Sleep(5 * time.Millisecond)
	f.push("event: changed\ndata: \"v1\"\n\n")
	eventually(t, 3*time.Second, "a sync", func() bool { return c.Status().LastSyncAt.After(before) })
	if got := len(f.requests("/sdk/v1/ruleset")); got != 1 {
		t.Fatalf("ruleset requests = %d, want 1", got)
	}
	// A keep-alive and an unknown event move nothing.
	at := c.Status().LastSyncAt
	f.push(": keep-alive\n\nevent: other\ndata: \"v9\"\n\n")
	time.Sleep(100 * time.Millisecond)
	if !c.Status().LastSyncAt.Equal(at) || len(f.requests("/sdk/v1/ruleset")) != 1 {
		t.Fatal("a keep-alive or an unknown event acted")
	}
}

func TestAStreamFetchThatFailsEndsTheStreamAndCountsAsNotConnected(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	errs := &errorLog{}
	c := newTestClient(t, f, WithStream(true), WithPollInterval(40*time.Millisecond), WithOnError(errs.add),
		func(cfg *config) { cfg.random = func() float64 { return 0 } })
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	f.mu.Lock()
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	f.mu.Unlock()
	f.publish(boolFlag("a", false, 2))
	eventually(t, 3*time.Second, "http_500", func() bool { return c.Status().LastError == "http_500" })
	eventually(t, 3*time.Second, "a second stream attempt", func() bool { return len(f.requests("/sdk/v1/stream")) >= 2 })
	if errs.count() == 0 {
		t.Fatal("no onError")
	}
	// The ruleset held is still the old one.
	if !c.BoolVariation("a", user, false) {
		t.Fatal("the old ruleset was lost")
	}
}

func TestAStreamThatEndsCleanlyIsNotAnError(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.streamFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	errs := &errorLog{}
	c := newTestClient(t, f, WithStream(true), WithPollInterval(30*time.Millisecond), WithOnError(errs.add),
		func(cfg *config) { cfg.random = func() float64 { return 0 } })
	c.Ready()
	eventually(t, 3*time.Second, "two stream attempts", func() bool { return len(f.requests("/sdk/v1/stream")) >= 2 })
	if errs.count() != 0 || c.Status().LastError != "" {
		t.Fatalf("a clean end was an error: %v %q", errs.texts(), c.Status().LastError)
	}
}

func TestAStreamStatusIsAnError(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.streamFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }
	c := newTestClient(t, f, WithStream(true), WithPollInterval(30*time.Millisecond),
		func(cfg *config) { cfg.random = func() float64 { return 0 } })
	c.Ready()
	eventually(t, 3*time.Second, "http_503", func() bool { return c.Status().LastError == "http_503" })
	// Never connected, so it polls once per failure.
	eventually(t, 3*time.Second, "a poll", func() bool { return len(f.requests("/sdk/v1/ruleset")) >= 2 })
}

func TestTheStreamOpenIsBoundedByTheRequestTimeout(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.streamFn = hang
	c := newTestClient(t, f, WithStream(true), WithRequestTimeout(150*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "timeout", func() bool { return c.Status().LastError == LastErrorTimeout })
	if st := c.Status().Stream; st != StreamDisconnected && st != StreamConnecting {
		t.Fatalf("stream = %q", st)
	}
}

func TestTheWatchdogDropsAQuietStreamAndAKeepAliveRestartsIt(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.streamFn = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour), func(cfg *config) { cfg.watchdog = 200 * time.Millisecond })
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	eventually(t, 3*time.Second, "the watchdog", func() bool { return c.Status().LastError == LastErrorTimeout })
	if c.Status().Stream == StreamConnected {
		t.Fatal("still connected after the watchdog")
	}

	g := newFakeServer(t, boolFlag("a", true, 1))
	g.streamFn = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		tick := time.NewTicker(60 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}
	d := newTestClient(t, g, WithStream(true), WithPollInterval(time.Hour), func(cfg *config) { cfg.watchdog = 200 * time.Millisecond })
	d.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return d.Status().Stream == StreamConnected })
	before := d.Status().LastSyncAt
	time.Sleep(700 * time.Millisecond)
	if d.Status().Stream != StreamConnected || d.Status().LastError != "" {
		t.Fatalf("a keep-alive did not restart the watchdog: %+v", d.Status())
	}
	if !d.Status().LastSyncAt.Equal(before) {
		t.Fatal("a keep-alive moved lastSyncAt")
	}
	if got := len(g.requests("/sdk/v1/stream")); got != 1 {
		t.Fatalf("stream requests = %d", got)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	c := New("http://127.0.0.1:1", testKey, WithStream(false))
	defer c.Close()
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"0", 0, true}, {"5", 5 * time.Second, true}, {"  7  ", 7 * time.Second, true}, {"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true}, {"99999999999999999999999", 300 * time.Second, true}, {"007", 7 * time.Second, true},
		{"", 0, false}, {"-1", 0, false}, {"1.5", 0, false}, {"1e3", 0, false}, {"5 s", 0, false}, {"+5", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false}, {"٣", 0, false},
	}
	for _, tc := range cases {
		got, ok := c.parseRetryAfter(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%q: got %v %v, want %v %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	c := New("http://127.0.0.1:1", testKey, WithStream(true), WithPollInterval(30*time.Second),
		func(cfg *config) { cfg.random = func() float64 { return 0 } })
	defer c.Close()
	want := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second}
	for failures, w := range want {
		if got := c.delay(failures); got != w {
			t.Errorf("failures %d: %v, want %v", failures, got, w)
		}
	}
	if got := c.delay(1000); got != 15*time.Second {
		t.Errorf("failures 1000: %v", got)
	}
	// The jitter stays in [0.5, 1) of the ceiling. A second client carries the seam: the first one's sync
	// goroutine reads its config, so a test never writes to it after New.
	top := New("http://127.0.0.1:1", testKey, WithStream(true), WithPollInterval(30*time.Second),
		func(cfg *config) { cfg.random = func() float64 { return 0.999999 } })
	defer top.Close()
	if got := top.delay(2); got >= 4*time.Second || got < 3900*time.Millisecond {
		t.Errorf("top of the jitter: %v", got)
	}
	// Stream off: the poll interval, every time.
	p := New("http://127.0.0.1:1", testKey, WithStream(false), WithPollInterval(7*time.Second))
	defer p.Close()
	for _, failures := range []int{0, 1, 9} {
		if got := p.delay(failures); got != 7*time.Second {
			t.Errorf("poll only, failures %d: %v", failures, got)
		}
	}
}

func TestRetryAfterHoldsTheRulesetAndStreamTogether(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	var first atomic.Bool
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) {
		if first.CompareAndSwap(false, true) {
			w.WriteHeader(429)
			return
		}
		f.defaultRuleset(w, r)
	}
	f.streamFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	}
	c := newTestClient(t, f, WithStream(true), WithPollInterval(20*time.Millisecond),
		func(cfg *config) { cfg.random = func() float64 { return 0 } })
	c.Ready()
	eventually(t, 3*time.Second, "the stream 429", func() bool { return len(f.requests("/sdk/v1/stream")) >= 1 })
	at := time.Now()
	eventually(t, 5*time.Second, "the next stream attempt", func() bool { return len(f.requests("/sdk/v1/stream")) >= 2 })
	if gap := time.Since(at); gap < 700*time.Millisecond {
		t.Fatalf("the hold was not kept: next attempt after %v", gap)
	}
	// While held, the ruleset route also got nothing: the polls that come after a refused stream wait too.
	reqs := f.requests("/sdk/v1/ruleset")
	if len(reqs) > 3 {
		t.Fatalf("ruleset requests during the hold: %d", len(reqs))
	}
}

func TestRetryAfterIsCappedOnAllThreeRoutes(t *testing.T) {
	for _, route := range []string{"ruleset", "stream", "events"} {
		t.Run(route, func(t *testing.T) {
			f := newFakeServer(t, boolFlag("a", true, 1))
			h := func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "99999")
				w.WriteHeader(429)
			}
			switch route {
			case "ruleset":
				f.rulesetFn = h
			case "stream":
				f.streamFn = h
			case "events":
				f.eventsFn = h
			}
			c := newTestClient(t, f, WithStream(route == "stream"), WithPollInterval(time.Hour), WithFlushInterval(30*time.Millisecond),
				func(cfg *config) { cfg.retryAfterCap = 400 * time.Millisecond })
			c.Ready()
			if route == "events" {
				c.BoolVariation("a", user, false)
				eventually(t, 3*time.Second, "an events request", func() bool { return len(f.requests("/sdk/v1/events")) >= 1 })
				left := c.holdLeft(&c.holdUsage)
				if left <= 0 || left > 400*time.Millisecond {
					t.Fatalf("usage hold left = %v", left)
				}
				return
			}
			eventually(t, 3*time.Second, "a 429", func() bool { return c.Status().LastError == "http_429" })
			left := c.holdLeft(&c.holdSync)
			if left <= 0 || left > 400*time.Millisecond {
				t.Fatalf("hold left = %v", left)
			}
		})
	}
}

func TestAHoldOnlyGrows(t *testing.T) {
	c := New("http://127.0.0.1:1", testKey, WithStream(false))
	defer c.Close()
	c.hold(&c.holdSync, 10*time.Second)
	c.hold(&c.holdSync, time.Second)
	if left := c.holdLeft(&c.holdSync); left < 9*time.Second {
		t.Fatalf("a shorter hold shortened it: %v", left)
	}
	c.hold(&c.holdSync, 20*time.Second)
	if left := c.holdLeft(&c.holdSync); left < 19*time.Second {
		t.Fatalf("a longer hold did not lengthen it: %v", left)
	}
	if c.holdLeft(&c.holdUsage) > 0 {
		t.Fatal("the usage hold moved with the ruleset hold")
	}
}
