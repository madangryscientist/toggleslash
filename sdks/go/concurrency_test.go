package toggleslash

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSwapAtomicity: while the ruleset is replaced over and over, the ETag, the flag version and the
// served value an evaluation reports always belong to one ruleset.
func TestSwapAtomicity(t *testing.T) {
	version := func(n int) string {
		return rulesetJSON(flagJSON("f", "["+strconv.Itoa(n)+"]", 0, n))
	}
	f := newFake(t, version(1))
	f.set(version(1), `"1"`)
	var bad atomic.Int32
	var seenEvents atomic.Int32
	c := makeClient(t, f, WithPollInterval(time.Millisecond),
		WithAfterEvaluation(func(ev EvaluationEvent) {
			seenEvents.Add(1)
			n, _ := ev.Value.Number()
			if ev.FlagVersion == nil || ev.ETag != `"`+strconv.Itoa(int(*ev.FlagVersion))+`"` || int(n) != int(*ev.FlagVersion) {
				if ev.ETag != "" {
					bad.Add(1)
				}
			}
		}))
	c.Ready()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 2; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			f.set(version(n), `"`+strconv.Itoa(n)+`"`)
			time.Sleep(time.Millisecond)
		}
	}()
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			end := time.Now().Add(400 * time.Millisecond)
			for time.Now().Before(end) {
				c.IntVariation("f", ctxFor("u"), -1)
			}
		}()
	}
	time.Sleep(450 * time.Millisecond)
	close(stop)
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d evaluations saw a ruleset mixed with another's ETag", bad.Load())
	}
	if seenEvents.Load() == 0 {
		t.Fatal("no evaluations ran")
	}
	if n, _ := strconv.Atoi(trimQuotes(c.Status().ETag)); n < 3 {
		t.Fatalf("the ruleset was never replaced (etag %s)", c.Status().ETag)
	}
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// TestEverythingAtOnce runs every operation from many goroutines, with Close racing them; run under -race.
func TestEverythingAtOnce(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true,false]`, 0, 1)))
	var closes sync.WaitGroup
	log := &errorLog{}
	c := New(f.url(), "k", WithStream(true), WithPollInterval(2*time.Millisecond), WithFlushInterval(2*time.Millisecond),
		WithRequestTimeout(2*time.Second), WithOnError(log.add), WithAfterEvaluation(func(EvaluationEvent) {}))
	var wg sync.WaitGroup
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			end := time.Now().Add(300 * time.Millisecond)
			for time.Now().Before(end) {
				ctx := ctxFor("user-" + strconv.Itoa(g))
				c.BoolVariation("a", ctx, false)
				c.StringVariationDetail("a", ctx, "x")
				c.IntVariation("missing", ctx, 1)
				c.Status()
				c.Ready()
			}
		}(g)
	}
	for i := 0; i < 4; i++ {
		closes.Add(1)
		go func() {
			defer closes.Done()
			time.Sleep(150 * time.Millisecond)
			c.Close()
		}()
	}
	within(t, "the goroutines", 20*time.Second, wg.Wait)
	within(t, "the concurrent Close calls", 10*time.Second, closes.Wait)
	c.Close() // a second Close after the others
	if got := c.Status().Stream; got != StreamClosed {
		t.Fatalf("stream = %s", got)
	}
	// Evaluation still answers after Close, from the ruleset held.
	if !c.BoolVariation("a", ctxFor("u"), false) {
		t.Fatal("evaluation stopped answering after Close")
	}
}

// clientRef hands a client to a callback that may run before New returns.
type clientRef struct{ p atomic.Pointer[Client] }

func (r *clientRef) get() *Client {
	for {
		if c := r.p.Load(); c != nil {
			return c
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCloseTwice(t *testing.T) {
	f := newFake(t, rulesetJSON())
	c := makeClient(t, f)
	c.Ready()
	within(t, "two Close calls", 5*time.Second, func() { c.Close(); c.Close() })
}

func TestCloseBeforeFirstLoadSettles(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(f.hang, nil, nil)
	c := makeClient(t, f, WithRequestTimeout(time.Hour))
	got := make(chan bool, 1)
	go func() { got <- c.Ready() }()
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	c.Close()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Close took %v with a hung request", took)
	}
	select {
	case ok := <-got:
		if ok {
			t.Fatal("Ready() = true")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Ready did not return after Close")
	}
}

func TestEvaluationDoesNotWaitOnAHungRulesetRequest(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	var n atomic.Int32
	f.on(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			f.defaultRuleset(w, r)
			return
		}
		f.hang(w, r)
	}, nil, nil)
	c := makeClient(t, f, WithPollInterval(5*time.Millisecond), WithRequestTimeout(time.Hour))
	c.Ready()
	eventually(t, "the hung request", 3*time.Second, func() bool { return n.Load() >= 2 })
	within(t, "variation, variationDetail, status and ready", 500*time.Millisecond, func() {
		c.BoolVariation("a", ctxFor("u"), false)
		c.BoolVariationDetail("a", ctxFor("u"), false)
		c.Status()
		c.Ready()
	})
}

func TestEvaluationDoesNotWaitOnAHungFlush(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	f.on(nil, nil, f.hang)
	c := makeClient(t, f, WithFlushInterval(5*time.Millisecond), WithRequestTimeout(500*time.Millisecond))
	c.Ready()
	c.BoolVariation("a", ctxFor("u"), false)
	eventually(t, "the hung flush", 3*time.Second, func() bool { return len(f.requests("/sdk/v1/events")) >= 1 })
	within(t, "evaluation during a hung flush", 500*time.Millisecond, func() {
		for i := 0; i < 1000; i++ {
			c.BoolVariationDetail("a", ctxFor("u"), false)
		}
		c.Status()
	})
	start := time.Now()
	c.Close()
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Close took %v, want about two request timeouts at most", took)
	}
}

// TestCallbacksMayCallBack: the hook and onError run outside every SDK lock, so they may call any operation.
func TestCallbacksMayCallBack(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	var c *Client
	var depth atomic.Int32
	var hooked atomic.Int32
	c = makeClient(t, f, WithAfterEvaluation(func(ev EvaluationEvent) {
		hooked.Add(1)
		c.Status()
		if depth.Add(1) == 1 {
			c.BoolVariation("a", ctxFor("inner"), false) // re-enters the hook once
		}
		depth.Add(-1)
	}))
	c.Ready()
	within(t, "a hook that calls back", 2*time.Second, func() { c.BoolVariation("a", ctxFor("u"), false) })
	if hooked.Load() != 2 {
		t.Fatalf("hook ran %d times, want 2", hooked.Load())
	}
}

func TestCloseAndReadyFromOnErrorOnTheLoopGoroutine(t *testing.T) {
	f := newFake(t, rulesetJSON())
	f.on(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, nil, nil)
	var ref clientRef
	var once sync.Once
	called := make(chan struct{})
	c := New(f.url(), "k", WithStream(false), WithOnError(func(error) {
		once.Do(func() {
			c := ref.get()
			c.Status()
			c.Ready() // settled already: answers without waiting
			c.Close() // on the SDK's own goroutine: stops everything and returns
			close(called)
		})
	}))
	ref.p.Store(c)
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("onError calling Close deadlocked")
	}
	within(t, "a later Close", 5*time.Second, c.Close)
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %s", c.Status().Stream)
	}
}

func TestCloseFromOnErrorOnTheFlushGoroutine(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	f.on(nil, nil, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var ref clientRef
	var once sync.Once
	called := make(chan struct{})
	c := New(f.url(), "k", WithStream(false), WithFlushInterval(5*time.Millisecond), WithOnError(func(err error) {
		var e *Error
		if errors.As(err, &e) && e.Op == "events" {
			once.Do(func() {
				ref.get().Close()
				close(called)
			})
		}
	}))
	ref.p.Store(c)
	c.Ready()
	c.BoolVariation("a", ctxFor("u"), false)
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("onError calling Close on the flush goroutine deadlocked")
	}
	within(t, "a later Close", 5*time.Second, c.Close)
}

func TestTwoCallbacksCloseAtOnce(t *testing.T) {
	f := newFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	f.on(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, nil, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	var ref clientRef
	var closed sync.WaitGroup
	closed.Add(2)
	var a, b sync.Once
	c := New(f.url(), "k", WithStream(false), WithFlushInterval(5*time.Millisecond), WithPollInterval(5*time.Millisecond),
		WithOnError(func(err error) {
			var e *Error
			if !errors.As(err, &e) {
				return
			}
			switch e.Op {
			case "ruleset":
				a.Do(func() { ref.get().Close(); closed.Done() })
			case "events":
				b.Do(func() { ref.get().Close(); closed.Done() })
			}
		}))
	ref.p.Store(c)
	// make the flush have something to send even though there is no ruleset: nothing is counted, so use a
	// direct count
	c.count("x", 0)
	within(t, "both callbacks' Close calls", 10*time.Second, closed.Wait)
	within(t, "a Close from outside", 10*time.Second, c.Close)
}

func TestTLS(t *testing.T) {
	f := newTLSFake(t, rulesetJSON(flagJSON("a", `[true]`, 0, 1)))
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	c := New(f.url(), "k", WithStream(false), withTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}), WithRequestTimeout(3*time.Second))
	defer c.Close()
	if !c.Ready() || !c.BoolVariation("a", ctxFor("u"), false) {
		t.Fatalf("no ruleset over TLS with a trusted certificate: %+v", c.Status())
	}
	// Without the certificate the connection is refused: the platform's roots do not know it.
	u := New(f.url(), "k", WithStream(false), WithRequestTimeout(3*time.Second))
	defer u.Close()
	if u.Ready() || u.Status().LastError != "network" {
		t.Fatalf("an untrusted certificate was accepted: %+v", u.Status())
	}
}

// TestNeverClosedClientDoesNotBlockExit runs a test binary that creates a client and returns without
// closing it; the process must exit.
func TestNeverClosedClientDoesNotBlockExit(t *testing.T) {
	if os.Getenv("TOGGLESLASH_EXIT_CHILD") == "1" {
		New("http://127.0.0.1:1", "k")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNeverClosedClientDoesNotBlockExit$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_EXIT_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the child did not exit cleanly: %v\n%s", err, out)
	}
}
