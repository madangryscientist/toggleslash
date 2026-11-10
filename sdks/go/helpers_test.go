package toggleslash

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "sdk-test-key"

// flagJSON is a flag that serves variation 1 (value on) when on, variation 0 (off) when off.
func flagJSON(key, offValue, onValue string, version int) string {
	return fmt.Sprintf(`{"key":%q,"on":true,"variations":[%s,%s],"offVariation":0,"salt":"s","version":%d,`+
		`"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1}}`, key, offValue, onValue, version)
}

func rulesetBody(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

var defaultRuleset = rulesetBody(flagJSON("checkout", "false", "true", 3), flagJSON("legacy", "true", "false", 1))

type recorded struct {
	route  string
	method string
	header http.Header
	body   string
}

// fakeServer is the ToggleSlash server as far as an SDK sees it: three routes, an ETag, a stream. A test
// replaces a route's handler to script an answer.
type fakeServer struct {
	t       *testing.T
	srv     *httptest.Server
	release chan struct{} // closed at cleanup so a hung handler returns
	relOnce sync.Once

	mu       sync.Mutex
	reqs     []recorded
	etag     string
	body     string
	handlers map[string]http.HandlerFunc
	streams  []chan string
	seq      int
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	return startFakeServer(t, httptest.NewServer)
}

func newTLSFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	return startFakeServer(t, httptest.NewTLSServer)
}

func startFakeServer(t *testing.T, start func(http.Handler) *httptest.Server) *fakeServer {
	f := &fakeServer{t: t, release: make(chan struct{}), etag: `"v1"`, body: defaultRuleset, handlers: map[string]http.HandlerFunc{}}
	f.srv = start(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.releaseAll()
		f.srv.Close()
	})
	return f
}

// releaseAll lets every hung handler return. It is safe to call more than once.
func (f *fakeServer) releaseAll() { f.relOnce.Do(func() { close(f.release) }) }

func (f *fakeServer) url() string { return f.srv.URL }

func routeOf(path string) string {
	switch path {
	case "/sdk/v1/ruleset":
		return "ruleset"
	case "/sdk/v1/stream":
		return "stream"
	case "/sdk/v1/events":
		return "events"
	}
	return "other"
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	route := routeOf(r.URL.Path)
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{route, r.Method, r.Header.Clone(), string(body)})
	h := f.handlers[route]
	f.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}
	switch route {
	case "ruleset":
		f.serveRuleset(w, r)
	case "stream":
		f.serveStream(w, r)
	case "events":
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeServer) serveRuleset(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	etag, body := f.etag, f.body
	f.mu.Unlock()
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

func (f *fakeServer) serveStream(w http.ResponseWriter, r *http.Request) {
	ch := make(chan string, 16)
	f.mu.Lock()
	f.streams = append(f.streams, ch)
	etag := f.etag
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := w.(http.Flusher)
	_, _ = fmt.Fprintf(w, "event: connected\ndata: %s\n\n", etag)
	fl.Flush()
	for {
		select {
		case chunk := <-ch:
			_, _ = io.WriteString(w, chunk)
			fl.Flush()
		case <-r.Context().Done():
			return
		case <-f.release:
			return
		}
	}
}

// set replaces the handler of a route; nil restores the default.
func (f *fakeServer) set(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h == nil {
		delete(f.handlers, route)
		return
	}
	f.handlers[route] = h
}

// hang answers a route never (until cleanup), after reading the request.
func (f *fakeServer) hang(route string) {
	f.set(route, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-f.release:
		}
	})
}

// publish changes the ruleset and tells every open stream.
func (f *fakeServer) publish(body string) string {
	f.mu.Lock()
	f.seq++
	f.etag = fmt.Sprintf(`"v%d"`, f.seq+1)
	f.body = body
	etag := f.etag
	streams := append([]chan string(nil), f.streams...)
	f.mu.Unlock()
	for _, ch := range streams {
		select {
		case ch <- "event: changed\ndata: " + etag + "\n\n":
		default:
		}
	}
	return etag
}

func (f *fakeServer) requests(route string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.reqs {
		if r.route == route {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeServer) all() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// eventually polls a condition until it holds or the time is up.
func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", within, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// within fails the test if fn does not return in time (a deadlock check).
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
		t.Fatalf("%s did not return within %v", what, d)
	}
}

// newClient makes a client against a fake server with short intervals, closed at cleanup.
func newClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	base := []Option{WithPollInterval(50 * time.Millisecond), WithRequestTimeout(2 * time.Second)}
	c := Create(f.url(), testKey, append(base, opts...)...)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func ctxOf(key string) Context {
	return Object(map[string]Value{"key": String(key)})
}

// errorLog collects onError calls.
type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	l.errs = append(l.errs, err)
	l.mu.Unlock()
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

// fakeClock is a clock that only moves when a fake sleep moves it.
type fakeClock struct {
	nanos  atomic.Int64
	mu     sync.Mutex
	sleeps []time.Duration
}

func newFakeClock(start time.Time) *fakeClock {
	c := &fakeClock{}
	c.nanos.Store(start.UnixNano())
	return c
}

func (c *fakeClock) now() time.Time { return time.Unix(0, c.nanos.Load()).UTC() }

func (c *fakeClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

// sleep records the wait, moves the clock by it and yields briefly so a loop does not spin.
func (c *fakeClock) sleep(ctx context.Context, d time.Duration) bool {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	c.advance(d)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(2 * time.Millisecond):
		return true
	}
}

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}
