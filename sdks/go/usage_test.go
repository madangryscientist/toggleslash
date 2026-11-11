package toggleslash

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func usageClient(t *testing.T, f *fakeServer, clk *fakeClock, opts ...Option) *Client {
	t.Helper()
	base := []Option{withClock(clk), WithStream(false), WithPollInterval(quietFlush), WithFlushInterval(quietFlush)}
	c := newTestClient(t, f, append(base, opts...)...)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	return c
}

// eventCount is the number of events in one /sdk/v1/events body.
func eventCount(t *testing.T, body string) int {
	t.Helper()
	root, err := jsonvalue.Parse([]byte(body))
	if err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, body)
	}
	events, _ := root.Get("events")
	arr, ok := events.AsArray()
	if !ok {
		t.Fatalf("no events array: %s", body)
	}
	return len(arr)
}

func TestUsageIsSummedPerFlagIndexAndDay(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := usageClient(t, f, clk)
	for i := 0; i < 3; i++ {
		c.BoolVariation("checkout", user, false)
	}
	c.BoolVariation("legacy", user, true)
	clk.Advance(13 * time.Hour) // past UTC midnight
	c.BoolVariation("checkout", user, false)
	c.flush()
	reqs := f.seen("events")
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	want := `{"events":[` +
		`{"flag":"checkout","variationIndex":1,"count":3,"day":"2026-11-11"},` +
		`{"flag":"legacy","variationIndex":0,"count":1,"day":"2026-11-11"},` +
		`{"flag":"checkout","variationIndex":1,"count":1,"day":"2026-11-12"}]}`
	if reqs[0].body != want {
		t.Errorf("body = %s\nwant   %s", reqs[0].body, want)
	}
	r := reqs[0]
	if r.method != "POST" || r.header.Get("Content-Type") != "application/json" || r.header.Get("Authorization") != "Bearer "+testKey {
		t.Errorf("request = %+v", r)
	}
}

func TestNothingPendingSendsNothing(t *testing.T) {
	f := newFakeServer(t)
	c := usageClient(t, f, newFakeClock())
	c.flush()
	c.BoolVariation("missing", user, false)
	c.flush()
	if n := len(f.seen("events")); n != 0 {
		t.Errorf("requests = %d", n)
	}
}

func TestTheFlushLoopSendsOnItsInterval(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := usageClient(t, f, clk, WithFlushInterval(10*time.Second))
	c.BoolVariation("checkout", user, false)
	eventually(t, 3*time.Second, "the flush timer", func() bool { return clk.Armed() >= 1 })
	clk.Advance(10 * time.Second)
	eventually(t, 3*time.Second, "the events request", func() bool { return len(f.seen("events")) == 1 })
}

func TestUsageContainsNothingAboutTheContext(t *testing.T) {
	f := newFakeServer(t)
	c := usageClient(t, f, newFakeClock())
	secret := jsonvalue.From(map[string]any{"key": "SENTINEL-KEY-91", "email": "SENTINEL-MAIL-91"})
	c.BoolVariation("checkout", secret, false)
	c.Close()
	for _, r := range f.all() {
		blob := r.path + r.body + fmt.Sprint(r.header)
		if strings.Contains(blob, "SENTINEL") {
			t.Errorf("%s %s carried context data: %s", r.method, r.path, blob)
		}
	}
	if len(f.seen("events")) != 1 {
		t.Errorf("events requests = %d", len(f.seen("events")))
	}
}

func TestBatchesOfAtMostFiveHundred(t *testing.T) {
	f := newFakeServer(t)
	c := usageClient(t, f, newFakeClock())
	for i := 0; i < 1200; i++ {
		c.record(fmt.Sprintf("f%04d", i), 0)
	}
	c.flush()
	reqs := f.seen("events")
	var sizes []int
	for _, r := range reqs {
		sizes = append(sizes, eventCount(t, r.body))
	}
	if fmt.Sprint(sizes) != "[500 500 200]" {
		t.Errorf("batch sizes = %v", sizes)
	}
	if c.pendingCount() != 0 {
		t.Errorf("pending = %d", c.pendingCount())
	}
}

func TestStatusesThatCanPassKeepTheCountsAndStopTheFlush(t *testing.T) {
	for _, status := range []int{500, 502, 503, 408, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFakeServer(t)
			f.script("events", answer{status: status})
			errs := &errorLog{}
			c := usageClient(t, f, newFakeClock(), WithOnError(errs.add))
			for i := 0; i < 600; i++ {
				c.record(fmt.Sprintf("f%04d", i), 0)
			}
			c.flush()
			if n := len(f.seen("events")); n != 1 {
				t.Errorf("requests = %d, want 1 (the flush stops)", n)
			}
			if c.pendingCount() != 600 {
				t.Errorf("pending = %d", c.pendingCount())
			}
			if !errs.has(fmt.Sprintf("http_%d", status)) {
				t.Errorf("errors = %v", errs.messages())
			}
			c.flush() // not held (no Retry-After): the next flush sends it all
			if c.pendingCount() != 0 {
				t.Errorf("pending after retry = %d", c.pendingCount())
			}
		})
	}
}

func TestStatusesTheServerRefusesAreDroppedAndTheFlushGoesOn(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 413, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFakeServer(t)
			f.script("events", answer{status: status})
			c := usageClient(t, f, newFakeClock())
			for i := 0; i < 1200; i++ {
				c.record(fmt.Sprintf("f%04d", i), 0)
			}
			c.flush()
			if n := len(f.seen("events")); n != 3 {
				t.Errorf("requests = %d, want 3", n)
			}
			if c.pendingCount() != 0 {
				t.Errorf("pending = %d (the refused batch came back)", c.pendingCount())
			}
		})
	}
}

func TestANetworkFailureKeepsTheCounts(t *testing.T) {
	f := newFakeServer(t)
	clk := newFakeClock()
	c := usageClient(t, f, clk)
	c.BoolVariation("checkout", user, false)
	f.close()
	c.flush()
	if c.pendingCount() != 1 {
		t.Errorf("pending = %d", c.pendingCount())
	}
}

func TestCountsMadeDuringAFailedSendAreMergedBack(t *testing.T) {
	f := newFakeServer(t)
	f.script("events", answer{status: 503})
	c := usageClient(t, f, newFakeClock())
	c.BoolVariation("checkout", user, false)
	c.flush()
	c.BoolVariation("checkout", user, false)
	c.flush()
	reqs := f.seen("events")
	if len(reqs) != 2 || !strings.Contains(reqs[1].body, `"count":2`) {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestRetryAfterOnEventsHoldsUsageNotTheRuleset(t *testing.T) {
	f := newFakeServer(t)
	f.script("events", answer{status: 429, header: map[string]string{"Retry-After": "60"}})
	clk := newFakeClock()
	c := usageClient(t, f, clk, WithPollInterval(10*time.Second))
	c.BoolVariation("checkout", user, false)
	c.flush()
	if len(f.seen("events")) != 1 {
		t.Fatal("no first request")
	}
	c.flush()
	if len(f.seen("events")) != 1 {
		t.Error("sent inside the hold")
	}
	// The ruleset poll is not held by it.
	eventually(t, 3*time.Second, "a ruleset wait", func() bool { return len(clk.waits()) >= 1 })
	if got := clk.waits()[0]; got != 10*time.Second {
		t.Errorf("poll wait = %s, the usage hold leaked into it", got)
	}
	clk.Advance(60 * time.Second)
	c.flush()
	if len(f.seen("events")) != 2 || c.pendingCount() != 0 {
		t.Errorf("after the hold: requests = %d, pending = %d", len(f.seen("events")), c.pendingCount())
	}
}

func TestCloseSendsTheLastCounts(t *testing.T) {
	f := newFakeServer(t)
	c := usageClient(t, f, newFakeClock())
	c.BoolVariation("checkout", user, false)
	c.Close()
	if n := len(f.seen("events")); n != 1 {
		t.Fatalf("events requests = %d", n)
	}
	// Nothing is counted afterwards, and evaluation still answers.
	if !c.BoolVariation("checkout", user, false) {
		t.Error("evaluation stopped after close")
	}
	c.Close()
	if n := len(f.seen("events")); n != 1 {
		t.Errorf("events requests after = %d", n)
	}
	if c.pendingCount() != 0 {
		t.Errorf("pending = %d", c.pendingCount())
	}
}

func TestCloseDoesNotWaitOutAHoldAndSaysWhatItLost(t *testing.T) {
	f := newFakeServer(t)
	f.script("events", answer{status: 429, header: map[string]string{"Retry-After": "120"}})
	errs := &errorLog{}
	c := usageClient(t, f, newFakeClock(), WithOnError(errs.add))
	c.BoolVariation("checkout", user, false)
	c.BoolVariation("legacy", user, false)
	c.flush() // the 429
	start := time.Now()
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Errorf("close took %s", time.Since(start))
	}
	if n := len(f.seen("events")); n != 1 {
		t.Errorf("events requests = %d, want only the 429", n)
	}
	if !errs.has("closed with 2 usage counts unsent: the server asked to wait") {
		t.Errorf("errors = %v", errs.messages())
	}
}

func TestAHungUsageFlushNeverBlocksEvaluationAndCloseIsBounded(t *testing.T) {
	f := newFakeServer(t)
	f.script("events", answer{hang: true})
	c := newTestClient(t, f, WithStream(false), WithFlushInterval(20*time.Millisecond), WithRequestTimeout(400*time.Millisecond))
	c.Ready()
	c.BoolVariation("checkout", user, false)
	eventually(t, 3*time.Second, "the flush to hang", func() bool { return len(f.seen("events")) == 1 })
	returnsWithin(t, 100*time.Millisecond, "variation during a hung flush", func() { c.BoolVariation("checkout", user, false) })
	start := time.Now()
	c.Close()
	if time.Since(start) > 2*400*time.Millisecond+500*time.Millisecond {
		t.Errorf("close took %s", time.Since(start))
	}
}

func TestRetryAfterOnEventsAbove300IsHeldFor300Seconds(t *testing.T) {
	f := newFakeServer(t)
	f.script("events", answer{status: 429, header: map[string]string{"Retry-After": "99999"}})
	clk := newFakeClock()
	c := usageClient(t, f, clk)
	c.BoolVariation("checkout", user, false)
	c.flush()
	clk.Advance(299 * time.Second)
	c.flush()
	if got := len(f.seen("events")); got != 1 {
		t.Fatalf("requests after 299 s = %d, want the hold still in force", got)
	}
	clk.Advance(2 * time.Second)
	c.flush()
	if got := len(f.seen("events")); got != 2 {
		t.Errorf("requests after 301 s = %d, want the hold over", got)
	}
}
