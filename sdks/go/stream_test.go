package toggleslash

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

func TestStreamFollowsAChangedTag(t *testing.T) {
	fake := newFakeServer(t)
	var rulesetRequests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		version := rulesetRequests.Add(1)
		if version == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 2, 0))))
	}
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("event: changed\ndata: \"v2\"\n\n"))
	}
	on := true
	clk := newFakeClock()
	opts := clockOptions(fake, clk)
	opts.Stream = &on
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	eventually(t, 3*time.Second, func() bool {
		status := client.Status()
		return status.ETag == `"v2"` && status.Stream == StreamDisconnected
	}, "the changed tag's ruleset")
	if got := rulesetRequests.Load(); got != 2 {
		t.Fatalf("ruleset requests = %d, want 2 (the first load and the changed tag)", got)
	}
	// A stream that was up and dropped waits 1 s of ceiling, halved by a zero
	// jitter, and does not poll.
	clk.waitForDuration(t, 500*time.Millisecond, 3*time.Second)
	client.Close()
	if got := rulesetRequests.Load(); got != 2 {
		t.Fatalf("ruleset requests = %d, want no poll after a stream that was up", got)
	}
}

func TestStreamWatchdogDropsASilentStream(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	quiet := make(chan struct{})
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// A real loopback socket that goes quiet: nothing follows, and the
		// handler holds the response open.
		select {
		case <-quiet:
		case <-r.Context().Done():
		}
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.clock = nil // a real watchdog on a real socket
	opts.Stream = &on
	opts.watchdog = 80 * time.Millisecond
	client := newClient(t, opts)
	defer close(quiet)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	eventually(t, 3*time.Second, func() bool {
		status := client.Status()
		return status.Stream == StreamDisconnected && status.LastError != nil && *status.LastError == LastErrorTimeout
	}, "the watchdog dropping a silent stream as a timeout")
}

func TestStreamKeepAliveRestartsTheWatchdog(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		for i := 0; i < 8; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(25 * time.Millisecond):
			}
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
		// Hold the stream open past the assertion: the test is that the
		// watchdog did not drop it, and a stream the handler ends cleanly
		// would turn disconnected under a slow scheduler, not because of the
		// watchdog.
		<-r.Context().Done()
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.clock = nil // a real watchdog on a real socket
	opts.Stream = &on
	opts.watchdog = 80 * time.Millisecond
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	eventually(t, 3*time.Second, func() bool { return client.Status().Stream == StreamConnected }, "the connected event")
	// Well past the watchdog's length, and still connected: every comment
	// restarted it.
	time.Sleep(150 * time.Millisecond)
	status := client.Status()
	if status.Stream != StreamConnected {
		t.Fatalf("stream = %q after keep-alives, want connected", status.Stream)
	}
	if status.LastError != nil {
		t.Fatalf("lastError = %q after keep-alives", *status.LastError)
	}
}

func TestStreamCleanEndIsNotAnError(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.Stream = &on
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	eventually(t, 3*time.Second, func() bool { return fake.count("/sdk/v1/stream") == 1 }, "one stream attempt")
	eventually(t, 3*time.Second, func() bool { return client.Status().Stream == StreamDisconnected }, "the stream's clean end")
	if status := client.Status(); status.LastError != nil {
		t.Fatalf("lastError = %q after a clean end, want none", *status.LastError)
	}
}

func TestSSEParser(t *testing.T) {
	input := "event: connected\r\ndata: \"a\"\r\n\r\n" +
		"data: one\ndata: two\n\n" +
		": keep-alive\n\n" +
		"event: empty\n\n" +
		"data: lone-cr\r\r" +
		"event: x\ndata: partial"
	reader := newSSEReader(bufio.NewReaderSize(iotest.OneByteReader(strings.NewReader(input)), 8), func() {})

	want := []struct {
		name string
		data string
	}{
		{name: "connected", data: `"a"`},
		{name: "message", data: "one\ntwo"},
		{name: "message", data: "lone-cr"},
	}
	for _, expected := range want {
		name, data, ok, err := reader.next()
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if !ok {
			t.Fatalf("an event with data was dropped: %+v", expected)
		}
		if name != expected.name || data != expected.data {
			t.Fatalf("event = (%q, %q), want (%q, %q)", name, data, expected.name, expected.data)
		}
	}
	if _, _, _, err := reader.next(); err != io.EOF {
		t.Fatalf("after the last event: err = %v, want EOF with the incomplete event discarded", err)
	}
}

func TestTLSURLReachesTheTLSPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/ruleset" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"tls"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}))
	t.Cleanup(server.Close)

	off := false
	client := newClient(t, Options{
		URL:            server.URL,
		SDKKey:         "test-key",
		Stream:         &off,
		RequestTimeout: 2 * time.Second,
		httpClient:     server.Client(),
	})
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true over https")
	}
	if !client.BoolVariation("feature", userContext("u1"), false) {
		t.Fatalf("an https client served the fallback")
	}
	if status := client.Status(); status.ETag != `"tls"` {
		t.Fatalf("etag = %q over https", status.ETag)
	}
}
