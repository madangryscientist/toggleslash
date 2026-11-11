package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// usageKey is what a count is summed by: a flag, the variation it served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	key   usageKey
	count int64
}

const eventsRoute = "POST /sdk/v1/events"

// record counts one evaluation that served a variation of a flag the ruleset holds.
func (c *Client) record(flag string, index int) {
	day := c.now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	c.counts[usageKey{flag, index, day}]++
	c.usageMu.Unlock()
}

// takePending swaps the counts out, in a fixed order so a batch is the same on every run.
func (c *Client) takePending() []usageEntry {
	c.usageMu.Lock()
	taken := c.counts
	c.counts = make(map[usageKey]int64)
	c.usageMu.Unlock()
	out := make([]usageEntry, 0, len(taken))
	for k, n := range taken {
		out = append(out, usageEntry{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].key, out[j].key
		if a.day != b.day {
			return a.day < b.day
		}
		if a.flag != b.flag {
			return a.flag < b.flag
		}
		return a.index < b.index
	})
	return out
}

// putBack returns counts that were not sent, adding to anything counted since.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.counts[e.key] += e.count
	}
	c.usageMu.Unlock()
}

func (c *Client) pendingCount() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.counts)
}

func (c *Client) eventsHold() time.Time {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.holdEvents
}

// flushLoop is the flush goroutine: a flush every flush interval, the first one an interval after New.
func (c *Client) flushLoop() {
	defer c.wg.Done()
	defer c.own()()
	ticker := time.NewTicker(c.opt.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.loopCtx.Done():
			return
		case <-ticker.C:
			c.flush(c.loopCtx)
		}
	}
}

// Flush sends the usage counts held now instead of waiting for the next flush. It sends nothing while
// the server has asked the SDK to wait. Counts that cannot be sent are kept for the next flush.
func (c *Client) Flush(ctx context.Context) {
	c.flush(ctx)
}

// flush sends the pending counts in batches. Flushes are serial. A request that fails in a way worth
// retrying stops the flush and keeps its batch and every batch not yet tried.
func (c *Client) flush(ctx context.Context) {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if ctx.Err() != nil || time.Now().Before(c.eventsHold()) {
		return
	}
	pending := c.takePending()
	for start := 0; start < len(pending); start += maxEventsPerRequest {
		end := min(start+maxEventsPerRequest, len(pending))
		if !c.post(ctx, pending[start:end]) {
			c.putBack(pending[start:])
			return
		}
	}
}

// finish is the last flush at close. It does not wait out a hold; counts a hold keeps back are lost,
// and onError is told how many.
func (c *Client) finish() {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if time.Now().Before(c.eventsHold()) {
		if n := c.pendingCount(); n > 0 {
			c.reportError(errors.New("closed with " + strconv.Itoa(n) + " usage counts unsent: the server asked to wait"))
		}
		return
	}
	pending := c.takePending()
	for start := 0; start < len(pending); start += maxEventsPerRequest {
		end := min(start+maxEventsPerRequest, len(pending))
		if !c.post(context.Background(), pending[start:end]) {
			return
		}
	}
}

func eventsBody(entries []usageEntry) []byte {
	b := []byte(`{"events":[`)
	for i, e := range entries {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"flag":`...)
		b = jsonv.AppendString(b, e.key.flag)
		b = append(b, `,"variationIndex":`...)
		b = strconv.AppendInt(b, int64(e.key.index), 10)
		b = append(b, `,"count":`...)
		b = strconv.AppendInt(b, e.count, 10)
		b = append(b, `,"day":`...)
		b = jsonv.AppendString(b, e.key.day)
		b = append(b, '}')
	}
	return append(b, `]}`...)
}

// post sends one batch. It reports whether the batch is done with: sent, or refused in a way a retry
// would not change. False means keep it for the next flush.
func (c *Client) post(ctx context.Context, entries []usageEntry) bool {
	rctx, cancel := context.WithTimeoutCause(ctx, c.opt.requestTimeout, errRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, c.base+"/sdk/v1/events", bytes.NewReader(eventsBody(entries)))
	if err != nil {
		c.reportError(errors.New(eventsRoute + " failed: network error"))
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.eventsFailure(ctx, rctx)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// The counts were received. Read the answer to let the connection be reused, under the same
		// deadline; a body that fails or never ends changes nothing about what was sent.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return true
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		c.holdFor(resp.Header, true)
	}
	c.reportError(errors.New(eventsRoute + " answered " + strconv.Itoa(resp.StatusCode)))
	retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests
	return !retryable
}

func (c *Client) eventsFailure(parent, req context.Context) {
	if parent.Err() != nil {
		return
	}
	if cause := context.Cause(req); errors.Is(cause, errRequestTimeout) {
		c.reportError(errors.New(eventsRoute + " timed out"))
		return
	}
	c.reportError(errors.New(eventsRoute + " failed: network error"))
}
