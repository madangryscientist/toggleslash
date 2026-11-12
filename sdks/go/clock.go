package toggleslash

import (
	"context"
	"time"
)

// clock is the time every wait of the loops and the day of a usage count come from. The tests replace it with
// one that fires on demand. The request timeout and the stream watchdog do not use it: a real socket needs real
// time.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) clockTimer
}

// clockTimer is the part of *time.Timer the SDK uses.
type clockTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) clockTimer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }

func (r realTimer) Stop() bool { return r.t.Stop() }

// sleep waits d on clk, or until ctx is done. It reports whether the full wait passed.
func sleep(ctx context.Context, clk clock, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := clk.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C():
		return ctx.Err() == nil
	}
}
