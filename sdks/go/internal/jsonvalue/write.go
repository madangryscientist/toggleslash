package jsonvalue

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Write returns v as compact JSON. A lone surrogate in a string is written as its \uXXXX escape, so the
// output is always valid UTF-8. Infinity is written as 1e999 and -Infinity as -1e999, which a binary64
// reader takes back to the same value; NaN cannot arise from the reader and is written as null.
func Write(v Value) string {
	var b strings.Builder
	write(&b, v)
	return b.String()
}

func write(b *strings.Builder, v Value) {
	switch v.Kind {
	case Null:
		b.WriteString("null")
	case Bool:
		if v.B {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case Number:
		b.WriteString(FormatNumber(v.N))
	case String:
		WriteString(b, v.S)
	case Array:
		b.WriteByte('[')
		for i, item := range v.A {
			if i > 0 {
				b.WriteByte(',')
			}
			write(b, item)
		}
		b.WriteByte(']')
	case Object:
		b.WriteByte('{')
		for i, k := range v.O.Keys {
			if i > 0 {
				b.WriteByte(',')
			}
			WriteString(b, k)
			b.WriteByte(':')
			write(b, v.O.M[k])
		}
		b.WriteByte('}')
	}
}

// FormatNumber is the shortest text that reads back as n, with no exponent for whole numbers below 1e21.
func FormatNumber(n float64) string {
	switch {
	case math.IsNaN(n):
		return "null"
	case math.IsInf(n, 1):
		return "1e999"
	case math.IsInf(n, -1):
		return "-1e999"
	case n == 0:
		if math.Signbit(n) {
			return "-0"
		}
		return "0"
	}
	if n == math.Trunc(n) && math.Abs(n) < 1e21 {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}

const hexDigits = "0123456789abcdef"

// WriteString writes s as a JSON string literal.
func WriteString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
			i++
		case c == '\\':
			b.WriteString(`\\`)
			i++
		case c == '\n':
			b.WriteString(`\n`)
			i++
		case c == '\r':
			b.WriteString(`\r`)
			i++
		case c == '\t':
			b.WriteString(`\t`)
			i++
		case c < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
			i++
		case c < 0x80:
			b.WriteByte(c)
			i++
		case c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0:
			// A generalized (WTF-8) surrogate: written as an escape.
			r := rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			b.WriteString(`\u`)
			b.WriteByte(hexDigits[r>>12&0xF])
			b.WriteByte(hexDigits[r>>8&0xF])
			b.WriteByte(hexDigits[r>>4&0xF])
			b.WriteByte(hexDigits[r&0xF])
			i += 3
		default:
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	b.WriteByte('"')
}
