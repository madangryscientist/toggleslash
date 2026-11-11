package toggleslash

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// The seams are unexported config fields, so a test sets them through these functions, which only a _test
// file defines. No exported option can reach them.

func withClock(c clock) Option           { return func(cfg *config) { cfg.clock = c } }
func withJitter(f func() float64) Option { return func(cfg *config) { cfg.jitter = f } }
func withWatchdog(d time.Duration) Option {
	return func(cfg *config) { cfg.watchdog = d }
}
func withEvaluate(f func(*evaluator.Ruleset, string, Value) evaluator.Result) Option {
	return func(cfg *config) { cfg.evaluate = f }
}

// fakeClock moves only when a test says so. Timers fire in the order of their times.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clk     *fakeClock
	at      time.Time
	ch      chan time.Time
	stopped bool
	fired   bool
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTimer(d time.Duration) timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{clk: f, at: f.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.fired = true
		t.ch <- f.now
		return t
	}
	f.timers = append(f.timers, t)
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// Armed is how many timers are waiting to fire. A test steps the clock only when the SDK is waiting on it.
// A client with no stream has two timers, the sync loop's poll and the flush: the flush timer is always
// there, so a test that waits for the poll waits for Armed() >= 2, never for > 0 (a step taken while only
// the flush timer is armed is lost, and the poll timer is then set from the new time).
func (f *fakeClock) Armed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// Waiting lists the durations the armed timers still have to wait.
func (f *fakeClock) Waiting() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []time.Duration
	for _, t := range f.timers {
		if !t.stopped && !t.fired {
			out = append(out, t.at.Sub(f.now))
		}
	}
	return out
}

// Advance moves time forward and fires every timer that has come due.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	for _, t := range f.timers {
		if !t.stopped && !t.fired && !t.at.After(f.now) {
			t.fired = true
			t.ch <- f.now
		}
	}
}

// eventually polls cond until it is true, and fails the test if it is not true within ten seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// errors collects what onError was told.
type errorLog struct {
	mu   sync.Mutex
	list []string
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = append(l.list, err.Error())
}

func (l *errorLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.list...)
}

func (l *errorLog) option() Option { return WithOnError(l.add) }

// seen is one request the fake server got.
type seen struct {
	method string
	path   string
	host   string
	header http.Header
	body   string
}

// fakeServer is a loopback ToggleSlash server. It answers as the real one does unless a test replaced a
// route's handler, and it records every request with its headers.
type fakeServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []seen
	ruleset  string
	etag     string
	handlers map[string]http.HandlerFunc
}

const (
	routeRuleset = "GET /sdk/v1/ruleset"
	routeStream  = "GET /sdk/v1/stream"
	routeEvents  = "POST /sdk/v1/events"
)

func newFakeServer(t *testing.T, ruleset string) *fakeServer {
	t.Helper()
	s := &fakeServer{ruleset: ruleset, etag: `"v1"`, handlers: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	route := r.Method + " " + r.URL.Path
	s.mu.Lock()
	s.requests = append(s.requests, seen{r.Method, r.URL.Path, r.Host, r.Header.Clone(), string(body)})
	h := s.handlers[route]
	s.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	switch route {
	case routeRuleset:
		s.mu.Lock()
		etag, ruleset := s.etag, s.ruleset
		s.mu.Unlock()
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ruleset)
	case routeStream:
		w.Header().Set("Content-Type", "text/event-stream")
		s.mu.Lock()
		etag := s.etag
		s.mu.Unlock()
		_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", etag)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	case routeEvents:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// handle replaces the answer of one route.
func (s *fakeServer) handle(route string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[route] = h
}

// publish makes a new ruleset current, under a new tag.
func (s *fakeServer) publish(ruleset, etag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ruleset, s.etag = ruleset, etag
}

// got lists the requests for a route so far.
func (s *fakeServer) got(route string) []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []seen
	for _, r := range s.requests {
		if r.method+" "+r.path == route {
			out = append(out, r)
		}
	}
	return out
}

func (s *fakeServer) count(route string) int { return len(s.got(route)) }

// drainThenHang reads the request body, so the SDK's write completes, and then answers nothing until the
// client gives up.
func drainThenHang(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

// flag builds one boolean flag that is on and serves variation fall; its variations are false then true.
func flag(key string, fall int, version int) string {
	return flagWith(key, `[false,true]`, fall, version)
}

func flagWith(key, variations string, fall, version int) string {
	return fmt.Sprintf(`{"key":%q,"variations":%s,"on":true,"offVariation":0,"targets":[],`+
		`"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`,
		key, variations, fall, version)
}

func rulesetOf(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

var shopper = NewContext("user-1", nil)

// newTLSServer serves one ruleset over HTTPS with a certificate only its own Client() trusts.
func newTLSServer(t *testing.T, ruleset string) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/ruleset" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"t1"`)
		_, _ = io.WriteString(w, ruleset)
	}))
	// The client that does not trust the certificate makes a handshake error the server would log.
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}
