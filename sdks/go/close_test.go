package toggleslash

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseIsSafeTwiceAndConcurrently(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithStream(true), WithFlushInterval(10*time.Millisecond))
	c.Ready()
	c.BoolVariation("a", user, false)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	wg.Wait()
	c.Close()
	if st := c.Status().Stream; st != StreamClosed {
		t.Fatalf("stream = %q after Close", st)
	}
	if len(f.sentEvents()) == 0 {
		t.Fatal("the final flush did not send")
	}
}

func TestCloseDoesTheFinalFlush(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithFlushInterval(time.Hour))
	c.Ready()
	for range 3 {
		c.BoolVariation("a", user, false)
	}
	c.Close()
	got := allSent(t, f)
	if len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("sent %+v", got)
	}
}

func TestNoRequestsAfterClose(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithStream(true), WithPollInterval(10*time.Millisecond), WithFlushInterval(10*time.Millisecond))
	c.Ready()
	c.Close()
	f.mu.Lock()
	n := len(f.reqs)
	f.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) != n {
		t.Fatalf("%d requests after Close", len(f.reqs)-n)
	}
}

func TestEvaluationAnswersAfterClose(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f)
	c.Ready()
	c.Close()
	d := c.BoolVariationDetail("a", user, false)
	if !d.Value || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("%+v", d)
	}
}

func TestCloseWithAHungFirstLoadIsBounded(t *testing.T) {
	f := newFakeServer(t)
	f.rulesetFn = hang
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(300*time.Millisecond))
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > 600*time.Millisecond {
		t.Fatalf("Close took %v", d)
	}
	if c.Ready() {
		t.Fatal("Ready is true after a Close that cut the first load")
	}
}

func TestCloseWithAHungStreamIsBounded(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.streamFn = hang
	c := New(f.url(), testKey, WithStream(true), WithRequestTimeout(5*time.Second))
	c.Ready()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestCloseRacingVariation(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithFlushInterval(5*time.Millisecond))
	c.Ready()
	var wg sync.WaitGroup
	var stop atomic.Bool
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if !c.BoolVariation("a", user, false) {
					t.Error("a wrong answer during Close")
					return
				}
			}
		}()
	}
	time.Sleep(30 * time.Millisecond)
	c.Close()
	stop.Store(true)
	wg.Wait()
}

func TestManyGoroutinesAgainstASwappingRuleset(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	var seen sync.Map
	c := newTestClient(t, f, WithStream(true), WithPollInterval(time.Hour), WithFlushInterval(5*time.Millisecond),
		WithAfterEvaluation(func(e EvaluationEvent) {
			// The etag and the flag version come from one ruleset: "vN" holds version N.
			if e.ETag != "" && e.FlagVersion != nil {
				if want := fmt.Sprintf(`"v%d"`, *e.FlagVersion); want != e.ETag {
					seen.Store(e.ETag+"/"+want, true)
				}
			}
		}))
	c.Ready()
	var wg sync.WaitGroup
	var stop atomic.Bool
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				c.BoolVariation("a", user, false)
				c.Status()
			}
		}()
	}
	for v := 2; v < 12; v++ {
		f.publish(boolFlag("a", v%2 == 0, v))
		eventually(t, 3*time.Second, "the swap", func() bool { return c.Status().ETag == fmt.Sprintf(`"v%d"`, v) })
	}
	stop.Store(true)
	wg.Wait()
	seen.Range(func(k, _ any) bool {
		t.Errorf("etag and flag version came from different rulesets: %v", k)
		return true
	})
}

func TestHookSeesFallbacksBeforeTheFirstLoad(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.rulesetFn = hang
	var got []EvaluationEvent
	var mu sync.Mutex
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(300*time.Millisecond),
		WithAfterEvaluation(func(e EvaluationEvent) { mu.Lock(); got = append(got, e); mu.Unlock() }))
	defer c.Close()
	if c.BoolVariation("a", user, true) != true {
		t.Fatal("fallback")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("%d events", len(got))
	}
	e := got[0]
	if e.FlagKey != "a" || e.VariationIndex != nil || e.ETag != "" || e.FlagVersion != nil || e.Reason.Code != ErrNotReady || !e.Context.Valid() {
		t.Fatalf("%+v", e)
	}
}

func TestHookSeesTheServedResult(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 7))
	var got []EvaluationEvent
	var mu sync.Mutex
	c := newTestClient(t, f, WithAfterEvaluation(func(e EvaluationEvent) { mu.Lock(); got = append(got, e); mu.Unlock() }))
	c.Ready()
	c.BoolVariation("a", user, false)
	c.BoolVariation("nope", user, false)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("%d events", len(got))
	}
	if e := got[0]; e.VariationIndex == nil || *e.VariationIndex != 1 || e.ETag != `"v1"` || e.FlagVersion == nil || *e.FlagVersion != 7 {
		t.Fatalf("%+v", e)
	}
	if e := got[1]; e.VariationIndex != nil || e.FlagVersion != nil || e.Reason.Kind != ReasonError {
		t.Fatalf("%+v", e)
	}
}

func TestAPanickingHookGoesToOnErrorAndTheEvaluationStillAnswers(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	errs := &errorLog{}
	c := newTestClient(t, f, WithOnError(errs.add), WithFlushInterval(time.Hour),
		WithAfterEvaluation(func(EvaluationEvent) { panic("boom") }))
	c.Ready()
	if !c.BoolVariation("a", user, false) {
		t.Fatal("the hook's panic changed the answer")
	}
	if errs.count() != 1 {
		t.Fatalf("%v", errs.texts())
	}
	var pe *PanicError
	if !errors.As(errs.all()[0], &pe) || fmt.Sprint(pe.Value) != "boom" {
		t.Fatalf("not a PanicError carrying the value: %v", errs.all()[0])
	}
	c.Close()
	if got := allSent(t, f); len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("the evaluation was not counted: %+v", got)
	}
}

func TestAPanicInOnErrorIsIgnored(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f, WithOnError(func(error) { panic("again") }),
		WithAfterEvaluation(func(EvaluationEvent) { panic("first") }))
	c.Ready()
	if !c.BoolVariation("a", user, false) {
		t.Fatal("wrong answer")
	}
	c.Close()
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.eventsFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	var ptr atomic.Pointer[Client]
	var calls atomic.Int32
	done := make(chan struct{})
	var once sync.Once
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(2*time.Second), WithFlushInterval(10*time.Millisecond),
		WithOnError(func(error) {
			calls.Add(1)
			cl := ptr.Load()
			if cl == nil {
				return
			}
			cl.Status()
			cl.BoolVariation("a", user, false)
			cl.Ready()
			cl.Close()
			once.Do(func() { close(done) })
		}))
	ptr.Store(c)
	c.Ready()
	c.BoolVariation("a", user, false)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onError calling Close deadlocked")
	}
	c.Close()
}

func TestAHookMayCallClose(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	var ptr atomic.Pointer[Client]
	c := New(f.url(), testKey, WithStream(false), WithAfterEvaluation(func(EvaluationEvent) {
		if cl := ptr.Load(); cl != nil {
			cl.Close()
		}
	}))
	ptr.Store(c)
	c.Ready()
	done := make(chan struct{})
	go func() { c.BoolVariation("a", user, false); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hook calling Close deadlocked")
	}
}

func TestNewAgainstARefusedConnectionDoesNotPanicAndReportsFixedText(t *testing.T) {
	c := New("http://127.0.0.1:1", testKey, WithStream(true), WithRequestTimeout(200*time.Millisecond))
	defer c.Close()
	if c.Ready() {
		t.Fatal("ready")
	}
	if c.BoolVariation("a", user, true) != true {
		t.Fatal("fallback")
	}
}

// TestANeverClosedClientDoesNotKeepTheProcessAlive re-runs this test binary with a marker set; the child makes a
// client and returns from main without closing it, and must exit.
func TestANeverClosedClientDoesNotKeepTheProcessAlive(t *testing.T) {
	if os.Getenv("TOGGLESLASH_NEVER_CLOSED_CHILD") == "1" {
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestNeverClosedChild$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_NEVER_CLOSED_CHILD=1")
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
			t.Fatalf("child failed: %v\n%s", err, out.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the child did not exit")
	}
}

func TestNeverClosedChild(t *testing.T) {
	if os.Getenv("TOGGLESLASH_NEVER_CLOSED_CHILD") != "1" {
		t.Skip("run by TestANeverClosedClientDoesNotKeepTheProcessAlive")
	}
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := New(f.url(), testKey, WithStream(true))
	c.Ready()
	c.BoolVariation("a", user, false)
	// Never closed. A Go program ends when main returns, goroutines or not; the point is that nothing in the SDK
	// calls os.Exit-blocking code or a finalizer wait, and that the test binary itself exits.
}
