package toggleslash

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter is however long a server asks, a client is back within this.
const maxRetryAfter = 300 * time.Second

var (
	// errTimeout is the cause of a request cancelled because it took longer than requestTimeout.
	errTimeout = errors.New("request timeout")
	// errQuiet is the cause of a stream cancelled because it sent nothing for the watchdog's time.
	errQuiet = errors.New("stream went quiet")
	// errBadRuleset: the ruleset body is not JSON or is not an object with a flags and a segments array.
	errBadRuleset = errors.New("bad ruleset")
)

// statusError is an answer the SDK did not expect.
type statusError struct{ status int }

func (e *statusError) Error() string { return "http_" + strconv.Itoa(e.status) }

// retryAfter is how long a 429 asks to wait, capped. It is false for any other answer, and for a Retry-After that is
// missing or is not whole decimal seconds (the server never sends a date), which leaves the caller's usual backoff
// in charge.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds > uint64(maxRetryAfter/time.Second) {
		return maxRetryAfter, true
	}
	return time.Duration(seconds) * time.Second, true
}

// syncCode names a failure for Status.LastError and for the Error handed to onError.
func syncCode(ctx context.Context, err error) string {
	var status *statusError
	if errors.As(err, &status) {
		return status.Error()
	}
	if errors.Is(err, errBadRuleset) {
		return SyncBadRuleset
	}
	if cause := context.Cause(ctx); errors.Is(cause, errTimeout) || errors.Is(cause, errQuiet) {
		return SyncTimeout
	}
	return SyncNetwork
}

// timedContext derives a context from parent that is cancelled with errTimeout once d has passed on the clock.
// The returned function releases it and must be called. The context covers whatever runs under it, a body read
// included, so a server that sends headers and then stalls is cut off too.
func (c *Client) timedContext(parent context.Context, d time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	timer := c.clk.AfterFunc(d, func() { cancel(errTimeout) })
	return ctx, func() {
		timer.Stop()
		cancel(nil)
	}
}
