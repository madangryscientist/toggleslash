package toggleslash

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// maxBatch is the most entries one POST /sdk/v1/events carries. The server takes 1000 events and 128 KiB; an
// entry is about 140 bytes at most, so 500 stay under both.
const maxBatch = 500

// usageKey is what a count is kept per: the flag, the variation served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

// count adds one served evaluation. It is a map increment under a lock nothing slow holds, so it is safe on a
// request path. After Close it counts nothing.
func (c *Client) count(flag string, index int) {
	k := usageKey{flag, index, utcDay(c.cfg.clock.Now())}
	c.usageMu.Lock()
	if !c.closed {
		c.counts[k]++
	}
	c.usageMu.Unlock()
}

// takeCounts swaps the pending counts out, in a stable order.
func (c *Client) takeCounts() []usageEntry {
	c.usageMu.Lock()
	taken := c.counts
	c.counts = map[usageKey]int64{}
	c.usageMu.Unlock()
	out := make([]usageEntry, 0, len(taken))
	for k, n := range taken {
		out = append(out, usageEntry{k, n})
	}
	slices.SortFunc(out, func(a, b usageEntry) int {
		if a.day != b.day {
			return cmpString(a.day, b.day)
		}
		if a.flag != b.flag {
			return cmpString(a.flag, b.flag)
		}
		return a.index - b.index
	})
	return out
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

type usageEntry struct {
	usageKey
	n int64
}

// putBack merges counts that were not sent into the pending ones. It ignores the closed flag: they were
// counted before it was set.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.counts[e.usageKey] += e.n
	}
	c.usageMu.Unlock()
}

// holdingUsage reports whether the server's 429 asked for no usage request yet. The caller holds flushMu.
func (c *Client) holdingUsage() bool { return c.cfg.clock.Now().Before(c.usageHold) }

// unsentAtClose is the number of pending entries when the last flush stopped because the server asked to
// wait, and zero otherwise.
func (c *Client) unsentAtClose() int {
	c.flushMu.Lock()
	holding := c.holdingUsage()
	c.flushMu.Unlock()
	if !holding {
		return 0
	}
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.counts)
}

// flush sends what is pending, one batch after another, and returns the failures for the caller to report
// once no lock is held. A batch the server did not take for a reason that can pass (it is down or busy) is
// put back and the flush stops; one the server refused is dropped, since it would be refused again. parent is
// the context the requests live in: Close cancels it for a flush in progress, and the final flush gets a
// context of its own.
func (c *Client) flush(parent context.Context, final bool) []error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.holdingUsage() {
		return nil
	}
	if !final && parent.Err() != nil {
		return nil
	}
	var errs []error
	entries := c.takeCounts()
	for start := 0; start < len(entries); start += maxBatch {
		batch := entries[start:min(start+maxBatch, len(entries))]
		keep, err := c.post(parent, batch)
		if err != nil {
			errs = append(errs, err)
		}
		if keep {
			c.putBack(entries[start:])
			break
		}
	}
	return errs
}

// post sends one batch. keep is true when the batch and everything after it should wait for another try.
func (c *Client) post(parent context.Context, batch []usageEntry) (keep bool, err error) {
	const what = "POST /sdk/v1/events"
	ctx, cancel := context.WithTimeoutCause(parent, c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, rerr := c.newRequest(ctx, http.MethodPost, "/sdk/v1/events", bytes.NewReader(encodeBatch(batch)))
	if rerr != nil {
		return true, &syncError{LastErrorNetwork, what + " failed: network error"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, derr := c.http.Do(req)
	if derr != nil {
		return true, usageFailure(ctx, what, derr)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	status := resp.StatusCode
	if status >= 200 && status < 300 {
		return false, nil
	}
	c.hold(&c.usageHold, resp.Header, status)
	return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests,
		httpError(what, status)
}

// usageFailure is the error of a request that did not get an answer; nil when Close cut it short.
func usageFailure(ctx context.Context, what string, err error) error {
	if e := classify(ctx, what, err); e != nil {
		return e
	}
	return nil
}

// encodeBatch writes {"events":[{"flag":...,"variationIndex":...,"count":...,"day":...}]}.
func encodeBatch(batch []usageEntry) []byte {
	b := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"flag":`...)
		b = jsonvalue.AppendString(b, e.flag)
		b = append(b, `,"variationIndex":`...)
		b = strconv.AppendInt(b, int64(e.index), 10)
		b = append(b, `,"count":`...)
		b = strconv.AppendInt(b, e.n, 10)
		b = append(b, `,"day":"`...)
		b = append(b, e.day...)
		b = append(b, `"}`...)
	}
	return append(b, ']', '}')
}

// flushLoop sends the pending counts one interval after the last send finished, until Close.
func (c *Client) flushLoop() {
	defer close(c.flushTask.done)
	for {
		t := c.cfg.clock.NewTimer(c.cfg.flushInterval)
		select {
		case <-t.C():
		case <-c.root.Done():
			t.Stop()
			return
		}
		c.report(c.flushTask, c.flush(c.root, false)...)
	}
}
