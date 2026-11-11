// Package wtf8 holds the string rules the SDK needs and Go's strings do not give.
//
// A string here is WTF-8: UTF-8, except that a lone UTF-16 surrogate (U+D800 to U+DFFF on its own) is
// kept, as the three bytes UTF-8 would use for it. A valid pair is always the one four-byte code point,
// so a string has one spelling and byte equality is code point equality. The spec orders strings and
// finds substrings by UTF-16 code unit, which WTF-8 bytes do not do, so this package does.
package wtf8

import (
	"strings"
	"unicode/utf8"
)

const replacement = '�'

// Scrub decodes b as the WHATWG UTF-8 decoder does: each maximal invalid subpart becomes one U+FFFD.
// The result is valid UTF-8 (so it holds no surrogate). b itself is returned when it is already valid.
func Scrub(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	out := make([]byte, 0, len(b)+8)
	var (
		cp           rune
		needed, seen int
		lower, upper byte = 0x80, 0xBF
	)
	reset := func() {
		cp, needed, seen = 0, 0, 0
		lower, upper = 0x80, 0xBF
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if needed == 0 {
			switch {
			case c <= 0x7F:
				out = append(out, c)
			case c >= 0xC2 && c <= 0xDF:
				needed, cp = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				switch c {
				case 0xE0:
					lower = 0xA0
				case 0xED:
					upper = 0x9F
				}
				needed, cp = 2, rune(c&0x0F)
			case c >= 0xF0 && c <= 0xF4:
				switch c {
				case 0xF0:
					lower = 0x90
				case 0xF4:
					upper = 0x8F
				}
				needed, cp = 3, rune(c&0x07)
			default:
				out = utf8.AppendRune(out, replacement)
			}
			continue
		}
		if c < lower || c > upper {
			reset()
			out = utf8.AppendRune(out, replacement)
			i-- // the byte is read again as the start of something
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
		out = utf8.AppendRune(out, replacement)
	}
	return out
}

// FromGo makes a Go string, which may hold invalid UTF-8, into a string of this package.
func FromGo(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(Scrub([]byte(s)))
}

// AppendRune appends r as UTF-8, or as WTF-8 when r is a surrogate.
func AppendRune(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// isSurrogateAt reports whether s[i:] starts a three-byte surrogate (ED A0..BF xx).
func isSurrogateAt(s string, i int) bool {
	return i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80
}

// DecodeRune returns the code point at s[i:] and its width in bytes. A lone surrogate is returned as
// itself. Anything that is not WTF-8 is utf8.RuneError with width 1 (no string made here holds any).
func DecodeRune(s string, i int) (rune, int) {
	if isSurrogateAt(s, i) {
		return rune(s[i]&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s[i:])
}

// Runes returns the code points of s. A lone surrogate is one code point with its own value.
func Runes(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s, i)
		out = append(out, r)
		i += n
	}
	return out
}

// HasLoneSurrogate reports whether s holds a lone surrogate.
func HasLoneSurrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && isSurrogateAt(s, i) {
			return true
		}
	}
	return false
}

// ToUTF8 returns s with each lone surrogate replaced by U+FFFD: the bytes the spec hashes.
func ToUTF8(s string) string {
	if !HasLoneSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if isSurrogateAt(s, i) {
			out = utf8.AppendRune(out, replacement)
			i += 3
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

// Units returns s as UTF-16 code units.
func Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s, i)
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

// Compare orders a and b by UTF-16 code unit: -1, 0 or 1. Code point order differs from it for a
// supplementary code point against U+E000 to U+FFFF, which this gets right.
func Compare(a, b string) int {
	// The common prefix in bytes is the same in units, so start at the first byte that differs.
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if i == len(a) && i == len(b) {
		return 0
	}
	// Back up to the start of the code point holding the difference.
	for i > 0 && i < len(a) && a[i]&0xC0 == 0x80 {
		i--
	}
	au, bu := Units(a[i:]), Units(b[i:])
	for k := 0; k < len(au) && k < len(bu); k++ {
		if au[k] != bu[k] {
			if au[k] < bu[k] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(au) < len(bu):
		return -1
	case len(au) > len(bu):
		return 1
	}
	return 0
}

// Contains reports whether sub occurs in s, by UTF-16 code unit.
func Contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	if !HasLoneSurrogate(sub) {
		return strings.Contains(s, sub)
	}
	return indexUnits(Units(s), Units(sub)) >= 0
}

// HasPrefix reports whether s begins with p, by UTF-16 code unit.
func HasPrefix(s, p string) bool {
	if !HasLoneSurrogate(p) {
		return len(s) >= len(p) && s[:len(p)] == p
	}
	su, pu := Units(s), Units(p)
	if len(pu) > len(su) {
		return false
	}
	return unitsEqual(su[:len(pu)], pu)
}

// HasSuffix reports whether s ends with x, by UTF-16 code unit.
func HasSuffix(s, x string) bool {
	if !HasLoneSurrogate(x) {
		return len(s) >= len(x) && s[len(s)-len(x):] == x
	}
	su, xu := Units(s), Units(x)
	if len(xu) > len(su) {
		return false
	}
	return unitsEqual(su[len(su)-len(xu):], xu)
}

func unitsEqual(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexUnits(s, sub []uint16) int {
	table := failure(len(sub), func(i int) uint16 { return sub[i] })
	return search(len(s), len(sub), func(i int) uint16 { return s[i] }, func(i int) uint16 { return sub[i] }, table)
}

// failure builds the Knuth-Morris-Pratt table.
func failure(n int, at func(int) uint16) []int {
	table := make([]int, n)
	k := 0
	for i := 1; i < n; i++ {
		for k > 0 && at(i) != at(k) {
			k = table[k-1]
		}
		if at(i) == at(k) {
			k++
		}
		table[i] = k
	}
	return table
}

func search(n, m int, s, p func(int) uint16, table []int) int {
	k := 0
	for i := 0; i < n; i++ {
		for k > 0 && s(i) != p(k) {
			k = table[k-1]
		}
		if s(i) == p(k) {
			k++
		}
		if k == m {
			return i - m + 1
		}
	}
	return -1
}
