package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// errTimeout is the cause a request context is cancelled with when its time ran out.
var errTimeout = errors.New("timeout")

// attemptError is why a ruleset or stream attempt failed, as the code Status shows.
type attemptError struct {
	code SyncErrorCode
	text string
}

func httpFailure(op string, status int) *attemptError {
	return &attemptError{code: SyncErrorCode("http_" + strconv.Itoa(status)), text: op + " answered " + strconv.Itoa(status)}
}

// failureOf classifies a transport or body error. cause is the context's cause when its time ran out.
func failureOf(op string, ctx context.Context, err error) *attemptError {
	if errors.Is(context.Cause(ctx), errTimeout) || errors.Is(err, errTimeout) {
		return &attemptError{code: SyncTimeout, text: op + " timed out"}
	}
	return &attemptError{code: SyncNetwork, text: op + " failed"}
}

// retryAfter reads a 429's Retry-After: whole decimal digits only (spaces around them are ignored), capped at
// maxRetryAfter. ok is false for any other status or any other header, which leaves the usual backoff in charge.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	text := strings.Trim(resp.Header.Get("Retry-After"), " \t")
	if text == "" {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	if len(text) > 6 {
		return maxRetryAfter, true
	}
	seconds, _ := strconv.Atoi(text)
	return min(time.Duration(seconds)*time.Second, maxRetryAfter), true
}

// hold records that nothing may reach the ruleset or stream routes before now+wait; a shorter ask never
// shortens a longer hold.
func (c *Client) hold(resp *http.Response) {
	wait, ok := retryAfter(resp)
	if !ok {
		return
	}
	until := c.cfg.now().Add(wait)
	c.statusMu.Lock()
	if until.After(c.holdUntil) {
		c.holdUntil = until
	}
	c.statusMu.Unlock()
}

// sleep waits for d, or until a held Retry-After has passed if that is later. It returns false when the
// client was closed meanwhile.
func (c *Client) sleep(d time.Duration) bool {
	c.statusMu.Lock()
	until := c.holdUntil
	c.statusMu.Unlock()
	d = max(d, until.Sub(c.cfg.now()))
	if d <= 0 {
		return c.syncCtx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.syncCtx.Done():
		return false
	}
}

// noteFailure records an attempt that failed and tells OnError, unless the client is closing: a failure
// that Close caused is never reported.
func (c *Client) noteFailure(op string, f *attemptError) {
	if c.syncCtx.Err() != nil {
		return
	}
	c.statusMu.Lock()
	c.lastError = f.code
	c.statusMu.Unlock()
	c.report(&Error{Op: op, Code: string(f.code), Message: f.text}, c.syncWorker)
}

// synced records that the held ruleset is confirmed current as of now.
func (c *Client) synced() {
	c.statusMu.Lock()
	c.lastSyncAt = c.cfg.now()
	c.lastError = ""
	c.statusMu.Unlock()
}

func (c *Client) setStream(state StreamState) {
	c.statusMu.Lock()
	if !c.closing.Load() {
		c.stream = state
	}
	c.statusMu.Unlock()
}

// hasRulesetShape: a JSON object with an array "flags" and an array "segments". Anything else is not a
// ruleset and the one held stays.
func hasRulesetShape(v jsonvalue.Value) bool {
	_, flags := v.Field("flags").Items()
	_, segments := v.Field("segments").Items()
	return v.IsObject() && flags && segments
}

// delay is the wait before the next attempt: exponential from a second up to the poll interval, jittered;
// with no stream, the poll interval.
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	ceiling := backoffBase << min(failures, 16)
	ceiling = min(c.cfg.pollInterval, ceiling)
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.jitter()/2))
}

// runSync is the sync goroutine: the first load, then the stream and the polling until Close.
func (c *Client) runSync() {
	defer close(c.syncWorker.done)
	c.refresh()
	c.settle(c.snap.Load() != nil)
	failures := 0
	for c.syncCtx.Err() == nil {
		// A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
		if c.cfg.stream && !c.sleep(0) {
			return
		}
		connected := c.cfg.stream && c.follow()
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.sleep(c.delay(failures)) {
			return
		}
		// Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was
		// up needs no poll, since reconnecting announces the tag and catches up on its own.
		if !connected {
			c.refresh()
		}
	}
}

// refresh is one conditional fetch of the ruleset. It is true when the held ruleset is now current.
func (c *Client) refresh() bool {
	ctx, cancel := context.WithTimeoutCause(c.syncCtx, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	failure := c.fetchRuleset(ctx)
	if failure != nil {
		c.noteFailure("sync", failure)
		return false
	}
	c.synced()
	return true
}

func (c *Client) fetchRuleset(ctx context.Context) *attemptError {
	const op = "GET /sdk/v1/ruleset"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.url+"/sdk/v1/ruleset", nil)
	if err != nil {
		return &attemptError{code: SyncNetwork, text: op + " failed"}
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	if held := c.snap.Load(); held != nil && held.etag != nil {
		req.Header.Set("If-None-Match", *held.etag)
	}
	resp, err := c.cfg.httpClient.Do(req)
	if err != nil {
		return failureOf(op, ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil
	case resp.StatusCode != http.StatusOK:
		c.hold(resp)
		return httpFailure(op, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return failureOf(op, ctx, err)
	}
	parsed, err := jsonvalue.Parse(body)
	if err != nil || !hasRulesetShape(parsed) {
		return &attemptError{code: SyncBadRuleset, text: op + " sent no flags and segments"}
	}
	next := &snapshot{ruleset: eval.Compile(parsed)}
	if etag := resp.Header.Get("ETag"); etag != "" {
		next.etag = &etag
	}
	c.statusMu.Lock()
	c.snap.Store(next)
	c.statusMu.Unlock()
	return nil
}

// follow reads GET /sdk/v1/stream until it ends or fails. It is true when the stream connected, so the caller
// can tell a stream that was up from one that never was.
func (c *Client) follow() (connected bool) {
	const op = "GET /sdk/v1/stream"
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)

	ctx, cancel := context.WithCancelCause(c.syncCtx)
	defer cancel(nil)
	open := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.url+"/sdk/v1/stream", nil)
	if err != nil {
		open.Stop()
		c.noteFailure("stream", &attemptError{code: SyncNetwork, text: op + " failed"})
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.cfg.httpClient.Do(req)
	open.Stop()
	if err != nil {
		c.noteFailure("stream", failureOf(op, ctx, err))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Body == nil {
		c.hold(resp)
		c.noteFailure("stream", httpFailure(op, resp.StatusCode))
		return false
	}

	idle := time.AfterFunc(c.cfg.idle, func() { cancel(errTimeout) })
	defer idle.Stop()
	var parser sseParser
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			idle.Stop()
			for _, event := range parser.feed(buf[:n]) {
				if event.name != "changed" && event.name != "connected" {
					continue
				}
				if event.name == "connected" {
					connected = true
					c.setStream(StreamConnected)
					c.statusMu.Lock()
					c.lastError = ""
					c.statusMu.Unlock()
				}
				if !c.onTag(event.data) {
					// The fetch the stream asked for failed, and a stream that stays up would never retry it.
					// End it and count it as not connected, so the backoff and the poll take over.
					return false
				}
			}
			idle.Reset(c.cfg.idle)
		}
		if err != nil {
			if err != io.EOF {
				c.noteFailure("stream", failureOf(op, ctx, err))
			}
			return connected
		}
	}
}

// onTag handles a tag the stream names: fetch only when it is not the one held. It is false when the fetch
// failed.
func (c *Client) onTag(tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag != nil && *held.etag == tag {
		c.synced()
		return true
	}
	return c.refresh()
}
