package jsonv

import (
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Marshal writes v as JSON. A non-finite number is written as null. A lone surrogate is written
// as a \uXXXX escape, so a reader of the output gets it back.
func Marshal(v Value) []byte { return Append(nil, v) }

// Append appends v as JSON to dst.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case Bool:
		return strconv.AppendBool(dst, v.b)
	case Number:
		return appendNumber(dst, v.n)
	case String:
		return appendString(dst, v.s)
	case Array:
		dst = append(dst, '[')
		for i, item := range v.a {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, item)
		}
		return append(dst, ']')
	case Object:
		dst = append(dst, '{')
		for i, k := range v.o.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, k)
			dst = append(dst, ':')
			dst = Append(dst, v.o.m[k])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

func appendNumber(dst []byte, n float64) []byte {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return append(dst, "null"...)
	}
	if n == math.Trunc(n) && math.Abs(n) < 1e21 {
		if n == 0 && math.Signbit(n) {
			return append(dst, "-0"...)
		}
		return strconv.AppendFloat(dst, n, 'f', -1, 64)
	}
	return strconv.AppendFloat(dst, n, 'g', -1, 64)
}

const hexDigits = "0123456789abcdef"

// AppendString appends s, a WTF-8 string, as a JSON string literal.
func AppendString(dst []byte, s string) []byte { return appendString(dst, s) }

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			dst = append(dst, '\\', c)
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
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			i++
		case c < 0x80:
			dst = append(dst, c)
			i++
		default:
			r, n := wtf8.DecodeRune(s[i:])
			if r >= 0xD800 && r <= 0xDFFF {
				dst = append(dst, '\\', 'u', hexDigits[r>>12], hexDigits[(r>>8)&0xF], hexDigits[(r>>4)&0xF], hexDigits[r&0xF])
			} else {
				dst = append(dst, s[i:i+n]...)
			}
			i += n
		}
	}
	return append(dst, '"')
}
