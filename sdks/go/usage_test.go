package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUsageCountsPerFlagIndexAndDay(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := newClient(t, f)
	c.Ready()
	for i := 0; i < 3; i++ {
		c.BoolVariation("f", user("u"), false)
	}
	c.StringVariation("s", user("u"), "")
	c.BoolVariation("missing", user("u"), false) // a fallback: not counted
	_ = c.Close()
	reqs := f.seen("/sdk/v1/events")
	if len(reqs) != 1 {
		t.Fatalf("%d usage requests", len(reqs))
	}
	today := time.Now().UTC().Format("2006-01-02")
	got := strings.Join(eventsOf(t, reqs[0].body), ",")
	// the clock may have crossed midnight between the calls; accept either single day
	want := fmt.Sprintf("f:1:%[1]s=3,s:1:%[1]s=1", today)
	if got != want {
		t.Logf("body %s", reqs[0].body)
		t.Fatalf("got %s want %s", got, want)
	}
	if reqs[0].header.Get("Content-Type") != "application/json" {
		t.Fatal("content type")
	}
	if strings.Contains(reqs[0].body, `"u"`) {
		t.Fatal("the usage body carries the context key")
	}
}

func TestFirstFlushIsOneIntervalAfterCreate(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(30*time.Second), withClock(clk))
	defer c.Close()
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clk.waitArmed(t, 2)
	clk.Advance(29 * time.Second)
	time.Sleep(30 * time.Millisecond)
	if f.count("/sdk/v1/events") != 0 {
		t.Fatal("flushed early")
	}
	clk.Advance(time.Second)
	eventually(t, "the first flush", func() bool { return f.count("/sdk/v1/events") == 1 })
	// an empty interval sends nothing
	clk.waitArmed(t, 2)
	clk.Advance(30 * time.Second)
	time.Sleep(30 * time.Millisecond)
	if f.count("/sdk/v1/events") != 1 {
		t.Fatal("an empty flush sent a request")
	}
}

func TestUTCMidnightGivesTwoDays(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC))
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withClock(clk))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clk.Advance(2 * time.Second)
	c.BoolVariation("f", user("u"), false)
	_ = c.Close()
	got := strings.Join(eventsOf(t, f.seen("/sdk/v1/events")[0].body), ",")
	if got != "f:1:2026-03-01=1,f:1:2026-03-02=1" {
		t.Fatalf("got %s", got)
	}
}

func TestAtMost500EntriesPerRequest(t *testing.T) {
	// 1200 distinct flag keys that are all missing would not count; use one ruleset with many flags.
	var sb strings.Builder
	sb.WriteString(`{"flags":[`)
	for i := 0; i < 1200; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"key":"k%d","version":1,"on":true,"salt":"a","variations":[false,true],"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1}}`, i)
	}
	sb.WriteString(`],"segments":[]}`)
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, sb.String()))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	c := newClient(t, f)
	c.Ready()
	for i := 0; i < 1200; i++ {
		c.BoolVariation(fmt.Sprintf("k%d", i), user("u"), false)
	}
	_ = c.Close()
	reqs := f.seen("/sdk/v1/events")
	if len(reqs) != 3 {
		t.Fatalf("%d requests", len(reqs))
	}
	total := 0
	for _, r := range reqs {
		n := len(eventsOf(t, r.body))
		if n > 500 {
			t.Fatalf("a request carried %d entries", n)
		}
		total += n
	}
	if total != 1200 {
		t.Fatalf("%d entries in all", total)
	}
}

func TestUsageResultTable(t *testing.T) {
	cases := []struct {
		status   int
		retained bool
		errCode  string
	}{
		{200, false, ""}, {202, false, ""}, {204, false, ""},
		{400, false, "http_400"}, {401, false, "http_401"}, {403, false, "http_403"}, {404, false, "http_404"}, {413, false, "http_413"},
		{408, true, "http_408"}, {429, true, "http_429"}, {500, true, "http_500"}, {503, true, "http_503"},
	}
	for _, tc := range cases {
		f := newFakeServer(t)
		f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
		f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) })
		log := &errorLog{}
		c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithOnError(log.add))
		c.Ready()
		c.BoolVariation("f", user("u"), false)
		if errs := c.safeFlush(false); tc.errCode == "" && len(errs) != 0 {
			t.Fatalf("%d: errors %v", tc.status, errs)
		} else if tc.errCode != "" && (len(errs) != 1 || errs[0].Error() != "toggleslash: usage request failed: "+tc.errCode) {
			t.Fatalf("%d: errors %v", tc.status, errs)
		}
		if kept := c.pendingEntries() > 0; kept != tc.retained {
			t.Fatalf("%d: retained=%v want %v", tc.status, kept, tc.retained)
		}
		c.usageHold = time.Time{} // a 429 with no Retry-After sets no hold
		_ = c.Close()
	}
}

func TestUsageNetworkAndTimeoutAreRetained(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	})
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), WithRequestTimeout(60*time.Millisecond))
	defer c.Close()
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	errs := c.safeFlush(false)
	if len(errs) != 1 || errs[0].Error() != "toggleslash: usage request failed: timeout" {
		t.Fatalf("%v", errs)
	}
	if c.pendingEntries() != 1 {
		t.Fatal("a timed out flush dropped its counts")
	}
}

func TestRetryAfterHoldsUsageAndFinalFlushReportsIt(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	})
	log := &errorLog{}
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(10*time.Second), withClock(clk), WithOnError(log.add))
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	clk.waitArmed(t, 2)
	clk.Advance(10 * time.Second)
	eventually(t, "the 429", func() bool { return f.count("/sdk/v1/events") == 1 })
	clk.waitArmed(t, 2)
	clk.Advance(10 * time.Second)
	time.Sleep(50 * time.Millisecond)
	if f.count("/sdk/v1/events") != 1 {
		t.Fatal("a flush went out inside the hold")
	}
	_ = c.Close()
	if f.count("/sdk/v1/events") != 1 {
		t.Fatal("the final flush waited out or ignored the hold")
	}
	errs := log.all()
	want := "toggleslash: closed with 1 usage counts unsent: the server asked to wait"
	if errs[len(errs)-1] != want {
		t.Fatalf("errors %v", errs)
	}
}

func TestRetryAfterCapOnEventsRoute(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400")
		w.WriteHeader(429)
	})
	c := New(f.URL, "k", WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withClock(clk))
	defer c.Close()
	c.Ready()
	c.BoolVariation("f", user("u"), false)
	c.safeFlush(false)
	if got := c.usageHold.Sub(clk.Now()); got != 300*time.Second {
		t.Fatalf("hold %v", got)
	}
}

func TestRetryAfterCapOnRulesetAndStreamRoutes(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	for _, route := range []string{"/sdk/v1/ruleset", "/sdk/v1/stream"} {
		f := newFakeServer(t)
		f.handle(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "86400")
			w.WriteHeader(429)
		})
		if route == "/sdk/v1/stream" {
			f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
		}
		c := New(f.URL, "k", WithStream(route == "/sdk/v1/stream"), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withClock(clk))
		c.Ready()
		eventually(t, route+" 429", func() bool { return c.Status().LastError == "http_429" })
		c.Close()
		// syncHold is only touched by the sync goroutine, which Close has waited for
		if got := c.syncHold.Sub(clk.Now()); got != 300*time.Second {
			t.Fatalf("%s: hold %v", route, got)
		}
	}
}

func TestLaterRetryAfterNeverShortensTheHold(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	c := &Client{cfg: defaultConfig()}
	c.cfg.clock = clk
	long, short := http.Header{}, http.Header{}
	long.Set("Retry-After", "100")
	short.Set("Retry-After", "5")
	c.holdSync(long)
	c.holdSync(short)
	if got := c.syncHold.Sub(clk.Now()); got != 100*time.Second {
		t.Fatalf("%v", got)
	}
}

func TestClosedCounterRacesAreSafe(t *testing.T) {
	var sent atomic.Int64
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { sent.Add(1); w.WriteHeader(202) })
	c := newClient(t, f, WithFlushInterval(time.Millisecond))
	c.Ready()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			c.BoolVariation("f", user("u"), false)
		}
		close(done)
	}()
	_ = c.Close()
	<-done
}

func TestNoContextInLogsOrErrors(t *testing.T) {
	f := newFakeServer(t)
	f.handle("/sdk/v1/ruleset", staticRuleset(`"a"`, flagRuleset))
	f.handle("/sdk/v1/events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	log := &errorLog{}
	var events []Event
	c := newClient(t, f, WithOnError(log.add), WithAfterEvaluation(func(e Event) { events = append(events, e) }))
	c.Ready()
	ctx := UserContext("secret-key-123", map[string]Value{"email": String("secret@example.com")})
	c.BoolVariation("f", ctx, false)
	c.safeFlush(false)
	_ = c.Close()
	for _, e := range log.all() {
		if strings.Contains(e, "secret") || strings.Contains(e, f.URL) || strings.Contains(e, "sdk-key") {
			t.Fatalf("error text leaks: %s", e)
		}
	}
	for _, r := range f.seen("/sdk/v1/events") {
		if strings.Contains(r.body, "secret") {
			t.Fatal("usage body leaks")
		}
	}
	if len(events) != 1 || events[0].FlagKey != "f" {
		t.Fatal("the hook did not run")
	}
}
