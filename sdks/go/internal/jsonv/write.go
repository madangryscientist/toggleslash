package jsonv

import (
	"math"
	"strconv"
	"unicode/utf8"
)

// Append writes v as JSON text. A number that is not finite is written as null, as JSON.stringify does,
// and negative zero as -0. A lone surrogate is written as a \uXXXX escape so the text is valid UTF-8.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case Bool:
		if v.b {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case Number:
		if math.IsInf(v.n, 0) || math.IsNaN(v.n) {
			return append(dst, "null"...)
		}
		if v.n == 0 && math.Signbit(v.n) {
			return append(dst, "-0"...)
		}
		return strconv.AppendFloat(dst, v.n, 'g', -1, 64)
	case String:
		return AppendString(dst, v.s)
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
			dst = AppendString(dst, k)
			dst = append(dst, ':')
			dst = Append(dst, v.o.m[k])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

const hexDigits = "0123456789abcdef"

// AppendString writes s as a JSON string literal.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		switch {
		case r == '"' || r == '\\':
			dst = append(dst, '\\', byte(r))
		case r == '\n':
			dst = append(dst, '\\', 'n')
		case r == '\r':
			dst = append(dst, '\\', 'r')
		case r == '\t':
			dst = append(dst, '\\', 't')
		case r < 0x20 || (r >= surrogateMin && r <= surrogateMax):
			dst = append(dst, '\\', 'u', hexDigits[r>>12&0xF], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF])
		case r == utf8.RuneError && n == 1:
			dst = append(dst, "\\ufffd"...)
		default:
			dst = append(dst, s[i:i+n]...)
		}
		i += n
	}
	return append(dst, '"')
}
