package toggleslash

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// flagJSON is a flag that serves variations[fallthrough] for everyone.
func flagJSON(key string, version int, variations string, serve int) string {
	return fmt.Sprintf(`{"key":%q,"variations":%s,"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},"salt":"s","version":%d}`,
		key, variations, serve, version)
}

func rulesetJSON(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

// boolRuleset has one boolean flag, "checkout", that serves true.
func boolRuleset() string {
	return rulesetJSON(flagJSON("checkout", 1, "[false,true]", 1))
}

// clock is a settable clock for the now seam.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *clock { return &clock{t: t} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// serve starts a loopback server that is closed when the test ends.
func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// rulesetHandler answers GET /sdk/v1/ruleset with body and an ETag, honouring If-None-Match, and anything
// else with the fallback handler (or 404).
func rulesetHandler(body, etag string, other http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/ruleset" {
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		if other != nil {
			other.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

// newTestClient starts a client with the given option and seam changes, and closes it when the test ends.
func newTestClient(t *testing.T, url string, seams func(*config), opts ...Option) *Client {
	t.Helper()
	cfg := newConfig(url, "sdk-key-secret", opts...)
	if seams != nil {
		seams(&cfg)
	}
	c := start(cfg)
	t.Cleanup(c.Close)
	return c
}

// within fails the test if f does not return in d.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s", what, d)
	}
}

// eventually polls until cond holds.
func eventually(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// errorLog collects what OnError is told.
type errorLog struct {
	mu   sync.Mutex
	errs []*Error
}

func (l *errorLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var e *Error
	if errors.As(err, &e) {
		l.errs = append(l.errs, e)
	}
}

func (l *errorLog) all() []*Error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Error(nil), l.errs...)
}

func (l *errorLog) count() int { return len(l.all()) }
