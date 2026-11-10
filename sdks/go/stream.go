package toggleslash

import (
	"context"
	"io"
	"net/http"
	"time"
)

// followStream opens the stream and reads it until it ends. It reports whether the stream's connected
// event arrived and the stream was not ended by a failed fetch.
func (c *Client) followStream() (connected bool) {
	c.setStream(StreamConnecting)
	defer func() {
		if c.root.Err() == nil {
			c.setStream(StreamDisconnected)
		}
	}()

	ctx, cancel := context.WithCancelCause(c.root)
	defer cancel(nil)
	openTimer := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	defer openTimer.Stop()

	req, err := c.request(ctx, http.MethodGet, "/sdk/v1/stream", nil)
	if err != nil {
		c.report(c.note("stream", &failure{code: LastErrorNetwork}))
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.http.Do(req)
	if err != nil {
		c.report(c.note("stream", c.classify(ctx, err)))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	openTimer.Stop()
	if resp.StatusCode != http.StatusOK {
		c.report(c.note("stream", c.statusFailure(resp)))
		return false
	}

	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errTimeout) })
	defer watchdog.Stop()
	var parser sseParser
	failedFetch := false
	handle := func(name, tag string) {
		if failedFetch {
			return
		}
		switch name {
		case "connected":
			connected = true
			c.update(func(v *view) {
				v.stream = StreamConnected
				v.lastError = ""
			})
		case "changed":
		default:
			return
		}
		if held := c.view.Load(); held.ruleset != nil && held.etag == tag {
			c.markSynced()
			return
		}
		if f := c.fetchRuleset(); f != nil {
			c.report(c.note("ruleset request", f))
			failedFetch = true
			cancel(errFetchFailed)
		}
	}

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// Every read restarts the watchdog; it watches the server's silence, not the time a fetch
			// takes, so it is stopped while the events of this read are handled.
			watchdog.Stop()
			parser.feed(buf[:n], handle)
			if failedFetch {
				return false
			}
			watchdog.Reset(c.cfg.watchdog)
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			if f := c.classify(ctx, err); !f.aborted {
				c.report(c.note("stream", f))
			}
		}
		return connected && !failedFetch
	}
}
