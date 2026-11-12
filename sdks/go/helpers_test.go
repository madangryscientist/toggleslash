package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

const testKey = "fs_server_test"

// fakeClock fires its timers only when a test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]struct{}
}

type fakeTimer struct {
	clk      *fakeClock
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC), timers: map[*fakeTimer]struct{}{}}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTimer(d time.Duration) clockTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{clk: f, deadline: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.timers[t] = struct{}{}
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	_, active := t.clk.timers[t]
	delete(t.clk.timers, t)
	return active
}

// Armed is how many timers are waiting.
func (f *fakeClock) Armed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// Advance moves time forward and fires every timer that falls due, earliest first.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	var due []*fakeTimer
	for t := range f.timers {
		if !t.deadline.After(f.now) {
			due = append(due, t)
			delete(f.timers, t)
		}
	}
	now := f.now
	f.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].deadline.Before(due[j].deadline) })
	for _, t := range due {
		t.ch <- now
	}
}

// recorded is one request the fake server saw.
type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// fakeServer answers the three SDK routes in memory.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	reqs     []recorded
	flags    []string // JSON of each flag
	version  int
	handlers map[string]http.HandlerFunc
	streams  []chan string
	eventsOK int // status for the events route; 0 means 200
}

func newFakeServer(t *testing.T, flags ...string) *fakeServer {
	t.Helper()
	s := &fakeServer{flags: flags, version: 1, handlers: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) etag() string { return `"v` + strconv.Itoa(s.version) + `"` }

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.reqs = append(s.reqs, recorded{r.Method, r.URL.Path, r.Header.Clone(), body})
	h := s.handlers[r.URL.Path]
	s.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	switch r.URL.Path {
	case "/sdk/v1/ruleset":
		s.mu.Lock()
		etag, doc := s.etag(), s.rulesetJSON()
		s.mu.Unlock()
		if r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, doc)
	case "/sdk/v1/stream":
		s.serveStream(w, r)
	case "/sdk/v1/events":
		s.mu.Lock()
		status := s.eventsOK
		s.mu.Unlock()
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeServer) rulesetJSON() string {
	doc := `{"flags":[`
	for i, f := range s.flags {
		if i > 0 {
			doc += ","
		}
		doc += f
	}
	return doc + `],"segments":[]}`
}

func (s *fakeServer) serveStream(w http.ResponseWriter, r *http.Request) {
	ch := make(chan string, 16)
	s.mu.Lock()
	s.streams = append(s.streams, ch)
	etag := s.etag()
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := w.(http.Flusher)
	_, _ = io.WriteString(w, "event: connected\ndata: "+etag+"\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case text, ok := <-ch:
			if !ok {
				return
			}
			_, _ = io.WriteString(w, text)
			fl.Flush()
		}
	}
}

// setFlags replaces the flags, bumps the version and tells every open stream.
func (s *fakeServer) setFlags(flags ...string) {
	s.mu.Lock()
	s.flags = flags
	s.version++
	etag := s.etag()
	streams := append([]chan string(nil), s.streams...)
	s.mu.Unlock()
	for _, ch := range streams {
		select {
		case ch <- "event: changed\ndata: " + etag + "\n\n":
		default:
		}
	}
}

// write sends raw text to every open stream.
func (s *fakeServer) write(text string) {
	s.mu.Lock()
	streams := append([]chan string(nil), s.streams...)
	s.mu.Unlock()
	for _, ch := range streams {
		ch <- text
	}
}

// dropStreams ends every open stream from the server's side.
func (s *fakeServer) dropStreams() {
	s.mu.Lock()
	streams := s.streams
	s.streams = nil
	s.mu.Unlock()
	for _, ch := range streams {
		close(ch)
	}
}

func (s *fakeServer) on(path string, h http.HandlerFunc) {
	s.mu.Lock()
	s.handlers[path] = h
	s.mu.Unlock()
}

func (s *fakeServer) requests(path string) []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recorded
	for _, r := range s.reqs {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func (s *fakeServer) all() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.reqs...)
}

// boolFlag is an on flag serving true (variation 1) to everyone, version 1.
func boolFlag(key string, serves bool) string {
	v := 0
	if serves {
		v = 1
	}
	return fmt.Sprintf(`{"key":%q,"variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":1}`, key, v)
}

// valueFlag is an on flag serving one JSON value to everyone.
func valueFlag(key, valueJSON string) string {
	return fmt.Sprintf(`{"key":%q,"variations":[%s],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":7}`, key, valueJSON)
}

// waitFor polls cond until it holds or two seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// errorLog collects what the OnError function receives.
type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, err)
}

func (l *errorLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.errs)
}

func (l *errorLog) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.errs))
	for i, e := range l.errs {
		out[i] = e.Error()
	}
	return out
}

func user(key string) Context { return NewContext(key, nil) }

func mustParse(t *testing.T, text string) jsonvalue.Value {
	t.Helper()
	v, err := jsonvalue.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// newTestClient makes a client against s with a short request timeout, closed when the test ends.
func newTestClient(t *testing.T, s *fakeServer, opts ...Option) *Client {
	t.Helper()
	base := []Option{WithRequestTimeout(2 * time.Second)}
	c := New(s.URL, testKey, append(base, opts...)...)
	t.Cleanup(c.Close)
	return c
}
