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

// streamServer serves /sdk/v1/stream and lets a test push events down every open connection.
type streamServer struct {
	mu    sync.Mutex
	conns []chan string
	opens atomic.Int32
}

func (s *streamServer) handler(initial string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.opens.Add(1)
		ch := make(chan string, 16)
		s.mu.Lock()
		s.conns = append(s.conns, ch)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		if initial != "" {
			_, _ = io.WriteString(w, initial)
			fl.Flush()
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				_, _ = io.WriteString(w, msg)
				fl.Flush()
			}
		}
	}
}

func (s *streamServer) send(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.conns {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *streamServer) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.conns {
		close(ch)
	}
	s.conns = nil
}

func TestStreamConnectsAndFollowsChanges(t *testing.T) {
	var tag atomic.Value
	tag.Store(`"a"`)
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", rulesetHandler(func() string { return tag.Load().(string) }, func() string { return flagRuleset }))
	ss := &streamServer{}
	f.handle("/sdk/v1/stream", ss.handler("event: connected\ndata: \"a\"\n\n"))
	c := New(f.URL, "k", WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	if !c.Ready() {
		t.Fatal("not ready")
	}
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := f.count("/sdk/v1/ruleset")
	tag.Store(`"b"`)
	ss.send("event: changed\ndata: \"b\"\n\n")
	eventually(t, "the new tag", func() bool { return c.Status().ETag == `"b"` })
	if f.count("/sdk/v1/ruleset") != before+1 {
		t.Fatalf("one fetch expected for a change, got %d", f.count("/sdk/v1/ruleset")-before)
	}
	// an event naming the held tag fetches nothing but counts as a sync
	at := c.Status().LastSyncAt
	time.Sleep(5 * time.Millisecond)
	ss.send("event: changed\ndata: \"b\"\n\n")
	eventually(t, "lastSyncAt to move", func() bool { return c.Status().LastSyncAt.After(at) })
	if f.count("/sdk/v1/ruleset") != before+1 {
		t.Fatal("an event naming the held tag fetched")
	}
	// keep-alive comments do not move lastSyncAt
	at = c.Status().LastSyncAt
	ss.send(": keep-alive\n\n")
	time.Sleep(30 * time.Millisecond)
	if !c.Status().LastSyncAt.Equal(at) {
		t.Fatal("a keep-alive moved lastSyncAt")
	}
	hdr := f.seen("/sdk/v1/stream")[0].header
	if hdr.Get("Accept") != "text/event-stream" || hdr.Get("Authorization") != "Bearer k" {
		t.Fatalf("stream headers %v", hdr)
	}
}

func TestStreamDropFallsBackToPollingAndReconnects(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	ss := &streamServer{}
	f.handle("/sdk/v1/stream", ss.handler("event: connected\ndata: \"a\"\n\n"))
	c := New(f.URL, "k", WithStream(true), WithPollInterval(20*time.Millisecond), WithFlushInterval(time.Hour), withJitter(func() float64 { return 0.5 }))
	defer c.Close()
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	ss.dropAll()
	eventually(t, "a reconnect", func() bool { return ss.opens.Load() >= 2 && c.Status().Stream == StreamConnected })
}

func TestStreamFailedFetchEndsTheStream(t *testing.T) {
	var failing atomic.Bool
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(500)
			return
		}
		staticRuleset(`"a"`, flagRuleset)(w, r)
	})
	ss := &streamServer{}
	f.handle("/sdk/v1/stream", ss.handler("event: connected\ndata: \"a\"\n\n"))
	log := &errorLog{}
	c := New(f.URL, "k", WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithOnError(log.add), withJitter(func() float64 { return 0.5 }))
	defer c.Close()
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	failing.Store(true)
	ss.send("event: changed\ndata: \"b\"\n\n")
	eventually(t, "the stream to end", func() bool { return c.Status().LastError == "http_500" })
	if len(log.all()) == 0 {
		t.Fatal("no onError for the failed fetch")
	}
	if !c.BoolVariation("f", user("u"), false) {
		t.Fatal("the ruleset held was lost")
	}
}

func TestStreamHTTPErrorIsReported(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	c := New(f.URL, "k", WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	c.Ready()
	eventually(t, "http_401", func() bool { return c.Status().LastError == "http_401" })
	if c.Status().Stream != StreamDisconnected {
		t.Fatalf("stream %q", c.Status().Stream)
	}
}

// The watchdog runs on a real loopback socket: a stream that goes quiet drops with "timeout", and a
// keep-alive restarts it.
func TestStreamWatchdog(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	ss := &streamServer{}
	f.handle("/sdk/v1/stream", ss.handler("event: connected\ndata: \"a\"\n\n"))
	c := New(f.URL, "k", WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(time.Hour),
		withIdle(250*time.Millisecond), withJitter(func() float64 { return 0.5 }))
	defer c.Close()
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	// keep-alives every 100 ms for 700 ms: still connected
	for i := 0; i < 7; i++ {
		ss.send(": keep-alive\n\n")
		time.Sleep(100 * time.Millisecond)
		if c.Status().Stream != StreamConnected {
			t.Fatalf("a keep-alive did not restart the watchdog (round %d, %q)", i, c.Status().Stream)
		}
	}
	// quiet: dropped with timeout
	eventually(t, "the watchdog", func() bool { return c.Status().LastError == SyncTimeout })
}

func TestRetryAfterOnRulesetHoldsAndCaps(t *testing.T) {
	cases := []struct {
		header string
		valid  bool
		want   time.Duration
	}{
		{"7", true, 7 * time.Second},
		{" 7 ", true, 7 * time.Second},
		{"0", true, 0},
		{"99999999", true, 300 * time.Second},
		{"99999999999999999999999999", true, 300 * time.Second},
		{"-1", false, 0},
		{"1.5", false, 0},
		{"Wed, 21 Oct 2026 07:28:00 GMT", false, 0},
		{"", false, 0},
		{"+5", false, 0},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.header != "" {
			h.Set("Retry-After", tc.header)
		}
		got, ok := parseRetryAfter(h)
		if ok != tc.valid || (ok && got != tc.want) {
			t.Errorf("%q: got %v %v, want %v %v", tc.header, got, ok, tc.want, tc.valid)
		}
	}
}

func TestRetryAfterHoldsThePollOnAFakeClock(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var limited atomic.Bool
	limited.Store(true)
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		if limited.Load() {
			w.Header().Set("Retry-After", "9999")
			w.WriteHeader(429)
			return
		}
		staticRuleset(`"a"`, flagRuleset)(w, r)
	})
	c := New(f.URL, "k", WithStream(false), WithPollInterval(10*time.Second), WithFlushInterval(time.Hour), withClock(clk))
	defer c.Close()
	c.Ready()
	if got := c.Status().LastError; got != "http_429" {
		t.Fatalf("lastError %q", got)
	}
	limited.Store(false)
	clk.waitArmed(t, 2) // the poll timer and the flush timer
	clk.Advance(11 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Fatalf("the poll went out inside the Retry-After hold (%d requests)", f.count("/sdk/v1/ruleset"))
	}
	clk.Advance(300 * time.Second)
	eventually(t, "the poll after the hold", func() bool { return f.count("/sdk/v1/ruleset") == 2 })
	eventually(t, "ready", func() bool { return c.Status().ETag == `"a"` })
}

func TestPollingIntervalOnAFakeClock(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	c := New(f.URL, "k", WithStream(false), WithPollInterval(30*time.Second), WithFlushInterval(time.Hour), withClock(clk))
	defer c.Close()
	c.Ready()
	clk.waitArmed(t, 2)
	clk.Advance(29 * time.Second)
	time.Sleep(30 * time.Millisecond)
	if f.count("/sdk/v1/ruleset") != 1 {
		t.Fatal("polled early")
	}
	clk.Advance(time.Second)
	eventually(t, "the second poll", func() bool { return f.count("/sdk/v1/ruleset") == 2 })
}

func TestStreamBackoffDelayFormula(t *testing.T) {
	c := &Client{cfg: defaultConfig()}
	c.cfg.stream = true
	c.cfg.pollInterval = 30 * time.Second
	c.cfg.jitter = func() float64 { return 1 }
	want := map[int]time.Duration{0: time.Second, 1: 2 * time.Second, 2: 4 * time.Second, 3: 8 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second, 40: 30 * time.Second}
	for failures, d := range want {
		if got := c.delay(failures); got != d {
			t.Errorf("failures %d: %v, want %v", failures, got, d)
		}
	}
	c.cfg.jitter = func() float64 { return 0.5 }
	if got := c.delay(5); got != 15*time.Second {
		t.Errorf("jittered: %v", got)
	}
	c.cfg.stream = false
	if got := c.delay(9); got != 30*time.Second {
		t.Errorf("polling: %v", got)
	}
}

func TestTLSWithALocalCertificate(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	_ = f
	tls := newFakeTLS(t)
	tls.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	// without the certificate the request fails
	bad := New(tls.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer bad.Close()
	if bad.Ready() {
		t.Fatal("an untrusted certificate was accepted")
	}
	good := New(tls.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withTLSConfig(tls.Client().Transport.(*http.Transport).TLSClientConfig))
	defer good.Close()
	if !good.Ready() {
		t.Fatalf("a trusted certificate failed: %q", good.Status().LastError)
	}
}

func TestSSEParserRules(t *testing.T) {
	var got []string
	run := func(chunks ...string) []string {
		got = nil
		var p sseParser
		for _, ch := range chunks {
			p.feed([]byte(ch), func(e sseEvent) bool { got = append(got, e.name+"="+e.data); return true })
		}
		return got
	}
	eq := func(name string, have []string, want ...string) {
		t.Helper()
		if strings.Join(have, "|") != strings.Join(want, "|") {
			t.Errorf("%s: got %q want %q", name, have, want)
		}
	}
	eq("simple", run("event: changed\ndata: x\n\n"), "changed=x")
	eq("default name", run("data: x\n\n"), "message=x")
	eq("split anywhere", run("eve", "nt: chan", "ged\nda", "ta: x\n", "\n"), "changed=x")
	eq("crlf", run("event: a\r\ndata: 1\r\n\r\n"), "a=1")
	eq("cr only", run("event: a\rdata: 1\r\r"), "a=1")
	eq("cr then lf across chunks", run("data: 1\r", "\ndata: 2\r\n\r\n"), "message=1\n2")
	eq("no data field", run("event: a\n\n"))
	eq("comment", run(": hi\n\ndata: 1\n\n"), "message=1")
	eq("one leading space", run("data:  x\n\n"), "message= x")
	eq("no space", run("data:x\n\n"), "message=x")
	eq("bom", run("\xef\xbb\xbfdata: 1\n\n"), "message=1")
	eq("bom split", run("\xef", "\xbb\xbfdata: 1\n\n"), "message=1")
	eq("unterminated event is dropped", run("data: 1\n"))
	eq("name resets", run("event: a\ndata: 1\n\ndata: 2\n\n"), "a=1", "message=2")
	eq("id and retry ignored", run("id: 1\nretry: 5\ndata: x\n\n"), "message=x")
	var p sseParser
	n := 0
	cont := p.feed([]byte("data: 1\n\ndata: 2\n\n"), func(sseEvent) bool { n++; return false })
	if cont || n != 1 {
		t.Errorf("a false return should stop the feed (cont=%v n=%d)", cont, n)
	}
}
