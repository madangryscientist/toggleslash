package wtf8

import (
	"slices"
	"testing"
)

// lone is a string holding one lone high surrogate, U+D83D, as WTF-8.
var lone = string(AppendRune(nil, 0xD83D))

func TestLoneSurrogateIsKept(t *testing.T) {
	if len(lone) != 3 || !HasSurrogate(lone) {
		t.Fatalf("lone surrogate = %q", lone)
	}
	if got := CodePoints("a" + lone + "b"); !slices.Equal(got, []rune{'a', 0xD83D, 'b'}) {
		t.Fatalf("code points = %x", got)
	}
	if got := UTF16(lone); !slices.Equal(got, []uint16{0xD83D}) {
		t.Fatalf("utf16 = %x", got)
	}
	if got := ToUTF8(lone); got != "�" {
		t.Fatalf("to utf8 = %q", got)
	}
}

func TestFromGoReplacesInvalidBytes(t *testing.T) {
	if got := FromGo("a\xffb"); got != "a�b" {
		t.Fatalf("got %q", got)
	}
}

func TestCompareIsByUTF16Unit(t *testing.T) {
	// U+FF5E is one unit 0xFF5E; U+1F600 is the pair D83D DE00. By unit the emoji sorts first, by
	// UTF-8 byte or code point it sorts last.
	if Compare("\U0001F600", "～") >= 0 {
		t.Error("the emoji should sort before U+FF5E by UTF-16 unit")
	}
	if Compare(lone, "～") >= 0 || Compare("a", "a") != 0 || Compare("b", "a") <= 0 {
		t.Error("compare order wrong")
	}
}

func TestSubstringOperationsAreByUnit(t *testing.T) {
	if !Contains("x"+lone+"y", lone) || Contains("xy", lone) {
		t.Error("contains with a surrogate needle")
	}
	// A lone high surrogate is the first unit of an emoji, so it is a prefix of it by unit.
	if !HasPrefix("\U0001F600", lone) {
		t.Error("a high surrogate is a prefix of the pair it starts")
	}
	if HasSuffix("\U0001F600", lone) {
		t.Error("a high surrogate is not a suffix of a pair")
	}
	if !HasSuffix("ab", "b") || !HasPrefix("ab", "a") || !Contains("abc", "") {
		t.Error("plain ordinal operations")
	}
}
