package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// watchdogReader restarts the stream's watchdog whenever bytes arrive.
type watchdogReader struct {
	r  io.Reader
	wd *time.Timer
	d  time.Duration
}

func (w *watchdogReader) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if n > 0 {
		w.wd.Reset(w.d)
	}
	return n, err
}

// followStream opens the stream (7.2) and follows it until it ends. It reports whether the stream
// connected and stayed healthy: true only if the connected event arrived and no fetch it asked for
// failed.
func (c *Client) followStream() (connected bool) {
	c.setStream(StreamConnecting)
	defer func() {
		if c.ctx.Err() == nil {
			c.setStream(StreamDisconnected)
		}
	}()

	sctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)
	openTimer := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	defer openTimer.Stop()

	req, err := c.newRequest(sctx, "/sdk/v1/stream")
	if err != nil {
		c.failStream(&syncFailure{code: SyncNetwork, source: SourceStream})
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.hc.Do(req)
	if err != nil {
		c.failStream(c.classify(sctx, SourceStream))
		return false
	}
	defer resp.Body.Close()
	openTimer.Stop()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			if d, ok := parseRetryAfter(resp.Header); ok {
				c.holdRoutes(d)
			}
		}
		c.failStream(&syncFailure{code: httpCode(resp.StatusCode), source: SourceStream})
		return false
	}

	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errTimeout) })
	defer watchdog.Stop()
	parser := newSSEParser(&watchdogReader{r: resp.Body, wd: watchdog, d: c.cfg.watchdog})
	for {
		ev, err := parser.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return connected
			}
			c.failStream(c.classify(sctx, SourceStream))
			return false
		}
		if ev.name != "connected" && ev.name != "changed" {
			continue
		}
		if ev.name == "connected" {
			c.stateMu.Lock()
			if c.stream != StreamClosed {
				c.stream = StreamConnected
			}
			c.lastErr = ""
			c.stateMu.Unlock()
		}
		if held := c.snap.Load(); held != nil && held.etag != "" && held.etag == ev.data {
			c.markSynced()
		} else {
			// The stream is not read while the ruleset is fetched, so its watchdog rests.
			watchdog.Stop()
			f := c.loadRuleset()
			if f != nil {
				if c.ctx.Err() == nil {
					c.reportFromLoop(f.toError())
				}
				return false
			}
			watchdog.Reset(c.cfg.watchdog)
		}
		if ev.name == "connected" {
			connected = true
		}
	}
}

// failStream records and reports a failed stream attempt. A nil failure means Close abandoned it.
func (c *Client) failStream(f *syncFailure) {
	if f == nil {
		return
	}
	c.markError(f.code)
	c.reportFromLoop(f.toError())
}
