package toggleslash

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// within runs work and fails the test when it has not returned by the timeout, so a deadlock is a failure and
// not a hung test run.
func within(t *testing.T, timeout time.Duration, what string, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("%s did not finish within %v", what, timeout)
	}
}

func TestEveryOperationFromManyGoroutines(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.pollInterval = 20 * time.Millisecond
		cfg.flushInterval = 20 * time.Millisecond
		cfg.requestTimeout = 300 * time.Millisecond
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	within(t, 20*time.Second, "the goroutines", func() {
		var group sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			group.Add(1)
			go func(worker int) {
				defer group.Done()
				for i := 0; i < 200; i++ {
					context := Context{"key": fmt.Sprintf("user-%d-%d", worker, i%5)}
					_ = client.Variation("checkout", context, nil)
					if i%20 == 0 {
						_ = client.Status()
						_ = client.Ready()
						_ = client.Int64Variation("checkout", context, 0)
					}
					if i%50 == 0 {
						server.publish(flagJSON("checkout", i%100 == 0, i+2))
					}
				}
			}(worker)
		}
		group.Add(1)
		go func() {
			defer group.Done()
			time.Sleep(20 * time.Millisecond)
			client.Close()
		}()
		group.Wait()
	})
	// Two closes racing each other, after one has already happened.
	within(t, 5*time.Second, "two closes", func() {
		var group sync.WaitGroup
		for i := 0; i < 2; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				client.Close()
			}()
		}
		group.Wait()
	})
}

func TestCloseRacingEvaluation(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	within(t, 10*time.Second, "close racing evaluation", func() {
		var group sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			group.Add(1)
			go func() {
				defer group.Done()
				for i := 0; i < 500; i++ {
					_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
				}
			}()
		}
		group.Add(1)
		go func() {
			defer group.Done()
			client.Close()
		}()
		group.Wait()
	})
}

func TestTwoClosesSendOneFinalFlush(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	within(t, 5*time.Second, "two closes", func() {
		var group sync.WaitGroup
		for i := 0; i < 2; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				client.Close()
			}()
		}
		group.Wait()
	})
	if got := server.countFor("/sdk/v1/events"); got != 1 {
		t.Errorf("%d final flushes went out, expected one", got)
	}
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	// A callback can run before New returns, so it reads the client through an atomic pointer.
	var holder atomic.Pointer[Client]
	client := start(t, server, func(cfg *config) {
		// A hook runs on the caller's goroutine, so it may read the client. Evaluating again from inside the
		// hook would call it again, so this one reads the state and asks ready, not variation.
		cfg.afterEvaluate = func(event EvaluationEvent) {
			if held := holder.Load(); held != nil {
				_ = held.Status()
				_ = held.Ready()
			}
		}
	})
	holder.Store(client)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	within(t, 5*time.Second, "a hook that calls back", func() {
		_ = client.Variation("checkout", Context{"key": "u"}, nil)
	})
}

func TestReadyAndCloseFromOnErrorOnTheSDKGoroutine(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	readyAnswers := make(chan bool, 4)
	closeDone := make(chan struct{})
	var once sync.Once
	var holder atomic.Pointer[Client]
	client := start(t, server, func(cfg *config) {
		cfg.requestTimeout = 200 * time.Millisecond
		cfg.onError = func(error) {
			// Both of these run on the sync goroutine, so neither may wait for its own work.
			held := holder.Load()
			if held == nil {
				return
			}
			readyAnswers <- held.Ready()
			once.Do(func() {
				held.Close()
				close(closeDone)
			})
		}
	})
	holder.Store(client)
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})
	within(t, 10*time.Second, "ready and close from onError", func() {
		<-readyAnswers
		<-closeDone
	})
	if client.Status().Stream != StreamClosed {
		t.Errorf("after a close from onError the stream read %v", client.Status().Stream)
	}
}

func TestStatusNeverShowsConnectedBesideAStreamError(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	// Each stream attempt announces the tag and then says nothing, so the short watchdog drops it and the next
	// attempt fails the same way: the state changes constantly while the readers look.
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/stream" {
			server.defaultAnswer(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(writer, "event: connected\ndata: %s\n\n", server.etag())
		flush(writer)
		<-request.Context().Done()
	})
	var reported recorder
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.pollInterval = 10 * time.Millisecond
		cfg.requestTimeout = 200 * time.Millisecond
		cfg.streamIdle = 20 * time.Millisecond
		cfg.onError = reported.add
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				status := client.Status()
				if status.Stream == StreamConnected && status.LastError != "" {
					t.Errorf("a status read showed %v beside %q", status.Stream, status.LastError)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if reported.count() == 0 {
		t.Error("no stream failure was reported, so the race was never exercised")
	}
	close(stop)
	readers.Wait()
}

func TestTheEtagAndTheFlagVersionComeFromOneRuleset(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	var mu sync.Mutex
	var mismatches []string
	client := start(t, server, func(cfg *config) {
		cfg.pollInterval = 5 * time.Millisecond
		cfg.afterEvaluate = func(event EvaluationEvent) {
			if event.FlagVersion == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			want := fmt.Sprintf("\"v%d\"", *event.FlagVersion)
			if event.ETag != want {
				mismatches = append(mismatches, fmt.Sprintf("%s with version %d", event.ETag, *event.FlagVersion))
			}
		}
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = client.Variation("checkout", Context{"key": "u"}, nil)
			}
		}
	}()
	for version := 2; version <= 40; version++ {
		server.publish(flagJSON("checkout", version%2 == 0, version))
		time.Sleep(3 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	readers.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(mismatches) != 0 {
		t.Errorf("%d hook events paired an etag with another ruleset's version: %v", len(mismatches), mismatches[:min(5, len(mismatches))])
	}
}

func TestOnErrorOnAnSDKGoroutineMayReadTheClient(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	seen := make(chan struct{}, 4)
	var holder atomic.Pointer[Client]
	// The stream route refuses from the first attempt, so each one reports before the next.
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sdk/v1/stream" {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		server.defaultAnswer(writer, request)
	})
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.requestTimeout = 300 * time.Millisecond
		cfg.onError = func(error) {
			held := holder.Load()
			if held == nil {
				return
			}
			// These run on the sync goroutine, so none of them may wait for it.
			_ = held.Status()
			_ = held.Variation("checkout", Context{"key": "u"}, nil)
			_ = held.Ready()
			seen <- struct{}{}
		}
	})
	holder.Store(client)
	// The first load works and the stream route refuses, so onError runs on the sync goroutine.
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	within(t, 10*time.Second, "onError to read the client", func() {
		for i := 0; i < 2; i++ {
			<-seen
		}
	})
}
