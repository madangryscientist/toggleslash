package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The causes a request's context is cancelled with. They never leave the package.
var (
	errTimeout  = errors.New("timeout")
	errWatchdog = errors.New("stream idle")
)

// maxRetryAfter is the longest wait a 429 can ask for.
const maxRetryAfter = 300 * time.Second

// codeAborted marks a request that Close abandoned: nothing records it.
const codeAborted = "aborted"

// newHTTPClient builds the client the SDK sends with. A redirect is never
// followed (a 3xx is http_3xx, and the key is never resent elsewhere), the
// client has no timeout of its own (each request gets a context deadline that
// covers the body), and compression is off so the stream is not buffered and
// the request carries no Accept-Encoding.
func newHTTPClient(user *http.Client) (*http.Client, *http.Transport) {
	if user != nil {
		cp := *user
		cp.Timeout = 0
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return &cp, nil
	}
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.DisableCompression = true
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, transport
}

type result struct {
	status int
	header http.Header
	body   []byte
	// code is "" for an answer, otherwise timeout, network or codeAborted.
	code string
}

// request does one bounded request. The deadline lives until the body has been
// read and closed, so a server that sends headers and then stalls is a timeout.
// readBody says, from the status, whether the body is wanted.
func (c *Client) request(parent context.Context, method, path string, headers [][2]string, payload []byte, readBody func(status int) bool) result {
	ctx, cancel := context.WithTimeoutCause(parent, c.timeout, errTimeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return result{code: "network"}
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return result{code: failureCode(parent, ctx)}
	}
	defer resp.Body.Close()
	out := result{status: resp.StatusCode, header: resp.Header}
	if readBody != nil && readBody(resp.StatusCode) {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return result{code: failureCode(parent, ctx)}
		}
		out.body = data
	}
	return out
}

// failureCode names why a request ended without an answer. A *url.Error holds
// the URL, so the error itself is never looked at, only the contexts.
func failureCode(parent, ctx context.Context) string {
	if parent.Err() != nil {
		return codeAborted
	}
	if errors.Is(context.Cause(ctx), errTimeout) {
		return "timeout"
	}
	return "network"
}

// statusCode is the lastError code for a status other than the expected one.
func statusCode(status int) string { return "http_" + strconv.Itoa(status) }

// retryAfter reads a Retry-After header: whole decimal seconds only (spaces
// around them are ignored), capped at five minutes. A date, a fraction, a
// negative number or nothing at all is not valid.
func retryAfter(h http.Header) (time.Duration, bool) {
	raw := strings.Trim(h.Get("Retry-After"), " ")
	if raw == "" {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
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

// safely runs fn and swallows a panic. The SDK never panics at a caller.
func safely(fn func()) {
	defer func() { _ = recover() }()
	fn()
}

// panicError turns a recovered value into an error for OnError.
func panicError(what string, r any) error {
	if err, ok := r.(error); ok {
		return fmt.Errorf("toggleslash: %s panicked: %w", what, err)
	}
	return fmt.Errorf("toggleslash: %s panicked: %v", what, r)
}

func newStreamRequest(ctx context.Context, c *Client) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/sdk/v1/stream", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "text/event-stream")
	return req, nil
}

// backgroundContext is the parent of a request that Close must not cancel.
func backgroundContext() context.Context { return context.Background() }
