package jsonvalue

import (
	"math"
	"strconv"
)

// AppendJSON appends the JSON text of v. A number that JSON cannot write (Infinity, NaN) is written as
// null, as JSON.stringify does. A lone surrogate in a string is written as a \udxxx escape, so the text
// is valid UTF-8 and reads back to the same value.
func (v Value) AppendJSON(dst []byte) []byte {
	switch v.kind {
	case Null:
		return append(dst, "null"...)
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
		return appendString(dst, v.s)
	case Array:
		dst = append(dst, '[')
		for i, e := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = e.AppendJSON(dst)
		}
		return append(dst, ']')
	case Object:
		dst = append(dst, '{')
		for i, m := range v.Members() {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, m.Key)
			dst = append(dst, ':')
			dst = m.Value.AppendJSON(dst)
		}
		return append(dst, '}')
	}
	return dst
}

// JSON returns the JSON text of v (see AppendJSON).
func (v Value) JSON() []byte { return v.AppendJSON(nil) }

// String returns the JSON text of v.
func (v Value) String() string { return string(v.JSON()) }

const hexDigits = "0123456789abcdef"

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
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			i++
		case isSurrogateAt(s, i):
			r, _ := DecodeRune(s[i:])
			dst = append(dst, '\\', 'u', hexDigits[r>>12&0xF], hexDigits[r>>8&0xF], hexDigits[r>>4&0xF], hexDigits[r&0xF])
			i += 3
		default:
			dst = append(dst, c)
			i++
		}
	}
	return append(dst, '"')
}
