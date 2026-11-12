package toggleslash

import (
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func TestReadyAndEvaluate(t *testing.T) {
	s := newFakeServer(t, boolFlag("checkout", true))
	c := newTestClient(t, s, WithStream(false))
	if !c.Ready() {
		t.Fatal("Ready() = false")
	}
	d := c.BoolVariationDetail("checkout", user("u"), false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("detail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.LastError != LastErrorNone || st.Stream != StreamOff {
		t.Fatalf("status = %+v", st)
	}
}

func TestNotReadyBeforeFirstLoad(t *testing.T) {
	c := New("http://127.0.0.1:1", testKey, WithStream(false), WithRequestTimeout(time.Second))
	t.Cleanup(c.Close)
	d := c.StringVariationDetail("x", user("u"), "fb")
	if d.Value != "fb" || d.VariationIndex != NoVariation || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		// The refused connection may have settled already; NOT_READY is what a load that failed also gives.
		t.Fatalf("detail = %+v", d)
	}
	if c.Ready() {
		t.Fatal("Ready() = true with no server")
	}
	if got := c.Status().LastError; got != LastErrorNetwork {
		t.Fatalf("lastError = %q", got)
	}
}

func TestTypeMismatchAndFallbacks(t *testing.T) {
	s := newFakeServer(t,
		valueFlag("str", `"hello"`), valueFlag("num", `2.5`), valueFlag("whole", `2.0`),
		valueFlag("huge", `1e30`), valueFlag("nul", `null`), valueFlag("obj", `{"a":1}`))
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	ctx := user("u")

	if d := c.BoolVariationDetail("str", ctx, true); !d.Value || d.Reason.Code != ErrTypeMismatch || d.VariationIndex != NoVariation {
		t.Errorf("bool on string: %+v", d)
	}
	if d := c.IntVariationDetail("num", ctx, 9); d.Value != 9 || d.Reason.Code != ErrTypeMismatch {
		t.Errorf("int on 2.5: %+v", d)
	}
	if d := c.IntVariationDetail("whole", ctx, 9); d.Value != 2 || d.Reason.Kind != ReasonFallthrough {
		t.Errorf("int on 2.0: %+v", d)
	}
	if d := c.IntVariationDetail("huge", ctx, 9); d.Value != 9 || d.Reason.Code != ErrTypeMismatch {
		t.Errorf("int on 1e30: %+v", d)
	}
	if d := c.Float64VariationDetail("num", ctx, 0); d.Value != 2.5 {
		t.Errorf("float: %+v", d)
	}
	if d := c.StringVariationDetail("nul", ctx, "fb"); d.Value != "fb" || d.Reason.Code != ErrTypeMismatch {
		t.Errorf("string on null: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", ctx, jsonvalue.Null()); !d.Value.IsNull() || d.VariationIndex != 0 {
		t.Errorf("json null on null: %+v", d)
	}
	if d := c.JSONVariationDetail("obj", ctx, jsonvalue.Null()); !d.Value.IsObject() || d.VariationIndex != 0 {
		t.Errorf("json null fallback accepts any kind: %+v", d)
	}
	if d := c.JSONVariationDetail("obj", ctx, jsonvalue.String("x")); d.Reason.Code != ErrTypeMismatch {
		t.Errorf("json string fallback on object: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", ctx, jsonvalue.String("x")); d.Reason.Code != ErrTypeMismatch {
		t.Errorf("json string fallback on null: %+v", d)
	}
	if d := c.BoolVariationDetail("missing", ctx, true); !d.Value || d.Reason.Code != ErrFlagNotFound || d.VariationIndex != NoVariation {
		t.Errorf("missing: %+v", d)
	}
	if d := c.BoolVariationDetail("str", NewContext("", nil), true); d.Reason.Code != ErrInvalidContext {
		t.Errorf("invalid context: %+v", d)
	}
}

func TestIntRangeEdges(t *testing.T) {
	cases := []struct {
		f  float64
		ok bool
	}{
		{0, true}, {2, true}, {2.5, false}, {math.Exp2(63), false}, {-math.Exp2(63), true},
		{math.Exp2(63) - 1024, true}, {math.Inf(1), false}, {math.NaN(), false}, {1e30, false},
	}
	for _, tc := range cases {
		if got := isInt64(tc.f); got != tc.ok {
			t.Errorf("isInt64(%v) = %v", tc.f, got)
		}
	}
}

func TestUsageOnlyForServedValues(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true), valueFlag("s", `"x"`))
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	c.BoolVariation("a", user("v"), false)
	c.BoolVariation("s", user("u"), false) // mismatch: not counted
	c.BoolVariation("gone", user("u"), false)
	if n := c.use.pending(); n != 1 {
		t.Fatalf("pending entries = %d", n)
	}
	e := c.use.take()
	if len(e) != 1 || e[0].flag != "a" || e[0].count != 2 || e[0].variationIndex != 1 {
		t.Fatalf("entries = %+v", e)
	}
}

func TestEvaluatorPanicBecomesEvaluationFailed(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var log errorLog
	c := newTestClient(t, s, WithStream(false), WithOnError(log.add),
		withEvaluator(func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result { panic("boom") }))
	c.Ready()
	d := c.BoolVariationDetail("a", user("u"), true)
	if !d.Value || d.Reason.Code != ErrEvaluationFailed || d.VariationIndex != NoVariation {
		t.Fatalf("detail = %+v", d)
	}
	if log.count() != 1 || !strings.Contains(log.messages()[0], "boom") {
		t.Fatalf("errors = %v", log.messages())
	}
	if c.use.pending() != 0 {
		t.Fatal("a failed evaluation was counted")
	}
}

func TestHookEventAndPanic(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var mu sync.Mutex
	var events []Event
	var log errorLog
	c := newTestClient(t, s, WithStream(false), WithOnError(log.add), WithAfterEvaluation(func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		if e.FlagKey == "bad" {
			panic("hook failed")
		}
	}))
	c.Ready()
	ctx := user("u1")
	c.BoolVariation("a", ctx, false)
	c.BoolVariation("bad", ctx, false)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("%d events", len(events))
	}
	e := events[0]
	served, _ := e.Value.AsBool()
	if e.FlagKey != "a" || e.ETag != `"v1"` || e.FlagVersion != 1 || e.VariationIndex != 1 || !served {
		t.Fatalf("event = %+v", e)
	}
	if events[1].FlagVersion != NoVersion || events[1].Reason.Code != ErrFlagNotFound {
		t.Fatalf("event for a missing flag = %+v", events[1])
	}
	if log.count() != 1 || !strings.Contains(log.messages()[0], "hook failed") {
		t.Fatalf("errors = %v", log.messages())
	}
}

func TestHookBeforeReadyHasNoETag(t *testing.T) {
	var got Event
	c := New("http://127.0.0.1:1", testKey, WithStream(false), WithRequestTimeout(time.Second),
		WithAfterEvaluation(func(e Event) { got = e }))
	t.Cleanup(c.Close)
	c.BoolVariation("a", user("u"), true)
	if got.ETag != "" || got.FlagVersion != NoVersion || got.Reason.Code != ErrNotReady {
		t.Fatalf("event = %+v", got)
	}
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	s := newFakeServer(t)
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	done := make(chan struct{})
	var once sync.Once
	var cp atomic.Pointer[Client]
	// The first load can fail before New has returned, so the callback waits for the client to be stored.
	stored := make(chan struct{})
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second),
		WithOnError(func(error) {
			<-stored
			c := cp.Load()
			c.Ready()
			c.Status()
			c.Close()
			once.Do(func() { close(done) })
		}))
	cp.Store(c)
	close(stored)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from OnError deadlocked")
	}
	c.Close()
}

func TestHookMayCallBackIntoTheClient(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var cp atomic.Pointer[Client]
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second),
		WithAfterEvaluation(func(e Event) {
			c := cp.Load()
			c.Status()
			c.Ready()
			if e.FlagKey == "a" {
				c.BoolVariation("b", e.Context, false)
			}
		}))
	cp.Store(c)
	t.Cleanup(c.Close)
	c.Ready()
	done := make(chan struct{})
	go func() { c.BoolVariation("a", user("u"), false); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("hook calling the client deadlocked")
	}
}

func TestReadyIsBoundedAndFixed(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	release := make(chan struct{})
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(300*time.Millisecond))
	t.Cleanup(c.Close)
	start := time.Now()
	if c.Ready() {
		t.Fatal("Ready() true for a hung server")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Ready took %v", took)
	}
	if got := c.Status().LastError; got != LastErrorTimeout {
		t.Fatalf("lastError = %q", got)
	}
	close(release)
	if c.Ready() {
		t.Fatal("Ready() changed its answer")
	}
}

func TestCloseBeforeFirstLoadSettlesReadyFalse(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	var log errorLog
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(5*time.Second), WithOnError(log.add))
	waitFor(t, "ruleset request", func() bool { return len(s.requests("/sdk/v1/ruleset")) == 1 })
	start := time.Now()
	c.Close()
	c.Close()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close took %v", took)
	}
	if c.Ready() {
		t.Fatal("Ready() true after Close")
	}
	if st := c.Status(); st.Stream != StreamClosed || st.LastError != LastErrorNone {
		t.Fatalf("status = %+v", st)
	}
	if log.count() != 0 {
		t.Fatalf("an abandoned request reached OnError: %v", log.messages())
	}
	if d := c.BoolVariationDetail("a", user("u"), true); d.Reason.Code != ErrNotReady {
		t.Fatalf("detail = %+v", d)
	}
}

func TestConcurrentCloseAndEvaluate(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := New(s.URL, testKey, WithRequestTimeout(2*time.Second))
	c.Ready()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.BoolVariation("a", user("u"), false)
				c.Status()
			}
		}()
		go func() { defer wg.Done(); c.Close() }()
	}
	wg.Wait()
	if !c.BoolVariation("a", user("u"), false) {
		t.Fatal("evaluation stopped answering after Close")
	}
}

func TestSnapshotPairsETagAndVersion(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var mu sync.Mutex
	var seen []Event
	c := newTestClient(t, s, WithPollInterval(10*time.Millisecond), WithStream(true), WithAfterEvaluation(func(e Event) {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	}))
	c.Ready()
	stop := make(chan struct{})
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			s.setFlags(boolFlag("a", i%2 == 0))
			time.Sleep(time.Millisecond)
		}
	}()
	for i := 0; i < 2000; i++ {
		c.BoolVariation("a", user("u"), false)
	}
	close(stop)
	mu.Lock()
	defer mu.Unlock()
	for _, e := range seen {
		// Every ruleset the fake serves holds flag a at version 1, so a pairing break would show as a missing version.
		if e.ETag != "" && e.FlagVersion != 1 {
			t.Fatalf("etag %q paired with version %d", e.ETag, e.FlagVersion)
		}
	}
}
