package jsonv

import (
	"math"
	"strconv"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Marshal writes v as JSON. A number that is NaN or an infinity has no JSON form and is written as null.
// A lone surrogate is written as its \uXXXX escape; every other character is written as UTF-8.
func Marshal(v Value) []byte {
	return appendValue(nil, v)
}

func appendValue(dst []byte, v Value) []byte {
	switch v.kind {
	case KindBool:
		if v.b {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case KindNumber:
		return appendNumber(dst, v.n)
	case KindString:
		return AppendString(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, e := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendValue(dst, e)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i, k := range v.obj.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendString(dst, k)
			dst = append(dst, ':')
			dst = appendValue(dst, v.obj.m[k])
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

// FormatNumber prints f the way ECMAScript's Number-to-string does (shortest round trip, no exponent
// from 1e-6 up to 1e21, -0 as 0). NaN and infinities print as null.
func FormatNumber(f float64) string { return string(appendNumber(nil, f)) }

func appendNumber(dst []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return append(dst, "null"...)
	}
	if f == 0 {
		return append(dst, '0')
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.AppendFloat(dst, f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // 1.5e-07, 1e+21
	mant, exp, _ := strings.Cut(s, "e")
	sign := exp[0]
	digits := strings.TrimLeft(exp[1:], "0")
	if digits == "" {
		digits = "0"
	}
	dst = append(dst, mant...)
	dst = append(dst, 'e', sign)
	return append(dst, digits...)
}

const hexDigits = "0123456789abcdef"

// AppendString appends s (WTF-8) as a JSON string literal.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x80 {
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
			case c < 0x20 || c == 0x7f:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&15])
			default:
				dst = append(dst, c)
			}
			i++
			continue
		}
		r, n := wtf8.DecodeRune(s[i:])
		if r >= 0xD800 && r <= 0xDFFF {
			dst = append(dst, '\\', 'u', hexDigits[r>>12], hexDigits[r>>8&15], hexDigits[r>>4&15], hexDigits[r&15])
		} else {
			dst = append(dst, s[i:i+n]...)
		}
		i += n
	}
	return append(dst, '"')
}
