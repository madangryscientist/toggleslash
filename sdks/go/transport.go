package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The HTTP side of the client, on net/http: one request is bounded as a whole (connect, TLS, head and body)
// by docs/sdk-contract.md 7.1, and the stream's open is bounded separately from its reads (7.2).

var (
	// errTimeout is a request that ran out of its request timeout, or a stream that went quiet.
	errTimeout = errors.New("timeout")
	// errAborted is a request the client abandoned because close was called.
	errAborted = errors.New("aborted")
	// errNetwork is any other transport failure. Its text never carries the URL, the key or any data.
	errNetwork = errors.New("network")
)

// retryAfterMax is the cap on a Retry-After hold (contract 7.4).
const retryAfterMax = 300 * time.Second

// retryAfter reads a Retry-After on a 429: whole decimal seconds only, surrounding spaces and tabs ignored,
// capped at five minutes.
func retryAfter(status int, value string) (time.Duration, bool) {
	if status != 429 {
		return 0, false
	}
	text := strings.Trim(value, " \t")
	if text == "" {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if !isDigitChar(text[i]) {
			return 0, false
		}
	}
	text = strings.TrimLeft(text, "0")
	if len(text) > 3 {
		return retryAfterMax, true
	}
	seconds := 0
	for i := 0; i < len(text); i++ {
		seconds = seconds*10 + int(text[i]-'0')
	}
	wait := time.Duration(seconds) * time.Second
	if wait > retryAfterMax {
		return retryAfterMax, true
	}
	return wait, true
}

// retryHold is a time before which nothing may be sent to one group of routes. It only ever lengthens.
type retryHold struct {
	mu    sync.Mutex
	until time.Time
	clock func() time.Time
}

func newRetryHold(clock func() time.Time) *retryHold {
	return &retryHold{clock: clock}
}

func (h *retryHold) raiseTo(wait time.Duration) {
	until := h.clock().Add(wait)
	h.mu.Lock()
	if until.After(h.until) {
		h.until = until
	}
	h.mu.Unlock()
}

func (h *retryHold) remaining() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	left := h.until.Sub(h.clock())
	if left < 0 {
		return 0
	}
	return left
}

// httpResponse is one answer, its body read whole.
type httpResponse struct {
	status int
	header http.Header
	body   []byte
}

// httpTransport sends to one server. Every request watches ctx, which close cancels, so a request in flight is
// abandoned at once.
type httpTransport struct {
	client *http.Client
	ctx    context.Context
}

func newHTTPTransport(ctx context.Context, roundTripper http.RoundTripper) *httpTransport {
	return &httpTransport{
		ctx: ctx,
		client: &http.Client{
			Transport: roundTripper,
			// Redirects are not followed: a 3xx becomes an http_<status> error like any other status.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// buildURL joins the configured base URL and a route path. Trailing "/" characters are removed from the base
// and nothing else is rewritten; a URL the HTTP client cannot use is a network error on every request.
func buildURL(base, path string) (string, error) {
	trimmed := strings.TrimRight(base, "/")
	if trimmed == "" {
		return "", errNetwork
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errNetwork
	}
	return trimmed + path, nil
}

// newRequest builds a request with its headers. No header outside the protocol's is set: net/http adds its own
// (User-Agent, Accept-Encoding, Host, Content-Length), and RUN.md lists each.
func (t *httpTransport) newRequest(ctx context.Context, method, base, path string, headers map[string]string, body []byte) (*http.Request, error) {
	target, err := buildURL(base, path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, errNetwork
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request, nil
}

// classify turns a transport error into one of the three codes, without ever reading the error's text (a
// net/http error carries the URL).
func (t *httpTransport) classify(ctx context.Context) error {
	if errors.Is(t.ctx.Err(), context.Canceled) {
		return errAborted
	}
	cause := context.Cause(ctx)
	if errors.Is(cause, errTimeout) || errors.Is(cause, context.DeadlineExceeded) {
		return errTimeout
	}
	if errors.Is(cause, context.Canceled) {
		return errAborted
	}
	return errNetwork
}

// do sends one request and reads its whole body, all within timeout.
func (t *httpTransport) do(method, base, path string, headers map[string]string, body []byte, timeout time.Duration) (*httpResponse, error) {
	ctx, cancel := context.WithTimeoutCause(t.ctx, timeout, errTimeout)
	defer cancel()
	request, err := t.newRequest(ctx, method, base, path, headers, body)
	if err != nil {
		return nil, err
	}
	response, err := t.client.Do(request)
	if err != nil {
		return nil, t.classify(ctx)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, t.classify(ctx)
	}
	return &httpResponse{status: response.StatusCode, header: response.Header, body: data}, nil
}

// openedStream is an answer whose head has arrived. Its body is read until it ends.
type openedStream struct {
	status int
	header http.Header
	body   io.ReadCloser
	cancel context.CancelFunc
}

func (o *openedStream) close() {
	o.cancel()
	if o.body != nil {
		_ = o.body.Close()
	}
}

// openStream sends GET and returns once the response headers have arrived, or after timeout, whichever comes
// first. The reads after that are bounded by the caller's watchdog, not by this timeout.
func (t *httpTransport) openStream(base, path string, headers map[string]string, timeout time.Duration) (*openedStream, error) {
	ctx, cancel := context.WithCancel(t.ctx)
	request, err := t.newRequest(ctx, http.MethodGet, base, path, headers, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	type answer struct {
		response *http.Response
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := t.client.Do(request)
		done <- answer{response: response, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case got := <-done:
		if got.err != nil {
			cancel()
			return nil, t.classify(ctx)
		}
		return &openedStream{status: got.response.StatusCode, header: got.response.Header, body: got.response.Body, cancel: cancel}, nil
	case <-timer.C:
		cancel()
		return nil, errTimeout
	case <-t.ctx.Done():
		cancel()
		return nil, errAborted
	}
}

// describeError is the fixed text an onError message carries for a failed attempt, so that no message can hold
// a URL, a key or anything about a context.
func describeError(code error, what string) string {
	switch {
	case errors.Is(code, errTimeout):
		return what + " timed out"
	case errors.Is(code, errAborted):
		return what + " was abandoned"
	default:
		return what + " failed"
	}
}
