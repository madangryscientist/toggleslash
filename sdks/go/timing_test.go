package toggleslash

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"120", 120 * time.Second, true},
		{"0", 0, true},
		{"007", 7 * time.Second, true},
		{" 30 ", 30 * time.Second, true},
		{"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true},
		{"600", 300 * time.Second, true},
		{"99999999999999999999", 300 * time.Second, true},
		{"1.5", 0, false},
		{"-5", 0, false},
		{"+5", 0, false},
		{"1e2", 0, false},
		{"", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDelay(t *testing.T) {
	jitter := 0.0
	c := &Client{cfg: config{stream: true, pollInterval: 30 * time.Second, jitter: func() float64 { return jitter }}}
	for failures, want := range map[int]time.Duration{1: 2 * time.Second, 2: 4 * time.Second, 4: 16 * time.Second, 5: 30 * time.Second, 6: 30 * time.Second, 40: 30 * time.Second} {
		jitter = 0
		if got := c.delay(failures); got != want/2 {
			t.Errorf("delay(%d) at jitter 0 = %v, want %v", failures, got, want/2)
		}
		jitter = 0.999999
		if got := c.delay(failures); got < want*99/100 || got >= want {
			t.Errorf("delay(%d) near jitter 1 = %v, want just under %v", failures, got, want)
		}
	}
	c.cfg.stream = false
	if got := c.delay(5); got != 30*time.Second {
		t.Errorf("stream off: delay = %v, want the poll interval", got)
	}
}

func TestRetryAfterHoldsTheRulesetAt300Seconds(t *testing.T) {
	f := newFake(t, rulesetJSON())
	var n atomic.Int32
	f.on(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		}
		f.defaultRuleset(w, r)
	}, nil, nil)
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithPollInterval(time.Second), WithFlushInterval(time.Hour))
	if c.Ready() {
		t.Fatal("Ready() = true after a 429")
	}
	if got := len(f.requests("/sdk/v1/ruleset")); got != 1 {
		t.Fatalf("the first load was held: %d requests", got)
	}
	if c.Status().LastError != "http_429" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
	// The poll wait is 1 s but the hold is 300 s: the wait must be the hold.
	clock.waitTimer(t, 300*time.Second)
	clock.advance(299 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := len(f.requests("/sdk/v1/ruleset")); got != 1 {
		t.Fatalf("a request was made inside the hold: %d requests", got)
	}
	clock.advance(time.Second)
	eventually(t, "the poll once the hold is over", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/ruleset")) == 2 })
	eventually(t, "a sync", 3*time.Second, func() bool { return c.Status().ETag != "" })
}

func TestRetryAfterHoldsTheStreamToo(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "250")
		w.WriteHeader(429)
	}, nil)
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, WithStream(true), withClock(clock), withJitter(func() float64 { return 0 }),
		WithPollInterval(30*time.Second), WithFlushInterval(time.Hour))
	c.Ready()
	clock.waitTimer(t, 250*time.Second)
	if c.Status().LastError != "http_429" {
		t.Fatalf("lastError = %q", c.Status().LastError)
	}
	if got := len(f.requests("/sdk/v1/ruleset")); got != 1 {
		t.Fatalf("a poll went out inside the hold: %d", got)
	}
}

func TestRetryAfterOnAnyOtherStatusIsIgnored(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "250")
		w.WriteHeader(503)
	}, nil, nil)
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithPollInterval(7*time.Second), WithFlushInterval(time.Hour))
	c.Ready()
	clock.waitTimer(t, 7*time.Second)
}

func TestUsageHoldIsSeparateFromTheRulesetHold(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	var polls atomic.Int32
	f.on(func(w http.ResponseWriter, r *http.Request) {
		if polls.Add(1) >= 2 {
			w.Header().Set("Retry-After", "200")
			w.WriteHeader(429)
			return
		}
		f.defaultRuleset(w, r)
	}, nil, nil)
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithPollInterval(time.Second), WithFlushInterval(10*time.Second))
	c.Ready()
	clock.waitTimer(t, time.Second)
	clock.advance(time.Second)
	eventually(t, "the 429 on the poll", 3*time.Second, func() bool { return c.Status().LastError == "http_429" })
	c.BoolVariation("a", ctxFor("u"), false)
	clock.advance(10 * time.Second)
	eventually(t, "usage sent while the ruleset is held", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) == 1 })
}

func TestUsage429HoldsUsageAndFinalFlushDoesNotWait(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	var posts atomic.Int32
	f.on(nil, nil, func(w http.ResponseWriter, r *http.Request) {
		if posts.Add(1) == 1 {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	})
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	log := &errorLog{}
	c := makeClient(t, f, withClock(clock), WithOnError(log.add), WithFlushInterval(10*time.Second))
	c.Ready()
	c.BoolVariation("a", ctxFor("u"), false)
	clock.waitTimer(t, 10*time.Second)
	clock.advance(10 * time.Second)
	eventually(t, "the first events request", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) == 1 })
	eventually(t, "the 429 reported", 3*time.Second, func() bool { return log.count() == 1 })
	// Inside the hold: the next flush does not send.
	clock.waitTimer(t, 10*time.Second)
	clock.advance(10 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := len(f.requests("/sdk/v1/events")); got != 1 {
		t.Fatalf("usage was sent inside the hold: %d requests", got)
	}
	start := time.Now()
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("the final flush waited out the hold")
	}
	if got := len(f.requests("/sdk/v1/events")); got != 1 {
		t.Fatalf("the final flush sent inside the hold: %d requests", got)
	}
	found := false
	for _, e := range log.all() {
		if strings.Contains(e.Error(), "closed with 1 usage counts unsent: the server asked to wait") {
			found = true
		}
	}
	if !found {
		t.Fatalf("onError was not told the counts were lost: %v", log.all())
	}
}

func TestWatchdogEndsAQuietStream(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, nil)
	c := makeClient(t, f, WithStream(true), withStreamIdle(300*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	eventually(t, "the watchdog's timeout", 3*time.Second, func() bool {
		st := c.Status()
		return st.LastError == "timeout" && st.Stream != StreamConnected
	})
}

func TestKeepAliveRestartsTheWatchdog(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		w.(http.Flusher).Flush()
		for i := 0; i < 10; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}, nil)
	c := makeClient(t, f, WithStream(true), withStreamIdle(350*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, "connected", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	// Three idle periods of keep-alives pass with no timeout.
	end := time.Now().Add(1050 * time.Millisecond)
	for time.Now().Before(end) {
		if st := c.Status(); st.Stream != StreamConnected || st.LastError != "" {
			t.Fatalf("the stream dropped while keep-alives arrived: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	eventually(t, "the timeout once the keep-alives stop", 4*time.Second, func() bool { return c.Status().LastError == "timeout" })
}

func TestStreamOpenIsBoundedByTheRequestTimeout(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(nil, f.hang, nil)
	c := makeClient(t, f, WithStream(true), WithRequestTimeout(200*time.Millisecond), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, "a timeout opening the stream", 3*time.Second, func() bool { return c.Status().LastError == "timeout" })
}

func TestUTCMidnightSplitsTheDay(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	clock := newFakeClock(time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithFlushInterval(time.Hour))
	c.Ready()
	c.BoolVariation("a", ctxFor("u"), false)
	c.BoolVariation("a", ctxFor("u"), false)
	clock.advance(2 * time.Second)
	c.BoolVariation("a", ctxFor("u"), false)
	c.Close()
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("%d events requests", len(reqs))
	}
	body := string(reqs[0].body)
	if !strings.Contains(body, `"count":2,"day":"2026-03-01"`) || !strings.Contains(body, `"count":1,"day":"2026-03-02"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestUsageBatchesOf500(t *testing.T) {
	var flags []string
	for i := 0; i < 1200; i++ {
		flags = append(flags, flagJSON("flag-"+itoa(i), `[true]`, 0, 1))
	}
	f := newFake(t, rulesetJSON(flags...))
	c := makeClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	for i := 0; i < 1200; i++ {
		c.BoolVariation("flag-"+itoa(i), ctxFor("u"), false)
	}
	c.Close()
	reqs := f.requests("/sdk/v1/events")
	if len(reqs) != 3 {
		t.Fatalf("%d events requests, want 3", len(reqs))
	}
	for i, want := range []int{500, 500, 200} {
		if got := strings.Count(string(reqs[i].body), `"variationIndex"`); got != want {
			t.Errorf("request %d holds %d entries, want %d", i, got, want)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

func TestUsageOutcomes(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1), flagJSON("b", `[true]`, 0, 1)))
	var posts atomic.Int32
	f.on(nil, nil, func(w http.ResponseWriter, r *http.Request) {
		switch posts.Add(1) {
		case 1:
			w.WriteHeader(503) // kept
		case 2:
			w.WriteHeader(400) // dropped
		default:
			w.WriteHeader(204)
		}
	})
	clock := newFakeClock(time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	c := makeClient(t, f, withClock(clock), WithFlushInterval(10*time.Second))
	c.Ready()
	flush := func(n int) {
		clock.waitTimer(t, 10*time.Second)
		clock.advance(10 * time.Second)
		eventually(t, "events request", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) == n })
		time.Sleep(30 * time.Millisecond)
	}
	c.BoolVariation("a", ctxFor("u"), false)
	flush(1)
	c.BoolVariation("b", ctxFor("u"), false)
	flush(2) // a is kept after the 503 and goes again with b; this one answers 400, so both are dropped
	c.BoolVariation("b", ctxFor("u"), false)
	flush(3)
	reqs := f.requests("/sdk/v1/events")
	if !strings.Contains(string(reqs[1].body), `"flag":"a"`) || !strings.Contains(string(reqs[1].body), `"flag":"b"`) {
		t.Fatalf("a 503 did not keep the counts: %s", reqs[1].body)
	}
	if strings.Contains(string(reqs[2].body), `"flag":"a"`) || !strings.Contains(string(reqs[2].body), `"count":1`) {
		t.Fatalf("a 400 did not drop the counts: %s", reqs[2].body)
	}
}
