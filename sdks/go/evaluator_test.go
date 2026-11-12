package toggleslash

import (
	"strings"
	"testing"
	"time"
)

func TestAPatternThatWouldHangABacktrackingEngineFinishes(t *testing.T) {
	program, ok := compilePattern("(a*)*b")
	if !ok {
		t.Fatal("(a*)*b was refused")
	}
	input := strings.Repeat("a", 5000)
	started := time.Now()
	if patternMatches(program, input) {
		t.Error("(a*)*b matched 5000 a's")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("matching took %v, so the engine is not a Pike VM", elapsed)
	}
}

func TestThePatternSizeAndLengthLimits(t *testing.T) {
	for _, one := range []struct {
		pattern string
		valid   bool
	}{
		{"(a{100}){100}", true},   // size exactly 10000
		{"(a{100}){100}b", false}, // 10001
		{"a{101}", false},         // a count above 100
		{"a{2,1}", false},
		{"a{,2}", false},
		{"a{01}", false},
		{strings.Repeat("a", 1024), true},
		{strings.Repeat("a", 1025), false},
		{"", true},   // the empty pattern matches every string
		{"()", true}, // an empty group
		{"[a-z-9]", false},
		{"[\\d-z]", false},
		{"a+?", false},
		{"\\b", false},
	} {
		program, ok := compilePattern(one.pattern)
		if ok != one.valid {
			t.Errorf("%.20q compiled=%v, want %v", one.pattern, ok, one.valid)
		}
		if ok && one.pattern == "" && !patternMatches(program, "anything") {
			t.Error("the empty pattern did not match")
		}
	}
}

func TestADateStringIsReadByHand(t *testing.T) {
	cases := map[string]float64{
		"2026-01-01T02:00:00+02:00": 1767225600000,
		"2026-01-01T00:00:00Z":      1767225600000,
		"0001-01-01T00:00:00Z":      -62135596800000,
		"9999-12-31T23:59:59.999Z":  253402300799999,
	}
	for text, want := range cases {
		got, ok := parseInstant(text)
		if !ok || got != want {
			t.Errorf("%s read as %v, %v; expected %v", text, got, ok, want)
		}
	}
	for _, text := range []string{"2026-01-01", "2026-01-01T00:00:00", "2026-01-01 00:00:00Z", "1900-02-29T00:00:00Z", "2026-13-01T00:00:00Z", "2026-01-01T24:00:00Z"} {
		if _, ok := parseInstant(text); ok {
			t.Errorf("%s was read as an instant", text)
		}
	}
}

func TestAVersionIsReadByHand(t *testing.T) {
	valid := []string{"1", "1.2", "1.2.3", "1.2.3-alpha.1", "1.2.3+build", "1.0.0-0a"}
	for _, text := range valid {
		if _, ok := parseSemanticVersion(text); !ok {
			t.Errorf("%s was refused", text)
		}
	}
	for _, text := range []string{"v1.2.3", " 1.2.3", "1.2-beta", "1.2.3.4", "", "01.2.3", "1.0.0-01"} {
		if _, ok := parseSemanticVersion(text); ok {
			t.Errorf("%s was accepted", text)
		}
	}
	short, _ := parseSemanticVersion("2.1")
	full, _ := parseSemanticVersion("2.1.0")
	if compareSemanticVersions(short, full) != 0 {
		t.Error("2.1 did not equal 2.1.0")
	}
	// Build metadata is ignored, and a digit string of any length compares exactly.
	a, _ := parseSemanticVersion("1.0.0+a")
	b, _ := parseSemanticVersion("1.0.0+b")
	if compareSemanticVersions(a, b) != 0 {
		t.Error("build metadata changed equality")
	}
	big, _ := parseSemanticVersion("1.0.99999999999999999999")
	small, _ := parseSemanticVersion("1.0.99999999999999999998")
	if compareSemanticVersions(big, small) <= 0 {
		t.Error("a long numeric part did not compare exactly")
	}
}
