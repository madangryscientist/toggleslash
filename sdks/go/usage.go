package toggleslash

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// maxBatch is the most entries one POST /sdk/v1/events carries: the server takes 1000 events and 128 KiB, and
// an entry is at most about 140 bytes, so 500 stay under both.
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

// record counts one served result. It is a map increment under a small lock, so it is safe on a request path.
func (c *Client) record(flagKey string, index int) {
	key := usageKey{flag: flagKey, index: index, day: c.cfg.now().UTC().Format("2006-01-02")}
	c.usageMu.Lock()
	if c.recording {
		c.pending[key]++
	}
	c.usageMu.Unlock()
}

// holding reports whether a 429 from the events route asked for nothing to be sent yet. flushMu is held.
func (c *Client) holding() bool { return c.cfg.now().Before(c.eventsHoldUntil) }

// runFlush is the flush goroutine: send what is pending every flush interval until Close.
func (c *Client) runFlush() {
	defer close(c.flushWorker.done)
	timer := time.NewTimer(c.cfg.flushInterval)
	defer timer.Stop()
	for {
		select {
		case <-c.flushCtx.Done():
			return
		case <-timer.C:
		}
		for _, err := range c.flush() {
			c.report(err, c.flushWorker)
		}
		timer.Reset(c.cfg.flushInterval)
	}
}

// finalFlush is the last flush, on Close. It will not wait out a Retry-After, and it will not ignore one, so
// counts still held back then are lost, and that is reported.
func (c *Client) finalFlush() []*Error {
	errs := c.flush()
	c.flushMu.Lock()
	held := c.holding()
	c.flushMu.Unlock()
	c.usageMu.Lock()
	unsent := len(c.pending)
	c.usageMu.Unlock()
	if unsent > 0 && held {
		errs = append(errs, &Error{Op: "close", Code: "usage_unsent", Message: "closed with " + strconv.Itoa(unsent) + " usage counts unsent: the server asked to wait"})
	}
	return errs
}

// take swaps the pending counts out, in a fixed order.
func (c *Client) take() []usageEntry {
	c.usageMu.Lock()
	taken := c.pending
	c.pending = map[usageKey]int64{}
	c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(taken))
	for key, count := range taken {
		entries = append(entries, usageEntry{key, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].usageKey, entries[j].usageKey
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

// putBack merges counts a failed send did not deliver back into the pending ones.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.pending[e.usageKey] += e.count
	}
	c.usageMu.Unlock()
}

// flush sends what is pending and returns the errors to tell OnError. Flushes are serial. A batch that
// fails for a reason that can pass (the server is down or busy) is put back and stops the flush; one the
// server refused is dropped, since it would be refused again.
func (c *Client) flush() []*Error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.holding() {
		return nil
	}
	entries := c.take()
	var errs []*Error
	for start := 0; start < len(entries); start += maxBatch {
		batch := entries[start:min(start+maxBatch, len(entries))]
		keep, err := c.post(batch)
		if err != nil {
			errs = append(errs, err)
		}
		if keep {
			c.putBack(entries[start:])
			return errs
		}
	}
	return errs
}

func marshalBatch(batch []usageEntry) []byte {
	body := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, `{"flag":`...)
		body = jsonvalue.String(e.flag).AppendJSON(body)
		body = append(body, `,"variationIndex":`...)
		body = strconv.AppendInt(body, int64(e.index), 10)
		body = append(body, `,"count":`...)
		body = strconv.AppendInt(body, e.count, 10)
		body = append(body, `,"day":"`...)
		body = append(body, e.day...)
		body = append(body, `"}`...)
	}
	return append(body, `]}`...)
}

// post sends one batch. keep is true when the batch should be tried again later.
func (c *Client) post(batch []usageEntry) (keep bool, err *Error) {
	const op = "POST /sdk/v1/events"
	// The request is not tied to Close: one already in flight is left to finish, bounded by the timeout.
	ctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.url+"/sdk/v1/events", bytes.NewReader(marshalBatch(batch)))
	if reqErr != nil {
		return true, &Error{Op: "usage", Code: string(SyncNetwork), Message: op + " failed"}
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.sdkKey)
	req.Header.Set("Content-Type", "application/json")
	resp, doErr := c.cfg.httpClient.Do(req)
	if doErr != nil {
		f := failureOf(op, ctx, doErr)
		return true, &Error{Op: "usage", Code: string(f.code), Message: f.text}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	status := resp.StatusCode
	if status >= 200 && status < 300 {
		return false, nil
	}
	if wait, ok := retryAfter(resp); ok {
		if until := c.cfg.now().Add(wait); until.After(c.eventsHoldUntil) {
			c.eventsHoldUntil = until
		}
	}
	f := httpFailure(op, status)
	return status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests,
		&Error{Op: "usage", Code: string(f.code), Message: f.text}
}
