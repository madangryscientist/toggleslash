package toggleslash

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/sse"
)

// follow opens GET /sdk/v1/stream and reads it until it ends or fails (7.2).
// It reports whether the stream connected and was not ended by a failed fetch,
// so the loop can tell a stream that was up from one that never was.
func (c *Client) follow() (connected bool) {
	c.setStream(StreamConnecting)
	defer func() {
		if c.ctx.Err() == nil {
			c.setStream(StreamDisconnected)
		}
	}()

	ctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)

	// Opening is bounded until the headers arrive; then the idle watchdog takes over.
	openTimer := time.AfterFunc(c.timeout, func() { cancel(errTimeout) })
	defer openTimer.Stop()

	req, err := newStreamRequest(ctx, c)
	if err != nil {
		c.fail("stream", failure{code: "network"})
		return false
	}
	resp, err := c.hc.Do(req)
	openTimer.Stop()
	if err != nil {
		c.fail("stream", failure{code: c.streamCode(ctx)})
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		f := failure{code: statusCode(resp.StatusCode)}
		if resp.StatusCode == 429 {
			f.retryAfter, f.hasRetryAfter = retryAfter(resp.Header)
		}
		c.fail("stream", f)
		return false
	}

	watchdog := time.AfterFunc(c.watchdog, func() { cancel(errWatchdog) })
	defer watchdog.Stop()

	var (
		parser    sse.Parser
		sawEvent  bool
		fetchFail bool
		buf       = make([]byte, 4096)
	)
	handle := func(e sse.Event) bool {
		if e.Name != "connected" && e.Name != "changed" {
			return true
		}
		if e.Name == "connected" {
			sawEvent = true
			c.stMu.Lock()
			c.streamState = StreamConnected
			c.lastError = ""
			c.stMu.Unlock()
		}
		if !c.onTag(e.Data) {
			// The fetch the stream asked for failed, and a stream that stays up would
			// never retry it. End it and count it as not connected, so the backoff and
			// the poll take over.
			fetchFail = true
			return false
		}
		return true
	}
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(c.watchdog)
			if !parser.Feed(buf[:n], handle) {
				return false
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				// The server ended the stream: not an error, and lastError stays as it is.
				return sawEvent && !fetchFail
			}
			c.fail("stream", failure{code: c.streamCode(ctx)})
			return sawEvent && !fetchFail
		}
	}
}

// streamCode names why the stream's request or read failed, from the cause its
// context was cancelled with. The error itself holds the URL and is not used.
func (c *Client) streamCode(ctx context.Context) string {
	if c.ctx.Err() != nil {
		return codeAborted
	}
	if cause := context.Cause(ctx); errors.Is(cause, errTimeout) || errors.Is(cause, errWatchdog) {
		return "timeout"
	}
	return "network"
}

// onTag handles the tag a connected or changed event names: when it is the
// ETag held, that is a sync and nothing is fetched; otherwise the ruleset is
// fetched. It reports false when that fetch failed.
func (c *Client) onTag(tag string) bool {
	if held := c.snap.Load(); held != nil && held.etag != "" && held.etag == tag {
		c.synced()
		return true
	}
	return c.refresh()
}
