package toggleslash

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

type eventsBody struct {
	Events []struct {
		Flag           string `json:"flag"`
		VariationIndex int    `json:"variationIndex"`
		Count          int64  `json:"count"`
		Day            string `json:"day"`
	} `json:"events"`
}

func decodeEvents(t *testing.T, body string) eventsBody {
	t.Helper()
	var out eventsBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("events body %q: %v", body, err)
	}
	return out
}

// quietClient has a flush interval so long that only an explicit flush or Close sends counts.
func quietClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	c := newClient(t, f, append([]Option{WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour)}, opts...)...)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	return c
}

func TestUsageSumsPerFlagIndexAndDay(t *testing.T) {
	f := newFakeServer(t)
	clock := newFakeClock(time.Date(2026, 11, 11, 23, 59, 58, 0, time.UTC))
	c := quietClient(t, f, withClock(clock.now))
	for i := 0; i < 3; i++ {
		c.BoolVariation("checkout", ctxOf("u"), false)
	}
	c.BoolVariation("legacy", ctxOf("u"), false)
	clock.advance(5 * time.Second) // UTC midnight passes
	for i := 0; i < 2; i++ {
		c.BoolVariation("checkout", ctxOf("u"), false)
	}
	_ = c.Close()
	reqs := f.requests("events")
	if len(reqs) != 1 {
		t.Fatalf("events requests = %d", len(reqs))
	}
	got := map[string]int64{}
	for _, e := range decodeEvents(t, reqs[0].body).Events {
		got[e.Flag+"/"+e.Day] += e.Count
	}
	want := map[string]int64{"checkout/2026-11-11": 3, "legacy/2026-11-11": 1, "checkout/2026-11-12": 2}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestUsageNotCountedForFallbacksOrMismatches(t *testing.T) {
	f := newFakeServer(t)
	c := quietClient(t, f)
	c.BoolVariation("nope", ctxOf("u"), false)      // flag not found
	c.StringVariation("checkout", ctxOf("u"), "x")  // type mismatch
	c.BoolVariation("checkout", Object(nil), false) // invalid context
	_ = c.Close()
	if n := len(f.requests("events")); n != 0 {
		t.Fatalf("sent %d events requests for nothing countable", n)
	}
}

func TestNothingCountedAfterCloseBegins(t *testing.T) {
	f := newFakeServer(t)
	c := quietClient(t, f)
	_ = c.Close()
	c.BoolVariation("checkout", ctxOf("u"), false)
	if c.pendingEntries() != 0 {
		t.Fatal("counted after close")
	}
}

func TestUsageChunksAt500Entries(t *testing.T) {
	f := newFakeServer(t)
	var flags []string
	for i := 0; i < 1100; i++ {
		flags = append(flags, flagJSON("f"+strings.Repeat("0", 4-len(itoa(i)))+itoa(i), "false", "true", 1))
	}
	f.mu.Lock()
	f.body = rulesetBody(flags...)
	f.mu.Unlock()
	c := quietClient(t, f)
	for i := 0; i < 1100; i++ {
		c.BoolVariation("f"+strings.Repeat("0", 4-len(itoa(i)))+itoa(i), ctxOf("u"), false)
	}
	_ = c.Close()
	reqs := f.requests("events")
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	sizes := []int{}
	for _, r := range reqs {
		sizes = append(sizes, len(decodeEvents(t, r.body).Events))
	}
	if sizes[0] != 500 || sizes[1] != 500 || sizes[2] != 100 {
		t.Fatalf("sizes = %v", sizes)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestUsageOutcomes(t *testing.T) {
	cases := []struct {
		status   int
		retained bool
	}{{200, false}, {204, false}, {500, true}, {503, true}, {408, true}, {429, true}, {400, false}, {401, false}, {413, false}}
	for _, tc := range cases {
		f := newFakeServer(t)
		f.set("events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) })
		c := quietClient(t, f)
		c.BoolVariation("checkout", ctxOf("u"), false)
		errs := c.flush(false)
		if (c.pendingEntries() == 1) != tc.retained {
			t.Errorf("status %d: pending = %d, retained want %v", tc.status, c.pendingEntries(), tc.retained)
		}
		if (tc.status >= 400) != (len(errs) == 1) {
			t.Errorf("status %d: errors = %v", tc.status, errs)
		}
	}
}

func TestUsageKeepsCountsOnNetworkErrorAndSendsThemLater(t *testing.T) {
	f := newFakeServer(t)
	var fail atomic.Bool
	fail.Store(true)
	f.set("events", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	})
	c := quietClient(t, f)
	c.BoolVariation("checkout", ctxOf("u"), false)
	c.BoolVariation("checkout", ctxOf("u"), false)
	c.flush(false)
	fail.Store(false)
	c.BoolVariation("checkout", ctxOf("u"), false)
	c.flush(false)
	reqs := f.requests("events")
	last := decodeEvents(t, reqs[len(reqs)-1].body)
	if len(last.Events) != 1 || last.Events[0].Count != 3 {
		t.Fatalf("retry body = %+v", last)
	}
}

func Test429RetryAfterIsCappedAndHeldOnEveryRoute(t *testing.T) {
	for _, route := range []string{"ruleset", "stream", "events"} {
		t.Run(route, func(t *testing.T) {
			f := newFakeServer(t)
			clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
			f.set(route, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", " 99999 ")
				w.WriteHeader(429)
			})
			c := newClient(t, f, withClock(clock.now), withSleep(func(ctx context.Context, d time.Duration) bool {
				return clock.sleep(ctx, d)
			}), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
			if route == "events" {
				c.Ready()
				c.BoolVariation("checkout", ctxOf("u"), false)
				c.flush(false)
				if d := c.usageHold.remaining(clock.now()); d != 300*time.Second {
					t.Fatalf("events hold = %v, want 300s", d)
				}
				return
			}
			eventually(t, 3*time.Second, "a held wait", func() bool {
				for _, d := range clock.recorded() {
					if d >= 300*time.Second {
						return true
					}
				}
				return false
			})
			for _, d := range clock.recorded() {
				if d > 300*time.Second+time.Hour {
					t.Fatalf("wait %v exceeds the cap", d)
				}
			}
			if d := c.syncHold.remaining(clock.now()); d > 300*time.Second {
				t.Fatalf("hold %v above the cap", d)
			}
		})
	}
}

func TestRetryAfterParsing(t *testing.T) {
	c := &Client{cfg: config{retryAfterCap: 300 * time.Second}}
	cases := []struct {
		in string
		d  time.Duration
		ok bool
	}{
		{"5", 5 * time.Second, true}, {" 7 ", 7 * time.Second, true}, {"0", 0, true},
		{"300", 300 * time.Second, true}, {"301", 300 * time.Second, true},
		{"99999999999999999999999", 300 * time.Second, true},
		{"", 0, false}, {"1.5", 0, false}, {"-1", 0, false}, {"abc", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false}, {"1e3", 0, false}, {"+5", 0, false},
	}
	for _, tc := range cases {
		h := http.Header{}
		h.Set("Retry-After", tc.in)
		d, ok := c.retryAfter(h)
		if d != tc.d || ok != tc.ok {
			t.Errorf("%q: got %v %v, want %v %v", tc.in, d, ok, tc.d, tc.ok)
		}
	}
}

func TestA429WithoutRetryAfterOnEventsKeepsCountsWithNoHold(t *testing.T) {
	f := newFakeServer(t)
	f.set("events", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) })
	c := quietClient(t, f)
	c.BoolVariation("checkout", ctxOf("u"), false)
	c.flush(false)
	if c.pendingEntries() != 1 || c.usageHold.remaining(time.Now()) != 0 {
		t.Fatalf("pending %d, hold %v", c.pendingEntries(), c.usageHold.remaining(time.Now()))
	}
}

func TestFinalFlushDoesNotWaitOutAHold(t *testing.T) {
	f := newFakeServer(t)
	f.set("events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	})
	log := &errorLog{}
	c := quietClient(t, f, WithOnError(log.add))
	c.BoolVariation("checkout", ctxOf("u"), false)
	c.flush(false) // earns the hold
	start := time.Now()
	_ = c.Close()
	if time.Since(start) > time.Second {
		t.Fatalf("Close waited %v", time.Since(start))
	}
	found := false
	for _, m := range log.messages() {
		if strings.Contains(m, "closed with 1 usage counts unsent") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lost counts not reported: %v", log.messages())
	}
	if n := len(f.requests("events")); n != 1 {
		t.Fatalf("events requests = %d, want 1 (no final request under a hold)", n)
	}
}

func TestPeriodicFlushSendsCounts(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithFlushInterval(30*time.Millisecond))
	c.Ready()
	c.BoolVariation("checkout", ctxOf("u"), false)
	eventually(t, 3*time.Second, "a periodic flush", func() bool { return len(f.requests("events")) >= 1 })
}

func TestEventsBodyHoldsNoContext(t *testing.T) {
	f := newFakeServer(t)
	c := quietClient(t, f)
	c.BoolVariation("checkout", Object(map[string]Value{"key": String("secret-user-key"), "email": String("a@b.c")}), false)
	_ = c.Close()
	for _, r := range f.all() {
		if strings.Contains(r.body, "secret-user-key") || strings.Contains(r.header.Get("Authorization"), "secret-user-key") {
			t.Fatalf("%s request carried the context", r.route)
		}
		for name, vals := range r.header {
			for _, v := range vals {
				if strings.Contains(v, "secret-user-key") {
					t.Fatalf("header %s carried the context", name)
				}
			}
		}
		if strings.Contains(r.route, "secret-user-key") {
			t.Fatal("route carried the context")
		}
	}
}

func TestEvaluationFailureHasOneOnErrorAndNoCount(t *testing.T) {
	f := newFakeServer(t)
	log := &errorLog{}
	c := quietClient(t, f, WithOnError(log.add), withEvaluator(func(*evaluator.Ruleset, string, Context) evaluator.Result {
		panic("boom with secret-user-key")
	}))
	d := c.BoolVariationDetail("checkout", ctxOf("secret-user-key"), true)
	if d.Reason.Kind != ReasonError || d.Reason.ErrorCode != ErrEvaluationFailed || d.VariationIndex != NoVariation || !d.Value {
		t.Fatalf("detail = %+v", d)
	}
	if log.count() != 1 {
		t.Fatalf("onError calls = %d, want 1", log.count())
	}
	if m := log.messages()[0]; strings.Contains(m, "secret-user-key") {
		t.Fatalf("onError text carries the context: %q", m)
	}
	if c.pendingEntries() != 0 {
		t.Fatal("a failed evaluation was counted")
	}
}

func TestIntAccessorRange(t *testing.T) {
	f := newFakeServer(t)
	f.mu.Lock()
	f.body = rulesetBody(
		flagJSON("whole", "1", "2.0", 1),
		flagJSON("frac", "1", "2.5", 1),
		flagJSON("big", "1", "9223372036854775808", 1),
		flagJSON("max", "1", "9007199254740992", 1),
		flagJSON("neg", "1", "-9223372036854775808", 1),
	)
	f.mu.Unlock()
	c := quietClient(t, f)
	cases := []struct {
		key  string
		want int64
		code ErrorCode
	}{
		{"whole", 2, ""}, {"frac", 7, ErrTypeMismatch}, {"big", 7, ErrTypeMismatch},
		{"max", 9007199254740992, ""}, {"neg", -9223372036854775808, ""},
	}
	for _, tc := range cases {
		d := c.IntVariationDetail(tc.key, ctxOf("u"), 7)
		if d.Value != tc.want || d.Reason.ErrorCode != tc.code {
			t.Errorf("%s: %+v, want %d %q", tc.key, d, tc.want, tc.code)
		}
	}
}
