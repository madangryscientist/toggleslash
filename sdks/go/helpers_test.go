package toggleslash

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock a test moves by hand. Timers fire when Advance passes them.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]struct{}
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	ch    chan time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start, timers: map[*fakeTimer]struct{}{}}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTimer(d time.Duration) timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{clock: f, at: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.timers[t] = struct{}{}
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	_, active := t.clock.timers[t]
	delete(t.clock.timers, t)
	return active
}

// Armed is how many timers are waiting.
func (f *fakeClock) Armed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// Advance moves the clock and fires every timer that is due.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	var due []*fakeTimer
	for t := range f.timers {
		if !t.at.After(f.now) {
			due = append(due, t)
			delete(f.timers, t)
		}
	}
	now := f.now
	f.mu.Unlock()
	for _, t := range due {
		t.ch <- now
	}
}

// waitArmed waits until at least n timers are waiting on the clock.
func (f *fakeClock) waitArmed(t *testing.T, n int) {
	t.Helper()
	eventually(t, "a timer to be armed", func() bool { return f.Armed() >= n })
}

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

// recorded is one request the fake server saw.
type recorded struct {
	method string
	path   string
	header http.Header
	body   string
}

// fakeServer is a ToggleSlash server on loopback that a test scripts per route.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
	handlers map[string]http.HandlerFunc
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{handlers: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(body)})
	h := f.handlers[r.URL.Path]
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
	f.handlers[path] = h
}

func (f *fakeServer) seen(path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if r.path == path {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeServer) count(path string) int { return len(f.seen(path)) }

// ruleset serves body with an ETag, and a 304 when If-None-Match matches.
func rulesetHandler(etag func() string, body func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := etag()
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", tag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body())
	}
}

func staticRuleset(etag, body string) http.HandlerFunc {
	return rulesetHandler(func() string { return etag }, func() string { return body })
}

// A ruleset with one boolean flag "f" that is on for everyone, and one string flag "s".
const flagRuleset = `{"flags":[
 {"key":"f","version":3,"on":true,"salt":"a","variations":[false,true],"offVariation":0,
  "targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1}},
 {"key":"s","version":1,"on":true,"salt":"b","variations":["x","y"],"offVariation":0,
  "targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1}}],
 "segments":[]}`

func user(key string) Context { return UserContext(key, nil) }

// eventsBody decodes a usage request's body into "flag:index:day=count" lines, sorted.
func eventsOf(t *testing.T, body string) []string {
	t.Helper()
	v, err := ParseValue([]byte(body))
	if err != nil {
		t.Fatalf("usage body is not JSON: %v", err)
	}
	list, _ := v.Field("events")
	items, _ := list.AsArray()
	var out []string
	for _, item := range items {
		flag, _ := item.Field("flag")
		idx, _ := item.Field("variationIndex")
		cnt, _ := item.Field("count")
		day, _ := item.Field("day")
		fs, _ := flag.AsString()
		ds, _ := day.AsString()
		i, _ := idx.AsNumber()
		n, _ := cnt.AsNumber()
		out = append(out, fs+":"+strconv.Itoa(int(i))+":"+ds+"="+strconv.Itoa(int(n)))
	}
	sort.Strings(out)
	return out
}

// errorLog collects what WithOnError receives.
type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, err)
}

func (l *errorLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.errs))
	for i, e := range l.errs {
		out[i] = e.Error()
	}
	return out
}

// newFakeTLS is a fakeServer over TLS with a certificate made for the test.
func newFakeTLS(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{handlers: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	f.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.StartTLS()
	t.Cleanup(f.Close)
	return f
}
