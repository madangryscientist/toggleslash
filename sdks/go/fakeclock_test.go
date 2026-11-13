package toggleslash

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is the test seam for time (PROMPT.md, "Phase 3: client"). A test
// steps it only while the SDK waits on it: it waits for a timer to be armed,
// then advances, and never after a fixed real pause.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) timer {
	c.mu.Lock()
	t := &fakeTimer{clock: c, deadline: c.now.Add(d), active: true, run: f}
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return t
}

// advance moves the clock and runs every timer that came due, in deadline
// order.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	due := make([]*fakeTimer, 0, len(c.timers))
	live := c.timers[:0]
	for _, t := range c.timers {
		if t.active && !t.deadline.After(c.now) {
			t.active = false
			due = append(due, t)
			continue
		}
		live = append(live, t)
	}
	c.timers = live
	c.mu.Unlock()
	for _, t := range due {
		t.run()
	}
}

// armedDuration answers the earliest armed timer's length, and false when the
// SDK is not waiting on the clock at all. A step is taken only once this is
// true: the SDK must be waiting on the clock for the step to mean anything.
func (c *fakeClock) armedDuration() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var earliest time.Time
	for _, timer := range c.timers {
		if !timer.active {
			continue
		}
		if earliest.IsZero() || timer.deadline.Before(earliest) {
			earliest = timer.deadline
		}
	}
	if earliest.IsZero() {
		return 0, false
	}
	return earliest.Sub(c.now), true
}

// waitForDuration waits until the SDK's earliest clock wait is exactly want.
func (c *fakeClock) waitForDuration(t *testing.T, want, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if d, ok := c.armedDuration(); ok && d == want {
			return
		}
		if time.Now().After(deadline) {
			d, ok := c.armedDuration()
			t.Fatalf("the SDK's clock wait was %v (armed %v), want %v within %v", d, ok, want, within)
		}
		time.Sleep(time.Millisecond)
	}
}

// advanceUntil steps the clock whenever the SDK is waiting on it, until cond
// holds. It observes completion, not a fixed pause.
func (c *fakeClock) advanceUntil(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if d, ok := c.armedDuration(); ok {
			c.advance(d)
			continue
		}
		if time.Now().After(deadline) {
			t.Fatalf("the condition was not reached within %v", within)
		}
		time.Sleep(time.Millisecond)
	}
}

// advanceBy steps the clock by d, refusing when nothing is armed (a step with
// nothing waiting would test nothing).
func (c *fakeClock) advanceBy(t *testing.T, d time.Duration) {
	t.Helper()
	if _, ok := c.armedDuration(); !ok {
		t.Fatalf("no timer is armed to step")
	}
	c.advance(d)
}

type fakeTimer struct {
	clock    *fakeClock
	deadline time.Time
	active   bool
	run      func()
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	active := t.active
	t.active = false
	return active
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	wasActive := t.active
	t.deadline = t.clock.now.Add(d)
	t.active = true
	if !wasActive {
		t.clock.timers = append(t.clock.timers, t)
	}
	t.clock.mu.Unlock()
	return wasActive
}
