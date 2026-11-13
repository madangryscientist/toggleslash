package jsonval

import (
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
)

// ErrNaN is returned by Write for a NaN, which JSON has no spelling for. A
// NaN cannot come from Read, so a value that carries one was built by hand.
var ErrNaN = errors.New("jsonval: cannot write NaN")

// Write writes v as JSON, with the same string escaping Read accepts: a lone
// surrogate goes back out as its \uXXXX escape rather than U+FFFD, and an
// infinity is written as 1e999 or -1e999, the only text that reads back as the
// same value (docs/sdk-contract.md section 7.1, docs/evaluation.md "Reading
// JSON").
func Write(w io.Writer, v Value) error {
	buf := make([]byte, 0, 128)
	buf, err := appendValue(buf, v)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func appendValue(dst []byte, v Value) ([]byte, error) {
	switch value := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case Null:
		return append(dst, "null"...), nil
	case Bool:
		if value {
			return append(dst, "true"...), nil
		}
		return append(dst, "false"...), nil
	case Number:
		return appendNumber(dst, float64(value))
	case String:
		return appendString(dst, string(value)), nil
	case Array:
		dst = append(dst, '[')
		for i, item := range value {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendValue(dst, item)
			if err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	case Object:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, key)
			dst = append(dst, ':')
			var err error
			dst, err = appendValue(dst, value[key])
			if err != nil {
				return dst, err
			}
		}
		return append(dst, '}'), nil
	default:
		return dst, ErrNaN
	}
}

func appendNumber(dst []byte, f float64) ([]byte, error) {
	switch {
	case math.IsNaN(f):
		return dst, ErrNaN
	case math.IsInf(f, 1):
		return append(dst, "1e999"...), nil
	case math.IsInf(f, -1):
		return append(dst, "-1e999"...), nil
	}
	return strconv.AppendFloat(dst, f, 'g', -1, 64), nil
}

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for _, cp := range CodePoints(s) {
		switch cp {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			switch {
			case cp < 0x20:
				dst = appendHex4(dst, cp)
			case cp >= 0xD800 && cp <= 0xDFFF:
				// A lone surrogate is not valid UTF-8, so it goes out as
				// its escape and reads back as the same code point.
				dst = appendHex4(dst, cp)
			default:
				dst = appendCodePoint(dst, cp)
			}
		}
	}
	return append(dst, '"')
}

func appendHex4(dst []byte, cp rune) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '\\', 'u')
	dst = append(dst, hex[(cp>>12)&0xF], hex[(cp>>8)&0xF], hex[(cp>>4)&0xF], hex[cp&0xF])
	return dst
}
