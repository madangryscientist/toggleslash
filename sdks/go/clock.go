package toggleslash

import "time"

// clock is the time the SDK waits on and reads, so a test can move it by hand. The real one is a thin
// wrapper over the time package. Nothing outside the package can set it: it is an unexported config field.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

// timer is a one-shot timer that has fired when C delivers.
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

// utcDay is the day a count belongs to, as POST /sdk/v1/events wants it.
func utcDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }
