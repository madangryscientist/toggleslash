package toggleslash

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadyAndVariation(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f)
	if !c.Ready() {
		t.Fatal("Ready() = false, want true")
	}
	if got := c.BoolVariation("checkout", ctxOf("u1"), false); !got {
		t.Fatal("checkout = false, want true")
	}
	d := c.BoolVariationDetail("legacy", ctxOf("u1"), true)
	if d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough || d.Value {
		t.Fatalf("legacy detail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.LastError != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestNotReadyBeforeAndAfterFailure(t *testing.T) {
	f := newFakeServer(t)
	f.hang("ruleset")
	log := &errorLog{}
	c := newClient(t, f, WithRequestTimeout(150*time.Millisecond), WithOnError(log.add))
	d := c.StringVariationDetail("checkout", ctxOf("u1"), "fb")
	if d.Value != "fb" || d.Reason.Kind != ReasonError || d.Reason.ErrorCode != ErrNotReady || d.VariationIndex != NoVariation {
		t.Fatalf("before first load: %+v", d)
	}
	if c.Ready() {
		t.Fatal("Ready() = true after a timed out first load")
	}
	if got := c.Status().LastError; got != LastErrorTimeout {
		t.Fatalf("LastError = %q", got)
	}
	if log.count() == 0 || !strings.Contains(log.messages()[0], "timeout") {
		t.Fatalf("onError = %v", log.messages())
	}
}

func TestReadyIsFixedAtTheFirstSettle(t *testing.T) {
	f := newFakeServer(t)
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	c := newClient(t, f, WithStream(false))
	if c.Ready() {
		t.Fatal("Ready() = true on a 503")
	}
	f.set("ruleset", nil)
	eventually(t, 3*time.Second, "a later load", func() bool { return c.Status().ETag != "" })
	if c.Ready() {
		t.Fatal("Ready() changed after a later load")
	}
	if !c.BoolVariation("checkout", ctxOf("u"), false) {
		t.Fatal("the later load did not serve")
	}
}

func TestRequestHeadersAreOnlyTheAllowedOnes(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithFlushInterval(20*time.Millisecond))
	c.Ready()
	c.BoolVariation("checkout", ctxOf("u"), false)
	eventually(t, 3*time.Second, "an events request", func() bool { return len(f.requests("events")) > 0 })
	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Accept-Encoding": true, "Content-Length": true, "Connection": true,
	}
	for _, r := range f.all() {
		for name := range r.header {
			if !allowed[name] {
				t.Errorf("%s request carried header %s", r.route, name)
			}
		}
		if got := r.header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("%s Authorization = %q", r.route, got)
		}
		if r.route == "stream" && r.header.Get("Accept-Encoding") != "identity" {
			t.Errorf("stream Accept-Encoding = %q", r.header.Get("Accept-Encoding"))
		}
	}
}

func TestIfNoneMatchOnlyWithHeldRulesetAndTag(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithStream(false))
	c.Ready()
	eventually(t, 3*time.Second, "a second ruleset request", func() bool { return len(f.requests("ruleset")) >= 2 })
	rs := f.requests("ruleset")
	if rs[0].header.Get("If-None-Match") != "" {
		t.Fatalf("first request sent If-None-Match %q", rs[0].header.Get("If-None-Match"))
	}
	if rs[1].header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("second request If-None-Match = %q", rs[1].header.Get("If-None-Match"))
	}
}

func TestNoETagMeansNoIfNoneMatch(t *testing.T) {
	f := newFakeServer(t)
	f.mu.Lock()
	f.etag = ""
	f.mu.Unlock()
	c := newClient(t, f, WithStream(false))
	c.Ready()
	eventually(t, 3*time.Second, "a second ruleset request", func() bool { return len(f.requests("ruleset")) >= 2 })
	for _, r := range f.requests("ruleset") {
		if r.header.Get("If-None-Match") != "" {
			t.Fatalf("sent If-None-Match %q with no ETag held", r.header.Get("If-None-Match"))
		}
	}
}

func TestBadRulesetKeepsTheOldOne(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithStream(false))
	c.Ready()
	for _, body := range []string{`[]`, `{"flags":[]}`, `{"flags":{},"segments":[]}`, `not json`, `{"flags":[],"segments":3}`} {
		f.mu.Lock()
		f.etag = `"bad"`
		f.body = body
		f.mu.Unlock()
		eventually(t, 3*time.Second, "bad_ruleset for "+body, func() bool { return c.Status().LastError == LastErrorBadRuleset })
		if c.Status().ETag != `"v1"` {
			t.Fatalf("ETag moved to %q on %s", c.Status().ETag, body)
		}
		if !c.BoolVariation("checkout", ctxOf("u"), false) {
			t.Fatalf("old ruleset lost on %s", body)
		}
		f.mu.Lock()
		f.etag = `"v1"`
		f.body = defaultRuleset
		f.mu.Unlock()
		eventually(t, 3*time.Second, "recovery", func() bool { return c.Status().LastError == "" })
	}
}

func TestHTTPStatusIsTheLastError(t *testing.T) {
	f := newFakeServer(t)
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	log := &errorLog{}
	c := newClient(t, f, WithStream(false), WithOnError(log.add))
	if c.Ready() {
		t.Fatal("Ready() on 401")
	}
	if c.Status().LastError != "http_401" {
		t.Fatalf("LastError = %q", c.Status().LastError)
	}
	for _, m := range log.messages() {
		if strings.Contains(m, testKey) || strings.Contains(m, f.url()) {
			t.Fatalf("onError text leaks: %q", m)
		}
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFakeServer(t)
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1:1/elsewhere")
		w.WriteHeader(302)
	})
	c := newClient(t, f, WithStream(false))
	if c.Ready() {
		t.Fatal("Ready() on a redirect")
	}
	if c.Status().LastError != "http_302" {
		t.Fatalf("LastError = %q", c.Status().LastError)
	}
}

func TestNetworkErrorTextHasNoURL(t *testing.T) {
	log := &errorLog{}
	c := Create("http://127.0.0.1:1/secret-path", testKey, WithStream(false), WithOnError(log.add), WithPollInterval(time.Hour))
	t.Cleanup(func() { _ = c.Close() })
	if c.Ready() {
		t.Fatal("Ready()")
	}
	if c.Status().LastError != LastErrorNetwork {
		t.Fatalf("LastError = %q", c.Status().LastError)
	}
	for _, m := range log.messages() {
		if strings.Contains(m, "127.0.0.1") || strings.Contains(m, "secret-path") || strings.Contains(m, testKey) {
			t.Fatalf("onError text leaks: %q", m)
		}
	}
}

func TestStreamChangeReachesTheClient(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "stream connected", func() bool { return c.Status().Stream == StreamConnected })
	if !c.BoolVariation("checkout", ctxOf("u"), false) {
		t.Fatal("checkout should start on")
	}
	f.publish(rulesetBody(flagJSON("checkout", "true", "false", 4)))
	eventually(t, 3*time.Second, "the new ruleset", func() bool { return !c.BoolVariation("checkout", ctxOf("u"), true) })
	if c.Status().ETag == `"v1"` {
		t.Fatal("ETag did not move")
	}
}

func TestStreamEventWithHeldTagIsASyncWithNoFetch(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "stream connected", func() bool { return c.Status().Stream == StreamConnected })
	if n := len(f.requests("ruleset")); n != 1 {
		t.Fatalf("ruleset requests = %d, want 1 (connected with the held tag fetches nothing)", n)
	}
}

func TestStreamReconnectsAfterItDrops(t *testing.T) {
	f := newFakeServer(t)
	var drops atomic.Int32
	f.set("stream", func(w http.ResponseWriter, r *http.Request) {
		drops.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		w.(http.Flusher).Flush()
	})
	c := newClient(t, f, WithPollInterval(80*time.Millisecond))
	c.Ready()
	eventually(t, 5*time.Second, "several stream opens", func() bool { return drops.Load() >= 3 })
}

func TestPollingWhenStreamIsOff(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithStream(false))
	c.Ready()
	if c.Status().Stream != StreamOff {
		t.Fatalf("Stream = %q", c.Status().Stream)
	}
	f.publish(rulesetBody(flagJSON("checkout", "true", "false", 4)))
	eventually(t, 3*time.Second, "polled change", func() bool { return !c.BoolVariation("checkout", ctxOf("u"), true) })
	if len(f.requests("stream")) != 0 {
		t.Fatal("a stream was opened with stream off")
	}
}

func TestFailedFetchAfterAStreamEventEndsTheStream(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "stream connected", func() bool { return c.Status().Stream == StreamConnected })
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	f.publish(rulesetBody(flagJSON("checkout", "true", "false", 4)))
	eventually(t, 3*time.Second, "http_500 and a dropped stream", func() bool {
		s := c.Status()
		return s.LastError == "http_500" && s.Stream != StreamConnected
	})
}

func TestStatusAfterClose(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f)
	c.Ready()
	_ = c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("Stream = %q", c.Status().Stream)
	}
	if !c.BoolVariation("checkout", ctxOf("u"), false) {
		t.Fatal("variation should still read the held ruleset after close")
	}
}

func TestCloseIsIdempotentAndBounded(t *testing.T) {
	f := newFakeServer(t)
	f.hang("ruleset")
	c := Create(f.url(), testKey, WithRequestTimeout(time.Second))
	start := time.Now()
	within(t, 3*time.Second, "Close", func() { _ = c.Close(); _ = c.Close() })
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
	if c.Ready() {
		t.Fatal("Ready() true after a close that cut the first load")
	}
}

func TestReconnectDelayFollowsContract73(t *testing.T) {
	cases := []struct {
		stream   bool
		failures int
		jitter   float64
		poll     time.Duration
		want     time.Duration
	}{
		{true, 1, 0.5, time.Hour, 1 * time.Second},           // 2^1 s... capped by exp: 1s << 1 = 2s, times 0.5
		{true, 3, 1.0, time.Hour, 8 * time.Second},           // 1s << 3
		{true, 20, 0.5, 24 * time.Hour, 32768 * time.Second}, // the exponent stops at 16: 65536 s, times 0.5
		{true, 10, 1.0, 30 * time.Second, 30 * time.Second},
		{false, 5, 0.5, 45 * time.Second, 45 * time.Second}, // stream off: the poll interval, no jitter
	}
	for _, tc := range cases {
		c := Create("http://127.0.0.1:1", testKey, WithStream(tc.stream), WithPollInterval(tc.poll), withJitter(func() float64 { return tc.jitter }),
			withSleep(func(ctx context.Context, d time.Duration) bool { <-ctx.Done(); return false }))
		if got := c.delay(tc.failures); got != tc.want {
			t.Errorf("stream=%v failures=%d jitter=%v: delay = %v, want %v", tc.stream, tc.failures, tc.jitter, got, tc.want)
		}
		_ = c.Close()
	}
}
