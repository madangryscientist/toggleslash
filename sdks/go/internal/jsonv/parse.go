package jsonv

import (
	"errors"
	"strconv"
)

// MaxDepth is the deepest nesting Parse accepts. The contract asks for at least 512.
const MaxDepth = 1000

// ErrSyntax is returned for text that is not JSON, or is nested deeper than MaxDepth.
var ErrSyntax = errors.New("invalid JSON")

// Parse reads one JSON value from data. Bytes that are not valid UTF-8 are replaced the way the WHATWG
// decoder does, a leading byte order mark is skipped, a duplicate key keeps the last value, a number
// too large reads as an infinity, and a lone surrogate escape is kept in the string.
func Parse(data []byte) (Value, error) {
	data = DecodeUTF8(data)
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	p := parser{b: data}
	p.ws()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.ws()
	if p.i != len(p.b) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	b []byte
	i int
}

func (p *parser) ws() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.b) {
		return Value{}, ErrSyntax
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Value{}, err
		}
		return NewString(s), nil
	case c == 't':
		return p.lit("true", NewBool(true))
	case c == 'f':
		return p.lit("false", NewBool(false))
	case c == 'n':
		return p.lit("null", Value{})
	case c == '-' || (c >= '0' && c <= '9'):
		return p.num()
	}
	return Value{}, ErrSyntax
}

func (p *parser) lit(word string, v Value) (Value, error) {
	if len(p.b)-p.i < len(word) || string(p.b[p.i:p.i+len(word)]) != word {
		return Value{}, ErrSyntax
	}
	p.i += len(word)
	return v, nil
}

func (p *parser) num() (Value, error) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.b) {
		return Value{}, ErrSyntax
	}
	switch {
	case p.b[p.i] == '0':
		p.i++
	case p.b[p.i] >= '1' && p.b[p.i] <= '9':
		p.digits()
	default:
		return Value{}, ErrSyntax
	}
	if p.i < len(p.b) && p.b[p.i] == '.' {
		p.i++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.i < len(p.b) && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < len(p.b) && (p.b[p.i] == '+' || p.b[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	// A range error still returns the nearest value, which is +Inf or -Inf for 1e999 and 0 for 1e-999.
	f, _ := strconv.ParseFloat(string(p.b[start:p.i]), 64)
	return NewNumber(f), nil
}

func (p *parser) digits() bool {
	n := 0
	for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
		p.i++
		n++
	}
	return n > 0
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

func (p *parser) str() (string, error) {
	p.i++ // opening quote
	start := p.i
	// Fast path: no escape and no control byte.
	for p.i < len(p.b) {
		c := p.b[p.i]
		if c == '"' {
			s := string(p.b[start:p.i])
			p.i++
			return s, nil
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.i++
	}
	out := append([]byte(nil), p.b[start:p.i]...)
	for p.i < len(p.b) {
		c := p.b[p.i]
		switch {
		case c == '"':
			p.i++
			return string(out), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			out = append(out, c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.b) {
			return "", ErrSyntax
		}
		e := p.b[p.i]
		p.i++
		switch e {
		case '"', '\\', '/':
			out = append(out, e)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r, ok := hex4(p.b[p.i:])
			if !ok {
				return "", ErrSyntax
			}
			p.i += 4
			if r >= 0xD800 && r <= 0xDBFF && p.i+1 < len(p.b) && p.b[p.i] == '\\' && p.b[p.i+1] == 'u' {
				if lo, ok := hex4(p.b[p.i+2:]); ok && lo >= 0xDC00 && lo <= 0xDFFF {
					p.i += 6
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				}
			}
			out = AppendRune(out, r)
		default:
			return "", ErrSyntax
		}
	}
	return "", ErrSyntax
}

func (p *parser) array(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	p.ws()
	elems := []Value{}
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return NewArray(elems), nil
	}
	for {
		p.ws()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		elems = append(elems, v)
		p.ws()
		if p.i >= len(p.b) {
			return Value{}, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return NewArray(elems), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	p.ws()
	members := map[string]Value{}
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return NewObject(members), nil
	}
	for {
		p.ws()
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return Value{}, ErrSyntax
		}
		k, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.ws()
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return Value{}, ErrSyntax
		}
		p.i++
		p.ws()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		members[k] = v
		p.ws()
		if p.i >= len(p.b) {
			return Value{}, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return NewObject(members), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}
