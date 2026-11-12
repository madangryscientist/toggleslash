package toggleslash

import "testing"

// loneSurrogate is U+D800 on its own, as the JSON reader keeps it (WTF-8).
const loneSurrogate = "\xed\xa0\x80"

func TestTextIsComparedByUTF16CodeUnit(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"B", "a", -1},
		{"10", "9", -1},
		{"\U0001F600", "\uFF5E", -1}, // 0xD83D sorts before 0xFF5E, which code point order would reverse
		{"abc", "abc", 0},
		{"abc", "abd", -1},
	}
	for _, one := range cases {
		if got := compareText(one.left, one.right); got != one.want {
			t.Errorf("compareText(%q, %q) = %d, want %d", one.left, one.right, got, one.want)
		}
	}
}

func TestALoneSurrogateIsItsOwnCharacter(t *testing.T) {
	if !sameText(loneSurrogate, loneSurrogate) {
		t.Error("a lone surrogate did not equal itself")
	}
	if sameText(loneSurrogate, "\uFFFD") {
		t.Error("a lone surrogate equalled U+FFFD")
	}
	if !containsText("a"+loneSurrogate+"b", loneSurrogate) {
		t.Error("a lone surrogate was not found in a string")
	}
	if compareText(loneSurrogate, "\uFFFD") >= 0 {
		t.Error("a lone surrogate should sort below U+FFFD")
	}
	// Hashing is the one place a lone surrogate becomes U+FFFD.
	if got := hashText(nil, loneSurrogate); string(got) != "\uFFFD" {
		t.Errorf("hashing a lone surrogate gave % x, expected the bytes of U+FFFD", got)
	}
}

func TestTheWorkedBucketExamples(t *testing.T) {
	if got := uint32Of("checkout", "a1b2c3", "user-123"); got != 498645874 {
		t.Errorf("uint32 was %d, expected 498645874", got)
	}
	if got := bucketOf("checkout", "a1b2c3", "user-123"); got != 45874 {
		t.Errorf("bucket was %d, expected 45874", got)
	}
	if got := bucketOf("checkout", "a1b2c3", "org:acme"); got != 82893 {
		t.Errorf("the org bucket was %d, expected 82893", got)
	}
}
