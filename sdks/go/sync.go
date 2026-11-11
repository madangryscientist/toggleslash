package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// Causes a request context is cancelled with, so a failure is told apart by its cause and never by the text
// of an error (which would carry the URL).
var (
	errRequestTimeout = errors.New("request timeout")
	errWatchdog       = errors.New("stream watchdog")
	// errClosed stands for a failure Close caused: nothing is recorded or reported for it.
	errClosed = errors.New("closed")
)

type httpResult struct {
	status     int
	retryAfter string
	etag       string
	body       []byte
}

// request sends one request bounded by requestTimeout, body included, and returns the status, the few
// headers the SDK reads and, when wantBody says so, the whole body. A failure is a *SyncError with fixed
// text, or errClosed when parent was cancelled (which only Close does).
func (c *Client) request(parent context.Context, route, method, path string, body []byte, wantBody func(int) bool) (httpResult, error) {
	ctx, cancel := context.WithTimeoutCause(parent, c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return httpResult{}, &SyncError{Route: route, Detail: LastErrorNetwork}
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if route == "ruleset" {
		if s := c.snap.Load(); s.rs != nil && s.etag != "" {
			req.Header.Set("If-None-Match", s.etag)
		}
	}
	fail := func() error { return c.classify(ctx, route, parent.Err() != nil) }
	resp, err := c.hc.Do(req)
	if err != nil {
		return httpResult{}, fail()
	}
	defer func() { _ = resp.Body.Close() }()
	res := httpResult{status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After"), etag: resp.Header.Get("ETag")}
	if wantBody != nil && wantBody(resp.StatusCode) {
		res.body, err = io.ReadAll(resp.Body)
		if err != nil {
			return httpResult{}, fail()
		}
	}
	return res, nil
}

// classify names why a request failed from its contexts only.
func (c *Client) classify(ctx context.Context, route string, closed bool) error {
	if closed {
		return errClosed
	}
	if errors.Is(context.Cause(ctx), errRequestTimeout) {
		return &SyncError{Route: route, Detail: LastErrorTimeout}
	}
	return &SyncError{Route: route, Detail: LastErrorNetwork}
}

func httpDetail(status int) string { return "http_" + strconv.Itoa(status) }

// parseRetryAfter reads a Retry-After value: optional spaces around one or more ASCII digits, nothing else.
// The wait is capped.
func (c *Client) parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.Trim(v, " ")
	if v == "" {
		return 0, false
	}
	for i := range len(v) {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	limit := c.cfg.retryAfterCap
	secs, err := strconv.ParseUint(v, 10, 64)
	if err != nil || secs > uint64(limit/time.Second)+1 {
		return limit, true
	}
	return min(time.Duration(secs)*time.Second, limit), true
}

// hold extends a Retry-After deadline; it never shortens it.
func (c *Client) hold(h *atomic.Int64, d time.Duration) {
	deadline := c.clock().Add(d).UnixNano()
	for {
		cur := h.Load()
		if deadline <= cur || h.CompareAndSwap(cur, deadline) {
			return
		}
	}
}

func (c *Client) holdLeft(h *atomic.Int64) time.Duration {
	return time.Duration(h.Load() - c.clock().UnixNano())
}

// sleep waits d, or until Close; it reports false when Close came first.
func (c *Client) sleep(d time.Duration) bool {
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-c.ctx.Done():
		}
	}
	return c.ctx.Err() == nil
}

// waitHold waits out the ruleset and stream Retry-After hold; it reports false when Close came first.
func (c *Client) waitHold() bool {
	for {
		left := c.holdLeft(&c.holdSync)
		if left <= 0 {
			return c.ctx.Err() == nil
		}
		if !c.sleep(left) {
			return false
		}
	}
}

// loadRuleset does the ruleset request of contract 7.1 and applies a sync. It returns nil on a sync, errClosed
// when Close interrupted it and a *SyncError otherwise; the caller records and reports that.
func (c *Client) loadRuleset() error {
	held := c.snap.Load().rs != nil
	res, err := c.request(c.ctx, "ruleset", http.MethodGet, "/sdk/v1/ruleset", nil, func(s int) bool { return s == 200 })
	if err != nil {
		return err
	}
	switch res.status {
	case 200:
		rs, ok := parseRuleset(res.body)
		if !ok {
			return &SyncError{Route: "ruleset", Detail: LastErrorBadRuleset}
		}
		c.update(func(s *snapshot) {
			s.rs, s.etag, s.lastSyncAt, s.lastError = rs, res.etag, c.clock(), ""
		})
		return nil
	case 304:
		if held {
			c.update(func(s *snapshot) { s.lastSyncAt, s.lastError = c.clock(), "" })
			return nil
		}
	case 429:
		if d, ok := c.parseRetryAfter(res.retryAfter); ok {
			c.hold(&c.holdSync, d)
		}
	}
	return &SyncError{Route: "ruleset", Detail: httpDetail(res.status)}
}

// parseRuleset reads a 200 body: JSON, an object whose flags and segments are arrays.
func parseRuleset(body []byte) (rs *evaluator.Ruleset, ok bool) {
	defer func() {
		if recover() != nil {
			rs, ok = nil, false
		}
	}()
	v, err := jsonvalue.Parse(body)
	if err != nil || v.Kind() != jsonvalue.KindObject {
		return nil, false
	}
	flags, hasFlags := v.Get("flags")
	segments, hasSegments := v.Get("segments")
	if !hasFlags || !hasSegments || flags.Kind() != jsonvalue.KindArray || segments.Kind() != jsonvalue.KindArray {
		return nil, false
	}
	return evaluator.Load(v), true
}

// record sets lastError for a failed ruleset or stream attempt and returns the error to report. A failure
// Close caused, and no failure at all, give nil and record nothing.
func (c *Client) record(err error) *SyncError {
	var se *SyncError
	if !errors.As(err, &se) || c.closing.Load() {
		return nil
	}
	c.update(func(s *snapshot) { s.lastError = se.Detail })
	return se
}

// fail records a failed attempt and reports it to onError.
func (c *Client) fail(err error) {
	if se := c.record(err); se != nil {
		c.notify(se, &c.syncCB)
	}
}

// delay is how long the loop waits after an attempt (contract 7.3).
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	ceiling := min(c.cfg.pollInterval, time.Second<<min(failures, maxBackoffExponent))
	r := rand.Float64
	if c.cfg.random != nil {
		r = c.cfg.random
	}
	return time.Duration(float64(ceiling) * (0.5 + 0.5*r()))
}

// syncLoop is the sync goroutine: the first load, then the loop of contract 7.3 until Close.
func (c *Client) syncLoop() {
	defer close(c.syncDone)
	defer c.settle()
	defer func() {
		if v := recover(); v != nil {
			c.notify(&PanicError{Where: "sync loop", Value: v}, &c.syncCB)
		}
	}()
	// The first load settles Ready after it records its error and before it calls onError, so a callback
	// that calls Ready never waits for itself.
	se := c.record(c.loadRuleset())
	c.settle()
	if se != nil {
		c.notify(se, &c.syncCB)
	}
	failures := 0
	for c.ctx.Err() == nil {
		if c.cfg.stream && !c.waitHold() {
			return
		}
		connected := c.cfg.stream && c.followStream()
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.sleep(c.delay(failures)) || !c.waitHold() {
			return
		}
		if !connected {
			c.fail(c.loadRuleset())
		}
	}
}
