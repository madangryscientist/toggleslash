// Package wtf8 holds the string helpers the SDK needs because the evaluation
// spec is written over UTF-16 strings that may hold a lone surrogate.
//
// A Go string here is WTF-8: well-formed UTF-8, plus the three-byte form of a
// lone surrogate (U+D800 to U+DFFF). A pair of surrogates is always stored as
// the one four-byte code point they stand for.
package wtf8

import (
	"strings"
	"unicode/utf8"
)

// Replacement is U+FFFD.
const Replacement = '�'

// IsSurrogate reports whether r is in U+D800 to U+DFFF.
func IsSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// DecodeRune decodes the first code point of s. It reads a three-byte surrogate
// as that surrogate, which utf8.DecodeRuneInString refuses. An invalid byte is
// returned as U+FFFD with width 1.
func DecodeRune(s string) (rune, int) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// AppendRune appends the WTF-8 form of r. A surrogate is written in three bytes.
func AppendRune(dst []byte, r rune) []byte {
	if IsSurrogate(r) {
		return append(dst, 0xED, 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// HasSurrogate reports whether s holds a lone surrogate.
func HasSurrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// CodePoints returns the code points of s; a lone surrogate is one code point.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for len(s) > 0 {
		r, n := DecodeRune(s)
		out = append(out, r)
		s = s[n:]
	}
	return out
}

// Units appends the UTF-16 code units of s.
func Units(dst []uint16, s string) []uint16 {
	for len(s) > 0 {
		r, n := DecodeRune(s)
		s = s[n:]
		if r >= 0x10000 {
			r -= 0x10000
			dst = append(dst, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			dst = append(dst, uint16(r))
		}
	}
	return dst
}

// unit returns the first UTF-16 code unit of the code point at the start of s,
// and the second (zero if there is none).
func unit(s string) (first, second uint16, width int) {
	r, n := DecodeRune(s)
	if r >= 0x10000 {
		r -= 0x10000
		return uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF)), n
	}
	return uint16(r), 0, n
}

// CompareUTF16 orders a and b by UTF-16 code unit, as a JavaScript string
// comparison does. WTF-8 byte order is code point order, which differs from it
// for U+E000 to U+FFFF against an astral code point, so this walks the strings.
func CompareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if a[0] == b[0] && a[0] < 0x80 {
			a, b = a[1:], b[1:]
			continue
		}
		a1, a2, an := unit(a)
		b1, b2, bn := unit(b)
		if a1 != b1 {
			if a1 < b1 {
				return -1
			}
			return 1
		}
		if a2 != b2 {
			if a2 < b2 {
				return -1
			}
			return 1
		}
		a, b = a[an:], b[bn:]
	}
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return -1
	default:
		return 1
	}
}

// DecodeUTF8 decodes bytes as the WHATWG Encoding standard's UTF-8 decoder does:
// one U+FFFD for each maximal invalid subpart. The result is well-formed UTF-8.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); {
		c := b[i]
		if c <= 0x7F {
			out = append(out, c)
			i++
			continue
		}
		var needed int
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			needed = 1
		case c >= 0xE0 && c <= 0xEF:
			needed = 2
			if c == 0xE0 {
				lower = 0xA0
			}
			if c == 0xED {
				upper = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			needed = 3
			if c == 0xF0 {
				lower = 0x90
			}
			if c == 0xF4 {
				upper = 0x8F
			}
		default:
			out = utf8.AppendRune(out, Replacement)
			i++
			continue
		}
		start := i
		i++
		ok := true
		for j := 0; j < needed; j++ {
			if i >= len(b) || b[i] < lower || b[i] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			i++
		}
		if ok {
			out = append(out, b[start:i]...)
		} else {
			out = utf8.AppendRune(out, Replacement)
		}
	}
	return string(out)
}

// ToUTF8 returns s with each lone surrogate replaced by U+FFFD, which is what
// a UTF-8 encoder (JavaScript's TextEncoder) writes for one.
func ToUTF8(s string) string {
	if !HasSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for len(s) > 0 {
		r, n := DecodeRune(s)
		if IsSurrogate(r) {
			r = Replacement
		}
		out = utf8.AppendRune(out, r)
		s = s[n:]
	}
	return string(out)
}

// Contains, HasPrefix and HasSuffix compare as JavaScript's includes,
// startsWith and endsWith do, on UTF-16 code units. Bytes agree with that for
// well-formed strings, so the unit walk runs only when a lone surrogate is
// involved (a lone surrogate can match half of a pair).

// Contains reports whether sub is within s.
func Contains(s, sub string) bool {
	if !HasSurrogate(s) && !HasSurrogate(sub) {
		return strings.Contains(s, sub)
	}
	return indexUnits(Units(nil, s), Units(nil, sub)) >= 0
}

// HasPrefix reports whether s begins with prefix.
func HasPrefix(s, prefix string) bool {
	if !HasSurrogate(s) && !HasSurrogate(prefix) {
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	a, b := Units(nil, s), Units(nil, prefix)
	return len(a) >= len(b) && equalUnits(a[:len(b)], b)
}

// HasSuffix reports whether s ends with suffix.
func HasSuffix(s, suffix string) bool {
	if !HasSurrogate(s) && !HasSurrogate(suffix) {
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	a, b := Units(nil, s), Units(nil, suffix)
	return len(a) >= len(b) && equalUnits(a[len(a)-len(b):], b)
}

func equalUnits(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexUnits(s, sub []uint16) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalUnits(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
