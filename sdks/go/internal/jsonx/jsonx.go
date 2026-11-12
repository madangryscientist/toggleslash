// Package jsonx is the SDK's JSON reader and writer, to the "Reading JSON" rules of docs/evaluation.md and
// the body rules of docs/sdk-contract.md section 7.1.
//
// The standard library's encoding/json cannot be used: it refuses `1e999` (which is Infinity, not an error
// here), and it replaces a lone surrogate escape with U+FFFD, where the spec keeps the surrogate. This reader
// keeps both. Strings are held as WTF-8: ordinary UTF-8 bytes, except that a lone surrogate, which UTF-8
// cannot spell, is written as its own three-byte sequence. Nothing else in the SDK reads a string as UTF-8
// without going through text.go for that reason.
package jsonx

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Error is a body that is not JSON this reader accepts.
var Error = errors.New("json: not valid")

// maxDepth bounds nesting so a hostile body cannot exhaust the stack. The contract allows a refusal deeper
// than the parser's limit, and requires that limit to be at least 512 levels.
const maxDepth = 2048

// Parse reads JSON text or UTF-8 bytes. A leading byte order mark is skipped, bytes that are not valid UTF-8
// are replaced as the WHATWG UTF-8 decoder replaces them (one U+FFFD per maximal invalid subpart), numbers are
// binary64 (`1e999` is Infinity), and a duplicate key in an object keeps the last value.
func Parse(data []byte) (any, error) {
	p := &parser{text: decodeText(data)}
	p.skipSpace()
	value, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.at < len(p.text) {
		return nil, Error
	}
	return value, nil
}

// DecodeText turns bytes into a string the way the contract says a body is read: an invalid byte becomes
// U+FFFD, one per maximal invalid subpart, and a leading byte order mark is skipped.
func DecodeText(data []byte) string { return decodeText(data) }

// DecodeStream is DecodeText without the byte order mark step, for a server-sent event line: the stream
// parser skips a mark itself, at the start of the stream only.
func DecodeStream(data []byte) string { return decodeBytes(data) }

func decodeText(data []byte) string {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	return decodeBytes(data)
}

func decodeBytes(data []byte) string {
	var out strings.Builder
	out.Grow(len(data))
	for i := 0; i < len(data); {
		b := data[i]
		if b < 0x80 {
			out.WriteByte(b)
			i++
			continue
		}
		// What the lead byte promises: a sequence length and the range its second byte may hold. A byte outside
		// all of these is an error on its own.
		size := 0
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case b >= 0xC2 && b <= 0xDF:
			size = 2
		case b == 0xE0:
			size, lower = 3, 0xA0
		case b >= 0xE1 && b <= 0xEC:
			size = 3
		case b == 0xED:
			size, upper = 3, 0x9F
		case b >= 0xEE && b <= 0xEF:
			size = 3
		case b == 0xF0:
			size, lower = 4, 0x90
		case b >= 0xF1 && b <= 0xF3:
			size = 4
		case b == 0xF4:
			size, upper = 4, 0x8F
		}
		if size == 0 {
			out.WriteString("�")
			i++
			continue
		}
		good := 1
		for good < size {
			if i+good >= len(data) {
				break // the body ends inside the sequence: one replacement for the whole subpart
			}
			c := data[i+good]
			if good == 1 {
				if c < lower || c > upper {
					break
				}
			} else if c < 0x80 || c > 0xBF {
				break
			}
			good++
		}
		if good < size {
			out.WriteString("�")
			i += good
			continue
		}
		code := rune(b & (0x7F >> uint(size)))
		for k := 1; k < size; k++ {
			code = code<<6 | rune(data[i+k]&0x3F)
		}
		out.WriteRune(code)
		i += size
	}
	return out.String()
}

type parser struct {
	text string
	at   int
}

func (p *parser) skipSpace() {
	for p.at < len(p.text) {
		switch p.text[p.at] {
		case ' ', '\t', '\n', '\r':
			p.at++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, Error
	}
	if p.at >= len(p.text) {
		return nil, Error
	}
	switch c := p.text[p.at]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		return p.string()
	case c == 't':
		if strings.HasPrefix(p.text[p.at:], "true") {
			p.at += 4
			return true, nil
		}
	case c == 'f':
		if strings.HasPrefix(p.text[p.at:], "false") {
			p.at += 5
			return false, nil
		}
	case c == 'n':
		if strings.HasPrefix(p.text[p.at:], "null") {
			p.at += 4
			return nil, nil
		}
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, Error
}

func (p *parser) object(depth int) (any, error) {
	p.at++ // {
	out := map[string]any{}
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == '}' {
		p.at++
		return out, nil
	}
	for {
		p.skipSpace()
		key, err := p.string()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.at >= len(p.text) || p.text[p.at] != ':' {
			return nil, Error
		}
		p.at++
		p.skipSpace()
		value, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		out[key] = value // a duplicate key keeps the last value
		p.skipSpace()
		if p.at >= len(p.text) {
			return nil, Error
		}
		if p.text[p.at] == ',' {
			p.at++
			continue
		}
		if p.text[p.at] == '}' {
			p.at++
			return out, nil
		}
		return nil, Error
	}
}

func (p *parser) array(depth int) (any, error) {
	p.at++ // [
	out := []any{}
	p.skipSpace()
	if p.at < len(p.text) && p.text[p.at] == ']' {
		p.at++
		return out, nil
	}
	for {
		p.skipSpace()
		value, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
		p.skipSpace()
		if p.at >= len(p.text) {
			return nil, Error
		}
		if p.text[p.at] == ',' {
			p.at++
			continue
		}
		if p.text[p.at] == ']' {
			p.at++
			return out, nil
		}
		return nil, Error
	}
}

// number scans the JSON number grammar and reads it as binary64. A value too large for a double is an
// infinity, not an error; a tiny one underflows to zero.
func (p *parser) number() (any, error) {
	start := p.at
	if p.at < len(p.text) && p.text[p.at] == '-' {
		p.at++
	}
	if p.at >= len(p.text) {
		return nil, Error
	}
	if p.text[p.at] == '0' {
		p.at++
	} else if p.text[p.at] >= '1' && p.text[p.at] <= '9' {
		for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
			p.at++
		}
	} else {
		return nil, Error
	}
	if p.at < len(p.text) && p.text[p.at] == '.' {
		p.at++
		if p.at >= len(p.text) || p.text[p.at] < '0' || p.text[p.at] > '9' {
			return nil, Error
		}
		for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
			p.at++
		}
	}
	if p.at < len(p.text) && (p.text[p.at] == 'e' || p.text[p.at] == 'E') {
		p.at++
		if p.at < len(p.text) && (p.text[p.at] == '+' || p.text[p.at] == '-') {
			p.at++
		}
		if p.at >= len(p.text) || p.text[p.at] < '0' || p.text[p.at] > '9' {
			return nil, Error
		}
		for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
			p.at++
		}
	}
	value, err := strconv.ParseFloat(p.text[start:p.at], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, Error
	}
	return value, nil
}

// string reads a quoted string. A `\uXXXX` escape for a lone surrogate is written as its WTF-8 sequence, so
// the code unit survives; a valid pair becomes one code point.
func (p *parser) string() (string, error) {
	if p.at >= len(p.text) || p.text[p.at] != '"' {
		return "", Error
	}
	p.at++
	var out strings.Builder
	for {
		if p.at >= len(p.text) {
			return "", Error
		}
		c := p.text[p.at]
		switch {
		case c == '"':
			p.at++
			return out.String(), nil
		case c == '\\':
			p.at++
			if p.at >= len(p.text) {
				return "", Error
			}
			switch p.text[p.at] {
			case '"':
				out.WriteByte('"')
			case '\\':
				out.WriteByte('\\')
			case '/':
				out.WriteByte('/')
			case 'b':
				out.WriteByte('\b')
			case 'f':
				out.WriteByte('\f')
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'u':
				code, err := p.hex4()
				if err != nil {
					return "", err
				}
				p.writeEscape(&out, code)
				continue
			default:
				return "", Error
			}
			p.at++
		case c < 0x20:
			return "", Error
		default:
			out.WriteByte(c)
			p.at++
		}
	}
}

// writeEscape writes one \u escape, combining a valid surrogate pair and keeping a lone surrogate.
func (p *parser) writeEscape(out *strings.Builder, code rune) {
	if code >= 0xD800 && code <= 0xDBFF && strings.HasPrefix(p.text[p.at:], "\\u") {
		// Look ahead for the low half of the pair without consuming it unless it is one.
		saved := p.at
		p.at++
		low, err := p.hex4()
		if err == nil && low >= 0xDC00 && low <= 0xDFFF {
			out.WriteRune(utf16.DecodeRune(code, low))
			return
		}
		p.at = saved
	}
	out.WriteString(wtf8(code))
}

func (p *parser) hex4() (rune, error) {
	if p.at+4 > len(p.text) || p.text[p.at] != 'u' {
		return 0, Error
	}
	var code rune
	for i := p.at + 1; i < p.at+5; i++ {
		digit := hexDigit(p.text[i])
		if digit < 0 {
			return 0, Error
		}
		code = code<<4 | rune(digit)
	}
	p.at += 5
	return code, nil
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// wtf8 spells a code point as WTF-8: UTF-8, plus the three-byte form for a lone surrogate.
func wtf8(code rune) string {
	if code >= 0xD800 && code <= 0xDFFF {
		return string([]byte{
			byte(0xE0 | code>>12),
			byte(0x80 | (code>>6)&0x3F),
			byte(0x80 | code&0x3F),
		})
	}
	return string(code)
}

// Encode writes compact JSON for a value the SDK builds: the usage body, an answer the test service gives, a
// hook event. A non-finite number is written as null, as JSON.stringify does, and a lone surrogate is written
// as its \uXXXX escape because JSON text cannot hold one.
func Encode(value any) []byte {
	var out strings.Builder
	encode(&out, value)
	return []byte(out.String())
}

func encode(out *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		encodeString(out, v)
	case float64:
		encodeFloat(out, v)
	case float32:
		encodeFloat(out, float64(v))
	case int:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int8:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int16:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int32:
		out.WriteString(strconv.FormatInt(int64(v), 10))
	case int64:
		out.WriteString(strconv.FormatInt(v, 10))
	case uint:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint8:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint16:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint32:
		out.WriteString(strconv.FormatUint(uint64(v), 10))
	case uint64:
		out.WriteString(strconv.FormatUint(v, 10))
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			encode(out, item)
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encodeString(out, key)
			out.WriteByte(':')
			encode(out, v[key])
		}
		out.WriteByte('}')
	default:
		out.WriteString("null")
	}
}

func encodeFloat(out *strings.Builder, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		out.WriteString("null")
		return
	}
	out.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
}

func encodeString(out *strings.Builder, text string) {
	out.WriteByte('"')
	for i := 0; i < len(text); {
		c := text[i]
		if c < 0x20 || c == '"' || c == '\\' {
			switch c {
			case '"':
				out.WriteString(`\"`)
			case '\\':
				out.WriteString(`\\`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			default:
				out.WriteString(`\u00`)
				out.WriteByte(hexDigits[c>>4])
				out.WriteByte(hexDigits[c&0xF])
			}
			i++
			continue
		}
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		size, code, ok := decodeWTF8(text[i:])
		if !ok {
			out.WriteString(`\uFFFD`)
			i++
			continue
		}
		if code >= 0xD800 && code <= 0xDFFF {
			out.WriteString(`\u`)
			out.WriteByte(hexDigits[code>>12&0xF])
			out.WriteByte(hexDigits[code>>8&0xF])
			out.WriteByte(hexDigits[code>>4&0xF])
			out.WriteByte(hexDigits[code&0xF])
		} else {
			out.WriteString(text[i : i+size])
		}
		i += size
	}
	out.WriteByte('"')
}

const hexDigits = "0123456789abcdef"

// decodeWTF8 decodes one code point of WTF-8 text, allowing the three-byte lone-surrogate form.
func decodeWTF8(b string) (int, rune, bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	c := b[0]
	var size int
	var code rune
	switch {
	case c < 0x80:
		return 1, rune(c), true
	case c&0xE0 == 0xC0:
		size, code = 2, rune(c&0x1F)
	case c&0xF0 == 0xE0:
		size, code = 3, rune(c&0x0F)
	case c&0xF8 == 0xF0:
		size, code = 4, rune(c&0x07)
	default:
		return 0, 0, false
	}
	if len(b) < size {
		return 0, 0, false
	}
	for i := 1; i < size; i++ {
		if b[i]&0xC0 != 0x80 {
			return 0, 0, false
		}
		code = code<<6 | rune(b[i]&0x3F)
	}
	if size == 2 && code < 0x80 || size == 3 && code < 0x800 || size == 4 && code < 0x10000 {
		return 0, 0, false
	}
	if code > 0x10FFFF {
		return 0, 0, false
	}
	return size, code, true
}
