package jsonv

import (
	"math"
	"sort"
	"strconv"
)

// Marshal writes v as JSON. A lone surrogate is written as a \udXXX escape. An infinity or NaN has no JSON
// form and is written as null, as JSON.stringify does. Object members are written in key order.
func Marshal(v Value) []byte {
	return appendValue(nil, v)
}

func appendValue(b []byte, v Value) []byte {
	switch v.kind {
	case Bool:
		if v.b {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case Number:
		if math.IsInf(v.n, 0) || math.IsNaN(v.n) {
			return append(b, "null"...)
		}
		return AppendNumber(b, v.n)
	case String:
		return AppendString(b, v.s)
	case Array:
		b = append(b, '[')
		for i, e := range v.a {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendValue(b, e)
		}
		return append(b, ']')
	case Object:
		keys := make([]string, 0, len(v.o))
		for k := range v.o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b = append(b, '{')
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendString(b, k)
			b = append(b, ':')
			b = appendValue(b, v.o[k])
		}
		return append(b, '}')
	}
	return append(b, "null"...)
}

// AppendNumber writes a finite float64 as a JSON number. -0 is written as -0.
func AppendNumber(b []byte, f float64) []byte {
	if f == 0 && math.Signbit(f) {
		return append(b, "-0"...)
	}
	return strconv.AppendFloat(b, f, 'g', -1, 64)
}

const hexDigits = "0123456789abcdef"

// AppendString writes s (WTF-8) as a JSON string.
func AppendString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); {
		c, n := DecodeRune(s[i:])
		i += n
		switch {
		case c == '"':
			b = append(b, '\\', '"')
		case c == '\\':
			b = append(b, '\\', '\\')
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\t':
			b = append(b, '\\', 't')
		case c < 0x20 || (c >= 0xD800 && c <= 0xDFFF):
			b = append(b, '\\', 'u', hexDigits[c>>12&0xF], hexDigits[c>>8&0xF], hexDigits[c>>4&0xF], hexDigits[c&0xF])
		default:
			b = AppendRune(b, c)
		}
	}
	return append(b, '"')
}
