package wtf8

import (
	"testing"
	"unicode/utf16"
)

func TestDecodeLossy(t *testing.T) {
	cases := map[string]string{
		"ok":                "ok",
		"a\xffb":            "a�b",
		"\xED\xA0\x80":      "���", // a surrogate written as bytes is invalid
		"\xE2\x82":          "�",   // a truncated sequence is one replacement
		"\xF0\x9F\x98\x80":  "\U0001F600",
		"\xC0\x80":          "��",
		"\xF4\x90\x80\x80x": "����x",
	}
	for in, want := range cases {
		if got := DecodeLossy([]byte(in)); got != want {
			t.Errorf("DecodeLossy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodePointsAndUnits(t *testing.T) {
	s := "a\U0001F600" + string(AppendRune(nil, 0xD800))
	cps := CodePoints(s)
	if len(cps) != 3 || cps[1] != 0x1F600 || cps[2] != 0xD800 {
		t.Errorf("CodePoints = %x", cps)
	}
	units := UTF16(s)
	want := append(utf16.Encode([]rune("a\U0001F600")), 0xD800)
	if len(units) != len(want) {
		t.Fatalf("UTF16 = %x, want %x", units, want)
	}
	for i := range units {
		if units[i] != want[i] {
			t.Errorf("unit %d = %x, want %x", i, units[i], want[i])
		}
	}
}

func TestCompareIsUTF16Order(t *testing.T) {
	// U+FF5E sorts above U+1F600 by byte but below it by UTF-16 code unit (0xD83D < 0xFF5E).
	a, b := "～", "\U0001F600"
	if Compare(a, b) <= 0 {
		t.Errorf("Compare(%q, %q) should be positive in UTF-16 order", a, b)
	}
	if Compare("a", "b") >= 0 || Compare("b", "b") != 0 {
		t.Error("ASCII order")
	}
}

func TestSubstringsByUnit(t *testing.T) {
	pair := "x\U0001F600"
	high := string(AppendRune(nil, 0xD83D))
	if !Contains(pair, high) {
		t.Error("a lone high surrogate is inside the pair in UTF-16")
	}
	if HasSuffix(pair, high) || !HasPrefix(pair, "x") || !HasSuffix(pair, "\U0001F600") {
		t.Error("prefix and suffix")
	}
}

func TestToUTF8(t *testing.T) {
	s := "a" + string(AppendRune(nil, 0xDC00)) + "b"
	if got := ToUTF8(s); got != "a�b" {
		t.Errorf("ToUTF8 = %q", got)
	}
}
