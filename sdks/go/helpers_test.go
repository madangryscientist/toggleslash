package toggleslash

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// recorded is one request the fake server received.
type recorded struct {
	route  string
	method string
	path   string
	header http.Header
	body   string
	at     time.Time
}

// answer is what the fake server does for one request on a route. The zero value is the route's normal answer.
type answer struct {
	status     int
	body       string
	header     map[string]string
	hang       bool // never answer
	hangBody   bool // send the headers, never finish the body
	sse        []string
	holdStream bool // keep the stream open after writing sse
	endStream  bool
}

// fakeServer plays the ToggleSlash server on a loopback listener and records every request.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	ruleset  string
	etag     string
	requests []recorded
	scripts  map[string][]answer
	streams  []chan string
	release  chan struct{}
}

const twoFlags = `{"flags":[
 {"key":"checkout","version":3,"on":true,"salt":"s","variations":[false,true],"offVariation":0,"fallthrough":{"variation":1},"rules":[],"targets":[],"prerequisites":[]},
 {"key":"legacy","version":1,"on":true,"salt":"s","variations":[false,true],"offVariation":0,"fallthrough":{"variation":0},"rules":[],"targets":[],"prerequisites":[]}
],"segments":[]}`

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{t: t, ruleset: twoFlags, etag: `"v1"`, scripts: map[string][]answer{}, release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.close)
	return f
}

func (f *fakeServer) close() {
	select {
	case <-f.release:
	default:
		close(f.release)
	}
	f.srv.CloseClientConnections()
	f.srv.Close()
}

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

// script queues answers for a route, used in order before the server answers normally.
func (f *fakeServer) script(route string, answers ...answer) {
	f.mu.Lock()
	f.scripts[route] = append(f.scripts[route], answers...)
	f.mu.Unlock()
}

// setRuleset replaces the ruleset served and its tag.
func (f *fakeServer) setRuleset(body, etag string) {
	f.mu.Lock()
	f.ruleset, f.etag = body, etag
	f.mu.Unlock()
}

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	route := routeOf(r.URL.Path)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{route: route, method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(body), at: time.Now()})
	var scripted *answer
	if queue := f.scripts[route]; len(queue) > 0 {
		a := queue[0]
		f.scripts[route] = queue[1:]
		scripted = &a
	}
	etag, ruleset := f.etag, f.ruleset
	f.mu.Unlock()

	if scripted != nil {
		if f.scripted(w, r, *scripted) {
			return
		}
	}
	switch route {
	case "ruleset":
		if etag != "" && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		_, _ = io.WriteString(w, ruleset)
	case "stream":
		f.serveStream(w, r, []string{"event: connected\ndata: " + etag + "\n\n"}, true)
	case "events":
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

// scripted runs a scripted answer; it reports whether the answer is complete.
func (f *fakeServer) scripted(w http.ResponseWriter, r *http.Request, a answer) bool {
	for k, v := range a.header {
		w.Header().Set(k, v)
	}
	switch {
	case a.hang:
		select {
		case <-f.release:
		case <-r.Context().Done():
		}
		return true
	case a.hangBody:
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-f.release:
		case <-r.Context().Done():
		}
		return true
	case a.sse != nil || a.holdStream || a.endStream:
		f.serveStream(w, r, a.sse, !a.endStream)
		return true
	case a.status != 0:
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
		return true
	case a.body != "":
		_, _ = io.WriteString(w, a.body)
		return true
	}
	return false
}

// serveStream writes chunks, then keeps the stream open (and forwards publish calls) until the client goes or the
// server closes.
func (f *fakeServer) serveStream(w http.ResponseWriter, r *http.Request, chunks []string, hold bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	for _, chunk := range chunks {
		_, _ = io.WriteString(w, chunk)
		flusher.Flush()
	}
	if !hold {
		return
	}
	feed := make(chan string, 16)
	f.mu.Lock()
	f.streams = append(f.streams, feed)
	f.mu.Unlock()
	for {
		select {
		case chunk := <-feed:
			_, _ = io.WriteString(w, chunk)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-f.release:
			return
		}
	}
}

// publish writes raw bytes to every open stream.
func (f *fakeServer) publish(chunk string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.streams {
		select {
		case s <- chunk:
		default:
		}
	}
}

// publishTag announces the tag the server holds now.
func (f *fakeServer) publishTag() {
	f.mu.Lock()
	tag := f.etag
	f.mu.Unlock()
	f.publish("event: changed\ndata: " + tag + "\n\n")
}

// seen returns the requests received on a route.
func (f *fakeServer) seen(route string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if r.route == route {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeServer) all() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

// eventually polls cond until it holds, failing the test after within. It never sleeps a fixed time.
func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cond() {
		return
	}
	t.Fatalf("timed out after %s waiting for %s", within, what)
}

// returnsWithin runs f and fails if it takes longer than within.
func returnsWithin(t *testing.T, within time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("%s did not return within %s", what, within)
	}
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

func (l *errorLog) has(substr string) bool {
	for _, m := range l.messages() {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

var user = jsonvalue.From(map[string]any{"key": "u1"})

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
	asked  []time.Duration // every wait asked of NewTimer, in order
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	ch    chan time.Time
	fn    func()
	live  bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
}

func (k *fakeClock) Now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now
}

func (k *fakeClock) arm(d time.Duration, fn func()) *clockTimer {
	t := &fakeTimer{clock: k, ch: make(chan time.Time, 1), fn: fn}
	k.mu.Lock()
	t.at, t.live = k.now.Add(d), true
	k.timers = append(k.timers, t)
	k.mu.Unlock()
	return &clockTimer{
		C: t.ch,
		stop: func() bool {
			k.mu.Lock()
			defer k.mu.Unlock()
			was := t.live
			t.live = false
			return was
		},
		reset: func(d time.Duration) bool {
			k.mu.Lock()
			defer k.mu.Unlock()
			was := t.live
			t.at, t.live = k.now.Add(d), true
			return was
		},
	}
}

func (k *fakeClock) NewTimer(d time.Duration) *clockTimer {
	handle := k.arm(d, nil)
	k.mu.Lock()
	k.asked = append(k.asked, d)
	k.mu.Unlock()
	return handle
}

func (k *fakeClock) AfterFunc(d time.Duration, f func()) *clockTimer {
	return k.arm(d, f)
}

// Armed is how many timers are waiting to fire.
func (k *fakeClock) Armed() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, t := range k.timers {
		if t.live {
			n++
		}
	}
	return n
}

// Advance moves time forward and fires every timer that falls due, earliest first.
func (k *fakeClock) Advance(d time.Duration) {
	k.mu.Lock()
	target := k.now.Add(d)
	k.mu.Unlock()
	for {
		k.mu.Lock()
		var due []*fakeTimer
		for _, t := range k.timers {
			if t.live && !t.at.After(target) {
				due = append(due, t)
			}
		}
		if len(due) == 0 {
			k.now = target
			k.mu.Unlock()
			return
		}
		sort.Slice(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
		first := due[0]
		first.live = false
		if first.at.After(k.now) {
			k.now = first.at
		}
		k.mu.Unlock()
		if first.fn != nil {
			go first.fn()
		} else {
			select {
			case first.ch <- target:
			default:
			}
		}
	}
}

// waits returns the durations asked of NewTimer so far, leaving out the flush loop's own (quietFlush).
func (k *fakeClock) waits() []time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []time.Duration
	for _, d := range k.asked {
		if d != quietFlush {
			out = append(out, d)
		}
	}
	return out
}
