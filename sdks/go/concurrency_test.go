package toggleslash

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSwapIsAtomic swaps between two rulesets as fast as the client polls, and checks that a hook
// never sees one ruleset's tag with the other's flag version (contract 6.3).
func TestSwapIsAtomic(t *testing.T) {
	var served atomic.Int64
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/ruleset" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		n := served.Add(1)
		etag, version := `"A"`, 1
		if n%2 == 0 {
			etag, version = `"B"`, 2
		}
		w.Header().Set("ETag", etag)
		_, _ = fmt.Fprint(w, rulesetJSON(boolFlagJSON("f", version)))
	})
	var bad, seen atomic.Int64
	c := newTestClient(t, f,
		WithStream(false), WithPollInterval(time.Millisecond), WithFlushInterval(time.Hour),
		WithAfterEvaluation(func(ev EvaluationEvent) {
			if ev.FlagVersion == nil {
				return
			}
			seen.Add(1)
			if (ev.ETag == `"A"`) != (*ev.FlagVersion == 1) {
				bad.Add(1)
			}
		}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !c.Ready(ctx) {
		t.Fatal("not ready")
	}
	var wg sync.WaitGroup
	stop := time.Now().Add(400 * time.Millisecond)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				c.BoolVariation("f", userCtx, false)
			}
		}()
	}
	wg.Wait()
	if served.Load() < 20 {
		t.Fatalf("only %d swaps happened; the test did not exercise the swap", served.Load())
	}
	if bad.Load() != 0 {
		t.Errorf("%d of %d events paired a tag with another ruleset's flag version", bad.Load(), seen.Load())
	}
}

// TestCallbacksMayCallBackIntoTheClient: an onError and a hook that call status, variation, ready and
// close do not deadlock, and ready from the SDK's own goroutine does not wait (contract 6.4).
func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	t.Run("onError runs on the sync goroutine", func(t *testing.T) {
		f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		var cp atomic.Pointer[Client]
		done := make(chan struct{})
		var once sync.Once
		c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour),
			WithOnError(func(error) {
				once.Do(func() {
					defer close(done)
					c := cp.Load()
					// Wait for the pointer: the first load can fail before New has returned.
					for c == nil {
						time.Sleep(time.Millisecond)
						c = cp.Load()
					}
					_ = c.Status()
					_ = c.BoolVariation("f", userCtx, false)
					start := time.Now()
					if c.Ready(context.Background()) {
						t.Error("Ready from inside the callback reported a ruleset")
					}
					if time.Since(start) > time.Second {
						t.Error("Ready from the SDK's own goroutine waited")
					}
					c.Close()
				})
			}))
		cp.Store(c)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the callback deadlocked")
		}
		within(t, 10*time.Second, "Close after a Close from a callback", c.Close)
		if st := c.Status(); st.Stream != StreamClosed {
			t.Errorf("stream = %q after close", st.Stream)
		}
	})

	t.Run("the hook runs on the caller", func(t *testing.T) {
		h := &ruleHolder{}
		h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
		var c *Client
		var inHook atomic.Bool
		reentered := make(chan Status, 1)
		cc, _ := readyClient(t, h, WithAfterEvaluation(func(EvaluationEvent) {
			if !inHook.CompareAndSwap(false, true) {
				return
			}
			_ = c.BoolVariation("f", userCtx, false)
			_ = c.Ready(context.Background())
			reentered <- c.Status()
		}))
		c = cc
		within(t, 5*time.Second, "an evaluation whose hook calls the client", func() {
			c.BoolVariation("f", userCtx, false)
		})
		select {
		case st := <-reentered:
			if !st.HasETag {
				t.Errorf("status inside the hook = %+v", st)
			}
		default:
			t.Error("the hook did not run")
		}
	})
}

// TestHookAndOnErrorFailuresAreContained: a hook that panics goes to onError, the caller gets the same
// answer, the evaluation is still counted, and an onError that panics is ignored.
func TestHookAndOnErrorFailuresAreContained(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	errs := &errorLog{}
	c, f := readyClient(t, h,
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook failed") }),
		WithOnError(func(err error) { errs.add(err); panic("onError failed") }),
	)
	d := c.BoolVariationDetail("f", userCtx, false)
	if !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 {
		t.Errorf("a failing hook changed the answer: %+v", d)
	}
	if errs.count() != 1 {
		t.Errorf("onError calls = %d", errs.count())
	}
	c.Flush(context.Background())
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("events requests = %d", len(reqs))
	}
	if want := `{"events":[{"flag":"f","variationIndex":1,"count":1,"day":"` + c.now().UTC().Format("2006-01-02") + `"}]}`; reqs[0].body != want {
		t.Errorf("body = %s\nwant   %s", reqs[0].body, want)
	}
}

// TestEvaluationDoesNotWaitOnAHungFlush: variation returns within a bound while a usage flush hangs
// (contract 5.2), and Close is bounded by the request timeout.
func TestEvaluationDoesNotWaitOnAHungFlush(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/events" {
			f.hang()
			return
		}
		h.serve(f, w, r)
	})
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour),
		WithFlushInterval(30*time.Millisecond), WithRequestTimeout(500*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !c.Ready(ctx) {
		t.Fatal("not ready")
	}
	c.BoolVariation("f", userCtx, false)
	eventually(t, "the flush to reach the server", func() bool {
		return len(f.requests("/sdk/v1/events")) > 0
	})
	within(t, 500*time.Millisecond, "5000 evaluations during a hung flush", func() {
		for range 5000 {
			c.BoolVariationDetail("f", userCtx, false)
		}
		_ = c.Status()
	})
	start := time.Now()
	within(t, 3*time.Second, "Close with a hung usage request", c.Close)
	if took := time.Since(start); took > 2*500*time.Millisecond+500*time.Millisecond {
		t.Errorf("Close took %v with a 500ms request timeout", took)
	}
}

// TestEveryOperationFromManyGoroutines runs every operation at once, with Close racing the
// evaluations and two Close calls racing each other. It is meant for the race detector.
func TestEveryOperationFromManyGoroutines(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(
		boolFlagJSON("b", 1), valueFlagJSON("s", `"x"`), valueFlagJSON("i", `5`),
		valueFlagJSON("f", `2.5`), valueFlagJSON("j", `[1,2]`),
	))
	f := newFake(t, h.serve)
	var errs errorLog
	var events atomic.Int64
	c := New(f.URL, "k", WithPollInterval(5*time.Millisecond), WithFlushInterval(5*time.Millisecond),
		WithRequestTimeout(2*time.Second), WithOnError(errs.add),
		WithAfterEvaluation(func(EvaluationEvent) { events.Add(1) }))
	defer c.Close()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				switch (g + i) % 8 {
				case 0:
					c.BoolVariation("b", userCtx, false)
				case 1:
					c.StringVariationDetail("s", userCtx, "")
				case 2:
					c.IntVariation("i", userCtx, 0)
				case 3:
					c.FloatVariationDetail("f", userCtx, 0)
				case 4:
					c.JSONVariation("j", userCtx, NewNull())
				case 5:
					_ = c.Status()
				case 6:
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
					c.Ready(ctx)
					cancel()
				case 7:
					c.Flush(context.Background())
				}
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	var closers sync.WaitGroup
	for range 3 {
		closers.Add(1)
		go func() { defer closers.Done(); c.Close() }()
	}
	within(t, 10*time.Second, "three racing Close calls", closers.Wait)
	time.Sleep(50 * time.Millisecond) // evaluations go on after Close
	close(stop)
	within(t, 10*time.Second, "the workers", wg.Wait)

	if events.Load() == 0 {
		t.Error("no evaluation ran")
	}
	if got := c.BoolVariation("b", userCtx, false); !got {
		t.Error("evaluation after Close should answer from the ruleset held")
	}
	if st := c.Status(); st.Stream != StreamClosed {
		t.Errorf("stream = %q", st.Stream)
	}
	for _, e := range errs.all() {
		t.Errorf("unexpected error: %s", e)
	}
}

// TestNothingIsCountedAfterClose and the final flush.
func TestCloseSendsTheCountsAndStopsCounting(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("f", 1)))
	c, f := readyClient(t, h)
	for range 3 {
		c.BoolVariation("f", userCtx, false)
	}
	c.Close()
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("events requests after Close = %d", len(reqs))
	}
	c.BoolVariation("f", userCtx, false)
	c.Close()
	if n := len(f.requests("/sdk/v1/events")); n != 1 {
		t.Errorf("a second Close or a later evaluation sent counts: %d requests", n)
	}
	if !c.Ready(context.Background()) {
		t.Error("Ready after Close should keep its answer")
	}
}

func TestGoroutineIDParses(t *testing.T) {
	if goroutineID() == 0 {
		t.Fatal("goroutineID could not read the runtime's stack header; the SDK would treat every goroutine as foreign")
	}
	other := make(chan uint64)
	go func() { other <- goroutineID() }()
	if got := <-other; got == 0 || got == goroutineID() {
		t.Errorf("two goroutines gave the same id %d", got)
	}
}
