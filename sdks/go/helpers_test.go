package toggleslash

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// flagJSON is an always-on flag with the given variations (a JSON array) and the fallthrough variation.
func flagJSON(key, variations string, fall int, version int) string {
	return fmt.Sprintf(`{"key":%q,"variations":%s,"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`,
		key, variations, fall, version)
}

func rulesetJSON(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

// seen is one request the fake server received.
type seen struct {
	method, path, query string
	host                string
	header              http.Header
	body                []byte
	conditional         bool
}

// route names the request the way the header audit does.
func (s seen) route() string {
	switch {
	case s.path == "/sdk/v1/ruleset" && s.conditional:
		return "ruleset (conditional)"
	case s.path == "/sdk/v1/ruleset":
		return "ruleset"
	case s.path == "/sdk/v1/stream":
		return "stream"
	case s.path == "/sdk/v1/events":
		return "events"
	}
	return s.path
}

// fake is a loopback ToggleSlash server. Its three handlers are replaceable; the defaults serve the held
// ruleset with an ETag (304 for a matching If-None-Match), hold a stream open after "connected", and
// accept usage.
type fake struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	reqs    []seen
	body    string
	etag    string
	ruleset http.HandlerFunc
	stream  http.HandlerFunc
	events  http.HandlerFunc

	done chan struct{}
}

func newFake(t *testing.T, body string) *fake {
	t.Helper()
	f := &fake{t: t, body: body, etag: `"v1"`, done: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.close)
	return f
}

func newTLSFake(t *testing.T, body string) *fake {
	t.Helper()
	f := &fake{t: t, body: body, etag: `"v1"`, done: make(chan struct{})}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.close)
	return f
}

func (f *fake) close() {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	f.srv.CloseClientConnections()
	f.srv.Close()
}

func (f *fake) url() string { return f.srv.URL }

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, seen{
		method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, host: r.Host,
		header: r.Header.Clone(), body: body, conditional: r.Header.Get("If-None-Match") != "",
	})
	ruleset, stream, events := f.ruleset, f.stream, f.events
	f.mu.Unlock()
	switch r.URL.Path {
	case "/sdk/v1/ruleset":
		if ruleset == nil {
			ruleset = f.defaultRuleset
		}
		ruleset(w, r)
	case "/sdk/v1/stream":
		if stream == nil {
			stream = f.defaultStream
		}
		stream(w, r)
	case "/sdk/v1/events":
		if events == nil {
			events = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
		}
		events(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) current() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body, f.etag
}

// set replaces the ruleset the default handler serves.
func (f *fake) set(body, etag string) {
	f.mu.Lock()
	f.body, f.etag = body, etag
	f.mu.Unlock()
}

func (f *fake) on(ruleset, stream, events http.HandlerFunc) {
	f.mu.Lock()
	if ruleset != nil {
		f.ruleset = ruleset
	}
	if stream != nil {
		f.stream = stream
	}
	if events != nil {
		f.events = events
	}
	f.mu.Unlock()
}

func (f *fake) defaultRuleset(w http.ResponseWriter, r *http.Request) {
	body, etag := f.current()
	if etag != "" && r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func (f *fake) defaultStream(w http.ResponseWriter, r *http.Request) {
	_, etag := f.current()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", etag)
	w.(http.Flusher).Flush()
	select {
	case <-r.Context().Done():
	case <-f.done:
	}
}

// hang drains the request body, so the client's write finishes, then holds the answer until the test ends.
func (f *fake) hang(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-f.done:
	}
}

func (f *fake) requests(path string) []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, s := range f.reqs {
		if s.path == path {
			out = append(out, s)
		}
	}
	return out
}

func (f *fake) all() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.reqs...)
}

// eventually polls cond until it holds or the time runs out.
func eventually(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for: %s", within, what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// within fails the test if fn does not return in time.
func within(t *testing.T, what string, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

// errorLog collects what onError is told.
type errorLog struct {
	mu   sync.Mutex
	errs []*Error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var e *Error
	if errors.As(err, &e) {
		l.errs = append(l.errs, e)
	} else {
		l.errs = append(l.errs, &Error{Op: "?", Message: err.Error()})
	}
}

func (l *errorLog) all() []*Error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Error(nil), l.errs...)
}

func (l *errorLog) count() int { return len(l.all()) }

// fakeClock is a clock the test moves by hand. Timers fire only when Advance passes them.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	c       chan time.Time
	at      time.Time
	d       time.Duration
	stopped bool
	fired   bool
}

func newFakeClock(at time.Time) *fakeClock { return &fakeClock{now: at} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, c: make(chan time.Time, 1), at: c.now.Add(d), d: d}
	if d <= 0 {
		t.fired = true
		t.c <- c.now
	}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// advance moves time forward and fires every timer that is now due.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			t.c <- c.now
		}
	}
}

// waitTimer waits until a live timer of exactly d exists: the SDK is then waiting on the clock.
func (c *fakeClock) waitTimer(t *testing.T, d time.Duration) {
	t.Helper()
	eventually(t, fmt.Sprintf("a pending %v timer", d), 5*time.Second, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, tm := range c.timers {
			if !tm.stopped && !tm.fired && tm.d == d {
				return true
			}
		}
		return false
	})
}

// makeClient is New with the stream off and a long flush interval unless the test says otherwise.
func makeClient(t *testing.T, f *fake, opts ...Option) *Client {
	t.Helper()
	base := []Option{WithStream(false), WithRequestTimeout(3 * time.Second)}
	c := New(f.url(), "sdk-test-key", append(base, opts...)...)
	t.Cleanup(c.Close)
	return c
}

func ctxFor(key string) Context { return NewContext("user", key, nil) }
