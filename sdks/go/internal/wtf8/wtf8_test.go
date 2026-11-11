package wtf8

import "testing"

func TestDecodeWHATWG(t *testing.T) {
	cases := []struct {
		in   []byte
		r    rune
		size int
	}{
		{[]byte("a"), 'a', 1},
		{[]byte{0xC3, 0xA9}, 0xE9, 2},
		{[]byte{0xF0, 0x9F, 0x98, 0x80}, 0x1F600, 4},
		{[]byte{0xFF}, Replacement, 1},
		{[]byte{0xC0, 0x80}, Replacement, 1},
		{[]byte{0xE2, 0x82}, Replacement, 2},
		{[]byte{0xE2, 0x82, 'a'}, Replacement, 2},
		{[]byte{0xED, 0xA0, 0x80}, Replacement, 1},
		{[]byte{0xF0, 0x9F, 0x98}, Replacement, 3},
		{[]byte{0xF4, 0x90, 0x80, 0x80}, Replacement, 1},
	}
	for _, c := range cases {
		r, n := DecodeWHATWG(c.in)
		if r != c.r || n != c.size {
			t.Errorf("DecodeWHATWG(% x) = %U, %d; want %U, %d", c.in, r, n, c.r, c.size)
		}
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize("ok\xe2\x82x\xed\xa0\x80"); got != "ok�x\xed\xa0\x80" { // a well-formed surrogate stays
		t.Errorf("Sanitize = %q", got)
	}
}

func TestSurrogates(t *testing.T) {
	lone := string(AppendCodePoint(nil, 0xD83D))
	if len(lone) != 3 || !HasLoneSurrogate(lone) {
		t.Fatalf("lone high = % x", lone)
	}
	r, n := Decode(lone, 0)
	if r != 0xD83D || n != 3 {
		t.Errorf("Decode lone = %U, %d", r, n)
	}
	if got := ToValidUTF8("a" + lone + "b"); got != "a�b" {
		t.Errorf("ToValidUTF8 = %q", got)
	}
	if u := UTF16("\U0001F600"); len(u) != 2 || u[0] != 0xD83D || u[1] != 0xDE00 {
		t.Errorf("UTF16 pair = %x", u)
	}
	if !Contains("\U0001F600", lone) {
		t.Errorf("a lone high surrogate occurs in a pair by code units")
	}
	if !HasPrefix("\U0001F600", lone) {
		t.Errorf("prefix by code units")
	}
	if HasSuffix("\U0001F600", lone) {
		t.Errorf("suffix by code units")
	}
}

func TestCompare(t *testing.T) {
	if Compare("\U0001F600", "～") >= 0 {
		t.Errorf("a pair sorts before U+FF5E by code unit")
	}
	if Compare("B", "a") >= 0 || Compare("10", "9") >= 0 || Compare("a", "a") != 0 || Compare("b", "a") <= 0 {
		t.Errorf("ascii order")
	}
	if Compare("ab", "a") <= 0 {
		t.Errorf("a longer string with the same prefix is greater")
	}
}
