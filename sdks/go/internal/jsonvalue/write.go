package jsonvalue

import (
	"math"
	"strconv"
)

const hexDigits = "0123456789abcdef"

// MarshalJSON writes v as JSON. It makes Value usable with encoding/json too.
func (v Value) MarshalJSON() ([]byte, error) { return v.AppendJSON(nil), nil }

// AppendJSON appends v as JSON. The infinities are written as 1e999 and -1e999 (so they read back as
// themselves), negative zero as -0, and a lone surrogate as a \udXXX escape. A NaN, which parsing never
// makes, is written as null.
func (v Value) AppendJSON(dst []byte) []byte {
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
			dst = item.AppendJSON(dst)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i, key := range v.obj.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, key)
			dst = append(dst, ':')
			dst = v.obj.vals[key].AppendJSON(dst)
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

func appendNumber(dst []byte, f float64) []byte {
	switch {
	case math.IsNaN(f):
		return append(dst, "null"...)
	case math.IsInf(f, 1):
		return append(dst, "1e999"...)
	case math.IsInf(f, -1):
		return append(dst, "-1e999"...)
	case f == 0 && math.Signbit(f):
		return append(dst, "-0"...)
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.AppendFloat(dst, f, 'f', -1, 64)
	}
	return strconv.AppendFloat(dst, f, 'e', -1, 64)
}

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			dst = append(dst, '\\', c)
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		case c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80:
			u := 0xD000 | int(s[i+1]&0x3F)<<6 | int(s[i+2]&0x3F)
			dst = append(dst, '\\', 'u', hexDigits[u>>12], hexDigits[u>>8&0xF], hexDigits[u>>4&0xF], hexDigits[u&0xF])
			i += 2
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}
