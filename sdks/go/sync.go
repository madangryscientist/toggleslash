package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/sse"
)

const (
	maxRetryAfter = 300 * time.Second
	backoffBase   = time.Second
)

// syncError is a ruleset or stream failure. Its text is fixed by the SDK and holds no URL, header, context or
// server text, so it is safe to log.
type syncError struct {
	code LastError
	text string
}

func (e *syncError) Error() string { return e.text }

// classify turns the error of a request into a syncError, or nil when the request was cut short by Close
// (which is not a failure). ctx is the request's context, whose cause says whether a deadline or Close ended it.
func classify(ctx context.Context, what string, err error) *syncError {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errClosed):
		return nil
	case errors.Is(cause, errTimeout):
		return &syncError{LastErrorTimeout, what + " timed out"}
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Timeout() {
		return &syncError{LastErrorTimeout, what + " timed out"}
	}
	return &syncError{LastErrorNetwork, what + " failed: network error"}
}

func httpError(what string, status int) *syncError {
	return &syncError{LastError("http_" + strconv.Itoa(status)), what + " answered " + strconv.Itoa(status)}
}

// sleep waits d, or until a Retry-After hold has passed if that is later, or until Close. It reports whether
// the client is still open.
func (c *Client) sleep(d time.Duration) bool {
	if hold := c.syncHold.Sub(c.cfg.clock.Now()); hold > d {
		d = hold
	}
	if d <= 0 {
		return c.root.Err() == nil
	}
	t := c.cfg.clock.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return c.root.Err() == nil
	case <-c.root.Done():
		return false
	}
}

// delay is the wait before the next attempt (contract 7.3): with a stream, an exponential backoff from a
// second up to the poll interval, jittered into [0.5, 1) of itself; without one, the poll interval.
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	wait := backoffBase << min(failures, 16)
	if wait <= 0 || wait > c.cfg.pollInterval {
		wait = c.cfg.pollInterval
	}
	return time.Duration(float64(wait) * (0.5 + c.cfg.jitter()/2))
}

// retryAfter reads the wait a 429 asked for: whole decimal digits only, capped. ok is false for any other
// answer and for a header that is missing or is not that.
func retryAfter(h http.Header, status int) (time.Duration, bool) {
	if status != http.StatusTooManyRequests {
		return 0, false
	}
	v := strings.Trim(h.Get("Retry-After"), " ")
	if v == "" {
		return 0, false
	}
	for i := range len(v) {
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

// hold records what a 429 asked for; a shorter answer never shortens a longer wait.
func (c *Client) hold(until *time.Time, h http.Header, status int) {
	if d, ok := retryAfter(h, status); ok {
		if t := c.cfg.clock.Now().Add(d); t.After(*until) {
			*until = t
		}
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	return req, nil
}

// fail records a failure in the status and returns the error, or nil if there was none worth recording.
func (c *Client) fail(err *syncError) error {
	if err == nil {
		return nil
	}
	c.update(func(s *state) { s.lastError = err.code })
	return err
}

// synced records that the ruleset held is confirmed current.
func (c *Client) synced(edit func(*state)) {
	now := c.cfg.clock.Now()
	c.update(func(s *state) {
		if edit != nil {
			edit(s)
		}
		s.lastSyncAt, s.lastError = now, ""
	})
}

// fetch makes one conditional load of the ruleset. It returns the failure, already recorded in the status, for
// the caller to report once no lock is held; a load Close cut short is not a failure.
func (c *Client) fetch() error {
	const what = "GET /sdk/v1/ruleset"
	ctx, cancel := context.WithTimeoutCause(c.root, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	held := c.state.Load()
	req, err := c.newRequest(ctx, http.MethodGet, "/sdk/v1/ruleset", nil)
	if err != nil {
		return c.fail(&syncError{LastErrorNetwork, what + " failed: network error"})
	}
	if held.rs != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.fail(classify(ctx, what, err))
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotModified:
		// A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is unexpected.
		if held.rs == nil {
			return c.fail(httpError(what, resp.StatusCode))
		}
		c.synced(nil)
		return nil
	case http.StatusOK:
	default:
		c.hold(&c.syncHold, resp.Header, resp.StatusCode)
		return c.fail(httpError(what, resp.StatusCode))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.fail(classify(ctx, what, err))
	}
	root, err := jsonvalue.ParseBody(data)
	if err != nil || !hasRulesetShape(root) {
		return c.fail(&syncError{LastErrorBadRuleset, what + " sent no usable ruleset"})
	}
	rs := evaluator.NewRuleset(root)
	etag := resp.Header.Get("ETag") // an empty tag is none, and is never sent back as If-None-Match
	c.synced(func(s *state) { s.rs, s.etag = rs, etag })
	return nil
}

// hasRulesetShape: an object whose "flags" and "segments" are both arrays.
func hasRulesetShape(v Value) bool {
	flags, ok1 := v.Get("flags")
	segments, ok2 := v.Get("segments")
	return v.IsObject() && ok1 && ok2 && flags.IsArray() && segments.IsArray()
}

// report hands failures to onError on the worker's goroutine, which is marked as running a callback.
func (c *Client) report(t *task, errs ...error) {
	var list []error
	for _, err := range errs {
		if err != nil {
			list = append(list, err)
		}
	}
	if len(list) == 0 || c.cfg.onError == nil {
		return
	}
	t.callback(func() {
		for _, err := range list {
			c.callOnError(err)
		}
	})
}

// callOnError calls the application's handler; a panic in it is ignored.
func (c *Client) callOnError(err error) {
	if c.cfg.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.onError(err)
}

// syncLoop loads the ruleset once, then follows the server until Close (contract 7.3).
func (c *Client) syncLoop() {
	defer close(c.syncTask.done)
	err := c.fetch()
	c.settleReady(c.state.Load().rs != nil)
	c.report(c.syncTask, err)
	failures := 0
	for c.root.Err() == nil {
		connected := false
		if c.cfg.stream {
			// A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
			if !c.sleep(0) {
				return
			}
			connected = c.follow()
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		// Wait first: the ruleset was only just loaded, or the stream only just failed. A stream that was
		// up needs no poll, since reconnecting announces the tag and catches up on its own.
		if !c.sleep(c.delay(failures)) {
			return
		}
		if !connected {
			c.report(c.syncTask, c.fetch())
		}
	}
}

// follow runs one stream connection until it ends or fails and reports whether it connected.
func (c *Client) follow() (connected bool) {
	const what = "GET /sdk/v1/stream"
	ctx, cancel := context.WithCancelCause(c.root)
	defer cancel(nil)
	defer c.update(func(s *state) { s.stream = StreamDisconnected })
	c.update(func(s *state) { s.stream = StreamConnecting })
	req, err := c.newRequest(ctx, http.MethodGet, "/sdk/v1/stream", nil)
	if err != nil {
		c.report(c.syncTask, c.fail(&syncError{LastErrorNetwork, what + " failed: network error"}))
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	open := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	resp, err := c.http.Do(req)
	open.Stop()
	if err != nil {
		c.report(c.syncTask, c.fail(classify(ctx, what, err)))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		c.hold(&c.syncHold, resp.Header, resp.StatusCode)
		c.report(c.syncTask, c.fail(httpError(what, resp.StatusCode)))
		return false
	}
	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errTimeout) })
	defer watchdog.Stop()
	var parser sse.Parser
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(c.cfg.watchdog)
			for _, ev := range parser.Write(buf[:n]) {
				if ev.Name != "connected" && ev.Name != "changed" {
					continue
				}
				if ev.Name == "connected" {
					connected = true
					c.update(func(s *state) { s.stream, s.lastError = StreamConnected, "" })
				}
				watchdog.Stop()
				ok := c.onTag(ev.Data)
				watchdog.Reset(c.cfg.watchdog)
				if !ok {
					// The fetch the stream asked for failed, and a stream that stays up would never retry
					// it. End it and count it as not connected, so the backoff and the poll take over.
					return false
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.report(c.syncTask, c.fail(classify(ctx, what, err)))
			}
			return connected
		}
	}
}

// onTag handles the ETag the stream names: nothing to fetch when it is the one held.
func (c *Client) onTag(tag string) bool {
	held := c.state.Load()
	if held.rs != nil && tag == held.etag {
		c.synced(nil)
		return true
	}
	err := c.fetch()
	c.report(c.syncTask, err)
	return err == nil
}
