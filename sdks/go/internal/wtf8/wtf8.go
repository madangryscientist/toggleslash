// Package wtf8 holds the string helpers the SDK needs because the spec's strings are UTF-16 sequences that
// may contain a lone surrogate, which Go's UTF-8 strings cannot hold.
//
// A string here is WTF-8: UTF-8 in which a lone surrogate (U+D800 to U+DFFF with no partner) is written as
// its three-byte generalized form (ED A0 80 to ED BF BF). A valid surrogate pair is always written as the
// one four-byte sequence of its code point, so two WTF-8 strings are equal exactly when the UTF-16
// sequences they stand for are equal.
package wtf8

import "unicode/utf8"

const replacement = 0xFFFD

// isSurrogate reports whether r is in the surrogate range.
func isSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// Append appends the WTF-8 encoding of the code point r to b. A surrogate is written in its three-byte form.
func Append(b []byte, r rune) []byte {
	if isSurrogate(r) {
		return append(b, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
	}
	return utf8.AppendRune(b, r)
}

// decodeAt decodes the code point that starts at s[i] and returns it with its width in bytes. Input that is
// not WTF-8 decodes as U+FFFD one byte at a time, which only happens for a string built by hand.
func decodeAt(s string, i int) (rune, int) {
	c := s[i]
	if c < 0x80 {
		return rune(c), 1
	}
	if c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80 {
		return rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	return r, size
}

// CodePoints returns the code points of s. A lone surrogate is one code point with its own value.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, size := decodeAt(s, i)
		out = append(out, r)
		i += size
	}
	return out
}

// plain reports whether every byte of s is below 0xED. Such a string has no surrogate (no lone one and no
// pair, which would be a code point above U+FFFF) and none of U+E000 and above, so byte order, byte
// equality and byte search agree with UTF-16 code unit order, equality and search.
func plain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xED {
			return false
		}
	}
	return true
}

// UTF16 returns the UTF-16 code units of s.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, size := decodeAt(s, i)
		i += size
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// Compare orders two strings by UTF-16 code unit, which is the ordinal order of the spec: -1, 0 or 1.
func Compare(a, b string) int {
	if plain(a) && plain(b) {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		}
		return 0
	}
	x, y := UTF16(a), UTF16(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(x) < len(y):
		return -1
	case len(x) > len(y):
		return 1
	}
	return 0
}

func indexUnits(x, y []uint16) int {
	for i := 0; i+len(y) <= len(x); i++ {
		match := true
		for j := range y {
			if x[i+j] != y[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Contains reports whether the UTF-16 units of sub occur in those of s. An empty sub occurs in every s.
func Contains(s, sub string) bool {
	if plain(s) && plain(sub) {
		return containsBytes(s, sub)
	}
	return indexUnits(UTF16(s), UTF16(sub)) >= 0
}

func containsBytes(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// HasPrefix reports whether the UTF-16 units of s begin with those of prefix.
func HasPrefix(s, prefix string) bool {
	if plain(s) && plain(prefix) {
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	x, y := UTF16(s), UTF16(prefix)
	if len(x) < len(y) {
		return false
	}
	return indexUnits(x[:len(y)], y) == 0
}

// HasSuffix reports whether the UTF-16 units of s end with those of suffix.
func HasSuffix(s, suffix string) bool {
	if plain(s) && plain(suffix) {
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	x, y := UTF16(s), UTF16(suffix)
	if len(x) < len(y) {
		return false
	}
	return indexUnits(x[len(x)-len(y):], y) == 0
}

// ToUTF8 returns s as well-formed UTF-8, with each lone surrogate replaced by U+FFFD. This is the form the
// spec hashes ("Bucketing", edge cases).
func ToUTF8(s string) string {
	found := false
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			found = true
			break
		}
	}
	if !found {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := decodeAt(s, i)
		i += size
		if isSurrogate(r) {
			r = replacement
		}
		out = utf8.AppendRune(out, r)
	}
	return string(out)
}

// Valid reports whether s is well-formed WTF-8.
func Valid(s string) bool {
	for i := 0; i < len(s); {
		r, size := decodeAt(s, i)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		i += size
	}
	return true
}

// Repair decodes bytes as UTF-8 the way the WHATWG Encoding standard does: each maximal invalid subpart
// becomes one U+FFFD. A surrogate written in UTF-8 form is invalid, so it is repaired too.
func Repair(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	var need int
	lower, upper := byte(0x80), byte(0xBF)
	var cp rune
	var seen int
	var pending []byte
	flushInvalid := func() {
		out = utf8.AppendRune(out, replacement)
		need, seen, cp = 0, 0, 0
		pending = pending[:0]
		lower, upper = 0x80, 0xBF
	}
	for i := 0; i < len(b); {
		c := b[i]
		if need == 0 {
			switch {
			case c < 0x80:
				out = append(out, c)
			case c >= 0xC2 && c <= 0xDF:
				need, cp = 1, rune(c&0x1F)
				pending = append(pending[:0], c)
			case c >= 0xE0 && c <= 0xEF:
				switch c {
				case 0xE0:
					lower = 0xA0
				case 0xED:
					upper = 0x9F
				}
				need, cp = 2, rune(c&0x0F)
				pending = append(pending[:0], c)
			case c >= 0xF0 && c <= 0xF4:
				switch c {
				case 0xF0:
					lower = 0x90
				case 0xF4:
					upper = 0x8F
				}
				need, cp = 3, rune(c&0x07)
				pending = append(pending[:0], c)
			default:
				out = utf8.AppendRune(out, replacement)
			}
			i++
			continue
		}
		if c < lower || c > upper {
			// The sequence is cut short: one U+FFFD for what was read, and this byte is read again.
			flushInvalid()
			continue
		}
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		pending = append(pending, c)
		seen++
		i++
		if seen == need {
			out = append(out, pending...)
			need, seen, cp = 0, 0, 0
			pending = pending[:0]
		}
	}
	if need != 0 {
		out = utf8.AppendRune(out, replacement)
	}
	return string(out)
}
