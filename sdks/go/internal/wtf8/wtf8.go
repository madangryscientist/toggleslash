// Package wtf8 holds the string helpers the SDK needs because a JSON string may carry a lone
// surrogate (docs/evaluation.md, "Reading JSON"), which Go's UTF-8 strings cannot hold.
//
// A string here is WTF-8: UTF-8, plus the three-byte encoding of a lone surrogate (U+D800 to
// U+DFFF on its own). A high surrogate followed by a low one is never two three-byte sequences:
// it is the one four-byte sequence of the supplementary code point, so two strings are the same
// UTF-16 code units exactly when they are the same bytes.
package wtf8

import (
	"strings"
	"unicode/utf8"
)

// Replacement is U+FFFD, what malformed input and a lone surrogate become where text must be UTF-8.
const Replacement = 0xFFFD

func isHigh(r rune) bool { return r >= 0xD800 && r <= 0xDBFF }
func isLow(r rune) bool  { return r >= 0xDC00 && r <= 0xDFFF }

func isCont(b byte) bool { return b&0xC0 == 0x80 }

// DecodeRune decodes the first code point of s and its width. A lone surrogate decodes to itself.
// A malformed sequence decodes to Replacement with width 1. For an empty s it returns (0, 0).
func DecodeRune(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	c := s[0]
	switch {
	case c < 0x80:
		return rune(c), 1
	case c >= 0xC2 && c <= 0xDF:
		if len(s) >= 2 && isCont(s[1]) {
			return rune(c&0x1F)<<6 | rune(s[1]&0x3F), 2
		}
	case c >= 0xE0 && c <= 0xEF:
		if len(s) >= 3 && isCont(s[1]) && isCont(s[2]) {
			r := rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F)
			if r >= 0x800 {
				return r, 3
			}
		}
	case c >= 0xF0 && c <= 0xF4:
		if len(s) >= 4 && isCont(s[1]) && isCont(s[2]) && isCont(s[3]) {
			r := rune(c&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F)
			if r >= 0x10000 && r <= 0x10FFFF {
				return r, 4
			}
		}
	}
	return Replacement, 1
}

// AppendRune appends the WTF-8 encoding of r, a lone surrogate included. A value above U+10FFFF
// or below zero becomes Replacement.
func AppendRune(dst []byte, r rune) []byte {
	switch {
	case r < 0:
		return append(dst, 0xEF, 0xBF, 0xBD)
	case r < 0x80:
		return append(dst, byte(r))
	case r < 0x800:
		return append(dst, 0xC0|byte(r>>6), 0x80|byte(r&0x3F))
	case r < 0x10000:
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6&0x3F), 0x80|byte(r&0x3F))
	case r <= 0x10FFFF:
		return append(dst, 0xF0|byte(r>>18), 0x80|byte(r>>12&0x3F), 0x80|byte(r>>6&0x3F), 0x80|byte(r&0x3F))
	}
	return append(dst, 0xEF, 0xBF, 0xBD)
}

// Sanitize returns s as well-formed WTF-8. Valid UTF-8 is returned as it is. Otherwise a malformed
// byte becomes Replacement, a lone surrogate stays, and a high surrogate directly followed by a low
// one becomes the one code point they spell.
func Sanitize(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		if isHigh(r) && i < len(s) {
			if next, m := DecodeRune(s[i:]); isLow(next) {
				r = 0x10000 + (r-0xD800)<<10 + (next - 0xDC00)
				i += m
			}
		}
		out = AppendRune(out, r)
	}
	return string(out)
}

// HasLoneSurrogate reports whether the well-formed WTF-8 string s holds a lone surrogate.
func HasLoneSurrogate(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0xED && i+1 < len(s) && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// ToUTF8 returns s with every lone surrogate replaced by U+FFFD: the bytes that get hashed.
func ToUTF8(s string) string {
	if !HasLoneSurrogate(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		if r >= 0xD800 && r <= 0xDFFF {
			b.WriteString("�")
		} else {
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// CodePoints returns the code points of s: a valid pair is one, a lone surrogate is itself.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// UTF16 returns the UTF-16 code units of s.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// Compare orders a and b by UTF-16 code unit: -1, 0 or 1. It is not code point order, which
// differs for a supplementary character against U+E000 to U+FFFF.
func Compare(a, b string) int {
	if a == b {
		return 0
	}
	ua, ub := UTF16(a), UTF16(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

// Contains reports whether v occurs in a, by UTF-16 code unit.
func Contains(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return strings.Contains(a, v)
	}
	ua, uv := UTF16(a), UTF16(v)
	for i := 0; i+len(uv) <= len(ua); i++ {
		if equalUnits(ua[i:i+len(uv)], uv) {
			return true
		}
	}
	return false
}

// HasPrefix reports whether a begins with v, by UTF-16 code unit.
func HasPrefix(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return strings.HasPrefix(a, v)
	}
	ua, uv := UTF16(a), UTF16(v)
	return len(uv) <= len(ua) && equalUnits(ua[:len(uv)], uv)
}

// HasSuffix reports whether a ends with v, by UTF-16 code unit.
func HasSuffix(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return strings.HasSuffix(a, v)
	}
	ua, uv := UTF16(a), UTF16(v)
	return len(uv) <= len(ua) && equalUnits(ua[len(ua)-len(uv):], uv)
}

func equalUnits(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
