package toggleslash

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// maxEventsPerRequest is the most entries one usage request carries.
const maxEventsPerRequest = 500

// usageKey is one counter: a flag, the variation served and the UTC day.
type usageKey struct {
	flag  string
	index int
	day   string
}

// count records one counted evaluation.
func (c *Client) count(flag string, index int) {
	key := usageKey{flag: flag, index: index, day: c.cfg.now().UTC().Format("2006-01-02")}
	c.usageMu.Lock()
	c.usage[key]++
	c.usageMu.Unlock()
}

type usageEntry struct {
	key   usageKey
	count int64
}

func (c *Client) takeUsage() []usageEntry {
	c.usageMu.Lock()
	taken := c.usage
	c.usage = map[usageKey]int64{}
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

// restoreUsage merges counts that were kept back into the ones recorded since.
func (c *Client) restoreUsage(entries []usageEntry) {
	c.usageMu.Lock()
	for _, e := range entries {
		c.usage[e.key] += e.count
	}
	c.usageMu.Unlock()
}

func (c *Client) flushLoop() {
	for c.sleep(c.cfg.flushInterval) {
		for _, e := range c.flush(false) {
			c.reportFromLoop(e)
		}
	}
}

// flush sends the pending counts (section 9) and returns the errors for the caller to report once
// the flush holds no lock. Flushes run one at a time.
func (c *Client) flush(final bool) []*Error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	pending := c.takeUsage()
	if len(pending) == 0 {
		return nil
	}
	if c.usageHeld() {
		if final {
			return []*Error{{
				Source: SourceClose,
				Message: "closed with " + strconv.Itoa(len(pending)) +
					" usage counts unsent: the server asked to wait",
			}}
		}
		c.restoreUsage(pending)
		return nil
	}
	var errs []*Error
	for i := 0; i < len(pending); i += maxEventsPerRequest {
		chunk := pending[i:min(i+maxEventsPerRequest, len(pending))]
		code, status, retry := c.postEvents(chunk)
		switch {
		case code == "":
			continue
		case status == 0 || status >= 500 || status == http.StatusRequestTimeout ||
			status == http.StatusTooManyRequests:
			c.restoreUsage(pending[i:])
			if status == http.StatusTooManyRequests && retry >= 0 {
				c.holdUsage(retry)
			}
			return append(errs, &Error{Source: SourceEvents, Code: code, Message: "usage request failed: " + code})
		default:
			errs = append(errs, &Error{Source: SourceEvents, Code: code, Message: "usage request failed: " + code})
		}
	}
	return errs
}

// postEvents sends one request. It returns the failure code ("" when the server took the counts),
// the status (0 when there was no answer) and, for a 429, the Retry-After (negative when none).
func (c *Client) postEvents(chunk []usageEntry) (code string, status int, retry time.Duration) {
	retry = -1
	rctx, cancel := context.WithTimeoutCause(context.Background(), c.cfg.requestTimeout, errTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, c.base+"/sdk/v1/events",
		bytes.NewReader(eventsBody(chunk)))
	if err != nil {
		return SyncNetwork, 0, retry
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		if errors.Is(context.Cause(rctx), errTimeout) {
			return SyncTimeout, 0, retry
		}
		return SyncNetwork, 0, retry
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "", resp.StatusCode, retry
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if d, ok := parseRetryAfter(resp.Header); ok {
			retry = d
		}
	}
	return httpCode(resp.StatusCode), resp.StatusCode, retry
}

func eventsBody(chunk []usageEntry) []byte {
	b := []byte(`{"events":[`)
	for i, e := range chunk {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, `{"flag":`...)
		b = jsonv.AppendString(b, e.key.flag)
		b = append(b, `,"variationIndex":`...)
		b = strconv.AppendInt(b, int64(e.key.index), 10)
		b = append(b, `,"count":`...)
		b = strconv.AppendInt(b, e.count, 10)
		b = append(b, `,"day":"`...)
		b = append(b, e.key.day...)
		b = append(b, `"}`...)
	}
	return append(b, "]}"...)
}
