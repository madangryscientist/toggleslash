package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// errTimeout is the cancel cause of a request that ran past requestTimeout or a stream that went quiet.
	errTimeout = errors.New("request timed out")
	// errClosed is the cancel cause of the client's lifetime context.
	errClosed = errors.New("client closed")
)

// newHTTPClient makes the client's own http.Client: a transport of its own (so Close can drop its idle
// connections) and no redirects, so the key is never sent to a second URL and a 3xx is an error.
func newHTTPClient() *http.Client {
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// header is one request header.
type header struct{ name, value string }

// reply is an answer with its body read, when the caller asked for the body.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// classify turns a failed request into the SDK's own error, from the cancel cause. A request that failed
// because the client closed returns (nil, true).
func classify(lifetime, requestCtx context.Context, what string) (*syncError, bool) {
	if errors.Is(context.Cause(requestCtx), errTimeout) {
		return newSyncError(what, LastErrorTimeout), false
	}
	if lifetime.Err() != nil {
		return nil, true
	}
	return newSyncError(what, LastErrorNetwork), false
}

// do makes one request and, when readBody is set and the answer is 200, reads its body. requestTimeout bounds
// all of it. A failure is a *syncError with a fixed text; the second result is true when the failure was the
// client closing, which the caller does not report.
func (c *Client) do(lifetime context.Context, method, path, what string, headers []header, body []byte, readBody bool) (reply, *syncError, bool) {
	ctx, cancel := context.WithCancelCause(lifetime)
	defer cancel(nil)
	stop := c.cfg.clock.AfterFunc(c.cfg.requestTimeout, func() { cancel(errTimeout) })
	defer stop.Stop()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.url+path, reader)
	if err != nil {
		return reply{}, newSyncError(what, LastErrorNetwork), false
	}
	for _, h := range headers {
		req.Header.Set(h.name, h.value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		e, closed := classify(lifetime, ctx, what)
		return reply{}, e, closed
	}
	defer func() { _ = resp.Body.Close() }()
	out := reply{status: resp.StatusCode, header: resp.Header}
	if readBody && resp.StatusCode == http.StatusOK {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			e, closed := classify(lifetime, ctx, what)
			return reply{}, e, closed
		}
		out.body = data
	}
	return out, nil, false
}

// retryAfter reads a Retry-After header: whole decimal seconds, surrounding spaces ignored, capped at 300 s.
// Anything else (a date, a fraction, a sign, nothing) is not a hold.
func retryAfter(h http.Header) (time.Duration, bool) {
	raw := strings.Trim(h.Get("Retry-After"), " ")
	if raw == "" {
		return 0, false
	}
	for i := range len(raw) {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || seconds > uint64(maxRetryAfter/time.Second) {
		return maxRetryAfter, true
	}
	return time.Duration(seconds) * time.Second, true
}
