package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// syncLoop is the sync goroutine: the first load, then the loop of contract section 7.3 until Close.
func (c *Client) syncLoop() {
	defer c.loops.done()
	c.refresh(false)
	failures := 0
	for c.ctx.Err() == nil {
		if c.cfg.stream && !c.waitHold() {
			return
		}
		connected := c.cfg.stream && c.follow()
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.waitAtLeast(c.delay(failures)) {
			return
		}
		// Wait first: a stream that was up needs no poll, since reconnecting announces the tag and catches up.
		if !connected {
			c.refresh(false)
		}
	}
}

// delay is the wait after a stream attempt: the poll interval with the stream off, otherwise an exponential
// backoff from one second up to the poll interval, jittered to between half and all of it.
func (c *Client) delay(failures int) time.Duration {
	poll := c.cfg.pollInterval
	if !c.cfg.stream {
		return poll
	}
	ceiling := min(poll, backoffBase<<min(failures, 16))
	return time.Duration(float64(ceiling) * (0.5 + c.random()/2))
}

// holdRemaining is how long until the ruleset and stream hold has passed, or zero.
func (c *Client) holdRemaining() time.Duration {
	c.holds.mu.Lock()
	defer c.holds.mu.Unlock()
	return c.holds.sync.Sub(c.clk.Now())
}

func (c *Client) waitHold() bool { return sleep(c.ctx, c.clk, c.holdRemaining()) }

// waitAtLeast waits d, or until the hold has passed if that is later. It reports false if Close came first.
func (c *Client) waitAtLeast(d time.Duration) bool {
	return sleep(c.ctx, c.clk, max(d, c.holdRemaining()))
}

// holdFor lengthens the ruleset and stream hold by what a 429 asked.
func (c *Client) holdFor(resp *http.Response) {
	wait, ok := retryAfter(resp)
	if !ok {
		return
	}
	c.holds.mu.Lock()
	raise(&c.holds.sync, c.clk.Now().Add(wait))
	c.holds.mu.Unlock()
}

// markSynced records that the ruleset held is confirmed current.
func (c *Client) markSynced() {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.closed {
		return
	}
	c.lastSyncAt = c.clk.Now()
	c.lastError = LastErrorNone
	c.settleLocked(c.snap.Load() != nil)
}

// install swaps in a new ruleset and its ETag together and records the sync, in one step.
func (c *Client) install(s *snapshot) {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.closed {
		return
	}
	c.snap.Store(s)
	c.lastSyncAt = c.clk.Now()
	c.lastError = LastErrorNone
	c.settleLocked(true)
}

// refresh is one ruleset request (7.1). A failure sets lastError, settles ready if it is not settled, and then
// calls the OnError function. endsStream is true when the stream asked for the request: a failure then also marks
// the stream disconnected, in the same step as lastError.
func (c *Client) refresh(endsStream bool) bool {
	err := c.fetchRuleset(c.snap.Load())
	if err == nil {
		return true
	}
	c.smu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.smu.Unlock()
		return false
	}
	c.lastError = err.code
	if endsStream {
		c.stream = StreamDisconnected
	}
	c.settleLocked(c.snap.Load() != nil)
	c.smu.Unlock()
	c.reportFromLoop(err)
	return false
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	return req, nil
}

func (c *Client) fetchRuleset(held *snapshot) *sdkError {
	const what = "GET /sdk/v1/ruleset"
	ctx, cancel := context.WithTimeoutCause(c.ctx, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, "/sdk/v1/ruleset")
	if err != nil {
		return &sdkError{LastErrorNetwork, what + " failed: network error"}
	}
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return c.transportError(ctx, what)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		// A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is unexpected.
		if held == nil {
			return &sdkError{httpError(304), what + " answered 304 with no ruleset held"}
		}
		c.markSynced()
		return nil
	default:
		c.holdFor(resp)
		return &sdkError{httpError(resp.StatusCode), what + " answered " + strconv.Itoa(resp.StatusCode)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.transportError(ctx, what)
	}
	value, err := jsonvalue.ParseBytes(body)
	if err != nil {
		return &sdkError{LastErrorBadRuleset, what + " sent a body that is not JSON"}
	}
	if !hasRulesetShape(value) {
		return &sdkError{LastErrorBadRuleset, what + " sent no flags and segments"}
	}
	// An empty ETag counts as none, so it is never sent back as If-None-Match.
	c.install(&snapshot{rules: evaluator.NewRuleset(value), etag: resp.Header.Get("ETag")})
	return nil
}

// hasRulesetShape reports whether v is an object whose flags and segments are both arrays: the only thing that
// replaces a ruleset.
func hasRulesetShape(v jsonvalue.Value) bool {
	flags, _ := v.Get("flags")
	segments, _ := v.Get("segments")
	_, okFlags := flags.AsArray()
	_, okSegments := segments.AsArray()
	return v.IsObject() && okFlags && okSegments
}

// follow runs one stream attempt (7.2) and reports whether the stream connected and was not ended by a failed
// fetch.
func (c *Client) follow() bool {
	c.smu.Lock()
	if !c.closed {
		c.stream = StreamConnecting
	}
	c.smu.Unlock()
	connected, serr := c.runStream()
	c.smu.Lock()
	suppressed := c.closed || c.ctx.Err() != nil
	if !suppressed {
		// lastError and the stream state change in one step, so a status never shows one without the other.
		c.stream = StreamDisconnected
		if serr != nil {
			c.lastError = serr.code
		}
	}
	c.smu.Unlock()
	if serr != nil && !suppressed {
		c.reportFromLoop(serr)
	}
	return connected
}

func (c *Client) markConnected() {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.closed {
		return
	}
	c.stream = StreamConnected
	c.lastError = LastErrorNone
}

// runStream opens the stream and reads it until it ends. The error is the stream's own failure; a failed fetch the
// stream asked for was already reported by refresh and returns no error here.
func (c *Client) runStream() (connected bool, serr *sdkError) {
	const what = "GET /sdk/v1/stream"
	sctx, scancel := context.WithCancelCause(c.ctx)
	defer scancel(nil)
	// The open is bounded by requestTimeout until the headers arrive; the request context itself has no deadline.
	opening := time.AfterFunc(c.cfg.requestTimeout, func() { scancel(errTimeout) })
	req, err := c.newRequest(sctx, "/sdk/v1/stream")
	if err != nil {
		opening.Stop()
		return false, &sdkError{LastErrorNetwork, what + " failed: network error"}
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.hc.Do(req)
	opening.Stop()
	if err != nil {
		return false, c.transportError(sctx, what)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Body == nil {
		c.holdFor(resp)
		return false, &sdkError{httpError(resp.StatusCode), what + " answered " + strconv.Itoa(resp.StatusCode)}
	}
	// Once open, a stream that sends no byte for the watchdog's time is dropped. Every read restarts it.
	watchdog := time.AfterFunc(c.cfg.watchdog, func() { scancel(errTimeout) })
	defer watchdog.Stop()
	var parser sseParser
	buf := make([]byte, 8192)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(c.cfg.watchdog)
			for _, ev := range parser.feed(buf[:n]) {
				if ev.name != "changed" && ev.name != "connected" {
					continue
				}
				if ev.name == "connected" {
					connected = true
					c.markConnected()
				}
				if !c.onTag(ev.data) {
					// The fetch the stream asked for failed, and a stream that stays up would never retry it.
					return false, nil
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return connected, nil
			}
			return connected, c.transportError(sctx, what)
		}
	}
}

// onTag handles the ETag a stream event names: it is a sync if it is the tag held, otherwise a ruleset request.
func (c *Client) onTag(tag string) bool {
	held := c.snap.Load()
	if held != nil && held.etag != "" && tag == held.etag {
		c.markSynced()
		return true
	}
	return c.refresh(true)
}
