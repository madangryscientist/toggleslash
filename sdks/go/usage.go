package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const maxEntriesPerRequest = 500

var errFetchFailed = errors.New("fetch failed")

type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	key   usageKey
	count int64
}

// count adds one evaluation to the in-memory sums. Nothing is counted once Close has begun.
func (c *Client) count(flagKey string, index int) {
	day := c.cfg.now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.countClosed {
		return
	}
	c.counts[usageKey{flagKey, index, day}]++
}

func (c *Client) takeCounts() []usageEntry {
	c.usageMu.Lock()
	taken := c.counts
	c.counts = map[usageKey]int64{}
	c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(taken))
	for k, n := range taken {
		entries = append(entries, usageEntry{k, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].key, entries[j].key
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

// restore merges entries that were not sent back with the counts recorded since.
func (c *Client) restore(entries []usageEntry) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	for _, e := range entries {
		c.counts[e.key] += e.count
	}
}

func (c *Client) pendingEntries() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.counts)
}

func (c *Client) runFlush() {
	defer close(c.flushDone)
	defer func() {
		if r := recover(); r != nil {
			c.report(panicError("usage flush", r))
		}
	}()
	c.flushGid.Store(goroutineID())
	for c.root.Err() == nil {
		if !realSleep(c.root, c.cfg.flushInterval) {
			return
		}
		for _, err := range c.flush(false) {
			c.report(err)
		}
	}
}

type outcome int

const (
	sent outcome = iota
	kept
	dropped
)

// flush sends the pending counts, at most 500 entries to a request, one request after another. It
// returns the errors for onError; the caller reports them with no lock held. The final flush does not
// wait out a hold.
func (c *Client) flush(final bool) (errs []error) {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			errs = append(errs, panicError("usage flush", r))
		}
	}()
	if c.usageHold.remaining(c.cfg.now()) > 0 {
		if final {
			errs = append(errs, c.lost()...)
		}
		return errs
	}
	entries := c.takeCounts()
	for start := 0; start < len(entries); start += maxEntriesPerRequest {
		end := min(start+maxEntriesPerRequest, len(entries))
		result, err := c.postEvents(entries[start:end])
		if err != nil {
			errs = append(errs, err)
		}
		if result == kept {
			c.restore(entries[start:])
			if final && c.usageHold.remaining(c.cfg.now()) > 0 {
				errs = append(errs, c.lost()...)
			}
			return errs
		}
	}
	return errs
}

// lost drops what is pending at close because a hold is in force, and says so.
func (c *Client) lost() []error {
	n := len(c.takeCounts())
	if n == 0 {
		return nil
	}
	return []error{errors.New("toggleslash: closed with " + strconv.Itoa(n) + " usage counts unsent: the server asked to wait")}
}

func (c *Client) postEvents(entries []usageEntry) (outcome, error) {
	body := make([]byte, 0, 64*len(entries)+16)
	body = append(body, `{"events":[`...)
	for i, e := range entries {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, `{"flag":`...)
		body = jsonv.AppendString(body, e.key.flag)
		body = append(body, `,"variationIndex":`...)
		body = strconv.AppendInt(body, int64(e.key.index), 10)
		body = append(body, `,"count":`...)
		body = strconv.AppendInt(body, e.count, 10)
		body = append(body, `,"day":`...)
		body = jsonv.AppendString(body, e.key.day)
		body = append(body, '}')
	}
	body = append(body, `]}`...)

	// Usage requests do not stop when the client closes: Close waits for a flush running and sends the
	// final one, each request bounded by the request timeout.
	ctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/sdk/v1/events", bytes.NewReader(body))
	if err != nil {
		return kept, &failureError{op: "usage request", code: LastErrorNetwork}
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return kept, &failureError{op: "usage request", code: errorCode(ctx, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	status := resp.StatusCode
	if status >= 200 && status < 300 {
		return sent, nil
	}
	failed := &failureError{op: "usage request", code: "http_" + strconv.Itoa(status)}
	if status == http.StatusTooManyRequests {
		if d, ok := c.retryAfter(resp.Header); ok {
			c.usageHold.extend(c.cfg.now().Add(d))
		}
	}
	if status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return kept, failed
	}
	return dropped, failed
}
