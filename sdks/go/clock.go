package toggleslash

import (
	"context"
	"time"
)

// clock is the time every wait and every timestamp goes through, so a test can
// drive the loops without real waiting.
type clock interface {
	Now() time.Time
	// Sleep waits d, or until ctx ends. It reports false when ctx ended first.
	Sleep(ctx context.Context, d time.Duration) bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
