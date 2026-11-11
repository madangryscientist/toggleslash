package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func eventsRequestsTotals(t *testing.T, f *fake) map[string]int {
	t.Helper()
	total := map[string]int{}
	for _, r := range f.routeRequests("/sdk/v1/events") {
		for k, n := range eventsOf(t, r.body) {
			total[k] += n
		}
	}
	return total
}

func TestUsageCountsOnlyServedResults(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(time.Hour),
		withClock(func() time.Time { return time.Date(2026, 11, 11, 23, 0, 0, 0, time.UTC) }))
	c.Variation("on", user, Bool(false)) // maybe not ready: never counted
	c.Ready()
	for i := 0; i < 3; i++ {
		c.Variation("on", user, Bool(false))
	}
	c.Variation("missing", user, Bool(false)) // no variation index: not counted
	c.StringVariation("on", user, "x")        // type mismatch: not counted
	c.Close()
	got := eventsRequestsTotals(t, f)
	if len(got) != 1 || got["on/1/2026-11-11"] != 3 {
		t.Errorf("sent = %v", got)
	}
	if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
		t.Errorf("requests = %d", n)
	}
}

func TestNothingPendingMeansNoRequest(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(10*time.Millisecond))
	c.Ready()
	time.Sleep(100 * time.Millisecond)
	c.Close()
	if n := len(f.routeRequests("/sdk/v1/events")); n != 0 {
		t.Errorf("requests = %d", n)
	}
}

func TestUsageSplitsAtUTCMidnight(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var mu sync.Mutex
	now := time.Date(2026, 11, 11, 23, 59, 59, 0, time.FixedZone("UTC+2", 2*3600)) // 21:59:59 UTC
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(time.Hour), withClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}))
	c.Ready()
	c.Variation("on", user, Bool(false))
	mu.Lock()
	now = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	mu.Unlock()
	c.Variation("on", user, Bool(false))
	c.Variation("on", user, Bool(false))
	c.Close()
	got := eventsRequestsTotals(t, f)
	if got["on/1/2026-11-11"] != 1 || got["on/1/2026-11-12"] != 2 || len(got) != 2 {
		t.Errorf("sent = %v", got)
	}
}

func TestUsageIsSentInBatchesOf500(t *testing.T) {
	var flags []string
	for i := 0; i < 1203; i++ {
		flags = append(flags, boolFlag(fmt.Sprintf("f%04d", i), true, 1))
	}
	f := newFake(t, flags...)
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(time.Hour))
	c.Ready()
	for i := 0; i < 1203; i++ {
		c.Variation(fmt.Sprintf("f%04d", i), user, Bool(false))
	}
	c.Close()
	reqs := f.routeRequests("/sdk/v1/events")
	if len(reqs) != 3 {
		t.Fatalf("requests = %d", len(reqs))
	}
	sizes := []int{}
	for _, r := range reqs {
		sizes = append(sizes, len(eventsOf(t, r.body)))
	}
	if sizes[0] != 500 || sizes[1] != 500 || sizes[2] != 203 {
		t.Errorf("batch sizes = %v", sizes)
	}
}

func TestFailedFlushKeepsTheCountsForTheNext(t *testing.T) {
	for _, status := range []int{500, 503, 408, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFake(t, boolFlag("on", true, 1))
			var mu sync.Mutex
			fail := true
			f.onEvents = func(w http.ResponseWriter, r *http.Request) bool {
				mu.Lock()
				defer mu.Unlock()
				if fail {
					w.WriteHeader(status)
					return true
				}
				return false
			}
			var log errorLog
			c := newClient(t, f.url(), WithStream(false), WithFlushInterval(20*time.Millisecond), WithOnError(log.add))
			c.Ready()
			c.Variation("on", user, Bool(false))
			c.Variation("on", user, Bool(false))
			waitFor(t, "a failed flush", 3*time.Second, func() bool { return log.count() >= 1 })
			if e := log.all()[0]; e.Source != SourceEvents || e.Code != fmt.Sprintf("http_%d", status) {
				t.Errorf("error = %+v", e)
			}
			mu.Lock()
			fail = false
			mu.Unlock()
			c.Variation("on", user, Bool(false))
			if status == 429 {
				c.Close() // no Retry-After: no hold, so the final flush sends everything
			} else {
				c.Close()
			}
			var sent int
			for _, r := range f.routeRequests("/sdk/v1/events") {
				for _, n := range eventsOf(t, r.body) {
					sent += n
				}
			}
			// Requests the server refused are in the log too; count only the last body.
			reqs := f.routeRequests("/sdk/v1/events")
			last := eventsOf(t, reqs[len(reqs)-1].body)
			_ = sent
			total := 0
			for _, n := range last {
				total += n
			}
			if total != 3 {
				t.Errorf("the accepted request carried %d counts, want 3 (kept plus new)", total)
			}
		})
	}
}

func TestFailedFlushOfOtherStatusDropsTheCounts(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onEvents = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	var log errorLog
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(time.Hour), WithOnError(log.add))
	c.Ready()
	c.Variation("on", user, Bool(false))
	c.Close()
	if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
		t.Errorf("requests = %d", n)
	}
	if log.count() != 1 || log.all()[0].Code != "http_400" {
		t.Errorf("errors = %v", log.all())
	}
	// A client whose counts were dropped sends nothing more.
	c2 := newClient(t, f.url(), WithStream(false), WithFlushInterval(20*time.Millisecond))
	c2.Ready()
	time.Sleep(80 * time.Millisecond)
	if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
		t.Errorf("a dropped count was sent again: %d requests", n)
	}
}

func TestCloseUnderAHoldReportsTheLostCounts(t *testing.T) {
	f := newFake(t, boolFlag("a", true, 1), boolFlag("b", true, 1))
	f.onEvents = tooMany("60")
	var log errorLog
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(20*time.Millisecond), WithOnError(log.add))
	c.Ready()
	c.Variation("a", user, Bool(false))
	waitFor(t, "the 429", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/events")) == 1 })
	waitFor(t, "the hold", 3*time.Second, func() bool { return holdLeft(c, "usage") > 0 })
	c.Variation("a", user, Bool(false))
	c.Variation("b", user, Bool(false))
	start := time.Now()
	c.Close()
	if took := time.Since(start); took > time.Second {
		t.Errorf("close waited out the hold: %v", took)
	}
	if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
		t.Errorf("close sent during the hold: %d requests", n)
	}
	var found bool
	for _, e := range log.all() {
		if e.Message == "closed with 2 usage counts unsent: the server asked to wait" {
			found = true
		}
	}
	if !found {
		t.Errorf("no loss report: %v", messages(log.all()))
	}
}

func messages(es []*Error) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Message)
	}
	return strings.Join(out, "; ")
}

func TestFlushIntervalStartsAfterTheFirstInterval(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithFlushInterval(300*time.Millisecond))
	c.Ready()
	c.Variation("on", user, Bool(false))
	time.Sleep(100 * time.Millisecond)
	if n := len(f.routeRequests("/sdk/v1/events")); n != 0 {
		t.Errorf("flushed after %d ms, before the interval", 100)
	}
	waitFor(t, "the first flush", 3*time.Second, func() bool { return len(f.routeRequests("/sdk/v1/events")) == 1 })
}
