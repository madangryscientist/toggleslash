// Package wtf8 holds strings as WTF-8: valid UTF-8, plus a lone UTF-16 surrogate written as the
// three-byte form it would have if surrogates were encodable (ED A0..BF xx). The spec keeps a lone
// surrogate as it is in a context key, an attribute or a clause value, and Go strings are bytes, so
// this is how one survives. A surrogate pair is always the one four-byte code point, so equal UTF-16
// code units mean equal bytes.
package wtf8

import "unicode/utf8"

const replacement = "�"

// AppendRune appends r, which may be a surrogate (U+D800 to U+DFFF), as WTF-8.
func AppendRune(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xED, byte(0x80|(r>>6)&0x3F), byte(0x80|r&0x3F))
	}
	return utf8.AppendRune(dst, r)
}

// isLone reports whether s starts with the three bytes of a lone surrogate.
func isLone(s string) bool {
	return len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80
}

// DecodeRune returns the first code point of s and its width. A lone surrogate decodes to itself.
func DecodeRune(s string) (rune, int) {
	if isLone(s) {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// CodePoints returns the code points of s. A lone surrogate is one code point with its own value.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// UTF16 returns the UTF-16 code units of s, the unit the spec orders and compares strings by.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// ReplaceLone returns s with each lone surrogate replaced by U+FFFD, which is the UTF-8 the spec
// hashes.
func ReplaceLone(s string) string {
	first := -1
	for i := 0; i+2 < len(s); i++ {
		if isLone(s[i:]) {
			first = i
			break
		}
	}
	if first < 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	out = append(out, s[:first]...)
	for i := first; i < len(s); {
		if isLone(s[i:]) {
			out = append(out, replacement...)
			i += 3
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

// StripBOM removes one leading UTF-8 byte order mark.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// DecodeLossy decodes UTF-8 the way the WHATWG Encoding Standard does: each maximal invalid
// subpart becomes one U+FFFD. A byte sequence that spells a surrogate is invalid, so the result is
// plain UTF-8.
func DecodeLossy(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	out := make([]byte, 0, len(b)+8)
	var (
		cp           rune
		needed, seen int
		lower, upper byte = 0x80, 0xBF
		reset             = func() { cp, needed, seen, lower, upper = 0, 0, 0, 0x80, 0xBF }
	)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if needed == 0 {
			switch {
			case c <= 0x7F:
				out = append(out, c)
			case c >= 0xC2 && c <= 0xDF:
				needed, cp = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				if c == 0xE0 {
					lower = 0xA0
				}
				if c == 0xED {
					upper = 0x9F
				}
				needed, cp = 2, rune(c&0x0F)
			case c >= 0xF0 && c <= 0xF4:
				if c == 0xF0 {
					lower = 0x90
				}
				if c == 0xF4 {
					upper = 0x8F
				}
				needed, cp = 3, rune(c&0x07)
			default:
				out = append(out, replacement...)
			}
			continue
		}
		if c < lower || c > upper {
			reset()
			out = append(out, replacement...)
			i--
			continue
		}
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		seen++
		if seen == needed {
			out = utf8.AppendRune(out, cp)
			reset()
		}
	}
	if needed != 0 {
		out = append(out, replacement...)
	}
	return out
}
