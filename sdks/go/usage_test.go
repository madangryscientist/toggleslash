package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func batches(t *testing.T, s *fakeServer) [][]jsonvalue.Value {
	t.Helper()
	var out [][]jsonvalue.Value
	for _, r := range s.requests("/sdk/v1/events") {
		doc := mustParse(t, string(r.Body))
		if doc.Len() != 1 {
			t.Fatalf("body has %d members: %s", doc.Len(), r.Body)
		}
		events, ok := doc.Members()[0].Value.AsArray()
		if doc.Members()[0].Key != "events" || !ok {
			t.Fatalf("body = %s", r.Body)
		}
		for _, e := range events {
			if e.Len() != 4 {
				t.Fatalf("entry = %s", e)
			}
		}
		out = append(out, events)
	}
	return out
}

func TestUsageFlushOnInterval(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	clk := newFakeClock()
	c := newTestClient(t, s, WithStream(false), withClock(clk), WithFlushInterval(30*time.Second), WithPollInterval(time.Hour))
	c.Ready()
	for i := 0; i < 3; i++ {
		c.BoolVariation("a", user("u"), false)
	}
	waitFor(t, "timers", func() bool { return clk.Armed() >= 2 })
	clk.Advance(30 * time.Second)
	waitFor(t, "events request", func() bool { return len(s.requests("/sdk/v1/events")) == 1 })
	r := s.requests("/sdk/v1/events")[0]
	if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+testKey || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %+v", r)
	}
	want := `{"events":[{"flag":"a","variationIndex":1,"count":3,"day":"2026-11-12"}]}`
	if string(r.Body) != want {
		t.Fatalf("body = %s", r.Body)
	}
}

func TestUsageSplitsAtTheUTCDay(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	clk := newFakeClock()
	clk.Advance(13*time.Hour + 59*time.Minute + 59*time.Second) // 23:59:59
	c := newTestClient(t, s, WithStream(false), withClock(clk), WithPollInterval(time.Hour))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	clk.Advance(time.Second)
	c.BoolVariation("a", user("u"), false)
	c.Close()
	b := batches(t, s)
	if len(b) != 1 || len(b[0]) != 2 {
		t.Fatalf("batches = %v", b)
	}
	days := []string{}
	for _, e := range b[0] {
		d, _ := e.Get("day")
		s, _ := d.AsString()
		days = append(days, s)
	}
	if days[0] != "2026-11-12" || days[1] != "2026-11-13" {
		t.Fatalf("days = %v", days)
	}
}

func TestUsageBatchesOf500(t *testing.T) {
	var fl []string
	for i := 0; i < 1203; i++ {
		fl = append(fl, boolFlag(fmt.Sprintf("f%d", i), true))
	}
	s := newFakeServer(t, fl...)
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	for i := 0; i < 1203; i++ {
		c.BoolVariation(fmt.Sprintf("f%d", i), user("u"), false)
	}
	c.Close()
	b := batches(t, s)
	if len(b) != 3 || len(b[0]) != 500 || len(b[1]) != 500 || len(b[2]) != 203 {
		sizes := []int{}
		for _, x := range b {
			sizes = append(sizes, len(x))
		}
		t.Fatalf("batch sizes = %v", sizes)
	}
}

func TestUsageFailureHandling(t *testing.T) {
	for _, tc := range []struct {
		status   int
		retained bool
	}{{500, true}, {503, true}, {408, true}, {429, true}, {400, false}, {401, false}, {413, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			s := newFakeServer(t, boolFlag("a", true))
			s.mu.Lock()
			s.eventsOK = tc.status
			s.mu.Unlock()
			var log errorLog
			c := newTestClient(t, s, WithStream(false), WithOnError(log.add))
			c.Ready()
			c.BoolVariation("a", user("u"), false)
			errs := c.flush(false)
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), fmt.Sprint(tc.status)) {
				t.Fatalf("errs = %v", errs)
			}
			if got := c.use.pending() == 1; got != tc.retained {
				t.Fatalf("retained = %v", got)
			}
		})
	}
}

func TestUsageStopsAtTheFirstRetryableBatch(t *testing.T) {
	var fl []string
	for i := 0; i < 600; i++ {
		fl = append(fl, boolFlag(fmt.Sprintf("f%d", i), true))
	}
	s := newFakeServer(t, fl...)
	var n atomic.Int32
	s.on("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(502)
		}
	})
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	for i := 0; i < 600; i++ {
		c.BoolVariation(fmt.Sprintf("f%d", i), user("u"), false)
	}
	c.flush(false)
	if n.Load() != 1 || c.use.pending() != 600 {
		t.Fatalf("requests = %d pending = %d", n.Load(), c.use.pending())
	}
}

func TestUsageContinuesPastADroppedBatch(t *testing.T) {
	var fl []string
	for i := 0; i < 600; i++ {
		fl = append(fl, boolFlag(fmt.Sprintf("f%d", i), true))
	}
	s := newFakeServer(t, fl...)
	var n atomic.Int32
	s.on("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(400)
		}
	})
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	for i := 0; i < 600; i++ {
		c.BoolVariation(fmt.Sprintf("f%d", i), user("u"), false)
	}
	c.flush(false)
	if n.Load() != 2 || c.use.pending() != 0 {
		t.Fatalf("requests = %d pending = %d", n.Load(), c.use.pending())
	}
}

func TestUsage429HoldAndFinalFlush(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	clk := newFakeClock()
	var log errorLog
	c := newTestClient(t, s, WithStream(false), withClock(clk), WithOnError(log.add), WithPollInterval(time.Hour))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	if errs := c.flush(false); len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	// During the hold a flush returns at once and sends nothing.
	if errs := c.flush(false); len(errs) != 0 || len(s.requests("/sdk/v1/events")) != 1 {
		t.Fatalf("flush during hold: %v, %d requests", errs, len(s.requests("/sdk/v1/events")))
	}
	c.Close()
	waitFor(t, "unsent error", func() bool { return log.count() >= 1 })
	last := log.messages()[log.count()-1]
	if last != "closed with 1 usage counts unsent: the server asked to wait" {
		t.Fatalf("last error = %q", last)
	}
	if len(s.requests("/sdk/v1/events")) != 1 {
		t.Fatal("the final flush waited out or ignored the hold")
	}
}

func TestUsage429WithoutHeaderIsKeptWithNoHold(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.mu.Lock()
	s.eventsOK = 429
	s.mu.Unlock()
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	c.flush(false)
	c.flush(false)
	if len(s.requests("/sdk/v1/events")) != 2 || c.use.pending() != 1 {
		t.Fatalf("requests = %d", len(s.requests("/sdk/v1/events")))
	}
}

func TestNothingCountedAfterClose(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	c.Close()
	c.BoolVariation("a", user("u"), false)
	if c.use.pending() != 0 {
		t.Fatal("counted after Close")
	}
}

func TestCloseFinalFlushIsBounded(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	s.on("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c := New(s.URL, testKey, WithStream(false), WithRequestTimeout(300*time.Millisecond))
	c.Ready()
	c.BoolVariation("a", user("u"), false)
	start := time.Now()
	c.Close()
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Close took %v", took)
	}
}
