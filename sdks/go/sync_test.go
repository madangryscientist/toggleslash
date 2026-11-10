package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder keeps the headers and paths of every request a server saw.
type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

type recorded struct {
	method, path string
	header       http.Header
	ifNoneMatch  string
	hasINM       bool
}

func (r *recorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		_, has := req.Header["If-None-Match"]
		r.reqs = append(r.reqs, recorded{req.Method, req.URL.Path, req.Header.Clone(), req.Header.Get("If-None-Match"), has})
		r.mu.Unlock()
		next.ServeHTTP(w, req)
	})
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.reqs...)
}

func (r *recorder) count(path string) int {
	n := 0
	for _, q := range r.all() {
		if q.path == path {
			n++
		}
	}
	return n
}

func TestIfNoneMatchIsTheETagAsReceivedAndOnlyWhenHeld(t *testing.T) {
	var rec recorder
	url := serve(t, rec.wrap(rulesetHandler(boolRuleset(), `W/"weak tag"`, nil)))
	c := newTestClient(t, url, nil, pollOnly()...)
	c.Ready()
	eventually(t, 3*time.Second, "a conditional poll", func() bool { return rec.count("/sdk/v1/ruleset") >= 3 })
	reqs := rec.all()
	if reqs[0].hasINM {
		t.Fatalf("the first request carried If-None-Match %q", reqs[0].ifNoneMatch)
	}
	for _, q := range reqs[1:] {
		if q.path == "/sdk/v1/ruleset" && q.ifNoneMatch != `W/"weak tag"` {
			t.Fatalf("If-None-Match = %q", q.ifNoneMatch)
		}
	}
	// A 304 is a sync: the clock moves and the ruleset stays.
	first := c.Status().LastSyncAt
	eventually(t, 3*time.Second, "LastSyncAt to move on a 304", func() bool { return c.Status().LastSyncAt.After(first) })
	if !c.BoolVariation("checkout", someone, false) {
		t.Fatal("ruleset lost after a 304")
	}
}

func TestNoETagMeansNoIfNoneMatch(t *testing.T) {
	var rec recorder
	url := serve(t, rec.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(boolRuleset())) })))
	c := newTestClient(t, url, nil, pollOnly()...)
	c.Ready()
	eventually(t, 3*time.Second, "two polls", func() bool { return rec.count("/sdk/v1/ruleset") >= 2 })
	for _, q := range rec.all() {
		if q.hasINM {
			t.Fatalf("If-None-Match sent with no ETag held: %q", q.ifNoneMatch)
		}
	}
	if c.Status().ETag != nil {
		t.Fatal("ETag set though the server sent none")
	}
}

func TestOnlyAJSONObjectWithFlagsAndSegmentsReplacesTheRuleset(t *testing.T) {
	bodies := []string{
		`not json`, `[]`, `null`, `"x"`, `{}`, `{"flags":[]}`, `{"flags":{},"segments":[]}`, `{"flags":[],"segments":{}}`,
		`{"flags":[],"segments":[]} trailing`, ``,
	}
	for _, bad := range bodies {
		t.Run(bad, func(t *testing.T) {
			var serveBad atomic.Bool
			url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if serveBad.Load() {
					w.Header().Set("ETag", `"bad"`)
					_, _ = w.Write([]byte(bad))
					return
				}
				w.Header().Set("ETag", `"good"`)
				_, _ = w.Write([]byte(boolRuleset()))
			}))
			var errs errorLog
			c := newTestClient(t, url, nil, pollOnly(WithOnError(errs.add))...)
			c.Ready()
			serveBad.Store(true)
			eventually(t, 3*time.Second, "bad_ruleset", func() bool { return c.Status().LastError == SyncBadRuleset })
			st := c.Status()
			if st.ETag == nil || *st.ETag != `"good"` || !c.BoolVariation("checkout", someone, false) {
				t.Fatalf("the last good ruleset was lost: %+v", st)
			}
			if errs.count() == 0 {
				t.Fatal("no onError for a bad ruleset")
			}
			serveBad.Store(false)
			eventually(t, 3*time.Second, "recovery to clear lastError", func() bool { return c.Status().LastError == "" })
		})
	}
}

func TestEmptyFlagsAndSegmentsIsAValidRuleset(t *testing.T) {
	url := serve(t, rulesetHandler(`{"flags":[],"segments":[],"extra":1}`, `"e"`, nil))
	c := newTestClient(t, url, nil, pollOnly()...)
	if !c.Ready() {
		t.Fatal("an empty ruleset must load")
	}
}

func TestHTTPFailureCodes(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500, 503, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status == 302 {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(status)
			}))
			c := newTestClient(t, url, nil, pollOnly()...)
			if c.Ready() {
				t.Fatal("ready")
			}
			if got, want := c.Status().LastError, SyncErrorCode(fmt.Sprintf("http_%d", status)); got != want {
				t.Fatalf("lastError %q, want %q", got, want)
			}
		})
	}
}

func TestARedirectIsNotFollowedSoTheKeyGoesNowhereElse(t *testing.T) {
	var elsewhere atomic.Int64
	other := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = w.Write([]byte(boolRuleset()))
	}))
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	c := newTestClient(t, url, nil, pollOnly()...)
	if c.Ready() || elsewhere.Load() != 0 {
		t.Fatalf("ready=%v, the redirect target saw %d requests", c.Ready(), elsewhere.Load())
	}
	if got := c.Status().LastError; got != "http_307" {
		t.Fatalf("lastError %q", got)
	}
}

func TestANetworkFailureIsNetworkAndNamesNothing(t *testing.T) {
	var errs errorLog
	c := newTestClient(t, "http://127.0.0.1:1", nil, pollOnly(WithOnError(errs.add))...)
	if c.Ready() {
		t.Fatal("ready with nothing listening")
	}
	if got := c.Status().LastError; got != SyncNetwork {
		t.Fatalf("lastError %q", got)
	}
	for _, e := range errs.all() {
		if strings.Contains(e.Error(), "127.0.0.1") || strings.Contains(e.Error(), "sdk-key") {
			t.Fatalf("an error names the host or the key: %q", e.Error())
		}
	}
	c.Close()
	// Failures that Close causes are not reported.
	n := errs.count()
	time.Sleep(50 * time.Millisecond)
	if errs.count() != n {
		t.Fatal("a failure was reported after Close")
	}
}

func TestAnUnusableURLIsAnetworkFailureNotAPanic(t *testing.T) {
	for _, bad := range []string{"", "not a url", "://", "http://[::1", "\x00"} {
		c := New(bad, "k", pollOnly()...)
		if c.Ready() {
			t.Fatalf("%q: ready", bad)
		}
		_ = c.BoolVariation("x", someone, false)
		c.Close()
	}
}

// sseServer serves the ruleset and a stream that writes what the test sends it.
type sseServer struct {
	events  chan string
	rec     recorder
	etag    atomic.Value
	body    atomic.Value
	streams atomic.Int64
}

func newSSE(t *testing.T) (*sseServer, string) {
	s := &sseServer{events: make(chan string, 64)}
	s.etag.Store(`"v1"`)
	s.body.Store(rulesetJSON(flagJSON("checkout", 1, "[false,true]", 0)))
	url := serve(t, s.rec.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			tag := s.etag.Load().(string)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", tag)
			_, _ = w.Write([]byte(s.body.Load().(string)))
		case "/sdk/v1/stream":
			s.streams.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			for {
				select {
				case ev := <-s.events:
					_, _ = w.Write([]byte(ev))
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		}
	})))
	return s, url
}

func TestStreamChangedEventFetchesOnlyANewTag(t *testing.T) {
	s, url := newSSE(t)
	c := newTestClient(t, url, nil, WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	s.events <- "event: connected\ndata: \"v1\"\n\n"
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := s.rec.count("/sdk/v1/ruleset")
	s.events <- "event: changed\ndata: \"v1\"\n\n" // the tag held: no fetch
	s.events <- "event: other\ndata: \"zzz\"\n\n"  // not an event it acts on
	s.events <- ": keep-alive\n\n"
	time.Sleep(100 * time.Millisecond)
	if got := s.rec.count("/sdk/v1/ruleset"); got != before {
		t.Fatalf("fetched %d times for tags it already held", got-before)
	}
	s.etag.Store(`"v2"`)
	s.body.Store(rulesetJSON(flagJSON("checkout", 2, "[false,true]", 1)))
	s.events <- "event: changed\ndata: \"v2\"\n\n"
	eventually(t, 3*time.Second, "the new ruleset", func() bool { return c.BoolVariation("checkout", someone, false) })
	if st := c.Status(); st.ETag == nil || *st.ETag != `"v2"` || st.Stream != StreamConnected {
		t.Fatalf("status %+v", st)
	}
	// The stream request carries the right headers.
	for _, q := range s.rec.all() {
		if q.path == "/sdk/v1/stream" && (q.header.Get("Accept") != "text/event-stream" || q.header.Get("Authorization") != "Bearer sdk-key-secret") {
			t.Fatalf("stream headers %v", q.header)
		}
	}
}

func TestConnectedEventClearsLastError(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	events := make(chan string, 4)
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			if fail.Load() {
				w.WriteHeader(500)
				return
			}
			w.Header().Set("ETag", `"c"`)
			_, _ = w.Write([]byte(boolRuleset()))
		case "/sdk/v1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			select {
			case ev := <-events:
				_, _ = w.Write([]byte(ev))
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
			<-r.Context().Done()
		}
	}))
	c := newTestClient(t, url, nil, WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	if c.Status().LastError != "http_500" {
		t.Fatalf("lastError %q", c.Status().LastError)
	}
	fail.Store(false)
	events <- "event: connected\ndata: \"c\"\n\n"
	eventually(t, 3*time.Second, "the connected event to clear lastError", func() bool {
		st := c.Status()
		return st.Stream == StreamConnected && st.LastError == "" && st.ETag != nil
	})
}

func TestStreamNon200IsHTTPStatusAndPollsAfterwards(t *testing.T) {
	var rec recorder
	url := serve(t, rec.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/stream" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("ETag", `"p"`)
		_, _ = w.Write([]byte(boolRuleset()))
	})))
	c := newTestClient(t, url, func(cfg *config) { cfg.jitter = func() float64 { return 0 } },
		WithPollInterval(30*time.Millisecond), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "http_503 on the stream", func() bool { return c.Status().LastError == "http_503" })
	eventually(t, 3*time.Second, "a poll after the failed stream", func() bool { return rec.count("/sdk/v1/ruleset") >= 2 })
	if st := c.Status().Stream; st != StreamDisconnected && st != StreamConnecting {
		t.Fatalf("stream state %q", st)
	}
}

// A stream that was up and dropped is retried with no poll; one that never connected is followed by a poll.
func TestAStreamThatWasUpIsRetriedWithoutAPoll(t *testing.T) {
	var streams atomic.Int64
	var rec recorder
	url := serve(t, rec.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			w.Header().Set("ETag", `"q"`)
			_, _ = w.Write([]byte(boolRuleset()))
		case "/sdk/v1/stream":
			streams.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: connected\ndata: \"q\"\n\n"))
			// returning ends the response: the stream drops
		}
	})))
	c := newTestClient(t, url, func(cfg *config) { cfg.jitter = func() float64 { return 0 } },
		WithPollInterval(20*time.Millisecond), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, 5*time.Second, "several reconnects", func() bool { return streams.Load() >= 4 })
	if got := rec.count("/sdk/v1/ruleset"); got != 1 {
		t.Fatalf("%d ruleset fetches; a stream that was up must be retried without polling", got)
	}
}

func TestDelayIsBoundedAndJittered(t *testing.T) {
	cfg := newConfig("http://x", "k", WithPollInterval(30*time.Second))
	c := &Client{cfg: cfg}
	cases := []struct {
		failures int
		jitter   float64
		want     time.Duration
	}{
		{0, 0, 500 * time.Millisecond},
		{1, 0, time.Second},
		{2, 0, 2 * time.Second},
		{3, 0.5, 6 * time.Second},
		{5, 0, 15 * time.Second},
		{6, 0.999999, 29999985 * time.Microsecond},
		{40, 0, 15 * time.Second},
	}
	for _, tc := range cases {
		c.cfg.jitter = func() float64 { return tc.jitter }
		got := c.delay(tc.failures)
		if tc.jitter == 0.999999 {
			if got < 29*time.Second || got >= 30*time.Second {
				t.Errorf("failures %d: %s", tc.failures, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("failures %d jitter %v: %s, want %s", tc.failures, tc.jitter, got, tc.want)
		}
	}
	c.cfg.stream = false
	if got := c.delay(9); got != c.cfg.pollInterval {
		t.Errorf("stream off: %s", got)
	}
}

func TestNonPositiveDurationsFallBackToTheDefault(t *testing.T) {
	cfg := newConfig("u", "k", WithPollInterval(0), WithFlushInterval(-time.Second), WithRequestTimeout(0))
	if cfg.pollInterval != 30*time.Second || cfg.flushInterval != 30*time.Second || cfg.requestTimeout != 10*time.Second {
		t.Fatalf("%+v", cfg)
	}
}

// A real loopback socket that sends the connected event and then goes quiet: the watchdog ends it as a timeout.
func TestAQuietStreamIsDroppedAsATimeout(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			w.Header().Set("ETag", `"w"`)
			_, _ = w.Write([]byte(boolRuleset()))
		case "/sdk/v1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: connected\ndata: \"w\"\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	c := newTestClient(t, url, func(cfg *config) { cfg.idle = 200 * time.Millisecond },
		WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	eventually(t, 3*time.Second, "the watchdog to drop the stream as a timeout", func() bool {
		st := c.Status()
		return st.LastError == SyncTimeout && st.Stream != StreamConnected
	})
}

// A keep-alive comment is a byte, and any byte restarts the whole idle window.
func TestAKeepAliveRestartsTheIdleWindow(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			w.Header().Set("ETag", `"k"`)
			_, _ = w.Write([]byte(boolRuleset()))
		case "/sdk/v1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: connected\ndata: \"k\"\n\n"))
			w.(http.Flusher).Flush()
			// Five keep-alives 120 ms apart: 600 ms in all against a 300 ms window, then silence.
			for range 5 {
				select {
				case <-time.After(120 * time.Millisecond):
					_, _ = w.Write([]byte(": keep-alive\n\n"))
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
			<-r.Context().Done()
		}
	}))
	c := newTestClient(t, url, func(cfg *config) { cfg.idle = 300 * time.Millisecond },
		WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	time.Sleep(500 * time.Millisecond) // past one idle window; only the keep-alives hold it open
	if st := c.Status(); st.Stream != StreamConnected || st.LastError == SyncTimeout {
		t.Fatalf("the stream was dropped despite keep-alives: %+v", st)
	}
	eventually(t, 3*time.Second, "the stream to time out once the keep-alives stop", func() bool { return c.Status().LastError == SyncTimeout })
}

func TestStreamOpenIsBoundedByTheRequestTimeout(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/stream" {
			<-r.Context().Done() // never sends headers
			return
		}
		_, _ = w.Write([]byte(boolRuleset()))
	}))
	c := newTestClient(t, url, nil, WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithRequestTimeout(150*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "the open to time out", func() bool { return c.Status().LastError == SyncTimeout })
}

func TestARulesetBodyThatNeverEndsIsBoundedByTheRequestTimeout(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte(`{"flags":[`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	c := newTestClient(t, url, nil, pollOnly(WithRequestTimeout(150*time.Millisecond))...)
	within(t, 3*time.Second, "Ready", func() { c.Ready() })
	if got := c.Status().LastError; got != SyncTimeout {
		t.Fatalf("lastError %q", got)
	}
}

// Retry-After on a ruleset 429 and on a stream 429 share one hold, which a later, shorter ask never shortens.
func TestRetryAfterAbove300IsHeldFor300OnTheRulesetAndStreamRoutes(t *testing.T) {
	for _, route := range []string{"/sdk/v1/ruleset", "/sdk/v1/stream"} {
		t.Run(route, func(t *testing.T) {
			var rec recorder
			url := serve(t, rec.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == route {
					w.Header().Set("Retry-After", "86400")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Header().Set("ETag", `"r"`)
				_, _ = w.Write([]byte(boolRuleset()))
			})))
			clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
			c := newTestClient(t, url, func(cfg *config) { cfg.now = clk.now },
				WithPollInterval(10*time.Millisecond), WithFlushInterval(time.Hour))
			c.Ready()
			eventually(t, 3*time.Second, "a 429 hold", func() bool {
				c.statusMu.Lock()
				defer c.statusMu.Unlock()
				return !c.holdUntil.IsZero()
			})
			c.statusMu.Lock()
			until := c.holdUntil
			c.statusMu.Unlock()
			if want := clk.now().Add(300 * time.Second); !until.Equal(want) {
				t.Fatalf("held until %s, want %s", until, want)
			}
			if got := c.Status().LastError; got != "http_429" {
				t.Fatalf("lastError %q", got)
			}
			// While held nothing else is asked of the server on either route.
			a := len(rec.all())
			time.Sleep(150 * time.Millisecond)
			if b := len(rec.all()); b != a {
				t.Fatalf("%d requests during the hold", b-a)
			}
		})
	}
}

func TestALater429NeverShortensARulesetHold(t *testing.T) {
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := &Client{cfg: newConfig("u", "k")}
	c.cfg.now = clk.now
	for _, header := range []string{"200", "5", "abc", "250"} {
		c.hold(&http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {header}}})
	}
	if want := clk.now().Add(250 * time.Second); !c.holdUntil.Equal(want) {
		t.Fatalf("held until %s, want %s", c.holdUntil, want)
	}
}

// Everything the SDK sends is in the contract's short list (plus the three headers Go's HTTP client adds on its own,
// which name no user or machine), and no request carries a context, a context key or an attribute.
func TestRequestsCarryOnlyAllowedHeadersAndNoContext(t *testing.T) {
	var rec recorder
	var events syncBuf
	url := serve(t, rec.wrap(rulesetHandler(boolRuleset(), `"hdr"`, events.handler())))
	secret := "context-key-should-never-be-sent-7f3a"
	attr := "attribute-value-should-never-be-sent-91c2"
	c := newTestClient(t, url, nil, pollOnly(WithFlushInterval(10*time.Millisecond))...)
	c.Ready()
	ctx := UserContext(secret).WithAttribute("email", String(attr))
	for range 5 {
		c.BoolVariation("checkout", ctx, false)
	}
	eventually(t, 3*time.Second, "a usage request", func() bool { return events.calls() > 0 })
	c.Close()
	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		// Added by net/http itself; see RUN.md.
		"User-Agent": true, "Accept-Encoding": true, "Content-Length": true,
	}
	for _, q := range rec.all() {
		for name, values := range q.header {
			if !allowed[name] {
				t.Errorf("%s %s carries header %s", q.method, q.path, name)
			}
			for _, v := range values {
				if strings.Contains(v, secret) || strings.Contains(v, attr) {
					t.Errorf("header %s carries the context", name)
				}
			}
		}
		if ua := q.header.Get("User-Agent"); ua != "" && ua != "Go-http-client/1.1" && ua != "Go-http-client/2.0" {
			t.Errorf("User-Agent %q", ua)
		}
	}
	for _, body := range events.bodies() {
		if strings.Contains(body, secret) || strings.Contains(body, attr) {
			t.Errorf("a usage body carries the context: %s", body)
		}
	}
	st := fmt.Sprintf("%+v", c.Status())
	if strings.Contains(st, "127.0.0.1") || strings.Contains(st, "sdk-key") || strings.Contains(st, secret) {
		t.Errorf("status names something it must not: %s", st)
	}
}

func TestCloseIsPromptAndLeavesNoStreamOpen(t *testing.T) {
	s, url := newSSE(t)
	c := newTestClient(t, url, nil, WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	s.events <- "event: connected\ndata: \"v1\"\n\n"
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	within(t, 3*time.Second, "Close", c.Close)
	if st := c.Status(); st.Stream != StreamClosed || st.LastError != "" {
		t.Fatalf("status %+v", st)
	}
	n := s.streams.Load()
	time.Sleep(100 * time.Millisecond)
	if s.streams.Load() != n {
		t.Fatal("the stream was reopened after Close")
	}
	// Evaluation still works after Close.
	if d := c.BoolVariationDetail("checkout", someone, true); d.Reason.Kind == ReasonError {
		t.Fatalf("after Close: %+v", d)
	}
}
