package jsonval

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

// TestReadKeepsWhatTheSpecNeeds covers the four things docs/evaluation.md
// "Reading JSON" needs a platform decoder to keep, on the paths the phase 6
// review found untested: a lone surrogate, 1e999, the depth limit, and an ill
// formed UTF-8 sequence at the end of the body.
func TestReadKeepsWhatTheSpecNeeds(t *testing.T) {
	t.Run("lone surrogate round trip", func(t *testing.T) {
		const high = "\xed\xa0\xbd" // U+D83D as WTF-8
		var out bytes.Buffer
		if err := Write(&out, String(high)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := out.String(); got != `"\ud83d"` {
			t.Fatalf("Write = %s, want the \\u escape", got)
		}
		back, err := Read(strings.NewReader(out.String()), 64)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if got, ok := back.(String); !ok || string(got) != high {
			t.Fatalf("Read = %#v, want the same lone surrogate", back)
		}
	})

	t.Run("1e999 is infinity", func(t *testing.T) {
		value, err := Read(strings.NewReader(`[1e999,-1e999]`), 64)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		array, ok := value.(Array)
		if !ok || len(array) != 2 {
			t.Fatalf("Read = %#v, want two numbers", value)
		}
		if !math.IsInf(float64(array[0].(Number)), 1) || !math.IsInf(float64(array[1].(Number)), -1) {
			t.Fatalf("Read = %v, want +Inf and -Inf", array)
		}
	})

	t.Run("depth limit", func(t *testing.T) {
		if _, err := Read(strings.NewReader(`[[[[1]]]]`), 3); err == nil {
			t.Fatalf("Read accepted four arrays at a depth of three")
		}
	})

	t.Run("truncated sequence keeps the closing quote", func(t *testing.T) {
		// The string ends with an incomplete three byte sequence: the lead byte
		// F0 and then the quote and the bracket. Only the lead is the maximal
		// subpart; the quote must still close the string.
		value, err := Read(strings.NewReader("[\"x\xf0\"]"), 64)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		array, ok := value.(Array)
		if !ok || len(array) != 1 {
			t.Fatalf("Read = %#v, want one string", value)
		}
		want := "x\xef\xbf\xbd"
		if got := string(array[0].(String)); got != want {
			t.Fatalf("Read = %q, want %q", got, want)
		}
	})
}
