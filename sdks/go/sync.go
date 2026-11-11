package toggleslash

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// maxRetryAfter is the longest a Retry-After makes the client wait (contract 7.4).
const maxRetryAfter = 300 * time.Second

func newTransport(tlsConfig *tls.Config) *http.Transport {
	var tr *http.Transport
	if def, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = def.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	// A stream read must hand over the bytes as they arrive, so nothing is compressed.
	tr.DisableCompression = true
	if tlsConfig != nil {
		tr.TLSClientConfig = tlsConfig.Clone()
	}
	return tr
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	return req, nil
}

// failureCode is why a request that did not get an answer failed: timeout when its context ran out of
// time, network otherwise. It reads no error text.
func failureCode(reqCtx context.Context) SyncErrorCode {
	if errors.Is(context.Cause(reqCtx), errTimeout) {
		return SyncTimeout
	}
	return SyncNetwork
}

// parseRetryAfter reads a Retry-After that is whole decimal seconds (spaces around it are ignored) and
// caps it at five minutes. A date, a fraction, a negative number or nothing is not valid.
func parseRetryAfter(h http.Header) (time.Duration, bool) {
	v := strings.Trim(h.Get("Retry-After"), " \t")
	if v == "" {
		return 0, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n > uint64(maxRetryAfter/time.Second) {
		return maxRetryAfter, true
	}
	return time.Duration(n) * time.Second, true
}

// holdSync records a Retry-After hold on the ruleset and stream routes; a later one only lengthens it.
// Only the sync goroutine calls it.
func (c *Client) holdSync(h http.Header) {
	d, ok := parseRetryAfter(h)
	if !ok {
		return
	}
	if until := c.cfg.clock.Now().Add(d); until.After(c.syncHold) {
		c.syncHold = until
	}
}

// hasRulesetShape is contract section 5, item 5: an object whose flags and segments are both arrays.
func hasRulesetShape(v jsonvalue.Value) bool {
	flags, _ := v.Field("flags")
	segments, _ := v.Field("segments")
	_, flagsOK := flags.AsArray()
	_, segmentsOK := segments.AsArray()
	return flagsOK && segmentsOK
}

// fetchRuleset does the ruleset request of contract 7.1 and says how it ended: the empty code when the
// ruleset held is now current (a 200 stored it, or a 304 confirmed it).
func (c *Client) fetchRuleset(ctx context.Context) SyncErrorCode {
	reqCtx, cancel := context.WithTimeoutCause(ctx, c.cfg.requestTimeout, errTimeout)
	defer cancel() // after the body is read, so a body that stalls is cut off too
	req, err := c.newRequest(reqCtx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if err != nil {
		return SyncNetwork
	}
	req.Header.Set("Accept", "application/json")
	if held := c.state.Load(); held.rs != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return failureCode(reqCtx)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return failureCode(reqCtx)
		}
		value, err := jsonvalue.Parse(body)
		if err != nil || !hasRulesetShape(value) {
			return SyncBadRuleset
		}
		rs := evaluator.New(value)
		etag := resp.Header.Get("ETag")
		now := c.cfg.clock.Now()
		c.update(func(s *snapshot) {
			s.rs, s.etag, s.lastSyncAt, s.lastError = rs, etag, now, ""
		})
		return ""
	case http.StatusNotModified:
		if c.state.Load().rs == nil {
			return httpErrorCode(resp.StatusCode)
		}
		c.markSynced()
		return ""
	case http.StatusTooManyRequests:
		c.holdSync(resp.Header)
	}
	return httpErrorCode(resp.StatusCode)
}

// markSynced records that the ruleset held matches the server.
func (c *Client) markSynced() {
	now := c.cfg.clock.Now()
	c.update(func(s *snapshot) { s.lastSyncAt, s.lastError = now, "" })
}

// fail records a failed ruleset or stream attempt and tells the application. A failure that Close caused
// is neither recorded nor reported. The first load has settled Ready before onError runs.
func (c *Client) fail(ctx context.Context, code SyncErrorCode) {
	c.syncCB.Add(1) // marked before the check, so a Close that sees no callback also sees ctx cancelled
	defer c.syncCB.Add(-1)
	if ctx.Err() != nil {
		return
	}
	c.update(func(s *snapshot) { s.lastError = code })
	c.settleReady(c.state.Load().rs != nil)
	c.callOnError(&SyncError{Code: code})
}

// guarded runs one step of the sync goroutine; a panic in it is reported and the loop goes on, because a
// panic that escaped would end the process.
func (c *Client) guarded(ctx context.Context, what string, step func()) {
	defer func() {
		if r := recover(); r != nil {
			c.syncCB.Add(1)
			defer c.syncCB.Add(-1)
			if ctx.Err() == nil {
				c.callOnError(&panicError{what: what, value: r})
			}
		}
	}()
	step()
}

// syncLoop is the sync goroutine: the first load at once, then contract 7.3 until the context is done.
func (c *Client) syncLoop(ctx context.Context) {
	defer close(c.syncDone)
	c.guarded(ctx, "the first load", func() {
		if code := c.fetchRuleset(ctx); code != "" {
			c.fail(ctx, code)
		}
	})
	c.settleReady(c.state.Load().rs != nil)

	failures := 0
	for ctx.Err() == nil {
		connected := false
		if c.cfg.stream {
			c.guarded(ctx, "the stream", func() { connected = c.attemptStream(ctx) })
		}
		if connected {
			failures = 0
		} else {
			failures = min(failures+1, 1<<20)
		}
		if !c.sleep(ctx, c.delay(failures)) {
			return
		}
		if !connected {
			c.guarded(ctx, "a poll", func() {
				if code := c.fetchRuleset(ctx); code != "" {
					c.fail(ctx, code)
				}
			})
		}
	}
}

// delay is the wait after an attempt (contract 7.3).
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	ceiling := c.cfg.pollInterval
	if backoff := time.Second << min(failures, 16); backoff < ceiling {
		ceiling = backoff
	}
	return time.Duration(float64(ceiling) * c.cfg.jitter())
}

// sleep waits d, and at least until the Retry-After hold has passed. It reports false when the context
// ended first.
func (c *Client) sleep(ctx context.Context, d time.Duration) bool {
	now := c.cfg.clock.Now()
	until := now.Add(d)
	if c.syncHold.After(until) {
		until = c.syncHold
	}
	return c.sleepUntil(ctx, until.Sub(now))
}

func (c *Client) sleepUntil(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := c.cfg.clock.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C():
		return ctx.Err() == nil
	}
}

// attemptStream waits out any hold, then follows the stream once (contract 7.2). It reports whether the
// stream connected and was not ended by a failed fetch.
func (c *Client) attemptStream(ctx context.Context) bool {
	if hold := c.syncHold.Sub(c.cfg.clock.Now()); hold > 0 && !c.sleepUntil(ctx, hold) {
		return false
	}
	c.update(func(s *snapshot) { s.stream = StreamConnecting })
	connected := c.followStream(ctx)
	c.update(func(s *snapshot) { s.stream = StreamDisconnected })
	return connected
}

func (c *Client) followStream(ctx context.Context) bool {
	streamCtx, cancelStream := context.WithCancelCause(ctx)
	defer cancelStream(nil)
	// One timer is the open bound (requestTimeout) until the headers arrive, then the watchdog.
	dog := time.AfterFunc(c.cfg.requestTimeout, func() { cancelStream(errTimeout) })
	defer dog.Stop()

	req, err := c.newRequest(streamCtx, http.MethodGet, "/sdk/v1/stream", nil)
	if err != nil {
		c.fail(ctx, SyncNetwork)
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		c.fail(ctx, failureCode(streamCtx))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	dog.Reset(c.cfg.idle)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.holdSync(resp.Header)
		}
		c.fail(ctx, httpErrorCode(resp.StatusCode))
		return false
	}

	connected, brokenByFetch := false, false
	handle := func(ev sseEvent) bool {
		switch ev.name {
		case "connected":
			connected = true
			c.update(func(s *snapshot) { s.stream, s.lastError = StreamConnected, "" })
		case "changed":
		default:
			return true
		}
		held := c.state.Load()
		if held.rs != nil && held.etag != "" && ev.data == held.etag {
			c.markSynced()
			return true
		}
		dog.Stop() // a fetch is not a quiet stream
		code := c.fetchRuleset(ctx)
		dog.Reset(c.cfg.idle)
		if code != "" {
			c.fail(ctx, code)
			brokenByFetch = true
			return false
		}
		return true
	}

	var parser sseParser
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			dog.Reset(c.cfg.idle)
			if !parser.feed(buf[:n], handle) {
				break
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.fail(ctx, failureCode(streamCtx))
			}
			break
		}
	}
	return connected && !brokenByFetch
}
