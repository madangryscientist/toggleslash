package toggleslash

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// runSync is the sync goroutine: the first load, then the loop of contract section 7.3.
func (c *Client) runSync() {
	defer close(c.syncLoop.done)
	// The first load settles Ready before it reports its failure, so a Ready called from onError answers at once.
	err := c.fetchRuleset()
	c.settleReady(c.snap.Load() != nil)
	c.reportError(c.syncLoop, err)

	failures := 0
	for c.ctx.Err() == nil {
		if c.cfg.stream {
			// A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
			if !c.wait(0) {
				return
			}
		}
		connected := false
		if c.cfg.stream {
			connected = c.follow()
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.wait(c.delay(failures)) {
			return
		}
		// Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was up
		// needs no poll, since reconnecting announces the tag and catches up on its own.
		if !connected && c.ctx.Err() == nil {
			c.reportError(c.syncLoop, c.fetchRuleset())
		}
	}
}

// wait sleeps d, or until the Retry-After hold has passed if that is later. False when the client closed.
func (c *Client) wait(d time.Duration) bool {
	return sleep(c.ctx, c.cfg.clock, max(d, c.holdWait()))
}

// delay is how long to wait after a stream attempt: with the stream on, an exponential backoff from a second
// up to the poll interval, jittered into [0.5, 1) of the ceiling; with it off, the poll interval.
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	ceiling := min(c.cfg.pollInterval, backoffBase<<min(failures, 16))
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.random()/2))
}

func (c *Client) setLastError(code string) {
	c.mu.Lock()
	c.lastError = code
	c.mu.Unlock()
}

// synced records that the ruleset held is confirmed current.
func (c *Client) synced() {
	now := c.cfg.clock.Now()
	c.mu.Lock()
	c.lastSyncAt = now
	c.lastError = ""
	c.mu.Unlock()
}

func (c *Client) setStream(s StreamState) {
	c.mu.Lock()
	c.stream = s
	c.mu.Unlock()
}

// fail records a failure in lastError and returns it for the caller to report (after it has released
// everything). A failure caused by the client closing is neither recorded nor returned.
func (c *Client) fail(e *syncError, closed bool) error {
	if closed || e == nil || c.ctx.Err() != nil {
		return nil
	}
	c.setLastError(e.code)
	return e
}

// statusFailure is the failure for an answer that is not the one wanted. A 429 starts a hold.
func (c *Client) statusFailure(what string, r reply) error {
	if r.status == http.StatusTooManyRequests {
		if wait, ok := retryAfter(r.header); ok {
			c.holdFor(wait)
		}
	}
	return c.fail(newSyncError(what, "http_"+strconv.Itoa(r.status)), false)
}

// fetchRuleset makes one conditional ruleset request. It returns the failure to report, or nil.
func (c *Client) fetchRuleset() error {
	const what = "ruleset request"
	held := c.snap.Load()
	headers := []header{{"Authorization", "Bearer " + c.cfg.sdkKey}}
	if held != nil && held.etag != "" {
		headers = append(headers, header{"If-None-Match", held.etag})
	}
	r, e, closed := c.do(c.ctx, http.MethodGet, "/sdk/v1/ruleset", what, headers, nil, true)
	if e != nil || closed {
		return c.fail(e, closed)
	}
	switch {
	case r.status == http.StatusNotModified:
		// A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is unexpected.
		if held == nil {
			return c.statusFailure(what, r)
		}
		c.synced()
		return nil
	case r.status != http.StatusOK:
		return c.statusFailure(what, r)
	}
	rules, ok := loadRuleset(r.body)
	if !ok {
		return c.fail(newSyncError(what, LastErrorBadRuleset), false)
	}
	c.mu.Lock()
	c.snap.Store(&snapshot{rules: rules, etag: r.header.Get("ETag")})
	c.lastSyncAt = c.cfg.clock.Now()
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// loadRuleset reads a ruleset body: WHATWG UTF-8, a byte order mark skipped, a JSON object whose flags and
// segments are both arrays. Anything else, and anything that makes the loader panic, is not a ruleset.
func loadRuleset(body []byte) (rules *evaluator.Ruleset, ok bool) {
	defer func() {
		if recover() != nil {
			rules, ok = nil, false
		}
	}()
	value, err := jsonv.Parse(jsonv.StripBOM(jsonv.DecodeUTF8(body)))
	if err != nil {
		return nil, false
	}
	if _, isArray := value.Get("flags").AsArray(); !isArray {
		return nil, false
	}
	if _, isArray := value.Get("segments").AsArray(); !isArray {
		return nil, false
	}
	return evaluator.Load(value), true
}

// watchdog cancels a request's context with errTimeout unless it is armed again or stopped first.
type watchdog struct {
	clock  clock
	cancel context.CancelCauseFunc
	timer  timer
}

func (w *watchdog) arm(d time.Duration) {
	w.stop()
	w.timer = w.clock.AfterFunc(d, func() { w.cancel(errTimeout) })
}

func (w *watchdog) stop() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// follow follows GET /sdk/v1/stream until it ends or fails. It reports whether the stream connected, so the
// caller can tell a stream that was up from one that never was.
func (c *Client) follow() (connected bool) {
	const what = "stream"
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)

	ctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)
	dog := &watchdog{clock: c.cfg.clock, cancel: cancel}
	defer dog.stop()
	// The open is bounded by requestTimeout; once the headers are in, the idle watchdog takes over.
	dog.arm(c.cfg.requestTimeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.url+"/sdk/v1/stream", nil)
	if err != nil {
		c.reportError(c.syncLoop, c.fail(newSyncError(what, LastErrorNetwork), false))
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		e, closed := classify(c.ctx, ctx, what)
		c.reportError(c.syncLoop, c.fail(e, closed))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		c.reportError(c.syncLoop, c.statusFailure(what, reply{status: resp.StatusCode, header: resp.Header}))
		return false
	}
	return c.pump(ctx, resp.Body, dog)
}

// pump reads events until the stream ends. The watchdog is armed before every read, so any byte (a keep-alive
// comment too) restarts it, and stopped while an event is handled.
func (c *Client) pump(ctx context.Context, body io.Reader, dog *watchdog) (connected bool) {
	const what = "stream"
	var parser sseParser
	buf := make([]byte, 4096)
	for {
		dog.arm(c.cfg.idle)
		n, err := body.Read(buf)
		dog.stop()
		failed := false
		parser.feed(buf[:n], func(ev sseEvent) {
			if failed || (ev.name != "changed" && ev.name != "connected") {
				return
			}
			if ev.name == "connected" {
				connected = true
				c.mu.Lock()
				c.stream = StreamConnected
				c.lastError = ""
				c.mu.Unlock()
			}
			if e := c.onTag(ev.data); e != nil {
				// The fetch the stream asked for failed, and a stream that stays up would never retry it. End
				// it and count it as not connected, so the backoff and the poll take over.
				failed = true
				connected = false
				c.reportError(c.syncLoop, e)
			}
		})
		if failed {
			return false
		}
		if err == io.EOF {
			return connected
		}
		if err != nil {
			e, closed := classify(c.ctx, ctx, what)
			c.reportError(c.syncLoop, c.fail(e, closed))
			return connected
		}
	}
}

// onTag handles a tag the stream announced: a sync if it is the ETag held, a ruleset request if not. It
// returns the failure of that request.
func (c *Client) onTag(tag string) error {
	if held := c.snap.Load(); held != nil && held.etag != "" && held.etag == tag {
		c.synced()
		return nil
	}
	return c.fetchRuleset()
}
