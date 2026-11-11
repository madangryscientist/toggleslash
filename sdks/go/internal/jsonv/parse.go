package jsonv

import (
	"errors"
	"fmt"
	"strconv"
)

// MaxDepth is the deepest nesting Parse reads. docs/sdk-contract.md asks for at least 512.
const MaxDepth = 10000

// SyntaxError is a JSON text Parse could not read. Its message never holds any of the text, so it can go to a
// log: a ruleset can hold a customer's flag names and a context can hold an end user's data.
type SyntaxError struct {
	Offset int
	Reason string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("invalid JSON at byte %d: %s", e.Offset, e.Reason)
}

// Parse reads one JSON value from bytes that are UTF-8 (use DecodeUTF8 first for bytes from a network).
// Numbers are binary64, a duplicate name keeps its last value, and a lone surrogate escape (\ud800) is kept
// as WTF-8.
func Parse(data []byte) (Value, error) {
	p := &parser{data: data}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.skipSpace()
	if p.pos != len(p.data) {
		return Value{}, p.fail("unexpected content after the value")
	}
	return v, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) fail(reason string) error { return &SyntaxError{Offset: p.pos, Reason: reason} }

func (p *parser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.data)-p.pos < len(word) || string(p.data[p.pos:p.pos+len(word)]) != word {
		return Value{}, p.fail("bad literal")
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) value(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, p.fail("nested too deeply")
	}
	if p.pos >= len(p.data) {
		return Value{}, p.fail("unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		return String(s), err
	case c == 't':
		return p.literal("true", Bool(true))
	case c == 'f':
		return p.literal("false", Bool(false))
	case c == 'n':
		return p.literal("null", Null())
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, p.fail("unexpected character")
}

func (p *parser) array(depth int) (Value, error) {
	p.pos++
	items := []Value{}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return Array(items), nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return Value{}, p.fail("unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Array(items), nil
		default:
			return Value{}, p.fail("expected , or ]")
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	p.pos++
	o := NewObject()
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return ObjectValue(o), nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return Value{}, p.fail("expected a name")
		}
		name, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return Value{}, p.fail("expected :")
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		o.Set(name, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return Value{}, p.fail("unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return ObjectValue(o), nil
		default:
			return Value{}, p.fail("expected , or }")
		}
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
		return Value{}, p.fail("bad number")
	}
	if p.data[p.pos] == '0' {
		p.pos++
	} else {
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return Value{}, p.fail("bad number")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return Value{}, p.fail("bad number")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	f, err := strconv.ParseFloat(string(p.data[start:p.pos]), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return Value{}, p.fail("bad number")
	}
	return Number(f), nil
}

func hexValue(c byte) int {
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

func (p *parser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	var r rune
	for i := range 4 {
		h := hexValue(p.data[p.pos+i])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | rune(h)
	}
	p.pos += 4
	return r, true
}

// str reads a string starting at its opening quote.
func (p *parser) str() (string, error) {
	p.pos++
	start := p.pos
	// Fast path: no escape before the closing quote.
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '"' {
			s := string(p.data[start:p.pos])
			p.pos++
			return s, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", p.fail("control character in a string")
		}
		p.pos++
	}
	buf := append([]byte(nil), p.data[start:p.pos]...)
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(buf), nil
		case c < 0x20:
			return "", p.fail("control character in a string")
		case c != '\\':
			buf = append(buf, c)
			p.pos++
			continue
		}
		p.pos++
		if p.pos >= len(p.data) {
			return "", p.fail("unexpected end of input")
		}
		esc := p.data[p.pos]
		p.pos++
		switch esc {
		case '"', '\\', '/':
			buf = append(buf, esc)
		case 'b':
			buf = append(buf, '\b')
		case 'f':
			buf = append(buf, '\f')
		case 'n':
			buf = append(buf, '\n')
		case 'r':
			buf = append(buf, '\r')
		case 't':
			buf = append(buf, '\t')
		case 'u':
			r, ok := p.hex4()
			if !ok {
				return "", p.fail("bad \\u escape")
			}
			if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
				save := p.pos
				p.pos += 2
				low, ok := p.hex4()
				if ok && low >= 0xDC00 && low <= 0xDFFF {
					r = 0x10000 + (r-0xD800)<<10 + (low - 0xDC00)
					buf = appendRune(buf, r)
					continue
				}
				p.pos = save
			}
			if r >= 0xD800 && r <= 0xDFFF {
				buf = AppendWTF8Surrogate(buf, r)
			} else {
				buf = appendRune(buf, r)
			}
		default:
			return "", p.fail("bad escape")
		}
	}
	return "", p.fail("unexpected end of input")
}

func appendRune(dst []byte, r rune) []byte {
	switch {
	case r < 0x80:
		return append(dst, byte(r))
	case r < 0x800:
		return append(dst, 0xC0|byte(r>>6), 0x80|byte(r&0x3F))
	case r < 0x10000:
		return append(dst, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
	}
	return append(dst, 0xF0|byte(r>>18), 0x80|byte((r>>12)&0x3F), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
}
