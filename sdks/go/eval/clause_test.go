package eval

import (
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// TestSubstringOpsUseUTF16CodeUnits is docs/evaluation.md, "String equality and
// the three substring ops are ordinal": the units compared are UTF-16's, so a
// lone surrogate matches half of a pair, which a byte comparison cannot see.
func TestSubstringOpsUseUTF16CodeUnits(t *testing.T) {
	const (
		face = "\U0001F600"   // two UTF-16 units: D83D DE00
		high = "\xed\xa0\xbd" // U+D83D on its own, as WTF-8
		low  = "\xed\xb8\x80" // U+DE00 on its own, as WTF-8
	)
	cases := []struct {
		op        string
		candidate string
		wanted    string
		want      bool
	}{
		{"contains", face, high, true},
		{"contains", face, low, true},
		{"contains", "a" + face + "b", low + "b", true},
		{"contains", "abc", "", true},
		{"contains", face, "\uFFFD", false},
		{"startsWith", face, high, true},
		{"startsWith", face, low, false},
		{"endsWith", face, low, true},
		{"endsWith", face, high, false},
		{"endsWith", "a" + face, low, true},
	}
	for _, tc := range cases {
		got := matchesValue(tc.op, jsonval.String(tc.candidate), jsonval.String(tc.wanted))
		if got != tc.want {
			t.Errorf("%s(%q, %q) = %v, want %v", tc.op, tc.candidate, tc.wanted, got, tc.want)
		}
	}
}

// TestPatternSizeSaturates covers docs/evaluation.md "Patterns": a size that
// overflows would pass the limit and then compile a pattern whose repetition
// multiplies out to a hang. 100^32 wraps to zero in a 64 bit int.
func TestPatternSizeSaturates(t *testing.T) {
	pattern := "a"
	for i := 0; i < 32; i++ {
		pattern = "(" + pattern + "){100}"
	}
	if len(pattern) > maxPatternPoints {
		t.Fatalf("the test's pattern is %d points, over the limit it means to stay under", len(pattern))
	}
	if matchesValue("matches", jsonval.String("a"), jsonval.String(pattern)) {
		t.Fatalf("a pattern whose size overflows the limit was accepted")
	}
}

// TestInstantTextUsesInt64Millis pins the epoch milliseconds of a date well
// after 1970: an int multiply overflows on a 32 bit build.
func TestInstantTextUsesInt64Millis(t *testing.T) {
	got, ok := parseInstantText("2026-11-13T00:00:00Z")
	if !ok {
		t.Fatalf("the instant did not parse")
	}
	want := float64(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC).UnixMilli())
	if got != want {
		t.Fatalf("parseInstantText = %v, want %v", got, want)
	}
}
