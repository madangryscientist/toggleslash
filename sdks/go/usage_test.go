package toggleslash

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

type eventsBody struct {
	Events []struct {
		Flag           string `json:"flag"`
		VariationIndex int    `json:"variationIndex"`
		Count          int    `json:"count"`
		Day            string `json:"day"`
	} `json:"events"`
}

func decodeEvents(t *testing.T, r recorded) eventsBody {
	t.Helper()
	var b eventsBody
	if err := json.Unmarshal([]byte(r.Body), &b); err != nil {
		t.Fatalf("events body %q: %v", r.Body, err)
	}
	return b
}

func TestUsageIsSummedPerFlagIndexAndDay(t *testing.T) {
	f := newFake(t)
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), WithFlushInterval(24*time.Hour))
	c.Ready()
	k.set(time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC))
	for i := 0; i < 3; i++ {
		c.BoolVariation("f", ctxFor("u"), false)
	}
	k.set(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	for i := 0; i < 2; i++ {
		c.BoolVariation("f", ctxFor("u"), false)
	}
	c.BoolVariation("missing", ctxFor("u"), false) // not served: not counted
	c.StringVariation("f", ctxFor("u"), "x")       // mismatch: not counted
	c.Flush()
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if reqs[0].Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", reqs[0].Header.Get("Content-Type"))
	}
	got := decodeEvents(t, reqs[0]).Events
	sort.Slice(got, func(i, j int) bool { return got[i].Day < got[j].Day })
	if len(got) != 2 || got[0].Day != "2026-03-01" || got[0].Count != 3 || got[1].Day != "2026-03-02" || got[1].Count != 2 ||
		got[0].Flag != "f" || got[0].VariationIndex != 1 {
		t.Fatalf("events = %+v", got)
	}
	c.Flush()
	if len(f.requests("/sdk/v1/events")) != 1 {
		t.Fatal("an empty flush sent a request")
	}
}

func TestUsageBatchesOf500(t *testing.T) {
	f := newFake(t)
	flags := ""
	for i := 0; i < 1200; i++ {
		if i > 0 {
			flags += ","
		}
		flags += flagJSON("f"+itoa(i), 1, `[false,true]`)
	}
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, rulesetBody(flags)) }
	c, _ := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	for i := 0; i < 1200; i++ {
		c.BoolVariation("f"+itoa(i), ctxFor("u"), false)
	}
	c.Flush()
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 3 {
		t.Fatalf("requests = %d", len(reqs))
	}
	sizes := []int{len(decodeEvents(t, reqs[0]).Events), len(decodeEvents(t, reqs[1]).Events), len(decodeEvents(t, reqs[2]).Events)}
	if sizes[0] != 500 || sizes[1] != 500 || sizes[2] != 200 {
		t.Fatalf("sizes = %v", sizes)
	}
}

func TestUsageServerErrorKeepsTheCounts(t *testing.T) {
	for _, status := range []int{500, 503, 408, 429} {
		f := newFake(t)
		var fail atomic.Bool
		fail.Store(true)
		f.events = func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(204)
		}
		c, e := newClient(t, f, WithFlushInterval(24*time.Hour))
		c.Ready()
		c.BoolVariation("f", ctxFor("u"), false)
		c.Flush()
		fail.Store(false)
		c.Flush()
		reqs := f.requests("/sdk/v1/events")
		if len(reqs) != 2 || decodeEvents(t, reqs[1]).Events[0].Count != 1 {
			t.Fatalf("status %d: requests = %d", status, len(reqs))
		}
		if len(e.all()) != 1 {
			t.Fatalf("status %d: errors = %v", status, e.all())
		}
	}
}

func TestUsageRefusedBatchIsDropped(t *testing.T) {
	f := newFake(t)
	f.events = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }
	c, e := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Flush()
	c.Flush()
	if len(f.requests("/sdk/v1/events")) != 1 || len(e.all()) != 1 {
		t.Fatalf("requests = %d, errors = %v", len(f.requests("/sdk/v1/events")), e.all())
	}
}

func TestUsageNetworkFailureKeepsTheCounts(t *testing.T) {
	f := newFake(t)
	var n atomic.Int64
	f.events = func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.WriteHeader(204)
	}
	c, e := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Flush()
	c.Flush()
	if n.Load() != 2 {
		t.Fatalf("requests = %d", n.Load())
	}
	if got := e.all(); len(got) != 1 || got[0] != "toggleslash: events request failed: network" {
		t.Fatalf("errors = %v", got)
	}
}

func TestUsageFlushLoopRunsOnItsInterval(t *testing.T) {
	f := newFake(t)
	k := newFakeClock()
	c, _ := newClient(t, f, withClock(k), WithFlushInterval(7*time.Second))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	k.release(t, 7*time.Second)
	eventually(t, "automatic flush", func() bool { return len(f.requests("/sdk/v1/events")) == 1 })
}

func TestCloseSendsTheFinalFlush(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Close()
	if len(f.requests("/sdk/v1/events")) != 1 {
		t.Fatal("close did not flush")
	}
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %q", c.Status().Stream)
	}
	if !c.BoolVariation("f", ctxFor("u"), false) {
		t.Fatal("evaluation stopped working after close")
	}
	c.Flush()
	c.Close()
	if len(f.requests("/sdk/v1/events")) != 1 {
		t.Fatal("a count after close was sent")
	}
}

func TestCloseDuringAHoldReportsTheLostCounts(t *testing.T) {
	f := newFake(t)
	f.events = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}
	c, e := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Flush()
	c.Close()
	got := e.all()
	want := "toggleslash: closed with 1 usage counts unsent: the server asked to wait"
	if len(got) != 2 || got[1] != want {
		t.Fatalf("errors = %v", got)
	}
	if len(f.requests("/sdk/v1/events")) != 1 {
		t.Fatal("close waited out or ignored the hold")
	}
}

func TestCloseWithoutAHoldReportsNothingExtra(t *testing.T) {
	f := newFake(t)
	f.events = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	c, e := newClient(t, f, WithFlushInterval(24*time.Hour))
	c.Ready()
	c.BoolVariation("f", ctxFor("u"), false)
	c.Close()
	if got := e.all(); len(got) != 1 {
		t.Fatalf("errors = %v", got)
	}
}
