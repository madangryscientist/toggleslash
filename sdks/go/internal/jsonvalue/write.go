package jsonvalue

import (
	"math"
	"strconv"
	"unicode/utf8"
)

const hexDigits = "0123456789abcdef"

// Append writes v as JSON. A lone surrogate is written as a \u escape, so the output is valid UTF-8;
// a number that is not finite is written as null and a negative zero as 0, the way JSON.stringify does.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case KindBool:
		if v.b {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case KindNumber:
		return appendNumber(dst, v.n)
	case KindString:
		return appendString(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, item := range v.a {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, item)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i := 0; i < v.o.Len(); i++ {
			if i > 0 {
				dst = append(dst, ',')
			}
			k, item := v.o.At(i)
			dst = appendString(dst, k)
			dst = append(dst, ':')
			dst = Append(dst, item)
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

// Marshal is Append to a new slice.
func Marshal(v Value) []byte { return Append(nil, v) }

// AppendString writes s, WTF-8, as a JSON string.
func AppendString(dst []byte, s string) []byte { return appendString(dst, s) }

func appendNumber(dst []byte, n float64) []byte {
	switch {
	case math.IsNaN(n) || math.IsInf(n, 0):
		return append(dst, "null"...)
	case n == 0:
		return append(dst, '0')
	}
	abs := math.Abs(n)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.AppendFloat(dst, n, 'f', -1, 64)
	}
	// Go writes the exponent with at least two digits ("1e-07"); JSON allows that, but keep it short.
	text := strconv.AppendFloat(nil, n, 'e', -1, 64)
	for i, c := range text {
		if c != 'e' {
			continue
		}
		dst = append(dst, text[:i+2]...)
		exp := text[i+2:]
		for len(exp) > 1 && exp[0] == '0' {
			exp = exp[1:]
		}
		return append(dst, exp...)
	}
	return append(dst, text...)
}

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
			i++
		case c == '\\':
			dst = append(dst, '\\', '\\')
			i++
		case c == '\n':
			dst = append(dst, '\\', 'n')
			i++
		case c == '\r':
			dst = append(dst, '\\', 'r')
			i++
		case c == '\t':
			dst = append(dst, '\\', 't')
			i++
		case c == '\b':
			dst = append(dst, '\\', 'b')
			i++
		case c == '\f':
			dst = append(dst, '\\', 'f')
			i++
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			i++
		case c < 0x80:
			dst = append(dst, c)
			i++
		default:
			r, n := DecodeRune(s[i:])
			switch {
			case r >= 0xD800 && r <= 0xDFFF:
				dst = append(dst, '\\', 'u', hexDigits[r>>12&0xF], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF])
			case r == utf8.RuneError && n == 1:
				dst = append(dst, replacement...)
			default:
				dst = append(dst, s[i:i+n]...)
			}
			i += n
		}
	}
	return append(dst, '"')
}
