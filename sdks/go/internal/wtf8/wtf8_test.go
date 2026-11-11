package wtf8

import (
	"reflect"
	"testing"
)

func TestLoneSurrogateSurvives(t *testing.T) {
	s := string(AppendRune(nil, 0xD83D)) // a lone high surrogate, held as WTF-8
	if len(s) != 3 {
		t.Fatalf("encoded length = %d", len(s))
	}
	r, n := DecodeRune(s)
	if r != 0xD83D || n != 3 {
		t.Errorf("decoded %U (%d bytes)", r, n)
	}
	if got := UTF16(s); !reflect.DeepEqual(got, []uint16{0xD83D}) {
		t.Errorf("utf16 = %x", got)
	}
	if got := ReplaceLone(s); got != "�" {
		t.Errorf("replaced = %q", got)
	}
}

func TestSupplementaryIsOneCodePointAndTwoUnits(t *testing.T) {
	s := "a\U0001F600"
	if got := CodePoints(s); !reflect.DeepEqual(got, []rune{'a', 0x1F600}) {
		t.Errorf("code points = %U", got)
	}
	if got := UTF16(s); !reflect.DeepEqual(got, []uint16{'a', 0xD83D, 0xDE00}) {
		t.Errorf("utf16 = %x", got)
	}
}

func TestStripBOMAndDecodeLossy(t *testing.T) {
	if got := StripBOM([]byte("\xef\xbb\xbfx")); string(got) != "x" {
		t.Errorf("strip = %q", got)
	}
	if got := StripBOM([]byte("x")); string(got) != "x" {
		t.Errorf("strip = %q", got)
	}
	// WHATWG replacement: one U+FFFD per maximal invalid subpart.
	if got := DecodeLossy([]byte("a\xffb\xe2\x82c")); string(got) != "a�b�c" {
		t.Errorf("lossy = %q", got)
	}
}
