package jsonvalue

import "unicode/utf8"

// Strings in this package are WTF-8: ordinary UTF-8, except that a lone surrogate (U+D800 to U+DFFF)
// is kept in its generalised three-byte form (ED A0..BF 80..BF). A surrogate pair is always the
// four-byte form of the code point it makes. This is how a JSON string such as "\ud800" survives, which
// a Go string of valid UTF-8 cannot hold.

// isSurrogateAt reports whether s[i:] starts with the three-byte form of a lone surrogate.
func isSurrogateAt(s string, i int) bool {
	return i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF &&
		s[i+2] >= 0x80 && s[i+2] <= 0xBF
}

// DecodeRune decodes the first code point of a WTF-8 string and its width in bytes. A lone surrogate
// decodes to itself. An empty string gives (utf8.RuneError, 0). A string that is not WTF-8 gives
// (utf8.RuneError, 1), as utf8.DecodeRuneInString does.
func DecodeRune(s string) (rune, int) {
	if isSurrogateAt(s, 0) {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// AppendCodePoint appends the WTF-8 form of r, surrogates included. A value that is not a code point
// is written as U+FFFD.
func AppendCodePoint(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// CodePoints returns the code points of a WTF-8 string, a lone surrogate as itself.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// UTF16 returns the UTF-16 code units of a WTF-8 string.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		switch {
		case r >= 0x10000:
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		default:
			out = append(out, uint16(r))
		}
	}
	return out
}

// FromUTF16 builds a WTF-8 string from UTF-16 code units. A pair of a high and a low surrogate becomes
// one code point; an unpaired unit is kept as a lone surrogate.
func FromUTF16(units []uint16) string {
	buf := make([]byte, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) {
			if lo := rune(units[i+1]); lo >= 0xDC00 && lo <= 0xDFFF {
				buf = utf8.AppendRune(buf, 0x10000+(u-0xD800)<<10+(lo-0xDC00))
				i++
				continue
			}
		}
		buf = AppendCodePoint(buf, u)
	}
	return string(buf)
}

// CompareUTF16 orders two WTF-8 strings by UTF-16 code unit, the order the spec uses. It returns -1, 0 or 1.
func CompareUTF16(a, b string) int {
	if a == b {
		return 0
	}
	if isASCII(a) && isASCII(b) {
		if a < b {
			return -1
		}
		return 1
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

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ReplaceLoneSurrogates returns s with each lone surrogate replaced by U+FFFD, which is what the
// spec hashes for one ("Bucketing").
func ReplaceLoneSurrogates(s string) string {
	found := false
	for i := 0; i < len(s); i++ {
		if isSurrogateAt(s, i) {
			found = true
			break
		}
	}
	if !found {
		return s
	}
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if isSurrogateAt(s, i) {
			buf = append(buf, 0xEF, 0xBF, 0xBD)
			i += 3
			continue
		}
		buf = append(buf, s[i])
		i++
	}
	return string(buf)
}

// LossyUTF8 decodes bytes as UTF-8 the way the WHATWG Encoding Standard does: each maximal invalid
// subpart becomes one U+FFFD. Valid input is returned as it is, copied only if it must change.
func LossyUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out = append(out, c)
			i++
			continue
		}
		var need int
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c == 0xE0:
			need, lower = 2, 0xA0
		case c >= 0xE1 && c <= 0xEC, c == 0xEE, c == 0xEF:
			need = 2
		case c == 0xED:
			need, upper = 2, 0x9F
		case c == 0xF0:
			need, lower = 3, 0x90
		case c >= 0xF1 && c <= 0xF3:
			need = 3
		case c == 0xF4:
			need, upper = 3, 0x8F
		default:
			out = append(out, 0xEF, 0xBF, 0xBD)
			i++
			continue
		}
		start := i
		i++
		bad := false
		for k := 0; k < need; k++ {
			if i >= len(b) || b[i] < lower || b[i] > upper {
				bad = true
				break
			}
			lower, upper = 0x80, 0xBF
			i++
		}
		if bad {
			out = append(out, 0xEF, 0xBF, 0xBD)
			continue
		}
		out = append(out, b[start:i]...)
	}
	return string(out)
}
