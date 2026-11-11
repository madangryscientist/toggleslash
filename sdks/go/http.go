package toggleslash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// maxRetryAfter caps a Retry-After hold at five minutes (contract 7.4).
const maxRetryAfter = 300

var (
	// errRequestTimeout is the cause of a context that ran out of requestTimeout (and of a stream that did
	// not open in time).
	errRequestTimeout = errors.New("request timeout")
	// errWatchdog is the cause of a stream that went 60 s without a byte.
	errWatchdog = errors.New("stream watchdog")
)

// newTransport returns a private transport, a clone of the default one, so Close can drop its idle
// connections without touching the application's.
func newTransport() *http.Transport {
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		return dt.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

// newHTTPClient builds the client every request goes through. A redirect is never followed.
func newHTTPClient(user *http.Client) (*http.Client, *http.Transport) {
	if user != nil {
		c := *user
		c.Timeout = 0
		c.CheckRedirect = noRedirect
		return &c, nil
	}
	tr := newTransport()
	return &http.Client{Transport: tr, CheckRedirect: noRedirect}, tr
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// newRequest builds a request to the configured base URL with the key on it. The only headers the client
// sets are Authorization here, and Accept, If-None-Match and Content-Type at the call sites.
func (c *Client) newRequest(ctx context.Context, method, path string, body *strings.Reader) (*http.Request, error) {
	var (
		req *http.Request
		err error
	)
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	return req, nil
}

func statusError(route string, status int) *RequestError {
	return &RequestError{Code: SyncError(fmt.Sprintf("http_%d", status)), Message: fmt.Sprintf("%s answered %d", route, status)}
}

func badRulesetError(route string) *RequestError {
	return &RequestError{Code: SyncBadRuleset, Message: route + " sent no flags and segments"}
}

// failureOf classifies a request that did not complete: a deadline or watchdog is "timeout", anything
// else "network". The text is fixed, because a *url.Error carries the URL.
func failureOf(ctx context.Context, route string) *RequestError {
	cause := context.Cause(ctx)
	if errors.Is(cause, errRequestTimeout) || errors.Is(cause, errWatchdog) {
		return &RequestError{Code: SyncTimeout, Message: route + " timed out"}
	}
	return &RequestError{Code: SyncNetwork, Message: route + " failed: network error"}
}

// parseRetryAfter reads a Retry-After value that is whole decimal seconds (spaces around it are ignored)
// and caps it at five minutes. A date, a fraction, a sign or an empty value is not valid.
func parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.Trim(v, " ")
	if v == "" {
		return 0, false
	}
	secs := 0
	for i := range len(v) {
		ch := v[i]
		if ch < '0' || ch > '9' {
			return 0, false
		}
		secs = min(secs*10+int(ch-'0'), maxRetryAfter)
	}
	return time.Duration(secs) * time.Second, true
}

// holdFrom raises hold from a 429's Retry-After header. A later answer can lengthen a hold and never
// shortens it.
func (c *Client) holdFrom(hold *atomic.Int64, resp *http.Response) {
	if resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"))
	if !ok {
		return
	}
	until := c.cfg.clock.Now().Add(wait).UnixNano()
	for {
		cur := hold.Load()
		if until <= cur || hold.CompareAndSwap(cur, until) {
			return
		}
	}
}

// holdLeft is how much of a hold is still to pass.
func (c *Client) holdLeft(hold *atomic.Int64) time.Duration {
	left := time.Duration(hold.Load() - c.cfg.clock.Now().UnixNano())
	return max(left, 0)
}

// wait sleeps for d on the client's clock, or until ctx ends; it reports whether the full time passed.
func (c *Client) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	fired := make(chan struct{})
	t := c.cfg.clock.AfterFunc(d, func() { close(fired) })
	select {
	case <-fired:
		return true
	case <-ctx.Done():
		t.Stop()
		return false
	}
}
