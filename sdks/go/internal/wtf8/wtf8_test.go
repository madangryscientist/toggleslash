package wtf8

import (
	"slices"
	"testing"
)

const (
	hi = "\xed\xa0\x80" // U+D800
	lo = "\xed\xb0\x80" // U+DC00
)

func TestSurrogateRoundTrip(t *testing.T) {
	s := "a" + hi + "b"
	points := CodePoints(s)
	if !slices.Equal(points, []rune{'a', 0xD800, 'b'}) {
		t.Fatalf("code points: %x", points)
	}
	if got := FromCodePoints(points); got != s {
		t.Fatalf("round trip: %q", got)
	}
	if !HasSurrogate(s) || HasSurrogate("abc\U0001F600") {
		t.Fatal("HasSurrogate")
	}
}

func TestToUTF8ReplacesLoneSurrogates(t *testing.T) {
	if got := ToUTF8("a" + hi + "b" + lo); got != "a\uFFFDb\uFFFD" {
		t.Fatalf("got %q", got)
	}
}

func TestUnits(t *testing.T) {
	got := Units("\U0001F600" + hi)
	if !slices.Equal(got, []uint16{0xD83D, 0xDE00, 0xD800}) {
		t.Fatalf("units: %x", got)
	}
	if FromUnits(got) != "\U0001F600"+hi {
		t.Fatal("FromUnits")
	}
	// A pair split across the end is joined, a reversed pair is two lone surrogates.
	if FromUnits([]uint16{0xDE00, 0xD83D}) != "\xed\xb8\x80\xed\xa0\xbd" {
		t.Fatalf("reversed pair: %q", FromUnits([]uint16{0xDE00, 0xD83D}))
	}
}

func TestCompareIsByUTF16Unit(t *testing.T) {
	// By code point U+FF5E sorts before U+1F600; by UTF-16 unit the pair starts with 0xD83D, so it sorts first.
	if Compare("\U0001F600", "\uFF5E") >= 0 {
		t.Fatal("a supplementary character must sort before U+FF5E by UTF-16 unit")
	}
	if Compare("a", "a") != 0 || Compare("a", "b") >= 0 || Compare("b", "a") <= 0 || Compare("a", "ab") >= 0 {
		t.Fatal("ordinary order")
	}
	if Compare("x"+hi, "x"+lo) >= 0 {
		t.Fatal("lone surrogates compare by value")
	}
}

func TestSubstringChecks(t *testing.T) {
	s := "x" + hi + "y"
	for _, c := range []struct {
		name string
		got  bool
		want bool
	}{
		{"contains lone", Contains(s, hi), true},
		{"contains absent lone", Contains(s, lo), false},
		{"prefix lone", HasPrefix(hi+"y", hi), true},
		{"suffix lone", HasSuffix(s, hi+"y"), true},
		{"suffix absent", HasSuffix(s, "x"), false},
		{"empty needle", Contains(s, ""), true},
		{"plain contains", Contains("hello", "ell"), true},
		{"pair never matches half", Contains("\U0001F600", hi), false},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %v", c.name, c.got)
		}
	}
}

func TestDecodeWHATWG(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"abc", "abc"},
		{"\xff", "\uFFFD"},
		{"\xe2\x82", "\uFFFD"},                 // a truncated three-byte sequence is one error
		{"\xe2\x28\xa1", "\uFFFD(\uFFFD"},      // the bad second byte is reprocessed
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"}, // an encoded surrogate is three errors
		{"\xf0\x9f\x98\x80", "\U0001F600"},
		{"\xf0\x9f\x98", "\uFFFD"},
		{"\xc0\x80", "\uFFFD\uFFFD"},
	} {
		if got := DecodeWHATWG([]byte(c.in)); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}
