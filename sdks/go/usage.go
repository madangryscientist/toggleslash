package toggleslash

import (
	"fmt"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
)

// maxBatch is the most entries one POST /sdk/v1/events carries.
const maxBatch = 500

// usageKey is one counter: a flag, the variation served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

// record counts one successful evaluation. It is a map increment under a
// short lock and does no I/O, so it is safe on a request path. After Close it
// counts nothing.
func (c *Client) record(flag string, index int) {
	if c.closing.Load() {
		return
	}
	day := c.clock.Now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	c.counts[usageKey{flag, index, day}]++
	c.usageMu.Unlock()
}

// flushLoop sends the pending counts one interval after the start, and then one
// interval after each flush finished.
func (c *Client) flushLoop() {
	defer c.loops.Done()
	defer c.own.enter()()
	for c.clock.Sleep(c.ctx, c.flushIv) {
		safely(func() { c.flush() })
	}
}

// Flush sends the pending usage counts now and returns when that is done. The
// SDK does this on its own every flush interval; this is for a program that is
// about to exit, or a test. After Close it does nothing.
func (c *Client) Flush() {
	if c.closing.Load() {
		return
	}
	safely(func() { c.flush() })
}

// finalFlush is the last flush, on Close. It does not wait out a Retry-After
// and does not ignore one either, so counts held back by it are lost, and that
// is reported.
func (c *Client) finalFlush() {
	c.flush()
	c.usageMu.Lock()
	pending := len(c.counts)
	holding := c.clock.Now().Before(c.usageHold)
	c.usageMu.Unlock()
	if pending > 0 && holding {
		c.reportError(fmt.Errorf("toggleslash: closed with %d usage counts unsent: the server asked to wait", pending))
	}
}

// usageEntry is one line of the request body.
type usageEntry struct {
	key   usageKey
	count int64
}

// flush sends what is pending, in batches, one at a time. Callbacks run after
// the flush lock is released.
func (c *Client) flush() {
	var errs []error
	func() {
		c.flushMu.Lock()
		defer c.flushMu.Unlock()
		errs = c.flushLocked()
	}()
	for _, err := range errs {
		c.reportError(err)
	}
}

func (c *Client) flushLocked() (errs []error) {
	c.usageMu.Lock()
	if c.clock.Now().Before(c.usageHold) || len(c.counts) == 0 {
		c.usageMu.Unlock()
		return nil
	}
	entries := make([]usageEntry, 0, len(c.counts))
	for k, n := range c.counts {
		entries = append(entries, usageEntry{k, n})
	}
	c.counts = map[usageKey]int64{}
	c.usageMu.Unlock()

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

	for start := 0; start < len(entries); start += maxBatch {
		batch := entries[start:min(start+maxBatch, len(entries))]
		keep, err := c.postUsage(batch)
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

// putBack returns entries that were not sent to the counters.
func (c *Client) putBack(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.counts[e.key] += e.count
	}
	c.usageMu.Unlock()
}

// postUsage sends one batch. keep is true when the batch (and the ones after
// it) should stay for the next flush: the server is down or busy, or the
// request failed. A batch the server refused for any other reason is dropped,
// since it would be refused again.
func (c *Client) postUsage(batch []usageEntry) (keep bool, err error) {
	list := make([]wjson.Value, 0, len(batch))
	for _, e := range batch {
		o := wjson.NewObject()
		o.Set("flag", wjson.String(e.key.flag))
		o.Set("variationIndex", wjson.Number(float64(e.key.index)))
		o.Set("count", wjson.Number(float64(e.count)))
		o.Set("day", wjson.String(e.key.day))
		list = append(list, wjson.ObjectValue(o))
	}
	body := wjson.NewObject()
	body.Set("events", wjson.Array(list))
	payload := wjson.Marshal(wjson.ObjectValue(body))

	// The request is not tied to Close, so the final flush can still go out. It has
	// its own deadline.
	r := c.request(backgroundContext(), "POST", "/sdk/v1/events", [][2]string{
		{"Authorization", "Bearer " + c.key},
		{"Content-Type", "application/json"},
	}, payload, nil)
	if r.code != "" {
		return true, &RequestError{Route: "events", Code: r.code}
	}
	if r.status >= 200 && r.status < 300 {
		return false, nil
	}
	if r.status == 429 {
		if d, ok := retryAfter(r.header); ok {
			until := c.clock.Now().Add(d)
			c.usageMu.Lock()
			if until.After(c.usageHold) {
				c.usageHold = until
			}
			c.usageMu.Unlock()
		}
	}
	err = &RequestError{Route: "events", Code: statusCode(r.status)}
	return r.status >= 500 || r.status == 408 || r.status == 429, err
}
