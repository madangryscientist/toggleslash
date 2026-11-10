package wtf8

import "testing"

func TestCompareUsesUTF16Units(t *testing.T) {
	// U+10000 is the units D800 DC00, so it sorts below U+E000 even though its code point is higher.
	if Compare("\U00010000", "\uE000") >= 0 {
		t.Error("supplementary should sort below U+E000 by UTF-16 unit")
	}
	if Compare("a", "a") != 0 || Compare("a", "b") >= 0 || Compare("b", "a") <= 0 || Compare("a", "ab") >= 0 {
		t.Error("basic order")
	}
}

func TestLoneSurrogate(t *testing.T) {
	lone := "\xed\xa0\x80"
	if !HasLoneSurrogate(lone) || HasLoneSurrogate("\U0001F600") {
		t.Error("HasLoneSurrogate")
	}
	if ToUTF8(lone+"a") != "\uFFFDa" {
		t.Error("ToUTF8")
	}
	if got := CodePoints("a" + lone); len(got) != 2 || got[1] != 0xD800 {
		t.Errorf("CodePoints %v", got)
	}
	if !Contains("x"+lone+"y", lone) || !HasPrefix(lone+"y", lone) || !HasSuffix("y"+lone, lone) {
		t.Error("unit-based search")
	}
	// A lone high surrogate matches the first half of a pair, by UTF-16 unit.
	if !Contains("\U00010000", lone) || !HasPrefix("\U00010000", lone) {
		t.Error("lone high surrogate should match the first unit of a pair")
	}
}

func TestSanitize(t *testing.T) {
	if Sanitize("a\xffb") != "a\uFFFDb" {
		t.Error("invalid byte")
	}
	// A surrogate pair written as two three-byte sequences becomes one code point.
	if Sanitize("\xed\xa0\xbd\xed\xb8\x80") != "\U0001F600" {
		t.Error("pair")
	}
}
