package toggleslash

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const (
	rulesetRoute = "GET /sdk/v1/ruleset"
	streamRoute  = "GET /sdk/v1/stream"
	// backoffBase is the first reconnect ceiling; it doubles with each failure up to the poll interval.
	backoffBase = time.Second
)

// snapshot is a ruleset and the ETag that names it. It is immutable, and one atomic pointer swap replaces
// both, so a reader sees the old pair or the new pair.
type snapshot struct {
	ruleset *eval.Ruleset
	etag    string
}

// runSync is the sync goroutine: the first load, then the loop of docs/sdk-contract.md 7.3 until Close.
func (c *Client) runSync(ctx context.Context) {
	defer close(c.syncDone)
	defer func() {
		if r := recover(); r != nil {
			c.settleReady(false)
			c.reportFromLoop(&PanicError{Where: "the sync loop", Value: r})
		}
	}()

	// The first attempt settles ready before it tells OnError, so a Ready called from OnError on this
	// goroutine does not wait for the load this goroutine runs.
	err := c.refresh(ctx)
	c.settleReady(c.snap.Load() != nil)
	if err != nil {
		c.reportFromLoop(err)
	}

	failures := 0
	for ctx.Err() == nil {
		if c.cfg.stream && !c.wait(ctx, c.holdLeft(&c.syncHold)) {
			return
		}
		connected := false
		if c.cfg.stream {
			connected = c.follow(ctx)
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.wait(ctx, max(c.delay(failures), c.holdLeft(&c.syncHold))) {
			return
		}
		// Sleep first: the ruleset was only just loaded or the stream only just failed. A stream that was
		// up needs no poll, since reconnecting announces the tag and catches up on its own.
		if !connected && ctx.Err() == nil {
			if err := c.refresh(ctx); err != nil {
				c.reportFromLoop(err)
			}
		}
	}
}

// delay is the wait before the next attempt: the poll interval with the stream off, otherwise an
// exponential ceiling from one second up to the poll interval, jittered to 50 to 100 percent of it.
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	exp := min(failures, 16)
	ceiling := min(c.cfg.pollInterval, backoffBase*time.Duration(1<<exp))
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.jitter()/2))
}

// refresh does one conditional ruleset request and applies its outcome to the client's state. It returns
// the failure, if there was one, for the caller to report; it returns nil for a request that Close
// abandoned.
func (c *Client) refresh(ctx context.Context) *RequestError {
	next, fail := c.fetchRuleset(ctx)
	if fail != nil {
		if ctx.Err() != nil {
			return nil
		}
		c.setLastError(fail.Code)
		return fail
	}
	c.install(next)
	return nil
}

// install records a sync. next is nil for a 304, which confirms the ruleset held.
func (c *Client) install(next *snapshot) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if next != nil {
		c.snap.Store(next)
	}
	c.lastSyncAt = c.cfg.clock.Now()
	c.lastError = ""
}

func (c *Client) setLastError(code SyncError) {
	c.stateMu.Lock()
	c.lastError = code
	c.stateMu.Unlock()
}

func (c *Client) setStream(s StreamState) {
	c.stateMu.Lock()
	c.stream = s
	c.stateMu.Unlock()
}

// fetchRuleset does GET /sdk/v1/ruleset. It returns the new snapshot, or nil and no failure for a 304 on a
// ruleset held. The whole request, the body read included, is bounded by requestTimeout.
func (c *Client) fetchRuleset(ctx context.Context) (*snapshot, *RequestError) {
	rctx, cancel := context.WithTimeoutCause(ctx, c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	req, err := c.newRequest(rctx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if err != nil {
		return nil, failureOf(rctx, rulesetRoute)
	}
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, failureOf(rctx, rulesetRoute)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		if held == nil {
			return nil, statusError(rulesetRoute, resp.StatusCode)
		}
		return nil, nil
	default:
		c.holdFrom(&c.syncHold, resp)
		return nil, statusError(rulesetRoute, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, failureOf(rctx, rulesetRoute)
	}
	parsed, err := jsonv.Parse(body)
	if err != nil || !hasRulesetShape(parsed) {
		return nil, badRulesetError(rulesetRoute)
	}
	return &snapshot{ruleset: eval.Load(parsed), etag: resp.Header.Get("ETag")}, nil
}

// hasRulesetShape: a JSON object whose "flags" and "segments" are both arrays.
func hasRulesetShape(v Value) bool {
	flags, okFlags := v.Get("flags")
	segments, okSegments := v.Get("segments")
	if !okFlags || !okSegments {
		return false
	}
	_, flagsArray := flags.AsArray()
	_, segmentsArray := segments.AsArray()
	return flagsArray && segmentsArray
}

// follow opens GET /sdk/v1/stream and reads it until it ends or fails. It reports whether the stream
// connected and stayed connected, so the loop can tell a stream that was up from one that never was.
func (c *Client) follow(ctx context.Context) (connected bool) {
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)
	sctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	req, err := c.newRequest(sctx, http.MethodGet, "/sdk/v1/stream", nil)
	if err != nil {
		c.streamFailed(ctx, failureOf(sctx, streamRoute))
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	opening := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errRequestTimeout) })
	resp, err := c.httpc.Do(req)
	opening.Stop()
	if err != nil {
		c.streamFailed(ctx, failureOf(sctx, streamRoute))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		c.holdFrom(&c.syncHold, resp)
		c.streamFailed(ctx, statusError(streamRoute, resp.StatusCode))
		return false
	}

	var parser sseParser
	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errWatchdog) })
	defer watchdog.Stop()
	buf := make([]byte, 8192)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			// Every read, a keep-alive comment included, restarts the watchdog. It is stopped while the events
			// are handled, since a fetch of the ruleset has its own bound.
			watchdog.Stop()
			for _, event := range parser.Feed(buf[:n]) {
				if event.Name != "connected" && event.Name != "changed" {
					continue
				}
				if event.Name == "connected" {
					connected = true
					c.markConnected()
				}
				if !c.onTag(ctx, event.Data) {
					// The fetch the stream asked for failed, and a stream that stays up would never retry it. End it
					// and count it as not connected, so the backoff and the poll take over.
					return false
				}
			}
			watchdog.Reset(c.cfg.watchdog)
		}
		if readErr != nil {
			if readErr != io.EOF {
				c.streamFailed(ctx, failureOf(sctx, streamRoute))
			}
			return connected
		}
	}
}

func (c *Client) markConnected() {
	c.stateMu.Lock()
	c.stream = StreamConnected
	c.lastError = ""
	c.stateMu.Unlock()
}

// streamFailed records and reports a stream failure, unless Close caused it.
func (c *Client) streamFailed(ctx context.Context, fail *RequestError) {
	if ctx.Err() != nil {
		return
	}
	c.setLastError(fail.Code)
	c.reportFromLoop(fail)
}

// onTag handles the ETag a connected or changed event names: it is a sync if it is the one held, and
// otherwise the ruleset is fetched. It reports false when that fetch failed.
func (c *Client) onTag(ctx context.Context, tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag != "" && held.etag == tag {
		c.install(nil)
		return true
	}
	if fail := c.refresh(ctx); fail != nil {
		c.reportFromLoop(fail)
		return false
	}
	return true
}
