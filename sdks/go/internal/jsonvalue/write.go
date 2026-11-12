package jsonvalue

import (
	"math"
	"strconv"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Marshal writes v as JSON. A number that is not finite is written as null, as JSON.stringify does, and a lone
// surrogate is written as a \uXXXX escape so the output is valid JSON.
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
		for i, item := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendValue(dst, item)
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

func appendNumber(dst []byte, n float64) []byte {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return append(dst, "null"...)
	}
	if n == 0 {
		return append(dst, '0')
	}
	abs := math.Abs(n)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.AppendFloat(dst, n, 'f', -1, 64)
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, n, 'e', -1, 64)
	// Go writes e-07 and JSON text usually writes e-7: drop a leading zero of the exponent.
	if e := start + strings.IndexByte(string(dst[start:]), 'e'); dst[e+2] == '0' && len(dst) > e+3 {
		dst = append(dst[:e+2], dst[e+3:]...)
	}
	return dst
}

const hexDigits = "0123456789abcdef"

// AppendString appends s as a JSON string literal.
func AppendString(dst []byte, s string) []byte {
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
				dst = append(dst, '\\', 'u', hexDigits[r>>12&0xF], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF])
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

// MarshalJSON writes v as JSON, so a Value works with encoding/json and fmt. See Marshal for the two cases (a
// non-finite number, a lone surrogate) that JSON text cannot spell.
func (v Value) MarshalJSON() ([]byte, error) { return Marshal(v), nil }

// String is the JSON text of v, for logs and failure messages.
func (v Value) String() string { return string(Marshal(v)) }
