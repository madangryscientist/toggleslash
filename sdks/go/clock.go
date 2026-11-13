package toggleslash

import (
	"sync"
	"time"
)

// clock is the SDK's only source of time. The production one is the system
// clock; a test sets Options.clock so long timers are stepped, never waited
// out (PROMPT.md, "Phase 3: client").
type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) timer
}

// timer is what clock.AfterFunc answers: a test's clock implements it too.
type timer interface {
	Stop() bool
	Reset(d time.Duration) bool
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) timer { return time.AfterFunc(d, f) }

// sleep waits d on the client's clock and answers false when the client was
// closed first. A non-positive d still checks the closed flag.
func (c *Client) sleep(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-c.ctx.Done():
			return false
		default:
			return true
		}
	}
	done := make(chan struct{})
	t := c.opts.clock.AfterFunc(d, func() { close(done) })
	select {
	case <-done:
		return true
	case <-c.ctx.Done():
		t.Stop()
		return false
	}
}

// waitFor waits d, and longer when a Retry-After hold reaches past it: contract
// 7.4 says each wait of the loop lasts at least until the hold. A hold raised
// while the wait is running is waited out too, by its deadline rather than by
// its length, so the wait never overshoots.
func (c *Client) waitFor(d time.Duration) bool {
	until := c.opts.clock.Now().Add(d)
	for {
		if deadline := c.requestHold.deadline(); deadline.After(until) {
			until = deadline
		}
		wait := until.Sub(c.opts.clock.Now())
		if wait <= 0 {
			return true
		}
		if !c.sleep(wait) {
			return false
		}
		if !c.requestHold.deadline().After(until) {
			return true
		}
	}
}

// hold is a Retry-After deadline. It is a time, not a sleep (contract 7.4): a
// later 429 may lengthen it and none shortens it.
type hold struct {
	mu    sync.Mutex
	until time.Time
}

// raise moves the hold to now+d, or leaves it when it already reaches further.
func (h *hold) raise(now time.Time, d time.Duration) {
	until := now.Add(d)
	h.mu.Lock()
	if until.After(h.until) {
		h.until = until
	}
	h.mu.Unlock()
}

// remaining is how long is left of the hold, zero when it has passed.
func (h *hold) remaining(now time.Time) time.Duration {
	h.mu.Lock()
	until := h.until
	h.mu.Unlock()
	if !until.After(now) {
		return 0
	}
	return until.Sub(now)
}

// deadline is the hold's time, the zero value when there is none.
func (h *hold) deadline() time.Time {
	h.mu.Lock()
	until := h.until
	h.mu.Unlock()
	return until
}
