package toggleslash

import (
	"net/http"
	"testing"
	"time"
)

// TestBackoffDoublesFromOneSecondAndIsJittered: after n failures the wait is a second doubled n times,
// capped at the poll interval, then jittered into half to all of that; with no stream it is the poll
// interval itself.
func TestBackoffDoublesFromOneSecondAndIsJittered(t *testing.T) {
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) { f.hang() })
	low := newTestClient(t, f, WithPollInterval(30*time.Second), withJitter(func() float64 { return 0 }))
	high := newTestClient(t, f, WithPollInterval(30*time.Second), withJitter(func() float64 { return 1 }))
	cases := []struct {
		failures  int
		low, high time.Duration
	}{
		{0, 500 * time.Millisecond, time.Second},
		{1, time.Second, 2 * time.Second},
		{3, 4 * time.Second, 8 * time.Second},
		{5, 15 * time.Second, 30 * time.Second},
		{40, 15 * time.Second, 30 * time.Second},
	}
	for _, tc := range cases {
		if got := low.delay(tc.failures); got != tc.low {
			t.Errorf("%d failures, jitter 0: %v, want %v", tc.failures, got, tc.low)
		}
		if got := high.delay(tc.failures); got != tc.high {
			t.Errorf("%d failures, jitter 1: %v, want %v", tc.failures, got, tc.high)
		}
	}
	polling := newTestClient(t, f, WithStream(false), WithPollInterval(7*time.Second))
	if got := polling.delay(9); got != 7*time.Second {
		t.Errorf("with no stream the wait is the poll interval, got %v", got)
	}
}
