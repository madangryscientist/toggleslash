// Package wtf8 holds the string helpers the SDK needs because a JSON string may carry a lone
// surrogate, which a Go string cannot hold as valid UTF-8 but can hold as WTF-8: a surrogate code
// point (U+D800 to U+DFFF) written with the three-byte form ED A0..BF xx. A valid surrogate pair is
// always one four-byte code point, never two three-byte surrogates.
package wtf8

import "unicode/utf8"

const replacement = 0xFFFD

// DecodeLossy turns bytes into a string the way the WHATWG Encoding standard's UTF-8 decoder does:
// one U+FFFD for each maximal invalid subpart. A surrogate written in bytes is invalid, so it never
// reaches the result as a surrogate.
func DecodeLossy(b []byte) string {
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
		need, lo, hi := leadInfo(c)
		if need == 0 {
			out = utf8.AppendRune(out, replacement)
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
			out = utf8.AppendRune(out, replacement)
		}
		i = j
	}
	return string(out)
}

// leadInfo gives the number of continuation bytes a lead byte needs and the range the first of them
// may take; need 0 means the byte cannot start a sequence.
func leadInfo(c byte) (need int, lo, hi byte) {
	switch {
	case c >= 0xC2 && c <= 0xDF:
		return 1, 0x80, 0xBF
	case c == 0xE0:
		return 2, 0xA0, 0xBF
	case c == 0xED:
		return 2, 0x80, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		return 2, 0x80, 0xBF
	case c == 0xF0:
		return 3, 0x90, 0xBF
	case c >= 0xF1 && c <= 0xF3:
		return 3, 0x80, 0xBF
	case c == 0xF4:
		return 3, 0x80, 0x8F
	}
	return 0, 0, 0
}

// AppendRune appends r as UTF-8, or as the three-byte WTF-8 form when r is a surrogate.
func AppendRune(dst []byte, r int32) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
	}
	return utf8.AppendRune(dst, r)
}

// Decode reads the code point at the start of s and its size in bytes. A surrogate in its
// three-byte form is itself; any other invalid byte is U+FFFD of size 1.
func Decode(s string) (int32, int) {
	if s == "" {
		return replacement, 0
	}
	c := s[0]
	if c < 0x80 {
		return int32(c), 1
	}
	if c == 0xED && len(s) >= 3 && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return int32(c&0x0F)<<12 | int32(s[1]&0x3F)<<6 | int32(s[2]&0x3F), 3
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size <= 1 {
		return replacement, 1
	}
	return r, size
}

// CodePoints returns the code points of s; a lone surrogate is one code point with its own value.
func CodePoints(s string) []int32 {
	out := make([]int32, 0, len(s))
	for len(s) > 0 {
		r, size := Decode(s)
		out = append(out, r)
		s = s[size:]
	}
	return out
}

// UTF16 returns the UTF-16 code units of s; a lone surrogate is its own unit.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for len(s) > 0 {
		r, size := Decode(s)
		s = s[size:]
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// plain reports whether the byte order of s is its UTF-16 order and its byte substrings are its
// UTF-16 substrings: no byte from 0xED up, which is where surrogates and supplementary planes start.
func plain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xED {
			return false
		}
	}
	return true
}

// Compare orders two strings by UTF-16 code unit: -1, 0 or 1.
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

func indexUnits(hay, needle []uint16) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
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

// Contains reports whether needle occurs in s as a run of UTF-16 code units.
func Contains(s, needle string) bool {
	if plain(s) && plain(needle) {
		return indexString(s, needle)
	}
	return indexUnits(UTF16(s), UTF16(needle)) >= 0
}

func indexString(s, needle string) bool {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// HasPrefix reports whether s begins with prefix, by UTF-16 code unit.
func HasPrefix(s, prefix string) bool {
	if plain(s) && plain(prefix) {
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	us, up := UTF16(s), UTF16(prefix)
	return len(us) >= len(up) && indexUnitsAt(us, up, 0)
}

// HasSuffix reports whether s ends with suffix, by UTF-16 code unit.
func HasSuffix(s, suffix string) bool {
	if plain(s) && plain(suffix) {
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	us, ux := UTF16(s), UTF16(suffix)
	return len(us) >= len(ux) && indexUnitsAt(us, ux, len(us)-len(ux))
}

func indexUnitsAt(hay, needle []uint16, at int) bool {
	for j := range needle {
		if hay[at+j] != needle[j] {
			return false
		}
	}
	return true
}

// ToUTF8 returns s as valid UTF-8, with each lone surrogate (and each other invalid byte) replaced by
// U+FFFD: the bytes that are hashed.
func ToUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	out := make([]byte, 0, len(s)+8)
	for len(s) > 0 {
		r, size := Decode(s)
		s = s[size:]
		if r >= 0xD800 && r <= 0xDFFF {
			r = replacement
		}
		out = utf8.AppendRune(out, r)
	}
	return string(out)
}
