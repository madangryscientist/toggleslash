package toggleslash

import (
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hang is a handler that never answers until the request is cancelled. It reads the body first, so the
// client's write finishes, and it returns when the client gives up.
func hang(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }

func within(t *testing.T, what string, limit time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s did not finish in %v", what, limit)
	}
}

func TestCloseIsIdempotentAndConcurrent(t *testing.T) {
	f := newFake(t)
	c := f.client(t)
	c.Ready()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	within(t, "eight Close calls", 10*time.Second, wg.Wait)
	c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("%+v", c.Status())
	}
}

func TestEvaluationAndStatusAnswerAfterClose(t *testing.T) {
	f := newFake(t)
	c := f.client(t)
	c.Ready()
	c.Close()
	if !c.BoolVariation("f", user, false) {
		t.Fatal("no answer")
	}
	if st := c.Status(); st.Stream != StreamClosed || st.ETag != `"v1"` {
		t.Fatalf("%+v", st)
	}
	if !c.Ready() {
		t.Fatal("Ready's answer is fixed")
	}
}

func TestCloseDuringAHungFirstLoad(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, hang)
	var log errorLog
	c := f.client(t, WithStream(false), WithOnError(log.add), WithRequestTimeout(10*time.Second))
	var readyAnswer atomic.Int32
	go func() {
		if c.Ready() {
			readyAnswer.Store(1)
		} else {
			readyAnswer.Store(2)
		}
	}()
	within(t, "Close", 3*time.Second, c.Close)
	waitFor(t, "Ready to settle", func() bool { return readyAnswer.Load() != 0 })
	if readyAnswer.Load() != 2 {
		t.Fatal("Ready true after Close with nothing loaded")
	}
	if c.Ready() {
		t.Fatal("Ready true")
	}
	if st := c.Status(); st.LastError != "" || st.Stream != StreamClosed {
		t.Fatalf("a failure caused by Close was recorded: %+v", st)
	}
	if log.count() != 0 {
		t.Fatalf("a failure caused by Close was reported: %q", log.text())
	}
}

func TestCloseDuringAnOpenStream(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	f.set(&f.stream, feed.handler)
	var log errorLog
	c := f.client(t, WithOnError(log.add))
	feed.waitOpened(t)
	feed.push("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	within(t, "Close", 3*time.Second, c.Close)
	if st := c.Status(); st.Stream != StreamClosed || st.LastError != "" || log.count() != 0 {
		t.Fatalf("%+v %q", st, log.text())
	}
}

func TestCloseIsBoundedWhenTheFinalFlushHangs(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, hang)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Hour), WithRequestTimeout(300*time.Millisecond))
	c.Ready()
	c.BoolVariation("f", user, false)
	start := time.Now()
	within(t, "Close", 5*time.Second, c.Close)
	if took := time.Since(start); took < 250*time.Millisecond {
		t.Fatalf("Close did not wait for the final flush (%v)", took)
	}
}

func TestCloseBoundIsAboutTwiceTheTimeout(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, hang)
	c := f.client(t, WithStream(false), WithFlushInterval(30*time.Millisecond), WithRequestTimeout(300*time.Millisecond))
	c.Ready()
	c.BoolVariation("f", user, false)
	waitFor(t, "a flush in flight", func() bool { return f.count("/sdk/v1/events") >= 1 })
	start := time.Now()
	within(t, "Close", 5*time.Second, c.Close)
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("Close took %v with a 300 ms timeout", took)
	}
}

func TestCloseRacingVariation(t *testing.T) {
	f := newFake(t)
	c := f.client(t, WithStream(false), WithFlushInterval(time.Millisecond))
	c.Ready()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.BoolVariation("f", user, false)
					c.Status()
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond) // let the evaluators run before Close lands
	within(t, "Close", 5*time.Second, c.Close)
	close(stop)
	wg.Wait()
}

func TestCloseFromOnError(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	holder := make(chan *Client, 1)
	finished := make(chan struct{})
	New1 := func() *Client {
		return New(f.srv.URL, "sdk-key", WithStream(false), WithOnError(func(error) {
			c := <-holder
			holder <- c
			c.Ready() // settled before the report, so this does not wait
			c.Close()
			c.Close()
			close(finished)
		}))
	}
	c := New1()
	holder <- c
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from onError never returned")
	}
	within(t, "Close", 5*time.Second, c.Close)
}

func TestCloseFromTheHook(t *testing.T) {
	f := newFake(t)
	holder := make(chan *Client, 1)
	var once sync.Once
	c := New(f.srv.URL, "sdk-key", WithStream(false), WithAfterEvaluation(func(Event) {
		once.Do(func() {
			c := <-holder
			c.Close()
		})
	}))
	holder <- c
	c.Ready()
	within(t, "the hook's Close", 5*time.Second, func() { c.BoolVariation("f", user, false) })
	if c.Status().Stream != StreamClosed {
		t.Fatalf("%+v", c.Status())
	}
}

func TestCloseFromTheFinalFlushCallback(t *testing.T) {
	f := newFake(t)
	f.set(&f.events, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	holder := make(chan *Client, 1)
	done := make(chan struct{})
	var once sync.Once
	c := New(f.srv.URL, "sdk-key", WithStream(false), WithFlushInterval(time.Hour), WithOnError(func(error) {
		once.Do(func() {
			c := <-holder
			c.Close() // a second Close from inside the first Close's callback must not wait for it
			close(done)
		})
	}))
	holder <- c
	c.Ready()
	c.BoolVariation("f", user, false)
	within(t, "Close", 5*time.Second, c.Close)
	select {
	case <-done:
	default:
		t.Fatal("the callback never ran")
	}
}

func TestEveryOperationFromManyGoroutines(t *testing.T) {
	f := newFake(t)
	feed := newFeed()
	f.set(&f.stream, feed.handler)
	var n atomic.Int32
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		v := n.Add(1)
		w.Header().Set("ETag", `"v`+strconv.Itoa(int(v%10))+`"`)
		_, _ = w.Write([]byte(rulesetJSON(flagJSON("f", `[false,true]`, int(v%2), int(v)))))
	})
	var log errorLog
	c := f.client(t, WithFlushInterval(5*time.Millisecond), WithPollInterval(5*time.Millisecond), WithOnError(log.add),
		WithAfterEvaluation(func(Event) {}))
	feed.waitOpened(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	work := []func(){
		func() { c.BoolVariation("f", user, false) },
		func() { c.StringVariationDetail("f", user, "x") },
		func() { c.IntVariation("f", user, 1) },
		func() { c.FloatVariationDetail("f", user, 1) },
		func() { c.JSONVariation("f", map[string]any{"key": "k"}, Value{}) },
		func() { _ = c.Status() },
		func() { _ = c.Ready() },
	}
	for _, w := range work {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						w()
					}
				}
			}()
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				feed.push("event: changed\ndata: \"t" + string(rune('a'+i%26)) + "\"\n\n")
				time.Sleep(time.Millisecond) // pacing the events
			}
		}
	}()
	waitFor(t, "ruleset churn", func() bool { return n.Load() > 20 })
	within(t, "Close", 10*time.Second, c.Close)
	close(stop)
	wg.Wait()
}

// TestNeverClosedClientDoesNotHoldTheProcess runs a helper process that makes a client and never closes it.
func TestNeverClosedClientDoesNotHoldTheProcess(t *testing.T) {
	if url := os.Getenv("TOGGLESLASH_HELPER_URL"); url != "" {
		c := New(url, "sdk-key")
		c.Ready()
		return // the test binary exits when its tests end, with the client still running
	}
	f := newFake(t)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNeverClosedClientDoesNotHoldTheProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_HELPER_URL="+f.srv.URL)
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
			t.Fatalf("%v\n%s", err, out.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process did not exit")
	}
}

// Variation and Status read memory: they return at once while a ruleset request hangs and while a usage
// flush hangs (contract 5.2 and 6.2).
func TestVariationAndStatusDoNotWaitOnAHungRequest(t *testing.T) {
	f := newFake(t)
	var rulesetCalls atomic.Int32
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		if rulesetCalls.Add(1) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(boolRuleset()))
			return
		}
		<-r.Context().Done()
	})
	f.set(&f.events, hang)
	c := f.client(t, WithStream(false), WithPollInterval(20*time.Millisecond), WithFlushInterval(20*time.Millisecond),
		WithRequestTimeout(500*time.Millisecond)) // Close at the end waits at most about this long
	if !c.Ready() {
		t.Fatal("not ready")
	}
	c.BoolVariation("f", user, false) // something for the flush to send
	waitFor(t, "a hung ruleset request and a hung flush", func() bool {
		return rulesetCalls.Load() >= 2 && f.count("/sdk/v1/events") >= 1
	})
	within(t, "2000 evaluations and status reads", 3*time.Second, func() {
		for range 2000 {
			if !c.BoolVariation("f", user, false) {
				t.Error("the held ruleset stopped serving")
				return
			}
			_ = c.Status()
		}
	})
}
