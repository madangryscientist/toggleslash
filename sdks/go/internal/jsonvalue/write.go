package jsonvalue

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Append writes v as JSON. The output is plain UTF-8: a lone surrogate is written as the escape
// \udXXX, because UTF-8 cannot hold it, and a number JSON cannot spell (Infinity, NaN) is null.
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
		for i, item := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, item)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i, k := range v.obj.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, k)
			dst = append(dst, ':')
			dst = Append(dst, v.obj.m[k])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

// Marshal is Append onto a new slice.
func Marshal(v Value) []byte { return Append(nil, v) }

func appendNumber(dst []byte, f float64) []byte {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return append(dst, "null"...)
	}
	if f == 0 {
		return append(dst, '0') // JSON spells -0 as 0 in every reader that matters
	}
	if math.Abs(f) < 1e21 && f == math.Trunc(f) {
		return strconv.AppendFloat(dst, f, 'f', -1, 64)
	}
	return strconv.AppendFloat(dst, f, 'g', -1, 64)
}

const hexDigits = "0123456789abcdef"

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
		case c < 0x20 || c == 0x7F:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			i++
		case c < utf8.RuneSelf:
			dst = append(dst, c)
			i++
		default:
			r, n := wtf8.DecodeRune(s, i)
			if r >= 0xD800 && r <= 0xDFFF {
				dst = append(dst, '\\', 'u', hexDigits[r>>12&0xF], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF])
			} else {
				dst = append(dst, s[i:i+n]...)
			}
			i += n
		}
	}
	return append(dst, '"')
}
