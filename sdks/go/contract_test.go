package toggleslash

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// evaluateMany runs n evaluations and fails the test if they take longer than bound.
func evaluateMany(t *testing.T, c *Client, n int, bound time.Duration) {
	t.Helper()
	start := time.Now()
	for i := 0; i < n; i++ {
		c.BoolVariation("a", user("u"), false)
		c.BoolVariationDetail("a", user("u"), false)
	}
	if took := time.Since(start); took > bound {
		t.Fatalf("%d evaluations took %v while a request hung", n, took)
	}
}

// Contract 5.2 and 6.2: an evaluation never waits on the network.
func TestEvaluationDoesNotWaitOnAHungRulesetRequest(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(5*time.Second))
	t.Cleanup(c.Close)
	waitFor(t, "ruleset request", func() bool { return len(s.requests("/sdk/v1/ruleset")) == 1 })
	evaluateMany(t, c, 200, time.Second)
	if d := c.BoolVariationDetail("a", user("u"), true); d.Reason.Code != ErrNotReady {
		t.Fatalf("detail = %+v", d)
	}
}

func TestEvaluationDoesNotWaitOnAHungUsageFlush(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c := newTestClient(t, s, WithStream(false), WithFlushInterval(20*time.Millisecond), WithRequestTimeout(600*time.Millisecond))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	waitFor(t, "events request", func() bool { return len(s.requests("/sdk/v1/events")) == 1 })
	evaluateMany(t, c, 200, time.Second)
}

// Contract 6.1: every operation, from many goroutines, with two Close calls and Ready racing the rest.
func TestEveryOperationFromManyGoroutines(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true), valueFlag("n", "3"), valueFlag("s", `"x"`))
	c := New(s.URL, testKey, WithPollInterval(5*time.Millisecond), WithFlushInterval(5*time.Millisecond),
		WithRequestTimeout(2*time.Second), WithAfterEvaluation(func(Event) {}), WithOnError(func(error) {}))
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				ctx := user("u")
				c.Ready()
				c.BoolVariation("a", ctx, false)
				c.StringVariation("s", ctx, "")
				c.Float64Variation("n", ctx, 0)
				c.IntVariationDetail("n", ctx, 0)
				c.JSONVariation("n", ctx, ValueOf(nil))
				c.Status()
				if j%100 == 0 {
					s.setFlags(boolFlag("a", j%200 == 0), valueFlag("n", "3"), valueFlag("s", `"x"`))
				}
			}
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); time.Sleep(20 * time.Millisecond); c.Close() }()
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a deadlock: the operations did not finish")
	}
}

// Contract 7.4: a Retry-After above 300 holds for 300 s on the ruleset and events routes (the stream's is in
// TestRetryAfterHoldsRulesetAndStream).
func TestRetryAfterCapOnRulesetAndEvents(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "99999")
		w.WriteHeader(429)
	})
	clk := newFakeClock()
	c := newTestClient(t, s, WithStream(false), withClock(clk), withRandom(func() float64 { return 0 }), WithPollInterval(time.Second))
	c.Ready()
	waitFor(t, "poll armed", func() bool { return clk.Armed() >= 2 })
	s.on("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "99999")
		w.WriteHeader(429)
	})
	clk.Advance(time.Second)
	waitFor(t, "ruleset 429", func() bool { return c.Status().LastError == "http_429" })
	if rem := c.holdRemaining(); rem <= 299*time.Second || rem > 300*time.Second {
		t.Fatalf("ruleset hold = %v", rem)
	}
	c.BoolVariation("a", user("u"), false)
	c.flush(false)
	c.holds.mu.Lock()
	rem := c.holds.usage.Sub(clk.Now())
	c.holds.mu.Unlock()
	if rem <= 299*time.Second || rem > 300*time.Second {
		t.Fatalf("usage hold = %v", rem)
	}
}

// Contract 6.5: a read from another goroutine never shows a stream failure's lastError beside connected.
func TestStatusNeverShowsAStreamFailureBesideConnected(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var n atomic.Int64
	s.on("/sdk/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1)%2 == 1 {
			w.WriteHeader(500)
			return
		}
		s.serveStream(w, r)
	})
	c := newTestClient(t, s, WithPollInterval(2*time.Millisecond))
	c.Ready()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.dropStreams()
			time.Sleep(3 * time.Millisecond)
		}
	}()
	var sawConnected, sawFailure bool
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		st := c.Status()
		if st.Stream == StreamConnected {
			sawConnected = true
			if st.LastError != LastErrorNone {
				t.Fatalf("status = %+v", st)
			}
		}
		if st.LastError != LastErrorNone {
			sawFailure = true
		}
	}
	close(stop)
	wg.Wait()
	if !sawConnected || !sawFailure {
		t.Fatalf("the test did not see both states (connected %v, failure %v)", sawConnected, sawFailure)
	}
}
