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

const testKey = "server-key-for-tests"

var user = NewContext("user-1", nil)

// boolFlag is a flag, on, that serves true or false to everyone. Variation 0 is false, 1 is true.
func boolFlag(key string, serves bool, version int) string {
	v := 0
	if serves {
		v = 1
	}
	return fmt.Sprintf(`{"key":%q,"variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`, key, v, version)
}

// valueFlag serves one JSON value (variation 1) to everyone; variation 0 is null.
func valueFlag(key, valueJSON string) string {
	return fmt.Sprintf(`{"key":%q,"variations":[null,%s],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":1}`, key, valueJSON)
}

type recorded struct {
	Method, Path, RawQuery string
	Header                 http.Header
	Body                   []byte
}

// fakeServer is the ToggleSlash server's SDK routes in memory, on a loopback port. Every request is
// recorded. A test replaces a route's handler to make it misbehave.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	flags   []string
	version int
	reqs    []recorded
	events  [][]byte
	streams []chan string

	rulesetFn func(w http.ResponseWriter, r *http.Request)
	streamFn  func(w http.ResponseWriter, r *http.Request)
	eventsFn  func(w http.ResponseWriter, r *http.Request)
}

func newFakeServer(t *testing.T, flags ...string) *fakeServer {
	f := &fakeServer{t: t, flags: flags, version: 1}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) url() string { return f.srv.URL }

func (f *fakeServer) etagLocked() string { return fmt.Sprintf(`"v%d"`, f.version) }

func (f *fakeServer) body() string {
	return `{"flags":[` + strings.Join(f.flags, ",") + `],"segments":[]}`
}

// publish replaces the flags, bumps the ETag and tells every open stream.
func (f *fakeServer) publish(flags ...string) {
	f.mu.Lock()
	f.flags = flags
	f.version++
	tag := f.etagLocked()
	chans := append([]chan string(nil), f.streams...)
	f.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- "event: changed\ndata: " + tag + "\n\n":
		default:
		}
	}
}

func (f *fakeServer) push(raw string) {
	f.mu.Lock()
	chans := append([]chan string(nil), f.streams...)
	f.mu.Unlock()
	for _, ch := range chans {
		ch <- raw
	}
}

func (f *fakeServer) requests(path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.reqs {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeServer) sentEvents() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.events...)
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
	rulesetFn, streamFn, eventsFn := f.rulesetFn, f.streamFn, f.eventsFn
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/sdk/v1/ruleset":
		if rulesetFn != nil {
			rulesetFn(w, r)
			return
		}
		f.defaultRuleset(w, r)
	case "/sdk/v1/stream":
		if streamFn != nil {
			streamFn(w, r)
			return
		}
		f.defaultStream(w, r)
	case "/sdk/v1/events":
		if eventsFn != nil {
			eventsFn(w, r)
			return
		}
		f.mu.Lock()
		f.events = append(f.events, body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeServer) defaultRuleset(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	tag, body := f.etagLocked(), f.body()
	f.mu.Unlock()
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func (f *fakeServer) defaultStream(w http.ResponseWriter, r *http.Request) {
	ch := make(chan string, 16)
	f.mu.Lock()
	f.streams = append(f.streams, ch)
	tag := f.etagLocked()
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		for i, c := range f.streams {
			if c == ch {
				f.streams = append(f.streams[:i], f.streams[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := w.(http.Flusher)
	_, _ = io.WriteString(w, "event: connected\ndata: "+tag+"\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case s := <-ch:
			if _, err := io.WriteString(w, s); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// hang holds a request open until the client gives up, after reading what it sent (net/http notices a
// disconnect only when the handler reads or writes).
func hang(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", within, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// errorLog collects what onError receives.
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

func (l *errorLog) texts() []string {
	var out []string
	for _, e := range l.all() {
		out = append(out, e.Error())
	}
	return out
}

func (l *errorLog) count() int { return len(l.all()) }

// newTestClient makes a client against the fake server and closes it when the test ends. It polls by
// default so a test that wants the stream turns it on.
func newTestClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	all := append([]Option{WithStream(false), WithRequestTimeout(2 * time.Second)}, opts...)
	c := New(f.url(), testKey, all...)
	t.Cleanup(c.Close)
	return c
}
