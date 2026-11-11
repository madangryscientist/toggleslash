package toggleslash

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// The causes a request's context is cancelled with. A cancel by Close has no cause of ours.
var (
	errRequestTimeout = errors.New("request timeout")
	errWatchdog       = errors.New("stream watchdog")
)

// maxRetryAfter is the longest a Retry-After can hold the SDK back.
const maxRetryAfter = 300 * time.Second

func randomFloat() float64 { return rand.Float64() }

// hold is a Retry-After deadline: a time, not a sleep.
type hold struct {
	mu    sync.Mutex
	until time.Time
}

// extend lengthens the hold to t. It never shortens it.
func (h *hold) extend(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t.After(h.until) {
		h.until = t
	}
}

func (h *hold) remaining(now time.Time) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.until.Sub(now)
}

// parseRetryAfter reads a Retry-After as whole decimal seconds: one or more digits and nothing else, with
// surrounding spaces ignored. The result is capped at 300 s.
func parseRetryAfter(v string) (time.Duration, bool) {
	start, end := 0, len(v)
	for start < end && (v[start] == ' ' || v[start] == '\t') {
		start++
	}
	for end > start && (v[end-1] == ' ' || v[end-1] == '\t') {
		end--
	}
	if start == end {
		return 0, false
	}
	var secs int64
	for i := start; i < end; i++ {
		d := v[i]
		if d < '0' || d > '9' {
			return 0, false
		}
		if secs < 1<<40 {
			secs = secs*10 + int64(d-'0')
		}
	}
	d := time.Duration(min(secs, int64(maxRetryAfter/time.Second))) * time.Second
	return d, true
}

func (c *Client) noteRetryAfter(h *hold, resp *http.Response) bool {
	d, ok := parseRetryAfter(resp.Header.Get("Retry-After"))
	if ok {
		h.extend(c.cfg.now().Add(d))
	}
	return ok
}

// report calls onError. A panic in it is ignored.
func (c *Client) report(err error) {
	fn := c.cfg.onError
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(err)
}

// fail records a failed ruleset or stream attempt. After Close it does nothing: Close caused it.
func (c *Client) fail(op, code string) {
	c.statusMu.Lock()
	if c.closed {
		c.statusMu.Unlock()
		return
	}
	c.lastError = code
	c.statusMu.Unlock()
	c.report(&Error{Op: op, Code: code})
}

// markSynced records that the ruleset held matches the server.
func (c *Client) markSynced() {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.closed {
		return
	}
	c.lastSyncAt = c.cfg.now()
	c.lastError = ""
}

// install swaps in a new ruleset and its ETag as one pair, and records the sync.
func (c *Client) install(rs *eval.Ruleset, etag string) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.closed {
		return
	}
	c.snap.Store(&snapshot{rs: rs, etag: etag})
	c.lastSyncAt = c.cfg.now()
	c.lastError = ""
}

func (c *Client) setStream(s StreamState) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if c.closed || c.stream == StreamOff {
		return
	}
	c.stream = s
}

func (c *Client) clearLastError() {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	if !c.closed {
		c.lastError = ""
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	return req, nil
}

// transportCode says why a request failed: "" when Close caused it, "timeout" when the request timeout or the
// stream watchdog did, else "network". The error's own text is never used: it carries the URL.
func (c *Client) transportCode(rctx context.Context) string {
	if c.ctx.Err() != nil {
		return ""
	}
	if cause := context.Cause(rctx); errors.Is(cause, errRequestTimeout) || errors.Is(cause, errWatchdog) {
		return LastErrorTimeout
	}
	return LastErrorNetwork
}

type fetchResult int

const (
	fetchOK fetchResult = iota
	fetchFailed
	fetchAborted
)

// fetchRuleset is the ruleset request of contract 7.1. It records its own outcome. The whole request, body
// included, is bounded by the request timeout.
func (c *Client) fetchRuleset() fetchResult {
	if c.ctx.Err() != nil {
		return fetchAborted
	}
	rctx, cancel := context.WithTimeoutCause(c.ctx, c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	req, err := c.newRequest(rctx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if err != nil {
		c.fail("ruleset", LastErrorNetwork)
		return fetchFailed
	}
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.transportFailure("ruleset", rctx)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return c.transportFailure("ruleset", rctx)
		}
		rs, ok := buildRuleset(body)
		if !ok {
			c.fail("ruleset", LastErrorBadRuleset)
			return fetchFailed
		}
		if c.ctx.Err() != nil {
			return fetchAborted
		}
		c.install(rs, resp.Header.Get("ETag"))
		return fetchOK
	case http.StatusNotModified:
		if held == nil {
			c.fail("ruleset", "http_304")
			return fetchFailed
		}
		c.markSynced()
		return fetchOK
	case http.StatusTooManyRequests:
		c.noteRetryAfter(&c.syncHold, resp)
	}
	c.fail("ruleset", "http_"+strconv.Itoa(resp.StatusCode))
	return fetchFailed
}

func (c *Client) transportFailure(op string, rctx context.Context) fetchResult {
	code := c.transportCode(rctx)
	if code == "" {
		return fetchAborted
	}
	c.fail(op, code)
	return fetchFailed
}

// buildRuleset reads a 200 body: invalid UTF-8 repaired, a byte order mark skipped, and the shape of
// contract section 5 item 5 required (an object whose flags and segments are both arrays).
func buildRuleset(body []byte) (*eval.Ruleset, bool) {
	v, err := jsonvalue.Parse(wtf8.Repair(body))
	if err != nil || !v.IsObject() {
		return nil, false
	}
	flags, _ := v.Get("flags")
	segments, _ := v.Get("segments")
	if flags.Kind != jsonvalue.Array || segments.Kind != jsonvalue.Array {
		return nil, false
	}
	return eval.NewRuleset(v), true
}

// sleep waits d, and reports false when Close ended the wait.
func (c *Client) sleep(d time.Duration) bool {
	if d <= 0 {
		return c.ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// waitHold waits until any Retry-After hold on the ruleset and stream routes has passed.
func (c *Client) waitHold() bool {
	for {
		d := c.syncHold.remaining(c.cfg.now())
		if d <= 0 {
			return c.ctx.Err() == nil
		}
		if !c.sleep(d) {
			return false
		}
	}
}

// backoffDelay is delay(failures) of contract 7.3. r is a draw from [0, 1).
func backoffDelay(streamOn bool, failures int, poll time.Duration, r float64) time.Duration {
	if !streamOn {
		return poll
	}
	ceiling := min(poll, time.Second<<min(failures, 16))
	return time.Duration(float64(ceiling) * (0.5 + r/2))
}

// safely runs fn and reports a panic as an error, so a bug in the SDK's own goroutine cannot end the process.
func (c *Client) safely(op string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			c.report(&Error{Op: op, Code: "internal"})
		}
	}()
	fn()
}

// syncLoop is the first load and the loop of contract 7.3.
func (c *Client) syncLoop() {
	defer close(c.syncDone)
	c.markGoroutine()
	defer c.unmarkGoroutine()
	c.safely("ruleset", func() { c.fetchRuleset() })
	c.settleReady(c.snap.Load() != nil)
	streamOn := *c.cfg.stream
	failures := 0
	for c.ctx.Err() == nil {
		if streamOn && !c.waitHold() {
			return
		}
		connected := false
		if streamOn {
			c.safely("stream", func() { connected = c.followStream() })
		}
		if c.ctx.Err() != nil {
			return
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.sleep(backoffDelay(streamOn, failures, c.cfg.pollInterval, c.cfg.random())) || !c.waitHold() {
			return
		}
		if !connected {
			c.safely("ruleset", func() { c.fetchRuleset() })
		}
	}
}

// followStream opens the stream and reads it until it ends (contract 7.2). It reports whether the stream
// connected and was not ended by a failed fetch.
func (c *Client) followStream() bool {
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)
	sctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)
	req, err := c.newRequest(sctx, http.MethodGet, "/sdk/v1/stream", nil)
	if err != nil {
		c.fail("stream", LastErrorNetwork)
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	opening := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errRequestTimeout) })
	resp, err := c.http.Do(req)
	opening.Stop()
	if err != nil {
		if code := c.transportCode(sctx); code != "" {
			c.fail("stream", code)
		}
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.noteRetryAfter(&c.syncHold, resp)
		}
		c.fail("stream", "http_"+strconv.Itoa(resp.StatusCode))
		return false
	}

	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errWatchdog) })
	defer watchdog.Stop()
	var parser sseParser
	buf := make([]byte, 4096)
	connected := false
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(c.cfg.watchdog)
			for _, ev := range parser.feed(buf[:n]) {
				if ev.name != "connected" && ev.name != "changed" {
					continue
				}
				if ev.name == "connected" {
					connected = true
					c.setStream(StreamConnected)
					c.clearLastError()
				}
				// A fetch is not a read, so the watchdog does not run across it.
				watchdog.Stop()
				ok := c.applyTag(ev.data)
				watchdog.Reset(c.cfg.watchdog)
				if !ok {
					return false
				}
				if c.ctx.Err() != nil {
					return connected
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				if code := c.transportCode(sctx); code != "" {
					c.fail("stream", code)
				}
			}
			return connected
		}
	}
}

// applyTag handles the tag of a connected or changed event. It is false only when a fetch it needed failed.
func (c *Client) applyTag(tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag != "" && held.etag == tag {
		c.markSynced()
		return true
	}
	return c.fetchRuleset() != fetchFailed
}
