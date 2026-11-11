package toggleslash

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type eventsRequest struct {
	Events []map[string]any `json:"events"`
}

func parseEvents(t *testing.T, r seen) eventsRequest {
	t.Helper()
	var out eventsRequest
	if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
		t.Fatalf("events body %q: %v", r.Body, err)
	}
	return out
}

func servingManyFlags(f *fakeServer, n int) {
	flags := make([]string, n)
	for i := range flags {
		flags[i] = flagJSON(fmt.Sprintf("flag-%04d", i), 1, `[false,true]`, 1)
	}
	body := rulesetJSON(flags...)
	f.handle("/sdk/v1/ruleset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"1"`)
		_, _ = io.WriteString(w, body)
	})
}

func TestUsageCountsAreSummedPerFlagVariationAndDay(t *testing.T) {
	f := newFakeServer(t)
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second), withClock(clock))
	c.Ready()
	for range 3 {
		c.BoolVariation("f", user("a"), false)
	}
	c.BoolVariation("f", user("b"), false)
	c.BoolVariation("missing", user("a"), false) // an error result is not a usage count
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 1)
	req := parseEvents(t, f.seen("/sdk/v1/events")[0])
	if len(req.Events) != 1 {
		t.Fatalf("events = %v", req.Events)
	}
	e := req.Events[0]
	if len(e) != 4 || e["flag"] != "f" || e["variationIndex"] != float64(1) || e["count"] != float64(4) || e["day"] != "2026-11-10" {
		t.Errorf("event = %v", e)
	}
	// Nothing recorded: the next flush sends nothing.
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	time.Sleep(30 * time.Millisecond)
	if f.count("/sdk/v1/events") != 1 {
		t.Error("an empty flush sent a request")
	}
}

func TestUsageIsSentInBatchesOfAtMost500Entries(t *testing.T) {
	f := newFakeServer(t)
	servingManyFlags(f, 1200)
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second), withClock(clock))
	c.Ready()
	for i := range 1200 {
		c.BoolVariation(fmt.Sprintf("flag-%04d", i), user("u"), false)
	}
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 3)
	sizes := []int{}
	seenFlags := map[string]bool{}
	for _, r := range f.seen("/sdk/v1/events") {
		req := parseEvents(t, r)
		sizes = append(sizes, len(req.Events))
		for _, e := range req.Events {
			seenFlags[e["flag"].(string)] = true
		}
	}
	if fmt.Sprint(sizes) != "[500 500 200]" || len(seenFlags) != 1200 {
		t.Errorf("batch sizes %v, distinct flags %d", sizes, len(seenFlags))
	}
}

func TestUsageIsKeptOn5xxAnd408And429AndNetworkErrorsAndDroppedOtherwise(t *testing.T) {
	for _, tc := range []struct {
		status int
		keep   bool
	}{{500, true}, {503, true}, {408, true}, {429, true}, {400, false}, {401, false}, {413, false}, {404, false}, {200, false}, {204, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			f := newFakeServer(t)
			var calls atomic.Int32
			f.handle("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			clock := newFakeClock(midday)
			var errs errorLog
			c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second),
				withClock(clock), WithOnError(errs.add))
			c.Ready()
			c.BoolVariation("f", user("u"), false)
			clock.awaitTimer(t, 30*time.Second)
			clock.advance(30 * time.Second)
			f.await("/sdk/v1/events", 1)
			failed := tc.status >= 400
			if failed {
				eventually(t, "the failure to be reported", func() bool { return errs.count() == 1 })
				if got := errs.messages()[0]; got != fmt.Sprintf("POST /sdk/v1/events answered %d", tc.status) {
					t.Errorf("message = %q", got)
				}
			}
			if c.Status().LastError != "" {
				t.Errorf("a usage failure reached status: %q", c.Status().LastError)
			}
			// Let a 429 hold pass entirely: the retry-after is absent, so there is none.
			c.BoolVariation("f", user("u"), false)
			clock.awaitTimer(t, 30*time.Second)
			clock.advance(30 * time.Second)
			if tc.keep {
				f.await("/sdk/v1/events", 2)
				req := parseEvents(t, f.seen("/sdk/v1/events")[1])
				if req.Events[0]["count"] != float64(2) {
					t.Errorf("kept counts were not merged with the new ones: %v", req.Events)
				}
			} else {
				f.await("/sdk/v1/events", 2)
				req := parseEvents(t, f.seen("/sdk/v1/events")[1])
				if req.Events[0]["count"] != float64(1) {
					t.Errorf("dropped counts came back: %v", req.Events)
				}
			}
		})
	}
}

func TestUsageIsKeptOnANetworkError(t *testing.T) {
	f := newFakeServer(t)
	var calls atomic.Int32
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// Close the connection without an answer.
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
	})
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second), withClock(clock))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 1)
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 2)
	if req := parseEvents(t, f.seen("/sdk/v1/events")[1]); req.Events[0]["count"] != float64(1) {
		t.Errorf("events = %v", req.Events)
	}
}

// At 23:59:59Z the day is the 10th; a second later it is the 11th. The two days are two entries.
func TestUsageSplitsAtUTCMidnight(t *testing.T) {
	f := newFakeServer(t)
	// 18:59:59 at UTC-5 is 23:59:59Z; a second later the local date is still the 10th and the UTC date is the 11th.
	clock := newFakeClock(time.Date(2026, 11, 10, 18, 59, 59, 0, time.FixedZone("west", -5*3600)))
	c := newTestClient(t, f, WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second), withClock(clock))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	c.BoolVariation("f", user("u"), false)
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(time.Second)
	c.BoolVariation("f", user("u"), false)
	clock.advance(29 * time.Second)
	f.await("/sdk/v1/events", 1)
	req := parseEvents(t, f.seen("/sdk/v1/events")[0])
	got := map[string]float64{}
	for _, e := range req.Events {
		got[e["day"].(string)] += e["count"].(float64)
	}
	if len(req.Events) != 2 || got["2026-11-10"] != 2 || got["2026-11-11"] != 1 {
		t.Errorf("events = %v", req.Events)
	}
}

func TestCloseSendsTheCountsOnce(t *testing.T) {
	f := newFakeServer(t)
	c := NewClient(f.url(), "k", quiet()...)
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	c.BoolVariation("f", user("u"), false)
	c.Close()
	req := parseEvents(t, f.seen("/sdk/v1/events")[0])
	if f.count("/sdk/v1/events") != 1 || req.Events[0]["count"] != float64(2) {
		t.Errorf("final flush: %d requests, %v", f.count("/sdk/v1/events"), req.Events)
	}
	c.BoolVariation("f", user("u"), false) // nothing is counted after Close
	c.Close()
	if f.count("/sdk/v1/events") != 1 {
		t.Error("a second Close sent counts again")
	}
}

func TestFinalFlushDoesNotWaitOutAHoldAndReportsTheLoss(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	clock := newFakeClock(midday)
	var errs errorLog
	c := NewClient(f.url(), "k", WithStream(false), WithPollInterval(bigInterval), WithFlushInterval(30*time.Second),
		withClock(clock), WithOnError(errs.add))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 1)
	eventually(t, "the 429 to be reported", func() bool { return errs.count() == 1 })
	c.BoolVariation("f", user("u"), false)
	within(t, 2*time.Second, "Close", c.Close)
	if f.count("/sdk/v1/events") != 1 {
		t.Error("the final flush sent during the hold")
	}
	msgs := errs.messages()
	want := "closed with 1 usage counts unsent: the server asked to wait"
	if len(msgs) != 2 || msgs[1] != want {
		t.Errorf("messages = %q, want the second to be %q", msgs, want)
	}
}

func TestUsageHoldDoesNotStopTheRuleset(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "250")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	clock := newFakeClock(midday)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(60*time.Second), WithFlushInterval(30*time.Second), withClock(clock))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 1)
	clock.awaitTimer(t, 60*time.Second)
	clock.advance(60 * time.Second)
	f.await("/sdk/v1/ruleset", 2)
}

func TestNoContextAppearsInAnyRequestOrError(t *testing.T) {
	const secret = "zz-distinctive-person-7f3a"
	f := newFakeServer(t)
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	clock := newFakeClock(midday)
	var errs errorLog
	var hooked atomic.Int32
	c := NewClient(f.url(), "k", WithPollInterval(bigInterval), WithFlushInterval(30*time.Second),
		withClock(clock), WithOnError(errs.add), WithAfterEvaluation(func(EvaluationEvent) { hooked.Add(1) }))
	ctx := Object(Member{Key: "key", Value: String(secret)}, Member{Key: "email", Value: String(secret + "@example.com")})
	c.Ready()
	f.await("/sdk/v1/stream", 1)
	c.BoolVariation("f", ctx, false)
	c.BoolVariation("f", Null(), false) // an invalid context
	clock.awaitTimer(t, 30*time.Second)
	clock.advance(30 * time.Second)
	f.await("/sdk/v1/events", 1)
	eventually(t, "the error", func() bool { return errs.count() >= 1 })
	c.Close()
	for _, path := range []string{"/sdk/v1/ruleset", "/sdk/v1/stream", "/sdk/v1/events"} {
		for _, r := range f.seen(path) {
			all := r.Path + r.Body + fmt.Sprint(r.Header) + r.Host
			if strings.Contains(all, secret) {
				t.Errorf("%s request carried the context: %s", path, all)
			}
		}
	}
	for _, m := range errs.messages() {
		if strings.Contains(m, secret) {
			t.Errorf("error message carried the context: %q", m)
		}
	}
	if hooked.Load() != 2 {
		t.Errorf("hook calls = %d", hooked.Load())
	}
}
