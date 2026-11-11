package toggleslash

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorded is one request the fake server saw.
type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

// fakeServer is a loopback ToggleSlash server whose answers a test sets.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	reqs     []recorded
	ruleset  func(w http.ResponseWriter, r *http.Request)
	stream   func(w http.ResponseWriter, r *http.Request)
	events   func(w http.ResponseWriter, r *http.Request)
	rulesetN atomic.Int64
}

func rulesetBody(flags string) string {
	return `{"flags":[` + flags + `],"segments":[]}`
}

func flagJSON(key string, version int, variations string) string {
	return `{"key":"` + key + `","variations":` + variations + `,"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":` + itoa(version) + `}`
}

func itoa(n int) string { return strconv.Itoa(n) }

var boolFlag = flagJSON("f", 3, `[false,true]`)

func newFake(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.Header.Clone(), string(b)})
		f.mu.Unlock()
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			f.rulesetN.Add(1)
			if f.ruleset != nil {
				f.ruleset(w, r)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			_, _ = io.WriteString(w, rulesetBody(boolFlag))
		case "/sdk/v1/stream":
			if f.stream != nil {
				f.stream(w, r)
				return
			}
			w.WriteHeader(404)
		case "/sdk/v1/events":
			if f.events != nil {
				f.events(w, r)
				return
			}
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.Close)
	return f
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

// errs collects what OnError received.
type errs struct {
	mu   sync.Mutex
	list []string
}

func (e *errs) add(err error) {
	e.mu.Lock()
	e.list = append(e.list, err.Error())
	e.mu.Unlock()
}

func (e *errs) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.list...)
}

// eventually polls until cond holds or a generous bound passes.
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

// fakeClock is a clock a test drives. Sleep records the wait it was asked for and
// blocks until the test releases a sleeper of that length (or the context ends);
// releasing advances time by that wait. With auto set, sleeps return at once and
// advance time. A wait of zero returns at once, as the real clock does.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waits   []time.Duration
	waiters []*waiter
	auto    bool
}

type waiter struct {
	d  time.Duration
	ch chan struct{}
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
}

func (k *fakeClock) Now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now
}

func (k *fakeClock) set(t time.Time) {
	k.mu.Lock()
	k.now = t
	k.mu.Unlock()
}

func (k *fakeClock) Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	k.mu.Lock()
	k.waits = append(k.waits, d)
	if k.auto {
		k.now = k.now.Add(d)
		k.mu.Unlock()
		time.Sleep(time.Millisecond)
		return ctx.Err() == nil
	}
	w := &waiter{d: d, ch: make(chan struct{})}
	k.waiters = append(k.waiters, w)
	k.mu.Unlock()
	select {
	case <-w.ch:
		return true
	case <-ctx.Done():
		k.mu.Lock()
		for i, x := range k.waiters {
			if x == w {
				k.waiters = append(k.waiters[:i], k.waiters[i+1:]...)
				break
			}
		}
		k.mu.Unlock()
		return false
	}
}

func (k *fakeClock) waitList() []time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]time.Duration(nil), k.waits...)
}

// release waits for a sleeper of exactly d, then lets it finish and moves time on.
func (k *fakeClock) release(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		k.mu.Lock()
		for i, w := range k.waiters {
			if w.d == d {
				k.waiters = append(k.waiters[:i], k.waiters[i+1:]...)
				k.now = k.now.Add(d)
				k.mu.Unlock()
				close(w.ch)
				return
			}
		}
		k.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("nothing slept for %v (waits %v)", d, k.waitList())
}

// longest polls until the longest wait asked for equals want.
func (k *fakeClock) sawWait(want time.Duration) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range k.waitList() {
			if w == want {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func ctxFor(key string) Context { return NewContext(key) }

func closeAfter(t *testing.T, c *Client) {
	t.Helper()
	t.Cleanup(c.Close)
}
