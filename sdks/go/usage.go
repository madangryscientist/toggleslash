package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// usageEntry is one line of POST /sdk/v1/events: counts only, never a context or anything about an end user.
type usageEntry struct {
	flag           string
	variationIndex int
	day            string
	count          int64
}

type usageKey struct {
	flag           string
	variationIndex int
	day            string
}

// usage keeps how often each flag variation was served, per UTC day, in the order first seen.
type usage struct {
	mu      sync.Mutex
	stopped bool
	index   map[usageKey]*usageEntry
	order   []*usageEntry
}

// record counts one served evaluation. It does nothing once the client has begun to close.
func (u *usage) record(flag string, variationIndex int, day string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return
	}
	u.add(usageEntry{flag: flag, variationIndex: variationIndex, day: day, count: 1})
}

func (u *usage) add(e usageEntry) {
	if u.index == nil {
		u.index = map[usageKey]*usageEntry{}
	}
	k := usageKey{e.flag, e.variationIndex, e.day}
	if have, ok := u.index[k]; ok {
		have.count += e.count
		return
	}
	entry := e
	u.index[k] = &entry
	u.order = append(u.order, &entry)
}

// close stops counting.
func (u *usage) close() {
	u.mu.Lock()
	u.stopped = true
	u.mu.Unlock()
}

// take removes and returns everything pending.
func (u *usage) take() []usageEntry {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]usageEntry, len(u.order))
	for i, e := range u.order {
		out[i] = *e
	}
	u.index = nil
	u.order = nil
	return out
}

// putBack merges entries that were not sent into what is pending now.
func (u *usage) putBack(entries []usageEntry) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, e := range entries {
		u.add(e)
	}
}

func (u *usage) pending() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.order)
}

func utcDay(t time.Time) string { return t.UTC().Format("2006-01-02") }

// retryable reports whether a batch the server did not take is worth sending again.
func retryable(status int) bool { return status >= 500 || status == 408 || status == 429 }

// flushLoop sends the counts every flush interval until the client closes.
func (c *Client) flushLoop() {
	defer c.loops.done()
	for {
		if !sleep(c.ctx, c.clk, c.cfg.flushInterval) {
			return
		}
		for _, err := range c.flush(false) {
			c.reportFromLoop(err)
		}
	}
}

// flush sends what is pending, one request of at most maxBatch entries after another, and returns the failures
// to report: the caller reports them once no lock is held. Flushes are serial. final is the flush Close makes: it
// adds the "unsent" failure when counts are still held back by a Retry-After.
func (c *Client) flush(final bool) []error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	var errs []error
	if !c.usageHeld() {
		entries := c.use.take()
		for start := 0; start < len(entries); start += maxBatch {
			batch := entries[start:min(start+maxBatch, len(entries))]
			keep, err := c.post(batch)
			if err != nil {
				errs = append(errs, err)
			}
			if keep {
				c.use.putBack(entries[start:])
				break
			}
		}
	}
	if final {
		if n := c.use.pending(); n > 0 && c.usageHeld() {
			errs = append(errs, fmt.Errorf("closed with %d usage counts unsent: the server asked to wait", n))
		}
	}
	return errs
}

func (c *Client) usageHeld() bool {
	c.holds.mu.Lock()
	defer c.holds.mu.Unlock()
	return c.clk.Now().Before(c.holds.usage)
}

// post sends one batch. keep is true when the batch (and the rest of the flush) should be kept for later.
func (c *Client) post(batch []usageEntry) (keep bool, err error) {
	// Not tied to the client's context: a flush that was running when Close began is not abandoned.
	ctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/sdk/v1/events", bytes.NewReader(encodeBatch(batch)))
	if reqErr != nil {
		return true, &sdkError{LastErrorNetwork, "POST /sdk/v1/events failed: network error"}
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, doErr := c.hc.Do(req)
	if doErr != nil {
		return true, c.transportError(ctx, "POST /sdk/v1/events")
	}
	defer func() { _ = resp.Body.Close() }()
	// The status alone decides. Drain a little so the connection can be reused; a failure here changes nothing.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	status := resp.StatusCode
	if status >= 200 && status < 300 {
		return false, nil
	}
	if wait, ok := retryAfter(resp); ok {
		c.holds.mu.Lock()
		raise(&c.holds.usage, c.clk.Now().Add(wait))
		c.holds.mu.Unlock()
	}
	return retryable(status), &sdkError{httpError(status), "POST /sdk/v1/events answered " + strconv.Itoa(status)}
}

// transportError is the error for a request that did not get an answer: a timeout if the request's own deadline
// fired, otherwise a network failure. Its text is fixed, because the text of the underlying error carries the URL.
func (c *Client) transportError(ctx context.Context, what string) *sdkError {
	if errors.Is(context.Cause(ctx), errTimeout) {
		return &sdkError{LastErrorTimeout, what + " timed out"}
	}
	return &sdkError{LastErrorNetwork, what + " failed: network error"}
}

// encodeBatch writes {"events":[...]} with exactly the four fields of an entry.
func encodeBatch(batch []usageEntry) []byte {
	out := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"flag":`...)
		out = jsonvalue.AppendString(out, e.flag)
		out = append(out, `,"variationIndex":`...)
		out = strconv.AppendInt(out, int64(e.variationIndex), 10)
		out = append(out, `,"count":`...)
		out = strconv.AppendInt(out, e.count, 10)
		out = append(out, `,"day":`...)
		out = jsonvalue.AppendString(out, e.day)
		out = append(out, '}')
	}
	return append(out, "]}"...)
}

// retryAfter reads how long a 429 asks to wait: whole decimal seconds, surrounding spaces ignored, capped at
// retryAfterCap. Any other status, and a header that is missing or is not whole seconds, gives false.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return parseRetryAfter(resp.Header.Get("Retry-After"))
}

func parseRetryAfter(header string) (time.Duration, bool) {
	start, end := 0, len(header)
	for start < end && (header[start] == ' ' || header[start] == '\t') {
		start++
	}
	for end > start && (header[end-1] == ' ' || header[end-1] == '\t') {
		end--
	}
	digits := header[start:end]
	if digits == "" {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || seconds > uint64(retryAfterCap/time.Second) {
		return retryAfterCap, true
	}
	return time.Duration(seconds) * time.Second, true
}
