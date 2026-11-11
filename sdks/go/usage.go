package toggleslash

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// maxBatch: the server accepts 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so 500 stay
// under both.
const maxBatch = 500

// usageKey is what counts are summed by: a flag, the variation served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	usageKey
	count int64
}

// record counts one served variation. It is a map increment under a lock nothing slow holds, so it is safe on a
// request path. After close nothing is counted.
func (c *Client) record(flag string, index int) {
	day := c.clk.Now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.closing.Load() {
		return
	}
	c.pending[usageKey{flag: flag, index: index, day: day}]++
}

// takePending empties the pending counts and returns them in a stable order.
func (c *Client) takePending() []usageEntry {
	c.usageMu.Lock()
	entries := make([]usageEntry, 0, len(c.pending))
	for key, count := range c.pending {
		entries = append(entries, usageEntry{usageKey: key, count: count})
	}
	c.pending = make(map[usageKey]int64)
	c.usageMu.Unlock()
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.day != b.day {
			return a.day < b.day
		}
		if a.flag != b.flag {
			return a.flag < b.flag
		}
		return a.index < b.index
	})
	return entries
}

// putBack returns counts a failed send did not deliver, merged with any counted meanwhile.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.pending[e.usageKey] += e.count
	}
	c.usageMu.Unlock()
}

func (c *Client) pendingCount() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.pending)
}

// flushLoop sends the pending counts every flushInterval until close.
func (c *Client) flushLoop() {
	defer c.wg.Done()
	defer c.sdkGoroutines.add()()
	for {
		timer := c.clk.NewTimer(c.cfg.flushInterval)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			c.flush()
		}
	}
}

// flush sends what is pending, serially with every other flush. A batch that fails for a reason that can pass (the
// server is down or busy) is put back with the rest, and the flush stops; one the server refused is dropped, since
// it would be refused again. After a 429 with a Retry-After, nothing is sent until that has passed.
func (c *Client) flush() {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.usageHold.remaining(c.clk.Now()) > 0 {
		return
	}
	entries := c.takePending()
	for start := 0; start < len(entries); start += maxBatch {
		end := start + maxBatch
		if end > len(entries) {
			end = len(entries)
		}
		if !c.post(entries[start:end]) {
			c.putBack(entries[start:])
			return
		}
	}
}

// finish is the last flush, on closing. It will not wait out a Retry-After and will not ignore one, so counts still
// held back then are lost, and that is reported.
func (c *Client) finish() {
	c.flush()
	if n := c.pendingCount(); n > 0 && c.usageHold.remaining(c.clk.Now()) > 0 {
		c.report(&Error{Op: "close", Code: "unsent", Count: n})
	}
}

func encodeBatch(batch []usageEntry) []byte {
	body := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, `{"flag":`...)
		body = jsonvalue.FromString(e.flag).AppendJSON(body)
		body = append(body, `,"variationIndex":`...)
		body = strconv.AppendInt(body, int64(e.index), 10)
		body = append(body, `,"count":`...)
		body = strconv.AppendInt(body, e.count, 10)
		body = append(body, `,"day":"`...)
		body = append(body, e.day...)
		body = append(body, `"}`...)
	}
	return append(body, "]}"...)
}

// retryable says what to do with a batch the server did not take: try it again later, or give up on it.
func retryable(status int) bool {
	return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests
}

// post sends one batch. It is false when the batch should be kept for another try. The request is bounded by
// requestTimeout and is not cancelled by close: the final flush is part of closing.
func (c *Client) post(batch []usageEntry) bool {
	ctx, release := c.timedContext(context.Background(), c.cfg.requestTimeout)
	defer release()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/sdk/v1/events", bytes.NewReader(encodeBatch(batch)))
	if err != nil {
		c.report(&Error{Op: "usage", Code: syncCode(ctx, err)})
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.report(&Error{Op: "usage", Code: syncCode(ctx, err)})
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}
	if wait, ok := retryAfter(resp); ok {
		c.usageHold.extend(c.clk.Now().Add(wait))
	}
	c.report(&Error{Op: "usage", Code: (&statusError{status: resp.StatusCode}).Error()})
	return !retryable(resp.StatusCode)
}
