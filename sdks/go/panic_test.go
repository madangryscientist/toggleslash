package toggleslash

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// closesWithin fails the test when Close does not return: the loops must end
// after a panic, so wg.Done runs exactly once.
func closesWithin(t *testing.T, client *Client, within time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		client.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("Close did not return within %v", within)
	}
}

// panicOnBuild makes newRuleset panic while the returned flag is set. The seam
// is installed once, before any client starts, because the sync goroutine reads
// it; the flag is the only thing a test changes afterwards.
func panicOnBuild(t *testing.T) *atomic.Bool {
	t.Helper()
	saved := newRuleset
	t.Cleanup(func() { newRuleset = saved })
	var armed atomic.Bool
	newRuleset = func(flags, segments jsonval.Value) *eval.Ruleset {
		if armed.Load() {
			panic("boom in the build")
		}
		return saved(flags, segments)
	}
	return &armed
}

func receiveError(t *testing.T, errs chan error) error {
	t.Helper()
	select {
	case err := <-errs:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("no onError call")
		return nil
	}
}

// TestRulesetBuildThatPanicsKeepsTheHeldRuleset is FS-178: a 200 whose build
// panics is a bad ruleset, the held one keeps answering, and the next good
// ruleset still applies.
func TestRulesetBuildThatPanicsKeepsTheHeldRuleset(t *testing.T) {
	armed := panicOnBuild(t)
	fake := newFakeServer(t)
	var version atomic.Int64
	version.Store(1)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		v := int(version.Load())
		w.Header().Set("ETag", `"v`+string(rune('0'+v))+`"`)
		// Version 3 flips the answer, so the swap is visible.
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", v, v/3))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	errs := make(chan error, 8)
	opts := clockOptions(fake, newFakeClock())
	opts.OnError = func(err error) { errs <- err }
	clk := opts.clock.(*fakeClock)
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	if client.BoolVariation("feature", userContext("u1"), true) {
		t.Fatalf("the first ruleset answers false")
	}

	armed.Store(true)
	version.Store(2)
	clk.advanceUntil(t, 3*time.Second, func() bool { return fake.count("/sdk/v1/ruleset") >= 2 })

	err := receiveError(t, errs)
	var built badRulesetPanic
	if !errors.Is(err, errBadRuleset) || !errors.As(err, &built) || built.cause != "boom in the build" {
		t.Fatalf("onError got %#v, want a bad ruleset carrying the panic value", err)
	}
	if client.BoolVariation("feature", userContext("u1"), true) {
		t.Fatalf("the held ruleset stopped answering after a build that panicked")
	}
	if got := client.Status(); got.ETag != `"v1"` || got.LastError == nil || *got.LastError != LastErrorBadRuleset {
		t.Fatalf("status = %+v, want the held tag and bad_ruleset", got)
	}

	// The loop is alive: the next good ruleset applies.
	armed.Store(false)
	version.Store(3)
	clk.advanceUntil(t, 3*time.Second, func() bool {
		return client.BoolVariation("feature", userContext("u1"), false)
	})
	closesWithin(t, client, 3*time.Second)
}

// TestSyncLoopThatPanicsGoesOn covers the last resort: a panic the ruleset
// parser did not see (here the transport's) is reported, the loop waits its
// backoff and runs again, and Close still completes.
func TestSyncLoopThatPanicsGoesOn(t *testing.T) {
	fake := newFakeServer(t)
	var version atomic.Int64
	version.Store(1)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		v := int(version.Load())
		w.Header().Set("ETag", `"v`+string(rune('0'+v))+`"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", v, v-1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	var requests atomic.Int64
	opts := clockOptions(fake, newFakeClock())
	opts.httpClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 2 {
			panic("boom in the transport")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	errs := make(chan error, 8)
	opts.OnError = func(err error) { errs <- err }
	clk := opts.clock.(*fakeClock)
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	clk.advanceUntil(t, 3*time.Second, func() bool { return requests.Load() >= 2 })
	err := receiveError(t, errs)
	var raised loopPanicError
	if !errors.As(err, &raised) || raised.loop != "sync" || raised.cause != "boom in the transport" {
		t.Fatalf("onError got %#v, want the sync loop's panic", err)
	}
	if client.BoolVariation("feature", userContext("u1"), true) {
		t.Fatalf("the held ruleset stopped answering after the loop panicked")
	}

	version.Store(2)
	clk.advanceUntil(t, 3*time.Second, func() bool {
		return client.BoolVariation("feature", userContext("u1"), false)
	})
	closesWithin(t, client, 3*time.Second)
}

// TestFirstLoadThatPanicsSettlesReady: a panic before the first load answered
// must not leave Ready waiting for ever.
func TestFirstLoadThatPanicsSettlesReady(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	var requests atomic.Int64
	opts := clockOptions(fake, newFakeClock())
	opts.httpClient = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			panic("boom in the first load")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	errs := make(chan error, 8)
	opts.OnError = func(err error) { errs <- err }
	client := newClient(t, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if client.Ready(ctx) {
		t.Fatalf("Ready = true after a first load that panicked")
	}
	if ctx.Err() != nil {
		t.Fatalf("Ready did not settle after a first load that panicked")
	}
	if got := client.Status(); got.LastError == nil || *got.LastError != LastErrorNetwork {
		t.Fatalf("status = %+v, want lastError set once Ready settled", got)
	}
	var raised loopPanicError
	if err := receiveError(t, errs); !errors.As(err, &raised) || raised.loop != "sync" {
		t.Fatalf("onError got %#v, want the sync loop's panic", err)
	}
	closesWithin(t, client, 3*time.Second)
}

// TestFlushThatPanicsIsReportedAndTheNextRuns is FS-178 for the flush loop.
func TestFlushThatPanicsIsReportedAndTheNextRuns(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	var probes atomic.Int64
	opts := clockOptions(fake, newFakeClock())
	opts.flushProbe = func() {
		if probes.Add(1) == 1 {
			panic("boom in the flush")
		}
	}
	errs := make(chan error, 8)
	opts.OnError = func(err error) { errs <- err }
	clk := opts.clock.(*fakeClock)
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)

	clk.advanceUntil(t, 3*time.Second, func() bool { return probes.Load() >= 1 })
	err := receiveError(t, errs)
	var raised loopPanicError
	if !errors.As(err, &raised) || raised.loop != "flush" || raised.cause != "boom in the flush" {
		t.Fatalf("onError got %#v, want the flush loop's panic", err)
	}

	clk.advanceUntil(t, 3*time.Second, func() bool { return fake.count("/sdk/v1/events") >= 1 })
	if probes.Load() < 2 {
		t.Fatalf("probes = %d, want a second flush after the panic", probes.Load())
	}
	closesWithin(t, client, 3*time.Second)
}

// TestCloseAfterAFlushPanicsInTheFinalFlush: the final flush is guarded too.
func TestCloseAfterAFlushPanicsInTheFinalFlush(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	opts := clockOptions(fake, newFakeClock())
	opts.flushProbe = func() { panic("boom in the final flush") }
	errs := make(chan error, 8)
	opts.OnError = func(err error) { errs <- err }
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)

	closesWithin(t, client, 3*time.Second)
	var raised loopPanicError
	if err := receiveError(t, errs); !errors.As(err, &raised) || raised.loop != "flush" {
		t.Fatalf("onError got %#v, want the flush loop's panic", err)
	}
}
