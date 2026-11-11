package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// quiet is the options for a client that only needs its first load: no stream, and timers far away.
func quiet(extra ...Option) []Option {
	return append([]Option{
		WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(bigInterval),
		WithRequestTimeout(5 * time.Second),
	}, extra...)
}

func servingFlags(f *fakeServer, flags ...string) {
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"1"`)
		_, _ = fmt.Fprint(w, rulesetJSON(flags...))
	})
}

func TestAccessors(t *testing.T) {
	f := newFakeServer(t)
	servingFlags(f,
		flagJSON("b", 1, `[false,true]`, 1),
		flagJSON("s", 1, `["a","b"]`, 1),
		flagJSON("n", 1, `[1.5]`, 0),
		flagJSON("i", 1, `[2.0]`, 0),
		flagJSON("frac", 1, `[2.5]`, 0),
		flagJSON("j", 1, `[{"k":1}]`, 0),
	)
	c := newTestClient(t, f, quiet()...)
	if !c.Ready() {
		t.Fatal("Ready() = false")
	}
	u := user("u1")

	if d := c.BoolVariationDetail("b", u, false); !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 ||
		d.Reason.Kind != ReasonFallthrough {
		t.Errorf("bool detail = %+v", d)
	}
	if got := c.StringVariation("s", u, "z"); got != "b" {
		t.Errorf("string = %q", got)
	}
	if got := c.FloatVariation("n", u, 0); got != 1.5 {
		t.Errorf("float = %v", got)
	}
	if got := c.IntVariation("i", u, 9); got != 2 {
		t.Errorf("int of 2.0 = %v, want 2", got)
	}
	d := c.IntVariationDetail("frac", u, 9)
	if d.Value != 9 || d.VariationIndex != nil || d.Reason != (Reason{Kind: ReasonError, Code: ErrTypeMismatch}) {
		t.Errorf("int of 2.5 = %+v", d)
	}
	if got := c.JSONVariation("j", u, Null()); !jsonv.Equal(got, ValueOf(map[string]any{"k": 1})) {
		t.Errorf("json with a null fallback = %v", got)
	}
	if d := c.JSONVariationDetail("j", u, String("x")); d.Reason.Code != ErrTypeMismatch {
		t.Errorf("json against a string fallback = %+v", d)
	}
	if d := c.StringVariationDetail("b", u, "z"); d.Value != "z" || d.Reason.Code != ErrTypeMismatch {
		t.Errorf("string of a bool flag = %+v", d)
	}
	d2 := c.BoolVariationDetail("missing", u, true)
	if !d2.Value || d2.VariationIndex != nil || d2.Reason != (Reason{Kind: ReasonError, Code: ErrFlagNotFound}) {
		t.Errorf("unknown flag = %+v", d2)
	}
	if d := c.BoolVariationDetail("b", Number(1), false); d.Reason.Code != ErrInvalidContext {
		t.Errorf("invalid context = %+v", d)
	}
}

func TestHugeIntegerIsATypeMismatch(t *testing.T) {
	f := newFakeServer(t)
	servingFlags(f,
		flagJSON("huge", 1, `[1e19]`, 0),
		flagJSON("edge", 1, `[9223372036854774784]`, 0), // 2^63 - 1024, the largest a binary64 can serve
		flagJSON("over", 1, `[9223372036854775808]`, 0), // 2^63
		flagJSON("low", 1, `[-9223372036854775808]`, 0), // -2^63
	)
	c := newTestClient(t, f, quiet()...)
	c.Ready()
	u := user("u")
	if d := c.IntVariationDetail("huge", u, 7); d.Value != 7 || d.Reason.Code != ErrTypeMismatch {
		t.Errorf("1e19 = %+v", d)
	}
	if got := c.IntVariation("edge", u, 7); got != 9223372036854774784 {
		t.Errorf("2^63-1024 = %d", got)
	}
	if d := c.IntVariationDetail("over", u, 7); d.Reason.Code != ErrTypeMismatch {
		t.Errorf("2^63 = %+v", d)
	}
	if got := c.IntVariation("low", u, 7); got != -9223372036854775808 {
		t.Errorf("-2^63 = %d", got)
	}
}

func TestNotReadyBeforeTheFirstLoad(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", f.hang)
	var events []EvaluationEvent
	var mu sync.Mutex
	c := newTestClient(t, f, quiet(WithRequestTimeout(300*time.Millisecond), WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}))...)
	within(t, time.Second, "evaluating while the first load hangs", func() {
		d := c.BoolVariationDetail("f", user("u"), true)
		if !d.Value || d.Reason != (Reason{Kind: ReasonError, Code: ErrNotReady}) || d.VariationIndex != nil {
			t.Errorf("before the load: %+v", d)
		}
	})
	mu.Lock()
	if len(events) != 1 || events[0].ETag != "" || events[0].FlagVersion != nil || events[0].Reason.Code != ErrNotReady {
		t.Errorf("hook events before the load = %+v", events)
	}
	mu.Unlock()
	if c.Ready() {
		t.Error("Ready() = true after a load that timed out")
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Errorf("lastError = %q, want timeout", got)
	}
}

func TestReadyIsFixedAfterTheFirstAttempt(t *testing.T) {
	f := newFakeServer(t)
	var calls atomic.Int32
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, defaultRuleset(1))
	})
	clock := newFakeClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := newTestClient(t, f, quiet(withClock(clock), WithPollInterval(30*time.Second))...)
	if c.Ready() {
		t.Fatal("Ready() = true after a 503")
	}
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	eventually(t, "the poll to load a ruleset", func() bool { return c.Status().ETag != "" || c.BoolVariation("f", user("u"), false) })
	if c.Ready() {
		t.Error("Ready() turned true after a later load")
	}
	if !c.BoolVariation("f", user("u"), false) {
		t.Error("the later load was not used")
	}
}

func TestEvaluationFailedFromAPanickingEvaluator(t *testing.T) {
	f := newFakeServer(t)
	var errs errorLog
	var hooks atomic.Int32
	c := newTestClient(t, f, quiet(
		WithOnError(errs.add),
		WithAfterEvaluation(func(EvaluationEvent) { hooks.Add(1) }),
		withEvaluator(func(*eval.Ruleset, string, Context) eval.Result { panic("the evaluator raised") }),
	)...)
	c.Ready()
	d := c.BoolVariationDetail("f", user("u"), true)
	if !d.Value || d.VariationIndex != nil || d.Reason != (Reason{Kind: ReasonError, Code: ErrEvaluationFailed}) {
		t.Fatalf("detail = %+v", d)
	}
	if errs.count() != 1 {
		t.Errorf("OnError calls = %d, want 1", errs.count())
	}
	if hooks.Load() != 1 {
		t.Errorf("hook calls = %d, want 1", hooks.Load())
	}
	c.usageMu.Lock()
	pending := len(c.pending)
	c.usageMu.Unlock()
	if pending != 0 {
		t.Errorf("a failed evaluation was counted (%d pending)", pending)
	}
}

func TestHookPanicGoesToOnErrorAndChangesNothing(t *testing.T) {
	f := newFakeServer(t)
	var errs errorLog
	c := newTestClient(t, f, quiet(
		WithOnError(errs.add),
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook raised") }),
	)...)
	c.Ready()
	if !c.BoolVariation("f", user("u"), false) {
		t.Error("a panicking hook changed the answer")
	}
	c.usageMu.Lock()
	pending := len(c.pending)
	c.usageMu.Unlock()
	if pending != 1 || errs.count() != 1 {
		t.Errorf("pending = %d, OnError calls = %d, want 1 and 1", pending, errs.count())
	}
}

func TestOnErrorThatPanicsIsIgnored(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	c := newTestClient(t, f, quiet(WithOnError(func(error) { panic("onError raised") }))...)
	if c.Ready() {
		t.Error("Ready() = true after a 500")
	}
	if got := c.Status().LastError; got != "http_500" {
		t.Errorf("lastError = %q", got)
	}
}

// A hook's etag and flagVersion belong to the same ruleset while another goroutine swaps rulesets.
func TestHookETagAndFlagVersionStayPaired(t *testing.T) {
	f := newFakeServer(t)
	var bad atomic.Int32
	var seenEvents atomic.Int32
	c := newTestClient(t, f, quiet(WithAfterEvaluation(func(e EvaluationEvent) {
		seenEvents.Add(1)
		if e.FlagVersion == nil || e.ETag != fmt.Sprintf(`"%d"`, int(*e.FlagVersion)) {
			bad.Add(1)
		}
	}))...)
	c.Ready()
	snaps := make([]*snapshot, 40)
	for i := range snaps {
		parsed, err := ParseJSON([]byte(defaultRuleset(i + 1)))
		if err != nil {
			t.Fatal(err)
		}
		snaps[i] = &snapshot{ruleset: eval.Load(parsed), etag: fmt.Sprintf(`"%d"`, i+1)}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
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
	// Swap until the readers have really overlapped with the swaps: a fixed number of rounds can finish
	// before a loaded machine has scheduled one reader.
	eventually(t, "evaluations racing the swaps", func() bool {
		for round := range 200 {
			c.install(snaps[round%len(snaps)])
		}
		return seenEvents.Load() >= 2000
	})
	close(stop)
	wg.Wait()
	if bad.Load() != 0 {
		t.Errorf("%d of %d hook events had an etag that does not match their flagVersion", bad.Load(), seenEvents.Load())
	}
	if seenEvents.Load() == 0 {
		t.Error("no evaluations ran")
	}
}

// Every operation from many goroutines, Close racing them, and two Closes racing. Run it under -race.
func TestManyGoroutinesWithCloseRacing(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f,
		WithPollInterval(20*time.Millisecond), WithFlushInterval(10*time.Millisecond),
		WithRequestTimeout(2*time.Second), WithAfterEvaluation(func(EvaluationEvent) {}),
		WithOnError(func(error) {}))
	c.Ready()
	var wg sync.WaitGroup
	for g := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := user(fmt.Sprintf("u%d", g))
			for range 300 {
				c.BoolVariation("f", u, false)
				c.StringVariationDetail("f", u, "x")
				c.JSONVariation("f", u, Null())
				c.IntVariation("f", u, 1)
				c.Status()
				c.Ready()
			}
		}()
	}
	closers := make(chan struct{}, 3)
	for range 3 {
		go func() {
			time.Sleep(5 * time.Millisecond)
			c.Close()
			closers <- struct{}{}
		}()
	}
	within(t, 20*time.Second, "the goroutines", wg.Wait)
	for range 3 {
		select {
		case <-closers:
		case <-time.After(10 * time.Second):
			t.Fatal("a Close call never returned")
		}
	}
	if got := c.Status().Stream; got != StreamClosed {
		t.Errorf("stream = %q after Close", got)
	}
	// Evaluation still answers from the ruleset held after Close.
	if !c.BoolVariation("f", user("late"), false) {
		t.Error("evaluation stopped after Close")
	}
}

func TestTLSPath(t *testing.T) {
	f := newTLSFakeServer(t)
	c := newTestClient(t, f, quiet(WithHTTPClient(f.srv.Client()))...)
	if !c.Ready() {
		t.Fatalf("Ready() = false over TLS, lastError %q", c.Status().LastError)
	}
	if !strings.HasPrefix(f.url(), "https://") {
		t.Fatalf("test server url = %q", f.url())
	}
	if got := c.Status().ETag; got != `"1"` {
		t.Errorf("etag = %q", got)
	}
	// Without the test certificate authority the same server is refused, as a network error.
	plain := NewClient(f.url(), "k", quiet()...)
	defer plain.Close()
	if plain.Ready() || plain.Status().LastError != SyncNetwork {
		t.Errorf("an untrusted certificate: ready %v, lastError %q", plain.Ready(), plain.Status().LastError)
	}
}

func TestEvaluationNeverWaitsOnARequest(t *testing.T) {
	t.Run("a ruleset request that hangs", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/ruleset", f.hang)
		c := newTestClient(t, f, quiet(WithRequestTimeout(2*time.Second))...)
		f.await("/sdk/v1/ruleset", 1)
		within(t, 500*time.Millisecond, "variation, detail and status", func() {
			for range 200 {
				c.BoolVariation("f", user("u"), false)
				c.JSONVariationDetail("f", user("u"), Null())
				c.Status()
			}
		})
	})
	t.Run("a usage flush that hangs", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/events", f.hang)
		c := newTestClient(t, f, quiet(WithFlushInterval(20*time.Millisecond), WithRequestTimeout(2*time.Second))...)
		c.Ready()
		c.BoolVariation("f", user("u"), false)
		f.await("/sdk/v1/events", 1)
		within(t, 500*time.Millisecond, "variation while a flush hangs", func() {
			for range 200 {
				if !c.BoolVariation("f", user("u"), false) {
					t.Error("wrong answer")
				}
				c.Status()
			}
		})
	})
}

func TestCloseReturnsWithinAboutTwiceTheRequestTimeoutWhenRequestsHang(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", f.hang)
	f.handle("/sdk/v1/events", f.hang)
	timeout := 300 * time.Millisecond
	c := NewClient(f.url(), "k", quiet(WithRequestTimeout(timeout))...)
	f.await("/sdk/v1/ruleset", 1)
	c.record("f", 0)
	start := time.Now()
	within(t, 5*time.Second, "Close", c.Close)
	if took := time.Since(start); took > 4*timeout {
		t.Errorf("Close took %v with request timeout %v", took, timeout)
	}
	if c.Ready() {
		t.Error("Ready() = true for a client closed before its first load")
	}
}

func TestCallbacksThatCallBackIntoTheClient(t *testing.T) {
	t.Run("OnError on the sync goroutine calls Ready, Status, variation and Close", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
		var c *Client
		called := make(chan struct{})
		var once sync.Once
		ready := make(chan *Client, 1)
		c = NewClient(f.url(), "k", quiet(WithOnError(func(error) {
			once.Do(func() {
				cc := <-ready
				cc.Ready()
				cc.Status()
				cc.BoolVariation("f", user("u"), false)
				cc.Close()
				close(called)
			})
		}))...)
		ready <- c
		select {
		case <-called:
		case <-time.After(5 * time.Second):
			t.Fatal("OnError calling back into the client never returned")
		}
		within(t, 5*time.Second, "Close after the callback", c.Close)
	})
	t.Run("OnError during the final flush calls Close", func(t *testing.T) {
		f := newFakeServer(t)
		f.handle("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
		var c *Client
		var calls atomic.Int32
		c = NewClient(f.url(), "k", quiet(WithOnError(func(error) {
			calls.Add(1)
			c.Close()
		}))...)
		c.Ready()
		c.BoolVariation("f", user("u"), false)
		within(t, 5*time.Second, "Close with an OnError that calls Close", c.Close)
		if calls.Load() == 0 {
			t.Error("the final flush did not report its failure")
		}
	})
	t.Run("the hook calls Status and another variation", func(t *testing.T) {
		f := newFakeServer(t)
		var c *Client
		var inHook atomic.Bool
		var calls atomic.Int32
		c = NewClient(f.url(), "k", quiet(WithAfterEvaluation(func(EvaluationEvent) {
			if inHook.CompareAndSwap(false, true) {
				defer inHook.Store(false)
				calls.Add(1)
				c.Status()
				c.Ready()
				c.BoolVariation("f", user("inner"), false)
			}
		}))...)
		defer c.Close()
		c.Ready()
		within(t, 5*time.Second, "a hook that calls the client", func() { c.BoolVariation("f", user("u"), false) })
		if calls.Load() != 1 {
			t.Errorf("hook ran its body %d times", calls.Load())
		}
	})
}
