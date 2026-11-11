package toggleslash

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

func TestServesFromTheRulesetAndTypedAccessors(t *testing.T) {
	f := newFake(t,
		boolFlag("on", true, 3),
		valueFlag("name", `"blue"`),
		valueFlag("size", `12.5`),
		valueFlag("config", `{"a":1}`),
	)
	c := newClient(t, f.url(), WithStream(false))
	if !c.Ready() {
		t.Fatal("ready is false")
	}
	if !c.BoolVariation("on", user, false) {
		t.Error("bool flag answered the fallback")
	}
	if got := c.StringVariation("name", user, "x"); got != "blue" {
		t.Errorf("string = %q", got)
	}
	if got := c.NumberVariation("size", user, 0); got != 12.5 {
		t.Errorf("number = %v", got)
	}
	if got := c.JSONVariation("config", user, Null()); got.Field("a").Number() != 1 {
		t.Errorf("json = %s", MarshalJSON(got))
	}
	d := c.BoolVariationDetail("on", user, false)
	if d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Errorf("detail = %+v", d)
	}
	// An unknown flag answers the fallback with the evaluator's reason, and no index.
	m := c.BoolVariationDetail("nope", user, true)
	if !m.Value || m.VariationIndex != NoVariation || m.Reason.Code != FlagNotFound {
		t.Errorf("unknown flag = %+v", m)
	}
	// A kind mismatch is TYPE_MISMATCH.
	s := c.StringVariationDetail("on", user, "fb")
	if s.Value != "fb" || s.VariationIndex != NoVariation || s.Reason.Code != TypeMismatch {
		t.Errorf("mismatch = %+v", s)
	}
	// A null fallback accepts any kind; a null served value against a non-null fallback is a mismatch.
	if got := c.Variation("name", user, Null()); got.String() != "blue" {
		t.Errorf("null fallback = %s", MarshalJSON(got))
	}
}

func TestNotReadyBeforeTheFirstLoad(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onRuleset = f.hang
	c := newClient(t, f.url(), WithStream(false), WithRequestTimeout(300*time.Millisecond))
	d := c.VariationDetail("on", user, Bool(false))
	if d.Reason.Code != NotReady || d.VariationIndex != NoVariation || d.Value.Bool() {
		t.Errorf("before the load = %+v", d)
	}
	if c.Ready() {
		t.Error("ready is true for a server that never answered")
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Errorf("lastError = %q", got)
	}
}

func TestIntAccessorRange(t *testing.T) {
	f := newFake(t,
		valueFlag("two", `2.0`),
		valueFlag("frac", `1.5`),
		valueFlag("big", `9223372036854774784`),  // 2^63 - 1024, the largest a binary64 holds in range
		valueFlag("edge", `9223372036854775807`), // reads as 2^63, out of range
		valueFlag("min", `-9223372036854775808`),
		valueFlag("below", `-9223372036854777856`),
		valueFlag("inf", `1e999`),
	)
	c := newClient(t, f.url(), WithStream(false))
	c.Ready()
	cases := []struct {
		key  string
		want int64
		code ErrorCode
	}{
		{"two", 2, ""},
		{"frac", -7, TypeMismatch},
		{"big", 9223372036854774784, ""},
		{"edge", -7, TypeMismatch},
		{"min", math.MinInt64, ""},
		{"below", -7, TypeMismatch},
		{"inf", -7, TypeMismatch},
	}
	for _, tc := range cases {
		d := c.IntVariationDetail(tc.key, user, -7)
		if d.Value != tc.want || d.Reason.Code != tc.code {
			t.Errorf("%s: got %d %+v, want %d %q", tc.key, d.Value, d.Reason, tc.want, tc.code)
		}
		if tc.code == TypeMismatch && d.VariationIndex != NoVariation {
			t.Errorf("%s: index %d on a mismatch", tc.key, d.VariationIndex)
		}
	}
	// The generic accessor does not apply the integer rule: 1.5 is a number.
	if got := c.Variation("frac", user, Number(0)); got.Number() != 1.5 {
		t.Errorf("generic = %s", MarshalJSON(got))
	}
}

func TestEvaluationFailedReportsExactlyOnce(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var log errorLog
	var hooked atomic.Int32
	c := newClient(t, f.url(), WithStream(false),
		WithOnError(log.add),
		WithAfterEvaluation(func(EvaluationEvent) { hooked.Add(1) }),
		withEvaluate(func(*evaluator.Ruleset, string, Context) evaluator.Result { panic("boom") }),
	)
	c.Ready()
	d := c.VariationDetail("on", user, Bool(true))
	if d.Reason.Code != EvaluationFailed || d.VariationIndex != NoVariation || !d.Value.Bool() {
		t.Errorf("detail = %+v", d)
	}
	if n := log.count(); n != 1 {
		t.Fatalf("onError calls = %d, want 1", n)
	}
	e := log.all()[0]
	var pe *PanicError
	if e.Source != SourceEvaluator || !errors.As(e, &pe) || pe.Value != "boom" {
		t.Errorf("error = %#v", e)
	}
	if hooked.Load() != 1 {
		t.Errorf("hook calls = %d", hooked.Load())
	}
	c.Close()
	if n := log.count(); n != 1 {
		t.Errorf("onError calls after close = %d, a failed evaluation counts nothing", n)
	}
}

func TestHookPanicGoesToOnErrorAndOnErrorPanicIsIgnored(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var calls atomic.Int32
	c := newClient(t, f.url(), WithStream(false),
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }),
		WithOnError(func(error) { calls.Add(1); panic("onError") }),
	)
	c.Ready()
	if got := c.BoolVariation("on", user, false); !got {
		t.Error("a panicking hook changed the answer")
	}
	if calls.Load() != 1 {
		t.Errorf("onError calls = %d", calls.Load())
	}
}

func TestHookEventFields(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 7))
	var mu sync.Mutex
	var events []EvaluationEvent
	c := newClient(t, f.url(), WithStream(false), WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}))
	// Before the load: no index, no tag, no version.
	c.Variation("on", user, Bool(false))
	c.Ready()
	c.Variation("on", user, Bool(false))
	c.Variation("missing", user, Bool(false))
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("events = %d", len(events))
	}
	if e := events[0]; e.ETag != "" || e.FlagVersion != nil || e.VariationIndex != NoVariation {
		// The first call may race the load; only a not-ready result has these.
		if e.Reason.Code == NotReady {
			t.Errorf("not-ready event = %+v", e)
		}
	}
	if e := events[1]; e.ETag != `"v1"` || e.FlagVersion == nil || *e.FlagVersion != 7 || e.VariationIndex != 1 {
		t.Errorf("served event = %+v", e)
	}
	if e := events[2]; e.FlagVersion != nil || e.Reason.Code != FlagNotFound {
		t.Errorf("unknown flag event = %+v", e)
	}
}

func TestHookETagAndVersionStayPairedUnderSwaps(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var seenMu sync.Mutex
	seen := map[string]bool{}
	// Polling, because the fake's stream never announces a change: the swaps arrive by the poll.
	c := newClient(t, f.url(), WithStream(false), WithPollInterval(20*time.Millisecond), WithAfterEvaluation(func(e EvaluationEvent) {
		if e.FlagVersion == nil {
			return
		}
		seenMu.Lock()
		seen[e.ETag] = true
		seenMu.Unlock()
		if want := fmt.Sprintf(`"v%d"`, *e.FlagVersion); e.ETag != want {
			panic(fmt.Sprintf("etag %s with version %d", e.ETag, *e.FlagVersion))
		}
	}), WithOnError(func(err error) {
		var e *Error
		if errors.As(err, &e) && e.Source == SourceHook {
			t.Errorf("unpaired: %v", e.Err)
		}
	}))
	c.Ready()
	const workers = 8
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var calls [workers]atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.Variation("on", user, Bool(false))
					calls[i].Add(1)
				}
			}
		}()
	}
	// The rounds are a fixed number, so the test then waits for what it does not control: the client
	// to reach the last ruleset, and every worker to make calls after that.
	last := 39
	for v := 2; v <= last; v++ {
		f.set(boolFlag("on", v%2 == 0, v))
		time.Sleep(10 * time.Millisecond)
	}
	waitFor(t, "the last ruleset", 10*time.Second, func() bool {
		return c.Status().ETag == fmt.Sprintf(`"v%d"`, last)
	})
	var after [workers]int64
	for i := range calls {
		after[i] = calls[i].Load()
	}
	waitFor(t, "every worker to call after the last swap", 10*time.Second, func() bool {
		for i := range calls {
			if calls[i].Load() <= after[i] {
				return false
			}
		}
		return true
	})
	close(stop)
	wg.Wait()
	seenMu.Lock()
	defer seenMu.Unlock()
	if len(seen) < 2 {
		t.Errorf("the hook saw %d rulesets, want at least 2", len(seen))
	}
}

func TestVariationNeverWaitsOnTheNetwork(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithPollInterval(20*time.Millisecond),
		WithFlushInterval(20*time.Millisecond), WithRequestTimeout(1500*time.Millisecond))
	c.Ready()
	// From now on every ruleset request and every usage flush hangs.
	f.mu.Lock()
	f.onRuleset = f.hang
	f.onEvents = f.hang
	f.mu.Unlock()
	c.Variation("on", user, Bool(false))
	time.Sleep(200 * time.Millisecond) // a poll and a flush are now stuck
	start := time.Now()
	for i := 0; i < 2000; i++ {
		if !c.BoolVariation("on", user, false) {
			t.Fatal("the held ruleset stopped answering")
		}
		_ = c.Status()
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("2000 evaluations took %v while requests hung", took)
	}
	// Close is bounded by about twice the request timeout.
	start = time.Now()
	c.Close()
	if took := time.Since(start); took > 3500*time.Millisecond {
		t.Errorf("close took %v", took)
	}
}

func TestOutOfRangeDurationsFallBackToTheDefaults(t *testing.T) {
	cfg := defaultConfig()
	for _, o := range []Option{WithPollInterval(0), WithFlushInterval(-1), WithRequestTimeout(-time.Second)} {
		o(&cfg)
	}
	if cfg.pollInterval != DefaultPollInterval || cfg.flushInterval != DefaultFlushInterval || cfg.requestTimeout != DefaultRequestTimeout {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestTLS(t *testing.T) {
	f := newFakeTLS(t, boolFlag("on", true, 1))
	var log errorLog
	c := newClient(t, f.url(), WithStream(true), WithHTTPClient(f.srv.Client()), WithOnError(log.add))
	if !c.Ready() {
		t.Fatalf("no ruleset over https: %v", log.all())
	}
	waitFor(t, "the stream over https", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	c.Variation("on", user, Bool(false))
	c.Close()
	if n := len(f.routeRequests("/sdk/v1/events")); n != 1 {
		t.Errorf("usage requests over https = %d", n)
	}
	// Without the test CA the same server is a network error, never a panic.
	bad := newClient(t, f.url(), WithStream(false), WithOnError(log.add))
	if bad.Ready() {
		t.Error("an unknown CA was accepted")
	}
	if got := bad.Status().LastError; got != SyncNetwork {
		t.Errorf("lastError = %q", got)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
		return true
	}
	c := newClient(t, f.url(), WithStream(false))
	if c.Ready() {
		t.Error("a redirect gave a ruleset")
	}
	if got := c.Status().LastError; got != "http_302" {
		t.Errorf("lastError = %q", got)
	}
	if n := len(f.routeRequests("/elsewhere")); n != 0 {
		t.Errorf("the redirect was followed %d times", n)
	}
}

func TestBadRulesetKeepsTheLastGoodOne(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	c := newClient(t, f.url(), WithStream(false), WithPollInterval(20*time.Millisecond))
	c.Ready()
	for _, body := range []string{`{"flags":{},"segments":[]}`, `{"flags":[]}`, `[]`, `nope`, ``} {
		f.mu.Lock()
		f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
			w.Header().Set("ETag", `"bad"`)
			_, _ = w.Write([]byte(body))
			return true
		}
		f.mu.Unlock()
		waitFor(t, "bad_ruleset for "+body, 3*time.Second, func() bool { return c.Status().LastError == SyncBadRuleset })
		if st := c.Status(); st.ETag != `"v1"` {
			t.Errorf("a bad ruleset replaced the tag: %+v", st)
		}
		if !c.BoolVariation("on", user, false) {
			t.Error("a bad ruleset replaced the flags")
		}
		f.mu.Lock()
		f.onRuleset = nil
		f.mu.Unlock()
		waitFor(t, "recovery", 3*time.Second, func() bool { return c.Status().LastError == "" })
	}
}

func TestBodyWithBOMInvalidUTF8AndDuplicateKeys(t *testing.T) {
	f := newFake(t)
	flag := strings.Replace(boolFlag("on", false, 1), `"salt":"s"`, `"salt":"s","salt":"s"`, 1)
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("ETag", `"x"`)
		// A byte order mark, a duplicate key (the last wins) and a byte that is not UTF-8.
		_, _ = w.Write([]byte("\xef\xbb\xbf{\"flags\":[" + flag + "],\"segments\":[],\"note\":\"\xff\",\"flags\":[" + boolFlag("on", true, 1) + "]}"))
		return true
	}
	c := newClient(t, f.url(), WithStream(false))
	if !c.Ready() {
		t.Fatalf("not ready: %+v", c.Status())
	}
	if !c.BoolVariation("on", user, false) {
		t.Error("the first duplicate key won")
	}
}

func TestReentrantCallbacksDoNotDeadlock(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onRuleset = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusInternalServerError)
		return true
	}
	var self atomic.Pointer[Client]
	var ran atomic.Int32
	done := make(chan struct{})
	onError := func(err error) {
		c := self.Load()
		if c == nil {
			// The callback ran before New returned.
			for self.Load() == nil {
				time.Sleep(time.Millisecond)
			}
			c = self.Load()
		}
		c.Ready()
		_ = c.Status()
		c.Variation("on", user, Bool(false))
		c.Close()
		c.Close()
		if ran.Add(1) == 1 {
			close(done)
		}
	}
	c := New(f.url(), "k", WithStream(false), WithOnError(onError), WithAfterEvaluation(func(EvaluationEvent) {
		if cc := self.Load(); cc != nil {
			cc.Ready()
			cc.Status()
		}
	}))
	self.Store(c)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: a callback that calls Ready and Close never returned")
	}
	finished := make(chan struct{})
	go func() { c.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Close from outside did not return")
	}
	if st := c.Status(); st.Stream != StreamClosed {
		t.Errorf("stream = %q", st.Stream)
	}
}

func TestOnErrorOfTheFinalFlushMayCallClose(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onEvents = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusInternalServerError)
		return true
	}
	var self atomic.Pointer[Client]
	var calls atomic.Int32
	c := New(f.url(), "k", WithStream(false), WithOnError(func(error) {
		calls.Add(1)
		if cc := self.Load(); cc != nil {
			cc.Close()
		}
	}))
	self.Store(c)
	c.Ready()
	c.Variation("on", user, Bool(false))
	done := make(chan struct{})
	go func() { c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close from the final flush's onError never returned")
	}
	if calls.Load() == 0 {
		t.Error("the failed final flush did not reach onError")
	}
}

func TestManyGoroutinesWithCloseRacing(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1), boolFlag("off", false, 1))
	c := New(f.url(), "k", WithStream(true), WithPollInterval(10*time.Millisecond),
		WithFlushInterval(10*time.Millisecond), WithAfterEvaluation(func(EvaluationEvent) {}))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for j := 0; j < 400; j++ {
				switch (i + j) % 6 {
				case 0, 1, 2:
					c.Variation("on", user, Bool(false))
				case 3:
					c.IntVariationDetail("off", user, 1)
				case 4:
					_ = c.Status()
				case 5:
					if j%50 == 5 {
						f.set(boolFlag("on", j%2 == 0, j), boolFlag("off", false, j))
					}
					_ = c.Ready()
				}
			}
		}(i)
	}
	close(start)
	time.Sleep(30 * time.Millisecond)
	var closers sync.WaitGroup
	for i := 0; i < 4; i++ {
		closers.Add(1)
		go func() { defer closers.Done(); c.Close() }()
	}
	wg.Wait()
	closers.Wait()
	if st := c.Status(); st.Stream != StreamClosed {
		t.Errorf("stream = %q", st.Stream)
	}
	// After close the client still answers from the last ruleset.
	if d := c.VariationDetail("on", user, Bool(false)); d.Reason.Code == NotReady {
		t.Error("no answer after close")
	}
}

func TestTwoClosesRaceAndShareOneShutdown(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var posts atomic.Int32
	f.onEvents = func(w http.ResponseWriter, r *http.Request) bool {
		posts.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		return true
	}
	c := New(f.url(), "k", WithStream(false))
	c.Ready()
	c.Variation("on", user, Bool(false))
	finished := make(chan time.Time, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close(); finished <- time.Now() }()
	}
	wg.Wait()
	// Each Close returned only after the one final flush was done.
	if posts.Load() != 1 {
		t.Errorf("final flush requests = %d, want 1", posts.Load())
	}
	if got := len(f.routeRequests("/sdk/v1/events")); got != 1 {
		t.Errorf("usage requests = %d", got)
	}
}

func TestReadyAfterCloseBeforeTheFirstLoadIsFalse(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	f.onRuleset = f.hang
	c := New(f.url(), "k", WithStream(false), WithRequestTimeout(5*time.Second))
	done := make(chan bool, 1)
	go func() { done <- c.Ready() }()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	c.Close()
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("close waited out the hung request: %v", took)
	}
	select {
	case ok := <-done:
		if ok {
			t.Error("ready is true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ready never settled")
	}
}

func TestNothingIsCountedOrReportedForAbandonedRequestsAfterClose(t *testing.T) {
	f := newFake(t, boolFlag("on", true, 1))
	var log errorLog
	c := New(f.url(), "k", WithStream(true), WithOnError(log.add))
	c.Ready()
	waitFor(t, "stream", 3*time.Second, func() bool { return c.Status().Stream == StreamConnected })
	c.Close()
	c.Variation("on", user, Bool(false))
	time.Sleep(50 * time.Millisecond)
	if n := log.count(); n != 0 {
		t.Errorf("onError after close: %v", log.all())
	}
	if n := len(f.routeRequests("/sdk/v1/events")); n != 0 {
		t.Errorf("a count made after close was sent: %d", n)
	}
	if st := c.Status(); st.Stream != StreamClosed || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
}
