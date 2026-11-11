package toggleslash

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(t *testing.T, f *fakeServer, opts ...Option) (*Client, *errs) {
	t.Helper()
	e := &errs{}
	all := append([]Option{WithOnError(e.add), WithStream(false)}, opts...)
	c := New(f.URL, "sdk-key", all...)
	closeAfter(t, c)
	return c, e
}

func TestServesFromTheLoadedRuleset(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	d := c.BoolVariationDetail("f", ctxFor("u"), false)
	if !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("detail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.LastError != "" || st.Stream != StreamOff {
		t.Fatalf("status = %+v", st)
	}
}

func TestNotReadyServesTheFallback(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	c, e := newClient(t, f)
	if c.Ready() {
		t.Fatal("ready on a 500")
	}
	d := c.BoolVariationDetail("f", ctxFor("u"), true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		t.Fatalf("detail = %+v", d)
	}
	if c.Status().LastError != "http_500" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
	eventually(t, "onError", func() bool { return len(e.all()) > 0 })
	if got := e.all()[0]; strings.Contains(got, f.URL) {
		t.Fatalf("error carries the URL: %q", got)
	}
}

func TestTypeMismatchIsNotCounted(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f)
	c.Ready()
	d := c.StringVariationDetail("f", ctxFor("u"), "x")
	if d.Value != "x" || d.VariationIndex != nil || d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("detail = %+v", d)
	}
	c.Flush()
	if n := len(f.requests("/sdk/v1/events")); n != 0 {
		t.Fatalf("a mismatch was counted: %d event requests", n)
	}
}

func TestMissingFlagKeepsTheEvaluatorsReason(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f)
	c.Ready()
	d := c.BoolVariationDetail("nope", ctxFor("u"), true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrFlagNotFound {
		t.Fatalf("detail = %+v", d)
	}
}

func TestInvalidContext(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f)
	c.Ready()
	d := c.BoolVariationDetail("f", Context{}, true)
	if !d.Value || d.Reason.Code != ErrInvalidContext {
		t.Fatalf("detail = %+v", d)
	}
}

func TestIntegerAccessor(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, rulesetBody(strings.Join([]string{
			flagJSON("two", 1, `[0,2.0]`),
			flagJSON("frac", 1, `[0,2.5]`),
			flagJSON("big", 1, `[0,9223372036854775807]`),
			flagJSON("edge", 1, `[0,9223372036853775360]`),
			flagJSON("neg", 1, `[0,-9223372036854775808]`),
			flagJSON("str", 1, `[0,"a"]`),
		}, ",")))
	}
	c, _ := newClient(t, f)
	c.Ready()
	if v := c.IntVariation("two", ctxFor("u"), 9); v != 2 {
		t.Fatalf("2.0 = %d", v)
	}
	for _, key := range []string{"frac", "big", "str"} {
		d := c.IntVariationDetail(key, ctxFor("u"), 7)
		if d.Value != 7 || d.Reason.Code != ErrTypeMismatch {
			t.Fatalf("%s: %+v", key, d)
		}
	}
	if v := c.IntVariation("edge", ctxFor("u"), 7); v != 9223372036853775360 {
		t.Fatalf("edge = %d", v)
	}
	if v := c.IntVariation("neg", ctxFor("u"), 7); v != -9223372036854775808 {
		t.Fatalf("neg = %d", v)
	}
	if v := c.FloatVariation("frac", ctxFor("u"), 7); v != 2.5 {
		t.Fatalf("float = %v", v)
	}
}

func TestJSONFallbackRules(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, rulesetBody(strings.Join([]string{
			flagJSON("obj", 1, `[0,{"a":1}]`),
			flagJSON("arr", 1, `[0,[1]]`),
			flagJSON("nul", 1, `[0,null]`),
		}, ",")))
	}
	c, _ := newClient(t, f)
	c.Ready()
	cx := ctxFor("u")
	if d := c.JSONVariationDetail("obj", cx, NullValue()); d.Value.Kind() != KindObject || d.VariationIndex == nil {
		t.Fatalf("null fallback must accept anything: %+v", d)
	}
	if d := c.JSONVariationDetail("obj", cx, ArrayValue()); d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("object served for an array fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("arr", cx, ObjectValue(NewObject())); d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("array served for an object fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", cx, StringValue("x")); d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("served null for a string fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", cx, NullValue()); d.VariationIndex == nil || !d.Value.IsNull() {
		t.Fatalf("null for null: %+v", d)
	}
}

func TestEvaluationThatPanicsGivesTheFallback(t *testing.T) {
	f := newFake(t)
	c, e := newClient(t, f)
	c.Ready()
	d := evaluate(c, "f", ctxFor("u"), false, BoolValue, func(Value) (bool, bool) { panic("boom") })
	if d.Value || d.VariationIndex != nil || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("detail = %+v", d)
	}
	eventually(t, "onError", func() bool { return len(e.all()) == 1 })
	c.Flush()
	if n := len(f.requests("/sdk/v1/events")); n != 0 {
		t.Fatal("a failed evaluation was counted")
	}
}

func TestConditionalRequest(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, rulesetBody(boolFlag))
	}
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), WithFlushInterval(time.Hour))
	c.Ready()
	k.release(t, 30*time.Second)
	eventually(t, "second ruleset request", func() bool { return len(f.requests("/sdk/v1/ruleset")) >= 2 })
	reqs := f.requests("/sdk/v1/ruleset")
	if reqs[0].Header.Get("If-None-Match") != "" {
		t.Fatal("first request was conditional")
	}
	if reqs[1].Header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("second If-None-Match = %q", reqs[1].Header.Get("If-None-Match"))
	}
}

func TestBadRulesetKeepsTheLastGoodOne(t *testing.T) {
	f := newFake(t)
	var n atomic.Int64
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, rulesetBody(boolFlag))
			return
		}
		_, _ = io.WriteString(w, `{"flags":{},"segments":[]}`)
	}
	k := newFakeClock()
	k.auto = true
	c, _ := newClient(t, f, withClock(k), WithPollInterval(time.Second))
	c.Ready()
	eventually(t, "bad_ruleset", func() bool { return c.Status().LastError == "bad_ruleset" })
	if !c.BoolVariation("f", ctxFor("u"), false) {
		t.Fatal("the last good ruleset was dropped")
	}
	if c.Status().ETag != `"v1"` {
		t.Fatalf("etag = %q", c.Status().ETag)
	}
}

func TestBadShapes(t *testing.T) {
	for name, body := range map[string]string{
		"array":          `[]`,
		"no segments":    `{"flags":[]}`,
		"flags not list": `{"flags":1,"segments":[]}`,
		"garbage":        `{`,
		"empty":          ``,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.ruleset = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }
			c, _ := newClient(t, f)
			if c.Ready() || c.Status().LastError != "bad_ruleset" {
				t.Fatalf("ready=%v status=%+v", c.Ready(), c.Status())
			}
		})
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}
	c, _ := newClient(t, f)
	if c.Ready() || c.Status().LastError != "http_302" {
		t.Fatalf("status = %+v", c.Status())
	}
}

func TestNotModifiedWithNoRulesetIsAnError(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) }
	c, _ := newClient(t, f)
	if c.Ready() || c.Status().LastError != "http_304" {
		t.Fatalf("status = %+v", c.Status())
	}
}

func TestBodyOverTheTimeoutIsATimeout(t *testing.T) {
	f := newFake(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	c, _ := newClient(t, f, WithRequestTimeout(150*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("ready was not bounded by the request timeout")
	}
	if c.Status().LastError != "timeout" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
}

func TestVariationDoesNotWaitOnTheNetwork(t *testing.T) {
	f := newFake(t)
	var hang atomic.Bool
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, rulesetBody(boolFlag))
	}
	f.events = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	k := newFakeClock()
	k.auto = true
	c, _ := newClient(t, f, withClock(k), WithPollInterval(time.Second), WithFlushInterval(time.Second), WithRequestTimeout(5*time.Second))
	c.Ready()
	hang.Store(true)
	c.BoolVariation("f", ctxFor("u"), false)
	eventually(t, "a request in flight", func() bool {
		return len(f.requests("/sdk/v1/events")) > 0 && len(f.requests("/sdk/v1/ruleset")) > 1
	})
	start := time.Now()
	for i := 0; i < 1000; i++ {
		c.BoolVariation("f", ctxFor("u"), false)
		_ = c.Status()
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("variation or status waited on a request")
	}
}

func TestRetryAfterIsCappedOnEveryRoute(t *testing.T) {
	t.Run("ruleset", func(t *testing.T) {
		f := newFake(t)
		var n atomic.Int64
		f.ruleset = func(w http.ResponseWriter, r *http.Request) {
			if n.Add(1) == 1 {
				w.Header().Set("ETag", `"v1"`)
				_, _ = io.WriteString(w, rulesetBody(boolFlag))
				return
			}
			w.Header().Set("Retry-After", "100000")
			w.WriteHeader(429)
		}
		k := newFakeClock()
		c, _ := newClient(t, f, withClock(k), WithPollInterval(time.Second), WithFlushInterval(24*time.Hour))
		c.Ready()
		k.release(t, time.Second)
		eventually(t, "429", func() bool { return c.Status().LastError == "http_429" })
		if !k.sawWait(300 * time.Second) {
			t.Fatalf("waits = %v", k.waitList())
		}
	})
	t.Run("stream", func(t *testing.T) {
		f := newFake(t)
		f.stream = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "100000")
			w.WriteHeader(429)
		}
		k := newFakeClock()
		c, _ := newClient(t, f, withClock(k), WithStream(true), WithFlushInterval(24*time.Hour))
		c.Ready()
		if !k.sawWait(300 * time.Second) {
			t.Fatalf("waits = %v", k.waitList())
		}
	})
	t.Run("events", func(t *testing.T) {
		f := newFake(t)
		f.events = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "100000")
			w.WriteHeader(429)
		}
		k := newFakeClock()
		c, _ := newClient(t, f, withClock(k), WithFlushInterval(24*time.Hour))
		c.Ready()
		c.BoolVariation("f", ctxFor("u"), false)
		c.Flush()
		c.usageMu.Lock()
		held := c.usageHold.Sub(k.Now())
		c.usageMu.Unlock()
		if held != 300*time.Second {
			t.Fatalf("hold = %v", held)
		}
		c.Flush() // during the hold: sends nothing
		if n := len(f.requests("/sdk/v1/events")); n != 1 {
			t.Fatalf("event requests = %d", n)
		}
	})
}

func TestRetryAfterParsing(t *testing.T) {
	for in, want := range map[string]time.Duration{"5": 5 * time.Second, "7": 7 * time.Second, "0": 0, "301": 300 * time.Second} {
		h := http.Header{"Retry-After": {in}}
		if got, ok := retryAfter(h); !ok || got != want {
			t.Errorf("%q = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-1", "1.5", "Wed, 21 Oct 2026 07:28:00 GMT", "5s", "+5", "0x10"} {
		if _, ok := retryAfter(http.Header{"Retry-After": {in}}); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	c := &Client{stream: true, poll: 30 * time.Second, jitter: func() float64 { return 0 }}
	// The ceiling is 2^failures seconds, at most the poll interval; jitter 0 gives half of it.
	for failures, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 5: 15 * time.Second, 6: 15 * time.Second, 40: 15 * time.Second} {
		if got := c.delay(failures); got != want {
			t.Errorf("delay(%d) = %v, want %v", failures, got, want)
		}
	}
	c.jitter = func() float64 { return 0.9999 }
	if got := c.delay(2); got >= 4*time.Second || got < 3900*time.Millisecond {
		t.Errorf("jittered delay = %v", got)
	}
	c.stream = false
	if got := c.delay(5); got != 30*time.Second {
		t.Errorf("no stream: %v", got)
	}
}

func TestHeadersSent(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f)
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Flush()
	allowed := map[string]bool{"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Content-Length": true, "Accept-Encoding": true}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) < 2 {
		t.Fatal("expected a ruleset and an events request")
	}
	for _, r := range f.reqs {
		for name := range r.Header {
			if !allowed[name] {
				t.Errorf("%s sent %s", r.Path, name)
			}
		}
		if r.Header.Get("Authorization") != "Bearer sdk-key" {
			t.Errorf("%s authorization = %q", r.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept-Encoding") != "" {
			t.Errorf("%s asked for a compressed answer", r.Path)
		}
	}
}

func TestStreamDoesNotFetchWhenTheTagIsHeld(t *testing.T) {
	f := newFake(t)
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	c, _ := newClient(t, f, WithStream(true), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	time.Sleep(50 * time.Millisecond)
	if n := f.rulesetN.Load(); n != 1 {
		t.Fatalf("ruleset requests = %d", n)
	}
	if c.Status().LastError != "" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
}

func TestStreamChangeFetchesTheRuleset(t *testing.T) {
	f := newFake(t)
	var version atomic.Int64
	version.Store(1)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v`+itoa(int(version.Load()))+`"`)
		_, _ = io.WriteString(w, rulesetBody(flagJSON("f", int(version.Load()), `[false,true]`)))
	}
	ready := make(chan struct{})
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		version.Store(2)
		_, _ = io.WriteString(w, "event: changed\ndata: \"v2\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	c, _ := newClient(t, f, WithStream(true), WithFlushInterval(time.Hour))
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	close(ready)
	eventually(t, "etag v2", func() bool { return c.Status().ETag == `"v2"` })
}

func TestStreamEndedCleanlyLeavesLastError(t *testing.T) {
	f := newFake(t)
	f.stream = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), WithStream(true), WithFlushInterval(24*time.Hour))
	c.Ready()
	eventually(t, "stream request", func() bool { return len(f.requests("/sdk/v1/stream")) > 0 })
	eventually(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
	if c.Status().LastError != "" {
		t.Fatalf("a clean end set lastError %q", c.Status().LastError)
	}
}

func TestStreamFetchFailureEndsTheStream(t *testing.T) {
	f := newFake(t)
	var n atomic.Int64
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, rulesetBody(boolFlag))
			return
		}
		w.WriteHeader(503)
	}
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v9\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), WithStream(true), WithFlushInterval(24*time.Hour))
	c.Ready()
	eventually(t, "http_503", func() bool { return c.Status().LastError == "http_503" })
	eventually(t, "disconnected", func() bool { return c.Status().Stream == StreamDisconnected })
}

func TestStreamWatchdogAndKeepAlive(t *testing.T) {
	f := newFake(t)
	var quiet atomic.Bool
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		for !quiet.Load() {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(40 * time.Millisecond):
				_, _ = io.WriteString(w, ": keep-alive\n\n")
				w.(http.Flusher).Flush()
			}
		}
		<-r.Context().Done()
	}
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), withWatchdog(200*time.Millisecond), WithStream(true), WithFlushInterval(24*time.Hour))
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := c.Status().LastSyncAt
	time.Sleep(700 * time.Millisecond) // three watchdog periods of keep-alives
	if c.Status().Stream != StreamConnected {
		t.Fatal("keep-alives did not restart the watchdog")
	}
	if !c.Status().LastSyncAt.Equal(before) {
		t.Fatal("a keep-alive moved lastSyncAt")
	}
	quiet.Store(true)
	eventually(t, "watchdog drop", func() bool {
		s := c.Status()
		return s.Stream == StreamDisconnected && s.LastError == "timeout"
	})
}

func TestStreamOpenTimeout(t *testing.T) {
	f := newFake(t)
	f.stream = func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), withJitter(func() float64 { return 0 }), WithStream(true), WithRequestTimeout(150*time.Millisecond), WithFlushInterval(24*time.Hour))
	c.Ready()
	eventually(t, "timeout", func() bool { return c.Status().LastError == "timeout" })
}

func TestHookSeesAConsistentPairUnderSwaps(t *testing.T) {
	f := newFake(t)
	var version atomic.Int64
	version.Store(1)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		v := int(version.Add(1))
		w.Header().Set("ETag", `"v`+itoa(v)+`"`)
		_, _ = io.WriteString(w, rulesetBody(flagJSON("f", v, `[false,true]`)))
	}
	var bad atomic.Int64
	var seen atomic.Int64
	k := newFakeClock()
	k.auto = true
	hook := func(e EvaluationEvent) {
		seen.Add(1)
		if e.FlagVersion == nil || e.ETag != `"v`+itoa(*e.FlagVersion)+`"` {
			bad.Add(1)
		}
	}
	c, _ := newClient(t, f, withClock(k), WithPollInterval(time.Millisecond), WithAfterEvaluation(hook))
	c.Ready()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				c.BoolVariation("f", ctxFor("u"), false)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d of %d hook events paired an etag with another version", bad.Load(), seen.Load())
	}
}

func TestHookPanicGoesToOnErrorAndTheAnswerIsTheSame(t *testing.T) {
	f := newFake(t)
	c, e := newClient(t, f, WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }), WithFlushInterval(time.Hour))
	c.Ready()
	if !c.BoolVariation("f", ctxFor("u"), false) {
		t.Fatal("hook changed the answer")
	}
	if len(e.all()) != 1 {
		t.Fatalf("errors = %v", e.all())
	}
	c.Flush()
	if len(f.requests("/sdk/v1/events")) != 1 {
		t.Fatal("the evaluation was not counted")
	}
}

func TestOnErrorThatPanicsIsIgnored(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	c := New(f.URL, "k", WithStream(false), WithOnError(func(error) { panic("x") }))
	closeAfter(t, c)
	if c.Ready() {
		t.Fatal("ready")
	}
}

func TestHookEventFields(t *testing.T) {
	f := newFake(t)
	var got EvaluationEvent
	c, _ := newClient(t, f, WithAfterEvaluation(func(e EvaluationEvent) { got = e }))
	c.Ready()
	c.StringVariation("f", ctxFor("u"), "fb")
	if got.FlagKey != "f" || got.VariationIndex != nil || got.Reason.Code != ErrTypeMismatch ||
		got.Value.Kind() != KindString || got.ETag != `"v1"` || got.FlagVersion == nil || *got.FlagVersion != 3 {
		t.Fatalf("event = %+v", got)
	}
	c.BoolVariation("missing", ctxFor("u"), true)
	if got.FlagVersion != nil || got.Value.Kind() != KindBool {
		t.Fatalf("event = %+v", got)
	}
}

func TestCallbacksMayCallBackIn(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	var cp atomic.Pointer[Client]
	done := make(chan struct{}, 1)
	c := New(f.URL, "k", WithStream(false), WithOnError(func(error) {
		cl := cp.Load()
		if cl == nil {
			return
		}
		select {
		case <-done:
			return
		default:
		}
		_ = cl.Ready()
		_ = cl.Status()
		cl.BoolVariation("f", ctxFor("u"), false)
		cl.Close()
		done <- struct{}{}
	}))
	cp.Store(c)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// the first error may have fired before the pointer was stored
		c.Flush()
	}
	closeAfter(t, c)
}

func TestReadyFromACallbackDoesNotWait(t *testing.T) {
	f := newFake(t)
	release := make(chan struct{})
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(500)
	}
	var cp atomic.Pointer[Client]
	res := make(chan bool, 4)
	c := New(f.URL, "k", WithStream(false), WithOnError(func(error) {
		if cl := cp.Load(); cl != nil {
			res <- cl.Ready()
		}
	}))
	cp.Store(c)
	closeAfter(t, c)
	close(release)
	if c.Ready() {
		t.Fatal("ready")
	}
	select {
	case <-res:
	case <-time.After(10 * time.Second):
		t.Fatal("Ready blocked in a callback")
	}
}
