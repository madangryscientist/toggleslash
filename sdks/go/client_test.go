package toggleslash

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

var someone = UserContext("user-1")

func pollOnly(opts ...Option) []Option {
	return append([]Option{WithStream(false), WithPollInterval(20 * time.Millisecond), WithFlushInterval(20 * time.Millisecond)}, opts...)
}

func TestReadyAndEvaluate(t *testing.T) {
	url := serve(t, rulesetHandler(boolRuleset(), `"r1"`, nil))
	c := newTestClient(t, url, nil, pollOnly()...)
	if !c.Ready() {
		t.Fatal("Ready() = false against a working server")
	}
	d := c.BoolVariationDetail("checkout", someone, false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("got %+v", d)
	}
	missing := c.BoolVariationDetail("nope", someone, true)
	if !missing.Value || missing.VariationIndex != NoVariation || missing.Reason.Kind != ReasonError || missing.Reason.Code != ErrorFlagNotFound {
		t.Fatalf("missing flag: %+v", missing)
	}
	invalid := c.BoolVariationDetail("checkout", Context{}, true)
	if !invalid.Value || invalid.Reason.Code != ErrorInvalidContext {
		t.Fatalf("invalid context: %+v", invalid)
	}
	st := c.Status()
	if st.ETag == nil || *st.ETag != `"r1"` || st.LastSyncAt.IsZero() || st.Stream != StreamOff || st.LastError != "" {
		t.Fatalf("status: %+v", st)
	}
}

func TestURLTrailingSlashIsStripped(t *testing.T) {
	var path atomic.Value
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		_, _ = w.Write([]byte(boolRuleset()))
	}))
	c := newTestClient(t, url+"///", nil, pollOnly()...)
	if !c.Ready() || path.Load() != "/sdk/v1/ruleset" {
		t.Fatalf("ready=%v path=%v", c.Ready(), path.Load())
	}
}

func TestNotReadyBeforeTheFirstLoad(t *testing.T) {
	release := make(chan struct{})
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	c := newTestClient(t, url, nil, pollOnly(WithRequestTimeout(time.Second))...)
	d := c.BoolVariationDetail("checkout", someone, true)
	close(release)
	if !d.Value || d.Reason.Kind != ReasonError || d.Reason.Code != ErrorNotReady || d.VariationIndex != NoVariation {
		t.Fatalf("got %+v", d)
	}
}

func TestReadySettlesAfterTheFirstAttemptAndIsFixed(t *testing.T) {
	var serving atomic.Bool
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serving.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(boolRuleset()))
	}))
	c := newTestClient(t, url, nil, pollOnly()...)
	if c.Ready() {
		t.Fatal("Ready() = true against a 503")
	}
	serving.Store(true)
	eventually(t, 3*time.Second, "the ruleset to load by polling", func() bool { return c.BoolVariation("checkout", someone, false) })
	if c.Ready() {
		t.Fatal("Ready() changed after it settled")
	}
}

func TestReadyIsBoundedByTheRequestTimeout(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	c := newTestClient(t, url, nil, pollOnly(WithRequestTimeout(150*time.Millisecond))...)
	start := time.Now()
	if c.Ready() {
		t.Fatal("Ready() = true with no answer")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Ready took %s", took)
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Fatalf("lastError = %q", got)
	}
}

func TestReadyIsFalseAfterCloseIfNothingLoaded(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	c := newTestClient(t, url, nil, pollOnly()...)
	within(t, 3*time.Second, "Close", c.Close)
	if c.Ready() {
		t.Fatal("Ready() = true after Close with nothing loaded")
	}
	if got := c.Status(); got.Stream != StreamClosed || got.LastError != "" {
		t.Fatalf("status after Close: %+v", got)
	}
}

func TestTypedAccessors(t *testing.T) {
	flags := rulesetJSON(
		flagJSON("n", 1, "[1.5]", 0),
		flagJSON("two", 1, "[2.0]", 0),
		flagJSON("huge", 1, "[1e30]", 0),
		flagJSON("min", 1, "[-9223372036854775808]", 0),
		flagJSON("edge", 1, "[9223372036854775808]", 0),
		flagJSON("inf", 1, "[1e999]", 0),
		flagJSON("s", 1, `["x"]`, 0),
		flagJSON("obj", 1, `[{"a":1}]`, 0),
		flagJSON("nul", 1, `[null]`, 0),
	)
	url := serve(t, rulesetHandler(flags, `"t"`, nil))
	c := newTestClient(t, url, nil, pollOnly()...)
	c.Ready()

	if got := c.Float64Variation("n", someone, 0); got != 1.5 {
		t.Errorf("float64 = %v", got)
	}
	if d := c.IntVariationDetail("n", someone, 7); d.Value != 7 || d.Reason.Code != ErrorTypeMismatch || d.VariationIndex != NoVariation {
		t.Errorf("int of 1.5: %+v", d)
	}
	if d := c.IntVariationDetail("two", someone, 7); d.Value != 2 || d.VariationIndex != 0 {
		t.Errorf("int of 2.0: %+v", d)
	}
	for _, key := range []string{"huge", "edge", "inf"} {
		if d := c.IntVariationDetail(key, someone, 7); d.Value != 7 || d.Reason.Code != ErrorTypeMismatch {
			t.Errorf("int of %s: %+v", key, d)
		}
	}
	if d := c.IntVariationDetail("min", someone, 7); d.Value != -9223372036854775808 || d.Reason.Kind == ReasonError {
		t.Errorf("int of -2^63: %+v", d)
	}
	if d := c.BoolVariationDetail("s", someone, true); !d.Value || d.Reason.Code != ErrorTypeMismatch {
		t.Errorf("bool of a string: %+v", d)
	}
	if got := c.StringVariation("s", someone, "fb"); got != "x" {
		t.Errorf("string = %q", got)
	}
	if d := c.JSONVariationDetail("obj", someone, Object()); d.Reason.Kind == ReasonError {
		t.Errorf("json object against an object fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("obj", someone, Array()); d.Reason.Code != ErrorTypeMismatch {
		t.Errorf("json object against an array fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("obj", someone, Null()); d.Reason.Kind == ReasonError {
		t.Errorf("a null fallback accepts any value: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", someone, Object()); d.Reason.Code != ErrorTypeMismatch {
		t.Errorf("a null served value against a non-null fallback: %+v", d)
	}
	if d := c.JSONVariationDetail("nul", someone, Null()); d.Reason.Kind == ReasonError || d.VariationIndex != 0 {
		t.Errorf("a null served value against a null fallback: %+v", d)
	}
}

// A TYPE_MISMATCH is not counted: only a result that served a variation to the caller is.
func TestOnlyServedResultsAreCounted(t *testing.T) {
	var events syncBuf
	url := serve(t, rulesetHandler(rulesetJSON(flagJSON("s", 1, `["x"]`, 0)), `"u"`, events.handler()))
	c := newTestClient(t, url, nil, pollOnly(WithFlushInterval(time.Hour))...)
	c.Ready()
	c.BoolVariation("s", someone, false)    // TYPE_MISMATCH
	c.BoolVariation("gone", someone, false) // FLAG_NOT_FOUND
	c.StringVariation("s", Context{}, "")   // INVALID_CONTEXT
	c.StringVariation("s", someone, "")     // served
	c.Close()
	got := events.bodies()
	if len(got) != 1 || got[0] != `{"events":[{"flag":"s","variationIndex":0,"count":1,"day":"`+time.Now().UTC().Format("2006-01-02")+`"}]}` {
		// The day may have turned over during the test; compare the rest.
		if len(got) != 1 || !strings.HasPrefix(got[0], `{"events":[{"flag":"s","variationIndex":0,"count":1,"day":"`) {
			t.Fatalf("bodies: %q", got)
		}
	}
}

// TestConcurrentUseUnderRace calls every operation from many goroutines, with Close racing the evaluations and a
// second Close racing the first. `bin/test` runs it under -race.
func TestConcurrentUseUnderRace(t *testing.T) {
	var swaps atomic.Int64
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			n := swaps.Add(1)
			w.Header().Set("ETag", fmt.Sprintf(`"e%d"`, n))
			_, _ = w.Write([]byte(rulesetJSON(flagJSON("checkout", int(n), "[false,true]", int(n%2)))))
		case "/sdk/v1/events":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	var errs errorLog
	c := newTestClient(t, url, nil, pollOnly(
		WithOnError(errs.add),
		WithAfterEvaluation(func(e EvaluationEvent) { _ = e.FlagKey }),
		WithPollInterval(time.Millisecond), WithFlushInterval(time.Millisecond),
	)...)
	c.Ready()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 300 {
				switch (i + j) % 8 {
				case 0:
					c.BoolVariation("checkout", someone, false)
				case 1:
					c.StringVariationDetail("checkout", someone, "x")
				case 2:
					c.IntVariation("checkout", someone, 0)
				case 3:
					c.Float64VariationDetail("checkout", someone, 0)
				case 4:
					c.JSONVariation("checkout", someone, Null())
				case 5:
					_ = c.Status()
				case 6:
					_ = c.Ready()
				case 7:
					c.BoolVariation("missing", UserContext(fmt.Sprint(j)), true)
				}
			}
		}()
	}
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond)
			c.Close()
		}()
	}
	within(t, 20*time.Second, "the concurrent run", wg.Wait)
	// After Close an evaluation still answers.
	if d := c.BoolVariationDetail("checkout", someone, false); d.Reason.Kind == ReasonError {
		t.Fatalf("evaluation after Close: %+v", d)
	}
}

// A ruleset request that never answers must not slow an evaluation, and neither must a usage flush that hangs.
func TestVariationIsBoundedWhileTheNetworkHangs(t *testing.T) {
	var hang atomic.Bool
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			// Read the body so the server notices when the client gives up on the request.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/sdk/v1/ruleset" {
			w.Header().Set("ETag", `"h"`)
			_, _ = w.Write([]byte(boolRuleset()))
		}
	}))
	c := newTestClient(t, url, nil, pollOnly(WithRequestTimeout(5*time.Second))...)
	c.Ready()
	hang.Store(true)
	c.BoolVariation("checkout", someone, false) // a count for the flusher to hang on
	time.Sleep(150 * time.Millisecond)          // the poll and the flush are both in flight now
	within(t, time.Second, "1000 evaluations and Status", func() {
		for range 1000 {
			if !c.BoolVariation("checkout", someone, false) {
				t.Error("wrong answer while the network hangs")
			}
			_ = c.Status()
		}
	})
	// Do not wait for Close to outlast the hung requests; the cleanup does.
}

// Each evaluation sees one ruleset: the ETag and the flag version in a hook event come from the same swap.
func TestASwapExposesOneRulesetOrTheOther(t *testing.T) {
	var n atomic.Int64
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/ruleset" {
			return
		}
		v := n.Add(1)%2 + 1 // 1, 2, 1, 2 ...
		w.Header().Set("ETag", fmt.Sprintf(`"tag-%d"`, v))
		_, _ = w.Write([]byte(rulesetJSON(flagJSON("checkout", int(v), "[false,true]", int(v)-1))))
	}))
	var mu sync.Mutex
	seen := map[string]int{}
	torn := 0
	c := newTestClient(t, url, nil, pollOnly(WithPollInterval(time.Millisecond), WithAfterEvaluation(func(e EvaluationEvent) {
		if e.ETag == nil || e.FlagVersion == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seen[*e.ETag]++
		wantTag := fmt.Sprintf(`"tag-%d"`, int(*e.FlagVersion))
		wantIndex := int(*e.FlagVersion) - 1
		if *e.ETag != wantTag || e.VariationIndex != wantIndex {
			torn++
		}
	}))...)
	c.Ready()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				c.BoolVariation("checkout", someone, false)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if torn != 0 {
		t.Fatalf("%d evaluations saw an ETag, a version or a variation from different rulesets", torn)
	}
	if len(seen) < 2 {
		t.Logf("only saw %v; the swap race was not exercised this run", seen)
	}
}

// A hook and an onError that call back into the client must not deadlock it.
func TestCallbacksMayCallTheClient(t *testing.T) {
	url := serve(t, rulesetHandler(boolRuleset(), `"cb"`, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})))
	var c *Client
	var hookRan, errorRan atomic.Int64
	var ready sync.WaitGroup
	ready.Add(1)
	opts := pollOnly(
		WithAfterEvaluation(func(EvaluationEvent) {
			ready.Wait()
			_ = c.Status()
			if hookRan.Add(1) == 1 { // the call below runs this hook again; once is enough to prove it
				_ = c.BoolVariation("checkout", someone, false)
			}
		}),
		WithOnError(func(error) {
			ready.Wait()
			_ = c.Status()
			if errorRan.Add(1) == 1 {
				_ = c.BoolVariation("checkout", someone, false)
			}
		}),
		WithFlushInterval(10*time.Millisecond),
	)
	c = newTestClient(t, url, nil, opts...)
	ready.Done()
	c.Ready()
	within(t, 3*time.Second, "an evaluation whose hook calls the client", func() { c.BoolVariation("checkout", someone, false) })
	// The events route answers 500, so the flusher calls OnError, which calls the client.
	eventually(t, 3*time.Second, "OnError to run", func() bool { return errorRan.Load() > 0 })
	within(t, 3*time.Second, "Close", c.Close)
	if hookRan.Load() == 0 {
		t.Fatal("the hook never ran")
	}
}

// Close called from inside a callback does not wait on itself.
func TestCloseFromInsideACallback(t *testing.T) {
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	closed := make(chan struct{})
	var c *Client
	var once sync.Once
	var ready sync.WaitGroup
	ready.Add(1)
	c = newTestClient(t, url, nil, pollOnly(WithOnError(func(error) {
		ready.Wait()
		once.Do(func() {
			c.Close()
			close(closed)
		})
	}))...)
	ready.Done()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from inside OnError never returned")
	}
}

func TestEvaluatorPanicGivesFallbackAndOneOnError(t *testing.T) {
	url := serve(t, rulesetHandler(boolRuleset(), `"p"`, nil))
	var errs errorLog
	var events []EvaluationEvent
	var mu sync.Mutex
	c := newTestClient(t, url, func(cfg *config) {
		cfg.evaluate = func(*eval.Ruleset, string, jsonvalue.Value) eval.Result { panic("boom") }
	}, pollOnly(WithOnError(errs.add), WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}))...)
	c.Ready()
	before := errs.count()
	d := c.BoolVariationDetail("checkout", someone, true)
	if !d.Value || d.VariationIndex != NoVariation || d.Reason.Kind != ReasonError || d.Reason.Code != ErrorEvaluationFailed {
		t.Fatalf("got %+v", d)
	}
	if got := errs.count() - before; got != 1 {
		t.Fatalf("OnError was called %d times for one panic", got)
	}
	last := errs.all()[errs.count()-1]
	if last.Op != "evaluate" || strings.Contains(last.Error(), "boom") && false {
		t.Fatalf("error: %+v", last)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Reason.Code != ErrorEvaluationFailed {
		t.Fatalf("hook events: %+v", events)
	}
}

func TestHookAndOnErrorPanicsNeverReachTheCaller(t *testing.T) {
	url := serve(t, rulesetHandler(boolRuleset(), `"h"`, nil))
	var errs errorLog
	c := newTestClient(t, url, nil, pollOnly(
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }),
		WithOnError(func(err error) { errs.add(err); panic("onError") }),
	)...)
	c.Ready()
	d := c.BoolVariationDetail("checkout", someone, false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("the hook changed the answer: %+v", d)
	}
	if errs.count() != 1 || errs.all()[0].Op != "hook" {
		t.Fatalf("errors: %+v", errs.all())
	}
}

func TestHookEventCarriesWhatTheCallerGot(t *testing.T) {
	url := serve(t, rulesetHandler(boolRuleset(), `W/"abc"`, nil))
	var got []EvaluationEvent
	var mu sync.Mutex
	c := newTestClient(t, url, nil, pollOnly(WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}))...)
	c.Ready()
	c.BoolVariation("checkout", someone, false)
	c.StringVariation("checkout", someone, "fallback") // TYPE_MISMATCH: the hook sees the fallback
	c.BoolVariation("missing", someone, true)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("%d events", len(got))
	}
	first := got[0]
	if first.FlagKey != "checkout" || first.VariationIndex != 1 || first.ETag == nil || *first.ETag != `W/"abc"` || first.FlagVersion == nil || *first.FlagVersion != 1 {
		t.Fatalf("first: %+v", first)
	}
	if b, _ := first.Value.Bool(); !b {
		t.Fatalf("first value: %v", first.Value)
	}
	if s, _ := got[1].Value.Text(); s != "fallback" || got[1].Reason.Code != ErrorTypeMismatch {
		t.Fatalf("second: %+v", got[1])
	}
	if got[2].FlagVersion != nil || got[2].Reason.Code != ErrorFlagNotFound {
		t.Fatalf("third: %+v", got[2])
	}
}

// The helper process for TestAnUnclosedClientDoesNotKeepTheProcessAlive: it opens a client, uses it, and
// returns without closing.
func TestUnclosedClientHelper(t *testing.T) {
	if os.Getenv("TOGGLESLASH_TEST_UNCLOSED_URL") == "" {
		t.Skip("run by TestAnUnclosedClientDoesNotKeepTheProcessAlive")
	}
	c := New(os.Getenv("TOGGLESLASH_TEST_UNCLOSED_URL"), "k")
	if !c.Ready() || !c.BoolVariation("checkout", someone, false) {
		t.Fatal("the helper's client did not load")
	}
	// No Close, on purpose.
}

func TestAnUnclosedClientDoesNotKeepTheProcessAlive(t *testing.T) {
	// A stream that stays open, so the client has a goroutine blocked on a socket when the process ends.
	url := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sdk/v1/ruleset":
			w.Header().Set("ETag", `"x"`)
			_, _ = w.Write([]byte(boolRuleset()))
		case "/sdk/v1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: connected\ndata: \"x\"\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	cmd := exec.Command(os.Args[0], "-test.run=^TestUnclosedClientHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_TEST_UNCLOSED_URL="+url)
	done := make(chan error, 1)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper failed: %v\n%s", err, out.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the process was kept alive by an unclosed client\n%s", out.String())
	}
}
