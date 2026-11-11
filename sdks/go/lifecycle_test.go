package toggleslash

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sseServer scripts the stream route: each call to send writes raw text to the open connection.
type sseServer struct {
	f      *fakeServer
	opened atomic.Int32
	ch     chan string
}

func newSSE(f *fakeServer) *sseServer {
	s := &sseServer{f: f, ch: make(chan string, 64)}
	f.handle(pathStream, func(w http.ResponseWriter, r *http.Request) {
		s.opened.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl := w.(http.Flusher)
		fl.Flush()
		for {
			select {
			case text := <-s.ch:
				if text == "\x00end" {
					return
				}
				_, _ = io.WriteString(w, text)
				fl.Flush()
			case <-r.Context().Done():
				return
			case <-f.quit:
				return
			}
		}
	})
	return s
}

func (s *sseServer) send(text string) { s.ch <- text }
func (s *sseServer) end()             { s.ch <- "\x00end" }

func TestStreamConnectsAndFollowsChanges(t *testing.T) {
	f := newFakeServer(t)
	var version atomic.Int32
	version.Store(1)
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		v := int(version.Load())
		serveRuleset(rulesetJSON(v, v%2), fmt.Sprintf(`"v%d"`, v))(w, r)
	})
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	s := newSSE(f)
	log := &errLog{}
	c := NewClient(f.url(), "k", WithOnError(log.add), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	if !c.Ready() {
		t.Fatalf("not ready: %+v", c.Status())
	}
	s.send("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	before := len(f.seen(pathRuleset))
	first := c.Status().LastSyncAt
	// A matching tag moves lastSyncAt without a fetch.
	time.Sleep(5 * time.Millisecond)
	s.send("event: changed\ndata: \"v1\"\n\n")
	waitFor(t, "lastSyncAt to move", func() bool { return c.Status().LastSyncAt.After(first) })
	if len(f.seen(pathRuleset)) != before {
		t.Fatal("a matching tag fetched the ruleset")
	}
	// A keep-alive moves nothing.
	moved := c.Status().LastSyncAt
	s.send(": keep-alive\n\n")
	time.Sleep(30 * time.Millisecond)
	if !c.Status().LastSyncAt.Equal(moved) {
		t.Fatal("a keep-alive moved lastSyncAt")
	}
	// A new tag fetches and swaps.
	version.Store(2)
	s.send("event: changed\ndata: \"v2\"\n\n")
	waitFor(t, "the new ruleset", func() bool { return c.Status().ETag == `"v2"` })
	if c.BoolVariation("flag", user, true) {
		t.Fatal("flag still serves the old ruleset")
	}
	if log.count() != 0 {
		t.Fatalf("errors %v", log.all())
	}
	req := f.seen(pathStream)[0]
	if req.header.Get("Accept") != "text/event-stream" || req.header.Get("Authorization") != "Bearer k" {
		t.Errorf("stream request headers = %v", req.header)
	}
	c.Close()
	if c.Status().Stream != StreamClosed {
		t.Fatalf("stream = %q after Close", c.Status().Stream)
	}
	if log.count() != 0 {
		t.Fatalf("Close caused errors: %v", log.all())
	}
}

func TestStreamDroppedAfterConnectingWaitsAndDoesNotPoll(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	s := newSSE(f)
	c := NewClient(f.url(), "k", WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withRandom(func() float64 { return 0 }))
	defer c.Close()
	c.Ready()
	s.send("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	polls := len(f.seen(pathRuleset))
	s.end()
	waitFor(t, "disconnected", func() bool { return c.Status().Stream != StreamConnected })
	// It reconnects after about 0.5 s (failures = 0, 1 s ceiling, jitter 0.5) without a poll in between.
	waitFor(t, "a reconnect", func() bool { return s.opened.Load() >= 2 })
	if got := len(f.seen(pathRuleset)); got != polls {
		t.Fatalf("polled %d times while only the stream dropped", got-polls)
	}
}

func TestStreamThatNeverConnectedPollsOnceThenRetries(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f.handle(pathStream, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	log := &errLog{}
	c := NewClient(f.url(), "k", WithOnError(log.add), WithPollInterval(80*time.Millisecond), WithFlushInterval(time.Hour), withRandom(func() float64 { return 0 }))
	defer c.Close()
	c.Ready()
	waitFor(t, "a poll after a failed stream", func() bool { return len(f.seen(pathRuleset)) >= 2 })
	waitFor(t, "a second stream attempt", func() bool { return len(f.seen(pathStream)) >= 2 })
	if !log.has("http_500") {
		t.Fatalf("errors %v", log.all())
	}
}

func TestWatchdogEndsAQuietStreamAndAKeepAliveRestartsIt(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	s := newSSE(f)
	log := &errLog{}
	c := NewClient(f.url(), "k", WithOnError(log.add), WithPollInterval(time.Hour), WithFlushInterval(time.Hour), withWatchdog(300*time.Millisecond))
	defer c.Close()
	c.Ready()
	s.send("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	// Keep-alives every 100 ms for a full second keep the stream up past three watchdog periods.
	for range 10 {
		s.send(": keep-alive\n\n")
		time.Sleep(100 * time.Millisecond)
	}
	if c.Status().Stream != StreamConnected || s.opened.Load() != 1 {
		t.Fatalf("a keep-alive did not restart the watchdog: %+v, opened %d", c.Status(), s.opened.Load())
	}
	// Then silence: the watchdog ends the stream and it reports a timeout.
	waitFor(t, "the watchdog", func() bool { return c.Status().Stream != StreamConnected })
	waitFor(t, "a timeout error", func() bool { return log.has("timeout") })
}

func TestOpeningAStreamIsBoundedByTheRequestTimeout(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f.handle(pathStream, f.hang)
	log := &errLog{}
	c := NewClient(f.url(), "k", WithOnError(log.add), WithRequestTimeout(150*time.Millisecond), WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	defer c.Close()
	c.Ready()
	waitFor(t, "a timeout", func() bool { return log.has("timeout") })
	// The hang is the whole response: no header was written, so only the open timer can end it.
	if c.Status().LastError != LastErrorTimeout {
		t.Fatalf("status %+v", c.Status())
	}
}

func TestSwapPairsTheETagWithItsRuleset(t *testing.T) {
	f := newFakeServer(t)
	var n atomic.Int32
	// Version v is served with etag "v<v>", and the flag serves index v%2, so a hook can check the pair.
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		v := int(n.Add(1))
		serveRuleset(rulesetJSON(v, v%2), fmt.Sprintf(`"v%d"`, v))(w, r)
	})
	var bad atomic.Int32
	c, _ := newTestClient(t, f, WithPollInterval(time.Millisecond), WithAfterEvaluation(func(e EvaluationEvent) {
		if e.FlagVersion == nil || e.ETag != fmt.Sprintf(`"v%d"`, *e.FlagVersion) {
			bad.Add(1)
		}
		if b, _ := e.Value.Bool(); e.FlagVersion != nil && b != (*e.FlagVersion%2 == 1) {
			bad.Add(1)
		}
	}))
	c.Ready()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(400 * time.Millisecond)
			for time.Now().Before(deadline) {
				c.BoolVariation("flag", user, false)
				c.Status()
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d evaluations saw an etag that did not match the ruleset", bad.Load())
	}
	if n.Load() < 5 {
		t.Fatalf("only %d swaps happened", n.Load())
	}
}

func TestConcurrentUseWithCloseRacing(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	c, _ := newTestClient(t, f, WithPollInterval(2*time.Millisecond), WithFlushInterval(3*time.Millisecond))
	c.Ready()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 300 {
				switch (i + j) % 6 {
				case 0:
					c.BoolVariation("flag", user, false)
				case 1:
					c.StringVariationDetail("flag", UserContext(fmt.Sprint(j)), "x")
				case 2:
					c.Status()
				case 3:
					c.Ready()
				case 4:
					c.AnyVariation("flag", user, NullValue())
				case 5:
					if j == 150 {
						c.Close()
					}
				}
			}
		}()
	}
	wg.Wait()
	// Evaluation still answers from the ruleset held after Close.
	if !c.BoolVariation("flag", user, false) {
		t.Fatal("no answer after Close")
	}
	c.Close()
}

func TestTwoRacingClosesBothReturn(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	c, log := newTestClient(t, f)
	c.Ready()
	c.BoolVariation("flag", user, false)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	wg.Wait()
	if n := len(f.seen(pathEvents)); n != 1 {
		t.Fatalf("%d final flushes", n)
	}
	if log.count() != 0 {
		t.Fatalf("errors %v", log.all())
	}
	if st := c.Status(); st.Stream != StreamClosed {
		t.Fatalf("status %+v", st)
	}
}

func TestCloseIsBoundedWhileTheRulesetRequestHangs(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, f.hang)
	c, log := newTestClient(t, f, WithRequestTimeout(time.Hour))
	start := time.Now()
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
	if c.Ready() {
		t.Fatal("ready after Close")
	}
	if log.count() != 0 {
		t.Fatalf("Close caused errors: %v", log.all())
	}
	if st := c.Status(); st.LastError != "" || st.Stream != StreamClosed {
		t.Fatalf("status %+v", st)
	}
}

func TestCloseIsBoundedWhileAFlushHangs(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathEvents, f.hang)
	c, _ := readyClient(t, f, WithRequestTimeout(200*time.Millisecond))
	c.BoolVariation("flag", user, false)
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > 2*400*time.Millisecond+time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestCloseWhileTheStreamIsOpenAndQuiet(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	s := newSSE(f)
	c := NewClient(f.url(), "k", WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	c.Ready()
	s.send("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	start := time.Now()
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
}

func TestCallbacksMayCallBackIntoTheClient(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	var c *Client
	built := make(chan struct{})
	var inside atomic.Int32
	done := make(chan struct{})
	var once sync.Once
	c = NewClient(f.url(), "k", WithStream(false), WithFlushInterval(time.Hour), WithOnError(func(error) {
		<-built
		// onError runs on an SDK goroutine: none of these may wait for the SDK.
		c.Status()
		c.Ready()
		c.BoolVariation("flag", user, false)
		inside.Add(1)
		once.Do(func() { c.Close(); close(done) })
	}))
	close(built)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from onError waited for the SDK")
	}
	c.Close() // from the test goroutine this one does wait, and returns
	if inside.Load() == 0 {
		t.Fatal("onError never ran")
	}
}

func TestHookMayCallBackIntoTheClient(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	var c *Client
	built := make(chan struct{})
	c = NewClient(f.url(), "k", WithStream(false), WithFlushInterval(time.Hour), WithAfterEvaluation(func(EvaluationEvent) {
		<-built
		c.Status()
		c.Ready()
	}))
	close(built)
	defer c.Close()
	if !c.Ready() {
		t.Fatal("not ready")
	}
	done := make(chan bool)
	go func() { done <- c.BoolVariation("flag", user, false) }()
	select {
	case got := <-done:
		if !got {
			t.Fatal("wrong answer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a hook that calls Ready deadlocked")
	}
}

func TestConcurrentRequestsAreSerialPerRoute(t *testing.T) {
	f := newFakeServer(t)
	var inflight, peak atomic.Int32
	f.handle(pathRuleset, func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inflight.Add(-1)
		serveRuleset(rulesetJSON(1, 1), `"v1"`)(w, r)
	})
	c, _ := newTestClient(t, f, WithPollInterval(time.Millisecond))
	c.Ready()
	time.Sleep(150 * time.Millisecond)
	if peak.Load() > 1 {
		t.Fatalf("%d ruleset requests in flight at once", peak.Load())
	}
}

// The header audit: every request the SDK makes, per route, with the headers the server saw.
func TestHeaderAudit(t *testing.T) {
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	f.handle(pathEvents, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	s := newSSE(f)
	c := NewClient(f.url(), "audit-key", WithPollInterval(20*time.Millisecond), WithFlushInterval(time.Hour))
	c.Ready()
	s.send("event: connected\ndata: \"v1\"\n\n")
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	waitFor(t, "a stream request", func() bool { return len(f.seen(pathStream)) > 0 })
	// A new tag makes a fetch that holds a ruleset and an etag, so it is conditional.
	s.send("event: changed\ndata: \"v2\"\n\n")
	waitFor(t, "a conditional fetch", func() bool { return len(f.seen(pathRuleset)) >= 2 })
	c.BoolVariation("flag", user, false)
	c.Close()

	// What the SDK sets, and what net/http adds on its own (see RUN.md for why none identifies a person).
	sdkSets := map[string]bool{"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true}
	httpAdds := map[string]bool{"User-Agent": true, "Accept-Encoding": true, "Content-Length": true, "Connection": true}
	for _, path := range []string{pathRuleset, pathStream, pathEvents} {
		reqs := f.seen(path)
		if len(reqs) == 0 {
			t.Fatalf("no request to %s", path)
		}
		names := map[string]bool{}
		for _, r := range reqs {
			for name := range r.header {
				names[name] = true
				if !sdkSets[name] && !httpAdds[name] {
					t.Errorf("%s: unexpected header %q = %v", path, name, r.header[name])
				}
			}
			if r.header.Get("Authorization") != "Bearer audit-key" {
				t.Errorf("%s: Authorization = %q", path, r.header.Get("Authorization"))
			}
			if ua := r.header.Get("User-Agent"); ua != "" && !strings.HasPrefix(ua, "Go-http-client/") {
				t.Errorf("%s: User-Agent = %q", path, ua)
			}
			if r.header.Get("Cookie") != "" || r.header.Get("Referer") != "" {
				t.Errorf("%s: cookie or referer sent", path)
			}
		}
		list := make([]string, 0, len(names))
		for n := range names {
			list = append(list, n)
		}
		sort.Strings(list)
		t.Logf("capture %-18s method=%s host=%s headers=%v", path, reqs[0].method, reqs[0].host, list)
	}
	// If-None-Match only on a request that holds a ruleset and an etag: not the first.
	if f.seen(pathRuleset)[0].header.Get("If-None-Match") != "" {
		t.Error("first ruleset request sent If-None-Match")
	}
	if !slices.ContainsFunc(f.seen(pathRuleset), func(r seen) bool { return r.header.Get("If-None-Match") == `"v1"` }) {
		t.Error("no conditional request")
	}
}

func TestNoRegexpAndNoContextLoggingInTheSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	logging := regexp.MustCompile(`\b(log\.|slog\.|fmt\.(Print|Fprint)|os\.Std(out|err)|println\()`)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "test-service") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.Contains(src, `"regexp"`) {
			t.Errorf("%s imports regexp: the pattern engine is hand-written", name)
		}
		for _, call := range []string{"json.Unmarshal(", "json.NewDecoder(", "json.Marshal(", "json.Valid("} {
			if strings.Contains(src, call) {
				t.Errorf("%s calls %s: the JSON reader and writer are hand-written", name, call)
			}
		}
		if loc := logging.FindStringIndex(src); loc != nil {
			t.Errorf("%s writes output: %q", name, src[loc[0]:min(loc[0]+30, len(src))])
		}
	}
}

// The subprocess helper: a program that creates a client and exits without Close must exit at once.
func TestUnclosedClientDoesNotBlockExit(t *testing.T) {
	if os.Getenv("TOGGLESLASH_EXIT_HELPER") != "" {
		c := NewClient(os.Getenv("TOGGLESLASH_EXIT_URL"), "k", WithPollInterval(time.Millisecond), WithFlushInterval(time.Millisecond))
		c.Ready()
		c.BoolVariation("flag", user, false)
		os.Exit(0)
	}
	f := newFakeServer(t)
	f.handle(pathRuleset, serveRuleset(rulesetJSON(1, 1), `"v1"`))
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestUnclosedClientDoesNotBlockExit$")
	cmd.Env = append(os.Environ(), "TOGGLESLASH_EXIT_HELPER=1", "TOGGLESLASH_EXIT_URL="+f.url())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process did not exit")
	}
}
