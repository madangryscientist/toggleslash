package jsonv

import (
	"math"
	"strconv"
)

// AppendJSON appends v as JSON text. A non-finite number is written as null (JSON has no spelling for it), a
// lone surrogate is written as a \uXXXX escape so the text stays valid UTF-8, and -0 is written as 0.
func AppendJSON(dst []byte, v Value) []byte {
	switch v.kind {
	case KindBool:
		return strconv.AppendBool(dst, v.b)
	case KindNumber:
		return appendNumber(dst, v.n)
	case KindString:
		return AppendString(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, item := range v.a {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendJSON(dst, item)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i, k := range v.o.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendString(dst, k)
			dst = append(dst, ':')
			dst = AppendJSON(dst, v.o.m[k])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

// Marshal returns v as JSON text.
func Marshal(v Value) []byte { return AppendJSON(nil, v) }

func appendNumber(dst []byte, f float64) []byte {
	if !IsFinite(f) {
		return append(dst, "null"...)
	}
	if f == 0 {
		return append(dst, '0')
	}
	format := byte('f')
	if abs := math.Abs(f); abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	dst = strconv.AppendFloat(dst, f, format, -1, 64)
	if format == 'e' {
		// Clean up e-09 to e-9, as encoding/json does.
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

const hexDigits = "0123456789abcdef"

// AppendString appends s as a JSON string. s is WTF-8: a lone surrogate becomes an escape.
func AppendString(dst []byte, s string) []byte {
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
		case c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF:
			r, n := DecodeRuneWTF8(s[i:])
			dst = append(dst, '\\', 'u', hexDigits[(r>>12)&0xF], hexDigits[(r>>8)&0xF], hexDigits[(r>>4)&0xF], hexDigits[r&0xF])
			i += n
		default:
			dst = append(dst, c)
			i++
		}
	}
	return append(dst, '"')
}
