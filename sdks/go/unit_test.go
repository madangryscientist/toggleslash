package toggleslash

import (
	"math"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"0", 0, true},
		{"5", 5 * time.Second, true},
		{"  7 ", 7 * time.Second, true},
		{"300", 300 * time.Second, true},
		{"301", 300 * time.Second, true},
		{"99999999999999999999999999", 300 * time.Second, true},
		{"", 0, false},
		{"   ", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1.5", 0, false},
		{"1e2", 0, false},
		{"5 s", 0, false},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0, false},
		{"٥", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	poll := 30 * time.Second
	// Stream off: a flat poll interval, whatever the draw.
	for _, r := range []float64{0, 0.5, 0.999} {
		if got := backoffDelay(false, 7, poll, r); got != poll {
			t.Errorf("stream off: %v", got)
		}
	}
	ceilings := map[int]time.Duration{0: 1, 1: 2, 2: 4, 3: 8, 4: 16, 5: 30, 6: 30, 16: 30, 40: 30}
	for failures, secs := range ceilings {
		ceiling := secs * time.Second
		if secs == 30 {
			ceiling = poll
		}
		lo := backoffDelay(true, failures, poll, 0)
		hi := backoffDelay(true, failures, poll, 0.999999)
		if lo != ceiling/2 {
			t.Errorf("failures %d: lowest %v, want %v", failures, lo, ceiling/2)
		}
		if hi >= ceiling || hi < ceiling*99/100 {
			t.Errorf("failures %d: highest %v, ceiling %v", failures, hi, ceiling)
		}
	}
	// The ceiling never passes the poll interval.
	if got := backoffDelay(true, 10, 3*time.Second, 0.999999); got >= 3*time.Second {
		t.Errorf("got %v", got)
	}
}

func TestWholeInt64(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
		ok   bool
	}{
		{2, 2, true},
		{math.Copysign(0, -1), 0, true}, // negative zero
		{2.5, 0, false},
		{9223372036854774784, 9223372036854774784, true}, // 2^63 - 1024
		{9223372036854775807, 0, false},                  // reads as 2^63
		{1e20, 0, false},
		{-9223372036854775808, -9223372036854775808, true},
		{-1e20, 0, false},
	}
	for _, tc := range cases {
		got, ok := wholeInt64(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("wholeInt64(%v) = %v, %v", tc.in, got, ok)
		}
	}
}
