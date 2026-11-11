package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Why a request was cancelled, read back with context.Cause to tell a timeout from a network failure.
var (
	errRequestTimeout = errors.New("request timeout")
	errWatchdog       = errors.New("stream went quiet")
)

// snapshot is a ruleset and the tag it came with. It is immutable and swapped as one pointer, so a
// reader never sees a ruleset with another ruleset's tag.
type snapshot struct {
	rs      *evaluator.Ruleset
	etag    string
	hasETag bool
}

// loadRuleset builds a ruleset from a parsed body, or reports that the body is not one: it must be an
// object whose flags and segments are both arrays.
func loadRuleset(body jsonv.Value) (rs *evaluator.Ruleset, ok bool) {
	flags, hasFlags := body.Get("flags")
	segments, hasSegments := body.Get("segments")
	if !hasFlags || !hasSegments {
		return nil, false
	}
	if _, isArr := flags.Array(); !isArr {
		return nil, false
	}
	if _, isArr := segments.Array(); !isArr {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			rs, ok = nil, false
		}
	}()
	return evaluator.Load(body), true
}

// retryAfter reads a Retry-After that is whole decimal digits (spaces around them are trimmed) as a
// wait, capped at maxRetryAfter. Anything else, an HTTP date included, is not read.
func retryAfter(h http.Header) (time.Duration, bool) {
	v := strings.Trim(h.Get("Retry-After"), " \t")
	if v == "" {
		return 0, false
	}
	secs := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
		if secs <= int(maxRetryAfter/time.Second) {
			secs = secs*10 + int(r-'0')
		}
	}
	d := time.Duration(secs) * time.Second
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d, true
}

// holdFor asks the ruleset and stream requests to wait until d from now; a shorter answer never
// shortens a longer wait.
func (c *Client) holdFor(h http.Header, events bool) {
	d, ok := retryAfter(h)
	if !ok {
		return
	}
	until := time.Now().Add(d)
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	target := &c.holdRuleset
	if events {
		target = &c.holdEvents
	}
	if until.After(*target) {
		*target = until
	}
}

func (c *Client) rulesetHold() time.Time {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.holdRuleset
}

// wait sleeps for d, or until the hold has passed if that is later, or until ctx is done.
func (c *Client) wait(ctx context.Context, d time.Duration) {
	until := time.Now().Add(d)
	if hold := c.rulesetHold(); hold.After(until) {
		until = hold
	}
	w := time.Until(until)
	if w <= 0 {
		return
	}
	t := time.NewTimer(w)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// delay is the wait after an attempt: the poll interval with no stream, otherwise exponential from a
// second up to the poll interval, jittered into [0.5, 1) of that.
func (c *Client) delay(failures int) time.Duration {
	if !c.opt.stream {
		return c.opt.pollInterval
	}
	ceiling := backoffBase << min(failures, 16)
	ceiling = min(ceiling, c.opt.pollInterval)
	return time.Duration(float64(ceiling) * (0.5 + c.jitter()/2))
}

// syncLoop is the sync goroutine: the first load at once, then contract 7.3's loop until closed.
func (c *Client) syncLoop() {
	defer c.wg.Done()
	defer c.own()()
	ctx := c.loopCtx
	c.refresh(ctx)
	c.settleReady(c.snap.Load() != nil)
	failures := 0
	for ctx.Err() == nil {
		// A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
		if c.opt.stream {
			c.wait(ctx, 0)
		}
		if ctx.Err() != nil {
			return
		}
		connected := c.opt.stream && c.follow(ctx)
		if connected {
			failures = 0
		} else {
			failures++
		}
		c.wait(ctx, c.delay(failures))
		// Wait first: the ruleset was only just loaded, or the stream only just failed. A stream that
		// was up needs no poll, since reconnecting announces the tag and catches up on its own.
		if !connected && ctx.Err() == nil {
			c.refresh(ctx)
		}
	}
}

// requestFailure turns a transport error into a LastError and its fixed text, or reports that the
// request was abandoned because the client is closing.
func (c *Client) requestFailure(parent, req context.Context) (LastError, bool) {
	if parent.Err() != nil {
		return "", false
	}
	cause := context.Cause(req)
	if errors.Is(cause, errRequestTimeout) || errors.Is(cause, errWatchdog) {
		return LastErrorTimeout, true
	}
	return LastErrorNetwork, true
}

// failed records why a ruleset or stream attempt failed and tells onError. The text is fixed: it never
// carries the URL, the key or anything from the response.
func (c *Client) failed(ctx context.Context, code LastError, route string) {
	if ctx.Err() != nil {
		return
	}
	c.statusMu.Lock()
	c.lastErr = code
	c.statusMu.Unlock()
	var msg string
	switch code {
	case LastErrorTimeout:
		msg = route + " timed out"
	case LastErrorNetwork:
		msg = route + " failed: network error"
	case LastErrorBadRuleset:
		msg = route + " sent no flags and segments"
	default:
		msg = route + " answered " + strings.TrimPrefix(string(code), "http_")
	}
	c.reportError(errors.New(msg))
}

// synced records that the ruleset held is confirmed current as of now.
func (c *Client) synced() {
	c.statusMu.Lock()
	c.lastSync = c.now()
	c.lastErr = LastErrorNone
	c.statusMu.Unlock()
}

// replace swaps in a new ruleset and tag together, and counts as a sync.
func (c *Client) replace(s *snapshot) {
	c.statusMu.Lock()
	c.snap.Store(s)
	c.lastSync = c.now()
	c.lastErr = LastErrorNone
	c.statusMu.Unlock()
}

const rulesetRoute = "GET /sdk/v1/ruleset"
const streamRoute = "GET /sdk/v1/stream"

// refresh is one conditional fetch of the ruleset. It reports whether the ruleset is now confirmed.
func (c *Client) refresh(ctx context.Context) bool {
	rctx, cancel := context.WithTimeoutCause(ctx, c.opt.requestTimeout, errRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, c.base+"/sdk/v1/ruleset", nil)
	if err != nil {
		c.failed(ctx, LastErrorNetwork, rulesetRoute)
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	held := c.snap.Load()
	if held != nil && held.hasETag {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if code, ok := c.requestFailure(ctx, rctx); ok {
			c.failed(ctx, code, rulesetRoute)
		}
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		// A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is unexpected.
		if c.snap.Load() == nil {
			c.failed(ctx, httpError(304), rulesetRoute)
			return false
		}
		c.synced()
		return true
	default:
		if resp.StatusCode == http.StatusTooManyRequests {
			c.holdFor(resp.Header, false)
		}
		c.failed(ctx, httpError(resp.StatusCode), rulesetRoute)
		return false
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if code, ok := c.requestFailure(ctx, rctx); ok {
			c.failed(ctx, code, rulesetRoute)
		}
		return false
	}
	body, err := jsonv.Parse(raw)
	if err != nil {
		c.failed(ctx, LastErrorBadRuleset, rulesetRoute)
		return false
	}
	rs, ok := loadRuleset(body)
	if !ok {
		c.failed(ctx, LastErrorBadRuleset, rulesetRoute)
		return false
	}
	// An empty ETag counts as none, so it is never sent back as If-None-Match.
	etag := resp.Header.Get("ETag")
	c.replace(&snapshot{rs: rs, etag: etag, hasETag: etag != ""})
	return true
}

// follow reads GET /sdk/v1/stream until it ends or fails, and reports whether it connected, so the
// caller can tell a stream that was up from one that never was.
func (c *Client) follow(ctx context.Context) (connected bool) {
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)
	sctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(c.opt.requestTimeout, func() { cancel(errRequestTimeout) })
	defer func() { timer.Stop() }()

	req, err := http.NewRequestWithContext(sctx, http.MethodGet, c.base+"/sdk/v1/stream", nil)
	if err != nil {
		c.failed(ctx, LastErrorNetwork, streamRoute)
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		if code, ok := c.requestFailure(ctx, sctx); ok {
			c.failed(ctx, code, streamRoute)
		}
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.holdFor(resp.Header, false)
		}
		c.failed(ctx, httpError(resp.StatusCode), streamRoute)
		return false
	}
	// The headers came: from here a stream that sends no byte for the idle time is dead.
	timer.Stop()
	timer = time.AfterFunc(c.opt.watchdog, func() { cancel(errWatchdog) })

	var parser sseParser
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(c.opt.watchdog)
			events, perr := parser.feed(buf[:n])
			for _, ev := range events {
				if ev.name != "changed" && ev.name != "connected" {
					continue
				}
				if ev.name == "connected" {
					connected = true
					c.markConnected()
				}
				if !c.onTag(ctx, ev.data) {
					// The fetch the stream asked for failed, and a stream that stays up would never retry
					// it. End it and count it as not connected, so the backoff and the poll take over.
					return false
				}
			}
			if perr != nil {
				c.failed(ctx, LastErrorNetwork, streamRoute)
				return false
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return connected
			}
			if code, ok := c.requestFailure(ctx, sctx); ok {
				c.failed(ctx, code, streamRoute)
			}
			return connected
		}
	}
}

// onTag handles a tag the stream announced: fetch only when it is not the one held.
func (c *Client) onTag(ctx context.Context, tag string) bool {
	if held := c.snap.Load(); held != nil && held.hasETag && tag == held.etag {
		c.synced()
		return true
	}
	return c.refresh(ctx)
}

func (c *Client) setStream(s StreamState) {
	if !c.opt.stream {
		return
	}
	c.statusMu.Lock()
	c.streamState = s
	c.statusMu.Unlock()
}

func (c *Client) markConnected() {
	c.statusMu.Lock()
	c.streamState = StreamConnected
	c.lastErr = LastErrorNone
	c.statusMu.Unlock()
}
