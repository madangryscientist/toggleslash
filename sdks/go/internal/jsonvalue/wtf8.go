package jsonvalue

import (
	"unicode/utf8"
)

// replacement is U+FFFD in UTF-8.
const replacement = "�"

// AppendCodePoint appends r as WTF-8: a surrogate code point gets its own three-byte form, which
// utf8.AppendRune would replace with U+FFFD.
func AppendCodePoint(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// DecodeRune reads one code point from the start of a WTF-8 string and returns it with its width in
// bytes. A lone surrogate comes back as itself. Input that is not WTF-8 (which the reader never makes)
// comes back as U+FFFD with width 1, so the loop that calls this always advances.
func DecodeRune(s string) (rune, int) {
	if s == "" {
		return utf8.RuneError, 0
	}
	c := s[0]
	if c < 0x80 {
		return rune(c), 1
	}
	if c == 0xED && len(s) >= 3 && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return rune(c&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// HasLoneSurrogate reports whether a WTF-8 string holds a lone surrogate (the bytes ED A0 to ED BF).
func HasLoneSurrogate(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// HasHighByte reports whether any byte of s is 0xED or more. Below that, bytes are code points under
// U+D000, where UTF-8 byte order is UTF-16 code unit order and a byte-wise substring test is a code
// unit one.
func HasHighByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xED {
			return true
		}
	}
	return false
}

// ReplaceLoneSurrogates returns s with each lone surrogate replaced by U+FFFD, which is what encoding the
// string to UTF-8 does. It returns s itself when there is none.
func ReplaceLoneSurrogates(s string) string {
	if !HasLoneSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		if r >= 0xD800 && r <= 0xDFFF {
			out = append(out, replacement...)
		} else {
			out = append(out, s[i:i+n]...)
		}
		i += n
	}
	return string(out)
}

// UTF16 returns the UTF-16 code units of a WTF-8 string, a lone surrogate included.
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

// CompareUTF16 orders two WTF-8 strings by UTF-16 code unit: -1, 0 or 1.
func CompareUTF16(a, b string) int {
	if !HasHighByte(a) && !HasHighByte(b) {
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

// containsUnits, hasPrefixUnits and hasSuffixUnits are the slow paths below: the same tests over code units.
func indexUnits(a, v []uint16) int {
	for i := 0; i+len(v) <= len(a); i++ {
		if equalUnits(a[i:i+len(v)], v) {
			return i
		}
	}
	return -1
}

func equalUnits(a, b []uint16) bool {
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

// ContainsUTF16 reports whether v occurs in a, by code units.
func ContainsUTF16(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return containsBytes(a, v)
	}
	return indexUnits(UTF16(a), UTF16(v)) >= 0
}

// HasPrefixUTF16 reports whether a begins with v, by code units.
func HasPrefixUTF16(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return len(a) >= len(v) && a[:len(v)] == v
	}
	x, y := UTF16(a), UTF16(v)
	return len(x) >= len(y) && equalUnits(x[:len(y)], y)
}

// HasSuffixUTF16 reports whether a ends with v, by code units.
func HasSuffixUTF16(a, v string) bool {
	if !HasLoneSurrogate(v) {
		return len(a) >= len(v) && a[len(a)-len(v):] == v
	}
	x, y := UTF16(a), UTF16(v)
	return len(x) >= len(y) && equalUnits(x[len(x)-len(y):], y)
}

func containsBytes(a, v string) bool {
	if v == "" {
		return true
	}
	for i := 0; i+len(v) <= len(a); i++ {
		if a[i] == v[0] && a[i:i+len(v)] == v {
			return true
		}
	}
	return false
}

// DecodeLossy decodes bytes as UTF-8 the way the WHATWG Encoding standard does: each maximal invalid
// subpart becomes one U+FFFD. It returns b itself when b is already valid UTF-8.
func DecodeLossy(b []byte) []byte {
	if utf8.Valid(b) {
		return b
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
		lo, hi := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			}
			if c == 0xED {
				hi = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			}
			if c == 0xF4 {
				hi = 0x8F
			}
		default:
			out = append(out, replacement...)
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lo || b[j] > hi {
				ok = false
				break
			}
			lo, hi = 0x80, 0xBF
			j++
		}
		if ok {
			out = append(out, b[i:j]...)
		} else {
			out = append(out, replacement...)
		}
		i = j
	}
	return out
}
