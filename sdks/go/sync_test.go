package toggleslash

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// allowedHeaders are the request headers the SDK sets, and the ones net/http adds on its own (listed in RUN.md).
var allowedHeaders = map[string]bool{
	"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true, // ours
	"User-Agent": true, "Accept-Encoding": true, "Content-Length": true, // net/http's
}

func auditHeaders(t *testing.T, route string, r recorded, want map[string]string) {
	t.Helper()
	for name, values := range r.header {
		if !allowedHeaders[name] {
			t.Errorf("%s: unexpected header %s: %v", route, name, values)
		}
	}
	for name, value := range want {
		if got := r.header.Get(name); got != value {
			t.Errorf("%s: header %s = %q, want %q", route, name, got, value)
		}
	}
	if !strings.HasPrefix(r.header.Get("User-Agent"), "Go-http-client/") {
		t.Errorf("%s: user agent %q", route, r.header.Get("User-Agent"))
	}
}

func TestHeadersOnEveryRoute(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	f.set(&f.stream, feed.handler)
	c := f.client(t, WithFlushInterval(time.Hour))
	feed.waitOpened(t)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	c.BoolVariation("f", user, false)
	c.Close()

	rs := f.requests("/sdk/v1/ruleset")
	auditHeaders(t, "ruleset", rs[0], map[string]string{"Authorization": "Bearer sdk-key"})
	if rs[0].header.Get("If-None-Match") != "" || rs[0].method != "GET" || rs[0].body != "" {
		t.Errorf("first ruleset request: %+v", rs[0])
	}
	st := f.requests("/sdk/v1/stream")
	auditHeaders(t, "stream", st[0], map[string]string{"Authorization": "Bearer sdk-key", "Accept": "text/event-stream"})
	ev := f.requests("/sdk/v1/events")
	if len(ev) != 1 {
		t.Fatalf("%d events requests", len(ev))
	}
	auditHeaders(t, "events", ev[0], map[string]string{"Authorization": "Bearer sdk-key", "Content-Type": "application/json"})
	if ev[0].method != "POST" {
		t.Errorf("method %s", ev[0].method)
	}
	for _, r := range f.requests("") {
		if r.host != strings.TrimPrefix(f.srv.URL, "http://") {
			t.Errorf("host %q", r.host)
		}
		if strings.Contains(r.body, "user-1") || strings.Contains(r.path, "user-1") {
			t.Errorf("the context reached the wire: %+v", r)
		}
	}
}

func TestTrailingSlashesAreRemoved(t *testing.T) {
	f := newFake(t)
	c := New(f.srv.URL+"///", "sdk-key", WithStream(false))
	t.Cleanup(c.Close)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if f.requests("/sdk/v1/ruleset")[0].path != "/sdk/v1/ruleset" {
		t.Fatalf("%+v", f.requests(""))
	}
}

func TestConditionalRequestAndNotModified(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(boolRuleset()))
	})
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), withClock(fc), WithRequestTimeout(testTimeout))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	first := c.Status().LastSyncAt
	if !first.Equal(epoch) {
		t.Fatalf("lastSyncAt %v", first)
	}
	// The poll timer is armed after Ready returns, so wait for it: advancing first could fire it early, or not at all.
	waitFor(t, "the poll timer", func() bool { return countArmed(fc, 30*time.Second) == 1 })
	fc.advance(time.Minute)
	waitFor(t, "the second ruleset request", func() bool { return f.count("/sdk/v1/ruleset") == 2 })
	waitFor(t, "the sync", func() bool { return c.Status().LastSyncAt.After(first) })
	second := f.requests("/sdk/v1/ruleset")[1]
	if second.header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("If-None-Match %q", second.header.Get("If-None-Match"))
	}
	if st := c.Status(); st.ETag != `"v1"` || st.LastError != "" {
		t.Fatalf("%+v", st)
	}
}

func TestNoETagMeansNoConditionalRequest(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(boolRuleset())) })
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), withClock(fc), WithRequestTimeout(testTimeout))
	c.Ready()
	fc.fireNext(t, "the poll", exactly(30*time.Second))
	waitFor(t, "the second request", func() bool { return f.count("/sdk/v1/ruleset") == 2 })
	if got := f.requests("/sdk/v1/ruleset")[1].header.Get("If-None-Match"); got != "" {
		t.Fatalf("If-None-Match %q", got)
	}
	if !c.BoolVariation("f", user, false) {
		t.Fatal("no ruleset")
	}
}

func TestETagIsKeptExactly(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"weak-tag"`)
		_, _ = w.Write([]byte(boolRuleset()))
	})
	c := f.client(t, WithStream(false))
	c.Ready()
	if c.Status().ETag != `W/"weak-tag"` {
		t.Fatalf("%q", c.Status().ETag)
	}
}

func TestNotModifiedWithNoRulesetIsAnError(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) })
	var log errorLog
	c := f.client(t, WithStream(false), WithOnError(log.add))
	if c.Ready() {
		t.Fatal("ready")
	}
	if st := c.Status(); st.LastError != "http_304" || !st.LastSyncAt.IsZero() {
		t.Fatalf("%+v", st)
	}
	waitFor(t, "onError", func() bool { return log.count() == 1 })
}

func TestBadRulesetKeepsTheLastGoodOne(t *testing.T) {
	bodies := []string{`{"flags":1,"segments":[]}`, `{"flags":[]}`, `[]`, `null`, `{"flags":[],"segments":{}}`, `not json`, ``, `{"flags":[],"segments":[`}
	for _, bad := range bodies {
		t.Run(bad, func(t *testing.T) {
			f := newFake(t)
			var calls atomic.Int32
			f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"v`+strconv.Itoa(int(calls.Add(1)))+`"`)
				if calls.Load() == 1 {
					_, _ = w.Write([]byte(boolRuleset()))
					return
				}
				_, _ = w.Write([]byte(bad))
			})
			fc := newFakeClock(epoch)
			c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), withClock(fc), WithRequestTimeout(testTimeout))
			c.Ready()
			fc.fireNext(t, "the poll", exactly(30*time.Second))
			waitFor(t, "bad_ruleset", func() bool { return c.Status().LastError == LastErrorBadRuleset })
			if !c.BoolVariation("f", user, false) {
				t.Fatal("the last good ruleset is gone")
			}
			if c.Status().ETag != `"v1"` {
				t.Fatalf("etag %q", c.Status().ETag)
			}
		})
	}
}

func TestRulesetBodyDecoding(t *testing.T) {
	flag := flagJSON("f", `[false,true]`, 1, 1)
	cases := map[string]string{
		"bom":                  "\xef\xbb\xbf" + rulesetJSON(flag),
		"invalid utf-8":        strings.Replace(rulesetJSON(flag), `"salt":"s"`, "\"salt\":\"s\xff\",\"note\":\"\xc0\xaf\"", 1),
		"duplicate keys":       strings.Replace(rulesetJSON(flag), `"flags":[`, `"flags":1,"flags":[`, 1),
		"escaped lone surrog.": strings.Replace(rulesetJSON(flag), `"salt":"s"`, `"salt":"\ud800"`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			c := f.client(t, WithStream(false))
			if !c.Ready() || !c.BoolVariation("f", user, false) {
				t.Fatalf("not loaded: %+v", c.Status())
			}
		})
	}
}

func TestDeeplyNestedRuleset(t *testing.T) {
	deep := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	body := strings.Replace(rulesetJSON(flagJSON("f", `[false,true]`, 1, 1)), `"salt":"s"`, `"salt":"s","extra":`+deep, 1)
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	c := f.client(t, WithStream(false))
	if !c.Ready() {
		t.Fatalf("nesting 600 refused: %+v", c.Status())
	}
}

func TestLargeRulesetHasNoSizeCap(t *testing.T) {
	var flags []string
	for i := range 20000 {
		flags = append(flags, flagJSON("flag-"+strings.Repeat("x", 40)+string(rune('a'+i%26))+strconv.Itoa(i), `[false,true]`, 1, 1))
	}
	f := newFake(t)
	body := rulesetJSON(flags...)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	c := f.client(t, WithStream(false))
	if !c.Ready() {
		t.Fatalf("%d bytes refused: %+v", len(body), c.Status())
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	c := f.client(t, WithStream(false))
	if c.Ready() {
		t.Fatal("ready")
	}
	if c.Status().LastError != "http_302" {
		t.Fatalf("%+v", c.Status())
	}
	if f.count("/elsewhere") != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestStatusCodesBecomeLastError(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 429, 500, 503} {
		f := newFake(t)
		f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		c := f.client(t, WithStream(false))
		if c.Ready() {
			t.Fatal("ready")
		}
		if got, want := c.Status().LastError, "http_"+strconv.Itoa(code); got != want {
			t.Errorf("%d: %q", code, got)
		}
	}
}

func TestBodyReadIsBoundedByTheTimeout(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte(`{"flags":[`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	var log errorLog
	c := f.client(t, WithStream(false), WithRequestTimeout(300*time.Millisecond), WithOnError(log.add))
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %v", took)
	}
	if c.Status().LastError != LastErrorTimeout {
		t.Fatalf("%+v", c.Status())
	}
}

func TestNetworkErrorTextHoldsNoURLOrKey(t *testing.T) {
	f := newFake(t)
	url := f.srv.URL
	f.srv.Close()
	var log errorLog
	c := New(url, "secret-sdk-key-value", WithStream(false), WithOnError(log.add))
	t.Cleanup(c.Close)
	if c.Ready() {
		t.Fatal("ready")
	}
	if c.Status().LastError != LastErrorNetwork {
		t.Fatalf("%+v", c.Status())
	}
	waitFor(t, "onError", func() bool { return log.count() >= 1 })
	text := log.text()
	host := strings.TrimPrefix(url, "http://")
	if strings.Contains(text, "secret-sdk-key-value") || strings.Contains(text, host) || strings.Contains(text, "127.0.0.1") {
		t.Fatalf("error text leaks: %q", text)
	}
}

func TestStatusHoldsNothingSecret(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false))
	c.Ready()
	st := c.Status()
	text := st.ETag + string(st.Stream) + st.LastError
	if strings.Contains(text, "sdk-key") || strings.Contains(text, f.srv.URL) {
		t.Fatalf("%q", text)
	}
}

func TestStreamOffStatus(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false))
	c.Ready()
	if c.Status().Stream != StreamOff || f.count("/sdk/v1/stream") != 0 {
		t.Fatal("stream off still streams")
	}
}

func TestTLS(t *testing.T) {
	f := newFakeWith(t, true)
	c := New(f.srv.URL, "sdk-key", WithStream(false), withHTTPClient(f.srv.Client()))
	t.Cleanup(c.Close)
	if !c.Ready() || !c.BoolVariation("f", user, false) {
		t.Fatalf("%+v", c.Status())
	}
}

func TestTLSUntrustedCertificateIsANetworkError(t *testing.T) {
	f := newFakeWith(t, true)
	c := New(f.srv.URL, "sdk-key", WithStream(false)) // the default client does not trust the test certificate
	t.Cleanup(c.Close)
	if c.Ready() || c.Status().LastError != LastErrorNetwork {
		t.Fatalf("%+v", c.Status())
	}
	if f.count("") != 0 {
		t.Fatal("a request went through an untrusted connection")
	}
}
