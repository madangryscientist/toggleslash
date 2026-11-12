package toggleslash

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// Usage counts: how often each flag variation was served, per UTC day, sent in batches. Counts only: no
// context, no context key and nothing about an end user.

// maxBatch is the most entries one usage request carries (contract section 9).
const maxBatch = 500

type usageKey struct {
	flag      string
	variation int
	day       string
}

type usageEntry struct {
	key   usageKey
	count int
}

type usageReporter struct {
	transport *httpTransport
	base      string
	sdkKey    string
	timeout   time.Duration
	onError   func(error)
	now       func() time.Time
	hold      *retryHold

	// flushLock is a one-slot semaphore, so a flush that starts while another runs waits for it.
	flushLock chan struct{}

	mu       sync.Mutex
	pending  map[usageKey]int
	counting bool
	stopping bool
}

func newUsageReporter(transport *httpTransport, base, sdkKey string, timeout time.Duration, onError func(error), now func() time.Time, hold *retryHold) *usageReporter {
	return &usageReporter{
		transport: transport,
		base:      base,
		sdkKey:    sdkKey,
		timeout:   timeout,
		onError:   onError,
		now:       now,
		hold:      hold,
		flushLock: make(chan struct{}, 1),
		pending:   map[usageKey]int{},
		counting:  true,
	}
}

// record adds one count. It is an in-memory add, cheap enough for a request path.
func (u *usageReporter) record(flag string, variation int) {
	day := u.now().UTC().Format("2006-01-02")
	u.mu.Lock()
	if u.counting {
		u.pending[usageKey{flag: flag, variation: variation, day: day}]++
	}
	u.mu.Unlock()
}

// stopCounting ends counting, and a flush in progress stops after its current batch.
func (u *usageReporter) stopCounting() {
	u.mu.Lock()
	u.counting = false
	u.stopping = true
	u.mu.Unlock()
}

func (u *usageReporter) isStopping() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stopping
}

func (u *usageReporter) pendingCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.pending)
}

// flush sends what is pending. Flushes run one at a time, and onError runs after the flush lock is released.
func (u *usageReporter) flush() {
	u.flushLock <- struct{}{}
	defer func() { <-u.flushLock }()
	u.tell(u.send(false))
}

// finish is the final flush. It does not wait out a Retry-After, and it says so when that costs counts.
func (u *usageReporter) finish(wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	var errors []error
	select {
	case u.flushLock <- struct{}{}:
	case <-timer.C:
		u.tell([]error{&Error{Message: fmt.Sprintf("closed with %d usage counts unsent: a flush was still running", u.pendingCount())}})
		return
	}
	errors = u.send(true)
	<-u.flushLock
	if left := u.pendingCount(); left > 0 && u.hold.remaining() > 0 {
		errors = append(errors, &Error{Message: fmt.Sprintf("closed with %d usage counts unsent: the server asked to wait", left)})
	}
	u.tell(errors)
}

func (u *usageReporter) tell(errors []error) {
	for _, err := range errors {
		u.onError(err)
	}
}

func (u *usageReporter) take() []usageEntry {
	u.mu.Lock()
	defer u.mu.Unlock()
	entries := make([]usageEntry, 0, len(u.pending))
	for key, count := range u.pending {
		entries = append(entries, usageEntry{key: key, count: count})
	}
	u.pending = map[usageKey]int{}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].key, entries[j].key
		if left.flag != right.flag {
			return left.flag < right.flag
		}
		if left.variation != right.variation {
			return left.variation < right.variation
		}
		return left.day < right.day
	})
	return entries
}

func (u *usageReporter) putBack(entries []usageEntry) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, entry := range entries {
		u.pending[entry.key] += entry.count
	}
}

// send posts the pending counts in batches of at most 500, one request after another.
func (u *usageReporter) send(final bool) []error {
	var errors []error
	if u.hold.remaining() > 0 {
		return errors
	}
	entries := u.take()
	for start := 0; start < len(entries); start += maxBatch {
		if !final && u.isStopping() {
			u.putBack(entries[start:])
			return errors
		}
		end := start + maxBatch
		if end > len(entries) {
			end = len(entries)
		}
		if !u.post(entries[start:end], &errors) {
			u.putBack(entries[start:])
			return errors
		}
	}
	return errors
}

// post sends one batch, answering false when the batch (and the rest) should be kept and the flush stopped.
func (u *usageReporter) post(batch []usageEntry, found *[]error) bool {
	events := make([]any, 0, len(batch))
	for _, entry := range batch {
		events = append(events, map[string]any{
			"flag":           entry.key.flag,
			"variationIndex": entry.key.variation,
			"count":          entry.count,
			"day":            entry.key.day,
		})
	}
	body := jsonx.Encode(map[string]any{"events": events})
	headers := map[string]string{
		"Authorization": "Bearer " + u.sdkKey,
		"Content-Type":  "application/json",
	}
	response, err := u.transport.do("POST", u.base, "/sdk/v1/events", headers, body, u.timeout)
	if err != nil {
		if errors.Is(err, errAborted) {
			return false
		}
		*found = append(*found, &Error{Message: describeError(err, "POST /sdk/v1/events"), Code: codeOf(err)})
		return false
	}
	if response.status >= 200 && response.status < 300 {
		return true
	}
	if wait, ok := retryAfter(response.status, response.header.Get("Retry-After")); ok {
		u.hold.raiseTo(wait)
	}
	*found = append(*found, &Error{
		Message: fmt.Sprintf("POST /sdk/v1/events answered %d", response.status),
		Code:    LastError(fmt.Sprintf("http_%d", response.status)),
	})
	return !retryableStatus(response.status)
}

// retryableStatus reports whether a batch the server refused is kept for another try. The status alone
// decides.
func retryableStatus(status int) bool {
	return status >= 500 || status == 408 || status == 429
}
