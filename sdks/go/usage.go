package toggleslash

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const (
	eventsRoute = "POST /sdk/v1/events"
	// maxEntries is the most entries one events request carries; the server takes 1000.
	maxEntries = 500
)

// usageKey is one counter: a flag, the variation served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	usageKey
	count int64
}

// record counts one served evaluation, in memory. Nothing is counted once Close has begun.
func (c *Client) record(flagKey string, index int) {
	if c.closing.Load() {
		return
	}
	key := usageKey{flag: flagKey, index: index, day: c.cfg.clock.Now().UTC().Format("2006-01-02")}
	c.usageMu.Lock()
	c.pending[key]++
	c.usageMu.Unlock()
}

// runFlush is the flush goroutine: one flush per interval, the first one interval after NewClient and each
// next one interval after the last flush ended.
func (c *Client) runFlush(ctx context.Context) {
	defer close(c.flushDone)
	defer func() {
		if r := recover(); r != nil {
			c.reportFromLoop(&PanicError{Where: "the flush loop", Value: r})
		}
	}()
	for c.wait(ctx, c.cfg.flushInterval) {
		for _, err := range c.flushUsage(ctx, false) {
			c.reportFromLoop(err)
		}
	}
}

// finalFlush is Close's flush: it does not wait out a hold, and it reports on the caller's goroutine.
func (c *Client) finalFlush() {
	c.inCallback.Add(1)
	defer c.inCallback.Add(-1)
	for _, err := range c.flushUsage(context.Background(), true) {
		c.report(err)
	}
}

// flushUsage sends what is pending, 500 entries a request, and returns the errors to report. Flushes are
// serial. It holds no lock while it reports, since the caller reports what it returns.
func (c *Client) flushUsage(ctx context.Context, final bool) []error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()

	entries := c.takePending()
	if len(entries) == 0 {
		return nil
	}
	if c.holdLeft(&c.usageHold) > 0 {
		c.restore(entries)
		if final {
			return []error{fmt.Errorf("closed with %d usage counts unsent: the server asked to wait", len(entries))}
		}
		return nil
	}

	var errs []error
	for start := 0; start < len(entries); start += maxEntries {
		batch := entries[start:min(start+maxEntries, len(entries))]
		keep, err := c.postBatch(ctx, batch)
		if err != nil && ctx.Err() == nil {
			errs = append(errs, err)
		}
		if keep {
			c.restore(entries[start:])
			break
		}
	}
	return errs
}

// takePending swaps the counters out, so recording goes on while a request is out. The entries are in a
// fixed order: day, flag, variation.
func (c *Client) takePending() []usageEntry {
	c.usageMu.Lock()
	taken := c.pending
	c.pending = make(map[usageKey]int64)
	c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(taken))
	for key, count := range taken {
		entries = append(entries, usageEntry{usageKey: key, count: count})
	}
	slices.SortFunc(entries, func(a, b usageEntry) int {
		return strings.Compare(a.day+"\x00"+a.flag+"\x00"+strconv.Itoa(a.index),
			b.day+"\x00"+b.flag+"\x00"+strconv.Itoa(b.index))
	})
	return entries
}

// restore merges entries back into the counters, with what was recorded meanwhile. Nothing is restored
// once Close has begun, except by the final flush's own decision: its entries are lost.
func (c *Client) restore(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.pending[e.usageKey] += e.count
	}
	c.usageMu.Unlock()
}

// postBatch sends one events request. keep reports that the counts stay for the next flush and the flush
// stops; false means they were sent or dropped.
func (c *Client) postBatch(ctx context.Context, batch []usageEntry) (keep bool, err error) {
	rctx, cancel := context.WithTimeoutCause(ctx, c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	body := strings.NewReader(string(eventsBody(batch)))
	req, reqErr := c.newRequest(rctx, http.MethodPost, "/sdk/v1/events", body)
	if reqErr != nil {
		return false, failureOf(rctx, eventsRoute)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, doErr := c.httpc.Do(req)
	if doErr != nil {
		return true, failureOf(rctx, eventsRoute)
	}
	defer func() { _ = resp.Body.Close() }()
	// A small read lets the connection be reused; the answer's body is not used.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		return false, nil
	case status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
		c.holdFrom(&c.usageHold, resp)
		return true, statusError(eventsRoute, status)
	default:
		return false, statusError(eventsRoute, status)
	}
}

// eventsBody is {"events":[{"flag":..,"variationIndex":..,"count":..,"day":..}]}: exactly those four
// fields.
func eventsBody(batch []usageEntry) []byte {
	b := []byte(`{"events":[`)
	for i, e := range batch {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"flag":`...)
		b = jsonv.AppendString(b, e.flag)
		b = append(b, `,"variationIndex":`...)
		b = strconv.AppendInt(b, int64(e.index), 10)
		b = append(b, `,"count":`...)
		b = strconv.AppendInt(b, e.count, 10)
		b = append(b, `,"day":`...)
		b = jsonv.AppendString(b, e.day)
		b = append(b, '}')
	}
	return append(b, `]}`...)
}
