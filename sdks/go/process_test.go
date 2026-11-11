package toggleslash

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestAClientThatIsNeverClosedDoesNotKeepTheProcessAlive: Go ends a process when main returns whatever
// goroutines run, so an unclosed client cannot hold it; this proves it with a real child process whose
// client has a request in flight against a server that never answers.
func TestAClientThatIsNeverClosedDoesNotKeepTheProcessAlive(t *testing.T) {
	if os.Getenv("TOGGLESLASH_TEST_UNCLOSED_URL") != "" {
		unclosedChild(os.Getenv("TOGGLESLASH_TEST_UNCLOSED_URL"))
		return
	}
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) { f.hang() })
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestAClientThatIsNeverClosedDoesNotKeepTheProcessAlive$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_TEST_UNCLOSED_URL="+f.URL)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the child failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("a process with an unclosed client did not exit")
	}
	if len(f.requests("/sdk/v1/ruleset")) == 0 {
		t.Error("the child never reached the server, so the test proved nothing")
	}
}

// unclosedChild is the child's whole program: start a client, wait for its request to be in flight,
// and return without Close.
func unclosedChild(url string) {
	c := New(url, "k")
	_ = c
	time.Sleep(300 * time.Millisecond)
	fmt.Fprintln(os.Stderr, "child done")
	os.Exit(0)
}
