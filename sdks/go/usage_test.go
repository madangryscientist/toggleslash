package toggleslash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type eventsRequest struct {
	Events []struct {
		Flag           string `json:"flag"`
		VariationIndex int    `json:"variationIndex"`
		Count          int64  `json:"count"`
		Day            string `json:"day"`
	} `json:"events"`
}

func decodeEvents(t *testing.T, body string) eventsRequest {
	t.Helper()
	var r eventsRequest
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("events body %q: %v", body, err)
	}
	return r
}

func readyClient(t *testing.T, f *fakeServer, opts ...Option) (*Client, *errLog) {
	t.Helper()
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	c, log := newTestClient(t, f, opts...)
	if !c.Ready() {
		t.Fatalf("not ready: %+v", c.Status())
	}
	return c, log
}

func TestUsageCountsPerFlagIndexAndDay(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
	c, _ := readyClient(t, f)
	for range 3 {
		c.BoolVariation("flag", user, false)
	}
	c.BoolVariation("missing", user, false) // an error result is not counted
	c.flush(false)
	reqs := f.seen(pathEvents)
	if len(reqs) != 1 {
		t.Fatalf("%d events requests", len(reqs))
	}
	r := reqs[0]
	if r.method != http.MethodPost || r.header.Get("Content-Type") != "application/json" || r.header.Get("Authorization") != "Bearer fs_server_test_key" {
		t.Errorf("request = %+v", r)
	}
	got := decodeEvents(t, r.body)
	if len(got.Events) != 1 || got.Events[0].Flag != "flag" || got.Events[0].VariationIndex != 1 || got.Events[0].Count != 3 {
		t.Fatalf("events = %+v", got)
	}
	if want := time.Now().UTC().Format("2006-01-02"); got.Events[0].Day != want && got.Events[0].Day != time.Now().UTC().Add(-time.Second).Format("2006-01-02") {
		t.Errorf("day = %q", got.Events[0].Day)
	}
	// Nothing pending: another flush sends nothing.
	c.flush(false)
	if n := len(f.seen(pathEvents)); n != 1 {
		t.Fatalf("%d requests after an empty flush", n)
	}
}

func TestUsageSplitsAtUTCMidnight(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
	var now atomic.Pointer[time.Time]
	set := func(s string) {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		now.Store(&v)
	}
	// 23:59:59 at UTC-5 is already the next day in UTC; the day must be the UTC one.
	set("2026-03-09T23:59:59-05:00")
	c, _ := readyClient(t, f, withClock(func() time.Time { return *now.Load() }))
	c.BoolVariation("flag", user, false)
	set("2026-03-10T23:59:59Z")
	c.BoolVariation("flag", user, false)
	set("2026-03-11T00:00:00Z")
	c.BoolVariation("flag", user, false)
	c.flush(false)
	got := decodeEvents(t, f.seen(pathEvents)[0].body)
	days := map[string]int64{}
	for _, e := range got.Events {
		days[e.Day] += e.Count
	}
	if len(days) != 2 || days["2026-03-10"] != 2 || days["2026-03-11"] != 1 {
		t.Fatalf("days = %v", days)
	}
}

func TestUsageBatchesOf500(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	c, _ := readyClient(t, f)
	for i := range 1201 {
		c.record(fmt.Sprintf("f%04d", i), 0)
	}
	c.flush(false)
	reqs := f.seen(pathEvents)
	if len(reqs) != 3 {
		t.Fatalf("%d requests", len(reqs))
	}
	total := 0
	for i, r := range reqs {
		n := len(decodeEvents(t, r.body).Events)
		if n > 500 {
			t.Errorf("request %d carries %d", i, n)
		}
		total += n
	}
	if total != 1201 {
		t.Fatalf("sent %d", total)
	}
}

func TestUsageKeptOnRetryableAndDroppedOtherwise(t *testing.T) {
	for _, tc := range []struct {
		status int
		kept   bool
	}{{500, true}, {503, true}, {408, true}, {429, true}, {400, false}, {401, false}, {403, false}, {413, false}, {404, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			f := newFakeServer(t)
			var answer atomic.Int32
			answer.Store(int32(tc.status))
			f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(int(answer.Load())) })
			c, log := readyClient(t, f)
			c.BoolVariation("flag", user, false)
			c.flush(false)
			if !log.has(fmt.Sprintf("http_%d", tc.status)) {
				t.Fatalf("errors = %v", log.all())
			}
			if tc.status == 429 {
				return // held: covered by the Retry-After tests
			}
			answer.Store(200)
			c.flush(false)
			reqs := f.seen(pathEvents)
			if tc.kept && len(reqs) != 2 {
				t.Fatalf("kept batch was not resent: %d requests", len(reqs))
			}
			if !tc.kept && len(reqs) != 1 {
				t.Fatalf("dropped batch was resent: %d requests", len(reqs))
			}
		})
	}
}

func TestUsageKeptBatchStopsTheFlushAndDroppedOneContinues(t *testing.T) {
	f := newFakeServer(t)
	var n atomic.Int32
	status := []int{400, 500}
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) {
		i := int(n.Add(1)) - 1
		if i < len(status) {
			w.WriteHeader(status[i])
			return
		}
		w.WriteHeader(200)
	})
	c, log := readyClient(t, f)
	for i := range 1501 {
		c.record(fmt.Sprintf("f%04d", i), 0)
	}
	c.flush(false)
	// Chunk 1 is refused (dropped, the flush goes on), chunk 2 hits a 500 (kept, the flush stops).
	if got := len(f.seen(pathEvents)); got != 2 {
		t.Fatalf("%d requests", got)
	}
	if c.pendingUsage() != 1001 {
		t.Fatalf("pending %d", c.pendingUsage())
	}
	if log.count() != 2 {
		t.Fatalf("errors = %v", log.all())
	}
}

func TestUsageNetworkAndTimeoutFailuresAreKept(t *testing.T) {
	f := newFakeServer(t)
	var hang atomic.Bool
	hang.Store(true)
	f.handle(pathEvents, func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			f.hang(w, r)
			return
		}
		w.WriteHeader(200)
	})
	c, log := readyClient(t, f, WithRequestTimeout(100*time.Millisecond))
	c.BoolVariation("flag", user, false)
	c.flush(false)
	if !log.has("timeout") || c.pendingUsage() != 1 {
		t.Fatalf("errors %v, pending %d", log.all(), c.pendingUsage())
	}
	hang.Store(false)
	c.flush(false)
	if c.pendingUsage() != 0 {
		t.Fatal("not resent")
	}
}

func TestFlushIntervalAndFlushOnClose(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	log := &errLog{}
	c := NewClient(f.url(), "k", WithStream(false), WithOnError(log.add), WithFlushInterval(60*time.Millisecond), WithPollInterval(time.Hour))
	defer c.Close()
	c.Ready()
	c.BoolVariation("flag", user, false)
	waitFor(t, "the interval flush", func() bool { return len(f.seen(pathEvents)) == 1 })
	c.BoolVariation("flag", user, false)
	c.Close()
	if got := len(f.seen(pathEvents)); got < 2 {
		t.Fatalf("%d requests: Close did not flush", got)
	}
	// Close then count: nothing is counted after Close.
	c.BoolVariation("flag", user, false)
	if c.pendingUsage() != 0 {
		t.Fatal("counted after Close")
	}
}

func TestRetryAfterHoldsEventsAndIsCapped(t *testing.T) {
	f := newFakeServer(t)
	var after atomic.Value
	after.Store("99999")
	var calls atomic.Int32
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", after.Load().(string))
		w.WriteHeader(429)
	})
	var now atomic.Pointer[time.Time]
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	now.Store(&start)
	c, log := readyClient(t, f, withClock(func() time.Time { return *now.Load() }))
	c.BoolVariation("flag", user, false)
	c.flush(false)
	if rem := c.eventsHold.remaining(*now.Load()); rem != 300*time.Second {
		t.Fatalf("hold = %v, want 300s", rem)
	}
	// Held: no request goes out, and counts stay.
	c.flush(false)
	if calls.Load() != 1 {
		t.Fatalf("%d requests during a hold", calls.Load())
	}
	// The ruleset route is not held by an events 429.
	if c.syncHold.remaining(*now.Load()) > 0 {
		t.Fatal("events 429 held the ruleset route")
	}
	// Close does not wait it out and reports what it lost.
	c.Close()
	if !log.has("closed with 1 usage counts unsent: the server asked to wait") {
		t.Fatalf("errors = %v", log.all())
	}
	// After the hold passes, nothing is sent after Close either (the client is closed).
	later := start.Add(301 * time.Second)
	now.Store(&later)
	c.flush(false)
}

func TestRetryAfterValuesThatAreNotHolds(t *testing.T) {
	for _, v := range []string{"", "abc", "-5", "1.5", "1e3", "Wed, 21 Oct 2026 07:28:00 GMT", "+5", "0x10", "٣"} {
		t.Run(v, func(t *testing.T) {
			f := newFakeServer(t)
			f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) {
				if v != "" {
					w.Header().Set("Retry-After", v)
				}
				w.WriteHeader(429)
			})
			c, _ := readyClient(t, f)
			c.BoolVariation("flag", user, false)
			c.flush(false)
			if rem := c.eventsHold.remaining(time.Now()); rem > 0 {
				t.Fatalf("hold %v for %q", rem, v)
			}
			c.flush(false)
			if len(f.seen(pathEvents)) != 2 {
				t.Fatalf("not retried at the next flush for %q", v)
			}
		})
	}
}

func TestRetryAfterOnTheRulesetAndStreamRoutesIsOneSharedCappedHold(t *testing.T) {
	f := newFakeServer(t)
	var rulesetCalls, streamCalls atomic.Int32
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		if rulesetCalls.Add(1) == 1 {
			serveRuleset(rulesetJSON(1, 1), `"v1"`)(w, r)
			return
		}
		w.WriteHeader(500)
	})
	f.handle(pathStream, func(w http.ResponseWriter, _ *http.Request) {
		streamCalls.Add(1)
		w.Header().Set("Retry-After", "1000")
		w.WriteHeader(429)
	})
	log := &errLog{}
	var now atomic.Pointer[time.Time]
	start := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	now.Store(&start)
	c := NewClient(f.url(), "k", WithStream(true), WithOnError(log.add), WithPollInterval(time.Hour),
		WithFlushInterval(time.Hour), withClock(func() time.Time { return *now.Load() }))
	defer c.Close()
	c.Ready()
	waitFor(t, "the stream 429", func() bool { return log.has("http_429") })
	if rem := c.syncHold.remaining(*now.Load()); rem != 300*time.Second {
		t.Fatalf("hold = %v", rem)
	}
}

func TestFirstLoadIsNotHeldByRetryAfter(t *testing.T) {
	f := newFakeServer(t)
	var n atomic.Int32
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "250")
			w.WriteHeader(429)
			return
		}
		serveRuleset(rulesetJSON(1, 1), `"v1"`)(w, r)
	})
	c, log := newTestClient(t, f)
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready after a 429")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Ready waited %v", time.Since(start))
	}
	if !log.has("http_429") {
		t.Fatalf("errors %v", log.all())
	}
	if c.syncHold.remaining(time.Now()) <= 0 {
		t.Fatal("the 429 did not set a hold for later requests")
	}
}

func TestEvaluationDoesNotBlockOnAHungFlush(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, f.hang)
	c, _ := readyClient(t, f, WithRequestTimeout(300*time.Millisecond))
	c.BoolVariation("flag", user, false)
	done := make(chan struct{})
	go func() { c.flush(false); close(done) }()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	for range 100 {
		c.BoolVariation("flag", user, false)
		c.Status()
	}
	if time.Since(start) > time.Second {
		t.Fatalf("evaluation blocked for %v", time.Since(start))
	}
	<-done
}

func TestUsageNeverCarriesAContext(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	c, _ := readyClient(t, f)
	secret := NewContext("user", "alice-secret-key").With("email", StringValue("alice@example.com"))
	c.BoolVariation("flag", secret, false)
	c.flush(false)
	for _, r := range f.seen(pathEvents) {
		if strings.Contains(r.body, "alice") || strings.Contains(r.body, "example.com") {
			t.Fatalf("body carries the context: %s", r.body)
		}
	}
}
