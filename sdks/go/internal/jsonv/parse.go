package jsonv

import (
	"errors"
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is the deepest nesting the reader accepts. The contract asks for at least 512.
const MaxDepth = 2048

// ErrSyntax is the one error the reader returns. It carries no text from the input, so it can be
// reported without leaking a context.
var ErrSyntax = errors.New("invalid JSON")

// ParseBody reads an HTTP body: one leading byte order mark is skipped, invalid UTF-8 becomes
// U+FFFD the WHATWG way, and the rest is read by Parse.
func ParseBody(body []byte) (Value, error) {
	return Parse(wtf8.DecodeLossy(wtf8.StripBOM(body)))
}

// Parse reads one JSON value from valid UTF-8. A number out of range is infinite, not an error; a
// lone surrogate escape is kept; a duplicate key keeps the last value.
func Parse(text []byte) (Value, error) {
	p := parser{in: text}
	p.skipSpace()
	v, ok := p.value(0)
	if !ok {
		return Value{}, ErrSyntax
	}
	p.skipSpace()
	if p.pos != len(p.in) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	in  []byte
	pos int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.in) {
		switch p.in[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) peek() int {
	if p.pos >= len(p.in) {
		return -1
	}
	return int(p.in[p.pos])
}

func (p *parser) literal(word string, v Value) (Value, bool) {
	if p.pos+len(word) > len(p.in) || string(p.in[p.pos:p.pos+len(word)]) != word {
		return Value{}, false
	}
	p.pos += len(word)
	return v, true
}

func (p *parser) value(depth int) (Value, bool) {
	if depth > MaxDepth {
		return Value{}, false
	}
	switch c := p.peek(); {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, ok := p.str()
		return NewString(s), ok
	case c == 't':
		return p.literal("true", NewBool(true))
	case c == 'f':
		return p.literal("false", NewBool(false))
	case c == 'n':
		return p.literal("null", Value{})
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, false
}

func (p *parser) object(depth int) (Value, bool) {
	p.pos++
	o := &Obj{m: map[string]Value{}}
	p.skipSpace()
	if p.peek() == '}' {
		p.pos++
		return Value{kind: Object, o: o}, true
	}
	for {
		p.skipSpace()
		if p.peek() != '"' {
			return Value{}, false
		}
		k, ok := p.str()
		if !ok {
			return Value{}, false
		}
		p.skipSpace()
		if p.peek() != ':' {
			return Value{}, false
		}
		p.pos++
		p.skipSpace()
		v, ok := p.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		o.set(k, v)
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return Value{kind: Object, o: o}, true
		default:
			return Value{}, false
		}
	}
}

func (p *parser) array(depth int) (Value, bool) {
	p.pos++
	var items []Value
	p.skipSpace()
	if p.peek() == ']' {
		p.pos++
		return Value{kind: Array, a: []Value{}}, true
	}
	for {
		p.skipSpace()
		v, ok := p.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		items = append(items, v)
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Value{kind: Array, a: items}, true
		default:
			return Value{}, false
		}
	}
}

func isDigit(c int) bool { return c >= '0' && c <= '9' }

func (p *parser) number() (Value, bool) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	switch {
	case p.peek() == '0':
		p.pos++
	case isDigit(p.peek()):
		for isDigit(p.peek()) {
			p.pos++
		}
	default:
		return Value{}, false
	}
	if p.peek() == '.' {
		p.pos++
		if !isDigit(p.peek()) {
			return Value{}, false
		}
		for isDigit(p.peek()) {
			p.pos++
		}
	}
	if c := p.peek(); c == 'e' || c == 'E' {
		p.pos++
		if c := p.peek(); c == '+' || c == '-' {
			p.pos++
		}
		if !isDigit(p.peek()) {
			return Value{}, false
		}
		for isDigit(p.peek()) {
			p.pos++
		}
	}
	f, err := strconv.ParseFloat(string(p.in[start:p.pos]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, false
		}
		// ParseFloat returns +-Inf for an overflow and 0 for an underflow; both are what the spec wants.
		if math.IsInf(f, 0) || f == 0 {
			return NewNumber(f), true
		}
		return Value{}, false
	}
	return NewNumber(f), true
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(d)
	}
	return r, true
}

// str reads a string at a quote and returns it as WTF-8.
func (p *parser) str() (string, bool) {
	p.pos++
	var buf []byte
	for {
		start := p.pos
		for p.pos < len(p.in) && p.in[p.pos] != '"' && p.in[p.pos] != '\\' && p.in[p.pos] >= 0x20 {
			p.pos++
		}
		buf = append(buf, p.in[start:p.pos]...)
		if p.pos >= len(p.in) {
			return "", false
		}
		c := p.in[p.pos]
		if c == '"' {
			p.pos++
			return string(buf), true
		}
		if c < 0x20 {
			return "", false
		}
		p.pos++ // the backslash
		if p.pos >= len(p.in) {
			return "", false
		}
		e := p.in[p.pos]
		p.pos++
		switch e {
		case '"', '\\', '/':
			buf = append(buf, e)
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
			r, ok := hex4(p.in[p.pos:])
			if !ok {
				return "", false
			}
			p.pos += 4
			if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.in) && p.in[p.pos] == '\\' && p.in[p.pos+1] == 'u' {
				if low, ok := hex4(p.in[p.pos+2:]); ok && low >= 0xDC00 && low <= 0xDFFF {
					p.pos += 6
					r = 0x10000 + (r-0xD800)<<10 + (low - 0xDC00)
				}
			}
			buf = wtf8.AppendRune(buf, r)
		default:
			return "", false
		}
	}
}
