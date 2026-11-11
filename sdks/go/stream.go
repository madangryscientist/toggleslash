package toggleslash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// followStream opens the stream and follows it until it ends (contract 7.2). It reports whether the stream
// connected and was not ended by a failed fetch.
func (c *Client) followStream() (connected bool) {
	c.update(func(s *snapshot) { s.stream = StreamConnecting })
	defer c.update(func(s *snapshot) { s.stream = StreamDisconnected })

	ctx, cancel := context.WithCancelCause(c.ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/sdk/v1/stream", nil)
	if err != nil {
		c.fail(&SyncError{Route: "stream", Detail: LastErrorNetwork})
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "text/event-stream")

	// The open is bounded until the headers arrive.
	opening := time.AfterFunc(c.cfg.requestTimeout, func() { cancel(errRequestTimeout) })
	resp, err := c.hc.Do(req)
	opening.Stop()
	if err != nil {
		c.fail(c.classifyStream(ctx))
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			if d, ok := c.parseRetryAfter(resp.Header.Get("Retry-After")); ok {
				c.hold(&c.holdSync, d)
			}
		}
		c.fail(&SyncError{Route: "stream", Detail: httpDetail(resp.StatusCode)})
		return false
	}

	failedFetch := false
	gotConnected := false
	parser := &sseParser{}
	parser.emit = func(name string, data []byte) {
		if name != "connected" && name != "changed" {
			return
		}
		tag := string(data)
		if name == "connected" {
			gotConnected = true
			c.update(func(s *snapshot) { s.stream, s.lastError = StreamConnected, "" })
		}
		if s := c.snap.Load(); s.rs != nil && s.etag != "" && s.etag == tag {
			c.update(func(s *snapshot) { s.lastSyncAt, s.lastError = c.clock(), "" })
			return
		}
		if err := c.loadRuleset(); err != nil {
			failedFetch = true
			c.fail(err)
			cancel(errStreamEnded)
		}
	}

	// The watchdog: any byte restarts it, a keep-alive comment included.
	watchdog := time.AfterFunc(c.cfg.watchdog, func() { cancel(errWatchdog) })
	defer watchdog.Stop()
	buf := make([]byte, 4096)
	for !failedFetch {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(c.cfg.watchdog)
			parser.feed(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !failedFetch {
				c.fail(c.classifyStream(ctx))
			}
			break
		}
	}
	return gotConnected && !failedFetch
}

var errStreamEnded = errors.New("stream ended")

// classifyStream names why the stream failed from its context only.
func (c *Client) classifyStream(ctx context.Context) error {
	if c.ctx.Err() != nil {
		return errClosed
	}
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errRequestTimeout), errors.Is(cause, errWatchdog):
		return &SyncError{Route: "stream", Detail: LastErrorTimeout}
	}
	return &SyncError{Route: "stream", Detail: LastErrorNetwork}
}
