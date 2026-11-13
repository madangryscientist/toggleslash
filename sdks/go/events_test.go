package toggleslash

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

func TestUsageCountsAcrossUTCMidnight(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	clk := newFakeClock()
	clk.now = time.Date(2026, 11, 10, 23, 59, 59, 0, time.UTC)
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	client.BoolVariation("feature", userContext("u1"), false)
	clk.advanceBy(t, 2*time.Second)
	client.BoolVariation("feature", userContext("u1"), false)
	client.Close()

	requests := fake.recorded("/sdk/v1/events")
	if len(requests) != 1 {
		t.Fatalf("events requests = %d, want one final flush", len(requests))
	}
	document := decodeBody(t, requests[0].body)
	events, ok := document["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events = %#v, want two entries either side of midnight", document["events"])
	}
	days := map[string]float64{}
	for _, raw := range events {
		entry := raw.(map[string]any)
		if len(entry) != 4 {
			t.Fatalf("an entry has fields %v, want exactly flag, variationIndex, count, day", entry)
		}
		days[entry["day"].(string)] += entry["count"].(float64)
		if entry["flag"] != "feature" {
			t.Fatalf("flag = %v", entry["flag"])
		}
	}
	if days["2026-11-10"] != 1 || days["2026-11-11"] != 1 {
		t.Fatalf("days = %v, want one count on each UTC day", days)
	}
}

func TestUsageFlushBatchesAt500(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	var mu sync.Mutex
	var bodies [][]byte
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.usageMu.Lock()
	for i := 0; i < 501; i++ {
		key := usageKey{flag: "flag-" + string(rune('a'+i%26)) + "-" + padInt(i), variation: 1, day: "2026-11-10"}
		client.usage[key] = 1
	}
	client.usageMu.Unlock()

	client.flush(false)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2 for 501 entries", len(bodies))
	}
	first := decodeBody(t, bodies[0])["events"].([]any)
	second := decodeBody(t, bodies[1])["events"].([]any)
	if len(first) != 500 || len(second) != 1 {
		t.Fatalf("batch sizes = %d and %d, want 500 and 1", len(first), len(second))
	}
}

func padInt(n int) string {
	if n < 10 {
		return "00" + string(rune('0'+n))
	}
	if n < 100 {
		return "0" + string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}

func TestUsageRequestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		kept   bool
		report string
	}{
		{name: "sent", status: http.StatusOK},
		{name: "dropped", status: http.StatusBadRequest},
		{name: "kept on 408", status: http.StatusRequestTimeout, kept: true},
		{name: "kept on 500", status: http.StatusInternalServerError, kept: true},
		{name: "kept on 429", status: http.StatusTooManyRequests, kept: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeServer(t)
			fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
				_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			}
			fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
				w.WriteHeader(testCase.status)
			}
			client := newClient(t, clockOptions(fake, newFakeClock()))
			if !client.Ready(context.Background()) {
				t.Fatalf("Ready = false, want true")
			}
			client.BoolVariation("feature", userContext("u1"), false)
			client.flush(false)

			client.usageMu.Lock()
			pending := len(client.usage)
			client.usageMu.Unlock()
			if testCase.kept && pending != 1 {
				t.Fatalf("pending = %d after a %d, want kept", pending, testCase.status)
			}
			if !testCase.kept && pending != 0 {
				t.Fatalf("pending = %d after a %d, want sent or dropped", pending, testCase.status)
			}
		})
	}
}

func TestRetryAfterCapsTheRulesetHoldAt300(t *testing.T) {
	fake := newFakeServer(t)
	var requests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1000")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	clk := newFakeClock()
	opts := clockOptions(fake, clk)
	opts.FlushInterval = 24 * time.Hour
	client := newClient(t, opts)
	if client.Ready(context.Background()) {
		t.Fatalf("Ready = true after a 429 with no ruleset")
	}
	clk.waitForDuration(t, 300*time.Second, 3*time.Second)
	// A later, shorter Retry-After never shortens the hold.
	client.readRetryAfter(&client.requestHold, "10")
	if got := client.requestHold.remaining(clk.Now()); got <= 299*time.Second {
		t.Fatalf("a shorter Retry-After shortened the hold to %v", got)
	}
}

func TestRetryAfterCapsTheStreamHoldAt300(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Retry-After", "99999")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	on := true
	clk := newFakeClock()
	opts := clockOptions(fake, clk)
	opts.Stream = &on
	opts.FlushInterval = 24 * time.Hour
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	eventually(t, 3*time.Second, func() bool { return fake.count("/sdk/v1/stream") == 1 }, "a stream attempt")
	clk.waitForDuration(t, 300*time.Second, 3*time.Second)
	if status := client.Status(); status.LastError == nil || *status.LastError != HTTPLastError(http.StatusTooManyRequests) {
		t.Fatalf("lastError = %v, want http_429", status.LastError)
	}
}

func TestRetryAfterHoldsTheUsageReports(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	var requests atomic.Int64
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1000")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	clk := newFakeClock()
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)
	client.flush(false)
	if requests.Load() != 1 {
		t.Fatalf("events requests = %d, want 1", requests.Load())
	}
	if got := client.eventsHold.remaining(clk.Now()); got != 300*time.Second {
		t.Fatalf("events hold = %v, want the 300s cap", got)
	}
	// A flush during the hold returns at once, keeping its counts.
	client.flush(false)
	if requests.Load() != 1 {
		t.Fatalf("a flush during the hold sent a request")
	}
	// Past the hold, the counts go out. One step may land before the flush
	// loop is asleep on the clock, so the second phase advances only when a
	// timer is armed; a loaded machine widens that gap (RUN.md, decision 60).
	clk.advance(301 * time.Second)
	clk.advanceUntil(t, 3*time.Second, func() bool { return requests.Load() >= 2 })
}

func TestCloseReportsCountsLostToARetryAfterHold(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	var requests atomic.Int64
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		requests.Add(1)
		w.Header().Set("Retry-After", "1000")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	errors := make(chan error, 4)
	clk := newFakeClock()
	opts := clockOptions(fake, clk)
	opts.OnError = func(err error) { errors <- err }
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)
	client.flush(false)
	client.Close()

	if requests.Load() != 1 {
		t.Fatalf("events requests = %d, want no final flush during the hold", requests.Load())
	}
	found := false
	timeout := time.After(2 * time.Second)
	for !found {
		select {
		case err := <-errors:
			if strings.Contains(err.Error(), "usage counts unsent") &&
				strings.Contains(err.Error(), "the server asked to wait") {
				found = true
			}
		case <-timeout:
			t.Fatalf("no onError call for the counts lost at close")
		}
	}
}

func TestUsageBodyCarriesOnlyTheFourFields(t *testing.T) {
	var buffer strings.Builder
	batch := []usageEntry{{key: usageKey{flag: "f", variation: 2, day: "2026-11-10"}, count: 3}}
	if err := jsonval.Write(&buffer, jsonval.Object{"events": usageBody(batch)}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := `{"events":[{"count":3,"day":"2026-11-10","flag":"f","variationIndex":2}]}`
	if buffer.String() != want {
		t.Fatalf("body = %s, want %s", buffer.String(), want)
	}
}
