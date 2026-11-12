package toggleslash

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheLoopPollsAndPicksUpAChange(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.pollInterval = 40 * time.Millisecond
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	server.publish(flagJSON("checkout", false, 2))
	waitFor(t, 3*time.Second, "the polled change", func() bool {
		return !client.BoolVariation("checkout", Context{"key": "user-1"}, true)
	})
	if got := client.Status().ETag; got != `"v2"` {
		t.Errorf("the tag held was %q, expected \"v2\"", got)
	}
	// The poll after the change asks with the new tag, and gets a 304.
	waitFor(t, 3*time.Second, "a conditional request with the new tag", func() bool {
		for _, request := range server.seenRequests() {
			if request.headers.Get("If-None-Match") == `"v2"` {
				return true
			}
		}
		return false
	})
}

func TestTheStreamDeliversAChangeWithoutAPoll(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.pollInterval = 10 * time.Second
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	waitFor(t, 3*time.Second, "a connected stream", func() bool {
		return client.Status().Stream == StreamConnected
	})
	polls := server.countFor("/sdk/v1/ruleset")
	server.publish(flagJSON("checkout", false, 2))
	waitFor(t, 3*time.Second, "the streamed change", func() bool {
		return !client.BoolVariation("checkout", Context{"key": "user-1"}, true)
	})
	if got := server.countFor("/sdk/v1/ruleset"); got != polls+1 {
		t.Errorf("the change took %d ruleset requests, expected one", got-polls)
	}
	if got := client.Status().ETag; got != `"v2"` || client.Status().Stream != StreamConnected {
		t.Errorf("after the change the status was %+v", client.Status())
	}
}

func TestTheStreamWatchdogDropsAQuietStream(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/stream" {
			server.defaultAnswer(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		flush(writer)
		<-request.Context().Done()
	})
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.streamIdle = 60 * time.Millisecond
		cfg.pollInterval = 10 * time.Second
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	waitFor(t, 3*time.Second, "the watchdog to drop the stream", func() bool {
		return client.Status().LastError == LastErrorTimeout
	})
	waitFor(t, 3*time.Second, "a second stream attempt", func() bool {
		return server.countFor("/sdk/v1/stream") >= 2
	})
}

func TestAKeepAliveRestartsTheWatchdog(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	stop := make(chan struct{})
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/stream" {
			server.defaultAnswer(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(writer, "event: connected\ndata: %s\n\n", server.etag())
		flush(writer)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = writer.Write([]byte(": keep-alive\n\n"))
				flush(writer)
			case <-stop:
				return
			case <-request.Context().Done():
				return
			}
		}
	})
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.streamIdle = 60 * time.Millisecond
		cfg.pollInterval = 10 * time.Second
	})
	defer func() { close(stop); client.Close() }()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	waitFor(t, 3*time.Second, "a connected stream", func() bool {
		return client.Status().Stream == StreamConnected
	})
	time.Sleep(220 * time.Millisecond)
	status := client.Status()
	if status.Stream != StreamConnected || status.LastError != "" {
		t.Errorf("keep-alives did not hold the stream: %+v", status)
	}
}

func TestAHangingRulesetRequestDoesNotHoldAnEvaluation(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.pollInterval = 5 * time.Millisecond
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/ruleset" {
			server.defaultAnswer(writer, request)
			return
		}
		<-request.Context().Done()
	})
	started := time.Now()
	for i := 0; i < 2000; i++ {
		_ = client.BoolVariation("checkout", Context{"key": "user-1"}, false)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("2000 evaluations took %v while a request hung", elapsed)
	}
	if got := client.BoolVariation("checkout", Context{"key": "user-1"}, false); !got {
		t.Error("the last ruleset stopped answering")
	}
}

func TestAHangingFlushDoesNotHoldAnEvaluation(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.flushInterval = 10 * time.Millisecond
		cfg.requestTimeout = 300 * time.Millisecond
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sdk/v1/events" {
			<-request.Context().Done()
			return
		}
		server.defaultAnswer(writer, request)
	})
	_ = client.BoolVariation("checkout", Context{"key": "user-1"}, false)
	waitFor(t, 2*time.Second, "a hanging flush", func() bool {
		return server.countFor("/sdk/v1/events") > 0
	})
	started := time.Now()
	for i := 0; i < 2000; i++ {
		_ = client.BoolVariation("checkout", Context{"key": "user-1"}, false)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("2000 evaluations took %v while a flush hung", elapsed)
	}
}

// fakeClock is a monotonic clock a test moves by hand. The SDK reads it from its own goroutines, so both the
// read and the move go through a mutex.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(by time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(by)
	c.mu.Unlock()
}

func TestARetryAfterAboveTheCapIsHeldForThreeHundredSeconds(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		stream bool
		hold   func(*Client) *retryHold
	}{
		{"ruleset", "/sdk/v1/ruleset", false, func(c *Client) *retryHold { return c.hold }},
		{"stream", "/sdk/v1/stream", true, func(c *Client) *retryHold { return c.hold }},
		{"events", "/sdk/v1/events", false, func(c *Client) *retryHold { return c.usage.hold }},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			clock := &fakeClock{at: time.Unix(1_700_000_000, 0)}
			server := newFakeServer(t, flagJSON("checkout", true, 1))
			server.set(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != one.path {
					server.defaultAnswer(writer, request)
					return
				}
				writer.Header().Set("Retry-After", "10000")
				writer.WriteHeader(http.StatusTooManyRequests)
			})
			client := start(t, server, func(cfg *config) {
				cfg.stream = one.stream
				cfg.monotonic = clock.now
				cfg.flushInterval = 20 * time.Millisecond
				cfg.pollInterval = 20 * time.Millisecond
			})
			defer client.Close()
			if one.name == "events" {
				// A first load that succeeds, a count to send, then the held events route.
				server.set(func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/sdk/v1/events" {
						writer.Header().Set("Retry-After", "10000")
						writer.WriteHeader(http.StatusTooManyRequests)
						return
					}
					server.defaultAnswer(writer, request)
				})
				if !client.Ready() {
					t.Fatal("the first load failed")
				}
				_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
			}
			held := one.hold(client)
			waitFor(t, 3*time.Second, "the hold", func() bool {
				return held.remaining() > 0
			})
			if got := held.remaining(); got != 300*time.Second {
				t.Errorf("the hold was %v, expected 300s", got)
			}
			clock.advance(301 * time.Second)
			if got := held.remaining(); got != 0 {
				t.Errorf("the hold did not pass after 301 s: %v", got)
			}
		})
	}
}

func TestARetryAfterThatIsNotWholeSecondsIsIgnored(t *testing.T) {
	for _, value := range []string{"1.5", "Wed, 21 Oct 2015 07:28:00 GMT", "-1", "", " 1 2"} {
		if _, ok := retryAfter(429, value); ok {
			t.Errorf("%q was read as a Retry-After", value)
		}
	}
	if wait, ok := retryAfter(429, " 2 \t"); !ok || wait != 2*time.Second {
		t.Errorf("a whole number of seconds with spaces gave %v, %v", wait, ok)
	}
	if wait, ok := retryAfter(200, "2"); ok {
		t.Errorf("a Retry-After outside a 429 gave %v", wait)
	}
}

func TestCloseStopsTheStreamAndThePollsAtOnce(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.pollInterval = 10 * time.Millisecond
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	waitFor(t, 3*time.Second, "a connected stream", func() bool {
		return client.Status().Stream == StreamConnected
	})
	client.Close()
	if got := client.Status().Stream; got != StreamClosed {
		t.Errorf("after close the stream read %v", got)
	}
	waitFor(t, 2*time.Second, "the server to see the stream end", func() bool {
		return server.openStreams() == 0
	})
	before := len(server.seenRequests())
	time.Sleep(200 * time.Millisecond)
	if after := len(server.seenRequests()); after != before {
		t.Errorf("%d requests reached the server after close", after-before)
	}
	// Evaluation still answers from the last ruleset, and counts nothing.
	pending := client.usage.pendingCount()
	if got := client.BoolVariation("checkout", Context{"key": "u"}, false); !got {
		t.Error("a closed client stopped answering from the ruleset held")
	}
	if client.usage.pendingCount() != pending {
		t.Error("a closed client counted an evaluation")
	}
}

func TestCloseBeforeTheFirstLoadSettlesReadyFalse(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	})
	client := start(t, server, func(cfg *config) { cfg.requestTimeout = 5 * time.Second })
	time.Sleep(20 * time.Millisecond)
	client.Close()
	if client.Ready() {
		t.Error("ready answered true after a close before the first load")
	}
}

func TestTheStreamReconnectsAfterAFailedRefresh(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	var refuse atomic.Bool
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sdk/v1/ruleset" && refuse.Load() {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		server.defaultAnswer(writer, request)
	})
	client := start(t, server, func(cfg *config) {
		cfg.stream = true
		cfg.pollInterval = 200 * time.Millisecond
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	waitFor(t, 3*time.Second, "a connected stream", func() bool {
		return client.Status().Stream == StreamConnected
	})
	refuse.Store(true)
	server.publish(flagJSON("checkout", false, 2))
	waitFor(t, 3*time.Second, "the failed refresh", func() bool {
		return client.Status().LastError == "http_500"
	})
	refuse.Store(false)
	waitFor(t, 4*time.Second, "the catch-up", func() bool {
		return !client.BoolVariation("checkout", Context{"key": "u"}, true)
	})
}

func TestABodyThatNeverFinishesIsATimeout(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"flags":[`))
		flush(writer)
		<-request.Context().Done()
	})
	client := start(t, server, func(cfg *config) { cfg.requestTimeout = 80 * time.Millisecond })
	defer client.Close()
	if client.Ready() {
		t.Error("ready answered true for a body that never finished")
	}
	if got := client.Status().LastError; got != LastErrorTimeout {
		t.Errorf("lastError was %q, expected timeout", got)
	}
	if detail := client.VariationDetail("checkout", Context{"key": "u"}, "fallback"); detail.Reason.Code != CodeNotReady {
		t.Errorf("after an endless body the client answered %v", detail.Reason)
	}
}
