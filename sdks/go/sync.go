package toggleslash

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const (
	// defaultWatchdog: the server sends a keep-alive every 25 seconds; this long without a byte and the stream is dead.
	defaultWatchdog = 60 * time.Second
	backoffBase     = time.Second
	maxBackoffShift = 16
)

// snapshot is a ruleset and the ETag it came with. It is never changed after it is built, so one atomic pointer
// swap replaces both together.
type snapshot struct {
	ruleset *eval.Ruleset
	etag    string
}

// run is the sync goroutine: the first load, then the stream or the poll until the client closes.
func (c *Client) run() {
	defer c.wg.Done()
	defer c.sdkGoroutines.add()()

	c.refresh()
	c.settleReady(c.snap.Load() != nil)
	failures := 0
	for !c.closing.Load() {
		// A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
		if c.streaming {
			c.sleep(0)
		}
		if c.closing.Load() {
			return
		}
		connected := c.streaming && c.follow()
		if connected {
			failures = 0
		} else {
			failures++
		}
		c.sleep(c.delay(failures))
		// Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was up needs no
		// poll, since reconnecting announces the tag and catches up on its own.
		if !connected && !c.closing.Load() {
			c.refresh()
		}
	}
}

// sleep waits d, or until the server's Retry-After for the ruleset has passed if that is later, or until close.
func (c *Client) sleep(d time.Duration) {
	if wait := c.syncHold.remaining(c.clk.Now()); wait > d {
		d = wait
	}
	if d <= 0 {
		return
	}
	timer := c.clk.NewTimer(d)
	select {
	case <-timer.C:
	case <-c.ctx.Done():
		timer.Stop()
	}
}

// delay is exponential from a second up to the poll interval, jittered to 0.5 to 1 of that; with no stream, just the
// poll interval.
func (c *Client) delay(failures int) time.Duration {
	if !c.streaming {
		return c.cfg.pollInterval
	}
	shift := failures
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	ceiling := backoffBase << uint(shift)
	if c.cfg.pollInterval < ceiling {
		ceiling = c.cfg.pollInterval
	}
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.jitter()/2))
}

// honourRetryAfter remembers what a 429 asked for.
func (c *Client) honourRetryAfter(resp *http.Response) {
	if wait, ok := retryAfter(resp); ok {
		c.syncHold.extend(c.clk.Now().Add(wait))
	}
}

// synced records that the held ruleset is confirmed current as of now.
func (c *Client) synced() {
	c.mu.Lock()
	c.lastSyncAt = c.clk.Now().UnixMilli()
	c.lastError = ""
	c.mu.Unlock()
}

// fail records why a ruleset or stream attempt failed and tells onError. A failure caused by close is neither.
func (c *Client) fail(ctx context.Context, op string, err error) {
	if c.closing.Load() {
		return
	}
	code := syncCode(ctx, err)
	c.mu.Lock()
	c.lastError = code
	c.mu.Unlock()
	c.report(&Error{Op: op, Code: code})
}

func (c *Client) setStream(state string) {
	c.mu.Lock()
	c.stream = state
	c.mu.Unlock()
}

// refresh is one conditional fetch of the ruleset. It reports whether the ruleset is now confirmed current.
func (c *Client) refresh() bool {
	ctx, release := c.timedContext(c.ctx, c.cfg.requestTimeout)
	defer release()
	err := c.fetchRuleset(ctx)
	if err == nil {
		c.synced()
		return true
	}
	c.fail(ctx, "ruleset", err)
	return false
}

func (c *Client) fetchRuleset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/sdk/v1/ruleset", http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		// A 304 answers If-None-Match, which is sent only with a ruleset held; with none it is unexpected.
		if held == nil {
			return &statusError{status: resp.StatusCode}
		}
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		c.honourRetryAfter(resp)
		return &statusError{status: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	next, ok := buildSnapshot(body, resp.Header.Get("ETag"))
	if !ok {
		return errBadRuleset
	}
	c.mu.Lock()
	c.snap.Store(next)
	c.mu.Unlock()
	return nil
}

// buildSnapshot parses a ruleset body. It is false unless the body is an object whose flags and segments are both
// arrays; an empty ETag counts as none, so it is never sent back as If-None-Match.
func buildSnapshot(body []byte, etag string) (next *snapshot, ok bool) {
	defer func() {
		if recover() != nil {
			next, ok = nil, false
		}
	}()
	root, err := jsonvalue.Parse(body)
	if err != nil || root.Kind() != jsonvalue.Object {
		return nil, false
	}
	flags, hasFlags := root.Get("flags")
	segments, hasSegments := root.Get("segments")
	if !hasFlags || !hasSegments || flags.Kind() != jsonvalue.Array || segments.Kind() != jsonvalue.Array {
		return nil, false
	}
	return &snapshot{ruleset: eval.NewRuleset(root), etag: etag}, true
}

// follow follows GET /sdk/v1/stream until it ends or fails. It reports whether it connected, so the caller can tell
// a stream that was up from one that never was.
func (c *Client) follow() (connected bool) {
	ctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)
	c.setStream(StreamConnecting)
	defer c.setStream(StreamDisconnected)

	opening := c.clk.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	err := c.openStream(ctx, cancel, opening, &connected)
	opening.Stop()
	if err != nil {
		c.fail(ctx, "stream", err)
	}
	return connected
}

func (c *Client) openStream(ctx context.Context, cancel context.CancelCauseFunc, opening *clockTimer, connected *bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/sdk/v1/stream", http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	opening.Stop()
	if resp.StatusCode != http.StatusOK || resp.Body == nil {
		c.honourRetryAfter(resp)
		return &statusError{status: resp.StatusCode}
	}
	return c.pump(ctx, cancel, resp.Body, connected)
}

// pump reads events until the stream ends; the watchdog drops one that goes quiet.
func (c *Client) pump(ctx context.Context, cancel context.CancelCauseFunc, body io.Reader, connected *bool) error {
	watchdog := c.clk.AfterFunc(c.cfg.watchdog, func() { cancel(errQuiet) })
	defer watchdog.Stop()
	var parser sseParser
	buf := make([]byte, 4096)
	for {
		watchdog.Reset(c.cfg.watchdog)
		n, readErr := body.Read(buf)
		watchdog.Stop()
		for _, event := range parser.feed(buf[:n]) {
			if event.name != "changed" && event.name != "connected" {
				continue
			}
			if event.name == "connected" {
				*connected = true
				c.mu.Lock()
				c.stream = StreamConnected
				c.lastError = ""
				c.mu.Unlock()
			}
			if !c.onTag(event.data) {
				// The fetch the stream asked for failed, and a stream that stays up would never retry it. End it and count
				// it as not connected, so the backoff and the poll take over.
				*connected = false
				return nil
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
	}
}

// onTag handles the ETag the stream names: it fetches only when that is not the one held.
func (c *Client) onTag(tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag == tag {
		c.synced()
		return true
	}
	return c.refresh()
}
