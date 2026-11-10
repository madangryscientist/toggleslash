// Package wtf8 holds the string operations the evaluator needs on WTF-8: UTF-8 in which a lone UTF-16
// surrogate is kept as the three bytes UTF-8 forbids for it (ED A0..BF xx). Every string the SDK reads
// from JSON is in this form, and a valid surrogate pair is always the one four-byte sequence, so byte
// equality is UTF-16 code unit equality. The spec compares by UTF-16 code unit and matches by code point,
// and Go's own string functions know neither, so this package supplies both.
package wtf8

import (
	"strings"
	"unicode/utf8"
)

// replacement is U+FFFD.
const replacement = "�"

func isSurrogateLead(s string, i int) bool {
	return s[i] == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80
}

// HasSurrogate reports whether s holds a lone surrogate.
func HasSurrogate(s string) bool {
	for i := 0; ; {
		at := strings.IndexByte(s[i:], 0xED)
		if at < 0 {
			return false
		}
		i += at
		if isSurrogateLead(s, i) {
			return true
		}
		i++
	}
}

// Next decodes the code point at byte offset i and its length in bytes. A lone surrogate is its own code
// point. Bytes that are not WTF-8 decode as U+FFFD of length 1, which a canonical string never holds.
func Next(s string, i int) (rune, int) {
	c := s[i]
	if c < utf8.RuneSelf {
		return rune(c), 1
	}
	if isSurrogateLead(s, i) {
		return 0xD000 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s[i:])
}

// AppendRune appends the WTF-8 form of cp: a surrogate as three bytes, anything else as UTF-8.
func AppendRune(dst []byte, cp rune) []byte {
	if cp >= 0xD800 && cp <= 0xDFFF {
		return append(dst, 0xE0|byte(cp>>12), 0x80|byte(cp>>6)&0x3F, 0x80|byte(cp)&0x3F)
	}
	return utf8.AppendRune(dst, cp)
}

// FromGo makes a Go string canonical: bytes that are not valid UTF-8 become U+FFFD.
func FromGo(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, replacement)
}

// CodePoints returns the code points of s; a lone surrogate is a code point of its own.
func CodePoints(s string) []rune {
	points := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		cp, n := Next(s, i)
		points = append(points, cp)
		i += n
	}
	return points
}

// UTF16 returns the UTF-16 code units of s.
func UTF16(s string) []uint16 {
	units := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		cp, n := Next(s, i)
		i += n
		if cp >= 0x10000 {
			cp -= 0x10000
			units = append(units, uint16(0xD800+(cp>>10)), uint16(0xDC00+(cp&0x3FF)))
		} else {
			units = append(units, uint16(cp))
		}
	}
	return units
}

// ToUTF8 is s with each lone surrogate replaced by U+FFFD, which is what encoding to UTF-8 does.
func ToUTF8(s string) string {
	if !HasSurrogate(s) {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		if isSurrogateLead(s, i) {
			out.WriteString(replacement)
			i += 3
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func firstUnit(cp rune) int {
	if cp >= 0x10000 {
		return 0xD800 + int((cp-0x10000)>>10)
	}
	return int(cp)
}

// Compare orders a and b by UTF-16 code unit: -1, 0 or 1.
func Compare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, na := Next(a, i)
		cb, nb := Next(b, j)
		if ca != cb {
			if ua, ub := firstUnit(ca), firstUnit(cb); ua != ub {
				if ua < ub {
					return -1
				}
				return 1
			}
			// The same high surrogate: the low surrogates differ.
			if ca < cb {
				return -1
			}
			return 1
		}
		i += na
		j += nb
	}
	switch {
	case i < len(a):
		return 1
	case j < len(b):
		return -1
	}
	return 0
}

// Contains is JavaScript's includes: a match in UTF-16 code units.
func Contains(s, sub string) bool {
	if !HasSurrogate(sub) {
		return strings.Contains(s, sub)
	}
	return indexUnits(UTF16(s), UTF16(sub)) >= 0
}

// HasPrefix is JavaScript's startsWith.
func HasPrefix(s, prefix string) bool {
	if !HasSurrogate(prefix) {
		return strings.HasPrefix(s, prefix)
	}
	u, p := UTF16(s), UTF16(prefix)
	return len(p) <= len(u) && equalUnits(u[:len(p)], p)
}

// HasSuffix is JavaScript's endsWith.
func HasSuffix(s, suffix string) bool {
	if !HasSurrogate(suffix) {
		return strings.HasSuffix(s, suffix)
	}
	u, p := UTF16(s), UTF16(suffix)
	return len(p) <= len(u) && equalUnits(u[len(u)-len(p):], p)
}

func equalUnits(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}

func indexUnits(s, sub []uint16) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalUnits(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
