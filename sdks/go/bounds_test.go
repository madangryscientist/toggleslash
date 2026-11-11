package toggleslash

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// TestEveryOperationFromManyGoroutines calls every operation of contract 2 from many goroutines at once while
// the ruleset swaps, and races two Close calls against them. Under -race it finds a data race; the deadline
// finds a deadlock. Each worker must make progress before Close and again after it.
func TestEveryOperationFromManyGoroutines(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(5*time.Millisecond))
	c.Ready()

	const workers = 12
	var wg sync.WaitGroup
	var stop atomic.Bool
	var progress [workers]atomic.Int64
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if !c.Ready() {
					t.Error("Ready changed its answer")
					return
				}
				c.Variation("a", user, jsonvalue.FromBool(false))
				c.VariationDetail("a", user, jsonvalue.FromBool(false))
				c.BoolVariationDetail("a", user, false)
				c.StringVariation("a", user, "fallback")
				c.Float64Variation("a", user, 0)
				c.Int64Variation("a", user, 0)
				c.Status()
				progress[w].Add(1)
			}
		}()
	}
	waitForProgress := func(what string, floor int64) {
		t.Helper()
		eventually(t, 5*time.Second, what, func() bool {
			for i := range progress {
				if progress[i].Load() < floor {
					return false
				}
			}
			return true
		})
	}
	waitForProgress("every worker to start", 5)
	for v := 2; v < 6; v++ {
		f.publish(boolFlag("a", v%2 == 0, v))
		eventually(t, 3*time.Second, "the swap", func() bool { return c.Status().ETag == fmt.Sprintf(`"v%d"`, v) })
	}
	var closers sync.WaitGroup
	for range 2 {
		closers.Add(1)
		go func() { defer closers.Done(); c.Close() }()
	}
	closers.Wait()
	var after [workers]int64
	for i := range progress {
		after[i] = progress[i].Load()
	}
	for i := range progress {
		floor := after[i] + 5
		eventually(t, 5*time.Second, "a worker to go on after Close", func() bool { return progress[i].Load() >= floor })
	}
	stop.Store(true)
	wg.Wait()
	if st := c.Status().Stream; st != StreamClosed {
		t.Fatalf("stream = %q after Close", st)
	}
}

// TestVariationIsBoundedWhileRequestsHang holds a ruleset poll and a usage flush open at the same time and
// checks that evaluation and status neither wait on them nor on each other (contract 5.2, 6.2).
func TestVariationIsBoundedWhileRequestsHang(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	var rulesetHangs, eventsHangs atomic.Int32
	counting := func(n *atomic.Int32) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) { n.Add(1); hang(w, r) }
	}
	c := newTestClient(t, f, WithPollInterval(20*time.Millisecond), WithFlushInterval(20*time.Millisecond), WithRequestTimeout(600*time.Millisecond))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	f.mu.Lock()
	f.rulesetFn = counting(&rulesetHangs)
	f.eventsFn = counting(&eventsHangs)
	f.mu.Unlock()
	for rulesetHangs.Load() == 0 || eventsHangs.Load() == 0 {
		c.BoolVariation("a", user, false)
		time.Sleep(5 * time.Millisecond)
		if t.Context().Err() != nil {
			t.Fatal("test ended")
		}
	}
	start := time.Now()
	for range 2000 {
		if !c.BoolVariation("a", user, false) {
			t.Fatal("the held ruleset stopped answering while a request hung")
		}
		c.VariationDetail("a", user, jsonvalue.FromBool(false))
		c.Status()
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("2000 evaluations took %v while a ruleset poll and a usage flush hung", took)
	}
}
