package toggleslash

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func newClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	opts = append([]Option{WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour)}, opts...)
	c := New(f.URL, "sdk-key", opts...)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestReadyAndVariation(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	c := newClient(t, f)
	if !c.Ready() {
		t.Fatal("Ready is false after a good first load")
	}
	d := c.BoolVariationDetail("f", user("u"), false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("unexpected detail %+v", d)
	}
	if got := c.StringVariation("s", user("u"), "z"); got != "y" {
		t.Fatalf("string flag gave %q", got)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.Stream != StreamOff || st.LastError != "" {
		t.Fatalf("status %+v", st)
	}
}

func TestNotReadyAndMismatch(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	log := &errorLog{}
	c := newClient(t, f, WithOnError(log.add))
	if c.Ready() {
		t.Fatal("Ready is true with no ruleset")
	}
	d := c.BoolVariationDetail("f", user("u"), true)
	if !d.Value || d.VariationIndex != NoVariation || d.Reason.Code != ErrNotReady {
		t.Fatalf("detail %+v", d)
	}
	if got := c.Status().LastError; got != "http_503" {
		t.Fatalf("lastError %q", got)
	}
	if errs := log.all(); len(errs) != 1 || errs[0] != "toggleslash: ruleset request failed: http_503" {
		t.Fatalf("errors %v", errs)
	}

	g := newFakeServer(t)
	g.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	c2 := newClient(t, g)
	c2.Ready()
	d2 := c2.BoolVariationDetail("s", user("u"), false)
	if d2.Value || d2.VariationIndex != NoVariation || d2.Reason.Code != ErrTypeMismatch {
		t.Fatalf("mismatch detail %+v", d2)
	}
	if n := c2.pendingEntries(); n != 0 {
		t.Fatalf("a type mismatch was counted (%d entries)", n)
	}
	// a null fallback accepts any kind; JSON takes the served value
	d3 := c2.JSONVariationDetail("s", user("u"), Null())
	if s, _ := d3.Value.AsString(); s != "y" || d3.VariationIndex != 1 {
		t.Fatalf("json detail %+v", d3)
	}
}

func TestUnknownFlagAndInvalidContext(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	c := newClient(t, f)
	c.Ready()
	d := c.BoolVariationDetail("nope", user("u"), true)
	if !d.Value || d.Reason.Code != ErrFlagNotFound || d.VariationIndex != NoVariation {
		t.Fatalf("%+v", d)
	}
	d = c.BoolVariationDetail("f", Context{}, true)
	if !d.Value || d.Reason.Code != ErrInvalidContext {
		t.Fatalf("%+v", d)
	}
	if n := c.pendingEntries(); n != 0 {
		t.Fatalf("a fallback was counted (%d)", n)
	}
}

func TestConditionalRequestAndNotModified(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	c := newClient(t, f, WithPollInterval(20*time.Millisecond))
	c.Ready()
	eventually(t, "a conditional poll", func() bool { return f.count("/sdk/v1/ruleset") >= 3 })
	reqs := f.seen("/sdk/v1/ruleset")
	if reqs[0].header.Get("If-None-Match") != "" {
		t.Fatal("the first request was conditional")
	}
	if reqs[2].header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("a later request did not carry the tag: %q", reqs[2].header.Get("If-None-Match"))
	}
	if c.Status().LastError != "" {
		t.Fatalf("a 304 left an error: %q", c.Status().LastError)
	}
}

func TestNotModifiedWithNothingHeldIsAnError(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(304) })
	c := newClient(t, f)
	if c.Ready() {
		t.Fatal("ready on a 304 with nothing held")
	}
	if got := c.Status().LastError; got != "http_304" {
		t.Fatalf("lastError %q", got)
	}
}

func TestBadRulesetKeepsTheOneHeld(t *testing.T) {
	f := newFakeServer(t)
	var bad atomic.Bool
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v2"`)
		if bad.Load() {
			_, _ = w.Write([]byte(`{"flags":{},"segments":[]}`))
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(flagRuleset))
	})
	log := &errorLog{}
	c := newClient(t, f, WithPollInterval(15*time.Millisecond), WithOnError(log.add))
	c.Ready()
	bad.Store(true)
	eventually(t, "bad_ruleset", func() bool { return c.Status().LastError == "bad_ruleset" })
	if c.Status().ETag != `"v1"` || !c.BoolVariation("f", user("u"), false) {
		t.Fatal("the held ruleset was replaced by a bad one")
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	c := newClient(t, f)
	if c.Ready() {
		t.Fatal("ready after a redirect")
	}
	if f.count("/elsewhere") != 0 {
		t.Fatal("the redirect was followed")
	}
	if got := c.Status().LastError; got != "http_302" {
		t.Fatalf("lastError %q", got)
	}
}

func TestRequestTimeout(t *testing.T) {
	f := newFakeServer(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	c := newClient(t, f, WithRequestTimeout(80*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready on a hung server")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Ready took %v", time.Since(start))
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Fatalf("lastError %q", got)
	}
}

func TestNetworkError(t *testing.T) {
	f := newFakeServer(t)
	url := f.URL
	f.Close()
	c := New(url, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	if c.Ready() {
		t.Fatal("ready with no server")
	}
	if got := c.Status().LastError; got != SyncNetwork {
		t.Fatalf("lastError %q", got)
	}
}

func TestVariationDoesNotBlockOnAHungRuleset(t *testing.T) {
	f := newFakeServer(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	var first atomic.Bool
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		if first.CompareAndSwap(false, true) {
			staticRuleset(`"v1"`, flagRuleset)(w, r)
			return
		}
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	})
	c := newClient(t, f, WithPollInterval(10*time.Millisecond), WithRequestTimeout(5*time.Second))
	c.Ready()
	eventually(t, "a hung poll", func() bool { return f.count("/sdk/v1/ruleset") >= 2 })
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			c.BoolVariation("f", user("u"), false)
			c.Status()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("variation blocked while a ruleset request hung")
	}
}

func TestVariationDoesNotBlockOnAHungFlush(t *testing.T) {
	f := newFakeServer(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	})
	c := newClient(t, f, WithFlushInterval(10*time.Millisecond), WithRequestTimeout(400*time.Millisecond))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	eventually(t, "a hung flush", func() bool { return f.count("/sdk/v1/events") >= 1 })
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			c.BoolVariation("f", user("u"), false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("variation blocked while a usage flush hung")
	}
}

func TestSwapAtomicityOfEventETagAndVersion(t *testing.T) {
	// Two rulesets: tag "a" has f at version 1, tag "b" has f at version 2. Every event pairs them.
	var tag atomic.Value
	tag.Store(`"a"`)
	body := func() string {
		v := 1
		if tag.Load().(string) == `"b"` {
			v = 2
		}
		return strings.Replace(flagRuleset, `"version":3`, fmt.Sprintf(`"version":%d`, v), 1)
	}
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", rulesetHandler(func() string { return tag.Load().(string) }, body))
	var bad atomic.Int32
	hook := func(e Event) {
		if e.FlagVersion == nil {
			return
		}
		want := map[string]int64{`"a"`: 1, `"b"`: 2}[e.ETag]
		if *e.FlagVersion != want {
			bad.Add(1)
		}
	}
	c := newClient(t, f, WithPollInterval(2*time.Millisecond), WithAfterEvaluation(hook))
	c.Ready()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.BoolVariation("f", user("u"), false)
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			tag.Store(`"b"`)
		} else {
			tag.Store(`"a"`)
		}
		time.Sleep(3 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d events paired an etag with another ruleset's flag version", bad.Load())
	}
}

func TestIntVariationRange(t *testing.T) {
	body := `{"flags":[{"key":"n","version":1,"on":true,"salt":"a","variations":[9007199254740993,1.5,9223372036854775808,-9223372036854775808,3],
	 "offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0}}],"segments":[]}`
	for idx, want := range []struct {
		value int64
		code  ErrorCode
	}{
		{0, ""}, {7, ErrTypeMismatch}, {7, ErrTypeMismatch}, {-9223372036854775808, ""}, {3, ""},
	} {
		f := newFakeServer(t)
		b := strings.Replace(body, `"fallthrough":{"variation":0}`, fmt.Sprintf(`"fallthrough":{"variation":%d}`, idx), 1)
		f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, b))
		c := newClient(t, f)
		c.Ready()
		d := c.IntVariationDetail("n", user("u"), 7)
		if idx == 0 {
			// 2^53+1 reads as the nearest binary64, 2^53, which is an integer in range.
			if d.Value != 9007199254740992 || d.Reason.Kind == ReasonError {
				t.Fatalf("index 0: %+v", d)
			}
			continue
		}
		if want.code != "" {
			if d.Value != 7 || d.Reason.Code != want.code || d.VariationIndex != NoVariation {
				t.Fatalf("index %d: %+v", idx, d)
			}
		} else if d.Value != want.value || d.Reason.Kind == ReasonError {
			t.Fatalf("index %d: %+v", idx, d)
		}
	}
}

func TestEvaluationFailedGivesFallbackAndOneOnError(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	log := &errorLog{}
	c := newClient(t, f, WithOnError(log.add), withEvaluate(func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result {
		panic("boom with secret-context-key")
	}))
	c.Ready()
	d := c.BoolVariationDetail("f", user("secret-context-key"), true)
	if !d.Value || d.VariationIndex != NoVariation || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("%+v", d)
	}
	errs := log.all()
	if len(errs) != 1 {
		t.Fatalf("want one onError, got %v", errs)
	}
	if n := c.pendingEntries(); n != 0 {
		t.Fatalf("a failed evaluation was counted (%d)", n)
	}
}

func TestHooksAndCallbacksAreReentrant(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	var c *Client
	var events atomic.Int32
	var inner atomic.Bool
	c = newClient(t, f, WithAfterEvaluation(func(e Event) {
		if events.Add(1) == 1 {
			// the hook evaluates and reads status; this must not deadlock or recurse forever
			inner.Store(c.Status().ETag == `"v1"`)
			_ = c.Ready()
		}
	}))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	if !inner.Load() {
		t.Fatal("the hook did not see the status")
	}
}

func TestOnErrorMayCallClientMethods(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var self atomic.Pointer[Client]
	called := make(chan struct{}, 8)
	started := make(chan struct{})
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithOnError(func(error) {
		<-started
		c := self.Load()
		_ = c.Ready()
		_ = c.Status()
		_ = c.BoolVariation("f", user("u"), true)
		called <- struct{}{}
	}))
	self.Store(c)
	close(started)
	defer c.Close()
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("onError never ran, or deadlocked on Ready")
	}
}

func TestCallbackPanicsAreContained(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	c := newClient(t, f, WithAfterEvaluation(func(Event) { panic("hook") }), WithOnError(func(error) { panic("onError") }))
	c.Ready()
	if !c.BoolVariation("f", user("u"), false) {
		t.Fatal("a panicking hook changed the value")
	}
}

func TestCloseIsIdempotentAndRacing(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := newClient(t, f)
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Errorf("Close returned %v", err)
			}
			if c.Status().Stream != StreamClosed {
				t.Error("stream state is not closed after Close")
			}
		}()
	}
	wg.Wait()
	if n := f.count("/sdk/v1/events"); n != 1 {
		t.Fatalf("the final flush went out %d times", n)
	}
	// evaluation still answers from the ruleset held, and counts nothing
	if !c.BoolVariation("f", user("u"), false) || c.pendingEntries() != 0 {
		t.Fatal("after Close")
	}
}

func TestManyGoroutinesAllOperations(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := newClient(t, f, WithPollInterval(time.Millisecond), WithFlushInterval(time.Millisecond), WithAfterEvaluation(func(Event) {}))
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				switch (g + i) % 7 {
				case 0:
					c.BoolVariation("f", user("u"), false)
				case 1:
					c.StringVariationDetail("s", user("u"), "")
				case 2:
					c.IntVariation("f", user("u"), 1)
				case 3:
					c.JSONVariation("s", user("u"), Null())
				case 4:
					c.Status()
				case 5:
					c.FloatVariation("f", user("u"), 0)
				default:
					c.ReadyContext(context.Background())
				}
				if g == 0 && i == 150 {
					_ = c.Close()
				}
			}
		}(g)
	}
	wg.Wait()
	_ = c.Close()
}

func TestCloseRacingVariation(t *testing.T) {
	for i := 0; i < 20; i++ {
		f := newFakeServer(t)
		f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
		f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
		c := newClient(t, f)
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					c.BoolVariation("f", user("u"), false)
				}
			}()
		}
		_ = c.Close()
		wg.Wait()
	}
}

func TestCloseReturnsWithinTwiceTheTimeoutWhenTheServerHangs(t *testing.T) {
	f := newFakeServer(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	})
	c := newClient(t, f, WithRequestTimeout(150*time.Millisecond))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	start := time.Now()
	_ = c.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestCloseFromInsideACallbackDoesNotDeadlock(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var self atomic.Pointer[Client]
	started := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithOnError(func(error) {
		<-started
		_ = self.Load().Close()
		once.Do(func() { close(done) })
	}))
	self.Store(c)
	close(started)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from onError deadlocked")
	}
	_ = c.Close()
}

func TestHeadersSentOnEveryRoute(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"v1"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := newClient(t, f, WithPollInterval(15*time.Millisecond))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	eventually(t, "a conditional poll", func() bool { return f.count("/sdk/v1/ruleset") >= 2 })
	_ = c.Close()
	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Content-Length": true, "Accept-Encoding": true,
	}
	for _, path := range []string{"/sdk/v1/ruleset", "/sdk/v1/events"} {
		for _, r := range f.seen(path) {
			for name := range r.header {
				if !allowed[name] {
					t.Errorf("%s carried the header %s", path, name)
				}
			}
			if r.header.Get("Authorization") != "Bearer sdk-key" {
				t.Errorf("%s authorization %q", path, r.header.Get("Authorization"))
			}
			if r.header.Get("Accept-Encoding") != "" {
				t.Errorf("%s asked for compression", path)
			}
		}
	}
}
