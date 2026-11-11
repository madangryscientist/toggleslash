package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// flagJSON is a flag that is on and serves the variation at index fall to everyone.
func flagJSON(key string, variations string, fall int, version int) string {
	return fmt.Sprintf(`{"key":%q,"variations":%s,"on":true,"offVariation":0,"targets":[],"prerequisites":[],`+
		`"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`, key, variations, fall, version)
}

// rulesetJSON wraps flags in a ruleset body.
func rulesetJSON(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

// boolRuleset has one flag "f" that serves true.
func boolRuleset() string { return rulesetJSON(flagJSON("f", `[false,true]`, 1, 7)) }

// user is a context.
var user = map[string]any{"key": "user-1"}

// waitFor polls cond until it is true or five seconds have passed. It waits on events, not on a fixed pause:
// the poll is only how often the condition is looked at.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// recorded is one request the fake server saw.
type recorded struct {
	method string
	path   string
	header http.Header
	host   string
	body   string
}

// fake is a loopback ToggleSlash server. Each route has a handler a test can replace.
type fake struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	reqs    []recorded
	ruleset http.HandlerFunc
	stream  http.HandlerFunc
	events  http.HandlerFunc
}

func newFake(t *testing.T) *fake { return newFakeWith(t, false) }

func newFakeWith(t *testing.T, useTLS bool) *fake {
	t.Helper()
	f := &fake{t: t}
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, boolRuleset())
	}
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no stream", http.StatusServiceUnavailable)
	}
	f.events = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }
	if useTLS {
		f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	} else {
		f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	}
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.Header.Clone(), r.Host, string(body)})
	handler := map[string]http.HandlerFunc{
		"/sdk/v1/ruleset": f.ruleset, "/sdk/v1/stream": f.stream, "/sdk/v1/events": f.events,
	}[r.URL.Path]
	f.mu.Unlock()
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	handler(w, r)
}

// set replaces a route's handler.
func (f *fake) set(route *http.HandlerFunc, h http.HandlerFunc) {
	f.mu.Lock()
	*route = h
	f.mu.Unlock()
}

// requests returns the requests so far to a path ("" for all).
func (f *fake) requests(path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.reqs {
		if path == "" || r.path == path {
			out = append(out, r)
		}
	}
	return out
}

func (f *fake) count(path string) int { return len(f.requests(path)) }

// sseFeed is a stream handler a test pushes bytes into.
type sseFeed struct {
	opened chan struct{}
	data   chan string
	end    chan struct{}
}

func newFeed() *sseFeed {
	return &sseFeed{opened: make(chan struct{}, 64), data: make(chan string, 64), end: make(chan struct{}, 1)}
}

func (s *sseFeed) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	s.opened <- struct{}{}
	for {
		select {
		case chunk := <-s.data:
			_, _ = io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
		case <-s.end:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (s *sseFeed) push(chunk string) { s.data <- chunk }

func (s *sseFeed) waitOpened(t *testing.T) {
	t.Helper()
	select {
	case <-s.opened:
	case <-time.After(10 * time.Second):
		t.Fatal("the stream was never opened")
	}
}

// fakeClock is a clock a test steps by hand. AfterFunc records the timer; advance fires every timer that has
// come due. Nothing in it sleeps.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	armed  []time.Duration // every duration ever armed, in order
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	d     time.Duration
	f     func()
	done  bool
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now.Add(d), d: d, f: f}
	c.timers = append(c.timers, t)
	c.armed = append(c.armed, d)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.done
	t.done = true
	return was
}

// live returns the durations of the timers still armed.
func (c *fakeClock) live() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []time.Duration
	for _, t := range c.timers {
		if !t.done {
			out = append(out, t.d)
		}
	}
	return out
}

// advance moves time forward and fires each timer that comes due, in order.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.done && !t.at.After(c.now) {
			t.done = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// fireNext waits until a live timer whose duration pred accepts exists, then moves time to it and fires it.
// It returns that timer's duration. This is how a test steps a loop: by counting armed timers, never by
// pausing for a fixed real time.
func (c *fakeClock) fireNext(t *testing.T, what string, pred func(time.Duration) bool) time.Duration {
	t.Helper()
	var chosen *fakeTimer
	waitFor(t, what, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, tm := range c.timers {
			if !tm.done && pred(tm.d) {
				chosen = tm
				return true
			}
		}
		return false
	})
	c.mu.Lock()
	remaining := chosen.at.Sub(c.now)
	c.mu.Unlock()
	c.advance(remaining)
	return chosen.d
}

// everArmed reports whether a timer matching pred was ever armed.
func (c *fakeClock) everArmed(pred func(time.Duration) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.armed {
		if pred(d) {
			return true
		}
	}
	return false
}

// errorLog collects what onError was told.
type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	l.errs = append(l.errs, err)
	l.mu.Unlock()
}

func (l *errorLog) all() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.errs...)
}

func (l *errorLog) count() int { return len(l.all()) }

func (l *errorLog) text() string {
	var b strings.Builder
	for _, e := range l.all() {
		b.WriteString(e.Error())
		b.WriteByte('\n')
	}
	return b.String()
}

// started makes a client against f, closes it with the test, and returns it.
func (f *fake) client(t *testing.T, opts ...Option) *Client {
	t.Helper()
	c := New(f.srv.URL, "sdk-key", opts...)
	t.Cleanup(c.Close)
	return c
}

// testTimeout is the request timeout tests that use a fake clock give the client. Nothing else sleeps for 7
// hours, so a timer of that length is a request's, and any other is a wait.
const testTimeout = 7 * time.Hour

// exactly matches one duration.
func exactly(want time.Duration) func(time.Duration) bool {
	return func(d time.Duration) bool { return d == want }
}

// epoch is where fake clocks start.
var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
