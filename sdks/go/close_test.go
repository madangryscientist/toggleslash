package toggleslash

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func within(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not finish within %v", what, d)
	}
}

func TestCloseSetsTheStreamClosedAndReadyFalse(t *testing.T) {
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, drainThenHang)
	c := New(srv.URL, "k", WithRequestTimeout(30*time.Second))
	within(t, "Close", 3*time.Second, c.Close)
	if c.Ready() {
		t.Fatal("Ready after Close with nothing loaded: want false")
	}
	if got := c.Status().Stream; got != StreamClosed {
		t.Fatalf("stream %q", got)
	}
	if got := c.Status().LastError; got != "" {
		t.Fatalf("Close is not a failure, lastError %q", got)
	}
}

func TestCloseDoesNotWaitOutAHungStream(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	f := newFeed()
	srv.handle(routeStream, f.handler)
	c := start(t, srv)
	c.Ready()
	f.send("event: connected\ndata: \"v1\"\n\n")
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	within(t, "Close", 3*time.Second, c.Close)
	if got := c.Status().Stream; got != StreamClosed {
		t.Fatalf("stream %q", got)
	}
}

func TestTwoRacingClosesBothReturnAfterTheWork(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	c := start(t, srv, WithStream(false))
	c.Ready()
	c.BoolVariation("f", shopper, false)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
			// Whichever Close returned, the final flush is done.
			if n := srv.count(routeEvents); n != 1 {
				t.Errorf("a Close returned with %d events requests", n)
			}
		}()
	}
	within(t, "both Closes", 5*time.Second, wg.Wait)
}

func TestManyGoroutinesEvaluateWhileClosing(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	c := start(t, srv, WithPollInterval(time.Millisecond), WithFlushInterval(time.Millisecond))
	c.Ready()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				c.BoolVariation("f", shopper, false)
				c.Status()
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	within(t, "the workers", 20*time.Second, wg.Wait)
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	t.Run("onError calls Close, Status, Variation and Ready", func(t *testing.T) {
		srv := newFakeServer(t, "")
		srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		var c atomic.Pointer[Client]
		called := make(chan struct{})
		var once sync.Once
		client := New(srv.URL, "k", WithStream(false), WithOnError(func(error) {
			cl := c.Load()
			cl.Ready()
			cl.Status()
			cl.BoolVariation("f", shopper, true)
			cl.Close()
			once.Do(func() { close(called) })
		}))
		c.Store(client)
		select {
		case <-called:
		case <-time.After(10 * time.Second):
			t.Fatal("onError never finished: deadlock")
		}
		within(t, "Close", 3*time.Second, client.Close)
	})
	t.Run("the hook calls Close", func(t *testing.T) {
		srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
		var c atomic.Pointer[Client]
		client := New(srv.URL, "k", WithStream(false), WithAfterEvaluation(func(EvaluationEvent) {
			c.Load().Close()
			c.Load().Status()
		}))
		c.Store(client)
		client.Ready()
		within(t, "an evaluation whose hook closes the client", 5*time.Second, func() {
			client.BoolVariation("f", shopper, false)
		})
		if got := client.Status().Stream; got != StreamClosed {
			t.Fatalf("stream %q", got)
		}
	})
	t.Run("Ready from onError", func(t *testing.T) {
		srv := newFakeServer(t, "")
		srv.handle(routeRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		var c atomic.Pointer[Client]
		got := make(chan bool, 1)
		client := New(srv.URL, "k", WithStream(false), WithOnError(func(error) {
			select {
			case got <- c.Load().Ready():
			default:
			}
		}))
		c.Store(client)
		t.Cleanup(client.Close)
		select {
		case ok := <-got:
			if ok {
				t.Fatal("Ready true after a failed load")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Ready from onError blocked")
		}
	})
	t.Run("a usage error callback calls Close", func(t *testing.T) {
		srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
		srv.handle(routeEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		var c atomic.Pointer[Client]
		done := make(chan struct{})
		var once sync.Once
		clk := newFakeClock(epoch)
		client := New(srv.URL, "k", WithStream(false), withClock(clk), WithPollInterval(24*time.Hour),
			WithOnError(func(error) {
				c.Load().Close()
				once.Do(func() { close(done) })
			}))
		c.Store(client)
		client.Ready()
		client.BoolVariation("f", shopper, false)
		eventually(t, "the flush timer", func() bool { return clk.Armed() >= 2 })
		clk.Advance(30 * time.Second)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock")
		}
		within(t, "Close", 3*time.Second, client.Close)
	})
}

func TestCloseIsBoundedWhileEverythingHangs(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	c := New(srv.URL, "k", WithStream(false), WithRequestTimeout(200*time.Millisecond), WithPollInterval(time.Millisecond))
	c.Ready()
	srv.handle(routeEvents, drainThenHang)
	srv.handle(routeRuleset, drainThenHang)
	c.BoolVariation("f", shopper, false)
	start := time.Now()
	within(t, "Close", 3*time.Second, c.Close)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v with a 200 ms timeout", d)
	}
}
