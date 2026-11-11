package toggleslash

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryAfterIsReadAsDigitsAndCapped(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"5", 5 * time.Second, true},
		{" 7 ", 7 * time.Second, true},
		{"0", 0, true},
		{"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true},
		{"99999999999999999999999", 300 * time.Second, true},
		{"", 0, false},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"1e3", 0, false},
		{"+5", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false},
		{"5 seconds", 0, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		h.Set("Retry-After", tc.in)
		got, ok := retryAfter(h)
		if got != tc.want || ok != tc.ok {
			t.Errorf("retryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAHoldOnlyLengthens(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	h := func(v string) http.Header { x := http.Header{}; x.Set("Retry-After", v); return x }
	c.holdFor(h("100"), false)
	long := c.rulesetHold()
	c.holdFor(h("10"), false)
	if got := c.rulesetHold(); got.Before(long) {
		t.Error("a shorter Retry-After shortened the hold")
	}
	c.holdFor(h("nonsense"), false)
	if got := c.rulesetHold(); got.Before(long) {
		t.Error("an unreadable Retry-After changed the hold")
	}
	if !c.eventsHold().IsZero() {
		t.Error("a ruleset hold leaked into the events hold")
	}
}

// holdIsCapped checks that a hold lasts about 300 s though the server asked for far more.
func holdIsCapped(t *testing.T, name string, until time.Time) {
	t.Helper()
	d := time.Until(until)
	if d < 290*time.Second || d > 301*time.Second {
		t.Errorf("%s hold is %v, want about 300s (the cap)", name, d)
	}
}

func TestRetryAfterCapOnEveryRoute(t *testing.T) {
	t.Run("ruleset", func(t *testing.T) {
		f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "999999")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		c := newTestClient(t, f, WithStream(false), WithPollInterval(10*time.Millisecond), WithFlushInterval(time.Hour))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if c.Ready(ctx) {
			t.Fatal("ready on a 429")
		}
		holdIsCapped(t, "ruleset", c.rulesetHold())
		if st := c.Status(); st.LastError != "http_429" {
			t.Errorf("lastError = %q", st.LastError)
		}
		time.Sleep(200 * time.Millisecond)
		if n := len(f.requests("/sdk/v1/ruleset")); n != 1 {
			t.Errorf("%d ruleset requests during the hold, want 1", n)
		}
	})

	t.Run("stream", func(t *testing.T) {
		h := &ruleHolder{}
		h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
		f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sdk/v1/stream" {
				w.Header().Set("Retry-After", "999999")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			h.serve(f, w, r)
		})
		c := newTestClient(t, f, WithPollInterval(10*time.Millisecond), WithFlushInterval(time.Hour))
		eventually(t, "the stream 429", func() bool { return len(f.requests("/sdk/v1/stream")) > 0 })
		eventually(t, "the hold", func() bool { return !c.rulesetHold().IsZero() })
		holdIsCapped(t, "stream", c.rulesetHold())
		time.Sleep(200 * time.Millisecond)
		if n := len(f.requests("/sdk/v1/stream")); n != 1 {
			t.Errorf("%d stream requests during the hold, want 1", n)
		}
		if n := len(f.requests("/sdk/v1/ruleset")); n != 1 {
			t.Errorf("%d ruleset requests during the hold, want 1", n)
		}
	})

	t.Run("events", func(t *testing.T) {
		h := &ruleHolder{}
		h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
		f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sdk/v1/events" {
				w.Header().Set("Retry-After", "999999")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			h.serve(f, w, r)
		})
		errs := &errorLog{}
		c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithOnError(errs.add))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !c.Ready(ctx) {
			t.Fatal("not ready")
		}
		c.BoolVariation("f", userCtx, false)
		c.Flush(ctx)
		holdIsCapped(t, "events", c.eventsHold())
		if n := c.pendingCount(); n != 1 {
			t.Errorf("a 429 must keep the counts; %d held", n)
		}
		c.Flush(ctx)
		if n := len(f.requests("/sdk/v1/events")); n != 1 {
			t.Errorf("%d events requests during the hold, want 1", n)
		}
		// The ruleset route is held separately and was not asked to wait.
		if !c.rulesetHold().IsZero() {
			t.Error("an events 429 held the ruleset route")
		}
		c.Close()
		found := false
		for _, e := range errs.all() {
			if strings.Contains(e, "closed with 1 usage counts unsent: the server asked to wait") {
				found = true
			}
		}
		if !found {
			t.Errorf("Close did not report the unsent counts: %v", errs.all())
		}
	})
}

// TestIdleStreamIsRestartedByTheWatchdog uses a real loopback socket: a stream that goes silent is
// closed and reopened, a stream that keeps sending comments is not, and the socket really is closed.
func TestIdleStreamIsRestartedByTheWatchdog(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	var streams atomic.Int32
	cancelled := make(chan struct{}, 8)
	var phase atomic.Int32 // 0: silent on the first stream, 1: keep-alive on the second
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/stream" {
			h.serve(f, w, r)
			return
		}
		n := streams.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", `"v1"`)
		flush(w)
		if n == 2 {
			// Keep-alive comments faster than the watchdog, for several watchdog periods.
			end := time.Now().Add(900 * time.Millisecond)
			for time.Now().Before(end) {
				select {
				case <-r.Context().Done():
					cancelled <- struct{}{}
					return
				case <-time.After(60 * time.Millisecond):
				}
				_, _ = fmt.Fprint(w, ": keep-alive\n\n")
				flush(w)
			}
			phase.Store(1)
		}
		select {
		case <-r.Context().Done():
			cancelled <- struct{}{}
		case <-f.done:
		}
	})
	c := newTestClient(t, f, WithPollInterval(20*time.Millisecond), WithFlushInterval(time.Hour),
		withWatchdog(250*time.Millisecond))

	// The first stream says nothing after connecting: closed by the watchdog, and the server sees it.
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the idle stream's socket was not closed")
	}
	eventually(t, "the second stream", func() bool { return streams.Load() >= 2 })
	// While keep-alives arrive no third stream opens, though four watchdog periods pass.
	time.Sleep(600 * time.Millisecond)
	if phase.Load() != 0 {
		t.Skip("the machine is too slow to hold the keep-alive window")
	}
	if n := streams.Load(); n != 2 {
		t.Errorf("%d streams opened while keep-alives were arriving, want 2", n)
	}
	// After the keep-alives stop the watchdog fires again.
	eventually(t, "the third stream", func() bool { return streams.Load() >= 3 })
	_ = c
}

func TestUTCMidnightGivesTwoDays(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	// An offset zone, so a wrong local-day count would show: 23:59:59.9 UTC is already the 2nd at +02:00.
	zone := time.FixedZone("plus2", 2*3600)
	var now atomic.Pointer[time.Time]
	set := func(tm time.Time) { now.Store(&tm) }
	set(time.Date(2026, 11, 10, 23, 59, 59, 900_000_000, time.UTC).In(zone))
	c, f := readyClient(t, h, withNow(func() time.Time { return *now.Load() }))
	c.BoolVariation("f", userCtx, false)
	set(time.Date(2026, 11, 11, 0, 0, 0, 100_000_000, time.UTC).In(zone))
	c.BoolVariation("f", userCtx, false)
	c.BoolVariation("f", userCtx, false)
	c.Flush(context.Background())
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("events requests = %d", len(reqs))
	}
	want := `{"events":[{"flag":"f","variationIndex":1,"count":1,"day":"2026-11-10"},` +
		`{"flag":"f","variationIndex":1,"count":2,"day":"2026-11-11"}]}`
	if reqs[0].body != want {
		t.Errorf("body = %s\nwant   %s", reqs[0].body, want)
	}
}

func TestTLSWithALocalCertificate(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	f := newFakeWith(t, h.serve, httptest.NewTLSServer)

	trusted := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour),
		withTransport(func(tr *http.Transport) {
			tr.TLSClientConfig = f.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !trusted.Ready(ctx) {
		t.Fatalf("a client that trusts the certificate was not ready: %+v", trusted.Status())
	}
	if !trusted.BoolVariation("f", userCtx, false) {
		t.Error("no value over TLS")
	}

	errs := &errorLog{}
	untrusted := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour),
		WithOnError(errs.add))
	if untrusted.Ready(ctx) {
		t.Fatal("a client that does not trust the certificate loaded a ruleset")
	}
	if st := untrusted.Status(); st.LastError != LastErrorNetwork {
		t.Errorf("lastError = %q", st.LastError)
	}
	u, _ := url.Parse(f.URL)
	for _, e := range errs.all() {
		if strings.Contains(e, u.Host) || strings.Contains(e, "test-key") {
			t.Errorf("an error carries the host or the key: %q", e)
		}
	}
}

// TestRequestsCarryOnlyWhatTheContractAllows audits every request of a full session: the headers, and
// that no context or flag value is in any URL, header or body.
func TestRequestsCarryOnlyWhatTheContractAllows(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	// The stream says connected at the first tag and, once the test pushes, that the tag changed; a
	// stream that opened with the second tag instead would make the second load a race.
	push := make(chan struct{})
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/stream" {
			h.serve(f, w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: connected\ndata: \"v1\"\n\n")
		flush(w)
		select {
		case <-push:
			_, _ = fmt.Fprint(w, "event: changed\ndata: \"v2\"\n\n")
			flush(w)
			select {
			case <-f.done:
			case <-r.Context().Done():
			}
		case <-f.done:
		case <-r.Context().Done():
		}
	})
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !c.Ready(ctx) {
		t.Fatal("not ready")
	}
	secret := ContextFrom(map[string]any{"key": "needle-user-9f3a", "email": "needle-mail-77@example.test"})
	for range 3 {
		c.BoolVariation("f", secret, false)
	}
	h.set(`"v2"`, rulesetJSON(boolFlagJSON("f", 2))) // a second load sends If-None-Match
	close(push)
	eventually(t, "the second ruleset", func() bool { return c.Status().ETag == `"v2"` })
	c.Flush(ctx)
	c.Close()

	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Accept-Encoding": true, "Content-Length": true,
	}
	var sawINM, firstRulesetHadINM bool
	rulesetSeen := 0
	for i, r := range f.requests("") {
		for name, vals := range r.header {
			if !allowed[name] {
				t.Errorf("request %d (%s) sent the header %s", i, r.path, name)
			}
			for _, v := range vals {
				if strings.Contains(v, "needle") {
					t.Errorf("header %s carries the context: %q", name, v)
				}
			}
		}
		if got := r.header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("request %d Authorization = %q", i, got)
		}
		if strings.Contains(r.path, "needle") || strings.Contains(r.body, "needle") {
			t.Errorf("request %d carries the context: %s %s", i, r.path, r.body)
		}
		switch r.path {
		case "/sdk/v1/ruleset":
			rulesetSeen++
			if r.header.Get("If-None-Match") != "" {
				sawINM = true
				if rulesetSeen == 1 {
					firstRulesetHadINM = true
				}
			}
			if r.header.Get("Accept") != "" && r.header.Get("Accept") == "text/event-stream" {
				t.Error("ruleset request asked for an event stream")
			}
		case "/sdk/v1/stream":
			if r.header.Get("Accept") != "text/event-stream" {
				t.Errorf("stream Accept = %q", r.header.Get("Accept"))
			}
			if r.header.Get("If-None-Match") != "" {
				t.Error("stream sent If-None-Match")
			}
		case "/sdk/v1/events":
			if r.header.Get("Content-Type") != "application/json" {
				t.Errorf("events Content-Type = %q", r.header.Get("Content-Type"))
			}
		}
	}
	if firstRulesetHadINM {
		t.Error("the first ruleset request sent If-None-Match with nothing held")
	}
	if !sawINM {
		t.Error("a later ruleset request never sent If-None-Match")
	}
}

// TestARedirectIsNotFollowed: the key must never be sent to where a redirect points.
func TestARedirectIsNotFollowed(t *testing.T) {
	var other atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1) }))
	defer target.Close()
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/sdk/v1/ruleset", http.StatusFound)
	})
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c.Ready(ctx) {
		t.Fatal("ready after a redirect")
	}
	if st := c.Status(); st.LastError != "http_302" {
		t.Errorf("lastError = %q", st.LastError)
	}
	if other.Load() != 0 {
		t.Error("the redirect was followed")
	}
}
