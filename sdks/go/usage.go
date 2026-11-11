package toggleslash

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// maxEventsPerRequest is how many entries one usage request carries (the server takes 1000).
const maxEventsPerRequest = 500

// usageKey is one counter: a flag, a variation index and a UTC day (yyyy-MM-dd).
type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	usageKey
	count int64
}

// count adds one to the (flag, variation index, UTC day) counter. Nothing is counted once Close began.
func (c *Client) count(flag string, index int) {
	day := c.cfg.clock.Now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.closed {
		return
	}
	c.pending[usageKey{flag: flag, index: index, day: day}]++
}

// takePending swaps the counters out, as a sorted list.
func (c *Client) takePending() []usageEntry {
	c.usageMu.Lock()
	taken := c.pending
	c.pending = map[usageKey]int64{}
	c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(taken))
	for k, n := range taken {
		entries = append(entries, usageEntry{usageKey: k, count: n})
	}
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

// restore merges counts that were not sent back into the pending ones.
func (c *Client) restore(entries []usageEntry) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	for _, e := range entries {
		c.pending[e.usageKey] += e.count
	}
}

func (c *Client) pendingEntries() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.pending)
}

type usageOutcome int

const (
	usageSent usageOutcome = iota
	usageKept
	usageDropped
)

// flushLoop is the flush goroutine: one flush per interval, the first one interval after New.
func (c *Client) flushLoop(ctx context.Context) {
	defer close(c.flushDone)
	for {
		t := c.cfg.clock.NewTimer(c.cfg.flushInterval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C():
		}
		if ctx.Err() != nil {
			return
		}
		if errs := c.safeFlush(false); len(errs) > 0 {
			c.reportFrom(&c.flushCB, errs...)
		}
	}
}

func (c *Client) safeFlush(final bool) (errs []error) {
	defer func() {
		if r := recover(); r != nil {
			errs = append(errs, &panicError{what: "the usage flush", value: r})
		}
	}()
	return c.flush(final)
}

// flush sends the pending counts (contract section 9) and returns the failures for the caller to report
// once no lock is held. Flushes are serial. The final flush does not wait out a Retry-After hold: it
// reports what it could not send.
func (c *Client) flush(final bool) []error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.cfg.clock.Now().Before(c.usageHold) {
		if final {
			if n := c.pendingEntries(); n > 0 {
				return []error{fmt.Errorf("toggleslash: closed with %d usage counts unsent: the server asked to wait", n)}
			}
		}
		return nil
	}
	entries := c.takePending()
	var errs []error
	for start := 0; start < len(entries); start += maxEventsPerRequest {
		chunk := entries[start:min(start+maxEventsPerRequest, len(entries))]
		outcome, err := c.postUsage(chunk)
		if err != nil {
			errs = append(errs, err)
		}
		if outcome == usageKept {
			c.restore(entries[start:])
			break
		}
	}
	return errs
}

func usageBody(entries []usageEntry) []byte {
	items := make([]jsonvalue.Value, 0, len(entries))
	for _, e := range entries {
		item := jsonvalue.NewObject()
		item.Set("flag", jsonvalue.String(wtf8.FromGo(e.flag)))
		item.Set("variationIndex", jsonvalue.Number(float64(e.index)))
		item.Set("count", jsonvalue.Number(float64(e.count)))
		item.Set("day", jsonvalue.String(e.day))
		items = append(items, jsonvalue.ObjectValue(item))
	}
	root := jsonvalue.NewObject()
	root.Set("events", jsonvalue.Array(items))
	return jsonvalue.Marshal(jsonvalue.ObjectValue(root))
}

// postUsage sends one request. A request the server cannot take again (5xx, 408, 429, a timeout or a
// network failure) is kept; any other refusal is dropped.
func (c *Client) postUsage(entries []usageEntry) (usageOutcome, error) {
	// Not the lifetime context: a flush that is running when Close begins finishes, and Close waits for it.
	reqCtx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := c.newRequest(reqCtx, http.MethodPost, "/sdk/v1/events", bytes.NewReader(usageBody(entries)))
	if err != nil {
		return usageKept, &SyncError{Code: SyncNetwork, Usage: true}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return usageKept, &SyncError{Code: failureCode(reqCtx), Usage: true}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		return usageSent, nil
	case status == http.StatusTooManyRequests:
		if d, ok := parseRetryAfter(resp.Header); ok {
			c.usageHold = c.cfg.clock.Now().Add(d)
		}
		return usageKept, &SyncError{Code: httpErrorCode(status), Usage: true}
	case status >= 500 || status == http.StatusRequestTimeout:
		return usageKept, &SyncError{Code: httpErrorCode(status), Usage: true}
	}
	return usageDropped, &SyncError{Code: httpErrorCode(status), Usage: true}
}
