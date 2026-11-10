package toggleslash

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// failure is why one request failed. aborted means close caused it, which nothing reports.
type failure struct {
	code          string
	aborted       bool
	retryAfter    time.Duration
	hasRetryAfter bool
}

// failureError is the error onError receives. Its text is fixed words: no URL, key, message or context.
type failureError struct {
	op   string
	code string
}

func (e *failureError) Error() string {
	return "toggleslash: " + e.op + " failed: " + e.code
}

func realSleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// realJitter is a multiplier drawn uniformly from [0.5, 1).
func realJitter() float64 { return 0.5 + rand.Float64()/2 }

// classify names why a request failed, from the context's cause and the error type, never from the
// error's text (a *url.Error carries the URL).
func (c *Client) classify(ctx context.Context, err error) *failure {
	if c.root.Err() != nil {
		return &failure{aborted: true}
	}
	return &failure{code: errorCode(ctx, err)}
}

func errorCode(ctx context.Context, err error) string {
	if errors.Is(context.Cause(ctx), errTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return LastErrorTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return LastErrorTimeout
	}
	return LastErrorNetwork
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	return req, nil
}

// retryAfter reads a Retry-After header: whole decimal seconds, spaces ignored, capped.
func (c *Client) retryAfter(h http.Header) (time.Duration, bool) {
	raw := strings.Trim(h.Get("Retry-After"), " ")
	if raw == "" {
		return 0, false
	}
	capSeconds := int64(c.cfg.retryAfterCap / time.Second)
	var seconds int64
	for i := 0; i < len(raw); i++ {
		d := raw[i]
		if d < '0' || d > '9' {
			return 0, false
		}
		if seconds <= capSeconds {
			seconds = seconds*10 + int64(d-'0')
		}
	}
	if seconds > capSeconds {
		seconds = capSeconds
	}
	return time.Duration(seconds) * time.Second, true
}

// statusFailure builds the failure for an unexpected status, reading Retry-After on a 429 only.
func (c *Client) statusFailure(resp *http.Response) *failure {
	f := &failure{code: "http_" + strconv.Itoa(resp.StatusCode)}
	if resp.StatusCode == http.StatusTooManyRequests {
		f.retryAfter, f.hasRetryAfter = c.retryAfter(resp.Header)
	}
	return f
}

// fetchRuleset does one ruleset request and applies its answer. nil is a sync.
func (c *Client) fetchRuleset() *failure {
	ctx, cancel := context.WithTimeoutCause(c.root, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := c.request(ctx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if err != nil {
		return &failure{code: LastErrorNetwork}
	}
	req.Header.Set("Accept", "application/json")
	if held := c.view.Load(); held.ruleset != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.classify(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotModified:
		if c.view.Load().ruleset == nil {
			return &failure{code: "http_304"}
		}
		c.markSynced()
		return nil
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return c.classify(ctx, err)
		}
		root, err := jsonv.Parse(body)
		if err != nil || !isRulesetShape(root) {
			return &failure{code: LastErrorBadRuleset}
		}
		rs := evaluator.NewRuleset(root)
		etag := resp.Header.Get("ETag")
		c.update(func(v *view) {
			v.ruleset = rs
			v.etag = etag
			v.lastSyncAt = c.cfg.now()
			v.lastError = ""
		})
		return nil
	default:
		return c.statusFailure(resp)
	}
}

// isRulesetShape is contract 5.5: an object whose flags and segments are both arrays.
func isRulesetShape(root Value) bool {
	if !root.IsObject() {
		return false
	}
	flags, ok1 := root.Get("flags")
	segments, ok2 := root.Get("segments")
	return ok1 && ok2 && flags.IsArray() && segments.IsArray()
}

// markSynced records that the ruleset held matches the server.
func (c *Client) markSynced() {
	c.update(func(v *view) {
		v.lastSyncAt = c.cfg.now()
		v.lastError = ""
	})
}

// closing reports whether Close has begun.
func (c *Client) closing() bool { return c.root.Err() != nil }

// note records a failure in Status and returns the error for onError, or nil when close caused it.
func (c *Client) note(op string, f *failure) error {
	if f.aborted || c.closing() {
		return nil
	}
	if f.hasRetryAfter {
		c.syncHold.extend(c.cfg.now().Add(f.retryAfter))
	}
	if !c.update(func(v *view) { v.lastError = f.code }) {
		return nil
	}
	return &failureError{op: op, code: f.code}
}

func (c *Client) setStream(s StreamState) {
	if !c.cfg.stream {
		return
	}
	c.update(func(v *view) { v.stream = s })
}

func (c *Client) runSync() {
	defer close(c.syncDone)
	defer func() {
		if r := recover(); r != nil {
			c.settle(c.view.Load().ruleset != nil)
			c.report(panicError("sync", r))
		}
	}()
	c.syncGid.Store(goroutineID())
	c.firstLoad()
	failures := 0
	for c.root.Err() == nil {
		if c.cfg.stream {
			if d := c.syncHold.remaining(c.cfg.now()); d > 0 && !c.cfg.sleep(c.root, d) {
				return
			}
		}
		connected := false
		if c.cfg.stream {
			connected = c.followStream()
		}
		if c.root.Err() != nil {
			return
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		wait := c.delay(failures)
		if d := c.syncHold.remaining(c.cfg.now()); d > wait {
			wait = d
		}
		if !c.cfg.sleep(c.root, wait) {
			return
		}
		if !connected {
			c.poll()
		}
	}
}

// firstLoad is the one ruleset request at create. Its end settles Ready. The failure is recorded before
// that, and onError is told before Ready answers, so a caller that sees Ready return sees the error too.
func (c *Client) firstLoad() {
	f := c.fetchRuleset()
	held := c.view.Load().ruleset != nil
	c.firstHeld.Store(held)
	var err error
	if f != nil {
		err = c.note("ruleset request", f)
	}
	c.report(err)
	c.settle(held)
}

func (c *Client) poll() {
	if f := c.fetchRuleset(); f != nil {
		c.report(c.note("ruleset request", f))
	}
}

// delay is contract 7.3's delay(failures).
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	exp := failures
	if exp > 16 {
		exp = 16
	}
	ceiling := time.Second << uint(exp)
	if ceiling > c.cfg.pollInterval {
		ceiling = c.cfg.pollInterval
	}
	return time.Duration(float64(ceiling) * c.cfg.jitter())
}
