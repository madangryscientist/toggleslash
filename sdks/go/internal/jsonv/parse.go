package jsonv

import (
	"errors"
	"math"
	"strconv"
)

// MaxDepth is how deeply arrays and objects may nest. The contract needs at least 512.
const MaxDepth = 1024

// ErrSyntax is returned for text that is not JSON, or that nests too deeply.
var ErrSyntax = errors.New("invalid JSON")

// Parse reads one JSON value from bytes. The bytes are decoded as UTF-8 the WHATWG way (invalid bytes
// become U+FFFD, one per maximal invalid subpart) after a leading byte order mark is skipped. Numbers are
// binary64, so 1e999 is +Inf. A lone surrogate written as an escape is kept (as WTF-8). A duplicate key
// keeps the last value.
func Parse(data []byte) (Value, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	p := parser{src: DecodeUTF8(data)}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	src []byte
	pos int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.src)-p.pos < len(word) || string(p.src[p.pos:p.pos+len(word)]) != word {
		return Value{}, ErrSyntax
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.src) {
		return Value{}, ErrSyntax
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Value{}, err
		}
		return Value{kind: String, s: s}, nil
	case c == 't':
		return p.literal("true", BoolValue(true))
	case c == 'f':
		return p.literal("false", BoolValue(false))
	case c == 'n':
		return p.literal("null", Value{})
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, ErrSyntax
}

func (p *parser) object(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrSyntax
	}
	p.pos++ // {
	members := NewMembers()
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return Value{kind: Object, o: members}, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return Value{}, ErrSyntax
		}
		key, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return Value{}, ErrSyntax
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		members.Set(key, v)
		p.skipSpace()
		if p.pos >= len(p.src) {
			return Value{}, ErrSyntax
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return Value{kind: Object, o: members}, nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) array(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrSyntax
	}
	p.pos++ // [
	var items []Value
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return Value{kind: Array, a: []Value{}}, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.skipSpace()
		if p.pos >= len(p.src) {
			return Value{}, ErrSyntax
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Value{kind: Array, a: items}, nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) digits() int {
	n := 0
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
		n++
	}
	return n
}

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return Value{}, ErrSyntax
	}
	if p.src[p.pos] == '0' {
		p.pos++
	} else if p.digits() == 0 {
		return Value{}, ErrSyntax
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		if p.digits() == 0 {
			return Value{}, ErrSyntax
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		if p.digits() == 0 {
			return Value{}, ErrSyntax
		}
	}
	f, err := strconv.ParseFloat(string(p.src[start:p.pos]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
		// 1e999 is Infinity, not an error; the result already is +/-Inf (or 0 on underflow).
		if math.IsNaN(f) {
			return Value{}, ErrSyntax
		}
	}
	return Value{kind: Number, n: f}, nil
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
	if len(p.src)-p.pos < 4 {
		return 0, false
	}
	var r rune
	for i := range 4 {
		h := hexValue(p.src[p.pos+i])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | rune(h)
	}
	p.pos += 4
	return r, true
}

// str reads a string whose opening quote is at p.pos.
func (p *parser) str() (string, error) {
	p.pos++ // opening quote
	var out []byte
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '"':
			if out == nil {
				s := string(p.src[start:p.pos])
				p.pos++
				return s, nil
			}
			out = append(out, p.src[start:p.pos]...)
			p.pos++
			return string(out), nil
		case c < 0x20:
			return "", ErrSyntax
		case c == '\\':
			out = append(out, p.src[start:p.pos]...)
			p.pos++
			if p.pos >= len(p.src) {
				return "", ErrSyntax
			}
			e := p.src[p.pos]
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
				if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.src) && p.src[p.pos] == '\\' && p.src[p.pos+1] == 'u' {
					save := p.pos
					p.pos += 2
					lo, ok := p.hex4()
					if ok && lo >= 0xDC00 && lo <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					} else {
						p.pos = save // the second escape stands alone
					}
				}
				out = AppendRune(out, r)
			default:
				return "", ErrSyntax
			}
			start = p.pos
		default:
			p.pos++
		}
	}
	return "", ErrSyntax
}
