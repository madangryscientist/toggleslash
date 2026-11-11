package toggleslash

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type usageBodyJSON struct {
	Events []struct {
		Flag           string `json:"flag"`
		VariationIndex int    `json:"variationIndex"`
		Count          int    `json:"count"`
		Day            string `json:"day"`
	} `json:"events"`
}

func decodeUsage(t *testing.T, body string) usageBodyJSON {
	t.Helper()
	var u usageBodyJSON
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return u
}

func (c *Client) pendingCount() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.pending)
}

func TestUsageBodyIsExact(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout), WithFlushInterval(100*time.Second))
	c.Ready()
	for range 3 {
		c.BoolVariation("f", user, false)
	}
	fc.fireNext(t, "the flush", exactly(100*time.Second))
	waitFor(t, "the events request", func() bool { return f.count("/sdk/v1/events") == 1 })
	want := `{"events":[{"flag":"f","variationIndex":1,"count":3,"day":"2026-01-01"}]}`
	if got := f.requests("/sdk/v1/events")[0].body; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestUsageNeverNamesTheContext(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false))
	c.Ready()
	c.BoolVariation("f", map[string]any{"key": "very-private-key", "attributes": map[string]any{"email": "a@b.c"}}, false)
	c.Close()
	for _, r := range f.requests("") {
		if strings.Contains(r.body, "very-private") || strings.Contains(r.body, "a@b.c") {
			t.Fatalf("%+v", r)
		}
	}
}

func TestFlushTimingFollowsTheEndOfTheLastFlush(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout), WithFlushInterval(100*time.Second))
	c.Ready()
	if f.count("/sdk/v1/events") != 0 {
		t.Fatal("flushed at once")
	}
	if countLive(fc, 100*time.Second) != 1 {
		t.Fatalf("live %v", fc.live())
	}
	c.BoolVariation("f", user, false)
	fc.fireNext(t, "first flush", exactly(100*time.Second))
	waitFor(t, "request", func() bool { return f.count("/sdk/v1/events") == 1 })
	// The next interval starts when the flush has finished.
	waitFor(t, "the interval re-armed", func() bool { return countArmed(fc, 100*time.Second) == 2 })
}

func TestUsageSplitsAtUTCMidnight(t *testing.T) {
	f := newFake(t)
	fc := newFakeClock(time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC))
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout), WithFlushInterval(time.Hour))
	c.Ready()
	c.BoolVariation("f", user, false)
	fc.advance(2 * time.Second) // 00:00:01 on April 1st; the fake clock fires nothing due before an hour
	c.BoolVariation("f", user, false)
	c.BoolVariation("f", user, false)
	c.Close()
	u := decodeUsage(t, f.requests("/sdk/v1/events")[0].body)
	if len(u.Events) != 2 || u.Events[0].Day != "2026-03-31" || u.Events[0].Count != 1 || u.Events[1].Day != "2026-04-01" || u.Events[1].Count != 2 {
		t.Fatalf("%+v", u)
	}
}

func TestUsageBatchesOf500(t *testing.T) {
	f := newFake(t)
	var sizes []int
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour))
	c.Ready()
	for i := range 1200 {
		c.recordUsage("flag-"+strconv.Itoa(i), 0)
	}
	c.Close()
	reqs := f.requests("/sdk/v1/events")
	for _, r := range reqs {
		sizes = append(sizes, len(decodeUsage(t, r.body).Events))
	}
	if len(sizes) != 3 || sizes[0] != 500 || sizes[1] != 500 || sizes[2] != 200 {
		t.Fatalf("%v", sizes)
	}
}

func TestUsageFailureKeepsTheRestAndStops(t *testing.T) {
	f := newFake(t)
	var calls atomic.Int32
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(202)
	})
	var log errorLog
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithOnError(log.add))
	c.Ready()
	for i := range 1200 {
		c.recordUsage("flag-"+strconv.Itoa(i), 0)
	}
	c.Close()
	if n := f.count("/sdk/v1/events"); n != 2 {
		t.Fatalf("%d requests", n)
	}
	if c.pendingCount() != 700 {
		t.Fatalf("%d kept", c.pendingCount())
	}
	if log.count() != 1 || !strings.Contains(log.text(), "http_500") {
		t.Fatalf("%q", log.text())
	}
}

func TestUsageRefusedBatchIsDroppedAndTheFlushContinues(t *testing.T) {
	f := newFake(t)
	var calls atomic.Int32
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(202)
	})
	var log errorLog
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithOnError(log.add))
	c.Ready()
	for i := range 1200 {
		c.recordUsage("flag-"+strconv.Itoa(i), 0)
	}
	c.Close()
	if n := f.count("/sdk/v1/events"); n != 3 {
		t.Fatalf("%d requests", n)
	}
	if c.pendingCount() != 0 || log.count() != 1 {
		t.Fatalf("pending %d, errors %q", c.pendingCount(), log.text())
	}
}

func TestUsageStatusDecisions(t *testing.T) {
	keep := []int{500, 502, 503, 504, 599, 408, 429}
	drop := []int{400, 401, 403, 404, 405, 409, 410, 413, 422, 451}
	for _, tc := range []struct {
		codes []int
		kept  bool
	}{{keep, true}, {drop, false}} {
		for _, code := range tc.codes {
			f := newFake(t)
			f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
			var log errorLog
			c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithOnError(log.add))
			c.Ready()
			c.recordUsage("f", 1)
			c.flush(nil)
			if (c.pendingCount() == 1) != tc.kept {
				t.Errorf("%d: kept = %v", code, c.pendingCount() == 1)
			}
			if log.count() != 1 || !strings.Contains(log.text(), "http_"+strconv.Itoa(code)) {
				t.Errorf("%d: onError %q", code, log.text())
			}
		}
	}
	for _, code := range []int{200, 201, 202, 204, 299} {
		f := newFake(t)
		f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		c := f.client(t, WithStream(false), WithFlushInterval(time.Hour))
		c.Ready()
		c.recordUsage("f", 1)
		c.flush(nil)
		if c.pendingCount() != 0 {
			t.Errorf("%d: not sent", code)
		}
	}
}

func TestUsageNetworkAndTimeoutKeepTheCounts(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	var log errorLog
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithRequestTimeout(200*time.Millisecond), WithOnError(log.add))
	c.Ready()
	c.recordUsage("f", 1)
	c.flush(nil)
	if c.pendingCount() != 1 || !strings.Contains(log.text(), "timeout") {
		t.Fatalf("pending %d, %q", c.pendingCount(), log.text())
	}
}

func TestUsageHoldAfter429(t *testing.T) {
	f := newFake(t)
	var status atomic.Int32
	status.Store(429)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) {
		if status.Load() == 429 {
			w.Header().Set("Retry-After", "100000")
		}
		w.WriteHeader(int(status.Load()))
	})
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout), WithFlushInterval(100*time.Second))
	c.Ready()
	c.recordUsage("f", 1)
	fc.fireNext(t, "flush at 100 s", exactly(100*time.Second)) // 429, held until 400 s (the cap)
	waitFor(t, "the 429", func() bool { return f.count("/sdk/v1/events") == 1 })
	waitFor(t, "re-armed", func() bool { return countArmed(fc, 100*time.Second) == 2 })
	status.Store(202)
	fc.fireNext(t, "flush at 200 s", exactly(100*time.Second))
	waitFor(t, "re-armed", func() bool { return countArmed(fc, 100*time.Second) == 3 })
	fc.fireNext(t, "flush at 300 s", exactly(100*time.Second))
	waitFor(t, "re-armed", func() bool { return countArmed(fc, 100*time.Second) == 4 })
	if n := f.count("/sdk/v1/events"); n != 1 {
		t.Fatalf("%d requests during the hold", n)
	}
	fc.fireNext(t, "flush at 400 s", exactly(100*time.Second))
	waitFor(t, "sent after the hold", func() bool { return f.count("/sdk/v1/events") == 2 })
	waitFor(t, "sent", func() bool { return c.pendingCount() == 0 })
}

func TestUsage429WithoutRetryAfterIsKeptWithNoHold(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour))
	c.Ready()
	c.recordUsage("f", 1)
	c.flush(nil)
	c.flush(nil)
	if f.count("/sdk/v1/events") != 2 || c.pendingCount() != 1 {
		t.Fatalf("%d requests, %d pending", f.count("/sdk/v1/events"), c.pendingCount())
	}
}

func TestUsageHoldIsSeparateFromTheRulesetHold(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	fc := newFakeClock(epoch)
	c := f.client(t, WithStream(false), withClock(fc), WithRequestTimeout(testTimeout))
	c.Ready()
	c.recordUsage("f", 1)
	c.flush(nil)
	if c.holdWait() > 0 {
		t.Fatal("an events 429 held the ruleset")
	}
}

func TestCloseReportsCountsHeldBack(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	var log errorLog
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithOnError(log.add))
	c.Ready()
	c.recordUsage("f", 1)
	c.recordUsage("g", 1)
	c.flush(nil) // held for 60 s now
	c.Close()
	if f.count("/sdk/v1/events") != 1 {
		t.Fatalf("Close waited out or ignored the hold: %d requests", f.count("/sdk/v1/events"))
	}
	if !strings.Contains(log.text(), "closed with 2 usage counts unsent: the server asked to wait") {
		t.Fatalf("%q", log.text())
	}
}

func TestNothingIsCountedAfterClose(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour))
	c.Ready()
	c.Close()
	if !c.BoolVariation("f", user, false) {
		t.Fatal("evaluation stopped answering after Close")
	}
	if c.pendingCount() != 0 {
		t.Fatal("counted after Close")
	}
	if f.count("/sdk/v1/events") != 0 {
		t.Fatal("sent after Close")
	}
}

func TestCloseSendsTheFinalFlush(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour))
	c.Ready()
	c.BoolVariation("f", user, false)
	c.Close()
	if f.count("/sdk/v1/events") != 1 {
		t.Fatalf("%d", f.count("/sdk/v1/events"))
	}
}
