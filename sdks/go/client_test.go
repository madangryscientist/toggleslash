package toggleslash

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

var user = UserContext("u1")

func TestLoadEvaluateAndStatus(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(4, 1), `"v1"`))
	c, log := newTestClient(t, f, WithPollInterval(20*time.Millisecond))
	if !c.Ready() {
		t.Fatal("Ready() = false")
	}
	d := c.BoolVariationDetail("flag", user, false)
	if !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("detail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.Stream != StreamOff || st.LastError != "" {
		t.Fatalf("status = %+v", st)
	}
	// The next poll sends If-None-Match, gets a 304, and moves lastSyncAt.
	waitFor(t, "a conditional poll", func() bool { return len(f.seen(pathRuleset)) >= 3 })
	reqs := f.seen(pathRuleset)
	if got := reqs[0].header.Get("If-None-Match"); got != "" {
		t.Errorf("first request sent If-None-Match %q", got)
	}
	if got := reqs[1].header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("second request sent If-None-Match %q", got)
	}
	if st2 := c.Status(); !st2.LastSyncAt.After(st.LastSyncAt) {
		t.Errorf("lastSyncAt did not move on a 304")
	}
	if n := log.count(); n != 0 {
		t.Errorf("onError called %d times: %v", n, log.all())
	}
}

func TestNotReadyAnswersAtOnce(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, f.hang)
	c, _ := newTestClient(t, f, WithRequestTimeout(time.Hour))
	start := time.Now()
	d := c.BoolVariationDetail("flag", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		t.Fatalf("detail = %+v", d)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestReadyIsBoundedByTheRequestTimeoutAndFixed(t *testing.T) {
	f := newFakeServer(t)
	var calls atomic.Int32
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			f.hang(w, r)
			return
		}
		serveRuleset(rulesetJSON(1, 1), `"v1"`)(w, r)
	})
	c, log := newTestClient(t, f, WithRequestTimeout(150*time.Millisecond), WithPollInterval(20*time.Millisecond))
	if c.Ready() {
		t.Fatal("Ready() = true after a timeout")
	}
	if c.Status().LastError != "" && c.Status().LastError != LastErrorTimeout {
		t.Fatalf("lastError %q", c.Status().LastError)
	}
	if !log.has("timeout") {
		t.Fatalf("no timeout error: %v", log.all())
	}
	// A later load does not turn the answer into true.
	waitFor(t, "a later load", func() bool { return c.BoolVariation("flag", user, false) })
	if c.Ready() {
		t.Fatal("Ready() changed after the first attempt settled")
	}
}

func TestBodyReadIsUnderTheTimeout(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"flags":[`)
		w.(http.Flusher).Flush()
		f.hang(w, r)
	})
	c, log := newTestClient(t, f, WithRequestTimeout(150*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("Ready() = true")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if !log.has("timeout") {
		t.Fatalf("errors: %v", log.all())
	}
}

func TestErrorCodes(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, "http_500"},
		{"403", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }, "http_403"},
		{"304 with nothing held", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(304) }, "http_304"},
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) }, "http_302"},
		{"not json", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>bad gateway") }, "bad_ruleset"},
		{"empty object", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }, "bad_ruleset"},
		{"flags not an array", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"flags":{},"segments":[]}`) }, "bad_ruleset"},
		{"array body", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `[]`) }, "bad_ruleset"},
		{"truncated", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"flags":[],"segm`) }, "bad_ruleset"},
		{"too deep", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("[", 5000)+strings.Repeat("]", 5000))
		}, "bad_ruleset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer(t)
			f.handle(pathRuleset, tc.handler)
			c, log := newTestClient(t, f)
			if c.Ready() {
				t.Fatal("Ready() = true")
			}
			if got := c.Status().LastError; got != tc.want {
				t.Fatalf("lastError = %q, want %q", got, tc.want)
			}
			if !log.has(tc.want) {
				t.Fatalf("onError got %v", log.all())
			}
			for _, e := range log.all() {
				if strings.Contains(e, f.url()) || strings.Contains(e, "127.0.0.1") {
					t.Fatalf("error text carries the URL: %q", e)
				}
			}
		})
	}
}

func TestAnUnusableURLIsANetworkError(t *testing.T) {
	log := &errLog{}
	c := NewClient("http://127.0.0.1:1", "k", WithStream(false), WithOnError(log.add))
	defer c.Close()
	if c.Ready() {
		t.Fatal("Ready() = true")
	}
	if got := c.Status().LastError; got != LastErrorNetwork {
		t.Fatalf("lastError %q", got)
	}
	bad := NewClient("ht\ttp://bad url", "k", WithStream(false), WithOnError(log.add))
	defer bad.Close()
	if bad.Ready() || bad.Status().LastError != LastErrorNetwork {
		t.Fatalf("status = %+v", bad.Status())
	}
	for _, e := range log.all() {
		if strings.Contains(e, "127.0.0.1") || strings.Contains(e, "bad url") {
			t.Fatalf("error text carries the URL: %q", e)
		}
	}
}

func TestOnlyARulesetReplacesARuleset(t *testing.T) {
	f := newFakeServer(t)
	var n atomic.Int32
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			serveRuleset(rulesetJSON(1, 1), `"v1"`)(w, r)
			return
		}
		_, _ = io.WriteString(w, "{}")
	})
	c, _ := newTestClient(t, f, WithPollInterval(10*time.Millisecond))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	waitFor(t, "a bad answer", func() bool { return c.Status().LastError == "bad_ruleset" })
	if !c.BoolVariation("flag", user, false) || c.Status().ETag != `"v1"` {
		t.Fatal("the ruleset held was replaced")
	}
}

func TestBodyDecoding(t *testing.T) {
	f := newFakeServer(t)
	body := "\xef\xbb\xbf" + rulesetWith("\"café \\ud800 \xff!\"")
	// A raw invalid byte inside a string, a BOM in front and a lone surrogate escape.
	f.handle(pathRuleset, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
	c, _ := newTestClient(t, f)
	if !c.Ready() {
		t.Fatalf("not ready: %+v", c.Status())
	}
	got := c.StringVariation("flag", user, "fallback")
	if !strings.Contains(got, "café ") || !strings.HasSuffix(got, "�!") {
		t.Fatalf("got %q", got)
	}
}

func TestTypedAccessorsAndTheMismatchRule(t *testing.T) {
	type check func(t *testing.T, c *Client)
	cases := []struct {
		name    string
		literal string
		check   check
	}{
		{"bool", `true`, func(t *testing.T, c *Client) {
			if !c.BoolVariation("flag", user, false) {
				t.Error("bool")
			}
			d := c.StringVariationDetail("flag", user, "x")
			if d.Value != "x" || d.VariationIndex != nil || d.Reason.Code != ErrTypeMismatch {
				t.Errorf("string on a bool: %+v", d)
			}
			a := c.AnyVariationDetail("flag", user, NullValue())
			if b, _ := a.Value.Bool(); !b || a.VariationIndex == nil {
				t.Errorf("any with a null fallback: %+v", a)
			}
			if d := c.AnyVariationDetail("flag", user, StringValue("x")); d.Reason.Code != ErrTypeMismatch {
				t.Errorf("any with a string fallback: %+v", d)
			}
		}},
		{"null served", `null`, func(t *testing.T, c *Client) {
			if d := c.StringVariationDetail("flag", user, "x"); d.Reason.Code != ErrTypeMismatch {
				t.Errorf("null against a string: %+v", d)
			}
			if d := c.AnyVariationDetail("flag", user, NullValue()); d.VariationIndex == nil || !d.Value.IsNull() {
				t.Errorf("null against a null fallback: %+v", d)
			}
		}},
		{"array and object differ", `[1,2]`, func(t *testing.T, c *Client) {
			if d := c.JSONVariationDetail("flag", user, ObjectValue(nil, nil)); d.Reason.Code != ErrTypeMismatch {
				t.Errorf("array against an object: %+v", d)
			}
			d := c.JSONVariationDetail("flag", user, ArrayValue())
			if d.VariationIndex == nil || d.Value.Len() != 2 || d.Value.JSON() != "[1,2]" {
				t.Errorf("array against an array: %+v", d)
			}
		}},
		{"fraction", `2.5`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Value != 7 || d.Reason.Code != ErrTypeMismatch {
				t.Errorf("int on a fraction: %+v", d)
			}
			if got := c.FloatVariation("flag", user, 0); got != 2.5 {
				t.Errorf("float: %v", got)
			}
		}},
		{"two point zero", `2.0`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Value != 2 || d.VariationIndex == nil {
				t.Errorf("int on 2.0: %+v", d)
			}
		}},
		{"top of int64", `9223372036854774784`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Value != 9223372036854774784 {
				t.Errorf("int on 2^63-1024: %+v", d)
			}
		}},
		{"just past int64", `9223372036854775807`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Value != 7 || d.Reason.Code != ErrTypeMismatch {
				t.Errorf("int on 2^63: %+v", d)
			}
		}},
		{"huge", `1e20`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Value != 7 || d.Reason.Code != ErrTypeMismatch {
				t.Errorf("int on 1e20: %+v", d)
			}
			if got := c.FloatVariation("flag", user, 0); got != 1e20 {
				t.Errorf("float: %v", got)
			}
		}},
		{"infinity", `1e999`, func(t *testing.T, c *Client) {
			if d := c.IntVariationDetail("flag", user, 7); d.Reason.Code != ErrTypeMismatch {
				t.Errorf("int on infinity: %+v", d)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer(t)
			f.handle(pathRuleset, serveRuleset(rulesetWith(tc.literal), `"e"`))
			c, _ := newTestClient(t, f)
			if !c.Ready() {
				t.Fatalf("not ready: %+v", c.Status())
			}
			tc.check(t, c)
		})
	}
}

func TestInvalidContextsAreFallbacks(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	c, _ := newTestClient(t, f)
	c.Ready()
	for name, ctx := range map[string]Context{
		"zero":        {},
		"empty key":   UserContext(""),
		"bad kind":    NewContext("has space", "k"),
		"not json":    ContextFromJSON([]byte("{")),
		"array":       ContextFromJSON([]byte("[]")),
		"bad native":  ContextFromMap(map[string]any{"key": make(chan int)}),
		"empty multi": MultiContext(),
	} {
		d := c.BoolVariationDetail("flag", ctx, false)
		if d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrInvalidContext {
			t.Errorf("%s: %+v", name, d)
		}
	}
	multi := MultiContext(UserContext("u1"), NewContext("org", "o1").With("plan", StringValue("pro")))
	if d := c.BoolVariationDetail("flag", multi, false); !d.Value {
		t.Errorf("multi: %+v", d)
	}
	if d := c.BoolVariationDetail("flag", ContextFromMap(map[string]any{"kind": "user", "key": "k", "attributes": map[string]any{"n": 1, "ok": true}}), false); !d.Value {
		t.Errorf("from map: %+v", d)
	}
	if d := c.BoolVariationDetail("nope", user, true); d.Reason.Code != ErrFlagNotFound || !d.Value {
		t.Errorf("unknown flag: %+v", d)
	}
}

func TestEvaluationFailedIsOneOnError(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	boom := func(*eval.Ruleset, string, jsonvalue.Value) eval.Result { panic("boom with secret-key-u1") }
	c, log := newTestClient(t, f, withEvaluator(boom))
	c.Ready()
	before := log.count()
	d := c.BoolVariationDetail("flag", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("detail = %+v", d)
	}
	if got := log.count() - before; got != 1 {
		t.Fatalf("onError called %d times", got)
	}
	for _, e := range log.all() {
		if strings.Contains(e, "secret-key-u1") || strings.Contains(e, "u1") {
			t.Fatalf("error carries the panic value or context: %q", e)
		}
	}
	// It was not counted.
	c.flush(false)
	if got := f.seen(pathEvents); len(got) != 0 {
		t.Fatalf("sent %d events requests", len(got))
	}
}

func TestHookSeesTheAnswerAndCannotChangeIt(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(9, 1), `"v1"`))
	var mu sync.Mutex
	var events []EvaluationEvent
	c, log := newTestClient(t, f, WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		panic("the hook raised")
	}))
	c.Ready()
	d := c.BoolVariationDetail("flag", user, false)
	if !d.Value || d.VariationIndex == nil {
		t.Fatalf("a panicking hook changed the answer: %+v", d)
	}
	mu.Lock()
	last := events[len(events)-1]
	mu.Unlock()
	if last.FlagKey != "flag" || last.ETag != `"v1"` || last.FlagVersion == nil || *last.FlagVersion != 9 || last.VariationIndex == nil {
		t.Fatalf("event = %+v", last)
	}
	if b, _ := last.Value.Bool(); !b {
		t.Fatalf("event value = %+v", last.Value)
	}
	if !log.has("hook") {
		t.Fatalf("errors: %v", log.all())
	}
	c.flush(false)
	reqs := f.seen(pathEvents)
	if len(reqs) != 1 || !strings.Contains(reqs[0].body, `"count":1`) {
		t.Fatalf("the evaluation was not counted once: %+v", reqs)
	}
	// The hook ran on the caller's goroutine: its panic was recovered there, not in the SDK's.
}

func TestOnErrorThatPanicsIsIgnored(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	c := NewClient(f.url(), "k", WithStream(false), WithOnError(func(error) { panic("no") }))
	defer c.Close()
	if c.Ready() {
		t.Fatal("ready")
	}
	if c.Status().LastError != "http_500" {
		t.Fatalf("status %+v", c.Status())
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	target := newFakeServer(t)
	target.handle("/target", serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f := newFakeServer(t)
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.url()+"/target", http.StatusTemporaryRedirect)
	})
	c, _ := newTestClient(t, f)
	if c.Ready() || c.Status().LastError != "http_307" {
		t.Fatalf("status %+v", c.Status())
	}
	if n := len(target.seen("/target")); n != 0 {
		t.Fatalf("the redirect target got %d requests", n)
	}
}

func TestTrailingSlashesAreRemoved(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	c := NewClient(f.url()+"///", "k", WithStream(false))
	defer c.Close()
	if !c.Ready() {
		t.Fatalf("status %+v", c.Status())
	}
}

func TestTLS(t *testing.T) {
	f := newFakeServer(t)
	_ = f
	tlsSrv := newTLSServer(t, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	c := NewClient(tlsSrv.URL, "k", WithStream(false), WithHTTPClient(tlsSrv.Client()))
	defer c.Close()
	if !c.Ready() {
		t.Fatalf("status %+v", c.Status())
	}
	// Without the test certificate in its pool the default client refuses the connection: a network error.
	bad := NewClient(tlsSrv.URL, "k", WithStream(false))
	defer bad.Close()
	if bad.Ready() || bad.Status().LastError != LastErrorNetwork {
		t.Fatalf("status %+v", bad.Status())
	}
}
