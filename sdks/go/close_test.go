package toggleslash

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseTwiceAndConcurrently(t *testing.T) {
	f := newFake(t)
	c, _ := newClient(t, f, WithStream(true))
	c.Ready()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
	c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %q", c.Status().Stream)
	}
}

func TestCloseIsBoundedWhileARequestHangs(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}
	c, e := newClient(t, f, WithRequestTimeout(10*time.Second))
	start := time.Now()
	c.Close()
	if time.Since(start) > 3*time.Second {
		t.Fatalf("close took %v", time.Since(start))
	}
	if c.Ready() {
		t.Fatal("ready after a close that cut the first load")
	}
	if got := e.all(); len(got) != 0 {
		t.Fatalf("close caused errors: %v", got)
	}
	if c.Status().LastError != "" {
		t.Fatalf("close set lastError %q", c.Status().LastError)
	}
}

func TestCloseCutsAnOpenStream(t *testing.T) {
	f := newFake(t)
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: connected\ndata: \"v1\"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	c, e := newClient(t, f, WithStream(true))
	c.Ready()
	eventually(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	c.Close()
	if c.Status().Stream != StreamClosed || c.Status().LastError != "" || len(e.all()) != 0 {
		t.Fatalf("status = %+v errors = %v", c.Status(), e.all())
	}
}

func TestCloseFromACallbackDoesNotWaitForItself(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	done := make(chan struct{})
	var once sync.Once
	var cp atomic.Pointer[Client]
	c := New(f.URL, "k", WithStream(false), WithOnError(func(error) {
		if cl := cp.Load(); cl != nil {
			cl.Close()
			once.Do(func() { close(done) })
		}
	}))
	cp.Store(c)
	// The first error can fire before the pointer is stored; poll again after a retry.
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		c.Flush()
	}
	closeAfter(t, c)
}

func TestHTTPSThroughALocalCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = io.WriteString(w, rulesetBody(boolFlag))
	}))
	defer srv.Close()
	c := New(srv.URL, "k", WithStream(false), WithHTTPClient(srv.Client()))
	defer c.Close()
	if !c.Ready() {
		t.Fatalf("not ready: %+v", c.Status())
	}
	if !c.BoolVariation("f", ctxFor("u"), false) {
		t.Fatal("wrong value")
	}
}

func TestEveryOperationRaces(t *testing.T) {
	f := newFake(t)
	var v atomic.Int64
	f.ruleset = func(w http.ResponseWriter, r *http.Request) {
		n := v.Add(1)
		w.Header().Set("ETag", `"v`+itoa(int(n))+`"`)
		_, _ = io.WriteString(w, rulesetBody(flagJSON("f", int(n), `[false,true]`)))
	}
	f.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		for i := 0; ; i++ {
			_, err := io.WriteString(w, "event: changed\ndata: \"x"+itoa(i)+"\"\n\n")
			if err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}
	e := &errs{}
	c := New(f.URL, "k", WithOnError(e.add), WithPollInterval(5*time.Millisecond), WithFlushInterval(5*time.Millisecond),
		WithAfterEvaluation(func(EvaluationEvent) {}))
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
				switch (g + i) % 7 {
				case 0:
					c.BoolVariationDetail("f", ctxFor("u"+itoa(i%5)), false)
				case 1:
					_ = c.Status()
				case 2:
					c.Ready()
				case 3:
					c.IntVariation("f", ctxFor("u"), 1)
				case 4:
					c.Flush()
				case 5:
					c.JSONVariation("f", ctxFor("u"), NullValue())
				case 6:
					c.ReadyContext(context.Background())
				}
			}
		}(g)
	}
	time.Sleep(300 * time.Millisecond)
	var closers sync.WaitGroup
	for i := 0; i < 4; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			c.Close()
		}()
	}
	closers.Wait()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %q", c.Status().Stream)
	}
}

// A client that is never closed must not keep the process alive: this test
// re-runs itself as a child that creates a client and returns from main.
func TestNeverClosedClientDoesNotKeepTheProcessAlive(t *testing.T) {
	if os.Getenv("TOGGLESLASH_ORPHAN_CHILD") != "" {
		f := newFake(t)
		_ = New(f.URL, "k")
		return
	}
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestNeverClosedClientDoesNotKeepTheProcessAlive$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_ORPHAN_CHILD=1")
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
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process outlived its main")
	}
}

func TestNoLogCarriesAContext(t *testing.T) {
	f := newFake(t)
	f.ruleset = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	e := &errs{}
	c := New(f.URL, "k", WithStream(false), WithOnError(e.add))
	c.BoolVariation("f", NewContext("secret-context-key").With("email", "x@y.z"), false)
	c.Ready()
	c.Close()
	all := strings.Join(e.all(), "\n")
	if strings.Contains(all, "secret-context-key") || strings.Contains(all, "x@y.z") {
		t.Fatalf("an error carried the context: %q", all)
	}
}
