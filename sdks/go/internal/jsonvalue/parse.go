package jsonvalue

import (
	"errors"
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is the deepest nesting the parser accepts. The contract asks for at least 512; a Go stack
// overflow is fatal, so there is a limit at all.
const MaxDepth = 10000

// ErrSyntax is the only error Parse returns. It carries no text of the input.
var ErrSyntax = errors.New("invalid JSON")

// Parse reads one JSON value (RFC 8259) from data. The bytes are first decoded as UTF-8 the WHATWG way
// (each invalid piece becomes U+FFFD) and a leading byte order mark is skipped. Numbers are binary64:
// 1e999 is +Inf, not an error. A lone surrogate escape (\ud800) is kept. A key given twice keeps its
// last value.
func Parse(data []byte) (Value, error) {
	data = wtf8.Scrub(data)
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	p := &parser{data: data}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.space()
	if p.pos != len(p.data) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) space() {
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
		return Value{}, ErrSyntax
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.data) {
		return Value{}, ErrSyntax
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
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
	return Value{}, ErrSyntax
}

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.pos++ // [
	items := []Value{}
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return Array(items), nil
	}
	for {
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.space()
		if p.pos >= len(p.data) {
			return Value{}, ErrSyntax
		}
		c := p.data[p.pos]
		p.pos++
		if c == ']' {
			return Array(items), nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.pos++ // {
	obj := NewObject()
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return ObjectValue(obj), nil
	}
	for {
		p.space()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return Value{}, ErrSyntax
		}
		key, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.space()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return Value{}, ErrSyntax
		}
		p.pos++
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		obj.Set(key, v)
		p.space()
		if p.pos >= len(p.data) {
			return Value{}, ErrSyntax
		}
		c := p.data[p.pos]
		p.pos++
		if c == '}' {
			return ObjectValue(obj), nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
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

// hex4 reads four hex digits after `\u`.
func (p *parser) hex4() (rune, bool) {
	if len(p.data)-p.pos < 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		h := hexValue(p.data[p.pos+i])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | rune(h)
	}
	p.pos += 4
	return r, true
}

// str reads a string; the opening quote is at p.pos. The result is WTF-8.
func (p *parser) str() (string, error) {
	p.pos++ // "
	start := p.pos
	// Fast path: no escape, no control character.
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '"' {
			s := string(p.data[start:p.pos])
			p.pos++
			return s, nil
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.pos++
	}
	out := append([]byte(nil), p.data[start:p.pos]...)
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			out = append(out, c)
			p.pos++
		default:
			p.pos++
			if p.pos >= len(p.data) {
				return "", ErrSyntax
			}
			e := p.data[p.pos]
			p.pos++
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
				r, ok := p.hex4()
				if !ok {
					return "", ErrSyntax
				}
				if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
					save := p.pos
					p.pos += 2
					lo, ok := p.hex4()
					if ok && lo >= 0xDC00 && lo <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					} else {
						p.pos = save // the next escape is read on its own
					}
				}
				out = wtf8.AppendRune(out, r)
			default:
				return "", ErrSyntax
			}
		}
	}
	return "", ErrSyntax
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
		return Value{}, ErrSyntax
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
			return Value{}, ErrSyntax
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
			return Value{}, ErrSyntax
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	f, err := strconv.ParseFloat(string(p.data[start:p.pos]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
		// ErrRange: f is already +Inf or -Inf (or 0 for a tiny one), which is what the spec wants.
	}
	if math.IsNaN(f) {
		return Value{}, ErrSyntax
	}
	return Number(f), nil
}
