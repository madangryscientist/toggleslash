package toggleslash

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestCloseIsSafeTwiceAndSettlesReady(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hang: true})
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(5*time.Second))
	ready := make(chan bool, 1)
	go func() { ready <- c.Ready() }()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	c.Close()
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Errorf("close took %s", time.Since(start))
	}
	select {
	case ok := <-ready:
		if ok {
			t.Error("Ready() = true after a close during the first load")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ready() was not settled by Close")
	}
	if st := c.Status(); st.Stream != StreamClosed || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestCloseCausedFailuresAreNotRecorded(t *testing.T) {
	f := newFakeServer(t)
	errs := &errorLog{}
	c := New(f.url(), testKey, WithOnError(errs.add), WithPollInterval(time.Hour))
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	c.Close()
	if errs.count() != 0 {
		t.Errorf("errors = %v", errs.messages())
	}
	if st := c.Status(); st.LastError != "" || st.Stream != StreamClosed {
		t.Errorf("status = %+v", st)
	}
}

func TestEvaluationStillAnswersAfterClose(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.url(), testKey, WithStream(false))
	c.Ready()
	c.Close()
	if !c.BoolVariation("checkout", user, false) {
		t.Error("the held ruleset was dropped")
	}
	if c.Status().ETag != `"v1"` {
		t.Errorf("etag = %q", c.Status().ETag)
	}
	if !c.Ready() {
		t.Error("Ready() changed after close")
	}
}

func TestCloseFromOnErrorDoesNotWaitOnItself(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{status: 503})
	var c *Client
	done := make(chan struct{})
	started := make(chan struct{})
	opt := WithOnError(func(error) {
		<-started
		c.Close()
		c.Ready()
		close(done)
	})
	c = New(f.url(), testKey, WithStream(false), opt)
	close(started)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close or Ready called from onError never returned")
	}
	returnsWithin(t, 2*time.Second, "Close from outside", c.Close)
}

func TestCloseFromTheHookDoesNotDeadlock(t *testing.T) {
	f := newFakeServer(t)
	var c *Client
	c = New(f.url(), testKey, WithStream(false), WithAfterEvaluation(func(EvaluationEvent) { c.Close() }))
	c.Ready()
	returnsWithin(t, 3*time.Second, "variation with a hook that closes", func() { c.BoolVariation("checkout", user, false) })
}

func TestEveryOperationFromManyGoroutinesWithCloseRacing(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.url(), testKey, WithPollInterval(5*time.Millisecond), WithFlushInterval(5*time.Millisecond))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				switch i % 4 {
				case 0:
					c.BoolVariation("checkout", user, false)
				case 1:
					c.VariationDetail("legacy", user, user)
				case 2:
					_ = c.Status()
				case 3:
					c.Ready()
					c.IntVariationDetail("missing", user, 1)
				}
			}
		}(i)
	}
	time.Sleep(150 * time.Millisecond)
	var closers sync.WaitGroup
	for i := 0; i < 4; i++ {
		closers.Add(1)
		go func() { defer closers.Done(); c.Close() }()
	}
	closers.Wait()
	close(stop)
	wg.Wait()
	if c.Status().Stream != StreamClosed {
		t.Errorf("stream = %q", c.Status().Stream)
	}
}

func TestACloseThatRacesTheFirstLoad(t *testing.T) {
	for i := 0; i < 30; i++ {
		f := newFakeServer(t)
		c := New(f.url(), testKey)
		if i%2 == 0 {
			time.Sleep(time.Duration(i) * time.Millisecond / 2)
		}
		c.Close()
		c.Ready()
		f.close()
	}
}

func TestACloseDuringAHungRulesetIsBounded(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hangBody: true})
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(300*time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	returnsWithin(t, 700*time.Millisecond, "close during a hung body", c.Close)
}

// TestNeverClosedClientDoesNotHoldTheProcess starts a child process that creates a client, never closes it, and
// returns from its test; the process must exit on its own.
func TestNeverClosedClientDoesNotHoldTheProcess(t *testing.T) {
	if url := os.Getenv("TOGGLESLASH_TEST_CHILD_URL"); url != "" {
		New(url, testKey)
		return
	}
	f := newFakeServer(t)
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestNeverClosedClientDoesNotHoldTheProcess$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_TEST_CHILD_URL="+f.url())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("child: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process did not exit with a client left open")
	}
}

func TestCallbacksMayCallStatusAndVariation(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{status: 503})
	var c *Client
	started := make(chan struct{})
	errDone := make(chan struct{})
	hookDone := make(chan struct{})
	var once sync.Once
	c = New(f.url(), testKey, WithStream(false), WithPollInterval(20*time.Millisecond),
		WithOnError(func(error) {
			<-started
			c.Status()
			c.BoolVariation("checkout", user, false)
			once.Do(func() { close(errDone) })
		}),
		WithAfterEvaluation(func(EvaluationEvent) {
			c.Status()
			c.Ready()
			select {
			case <-hookDone:
			default:
				close(hookDone)
			}
		}))
	close(started)
	select {
	case <-errDone:
	case <-time.After(3 * time.Second):
		t.Fatal("onError calling Status and BoolVariation never returned")
	}
	c.Ready()
	returnsWithin(t, 3*time.Second, "variation with a hook that calls back", func() { c.BoolVariation("checkout", user, false) })
	select {
	case <-hookDone:
	default:
		t.Error("the hook never ran")
	}
	c.Close()
}
