package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const maxEventsPerRequest = 500

// count adds one served evaluation to today's count for the flag and variation. It is a memory operation
// under usageMu only. Nothing is counted once Close has begun.
func (c *Client) count(flagKey string, index int) {
	day := c.cfg.clock.Now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	if !c.noCount {
		c.usage[usageKey{flagKey, index, day}]++
	}
	c.usageMu.Unlock()
}

type usageEntry struct {
	key   usageKey
	count int64
}

// takeUsage empties the map and returns its entries in a fixed order.
func (c *Client) takeUsage() []usageEntry {
	c.usageMu.Lock()
	taken := c.usage
	c.usage = map[usageKey]int64{}
	c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(taken))
	for k, n := range taken {
		entries = append(entries, usageEntry{k, n})
	}
	slices.SortFunc(entries, func(a, b usageEntry) int {
		if a.key.day != b.key.day {
			return compareStrings(a.key.day, b.key.day)
		}
		if a.key.flag != b.key.flag {
			return compareStrings(a.key.flag, b.key.flag)
		}
		return a.key.index - b.key.index
	})
	return entries
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// keepUsage puts entries back, summed with whatever was counted meanwhile.
func (c *Client) keepUsage(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.usage[e.key] += e.count
	}
	c.usageMu.Unlock()
}

// flushLoop is the flush goroutine: a flush every flush interval, and a final one when Close begins.
func (c *Client) flushLoop() {
	defer close(c.flushDone)
	c.flushGID.Store(goroutineID())
	for {
		t := c.cfg.clock.NewTimer(c.cfg.flushInterval)
		select {
		case <-t.C():
			c.flush(false)
		case <-c.root.Done():
			t.Stop()
			c.flush(true)
			return
		}
	}
}

type sendResult uint8

const (
	sent sendResult = iota
	kept
	dropped
)

// flush sends the pending counts, at most 500 entries to a request, one request after another. Flushes are
// serial. The final flush does not wait out a Retry-After hold: the counts still pending are lost and
// onError is told.
func (c *Client) flush(final bool) {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	c.mu.Lock()
	held := c.holdUsage.After(c.cfg.clock.Now())
	c.mu.Unlock()
	if held {
		if final {
			c.usageMu.Lock()
			pending := len(c.usage)
			c.usageMu.Unlock()
			if pending > 0 {
				c.report(&Error{Op: "close", Message: "closed with " + strconv.Itoa(pending) + " usage counts unsent: the server asked to wait"})
			}
		}
		return
	}
	entries := c.takeUsage()
	for i := 0; i < len(entries); i += maxEventsPerRequest {
		batch := entries[i:min(i+maxEventsPerRequest, len(entries))]
		result, err := c.sendUsage(batch)
		c.report(err)
		if result == kept {
			c.keepUsage(entries[i:])
			return
		}
	}
}

// sendUsage posts one batch. The request is bounded by the request timeout and does not stop when Close
// begins, so a flush already running finishes.
func (c *Client) sendUsage(batch []usageEntry) (sendResult, *Error) {
	body := make([]byte, 0, 64*len(batch)+16)
	body = append(body, `{"events":[`...)
	for i, e := range batch {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, `{"flag":`...)
		body = jsonvalue.Append(body, jsonvalue.NewString(e.key.flag))
		body = append(body, `,"variationIndex":`...)
		body = strconv.AppendInt(body, int64(e.key.index), 10)
		body = append(body, `,"count":`...)
		body = strconv.AppendInt(body, e.count, 10)
		body = append(body, `,"day":"`...)
		body = append(body, e.key.day...)
		body = append(body, `"}`...)
	}
	body = append(body, `]}`...)

	ctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	req := c.newRequest(ctx, http.MethodPost, "/sdk/v1/events", bytes.NewReader(body))
	if req == nil {
		return kept, &Error{Op: "events", Code: LastErrorNetwork}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(context.Cause(ctx), errRequestTimeout) {
			return kept, &Error{Op: "events", Code: LastErrorTimeout}
		}
		return kept, &Error{Op: "events", Code: LastErrorNetwork}
	}
	_ = resp.Body.Close()
	status := resp.StatusCode
	switch {
	case status >= 200 && status < 300:
		return sent, nil
	case status >= 500, status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
		c.holdFrom(resp, &c.holdUsage)
		return kept, &Error{Op: "events", Code: "http_" + strconv.Itoa(status)}
	default:
		return dropped, &Error{Op: "events", Code: "http_" + strconv.Itoa(status)}
	}
}
