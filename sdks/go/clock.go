package toggleslash

import (
	"sync"
	"time"
)

// clock is time as the SDK sees it, so a test moves it by hand instead of waiting. Every wait and every
// timeout in the SDK goes through it.
type clock interface {
	Now() time.Time
	// NewTimer returns a timer whose channel receives once after d.
	NewTimer(d time.Duration) *clockTimer
	// AfterFunc runs f on its own goroutine after d, unless the timer is stopped first.
	AfterFunc(d time.Duration, f func()) *clockTimer
}

// clockTimer is a *time.Timer for the real clock; the fake clock fills the same shape.
type clockTimer struct {
	C     <-chan time.Time
	stop  func() bool
	reset func(time.Duration) bool
}

// Stop prevents the timer from firing. It reports whether it was still pending.
func (t *clockTimer) Stop() bool { return t.stop() }

// Reset re-arms the timer to fire after d.
func (t *clockTimer) Reset(d time.Duration) bool { return t.reset(d) }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) *clockTimer {
	t := time.NewTimer(d)
	return &clockTimer{C: t.C, stop: t.Stop, reset: t.Reset}
}

func (realClock) AfterFunc(d time.Duration, f func()) *clockTimer {
	t := time.AfterFunc(d, f)
	return &clockTimer{stop: t.Stop, reset: t.Reset}
}

// hold is a Retry-After the server asked for: nothing of one kind may reach it before the time passes. A shorter
// answer never shortens a longer wait.
type hold struct {
	mu    sync.Mutex
	until time.Time
}

func (h *hold) extend(t time.Time) {
	h.mu.Lock()
	if t.After(h.until) {
		h.until = t
	}
	h.mu.Unlock()
}

// remaining is how long is left of the hold at now; zero or negative when it is over.
func (h *hold) remaining(now time.Time) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return max(h.until.Sub(now), 0)
}
