package jsonv

import "unicode/utf8"

// replacement is U+FFFD in UTF-8.
const replacement = "\xEF\xBF\xBD"

// DecodeUTF8 decodes bytes as the WHATWG UTF-8 decoder does: every maximal invalid subpart becomes one U+FFFD
// and an encoded surrogate is invalid. The result is always valid UTF-8. A leading byte order mark is not
// removed here; see StripBOM.
func DecodeUTF8(b []byte) []byte {
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
		need := 0
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lower = 0xA0
			}
			if c == 0xED {
				upper = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lower = 0x90
			}
			if c == 0xF4 {
				upper = 0x8F
			}
		default:
			out = append(out, replacement...)
			i++
			continue
		}
		j := i + 1
		ok := true
		for range need {
			if j >= len(b) || b[j] < lower || b[j] > upper {
				ok = false
				break
			}
			lower, upper = 0x80, 0xBF
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

// StripBOM removes one leading UTF-8 byte order mark.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// AppendWTF8Surrogate appends the generalised three-byte encoding of a lone surrogate (U+D800 to U+DFFF).
func AppendWTF8Surrogate(dst []byte, cp rune) []byte {
	return append(dst, 0xED, byte(0xA0|((cp>>6)&0x1F)), byte(0x80|(cp&0x3F)))
}

// DecodeRuneWTF8 decodes the first code point of s, which may be a lone surrogate in its three-byte form.
// size is 0 for an empty s. A byte that is not part of a well-formed sequence is returned as U+FFFD with size 1.
func DecodeRuneWTF8(s string) (r rune, size int) {
	if len(s) == 0 {
		return 0, 0
	}
	if s[0] == 0xED && len(s) >= 3 && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return rune(0xD000) | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// Runes returns the code points of a WTF-8 string, a lone surrogate being one code point with its value.
func Runes(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRuneWTF8(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// HasLoneSurrogate reports whether a WTF-8 string holds a surrogate code point.
func HasLoneSurrogate(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// HasHighBytes reports whether any byte of s is 0xED or above, which is where WTF-8 byte order and UTF-16
// code unit order can differ (U+D000 and up, surrogates, and everything outside the basic plane).
func HasHighBytes(s string) bool {
	for i := range len(s) {
		if s[i] >= 0xED {
			return true
		}
	}
	return false
}

// UTF16 returns the UTF-16 code units of a WTF-8 string.
func UTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRuneWTF8(s[i:])
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

// ToValidUTF8 returns s with every lone surrogate (and any other invalid byte) replaced by U+FFFD: the bytes
// that a UTF-8 encoder would write.
func ToValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF {
			out = append(out, replacement...)
			i += 3
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			out = append(out, replacement...)
		} else {
			out = append(out, s[i:i+n]...)
		}
		i += n
	}
	return string(out)
}
