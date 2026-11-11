package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// maxEventsPerRequest is how many entries one events request carries (the server takes 1000).
const maxEventsPerRequest = 500

type usageKey struct {
	flag  string
	index int
	day   string
}

type usageEntry struct {
	usageKey
	count int64
}

// record counts one served variation for today (UTC). It is an in-memory add. Nothing is counted after Close.
func (c *Client) record(flagKey string, index int) {
	day := c.cfg.now().UTC().Format("2006-01-02")
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usageClosed {
		return
	}
	c.usage[usageKey{flagKey, index, day}]++
}

func (c *Client) takeUsage() []usageEntry {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	entries := make([]usageEntry, 0, len(c.usage))
	for k, n := range c.usage {
		entries = append(entries, usageEntry{k, n})
	}
	clear(c.usage)
	slices.SortFunc(entries, func(a, b usageEntry) int {
		if a.day != b.day {
			return strings.Compare(a.day, b.day)
		}
		if a.flag != b.flag {
			return strings.Compare(a.flag, b.flag)
		}
		return a.index - b.index
	})
	return entries
}

// restoreUsage merges counts that were not sent with the ones recorded since.
func (c *Client) restoreUsage(entries []usageEntry) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	for _, e := range entries {
		c.usage[e.usageKey] += e.count
	}
}

func (c *Client) pendingUsage() int {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return len(c.usage)
}

func (c *Client) flushLoop() {
	defer close(c.flushDone)
	c.markGoroutine()
	defer c.unmarkGoroutine()
	timer := time.NewTimer(c.cfg.flushInterval)
	defer timer.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-timer.C:
		}
		c.safely("events", func() { c.flush(false) })
		timer.Reset(c.cfg.flushInterval)
	}
}

type postOutcome int

const (
	postSent postOutcome = iota
	postKept
	postDropped
)

// flush sends the pending counts. Flushes are serial. final is the flush Close makes: it does not wait out a
// Retry-After, and counts still held back by one are lost and reported.
func (c *Client) flush(final bool) {
	c.flushMu.Lock()
	errs := c.flushLocked()
	c.flushMu.Unlock()
	if final && c.pendingUsage() > 0 && c.eventsHold.remaining(c.cfg.now()) > 0 {
		errs = append(errs, &Error{Op: "close", Code: fmt.Sprintf("closed with %d usage counts unsent: the server asked to wait", c.pendingUsage())})
	}
	for _, err := range errs {
		c.report(err)
	}
}

func (c *Client) flushLocked() []error {
	if c.eventsHold.remaining(c.cfg.now()) > 0 {
		return nil
	}
	entries := c.takeUsage()
	var errs []error
	for start := 0; start < len(entries); start += maxEventsPerRequest {
		chunk := entries[start:min(start+maxEventsPerRequest, len(entries))]
		outcome, err := c.postEvents(chunk)
		if err != nil {
			errs = append(errs, err)
		}
		switch outcome {
		case postKept:
			c.restoreUsage(entries[start:])
			return errs
		case postSent, postDropped:
		}
	}
	return errs
}

func eventsBody(entries []usageEntry) []byte {
	var b strings.Builder
	b.WriteString(`{"events":[`)
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"flag":`)
		jsonvalue.WriteString(&b, e.flag)
		b.WriteString(`,"variationIndex":`)
		b.WriteString(strconv.Itoa(e.index))
		b.WriteString(`,"count":`)
		b.WriteString(strconv.FormatInt(e.count, 10))
		b.WriteString(`,"day":`)
		jsonvalue.WriteString(&b, e.day)
		b.WriteByte('}')
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// postEvents sends one request. The flush context is not the sync context: Close waits for a flush that is
// running, and each request is bounded by the request timeout.
func (c *Client) postEvents(entries []usageEntry) (postOutcome, error) {
	rctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errRequestTimeout)
	defer cancel()
	req, err := c.newRequest(rctx, http.MethodPost, "/sdk/v1/events", bytes.NewReader(eventsBody(entries)))
	if err != nil {
		return postKept, &Error{Op: "events", Code: LastErrorNetwork}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		code := LastErrorNetwork
		if errors.Is(context.Cause(rctx), errRequestTimeout) {
			code = LastErrorTimeout
		}
		return postKept, &Error{Op: "events", Code: code}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	status := resp.StatusCode
	if status >= 200 && status < 300 {
		return postSent, nil
	}
	failure := &Error{Op: "events", Code: "http_" + strconv.Itoa(status)}
	if status == http.StatusTooManyRequests {
		c.noteRetryAfter(&c.eventsHold, resp)
	}
	if status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return postKept, failure
	}
	return postDropped, failure
}
