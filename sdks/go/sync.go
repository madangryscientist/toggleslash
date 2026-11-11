package toggleslash

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// maxRetryAfter is the longest a Retry-After can make the SDK wait (contract 7.4).
const maxRetryAfter = 300 * time.Second

// errTimeout is the cause of a request that ran out of time.
var errTimeout = errors.New("timeout")

// syncFailure is a failed ruleset or stream attempt. Its text is fixed: the code only.
type syncFailure struct {
	code   string
	source ErrorSource
}

func (f *syncFailure) toError() *Error {
	what := "ruleset request failed: "
	if f.source == SourceStream {
		what = "stream failed: "
	}
	return &Error{Source: f.source, Code: f.code, Message: what + f.code}
}

func httpCode(status int) string { return "http_" + strconv.Itoa(status) }

// classify turns the error of a request into a failure code. It returns nil when the request was
// abandoned by Close, which reports nothing.
func (c *Client) classify(rctx context.Context, source ErrorSource) *syncFailure {
	if c.ctx.Err() != nil {
		return nil
	}
	if errors.Is(context.Cause(rctx), errTimeout) {
		return &syncFailure{code: SyncTimeout, source: source}
	}
	return &syncFailure{code: SyncNetwork, source: source}
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	return req, nil
}

// parseRetryAfter reads the header of a 429: whole decimal seconds, spaces ignored, capped.
func parseRetryAfter(h http.Header) (time.Duration, bool) {
	v := strings.Trim(h.Get("Retry-After"), " ")
	if v == "" {
		return 0, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	v = strings.TrimLeft(v, "0")
	if len(v) > 6 {
		return maxRetryAfter, true
	}
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	d := time.Duration(n) * time.Second
	return min(d, maxRetryAfter), true
}

// holdRoutes holds the ruleset and stream routes until d from now; a later hold only lengthens it.
func (c *Client) holdRoutes(d time.Duration) {
	until := c.cfg.now().Add(d)
	c.holdMu.Lock()
	if until.After(c.holdSync) {
		c.holdSync = until
	}
	c.holdMu.Unlock()
}

func (c *Client) holdUsage(d time.Duration) {
	until := c.cfg.now().Add(d)
	c.holdMu.Lock()
	if until.After(c.holdUse) {
		c.holdUse = until
	}
	c.holdMu.Unlock()
}

func (c *Client) routesHeldFor() time.Duration {
	c.holdMu.Lock()
	defer c.holdMu.Unlock()
	return c.holdSync.Sub(c.cfg.now())
}

func (c *Client) usageHeld() bool {
	c.holdMu.Lock()
	defer c.holdMu.Unlock()
	return c.holdUse.After(c.cfg.now())
}

// sleep waits d, and reports false if Close came first.
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

// markSynced records that the held ruleset is current.
func (c *Client) markSynced() {
	c.stateMu.Lock()
	c.lastSync = c.cfg.now()
	c.synced = true
	c.lastErr = ""
	c.stateMu.Unlock()
}

func (c *Client) markError(code string) {
	c.stateMu.Lock()
	c.lastErr = code
	c.stateMu.Unlock()
}

func (c *Client) swapRuleset(rules *evaluator.Ruleset, etag string) {
	c.stateMu.Lock()
	c.snap.Store(&snapshot{rules: rules, etag: etag})
	c.lastSync = c.cfg.now()
	c.synced = true
	c.lastErr = ""
	c.stateMu.Unlock()
}

// loadRuleset does the ruleset request of 7.1 and applies its outcome. A failure is recorded in
// the status and returned, for the caller to hand to onError once it holds no state.
func (c *Client) loadRuleset() *syncFailure {
	f := c.fetchRuleset()
	if f != nil {
		c.markError(f.code)
	}
	return f
}

func (c *Client) fetchRuleset() *syncFailure {
	if c.ctx.Err() != nil {
		return nil
	}
	rctx, cancel := context.WithTimeoutCause(c.ctx, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := c.newRequest(rctx, "/sdk/v1/ruleset")
	if err != nil {
		return &syncFailure{code: SyncNetwork, source: SourceRuleset}
	}
	req.Header.Set("Accept", "application/json")
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return c.classify(rctx, SourceRuleset)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return c.classify(rctx, SourceRuleset)
		}
		return c.applyBody(body, resp.Header.Get("ETag"))
	case http.StatusNotModified:
		if held == nil {
			return &syncFailure{code: httpCode(304), source: SourceRuleset}
		}
		c.markSynced()
		return nil
	default:
		if resp.StatusCode == http.StatusTooManyRequests {
			if d, ok := parseRetryAfter(resp.Header); ok {
				c.holdRoutes(d)
			}
		}
		return &syncFailure{code: httpCode(resp.StatusCode), source: SourceRuleset}
	}
}

func (c *Client) applyBody(body []byte, etag string) (f *syncFailure) {
	bad := &syncFailure{code: SyncBadRuleset, source: SourceRuleset}
	doc, err := jsonv.ParseBody(body)
	if err != nil {
		return bad
	}
	if doc.Kind() != jsonv.Object || doc.Field("flags").Kind() != jsonv.Array ||
		doc.Field("segments").Kind() != jsonv.Array {
		return bad
	}
	defer func() {
		if recover() != nil {
			f = bad
		}
	}()
	c.swapRuleset(evaluator.Load(doc), etag)
	return nil
}

// delay is the wait between two attempts of the loop (7.3).
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	exp := min(failures, 16)
	ceiling := min(c.cfg.pollInterval, time.Second<<exp)
	return time.Duration(float64(ceiling) * (0.5 + 0.5*c.jitter()))
}

func (c *Client) jitter() float64 {
	if c.cfg.jitter != nil {
		return c.cfg.jitter()
	}
	return rand.Float64()
}

// syncLoop is the SDK goroutine that keeps the ruleset fresh.
func (c *Client) syncLoop() {
	f := c.loadRuleset()
	c.settleReady(c.snap.Load() != nil)
	if f != nil {
		c.reportFromLoop(f.toError())
	}
	failures := 0
	for c.ctx.Err() == nil {
		if c.cfg.stream && !c.sleep(c.routesHeldFor()) {
			return
		}
		connected := false
		if c.cfg.stream {
			connected = c.followStream()
		}
		if c.ctx.Err() != nil {
			return
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.sleep(max(c.delay(failures), c.routesHeldFor())) {
			return
		}
		if !connected {
			if f := c.loadRuleset(); f != nil && c.ctx.Err() == nil {
				c.reportFromLoop(f.toError())
			}
		}
	}
}
