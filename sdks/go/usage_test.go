package toggleslash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type wireEntry struct {
	Flag           string `json:"flag"`
	VariationIndex int    `json:"variationIndex"`
	Count          int64  `json:"count"`
	Day            string `json:"day"`
}

func decodeBatch(t *testing.T, body string) []wireEntry {
	t.Helper()
	var doc struct {
		Events []wireEntry `json:"events"`
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("events body %q: %v", body, err)
	}
	return doc.Events
}

func TestUsageIsCountedPerFlagVariationAndDay(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("a", 1, 1), flag("b", 0, 1)))
	clk := newFakeClock(time.Date(2030, 3, 4, 23, 59, 58, 0, time.UTC))
	c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(24*time.Hour))
	c.Ready()
	for range 3 {
		c.BoolVariation("a", shopper, false)
	}
	c.BoolVariation("b", shopper, true)
	c.BoolVariation("missing", shopper, true) // a fallback is not counted
	clk.Advance(5 * time.Second)              // past UTC midnight, before the flush
	c.BoolVariation("a", shopper, false)
	eventually(t, "the flush timer", func() bool { return slices.Contains(clk.Waiting(), 25*time.Second) })
	clk.Advance(25 * time.Second)
	eventually(t, "the events request", func() bool { return srv.count(routeEvents) == 1 })
	got := decodeBatch(t, srv.got(routeEvents)[0].body)
	want := []wireEntry{
		{"a", 1, 3, "2030-03-04"},
		{"b", 0, 1, "2030-03-04"},
		{"a", 1, 1, "2030-03-05"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestUsageBatchesStayUnder500(t *testing.T) {
	var flags []string
	for i := range 1201 {
		flags = append(flags, flag(fmt.Sprintf("flag-%04d", i), 1, 1))
	}
	srv := newFakeServer(t, rulesetOf(flags...))
	clk := newFakeClock(epoch)
	c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(24*time.Hour))
	c.Ready()
	for i := range 1201 {
		c.BoolVariation(fmt.Sprintf("flag-%04d", i), shopper, false)
	}
	eventually(t, "the flush timer", func() bool { return slices.Contains(clk.Waiting(), 30*time.Second) })
	clk.Advance(30 * time.Second)
	eventually(t, "three requests", func() bool { return srv.count(routeEvents) == 3 })
	var sizes []int
	for _, r := range srv.got(routeEvents) {
		sizes = append(sizes, len(decodeBatch(t, r.body)))
	}
	if !slices.Equal(sizes, []int{500, 500, 201}) {
		t.Fatalf("batch sizes %v", sizes)
	}
}

func TestUsageKeptOnServerErrorAndDroppedOnClientError(t *testing.T) {
	for _, tc := range []struct {
		status   int
		retained bool
	}{{500, true}, {503, true}, {408, true}, {429, true}, {400, false}, {401, false}, {413, false}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			srv := newFakeServer(t, rulesetOf(flag("a", 1, 1)))
			var mu sync.Mutex
			status := tc.status
			srv.handle(routeEvents, func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.WriteHeader(status)
			})
			log := &errorLog{}
			clk := newFakeClock(epoch)
			c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(24*time.Hour), log.option())
			c.Ready()
			c.BoolVariation("a", shopper, false)
			c.BoolVariation("a", shopper, false)
			eventually(t, "the flush timer", func() bool { return slices.Contains(clk.Waiting(), 30*time.Second) })
			clk.Advance(30 * time.Second)
			eventually(t, "the first request", func() bool { return srv.count(routeEvents) == 1 })
			eventually(t, "the error", func() bool { return len(log.all()) == 1 })
			if want := fmt.Sprintf("POST /sdk/v1/events answered %d", tc.status); log.all()[0] != want {
				t.Fatalf("error %q, want %q", log.all()[0], want)
			}
			mu.Lock()
			status = 204
			mu.Unlock()
			// A 429 with no Retry-After holds nothing, so the next interval tries again.
			eventually(t, "the flush timer again", func() bool { return slices.Contains(clk.Waiting(), 30*time.Second) })
			clk.Advance(30 * time.Second)
			if tc.retained {
				eventually(t, "the retry", func() bool { return srv.count(routeEvents) == 2 })
				if got := decodeBatch(t, srv.got(routeEvents)[1].body); len(got) != 1 || got[0].Count != 2 {
					t.Fatalf("retained %+v", got)
				}
			} else {
				time.Sleep(50 * time.Millisecond)
				if n := srv.count(routeEvents); n != 1 {
					t.Fatalf("a refused batch was sent again (%d requests)", n)
				}
			}
		})
	}
}

func TestUsageHoldOn429AndTheUnsentCloseMessage(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("a", 1, 1)))
	srv.handle(routeEvents, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "100000")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	log := &errorLog{}
	clk := newFakeClock(epoch)
	c := start(t, srv, WithStream(false), withClock(clk), WithPollInterval(24*time.Hour), log.option())
	c.Ready()
	c.BoolVariation("a", shopper, false)
	eventually(t, "the flush timer", func() bool { return slices.Contains(clk.Waiting(), 30*time.Second) })
	clk.Advance(30 * time.Second)
	eventually(t, "the first request", func() bool { return srv.count(routeEvents) == 1 })
	c.BoolVariation("a", shopper, false)
	// 30 s later, and 299 s in total: still inside the 300 s hold, so nothing is sent.
	for range 9 {
		eventually(t, "the flush timer", func() bool { return slices.Contains(clk.Waiting(), 30*time.Second) })
		clk.Advance(30 * time.Second)
		time.Sleep(5 * time.Millisecond)
	}
	if n := srv.count(routeEvents); n != 1 {
		t.Fatalf("sent during the hold: %d requests", n)
	}
	c.Close()
	if n := srv.count(routeEvents); n != 1 {
		t.Fatalf("Close sent during the hold: %d requests", n)
	}
	want := "closed with 1 usage counts unsent: the server asked to wait"
	if all := log.all(); !slices.Contains(all, want) {
		t.Fatalf("errors %q, want one %q", all, want)
	}
}

func TestCloseSendsThePendingCountsOnce(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("a", 1, 1)))
	c := start(t, srv, WithStream(false))
	c.Ready()
	c.BoolVariation("a", shopper, false)
	c.Close()
	c.Close()
	if n := srv.count(routeEvents); n != 1 {
		t.Fatalf("events requests: %d", n)
	}
	// After Close nothing is counted.
	c.BoolVariation("a", shopper, false)
	c.Close()
	if n := srv.count(routeEvents); n != 1 {
		t.Fatalf("events requests after: %d", n)
	}
}

func TestUsageBodyHoldsNoContext(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("a", 1, 1)))
	c := start(t, srv, WithStream(false))
	c.Ready()
	ctx := NewContext("very-private-key", map[string]any{"email": "private@example.com", "plan": "gold"})
	c.BoolVariation("a", ctx, false)
	c.Close()
	body := srv.got(routeEvents)[0].body
	for _, secret := range []string{"very-private-key", "private@example.com", "gold", "email"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the body holds %q: %s", secret, body)
		}
	}
	decodeBatch(t, body) // only the four known fields
}
