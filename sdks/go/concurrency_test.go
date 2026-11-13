package toggleslash

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// TestConcurrentOperations is the contract 6.1 race: every operation called
// from many goroutines, a close racing the readers and then two closes racing
// each other, under the race detector.
func TestConcurrentOperations(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	var errors atomic.Int64
	off := false
	client := newClient(t, Options{
		URL:            fake.url(),
		SDKKey:         "test-key",
		Stream:         &off,
		PollInterval:   5 * time.Millisecond,
		FlushInterval:  5 * time.Millisecond,
		RequestTimeout: 2 * time.Second,
		OnError:        func(error) { errors.Add(1) },
	})
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	stop := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				context := userContext("user-" + padInt(worker))
				client.BoolVariation("feature", context, false)
				client.StringVariationDetail("feature", context, "fallback")
				client.IntVariation("feature", context, 0)
				client.FloatVariationDetail("feature", context, 0)
				client.JSONVariation("feature", context, jsonval.Null{})
				_ = client.Status()
				_ = client.Ready(contextBackgroundWithTimeout(t))
			}
		}(i)
	}

	// A close racing the readers, while the loops are polling and flushing.
	time.Sleep(20 * time.Millisecond)
	var closers sync.WaitGroup
	for i := 0; i < 8; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			client.Close()
		}()
	}
	closers.Wait()
	close(stop)
	workers.Wait()

	if got := errors.Load(); got != 0 {
		t.Fatalf("onError was called %d times with a healthy server", got)
	}
	// Contract 11: evaluation still answers from the ruleset held.
	if !client.BoolVariation("feature", userContext("after-close"), false) {
		t.Fatalf("a closed client stopped answering from the ruleset it held")
	}
	if status := client.Status(); status.Stream != StreamClosed {
		t.Fatalf("stream = %q after close, want closed", status.Stream)
	}
}

func contextBackgroundWithTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// TestVariationNeverWaits is contract 5.2 and 6.2: evaluation returns within a
// bound while a ruleset request hangs, and while a usage flush hangs.
func TestVariationNeverWaits(t *testing.T) {
	t.Run("ruleset request hangs", func(t *testing.T) {
		fake := newFakeServer(t)
		release := make(chan struct{})
		fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		off := false
		client := newClient(t, Options{
			URL:            fake.url(),
			SDKKey:         "test-key",
			Stream:         &off,
			RequestTimeout: 10 * time.Second,
		})
		defer close(release)

		started := time.Now()
		detail := client.BoolVariationDetail("feature", userContext("u1"), true)
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("variation took %v while a ruleset request hung", elapsed)
		}
		if detail.Reason.Code != CodeNotReady {
			t.Fatalf("reason = %+v, want NOT_READY", detail.Reason)
		}
	})

	t.Run("usage flush hangs", func(t *testing.T) {
		fake := newFakeServer(t)
		fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
		}
		release := make(chan struct{})
		fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		off := false
		client := newClient(t, Options{
			URL:            fake.url(),
			SDKKey:         "test-key",
			Stream:         &off,
			RequestTimeout: 10 * time.Second,
		})
		defer close(release)
		if !client.Ready(context.Background()) {
			t.Fatalf("Ready = false, want true")
		}
		client.BoolVariation("feature", userContext("u1"), false)
		flushDone := make(chan struct{})
		go func() {
			defer close(flushDone)
			client.flush(false)
		}()
		time.Sleep(10 * time.Millisecond)

		started := time.Now()
		client.BoolVariation("feature", userContext("u2"), false)
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("variation took %v while a usage flush hung", elapsed)
		}
		client.Close()
		<-flushDone
	})
}

// TestSwapKeepsEtagAndVersionPaired is contract 6.3: a reader sees one ruleset
// or the other, so the hook's etag and flagVersion always belong to the same
// ruleset.
func TestSwapKeepsEtagAndVersionPaired(t *testing.T) {
	fake := newFakeServer(t)
	var requests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if requests.Add(1)%2 == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 2, 0))))
	}
	var violations atomic.Int64
	var pairsMu sync.Mutex
	pairs := map[string]int{}
	off := false
	clk := newFakeClock()
	opts := clockOptions(fake, clk)
	opts.Stream = &off
	opts.Hooks.AfterEvaluation = func(event Event) {
		expected := int64(1)
		if event.ETag == `"v2"` {
			expected = 2
		}
		if event.FlagVersion == nil || *event.FlagVersion != expected {
			violations.Add(1)
		}
		pairsMu.Lock()
		pairs[event.ETag]++
		pairsMu.Unlock()
	}
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					client.BoolVariation("feature", userContext("u1"), false)
				}
			}
		}()
	}
	for i := 0; i < 12; i++ {
		before := requests.Load()
		clk.advanceUntil(t, 3*time.Second, func() bool { return requests.Load() > before })
	}
	close(stop)
	readers.Wait()

	if got := violations.Load(); got != 0 {
		t.Fatalf("%d hook events paired an etag with another ruleset's version", got)
	}
	pairsMu.Lock()
	defer pairsMu.Unlock()
	if pairs[`"v1"`] == 0 || pairs[`"v2"`] == 0 {
		t.Fatalf("etags observed = %v, want both rulesets read", pairs)
	}
}

// TestCallbacksCallBackIntoTheClient is contract 6.4: a hook and an onError
// that call back into the client do not deadlock, and ready from a callback on
// the SDK's own goroutine returns at once.
func TestCallbacksCallBackIntoTheClient(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	done := make(chan string, 4)
	var inHook atomic.Bool
	var client *Client
	off := false
	opts := clockOptions(fake, newFakeClock())
	opts.clock = nil // a real flush timer, so the failing flush runs
	opts.Stream = &off
	opts.FlushInterval = 10 * time.Millisecond
	opts.Hooks.AfterEvaluation = func(Event) {
		if !inHook.CompareAndSwap(false, true) {
			return
		}
		defer inHook.Store(false)
		_ = client.Status()
		client.BoolVariation("feature", userContext("inner"), false)
		if client.Ready(context.Background()) != true {
			t.Errorf("Ready inside a hook = false")
		}
		done <- "hook"
	}
	opts.OnError = func(error) {
		_ = client.Status()
		if !client.Ready(context.Background()) {
			t.Errorf("Ready inside onError = false")
		}
		done <- "onError"
	}
	client = newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)

	want := map[string]bool{"hook": false, "onError": false}
	deadline := time.After(3 * time.Second)
	for !want["hook"] || !want["onError"] {
		select {
		case which := <-done:
			want[which] = true
		case <-deadline:
			t.Fatalf("callbacks that call back into the client deadlocked (seen %v)", want)
		}
	}
}

// TestCloseFromInsideOnError is contract 6.4's close case: a close called from
// inside onError does not wait for the goroutine that is running it.
func TestCloseFromInsideOnError(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	done := make(chan struct{}, 1)
	var client *Client
	off := false
	opts := clockOptions(fake, newFakeClock())
	opts.clock = nil // a real flush timer, so the failing flush runs
	opts.Stream = &off
	opts.FlushInterval = 10 * time.Millisecond
	opts.OnError = func(error) {
		client.Close()
		done <- struct{}{}
	}
	client = newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Close from inside onError deadlocked")
	}
}

// TestCloseBeforeReadySettles answers false, without waiting.
func TestCloseBeforeReadySettles(t *testing.T) {
	fake := newFakeServer(t)
	release := make(chan struct{})
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	off := false
	client := newClient(t, Options{
		URL:            fake.url(),
		SDKKey:         "test-key",
		Stream:         &off,
		RequestTimeout: 10 * time.Second,
	})
	defer close(release)
	client.Close()
	if client.Ready(context.Background()) {
		t.Fatalf("Ready = true on a client closed before its first load")
	}
	if status := client.Status(); status.Stream != StreamClosed {
		t.Fatalf("stream = %q after close", status.Stream)
	}
}

// TestBadRulesetNeverReplacesTheRuleset is contract 5, item 5, through the
// client: a bad gateway page and a truncated body are errors that keep the
// last good ruleset.
func TestBodyThatIsNotARulesetKeepsTheRuleset(t *testing.T) {
	fake := newFakeServer(t)
	var requests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			return
		}
		_, _ = w.Write([]byte(`<html>bad gateway</html>`))
	}
	clk := newFakeClock()
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	clk.advanceUntil(t, 3*time.Second, func() bool {
		status := client.Status()
		return status.LastError != nil && *status.LastError == LastErrorBadRuleset
	})
	if !client.BoolVariation("feature", userContext("u1"), false) {
		t.Fatalf("the last good ruleset stopped answering")
	}
}

// TestStatusNeverPairsAFailureWithConnected is contract 6.5 and the checklist's
// last "Code and documents" line: a reader on another goroutine samples status
// while stream attempts end in a timeout, and never sees that failure's
// lastError beside connected. The watchdog's own store sets both fields at
// once (client.go, streamFailed), and the connected event clears lastError in
// its own store; this test reads across both under the race detector.
func TestStatusNeverPairsAFailureWithConnected(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	quiet := make(chan struct{})
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Silent after connected: the watchdog ends this attempt as a timeout.
		select {
		case <-quiet:
		case <-r.Context().Done():
		}
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.clock = nil // a real watchdog, on a real loopback socket
	opts.Stream = &on
	opts.watchdog = 30 * time.Millisecond
	opts.PollInterval = 20 * time.Millisecond
	client := newClient(t, opts)
	defer close(quiet)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	var failures atomic.Int64
	var connects atomic.Int64
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			status := client.Status()
			if status.Stream == StreamConnected && status.LastError != nil && *status.LastError == LastErrorTimeout {
				t.Errorf("status showed the stream timeout beside connected")
				return
			}
			if status.Stream == StreamConnected {
				connects.Add(1)
			}
			if status.Stream == StreamDisconnected && status.LastError != nil && *status.LastError == LastErrorTimeout {
				failures.Add(1)
			}
		}
	}()

	eventually(t, 5*time.Second, func() bool {
		return connects.Load() >= 1 && failures.Load() >= 1 && fake.count("/sdk/v1/stream") >= 3
	}, "a reader sampling connected and a timeout across three attempts")
	close(stop)
	<-readerDone
}
