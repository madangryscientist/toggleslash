package toggleslash

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// A Go string holds bytes, and this SDK holds a string as WTF-8: ordinary UTF-8, except that a JSON escape for
// a lone surrogate (`"\ud800"`) is kept as the three-byte sequence for that code unit, because the spec keeps
// it and compares it as itself. Every comparison below therefore reads code units or code points rather than
// bytes, as docs/evaluation.md requires, and every hash writes U+FFFD in a lone surrogate's place.

// decodeWTF8At decodes one code point at the start of s, allowing the three-byte lone-surrogate form. It
// returns the code point and its width, or ok false for a byte that starts nothing.
func decodeWTF8At(s string, at int) (code rune, size int, ok bool) {
	if at < 0 || at >= len(s) {
		return 0, 0, false
	}
	c := s[at]
	switch {
	case c < 0x80:
		return rune(c), 1, true
	case c&0xE0 == 0xC0:
		size, code = 2, rune(c&0x1F)
	case c&0xF0 == 0xE0:
		size, code = 3, rune(c&0x0F)
	case c&0xF8 == 0xF0:
		size, code = 4, rune(c&0x07)
	default:
		return 0, 0, false
	}
	if at+size > len(s) {
		return 0, 0, false
	}
	for i := 1; i < size; i++ {
		b := s[at+i]
		if b&0xC0 != 0x80 {
			return 0, 0, false
		}
		code = code<<6 | rune(b&0x3F)
	}
	if size == 2 && code < 0x80 || size == 3 && code < 0x800 || size == 4 && code < 0x10000 || code > 0x10FFFF {
		return 0, 0, false
	}
	return code, size, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// codeUnitsOf is the UTF-16 code units of a WTF-8 string: a surrogate pair is two units, a lone surrogate is
// one.
func codeUnitsOf(s string) []uint16 {
	units := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		code, size, ok := decodeWTF8At(s, i)
		if !ok {
			units = append(units, 0xFFFD)
			i++
			continue
		}
		if code > 0xFFFF {
			high, low := utf16.EncodeRune(code)
			units = append(units, uint16(high), uint16(low))
		} else {
			units = append(units, uint16(code))
		}
		i += size
	}
	return units
}

// codePointsOf is the code points of a WTF-8 string: a valid pair is one code point, a lone surrogate is one
// code point with its own value.
func codePointsOf(s string) []rune {
	points := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		code, size, ok := decodeWTF8At(s, i)
		if !ok {
			points = append(points, 0xFFFD)
			i++
			continue
		}
		points = append(points, code)
		i += size
	}
	return points
}

// unitBytes is the UTF-16 big-endian form of a string, which orders and compares as the code units do.
func unitBytes(s string) []byte {
	units := codeUnitsOf(s)
	out := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		out = append(out, byte(unit>>8), byte(unit))
	}
	return out
}

// compareText orders two strings by UTF-16 code unit, never by a culture's collation.
func compareText(a, b string) int {
	if isASCII(a) && isASCII(b) {
		return strings.Compare(a, b)
	}
	return bytes.Compare(unitBytes(a), unitBytes(b))
}

func sameText(a, b string) bool {
	if a == b {
		return true
	}
	if isASCII(a) && isASCII(b) {
		return false
	}
	return bytes.Equal(unitBytes(a), unitBytes(b))
}

func containsText(haystack, needle string) bool {
	if isASCII(haystack) && isASCII(needle) {
		return strings.Contains(haystack, needle)
	}
	h, n := unitBytes(haystack), unitBytes(needle)
	for start := 0; start+len(n) <= len(h); start += 2 {
		if bytes.Equal(h[start:start+len(n)], n) {
			return true
		}
	}
	return false
}

func hasPrefixText(text, prefix string) bool {
	if isASCII(text) && isASCII(prefix) {
		return strings.HasPrefix(text, prefix)
	}
	return bytes.HasPrefix(unitBytes(text), unitBytes(prefix))
}

func hasSuffixText(text, suffix string) bool {
	if isASCII(text) && isASCII(suffix) {
		return strings.HasSuffix(text, suffix)
	}
	return bytes.HasSuffix(unitBytes(text), unitBytes(suffix))
}

// hashText appends the bytes a hash is computed over: the UTF-8 encoding of the string, in which a lone
// surrogate becomes U+FFFD, as both JavaScript's TextEncoder and .NET's UTF8Encoding do.
func hashText(buf []byte, text string) []byte {
	for i := 0; i < len(text); {
		code, size, ok := decodeWTF8At(text, i)
		if !ok {
			buf = append(buf, 0xEF, 0xBF, 0xBD)
			i++
			continue
		}
		if code >= 0xD800 && code <= 0xDFFF {
			buf = append(buf, 0xEF, 0xBF, 0xBD)
		} else {
			buf = append(buf, text[i:i+size]...)
		}
		i += size
	}
	return buf
}

// bucketCount is the range a bucket lands in.
const bucketCount = 100000

// uint32Of and bucketOf are the bucketing of docs/evaluation.md: the first four bytes of SHA-256 over
// "<key>.<salt>.<contextKey>", read big-endian, then taken modulo 100000.
func uint32Of(key, salt, contextKey string) uint32 {
	input := make([]byte, 0, len(key)+len(salt)+len(contextKey)+2)
	input = hashText(input, key)
	input = append(input, '.')
	input = hashText(input, salt)
	input = append(input, '.')
	input = hashText(input, contextKey)
	sum := sha256.Sum256(input)
	return binary.BigEndian.Uint32(sum[:4])
}

func bucketOf(key, salt, contextKey string) int {
	return int(uint32Of(key, salt, contextKey) % bucketCount)
}
