package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// seen is one request the fake server received.
type seen struct {
	Method string
	Path   string
	Body   string
	Header http.Header
	Host   string
}

// fakeServer is one loopback server for all three routes. It records every request (headers included) and
// lets a test swap the handler of a route. A handler that hangs must return when f.release closes.
type fakeServer struct {
	t       *testing.T
	srv     *httptest.Server
	release chan struct{}
	feed    chan string // raw bytes the default stream handler sends after "connected"

	mu       sync.Mutex
	requests []seen
	handlers map[string]http.HandlerFunc
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	return startFake(t, false)
}

func newTLSFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	return startFake(t, true)
}

func startFake(t *testing.T, useTLS bool) *fakeServer {
	t.Helper()
	f := &fakeServer{
		t:        t,
		release:  make(chan struct{}),
		feed:     make(chan string, 64),
		handlers: map[string]http.HandlerFunc{},
	}
	f.handlers["/sdk/v1/ruleset"] = f.rulesetVersion(1)
	f.handlers["/sdk/v1/stream"] = f.defaultStream
	f.handlers["/sdk/v1/events"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, seen{r.Method, r.URL.Path, string(body), r.Header.Clone(), r.Host})
		handler := f.handlers[r.URL.Path]
		f.mu.Unlock()
		if handler == nil {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
	if useTLS {
		f.srv = httptest.NewTLSServer(h)
	} else {
		f.srv = httptest.NewServer(h)
	}
	t.Cleanup(func() {
		close(f.release)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

func (f *fakeServer) url() string { return f.srv.URL }

// handle replaces the handler of a path, such as "/sdk/v1/events".
func (f *fakeServer) handle(path string, h http.HandlerFunc) {
	f.mu.Lock()
	f.handlers[path] = h
	f.mu.Unlock()
}

// count is how many requests a path has seen.
func (f *fakeServer) count(path string) int { return len(f.seen(path)) }

func (f *fakeServer) seen(path string) []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, r := range f.requests {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// await waits until a path has seen at least n requests.
func (f *fakeServer) await(path string, n int) {
	f.t.Helper()
	eventually(f.t, fmt.Sprintf("%d requests to %s", n, path), func() bool { return f.count(path) >= n })
}

// rulesetVersion serves the default ruleset at that version: one flag "f", version n, ETag "n", and 304 for
// a matching If-None-Match.
func (f *fakeServer) rulesetVersion(n int) http.HandlerFunc {
	etag := fmt.Sprintf(`"%d"`, n)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, defaultRuleset(n))
	}
}

func (f *fakeServer) defaultStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event: connected\ndata: \"1\"\n\n")
	w.(http.Flusher).Flush()
	for {
		select {
		case chunk := <-f.feed:
			_, _ = io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		case <-f.release:
			return
		}
	}
}

// hang blocks until the client gives up or the test ends.
func (f *fakeServer) hang(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-f.release:
	}
	_ = w
}

// flagJSON is one valid flag with one serve: variations is a JSON array, serve the index the fallthrough
// serves.
func flagJSON(key string, version int, variations string, serve int) string {
	return fmt.Sprintf(`{"key":%q,"on":true,"variations":%s,"offVariation":0,"salt":"s","version":%d,`+
		`"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d}}`, key, variations, version, serve)
}

func rulesetJSON(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

// defaultRuleset has the flag "f" (false, true; serves true) at version n.
func defaultRuleset(n int) string { return rulesetJSON(flagJSON("f", n, `[false,true]`, 1)) }

// user is a valid single context.
func user(key string) Context { return Object(Member{Key: "key", Value: String(key)}) }

// eventually polls cond for up to ten seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newTestClient makes a client against f and closes it when the test ends.
func newTestClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	c := NewClient(f.url(), "test-key", opts...)
	t.Cleanup(c.Close)
	return c
}

// within runs fn and fails the test if it takes longer than d.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s took longer than %v", what, d)
	}
}

// errorLog collects what OnError was told.
type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	l.errs = append(l.errs, err)
	l.mu.Unlock()
}

func (l *errorLog) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.errs))
	for i, err := range l.errs {
		out[i] = err.Error()
	}
	return out
}

func (l *errorLog) count() int { return len(l.messages()) }

// fakeClock is time that moves only when a test says so. Timers fire in order inside Advance.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	at      time.Time
	d       time.Duration
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now.Add(d), d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// armed lists the durations of the timers that are waiting, longest last.
func (c *fakeClock) armed() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []time.Duration
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			out = append(out, t.d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// awaitTimer waits until a timer of exactly d is armed.
func (c *fakeClock) awaitTimer(t *testing.T, d time.Duration) {
	t.Helper()
	eventually(t, fmt.Sprintf("a %v timer to be armed (armed: %v)", d, c.armed()), func() bool {
		for _, a := range c.armed() {
			if a == d {
				return true
			}
		}
		return false
	})
}

// advance moves time forward by d, firing each timer that falls due, in order.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if t.stopped || t.fired || t.at.After(target) {
				continue
			}
			if next == nil || t.at.Before(next.at) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		next.fired = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
	}
}

// bigInterval keeps a loop's timer out of the way of a test about the other loop.
const bigInterval = 24 * time.Hour
