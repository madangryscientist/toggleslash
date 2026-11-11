package toggleslash

import (
	"context"
	"time"
)

// timer is what clock.AfterFunc returns.
type timer interface{ Stop() bool }

// clock is the SDK's time: the real one outside tests, a stepped fake in them.
type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) timer
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) timer { return time.AfterFunc(d, f) }

// sleep waits d on the clock, or until ctx is done. It reports whether the full time passed.
func sleep(ctx context.Context, k clock, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	fired := make(chan struct{})
	t := k.AfterFunc(d, func() { close(fired) })
	defer t.Stop()
	select {
	case <-fired:
		return true
	case <-ctx.Done():
		return false
	}
}

// utcDay is the UTC calendar day of t as yyyy-MM-dd.
func utcDay(t time.Time) string { return t.UTC().Format("2006-01-02") }
