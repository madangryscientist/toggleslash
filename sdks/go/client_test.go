package toggleslash

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func TestLoadAndEvaluate(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("on", `[false,true]`, 1, 3), flagJSON("name", `["a","b"]`, 1, 1)))
	c := makeClient(t, f)
	if !c.Ready() {
		t.Fatal("Ready() = false after a good first load")
	}
	if !c.BoolVariation("on", ctxFor("u1"), false) {
		t.Fatal("BoolVariation(on) = false")
	}
	d := c.StringVariationDetail("name", ctxFor("u1"), "x")
	if d.Value != "b" || d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonKindFallthrough {
		t.Fatalf("StringVariationDetail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.LastError != "" || st.Stream != StreamOff {
		t.Fatalf("Status = %+v", st)
	}
}

func TestUnknownFlagAndNotReady(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(f.hang, nil, nil)
	log := &errorLog{}
	var events atomic.Int32
	c := makeClient(t, f, WithOnError(log.add), WithAfterEvaluation(func(EvaluationEvent) { events.Add(1) }), WithRequestTimeout(time.Hour))
	d := c.BoolVariationDetail("x", ctxFor("u"), true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonKindError || d.Reason.ErrorCode != ErrorCodeNotReady {
		t.Fatalf("not ready detail = %+v", d)
	}
	if events.Load() != 1 {
		t.Fatalf("hook ran %d times, want 1", events.Load())
	}
	if log.count() != 0 {
		t.Fatalf("onError told %v", log.all())
	}
	c.Close()
	if c.Ready() {
		t.Fatal("Ready() = true after Close with no ruleset")
	}
}

func TestFlagNotFound(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[1]`, 0, 1)))
	c := makeClient(t, f)
	c.Ready()
	d := c.IntVariationDetail("missing", ctxFor("u"), 7)
	if d.Value != 7 || d.Reason.Kind != ReasonKindError || d.Reason.ErrorCode != ErrorCodeFlagNotFound || d.VariationIndex != nil {
		t.Fatalf("detail = %+v", d)
	}
}

func TestTypeMismatch(t *testing.T) {
	f := newFake(t, rulesetJSON(
		flagJSON("s", `["x"]`, 0, 1),
		flagJSON("n", `[3]`, 0, 1),
		flagJSON("b", `[true]`, 0, 1),
		flagJSON("o", `[{"a":1}]`, 0, 1),
		flagJSON("z", `[null]`, 0, 1),
	))
	c := makeClient(t, f)
	c.Ready()
	ctx := ctxFor("u")
	mismatch := func(name string, reason Reason, value any, want any) {
		t.Helper()
		if reason.Kind != ReasonKindError || reason.ErrorCode != ErrorCodeTypeMismatch || value != want {
			t.Errorf("%s: value %v reason %+v", name, value, reason)
		}
	}
	bd := c.BoolVariationDetail("s", ctx, false)
	mismatch("bool on a string", bd.Reason, bd.Value, false)
	sd := c.StringVariationDetail("n", ctx, "fb")
	mismatch("string on a number", sd.Reason, sd.Value, "fb")
	fd := c.FloatVariationDetail("b", ctx, 1.5)
	mismatch("float on a bool", fd.Reason, fd.Value, 1.5)
	jd := c.JSONVariationDetail("o", ctx, jsonvalue.NewArray(nil))
	if jd.Reason.ErrorCode != ErrorCodeTypeMismatch {
		t.Errorf("array fallback on an object: %+v", jd.Reason)
	}
	jd = c.JSONVariationDetail("o", ctx, jsonvalue.Value{})
	if jd.Reason.Kind != ReasonKindFallthrough {
		t.Errorf("null fallback accepts any served value: %+v", jd.Reason)
	}
	jd = c.JSONVariationDetail("z", ctx, jsonvalue.NewString("x"))
	if jd.Reason.ErrorCode != ErrorCodeTypeMismatch {
		t.Errorf("null served against a string fallback: %+v", jd.Reason)
	}
}

func TestIntegerAccessorRange(t *testing.T) {
	cases := []struct {
		served string
		want   int64
		ok     bool
	}{
		{"2.0", 2, true},
		{"2.5", 9, false},
		{"-0", 0, true},
		{"9223372036854774784", 9223372036854774784, true},
		{"9223372036854775808", 9, false},
		{"1e30", 9, false},
		{"-9223372036854775808", -9223372036854775808, true},
		{"-9223372036854777856", 9, false},
		{"1e999", 9, false},
	}
	var flags []string
	for i, tc := range cases {
		flags = append(flags, flagJSON("f"+string(rune('a'+i)), "["+tc.served+"]", 0, 1))
	}
	f := newFake(t, rulesetJSON(flags...))
	c := makeClient(t, f)
	c.Ready()
	for i, tc := range cases {
		d := c.IntVariationDetail("f"+string(rune('a'+i)), ctxFor("u"), 9)
		if tc.ok && (d.Value != tc.want || d.Reason.Kind != ReasonKindFallthrough) {
			t.Errorf("%s: got %d %+v, want %d", tc.served, d.Value, d.Reason, tc.want)
		}
		if !tc.ok && (d.Value != 9 || d.Reason.ErrorCode != ErrorCodeTypeMismatch || d.VariationIndex != nil) {
			t.Errorf("%s: got %d %+v, want a mismatch", tc.served, d.Value, d.Reason)
		}
	}
	// The float accessor takes the same numbers, 1e999 as +Inf.
	if v := c.FloatVariation("fi", ctxFor("u"), 0); v <= 1e308 {
		t.Errorf("FloatVariation(1e999) = %v, want +Inf", v)
	}
}

func TestEvaluationFailed(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	log := &errorLog{}
	var hooked atomic.Int32
	c := makeClient(t, f, WithOnError(log.add),
		WithAfterEvaluation(func(EvaluationEvent) { hooked.Add(1) }),
		withEvaluator(func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result { panic("boom") }))
	c.Ready()
	d := c.BoolVariationDetail("a", ctxFor("secret-user"), false)
	if d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonKindError || d.Reason.ErrorCode != ErrorCodeEvaluationFailed {
		t.Fatalf("detail = %+v", d)
	}
	errs := log.all()
	if len(errs) != 1 || errs[0].Op != "evaluate" || errs[0].Code != "EVALUATION_FAILED" {
		t.Fatalf("onError got %v, want exactly one EVALUATION_FAILED", errs)
	}
	var pe *PanicError
	if !errors.As(errs[0], &pe) {
		t.Fatal("the error does not carry the panic")
	}
	if hooked.Load() != 1 {
		t.Fatalf("hook ran %d times", hooked.Load())
	}
	c.Close()
	for _, r := range f.requests("/sdk/v1/events") {
		if strings.Contains(string(r.body), `"count"`) {
			t.Fatalf("a failed evaluation was counted: %s", r.body)
		}
	}
}

func TestHookEvent(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[false,true]`, 1, 5)))
	var got EvaluationEvent
	c := makeClient(t, f, WithAfterEvaluation(func(ev EvaluationEvent) { got = ev }))
	c.Ready()
	ctx := ctxFor("u1")
	c.BoolVariation("a", ctx, false)
	if got.FlagKey != "a" || got.ETag != `"v1"` || got.FlagVersion == nil || *got.FlagVersion != 5 ||
		got.VariationIndex == nil || *got.VariationIndex != 1 || !jsonvalue.Equal(got.Context, ctx) {
		t.Fatalf("event = %+v", got)
	}
	c.BoolVariation("nope", ctx, false)
	if got.FlagVersion != nil || got.VariationIndex != nil || got.ETag != `"v1"` {
		t.Fatalf("event for an unknown flag = %+v", got)
	}
}

func TestHookAndOnErrorPanics(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	f.on(nil, nil, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var told atomic.Int32
	c := makeClient(t, f,
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }),
		WithOnError(func(error) { told.Add(1); panic("onError") }),
		WithFlushInterval(10*time.Millisecond))
	c.Ready()
	if !c.BoolVariation("a", ctxFor("u"), false) {
		t.Fatal("a panicking hook changed the answer")
	}
	if told.Load() < 1 {
		t.Fatal("a hook panic did not reach onError")
	}
	eventually(t, "a failing flush reported", 3*time.Second, func() bool { return told.Load() >= 2 })
	c.Close() // a panic in onError must not escape from the SDK goroutines or from Close
}

func TestReadyIsFixedOnceSettled(t *testing.T) {
	f := newFake(t, rulesetJSON())
	var calls atomic.Int32
	f.on(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(500)
			return
		}
		f.defaultRuleset(w, r)
	}, nil, nil)
	c := makeClient(t, f, WithPollInterval(5*time.Millisecond))
	if c.Ready() {
		t.Fatal("Ready() = true after a failed first load")
	}
	eventually(t, "a later load", 3*time.Second, func() bool { return c.Status().ETag != "" })
	if c.Ready() {
		t.Fatal("Ready() changed its answer once settled")
	}
}

func TestSyncFailures(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }, "http_401"},
		{"503", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, "http_503"},
		{"304 with nothing held", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) }, "http_304"},
		{"redirect is not followed", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}, "http_302"},
		{"not JSON", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) }, "bad_ruleset"},
		{"flags not an array", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"flags":{},"segments":[]}`)) }, "bad_ruleset"},
		{"segments missing", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"flags":[]}`)) }, "bad_ruleset"},
		{"an array", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }, "bad_ruleset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, "")
			f.on(tc.handler, nil, nil)
			log := &errorLog{}
			c := makeClient(t, f, WithOnError(log.add))
			if c.Ready() {
				t.Fatal("Ready() = true")
			}
			if st := c.Status(); st.LastError != tc.want || st.ETag != "" {
				t.Fatalf("Status = %+v, want lastError %s", st, tc.want)
			}
			// Ready settles before the loop goroutine tells onError, so wait for the call.
			eventually(t, "onError told", 3*time.Second, func() bool { return log.count() > 0 })
			errs := log.all()
			if len(errs) == 0 || errs[0].Op != "ruleset" || errs[0].Code != tc.want {
				t.Fatalf("onError got %v", errs)
			}
			if strings.Contains(errs[0].Error(), f.url()) || strings.Contains(errs[0].Error(), "sdk-test-key") {
				t.Fatalf("an error text leaks the URL or key: %q", errs[0].Error())
			}
		})
	}
}

func TestBadRulesetKeepsTheHeldOne(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	c := makeClient(t, f, WithPollInterval(5*time.Millisecond))
	c.Ready()
	f.set(`{"flags":"no"}`, `"v2"`)
	eventually(t, "bad_ruleset reported", 3*time.Second, func() bool { return c.Status().LastError == "bad_ruleset" })
	if st := c.Status(); st.ETag != `"v1"` {
		t.Fatalf("the held ETag changed to %q", st.ETag)
	}
	if !c.BoolVariation("a", ctxFor("u"), false) {
		t.Fatal("the held ruleset stopped answering")
	}
}

func TestNetworkFailure(t *testing.T) {
	f := newFake(t, "")
	url := f.url()
	f.close()
	log := &errorLog{}
	c := New(url, "k", WithStream(false), WithOnError(log.add), WithRequestTimeout(2*time.Second))
	defer c.Close()
	if c.Ready() {
		t.Fatal("Ready() = true")
	}
	if st := c.Status(); st.LastError != "network" {
		t.Fatalf("lastError = %q", st.LastError)
	}
	eventually(t, "onError told", 3*time.Second, func() bool { return log.count() > 0 })
	for _, e := range log.all() {
		if strings.Contains(e.Error(), url) || strings.Contains(e.Error(), "127.0.0.1") {
			t.Fatalf("a network error text carries the URL: %q", e.Error())
		}
	}
}

func TestRequestTimeoutCoversTheBody(t *testing.T) {
	f := newFake(t, "")
	f.on(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"slow"`)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"flags":[`))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-f.done:
		}
	}, nil, nil)
	c := makeClient(t, f, WithRequestTimeout(250*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("Ready() = true")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Ready took %v", took)
	}
	if st := c.Status(); st.LastError != "timeout" {
		t.Fatalf("lastError = %q, want timeout", st.LastError)
	}
}

func TestConditionalRequestAndSync(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithPollInterval(10*time.Second), WithFlushInterval(time.Hour))
	c.Ready()
	first := c.Status().LastSyncAt
	clock.waitTimer(t, 10*time.Second)
	clock.advance(10 * time.Second)
	eventually(t, "a conditional poll", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/ruleset")) == 2 })
	reqs := f.requests("/sdk/v1/ruleset")
	if reqs[0].conditional || reqs[1].header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("If-None-Match: first %q, second %q", reqs[0].header.Get("If-None-Match"), reqs[1].header.Get("If-None-Match"))
	}
	eventually(t, "lastSyncAt to move on a 304", 3*time.Second, func() bool { return c.Status().LastSyncAt.After(first) })
}

func TestStreamEvents(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[1,2]`, 0, 1)))
	f.on(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: connected\r\ndata: \"v1\"\r\n\r\n"))
		w.(http.Flusher).Flush()
		// wait for the test to change the ruleset
		for f.etagNow() == `"v1"` {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		_, _ = w.Write([]byte(": keep-alive\n\nevent: changed\ndata: " + f.etagNow() + "\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-f.done:
		}
	}, nil)
	c := makeClient(t, f, WithStream(true))
	c.Ready()
	eventually(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	f.set(rulesetJSON(flagJSON("a", `[1,2]`, 1, 2)), `"v2"`)
	eventually(t, "the changed event to fetch v2", 3*time.Second, func() bool { return c.Status().ETag == `"v2"` })
	if v := c.IntVariation("a", ctxFor("u"), 0); v != 2 {
		t.Fatalf("IntVariation = %d after the swap", v)
	}
	c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %s after Close", c.Status().Stream)
	}
}

func (f *fake) etagNow() string { _, e := f.current(); return e }

func TestStreamNon200(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }, nil)
	c := makeClient(t, f, WithStream(true), WithPollInterval(20*time.Millisecond))
	c.Ready()
	eventually(t, "http_404 on the stream", 3*time.Second, func() bool {
		st := c.Status()
		return st.LastError == "http_404" && st.Stream == StreamDisconnected
	})
}

func TestStreamKeepAliveDoesNotMoveLastSync(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		w.(http.Flusher).Flush()
		for i := 0; i < 20; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			_, _ = w.Write([]byte(": ping\n\n"))
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}, nil)
	c := makeClient(t, f, WithStream(true))
	c.Ready()
	eventually(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	at := c.Status().LastSyncAt
	time.Sleep(150 * time.Millisecond)
	if !c.Status().LastSyncAt.Equal(at) {
		t.Fatal("a keep-alive comment moved lastSyncAt")
	}
}

func TestPrivacyNothingOfAContextIsSent(t *testing.T) {
	const sentinel = "SENTINEL-user-9f3a7c"
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	log := &errorLog{}
	c := makeClient(t, f, WithOnError(log.add), WithFlushInterval(5*time.Millisecond))
	c.Ready()
	ctx := NewContext("user", sentinel, map[string]any{"email": sentinel + "@example.com", "name": sentinel})
	for i := 0; i < 5; i++ {
		c.BoolVariation("a", ctx, false)
		c.BoolVariation("missing-"+sentinel, ctx, false)
	}
	eventually(t, "a usage flush", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) > 0 })
	c.Close()
	for _, r := range f.all() {
		blob, _ := json.Marshal(r.header)
		all := r.path + r.query + r.host + string(blob) + string(r.body)
		if strings.Contains(all, sentinel) {
			t.Fatalf("a %s request carries context data: %s", r.route(), all)
		}
	}
	for _, e := range log.all() {
		if strings.Contains(e.Error(), sentinel) {
			t.Fatalf("an error text carries context data: %q", e.Error())
		}
	}
	if strings.Contains(c.Status().LastError, sentinel) {
		t.Fatal("status carries context data")
	}
}

// TestHeaderAudit records the header names each route sends. Only the allowed set may appear; the go
// net/http additions (User-Agent, Content-Length) and Host are accepted, Accept-Encoding is switched off.
func TestHeaderAudit(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	c := New(f.url(), "sdk-test-key", WithStream(true), WithPollInterval(20*time.Millisecond), WithFlushInterval(5*time.Millisecond))
	defer c.Close()
	c.Ready()
	eventually(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	c.BoolVariation("a", ctxFor("u"), false)
	eventually(t, "a usage flush", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) > 0 })
	// a second client that polls shows the conditional request
	p := New(f.url(), "sdk-test-key", WithStream(false), WithPollInterval(10*time.Millisecond))
	defer p.Close()
	eventually(t, "a conditional request", 3*time.Second, func() bool {
		for _, r := range f.requests("/sdk/v1/ruleset") {
			if r.conditional {
				return true
			}
		}
		return false
	})
	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Content-Length": true,
	}
	names := map[string]map[string]bool{}
	for _, r := range f.all() {
		set := names[r.route()]
		if set == nil {
			set = map[string]bool{}
			names[r.route()] = set
		}
		for name := range r.header {
			set[name] = true
			if !allowed[name] {
				t.Errorf("%s sends the header %s", r.route(), name)
			}
		}
		if !strings.HasPrefix(r.header.Get("User-Agent"), "Go-http-client") {
			t.Errorf("%s User-Agent = %q", r.route(), r.header.Get("User-Agent"))
		}
		if r.header.Get("Authorization") != "Bearer sdk-test-key" {
			t.Errorf("%s Authorization = %q", r.route(), r.header.Get("Authorization"))
		}
	}
	for _, route := range []string{"ruleset", "ruleset (conditional)", "stream", "events"} {
		var list []string
		for name := range names[route] {
			list = append(list, name)
		}
		if len(list) == 0 {
			t.Errorf("no %s request was seen", route)
		}
		sortStrings(list)
		t.Logf("header audit: %-22s Host + %s", route, strings.Join(list, ", "))
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
