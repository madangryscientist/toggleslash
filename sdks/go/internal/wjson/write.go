package wjson

import (
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Marshal writes v as JSON. A non-finite number is written as null, as
// JavaScript's JSON.stringify does, and a lone surrogate as a \udXXX escape.
func Marshal(v Value) []byte { return Append(nil, v) }

// Append appends v as JSON to dst.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case KindBool:
		return strconv.AppendBool(dst, v.b)
	case KindNumber:
		return appendNumber(dst, v.n)
	case KindString:
		return appendString(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, e := range v.a {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, e)
		}
		return append(dst, ']')
	case KindObject:
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
	default:
		return append(dst, "null"...)
	}
}

func appendNumber(dst []byte, n float64) []byte {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return append(dst, "null"...)
	}
	if n == 0 {
		if math.Signbit(n) {
			return append(dst, "-0"...)
		}
		return append(dst, '0')
	}
	abs := math.Abs(n)
	if abs >= 1e21 || abs < 1e-6 {
		return strconv.AppendFloat(dst, n, 'e', -1, 64)
	}
	return strconv.AppendFloat(dst, n, 'f', -1, 64)
}

const hexDigits = "0123456789abcdef"

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		case c < 0x80:
			dst = append(dst, c)
		default:
			r, n := wtf8.DecodeRune(s[i:])
			if wtf8.IsSurrogate(r) {
				dst = append(dst, '\\', 'u', hexDigits[r>>12], hexDigits[(r>>8)&0xF], hexDigits[(r>>4)&0xF], hexDigits[r&0xF])
			} else {
				dst = append(dst, s[i:i+n]...)
			}
			i += n
			continue
		}
		i++
	}
	return append(dst, '"')
}
