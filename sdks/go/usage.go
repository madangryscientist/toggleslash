package toggleslash

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const maxUsageEntries = 500

// usageKey is what counts are summed by: the flag, the variation index and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

// countUsage counts one evaluation that served a value from the ruleset. Nothing is counted once Close has
// begun; the check is under the lock the final flush takes, so a count is either flushed or never taken.
func (c *Client) countUsage(flag string, index int) {
	day := c.clock().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.closing.Load() {
		return
	}
	c.counts[usageKey{flag, index, day}]++
}

func (c *Client) flushLoop() {
	defer close(c.flushDone)
	t := time.NewTimer(c.cfg.flushInterval)
	defer t.Stop()
	for {
		select {
		case <-c.flushStop:
			return
		case <-t.C:
		}
		c.flush(&c.flushCB, false)
		t.Reset(c.cfg.flushInterval)
	}
}

// flushFinal is the flush Close sends, on the goroutine that called Close.
func (c *Client) flushFinal() { c.flush(&c.closerCB, true) }

// flush sends the pending counts. Flushes are serial. The errors are reported after the lock is released.
func (c *Client) flush(mark *atomic.Int32, final bool) {
	defer func() {
		if v := recover(); v != nil {
			c.notify(&PanicError{Where: "usage flush", Value: v}, mark)
		}
	}()
	c.flushMu.Lock()
	errs := c.flushLocked(final)
	c.flushMu.Unlock()
	for _, err := range errs {
		c.notify(err, mark)
	}
}

func (c *Client) takeCounts() []usageEntry {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if len(c.counts) == 0 {
		return nil
	}
	out := make([]usageEntry, 0, len(c.counts))
	for k, n := range c.counts {
		out = append(out, usageEntry{k, n})
	}
	c.counts = map[usageKey]int64{}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].usageKey, out[j].usageKey
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

type usageEntry struct {
	usageKey
	count int64
}

// putBack merges counts that were not sent into the pending ones.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	for _, e := range entries {
		c.counts[e.usageKey] += e.count
	}
}

func (c *Client) flushLocked(final bool) []error {
	if left := c.holdLeft(&c.holdUsage); left > 0 {
		if !final {
			return nil
		}
		c.usageMu.Lock()
		pending := len(c.counts)
		c.usageMu.Unlock()
		if pending == 0 {
			return nil
		}
		return []error{&LostCountsError{Message: fmt.Sprintf("closed with %d usage counts unsent: the server asked to wait", pending)}}
	}
	entries := c.takeCounts()
	var errs []error
	for start := 0; start < len(entries); start += maxUsageEntries {
		end := min(start+maxUsageEntries, len(entries))
		keep, err := c.sendBatch(entries[start:end])
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

// sendBatch sends one request. keep is true when the counts must be kept for the next flush and this flush
// stops; otherwise they were sent or are dropped for good and the flush goes on.
func (c *Client) sendBatch(batch []usageEntry) (keep bool, err error) {
	body := make([]byte, 0, 64+len(batch)*80)
	body = append(body, `{"events":[`...)
	for i, e := range batch {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, `{"flag":`...)
		body = jsonvalue.AppendQuoted(body, e.flag)
		body = append(body, `,"variationIndex":`...)
		body = strconv.AppendInt(body, int64(e.index), 10)
		body = append(body, `,"count":`...)
		body = strconv.AppendInt(body, e.count, 10)
		body = append(body, `,"day":"`...)
		body = append(body, e.day...)
		body = append(body, `"}`...)
	}
	body = append(body, `]}`...)

	res, err := c.request(c.usageCtx, "events", http.MethodPost, "/sdk/v1/events", body, nil)
	if err != nil {
		if errors.Is(err, errClosed) {
			return true, nil
		}
		return true, err
	}
	switch {
	case res.status >= 200 && res.status < 300:
		return false, nil
	case res.status == 429:
		if d, ok := c.parseRetryAfter(res.retryAfter); ok {
			c.hold(&c.holdUsage, d)
		}
		return true, &SyncError{Route: "events", Detail: httpDetail(res.status)}
	case res.status >= 500 || res.status == 408:
		return true, &SyncError{Route: "events", Detail: httpDetail(res.status)}
	}
	return false, &SyncError{Route: "events", Detail: httpDetail(res.status)}
}
