package toggleslash

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// usageKey is one count: a flag key, a variation index and a UTC day
// (contract 9, "What is kept").
type usageKey struct {
	flag      string
	variation int
	day       string
}

// usageEntry is one count on its way out.
type usageEntry struct {
	key   usageKey
	count int64
}

// record adds one count. Recording is an in-memory add, and nothing is counted
// after Close begins (contract 9, 11).
func (c *Client) record(flagKey string, variation int) {
	day := c.opts.clock.Now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	// The check is inside the lock: Close sets closed and then drains the map
	// under this lock, so a count that passed an unlocked check could land
	// after the final flush and never be reported. Here it either lands before
	// that drain or is dropped as post-close work.
	if c.closed.Load() {
		c.usageMu.Unlock()
		return
	}
	c.usage[usageKey{flag: flagKey, variation: variation, day: day}]++
	c.usageMu.Unlock()
}

// takeUsage swaps the pending counts for an empty map.
func (c *Client) takeUsage() []usageEntry {
	c.usageMu.Lock()
	if len(c.usage) == 0 {
		c.usageMu.Unlock()
		return nil
	}
	old := c.usage
	c.usage = map[usageKey]int64{}
	c.usageMu.Unlock()

	entries := make([]usageEntry, 0, len(old))
	for key, count := range old {
		entries = append(entries, usageEntry{key: key, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].key, entries[j].key
		if a.flag != b.flag {
			return a.flag < b.flag
		}
		if a.variation != b.variation {
			return a.variation < b.variation
		}
		return a.day < b.day
	})
	return entries
}

// mergeUsage puts counts back, adding them to anything recorded since.
func (c *Client) mergeUsage(entries []usageEntry) {
	c.usageMu.Lock()
	for _, entry := range entries {
		c.usage[entry.key] += entry.count
	}
	c.usageMu.Unlock()
}

// flushLoop sends the counts every flushInterval, the first one interval after
// New, and does the final flush on the way out. A panic in a flush is reported
// and the loop goes on; so is one that reaches the loop itself, which then
// starts again with its sleep unless the client is closed (FS-178).
func (c *Client) flushLoop() {
	defer c.wg.Done()
	for {
		raised, panicked := catch(func() {
			for c.sleep(c.opts.FlushInterval) {
				c.flushGuarded()
			}
		})
		if !panicked {
			break
		}
		c.reportPanic("flush", raised)
		if c.closed.Load() {
			break
		}
	}
	if raised, panicked := catch(c.flushFinal); panicked {
		c.reportPanic("flush", raised)
	}
}

// flushGuarded is flush with a panic reported instead of raised. The counts
// the flush had taken are lost.
func (c *Client) flushGuarded() {
	if raised, panicked := catch(func() { c.flush(false) }); panicked {
		c.reportPanic("flush", raised)
	}
}

// flushFinal is contract 11's step 4: a flush bounded by requestTimeout per
// request, that does not wait out a Retry-After.
func (c *Client) flushFinal() {
	now := c.opts.clock.Now()
	if c.eventsHold.remaining(now) > 0 {
		lost := c.takeUsage()
		if len(lost) > 0 {
			c.report(unsentError{count: len(lost), waiting: true})
		}
		return
	}
	c.flush(true)
}

// unsentError is what onError receives for counts lost at close (contract 10,
// 11).
type unsentError struct {
	count   int
	waiting bool
}

func (e unsentError) Error() string {
	if e.waiting {
		return fmt.Sprintf("toggleslash: closed with %d usage counts unsent: the server asked to wait", e.count)
	}
	return fmt.Sprintf("toggleslash: closed with %d usage counts unsent", e.count)
}

// flush sends the pending counts, in requests of at most 500 entries, one
// after another (contract 9). With final set, the flush cannot keep anything
// for a later one, so a kept batch is lost and reported.
func (c *Client) flush(final bool) {
	if c.opts.flushProbe != nil {
		c.opts.flushProbe()
	}
	if !final && c.eventsHold.remaining(c.opts.clock.Now()) > 0 {
		// A flush during a hold returns at once, keeping its counts.
		return
	}
	entries := c.takeUsage()
	for len(entries) > 0 {
		size := len(entries)
		if size > maxUsageEntries {
			size = maxUsageEntries
		}
		batch, rest := entries[:size], entries[size:]
		outcome := c.sendUsage(batch)
		if outcome == outcomeKeep {
			if final {
				c.report(unsentError{count: len(entries), waiting: c.eventsHold.remaining(c.opts.clock.Now()) > 0})
				return
			}
			c.mergeUsage(entries)
			return
		}
		entries = rest
	}
}

// usageOutcome is what a usage request told the flush to do with its batch.
type usageOutcome int

const (
	outcomeSent usageOutcome = iota
	outcomeKeep
	outcomeDrop
)

// sendUsage sends one batch and applies contract 9's table.
func (c *Client) sendUsage(batch []usageEntry) usageOutcome {
	body := jsonval.Object{"events": usageBody(batch)}
	var buffer bytes.Buffer
	if err := jsonval.Write(&buffer, body); err != nil {
		// Counts that cannot be written cannot be sent either; the batch was
		// dropped by a bug rather than by the server.
		c.report(err)
		return outcomeDrop
	}

	// A usage request is not abandoned by Close: contract 11 waits for a flush
	// already running, so it is bounded by requestTimeout alone.
	ctx, cancel := context.WithTimeout(context.Background(), c.opts.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.URL+"/sdk/v1/events", bytes.NewReader(buffer.Bytes()))
	if err != nil {
		c.report(networkError{cause: err})
		return outcomeKeep
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.SDKKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		code := LastErrorNetwork
		if isTimeout(err) {
			code = LastErrorTimeout
		}
		c.report(failureError{code: code, cause: err})
		return outcomeKeep
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return outcomeSent
	case resp.StatusCode == http.StatusTooManyRequests:
		c.readRetryAfter(&c.eventsHold, resp.Header.Get("Retry-After"))
		c.report(failureError{code: HTTPLastError(resp.StatusCode)})
		return outcomeKeep
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		c.report(failureError{code: HTTPLastError(resp.StatusCode)})
		return outcomeKeep
	default:
		// A 400, 401, 413 or 404 would be refused again: drop the batch and go
		// on with the next request.
		c.report(failureError{code: HTTPLastError(resp.StatusCode)})
		return outcomeDrop
	}
}

// usageBody writes exactly the four fields of contract 9 and nothing else.
func usageBody(batch []usageEntry) jsonval.Array {
	events := make(jsonval.Array, 0, len(batch))
	for _, entry := range batch {
		events = append(events, jsonval.Object{
			"flag":           jsonval.String(entry.key.flag),
			"variationIndex": jsonval.Number(float64(entry.key.variation)),
			"count":          jsonval.Number(float64(entry.count)),
			"day":            jsonval.String(entry.key.day),
		})
	}
	return events
}
