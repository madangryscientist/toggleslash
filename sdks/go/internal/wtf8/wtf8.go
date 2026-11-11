// Package wtf8 holds the string helpers the SDK needs because a JSON string may carry a lone surrogate.
//
// Strings in this module are Go strings holding WTF-8: UTF-8 in which a lone surrogate (U+D800 to U+DFFF
// on its own) is written as its three-byte form, which valid UTF-8 never contains. A valid pair is always
// one four-byte code point, never two three-byte halves.
package wtf8

import "unicode/utf8"

// Replacement is U+FFFD.
const Replacement rune = 0xFFFD

// DecodeWHATWG decodes one code point from raw bytes the way the WHATWG Encoding standard's UTF-8 decoder
// does: an invalid sequence becomes one U+FFFD for its maximal invalid subpart, not one per byte. The
// second result is the number of bytes consumed. b must not be empty.
func DecodeWHATWG(b []byte) (rune, int) {
	c := b[0]
	if c < 0x80 {
		return rune(c), 1
	}
	var need int
	var cp rune
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need, cp = 1, rune(c&0x1F)
	case c >= 0xE0 && c <= 0xEF:
		need, cp = 2, rune(c&0x0F)
		if c == 0xE0 {
			lo = 0xA0
		}
		if c == 0xED {
			hi = 0x9F
		}
	case c >= 0xF0 && c <= 0xF4:
		need, cp = 3, rune(c&0x07)
		if c == 0xF0 {
			lo = 0x90
		}
		if c == 0xF4 {
			hi = 0x8F
		}
	default:
		return Replacement, 1
	}
	i := 1
	for ; need > 0; need-- {
		if i >= len(b) || b[i] < lo || b[i] > hi {
			return Replacement, i
		}
		cp = cp<<6 | rune(b[i]&0x3F)
		lo, hi = 0x80, 0xBF
		i++
	}
	return cp, i
}

// Sanitize returns s as WTF-8: every invalid UTF-8 byte sequence is replaced as DecodeWHATWG does, and the
// three-byte form of a lone surrogate is kept. A string that is valid UTF-8 comes back as it is. It is for
// strings that came from Go code; the bytes of an HTTP body go through DecodeWHATWG, where that three-byte
// form is invalid like any other.
func Sanitize(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	b := []byte(s)
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); {
		if IsSurrogateAt(s, i) && b[i+2] >= 0x80 && b[i+2] <= 0xBF {
			out = append(out, b[i:i+3]...)
			i += 3
			continue
		}
		r, n := DecodeWHATWG(b[i:])
		out = utf8.AppendRune(out, r)
		i += n
	}
	return string(out)
}

// AppendCodePoint appends r to dst, writing a surrogate in its three-byte form.
func AppendCodePoint(dst []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
	}
	return utf8.AppendRune(dst, r)
}

// IsSurrogateAt reports whether s[i:] starts with the three-byte form of a lone surrogate.
func IsSurrogateAt(s string, i int) bool {
	return i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF
}

// HasLoneSurrogate reports whether s holds a lone surrogate.
func HasLoneSurrogate(s string) bool {
	for i := range len(s) {
		if s[i] == 0xED && IsSurrogateAt(s, i) {
			return true
		}
	}
	return false
}

// Decode returns the code point at s[i] and its length in bytes. A lone surrogate is returned as its own
// value. A byte that is not part of any valid sequence is returned as U+FFFD with length 1 (a string this
// module built has none).
func Decode(s string, i int) (rune, int) {
	c := s[i]
	if c < 0x80 {
		return rune(c), 1
	}
	if c == 0xED && IsSurrogateAt(s, i) && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
		return rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s[i:])
}

// CodePoints returns the code points of s.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := Decode(s, i)
		out = append(out, r)
		i += n
	}
	return out
}

// UTF16 returns the UTF-16 code units of s.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := Decode(s, i)
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

// ToValidUTF8 returns s with each lone surrogate replaced by U+FFFD: the bytes that are hashed.
func ToValidUTF8(s string) string {
	if !HasLoneSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if IsSurrogateAt(s, i) {
			out = utf8.AppendRune(out, Replacement)
			i += 3
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

// bytesOrderIsUnitOrder reports whether comparing the bytes of a and b gives the UTF-16 code unit order:
// true when neither holds a code point from U+E000 up, where the two orders differ.
func bytesOrderIsUnitOrder(a, b string) bool {
	for i := range len(a) {
		if a[i] >= 0xEE {
			return false
		}
	}
	for i := range len(b) {
		if b[i] >= 0xEE {
			return false
		}
	}
	return true
}

// Compare orders a and b by UTF-16 code unit, as the evaluation spec requires: -1, 0 or 1.
func Compare(a, b string) int {
	if bytesOrderIsUnitOrder(a, b) {
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

func unitIndex(h, n []uint16, from int) int {
	for i := from; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
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

// Contains reports whether sub occurs in s as a run of UTF-16 code units.
func Contains(s, sub string) bool {
	if !HasLoneSurrogate(s) && !HasLoneSurrogate(sub) {
		return indexString(s, sub) >= 0
	}
	return unitIndex(UTF16(s), UTF16(sub), 0) >= 0
}

// HasPrefix reports whether s begins with prefix, as UTF-16 code units.
func HasPrefix(s, prefix string) bool {
	if !HasLoneSurrogate(s) && !HasLoneSurrogate(prefix) {
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	us, up := UTF16(s), UTF16(prefix)
	if len(up) > len(us) {
		return false
	}
	for i := range up {
		if us[i] != up[i] {
			return false
		}
	}
	return true
}

// HasSuffix reports whether s ends with suffix, as UTF-16 code units.
func HasSuffix(s, suffix string) bool {
	if !HasLoneSurrogate(s) && !HasLoneSurrogate(suffix) {
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	}
	us, up := UTF16(s), UTF16(suffix)
	if len(up) > len(us) {
		return false
	}
	off := len(us) - len(up)
	for i := range up {
		if us[off+i] != up[i] {
			return false
		}
	}
	return true
}

func indexString(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i] == sub[0] && s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
