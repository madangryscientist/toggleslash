package toggleslash

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"0", 0, true},
		{"7", 7 * time.Second, true},
		{"  12  ", 12 * time.Second, true},
		{"0030", 30 * time.Second, true},
		{"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true},
		{"99999", 300 * time.Second, true},
		{"99999999999999999999", 300 * time.Second, true},
		{"1.5", 0, false},
		{"-3", 0, false},
		{"+3", 0, false},
		{"3s", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.in != "" {
			h.Set("Retry-After", tc.in)
		}
		got, ok := parseRetryAfter(h)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got %v %v, want %v %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func tooMany(retryAfter string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = io.Copy(io.Discard, r.Body)
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
}

func holdLeft(c *Client, which string) time.Duration {
	c.holdMu.Lock()
	defer c.holdMu.Unlock()
	at := c.holdSync
	if which == "usage" {
		at = c.holdUse
	}
	return time.Until(at)
}

func TestRetryAfterIsCappedAt300SecondsOnEveryRoute(t *testing.T) {
	t.Run("ruleset", func(t *testing.T) {
		f := newFake(t, boolFlag("on", true, 1))
		f.onRuleset = tooMany("99999")
		c := newClient(t, f.url(), WithStream(false))
		c.Ready()
		if got := c.Status().LastError; got != "http_429" {
			t.Fatalf("lastError = %q", got)
		}
		if left := holdLeft(c, "routes"); left < 299*time.Second || left > 300*time.Second {
			t.Errorf("hold = %v", left)
		}
	})
	t.Run("stream", func(t *testing.T) {
		f := newFake(t, boolFlag("on", true, 1))
		f.onStream = tooMany("1000")
		c := newClient(t, f.url(), WithStream(true))
		c.Ready()
		waitFor(t, "stream 429", 3*time.Second, func() bool { return c.Status().LastError == "http_429" })
		if left := holdLeft(c, "routes"); left < 299*time.Second || left > 300*time.Second {
			t.Errorf("hold = %v", left)
		}
		// The hold covers the ruleset route too: no poll comes inside it.
		if n := len(f.routeRequests("/sdk/v1/ruleset")); n != 1 {
			t.Errorf("ruleset requests during the hold = %d", n)
		}
	})
	t.Run("events", func(t *testing.T) {
		f := newFake(t, boolFlag("on", true, 1))
		f.onEvents = tooMany("301")
		c := newClient(t, f.url(), WithStream(false), WithFlushInterval(20*time.Millisecond))
		c.Ready()
		c.Variation("on", user, Bool(false))
		waitFor(t, "events 429", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/events")) == 1 })
		waitFor(t, "hold", 3*time.Second, func() bool { return holdLeft(c, "usage") > 0 })
		if left := holdLeft(c, "usage"); left < 299*time.Second || left > 300*time.Second {
			t.Errorf("hold = %v", left)
		}
		// An events 429 does not hold the ruleset route.
		if left := holdLeft(c, "routes"); left > 0 {
			t.Errorf("routes held for %v by an events 429", left)
		}
		// Flushes during the hold send nothing and keep the counts.
		time.Sleep(150 * time.Millisecond)
		if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
			t.Errorf("events requests during the hold = %d", n)
		}
	})
}

func TestARetryAfterOnlyLengthensAHold(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f.url(), WithStream(false))
	c.Ready()
	c.holdRoutes(100 * time.Second)
	c.holdRoutes(10 * time.Second)
	if left := holdLeft(c, "routes"); left < 99*time.Second {
		t.Errorf("a shorter hold shortened it: %v", left)
	}
	c.holdRoutes(200 * time.Second)
	if left := holdLeft(c, "routes"); left < 199*time.Second {
		t.Errorf("a longer hold did not lengthen it: %v", left)
	}
}

func TestA429WithoutRetryAfterKeepsCountsAndHoldsNothing(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onEvents = tooMany("")
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(20*time.Millisecond))
	c.Ready()
	c.Variation("on", user, Bool(false))
	waitFor(t, "two attempts", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/events")) >= 2 })
	for _, r := range f.routeRequests("/sdk/v1/events") {
		if got := eventsOf(t, r.body); len(got) != 1 {
			t.Errorf("body = %v", got)
		}
	}
}

func TestConditionalRulesetRequest(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithPollInterval(20*time.Millisecond))
	c.Ready()
	waitFor(t, "a poll", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/ruleset")) >= 3 })
	reqs := f.routeRequests("/sdk/v1/ruleset")
	if got := reqs[0].header.Get("If-None-Match"); got != "" {
		t.Errorf("first request sent If-None-Match %q", got)
	}
	if got := reqs[1].header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("second request If-None-Match = %q", got)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt == nil || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestNoETagMeansNoConditionalRequest(t *testing.T) {
	f := newFake(t)
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = io.WriteString(w, `{"flags":[],"segments":[]}`)
		return true
	}
	c := newClient(t, f.url(), WithStream(false), WithPollInterval(20*time.Millisecond))
	c.Ready()
	waitFor(t, "polls", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/ruleset")) >= 3 })
	for i, r := range f.routeRequests("/sdk/v1/ruleset") {
		if got := r.header.Get("If-None-Match"); got != "" {
			t.Errorf("request %d sent If-None-Match %q with no tag held", i, got)
		}
	}
	if st := c.Status(); st.ETag != "" {
		t.Errorf("etag = %q", st.ETag)
	}
}

func TestA304WithNoRulesetIsHTTP304(t *testing.T) {
	f := newFake(t)
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	c := newClient(t, f.url(), WithStream(false))
	if c.Ready() {
		t.Error("ready")
	}
	if got := c.Status().LastError; got != "http_304" {
		t.Errorf("lastError = %q", got)
	}
}

// The headers each request carries. Everything else is a header the SDK should not send.
var allowedHeaders = map[string]bool{
	"Authorization":   true,
	"Accept":          true,
	"Accept-Encoding": false, // the SDK turns compression off, so the transport does not add it
	"User-Agent":      true,
	"Content-Type":    true,
	"Content-Length":  true,
	"If-None-Match":   true,
}

func TestHeaderAudit(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := New(f.url(), "fs_server_audit", WithPollInterval(20*time.Millisecond), WithFlushInterval(20*time.Millisecond))
	defer c.Close()
	c.Ready()
	c.Variation("on", user, Bool(false))
	waitFor(t, "stream and events", 3*time.Second, func() bool {
		return len(f.routeRequests("/sdk/v1/stream")) >= 1 && len(f.routeRequests("/sdk/v1/events")) >= 1
	})
	// A conditional ruleset request comes after the stream's first load is held: force one.
	c2 := New(f.url(), "fs_server_audit", WithStream(false), WithPollInterval(20*time.Millisecond))
	defer c2.Close()
	c2.Ready()
	waitFor(t, "a conditional request", 3*time.Second, func() bool {
		for _, r := range f.routeRequests("/sdk/v1/ruleset") {
			if r.header.Get("If-None-Match") != "" {
				return true
			}
		}
		return false
	})
	rows := map[string]map[string]string{}
	f.mu.Lock()
	for _, r := range f.requests {
		name := r.method + " " + r.path
		if r.path == "/sdk/v1/ruleset" && r.header.Get("If-None-Match") != "" {
			name += " (conditional)"
		}
		if rows[name] == nil {
			rows[name] = map[string]string{}
		}
		for k := range r.header {
			rows[name][k] = r.header.Get(k)
		}
	}
	f.mu.Unlock()
	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		keys := make([]string, 0)
		for k := range rows[n] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Logf("HEADERS %s: %s", n, strings.Join(keys, ", "))
		for _, k := range keys {
			if k == "Authorization" {
				if rows[n][k] != "Bearer fs_server_audit" {
					t.Errorf("%s: Authorization = %q", n, rows[n][k])
				}
			}
			if allowed, known := allowedHeaders[k]; !known || !allowed {
				t.Errorf("%s: unexpected header %s", n, k)
			}
			if strings.Contains(rows[n][k], "u1") && k != "User-Agent" {
				t.Errorf("%s: header %s carries the context key", n, k)
			}
		}
		if ua := rows[n]["User-Agent"]; !strings.HasPrefix(ua, "Go-http-client/") {
			t.Errorf("%s: User-Agent = %q", n, ua)
		}
	}
	for _, want := range []string{"GET /sdk/v1/ruleset", "GET /sdk/v1/ruleset (conditional)", "GET /sdk/v1/stream", "POST /sdk/v1/events"} {
		if rows[want] == nil {
			t.Errorf("no request seen for %s", want)
		}
	}
	if rows["GET /sdk/v1/stream"]["Accept"] != "text/event-stream" {
		t.Errorf("stream Accept = %q", rows["GET /sdk/v1/stream"]["Accept"])
	}
	if rows["POST /sdk/v1/events"]["Content-Type"] != "application/json" {
		t.Errorf("events Content-Type = %q", rows["POST /sdk/v1/events"]["Content-Type"])
	}
}

func TestRequestsNeverCarryTheContext(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	ctx := UserContext("secret-user-key-123", map[string]any{"email": "someone@example.invalid"})
	var log errorLog
	c := New(f.url(), "k", WithFlushInterval(20*time.Millisecond), WithOnError(log.add))
	c.Ready()
	c.Variation("on", ctx, Bool(false))
	waitFor(t, "events", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/events")) >= 1 })
	c.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		all := r.path + r.body + fmt.Sprint(r.header)
		for _, secret := range []string{"secret-user-key-123", "someone@example.invalid"} {
			if strings.Contains(all, secret) {
				t.Errorf("%s %s carries %q", r.method, r.path, secret)
			}
		}
	}
	st := fmt.Sprintf("%+v", c.Status())
	if strings.Contains(st, "secret-user-key") || strings.Contains(st, f.url()) {
		t.Errorf("status = %s", st)
	}
}

// streamBody serves a stream that writes raw text and then holds the connection.
func streamBody(text string, then func(w http.ResponseWriter, r *http.Request)) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, text)
		w.(http.Flusher).Flush()
		if then != nil {
			then(w, r)
		}
		<-r.Context().Done()
		return true
	}
}

func TestStreamTagEqualToTheHeldOneIsASyncWithoutAFetch(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = streamBody("event: connected\ndata: \"v1\"\n\n", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "event: changed\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
	})
	c := newClient(t, f.url())
	c.Ready()
	waitFor(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	first := c.Status().LastSyncAt
	waitFor(t, "the second sync", 3*time.Second, func() bool {
		s := c.Status().LastSyncAt
		return s != nil && first != nil && s.After(*first)
	})
	if n := len(f.routeRequests("/sdk/v1/ruleset")); n != 1 {
		t.Errorf("ruleset requests = %d, a tag-equal event needs none", n)
	}
}

func TestStreamChangedEventFetchesTheRuleset(t *testing.T) {
	f := newFake(t, boolFlag("on", false, 1))
	var push atomic.Bool
	f.onStream = streamBody("event: connected\ndata: \"v1\"\n\n", func(w http.ResponseWriter, r *http.Request) {
		for !push.Load() {
			if r.Context().Err() != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		_, _ = io.WriteString(w, "event: changed\ndata: \"v2\"\n\n")
		w.(http.Flusher).Flush()
	})
	c := newClient(t, f.url())
	c.Ready()
	waitFor(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	if c.BoolVariation("on", user, true) {
		t.Fatal("the flag should serve false")
	}
	f.set(boolFlag("on", true, 2))
	push.Store(true)
	waitFor(t, "the new ruleset", 3*time.Second, func() bool { return c.BoolVariation("on", user, false) })
	if st := c.Status(); st.ETag != `"v2"` || st.Stream != StreamConnected {
		t.Errorf("status = %+v", st)
	}
}

func TestStreamIgnoresUnknownEventsAndEventsWithNoData(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = streamBody(": comment\nevent: other\ndata: x\n\nevent: changed\n\nevent: connected\ndata: \"v1\"\n\n", nil)
	c := newClient(t, f.url())
	c.Ready()
	waitFor(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	if n := len(f.routeRequests("/sdk/v1/ruleset")); n != 1 {
		t.Errorf("ruleset requests = %d, a data-less changed event must be dropped", n)
	}
}

func TestStreamWatchdogDropsASilentStream(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = streamBody("event: connected\ndata: \"v1\"\n\n", nil)
	c := newClient(t, f.url(), withWatchdog(150*time.Millisecond), withJitter(func() float64 { return 0 }))
	c.Ready()
	waitFor(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	waitFor(t, "the watchdog", 3*time.Second, func() bool {
		st := c.Status()
		return st.Stream != StreamConnected && st.LastError == SyncTimeout
	})
	// The loop goes on: it opens the stream again.
	waitFor(t, "a second stream request", 5*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/stream")) >= 2 })
}

func TestStreamKeepAliveCommentsRestartTheWatchdog(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = streamBody("event: connected\ndata: \"v1\"\n\n", func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 20 && r.Context().Err() == nil; i++ {
			time.Sleep(50 * time.Millisecond)
			_, _ = io.WriteString(w, ": keep-alive\n\n")
			w.(http.Flusher).Flush()
		}
	})
	c := newClient(t, f.url(), withWatchdog(200*time.Millisecond))
	c.Ready()
	waitFor(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	first := c.Status().LastSyncAt
	time.Sleep(600 * time.Millisecond) // three watchdog periods of comments
	st := c.Status()
	if st.Stream != StreamConnected || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
	if st.LastSyncAt == nil || first == nil || !st.LastSyncAt.Equal(*first) {
		t.Errorf("keep-alives moved lastSyncAt: %v then %v", first, st.LastSyncAt)
	}
}

func TestStreamFailedFetchEndsTheStream(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var n atomic.Int32
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		if n.Add(1) == 1 {
			return false
		}
		w.WriteHeader(http.StatusInternalServerError)
		return true
	}
	f.onStream = streamBody("event: connected\ndata: \"v1\"\n\nevent: changed\ndata: \"v2\"\n\n", nil)
	c := newClient(t, f.url(), withJitter(func() float64 { return 0 }))
	c.Ready()
	waitFor(t, "http_500", 3*time.Second, func() bool { return c.Status().LastError == "http_500" })
	if st := c.Status(); st.Stream == StreamConnected {
		t.Errorf("the stream stays connected after a failed fetch: %+v", st)
	}
}

func TestStreamNon200IsHTTPCode(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusForbidden)
		return true
	}
	c := newClient(t, f.url())
	c.Ready()
	waitFor(t, "http_403", 3*time.Second, func() bool { return c.Status().LastError == "http_403" })
	if got := c.Status().Stream; got == StreamConnected {
		t.Errorf("stream = %q", got)
	}
}

func TestStreamCleanEndLeavesTheLastError(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onStream = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		return true // returning ends the response cleanly
	}
	var log errorLog
	c := newClient(t, f.url(), WithOnError(log.add), withJitter(func() float64 { return 0 }))
	c.Ready()
	waitFor(t, "a second stream request", 5*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/stream")) >= 2 })
	if n := log.count(); n != 0 {
		t.Errorf("a clean end reached onError: %v", log.all())
	}
}

func TestSSEParser(t *testing.T) {
	in := "\xef\xbb\xbfevent: a\r\ndata: 1\r\ndata:2\r\n\r\n" + // BOM, CRLF, two data lines, no space
		"event:b\rdata: x\r\r" + // CR line ends
		": comment\nevent: skipped\n\n" + // no data: dropped
		"data\n\n" + // a data field with no colon: empty data
		"event: c\ndata: tail" // incomplete at the end: discarded
	p := newSSEParser(strings.NewReader(in))
	var got []string
	for {
		ev, err := p.next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
		got = append(got, ev.name+"="+strings.ReplaceAll(ev.data, "\n", "|"))
	}
	want := []string{"a=1|2", "b=x", "message="}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func TestSSEParserLoneCRThenLF(t *testing.T) {
	p := newSSEParser(strings.NewReader("data: a\r\n\r\ndata: b\n\n"))
	var got []string
	for {
		ev, err := p.next()
		if err != nil {
			break
		}
		got = append(got, ev.data)
	}
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("events = %q", got)
	}
}
