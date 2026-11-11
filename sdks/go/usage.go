package toggleslash

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// usageKey is what a count is summed by: a flag, a variation index and a UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	usageKey
	count int64
}

// recordUsage counts one served variation. Nothing is counted once Close has begun. It takes usageMu for one
// map add, which no request ever holds.
func (c *Client) recordUsage(flag string, index int) {
	if c.closing.Load() {
		return
	}
	key := usageKey{flag: flag, index: index, day: utcDay(c.cfg.clock.Now())}
	c.usageMu.Lock()
	c.pending[key]++
	c.usageMu.Unlock()
}

// runFlush is the flush goroutine: one flush per interval, the interval counted from the end of the last one.
func (c *Client) runFlush() {
	defer close(c.flushLoop.done)
	for sleep(c.ctx, c.cfg.clock, c.cfg.flushInterval) {
		c.flush(c.flushLoop)
	}
}

// flush sends what is pending. It reports its failures after releasing flushMu, so a callback that calls
// back into the client cannot deadlock a flush.
func (c *Client) flush(who *loop) {
	c.flushMu.Lock()
	errs := c.flushLocked()
	c.flushMu.Unlock()
	for _, err := range errs {
		c.reportError(who, err)
	}
}

// finishUsage is the last flush, on closing. It will not wait out a Retry-After, and it will not ignore one, so
// counts still held back then are lost, and that is reported.
func (c *Client) finishUsage() {
	c.flush(&c.closerLoop)
	c.usageMu.Lock()
	unsent := len(c.pending)
	holding := c.cfg.clock.Now().Before(c.usageHold)
	c.usageMu.Unlock()
	if unsent > 0 && holding {
		c.reportError(&c.closerLoop, &syncError{
			code: "held",
			what: "closed with " + strconv.Itoa(unsent) + " usage counts unsent: the server asked to wait",
		})
	}
}

// takePending removes and returns the pending counts in a fixed order, or nothing during a hold.
func (c *Client) takePending() []usageEntry {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if len(c.pending) == 0 || c.cfg.clock.Now().Before(c.usageHold) {
		return nil
	}
	entries := make([]usageEntry, 0, len(c.pending))
	for key, count := range c.pending {
		entries = append(entries, usageEntry{key, count})
	}
	clear(c.pending)
	slices.SortFunc(entries, func(a, b usageEntry) int {
		if a.day != b.day {
			return strings.Compare(a.day, b.day)
		}
		if a.flag != b.flag {
			return strings.Compare(a.flag, b.flag)
		}
		return a.index - b.index
	})
	return entries
}

// putBack merges counts a failed request did not deliver with whatever was counted meanwhile.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.pending[e.usageKey] += e.count
	}
	c.usageMu.Unlock()
}

// retryableStatus says whether a refused batch is worth sending again: the server is down or busy.
func retryableStatus(status int) bool {
	return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests
}

// flushLocked sends the pending counts, 500 entries a request, one request at a time. A batch that fails for a
// reason that can pass is kept with everything after it, and the flush stops; one the server refused for good
// is dropped, and the flush goes on.
func (c *Client) flushLocked() []error {
	const what = "usage request"
	entries := c.takePending()
	var errs []error
	for start := 0; start < len(entries); start += maxBatch {
		batch := entries[start:min(start+maxBatch, len(entries))]
		headers := []header{
			{"Authorization", "Bearer " + c.cfg.sdkKey},
			{"Content-Type", "application/json"},
		}
		// Usage requests are not tied to the client's lifetime: a flush that is under way when Close starts
		// finishes (within requestTimeout) instead of being cut off half delivered.
		r, e, _ := c.do(context.Background(), http.MethodPost, "/sdk/v1/events", what, headers, usageBody(batch), false)
		if e != nil {
			errs = append(errs, e)
			c.putBack(entries[start:])
			return errs
		}
		if r.status >= 200 && r.status < 300 {
			continue
		}
		if r.status == http.StatusTooManyRequests {
			if wait, ok := retryAfter(r.header); ok {
				until := c.cfg.clock.Now().Add(wait)
				c.usageMu.Lock()
				if until.After(c.usageHold) {
					c.usageHold = until
				}
				c.usageMu.Unlock()
			}
		}
		errs = append(errs, newSyncError(what, "http_"+strconv.Itoa(r.status)))
		if retryableStatus(r.status) {
			c.putBack(entries[start:])
			return errs
		}
	}
	return errs
}

// usageBody writes {"events":[{"flag","variationIndex","count","day"}]}: counts only, nothing about an end user.
func usageBody(batch []usageEntry) []byte {
	out := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"flag":`...)
		out = jsonv.AppendString(out, e.flag)
		out = append(out, `,"variationIndex":`...)
		out = strconv.AppendInt(out, int64(e.index), 10)
		out = append(out, `,"count":`...)
		out = strconv.AppendInt(out, e.count, 10)
		out = append(out, `,"day":"`...)
		out = append(out, e.day...)
		out = append(out, `"}`...)
	}
	return append(out, `]}`...)
}
