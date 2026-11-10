package toggleslash

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEveryOperationFromManyGoroutines(t *testing.T) {
	f := newFakeServer(t)
	c := newClient(t, f, WithPollInterval(10*time.Millisecond), WithFlushInterval(10*time.Millisecond),
		WithAfterEvaluation(func(EvaluationEvent) {}), WithOnError(func(error) {}))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				ctx := ctxOf("u" + strconv.Itoa(g))
				switch i % 8 {
				case 0:
					c.BoolVariation("checkout", ctx, false)
				case 1:
					c.StringVariationDetail("checkout", ctx, "x")
				case 2:
					c.NumberVariation("legacy", ctx, 1)
				case 3:
					c.IntVariationDetail("checkout", ctx, 1)
				case 4:
					c.JSONVariation("checkout", ctx, Null())
				case 5:
					_ = c.Status()
				case 6:
					c.Ready()
				case 7:
					c.BoolVariation("missing", ctx, true)
				}
			}
		}(g)
	}
	for i := 0; i < 20; i++ {
		f.publish(rulesetBody(flagJSON("checkout", "false", "true", i), flagJSON("legacy", "1", "2", i)))
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	within(t, 5*time.Second, "Close", func() { _ = c.Close() })
}

func TestCloseRacingVariationAndAnotherClose(t *testing.T) {
	for round := 0; round < 20; round++ {
		f := newFakeServer(t)
		c := Create(f.url(), testKey, WithPollInterval(5*time.Millisecond), WithFlushInterval(5*time.Millisecond), WithRequestTimeout(time.Second))
		c.Ready()
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					c.BoolVariation("checkout", ctxOf("u"), false)
					_ = c.Status()
				}
			}()
		}
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = c.Close() }()
		}
		within(t, 10*time.Second, "racing operations", wg.Wait)
		if c.Status().Stream != StreamClosed {
			t.Fatalf("Stream = %q", c.Status().Stream)
		}
	}
}

func TestVariationReturnsWhileRequestsHang(t *testing.T) {
	f := newFakeServer(t)
	c := quietClient(t, f, WithRequestTimeout(5*time.Second))
	c.BoolVariation("checkout", ctxOf("u"), false)
	f.hang("events")
	f.hang("ruleset")
	flushing := make(chan struct{})
	go func() { defer close(flushing); c.flush(false) }()
	eventually(t, 3*time.Second, "the hung flush", func() bool { return len(f.requests("events")) >= 1 })
	start := time.Now()
	for i := 0; i < 1000; i++ {
		c.BoolVariation("checkout", ctxOf("u"), false)
		_ = c.Status()
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("1000 evaluations took %v behind hung requests", d)
	}
	f.releaseAll()
	<-flushing
}

func TestSwapPairsTheTagWithTheFlagVersion(t *testing.T) {
	f := newFakeServer(t)
	var n atomic.Int64
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		w.Header().Set("ETag", fmt.Sprintf(`"e%d"`, i))
		_, _ = fmt.Fprint(w, rulesetBody(flagJSON("checkout", "false", "true", int(i))))
	})
	var bad atomic.Int64
	var seen atomic.Int64
	hook := func(e EvaluationEvent) {
		seen.Add(1)
		if e.FlagVersion == nil || e.ETag != fmt.Sprintf(`"e%d"`, int64(*e.FlagVersion)) {
			bad.Add(1)
		}
	}
	c := newClient(t, f, WithStream(false), WithPollInterval(time.Millisecond), WithAfterEvaluation(hook))
	c.Ready()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				c.BoolVariation("checkout", ctxOf("u"), false)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d of %d events paired a tag with another ruleset's version", bad.Load(), seen.Load())
	}
	if n.Load() < 5 {
		t.Fatalf("only %d swaps happened, the test proved little", n.Load())
	}
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	f := newFakeServer(t)
	f.set("ruleset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var cp atomic.Pointer[Client]
	made := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	onError := func(error) {
		<-made
		c := cp.Load()
		once.Do(func() {
			defer close(done)
			_ = c.Status()
			_ = c.Ready()
			c.BoolVariation("checkout", ctxOf("u"), false)
			_ = c.Close()
			_ = c.Close()
		})
	}
	c := Create(f.url(), testKey, WithStream(false), WithOnError(onError), WithRequestTimeout(time.Second))
	cp.Store(c)
	close(made)
	within(t, 5*time.Second, "an onError that calls Ready and Close", func() { <-done })
	within(t, 5*time.Second, "Close", func() { _ = c.Close() })
}

func TestHookMayCloseAndEvaluate(t *testing.T) {
	f := newFakeServer(t)
	var cp atomic.Pointer[Client]
	var depth atomic.Int64
	hook := func(EvaluationEvent) {
		c := cp.Load()
		_ = c.Status()
		_ = c.Close()
		if depth.Add(1) == 1 { // one nested evaluation; the nested one runs this hook again
			c.BoolVariation("checkout", ctxOf("u"), false)
		}
	}
	c := Create(f.url(), testKey, WithAfterEvaluation(hook), WithRequestTimeout(time.Second))
	cp.Store(c)
	c.Ready()
	within(t, 5*time.Second, "a hook that closes", func() { c.BoolVariation("checkout", ctxOf("u"), false) })
	within(t, 5*time.Second, "Close", func() { _ = c.Close() })
}

func TestHookAndOnErrorPanicsStayInside(t *testing.T) {
	f := newFakeServer(t)
	log := &errorLog{}
	c := newClient(t, f, WithOnError(func(err error) { log.add(err); panic("onError panics too") }),
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }))
	c.Ready()
	d := c.BoolVariationDetail("checkout", ctxOf("u"), false)
	if !d.Value || d.Reason.Kind == ReasonError {
		t.Fatalf("a hook panic changed the answer: %+v", d)
	}
	if log.count() != 1 {
		t.Fatalf("onError calls = %d", log.count())
	}
}

func TestWatchdogRestartsAQuietStreamAndAKeepAliveHoldsIt(t *testing.T) {
	f := newFakeServer(t)
	var opens atomic.Int64
	var mode atomic.Int64 // 0 quiet, 1 keep-alive
	f.set("stream", func(w http.ResponseWriter, r *http.Request) {
		opens.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "event: connected\ndata: \"v1\"\n\n")
		fl := w.(http.Flusher)
		fl.Flush()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if mode.Load() == 1 {
					_, _ = fmt.Fprint(w, ": ka\n\n")
					fl.Flush()
				}
			case <-r.Context().Done():
				return
			case <-f.release:
				return
			}
		}
	})
	c := newClient(t, f, withWatchdog(300*time.Millisecond), WithPollInterval(20*time.Millisecond))
	c.Ready()

	mode.Store(1)
	eventually(t, 3*time.Second, "first open", func() bool { return opens.Load() >= 1 })
	time.Sleep(900 * time.Millisecond) // three watchdog periods of keep-alives
	if opens.Load() != 1 {
		t.Fatalf("a stream with keep-alives was restarted: %d opens", opens.Load())
	}
	mode.Store(0)
	eventually(t, 3*time.Second, "a restart after silence", func() bool { return opens.Load() >= 2 })
}

func TestTLSWithTheServersCertificate(t *testing.T) {
	f := newTLSFakeServer(t)
	c := Create(f.url(), testKey, withHTTPClient(f.srv.Client()), WithStream(false), WithPollInterval(time.Hour), WithRequestTimeout(2*time.Second))
	t.Cleanup(func() { _ = c.Close() })
	if !c.Ready() {
		t.Fatalf("not ready over TLS: %q", c.Status().LastError)
	}
}

func TestTLSWithAnUntrustedCertificateFails(t *testing.T) {
	f := newTLSFakeServer(t)
	c := Create(f.url(), testKey, WithStream(false), WithPollInterval(time.Hour), WithRequestTimeout(2*time.Second))
	t.Cleanup(func() { _ = c.Close() })
	if c.Ready() {
		t.Fatal("trusted a certificate nobody vouched for")
	}
	if c.Status().LastError != LastErrorNetwork {
		t.Fatalf("LastError = %q", c.Status().LastError)
	}
	if n := len(f.all()); n != 0 {
		t.Fatalf("the key reached a server with an untrusted certificate (%d requests)", n)
	}
}

func TestACloseDuringTheFirstLoadIsQuiet(t *testing.T) {
	f := newFakeServer(t)
	f.hang("ruleset")
	log := &errorLog{}
	c := Create(f.url(), testKey, WithOnError(log.add), WithRequestTimeout(5*time.Second))
	eventually(t, 3*time.Second, "the request", func() bool { return len(f.requests("ruleset")) == 1 })
	within(t, 3*time.Second, "Close", func() { _ = c.Close() })
	if log.count() != 0 {
		t.Fatalf("a request abandoned by Close reported %v", log.messages())
	}
	if c.Status().LastError != "" {
		t.Fatalf("LastError = %q", c.Status().LastError)
	}
}

func TestContextCauseOfTimeoutIsTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), time.Millisecond, errTimeout)
	defer cancel()
	<-ctx.Done()
	if got := errorCode(ctx, ctx.Err()); got != LastErrorTimeout {
		t.Fatalf("errorCode = %q", got)
	}
}

// TestNeverClosedClientDoesNotHoldTheProcess starts this test binary again; the child makes a client,
// never closes it and returns.
func TestNeverClosedClientDoesNotHoldTheProcess(t *testing.T) {
	if url := os.Getenv("TOGGLESLASH_TEST_CHILD_URL"); url != "" {
		_ = Create(url, testKey)
		return
	}
	f := newFakeServer(t)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNeverClosedClientDoesNotHoldTheProcess$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_TEST_CHILD_URL="+f.url())
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("a client that was never closed kept the process alive")
	}
}
