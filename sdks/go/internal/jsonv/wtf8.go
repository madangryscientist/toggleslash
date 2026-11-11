package jsonv

import (
	"strings"
	"unicode/utf8"
)

const (
	surrogateMin = 0xD800
	surrogateMax = 0xDFFF
	replacement  = 0xFFFD
)

// DecodeUTF8 decodes bytes with the WHATWG Encoding standard's UTF-8 decoder: one U+FFFD per maximal
// invalid subpart, not one per byte. A leading byte order mark is NOT removed here. The result is valid
// UTF-8 (no surrogates).
func DecodeUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	out := make([]byte, 0, len(b)+8)
	var (
		cp           rune
		needed, seen int
		lower, upper byte = 0x80, 0xBF
	)
	for i := 0; i < len(b); {
		c := b[i]
		if needed == 0 {
			i++
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
				out = utf8.AppendRune(out, replacement)
			}
			continue
		}
		if c < lower || c > upper {
			cp, needed, seen, lower, upper = 0, 0, 0, 0x80, 0xBF
			out = utf8.AppendRune(out, replacement)
			continue // reprocess this byte
		}
		i++
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		seen++
		if seen == needed {
			out = utf8.AppendRune(out, cp)
			cp, needed, seen = 0, 0, 0
		}
	}
	if needed != 0 {
		out = utf8.AppendRune(out, replacement)
	}
	return out
}

// AppendRune appends the WTF-8 form of r: valid UTF-8 for a scalar value, and the three-byte form for a
// surrogate code point (which utf8.AppendRune would replace).
func AppendRune(dst []byte, r rune) []byte {
	if r >= surrogateMin && r <= surrogateMax {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// DecodeRune decodes the first code point of s as WTF-8. A lone surrogate is returned as its own value.
// Bytes that are not WTF-8 decode as U+FFFD with width 1.
func DecodeRune(s string) (rune, int) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// hasLoneSurrogate reports whether s holds a surrogate in its three-byte form.
func hasLoneSurrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// CodePoints returns the code points of s: a valid pair is one, and a lone surrogate is one with its own
// value ("Patterns": strings are code points).
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// UTF16 returns the UTF-16 code units of s, the unit the spec's ordinal operations compare.
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

// Compare orders a and b by UTF-16 code unit (ordinal), -1, 0 or +1.
func Compare(a, b string) int {
	if isASCII(a) && isASCII(b) {
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
	if len(y) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(y) <= len(x); i++ {
		for j := range y {
			if x[i+j] != y[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

// Contains reports whether v occurs in a, by UTF-16 code unit. An empty v occurs everywhere.
func Contains(a, v string) bool {
	if !hasLoneSurrogate(v) {
		return strings.Contains(a, v)
	}
	return indexUnits(UTF16(a), UTF16(v)) >= 0
}

// HasPrefix reports whether a begins with v, by UTF-16 code unit.
func HasPrefix(a, v string) bool {
	if !hasLoneSurrogate(v) {
		return len(a) >= len(v) && a[:len(v)] == v
	}
	x, y := UTF16(a), UTF16(v)
	return len(x) >= len(y) && indexUnits(x[:len(y)], y) == 0
}

// HasSuffix reports whether a ends with v, by UTF-16 code unit.
func HasSuffix(a, v string) bool {
	if !hasLoneSurrogate(v) {
		return len(a) >= len(v) && a[len(a)-len(v):] == v
	}
	x, y := UTF16(a), UTF16(v)
	return len(x) >= len(y) && indexUnits(x[len(x)-len(y):], y) == 0
}

// ToUTF8 returns the bytes to hash for s: its UTF-8 form, in which a lone surrogate becomes U+FFFD
// ("Bucketing", edge cases).
func ToUTF8(s string) string {
	if !hasLoneSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		if r >= surrogateMin && r <= surrogateMax {
			r = replacement
		}
		out = utf8.AppendRune(out, r)
	}
	return string(out)
}
