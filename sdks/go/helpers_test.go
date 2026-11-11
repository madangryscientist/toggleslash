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

// seen is one request the fake server received.
type seen struct {
	method string
	path   string
	host   string
	header http.Header
	body   string
}

// fake is a ToggleSlash server on a loopback socket. It serves one ruleset, bumps its tag on
// change, and records every request. A test overrides a route with a handler.
type fake struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	version  int
	flags    []string // flag JSON texts
	requests []seen

	// A handler returns true when it answered.
	onRuleset func(w http.ResponseWriter, r *http.Request) bool
	onStream  func(w http.ResponseWriter, r *http.Request) bool
	onEvents  func(w http.ResponseWriter, r *http.Request) bool
	// release ends every handler a test left hanging.
	release chan struct{}
}

func newFake(t *testing.T, flags ...string) *fake {
	t.Helper()
	f := &fake{t: t, version: 1, flags: flags, release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(func() {
		close(f.release)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

func newFakeTLS(t *testing.T, flags ...string) *fake {
	t.Helper()
	f := &fake{t: t, version: 1, flags: flags, release: make(chan struct{})}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(func() {
		close(f.release)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

func (f *fake) url() string { return f.srv.URL }

func (f *fake) etag() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf(`"v%d"`, f.version)
}

// set replaces the ruleset and bumps the tag.
func (f *fake) set(flags ...string) {
	f.mu.Lock()
	f.flags = flags
	f.version++
	f.mu.Unlock()
}

func (f *fake) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, seen{
		method: r.Method, path: r.URL.Path, host: r.Host, header: r.Header.Clone(), body: string(body),
	})
	onRuleset, onStream, onEvents := f.onRuleset, f.onStream, f.onEvents
	f.mu.Unlock()
	switch r.URL.Path {
	case "/sdk/v1/ruleset":
		if onRuleset != nil && onRuleset(w, r) {
			return
		}
		f.serveRuleset(w, r)
	case "/sdk/v1/stream":
		if onStream != nil && onStream(w, r) {
			return
		}
		f.serveStream(w, r)
	case "/sdk/v1/events":
		if onEvents != nil && onEvents(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) serveRuleset(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	tag := fmt.Sprintf(`"v%d"`, f.version)
	body := `{"flags":[` + strings.Join(f.flags, ",") + `],"segments":[]}`
	f.mu.Unlock()
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", tag)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// serveStream sends connected and then holds the stream open until the test ends.
func (f *fake) serveStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", f.etag())
	w.(http.Flusher).Flush()
	select {
	case <-r.Context().Done():
	case <-f.release:
	}
}

// hang drains the request and never answers, as a stuck server does. net/http does not notice a
// client that went away until the handler reads or writes, so a handler that only slept would hold
// the test up.
func (f *fake) hang(w http.ResponseWriter, r *http.Request) bool {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-f.release:
	}
	return true
}

func (f *fake) routeRequests(path string) []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, s := range f.requests {
		if s.path == path {
			out = append(out, s)
		}
	}
	return out
}

// boolFlag is a boolean flag, on, serving serves, at the given version.
func boolFlag(key string, serves bool, version int) string {
	idx := 0
	if serves {
		idx = 1
	}
	return fmt.Sprintf(
		`{"key":%q,"variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],`+
			`"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`, key, idx, version)
}

// valueFlag is a flag that serves one value, given as JSON text.
func valueFlag(key, valueJSON string) string {
	return fmt.Sprintf(
		`{"key":%q,"variations":[%s],"on":true,"offVariation":0,"targets":[],"prerequisites":[],`+
			`"rules":[],"fallthrough":{"variation":0},"salt":"s","version":1}`, key, valueJSON)
}

var user = UserContext("u1", nil)

// waitFor polls cond until it holds or the time is up.
func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// errorLog collects what onError receives.
type errorLog struct {
	mu   sync.Mutex
	errs []*Error
}

func (l *errorLog) add(err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Message: err.Error()}
	}
	l.mu.Lock()
	l.errs = append(l.errs, e)
	l.mu.Unlock()
}

func (l *errorLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.errs)
}

func (l *errorLog) all() []*Error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Error(nil), l.errs...)
}

// eventsOf parses a usage request body into "flag/variation/day" -> count.
func eventsOf(t *testing.T, body string) map[string]int {
	t.Helper()
	v, err := ParseJSON([]byte(body))
	if err != nil {
		t.Fatalf("usage body is not JSON: %v", err)
	}
	out := map[string]int{}
	for _, e := range v.Field("events").Items() {
		k := fmt.Sprintf("%s/%d/%s", e.Field("flag").String(), int(e.Field("variationIndex").Number()), e.Field("day").String())
		out[k] += int(e.Field("count").Number())
	}
	return out
}

func newClient(t *testing.T, url string, opts ...Option) *Client {
	t.Helper()
	c := New(url, "fs_server_test", opts...)
	t.Cleanup(c.Close)
	return c
}
