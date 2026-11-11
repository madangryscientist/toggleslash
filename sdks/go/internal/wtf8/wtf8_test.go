package wtf8

import (
	"bytes"
	"testing"
)

const (
	loneHigh = "\xED\xA0\xBD" // U+D83D
	loneLow  = "\xED\xB8\x80" // U+DE00
	emoji    = "\U0001F600"   // U+D83D U+DE00 as a pair
)

func TestDecodeUTF8IsWHATWG(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{"\xFF", "�"},
		{"a\x80b", "a�b"},
		{"\xE2\x82", "�"},       // a truncated three-byte sequence is one replacement
		{"\xE2\x82x", "�x"},     // and the byte after it is reprocessed
		{"\xF0\x9F\x98", "�"},   // a truncated four-byte sequence
		{"\xED\xA0\x80", "���"}, // an encoded surrogate is three bad bytes
		{"\xC0\x80", "��"},      // overlong
		{"\xF4\x90\x80\x80", "����"},
	}
	for _, c := range cases {
		if got := DecodeUTF8([]byte(c.in)); got != c.want {
			t.Errorf("DecodeUTF8(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSurrogates(t *testing.T) {
	if !HasSurrogate("a"+loneHigh) || HasSurrogate("a"+emoji) {
		t.Error("HasSurrogate")
	}
	if got := ToUTF8("a" + loneHigh + "b"); got != "a�b" {
		t.Errorf("ToUTF8 = %q", got)
	}
	units := Units(nil, emoji+loneLow)
	if len(units) != 3 || units[0] != 0xD83D || units[1] != 0xDE00 || units[2] != 0xDE00 {
		t.Errorf("Units = %x", units)
	}
	if got := CodePoints("a" + loneHigh + emoji); len(got) != 3 || got[1] != 0xD83D || got[2] != 0x1F600 {
		t.Errorf("CodePoints = %x", got)
	}
	out := AppendRune(nil, 0xD83D)
	if !bytes.Equal(out, []byte(loneHigh)) {
		t.Errorf("AppendRune = %x", out)
	}
}

func TestCompareUTF16(t *testing.T) {
	// U+FFFF sorts above an astral code point in UTF-16 but below it in UTF-8.
	if CompareUTF16("￿", emoji) <= 0 {
		t.Error("U+FFFF should sort above U+1F600 by UTF-16 code unit")
	}
	if CompareUTF16("a", "b") >= 0 || CompareUTF16("b", "a") <= 0 || CompareUTF16("a", "a") != 0 {
		t.Error("ascii order")
	}
	if CompareUTF16("a", "ab") >= 0 || CompareUTF16("ab", "a") <= 0 {
		t.Error("prefix order")
	}
}

func TestJavaScriptSubstringSemantics(t *testing.T) {
	// A lone surrogate matches half of a pair, as JavaScript's includes does.
	if !Contains(emoji, loneHigh) || !Contains("x"+emoji, loneLow) {
		t.Error("Contains with a lone surrogate")
	}
	if !HasPrefix(emoji, loneHigh) || HasPrefix(emoji, loneLow) {
		t.Error("HasPrefix with a lone surrogate")
	}
	if !HasSuffix(emoji, loneLow) || HasSuffix(emoji, loneHigh) {
		t.Error("HasSuffix with a lone surrogate")
	}
	if !Contains("hello", "ell") || Contains("hello", "elx") || !HasPrefix("hello", "he") || !HasSuffix("hello", "lo") {
		t.Error("ascii")
	}
}
