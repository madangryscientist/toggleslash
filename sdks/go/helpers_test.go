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

// seen is one request the fake server received, with everything the client sent.
type seen struct {
	method  string
	path    string
	header  http.Header
	host    string
	length  int64
	body    string
	arrived time.Time
}

// fakeServer is the loopback server the tests run the SDK against. It records every request and lets each
// route be scripted. A route with no handler answers 404.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	requests []seen
	routes   map[string]http.HandlerFunc

	// quit is closed at cleanup so hung handlers return.
	quit chan struct{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{t: t, routes: map[string]http.HandlerFunc{}, quit: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		close(f.quit)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, seen{
		method: r.Method, path: r.URL.Path, header: r.Header.Clone(), host: r.Host,
		length: r.ContentLength, body: string(body), arrived: time.Now(),
	})
	h := f.routes[r.URL.Path]
	f.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (f *fakeServer) handle(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = h
}

func (f *fakeServer) url() string { return f.srv.URL }

func (f *fakeServer) seen(path string) []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []seen
	for _, r := range f.requests {
		if r.path == path {
			out = append(out, r)
		}
	}
	return out
}

// hang answers nothing: it waits until the client goes away or the test ends. The request body was already
// drained by serve, which net/http needs before it notices a client that disconnected.
func (f *fakeServer) hang(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-f.quit:
	}
}

const (
	pathRuleset = "/sdk/v1/ruleset"
	pathStream  = "/sdk/v1/stream"
	pathEvents  = "/sdk/v1/events"
)

// rulesetJSON is a ruleset with one boolean flag, "flag", whose fallthrough serves variation `serve`
// (0: false, 1: true), at the given version.
func rulesetJSON(version int, serve int) string {
	return fmt.Sprintf(`{"flags":[{"key":"flag","version":%d,"salt":"s","on":true,
"variations":[false,true],"offVariation":0,"targets":[],"rules":[],"fallthrough":{"variation":%d},"prerequisites":[]}],
"segments":[]}`, version, serve)
}

// rulesetWith builds a ruleset with one flag whose single variation is the given JSON literal.
func rulesetWith(literal string) string {
	return fmt.Sprintf(`{"flags":[{"key":"flag","version":3,"salt":"s","on":true,
"variations":[%s],"offVariation":0,"targets":[],"rules":[],"fallthrough":{"variation":0},"prerequisites":[]}],
"segments":[]}`, literal)
}

func serveRuleset(body, etag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if etag != "" {
			if r.Header.Get("If-None-Match") == etag {
				w.Header().Set("ETag", etag)
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

// waitFor polls cond until it holds. It fails the test at the deadline, which is long: a test that passes
// does not wait for it.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// errLog collects what onError receives.
type errLog struct {
	mu   sync.Mutex
	errs []string
}

func (l *errLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, err.Error())
}

func (l *errLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.errs...)
}

func (l *errLog) count() int { return len(l.all()) }

func (l *errLog) has(sub string) bool {
	for _, e := range l.all() {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

// newTestClient builds a polling client (no stream) against a fake server, closes it at cleanup and waits
// for its first load.
func newTestClient(t *testing.T, f *fakeServer, opts ...Option) (*Client, *errLog) {
	t.Helper()
	log := &errLog{}
	all := append([]Option{WithStream(false), WithOnError(log.add), WithFlushInterval(time.Hour)}, opts...)
	c := NewClient(f.url(), "fs_server_test_key", all...)
	t.Cleanup(c.Close)
	return c, log
}

func newTLSServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}
