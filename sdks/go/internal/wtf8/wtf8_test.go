package wtf8

import "testing"

const (
	lone     = "\xed\xa0\x80"        // U+D800 on its own
	loneLow  = "\xed\xb0\x80"        // U+DC00 on its own
	astral   = "\U0001F600"          // U+1F600, surrogate pair D83D DE00 in UTF-16
	highHalf = "\xed\xa0\xbd"        // U+D83D
	lowHalf  = "\xed\xb8\x80"        // U+DE00
	bmpTop   = "\uffff"              // U+FFFF, one unit above every surrogate
	tail     = "\ufffd" + lone + "z" // mixed
)

func TestScrubFollowsWHATWG(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"valid is untouched", "a\u00e9\U0001F600", "a\u00e9\U0001F600"},
		{"lone continuation", "a\x80b", "a\ufffdb"},
		{"truncated three byte", "\xe2\x82", "\ufffd"},
		{"truncated then ascii", "\xe2\x82a", "\ufffda"},
		{"overlong", "\xc0\x80", "\ufffd\ufffd"},
		{"surrogate encoding is invalid utf-8", lone, "\ufffd\ufffd\ufffd"},
		{"beyond U+10FFFF", "\xf4\x90\x80\x80", "\ufffd\ufffd\ufffd\ufffd"},
	}
	for _, c := range cases {
		if got := string(Scrub([]byte(c.in))); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLoneSurrogates(t *testing.T) {
	if !HasLoneSurrogate(lone) || !HasLoneSurrogate("a"+loneLow) {
		t.Error("a lone surrogate was not found")
	}
	if HasLoneSurrogate("a\u00e9"+astral) || HasLoneSurrogate("") {
		t.Error("a valid string reported a lone surrogate")
	}
	if got := ToUTF8(tail); got != "\ufffd\ufffdz" {
		t.Errorf("ToUTF8 = %q", got)
	}
	if got := ToUTF8("plain"); got != "plain" {
		t.Errorf("ToUTF8 changed a valid string: %q", got)
	}
}

func TestRunesAndUnits(t *testing.T) {
	runes := Runes("a" + lone + astral)
	if len(runes) != 3 || runes[1] != 0xD800 || runes[2] != 0x1F600 {
		t.Errorf("Runes = %v", runes)
	}
	units := Units("a" + lone + astral)
	want := []uint16{'a', 0xD800, 0xD83D, 0xDE00}
	if len(units) != len(want) {
		t.Fatalf("Units = %v", units)
	}
	for i := range want {
		if units[i] != want[i] {
			t.Errorf("Units[%d] = %x, want %x", i, units[i], want[i])
		}
	}
}

func TestAppendRuneWritesSurrogatesAsWTF8(t *testing.T) {
	if got := string(AppendRune(nil, 0xD800)); got != lone {
		t.Errorf("AppendRune(D800) = %q", got)
	}
	if got := string(AppendRune(nil, 0x1F600)); got != astral {
		t.Errorf("AppendRune(1F600) = %q", got)
	}
}

func TestCompareIsUTF16Order(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"a", "b", -1},
		{"b", "a", 1},
		{"a", "a", 0},
		{"", "a", -1},
		{"ab", "a", 1},
		// Code point order puts U+FFFF before U+10000, UTF-16 order puts the pair (D800 ...) before U+FFFF.
		{astral, bmpTop, -1},
		{bmpTop, astral, 1},
		{lone, astral, -1},
		{lone, bmpTop, -1},
		{loneLow, bmpTop, -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
