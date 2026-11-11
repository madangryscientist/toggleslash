package jsonv

import (
	"strings"
	"unicode/utf8"
)

// Strings are held as WTF-8: UTF-8, except that a lone surrogate (U+D800 to U+DFFF on its own) is kept
// as its own three bytes, so it survives parsing, comparison and output. A valid surrogate pair is always
// joined into one four byte code point before it is stored, so two equal UTF-16 strings are two equal Go
// strings.

const replacement = 0xFFFD

// DecodeRune reads the code point at the start of s. A lone surrogate comes back as itself, with size 3.
// Bytes that are not WTF-8 come back as U+FFFD with size 1.
func DecodeRune(s string) (rune, int) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// AppendRune appends the WTF-8 bytes of c. A surrogate is written as three bytes.
func AppendRune(b []byte, c rune) []byte {
	if c >= 0xD800 && c <= 0xDFFF {
		return append(b, 0xED, byte(0x80|(c>>6)&0x3F), byte(0x80|c&0x3F))
	}
	return utf8.AppendRune(b, c)
}

// CodePoints returns the code points of s.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		c, n := DecodeRune(s[i:])
		out = append(out, c)
		i += n
	}
	return out
}

// isASCII reports whether every byte of s is below 0x80.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// units returns the UTF-16 code units of s.
func units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		c, n := DecodeRune(s[i:])
		i += n
		if c >= 0x10000 {
			c -= 0x10000
			out = append(out, uint16(0xD800+(c>>10)), uint16(0xDC00+(c&0x3FF)))
		} else {
			out = append(out, uint16(c))
		}
	}
	return out
}

// Compare orders two strings by UTF-16 code unit, which is not the byte order of WTF-8 for code points
// above U+FFFF against U+E000 to U+FFFF.
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
	ua, ub := units(a), units(b)
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

// DecodeUTF8 replaces the bytes of data that are not valid UTF-8 the way the WHATWG Encoding standard's
// UTF-8 decoder does: one U+FFFD for each maximal invalid subpart. A byte order mark is not touched. A
// surrogate written as UTF-8 (ED A0 to ED BF) is invalid.
func DecodeUTF8(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	out := make([]byte, 0, len(data)+16)
	for i := 0; i < len(data); {
		b := data[i]
		if b < 0x80 {
			out = append(out, b)
			i++
			continue
		}
		var need int
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case b >= 0xC2 && b <= 0xDF:
			need = 1
		case b >= 0xE0 && b <= 0xEF:
			need = 2
			switch b {
			case 0xE0:
				lower = 0xA0
			case 0xED:
				upper = 0x9F
			}
		case b >= 0xF0 && b <= 0xF4:
			need = 3
			switch b {
			case 0xF0:
				lower = 0x90
			case 0xF4:
				upper = 0x8F
			}
		default:
			out = utf8.AppendRune(out, replacement)
			i++
			continue
		}
		start := i
		i++
		ok := true
		for k := 0; k < need; k++ {
			if i >= len(data) || data[i] < lower || data[i] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
			i++
		}
		if ok {
			out = append(out, data[start:i]...)
		} else {
			out = utf8.AppendRune(out, replacement)
		}
	}
	return out
}

// hasLoneSurrogate reports whether s holds a lone surrogate (the three byte form ED A0..BF xx).
func hasLoneSurrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80 {
			return true
		}
	}
	return false
}

// HasLoneSurrogate reports whether s holds a lone surrogate.
func HasLoneSurrogate(s string) bool { return hasLoneSurrogate(s) }

// ToUTF8 returns s with each lone surrogate replaced by U+FFFD, so the result is valid UTF-8. This is the
// encoding hashing uses.
func ToUTF8(s string) string {
	if !hasLoneSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c, n := DecodeRune(s[i:])
		if c >= 0xD800 && c <= 0xDFFF {
			c = replacement
		}
		out = utf8.AppendRune(out, c)
		i += n
	}
	return string(out)
}

// Contains reports whether needle occurs in s as a run of UTF-16 code units. Byte search is exact unless
// needle holds a lone surrogate, which can match half of a pair in s.
func Contains(s, needle string) bool {
	if !hasLoneSurrogate(needle) {
		return strings.Contains(s, needle)
	}
	us, un := units(s), units(needle)
	for i := 0; i+len(un) <= len(us); i++ {
		if equalUnits(us[i:i+len(un)], un) {
			return true
		}
	}
	return false
}

// HasPrefix reports whether s begins with prefix, by UTF-16 code units.
func HasPrefix(s, prefix string) bool {
	if !hasLoneSurrogate(prefix) {
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	us, up := units(s), units(prefix)
	return len(us) >= len(up) && equalUnits(us[:len(up)], up)
}

// HasSuffix reports whether s ends with suffix, by UTF-16 code units.
func HasSuffix(s, suffix string) bool {
	if !hasLoneSurrogate(suffix) {
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	us, uf := units(s), units(suffix)
	return len(us) >= len(uf) && equalUnits(us[len(us)-len(uf):], uf)
}

func equalUnits(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
