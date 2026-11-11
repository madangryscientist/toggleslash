package toggleslash

import "time"

// clock is the one place the SDK reads time and arms a wait, so a test can run the sync loop, the flush
// loop, the Retry-After holds and the UTC day on a clock it moves by hand. The request timeout and the
// stream watchdog are real time: they bound real sockets.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type timer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }

func (r realTimer) Stop() bool { return r.t.Stop() }
