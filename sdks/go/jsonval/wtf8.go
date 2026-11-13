package jsonval

import (
	"fmt"
	"unicode/utf8"
)

// CodePoints decodes s into Unicode code points, one per code point of its
// UTF-8 or, where the input held a lone surrogate escape, its WTF-8 form. A
// lone surrogate comes back as the code point of that value, not as U+FFFD.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if cp, n := decodeWTF8Surrogate(s[i:]); n > 0 {
				out = append(out, cp)
				i += n
				continue
			}
		}
		out = append(out, r)
		i += size
	}
	return out
}

// ReplaceLoneSurrogates returns s unchanged when it is valid UTF-8, and
// otherwise with each lone surrogate replaced by U+FFFD, which is what an
// encoder writes for one. Only hashing needs this: everywhere else a lone
// surrogate is kept and compares equal only to itself.
func ReplaceLoneSurrogates(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for _, cp := range CodePoints(s) {
		if cp >= 0xD800 && cp <= 0xDFFF {
			cp = utf8.RuneError
		}
		out = appendCodePoint(out, cp)
	}
	return string(out)
}

// decodeWTF8Surrogate reads one three byte WTF-8 encoded surrogate, if s starts
// with one, and reports how many bytes it took. Any other start returns 0.
func decodeWTF8Surrogate(s string) (rune, int) {
	if len(s) < 3 || s[0] != 0xED {
		return 0, 0
	}
	if s[1] < 0xA0 || s[1] > 0xBF {
		return 0, 0
	}
	if s[2] < 0x80 || s[2] > 0xBF {
		return 0, 0
	}
	cp := rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F)
	return cp, 3
}

// appendCodePoint writes cp as UTF-8, or as WTF-8 when cp is a lone surrogate.
func appendCodePoint(dst []byte, cp rune) []byte {
	switch {
	case cp < 0x80:
		return append(dst, byte(cp))
	case cp < 0x800:
		return append(dst, 0xC0|byte(cp>>6), 0x80|(byte(cp)&0x3F))
	case cp < 0x10000:
		return append(dst,
			0xE0|byte(cp>>12),
			0x80|(byte(cp>>6)&0x3F),
			0x80|(byte(cp)&0x3F))
	default:
		return append(dst,
			0xF0|byte(cp>>18),
			0x80|(byte(cp>>12)&0x3F),
			0x80|(byte(cp>>6)&0x3F),
			0x80|(byte(cp)&0x3F))
	}
}

// SyntaxError is a malformed JSON document, with the byte offset it failed at.
type SyntaxError struct {
	Offset int64
	Msg    string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("jsonval: %s at byte %d", e.Msg, e.Offset)
}
