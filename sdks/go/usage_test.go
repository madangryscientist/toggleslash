package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// syncBuf is a fake events route: it records every body and answers with a settable status.
type syncBuf struct {
	mu      sync.Mutex
	got     []string
	headers []http.Header
	status  func(call int) (int, http.Header)
}

func (b *syncBuf) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/events" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.got = append(b.got, string(body))
		b.headers = append(b.headers, r.Header.Clone())
		call := len(b.got)
		status := b.status
		b.mu.Unlock()
		code, header := http.StatusOK, http.Header(nil)
		if status != nil {
			code, header = status(call)
		}
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(code)
	})
}

func (b *syncBuf) bodies() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.got...)
}

func (b *syncBuf) calls() int { return len(b.bodies()) }

func usageClient(t *testing.T, events *syncBuf, clk *clock, opts ...Option) *Client {
	t.Helper()
	url := serve(t, rulesetHandler(boolRuleset(), `"u"`, events.handler()))
	c := newTestClient(t, url, func(cfg *config) {
		if clk != nil {
			cfg.now = clk.now
		}
	}, append(pollOnly(WithFlushInterval(time.Hour)), opts...)...)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	return c
}

func TestUsageIsSummedAndSentOnCloseWithTheExactBody(t *testing.T) {
	var events syncBuf
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := usageClient(t, &events, clk)
	for range 3 {
		c.BoolVariation("checkout", someone, false)
	}
	c.BoolVariation("checkout", UserContext("someone-else"), false)
	c.Close()
	want := `{"events":[{"flag":"checkout","variationIndex":1,"count":4,"day":"2026-11-10"}]}`
	if got := events.bodies(); len(got) != 1 || got[0] != want {
		t.Fatalf("bodies %q, want %q", got, want)
	}
	h := events.headers[0]
	if h.Get("Content-Type") != "application/json" || h.Get("Authorization") != "Bearer sdk-key-secret" {
		t.Fatalf("headers: %v", h)
	}
}

func TestNothingPendingMeansNoRequest(t *testing.T) {
	var events syncBuf
	c := usageClient(t, &events, nil, WithFlushInterval(10*time.Millisecond))
	time.Sleep(100 * time.Millisecond)
	c.Close()
	if events.calls() != 0 {
		t.Fatalf("%d requests with nothing to send", events.calls())
	}
}

func TestCountsEitherSideOfUTCMidnightGoOutAsTwoDays(t *testing.T) {
	var events syncBuf
	clk := newClock(time.Date(2026, 11, 10, 23, 59, 59, 0, time.UTC))
	c := usageClient(t, &events, clk)
	c.BoolVariation("checkout", someone, false)
	clk.advance(2 * time.Second)
	c.BoolVariation("checkout", someone, false)
	c.BoolVariation("checkout", someone, false)
	c.Close()
	want := `{"events":[{"flag":"checkout","variationIndex":1,"count":1,"day":"2026-11-10"},{"flag":"checkout","variationIndex":1,"count":2,"day":"2026-11-11"}]}`
	if got := events.bodies(); len(got) != 1 || got[0] != want {
		t.Fatalf("bodies %q, want %q", got, want)
	}
}

// The day is UTC whatever the zone of the clock the process uses.
func TestTheDayIsUTCNotLocal(t *testing.T) {
	var events syncBuf
	zone := time.FixedZone("far east", 14*3600)
	clk := newClock(time.Date(2026, 11, 11, 3, 0, 0, 0, zone)) // 2026-11-10 13:00 UTC
	c := usageClient(t, &events, clk)
	c.BoolVariation("checkout", someone, false)
	c.Close()
	if got := events.bodies(); len(got) != 1 || !strings.Contains(got[0], `"day":"2026-11-10"`) {
		t.Fatalf("bodies %q", got)
	}
}

func TestBatchesOfAtMost500(t *testing.T) {
	var events syncBuf
	c := usageClient(t, &events, nil)
	for i := range 1100 {
		c.record(fmt.Sprintf("flag-%04d", i), 0)
	}
	c.Close()
	got := events.bodies()
	if len(got) != 3 {
		t.Fatalf("%d requests, want 3", len(got))
	}
	sizes := []int{strings.Count(got[0], `"flag"`), strings.Count(got[1], `"flag"`), strings.Count(got[2], `"flag"`)}
	if sizes[0] != 500 || sizes[1] != 500 || sizes[2] != 100 {
		t.Fatalf("batch sizes %v", sizes)
	}
}

func TestAFailedFlushIsKeptAndStopsTheFlush(t *testing.T) {
	for _, status := range []int{500, 503, 408, 429, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var events syncBuf
			var failing atomic.Bool
			failing.Store(true)
			events.status = func(int) (int, http.Header) {
				if failing.Load() {
					return status, nil
				}
				return 200, nil
			}
			var errs errorLog
			c := usageClient(t, &events, nil, WithOnError(errs.add))
			for i := range 600 { // two batches
				c.record(fmt.Sprintf("f%03d", i), 0)
			}
			for _, e := range c.flush() {
				errs.add(e)
			}
			if events.calls() != 1 {
				t.Fatalf("a failed batch must stop the flush; %d requests", events.calls())
			}
			failing.Store(false)
			if errs := c.flush(); len(errs) != 0 {
				t.Fatalf("second flush errors: %v", errs)
			}
			// The first flush sent one 500-entry batch; all 600 counts must now have gone out in the second.
			total := 0
			for _, body := range events.bodies()[1:] {
				total += strings.Count(body, `"flag"`)
			}
			if total != 600 {
				t.Fatalf("%d counts delivered after the retry, want 600", total)
			}
		})
	}
}

func TestARefusedBatchIsDroppedAndTheFlushContinues(t *testing.T) {
	var events syncBuf
	events.status = func(call int) (int, http.Header) {
		if call == 1 {
			return http.StatusBadRequest, nil
		}
		return 200, nil
	}
	var errs errorLog
	c := usageClient(t, &events, nil, WithOnError(errs.add))
	for i := range 600 {
		c.record(fmt.Sprintf("f%03d", i), 0)
	}
	for _, e := range c.flush() {
		errs.add(e)
	}
	if events.calls() != 2 {
		t.Fatalf("%d requests, want 2 (the 400 batch is dropped, the next one goes)", events.calls())
	}
	if errs.count() != 1 || errs.all()[0].Code != "http_400" {
		t.Fatalf("errors %+v", errs.all())
	}
	if again := c.flush(); len(again) != 0 || events.calls() != 2 {
		t.Fatalf("the dropped batch came back: %d requests", events.calls())
	}
}

func TestSerialFlushesNeverOverlap(t *testing.T) {
	var inFlight, peak atomic.Int64
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/ruleset" {
			w.Header().Set("ETag", `"s"`)
			_, _ = w.Write([]byte(boolRuleset()))
			return
		}
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
	}))
	c := newTestClient(t, url, nil, pollOnly(WithFlushInterval(time.Millisecond))...)
	c.Ready()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				c.BoolVariation("checkout", someone, false)
				c.flush()
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Fatalf("%d usage requests were in flight at once", peak.Load())
	}
}

func TestNothingIsCountedAfterClose(t *testing.T) {
	var events syncBuf
	c := usageClient(t, &events, nil)
	c.Close()
	for range 5 {
		c.BoolVariation("checkout", someone, false)
	}
	c.usageMu.Lock()
	n := len(c.pending)
	c.usageMu.Unlock()
	if n != 0 || events.calls() != 0 {
		t.Fatalf("pending %d, requests %d", n, events.calls())
	}
}

func TestUsageHoldAfter429AndCloseReportsTheUnsentCounts(t *testing.T) {
	var events syncBuf
	events.status = func(int) (int, http.Header) { return 429, http.Header{"Retry-After": {"120"}} }
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	var errs errorLog
	c := usageClient(t, &events, clk, WithOnError(errs.add))
	c.BoolVariation("checkout", someone, false)
	c.flush()
	if events.calls() != 1 {
		t.Fatalf("%d requests", events.calls())
	}
	c.BoolVariation("checkout", someone, false)
	c.flush() // held: no request
	if events.calls() != 1 {
		t.Fatalf("a held flush sent a request (%d)", events.calls())
	}
	clk.advance(119 * time.Second)
	c.flush()
	if events.calls() != 1 {
		t.Fatalf("still held at 119 s, but %d requests", events.calls())
	}
	// Close inside the hold neither waits nor sends, and says what it lost.
	within(t, 3*time.Second, "Close inside a hold", c.Close)
	if events.calls() != 1 {
		t.Fatalf("Close sent during the hold (%d requests)", events.calls())
	}
	var closeErr *Error
	for _, e := range errs.all() {
		if e.Op == "close" {
			closeErr = e
		}
	}
	if closeErr == nil || closeErr.Message != "closed with 1 usage counts unsent: the server asked to wait" {
		t.Fatalf("errors %+v", errs.all())
	}
}

func TestAHoldPassesAndTheCountsGoOut(t *testing.T) {
	var events syncBuf
	events.status = func(call int) (int, http.Header) {
		if call == 1 {
			return 429, http.Header{"Retry-After": {"10"}}
		}
		return 200, nil
	}
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := usageClient(t, &events, clk)
	c.BoolVariation("checkout", someone, false)
	c.flush()
	clk.advance(11 * time.Second)
	c.flush()
	if events.calls() != 2 {
		t.Fatalf("%d requests", events.calls())
	}
	if !strings.Contains(events.bodies()[1], `"count":1`) {
		t.Fatalf("the kept count was not resent: %q", events.bodies()[1])
	}
}

func TestUsageRetryAfterAbove300IsHeldFor300(t *testing.T) {
	var events syncBuf
	events.status = func(int) (int, http.Header) { return 429, http.Header{"Retry-After": {"86400"}} }
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := usageClient(t, &events, clk)
	c.BoolVariation("checkout", someone, false)
	c.flush()
	c.flushMu.Lock()
	until := c.eventsHoldUntil
	c.flushMu.Unlock()
	if want := clk.now().Add(300 * time.Second); !until.Equal(want) {
		t.Fatalf("held until %s, want %s", until, want)
	}
	clk.advance(299 * time.Second)
	c.flush()
	clk.advance(2 * time.Second)
	c.flush()
	if events.calls() != 2 {
		t.Fatalf("%d requests: one at the start and one after 300 s", events.calls())
	}
}

func TestALater429NeverShortensTheEventsHold(t *testing.T) {
	var events syncBuf
	events.status = func(int) (int, http.Header) { return 429, http.Header{"Retry-After": {"5"}} }
	clk := newClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	c := usageClient(t, &events, clk)
	long := clk.now().Add(250 * time.Second)
	c.flushMu.Lock()
	c.eventsHoldUntil = long
	keep, _ := c.post([]usageEntry{{usageKey{"checkout", 1, "2026-11-10"}, 1}})
	got := c.eventsHoldUntil
	c.flushMu.Unlock()
	if !keep || !got.Equal(long) {
		t.Fatalf("keep=%v; a 429 asking for 5 s moved a 250 s hold to %s", keep, got)
	}
}

func TestRetryAfterMustBeWholeDigits(t *testing.T) {
	cases := []struct {
		header string
		wait   time.Duration
		ok     bool
	}{
		{"0", 0, true},
		{"7", 7 * time.Second, true},
		{" 7 ", 7 * time.Second, true},
		{"\t12\t", 12 * time.Second, true},
		{"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true},
		{"99999999999999999999", 300 * time.Second, true},
		{"1.5", 0, false},
		{"-1", 0, false},
		{"+5", 0, false},
		{"1e2", 0, false},
		{"0x10", 0, false},
		{"", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false},
		{"5 6", 0, false},
		{"٥", 0, false}, // an Arabic-Indic digit is not a decimal digit here
	}
	for _, tc := range cases {
		resp := &http.Response{StatusCode: 429, Header: http.Header{}}
		if tc.header != "" {
			resp.Header.Set("Retry-After", tc.header)
		}
		wait, ok := retryAfter(resp)
		if wait != tc.wait || ok != tc.ok {
			t.Errorf("Retry-After %q: got (%s, %v), want (%s, %v)", tc.header, wait, ok, tc.wait, tc.ok)
		}
	}
	if _, ok := retryAfter(&http.Response{StatusCode: 503, Header: http.Header{"Retry-After": {"5"}}}); ok {
		t.Error("a 503 must not hold")
	}
}
