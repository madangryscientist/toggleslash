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

// boolFlagJSON is a boolean flag that is on and serves true, at the given version.
func boolFlagJSON(key string, version int) string {
	return fmt.Sprintf(`{"key":%q,"variations":[false,true],"on":true,"offVariation":0,"targets":[],`+
		`"prerequisites":[],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":%d}`, key, version)
}

// valueFlagJSON is a flag that is on and serves the one JSON value given.
func valueFlagJSON(key, value string) string {
	return fmt.Sprintf(`{"key":%q,"variations":[%s],"on":true,"offVariation":0,"targets":[],`+
		`"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":7}`, key, value)
}

func rulesetJSON(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

// request is what the fake server saw.
type request struct {
	method string
	path   string
	header http.Header
	body   string
	at     time.Time
}

// fakeServer is one loopback server that records every request and answers with its handler. A
// handler that hangs waits on done, which cleanup closes first, so the server can shut down.
type fakeServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []request
	done chan struct{}
}

func newFake(t *testing.T, handler func(f *fakeServer, w http.ResponseWriter, r *http.Request)) *fakeServer {
	return newFakeWith(t, handler, httptest.NewServer)
}

func newFakeWith(t *testing.T, handler func(f *fakeServer, w http.ResponseWriter, r *http.Request), start func(http.Handler) *httptest.Server) *fakeServer {
	t.Helper()
	f := &fakeServer{done: make(chan struct{})}
	f.Server = start(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the whole body first: a handler that hangs must not leave an unread request body.
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<24))
		f.mu.Lock()
		f.reqs = append(f.reqs, request{r.Method, r.URL.Path, r.Header.Clone(), string(body), time.Now()})
		f.mu.Unlock()
		handler(f, w, r)
	}))
	t.Cleanup(func() {
		close(f.done)
		f.CloseClientConnections()
		f.Close()
	})
	return f
}

func (f *fakeServer) requests(path string) []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []request
	for _, r := range f.reqs {
		if path == "" || r.path == path {
			out = append(out, r)
		}
	}
	return out
}

// hang blocks until the test ends.
func (f *fakeServer) hang() { <-f.done }

// flush writes what has been written so far to the client.
func flush(w http.ResponseWriter) {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

// ruleHolder serves a ruleset and its tag, which a test can swap.
type ruleHolder struct {
	mu   sync.Mutex
	etag string
	body string
}

func (h *ruleHolder) set(etag, body string) {
	h.mu.Lock()
	h.etag, h.body = etag, body
	h.mu.Unlock()
}

func (h *ruleHolder) get() (string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.etag, h.body
}

// serve answers the ruleset route with the held ruleset (a 304 for the tag held), the stream route
// with a connected event and then silence, and the events route with 204.
func (h *ruleHolder) serve(f *fakeServer, w http.ResponseWriter, r *http.Request) {
	etag, body := h.get()
	switch r.URL.Path {
	case "/sdk/v1/ruleset":
		if etag != "" && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	case "/sdk/v1/stream":
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", etag)
		flush(w)
		select {
		case <-f.done:
		case <-r.Context().Done():
		}
	case "/sdk/v1/events":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// eventually polls cond until it holds or the time is up.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	const limit = 5 * time.Second
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", limit, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// within fails the test if fn takes longer than d.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

// newTestClient starts a client against the server and closes it when the test ends.
func newTestClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	c := New(f.URL, "test-key", opts...)
	t.Cleanup(c.Close)
	return c
}

// errorLog collects what onError receives.
type errorLog struct {
	mu   sync.Mutex
	errs []string
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	l.errs = append(l.errs, err.Error())
	l.mu.Unlock()
}

func (l *errorLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.errs...)
}

func (l *errorLog) count() int { return len(l.all()) }

var userCtx = NewUserContext("u1")
