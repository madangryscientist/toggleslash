package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

type sentEntry struct {
	Flag  string
	Index int
	Count int64
	Day   string
}

// decodeEvents reads a usage request body with the SDK's own reader, so the test needs no other JSON package.
func decodeEvents(t *testing.T, body []byte) []sentEntry {
	t.Helper()
	root, err := jsonvalue.Parse(body)
	if err != nil {
		t.Fatalf("events body is not JSON: %v\n%s", err, body)
	}
	if root.Kind() != jsonvalue.KindObject || len(root.Members()) != 1 {
		t.Fatalf("events body shape: %s", body)
	}
	events, ok := root.Get("events")
	if !ok || events.Kind() != jsonvalue.KindArray {
		t.Fatalf("events is not an array: %s", body)
	}
	var out []sentEntry
	for _, item := range events.Elements() {
		if len(item.Members()) != 4 {
			t.Fatalf("an entry has %d members: %s", len(item.Members()), body)
		}
		flag, _ := item.Get("flag")
		idx, _ := item.Get("variationIndex")
		count, _ := item.Get("count")
		day, _ := item.Get("day")
		var e sentEntry
		e.Flag, _ = flag.AsString()
		i, _ := idx.AsNumber()
		n, _ := count.AsNumber()
		e.Index, e.Count = int(i), int64(n)
		e.Day, _ = day.AsString()
		out = append(out, e)
	}
	return out
}

func allSent(t *testing.T, f *fakeServer) []sentEntry {
	var out []sentEntry
	for _, b := range f.sentEvents() {
		out = append(out, decodeEvents(t, b)...)
	}
	return out
}

func TestUsageIsSummedPerFlagIndexAndDay(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1), boolFlag("b", false, 1))
	var clock atomic.Pointer[time.Time]
	set := func(s string) { tm, _ := time.Parse(time.RFC3339, s); clock.Store(&tm) }
	set("2026-11-11T23:59:59Z")
	c := newTestClient(t, f, WithFlushInterval(time.Hour), func(cfg *config) { cfg.now = func() time.Time { return *clock.Load() } })
	if !c.Ready() {
		t.Fatal("not ready")
	}
	c.BoolVariation("a", user, false)
	c.BoolVariation("a", NewContext("other", nil), false)
	c.BoolVariation("b", user, true)
	set("2026-11-12T00:00:00Z")
	c.BoolVariation("a", user, false)
	// A missing flag and a fallback are not counted.
	c.BoolVariation("missing", user, false)
	c.Close()

	got := map[sentEntry]bool{}
	for _, e := range allSent(t, f) {
		got[e] = true
	}
	want := []sentEntry{
		{"a", 1, 2, "2026-11-11"}, {"b", 0, 1, "2026-11-11"}, {"a", 1, 1, "2026-11-12"},
	}
	if len(got) != len(want) {
		t.Fatalf("sent %v", got)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing %+v in %v", w, got)
		}
	}
}

func TestUsageBodyCarriesNoContext(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	c.BoolVariation("a", NewContext("secret-user-key", map[string]any{"email": "someone@example.com"}), false)
	c.Close()
	for _, r := range f.requests("/sdk/v1/events") {
		s := string(r.Body)
		if strings.Contains(s, "secret-user-key") || strings.Contains(s, "someone@example.com") || strings.Contains(s, "email") {
			t.Fatalf("the usage body carries context: %s", s)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Method != http.MethodPost {
			t.Fatalf("method/type: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
	}
}

func TestUsageIsBatchedAt500Entries(t *testing.T) {
	var flags []string
	for i := range 501 {
		flags = append(flags, boolFlag(fmt.Sprintf("f%03d", i), true, 1))
	}
	f := newFakeServer(t, flags...)
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	for i := range 501 {
		c.BoolVariation(fmt.Sprintf("f%03d", i), user, false)
	}
	c.Close()
	bodies := f.sentEvents()
	if len(bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(bodies))
	}
	n0, n1 := len(decodeEvents(t, bodies[0])), len(decodeEvents(t, bodies[1]))
	if n0 != 500 || n1 != 1 {
		t.Fatalf("batch sizes %d and %d", n0, n1)
	}
}

func TestUsageOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		retry    string
		kept     bool
		held     bool
		errCount int
	}{
		{"200", 200, "", false, false, 0},
		{"204", 204, "", false, false, 0},
		{"500 is kept", 500, "", true, false, 1},
		{"503 is kept", 503, "", true, false, 1},
		{"408 is kept", 408, "", true, false, 1},
		{"429 is kept", 429, "", true, false, 1},
		{"429 with Retry-After holds", 429, "30", true, true, 1},
		{"400 is dropped", 400, "", false, false, 1},
		{"401 is dropped", 401, "", false, false, 1},
		{"404 is dropped", 404, "", false, false, 1},
		{"413 is dropped", 413, "", false, false, 1},
		{"302 is dropped", 302, "", false, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer(t, boolFlag("a", true, 1))
			var fail atomic.Bool
			fail.Store(true)
			f.eventsFn = func(w http.ResponseWriter, r *http.Request) {
				if fail.Load() {
					if tc.retry != "" {
						w.Header().Set("Retry-After", tc.retry)
					}
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(200)
			}
			errs := &errorLog{}
			c := newTestClient(t, f, WithFlushInterval(time.Hour), WithOnError(errs.add))
			c.Ready()
			c.BoolVariation("a", user, false)
			c.flush(&c.closerCB, false)
			if got := errs.count(); got != tc.errCount {
				t.Fatalf("onError %d times: %v", got, errs.texts())
			}
			if (c.holdLeft(&c.holdUsage) > 0) != tc.held {
				t.Fatalf("hold = %v", c.holdLeft(&c.holdUsage))
			}
			c.usageMu.Lock()
			pending := len(c.counts)
			c.usageMu.Unlock()
			if (pending > 0) != tc.kept {
				t.Fatalf("pending = %d, kept = %v", pending, tc.kept)
			}
			for _, e := range errs.all() {
				if strings.Contains(e.Error(), f.url()) || strings.Contains(e.Error(), testKey) {
					t.Fatalf("leaky error %q", e)
				}
			}
		})
	}
}

func TestADroppedBatchDoesNotStopTheFlush(t *testing.T) {
	var flags []string
	for i := range 501 {
		flags = append(flags, boolFlag(fmt.Sprintf("f%03d", i), true, 1))
	}
	f := newFakeServer(t, flags...)
	var n atomic.Int32
	f.eventsFn = func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(200)
	}
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	for i := range 501 {
		c.BoolVariation(fmt.Sprintf("f%03d", i), user, false)
	}
	c.flush(&c.closerCB, false)
	if got := n.Load(); got != 2 {
		t.Fatalf("%d requests, want 2 (a 400 drops its batch and the flush goes on)", got)
	}
}

func TestAKeptBatchStopsTheFlushAndIsResentWithNewCounts(t *testing.T) {
	var flags []string
	for i := range 501 {
		flags = append(flags, boolFlag(fmt.Sprintf("f%03d", i), true, 1))
	}
	f := newFakeServer(t, flags...)
	var fail atomic.Bool
	fail.Store(true)
	var n atomic.Int32
	f.eventsFn = func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		f.mu.Lock()
		f.events = append(f.events, nil)
		f.mu.Unlock()
		w.WriteHeader(200)
	}
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	for i := range 501 {
		c.BoolVariation(fmt.Sprintf("f%03d", i), user, false)
	}
	c.flush(&c.closerCB, false)
	if n.Load() != 1 {
		t.Fatalf("%d requests after a 503, want 1", n.Load())
	}
	c.BoolVariation("f000", user, false)
	c.usageMu.Lock()
	var total int64
	for _, v := range c.counts {
		total += v
	}
	pending := len(c.counts)
	c.usageMu.Unlock()
	if pending != 501 || total != 502 {
		t.Fatalf("pending %d entries, %d counts: the kept counts were lost or not merged", pending, total)
	}
}

func TestFlushesAreSerial(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	var inflight, peak atomic.Int32
	f.eventsFn = func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inflight.Add(-1)
		w.WriteHeader(200)
	}
	c := newTestClient(t, f, WithFlushInterval(5*time.Millisecond))
	c.Ready()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				c.BoolVariation("a", user, false)
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	c.Close()
	if peak.Load() != 1 {
		t.Fatalf("%d usage requests at once", peak.Load())
	}
}

func TestTheFinalFlushDoesNotWaitOutAHold(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1), boolFlag("b", true, 1))
	f.eventsFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}
	errs := &errorLog{}
	c := newTestClient(t, f, WithFlushInterval(30*time.Millisecond), WithOnError(errs.add))
	c.Ready()
	c.BoolVariation("a", user, false)
	eventually(t, 3*time.Second, "a 429", func() bool { return c.holdLeft(&c.holdUsage) > 0 })
	c.BoolVariation("b", user, false)
	n := len(f.requests("/sdk/v1/events"))
	start := time.Now()
	c.Close()
	if time.Since(start) > time.Second {
		t.Fatalf("Close waited %v", time.Since(start))
	}
	if got := len(f.requests("/sdk/v1/events")); got != n {
		t.Fatalf("the final flush sent a request during a hold (%d -> %d)", n, got)
	}
	want := "closed with 2 usage counts unsent: the server asked to wait"
	found := false
	for _, s := range errs.texts() {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("no %q in %v", want, errs.texts())
	}
}

func TestNothingIsCountedAfterCloseBegins(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	c.Close()
	if !c.BoolVariation("a", user, false) {
		t.Fatal("evaluation stopped answering after Close")
	}
	c.usageMu.Lock()
	pending := len(c.counts)
	c.usageMu.Unlock()
	if pending != 0 || len(f.sentEvents()) != 0 {
		t.Fatalf("counted after close: %d pending, %d sent", pending, len(f.sentEvents()))
	}
}

func TestAHungUsageRequestIsBoundedByTheRequestTimeout(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.eventsFn = hang
	errs := &errorLog{}
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(200*time.Millisecond), WithFlushInterval(time.Hour), WithOnError(errs.add))
	c.Ready()
	c.BoolVariation("a", user, false)
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Fatalf("Close took %v with a 200ms timeout", d)
	}
	if errs.count() == 0 {
		t.Fatal("a timed out flush was not reported")
	}
}
