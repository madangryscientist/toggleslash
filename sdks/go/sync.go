package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

var errStreamIdle = errors.New("stream idle")

// attempt is how one ruleset or stream attempt ended. err is nil on success and when the attempt was
// abandoned by Close, which is neither reported nor recorded.
type attempt struct {
	ok  bool
	err *Error
}

// loop is the loop goroutine: the first load, then the stream and the polling of contract 7.3, until Close.
func (c *Client) loop() {
	defer close(c.loopDone)
	c.loopGID.Store(goroutineID())
	first := c.fetchRuleset()
	c.settleFirst(c.snap.Load() != nil)
	c.report(first.err)

	failures := 0
	for c.root.Err() == nil {
		connected := false
		if c.cfg.stream {
			if !c.sleep(0) {
				return
			}
			connected = c.followStream()
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.sleep(c.delay(failures)) {
			return
		}
		if !connected {
			a := c.fetchRuleset()
			c.report(a.err)
		}
	}
}

// delay is the wait of contract 7.3: the poll interval with the stream off, else a jittered backoff.
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	exp := min(failures, 16)
	ceiling := min(c.cfg.pollInterval, time.Second<<exp)
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.jitter()/2))
}

// sleep waits d, and at least until any Retry-After hold on the ruleset and stream has passed. It reports
// false when Close ended the wait.
func (c *Client) sleep(d time.Duration) bool {
	target := c.cfg.clock.Now().Add(d)
	for {
		c.mu.Lock()
		hold := c.holdSync
		c.mu.Unlock()
		if hold.After(target) {
			target = hold
		}
		remaining := target.Sub(c.cfg.clock.Now())
		if remaining <= 0 {
			return c.root.Err() == nil
		}
		t := c.cfg.clock.NewTimer(remaining)
		select {
		case <-t.C():
		case <-c.root.Done():
			t.Stop()
			return false
		}
	}
}

// parseRetryAfter reads a Retry-After header: whole decimal seconds only, capped at five minutes. ok is
// false for anything else (a date, a fraction, a negative number, nothing).
func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.Trim(value, " \t")
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	value = strings.TrimLeft(value, "0")
	if len(value) > 6 {
		return maxRetryAfter, true
	}
	if value == "" {
		return 0, true
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return min(time.Duration(seconds)*time.Second, maxRetryAfter), true
}

// holdFrom lengthens a hold to the Retry-After in a 429 answer; a later one never shortens it.
func (c *Client) holdFrom(resp *http.Response, hold *time.Time) {
	if resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"))
	if !ok {
		return
	}
	until := c.cfg.clock.Now().Add(wait)
	c.mu.Lock()
	if until.After(*hold) {
		*hold = until
	}
	c.mu.Unlock()
}

// failure classifies a request error into a code. abandoned is true when Close caused it.
func (c *Client) failure(ctx context.Context, op string) (err *Error, abandoned bool) {
	if c.isClosing() {
		return nil, true
	}
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errRequestTimeout), errors.Is(cause, errStreamIdle):
		return &Error{Op: op, Code: LastErrorTimeout}, false
	default:
		return &Error{Op: op, Code: LastErrorNetwork}, false
	}
}

// fail records a failed ruleset or stream attempt in lastError, unless Close caused it.
func (c *Client) fail(err *Error) attempt {
	if err == nil {
		return attempt{}
	}
	c.mu.Lock()
	if !c.closing {
		c.lastError = err.Code
	} else {
		err = nil
	}
	c.mu.Unlock()
	return attempt{err: err}
}

// synced records that the ruleset held matches the server: lastSyncAt moves and lastError clears.
func (c *Client) synced() {
	c.mu.Lock()
	c.lastSyncAt = c.cfg.clock.Now()
	c.lastError = ""
	c.mu.Unlock()
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	return req
}

// fetchRuleset does the ruleset request of contract 7.1, bounded as a whole (the body read included) by
// the request timeout, and applies the answer. It does not call onError: the caller does, after it has
// settled ready if this was the first load.
func (c *Client) fetchRuleset() attempt {
	ctx, cancel := context.WithTimeoutCause(c.root, c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	req := c.newRequest(ctx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if req == nil {
		return c.fail(&Error{Op: "ruleset", Code: LastErrorNetwork})
	}
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		e, abandoned := c.failure(ctx, "ruleset")
		if abandoned {
			return attempt{}
		}
		return c.fail(e)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return c.applyRuleset(ctx, resp)
	case http.StatusNotModified:
		if held == nil {
			return c.fail(&Error{Op: "ruleset", Code: "http_304"})
		}
		c.synced()
		return attempt{ok: true}
	default:
		c.holdFrom(resp, &c.holdSync)
		return c.fail(&Error{Op: "ruleset", Code: "http_" + strconv.Itoa(resp.StatusCode)})
	}
}

// applyRuleset reads a 200 body and, if it is a ruleset, swaps it in with its ETag.
func (c *Client) applyRuleset(ctx context.Context, resp *http.Response) attempt {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e, abandoned := c.failure(ctx, "ruleset")
		if abandoned {
			return attempt{}
		}
		return c.fail(e)
	}
	value, err := jsonvalue.Parse(body)
	if err != nil || !hasRulesetShape(value) {
		if c.root.Err() != nil {
			return attempt{}
		}
		return c.fail(&Error{Op: "ruleset", Code: LastErrorBadRuleset})
	}
	rs, ok := compile(value)
	if !ok {
		return c.fail(&Error{Op: "ruleset", Code: LastErrorNetwork})
	}
	next := &snapshot{ruleset: rs, etag: resp.Header.Get("ETag")}
	c.mu.Lock()
	c.snap.Store(next)
	c.lastSyncAt = c.cfg.clock.Now()
	c.lastError = ""
	c.mu.Unlock()
	return attempt{ok: true}
}

// compile builds the evaluator's ruleset, turning an unexpected panic into a failure.
func compile(value jsonvalue.Value) (rs *evaluator.Ruleset, ok bool) {
	defer func() {
		if recover() != nil {
			rs, ok = nil, false
		}
	}()
	return evaluator.Compile(value), true
}

// hasRulesetShape is contract 5.5: an object whose flags and segments are both arrays.
func hasRulesetShape(v jsonvalue.Value) bool {
	if v.Kind() != jsonvalue.Object {
		return false
	}
	return v.Get("flags").Kind() == jsonvalue.Array && v.Get("segments").Kind() == jsonvalue.Array
}

// followStream opens GET /sdk/v1/stream and reads events until it ends or fails (contract 7.2). It reports
// whether the stream connected and was not ended by a failed fetch.
func (c *Client) followStream() (connected bool) {
	ctx, cancel := context.WithCancelCause(c.root)
	defer cancel(nil)
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)

	opening := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errRequestTimeout) })
	defer opening.Stop()

	req := c.newRequest(ctx, http.MethodGet, "/sdk/v1/stream", nil)
	if req == nil {
		c.report(c.fail(&Error{Op: "stream", Code: LastErrorNetwork}).err)
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		e, abandoned := c.failure(ctx, "stream")
		if !abandoned {
			c.report(c.fail(e).err)
		}
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	opening.Stop()
	if resp.StatusCode != http.StatusOK {
		c.holdFrom(resp, &c.holdSync)
		c.report(c.fail(&Error{Op: "stream", Code: "http_" + strconv.Itoa(resp.StatusCode)}).err)
		return false
	}

	idle := time.AfterFunc(c.cfg.idle, func() { cancel(errStreamIdle) })
	defer idle.Stop()

	fetchFailed := false
	stop := false
	parser := newSSEParser(func(name, data string) {
		if stop || (name != "connected" && name != "changed") {
			return
		}
		if name == "connected" {
			connected = true
			c.setStream(StreamConnected)
			c.mu.Lock()
			c.lastError = ""
			c.mu.Unlock()
		}
		if !c.onTag(data) {
			fetchFailed, stop = true, true
			cancel(errStreamEnded)
		}
	})
	buf := make([]byte, 4096)
	for !stop {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(c.cfg.idle)
			parser.feed(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !stop {
				if e, abandoned := c.failure(ctx, "stream"); !abandoned {
					c.report(c.fail(e).err)
				}
			}
			break
		}
	}
	return connected && !fetchFailed
}

var errStreamEnded = errors.New("stream ended")

// onTag handles the ETag a stream event names: a sync if it is the one held, else a ruleset request. It
// reports false when that request failed, which ends the stream.
func (c *Client) onTag(tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag == tag {
		c.synced()
		return true
	}
	a := c.fetchRuleset()
	c.report(a.err)
	return a.ok
}
